package domain

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

// 通知外发（ADR 0019）分三层：站内「待我处理」是事实源；组织决定允许哪些通道、哪些事件类型可外发；
// 个人在组织允许的范围内为每种事件选通道与安静时段。这里是纯规则：事件类型目录、组织策略与个人规则的校验、
// 默认值、安静时段的计算；不碰数据库。

// 事件类型（外发的 kind）。顺序即界面顺序。
const (
	NotifyProposal     = "proposal"      // 待确认操作
	NotifyReview       = "review"        // 等我验收
	NotifyAssigned     = "assigned"      // 指派给我
	NotifyQuestion     = "question"      // 提问
	NotifyOverdue      = "overdue"       // 逾期
	NotifyMilestoneDue = "milestone_due" // 里程碑到期
	NotifyBlocked      = "blocked"       // 任务被阻塞（外部事件触发，ADR 0020）
)

// NotifyKinds 是全部事件类型。
var NotifyKinds = []string{NotifyProposal, NotifyReview, NotifyAssigned, NotifyQuestion, NotifyBlocked, NotifyOverdue, NotifyMilestoneDue}

// NotifyKindTitle 是事件类型的显示名。
var NotifyKindTitle = map[string]i18n.Text{
	NotifyProposal:     i18n.T("待确认操作", "Pending actions"),
	NotifyReview:       i18n.T("等我验收", "Awaiting my review"),
	NotifyAssigned:     i18n.T("指派给我", "Assigned to me"),
	NotifyQuestion:     i18n.T("提问", "Questions"),
	NotifyOverdue:      i18n.T("逾期", "Overdue"),
	NotifyMilestoneDue: i18n.T("里程碑到期", "Milestone due"),
	NotifyBlocked:      i18n.T("任务被阻塞", "Task blocked"),
}

// 简单通道的代码名；IM 通道的代码名就是提供方的 key（feishu、wecom）。
const (
	ChannelEmail   = "email"
	ChannelWebhook = "webhook"
)

// IsNotifyKind 判断是不是认识的事件类型。
func IsNotifyKind(k string) bool {
	for _, x := range NotifyKinds {
		if x == k {
			return true
		}
	}
	return false
}

// NotifyError 是策略或偏好不合法：一句完整的话。
type NotifyError struct{ Msg i18n.Msg }

func (e *NotifyError) Error() string { return e.Msg.Render(i18n.Default) }

func notifyErr(key string, args ...any) error { return &NotifyError{Msg: i18n.M(key, args...)} }

// NormalizeKinds 去空白、去重、按目录顺序排；不认识的报错。
func NormalizeKinds(kinds []string) ([]string, error) {
	seen := map[string]bool{}
	for _, k := range kinds {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if !IsNotifyKind(k) {
			return nil, notifyErr("err.notify_kind_unknown", k)
		}
		seen[k] = true
	}
	out := []string{}
	for _, k := range NotifyKinds {
		if seen[k] {
			out = append(out, k)
		}
	}
	return out, nil
}

// QuietHours 是安静时段：from 到 to 之间不外发，到点再发；跨午夜（22:00–08:00）也可以。都是 HH:MM。
type QuietHours struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func parseHHMM(s string) (int, error) {
	var h, m int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d:%d", &h, &m); err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, notifyErr("err.notify_quiet_format", s)
	}
	return h*60 + m, nil
}

// ValidateQuietHours 校验时段：两端都是 HH:MM 且不相同。
func ValidateQuietHours(q *QuietHours) error {
	if q == nil {
		return nil
	}
	from, err := parseHHMM(q.From)
	if err != nil {
		return err
	}
	to, err := parseHHMM(q.To)
	if err != nil {
		return err
	}
	if from == to {
		return notifyErr("err.notify_quiet_same")
	}
	return nil
}

// QuietUntil 判断 now 是否落在安静时段里；是则返回这段安静时段结束的时刻（本地时间），否则返回零值。
func QuietUntil(q *QuietHours, now time.Time) time.Time {
	if q == nil {
		return time.Time{}
	}
	from, err1 := parseHHMM(q.From)
	to, err2 := parseHHMM(q.To)
	if err1 != nil || err2 != nil || from == to {
		return time.Time{}
	}
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	cur := now.Hour()*60 + now.Minute()
	at := func(min int, d time.Time) time.Time { return d.Add(time.Duration(min) * time.Minute) }
	if from < to {
		if cur >= from && cur < to {
			return at(to, day)
		}
		return time.Time{}
	}
	// 跨午夜：from 之后到第二天 to 之前
	if cur >= from {
		return at(to, day.AddDate(0, 0, 1))
	}
	if cur < to {
		return at(to, day)
	}
	return time.Time{}
}

// NotifyPrefs 是一个人的通知偏好（只存明确设过的部分）：每种事件走哪些通道，以及安静时段。
type NotifyPrefs struct {
	Rules      map[string][]string `json:"rules,omitempty"`
	QuietHours *QuietHours         `json:"quiet_hours,omitempty"`
	// QuietSet 为真表示安静时段是明确设过的（包括明确设为 null）。
	QuietSet bool `json:"quiet_set,omitempty"`
}

// IsEmpty 判断是否什么都没设。
func (p NotifyPrefs) IsEmpty() bool { return len(p.Rules) == 0 && !p.QuietSet }

// ParseNotifyPrefsPatch 解析 PUT /me/notifications 的请求体：rules 里出现的事件类型被设置（空列表 = 只站内），
// quiet_hours 出现（含 null）即设置。不认识的键报错。
func ParseNotifyPrefsPatch(raw []byte) (NotifyPrefs, error) {
	var in map[string]json.RawMessage
	if len(raw) == 0 {
		return NotifyPrefs{}, notifyErr("err.notify_body_empty")
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return NotifyPrefs{}, notifyErr("err.bad_json", err.Error())
	}
	out := NotifyPrefs{}
	for k, v := range in {
		switch k {
		case "rules":
			rules := map[string][]string{}
			if string(v) != "null" {
				if err := json.Unmarshal(v, &rules); err != nil {
					return NotifyPrefs{}, notifyErr("err.notify_rules_shape")
				}
			}
			out.Rules = map[string][]string{}
			for kind, chans := range rules {
				if !IsNotifyKind(kind) {
					return NotifyPrefs{}, notifyErr("err.notify_kind_unknown", kind)
				}
				out.Rules[kind] = dedupe(chans)
			}
		case "quiet_hours":
			out.QuietSet = true
			if string(v) != "null" {
				var q QuietHours
				if err := json.Unmarshal(v, &q); err != nil {
					return NotifyPrefs{}, notifyErr("err.notify_quiet_format", string(v))
				}
				if err := ValidateQuietHours(&q); err != nil {
					return NotifyPrefs{}, err
				}
				out.QuietHours = &q
			}
		default:
			return NotifyPrefs{}, notifyErr("err.notify_key_unknown", k)
		}
	}
	if out.IsEmpty() {
		return NotifyPrefs{}, notifyErr("err.notify_body_empty")
	}
	return out, nil
}

func dedupe(xs []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, x := range xs {
		x = strings.TrimSpace(x)
		if x == "" || seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	return out
}

// CheckRulesAllowed 校验个人规则在组织允许的范围内：通道必须在 channels 里（组织启用且已配置且允许），
// 事件类型必须在 allowedKinds 里（否则这一项只能是空列表）。channelTitle 用来把通道名写进那句话。
func CheckRulesAllowed(rules map[string][]string, channels []string, allowedKinds []string, loc i18n.Locale, channelTitle func(string) string) error {
	for _, kind := range NotifyKinds {
		chans, ok := rules[kind]
		if !ok || len(chans) == 0 {
			continue
		}
		if !contains(allowedKinds, kind) {
			return notifyErr("err.notify_kind_not_allowed", NotifyKindTitle[kind])
		}
		for _, c := range chans {
			if !contains(channels, c) {
				return notifyErr("err.notify_channel_not_allowed", channelTitle(c))
			}
		}
	}
	return nil
}

// MergeNotifyPrefs 把一次部分修改合到已有偏好上。
func MergeNotifyPrefs(base, in NotifyPrefs) NotifyPrefs {
	out := NotifyPrefs{Rules: map[string][]string{}, QuietHours: base.QuietHours, QuietSet: base.QuietSet}
	for k, v := range base.Rules {
		out.Rules[k] = v
	}
	for k, v := range in.Rules {
		out.Rules[k] = v
	}
	if in.QuietSet {
		out.QuietHours, out.QuietSet = in.QuietHours, true
	}
	if len(out.Rules) == 0 {
		out.Rules = nil
	}
	return out
}

// DefaultNotifyRules 是个人没设时的默认：待确认操作与等我验收走 IM 私聊（这个人绑定了 IM 身份且组织开了 IM 通道时），
// 否则走邮件（组织开了邮件时）；其余只站内。只在允许的事件类型上给默认。
func DefaultNotifyRules(imChannel string, imBound bool, channels []string, allowedKinds []string) map[string][]string {
	out := map[string][]string{}
	for _, k := range NotifyKinds {
		out[k] = []string{}
	}
	var primary []string
	if imChannel != "" && imBound && contains(channels, imChannel) {
		primary = []string{imChannel}
	} else if contains(channels, ChannelEmail) {
		primary = []string{ChannelEmail}
	}
	for _, k := range []string{NotifyProposal, NotifyReview} {
		if primary != nil && contains(allowedKinds, k) {
			out[k] = append([]string{}, primary...)
		}
	}
	return out
}

// ResolveNotifyRules 按个人 → 默认逐项解析出实际生效的规则，并把组织不再允许的通道 / 事件裁掉。
// 返回的 sources 记每种事件的来源（personal | default）。
func ResolveNotifyRules(personal map[string][]string, defaults map[string][]string, channels []string, allowedKinds []string) (rules map[string][]string, sources map[string]string) {
	rules, sources = map[string][]string{}, map[string]string{}
	for _, k := range NotifyKinds {
		chans, src := defaults[k], "default"
		if p, ok := personal[k]; ok {
			chans, src = p, "personal"
		}
		kept := []string{}
		if contains(allowedKinds, k) {
			for _, c := range chans {
				if contains(channels, c) {
					kept = append(kept, c)
				}
			}
		}
		rules[k], sources[k] = kept, src
	}
	return rules, sources
}

// SortedKeys 是给测试与展示用的稳定顺序。
func SortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
