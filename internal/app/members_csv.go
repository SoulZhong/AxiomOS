package app

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
)

// 成员名单的 CSV 导入 / 导出（DESIGN.md §16）。列：姓名、邮箱、团队、角色、状态、来源。
// 团队写完整路径「产品事业部 / 研发组」，多个角色用「、」连接。导入以邮箱认人，先预览再确认。

var csvHeader = []string{"姓名", "邮箱", "团队", "角色", "状态", "来源"}

const utf8BOM = "\xEF\xBB\xBF"

// ExportMembersCSV 导出全部成员（含停用），UTF-8 带 BOM，Excel 直接能开。
func (a *App) ExportMembersCSV(ctx context.Context, sess *Session) ([]byte, error) {
	ms, err := a.OrgMembers(ctx, sess)
	if err != nil {
		return nil, err
	}
	loc := sess.Loc()
	var byID map[string]*domain.Team
	var roles []*domain.Role
	if err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		teams, err := a.Store.ListTeams(ctx, tx)
		if err != nil {
			return err
		}
		byID = teamsByID(teams)
		roles, err = a.Store.ListRoles(ctx, tx)
		return err
	}); err != nil {
		return nil, err
	}
	title := map[string]string{}
	for _, r := range roles {
		title[r.Name] = r.Title.In(loc)
	}
	var buf bytes.Buffer
	buf.WriteString(utf8BOM)
	w := csv.NewWriter(&buf)
	_ = w.Write(csvHeader)
	sep := i18n.Tr(loc, "sep.list")
	for _, m := range ms {
		names := make([]string, 0, len(m.Roles))
		for _, r := range m.Roles {
			if t, ok := title[r]; ok && t != "" {
				names = append(names, t)
			} else {
				names = append(names, r)
			}
		}
		status := m.DerivedStatus()
		_ = w.Write([]string{m.Name, m.Email, teamPath(byID, m.TeamID), strings.Join(names, sep), i18n.Tr(loc, "member.status."+status), SourceTitle(m.Source, loc)})
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// ImportRow 是导入预览里的一行。
type ImportRow struct {
	Line     int      `json:"line"`
	Name     string   `json:"name"`
	Email    string   `json:"email"`
	TeamPath string   `json:"team_path"`
	TeamID   *string  `json:"team_id"`
	Roles    []string `json:"roles"`
	// Action 是 create（新邮箱：建待激活成员并发邀请）、update（已有的手工成员：改姓名 / 角色 / 团队）、
	// confirm（新邮箱，但姓名与团队都对上了某个手工成员，要人决定：合并 / 新建 / 跳过，ADR 0017 补记四）、
	// skip（按决定跳过）、invalid（有问题，不会动）。
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
	// Candidates 只在 confirm 时有：可能是同一个人的手工成员。
	Candidates []CandidateView `json:"candidates,omitempty"`

	memberID string
	hasRoles bool // 文件里有角色列（没有则不改角色）
	hasTeam  bool // 文件里有团队列且这一行填了（没填则不改团队）
}

// ImportSummary 是各种动作的行数。
type ImportSummary struct {
	Create  int `json:"create"`
	Update  int `json:"update"`
	Confirm int `json:"confirm"`
	Skip    int `json:"skip"`
	Invalid int `json:"invalid"`
}

// ImportDecision 是对某一行候选的决定：merge（并入 local_id 指的成员）、create、skip。
type ImportDecision struct {
	Line     int    `json:"line"`
	Decision string `json:"decision"`
	LocalID  string `json:"local_id"`
}

func decisionsByLine(ds []ImportDecision) map[int]ImportDecision {
	out := map[int]ImportDecision{}
	for _, d := range ds {
		out[d.Line] = d
	}
	return out
}

// ImportPreview 是「导入会做什么」。
type ImportPreview struct {
	Rows    []ImportRow   `json:"rows"`
	Summary ImportSummary `json:"summary"`
}

// ImportSkip 是导入时没有动的一行。
type ImportSkip struct {
	Line   int    `json:"line"`
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

// InvitationLink 是导入新建成员后的邀请链接（只在这一次响应里出现）。
type InvitationLink struct {
	Email string `json:"email"`
	URL   string `json:"url"`
}

// ImportResult 是导入的结果。
type ImportResult struct {
	Created     int              `json:"created"`
	Updated     int              `json:"updated"`
	Unchanged   int              `json:"unchanged"`
	Skipped     []ImportSkip     `json:"skipped"`
	Invitations []InvitationLink `json:"invitations"`
}

type csvRecord struct {
	line                     int
	name, email, team, roles string
	hasRoles, hasTeam        bool
}

// parseMembersCSV 读表头与数据行。表头必须有「邮箱」（或 email）；姓名 / name、团队 / team、角色 / roles 可选，其余列忽略。
func parseMembersCSV(data []byte) ([]csvRecord, error) {
	data = bytes.TrimPrefix(data, []byte(utf8BOM))
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, Bad("err.csv_empty")
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true
	header, err := r.Read()
	if err != nil {
		return nil, Bad("err.csv_parse", 1, err.Error())
	}
	col := map[string]int{}
	for i, h := range header {
		switch strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, utf8BOM))) {
		case "姓名", "name", "名字":
			col["name"] = i
		case "邮箱", "email", "e-mail", "邮件":
			col["email"] = i
		case "团队", "team", "team_path", "部门":
			col["team"] = i
		case "角色", "roles", "role":
			col["roles"] = i
		}
	}
	if _, ok := col["email"]; !ok {
		return nil, Bad("err.csv_header")
	}
	get := func(rec []string, key string) (string, bool) {
		i, ok := col[key]
		if !ok || i >= len(rec) {
			return "", ok
		}
		return strings.TrimSpace(rec[i]), true
	}
	var out []csvRecord
	for line := 2; ; line++ {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				line = pe.Line
			}
			return nil, Bad("err.csv_parse", line, err.Error())
		}
		blank := true
		for _, c := range rec {
			if strings.TrimSpace(c) != "" {
				blank = false
				break
			}
		}
		if blank {
			continue
		}
		row := csvRecord{line: line}
		row.name, _ = get(rec, "name")
		row.email, _ = get(rec, "email")
		row.team, row.hasTeam = get(rec, "team")
		row.roles, row.hasRoles = get(rec, "roles")
		row.hasTeam = row.hasTeam && row.team != ""
		out = append(out, row)
	}
	if len(out) == 0 {
		return nil, Bad("err.csv_empty")
	}
	return out, nil
}

var listSeps = strings.NewReplacer("，", "、", ",", "、", "；", "、", ";", "、", "|", "、", "/", "、")

// splitList 把「产品、开发」「产品,开发」一类写法拆成列表。
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(listSeps.Replace(s), "、") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

var pathSeps = strings.NewReplacer("／", "/", ">", "/", "›", "/", "\\", "/")

// normalizePath 把「产品事业部/研发组」「产品事业部 > 研发组」归一成「产品事业部 / 研发组」。
func normalizePath(s string) string {
	var parts []string
	for _, p := range strings.Split(pathSeps.Replace(s), "/") {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " / ")
}

// resolveImport 在事务里把每一行对到成员、团队、角色上，判定动作与理由。
func (a *App) resolveImport(ctx context.Context, tx pgx.Tx, sess *Session, recs []csvRecord, decisions map[int]ImportDecision) (*ImportPreview, error) {
	loc := sess.Loc()
	ms, err := a.Store.ListMembers(ctx, tx)
	if err != nil {
		return nil, err
	}
	byEmail := map[string]*domain.Member{}
	for _, m := range ms {
		if acc, err := a.Store.AccountByID(ctx, tx, m.AccountID); err == nil {
			byEmail[strings.ToLower(acc.Email)] = m
		}
	}
	teams, err := a.Store.ListTeams(ctx, tx)
	if err != nil {
		return nil, err
	}
	byID := teamsByID(teams)
	parents := teamParents(teams)
	memberships, err := a.Store.TeamMemberships(ctx, tx)
	if err != nil {
		return nil, err
	}
	// 第四级认法（ADR 0017 补记四）：邮箱不同但姓名相同、且行里的团队与手工成员的主团队同名 → 候选，要人确认
	candidatesFor := func(name string, teamID *string) []CandidateView {
		if name == "" || teamID == nil {
			return nil
		}
		want := byID[*teamID]
		if want == nil {
			return nil
		}
		var out []CandidateView
		for _, m := range ms {
			if m.Source != domain.SourceManual || strings.TrimSpace(m.Name) != name {
				continue
			}
			primary := byID[primaryTeam(parents, memberships[m.ID])]
			if primary == nil || primary.Name != want.Name {
				continue
			}
			out = append(out, CandidateView{LocalID: m.ID, Name: m.Name, TeamPath: teamPath(byID, primary.ID), Source: m.Source, SourceTitle: SourceTitle(m.Source, loc),
				Reason: domain.ReasonNameTeam, ReasonText: reasonText(domain.MatchCandidate{Reason: domain.ReasonNameTeam, Via: primary.Name}, loc)})
		}
		return out
	}
	byPath := map[string]*domain.Team{}
	byName := map[string][]*domain.Team{}
	for _, t := range teams {
		byPath[teamPath(byID, t.ID)] = t
		byName[t.Name] = append(byName[t.Name], t)
	}
	roles, err := a.Store.ListRoles(ctx, tx)
	if err != nil {
		return nil, err
	}
	roleByText := map[string]string{}
	for _, r := range roles {
		roleByText[strings.ToLower(r.Name)] = r.Name
		for _, t := range r.Title {
			roleByText[strings.ToLower(t)] = r.Name
		}
	}
	pv := &ImportPreview{Rows: []ImportRow{}}
	seen := map[string]int{}
	for _, rec := range recs {
		row := ImportRow{Line: rec.line, Name: rec.name, Email: strings.ToLower(rec.email), Roles: []string{}, hasRoles: rec.hasRoles, hasTeam: rec.hasTeam}
		invalid := func(err error) {
			row.Action, row.Reason = "invalid", renderReason(err, loc)
		}
		if rec.hasTeam {
			row.TeamPath = normalizePath(rec.team)
		}
		switch {
		case !emailRe.MatchString(row.Email):
			invalid(Bad("csv.bad_email"))
		case seen[row.Email] > 0:
			invalid(Bad("csv.dup_email", seen[row.Email]))
		}
		seen[row.Email] = rec.line
		if row.Action == "" && rec.hasRoles {
			for _, r := range splitList(rec.roles) {
				name, ok := roleByText[strings.ToLower(r)]
				if !ok {
					invalid(Bad("csv.role_unknown", r))
					break
				}
				if !contains(row.Roles, name) {
					row.Roles = append(row.Roles, name)
				}
			}
		}
		if row.Action == "" && rec.hasTeam {
			t := byPath[row.TeamPath]
			if t == nil && !strings.Contains(row.TeamPath, " / ") {
				switch cands := byName[row.TeamPath]; len(cands) {
				case 0:
				case 1:
					t = cands[0]
				default:
					invalid(Bad("csv.team_ambiguous", row.TeamPath))
				}
			}
			switch {
			case row.Action != "":
			case t == nil:
				invalid(Bad("csv.team_unknown", row.TeamPath))
			case t.Inactive:
				invalid(Bad("csv.team_inactive", t.Name))
			default:
				id := t.ID
				row.TeamID = &id
				row.TeamPath = teamPath(byID, id)
			}
		}
		if row.Action == "" {
			if m := byEmail[row.Email]; m != nil {
				if m.Source != domain.SourceManual {
					invalid(Bad("csv.member_synced", SourceText(m.Source)))
				} else {
					row.Action, row.memberID = "update", m.ID
					if row.Name == "" {
						row.Name = m.Name
					}
				}
			} else if cands := candidatesFor(row.Name, row.TeamID); len(cands) > 0 {
				row.Candidates = cands
				switch d := decisions[row.Line]; d.Decision {
				case domain.DecisionMerge:
					ok := false
					for _, c := range cands {
						ok = ok || c.LocalID == d.LocalID
					}
					if !ok {
						invalid(Bad("csv.decision_target", row.Line))
					} else {
						row.Action, row.memberID = "update", d.LocalID
					}
				case domain.DecisionCreate:
					row.Action = "create"
				case domain.DecisionSkip:
					row.Action, row.Reason = "skip", i18n.Tr(loc, "csv.skipped_by_decision")
				default:
					row.Action = "confirm"
				}
			} else {
				row.Action = "create"
			}
		}
		switch row.Action {
		case "create":
			pv.Summary.Create++
		case "update":
			pv.Summary.Update++
		case "confirm":
			pv.Summary.Confirm++
		case "skip":
			pv.Summary.Skip++
		default:
			pv.Summary.Invalid++
		}
		pv.Rows = append(pv.Rows, row)
	}
	return pv, nil
}

// PreviewMembersImport 只解析与对照，不落库。decisions 是对候选行的决定（可空），带上后预览按决定显示。
func (a *App) PreviewMembersImport(ctx context.Context, sess *Session, data []byte, decisions []ImportDecision) (*ImportPreview, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	recs, err := parseMembersCSV(data)
	if err != nil {
		return nil, err
	}
	var pv *ImportPreview
	err = a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		pv, err = a.resolveImport(ctx, tx, sess, recs, decisionsByLine(decisions))
		return
	})
	return pv, err
}

// ImportMembers 执行导入：新邮箱建待激活成员（带角色与团队）并发邀请链接；已有的手工成员改姓名 / 角色 / 团队；
// 有问题的行与来自IM 集成的成员跳过并说明；有候选但没决定的行（confirm）也跳过并说明。每处改动各记一条动态。
func (a *App) ImportMembers(ctx context.Context, sess *Session, data []byte, decisions []ImportDecision) (*ImportResult, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	recs, err := parseMembersCSV(data)
	if err != nil {
		return nil, err
	}
	res := &ImportResult{Skipped: []ImportSkip{}, Invitations: []InvitationLink{}}
	loc := sess.Loc()
	err = a.tx(ctx, sess, func(tx pgx.Tx) error {
		pv, err := a.resolveImport(ctx, tx, sess, recs, decisionsByLine(decisions))
		if err != nil {
			return err
		}
		teams, err := a.Store.ListTeams(ctx, tx)
		if err != nil {
			return err
		}
		byID := teamsByID(teams)
		parents := teamParents(teams)
		for _, row := range pv.Rows {
			switch row.Action {
			case "invalid", "skip":
				res.Skipped = append(res.Skipped, ImportSkip{Line: row.Line, Email: row.Email, Reason: row.Reason})
				continue
			case "confirm":
				res.Skipped = append(res.Skipped, ImportSkip{Line: row.Line, Email: row.Email, Reason: i18n.Tr(loc, "csv.confirm_pending")})
				continue
			}
			var events []domain.Event
			ev := func(kind string, m *domain.Member, data map[string]any) {
				data["member_id"], data["name"] = m.ID, m.Name
				events = append(events, domain.Event{Type: kind, ActorID: sess.Actor.ID, At: time.Now(), Data: data})
			}
			if row.Action == "create" {
				// 与 POST /org/invitations 同一条路：新邮箱预建待激活的手工成员（带角色与团队）并发邀请；
				// 邮箱已有可登录账号（别的组织的人）时只发邀请、不预建成员，接受时用自己的密码验证
				inv, err := a.inviteTx(ctx, tx, sess, row.Email, row.Name, row.Roles, row.TeamID)
				if err != nil {
					return err
				}
				res.Created++
				res.Invitations = append(res.Invitations, InvitationLink{Email: row.Email, URL: inv.URL})
				if inv.Member != nil {
					ev("MemberImported", inv.Member, map[string]any{"email": row.Email})
					if err := a.insertEvents(ctx, tx, sess, events); err != nil {
						return err
					}
				}
				continue
			}
			// update
			m, err := a.Store.MemberByID(ctx, tx, row.memberID)
			if err != nil {
				return err
			}
			changed := false
			if row.Name != "" && row.Name != m.Name {
				old := m.Name
				m.Name = row.Name
				changed = true
				ev("MemberRenamed", m, map[string]any{"from": old})
			}
			if row.hasRoles && !sameStrings(m.Roles, row.Roles) {
				m.Roles = row.Roles
				changed = true
				ev("MemberRolesChanged", m, map[string]any{"roles": row.Roles})
			}
			if changed {
				if err := a.Store.UpdateMember(ctx, tx, m); err != nil {
					return err
				}
			}
			if row.TeamID != nil {
				current, err := a.Store.TeamsOfMember(ctx, tx, m.ID)
				if err != nil {
					return err
				}
				if primaryTeam(parents, current) != *row.TeamID {
					if err := a.Store.SetMemberTeam(ctx, tx, sess.OrgID, m.ID, *row.TeamID); err != nil {
						return err
					}
					changed = true
					ev("MemberTeamChanged", m, map[string]any{"team_id": *row.TeamID, "team_name": byID[*row.TeamID].Name, "mode": "move"})
				}
			}
			if !changed {
				res.Unchanged++
				continue
			}
			res.Updated++
			if err := a.insertEvents(ctx, tx, sess, events); err != nil {
				return err
			}
		}
		return nil
	})
	return res, err
}
