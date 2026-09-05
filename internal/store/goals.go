package store

import (
	"context"
	"time"

	"github.com/teemo/axiomos/internal/domain"
)

const goalCols = `id,org_id,coalesce(parent_id,''),coalesce(team_id,''),owner_member_id,title,description,status,budget,deadline,planned_start,planned_end,progress_override,visibility,created_at,updated_at`

func scanGoal(r interface{ Scan(...any) error }) (*domain.Goal, error) {
	g := &domain.Goal{}
	var status string
	err := r.Scan(&g.ID, &g.OrgID, &g.ParentID, &g.TeamID, &g.OwnerMemberID, &g.Title, &g.Description, &status, &g.Budget, &g.Deadline, &g.PlannedStart, &g.PlannedEnd, &g.ProgressOverride, &g.Visibility, &g.CreatedAt, &g.UpdatedAt)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	g.Status = domain.GoalStatus(status)
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
	_, err := q.Exec(ctx, `insert into goals(id,org_id,parent_id,team_id,owner_member_id,title,description,status,budget,deadline,planned_start,planned_end,visibility,created_at,updated_at)
		values($1,$2,nullif($3,''),nullif($4,''),$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		g.ID, g.OrgID, g.ParentID, g.TeamID, g.OwnerMemberID, g.Title, g.Description, string(g.Status), g.Budget, g.Deadline, g.PlannedStart, g.PlannedEnd, g.Visibility, g.CreatedAt, g.UpdatedAt)
	return err
}

func (s *Store) UpdateGoal(ctx context.Context, q Querier, g *domain.Goal) error {
	g.UpdatedAt = time.Now()
	_, err := q.Exec(ctx, `update goals set parent_id=nullif($2,''),team_id=nullif($3,''),owner_member_id=$4,title=$5,description=$6,status=$7,budget=$8,deadline=$9,planned_start=$10,planned_end=$11,progress_override=$12,visibility=$13,updated_at=$14 where id=$1`,
		g.ID, g.ParentID, g.TeamID, g.OwnerMemberID, g.Title, g.Description, string(g.Status), g.Budget, g.Deadline, g.PlannedStart, g.PlannedEnd, g.ProgressOverride, g.Visibility, g.UpdatedAt)
	return err
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
