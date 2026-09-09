package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/teemo/axiomos/internal/app"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
)

const adminCookie = "axiomos_admin"

// ---------- 组织设置 ----------

func (s *Server) orgRoutes(mux *http.ServeMux, auth func(string, http.HandlerFunc), pub func(string, http.HandlerFunc)) {
	auth("GET /api/v1/org", s.orgGet)
	auth("PATCH /api/v1/org", s.orgPatch)
	auth("GET /api/v1/org/members", s.orgMembers)
	auth("PATCH /api/v1/org/members/{id}", s.orgMemberPatch)
	auth("POST /api/v1/org/members/bulk", s.orgMembersBulk)
	auth("GET /api/v1/org/members/export.csv", s.orgMembersExport)
	auth("POST /api/v1/org/members/import/preview", s.orgMembersImportPreview)
	auth("POST /api/v1/org/members/import", s.orgMembersImport)
	auth("POST /api/v1/org/members/{id}/make-owner", s.orgMakeOwner)
	auth("POST /api/v1/org/members/{id}/merge", s.orgMemberMerge)
	auth("GET /api/v1/org/invitations", s.orgInvitations)
	auth("POST /api/v1/org/invitations", s.orgInvite)
	auth("DELETE /api/v1/org/invitations/{id}", s.orgInvitationDelete)
	auth("GET /api/v1/org/roles", s.orgRoles)
	auth("PUT /api/v1/org/roles/{name}", s.orgRolePut)
	auth("DELETE /api/v1/org/roles/{name}", s.orgRoleDelete)
	auth("GET /api/v1/org/settings", s.orgSettings)
	auth("PATCH /api/v1/org/settings", s.orgSettingsPatch)
	auth("GET /api/v1/org/visibility-preview", s.orgVisibilityPreview)
	auth("GET /api/v1/org/teams", s.orgTeams)
	auth("POST /api/v1/org/teams", s.orgTeamCreate)
	auth("PATCH /api/v1/org/teams/{id}", s.orgTeamPatch)
	auth("DELETE /api/v1/org/teams/{id}", s.orgTeamDelete)
	auth("POST /api/v1/org/teams/{id}/merge", s.orgTeamMerge)
	auth("GET /api/v1/org/capabilities", s.orgCapabilities)
	auth("PUT /api/v1/org/capabilities/{name}", s.orgCapabilityPut)
	auth("DELETE /api/v1/org/capabilities/{name}", s.orgCapabilityDelete)
	auth("GET /api/v1/org/goal-types", s.orgGoalTypes)
	auth("POST /api/v1/org/goal-types", s.orgGoalTypeCreate)
	auth("PATCH /api/v1/org/goal-types/{id}", s.orgGoalTypePatch)
	auth("DELETE /api/v1/org/goal-types/{id}", s.orgGoalTypeDelete)
	auth("GET /api/v1/org/pricing", s.orgPricing)
	auth("PUT /api/v1/org/pricing/models/{model}", s.orgPricePut)
	auth("DELETE /api/v1/org/pricing/models/{model}", s.orgPriceDelete)
	auth("PUT /api/v1/org/pricing/rates", s.orgRatePut)
	s.directoryRoutes(auth)

	pub("GET /api/v1/invitations/{token}", s.invitationLookup)
	pub("POST /api/v1/invitations/{token}/accept", s.invitationAccept)
}

type orgV struct {
	ID            string      `json:"id"`
	Slug          string      `json:"slug"`
	Name          string      `json:"name"`
	Currency      string      `json:"currency"`
	DefaultLocale string      `json:"default_locale"`
	Owner         ExecutorRef `json:"owner"`
}

func (s *Server) orgView(r *http.Request, org *domain.Organization) orgV {
	rf, _ := s.refsFor(r)
	return orgV{ID: org.ID, Slug: org.Slug, Name: org.Name, Currency: org.Currency, DefaultLocale: org.DefaultLocale, Owner: rf.must(org.OwnerMemberID)}
}

func (s *Server) orgGet(w http.ResponseWriter, r *http.Request) {
	org, err := s.App.GetOrganization(r.Context(), sessionOf(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, s.orgView(r, org))
}

func (s *Server) orgPatch(w http.ResponseWriter, r *http.Request) {
	var in app.OrgPatch
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	org, err := s.App.UpdateOrganization(r.Context(), sessionOf(r), in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, s.orgView(r, org))
}

type orgMemberV struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Roles     []string  `json:"roles"`
	TeamID    *string   `json:"team_id"`
	TeamIDs   []string  `json:"team_ids"`
	Active    bool      `json:"active"`
	IsOwner   bool      `json:"is_owner"`
	Locale    string    `json:"locale"`
	CreatedAt time.Time `json:"created_at"`
	// 来源与激活状态（ADR 0017）；待激活成员带上尚未接受的邀请（链接只在创建时返回一次）
	Source      string       `json:"source"`
	SourceTitle string       `json:"source_title"`
	Status      string       `json:"status"`
	StatusTitle string       `json:"status_title"`
	Invitation  *invitationV `json:"invitation,omitempty"`
	// PossibleDuplicateOf 是「可能与 X 重复」提示（ADR 0017 补记四），列表调用时算一次
	PossibleDuplicateOf []app.DuplicateRef `json:"possible_duplicate_of,omitempty"`
	// AgentCount 是名下没被吊销的 Agent 数（0 = 还没接入）
	AgentCount int `json:"agent_count"`
}

func orgMemberView(m app.MemberDetail, loc i18n.Locale) orgMemberV {
	roles := m.Roles
	if roles == nil {
		roles = []string{}
	}
	status := m.DerivedStatus()
	v := orgMemberV{ID: m.ID, Name: m.Name, Email: m.Email, Roles: roles, TeamID: nullable(m.TeamID), TeamIDs: orEmpty(m.TeamIDs), Active: m.Active, IsOwner: m.IsOwner, Locale: string(m.Locale), CreatedAt: m.CreatedAt,
		Source: m.Source, SourceTitle: app.SourceTitle(m.Source, loc), Status: status, StatusTitle: i18n.Tr(loc, "member.status."+status), PossibleDuplicateOf: m.PossibleDuplicateOf, AgentCount: m.AgentCount}
	if m.Invitation != nil {
		iv := invitationView(app.InvitationView{Invitation: m.Invitation})
		v.Invitation = &iv
	}
	return v
}

func (s *Server) orgMembers(w http.ResponseWriter, r *http.Request) {
	ms, err := s.App.OrgMembers(r.Context(), sessionOf(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out := []orgMemberV{}
	for _, m := range ms {
		out = append(out, orgMemberView(m, sessionOf(r).Loc()))
	}
	writeJSON(w, 200, out)
}

func (s *Server) orgMemberPatch(w http.ResponseWriter, r *http.Request) {
	var in app.MemberPatch
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	m, err := s.App.UpdateMember(r.Context(), sessionOf(r), r.PathValue("id"), in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, orgMemberView(*m, sessionOf(r).Loc()))
}

func (s *Server) orgMembersBulk(w http.ResponseWriter, r *http.Request) {
	var in app.BulkMembersInput
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := s.App.BulkMembers(r.Context(), sessionOf(r), in)
	respond(w, r, out, err)
}

func (s *Server) orgMembersExport(w http.ResponseWriter, r *http.Request) {
	data, err := s.App.ExportMembersCSV(r.Context(), sessionOf(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="members.csv"`)
	w.WriteHeader(200)
	_, _ = w.Write(data)
}

const csvMaxBytes = 8 << 20

// readCSVBody 取导入的 CSV：multipart 里的文件（任意字段名）或文本字段 csv，JSON 里的 {csv}，否则整个请求体就是 CSV。
// readCSVBody 读 CSV 正文与对候选行的决定：JSON 形式 `{csv, decisions[]}`；multipart 形式里 `decisions` 字段是同样的 JSON 数组。
func readCSVBody(w http.ResponseWriter, r *http.Request) ([]byte, []app.ImportDecision, error) {
	r.Body = http.MaxBytesReader(w, r.Body, csvMaxBytes)
	ct := r.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(ct, "multipart/form-data"):
		if err := r.ParseMultipartForm(csvMaxBytes); err != nil {
			return nil, nil, app.Bad("err.csv_body")
		}
		var decisions []app.ImportDecision
		if v := r.FormValue("decisions"); v != "" {
			if err := json.Unmarshal([]byte(v), &decisions); err != nil {
				return nil, nil, app.Bad("err.bad_json", err.Error())
			}
		}
		for _, fhs := range r.MultipartForm.File {
			for _, fh := range fhs {
				f, err := fh.Open()
				if err != nil {
					return nil, nil, err
				}
				defer f.Close()
				data, err := io.ReadAll(f)
				return data, decisions, err
			}
		}
		if v := r.FormValue("csv"); v != "" {
			return []byte(v), decisions, nil
		}
		return nil, nil, app.Bad("err.csv_body")
	case strings.HasPrefix(ct, "application/json"):
		var in struct {
			CSV       string               `json:"csv"`
			Decisions []app.ImportDecision `json:"decisions"`
		}
		if err := decode(r, &in); err != nil {
			return nil, nil, err
		}
		if in.CSV == "" {
			return nil, nil, app.Bad("err.csv_body")
		}
		return []byte(in.CSV), in.Decisions, nil
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, nil, app.Bad("err.csv_body")
	}
	return data, nil, nil
}

func (s *Server) orgMembersImportPreview(w http.ResponseWriter, r *http.Request) {
	data, decisions, err := readCSVBody(w, r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := s.App.PreviewMembersImport(r.Context(), sessionOf(r), data, decisions)
	respond(w, r, out, err)
}

func (s *Server) orgMembersImport(w http.ResponseWriter, r *http.Request) {
	data, decisions, err := readCSVBody(w, r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := s.App.ImportMembers(r.Context(), sessionOf(r), data, decisions)
	respond(w, r, out, err)
}

// orgMemberMerge 把成员 {id} 并入 into（ADR 0017 补记四）。
func (s *Server) orgMemberMerge(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Into string `json:"into"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	m, err := s.App.MergeMembers(r.Context(), sessionOf(r), r.PathValue("id"), in.Into)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, orgMemberView(*m, sessionOf(r).Loc()))
}

func (s *Server) orgMakeOwner(w http.ResponseWriter, r *http.Request) {
	if err := s.App.MakeOwner(r.Context(), sessionOf(r), r.PathValue("id")); err != nil {
		writeErr(w, r, err)
		return
	}
	ms, err := s.App.OrgMembers(r.Context(), sessionOf(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	for _, m := range ms {
		if m.ID == r.PathValue("id") {
			writeJSON(w, 200, orgMemberView(m, sessionOf(r).Loc()))
			return
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

type invitationV struct {
	ID         string     `json:"id"`
	Email      string     `json:"email"`
	Name       string     `json:"name"`
	Roles      []string   `json:"roles"`
	URL        string     `json:"url,omitempty"`
	TeamID     *string    `json:"team_id"`
	MemberID   *string    `json:"member_id"`
	ExpiresAt  time.Time  `json:"expires_at"`
	AcceptedAt *time.Time `json:"accepted_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

func invitationView(v app.InvitationView) invitationV {
	return invitationV{ID: v.ID, Email: v.Email, Name: v.Name, Roles: v.Roles, URL: v.URL, TeamID: nullable(v.TeamID), MemberID: nullable(v.MemberID), ExpiresAt: v.ExpiresAt, AcceptedAt: v.AcceptedAt, CreatedAt: v.CreatedAt}
}

func (s *Server) orgInvitations(w http.ResponseWriter, r *http.Request) {
	invs, err := s.App.ListInvitations(r.Context(), sessionOf(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out := []invitationV{}
	for _, v := range invs {
		out = append(out, invitationView(v))
	}
	writeJSON(w, 200, out)
}

func (s *Server) orgInvite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email  string   `json:"email"`
		Name   string   `json:"name"`
		Roles  []string `json:"roles"`
		TeamID *string  `json:"team_id"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.App.Invite(r.Context(), sessionOf(r), in.Email, in.Name, in.Roles, in.TeamID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, invitationView(*v))
}

func (s *Server) orgInvitationDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.App.DeleteInvitation(r.Context(), sessionOf(r), r.PathValue("id")); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type roleV struct {
	Name        string            `json:"name"`
	Title       string            `json:"title"`
	Titles      map[string]string `json:"titles"`
	Builtin     bool              `json:"builtin"`
	Permissions []string          `json:"permissions"`
}

func roleView(ro *domain.Role, loc i18n.Locale) roleV {
	titles := map[string]string{}
	for k, v := range ro.Title {
		titles[string(k)] = v
	}
	return roleV{Name: ro.Name, Title: ro.Title.In(loc), Titles: titles, Builtin: ro.BuiltIn, Permissions: orEmpty(ro.Permissions)}
}

func (s *Server) orgRoles(w http.ResponseWriter, r *http.Request) {
	rs, err := s.App.ListRoles(r.Context(), sessionOf(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out := []roleV{}
	for _, ro := range rs {
		out = append(out, roleView(ro, sessionOf(r).Loc()))
	}
	writeJSON(w, 200, out)
}

// titleIn 接受字符串或多语言对象。
type titleIn struct{ Text i18n.Text }

func (t *titleIn) UnmarshalJSON(b []byte) error { return t.Text.UnmarshalJSON(b) }

func (s *Server) orgRolePut(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title       titleIn  `json:"title"`
		Permissions []string `json:"permissions"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	title := in.Title.Text
	// 字符串形式视为当前语言
	if len(title) == 1 {
		if v, ok := title[i18n.Default]; ok && sessionOf(r).Loc() != i18n.Default {
			title = i18n.Text{sessionOf(r).Loc(): v}
		}
	}
	ro, err := s.App.SaveRole(r.Context(), sessionOf(r), strings.ToLower(r.PathValue("name")), title, in.Permissions)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, roleView(ro, sessionOf(r).Loc()))
}

func (s *Server) orgRoleDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.App.DeleteRole(r.Context(), sessionOf(r), r.PathValue("name")); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type teamV struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	ParentID   *string  `json:"parent_id"`
	LeadID     *string  `json:"lead_id"`
	MemberIDs  []string `json:"member_ids"`
	IsBoundary bool     `json:"is_boundary"`
	// 来源（ADR 0017）：manual 或提供方代码名（feishu、wecom…）；external_name 是外部目录里的原名；active 为假表示外部目录里已不存在
	Source       string  `json:"source"`
	SourceTitle  string  `json:"source_title"`
	ExternalName *string `json:"external_name"`
	Active       bool    `json:"active"`
	// 人数：直属的正常成员数，以及含全部下级团队的不重复正常成员数
	MemberCount        int `json:"member_count"`
	SubtreeMemberCount int `json:"subtree_member_count"`
}

func teamView(t app.TeamView, loc i18n.Locale) teamV {
	return teamV{ID: t.ID, Name: t.Name, ParentID: nullable(t.ParentID), LeadID: nullable(t.LeadMemberID), MemberIDs: orEmpty(t.MemberIDs), IsBoundary: t.IsBoundary,
		Source: t.Source, SourceTitle: app.SourceTitle(t.Source, loc), ExternalName: nullable(t.ExternalName), Active: !t.Inactive,
		MemberCount: t.MemberCount, SubtreeMemberCount: t.SubtreeMemberCount}
}

// 组织的可见性策略与「某某能看到什么」预览（ADR 0013）。

func (s *Server) orgSettings(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.GetOrgSettings(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) orgSettingsPatch(w http.ResponseWriter, r *http.Request) {
	var in app.OrgSettingsPatch
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.App.UpdateOrgSettings(r.Context(), sessionOf(r), in)
	respond(w, r, v, err)
}

func (s *Server) orgVisibilityPreview(w http.ResponseWriter, r *http.Request) {
	member := r.URL.Query().Get("member")
	if member == "" {
		member = sessionOf(r).MemberID
	}
	v, err := s.App.PreviewVisibility(r.Context(), sessionOf(r), member)
	respond(w, r, v, err)
}

func (s *Server) orgTeams(w http.ResponseWriter, r *http.Request) {
	ts, err := s.App.ListTeams(r.Context(), sessionOf(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out := []teamV{}
	for _, t := range ts {
		out = append(out, teamView(t, sessionOf(r).Loc()))
	}
	writeJSON(w, 200, out)
}

func (s *Server) orgTeamCreate(w http.ResponseWriter, r *http.Request) {
	var in app.TeamPatch
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	t, err := s.App.SaveTeam(r.Context(), sessionOf(r), "", in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, teamView(*t, sessionOf(r).Loc()))
}

func (s *Server) orgTeamPatch(w http.ResponseWriter, r *http.Request) {
	var in app.TeamPatch
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	t, err := s.App.SaveTeam(r.Context(), sessionOf(r), r.PathValue("id"), in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, teamView(*t, sessionOf(r).Loc()))
}

// orgTeamMerge 把团队 {id} 并入 into（ADR 0017 补记四）。
func (s *Server) orgTeamMerge(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Into string `json:"into"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	t, err := s.App.MergeTeams(r.Context(), sessionOf(r), r.PathValue("id"), in.Into)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, teamView(*t, sessionOf(r).Loc()))
}

func (s *Server) orgTeamDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.App.DeleteTeam(r.Context(), sessionOf(r), r.PathValue("id")); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type capabilityV struct {
	Name   string            `json:"name"`
	Title  string            `json:"title"`
	Titles map[string]string `json:"titles"`
}

func (s *Server) orgCapabilities(w http.ResponseWriter, r *http.Request) {
	caps, err := s.App.Capabilities(r.Context(), sessionOf(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	loc := sessionOf(r).Loc()
	out := []capabilityV{}
	for name, t := range caps {
		titles := map[string]string{}
		for k, v := range t {
			titles[string(k)] = v
		}
		out = append(out, capabilityV{Name: name, Title: t.In(loc), Titles: titles})
	}
	sortCaps(out)
	writeJSON(w, 200, out)
}

func sortCaps(cs []capabilityV) {
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0 && cs[j].Name < cs[j-1].Name; j-- {
			cs[j], cs[j-1] = cs[j-1], cs[j]
		}
	}
}

func (s *Server) orgCapabilityPut(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title titleIn `json:"title"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	title := in.Title.Text
	if len(title) == 1 {
		if v, ok := title[i18n.Default]; ok && sessionOf(r).Loc() != i18n.Default {
			title = i18n.Text{sessionOf(r).Loc(): v}
		}
	}
	name := strings.ToLower(r.PathValue("name"))
	if err := s.App.SaveCapability(r.Context(), sessionOf(r), name, title); err != nil {
		writeErr(w, r, err)
		return
	}
	titles := map[string]string{}
	for k, v := range title {
		titles[string(k)] = v
	}
	writeJSON(w, 200, capabilityV{Name: name, Title: title.In(sessionOf(r).Loc()), Titles: titles})
}

func (s *Server) orgCapabilityDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.App.DeleteCapability(r.Context(), sessionOf(r), r.PathValue("name")); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- 目标类型（ADR 0023）----------
//
// 与能力标签、价格表同级的一页组织设置。读：所有登录成员（新建目标要选它）；写：`org_settings`。
// 这里只有分类与显示需要的字段——类型不带流程，也不决定权限、成本归口与可见范围。

type goalTypeV struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Color            string `json:"color"`
	Icon             string `json:"icon"`
	Sort             int    `json:"sort"`
	Active           bool   `json:"active"`
	DefaultPrecision string `json:"default_precision"`
	// GoalCount 是这个类型下面有多少个目标（含已达成、已放弃的）：删之前要看的就是它。
	GoalCount int `json:"goal_count"`
}

func goalTypeView(v app.GoalTypeView) goalTypeV {
	return goalTypeV{ID: v.ID, Name: v.Name, Color: v.Color, Icon: v.Icon, Sort: v.Sort, Active: v.Active,
		DefaultPrecision: string(v.DefaultPrecision), GoalCount: v.GoalCount}
}

func (s *Server) orgGoalTypes(w http.ResponseWriter, r *http.Request) {
	types, err := s.App.ListGoalTypes(r.Context(), sessionOf(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out := []goalTypeV{}
	for _, t := range types {
		out = append(out, goalTypeView(t))
	}
	writeJSON(w, 200, out)
}

func (s *Server) orgGoalTypeCreate(w http.ResponseWriter, r *http.Request) {
	var in app.GoalTypePatch
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	t, err := s.App.CreateGoalType(r.Context(), sessionOf(r), in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, goalTypeView(*t))
}

func (s *Server) orgGoalTypePatch(w http.ResponseWriter, r *http.Request) {
	var in app.GoalTypePatch
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	t, err := s.App.UpdateGoalType(r.Context(), sessionOf(r), r.PathValue("id"), in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, goalTypeView(*t))
}

func (s *Server) orgGoalTypeDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.App.DeleteGoalType(r.Context(), sessionOf(r), r.PathValue("id")); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) orgPricing(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.Pricing(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) orgPricePut(w http.ResponseWriter, r *http.Request) {
	var p domain.Price
	if err := decode(r, &p); err != nil {
		writeErr(w, r, err)
		return
	}
	p.ModelID = r.PathValue("model")
	if err := s.App.SetPriceOverride(r.Context(), sessionOf(r), p); err != nil {
		writeErr(w, r, err)
		return
	}
	s.orgPricing(w, r)
}

func (s *Server) orgPriceDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.App.DeletePriceOverride(r.Context(), sessionOf(r), r.PathValue("model")); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) orgRatePut(w http.ResponseWriter, r *http.Request) {
	var in app.ExchangeRate
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.App.SetExchangeRate(r.Context(), sessionOf(r), in.From, in.To, in.Rate); err != nil {
		writeErr(w, r, err)
		return
	}
	s.orgPricing(w, r)
}

// ---------- 邀请（公开） ----------

func (s *Server) invitationLookup(w http.ResponseWriter, r *http.Request) {
	info, err := s.App.LookupInvitation(r.Context(), r.PathValue("token"))
	respond(w, r, info, err)
}

func (s *Server) invitationAccept(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		Password string `json:"password"`
		Locale   string `json:"locale"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	loc := i18n.Normalize(in.Locale)
	if loc == "" {
		loc = localeOf(r)
	}
	token, sess, err := s.App.AcceptInvitation(r.Context(), r.PathValue("token"), in.Name, in.Password, loc)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: s.SecureCookie, SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(30 * 24 * time.Hour)})
	v, err := s.sessionView(r.WithContext(context.WithValue(r.Context(), sessKey, sess)), sess)
	respond(w, r, v, err)
}

// ---------- 平台后台 ----------

type adminKey int

const adminSessKey adminKey = 2

func (s *Server) adminRoutes(mux *http.ServeMux, pub func(string, http.HandlerFunc)) {
	pub("POST /api/v1/admin/login", s.adminLogin)
	pub("POST /api/v1/admin/logout", s.adminLogout)
	adm := func(pattern string, h http.HandlerFunc) {
		mux.Handle(pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(adminCookie)
			if err != nil || c.Value == "" {
				writeErr(w, r, app.Unauthorized("err.admin_required"))
				return
			}
			as, err := s.App.AdminSessionFromToken(r.Context(), c.Value)
			if err != nil {
				writeErr(w, r, err)
				return
			}
			h(w, r.WithContext(context.WithValue(r.Context(), adminSessKey, as)))
		}))
	}
	adm("GET /api/v1/admin/me", s.adminMe)
	adm("GET /api/v1/admin/organizations", s.adminOrgs)
	adm("POST /api/v1/admin/organizations", s.adminOrgCreate)
	adm("GET /api/v1/admin/organizations/{id}", s.adminOrg)
	adm("PATCH /api/v1/admin/organizations/{id}", s.adminOrgPatch)
	adm("GET /api/v1/admin/pricing", s.adminPricing)
	adm("PUT /api/v1/admin/pricing/{model}", s.adminPricePut)
	adm("DELETE /api/v1/admin/pricing/{model}", s.adminPriceDelete)
	adm("GET /api/v1/admin/stats", s.adminStats)
	adm("GET /api/v1/admin/admins", s.adminAdmins)
	adm("POST /api/v1/admin/admins", s.adminAddAdmin)
}

func adminView(acc *domain.Account) map[string]any {
	return map[string]any{"admin": map[string]any{"id": acc.ID, "email": acc.Email, "name": acc.Name}}
}

func (s *Server) adminLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	token, as, err := s.App.AdminLogin(r.Context(), in.Email, in.Password)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: adminCookie, Value: token, Path: "/", HttpOnly: true, Secure: s.SecureCookie, SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(30 * 24 * time.Hour)})
	writeJSON(w, 200, adminView(as.Account))
}

func (s *Server) adminLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(adminCookie); err == nil {
		_ = s.App.Logout(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: adminCookie, Value: "", Path: "/", MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminMe(w http.ResponseWriter, r *http.Request) {
	as := r.Context().Value(adminSessKey).(*app.AdminSession)
	writeJSON(w, 200, adminView(as.Account))
}

type adminOrgV struct {
	ID            string     `json:"id"`
	Slug          string     `json:"slug"`
	Name          string     `json:"name"`
	Currency      string     `json:"currency"`
	DefaultLocale string     `json:"default_locale"`
	Owner         any        `json:"owner"`
	MemberCount   int        `json:"member_count"`
	AgentCount    int        `json:"agent_count"`
	TaskCount     int        `json:"task_count"`
	Cost30d       float64    `json:"cost_30d"`
	DeactivatedAt *time.Time `json:"deactivated_at"`
	CreatedAt     time.Time  `json:"created_at"`
	OwnerInvite   string     `json:"owner_invite_url,omitempty"`
}

func adminOrgView(o *app.OrgOverview) adminOrgV {
	v := adminOrgV{ID: o.ID, Slug: o.Slug, Name: o.Name, Currency: o.Currency, DefaultLocale: o.DefaultLocale, MemberCount: o.MemberCount, AgentCount: o.AgentCount, TaskCount: o.TaskCount, Cost30d: o.Cost30d, DeactivatedAt: o.DeactivatedAt, CreatedAt: o.CreatedAt}
	if o.Owner != nil {
		v.Owner = o.Owner
	}
	return v
}

func (s *Server) adminOrgs(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.AdminListOrganizations(r.Context())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	views := []adminOrgV{}
	for _, o := range out {
		views = append(views, adminOrgView(o))
	}
	writeJSON(w, 200, views)
}

func (s *Server) adminOrg(w http.ResponseWriter, r *http.Request) {
	o, err := s.App.AdminGetOrganization(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, adminOrgView(o))
}

func (s *Server) adminOrgCreate(w http.ResponseWriter, r *http.Request) {
	var in app.CreateOrgInput
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	c, err := s.App.AdminCreateOrganization(r.Context(), in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	v := adminOrgView(c.OrgOverview)
	v.OwnerInvite = c.OwnerInviteURL
	writeJSON(w, 200, v)
}

func (s *Server) adminOrgPatch(w http.ResponseWriter, r *http.Request) {
	var in app.OrgAdminPatch
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	o, err := s.App.AdminUpdateOrganization(r.Context(), r.PathValue("id"), in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, adminOrgView(o))
}

func (s *Server) adminPricing(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.AdminGlobalPrices(r.Context())
	respond(w, r, out, err)
}

func (s *Server) adminPricePut(w http.ResponseWriter, r *http.Request) {
	var p domain.Price
	if err := decode(r, &p); err != nil {
		writeErr(w, r, err)
		return
	}
	p.ModelID = r.PathValue("model")
	if err := s.App.AdminSetGlobalPrice(r.Context(), p); err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) adminPriceDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.App.AdminDeleteGlobalPrice(r.Context(), r.PathValue("model")); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminStats(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.AdminStatsView(r.Context())
	respond(w, r, out, err)
}

func (s *Server) adminAdmins(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.AdminListAdmins(r.Context())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	views := []map[string]any{}
	for _, a := range out {
		views = append(views, map[string]any{"id": a.ID, "email": a.Email, "name": a.Name})
	}
	writeJSON(w, 200, views)
}

func (s *Server) adminAddAdmin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	acc, err := s.App.AdminAddAdmin(r.Context(), in.Email, in.Name, in.Password)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"id": acc.ID, "email": acc.Email, "name": acc.Name})
}
