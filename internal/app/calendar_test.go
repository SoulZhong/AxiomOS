package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/directory"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/store"
)

// 日程与外部日历（ADR 0032）端到端：连接一家外部日历（内存假服务）→ 同步 → 我的日程里能看到任务、目标、里程碑与会议；
// 别人看我的会议只见「忙」；团队日程按成员并排；Agent 看所有者的；断开后会议消失；只看不做一行不落。
func TestScheduleAndCalendarSync(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)
	fake := directory.NewFakeCalendar()
	directory.UseFakeCalendar(fake, "good-token")

	day := func(n int) *time.Time {
		d := time.Now().AddDate(0, 0, n).Truncate(24 * time.Hour)
		return &d
	}
	// 乙负责一个任务（有计划起止）、一个只有截止日的任务、一个目标（有计划起止 + 里程碑）
	if _, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: "写方案", AssigneeID: yi.MemberID, PlannedStart: day(1), PlannedEnd: day(3), Ready: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: "交报告", AssigneeID: yi.MemberID, PlannedEnd: day(5), Ready: true}); err != nil {
		t.Fatal(err)
	}
	goal, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "本周目标", OwnerMemberID: yi.MemberID, PlannedStart: day(0), PlannedEnd: day(6)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateMilestone(ctx, jia, CreateMilestoneInput{GoalID: goal.ID, Title: "中点", DueOn: day(3)}); err != nil {
		t.Fatal(err)
	}
	// 乙的外部身份（外部目录同步时会记下）与假日历里的两场会议
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		return a.Store.UpsertExternalIdentity(ctx, tx, store.ExternalIdentity{OrgID: orgID, Provider: directory.FakeCalendarKey, Kind: "member", ExternalID: "ext-yi", LocalID: yi.MemberID})
	}); err != nil {
		t.Fatal(err)
	}
	meet := day(2).Add(10 * time.Hour)
	fake.Put("ext-yi", directory.CalendarEvent{ExternalID: "e1", Title: "周会", Start: meet, End: meet.Add(time.Hour), Busy: true, URL: "https://cal.example/e1"})
	fake.Put("ext-yi", directory.CalendarEvent{ExternalID: "e2", Title: "年假", Start: *day(4), End: *day(5), AllDay: true, Busy: true})

	// 只有组织设置权限的人能连；Agent 与普通成员不能
	if _, err := a.SaveCalendar(ctx, yi, CalendarInput{Provider: directory.FakeCalendarKey, Credentials: map[string]string{"token": "good-token"}}); err == nil {
		t.Fatal("没有组织设置权限的人不能连外部日历")
	}
	// 只看不做：一行不落
	if _, err := a.SaveCalendar(ctx, jia.WithWrite(WriteOptions{DryRun: true}), CalendarInput{Provider: directory.FakeCalendarKey, Credentials: map[string]string{"token": "good-token"}}); err == nil {
		t.Fatal("只看不做应返回预演")
	} else if _, ok := AsDryRun(err); !ok {
		t.Fatalf("应是预演结果，实际 %v", err)
	}
	if cs, _ := a.ListCalendars(ctx, jia); len(cs.Providers) == 0 {
		t.Fatal("应列出提供方")
	} else {
		for _, p := range cs.Providers {
			if p.Provider == directory.FakeCalendarKey && p.Configured {
				t.Fatal("只看不做不该存下配置")
			}
		}
	}
	// 错的令牌：接入检查说清楚；对的令牌：通过
	if _, err := a.SaveCalendar(ctx, jia, CalendarInput{Provider: directory.FakeCalendarKey, Credentials: map[string]string{"token": "bad"}}); err != nil {
		t.Fatal(err)
	}
	if res, err := a.TestCalendar(ctx, jia, directory.FakeCalendarKey); err != nil || res.OK || res.Error == "" {
		t.Fatalf("错的令牌接入检查应失败并说明原因，实际 %+v %v", res, err)
	}
	v, err := a.SaveCalendar(ctx, jia, CalendarInput{Provider: directory.FakeCalendarKey, Credentials: map[string]string{"token": "good-token"}})
	if err != nil {
		t.Fatal(err)
	}
	if !v.Configured || !v.SecretsSet["token"] || v.Credentials["token"] != "" {
		t.Fatalf("保存后应显示已配置、保密字段只显示是否已设置，实际 %+v", v)
	}
	if res, err := a.TestCalendar(ctx, jia, directory.FakeCalendarKey); err != nil || !res.OK {
		t.Fatalf("对的令牌接入检查应通过，实际 %+v %v", res, err)
	}
	// 同步：一个成员，两场会议
	sync, err := a.SyncCalendar(ctx, jia, directory.FakeCalendarKey)
	if err != nil {
		t.Fatal(err)
	}
	if sync.Members != 1 || sync.Events != 2 || sync.Status != "ok" {
		t.Fatalf("同步结果不对：%+v", sync)
	}
	if hasEventOfType(t, a, ctx, orgID, "CalendarSyncRan") == false {
		t.Fatal("同步应留下动态")
	}

	// 乙看自己的日程：任务 2、目标 1、里程碑 1、会议 2（标题可见）
	sched, err := a.ScheduleOf(ctx, yi, "me", *day(0), *day(6))
	if err != nil {
		t.Fatal(err)
	}
	count := map[string]int{}
	for _, it := range sched.Items {
		count[it.Kind]++
		if it.Kind == "event" && (it.TitleHidden || it.Title == "") {
			t.Fatalf("自己的会议应看到标题：%+v", it)
		}
		if it.Kind == "task" && it.Title == "交报告" && (!it.Deadline || it.Date != day(5).Format("2006-01-02")) {
			t.Fatalf("只有截止日的任务应画成一枚截止标记：%+v", it)
		}
	}
	if count["task"] != 2 || count["goal"] != 1 || count["milestone"] != 1 || count["event"] != 2 {
		t.Fatalf("日程条数不对：%v", count)
	}
	if len(sched.Sources) != 1 || sched.Sources[0] != directory.FakeCalendarKey {
		t.Fatalf("应标出会议来源：%v", sched.Sources)
	}
	// 甲（组织负责人）看乙的日程：会议只见「忙」，没有标题与链接
	other, err := a.ScheduleOf(ctx, jia, yi.MemberID, *day(0), *day(6))
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range other.Items {
		if it.Kind == "event" && (!it.TitleHidden || it.URL != "" || it.Title == "周会") {
			t.Fatalf("别人的会议应只画成忙：%+v", it)
		}
	}
	// 团队日程：把乙放进一个团队，按团队看能看到乙的
	var team *domain.Team
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		team = &domain.Team{OrgID: orgID, Name: "日程组"}
		if err := a.Store.CreateTeam(ctx, tx, team); err != nil {
			return err
		}
		return a.Store.AddTeamMember(ctx, tx, orgID, team.ID, yi.MemberID)
	}); err != nil {
		t.Fatal(err)
	}
	ts, err := a.ScheduleOf(ctx, jia, team.ID, *day(0), *day(6))
	if err != nil {
		t.Fatal(err)
	}
	if len(ts.Members) != 1 || ts.Members[0].ID != yi.MemberID || len(ts.Items) != len(sched.Items) {
		t.Fatalf("团队日程应含乙的全部安排，实际 %d 人 %d 条", len(ts.Members), len(ts.Items))
	}
	// 超过 62 天、结束早于开始都拒
	// 整个组织（who = 组织 ID）：能看整个组织的人（甲是负责人）看到全部在职成员
	whole, err := a.ScheduleOf(ctx, jia, orgID, *day(0), *day(6))
	if err != nil || len(whole.Members) < len(ts.Members) || len(whole.Items) < len(ts.Items) {
		t.Fatalf("整个组织的日程至少包含团队的：members=%d/%d items=%d/%d %v", len(whole.Members), len(ts.Members), len(whole.Items), len(ts.Items), err)
	}
	if _, err := a.ScheduleOf(ctx, yi, "me", *day(0), *day(62)); err == nil || !strings.Contains(err.Error(), "62") {
		t.Fatalf("含首尾 63 天应被拒，实际 %v", err)
	}
	if _, err := a.ScheduleOf(ctx, yi, "me", *day(0), *day(61)); err != nil {
		t.Fatalf("含首尾 62 天应允许，实际 %v", err)
	}
	if f, tt := CurrentWeek(time.Date(2026, 9, 10, 15, 0, 0, 0, time.Local)); f.Weekday() != time.Monday || tt.Weekday() != time.Sunday || f.Day() != 7 || tt.Day() != 13 {
		t.Fatalf("本周应是周一到周日，实际 %v – %v", f, tt)
	}
	if _, err := a.ScheduleOf(ctx, yi, "me", *day(3), *day(1)); err == nil {
		t.Fatal("结束早于开始应被拒")
	}
	// Agent 看所有者的日程
	_, ag := agentSessionFor(t, a, ctx, yi, "乙的 Agent", map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantDirect})
	mine, err := a.MyScheduleOf(ctx, ag, *day(0), *day(6))
	if err != nil || len(mine.Items) != len(sched.Items) {
		t.Fatalf("Agent 应看到所有者的日程，实际 %v %d", err, len(mine.Items))
	}
	// 成员自己看连接情况：沿用外部目录身份，算已连接
	my, err := a.MyCalendars(ctx, yi)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range my {
		if c.Provider == directory.FakeCalendarKey {
			found = true
			if !c.OrgConfigured || !c.Connected || c.ExternalID != "ext-yi" {
				t.Fatalf("成员应看到已连接：%+v", c)
			}
		}
	}
	if !found {
		t.Fatal("成员应看到测试提供方")
	}
	// 断开：连接、会议一起清掉
	if err := a.DisconnectCalendar(ctx, jia, directory.FakeCalendarKey); err != nil {
		t.Fatal(err)
	}
	after, _ := a.ScheduleOf(ctx, yi, "me", *day(0), *day(6))
	for _, it := range after.Items {
		if it.Kind == "event" {
			t.Fatal("断开后不该还有会议")
		}
	}
}

// 后台同步：到点才跑，跑过之后 15 分钟内不再跑；拉取失败记在连接上、状态是失败。
func TestScheduledCalendarSyncs(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)
	fake := directory.NewFakeCalendar()
	directory.UseFakeCalendar(fake, "good-token")
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		return a.Store.UpsertExternalIdentity(ctx, tx, store.ExternalIdentity{OrgID: orgID, Provider: directory.FakeCalendarKey, Kind: "member", ExternalID: "ext-yi", LocalID: yi.MemberID})
	}); err != nil {
		t.Fatal(err)
	}
	// 测试库里留着以前跑出来的组织，它们的假日历也会到点；先把这些跑掉，再连本组织的，计数才只算这一个
	a.RunScheduledCalendarSyncs(ctx, time.Now())
	if _, err := a.SaveCalendar(ctx, jia, CalendarInput{Provider: directory.FakeCalendarKey, Credentials: map[string]string{"token": "good-token"}}); err != nil {
		t.Fatal(err)
	}
	before := fake.Calls
	a.RunScheduledCalendarSyncs(ctx, time.Now())
	if fake.Calls != before+1 {
		t.Fatalf("第一次巡检应同步一次，实际调用 %d 次", fake.Calls-before)
	}
	a.RunScheduledCalendarSyncs(ctx, time.Now())
	if fake.Calls != before+1 {
		t.Fatal("15 分钟内不该再同步")
	}
	fake.Err = &directory.RejectedError{Code: 403, Msg: "no permission"}
	a.RunScheduledCalendarSyncs(ctx, time.Now().Add(time.Hour))
	cs, err := a.ListCalendars(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range cs.Providers {
		if p.Provider == directory.FakeCalendarKey && (p.LastStatus != "failed" || !strings.Contains(p.LastError, "no permission")) {
			t.Fatalf("失败原因应记在连接上：%+v", p)
		}
	}
}

// 日历订阅链接：成员自己贴链接就能连（不用组织配置）、立刻同步、状态记在自己的绑定上、链接不回显；
// 对外订阅源：生成链接后公开拉取得到 iCalendar，里面有他的任务与目标；换链接旧的失效；停用后 404。
func TestICSSubscriptionAndFeed(t *testing.T) {
	t.Setenv("AXIOMOS_ALLOW_PRIVATE_EGRESS", "1")
	a, ctx := testApp(t)
	_, jia, yi := newTestOrg(t, a, ctx)
	day := func(n int) *time.Time {
		d := time.Now().AddDate(0, 0, n).Truncate(24 * time.Hour)
		return &d
	}
	ics := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nX-WR-CALNAME:乙的日历\r\nBEGIN:VEVENT\r\nUID:e1@x\r\nSUMMARY:例会\r\nDTSTART:" +
		day(1).UTC().Add(2*time.Hour).Format("20060102T150405Z") + "\r\nDTEND:" + day(1).UTC().Add(3*time.Hour).Format("20060102T150405Z") + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/secret.ics" {
			_, _ = w.Write([]byte(ics))
			return
		}
		_, _ = w.Write([]byte("<html>"))
	}))
	defer srv.Close()

	// 不是 ICS 的链接被拒，且拒绝理由是整句
	if _, err := a.ConnectMyCalendar(ctx, yi, directory.ICSKey, map[string]string{"url": srv.URL + "/page"}); err == nil || !strings.Contains(err.Error(), "不是日历文件") {
		t.Fatalf("网页链接应被拒，实际 %v", err)
	}
	v, err := a.ConnectMyCalendar(ctx, yi, directory.ICSKey, map[string]string{"url": srv.URL + "/secret.ics"})
	if err != nil || !v.Connected || v.Label != "乙的日历" || v.LastStatus != "ok" || !v.SelfService {
		t.Fatalf("贴链接应连上并立刻同步: %+v %v", v, err)
	}
	if v.Email != "" || v.ExternalID != "" {
		t.Fatalf("链接是密钥，不该以任何形式回显: %+v", v)
	}
	sched, err := a.ScheduleOf(ctx, yi, "me", *day(0), *day(6))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range sched.Items {
		if it.Kind == "event" && it.Title == "例会" && it.Provider == directory.ICSKey {
			found = true
		}
	}
	if !found {
		t.Fatalf("订阅来的例会应出现在日程里: %+v", sched.Items)
	}
	// 组织设置页不列自助提供方
	orgView, err := a.ListCalendars(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range orgView.Providers {
		if p.Provider == directory.ICSKey {
			t.Fatal("组织设置页不该出现日历订阅链接")
		}
	}
	// 后台巡检也会跑它（连接行是自动建的）
	a.RunScheduledCalendarSyncs(ctx, time.Now().Add(calendarSyncInterval+time.Minute))
	if err := a.DisconnectMyCalendar(ctx, yi, directory.ICSKey); err != nil {
		t.Fatal(err)
	}
	// 最后一个人断开后，自动建的连接行也收掉
	if err := a.Store.WithOrg(ctx, yi.OrgID, func(tx pgx.Tx) error {
		_, err := a.Store.CalendarConfig(ctx, tx, directory.ICSKey)
		return err
	}); err != store.ErrNotFound {
		t.Fatalf("断开后连接行应删掉，实际 %v", err)
	}

	// 对外订阅源
	if _, err := a.CreateTask(ctx, jia, CreateTaskInput{Title: "订阅源里的任务", AssigneeID: yi.MemberID, PlannedStart: day(2), PlannedEnd: day(4), Ready: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "订阅源里的目标", OwnerMemberID: yi.MemberID, PlannedStart: day(0), PlannedEnd: day(30)}); err != nil {
		t.Fatal(err)
	}
	if f, err := a.MyFeed(ctx, yi); err != nil || f.Enabled {
		t.Fatalf("一开始没有订阅源: %+v %v", f, err)
	}
	f1, err := a.ResetMyFeed(ctx, yi)
	if err != nil || !f1.Enabled || !strings.Contains(f1.URL, "/api/v1/feeds/") || !strings.HasPrefix(f1.WebcalURL, "webcal://") {
		t.Fatalf("生成订阅源应给出链接: %+v %v", f1, err)
	}
	token := strings.TrimSuffix(f1.URL[strings.LastIndex(f1.URL, "/")+1:], ".ics")
	body, err := a.FeedICS(ctx, token+".ics")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{"BEGIN:VCALENDAR", "订阅源里的任务", "目标 订阅源里的目标", "TRANSP:TRANSPARENT", "X-WR-CALNAME:AxiomOS"} {
		if !strings.Contains(text, want) {
			t.Fatalf("订阅源里应有 %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "例会") {
		t.Fatal("订阅源不该带外部日历同步来的会议")
	}
	// 换链接：旧的失效
	f2, err := a.ResetMyFeed(ctx, yi)
	if err != nil || f2.URL == f1.URL {
		t.Fatalf("重新生成应换链接: %+v %v", f2, err)
	}
	if _, err := a.FeedICS(ctx, token); err == nil {
		t.Fatal("旧链接应失效")
	}
	if err := a.DeleteMyFeed(ctx, yi); err != nil {
		t.Fatal(err)
	}
	if f, err := a.MyFeed(ctx, yi); err != nil || f.Enabled {
		t.Fatalf("停用后应没有订阅源: %+v %v", f, err)
	}
	if _, err := a.FeedICS(ctx, "fd-nope"); err == nil {
		t.Fatal("不存在的 token 应 404")
	}
}
