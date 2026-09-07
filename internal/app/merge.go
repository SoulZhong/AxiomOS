package app

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 合并（ADR 0017 补记四）。团队合并：旧团队的成员、目标、任务（经目标）、迭代、共享边界、下级团队整体并入目标团队，
// 旧团队停用不删除。成员合并：保留目标成员的角色、Agent 等一切，把旧成员身上"现在归谁"的引用（任务负责人 / 验收人 /
// 参与角色、目标负责人、Agent 所有者、团队归属）改到目标成员，外部身份搬过去，旧成员停用；历史（创建者、评论、动态）不改。
// 合并后若目标接上了 IM 身份，姓名 / 名称按 IM（ADR 0017 补记四「合并后的字段归属」）。

// mergeTeamTx 把 loser 并入 target（事务内，被同步与 POST /org/teams/{id}/merge 共用）。
func (a *App) mergeTeamTx(ctx context.Context, tx pgx.Tx, sess *Session, teams []*domain.Team, loser, target *domain.Team) error {
	if loser.ID == target.ID {
		return Bad("err.merge_same")
	}
	if target.Inactive {
		return Bad("err.team_merge_inactive_target", target.Name)
	}
	parents := teamParents(teams)
	// 目标在旧团队的子树里：先把目标提到旧团队的位置，再把旧团队的其他下级挂到目标下面，才不会成环
	if inSubtree(parents, target.ID, loser.ID) {
		target.ParentID = loser.ParentID
	}
	if err := a.Store.MergeTeamRefs(ctx, tx, sess.OrgID, loser.ID, target.ID); err != nil {
		return err
	}
	if loser.IsBoundary {
		target.IsBoundary = true
	}
	if loser.LeadMemberID != "" && target.LeadMemberID == "" {
		target.LeadMemberID = loser.LeadMemberID
	}
	// 旧团队来自 IM、目标是手工建的：目标接上 IM 身份，名称按 IM
	if loser.Source != domain.SourceManual && target.Source == domain.SourceManual {
		target.Source = loser.Source
		if loser.ExternalName != "" {
			target.ExternalName, target.Name = loser.ExternalName, loser.ExternalName
		}
	}
	if err := a.Store.RepointIdentities(ctx, tx, store.IdentityTeam, loser.ID, target.ID); err != nil {
		return err
	}
	if err := a.Store.UpdateTeam(ctx, tx, target); err != nil {
		return err
	}
	oldName := loser.Name
	loser.Inactive, loser.Source, loser.ExternalName = true, domain.SourceManual, ""
	if err := a.Store.UpdateTeam(ctx, tx, loser); err != nil {
		return err
	}
	return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "TeamMerged", ActorID: sess.Actor.ID, At: time.Now(),
		Data: map[string]any{"team_id": loser.ID, "name": oldName, "into_id": target.ID, "into_name": target.Name}}})
}

// mergeMemberTx 把 loser 并入 target（事务内，被同步与 POST /org/members/{id}/merge 共用）。
func (a *App) mergeMemberTx(ctx context.Context, tx pgx.Tx, sess *Session, ownerID string, loser, target *domain.Member) error {
	if loser.ID == target.ID {
		return Bad("err.merge_same")
	}
	if loser.ID == ownerID {
		return Bad("err.member_merge_owner")
	}
	if !target.Active {
		return Bad("err.member_merge_inactive_target", target.Name)
	}
	if err := a.Store.MergeMemberRefs(ctx, tx, sess.OrgID, loser.ID, target.ID); err != nil {
		return err
	}
	if err := a.Store.RepointIdentities(ctx, tx, store.IdentityMember, loser.ID, target.ID); err != nil {
		return err
	}
	// 旧成员来自 IM、目标是手工建的：目标接上 IM 身份，姓名按 IM；角色等一切保留目标的
	if loser.Source != domain.SourceManual && target.Source == domain.SourceManual {
		target.Source, target.Name = loser.Source, loser.Name
	}
	if err := a.Store.UpdateMember(ctx, tx, target); err != nil {
		return err
	}
	loser.Active, loser.Source = false, domain.SourceManual
	if err := a.Store.UpdateMember(ctx, tx, loser); err != nil {
		return err
	}
	return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "MemberMerged", ActorID: sess.Actor.ID, At: time.Now(),
		Data: map[string]any{"member_id": loser.ID, "name": loser.Name, "into_id": target.ID, "into_name": target.Name}}})
}

// ---------- 哪一边不能被并走 ----------
//
// 界面上要先知道哪个方向根本不成立，才不会让人选了再挨一个红字。下面两个函数是上面那些拦截里
// 「只看被并走的那一个」的部分，一条对一条；服务端的拦截照旧，这里只是同一套规矩的另一种说法。
// 只看被并走的那一个的规矩：成员是 mergeMemberTx 里的 err.member_merge_owner（组织负责人）；
// 团队是 MergeTeams 里的 err.team_merge_inactive_source（已停用的团队没有可以并入的内容）。
// 其余拦截（并入自己、目标已停用）看的是另一边或两边，不属于这里。

// memberKeepReason 说这个成员为什么不能被并进别人，能被并走时返回空串。
func memberKeepReason(m *domain.Member, ownerMemberID string, loc i18n.Locale) string {
	if m == nil {
		return ""
	}
	if m.ID == ownerMemberID {
		return i18n.Tr(loc, "merge.keep.owner")
	}
	return ""
}

// teamKeepReason 说这个团队为什么不能被并进别的团队，能被并走时返回空串。
func teamKeepReason(t *domain.Team, loc i18n.Locale) string {
	if t == nil {
		return ""
	}
	if t.Inactive {
		return i18n.Tr(loc, "merge.keep.team_inactive")
	}
	return ""
}

// MergeTeams 把团队 id 并入 into（POST /org/teams/{id}/merge）。返回合并后的目标团队。
func (a *App) MergeTeams(ctx context.Context, sess *Session, id, into string) (*TeamView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		teams, err := a.Store.ListTeams(ctx, tx)
		if err != nil {
			return err
		}
		byID := teamsByID(teams)
		loser, target := byID[id], byID[into]
		if loser == nil || target == nil {
			return Bad("err.team_missing")
		}
		if loser.Inactive {
			return Bad("err.team_merge_inactive_source", loser.Name)
		}
		return a.mergeTeamTx(ctx, tx, sess, teams, loser, target)
	})
	if err != nil {
		return nil, err
	}
	teams, err := a.ListTeams(ctx, sess)
	if err != nil {
		return nil, err
	}
	for i := range teams {
		if teams[i].ID == into {
			return &teams[i], nil
		}
	}
	return nil, NotFound("err.team_missing")
}

// MergeMembers 把成员 id 并入 into（POST /org/members/{id}/merge）。返回合并后的目标成员。
func (a *App) MergeMembers(ctx context.Context, sess *Session, id, into string) (*MemberDetail, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		loser, err := a.Store.MemberByID(ctx, tx, id)
		if err != nil {
			return Bad("err.member_missing")
		}
		target, err := a.Store.MemberByID(ctx, tx, into)
		if err != nil {
			return Bad("err.member_missing")
		}
		org, err := a.Store.OrganizationByID(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		return a.mergeMemberTx(ctx, tx, sess, org.OwnerMemberID, loser, target)
	})
	if err != nil {
		return nil, err
	}
	ms, err := a.OrgMembers(ctx, sess)
	if err != nil {
		return nil, err
	}
	for i := range ms {
		if ms[i].ID == into {
			return &ms[i], nil
		}
	}
	return nil, NotFound("err.member_missing")
}
