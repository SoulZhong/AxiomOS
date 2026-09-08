package store

import (
	"context"
	"time"

	"github.com/teemo/axiomos/internal/domain"
)

// 目标类型（ADR 0023）：组织自己维护的一张词表。这里只有存取，没有规则——
// 「有目标在用就不许删」由数据库的 on delete restrict 兜底，判断与措辞在应用层。

const goalTypeCols = `id,org_id,name,color,icon,sort,active,default_precision,created_at,updated_at`

func scanGoalType(r interface{ Scan(...any) error }) (*domain.GoalType, error) {
	t := &domain.GoalType{}
	var precision string
	err := r.Scan(&t.ID, &t.OrgID, &t.Name, &t.Color, &t.Icon, &t.Sort, &t.Active, &precision, &t.CreatedAt, &t.UpdatedAt)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	t.DefaultPrecision = domain.DatePrecision(precision)
	return t, err
}

// ListGoalTypes 按排序权重、创建时间列出全部类型（含已停用的：已有目标还要显示它们）。
func (s *Store) ListGoalTypes(ctx context.Context, q Querier) ([]*domain.GoalType, error) {
	rows, err := q.Query(ctx, `select `+goalTypeCols+` from goal_types order by sort, created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.GoalType{}
	for rows.Next() {
		t, err := scanGoalType(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) GoalTypeByID(ctx context.Context, q Querier, id string) (*domain.GoalType, error) {
	return scanGoalType(q.QueryRow(ctx, `select `+goalTypeCols+` from goal_types where id=$1`, id))
}

func (s *Store) CreateGoalType(ctx context.Context, q Querier, t *domain.GoalType) error {
	if t.ID == "" {
		t.ID = NewID("gtype")
	}
	now := time.Now()
	t.CreatedAt, t.UpdatedAt = now, now
	_, err := q.Exec(ctx, `insert into goal_types(id,org_id,name,color,icon,sort,active,default_precision,created_at,updated_at)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		t.ID, t.OrgID, t.Name, t.Color, t.Icon, t.Sort, t.Active, string(t.DefaultPrecision), t.CreatedAt, t.UpdatedAt)
	return err
}

func (s *Store) UpdateGoalType(ctx context.Context, q Querier, t *domain.GoalType) error {
	t.UpdatedAt = time.Now()
	_, err := q.Exec(ctx, `update goal_types set name=$2,color=$3,icon=$4,sort=$5,active=$6,default_precision=$7,updated_at=$8 where id=$1`,
		t.ID, t.Name, t.Color, t.Icon, t.Sort, t.Active, string(t.DefaultPrecision), t.UpdatedAt)
	return err
}

func (s *Store) DeleteGoalType(ctx context.Context, q Querier, id string) error {
	_, err := q.Exec(ctx, `delete from goal_types where id=$1`, id)
	return err
}

// GoalTypeUsage 数每个类型下面有多少个目标（含已放弃、已达成的：它们照样显示这个类型）。
func (s *Store) GoalTypeUsage(ctx context.Context, q Querier) (map[string]int, error) {
	rows, err := q.Query(ctx, `select type_id, count(*) from goals where type_id is not null group by type_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
