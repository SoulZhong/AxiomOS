package directory

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

// 外部日历（ADR 0032）：注册表的第四种能力。只读——拉取某个成员在某段时间里的会议（忙闲、标题、起止、全天、链接），
// 不创建、不修改、不删除外部日历里的任何东西。
//
// 两种接法：
//   - 组织级凭据（飞书、企业微信）：用企业应用的凭据，按成员在该平台的外部身份拉取；
//   - 成员各自授权（Google Calendar）：组织登记 OAuth 客户端，成员自己授权一次，刷新令牌按成员加密存，
//     建客户端时把成员的令牌并进凭据（PerMemberCalendar 为真的提供方）。

// CalendarEvent 是外部日历里的一条会议（统一模型）。
type CalendarEvent struct {
	ExternalID string
	Title      string
	Start      time.Time
	End        time.Time
	AllDay     bool
	Busy       bool // 忙（默认）还是「空闲」类的事件
	URL        string
}

// Calendar 是外部日历客户端。externalUserID 是成员在该平台的身份（Google 为空，令牌本身就是身份）。
type Calendar interface {
	Events(ctx context.Context, externalUserID string, from, to time.Time) ([]CalendarEvent, error)
}

// CalendarSummary 是一次拉取里某一本日历的情况：叫什么、拉到几场、用的哪种查法（排查用）。
type CalendarSummary struct {
	Name   string `json:"name"`
	Events int    `json:"events"`
	Via    string `json:"via"`
}

// CalendarSummarizer 可选：拉过一次之后能逐本日历说明拉到多少（CalDAV 有几本日历，各家服务器认的查法也不一样）。
type CalendarSummarizer interface {
	Summary() []CalendarSummary
}

// CalendarDiagnoser 可选：接入检查逐项给结论。
type CalendarDiagnoser interface {
	DiagnoseCalendar(ctx context.Context) ([]Check, error)
}

// OAuthCalendar 可选：成员各自授权的提供方（Google）要能出授权地址、换令牌。
type OAuthCalendar interface {
	// AuthURL 返回让成员去授权的地址。
	AuthURL(redirectURL, state string) string
	// Exchange 用回调带回的 code 换刷新令牌，并返回该账号的邮箱（用来显示"连的是谁"）。
	Exchange(ctx context.Context, code, redirectURL string) (refreshToken, email string, err error)
}

// CanCalendar 判断能否读外部日历（ADR 0032）。
func (p Provider) CanCalendar() bool { return p.NewCalendar != nil }

// CalendarField 按键找日历凭据字段。
func (p Provider) CalendarField(key string) *CredentialField {
	for i := range p.CalendarFields {
		if p.CalendarFields[i].Key == key {
			return &p.CalendarFields[i]
		}
	}
	return nil
}

// CalendarProviders 列出全部能读外部日历的生产提供方，顺序即注册顺序（feishu、googlecal、wecom 按文件名）。
func CalendarProviders() []Provider {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Provider, 0, len(registry))
	for _, p := range registry {
		if !p.hidden && p.CanCalendar() {
			out = append(out, p)
		}
	}
	return out
}

// ---------- 测试用的内存日历 ----------

// FakeCalendarKey 是测试日历提供方的代码名。
const FakeCalendarKey = "fakecal"

// FakeCalendar 是内存日历：按外部用户放好事件；Err 非空时拉取失败。
type FakeCalendar struct {
	mu    sync.Mutex
	Items map[string][]CalendarEvent
	Err   error
	Calls int
}

// NewFakeCalendar 造一个空的内存日历。
func NewFakeCalendar() *FakeCalendar { return &FakeCalendar{Items: map[string][]CalendarEvent{}} }

// Put 给某个外部用户放一条事件。
func (f *FakeCalendar) Put(externalUserID string, ev CalendarEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Items[externalUserID] = append(f.Items[externalUserID], ev)
}

// Events 返回落在区间里的事件（按开始时间排序）。
func (f *FakeCalendar) Events(ctx context.Context, externalUserID string, from, to time.Time) ([]CalendarEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	if f.Err != nil {
		return nil, f.Err
	}
	var out []CalendarEvent
	for _, ev := range f.Items[externalUserID] {
		if ev.End.After(from) && ev.Start.Before(to) {
			out = append(out, ev)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, nil
}

// UseFakeCalendar 把一个内存日历登记成测试提供方 fakecal：凭据只有一个保密的 token，不等于 goodToken 时拉取被拒。
func UseFakeCalendar(f *FakeCalendar, goodToken string) Provider {
	p := Provider{
		Key:            FakeCalendarKey,
		Title:          i18n.T("测试日历", "Test calendar"),
		CalendarFields: []CredentialField{{Key: "token", Title: i18n.T("访问令牌", "Access token"), Secret: true}},
		CalendarPrerequisites: []i18n.Text{
			i18n.T("测试用，不需要真的日历。", "For tests; no real calendar needed."),
		},
		Tip: GuideStep{Text: i18n.T("测试用。", "For tests.")},
		NewCalendar: func(creds map[string]string, opts Options) (Calendar, error) {
			if creds["token"] != goodToken {
				return &FakeCalendar{Items: map[string][]CalendarEvent{}, Err: &RejectedError{Code: 401, Msg: "Bad credentials"}}, nil
			}
			return f, nil
		},
	}
	RegisterForTest(p)
	return p
}
