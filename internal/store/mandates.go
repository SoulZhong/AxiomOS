package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/teemo/axiomos/internal/domain"
)

const mandateCols = `id,org_id,task_id,agent_id,owner_id,plan_id,goal_id,type_version,budget_cost,budget_tokens,deadline,side_effects,ask_me,grants,task_version,assignee_id,status,reason,created_at,ended_at`

func scanMandate(r interface{ Scan(...any) error }) (*domain.Mandate, error) {
	m := &domain.Mandate{}
	var side, grants []byte
	err := r.Scan(&m.ID, &m.OrgID, &m.TaskID, &m.AgentID, &m.OwnerID, &m.PlanID, &m.GoalID, &m.TypeVersion, &m.BudgetCost, &m.BudgetTokens, &m.Deadline,
		&side, &m.AskMe, &grants, &m.TaskVersion, &m.AssigneeID, &m.Status, &m.Reason, &m.CreatedAt, &m.EndedAt)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.SideEffects = map[string]bool{}
	_ = json.Unmarshal(side, &m.SideEffects)
	m.Grants = map[domain.Grant]domain.GrantMode{}
	_ = json.Unmarshal(grants, &m.Grants)
	if m.AskMe == nil {
		m.AskMe = []string{}
	}
	return m, nil
}

// InsertMandate 写入一份委托。同一任务、同一 Agent 已有活动中的委托时由唯一索引拒绝。
func (s *Store) InsertMandate(ctx context.Context, q Querier, m *domain.Mandate) error {
	if m.ID == "" {
		m.ID = NewID("mdt")
	}
	if m.Status == "" {
		m.Status = domain.MandateActive
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now()
	}
	if m.SideEffects == nil {
		m.SideEffects = map[string]bool{}
	}
	if m.AskMe == nil {
		m.AskMe = []string{}
	}
	if m.Grants == nil {
		m.Grants = map[domain.Grant]domain.GrantMode{}
	}
	side, _ := json.Marshal(m.SideEffects)
	grants, _ := json.Marshal(m.Grants)
	_, err := q.Exec(ctx, `insert into mandates(id,org_id,task_id,agent_id,owner_id,plan_id,goal_id,type_version,budget_cost,budget_tokens,deadline,side_effects,ask_me,grants,task_version,assignee_id,status,reason,created_at)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
		m.ID, m.OrgID, m.TaskID, m.AgentID, m.OwnerID, m.PlanID, m.GoalID, m.TypeVersion, m.BudgetCost, m.BudgetTokens, m.Deadline, side, m.AskMe, grants, m.TaskVersion, m.AssigneeID, m.Status, m.Reason, m.CreatedAt)
	return err
}

// ActiveMandate 找某个 Agent 在某个任务上活动中的委托；没有返回 nil。
func (s *Store) ActiveMandate(ctx context.Context, q Querier, taskID, agentID string) (*domain.Mandate, error) {
	m, err := scanMandate(q.QueryRow(ctx, `select `+mandateCols+` from mandates where task_id=$1 and agent_id=$2 and status='active'`, taskID, agentID))
	if err == ErrNotFound {
		return nil, nil
	}
	return m, err
}

// MandateByID 读一份委托。
func (s *Store) MandateByID(ctx context.Context, q Querier, id string) (*domain.Mandate, error) {
	return scanMandate(q.QueryRow(ctx, `select `+mandateCols+` from mandates where id=$1`, id))
}

// MandateFilter 是委托列表的过滤条件。
type MandateFilter struct {
	TaskID     string
	AgentID    string
	PlanID     string
	OnlyActive bool
}

// ListMandates 列委托，新的在前。
func (s *Store) ListMandates(ctx context.Context, q Querier, f MandateFilter) ([]*domain.Mandate, error) {
	where, args := ` where true`, []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		where += ` and ` + cond + `$` + itoa(len(args))
	}
	if f.TaskID != "" {
		add("task_id=", f.TaskID)
	}
	if f.AgentID != "" {
		add("agent_id=", f.AgentID)
	}
	if f.PlanID != "" {
		add("plan_id=", f.PlanID)
	}
	if f.OnlyActive {
		where += ` and status='active'`
	}
	rows, err := q.Query(ctx, `select `+mandateCols+` from mandates`+where+` order by created_at desc`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Mandate
	for rows.Next() {
		m, err := scanMandate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// EndMandate 把委托置为收回或失效。
func (s *Store) EndMandate(ctx context.Context, q Querier, id, status, reason string) error {
	_, err := q.Exec(ctx, `update mandates set status=$2, reason=$3, ended_at=now() where id=$1 and status='active'`, id, status, reason)
	return err
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
