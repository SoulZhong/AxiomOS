package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/teemo/axiomos/internal/domain"
)

const taskCols = `id,org_id,coalesce(goal_id,''),coalesce(parent_id,''),type_name,type_version,title,description,creator_id,reviewer_id,coalesce(assignee_id,''),coalesce(required_role,''),coalesce(pending_slot,''),required_capabilities,human_only,state,coalesce(previous_state,''),last_weight,participants,fields,priority,estimate_hours,planned_start,planned_end,actual_start,actual_end,created_at,updated_at,points,coalesce(sprint_id,'')`

func scanTask(r interface{ Scan(...any) error }) (*domain.Task, error) {
	t := &domain.Task{}
	var participants, fields []byte
	err := r.Scan(&t.ID, &t.OrgID, &t.GoalID, &t.ParentID, &t.TypeName, &t.TypeVersion, &t.Title, &t.Description, &t.CreatorID, &t.ReviewerID, &t.AssigneeID, &t.RequiredRole, &t.PendingSlot, &t.RequiredCapabilities, &t.HumanOnly, &t.State, &t.PreviousState, &t.LastWeight, &participants, &fields, &t.Priority, &t.EstimateHours, &t.PlannedStart, &t.PlannedEnd, &t.ActualStart, &t.ActualEnd, &t.CreatedAt, &t.UpdatedAt, &t.Points, &t.SprintID)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	t.Participants = map[string]string{}
	_ = json.Unmarshal(participants, &t.Participants)
	t.Fields = map[string]any{}
	_ = json.Unmarshal(fields, &t.Fields)
	if t.RequiredCapabilities == nil {
		t.RequiredCapabilities = []string{}
	}
	t.Artifacts, t.Comments, t.Relations = []domain.Artifact{}, []domain.Comment{}, []domain.Relation{}
	return t, nil
}

func (s *Store) InsertTask(ctx context.Context, q Querier, t *domain.Task) error {
	if t.ID == "" {
		t.ID = NewID("tsk")
	}
	now := time.Now()
	t.CreatedAt, t.UpdatedAt = now, now
	participants, _ := json.Marshal(t.Participants)
	fields, _ := json.Marshal(t.Fields)
	if t.RequiredCapabilities == nil {
		t.RequiredCapabilities = []string{}
	}
	_, err := q.Exec(ctx, `insert into tasks(id,org_id,goal_id,parent_id,type_name,type_version,title,description,creator_id,reviewer_id,assignee_id,required_role,pending_slot,required_capabilities,human_only,state,previous_state,last_weight,participants,fields,priority,estimate_hours,planned_start,planned_end,actual_start,actual_end,created_at,updated_at,points,sprint_id)
		values($1,$2,nullif($3,''),nullif($4,''),$5,$6,$7,$8,$9,$10,nullif($11,''),nullif($12,''),nullif($13,''),$14,$15,$16,nullif($17,''),$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,nullif($30,''))`,
		t.ID, t.OrgID, t.GoalID, t.ParentID, t.TypeName, t.TypeVersion, t.Title, t.Description, t.CreatorID, t.ReviewerID, t.AssigneeID, t.RequiredRole, t.PendingSlot, t.RequiredCapabilities, t.HumanOnly, t.State, t.PreviousState, t.LastWeight, participants, fields, t.Priority, t.EstimateHours, t.PlannedStart, t.PlannedEnd, t.ActualStart, t.ActualEnd, t.CreatedAt, t.UpdatedAt, t.Points, t.SprintID)
	return err
}

// UpdateTask 写回任务主表（关联、交付物、评论另有表）。
func (s *Store) UpdateTask(ctx context.Context, q Querier, t *domain.Task) error {
	t.UpdatedAt = time.Now()
	participants, _ := json.Marshal(t.Participants)
	fields, _ := json.Marshal(t.Fields)
	_, err := q.Exec(ctx, `update tasks set goal_id=nullif($2,''),parent_id=nullif($3,''),title=$4,description=$5,reviewer_id=$6,assignee_id=nullif($7,''),required_role=nullif($8,''),pending_slot=nullif($9,''),required_capabilities=$10,human_only=$11,state=$12,previous_state=nullif($13,''),last_weight=$14,participants=$15,fields=$16,priority=$17,estimate_hours=$18,planned_start=$19,planned_end=$20,actual_start=$21,actual_end=$22,updated_at=$23,points=$24,sprint_id=nullif($25,'') where id=$1`,
		t.ID, t.GoalID, t.ParentID, t.Title, t.Description, t.ReviewerID, t.AssigneeID, t.RequiredRole, t.PendingSlot, t.RequiredCapabilities, t.HumanOnly, t.State, t.PreviousState, t.LastWeight, participants, fields, t.Priority, t.EstimateHours, t.PlannedStart, t.PlannedEnd, t.ActualStart, t.ActualEnd, t.UpdatedAt, t.Points, t.SprintID)
	return err
}

// TaskByID 装载任务及其关联、交付物、评论。
func (s *Store) TaskByID(ctx context.Context, q Querier, id string) (*domain.Task, error) {
	t, err := scanTask(q.QueryRow(ctx, `select `+taskCols+` from tasks where id=$1`, id))
	if err != nil {
		return nil, err
	}
	if err := s.loadTaskChildren(ctx, q, []*domain.Task{t}); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Store) loadTaskChildren(ctx context.Context, q Querier, tasks []*domain.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	byID := map[string]*domain.Task{}
	ids := make([]string, 0, len(tasks))
	for _, t := range tasks {
		byID[t.ID] = t
		ids = append(ids, t.ID)
	}
	rows, err := q.Query(ctx, `select task_id,type,other_id from task_relations where task_id = any($1) order by created_at`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var tid, typ, other string
		if err := rows.Scan(&tid, &typ, &other); err != nil {
			rows.Close()
			return err
		}
		byID[tid].Relations = append(byID[tid].Relations, domain.Relation{Type: domain.RelationType(typ), OtherID: other})
	}
	rows.Close()
	rows, err = q.Query(ctx, `select id,task_id,type,title,ref,by_id,created_at from artifacts where task_id = any($1) order by created_at`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var a domain.Artifact
		var tid string
		if err := rows.Scan(&a.ID, &tid, &a.Type, &a.Title, &a.Ref, &a.ByID, &a.CreatedAt); err != nil {
			rows.Close()
			return err
		}
		byID[tid].Artifacts = append(byID[tid].Artifacts, a)
	}
	rows.Close()
	rows, err = q.Query(ctx, `select id,task_id,by_id,text,is_note,created_at from comments where task_id = any($1) order by created_at`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var c domain.Comment
		var tid string
		if err := rows.Scan(&c.ID, &tid, &c.ByID, &c.Text, &c.IsNote, &c.CreatedAt); err != nil {
			rows.Close()
			return err
		}
		byID[tid].Comments = append(byID[tid].Comments, c)
	}
	rows.Close()
	return nil
}

// TaskFilter 是任务列表的过滤条件。
type TaskFilter struct {
	GoalID     string
	AssigneeID string
	Executors  []string // 负责人属于这些 ID 之一（成员本人 + 他的 Agent）
	State      string
	TypeName   string
	Backlog    bool // 无负责人
	Open       bool // 非终止（由应用层按类型过滤，这里只排除 assignee 条件）
	ParentID   string
	SprintID   string // 属于某个迭代
	NoSprint   bool   // 不在任何迭代里
	Limit      int
}

func (s *Store) ListTasks(ctx context.Context, q Querier, f TaskFilter) ([]*domain.Task, error) {
	var where []string
	var args []any
	arg := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
	if f.GoalID != "" {
		where = append(where, "goal_id="+arg(f.GoalID))
	}
	if f.ParentID != "" {
		where = append(where, "parent_id="+arg(f.ParentID))
	}
	if f.AssigneeID != "" {
		where = append(where, "assignee_id="+arg(f.AssigneeID))
	}
	if len(f.Executors) > 0 {
		where = append(where, "assignee_id = any("+arg(f.Executors)+")")
	}
	if f.State != "" {
		where = append(where, "state="+arg(f.State))
	}
	if f.TypeName != "" {
		where = append(where, "type_name="+arg(f.TypeName))
	}
	if f.Backlog {
		where = append(where, "assignee_id is null")
	}
	if f.SprintID != "" {
		where = append(where, "sprint_id="+arg(f.SprintID))
	}
	if f.NoSprint {
		where = append(where, "sprint_id is null")
	}
	sql := `select ` + taskCols + ` from tasks`
	if len(where) > 0 {
		sql += " where " + strings.Join(where, " and ")
	}
	sql += " order by priority, coalesce(planned_end, '9999-12-31'), created_at"
	if f.Limit > 0 {
		sql += fmt.Sprintf(" limit %d", f.Limit)
	}
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	var out []*domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, t)
	}
	rows.Close()
	if err := s.loadTaskChildren(ctx, q, out); err != nil {
		return nil, err
	}
	return out, nil
}

// TasksByIDs 批量装载。
func (s *Store) TasksByIDs(ctx context.Context, q Querier, ids []string) ([]*domain.Task, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := q.Query(ctx, `select `+taskCols+` from tasks where id = any($1)`, ids)
	if err != nil {
		return nil, err
	}
	var out []*domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, t)
	}
	rows.Close()
	return out, s.loadTaskChildren(ctx, q, out)
}

// Predecessors 返回以 taskID 为后置的前置任务（它们 blocks taskID）。
func (s *Store) Predecessors(ctx context.Context, q Querier, taskID string) ([]*domain.Task, error) {
	return s.relatedBy(ctx, q, taskID, domain.RelationBlocks, "")
}

// BugsFoundIn 返回「发现于」taskID 的 Bug 类型任务。
func (s *Store) BugsFoundIn(ctx context.Context, q Querier, taskID string) ([]*domain.Task, error) {
	return s.relatedBy(ctx, q, taskID, domain.RelationFoundIn, "bug")
}

func (s *Store) relatedBy(ctx context.Context, q Querier, otherID string, typ domain.RelationType, typeName string) ([]*domain.Task, error) {
	rows, err := q.Query(ctx, `select task_id from task_relations where other_id=$1 and type=$2`, otherID, string(typ))
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	tasks, err := s.TasksByIDs(ctx, q, ids)
	if err != nil || typeName == "" {
		return tasks, err
	}
	var out []*domain.Task
	for _, t := range tasks {
		if t.TypeName == typeName {
			out = append(out, t)
		}
	}
	return out, nil
}

// BlocksOf 返回某任务作为前置指向的后置任务 ID（绕圈检测用）。
func (s *Store) BlocksOf(ctx context.Context, q Querier, taskID string) ([]string, error) {
	rows, err := q.Query(ctx, `select other_id from task_relations where task_id=$1 and type='blocks'`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SyncRelations 让 task_relations 与任务上的 Relations 一致（用于自动解除前置）。
func (s *Store) SyncRelations(ctx context.Context, q Querier, t *domain.Task) error {
	if _, err := q.Exec(ctx, `delete from task_relations where task_id=$1`, t.ID); err != nil {
		return err
	}
	for _, r := range t.Relations {
		if _, err := q.Exec(ctx, `insert into task_relations(org_id,task_id,type,other_id) values($1,$2,$3,$4) on conflict do nothing`, t.OrgID, t.ID, string(r.Type), r.OtherID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) InsertArtifact(ctx context.Context, q Querier, orgID, taskID string, a domain.Artifact) error {
	_, err := q.Exec(ctx, `insert into artifacts(id,org_id,task_id,type,title,ref,by_id,created_at) values($1,$2,$3,$4,$5,$6,$7,$8)`, a.ID, orgID, taskID, a.Type, a.Title, a.Ref, a.ByID, a.CreatedAt)
	return err
}

func (s *Store) InsertComment(ctx context.Context, q Querier, orgID, taskID string, c domain.Comment) error {
	_, err := q.Exec(ctx, `insert into comments(id,org_id,task_id,by_id,text,is_note,created_at) values($1,$2,$3,$4,$5,$6,$7)`, c.ID, orgID, taskID, c.ByID, c.Text, c.IsNote, c.CreatedAt)
	return err
}

// AllTasks 返回组织内全部任务（甘特图与统计用）。
func (s *Store) AllTasks(ctx context.Context, q Querier) ([]*domain.Task, error) {
	return s.ListTasks(ctx, q, TaskFilter{})
}

// IncomingRel 是指向某任务的一条关联及其来源任务。
type IncomingRel struct {
	Type domain.RelationType
	Task *domain.Task
}

// IncomingRelations 返回所有指向 taskID 的关联。
func (s *Store) IncomingRelations(ctx context.Context, q Querier, taskID string) ([]IncomingRel, error) {
	rows, err := q.Query(ctx, `select task_id,type from task_relations where other_id=$1 order by created_at`, taskID)
	if err != nil {
		return nil, err
	}
	type pair struct{ id, typ string }
	var pairs []pair
	var ids []string
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.id, &p.typ); err != nil {
			rows.Close()
			return nil, err
		}
		pairs = append(pairs, p)
		ids = append(ids, p.id)
	}
	rows.Close()
	tasks, err := s.TasksByIDs(ctx, q, ids)
	if err != nil {
		return nil, err
	}
	byID := map[string]*domain.Task{}
	for _, t := range tasks {
		byID[t.ID] = t
	}
	var out []IncomingRel
	for _, p := range pairs {
		if t := byID[p.id]; t != nil {
			out = append(out, IncomingRel{Type: domain.RelationType(p.typ), Task: t})
		}
	}
	return out, nil
}
