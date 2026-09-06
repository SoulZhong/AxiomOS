package directory

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/teemo/axiomos/internal/i18n"
)

// 测试用的代码平台（ADR 0020）：只通过 RegisterForTest 登记，不会出现在生产的代码平台列表里。
// 应用层的测试用它跑「配置 → 选仓库 → 收回调 → 推进任务」的全程，不出网。

// FakeCodeKey 是测试代码平台的代码名。
const FakeCodeKey = "fakecode"

// FakeCodeHost 是内存代码平台：仓库列表摆好，回调用一个极简的载荷形状与逐字节比对的密钥头。
type FakeCodeHost struct {
	mu    sync.Mutex
	Repos []Repo
	// ListErr 非空时读仓库失败（模拟令牌被拒）。
	ListErr error
	// Hooks 是已经建好的回调：仓库全名 → 密钥。
	Hooks map[string]string
	// EnsureErr 非空时建回调失败。
	EnsureErr error
}

// NewFakeCodeHost 造一个带一个仓库的内存代码平台。
func NewFakeCodeHost() *FakeCodeHost {
	return &FakeCodeHost{Repos: []Repo{{ID: "1", FullName: "acme/app", URL: "https://code.example/acme/app"}}, Hooks: map[string]string{}}
}

// UseFakeCodeHost 把一个内存代码平台登记成测试提供方 fakecode：凭据只有一个保密的 token。
// token 不等于 goodToken 时读仓库被拒（与真实平台的时机一致）。
func UseFakeCodeHost(h *FakeCodeHost, goodToken string) Provider {
	p := Provider{
		Key:           FakeCodeKey,
		Title:         T("测试代码平台", "Test code platform"),
		Fields:        []CredentialField{{Key: "token", Title: T("访问令牌", "Access token"), Secret: true}},
		Prerequisites: []i18n.Text{T("测试用，不需要真的仓库。", "For tests; no real repository needed.")},
		Tip:           GuideStep{Text: T("测试用，不需要真的令牌。", "For tests; no real token needed.")},
		ConsoleURL:    func(map[string]string) string { return "https://code.example/settings/tokens" },
		NewCodeHost: func(creds map[string]string, opts Options) (CodeHost, error) {
			if creds["token"] != goodToken {
				bad := NewFakeCodeHost()
				bad.ListErr = &RejectedError{Code: 401, Msg: "Bad credentials"}
				return bad, nil
			}
			return h, nil
		},
	}
	RegisterForTest(p)
	return p
}

// ListRepos 返回摆好的仓库。
func (f *FakeCodeHost) ListRepos(ctx context.Context) ([]Repo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	return append([]Repo{}, f.Repos...), nil
}

// EnsureWebhook 记下这个仓库的回调密钥。
func (f *FakeCodeHost) EnsureWebhook(ctx context.Context, repo, callbackURL, secret string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.EnsureErr != nil {
		return f.EnsureErr
	}
	if f.Hooks == nil {
		f.Hooks = map[string]string{}
	}
	f.Hooks[repo] = secret
	return nil
}

// FakeCodePayload 是测试代码平台的回调载荷。
type FakeCodePayload struct {
	Event  string `json:"event"`  // 六种外部事件之一，或空（只更新链接）
	Repo   string `json:"repo"`   //
	Number int    `json:"number"` //
	Title  string `json:"title"`  //
	Branch string `json:"branch"` //
	Body   string `json:"body"`   //
	URL    string `json:"url"`    //
	Status string `json:"status"` //
	Actor  string `json:"actor"`  //
	Link   bool   `json:"link"`   // 为真表示这件事要建 / 更新一条 PR 链接
}

// VerifyWebhook 校验密钥头 X-Fake-Token 并翻译载荷。
func (f *FakeCodeHost) VerifyWebhook(h http.Header, body []byte, secret string) (CodeEvent, error) {
	if secret != "" && h.Get("X-Fake-Token") != secret {
		return CodeEvent{}, ErrBadSignature
	}
	var p FakeCodePayload
	if err := json.Unmarshal(body, &p); err != nil {
		return CodeEvent{}, fmt.Errorf("fakecode: 载荷不是 JSON: %w", err)
	}
	if p.Repo == "" {
		return CodeEvent{}, ErrNotForUs
	}
	ev := CodeEvent{Provider: FakeCodeKey, DeliveryID: h.Get("X-Fake-Delivery"), Kind: p.Event, Repo: p.Repo, Number: p.Number,
		Title: p.Title, Branch: p.Branch, Body: p.Body, URL: p.URL, Status: p.Status, ActorName: p.Actor}
	if ev.DeliveryID == "" {
		ev.DeliveryID = BodyDigest(body)
	}
	if p.Number > 0 {
		ev.ExternalID = fmt.Sprintf("%s:%s#%d", FakeCodeKey, p.Repo, p.Number)
	}
	if p.Link {
		ev.LinkKind = "pr"
	}
	return ev, nil
}

// DiagnoseCode 跑与真实平台同一套三项检查。
func (f *FakeCodeHost) DiagnoseCode(ctx context.Context, repos []string, callbackURL string) ([]Check, error) {
	return diagnoseCode(ctx, T("测试代码平台", "Test code platform"), "https://code.example/settings/tokens", repos, callbackURL, f.ListRepos,
		func(ctx context.Context, repo string) (hookState, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			s, ok := f.Hooks[repo]
			return hookState{Found: ok, Signed: s != ""}, nil
		})
}
