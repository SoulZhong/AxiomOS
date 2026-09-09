package app

// 日程（ADR 0032）：目标、任务、里程碑与外部日历的一种看法，读的时候拼出来，不落库。

import (
	"context"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// ScheduleMaxDays 一次最多取多少天。
const ScheduleMaxDays = 62

// ScheduleItem 是日程里的一条。日期一律是当地日期 YYYY-MM-DD；带时刻的会议另给 starts_at / ends_at。
type ScheduleItem struct {
	Kind     string `json:"kind"` // task | goal | milestone | event
	ID       string `json:"id"`
	Title    string `json:"title"`
	MemberID string `json:"member_id"` // 这是谁的日程
	// 跨天的一段：起止日期（含）；单日的一枚：Date
	StartDate string `json:"start_date,omitempty"`
	EndDate   string `json:"end_date,omitempty"`
	Date      string `json:"date,omitempty"`
	// 会议：精确起止与全天
	StartsAt *time.Time `json:"starts_at,omitempty"`
	EndsAt   *time.Time `json:"ends_at,omitempty"`
	AllDay   bool       `json:"all_day,omitempty"`
	// 任务
	Number     int        `json:"number,omitempty"`
	State      *StateView `json:"state,omitempty"`
	Progress   int        `json:"progress,omitempty"`
	Deadline   bool       `json:"deadline,omitempty"` // 这枚是任务的截止日
	AssigneeID string     `json:"assignee_id,omitempty"`
	// 目标
	Status string `json:"status,omitempty"`
	// 里程碑
	GoalID          string `json:"goal_id,omitempty"`
	MilestoneStatus string `json:"milestone_status,omitempty"`
	// 会议
	Provider    string `json:"provider,omitempty"`
	URL         string `json:"url,omitempty"`
	Busy        bool   `json:"busy,omitempty"`
	TitleHidden bool   `json:"title_hidden,omitempty"` // 别人的会议只画成「忙」
	Overdue     bool   `json:"overdue,omitempty"`
}

// ScheduleMember 是日程里的一个人。
type ScheduleMember struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ScheduleView 是一段时间里若干人的日程。
type ScheduleView struct {
	From    string           `json:"from"`
	To      string           `json:"to"` // 含
	Who     string           `json:"who"`
	Members []ScheduleMember `json:"members"`
	Items   []ScheduleItem   `json:"items"`
	// Sources 哪几家外部日历接上了（界面标来源用）
	Sources []string `json:"sources"`
}

func isoDay(t time.Time) string { return t.Format("2006-01-02") }

// ScheduleOf 取日程：who 为空或 me 是我自己；成员 ID 看那个人；团队 ID 看团队全部成员（含下级团队）。
func (a *App) ScheduleOf(ctx context.Context, sess *Session, who string, from, to time.Time) (*ScheduleView, error) {
	from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.Local)
	to = time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.Local)
	if to.Before(from) {
		return nil, Bad("err.schedule_range")
	}
	// 含首尾的天数不能超过 62（from 与 to 相差 61 天就是 62 天）
	if int(to.Sub(from).Hours()/24)+1 > ScheduleMaxDays {
		return nil, Bad("err.schedule_too_long", ScheduleMaxDays)
	}
	toExcl := to.Add(24 * time.Hour)
	if who == "" || who == "me" {
		who = sess.MemberID
	}
	v := &ScheduleView{From: isoDay(from), To: isoDay(to), Who: who, Members: []ScheduleMember{}, Items: []ScheduleItem{}, Sources: []string{}}
	loc := sess.Loc()
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		names, err := a.Store.ExecutorNames(ctx, tx)
		if err != nil {
			return err
		}
		members, err := a.scheduleMembers(ctx, tx, sess, who)
		if err != nil {
			return err
		}
		for _, m := range members {
			v.Members = append(v.Members, ScheduleMember{ID: m, Name: names[m]})
		}
		// 执行者 = 成员本人 + 他名下的 Agent（Agent 替人做事，占的是人的时间）
		agents, err := a.Store.ListAgents(ctx, tx)
		if err != nil {
			return err
		}
		ownerOf := map[string]string{}
		execs := append([]string{}, members...)
		memberSet := map[string]bool{}
		for _, m := range members {
			memberSet[m] = true
		}
		for _, ag := range agents {
			if memberSet[ag.OwnerMemberID] {
				execs = append(execs, ag.ID)
				ownerOf[ag.ID] = ag.OwnerMemberID
			}
		}
		now := time.Now()
		// 任务
		tasks, err := a.Store.TasksInRange(ctx, tx, execs, from, toExcl)
		if err != nil {
			return err
		}
		types, err := a.Store.TaskTypesFor(ctx, tx, tasks)
		if err != nil {
			return err
		}
		for _, t := range tasks {
			if err := a.requireTaskVisible(ctx, tx, sess, t); err != nil {
				continue
			}
			owner := t.AssigneeID
			if o, ok := ownerOf[owner]; ok {
				owner = o
			}
			tt := types[t.TypeName]
			it := ScheduleItem{Kind: "task", ID: t.ID, Title: t.Title, MemberID: owner, Number: t.Number, AssigneeID: t.AssigneeID}
			if tt != nil {
				if st := tt.Workflow.State(t.State); st != nil {
					it.State = &StateView{Name: st.Name, Title: st.Title.In(loc), Label: st.Label, LabelTitle: domain.LabelTitle(st.Label, loc)}
					it.Overdue = t.PlannedEnd != nil && t.PlannedEnd.Before(now) && !st.Label.IsTerminal()
				}
				it.Progress = domain.Progress(t, tt)
			}
			switch {
			case t.PlannedStart != nil && t.PlannedEnd != nil:
				it.StartDate, it.EndDate = isoDay(*t.PlannedStart), isoDay(*t.PlannedEnd)
			case t.PlannedStart != nil:
				it.Date = isoDay(*t.PlannedStart)
			case t.PlannedEnd != nil:
				it.Date, it.Deadline = isoDay(*t.PlannedEnd), true
			}
			v.Items = append(v.Items, it)
		}
		// 目标（负责人是这些人的）
		goals, err := a.Store.GoalsInRange(ctx, tx, members, from, toExcl)
		if err != nil {
			return err
		}
		for _, g := range goals {
			if g.TeamID != "" && !sess.CanSeeCollabTeam(g.TeamID) {
				continue
			}
			v.Items = append(v.Items, ScheduleItem{Kind: "goal", ID: g.ID, Title: g.Title, MemberID: g.OwnerMemberID, Status: string(g.Status),
				StartDate: isoDay(*g.PlannedStart), EndDate: isoDay(*g.PlannedEnd)})
		}
		// 里程碑
		ms, err := a.Store.MilestonesInRange(ctx, tx, members, from, toExcl)
		if err != nil {
			return err
		}
		goalOwner := map[string]string{}
		goalTeam := map[string]string{}
		for _, g := range goals {
			goalOwner[g.ID], goalTeam[g.ID] = g.OwnerMemberID, g.TeamID
		}
		for _, m := range ms {
			owner, ok := goalOwner[m.GoalID]
			if !ok {
				g, err := a.Store.GoalByID(ctx, tx, m.GoalID)
				if err != nil {
					continue
				}
				owner = g.OwnerMemberID
				goalOwner[m.GoalID], goalTeam[m.GoalID] = owner, g.TeamID
			}
			if team := goalTeam[m.GoalID]; team != "" && !sess.CanSeeCollabTeam(team) {
				continue
			}
			v.Items = append(v.Items, ScheduleItem{Kind: "milestone", ID: m.ID, Title: m.Title, MemberID: owner, GoalID: m.GoalID,
				Date: isoDay(m.DueOn), MilestoneStatus: string(domain.StatusOfMilestone(m, now))})
		}
		// 外部日历：自己的会议看得到标题，别人的只画成「忙」
		events, err := a.Store.CalendarEventsOf(ctx, tx, members, from, toExcl)
		if err != nil {
			return err
		}
		seenSrc := map[string]bool{}
		for _, e := range events {
			it := ScheduleItem{Kind: "event", ID: e.ID, Title: e.Title, MemberID: e.MemberID, Provider: e.Provider, URL: e.URL, Busy: e.Busy, AllDay: e.AllDay}
			s, en := e.StartsAt, e.EndsAt
			it.StartsAt, it.EndsAt = &s, &en
			if e.AllDay {
				it.StartDate, it.EndDate = isoDay(e.StartsAt), isoDay(e.EndsAt.Add(-time.Second))
			} else {
				it.Date = isoDay(e.StartsAt.In(time.Local))
			}
			if e.MemberID != sess.MemberID {
				it.Title, it.URL, it.TitleHidden = i18n.Tr(loc, "schedule.busy"), "", true
			}
			if !seenSrc[e.Provider] {
				seenSrc[e.Provider] = true
				v.Sources = append(v.Sources, e.Provider)
			}
			v.Items = append(v.Items, it)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(v.Items, func(i, j int) bool {
		a1, b1 := v.Items[i], v.Items[j]
		da, db := a1.StartDate, b1.StartDate
		if da == "" {
			da = a1.Date
		}
		if db == "" {
			db = b1.Date
		}
		if da != db {
			return da < db
		}
		if a1.StartsAt != nil && b1.StartsAt != nil {
			return a1.StartsAt.Before(*b1.StartsAt)
		}
		return kindRank(a1.Kind) < kindRank(b1.Kind)
	})
	sort.Strings(v.Sources)
	return v, nil
}

func kindRank(k string) int {
	switch k {
	case "goal":
		return 0
	case "milestone":
		return 1
	case "task":
		return 2
	}
	return 3
}

// scheduleMembers 解析 who：我 / 某个成员 / 某个团队（含下级）；并按 ADR 0013 判能不能看。
func (a *App) scheduleMembers(ctx context.Context, tx pgx.Tx, sess *Session, who string) ([]string, error) {
	if who == sess.MemberID {
		return []string{who}, nil
	}
	teams, err := a.Store.ListTeams(ctx, tx)
	if err != nil {
		return nil, err
	}
	byID := map[string]*domain.Team{}
	for _, t := range teams {
		byID[t.ID] = t
	}
	if _, isTeam := byID[who]; isTeam {
		if !sess.CanSeeCollabTeam(who) {
			return nil, Forbidden("err.schedule_team_hidden")
		}
		// 含下级团队
		want := map[string]bool{who: true}
		for changed := true; changed; {
			changed = false
			for _, t := range teams {
				if !want[t.ID] && want[t.ParentID] {
					want[t.ID] = true
					changed = true
				}
			}
		}
		tm, err := a.Store.TeamMembers(ctx, tx)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		var out []string
		for tid := range want {
			for _, m := range tm[tid] {
				if !seen[m] {
					seen[m] = true
					out = append(out, m)
				}
			}
		}
		members, err := a.Store.ListMembers(ctx, tx)
		if err != nil {
			return nil, err
		}
		active := map[string]bool{}
		for _, m := range members {
			active[m.ID] = m.Active
		}
		kept := out[:0]
		for _, m := range out {
			if active[m] {
				kept = append(kept, m)
			}
		}
		sort.Strings(kept)
		return kept, nil
	}
	// 某个成员：他在我能看的团队里，或我能看全公司
	if _, err := a.Store.MemberByID(ctx, tx, who); err != nil {
		return nil, NotFound("err.member_missing")
	}
	if !sess.CollabAll() {
		theirs, err := a.Store.TeamsOfMember(ctx, tx, who)
		if err != nil {
			return nil, err
		}
		ok := false
		for _, t := range theirs {
			if sess.CanSeeCollabTeam(t) {
				ok = true
			}
		}
		if !ok {
			return nil, Forbidden("err.schedule_member_hidden")
		}
	}
	return []string{who}, nil
}

// MyScheduleOf 给 Agent：它所有者的日程（只读）。
func (a *App) MyScheduleOf(ctx context.Context, sess *Session, from, to time.Time) (*ScheduleView, error) {
	return a.ScheduleOf(ctx, sess, sess.MemberID, from, to)
}

// CurrentWeek 返回今天所在的一周（周一到周日，当地日期），日程的默认区间。
func CurrentWeek(now time.Time) (time.Time, time.Time) {
	d := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	off := (int(d.Weekday()) + 6) % 7
	from := d.AddDate(0, 0, -off)
	return from, from.AddDate(0, 0, 6)
}

var _ = store.ErrNotFound
