package app

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
)

// 成员的批量操作（DESIGN.md §16「成员与团队」）。每个成员单独判定、单独记动态；
// 不满足规则的成员跳过并给一句完整的中文理由，其余照做。

// BulkMembersInput 是一次批量操作。
type BulkMembersInput struct {
	MemberIDs []string `json:"member_ids"`
	// Action 是 move_team（换团队：替换全部所属团队）、add_team（加入团队：追加一个）、set_roles、deactivate、reactivate。
	Action string   `json:"action"`
	TeamID string   `json:"team_id"`
	Roles  []string `json:"roles"`
}

// BulkSkip 是被跳过的成员与理由。
type BulkSkip struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// BulkResult 是批量操作的结果。
type BulkResult struct {
	Updated int        `json:"updated"`
	Skipped []BulkSkip `json:"skipped"`
}

var bulkActions = []string{"move_team", "add_team", "set_roles", "deactivate", "reactivate"}

// BulkMembers 批量变更成员：换团队、加入团队、改角色、停用、恢复。
func (a *App) BulkMembers(ctx context.Context, sess *Session, in BulkMembersInput) (*BulkResult, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	if !contains(bulkActions, in.Action) {
		return nil, Bad("err.bulk_action", in.Action)
	}
	if len(in.MemberIDs) == 0 {
		return nil, Bad("err.bulk_empty")
	}
	loc := sess.Loc()
	out := &BulkResult{Skipped: []BulkSkip{}}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		org, err := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		teams, err := a.Store.ListTeams(ctx, tx)
		if err != nil {
			return err
		}
		byID := teamsByID(teams)
		var target *domain.Team
		switch in.Action {
		case "move_team", "add_team":
			if in.TeamID == "" {
				return Bad("err.team_required")
			}
			target = byID[in.TeamID]
			if target == nil {
				return Bad("err.team_missing")
			}
			if target.Inactive {
				return Bad("err.team_inactive", target.Name)
			}
		case "set_roles":
			if in.Roles == nil {
				in.Roles = []string{}
			}
			if err := a.checkRoles(ctx, tx, in.Roles); err != nil {
				return err
			}
		}
		memberships, err := a.Store.TeamMemberships(ctx, tx)
		if err != nil {
			return err
		}
		skip := func(m *domain.Member, id string, err error) {
			name := ""
			if m != nil {
				name = m.Name
			}
			out.Skipped = append(out.Skipped, BulkSkip{ID: id, Name: name, Reason: renderReason(err, loc)})
		}
		seen := map[string]bool{}
		for _, id := range in.MemberIDs {
			if seen[id] {
				continue
			}
			seen[id] = true
			m, err := a.Store.MemberByID(ctx, tx, id)
			if err != nil {
				skip(nil, id, NotFound("err.member_missing"))
				continue
			}
			var events []domain.Event
			ev := func(kind string, data map[string]any) {
				data["member_id"], data["name"] = m.ID, m.Name
				events = append(events, domain.Event{Type: kind, ActorID: sess.Actor.ID, At: time.Now(), Data: data})
			}
			switch in.Action {
			case "move_team":
				current := memberships[m.ID]
				if err := checkMemberTeamChange(m, byID, current, target.ID, false); err != nil {
					skip(m, id, err)
					continue
				}
				if err := a.Store.SetMemberTeam(ctx, tx, sess.OrgID, m.ID, target.ID); err != nil {
					return err
				}
				ev("MemberTeamChanged", map[string]any{"team_id": target.ID, "team_name": target.Name, "mode": "move"})
			case "add_team":
				if contains(memberships[m.ID], target.ID) {
					skip(m, id, Bad("err.member_in_team", target.Name))
					continue
				}
				if err := checkMemberTeamChange(m, byID, memberships[m.ID], target.ID, true); err != nil {
					skip(m, id, err)
					continue
				}
				if err := a.Store.AddTeamMember(ctx, tx, sess.OrgID, target.ID, m.ID); err != nil {
					return err
				}
				ev("MemberTeamChanged", map[string]any{"team_id": target.ID, "team_name": target.Name, "mode": "add"})
			case "set_roles":
				m.Roles = in.Roles
				if err := a.Store.UpdateMember(ctx, tx, m); err != nil {
					return err
				}
				ev("MemberRolesChanged", map[string]any{"roles": in.Roles})
			case "deactivate":
				if !m.Active {
					skip(m, id, Bad("err.member_already_inactive"))
					continue
				}
				if err := checkDeactivate(sess, org, m); err != nil {
					skip(m, id, err)
					continue
				}
				m.Active = false
				if err := a.Store.UpdateMember(ctx, tx, m); err != nil {
					return err
				}
				if err := a.deactivateMemberEffects(ctx, tx, sess, m); err != nil {
					return err
				}
				ev("MemberDeactivated", map[string]any{})
			case "reactivate":
				if m.Active {
					skip(m, id, Bad("err.member_not_inactive"))
					continue
				}
				m.Active = true
				if err := a.Store.UpdateMember(ctx, tx, m); err != nil {
					return err
				}
				ev("MemberReactivated", map[string]any{})
			}
			out.Updated++
			if err := a.insertEvents(ctx, tx, sess, events); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

// renderReason 把用户错误渲染成读者语言的一句话（非用户错误照原样）。
func renderReason(err error, loc i18n.Locale) string {
	if ue, ok := err.(*UserError); ok {
		return ue.Render(loc)
	}
	return err.Error()
}
