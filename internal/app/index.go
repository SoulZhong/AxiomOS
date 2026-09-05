package app

import (
	"context"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// ExecutorInfo 是执行者索引里的一条。
type ExecutorInfo struct {
	Kind    domain.ExecutorKind
	Name    string
	OwnerID string
}

// ExecutorIndex 返回组织内全部成员与 Agent 的索引。
func (a *App) ExecutorIndex(ctx context.Context, sess *Session) (map[string]ExecutorInfo, error) {
	out := map[string]ExecutorInfo{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		ms, err := a.Store.ListMembers(ctx, tx)
		if err != nil {
			return err
		}
		for _, m := range ms {
			out[m.ID] = ExecutorInfo{Kind: domain.ExecutorMember, Name: m.Name}
		}
		ags, err := a.Store.ListAgents(ctx, tx)
		if err != nil {
			return err
		}
		for _, ag := range ags {
			out[ag.ID] = ExecutorInfo{Kind: domain.ExecutorAgent, Name: ag.Name, OwnerID: ag.OwnerMemberID}
		}
		return nil
	})
	return out, err
}

// OrgDirectory 是会话页面需要的组织目录。
type OrgDirectory struct {
	Teams  []*domain.Team
	TeamOf map[string]string
	Caps   map[string]i18n.Text
	Emails map[string]string // member id -> email
}

// Directory 读取团队、能力标签、成员邮箱。
func (a *App) Directory(ctx context.Context, sess *Session) (*OrgDirectory, error) {
	d := &OrgDirectory{Emails: map[string]string{}}
	err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		if d.Teams, err = a.Store.ListTeams(ctx, tx); err != nil {
			return
		}
		if d.TeamOf, err = a.Store.TeamOfMembers(ctx, tx); err != nil {
			return
		}
		if d.Caps, err = a.Store.ListCapabilities(ctx, tx); err != nil {
			return
		}
		ms, err := a.Store.ListMembers(ctx, tx)
		if err != nil {
			return err
		}
		for _, m := range ms {
			if acc, err := a.Store.AccountByID(ctx, tx, m.AccountID); err == nil {
				d.Emails[m.ID] = acc.Email
			}
		}
		return nil
	})
	if d.Teams == nil {
		d.Teams = []*domain.Team{}
	}
	return d, err
}

// IncomingRelation 是指向某任务的关联。
type IncomingRelation struct {
	Type domain.RelationType
	Task *domain.Task
}

// TaskRelationsContext 返回任务详情所需的关联任务：本任务指向的与指向本任务的。
func (a *App) TaskRelationsContext(ctx context.Context, sess *Session, t *domain.Task) (related map[string]*domain.Task, incoming []IncomingRelation, types map[string]*domain.TaskType, err error) {
	related = map[string]*domain.Task{}
	incoming = []IncomingRelation{}
	err = a.tx(ctx, sess, func(tx pgx.Tx) error {
		var ids []string
		for _, r := range t.Relations {
			ids = append(ids, r.OtherID)
		}
		others, err := a.Store.TasksByIDs(ctx, tx, ids)
		if err != nil {
			return err
		}
		for _, o := range others {
			related[o.ID] = o
		}
		ins, err := a.Store.IncomingRelations(ctx, tx, t.ID)
		if err != nil {
			return err
		}
		all := append([]*domain.Task{}, others...)
		for _, in := range ins {
			incoming = append(incoming, IncomingRelation{Type: in.Type, Task: in.Task})
			all = append(all, in.Task)
		}
		types, err = a.Store.TaskTypesFor(ctx, tx, all)
		return err
	})
	return
}

// GoalTitles 返回目标 ID → 标题。
func (a *App) GoalTitles(ctx context.Context, sess *Session) (map[string]string, error) {
	out := map[string]string{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		gs, err := a.Store.ListGoals(ctx, tx)
		if err != nil {
			return err
		}
		for _, g := range gs {
			out[g.ID] = g.Title
		}
		return nil
	})
	return out, err
}

// ---------- 网页统计（比 MCP 用的更细） ----------

// CycleDetail 是按任务类型的周期统计。
type CycleDetail struct {
	TypeName       string
	TypeTitle      string
	Sample         int
	AvgHours       float64
	MedianHours    float64
	AvgActiveHours float64
}

func (a *App) CycleDetails(ctx context.Context, sess *Session) ([]CycleDetail, error) {
	out := []CycleDetail{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		scope, ix, err := a.scopedIndex(ctx, tx, sess)
		if err != nil {
			return err
		}
		tasks, err := a.Store.AllTasks(ctx, tx)
		if err != nil {
			return err
		}
		tasks = ix.filterTasks(scope, tasks)
		types, err := a.Store.TaskTypesFor(ctx, tx, tasks)
		if err != nil {
			return err
		}
		runs, err := a.Store.AllRuns(ctx, tx)
		if err != nil {
			return err
		}
		active := map[string]float64{}
		for _, r := range runs {
			end := time.Now()
			if r.EndedAt != nil {
				end = *r.EndedAt
			}
			active[r.TaskID] += end.Sub(r.StartedAt).Hours()
		}
		lead := map[string][]float64{}
		act := map[string][]float64{}
		for _, t := range tasks {
			tt := types[t.TypeName]
			if tt == nil || t.ActualEnd == nil {
				continue
			}
			if st := tt.Workflow.State(t.State); st == nil || st.Label != domain.LabelTerminalSuccess {
				continue
			}
			lead[t.TypeName] = append(lead[t.TypeName], t.ActualEnd.Sub(t.CreatedAt).Hours())
			act[t.TypeName] = append(act[t.TypeName], active[t.ID])
		}
		for name, hs := range lead {
			sort.Float64s(hs)
			d := CycleDetail{TypeName: name, TypeTitle: types[name].Title.In(sess.Loc()), Sample: len(hs)}
			sum := 0.0
			for _, h := range hs {
				sum += h
			}
			d.AvgHours = sum / float64(len(hs))
			d.MedianHours = hs[len(hs)/2]
			if len(hs)%2 == 0 {
				d.MedianHours = (hs[len(hs)/2-1] + hs[len(hs)/2]) / 2
			}
			as := 0.0
			for _, h := range act[name] {
				as += h
			}
			d.AvgActiveHours = as / float64(len(hs))
			out = append(out, d)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].TypeName < out[j].TypeName })
		return nil
	})
	return out, err
}

// WeekStat 是一周的创建/完成/终止数。
type WeekStat struct {
	Week       string
	Created    int
	Done       int
	Terminated int
}

func (a *App) WeekStats(ctx context.Context, sess *Session) ([]WeekStat, error) {
	out := []WeekStat{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		scope, ix, err := a.scopedIndex(ctx, tx, sess)
		if err != nil {
			return err
		}
		tasks, err := a.Store.AllTasks(ctx, tx)
		if err != nil {
			return err
		}
		tasks = ix.filterTasks(scope, tasks)
		types, err := a.Store.TaskTypesFor(ctx, tx, tasks)
		if err != nil {
			return err
		}
		created, done, term := map[string]int{}, map[string]int{}, map[string]int{}
		for _, t := range tasks {
			created[weekOf(t.CreatedAt)]++
			tt := types[t.TypeName]
			if tt == nil || t.ActualEnd == nil {
				continue
			}
			if st := tt.Workflow.State(t.State); st != nil {
				switch st.Label {
				case domain.LabelTerminalSuccess:
					done[weekOf(*t.ActualEnd)]++
				case domain.LabelTerminalFailure:
					term[weekOf(*t.ActualEnd)]++
				}
			}
		}
		now := time.Now()
		for i := 11; i >= 0; i-- {
			w := weekOf(now.AddDate(0, 0, -7*i))
			out = append(out, WeekStat{Week: w, Created: created[w], Done: done[w], Terminated: term[w]})
		}
		return nil
	})
	return out, err
}

func weekOf(t time.Time) string {
	t = t.Local()
	t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	return t.AddDate(0, 0, -(wd - 1)).Format("2006-01-02")
}

// AgentQuality 是 Agent 的质量统计（含验收通过数）。
type AgentQuality struct {
	AgentID  string
	OwnerID  string
	Runs     int
	Accepted int
	Rejected int
	Tokens   int64
	Cost     float64
}

func (a *App) AgentQualities(ctx context.Context, sess *Session) ([]AgentQuality, error) {
	out := []AgentQuality{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		// Agent 成本是财务数据：范围越权 403（ADR 0013）。
		scope, err := a.financeScope(ctx, tx, sess)
		if err != nil {
			return err
		}
		ix, err := a.OrgIndex(ctx, tx)
		if err != nil {
			return err
		}
		agents, err := a.Store.ListAgents(ctx, tx)
		if err != nil {
			return err
		}
		runs, err := a.Store.AllRuns(ctx, tx)
		if err != nil {
			return err
		}
		events, err := a.Store.ListEvents(ctx, tx, "", 100000)
		if err != nil {
			return err
		}
		runByID := map[string]*store.RunRow{}
		for _, r := range runs {
			runByID[r.ID] = r
		}
		lastExecutor := map[string]string{}
		accepted, rejected := map[string]int{}, map[string]int{}
		for i := len(events) - 1; i >= 0; i-- {
			e := events[i]
			switch e.Type {
			case "RunEnded":
				if id, _ := e.Data["run_id"].(string); id != "" {
					if r := runByID[id]; r != nil {
						lastExecutor[e.TaskID] = r.ExecutorID
					}
				}
			case "TaskTransitioned":
				tr, _ := e.Data["transition"].(string)
				switch tr {
				case "accept", "verify", "test_pass":
					accepted[lastExecutor[e.TaskID]]++
				case "reject", "test_fail", "reopen":
					rejected[lastExecutor[e.TaskID]]++
				}
			}
		}
		for _, ag := range agents {
			if !scope.HasTeam(ix.TeamOfExecutor(ag.ID)) {
				continue
			}
			q := AgentQuality{AgentID: ag.ID, OwnerID: ag.OwnerMemberID, Accepted: accepted[ag.ID], Rejected: rejected[ag.ID]}
			for _, r := range runs {
				if r.ExecutorID != ag.ID {
					continue
				}
				q.Runs++
				q.Cost += r.Cost
				for _, u := range r.Usage {
					q.Tokens += u.TotalTokens()
				}
			}
			out = append(out, q)
		}
		return nil
	})
	return out, err
}
