package app

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/directory"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// notifyTestOrg 建一个接了测试 IM 提供方的组织：fake 通道可用；乙绑定了 IM 身份 ou-yi。
func notifyTestOrg(t *testing.T, a *App, ctx context.Context) (orgID string, jia, yi *Session, fake *directory.Fake) {
	t.Helper()
	t.Setenv("SMTP_URL", "")
	t.Setenv("SMTP_FROM", "")
	orgID, jia, yi = newTestOrg(t, a, ctx)
	fake = fakeDirectoryApp(t, a)
	if _, err := a.SaveDirectoryConfig(ctx, jia, DirectoryConfigInput{Provider: strp("fake"), Credentials: creds("app_id", "cli_x", "app_secret", "app-secret-plain")}); err != nil {
		t.Fatal(err)
	}
	bindIM(t, a, ctx, orgID, yi.MemberID, "ou-yi-"+orgID)
	return
}

func bindIM(t *testing.T, a *App, ctx context.Context, orgID, memberID, ext string) {
	t.Helper()
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		return a.Store.UpsertExternalIdentity(ctx, tx, store.ExternalIdentity{OrgID: orgID, Provider: "fake", Kind: store.IdentityMember, ExternalID: ext, LocalID: memberID})
	}); err != nil {
		t.Fatal(err)
	}
}

func deliveriesOf(t *testing.T, a *App, ctx context.Context, sess *Session, kind string) []DeliveryView {
	t.Helper()
	all, err := a.MyDeliveries(ctx, sess, 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []DeliveryView
	for _, d := range all {
		if kind == "" || d.Kind == kind {
			out = append(out, d)
		}
	}
	return out
}

func runDeliveries(t *testing.T, a *App, ctx context.Context, now time.Time) DeliveryStats {
	t.Helper()
	st, err := a.RunNotificationDeliveries(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// 组织策略：默认全部允许、IM 通道接入即可用、邮件 / webhook 没配就不可用；校验事件类型、通道、字段；保密字段只显示是否已设置；动态。
func TestNotifyPolicy(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi, _ := notifyTestOrg(t, a, ctx)
	if _, err := a.GetNotifyPolicy(ctx, yi); err == nil {
		t.Fatal("无组织设置权限者不应能看通知策略")
	}
	v, err := a.GetNotifyPolicy(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.AllowedKinds) != len(domain.NotifyKinds) || len(v.Kinds) != len(domain.NotifyKinds) || v.Kinds[0].Title != "待确认操作" {
		t.Fatalf("默认全部允许 %+v", v)
	}
	if c := v.Channels["fake"]; !c.Available || !c.Configured || !c.Enabled || !c.IM || c.Health.Status != "ok" {
		t.Fatalf("接入了的 IM 通道默认可用 %+v", c)
	}
	if c := v.Channels["feishu"]; c.Available || c.Hint != "先在「IM 集成」里接入飞书，这里才能用它发消息。" {
		t.Fatalf("不是当前 IM 提供方的通道不可用并说明 %+v", c)
	}
	if c := v.Channels["email"]; c.Available || c.Configured || !c.Enabled || c.Hint != "还没配置邮件。" || len(c.Fields) != 5 {
		t.Fatalf("邮件没配就不可用 %+v", c)
	}
	if v.ChannelOrder[0] != "fake" || v.ChannelOrder[len(v.ChannelOrder)-1] != "webhook" {
		t.Fatalf("通道顺序 %v", v.ChannelOrder)
	}
	// 校验
	if _, err := a.SetNotifyPolicy(ctx, jia, NotifyPolicyInput{AllowedKinds: &[]string{"nope"}}); err == nil || !strings.Contains(err.Error(), "没有「nope」这种事件类型") {
		t.Fatalf("不认识的事件类型: %v", err)
	}
	if _, err := a.SetNotifyPolicy(ctx, jia, NotifyPolicyInput{Channels: map[string]NotifyChannelInput{"sms": {}}}); err == nil || !strings.Contains(err.Error(), "没有「sms」这个通道") {
		t.Fatalf("不认识的通道: %v", err)
	}
	if _, err := a.SetNotifyPolicy(ctx, jia, NotifyPolicyInput{Channels: map[string]NotifyChannelInput{"webhook": {Config: map[string]string{"token": "x"}}}}); err == nil || !strings.Contains(err.Error(), "Webhook通道没有「token」这个字段") {
		t.Fatalf("不认识的字段: %v", err)
	}
	if _, err := a.SetNotifyPolicy(ctx, jia, NotifyPolicyInput{}); err == nil {
		t.Fatal("空请求应报错")
	}
	// 配 webhook + 收窄允许范围
	off := false
	v, err = a.SetNotifyPolicy(ctx, jia, NotifyPolicyInput{
		Channels:     map[string]NotifyChannelInput{"webhook": {Config: map[string]string{"url": "https://hooks.example/axiom", "secret": "s3"}}, "email": {Enabled: &off}},
		AllowedKinds: &[]string{"assigned", "proposal", "overdue"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if c := v.Channels["webhook"]; !c.Available || c.Config["url"] != "https://hooks.example/axiom" || !c.SecretsSet["secret"] || c.Config["secret"] != "" {
		t.Fatalf("webhook 应可用且不回显密钥 %+v", c)
	}
	if c := v.Channels["email"]; c.Enabled {
		t.Fatalf("邮件应已关闭 %+v", c)
	}
	if strings.Join(v.AllowedKinds, ",") != "proposal,assigned,overdue" {
		t.Fatalf("允许范围应按目录顺序 %v", v.AllowedKinds)
	}
	// 再存一次不带密钥：密钥保留
	v, err = a.SetNotifyPolicy(ctx, jia, NotifyPolicyInput{Channels: map[string]NotifyChannelInput{"webhook": {Config: map[string]string{"url": "https://hooks.example/v2", "secret": ""}}}})
	if err != nil || !v.Channels["webhook"].SecretsSet["secret"] || v.Channels["webhook"].Config["url"] != "https://hooks.example/v2" {
		t.Fatalf("省略密钥应保留 %+v %v", v.Channels["webhook"], err)
	}
	ev := eventTypes(t, a, ctx, orgID)
	if ev["NotificationChannelConfigured"] != 3 || ev["NotificationPolicyChanged"] != 1 {
		t.Fatalf("应有动态 %+v", ev)
	}
}

// 个人偏好：默认（绑定了 IM → 待确认操作与等我验收走 IM；没绑定且没邮件 → 只站内）；只能在组织允许的范围内选；重置；Agent 403。
func TestNotifyPersonalRules(t *testing.T) {
	a, ctx := testApp(t)
	_, jia, yi, _ := notifyTestOrg(t, a, ctx)
	v, err := a.MyNotifications(ctx, yi)
	if err != nil {
		t.Fatal(err)
	}
	if v.Source != "default" || len(v.Rules["proposal"]) != 1 || v.Rules["proposal"][0] != "fake" || len(v.Rules["review"]) != 1 || len(v.Rules["assigned"]) != 0 || v.Sources["proposal"] != "default" {
		t.Fatalf("绑定了 IM 的人默认待确认操作与验收走 IM %+v", v)
	}
	if len(v.AvailableChannels) != 1 || v.AvailableChannels[0].Key != "fake" || !v.AvailableChannels[0].Bound || v.AvailableChannels[0].Title != "测试目录" {
		t.Fatalf("可选通道只有接入了的 IM %+v", v.AvailableChannels)
	}
	jv, err := a.MyNotifications(ctx, jia)
	if err != nil || len(jv.Rules["proposal"]) != 0 || jv.AvailableChannels[0].Bound || !strings.Contains(jv.AvailableChannels[0].Hint, "还没有绑定测试目录身份") {
		t.Fatalf("没绑定的人默认只站内并提示绑定 %+v %v", jv, err)
	}
	// 校验
	if _, err := a.SetMyNotifications(ctx, yi, []byte(`{"rules":{"assigned":["email"]}}`)); err == nil || !strings.Contains(err.Error(), "组织没有开放邮件通道") {
		t.Fatalf("组织没开的通道: %v", err)
	}
	if _, err := a.SetNotifyPolicy(ctx, jia, NotifyPolicyInput{AllowedKinds: &[]string{"proposal", "assigned", "review"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetMyNotifications(ctx, yi, []byte(`{"rules":{"overdue":["fake"]}}`)); err == nil || !strings.Contains(err.Error(), "组织不允许外发「逾期」") {
		t.Fatalf("组织不允许的事件: %v", err)
	}
	if _, err := a.SetMyNotifications(ctx, yi, []byte(`{"rules":{"assigned":["fake"]},"quiet_hours":{"from":"22:00","to":"08:00"}}`)); err != nil {
		t.Fatal(err)
	}
	v, _ = a.MyNotifications(ctx, yi)
	if v.Source != "personal" || v.Sources["assigned"] != "personal" || v.Sources["proposal"] != "default" || v.QuietHours == nil || v.QuietHours.From != "22:00" || len(v.Rules["assigned"]) != 1 {
		t.Fatalf("个人规则应叠在默认上 %+v", v)
	}
	// 组织收窄后个人规则被裁掉但记录还在
	if _, err := a.SetNotifyPolicy(ctx, jia, NotifyPolicyInput{AllowedKinds: &[]string{"proposal"}}); err != nil {
		t.Fatal(err)
	}
	v, _ = a.MyNotifications(ctx, yi)
	if len(v.Rules["assigned"]) != 0 || v.Sources["assigned"] != "personal" {
		t.Fatalf("组织不再允许的事件应裁成空 %+v", v)
	}
	if v, err := a.ClearMyNotifications(ctx, yi); err != nil || v.Source != "default" || v.QuietHours != nil {
		t.Fatalf("重置后回到默认 %+v %v", v, err)
	}
	if _, err := a.SetMyNotifications(ctx, yi, []byte(`{"quiet_hours":{"from":"9:00","to":"9:00"}}`)); err == nil || !strings.Contains(err.Error(), "不能相同") {
		t.Fatalf("安静时段校验: %v", err)
	}
	_, agent := agentSessionFor(t, a, ctx, yi, "乙的 Agent", map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantDirect})
	if _, err := a.MyNotifications(ctx, agent); err == nil {
		t.Fatal("Agent 没有通知偏好")
	}
}

// 排队与投递：指派 → 站内通知 + 一条排队投递 → 巡检发出（卡片内容、直达链接）→ 失败退避重试 → 三次失败后记失败、
// 通道亮黄 → 安静时段顺延 → 没绑定的人记为未发 → 待确认操作走默认规则。
func TestNotifyDeliveryWorker(t *testing.T) {
	a, ctx := testApp(t)
	a.PublicURL = "https://axiom.example"
	orgID, jia, yi, fake := notifyTestOrg(t, a, ctx)
	ext := "ou-yi-" + orgID
	if _, err := a.SetMyNotifications(ctx, yi, []byte(`{"rules":{"assigned":["fake"]}}`)); err != nil {
		t.Fatal(err)
	}
	goal, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "目标"})
	if err != nil {
		t.Fatal(err)
	}
	newTask := func(title string) *domain.Task {
		t.Helper()
		task, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: goal.ID, Title: title, AssigneeID: yi.MemberID, Ready: true})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	task := newTask("写文档")
	ds := deliveriesOf(t, a, ctx, yi, "assigned")
	if len(ds) != 1 || ds[0].Status != "queued" || ds[0].Channel != "fake" || ds[0].URL != "https://axiom.example/tasks/"+task.ID+"/" || ds[0].Title != "你有一个新任务：写文档" || ds[0].KindTitle != "指派给我" || ds[0].StatusTitle != "排队中" {
		t.Fatalf("指派后应排一条投递 %+v", ds)
	}
	now := time.Now()
	st := runDeliveries(t, a, ctx, now)
	if st.Sent < 1 {
		t.Fatalf("应发出 %+v", st)
	}
	sent := fake.SentTo(ext)
	if len(sent) != 1 || sent[0].Kind != "assigned" || sent[0].URL != ds[0].URL || sent[0].Text != "由 甲 指派" || sent[0].Open != "打开" || sent[0].Recipient.Name != "乙" || sent[0].Recipient.ID != yi.MemberID {
		t.Fatalf("发出的消息 %+v", sent)
	}
	ds = deliveriesOf(t, a, ctx, yi, "assigned")
	if ds[0].Status != "sent" || ds[0].SentAt == nil || ds[0].Attempts != 1 {
		t.Fatalf("投递应记为已发出 %+v", ds[0])
	}
	// 再跑一次不会重发
	runDeliveries(t, a, ctx, now)
	if len(fake.SentTo(ext)) != 1 {
		t.Fatal("已发出的不重发")
	}

	// 失败一次后退避重试
	fake.FailNextSends(1, &directory.RejectedError{Code: 429, Msg: "rate limited (code 429)"})
	task2 := newTask("第二个")
	now = time.Now()
	st = runDeliveries(t, a, ctx, now)
	if st.Retried != 1 {
		t.Fatalf("第一次失败应重试 %+v", st)
	}
	d2 := deliveriesOf(t, a, ctx, yi, "assigned")[0]
	if d2.Status != "queued" || d2.Attempts != 1 || d2.Error != "rate limited (code 429)" || !strings.Contains(d2.URL, task2.ID) {
		t.Fatalf("重试中的投递 %+v", d2)
	}
	if st := runDeliveries(t, a, ctx, now.Add(10*time.Second)); st.Sent != 0 {
		t.Fatalf("退避期内不该发 %+v", st)
	}
	if st := runDeliveries(t, a, ctx, now.Add(31*time.Second)); st.Sent != 1 {
		t.Fatalf("退避后应发出 %+v", st)
	}
	if len(fake.SentTo(ext)) != 2 {
		t.Fatal("重试成功后应有两条")
	}

	// 一直失败：三次后记为失败；三条连续失败 → 通道异常，检查清单亮黄并带原话
	fake.SetSendErr(&directory.RejectedError{Code: 99991672, Msg: "no permission im:message"})
	for _, title := range []string{"甲", "乙", "丙"} {
		newTask("失败 " + title)
	}
	base := now.Add(time.Hour)
	runDeliveries(t, a, ctx, base)
	runDeliveries(t, a, ctx, base.Add(31*time.Second))
	st = runDeliveries(t, a, ctx, base.Add(3*time.Minute))
	if st.Failed != 3 {
		t.Fatalf("三次后应记失败 %+v", st)
	}
	failed, err := a.OrgDeliveries(ctx, jia, 10, "fake", "failed")
	if err != nil || len(failed) != 3 || failed[0].Attempts != 3 || failed[0].Error != "no permission im:message" || failed[0].Recipient == nil || failed[0].Recipient.Name != "乙" {
		t.Fatalf("组织侧应看到失败的投递 %+v %v", failed, err)
	}
	pv, _ := a.GetNotifyPolicy(ctx, jia)
	if h := pv.Health["fake"]; h.Status != "degraded" || h.Streak != 3 || h.LastError != "no permission im:message" {
		t.Fatalf("连续失败应亮黄 %+v", h)
	}
	cl, err := a.DirectoryChecklist(ctx, jia)
	if err != nil || checkStatus(cl, "messaging") != "todo" || !cl.Ready {
		t.Fatalf("检查清单的发消息项应待处理且不阻塞 %+v %v", cl, err)
	}
	for _, c := range cl.Checks {
		if c.Key == "messaging" && (!strings.Contains(c.Detail, "连续 3 次") || !strings.Contains(c.Detail, "no permission im:message") || c.Blocking) {
			t.Fatalf("发消息项应带原话 %+v", c)
		}
	}
	fake.SetSendErr(nil)
	if _, err := a.OrgDeliveries(ctx, yi, 10, "", ""); err == nil {
		t.Fatal("无权限者不能看组织投递")
	}

	// 安静时段：排队时就顺延到时段结束，巡检不发
	now = time.Now()
	from, to := now.Add(-time.Hour).Format("15:04"), now.Add(time.Hour).Format("15:04")
	if _, err := a.SetMyNotifications(ctx, yi, []byte(`{"quiet_hours":{"from":"`+from+`","to":"`+to+`"}}`)); err != nil {
		t.Fatal(err)
	}
	quiet := newTask("安静")
	runDeliveries(t, a, ctx, time.Now())
	qd := deliveriesOf(t, a, ctx, yi, "assigned")[0]
	if qd.Status != "queued" || !strings.Contains(qd.URL, quiet.ID) {
		t.Fatalf("安静时段内不该发 %+v", qd)
	}
	until := domain.QuietUntil(&domain.QuietHours{From: from, To: to}, now)
	if st := runDeliveries(t, a, ctx, until.Add(time.Minute)); st.Sent != 1 {
		t.Fatalf("安静时段过后应发 %+v", st)
	}
	if _, err := a.SetMyNotifications(ctx, yi, []byte(`{"quiet_hours":null}`)); err != nil {
		t.Fatal(err)
	}

	// 待确认操作：乙的 Agent 发起 → 乙按默认规则收 IM，链接是首页；甲验收提醒：甲没绑定 → 记为未发
	_, agent := agentSessionFor(t, a, ctx, yi, "乙的 Agent", map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantWithApproval})
	ptask := newTask("需要确认")
	if _, err := a.Transition(ctx, agent, ptask.ID, "start", TransitionPayload{}); err == nil {
		t.Fatal("应是待确认操作")
	}
	pd := deliveriesOf(t, a, ctx, yi, "proposal")
	if len(pd) != 1 || pd[0].URL != "https://axiom.example/" || !strings.Contains(pd[0].Title, "提交了一个待确认操作") {
		t.Fatalf("待确认操作应按默认规则排 IM %+v", pd)
	}
	if _, err := a.SetMyNotifications(ctx, jia, []byte(`{"rules":{"review":["fake"]}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, yi, ptask.ID, "start", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddArtifact(ctx, yi, ptask.ID, domain.Artifact{Type: "result", Title: "文档"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, yi, ptask.ID, "submit", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	runDeliveries(t, a, ctx, time.Now())
	rd := deliveriesOf(t, a, ctx, jia, "review")
	if len(rd) != 1 || rd[0].Status != "skipped" || rd[0].Error != "这个成员没有绑定测试目录身份，收不到测试目录消息。" {
		t.Fatalf("没绑定的人应记为未发并说明 %+v", rd)
	}
	ev := eventTypes(t, a, ctx, orgID)
	if ev["NotificationPreferencesUpdated"] < 3 {
		t.Fatalf("个人偏好改动应有动态 %+v", ev)
	}
}

// 测试消息：没绑定 → 一句说明；绑定后 → 发到并记一条 kind=test 的投递；通道没配置 → 400。
func TestNotifyChannelTest(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, _, fake := notifyTestOrg(t, a, ctx)
	res, err := a.TestNotifyChannel(ctx, jia, "fake")
	if err != nil || res.OK || res.Message != "这个成员没有绑定测试目录身份，收不到测试目录消息。" || res.Delivery.Status != "skipped" {
		t.Fatalf("没绑定 %+v %v", res, err)
	}
	bindIM(t, a, ctx, orgID, jia.MemberID, "ou-jia-"+orgID)
	res, err = a.TestNotifyChannel(ctx, jia, "fake")
	if err != nil || !res.OK || res.Message != "已通过测试目录发给你，去看看。" || res.Delivery.Kind != "test" || res.Delivery.Status != "sent" {
		t.Fatalf("绑定后 %+v %v", res, err)
	}
	if sent := fake.SentTo("ou-jia-" + orgID); len(sent) != 1 || sent[0].Title != "AxiomOS 通知测试" || !strings.Contains(sent[0].Text, "甲 在组织设置里点了") {
		t.Fatalf("测试消息 %+v", sent)
	}
	fake.SetSendErr(&directory.RejectedError{Code: 1, Msg: "boom"})
	res, err = a.TestNotifyChannel(ctx, jia, "fake")
	if err != nil || res.OK || res.Message != "测试目录没发出去：boom" || res.Delivery.Status != "failed" {
		t.Fatalf("失败要带原话 %+v %v", res, err)
	}
	fake.SetSendErr(nil)
	if _, err := a.TestNotifyChannel(ctx, jia, "webhook"); err == nil || !strings.Contains(err.Error(), "Webhook通道还不能用：还没配置Webhook。") {
		t.Fatalf("没配置的通道: %v", err)
	}
	if _, err := a.TestNotifyChannel(ctx, jia, "x"); err == nil {
		t.Fatal("不认识的通道")
	}
	all, _ := a.OrgDeliveries(ctx, jia, 10, "", "")
	if len(all) != 3 || all[0].KindTitle != "测试" {
		t.Fatalf("三条测试投递 %+v", all)
	}
}

// 逾期任务与到期里程碑的提醒：同一事项只排一次；只提醒最近几天内的；标题带序号与天数。
func TestNotifyDueReminders(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi, fake := notifyTestOrg(t, a, ctx)
	bindIM(t, a, ctx, orgID, jia.MemberID, "ou-jia-"+orgID)
	if _, err := a.SetMyNotifications(ctx, yi, []byte(`{"rules":{"overdue":["fake"]}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetMyNotifications(ctx, jia, []byte(`{"rules":{"milestone_due":["fake"]}}`)); err != nil {
		t.Fatal(err)
	}
	goal, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "目标"})
	if err != nil {
		t.Fatal(err)
	}
	yesterday := startOfDay(time.Now()).AddDate(0, 0, -1)
	old := startOfDay(time.Now()).AddDate(0, 0, -30)
	late, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: goal.ID, Title: "晚了", AssigneeID: yi.MemberID, Ready: true, PlannedEnd: &yesterday})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: goal.ID, Title: "老账", AssigneeID: yi.MemberID, Ready: true, PlannedEnd: &old}); err != nil {
		t.Fatal(err)
	}
	today := startOfDay(time.Now())
	if _, err := a.CreateMilestone(ctx, jia, CreateMilestoneInput{GoalID: goal.ID, Title: "发布", DueOn: &today}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := a.EnqueueDueReminders(ctx, orgID, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	od := deliveriesOf(t, a, ctx, yi, "overdue")
	if len(od) != 1 || od[0].Title != "#"+strconv.Itoa(late.Number)+"「晚了」已逾期 1 天" || !strings.Contains(od[0].URL, late.ID) {
		t.Fatalf("逾期提醒只排一次、不补老账 %+v", od)
	}
	md := deliveriesOf(t, a, ctx, jia, "milestone_due")
	if len(md) != 1 || !strings.Contains(md[0].Title, "里程碑「发布」今天到期") || !strings.HasSuffix(md[0].URL, "/goals/"+goal.ID+"/") {
		t.Fatalf("里程碑到期提醒 %+v", md)
	}
	runDeliveries(t, a, ctx, time.Now())
	if len(fake.SentTo("ou-jia-"+orgID)) != 1 || len(fake.SentTo("ou-yi-"+orgID)) != 1 {
		t.Fatal("两条提醒都应发出")
	}
}

// 投递记录里的提供方原话要翻成人话，飞书缺发消息权限时带上它原话里的开通链接。
func TestFriendlyDeliveryError(t *testing.T) {
	raw := `Access denied. One of the following scopes is required: [im:message:send, im:message].应用尚未开通所需的应用身份权限：[im:message:send]，点击链接申请并开通任一权限即可：https://open.feishu.cn/app/cli_x/auth?q=im:message:send&op_from=openapi&token_type=tenant (code 99991672)`
	msg, fix, ok := friendlyDeliveryError("feishu", raw, i18n.ZhCN)
	if !ok || !strings.Contains(msg, "以应用的身份发消息") || fix != "https://open.feishu.cn/app/cli_x/auth?q=im:message:send&op_from=openapi&token_type=tenant" {
		t.Fatalf("应翻成人话并挑出开通链接: %q %q %v", msg, fix, ok)
	}
	if _, _, ok := friendlyDeliveryError("feishu", "something else", i18n.ZhCN); ok {
		t.Fatal("认不出的原话应原样保留")
	}
	if msg, _, ok := friendlyDeliveryError("wecom", "unreachable: dial tcp: i/o timeout", i18n.ZhCN); !ok || !strings.Contains(msg, "连不上提供方") {
		t.Fatalf("连不上应翻成人话: %q", msg)
	}
}
