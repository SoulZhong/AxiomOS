package store

import (
	"context"
	"encoding/json"

	"github.com/teemo/axiomos/internal/domain"
)

// ---------- 工作台布局（ADR 0015） ----------

const layoutCols = `coalesce(role_name,''),coalesce(member_id,''),blocks,coalesce(preset,''),updated_at`

func scanLayout(r interface{ Scan(...any) error }) (*domain.WorkspaceLayout, error) {
	l := &domain.WorkspaceLayout{}
	var blocks []byte
	err := r.Scan(&l.RoleName, &l.MemberID, &blocks, &l.Preset, &l.UpdatedAt)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	// 兼容：blocks 先后存过区块键数组（["my_tasks", ...]）、{key, w, h} 对象（补记二）与 {key, x, y, w, h}
	// 对象（补记三）。三种形状都读：旧行缺省的宽高按目录默认值补齐，缺 x / y 的按顺序致密排布（读时排、不迁移）；
	// 这里只解析形状、不校验，写入时已经校验过。
	var raw []any
	_ = json.Unmarshal(blocks, &raw)
	l.Blocks, _ = domain.ParseLayout(raw)
	if l.Blocks == nil {
		l.Blocks = []domain.LayoutBlock{}
	}
	return l, nil
}

// RoleLayout 读一个角色的布局。
func (s *Store) RoleLayout(ctx context.Context, q Querier, role string) (*domain.WorkspaceLayout, error) {
	return scanLayout(q.QueryRow(ctx, `select `+layoutCols+` from workspace_layouts where role_name=$1`, role))
}

// MemberLayout 读一个成员的个人微调。
func (s *Store) MemberLayout(ctx context.Context, q Querier, memberID string) (*domain.WorkspaceLayout, error) {
	return scanLayout(q.QueryRow(ctx, `select `+layoutCols+` from workspace_layouts where member_id=$1`, memberID))
}

// ListRoleLayouts 列出组织里全部角色布局。
func (s *Store) ListRoleLayouts(ctx context.Context, q Querier) ([]*domain.WorkspaceLayout, error) {
	rows, err := q.Query(ctx, `select `+layoutCols+` from workspace_layouts where role_name is not null order by role_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.WorkspaceLayout
	for rows.Next() {
		l, err := scanLayout(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// marshalLayout 把布局编成 jsonb：总是对象数组，nil 也编成 []。
func marshalLayout(blocks []domain.LayoutBlock) []byte {
	if blocks == nil {
		blocks = []domain.LayoutBlock{}
	}
	b, _ := json.Marshal(blocks)
	return b
}

// PutRoleLayout 写入或覆盖一个角色的布局。preset 为空表示逐块配置。
func (s *Store) PutRoleLayout(ctx context.Context, q Querier, orgID, role string, blocks []domain.LayoutBlock, preset string) error {
	b := marshalLayout(blocks)
	_, err := q.Exec(ctx, `insert into workspace_layouts(org_id,role_name,blocks,preset,updated_at) values($1,$2,$3,nullif($4,''),now())
		on conflict (org_id,role_name) where role_name is not null do update set blocks=excluded.blocks, preset=excluded.preset, updated_at=now()`, orgID, role, b, preset)
	return err
}

// PutMemberLayout 写入或覆盖一个成员的个人微调。
func (s *Store) PutMemberLayout(ctx context.Context, q Querier, orgID, memberID string, blocks []domain.LayoutBlock) error {
	b := marshalLayout(blocks)
	_, err := q.Exec(ctx, `insert into workspace_layouts(org_id,member_id,blocks,preset,updated_at) values($1,$2,$3,null,now())
		on conflict (org_id,member_id) where member_id is not null do update set blocks=excluded.blocks, preset=null, updated_at=now()`, orgID, memberID, b)
	return err
}

// DeleteRoleLayout 清除一个角色的布局。
func (s *Store) DeleteRoleLayout(ctx context.Context, q Querier, role string) error {
	_, err := q.Exec(ctx, `delete from workspace_layouts where role_name=$1`, role)
	return err
}

// DeleteMemberLayout 清除一个成员的个人微调。
func (s *Store) DeleteMemberLayout(ctx context.Context, q Querier, memberID string) error {
	_, err := q.Exec(ctx, `delete from workspace_layouts where member_id=$1`, memberID)
	return err
}
