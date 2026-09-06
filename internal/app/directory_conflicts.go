package app

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/directory"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 冲突在预览里解决，对应关系随时可改（ADR 0017 补记四）。
// 这里负责：把计划里的候选整理成「需要你确认」，读写决定，对应关系面板（已绑定 / 可能重复 / 已跳过）与解绑。
// 认法本身在 domain.PlanDirectory / domain.FindDuplicates 里，这里只做展示与持久化。

// ExternalRef 是 IM 里的一个对象在预览里的样子。邮箱只给打码后的，手机号只给尾号。
type ExternalRef struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Dept        string `json:"dept,omitempty"`   // 成员：所在部门名（多个用「、」连）
	Parent      string `json:"parent,omitempty"` // 团队：上级部门名
	EmailMasked string `json:"email_masked,omitempty"`
	MobileTail  string `json:"mobile_tail,omitempty"`
}

// CandidateView 是一个可能是同一个对象的本地候选。
type CandidateView struct {
	LocalID     string `json:"local_id"`
	Name        string `json:"name"`
	TeamPath    string `json:"team_path,omitempty"`
	Source      string `json:"source"`
	SourceTitle string `json:"source_title"`
	Reason      string `json:"reason"`
	ReasonText  string `json:"reason_text"`
}

// ConfirmationView 是预览里「需要你确认」的一条；已决定的也用它，多了 decision 与 decided_local_id。
type ConfirmationView struct {
	Kind           string          `json:"kind"` // member | team
	External       ExternalRef     `json:"external"`
	Candidates     []CandidateView `json:"candidates"`
	Decision       string          `json:"decision,omitempty"`
	DecisionTitle  string          `json:"decision_title,omitempty"`
	DecidedLocalID string          `json:"decided_local_id,omitempty"`
	DecidedLocal   string          `json:"decided_local_name,omitempty"`
}

// SkippedView 是决定跳过的一条。
type SkippedView struct {
	Kind         string    `json:"kind"`
	ExternalID   string    `json:"external_id"`
	ExternalName string    `json:"external_name"`
	DecidedAt    time.Time `json:"decided_at"`
}

// reasonText 把认法翻成一句小字（「邮箱相同」「姓名相同且都在「研发组」下」「团队同名同层级」）。
func reasonText(c domain.MatchCandidate, loc i18n.Locale) string {
	switch c.Reason {
	case domain.ReasonNameTeam:
		return i18n.Trf(loc, "directory.reason.name_team", c.Via)
	default:
		return i18n.Tr(loc, "directory.reason."+c.Reason)
	}
}

// maskEmail 把 li@x.com 打成 l***@x.com。
func maskEmail(e string) string {
	e = strings.TrimSpace(e)
	if e == "" {
		return ""
	}
	at := strings.Index(e, "@")
	if at <= 0 {
		return "***"
	}
	first := string([]rune(e[:at])[0])
	return first + "***" + e[at:]
}

// mobileTail 只给手机号最后四位。
func mobileTail(m string) string {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, m)
	if len(digits) < 4 {
		return ""
	}
	return digits[len(digits)-4:]
}

// conflictViews 把计划里的候选整理成三组：待确认、已决定、已跳过。
func (a *App) conflictViews(plan domain.DirectoryPlan, st domain.DirectoryState, p *pulled, decisions []store.DirectoryDecision, loc i18n.Locale) (confirm, decided []ConfirmationView, skipped []SkippedView) {
	confirm, decided, skipped = []ConfirmationView{}, []ConfirmationView{}, []SkippedView{}
	teamByID := teamsByID(st.Teams)
	memberByID := map[string]*domain.Member{}
	for _, m := range st.Members {
		memberByID[m.ID] = m
	}
	deptName := map[string]string{}
	deptParent := map[string]string{}
	for _, d := range p.depts {
		deptName[d.ID], deptParent[d.ID] = d.Name, d.ParentID
	}
	userByID := map[string]domain.ExternalUser{}
	for _, u := range p.users {
		userByID[u.ID] = u
	}
	decisionOf := map[string]store.DirectoryDecision{}
	for _, d := range decisions {
		decisionOf[d.Kind+"\x00"+d.ExternalID] = d
	}
	sep := i18n.Tr(loc, "sep.list")
	candidates := func(cs []domain.MatchCandidate, kind string) []CandidateView {
		out := []CandidateView{}
		for _, c := range cs {
			v := CandidateView{LocalID: c.LocalID, Reason: c.Reason, ReasonText: reasonText(c, loc)}
			if kind == store.IdentityTeam {
				if t := teamByID[c.LocalID]; t != nil {
					v.Name, v.TeamPath, v.Source = t.Name, teamPath(teamByID, t.ID), t.Source
				}
			} else if m := memberByID[c.LocalID]; m != nil {
				v.Name, v.Source = m.Name, m.Source
				v.TeamPath = teamPath(teamByID, primaryTeam(teamParents(st.Teams), st.MemberTeams[m.ID]))
			}
			v.SourceTitle = SourceTitle(v.Source, loc)
			out = append(out, v)
		}
		return out
	}
	decide := func(v *ConfirmationView, kind, ext string) bool {
		d, ok := decisionOf[kind+"\x00"+ext]
		if !ok || d.Decision == domain.DecisionSkip {
			return false
		}
		v.Decision, v.DecisionTitle, v.DecidedLocalID = d.Decision, i18n.Tr(loc, "directory.decision."+d.Decision), d.LocalID
		if kind == store.IdentityTeam {
			if t := teamByID[d.LocalID]; t != nil {
				v.DecidedLocal = t.Name
			}
		} else if m := memberByID[d.LocalID]; m != nil {
			v.DecidedLocal = m.Name
		}
		return true
	}
	for _, tp := range plan.Teams {
		v := ConfirmationView{Kind: store.IdentityTeam, External: ExternalRef{ID: tp.ExternalID, Name: tp.Name, Parent: deptName[tp.ParentExternalID]}, Candidates: candidates(tp.Candidates, store.IdentityTeam)}
		switch {
		case tp.Action == domain.PlanConfirm:
			confirm = append(confirm, v)
		case decide(&v, store.IdentityTeam, tp.ExternalID):
			decided = append(decided, v)
		}
	}
	for _, mp := range plan.Members {
		names := []string{}
		for _, d := range mp.DeptIDs {
			if n := deptName[d]; n != "" {
				names = append(names, n)
			}
		}
		v := ConfirmationView{Kind: store.IdentityMember, External: ExternalRef{ID: mp.ExternalID, Name: mp.Name, Dept: strings.Join(names, sep), EmailMasked: maskEmail(mp.Email), MobileTail: mobileTail(mp.Mobile)}, Candidates: candidates(mp.Candidates, store.IdentityMember)}
		switch {
		case mp.Action == domain.PlanConfirm:
			confirm = append(confirm, v)
		case decide(&v, store.IdentityMember, mp.ExternalID):
			decided = append(decided, v)
		}
	}
	for _, ext := range plan.SkippedTeams {
		d := decisionOf[store.IdentityTeam+"\x00"+ext]
		name := deptName[ext]
		if name == "" {
			name = d.ExternalName
		}
		skipped = append(skipped, SkippedView{Kind: store.IdentityTeam, ExternalID: ext, ExternalName: name, DecidedAt: d.DecidedAt})
	}
	for _, ext := range plan.SkippedMembers {
		d := decisionOf[store.IdentityMember+"\x00"+ext]
		name := userByID[ext].Name
		if name == "" {
			name = d.ExternalName
		}
		skipped = append(skipped, SkippedView{Kind: store.IdentityMember, ExternalID: ext, ExternalName: name, DecidedAt: d.DecidedAt})
	}
	return confirm, decided, skipped
}

// ---------- 决定 ----------

// DecisionInput 是 PUT /org/directory/decisions 里的一条。
type DecisionInput struct {
	Kind         string `json:"kind"`
	ExternalID   string `json:"external_id"`
	ExternalName string `json:"external_name"`
	Decision     string `json:"decision"`
	LocalID      string `json:"local_id"`
}

// DecisionsInput 是 PUT /org/directory/decisions 的输入。
type DecisionsInput struct {
	Items []DecisionInput `json:"items"`
}

// DecisionView 是存下来的一条决定。
type DecisionView struct {
	Kind          string    `json:"kind"`
	ExternalID    string    `json:"external_id"`
	ExternalName  string    `json:"external_name"`
	Decision      string    `json:"decision"`
	DecisionTitle string    `json:"decision_title"`
	LocalID       string    `json:"local_id,omitempty"`
	LocalName     string    `json:"local_name,omitempty"`
	DecidedAt     time.Time `json:"decided_at"`
}

var decisionValues = []string{domain.DecisionMerge, domain.DecisionCreate, domain.DecisionSkip}

// currentProvider 读当前提供方（没配置时报「还没有配置IM 集成」）。
func (a *App) currentProvider(ctx context.Context, tx pgx.Tx, orgID string) (directory.Provider, *store.DirectoryConfig, error) {
	cfg, err := a.loadDirectoryConfig(ctx, tx, orgID)
	if err == store.ErrNotFound {
		return directory.Provider{}, nil, Bad("err.directory_not_configured")
	}
	if err != nil {
		return directory.Provider{}, nil, err
	}
	prov, ok := directory.Lookup(cfg.Provider)
	if !ok {
		return directory.Provider{}, nil, Bad("err.directory_provider", cfg.Provider)
	}
	return prov, cfg, nil
}

// PutDirectoryDecisions 批量写决定：合并到已有（要指明本地对象）、作为新建、跳过。每条一条动态 DirectoryDecided。
func (a *App) PutDirectoryDecisions(ctx context.Context, sess *Session, in DecisionsInput) ([]DecisionView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	if len(in.Items) == 0 {
		return nil, Bad("err.directory_decisions_empty")
	}
	out := []DecisionView{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		prov, _, err := a.currentProvider(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		teams, err := a.Store.ListTeams(ctx, tx)
		if err != nil {
			return err
		}
		teamByID := teamsByID(teams)
		members, err := a.Store.ListMembers(ctx, tx)
		if err != nil {
			return err
		}
		memberByID := map[string]*domain.Member{}
		for _, m := range members {
			memberByID[m.ID] = m
		}
		loc := sess.Loc()
		var events []domain.Event
		for _, it := range in.Items {
			kind, ext, dec := strings.TrimSpace(it.Kind), strings.TrimSpace(it.ExternalID), strings.TrimSpace(it.Decision)
			if kind != store.IdentityMember && kind != store.IdentityTeam {
				return Bad("err.directory_kind", it.Kind)
			}
			if ext == "" {
				return Bad("err.directory_external_id_required")
			}
			if !contains(decisionValues, dec) {
				return Bad("err.directory_decision", it.Decision)
			}
			local, localName := "", ""
			if dec == domain.DecisionMerge {
				local = strings.TrimSpace(it.LocalID)
				if local == "" {
					return Bad("err.directory_merge_target")
				}
				if kind == store.IdentityTeam {
					t := teamByID[local]
					if t == nil {
						return Bad("err.team_missing")
					}
					if t.Inactive {
						return Bad("err.team_merge_inactive_target", t.Name)
					}
					localName = t.Name
				} else {
					m := memberByID[local]
					if m == nil {
						return Bad("err.member_missing")
					}
					localName = m.Name
				}
			}
			d := &store.DirectoryDecision{OrgID: sess.OrgID, Provider: prov.Key, Kind: kind, ExternalID: ext, ExternalName: strings.TrimSpace(it.ExternalName), Decision: dec, LocalID: local, DecidedBy: sess.MemberID}
			if err := a.Store.UpsertDirectoryDecision(ctx, tx, d); err != nil {
				return err
			}
			out = append(out, DecisionView{Kind: kind, ExternalID: ext, ExternalName: d.ExternalName, Decision: dec, DecisionTitle: i18n.Tr(loc, "directory.decision."+dec), LocalID: local, LocalName: localName, DecidedAt: d.DecidedAt})
			events = append(events, domain.Event{Type: "DirectoryDecided", ActorID: sess.Actor.ID, At: time.Now(),
				Data: map[string]any{"kind": kind, "external_id": ext, "external_name": d.ExternalName, "decision": dec, "local_id": local, "local_name": localName, "provider": prov.Key}})
		}
		return a.insertEvents(ctx, tx, sess, events)
	})
	return out, err
}

// DeleteDirectoryDecision 撤回一条决定（重新考虑）：下次预览会再问。
func (a *App) DeleteDirectoryDecision(ctx context.Context, sess *Session, kind, externalID string) error {
	if err := a.requireOrgSettings(sess); err != nil {
		return err
	}
	if kind != store.IdentityMember && kind != store.IdentityTeam {
		return Bad("err.directory_kind", kind)
	}
	return a.tx(ctx, sess, func(tx pgx.Tx) error {
		prov, _, err := a.currentProvider(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		name := ""
		if ds, err := a.Store.ListDirectoryDecisions(ctx, tx, prov.Key); err == nil {
			for _, d := range ds {
				if d.Kind == kind && d.ExternalID == externalID {
					name = d.ExternalName
				}
			}
		}
		ok, err := a.Store.DeleteDirectoryDecision(ctx, tx, prov.Key, kind, externalID)
		if err != nil {
			return err
		}
		if !ok {
			return NotFound("err.directory_decision_missing")
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "DirectoryDecided", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"kind": kind, "external_id": externalID, "external_name": name, "decision": "reconsider", "provider": prov.Key}}})
	})
}

// ---------- 对应关系面板 ----------

// BoundView 是一条已绑定的对应关系：IM 对象 ↔ 本地对象。
type BoundView struct {
	Kind         string    `json:"kind"`
	ExternalID   string    `json:"external_id"`
	ExternalName string    `json:"external_name,omitempty"`
	LocalID      string    `json:"local_id"`
	LocalName    string    `json:"local_name"`
	LocalActive  bool      `json:"local_active"`
	Since        time.Time `json:"since"`
}

// DuplicateSide 是疑似重复里的一边。
type DuplicateSide struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Source      string `json:"source"`
	SourceTitle string `json:"source_title"`
	TeamPath    string `json:"team_path,omitempty"`
}

// DuplicateView 是一对疑似重复：A 是手工对象，B 是同步来的对象。
type DuplicateView struct {
	Kind       string        `json:"kind"`
	A          DuplicateSide `json:"a"`
	B          DuplicateSide `json:"b"`
	Reason     string        `json:"reason"`
	ReasonText string        `json:"reason_text"`
}

// MappingsView 是 GET /org/directory/mappings 的返回。
type MappingsView struct {
	Provider      string          `json:"provider,omitempty"`
	ProviderTitle string          `json:"provider_title,omitempty"`
	Bound         []BoundView     `json:"bound"`
	Duplicates    []DuplicateView `json:"duplicates"`
	Skipped       []SkippedView   `json:"skipped"`
}

// duplicateState 读疑似重复检测要用的现状（不需要提供方）。
func (a *App) duplicateState(ctx context.Context, tx pgx.Tx) (domain.DirectoryState, error) {
	st := domain.DirectoryState{Emails: map[string]string{}, Mobiles: map[string]string{}}
	var err error
	if st.Teams, err = a.Store.ListTeams(ctx, tx); err != nil {
		return st, err
	}
	if st.Members, err = a.Store.ListMembers(ctx, tx); err != nil {
		return st, err
	}
	for _, m := range st.Members {
		if acc, err := a.Store.AccountByID(ctx, tx, m.AccountID); err == nil {
			st.Emails[m.ID] = strings.ToLower(acc.Email)
		}
	}
	st.MemberTeams, err = a.Store.TeamMemberships(ctx, tx)
	return st, err
}

// duplicateViews 把 domain.FindDuplicates 的结果整理成展示。
func duplicateViews(st domain.DirectoryState, loc i18n.Locale) []DuplicateView {
	teamByID := teamsByID(st.Teams)
	parents := teamParents(st.Teams)
	memberByID := map[string]*domain.Member{}
	for _, m := range st.Members {
		memberByID[m.ID] = m
	}
	side := func(kind, id string) DuplicateSide {
		if kind == store.IdentityTeam {
			t := teamByID[id]
			if t == nil {
				return DuplicateSide{ID: id}
			}
			return DuplicateSide{ID: id, Name: t.Name, Source: t.Source, SourceTitle: SourceTitle(t.Source, loc), TeamPath: teamPath(teamByID, id)}
		}
		m := memberByID[id]
		if m == nil {
			return DuplicateSide{ID: id}
		}
		return DuplicateSide{ID: id, Name: m.Name, Source: m.Source, SourceTitle: SourceTitle(m.Source, loc), TeamPath: teamPath(teamByID, primaryTeam(parents, st.MemberTeams[id]))}
	}
	out := []DuplicateView{}
	for _, d := range domain.FindDuplicates(st) {
		out = append(out, DuplicateView{Kind: d.Kind, A: side(d.Kind, d.A), B: side(d.Kind, d.B), Reason: d.Reason, ReasonText: reasonText(domain.MatchCandidate{Reason: d.Reason, Via: d.Via}, loc)})
	}
	return out
}

// DirectoryMappings 是对应关系面板的数据：已绑定、可能重复、已跳过。没配置提供方时只有可能重复。
func (a *App) DirectoryMappings(ctx context.Context, sess *Session) (*MappingsView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	loc := sess.Loc()
	out := &MappingsView{Bound: []BoundView{}, Duplicates: []DuplicateView{}, Skipped: []SkippedView{}}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		st, err := a.duplicateState(ctx, tx)
		if err != nil {
			return err
		}
		out.Duplicates = duplicateViews(st, loc)
		cfg, err := a.loadDirectoryConfig(ctx, tx, sess.OrgID)
		if err == store.ErrNotFound {
			return nil
		}
		if err != nil {
			return err
		}
		out.Provider = cfg.Provider
		if p, ok := directory.Lookup(cfg.Provider); ok {
			out.ProviderTitle = p.Title.In(loc)
		}
		teamByID := teamsByID(st.Teams)
		memberByID := map[string]*domain.Member{}
		for _, m := range st.Members {
			memberByID[m.ID] = m
		}
		ids, err := a.Store.ListExternalIdentityRows(ctx, tx, cfg.Provider)
		if err != nil {
			return err
		}
		for _, x := range ids {
			b := BoundView{Kind: x.Kind, ExternalID: x.ExternalID, LocalID: x.LocalID, Since: x.CreatedAt}
			if x.Kind == store.IdentityTeam {
				if t := teamByID[x.LocalID]; t != nil {
					b.LocalName, b.ExternalName, b.LocalActive = t.Name, t.ExternalName, !t.Inactive
				}
			} else if m := memberByID[x.LocalID]; m != nil {
				b.LocalName, b.LocalActive = m.Name, m.Active
			}
			out.Bound = append(out.Bound, b)
		}
		decisions, err := a.Store.ListDirectoryDecisions(ctx, tx, cfg.Provider)
		if err != nil {
			return err
		}
		for _, d := range decisions {
			if d.Decision == domain.DecisionSkip {
				out.Skipped = append(out.Skipped, SkippedView{Kind: d.Kind, ExternalID: d.ExternalID, ExternalName: d.ExternalName, DecidedAt: d.DecidedAt})
			}
		}
		return nil
	})
	return out, err
}

// UnbindDirectoryIdentity 解绑一条对应关系：删外部身份与它的决定，本地对象改为手工维护。动态 DirectoryUnbound。
func (a *App) UnbindDirectoryIdentity(ctx context.Context, sess *Session, kind, externalID string) error {
	if err := a.requireOrgSettings(sess); err != nil {
		return err
	}
	if kind != store.IdentityMember && kind != store.IdentityTeam {
		return Bad("err.directory_kind", kind)
	}
	return a.tx(ctx, sess, func(tx pgx.Tx) error {
		prov, _, err := a.currentProvider(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		ids, err := a.Store.ListExternalIdentities(ctx, tx, prov.Key)
		if err != nil {
			return err
		}
		var found *store.ExternalIdentity
		for i := range ids {
			if ids[i].Kind == kind && ids[i].ExternalID == externalID {
				found = &ids[i]
			}
		}
		if found == nil {
			return NotFound("err.binding_missing")
		}
		if _, err := a.Store.DeleteExternalIdentity(ctx, tx, prov.Key, kind, externalID); err != nil {
			return err
		}
		if _, err := a.Store.DeleteDirectoryDecision(ctx, tx, prov.Key, kind, externalID); err != nil {
			return err
		}
		name := ""
		if kind == store.IdentityTeam {
			t, err := teamByID(ctx, tx, a, found.LocalID)
			if err == nil {
				name = t.Name
				t.Source, t.ExternalName = domain.SourceManual, ""
				if err := a.Store.UpdateTeam(ctx, tx, t); err != nil {
					return err
				}
			}
		} else if m, err := a.Store.MemberByID(ctx, tx, found.LocalID); err == nil {
			name = m.Name
			m.Source = domain.SourceManual
			if err := a.Store.UpdateMember(ctx, tx, m); err != nil {
				return err
			}
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "DirectoryUnbound", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"kind": kind, "id": found.LocalID, "name": name, "external_id": externalID, "provider": prov.Key}}})
	})
}
