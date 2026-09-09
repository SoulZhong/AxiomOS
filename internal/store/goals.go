package store

import (
	"context"
	"time"

	"github.com/teemo/axiomos/internal/domain"
)

const goalCols = `id,org_id,coalesce(parent_id,''),coalesce(team_id,''),coalesce(type_id,''),owner_member_id,title,description,status,budget,deadline,planned_start,planned_end,progress_override,horizon,confidence,outcome,date_precision,rank,visibility,created_at,updated_at,version`

func scanGoal(r interface{ Scan(...any) error }) (*domain.Goal, error) {
	g := &domain.Goal{}
	var status, horizon, confidence, precision string
	err := r.Scan(&g.ID, &g.OrgID, &g.ParentID, &g.TeamID, &g.TypeID, &g.OwnerMemberID, &g.Title, &g.Description, &status, &g.Budget, &g.Deadline, &g.PlannedStart, &g.PlannedEnd, &g.ProgressOverride, &horizon, &confidence, &g.Outcome, &precision, &g.Rank, &g.Visibility, &g.CreatedAt, &g.UpdatedAt, &g.Version)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	g.Status = domain.GoalStatus(status)
	g.Horizon, g.Confidence = domain.GoalHorizon(horizon), domain.GoalConfidence(confidence)
	g.DatePrecision = domain.DatePrecision(precision)
	return g, err
}

func (s *Store) CreateGoal(ctx context.Context, q Querier, g *domain.Goal) error {
	if g.ID == "" {
		g.ID = NewID("goal")
	}
	if g.Status == "" {
		g.Status = domain.GoalActive
	}
	if g.Visibility == "" {
		g.Visibility = "org"
	}
	now := time.Now()
	g.CreatedAt, g.UpdatedAt = now, now
	g.Version = 1
	g.DatePrecision = domain.NormalizeDatePrecision(g.DatePrecision)
	_, err := q.Exec(ctx, `insert into goals(id,org_id,parent_id,team_id,type_id,owner_member_id,title,description,status,budget,deadline,planned_start,planned_end,horizon,confidence,outcome,date_precision,rank,visibility,created_at,updated_at)
		values($1,$2,nullif($3,''),nullif($4,''),nullif($5,''),$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`,
		g.ID, g.OrgID, g.ParentID, g.TeamID, g.TypeID, g.OwnerMemberID, g.Title, g.Description, string(g.Status), g.Budget, g.Deadline, g.PlannedStart, g.PlannedEnd,
		string(g.Horizon), string(g.Confidence), g.Outcome, string(g.DatePrecision), g.Rank, g.Visibility, g.CreatedAt, g.UpdatedAt)
	return err
}

func (s *Store) UpdateGoal(ctx context.Context, q Querier, g *domain.Goal) error {
	g.UpdatedAt = time.Now()
	g.DatePrecision = domain.NormalizeDatePrecision(g.DatePrecision)
	tag, err := q.Exec(ctx, `update goals set parent_id=nullif($2,''),team_id=nullif($3,''),type_id=nullif($4,''),owner_member_id=$5,title=$6,description=$7,status=$8,budget=$9,deadline=$10,planned_start=$11,planned_end=$12,progress_override=$13,horizon=$14,confidence=$15,outcome=$16,date_precision=$17,rank=$18,visibility=$19,updated_at=$20,version=version+1 where id=$1 and version=$21`,
		g.ID, g.ParentID, g.TeamID, g.TypeID, g.OwnerMemberID, g.Title, g.Description, string(g.Status), g.Budget, g.Deadline, g.PlannedStart, g.PlannedEnd, g.ProgressOverride,
		string(g.Horizon), string(g.Confidence), g.Outcome, string(g.DatePrecision), g.Rank, g.Visibility, g.UpdatedAt, g.Version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrStale
	}
	g.Version++
	return nil
}

// GoalUsage 返回一个目标下的子目标数与任务数（含已取消的），用来判断能不能删。
func (s *Store) GoalUsage(ctx context.Context, q Querier, id string) (children, tasks int, err error) {
	if err = q.QueryRow(ctx, `select count(*) from goals where parent_id=$1`, id).Scan(&children); err != nil {
		return
	}
	err = q.QueryRow(ctx, `select count(*) from tasks where goal_id=$1`, id).Scan(&tasks)
	return
}

// DeleteGoal 物理删除一个目标。调用方必须先确认它没有子目标也没有任务。
// 里程碑不算「内容」：它们是目标自己的时间刻度（ADR 0016），没有任务那样的历史与成本，
// 随目标一起删（milestones.goal_id on delete cascade），所以这里不再单独检查。
func (s *Store) DeleteGoal(ctx context.Context, q Querier, id string) error {
	_, err := q.Exec(ctx, `delete from goals where id=$1`, id)
	return err
}

func (s *Store) GoalByID(ctx context.Context, q Querier, id string) (*domain.Goal, error) {
	return scanGoal(q.QueryRow(ctx, `select `+goalCols+` from goals where id=$1`, id))
}

func (s *Store) ListGoals(ctx context.Context, q Querier) ([]*domain.Goal, error) {
	rows, err := q.Query(ctx, `select `+goalCols+` from goals order by created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Goal
	for rows.Next() {
		g, err := scanGoal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
