package domain

import (
	"strings"
	"testing"
	"time"
)

// 安静时段：同日区间、跨午夜区间、不在时段里。
func TestQuietUntil(t *testing.T) {
	loc := time.Local
	at := func(h, m int) time.Time { return time.Date(2026, 9, 6, h, m, 0, 0, loc) }
	q := &QuietHours{From: "22:00", To: "08:00"}
	if u := QuietUntil(q, at(23, 30)); !u.Equal(time.Date(2026, 9, 7, 8, 0, 0, 0, loc)) {
		t.Fatalf("23:30 在跨午夜的安静时段里，应顺延到次日 08:00，实际 %v", u)
	}
	if u := QuietUntil(q, at(7, 59)); !u.Equal(at(8, 0)) {
		t.Fatalf("07:59 应顺延到当天 08:00，实际 %v", u)
	}
	if u := QuietUntil(q, at(12, 0)); !u.IsZero() {
		t.Fatalf("中午不在安静时段里，实际 %v", u)
	}
	day := &QuietHours{From: "12:00", To: "14:00"}
	if u := QuietUntil(day, at(13, 0)); !u.Equal(at(14, 0)) {
		t.Fatalf("13:00 应顺延到 14:00，实际 %v", u)
	}
	if u := QuietUntil(day, at(14, 0)); !u.IsZero() {
		t.Fatal("14:00 整已经出了安静时段")
	}
	if u := QuietUntil(nil, at(13, 0)); !u.IsZero() {
		t.Fatal("没设安静时段就不顺延")
	}
	if err := ValidateQuietHours(&QuietHours{From: "25:00", To: "08:00"}); err == nil || !strings.Contains(err.Error(), "HH:MM") {
		t.Fatalf("25:00 应报格式错: %v", err)
	}
	if err := ValidateQuietHours(&QuietHours{From: "08:00", To: "08:00"}); err == nil || !strings.Contains(err.Error(), "不能相同") {
		t.Fatalf("首尾相同应报错: %v", err)
	}
}

// 个人偏好的解析与校验：rules 只认目录里的事件类型；quiet_hours 传 null 表示清掉；不认识的键报错；通道必须在允许范围内。
func TestNotifyPrefsPatch(t *testing.T) {
	p, err := ParseNotifyPrefsPatch([]byte(`{"rules":{"proposal":["feishu","feishu"],"assigned":[]},"quiet_hours":{"from":"22:00","to":"08:00"}}`))
	if err != nil || len(p.Rules["proposal"]) != 1 || p.Rules["proposal"][0] != "feishu" || len(p.Rules["assigned"]) != 0 || !p.QuietSet || p.QuietHours.From != "22:00" {
		t.Fatalf("解析不对 %+v %v", p, err)
	}
	if _, ok := p.Rules["assigned"]; !ok {
		t.Fatal("空列表也是明确设过")
	}
	if _, err := ParseNotifyPrefsPatch([]byte(`{"rules":{"foo":["feishu"]}}`)); err == nil || !strings.Contains(err.Error(), "没有「foo」这种事件类型") {
		t.Fatalf("不认识的事件类型应报错: %v", err)
	}
	if _, err := ParseNotifyPrefsPatch([]byte(`{"colour":"red"}`)); err == nil || !strings.Contains(err.Error(), "不认识的字段") {
		t.Fatalf("不认识的键应报错: %v", err)
	}
	if _, err := ParseNotifyPrefsPatch([]byte(`{}`)); err == nil {
		t.Fatal("空对象应报错")
	}
	q, err := ParseNotifyPrefsPatch([]byte(`{"quiet_hours":null}`))
	if err != nil || !q.QuietSet || q.QuietHours != nil {
		t.Fatalf("quiet_hours: null 应表示清掉 %+v %v", q, err)
	}
	title := func(k string) string { return "「" + k + "」" }
	if err := CheckRulesAllowed(map[string][]string{"proposal": {"webhook"}}, []string{"feishu", "email"}, NotifyKinds, "zh-CN", title); err == nil || !strings.Contains(err.Error(), "没有开放「webhook」通道") {
		t.Fatalf("组织没开的通道应被拒: %v", err)
	}
	if err := CheckRulesAllowed(map[string][]string{"overdue": {"email"}}, []string{"email"}, []string{"proposal"}, "zh-CN", title); err == nil || !strings.Contains(err.Error(), "组织不允许外发「逾期」") {
		t.Fatalf("组织不允许的事件应被拒: %v", err)
	}
	if err := CheckRulesAllowed(map[string][]string{"overdue": {}}, []string{"email"}, []string{"proposal"}, "zh-CN", title); err != nil {
		t.Fatalf("不允许的事件设成空列表是可以的: %v", err)
	}
	merged := MergeNotifyPrefs(NotifyPrefs{Rules: map[string][]string{"proposal": {"feishu"}}, QuietHours: &QuietHours{From: "22:00", To: "08:00"}, QuietSet: true}, q)
	if merged.QuietHours != nil || !merged.QuietSet || len(merged.Rules["proposal"]) != 1 {
		t.Fatalf("合并应清掉安静时段、保留规则 %+v", merged)
	}
}

// 默认与解析：待确认操作与等我验收走 IM（绑定了才算），否则邮件；组织不再允许的通道 / 事件被裁掉。
func TestNotifyDefaultsAndResolve(t *testing.T) {
	d := DefaultNotifyRules("feishu", true, []string{"feishu", "email"}, NotifyKinds)
	if len(d[NotifyProposal]) != 1 || d[NotifyProposal][0] != "feishu" || len(d[NotifyReview]) != 1 || len(d[NotifyAssigned]) != 0 {
		t.Fatalf("绑定了 IM 时默认走飞书 %+v", d)
	}
	d = DefaultNotifyRules("feishu", false, []string{"feishu", "email"}, NotifyKinds)
	if len(d[NotifyProposal]) != 1 || d[NotifyProposal][0] != "email" {
		t.Fatalf("没绑定时默认走邮件 %+v", d)
	}
	d = DefaultNotifyRules("", false, []string{"webhook"}, NotifyKinds)
	if len(d[NotifyProposal]) != 0 {
		t.Fatalf("既没 IM 又没邮件时只站内 %+v", d)
	}
	d = DefaultNotifyRules("feishu", true, []string{"feishu"}, []string{NotifyAssigned})
	if len(d[NotifyProposal]) != 0 {
		t.Fatalf("组织不允许待确认操作外发时默认也不给 %+v", d)
	}
	rules, sources := ResolveNotifyRules(map[string][]string{NotifyAssigned: {"feishu", "webhook"}}, DefaultNotifyRules("feishu", true, []string{"feishu"}, NotifyKinds), []string{"feishu"}, []string{NotifyAssigned, NotifyProposal})
	if sources[NotifyAssigned] != "personal" || len(rules[NotifyAssigned]) != 1 || rules[NotifyAssigned][0] != "feishu" {
		t.Fatalf("个人规则里组织没开的通道应裁掉 %+v %+v", rules, sources)
	}
	if sources[NotifyReview] != "default" || len(rules[NotifyReview]) != 0 {
		t.Fatalf("组织不再允许的事件应裁成空 %+v", rules)
	}
	if _, err := NormalizeKinds([]string{"review", "proposal", "review"}); err != nil {
		t.Fatal(err)
	}
	if ks, _ := NormalizeKinds([]string{"review", "proposal"}); len(ks) != 2 || ks[0] != NotifyProposal {
		t.Fatalf("应按目录顺序 %v", ks)
	}
	if _, err := NormalizeKinds([]string{"nope"}); err == nil {
		t.Fatal("不认识的事件类型应报错")
	}
}
