package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/teemo/axiomos/internal/domain"
)

const sprintCols = `id,org_id,coalesce(team_id,''),name,goal,starts_on,ends_on,status,created_by,started_at,closed_at,created_at,updated_at`

// localDate 把数据库 date 列（UTC 零点）归一到当地零点，方便按天比较。
func localDate(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

func scanSprint(r interface{ Scan(...any) error }) (*domain.Sprint, error) {
	s := &domain.Sprint{}
	var status string
	err := r.Scan(&s.ID, &s.OrgID, &s.TeamID, &s.Name, &s.Goal, &s.StartsOn, &s.EndsOn, &status, &s.CreatedBy, &s.StartedAt, &s.ClosedAt, &s.CreatedAt, &s.UpdatedAt)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	s.Status = domain.SprintStatus(status)
	s.StartsOn, s.EndsOn = localDate(s.StartsOn), localDate(s.EndsOn)
	return s, nil
}

func dateArg(t time.Time) string { return t.Format("2006-01-02") }

func (s *Store) InsertSprint(ctx context.Context, q Querier, sp *domain.Sprint) error {
	if sp.ID == "" {
		sp.ID = NewID("spr")
	}
	if sp.Status == "" {
		sp.Status = domain.SprintPlanning
	}
	now := time.Now()
	sp.CreatedAt, sp.UpdatedAt = now, now
	_, err := q.Exec(ctx, `insert into sprints(id,org_id,team_id,name,goal,starts_on,ends_on,status,created_by,started_at,closed_at,created_at,updated_at)
		values($1,$2,nullif($3,''),$4,$5,$6::date,$7::date,$8,$9,$10,$11,$12,$13)`,
		sp.ID, sp.OrgID, sp.TeamID, sp.Name, sp.Goal, dateArg(sp.StartsOn), dateArg(sp.EndsOn), string(sp.Status), sp.CreatedBy, sp.StartedAt, sp.ClosedAt, sp.CreatedAt, sp.UpdatedAt)
	return err
}

func (s *Store) UpdateSprint(ctx context.Context, q Querier, sp *domain.Sprint) error {
	sp.UpdatedAt = time.Now()
	_, err := q.Exec(ctx, `update sprints set team_id=nullif($2,''),name=$3,goal=$4,starts_on=$5::date,ends_on=$6::date,status=$7,started_at=$8,closed_at=$9,updated_at=$10 where id=$1`,
		sp.ID, sp.TeamID, sp.Name, sp.Goal, dateArg(sp.StartsOn), dateArg(sp.EndsOn), string(sp.Status), sp.StartedAt, sp.ClosedAt, sp.UpdatedAt)
	return err
}

func (s *Store) SprintByID(ctx context.Context, q Querier, id string) (*domain.Sprint, error) {
	return scanSprint(q.QueryRow(ctx, `select `+sprintCols+` from sprints where id=$1`, id))
}

// SprintFilter 是迭代列表的过滤条件。
type SprintFilter struct {
	TeamID   string // 空表示不过滤
	OrgOnly  bool   // 只要没有团队的迭代
	Status   domain.SprintStatus
	Statuses []domain.SprintStatus
	Limit    int
}

// ListSprints 按开始日期倒序列出迭代（进行中的排最前）。
func (s *Store) ListSprints(ctx context.Context, q Querier, f SprintFilter) ([]*domain.Sprint, error) {
	var where []string
	var args []any
	arg := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
	if f.TeamID != "" {
		where = append(where, "team_id="+arg(f.TeamID))
	}
	if f.OrgOnly {
		where = append(where, "team_id is null")
	}
	if f.Status != "" {
		where = append(where, "status="+arg(string(f.Status)))
	}
	if len(f.Statuses) > 0 {
		ss := make([]string, len(f.Statuses))
		for i, st := range f.Statuses {
			ss[i] = string(st)
		}
		where = append(where, "status = any("+arg(ss)+")")
	}
	sql := `select ` + sprintCols + ` from sprints`
	if len(where) > 0 {
		sql += " where " + strings.Join(where, " and ")
	}
	sql += ` order by case status when 'active' then 0 when 'planning' then 1 else 2 end, starts_on desc, created_at desc`
	if f.Limit > 0 {
		sql += fmt.Sprintf(" limit %d", f.Limit)
	}
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Sprint
	for rows.Next() {
		sp, err := scanSprint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

// ActiveSprint 返回某团队（teamID 空表示无团队）当前进行中的迭代；没有返回 nil。
func (s *Store) ActiveSprint(ctx context.Context, q Querier, teamID string) (*domain.Sprint, error) {
	var row interface{ Scan(...any) error }
	if teamID == "" {
		row = q.QueryRow(ctx, `select `+sprintCols+` from sprints where team_id is null and status='active' order by started_at desc limit 1`)
	} else {
		row = q.QueryRow(ctx, `select `+sprintCols+` from sprints where team_id=$1 and status='active' order by started_at desc limit 1`, teamID)
	}
	sp, err := scanSprint(row)
	if err == ErrNotFound {
		return nil, nil
	}
	return sp, err
}

// SprintNames 返回迭代 ID → 名称。
func (s *Store) SprintNames(ctx context.Context, q Querier) (map[string]string, error) {
	rows, err := q.Query(ctx, `select id,name from sprints`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// SetTaskSprint 只改任务的迭代归属。
func (s *Store) SetTaskSprint(ctx context.Context, q Querier, taskID, sprintID string) error {
	_, err := q.Exec(ctx, `update tasks set sprint_id=nullif($2,''),updated_at=now(),version=version+1 where id=$1`, taskID, sprintID)
	return err
}

// EventsOfKinds 按时间升序返回某几种动态（燃尽图回放用）。
func (s *Store) EventsOfKinds(ctx context.Context, q Querier, kinds []string) ([]*EventRow, error) {
	rows, err := q.Query(ctx, `select id,type,coalesce(task_id,''),coalesce(actor_id,''),at,data from events where type = any($1) order by at, id`, kinds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*EventRow
	for rows.Next() {
		e := &EventRow{}
		var data []byte
		if err := rows.Scan(&e.ID, &e.Type, &e.TaskID, &e.ActorID, &e.At, &data); err != nil {
			return nil, err
		}
		e.Data = map[string]any{}
		_ = json.Unmarshal(data, &e.Data)
		out = append(out, e)
	}
	return out, rows.Err()
}
