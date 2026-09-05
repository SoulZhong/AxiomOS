package store

import (
	"context"
	"time"

	"github.com/teemo/axiomos/internal/domain"
)

const milestoneCols = `id,org_id,goal_id,title,description,due_on,reached_at,created_by,created_at,updated_at`

func scanMilestone(r interface{ Scan(...any) error }) (*domain.Milestone, error) {
	m := &domain.Milestone{}
	err := r.Scan(&m.ID, &m.OrgID, &m.GoalID, &m.Title, &m.Description, &m.DueOn, &m.ReachedAt, &m.CreatedBy, &m.CreatedAt, &m.UpdatedAt)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.DueOn = localDate(m.DueOn)
	return m, nil
}

func (s *Store) InsertMilestone(ctx context.Context, q Querier, m *domain.Milestone) error {
	if m.ID == "" {
		m.ID = NewID("mls")
	}
	now := time.Now()
	m.CreatedAt, m.UpdatedAt = now, now
	_, err := q.Exec(ctx, `insert into milestones(id,org_id,goal_id,title,description,due_on,reached_at,created_by,created_at,updated_at)
		values($1,$2,$3,$4,$5,$6::date,$7,$8,$9,$10)`,
		m.ID, m.OrgID, m.GoalID, m.Title, m.Description, dateArg(m.DueOn), m.ReachedAt, m.CreatedBy, m.CreatedAt, m.UpdatedAt)
	return err
}

func (s *Store) UpdateMilestone(ctx context.Context, q Querier, m *domain.Milestone) error {
	m.UpdatedAt = time.Now()
	_, err := q.Exec(ctx, `update milestones set title=$2,description=$3,due_on=$4::date,reached_at=$5,updated_at=$6 where id=$1`,
		m.ID, m.Title, m.Description, dateArg(m.DueOn), m.ReachedAt, m.UpdatedAt)
	return err
}

func (s *Store) DeleteMilestone(ctx context.Context, q Querier, id string) error {
	_, err := q.Exec(ctx, `delete from milestones where id=$1`, id)
	return err
}

func (s *Store) MilestoneByID(ctx context.Context, q Querier, id string) (*domain.Milestone, error) {
	return scanMilestone(q.QueryRow(ctx, `select `+milestoneCols+` from milestones where id=$1`, id))
}

// MilestonesByGoal 列出一个目标自己的里程碑（不含子目标），按日期升序。
func (s *Store) MilestonesByGoal(ctx context.Context, q Querier, goalID string) ([]*domain.Milestone, error) {
	return s.MilestonesByGoals(ctx, q, []string{goalID})
}

// MilestonesByGoals 列出一组目标的里程碑（目标树与甘特图用），按日期升序。
func (s *Store) MilestonesByGoals(ctx context.Context, q Querier, goalIDs []string) ([]*domain.Milestone, error) {
	out := []*domain.Milestone{}
	if len(goalIDs) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `select `+milestoneCols+` from milestones where goal_id = any($1) order by due_on, created_at`, goalIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scanMilestone(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CountMilestones 返回一个目标自己的里程碑数。
func (s *Store) CountMilestones(ctx context.Context, q Querier, goalID string) (int, error) {
	n := 0
	err := q.QueryRow(ctx, `select count(*) from milestones where goal_id=$1`, goalID).Scan(&n)
	return n, err
}
