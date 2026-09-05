package app

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 工作台（ADR 0015）：首页由区块按顺序组成。解析顺序：个人微调 → 各角色布局并集（按成员的
// 角色顺序、首次出现去重）→ 默认（组织负责人全局视角，其他人执行视角）。
// 布局按角色配是组织的管理策略；区块与预设的集合是系统定义的（ADR 0014）。

// WorkspaceBlock 是已解析工作台里的一个区块。
type WorkspaceBlock struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

// Workspace 是某个人已解析好的工作台。
type Workspace struct {
	Blocks       []WorkspaceBlock `json:"blocks"`
	Source       string           `json:"source"` // personal | roles | default
	RolesUsed    []string         `json:"roles_used"`
	CanCustomize bool             `json:"can_customize"`
}

// BlockInfo 是目录里的一个区块。
type BlockInfo struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// PresetInfo 是目录里的一个预设。
type PresetInfo struct {
	Key         string   `json:"key"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Blocks      []string `json:"blocks"`
}

// WorkspaceCatalogView 是全部区块与预设。
type WorkspaceCatalogView struct {
	Blocks  []BlockInfo  `json:"blocks"`
	Presets []PresetInfo `json:"presets"`
}

// RoleLayoutView 是组织设置里一个角色的布局。没有配置时 Blocks 是解析出来的默认值，Preset 为 null。
type RoleLayoutView struct {
	Role        string   `json:"role"`
	RoleTitle   string   `json:"role_title"`
	Blocks      []string `json:"blocks"`
	Preset      *string  `json:"preset"`
	MemberCount int      `json:"member_count"`
}

func blockTitles(keys []string, loc i18n.Locale) []WorkspaceBlock {
	out := make([]WorkspaceBlock, 0, len(keys))
	for _, k := range keys {
		b, _ := domain.BlockByKey(k)
		out = append(out, WorkspaceBlock{Key: k, Title: b.Title.In(loc)})
	}
	return out
}

func blocksError(err error) error {
	if we, ok := err.(*domain.WorkspaceError); ok {
		return &UserError{Status: 400, Reasons: []i18n.Msg{we.Msg}}
	}
	return err
}

// requireHuman 工作台只属于人：Agent 没有第一屏。
func requireHuman(sess *Session) error {
	if sess.IsAgent() {
		return Forbidden("err.workspace_agent")
	}
	return nil
}

// resolveFor 在事务里解析一个成员的工作台。
func (a *App) resolveFor(ctx context.Context, tx pgx.Tx, sess *Session, memberID string) (*Workspace, error) {
	m, err := a.Store.MemberByID(ctx, tx, memberID)
	if err != nil {
		return nil, err
	}
	var personal []string
	if pl, err := a.Store.MemberLayout(ctx, tx, memberID); err == nil {
		personal = pl.Blocks
	} else if err != store.ErrNotFound {
		return nil, err
	}
	layouts, err := a.Store.ListRoleLayouts(ctx, tx)
	if err != nil {
		return nil, err
	}
	byRole := map[string]*domain.WorkspaceLayout{}
	for _, l := range layouts {
		byRole[l.RoleName] = l
	}
	var roleLayouts [][]string
	var used []string
	for _, r := range m.Roles {
		if l := byRole[r]; l != nil && len(l.Blocks) > 0 {
			roleLayouts = append(roleLayouts, l.Blocks)
			used = append(used, r)
		}
	}
	blocks, source := domain.ResolveWorkspace(personal, roleLayouts, sess.IsOwner)
	ws := &Workspace{Blocks: blockTitles(blocks, sess.Loc()), Source: source, RolesUsed: []string{}, CanCustomize: true}
	if source == domain.WorkspaceSourceRoles {
		ws.RolesUsed = used
	}
	return ws, nil
}

// MyWorkspace 返回当前成员已解析的工作台。
func (a *App) MyWorkspace(ctx context.Context, sess *Session) (*Workspace, error) {
	if err := requireHuman(sess); err != nil {
		return nil, err
	}
	var out *Workspace
	err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		out, err = a.resolveFor(ctx, tx, sess, sess.MemberID)
		return
	})
	return out, err
}

// SetMyWorkspace 保存个人微调，只影响自己。
func (a *App) SetMyWorkspace(ctx context.Context, sess *Session, blocks []string) (*Workspace, error) {
	if err := requireHuman(sess); err != nil {
		return nil, err
	}
	if err := domain.ValidateBlocks(blocks); err != nil {
		return nil, blocksError(err)
	}
	var out *Workspace
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		if err := a.Store.PutMemberLayout(ctx, tx, sess.OrgID, sess.MemberID, blocks); err != nil {
			return err
		}
		if err := a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "WorkspaceLayoutUpdated", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"target": "member", "member": sess.MemberID, "blocks": blocks}}}); err != nil {
			return err
		}
		var err error
		out, err = a.resolveFor(ctx, tx, sess, sess.MemberID)
		return err
	})
	return out, err
}

// ClearMyWorkspace 清除个人微调，回到角色默认。
func (a *App) ClearMyWorkspace(ctx context.Context, sess *Session) (*Workspace, error) {
	if err := requireHuman(sess); err != nil {
		return nil, err
	}
	var out *Workspace
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		if err := a.Store.DeleteMemberLayout(ctx, tx, sess.MemberID); err != nil {
			return err
		}
		if err := a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "WorkspaceLayoutUpdated", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"target": "member", "member": sess.MemberID, "cleared": true}}}); err != nil {
			return err
		}
		var err error
		out, err = a.resolveFor(ctx, tx, sess, sess.MemberID)
		return err
	})
	return out, err
}

// WorkspaceCatalog 返回全部区块与预设，标题按请求者语言。
func (a *App) WorkspaceCatalog(sess *Session) *WorkspaceCatalogView {
	loc := sess.Loc()
	out := &WorkspaceCatalogView{Blocks: []BlockInfo{}, Presets: []PresetInfo{}}
	for _, b := range domain.Blocks {
		out.Blocks = append(out.Blocks, BlockInfo{Key: b.Key, Title: b.Title.In(loc), Description: b.Description.In(loc)})
	}
	for _, p := range domain.Presets {
		out.Presets = append(out.Presets, PresetInfo{Key: p.Key, Title: p.Title.In(loc), Description: p.Description.In(loc), Blocks: append([]string{}, p.Blocks...)})
	}
	return out
}

// roleLayoutView 组一个角色的布局视图；没有布局时区块取非负责人的默认预设，preset 为 null。
func roleLayoutView(r *domain.Role, l *domain.WorkspaceLayout, memberCount int, loc i18n.Locale) RoleLayoutView {
	v := RoleLayoutView{Role: r.Name, RoleTitle: r.Title.In(loc), MemberCount: memberCount}
	if l != nil && len(l.Blocks) > 0 {
		v.Blocks = append([]string{}, l.Blocks...)
		if l.Preset != "" {
			p := l.Preset
			v.Preset = &p
		}
		return v
	}
	def, _ := domain.PresetByKey(domain.DefaultPreset(false))
	v.Blocks = append([]string{}, def.Blocks...)
	return v
}

// activeMembersByRole 统计每个角色有多少在职成员。
func (a *App) activeMembersByRole(ctx context.Context, tx pgx.Tx) (map[string]int, error) {
	ms, err := a.Store.ListMembers(ctx, tx)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, m := range ms {
		if !m.Active {
			continue
		}
		for _, r := range m.Roles {
			counts[r]++
		}
	}
	return counts, nil
}

// OrgWorkspaceLayouts 列出各角色的布局（需要「组织设置」权限）。没有布局的角色也列出来，区块是解析出的默认值。
func (a *App) OrgWorkspaceLayouts(ctx context.Context, sess *Session) ([]RoleLayoutView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	out := []RoleLayoutView{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		roles, err := a.Store.ListRoles(ctx, tx)
		if err != nil {
			return err
		}
		layouts, err := a.Store.ListRoleLayouts(ctx, tx)
		if err != nil {
			return err
		}
		byRole := map[string]*domain.WorkspaceLayout{}
		for _, l := range layouts {
			byRole[l.RoleName] = l
		}
		counts, err := a.activeMembersByRole(ctx, tx)
		if err != nil {
			return err
		}
		for _, r := range roles {
			out = append(out, roleLayoutView(r, byRole[r.Name], counts[r.Name], sess.Loc()))
		}
		return nil
	})
	return out, err
}

// roleByName 在事务里找角色；不存在返回 404。
func (a *App) roleByName(ctx context.Context, tx pgx.Tx, name string) (*domain.Role, error) {
	roles, err := a.Store.ListRoles(ctx, tx)
	if err != nil {
		return nil, err
	}
	for _, r := range roles {
		if r.Name == name {
			return r, nil
		}
	}
	return nil, NotFound("err.role_unknown", name)
}

// SetRoleWorkspace 给角色配布局：给 preset 就整套套用，给 blocks 就逐块配置，二者选一。需要「组织设置」权限。
func (a *App) SetRoleWorkspace(ctx context.Context, sess *Session, role string, blocks []string, preset string) (*RoleLayoutView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	preset = strings.TrimSpace(preset)
	if (preset == "") == (len(blocks) == 0) {
		return nil, Bad("err.workspace_body")
	}
	if preset != "" {
		p, ok := domain.PresetByKey(preset)
		if !ok {
			return nil, Bad("err.preset_unknown", preset)
		}
		blocks = append([]string{}, p.Blocks...)
	} else if err := domain.ValidateBlocks(blocks); err != nil {
		return nil, blocksError(err)
	}
	var out *RoleLayoutView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		r, err := a.roleByName(ctx, tx, role)
		if err != nil {
			return err
		}
		if err := a.Store.PutRoleLayout(ctx, tx, sess.OrgID, role, blocks, preset); err != nil {
			return err
		}
		if err := a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "WorkspaceLayoutUpdated", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"target": "role", "role": role, "preset": preset, "blocks": blocks}}}); err != nil {
			return err
		}
		l, err := a.Store.RoleLayout(ctx, tx, role)
		if err != nil {
			return err
		}
		counts, err := a.activeMembersByRole(ctx, tx)
		if err != nil {
			return err
		}
		v := roleLayoutView(r, l, counts[role], sess.Loc())
		out = &v
		return nil
	})
	return out, err
}

// ClearRoleWorkspace 清除角色布局，回到默认。需要「组织设置」权限。
func (a *App) ClearRoleWorkspace(ctx context.Context, sess *Session, role string) (*RoleLayoutView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	var out *RoleLayoutView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		r, err := a.roleByName(ctx, tx, role)
		if err != nil {
			return err
		}
		// 不物理删除，而是留一条空布局作为"明确清除"的记录：解析时空布局等同于没有布局，
		// 但 EnsureOrgDefaults 看到有记录就不会在下次启动时把内置预设重新填回来。
		if err := a.Store.PutRoleLayout(ctx, tx, sess.OrgID, role, []string{}, ""); err != nil {
			return err
		}
		if err := a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "WorkspaceLayoutUpdated", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"target": "role", "role": role, "cleared": true}}}); err != nil {
			return err
		}
		counts, err := a.activeMembersByRole(ctx, tx)
		if err != nil {
			return err
		}
		v := roleLayoutView(r, nil, counts[role], sess.Loc())
		out = &v
		return nil
	})
	return out, err
}

// ensureRoleLayoutDefaults 给还没有布局的内置角色套上默认预设（幂等，从不覆盖已有布局）。
// 管理员 → 全局视角；运营、流程管理 → 运营视角；产品、设计、开发、测试、发布 → 执行视角。
func (a *App) ensureRoleLayoutDefaults(ctx context.Context, tx pgx.Tx, orgID string) error {
	layouts, err := a.Store.ListRoleLayouts(ctx, tx)
	if err != nil {
		return err
	}
	has := map[string]bool{}
	for _, l := range layouts {
		has[l.RoleName] = true
	}
	for _, br := range domain.BuiltinRoles {
		if has[br.Name] {
			continue
		}
		key := domain.BuiltinRolePreset(br.Name)
		p, ok := domain.PresetByKey(key)
		if !ok {
			continue
		}
		if err := a.Store.PutRoleLayout(ctx, tx, orgID, br.Name, append([]string{}, p.Blocks...), key); err != nil {
			return err
		}
	}
	return nil
}
