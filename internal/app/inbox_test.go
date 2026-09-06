package app

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"time"
)

func mustNotes(t *testing.T, a *App, ctx context.Context, sess *Session) []*domain.Notification {
	t.Helper()
	notes, err := a.Notifications(ctx, sess, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) == 0 {
		t.Fatalf("%s 应有通知", sess.Actor.Name)
	}
	return notes
}

func inboxKinds(in *Inbox) []string {
	out := make([]string, 0, len(in.Groups))
	for _, g := range in.Groups {
		out = append(out, g.Kind)
	}
	return out
}

// 待我处理全程（DESIGN.md §12）：乙手上六种事各一件 → 六组按紧急度排好、读数吻合；
// 空组省略；Agent 被拒；标已读只影响自己的通知、不产生动态；只看本人不套范围。
func TestInboxGroupsAndCounts(t *testing.T) {
	a, ctx := testApp(t)
	orgID, jia, yi := newTestOrg(t, a, ctx)

	// ---- 什么都没有：全空，只有那句话 ----
	empty, err := a.Inbox(ctx, yi)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Count != 0 || len(empty.Groups) != 0 || empty.Empty != "没有等你处理的事。" {
		t.Fatalf("空的待我处理应只有那句话，实际 %+v", empty)
	}

	goal, err := a.CreateGoal(ctx, jia, CreateGoalInput{Title: "待我处理目标"})
	if err != nil {
		t.Fatal(err)
	}

	// 逾期：乙负责、三天前该结束、已开始（所以不算"没开始"）
	overdue, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: goal.ID, Title: "逾期任务", AssigneeID: yi.MemberID, PlannedEnd: dayp(-3), Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, yi, overdue.ID, "start", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	// 没开始：乙负责、还在待办
	unstarted, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: goal.ID, Title: "还没开始的任务", AssigneeID: yi.MemberID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	// 等我验收：甲做、乙验收，已提交
	review, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: goal.ID, Title: "等乙验收的任务", AssigneeID: jia.MemberID, ReviewerID: yi.MemberID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, jia, review.ID, "start", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddArtifact(ctx, jia, review.ID, domain.Artifact{Type: "result", Title: "结果"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, jia, review.ID, "submit", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	// 等我答复：甲做、乙是验收人，甲提问等待；「答复并恢复」由创建者或验收人触发
	question, err := a.CreateTask(ctx, jia, CreateTaskInput{GoalID: goal.ID, Title: "有提问的任务", AssigneeID: jia.MemberID, ReviewerID: yi.MemberID, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, jia, question.ID, "start", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, jia, question.ID, "ask_for_input", TransitionPayload{Comment: "验收标准是什么？"}); err != nil {
		t.Fatal(err)
	}
	// 待确认操作 + 通知：乙的 Agent 要开始「还没开始的任务」，授权是需要人确认 → 记一条待确认操作并通知乙
	_, agent := agentSessionFor(t, a, ctx, yi, "乙的 Agent", map[domain.Grant]domain.GrantMode{domain.GrantExecute: domain.GrantWithApproval})
	_, err = a.Transition(ctx, agent, unstarted.ID, "start", TransitionPayload{})
	pp := mustPending(t, err)

	// ---- 乙的待我处理：六组，顺序即紧急度；通知是被指派 ×2、待验收、待确认操作共四条 ----
	unread := 0
	for _, n := range mustNotes(t, a, ctx, yi) {
		if n.ReadAt == nil {
			unread++
		}
	}
	if unread != 4 {
		t.Fatalf("乙应有 4 条未读通知，实际 %d", unread)
	}
	in, err := a.Inbox(ctx, yi)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{InboxOverdue, InboxProposals, InboxReview, InboxQuestions, InboxUnstarted, InboxNotifications}
	if got := inboxKinds(in); len(got) != len(want) {
		t.Fatalf("应有六组，实际 %v", got)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("组顺序应是 %v，实际 %v", want, got)
			}
		}
	}
	if in.Count != 5+unread || in.Empty != "" {
		t.Fatalf("总数应是 %d 且没有空句子，实际 %d %q", 5+unread, in.Count, in.Empty)
	}
	for _, g := range in.Groups {
		wantN := 1
		if g.Kind == InboxNotifications {
			wantN = unread
		}
		if g.Count != wantN || len(g.Tasks)+len(g.Proposals)+len(g.Questions)+len(g.Notifications) != wantN {
			t.Fatalf("%s 组应有 %d 条，实际 %d", g.Kind, wantN, g.Count)
		}
		if g.Title == "" || g.Title == "inbox.kind."+g.Kind {
			t.Fatalf("%s 组应有中文组名，实际 %q", g.Kind, g.Title)
		}
		switch g.Kind {
		case InboxOverdue:
			// 期望值用与实现相同的口径算（本地日历日），dayp 用 UTC 截断，靠近本地零点时会差一天
			wantDays := daysOverdue(overdue.PlannedEnd, startOfDay(time.Now()))
			if g.Tasks[0].Task.ID != overdue.ID || g.Tasks[0].DaysOverdue != wantDays {
				t.Fatalf("逾期组应是「逾期任务」并逾期 3 天，实际 %+v", g.Tasks[0])
			}
		case InboxProposals:
			if g.Proposals[0].ID != pp.Proposal.ID || !g.Proposals[0].CanDecide {
				t.Fatalf("待确认组应是那条 Agent 的操作且可确认，实际 %+v", g.Proposals[0])
			}
		case InboxReview:
			if g.Tasks[0].Task.ID != review.ID || g.Tasks[0].DaysOverdue != 0 {
				t.Fatalf("待验收组应是「等乙验收的任务」，实际 %+v", g.Tasks[0])
			}
		case InboxQuestions:
			if g.Questions[0].Task.ID != question.ID || g.Questions[0].Comment.Text != "验收标准是什么？" || g.Questions[0].Comment.ByID != jia.MemberID {
				t.Fatalf("待答复组应带上甲的提问，实际 %+v", g.Questions[0])
			}
		case InboxUnstarted:
			if g.Tasks[0].Task.ID != unstarted.ID {
				t.Fatalf("没开始组应是「还没开始的任务」，实际 %+v", g.Tasks[0])
			}
		case InboxNotifications:
			for _, n := range g.Notifications {
				if n.ReadAt != nil || n.MemberID != yi.MemberID {
					t.Fatalf("通知组应只有乙的未读通知，实际 %+v", n)
				}
			}
		}
	}
	// 英文组名
	en := *yi
	en.Locale = i18n.EnUS
	if inEn, err := a.Inbox(ctx, &en); err != nil || inEn.Groups[0].Title != "My overdue tasks" {
		t.Fatalf("组名应按语言，实际 %v %+v", err, inEn.Groups[0].Title)
	}

	// ---- 读数版与完整版一致 ----
	cnt, err := a.InboxCount(ctx, yi)
	if err != nil {
		t.Fatal(err)
	}
	if cnt.Count != in.Count || cnt.ByKind[InboxNotifications] != unread {
		t.Fatalf("读数应与完整版一致（%d），实际 %+v", in.Count, cnt)
	}
	for _, k := range InboxKinds[:5] {
		if cnt.ByKind[k] != 1 {
			t.Fatalf("by_kind.%s 应是 1，实际 %+v", k, cnt.ByKind)
		}
	}

	// ---- 甲：能确认那条待确认操作（组织负责人）；自己提的问不算等自己答复；其余为空 → 空组省略 ----
	jin, err := a.Inbox(ctx, jia)
	if err != nil {
		t.Fatal(err)
	}
	if got := inboxKinds(jin); len(got) != 1 || got[0] != InboxProposals || jin.Count != 1 {
		t.Fatalf("甲的待我处理应只有待确认一组，实际 %v", got)
	}

	// ---- Agent 没有待我处理 ----
	if _, err := a.Inbox(ctx, agent); err == nil {
		t.Fatal("Agent 不应有待我处理")
	} else if ue, ok := err.(*UserError); !ok || ue.Status != 403 || ue.Render(i18n.ZhCN) == "err.inbox_agent" {
		t.Fatalf("Agent 应得到 403 与完整中文句子，实际 %v", err)
	}
	if _, err := a.InboxCount(ctx, agent); err == nil {
		t.Fatal("Agent 不应有待我处理读数")
	}

	// ---- 标已读：只动自己的，不产生动态，返回剩余未读数 ----
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		return a.Store.InsertNotification(ctx, tx, orgID, domain.Notification{MemberID: jia.MemberID, Title: "给甲的通知"})
	}); err != nil {
		t.Fatal(err)
	}
	jiaNotes, yiNotes := mustNotes(t, a, ctx, jia), mustNotes(t, a, ctx, yi)
	ids := []int64{jiaNotes[0].ID}
	for _, n := range yiNotes[1:] { // 留最后一条不标，验证"剩余未读数"
		ids = append(ids, n.ID)
	}
	before, _ := a.Events(ctx, jia, "", 10000)
	remaining, err := a.MarkNotificationsRead(ctx, yi, ids)
	if err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("乙应还剩 1 条未读，实际 %d", remaining)
	}
	if remaining, err = a.MarkNotificationsRead(ctx, yi, []int64{yiNotes[0].ID}); err != nil || remaining != 0 {
		t.Fatalf("乙标完应没有未读，实际 %d %v", remaining, err)
	}
	after, _ := a.Events(ctx, jia, "", 10000)
	if len(after) != len(before) {
		t.Fatal("标已读不应产生动态")
	}
	jiaNotes, _ = a.Notifications(ctx, jia, false)
	if jiaNotes[0].ReadAt != nil {
		t.Fatal("乙不能把甲的通知标为已读")
	}
	cnt, _ = a.InboxCount(ctx, yi)
	if cnt.ByKind[InboxNotifications] != 0 || cnt.Count != 5 {
		t.Fatalf("标已读后通知读数应归零，实际 %+v", cnt)
	}
	if _, err := a.MarkNotificationsRead(ctx, agent, nil); err == nil {
		t.Fatal("Agent 不能标通知")
	}

	// ---- 答复之后不再是「等我答复」；验收之后不再是「等我验收」 ----
	if _, err := a.Transition(ctx, yi, question.ID, "resume", TransitionPayload{Comment: "按需求文档验收。"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transition(ctx, yi, review.ID, "accept", TransitionPayload{}); err != nil {
		t.Fatal(err)
	}
	cnt, _ = a.InboxCount(ctx, yi)
	if cnt.ByKind[InboxQuestions] != 0 || cnt.ByKind[InboxReview] != 0 || cnt.Count != 3 {
		t.Fatalf("处理完后待答复与待验收应归零，实际 %+v", cnt)
	}
}

// 「等我验收」「等我答复」两条规则只认状态类型与步骤声明（ADR 0005）：
// 阻塞（等待状态但不要求评论）、待验收（带验收授权）都不算提问；非验收人不算等他验收。
func TestInboxFlowRulesAreStructural(t *testing.T) {
	types := domain.BuiltinTaskTypes()
	var generic *domain.TaskType
	for _, tt := range types {
		if tt.Name == "generic" {
			generic = tt
		}
	}
	me := &domain.Executor{ID: "m1", Kind: domain.ExecutorMember}
	other := &domain.Executor{ID: "m2", Kind: domain.ExecutorMember}
	task := &domain.Task{CreatorID: "m1", ReviewerID: "m1", AssigneeID: "m2", State: "submitted",
		Comments: []domain.Comment{{ID: "c1", ByID: "m2", Text: "请验收"}}}
	if !domain.AwaitsReviewBy(task, generic, me) || domain.AwaitsReviewBy(task, generic, other) {
		t.Fatal("待验收状态应只等验收人")
	}
	if domain.AwaitsReplyFrom(task, generic, me) {
		t.Fatal("待验收不是提问：验收打回虽要评论，但带验收授权")
	}
	task.State = "waiting"
	if !domain.AwaitsReplyFrom(task, generic, me) || domain.AwaitsReplyFrom(task, generic, other) {
		t.Fatal("等待答复应只等创建者或验收人")
	}
	if domain.AwaitsReviewBy(task, generic, me) {
		t.Fatal("等待答复不是等验收")
	}
	if q := domain.OpenQuestion(task, me); q == nil || q.ID != "c1" {
		t.Fatalf("应取最后一条别人写的评论，实际 %+v", q)
	}
	task.Comments = append(task.Comments, domain.Comment{ID: "n1", ByID: "m1", Text: "日志", IsNote: true})
	if q := domain.OpenQuestion(task, me); q == nil || q.ID != "c1" {
		t.Fatal("工作日志不算答复")
	}
	task.Comments = append(task.Comments, domain.Comment{ID: "c2", ByID: "m1", Text: "答复了"})
	if domain.OpenQuestion(task, me) != nil {
		t.Fatal("我回过话之后不再是等我答复")
	}
	task.State = "blocked"
	if domain.AwaitsReplyFrom(task, generic, me) || domain.AwaitsReviewBy(task, generic, me) {
		t.Fatal("已阻塞既不是提问也不是待验收")
	}
	task.State = "done"
	if domain.AwaitsReplyFrom(task, generic, me) || domain.AwaitsReviewBy(task, generic, me) {
		t.Fatal("终止状态什么都不等")
	}
}

// 旧布局里的 proposals 区块读出来时静默丢弃（已并入待我处理）。
func TestWorkspaceDropsRetiredProposalsBlock(t *testing.T) {
	a, ctx := testApp(t)
	f := newWorkspaceOrg(t, a, ctx)
	if err := a.Store.WithOrg(ctx, f.orgID, func(tx pgx.Tx) error {
		if err := a.Store.PutRoleLayout(ctx, tx, f.orgID, "developer", domain.SizedBlocks("proposals", "my_review", "events"), ""); err != nil {
			return err
		}
		return a.Store.PutRoleLayout(ctx, tx, f.orgID, "tester", domain.SizedBlocks("proposals"), "")
	}); err != nil {
		t.Fatal(err)
	}
	ws, err := a.MyWorkspace(ctx, f.dev)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Source != "roles" || len(ws.Blocks) != 2 || ws.Blocks[0].Key != "my_review" || ws.Blocks[1].Key != "events" {
		t.Fatalf("角色布局里的 proposals 应被丢弃: %+v", ws)
	}
	if err := a.Store.WithOrg(ctx, f.orgID, func(tx pgx.Tx) error {
		return a.Store.PutMemberLayout(ctx, tx, f.orgID, f.dev.MemberID, domain.SizedBlocks("proposals", "sprint"))
	}); err != nil {
		t.Fatal(err)
	}
	ws, _ = a.MyWorkspace(ctx, f.dev)
	if ws.Source != "personal" || len(ws.Blocks) != 1 || ws.Blocks[0].Key != "sprint" {
		t.Fatalf("个人布局里的 proposals 应被丢弃: %+v", ws)
	}
	// 组织设置里的角色布局视图同样不再出现它
	layouts, err := a.OrgWorkspaceLayouts(ctx, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range layouts {
		for _, b := range l.Blocks {
			if b.Key == "proposals" {
				t.Fatalf("角色 %s 的布局视图不应含 proposals", l.Role)
			}
		}
	}
	// 再保存时带上它也不报错、不落库
	ws, err = a.SetMyWorkspace(ctx, f.dev, []string{"proposals", "my_tasks"})
	if err != nil || len(ws.Blocks) != 1 || ws.Blocks[0].Key != "my_tasks" {
		t.Fatalf("保存时应静默去掉 proposals: %v %+v", err, ws)
	}
}
