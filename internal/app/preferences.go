package app

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 显示偏好（功能规划第 4 项）：任务列表的列、看板卡片的字段、默认任务视图、紧凑模式、侧栏默认状态。
// 解析顺序与工作台一致（ADR 0015）：个人 → 角色（按成员的角色顺序取第一个设了这项的）→ 系统默认。
// 列与字段的集合是系统定义的（ADR 0014）；组织给角色配默认值是管理策略，个人再微调。

// PreferencesView 是解析完成的偏好 + 来源。
type PreferencesView struct {
	domain.Preferences
	Source    string            `json:"source"`     // personal | role | default（整体）
	Sources   map[string]string `json:"sources"`    // 每个字段的来源
	RolesUsed []string          `json:"roles_used"` // 贡献了字段的角色
	Overrides []string          `json:"overrides"`  // 个人明确设过的字段
}

// PreferenceItemView 是目录里的一项。
type PreferenceItemView struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

// PreferenceCatalogView 是全部可选列、字段与视图，以及系统默认值。
type PreferenceCatalogView struct {
	Columns    []PreferenceItemView `json:"columns"`
	CardFields []PreferenceItemView `json:"card_fields"`
	Views      []PreferenceItemView `json:"views"`
	Defaults   domain.Preferences   `json:"defaults"`
}

// RolePreferencesView 是组织设置里一个角色的偏好：只含它自己设了的字段。
type RolePreferencesView struct {
	Role        string                 `json:"role"`
	RoleTitle   string                 `json:"role_title"`
	Data        domain.PreferencePatch `json:"data"`
	Fields      []string               `json:"fields"`
	MemberCount int                    `json:"member_count"`
}

func prefError(err error) error {
	if pe, ok := err.(*domain.PreferenceError); ok {
		return &UserError{Status: 400, Reasons: []i18n.Msg{pe.Msg}}
	}
	return err
}

func requireHumanPreferences(sess *Session) error {
	if sess.IsAgent() {
		return Forbidden("err.preferences_agent")
	}
	return nil
}

// resolvePreferencesFor 在事务里解析一个成员的偏好。
func (a *App) resolvePreferencesFor(ctx context.Context, tx pgx.Tx, memberID string) (*PreferencesView, error) {
	m, err := a.Store.MemberByID(ctx, tx, memberID)
	if err != nil {
		return nil, err
	}
	var personal *domain.PreferencePatch
	if row, err := a.Store.Preference(ctx, tx, "member", memberID); err == nil {
		p := row.Patch
		personal = &p
	} else if err != store.ErrNotFound {
		return nil, err
	}
	rows, err := a.Store.ListRolePreferences(ctx, tx)
	if err != nil {
		return nil, err
	}
	byRole := map[string]domain.PreferencePatch{}
	for _, r := range rows {
		byRole[r.Subject] = r.Patch
	}
	prefs, sources, overall := domain.ResolvePreferences(personal, m.Roles, byRole)
	v := &PreferencesView{Preferences: prefs, Source: overall, Sources: sources, RolesUsed: []string{}, Overrides: []string{}}
	for _, r := range m.Roles {
		if p, ok := byRole[r]; ok && !p.IsEmpty() {
			v.RolesUsed = append(v.RolesUsed, r)
		}
	}
	if personal != nil {
		v.Overrides = personal.Fields()
		if v.Overrides == nil {
			v.Overrides = []string{}
		}
	}
	return v, nil
}

// MyPreferences 返回当前成员解析后的显示偏好。
func (a *App) MyPreferences(ctx context.Context, sess *Session) (*PreferencesView, error) {
	if err := requireHumanPreferences(sess); err != nil {
		return nil, err
	}
	var out *PreferencesView
	err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		out, err = a.resolvePreferencesFor(ctx, tx, sess.MemberID)
		return
	})
	return out, err
}

// SetMyPreferences 部分修改个人偏好：只改请求里带的字段，传 null 的字段回到角色 / 默认。
func (a *App) SetMyPreferences(ctx context.Context, sess *Session, raw []byte) (*PreferencesView, error) {
	if err := requireHumanPreferences(sess); err != nil {
		return nil, err
	}
	in, cleared, err := domain.ParsePreferencePatch(raw)
	if err != nil {
		return nil, prefError(err)
	}
	if in.IsEmpty() && len(cleared) == 0 {
		return nil, Bad("err.pref_body_empty")
	}
	if err := domain.ValidatePreferencePatch(in); err != nil {
		return nil, prefError(err)
	}
	var out *PreferencesView
	err = a.tx(ctx, sess, func(tx pgx.Tx) error {
		var base domain.PreferencePatch
		if row, err := a.Store.Preference(ctx, tx, "member", sess.MemberID); err == nil {
			base = row.Patch
		} else if err != store.ErrNotFound {
			return err
		}
		merged := domain.MergePreferencePatch(base, in, cleared)
		if merged.IsEmpty() {
			if err := a.Store.DeletePreference(ctx, tx, "member", sess.MemberID); err != nil {
				return err
			}
		} else if err := a.Store.PutPreference(ctx, tx, sess.OrgID, "member", sess.MemberID, merged); err != nil {
			return err
		}
		if err := a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "PreferencesUpdated", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"target": "member", "member": sess.MemberID, "fields": append(in.Fields(), cleared...)}}}); err != nil {
			return err
		}
		var err error
		out, err = a.resolvePreferencesFor(ctx, tx, sess.MemberID)
		return err
	})
	return out, err
}

// ClearMyPreferences 一键重置：清掉个人记录，回到角色 / 默认。
func (a *App) ClearMyPreferences(ctx context.Context, sess *Session) (*PreferencesView, error) {
	if err := requireHumanPreferences(sess); err != nil {
		return nil, err
	}
	var out *PreferencesView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		if err := a.Store.DeletePreference(ctx, tx, "member", sess.MemberID); err != nil {
			return err
		}
		if err := a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "PreferencesUpdated", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"target": "member", "member": sess.MemberID, "cleared": true}}}); err != nil {
			return err
		}
		var err error
		out, err = a.resolvePreferencesFor(ctx, tx, sess.MemberID)
		return err
	})
	return out, err
}

// PreferenceCatalog 返回可选的列、字段、视图与系统默认值，标题按请求者语言。
func (a *App) PreferenceCatalog(sess *Session) *PreferenceCatalogView {
	loc := sess.Loc()
	conv := func(items []domain.PreferenceItem) []PreferenceItemView {
		out := make([]PreferenceItemView, 0, len(items))
		for _, it := range items {
			out = append(out, PreferenceItemView{Key: it.Key, Title: it.Title.In(loc)})
		}
		return out
	}
	return &PreferenceCatalogView{Columns: conv(domain.TaskListColumns), CardFields: conv(domain.TaskCardFields), Views: conv(domain.TaskViews), Defaults: domain.DefaultPreferences()}
}

func rolePreferencesView(r *domain.Role, p domain.PreferencePatch, memberCount int, loc i18n.Locale) RolePreferencesView {
	fields := p.Fields()
	if fields == nil {
		fields = []string{}
	}
	return RolePreferencesView{Role: r.Name, RoleTitle: r.Title.In(loc), Data: p, Fields: fields, MemberCount: memberCount}
}

// OrgPreferences 列出各角色的偏好（需要「组织设置」权限）；没配的角色也列出来，data 为空。
func (a *App) OrgPreferences(ctx context.Context, sess *Session) ([]RolePreferencesView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	out := []RolePreferencesView{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		roles, err := a.Store.ListRoles(ctx, tx)
		if err != nil {
			return err
		}
		rows, err := a.Store.ListRolePreferences(ctx, tx)
		if err != nil {
			return err
		}
		byRole := map[string]domain.PreferencePatch{}
		for _, r := range rows {
			byRole[r.Subject] = r.Patch
		}
		counts, err := a.activeMembersByRole(ctx, tx)
		if err != nil {
			return err
		}
		for _, r := range roles {
			out = append(out, rolePreferencesView(r, byRole[r.Name], counts[r.Name], sess.Loc()))
		}
		return nil
	})
	return out, err
}

// SetRolePreferences 给角色配偏好（部分修改，语义同 SetMyPreferences）。需要「组织设置」权限。
func (a *App) SetRolePreferences(ctx context.Context, sess *Session, role string, raw []byte) (*RolePreferencesView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	in, cleared, err := domain.ParsePreferencePatch(raw)
	if err != nil {
		return nil, prefError(err)
	}
	if in.IsEmpty() && len(cleared) == 0 {
		return nil, Bad("err.pref_body_empty")
	}
	if err := domain.ValidatePreferencePatch(in); err != nil {
		return nil, prefError(err)
	}
	var out *RolePreferencesView
	err = a.tx(ctx, sess, func(tx pgx.Tx) error {
		r, err := a.roleByName(ctx, tx, role)
		if err != nil {
			return err
		}
		var base domain.PreferencePatch
		if row, err := a.Store.Preference(ctx, tx, "role", role); err == nil {
			base = row.Patch
		} else if err != store.ErrNotFound {
			return err
		}
		merged := domain.MergePreferencePatch(base, in, cleared)
		if merged.IsEmpty() {
			if err := a.Store.DeletePreference(ctx, tx, "role", role); err != nil {
				return err
			}
		} else if err := a.Store.PutPreference(ctx, tx, sess.OrgID, "role", role, merged); err != nil {
			return err
		}
		if err := a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "PreferencesUpdated", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"target": "role", "role": role, "fields": append(in.Fields(), cleared...)}}}); err != nil {
			return err
		}
		counts, err := a.activeMembersByRole(ctx, tx)
		if err != nil {
			return err
		}
		v := rolePreferencesView(r, merged, counts[role], sess.Loc())
		out = &v
		return nil
	})
	return out, err
}

// ClearRolePreferences 清除角色偏好，回到系统默认。需要「组织设置」权限。
func (a *App) ClearRolePreferences(ctx context.Context, sess *Session, role string) (*RolePreferencesView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	var out *RolePreferencesView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		r, err := a.roleByName(ctx, tx, role)
		if err != nil {
			return err
		}
		if err := a.Store.DeletePreference(ctx, tx, "role", role); err != nil {
			return err
		}
		if err := a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "PreferencesUpdated", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"target": "role", "role": role, "cleared": true}}}); err != nil {
			return err
		}
		counts, err := a.activeMembersByRole(ctx, tx)
		if err != nil {
			return err
		}
		v := rolePreferencesView(r, domain.PreferencePatch{}, counts[role], sess.Loc())
		out = &v
		return nil
	})
	return out, err
}
