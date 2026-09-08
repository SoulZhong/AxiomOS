package app

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/store"
)

// 目标类型（ADR 0023）：组织自己维护的词表，与能力标签、价格表同级的一页组织设置。
// 读：所有成员都能读（新建目标要选它，目标树要显示它）。
// 写：要 `org_settings` 权限，与团队、能力标签、价格表同一道门。
// 删：还有目标在用的类型不许删，只能停用——与团队、能力标签同一套规则。

// GoalTypeView 是一个目标类型加上「下面有多少个目标」。
type GoalTypeView struct {
	*domain.GoalType
	GoalCount int `json:"goal_count"`
}

// ListGoalTypes 列出全部目标类型（含已停用的：已有目标还要显示它们），带各自的目标数。
func (a *App) ListGoalTypes(ctx context.Context, sess *Session) ([]GoalTypeView, error) {
	out := []GoalTypeView{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		types, err := a.Store.ListGoalTypes(ctx, tx)
		if err != nil {
			return err
		}
		usage, err := a.Store.GoalTypeUsage(ctx, tx)
		if err != nil {
			return err
		}
		for _, t := range types {
			out = append(out, GoalTypeView{GoalType: t, GoalCount: usage[t.ID]})
		}
		return nil
	})
	return out, err
}

// GoalTypePatch 是目标类型的可改字段；nil 表示不改。
type GoalTypePatch struct {
	Name             *string               `json:"name"`
	Color            *string               `json:"color"`
	Icon             *string               `json:"icon"`
	Sort             *int                  `json:"sort"`
	Active           *bool                 `json:"active"`
	DefaultPrecision *domain.DatePrecision `json:"default_precision"`
}

// CreateGoalType 新建一个目标类型。排序权重不给就排到最后。
func (a *App) CreateGoalType(ctx context.Context, sess *Session, in GoalTypePatch) (*GoalTypeView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	name := ""
	if in.Name != nil {
		name = domain.NormalizeGoalTypeName(*in.Name)
	}
	precision := domain.DatePrecision("")
	if in.DefaultPrecision != nil {
		precision = *in.DefaultPrecision
	}
	if errs := domain.ValidateGoalType(name, precision); len(errs) > 0 {
		return nil, &UserError{Status: 400, Reasons: reasonsOf(errs)}
	}
	t := &domain.GoalType{OrgID: sess.OrgID, Name: name, Active: true, DefaultPrecision: precision}
	if in.Color != nil {
		t.Color = *in.Color
	}
	if in.Icon != nil {
		t.Icon = *in.Icon
	}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		existing, err := a.Store.ListGoalTypes(ctx, tx)
		if err != nil {
			return err
		}
		for _, x := range existing {
			if x.Name == name {
				return Bad("err.goal_type_duplicate", name)
			}
		}
		if in.Sort != nil {
			t.Sort = *in.Sort
		} else {
			for _, x := range existing {
				if x.Sort >= t.Sort {
					t.Sort = x.Sort + 1
				}
			}
		}
		if err := a.Store.CreateGoalType(ctx, tx, t); err != nil {
			return err
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "GoalTypeCreated", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"type_id": t.ID, "name": t.Name}}})
	})
	if err != nil {
		return nil, err
	}
	return &GoalTypeView{GoalType: t}, nil
}

// UpdateGoalType 改一个目标类型。改名只改显示，不动历史：动态里记的是当时的名字，
// 已有目标的类型引用是编号，改名之后旧动态照旧念旧名字（ADR 0023 第 6 条）。
func (a *App) UpdateGoalType(ctx context.Context, sess *Session, id string, in GoalTypePatch) (*GoalTypeView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	var view *GoalTypeView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		t, err := a.Store.GoalTypeByID(ctx, tx, id)
		if err == store.ErrNotFound {
			return NotFound("err.goal_type_missing")
		}
		if err != nil {
			return err
		}
		types, err := a.Store.ListGoalTypes(ctx, tx)
		if err != nil {
			return err
		}
		var events []domain.Event
		ev := func(kind string, data map[string]any) {
			data["type_id"] = t.ID
			events = append(events, domain.Event{Type: kind, ActorID: sess.Actor.ID, At: time.Now(), Data: data})
		}
		name, precision := t.Name, t.DefaultPrecision
		if in.Name != nil {
			name = domain.NormalizeGoalTypeName(*in.Name)
		}
		if in.DefaultPrecision != nil {
			precision = *in.DefaultPrecision
		}
		if errs := domain.ValidateGoalType(name, precision); len(errs) > 0 {
			return &UserError{Status: 400, Reasons: reasonsOf(errs)}
		}
		if name != t.Name {
			for _, x := range types {
				if x.ID != t.ID && x.Name == name {
					return Bad("err.goal_type_duplicate", name)
				}
			}
			ev("GoalTypeRenamed", map[string]any{"name": name, "from": t.Name})
			t.Name = name
		}
		plain := false // 颜色 / 图标 / 排序 / 默认时间粒度这类只改显示的改动，合记一条
		if precision != t.DefaultPrecision {
			t.DefaultPrecision = precision
			plain = true
		}
		if in.Color != nil && *in.Color != t.Color {
			t.Color = *in.Color
			plain = true
		}
		if in.Icon != nil && *in.Icon != t.Icon {
			t.Icon = *in.Icon
			plain = true
		}
		if in.Sort != nil && *in.Sort != t.Sort {
			t.Sort = *in.Sort
			plain = true
		}
		if in.Active != nil && *in.Active != t.Active {
			t.Active = *in.Active
			if t.Active {
				ev("GoalTypeReactivated", map[string]any{"name": t.Name})
			} else {
				ev("GoalTypeDeactivated", map[string]any{"name": t.Name})
			}
		}
		if plain {
			ev("GoalTypeUpdated", map[string]any{"name": t.Name})
		}
		if len(events) == 0 {
			view = &GoalTypeView{GoalType: t}
			return nil
		}
		if err := a.Store.UpdateGoalType(ctx, tx, t); err != nil {
			return err
		}
		usage, err := a.Store.GoalTypeUsage(ctx, tx)
		if err != nil {
			return err
		}
		view = &GoalTypeView{GoalType: t, GoalCount: usage[t.ID]}
		return a.insertEvents(ctx, tx, sess, events)
	})
	if err != nil {
		return nil, err
	}
	return view, nil
}

// DeleteGoalType 删除一个目标类型。还有目标在用的不许删（数据库上也是 on delete restrict）：
// 那些目标要么先改成别的类型，要么把这个类型停用——停用之后新建时选不到，已有目标照常显示。
func (a *App) DeleteGoalType(ctx context.Context, sess *Session, id string) error {
	if err := a.requireOrgSettings(sess); err != nil {
		return err
	}
	return a.tx(ctx, sess, func(tx pgx.Tx) error {
		t, err := a.Store.GoalTypeByID(ctx, tx, id)
		if err == store.ErrNotFound {
			return NotFound("err.goal_type_missing")
		}
		if err != nil {
			return err
		}
		usage, err := a.Store.GoalTypeUsage(ctx, tx)
		if err != nil {
			return err
		}
		if n := usage[id]; n > 0 {
			return Bad("err.goal_type_in_use", t.Name, n)
		}
		if err := a.Store.DeleteGoalType(ctx, tx, id); err != nil {
			return err
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "GoalTypeDeleted", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"type_id": t.ID, "name": t.Name}}})
	})
}

// goalTypesByID 是一次事务里查一遍类型表的小工具。
func (a *App) goalTypesByID(ctx context.Context, tx pgx.Tx) (map[string]*domain.GoalType, error) {
	types, err := a.Store.ListGoalTypes(ctx, tx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*domain.GoalType, len(types))
	for _, t := range types {
		out[t.ID] = t
	}
	return out, nil
}
