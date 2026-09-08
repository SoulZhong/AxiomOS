package api

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/teemo/axiomos/internal/app"
	"github.com/teemo/axiomos/internal/directory"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 本文件把领域对象转换成网页需要的形状（web/src/lib/api.ts 里的类型）。

// Date 接受 "2006-01-02" 或 RFC3339，输出 "2006-01-02"。
type Date struct{ T *time.Time }

func (d *Date) UnmarshalJSON(b []byte) error {
	var s *string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if s == nil || *s == "" {
		d.T = nil
		return nil
	}
	if t, err := time.ParseInLocation("2006-01-02", *s, time.Local); err == nil {
		d.T = &t
		return nil
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return fmt.Errorf("日期格式应为 YYYY-MM-DD：%s", *s)
	}
	d.T = &t
	return nil
}

// OptDate 是修改输入里的日期：区分「没带」「带了 null / ""（清空）」「带了日期」。
type OptDate struct {
	Set bool
	T   *time.Time
}

func (d *OptDate) UnmarshalJSON(b []byte) error {
	d.Set = true
	var inner Date
	if err := inner.UnmarshalJSON(b); err != nil {
		return err
	}
	d.T = inner.T
	return nil
}

func dateStr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Local().Format("2006-01-02")
	return &s
}

func timeStr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}

// ---------- 通用 ----------

type ExecutorRef struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	OwnerID string `json:"owner_id,omitempty"`
}

// refs 是执行者索引。
type refs map[string]ExecutorRef

func (r refs) get(id string) *ExecutorRef {
	if id == "" {
		return nil
	}
	if x, ok := r[id]; ok {
		return &x
	}
	return &ExecutorRef{ID: id, Kind: "member", Name: id}
}

func (r refs) must(id string) ExecutorRef {
	if x := r.get(id); x != nil {
		return *x
	}
	return ExecutorRef{ID: id, Kind: "member", Name: "未知"}
}

func buildRefs(idx map[string]app.ExecutorInfo) refs {
	out := refs{}
	for id, e := range idx {
		out[id] = ExecutorRef{ID: id, Kind: string(e.Kind), Name: e.Name, OwnerID: e.OwnerID}
	}
	return out
}

// roleTitle 用组织角色表翻译角色名，缺失时回退内置。
// fieldTitles 把动态里记下的字段名换成提供方声明的显示名，拼成一句「访问令牌和接口地址」。
// 动态里只有字段名、没有字段值（配置值可能是内网地址或用户名）。
func fieldTitles(provider string, raw any, loc i18n.Locale) string {
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		return ""
	}
	prov, known := directory.Lookup(provider)
	var titles []string
	for _, x := range list {
		key, _ := x.(string)
		if key == "" {
			continue
		}
		title := key
		if known {
			if f, ok := prov.Field(key); ok {
				title = f.Title.In(loc)
			} else if f, ok := prov.MessagingField(key); ok {
				title = f.Title.In(loc)
			} else if key == "proxy_url" {
				title = i18n.Tr(loc, "directory.field.proxy_url")
			}
		} else if key == "proxy_url" {
			title = i18n.Tr(loc, "directory.field.proxy_url")
		}
		titles = append(titles, title)
	}
	return strings.Join(titles, i18n.Tr(loc, "sep.list"))
}

func roleTitle(roles map[string]i18n.Text, r string, loc i18n.Locale) string {
	if t, ok := roles[r]; ok && !t.IsZero() {
		return t.In(loc)
	}
	return domain.RoleTitle(r).In(loc)
}

var priorityNames = map[int]string{0: "urgent", 1: "high", 2: "normal", 3: "low"}
var priorityValues = map[string]int{"urgent": 0, "high": 1, "normal": 2, "low": 3}

func priorityName(p int) string {
	if n, ok := priorityNames[p]; ok {
		return n
	}
	return "normal"
}

func stateView(tt *domain.TaskType, name string, loc i18n.Locale) TaskState {
	if tt != nil {
		if s := tt.Workflow.State(name); s != nil {
			return TaskState{Name: s.Name, Title: s.Title.In(loc), Label: s.Label, Weight: s.Weight, Claimable: s.Claimable}
		}
	}
	return TaskState{Name: name, Title: name, Label: domain.LabelPending}
}

type TaskState struct {
	Name      string       `json:"name"`
	Title     string       `json:"title"`
	Label     domain.Label `json:"label"`
	Weight    *int         `json:"weight,omitempty"`
	Claimable bool         `json:"claimable,omitempty"`
}

// ---------- 会话 ----------

type OrganizationV struct {
	ID            string    `json:"id"`
	Slug          string    `json:"slug"`
	Name          string    `json:"name"`
	OwnerID       string    `json:"owner_id"`
	Currency      string    `json:"currency"`
	DefaultLocale string    `json:"default_locale"`
	CreatedAt     time.Time `json:"created_at"`
}

type MemberV struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Roles     []string  `json:"roles"`
	TeamID    *string   `json:"team_id"`
	Locale    string    `json:"locale"`
	CreatedAt time.Time `json:"created_at"`
	// 来源与激活状态（ADR 0017）：manual 或提供方代码名；active | pending_activation | inactive
	Source string `json:"source"`
	Status string `json:"status"`
}

type RoleV struct {
	Name  string `json:"name"`
	Title string `json:"title"`
}

type TeamV struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	LeadID     *string `json:"lead_id"`
	ParentID   *string `json:"parent_id"`
	IsBoundary bool    `json:"is_boundary"`
	// 来源（ADR 0017）：manual 或提供方代码名；ExternalName 是外部目录里的原名；Active 为假表示外部目录里已不存在
	Source       string  `json:"source"`
	ExternalName *string `json:"external_name"`
	Active       bool    `json:"active"`
}

func teamVOf(t *domain.Team) TeamV {
	return TeamV{ID: t.ID, Name: t.Name, LeadID: nullable(t.LeadMemberID), ParentID: nullable(t.ParentID), IsBoundary: t.IsBoundary, Source: t.Source, ExternalName: nullable(t.ExternalName), Active: !t.Inactive}
}

// ScopeV 是范围选择器里的一档（ADR 0013）。
type ScopeV struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Depth     int    `json:"depth"`
	Financial bool   `json:"financial"`
}

type VisibilityV struct {
	CollaborationVisibility string `json:"collaboration_visibility"`
	FinanceVisibility       string `json:"finance_visibility"`
}

type SessionV struct {
	Member        MemberV           `json:"member"`
	Organization  OrganizationV     `json:"organization"`
	Roles         []RoleV           `json:"roles"`
	Teams         []TeamV           `json:"teams"`
	ArtifactTypes map[string]string `json:"artifact_types"`
	Capabilities  []string          `json:"capabilities"`
	CapabilityMap map[string]string `json:"capability_titles"`
	Permissions   []string          `json:"permissions"`
	IsOwner       bool              `json:"is_owner"`
	Scopes        []ScopeV          `json:"scopes"`
	DefaultScope  string            `json:"default_scope"`
	// 组织的两项可见性策略（ADR 0013），前端据此措辞"为什么看不到成本"
	Settings *VisibilityV `json:"settings,omitempty"`
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func memberView(m *domain.Member, email string, teamOf map[string]string) MemberV {
	v := MemberV{ID: m.ID, Name: m.Name, Email: email, Roles: m.Roles, CreatedAt: m.CreatedAt, Source: m.Source, Status: m.DerivedStatus()}
	if v.Roles == nil {
		v.Roles = []string{}
	}
	v.TeamID = nullable(teamOf[m.ID])
	return v
}

func sessionView(me *app.Me, email string, teams []*domain.Team, teamOf map[string]string, caps map[string]i18n.Text, orgRoles []*domain.Role, loc i18n.Locale) SessionV {
	roles := []RoleV{}
	seen := map[string]bool{}
	for _, r := range orgRoles {
		roles = append(roles, RoleV{Name: r.Name, Title: r.Title.In(loc)})
		seen[r.Name] = true
	}
	for _, r := range me.Member.Roles {
		if !seen[r] {
			roles = append(roles, RoleV{Name: r, Title: domain.RoleTitle(r).In(loc)})
		}
	}
	tv := []TeamV{}
	for _, t := range teams {
		tv = append(tv, teamVOf(t))
	}
	capNames := make([]string, 0, len(caps))
	capTitles := map[string]string{}
	for n, t := range caps {
		capNames = append(capNames, n)
		capTitles[n] = t.In(loc)
	}
	sort.Strings(capNames)
	arts := map[string]string{}
	for k, t := range domain.BuiltinArtifactTitles {
		arts[k] = t.In(loc)
	}
	m := memberView(me.Member, email, teamOf)
	m.Locale = string(loc)
	perms := me.Permissions
	if perms == nil {
		perms = []string{}
	}
	sort.Strings(perms)
	scopes := []ScopeV{}
	for _, sc := range me.Scopes {
		scopes = append(scopes, ScopeV{ID: sc.ID, Title: sc.Title, Depth: sc.Depth, Financial: sc.Financial})
	}
	return SessionV{
		Scopes:        scopes,
		DefaultScope:  me.DefaultScope,
		Settings:      &VisibilityV{CollaborationVisibility: string(me.Organization.CollaborationVisibility), FinanceVisibility: string(me.Organization.FinanceVisibility)},
		Member:        m,
		Organization:  OrganizationV{ID: me.Organization.ID, Slug: me.Organization.Slug, Name: me.Organization.Name, OwnerID: me.Organization.OwnerMemberID, Currency: me.Organization.Currency, DefaultLocale: me.Organization.DefaultLocale, CreatedAt: me.Organization.CreatedAt},
		Roles:         roles,
		Teams:         tv,
		ArtifactTypes: arts,
		Capabilities:  capNames,
		CapabilityMap: capTitles,
		Permissions:   perms,
		IsOwner:       me.IsOrgOwner,
	}
}

// ---------- Agent ----------

type GrantV struct {
	Name domain.Grant     `json:"name"`
	Mode domain.GrantMode `json:"mode"`
}

type AgentV struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Owner        ExecutorRef `json:"owner"`
	Shared       bool        `json:"shared"`
	Capabilities []string    `json:"capabilities"`
	Grants       []GrantV    `json:"grants"`
	// State / StateTitle 是对外的状态：执行中 / 可用 / 已停用（CONTEXT.md「Agent 状态」）。
	State      domain.AgentState `json:"state"`
	StateTitle string            `json:"state_title"`
	// Online 是旧口径（最近有没有心跳），只为兼容旧客户端保留，界面不再用它。
	Online bool `json:"online"`
	// LastActiveAt 是最近一次活动：心跳与最近一次工具调用里更晚的那个。
	LastSeenAt     *string   `json:"last_seen_at"`
	LastActiveAt   *string   `json:"last_active_at"`
	MaxConcurrency int       `json:"max_concurrency"`
	Runtime        string    `json:"runtime"`
	CreatedAt      time.Time `json:"created_at"`
	// CanManage：当前用户能否修改 / 吊销它（所有者、组织负责人或持「组织设置」权限的成员）
	CanManage bool `json:"can_manage"`
}

func agentView(a *domain.Agent, r refs, now time.Time, canManage bool, st app.AgentStatus) AgentV {
	grants := []GrantV{}
	for g, m := range a.Grants {
		grants = append(grants, GrantV{Name: g, Mode: m})
	}
	sort.Slice(grants, func(i, j int) bool { return grants[i].Name < grants[j].Name })
	return AgentV{ID: a.ID, Name: a.Name, Owner: r.must(a.OwnerMemberID), Shared: a.Shared, Capabilities: a.Capabilities, Grants: grants,
		State: st.State, StateTitle: st.Title, Online: a.Online(now), LastSeenAt: timeStr(a.LastSeenAt), LastActiveAt: timeStr(st.LastActiveAt),
		MaxConcurrency: a.MaxConcurrent, Runtime: a.Runtime, CreatedAt: a.CreatedAt, CanManage: canManage}
}

type AgentInputV struct {
	Name           string   `json:"name"`
	Runtime        string   `json:"runtime"`
	Capabilities   []string `json:"capabilities"`
	Grants         []GrantV `json:"grants"`
	Shared         bool     `json:"shared"`
	MaxConcurrency int      `json:"max_concurrency"`
}

func (in AgentInputV) toApp() app.RegisterAgentInput {
	out := app.RegisterAgentInput{Name: in.Name, Runtime: in.Runtime, Capabilities: in.Capabilities, Shared: in.Shared, MaxConcurrent: in.MaxConcurrency}
	if in.Grants != nil {
		out.Grants = map[domain.Grant]domain.GrantMode{}
		for _, g := range in.Grants {
			mode := g.Mode
			if mode == "" {
				mode = domain.GrantDirect
			}
			out.Grants[g.Name] = mode
		}
	}
	return out
}

// ---------- 目标 ----------

type GoalV struct {
	ID            string      `json:"id"`
	Title         string      `json:"title"`
	Description   string      `json:"description"`
	Owner         ExecutorRef `json:"owner"`
	ParentID      *string     `json:"parent_id"`
	TeamID        *string     `json:"team_id"`
	Progress      int         `json:"progress"`
	Achieved      bool        `json:"achieved"`
	Status        string      `json:"status"`
	Budget        *float64    `json:"budget"`
	Cost          float64     `json:"cost"`
	OverBudget    bool        `json:"over_budget"`
	PlannedStart  *string     `json:"planned_start"`
	PlannedEnd    *string     `json:"planned_end"`
	ActualStart   *string     `json:"actual_start"`
	ActualEnd     *string     `json:"actual_end"`
	Deadline      *string     `json:"deadline"`
	TaskCount     int         `json:"task_count"`
	DoneTaskCount int         `json:"done_task_count"`
	Children      []GoalV     `json:"children"`
	CreatedAt     time.Time   `json:"created_at"`
	// 路线图三字段（ADR 0021）：取值原样给，标题按当前语言渲染，没填时两个都是空串
	Horizon         string `json:"horizon"`
	HorizonTitle    string `json:"horizon_title"`
	Confidence      string `json:"confidence"`
	ConfidenceTitle string `json:"confidence_title"`
	Outcome         string `json:"outcome"`
	// 时间线两字段（ADR 0022）。date_precision 给的是有效值：落库的空一律读作 week，界面上不用再判空。
	DatePrecision      string   `json:"date_precision"`
	DatePrecisionTitle string   `json:"date_precision_title"`
	Rank               *float64 `json:"rank"`
	// 条画在刻度边界上：这两个字段是按粒度吸附之后的起止（每一档都吸，到周吸到周一与周日），
	// 服务端算好免得两边算不一样。有日期就有值，画条只看它们；planned_start / planned_end 仍是原值。
	PlannedStartSnapped *string `json:"planned_start_snapped,omitempty"`
	PlannedEndSnapped   *string `json:"planned_end_snapped,omitempty"`
	// 汇总（ADR 0022 第 6 条，只在读时算、不落库）：目标自己没填计划起止时从子目标与任务推出来的日期，
	// 以及「这一端是推出来的吗」——为真的那一端画斜纹 / 渐隐。
	DerivedStart *string  `json:"derived_start,omitempty"`
	DerivedEnd   *string  `json:"derived_end,omitempty"`
	Derived      DerivedV `json:"derived"`
	// 里程碑（ADR 0016）：目标自己的，按日期升序
	Milestones       []MilestoneV      `json:"milestones"`
	MilestoneSummary MilestoneSummaryV `json:"milestone_summary"`
	// 目标类型（ADR 0023）：未分类时为 null。只带显示要用的四个字段——类型不带流程，也没有别的行为。
	Type *GoalTypeRefV `json:"type"`
}

// GoalTypeRefV 是目标身上带的那枚类型标签：只有显示要用的字段。
type GoalTypeRefV struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Icon  string `json:"icon"`
}

// DerivedV 说的是条的两端各自是不是推算出来的（目标自己没填那一端的日期）。
type DerivedV struct {
	Start bool `json:"start"`
	End   bool `json:"end"`
}

// goalView 递归转换；actual 从任务实际时间聚合。loc 用来渲染时间桶与信心度的名字（它们只存取值，不存名字）。
func goalView(g *app.GoalView, r refs, actual map[string][2]*time.Time, loc i18n.Locale) GoalV {
	v := GoalV{ID: g.ID, Title: g.Title, Description: g.Description, Owner: r.must(g.OwnerMemberID), ParentID: nullable(g.ParentID), TeamID: nullable(g.TeamID),
		Progress: g.Progress, Achieved: g.Status == domain.GoalAchieved, Status: string(g.Status), Budget: g.Budget, Cost: g.Cost, OverBudget: g.OverBudget,
		PlannedStart: dateStr(g.Start), PlannedEnd: dateStr(g.End), Deadline: dateStr(g.Deadline), TaskCount: g.TaskCount, DoneTaskCount: g.DoneCount, Children: []GoalV{}, CreatedAt: g.CreatedAt,
		Milestones: milestoneViews(g.Milestones, r), MilestoneSummary: milestoneSummaryView(g),
		Horizon: string(g.Horizon), HorizonTitle: enumTitle(loc, "horizon", string(g.Horizon)),
		Confidence: string(g.Confidence), ConfidenceTitle: enumTitle(loc, "confidence", string(g.Confidence)), Outcome: g.Outcome,
		Rank: g.Rank}
	// 时间线（ADR 0022）：粒度的空值一律读作「到周」
	p := domain.EffectiveDatePrecision(g.DatePrecision)
	v.DatePrecision, v.DatePrecisionTitle = string(p), enumTitle(loc, "date_precision", string(p))
	// 条画在哪：自己填了就用自己的，没填才用推算值，并把「这一端是推出来的」告诉界面
	start, end := g.PlannedStart, g.PlannedEnd
	if start == nil && g.DerivedStart != nil {
		start = g.DerivedStart
		v.DerivedStart, v.Derived.Start = dateStr(g.DerivedStart), true
	}
	if end == nil && g.DerivedEnd != nil {
		end = g.DerivedEnd
		v.DerivedEnd, v.Derived.End = dateStr(g.DerivedEnd), true
	}
	if start != nil || end != nil {
		s, e := zeroTime(start), zeroTime(end)
		s, e = domain.SnapRange(s, e, p)
		if start != nil {
			v.PlannedStartSnapped = dateStr(&s)
		}
		if end != nil {
			v.PlannedEndSnapped = dateStr(&e)
		}
	}
	if g.PlannedStart != nil {
		v.PlannedStart = dateStr(g.PlannedStart)
	}
	if g.PlannedEnd != nil {
		v.PlannedEnd = dateStr(g.PlannedEnd)
	}
	if a, ok := actual[g.ID]; ok {
		v.ActualStart, v.ActualEnd = dateStr(a[0]), dateStr(a[1])
	}
	if g.Type != nil {
		v.Type = &GoalTypeRefV{ID: g.Type.ID, Name: g.Type.Name, Color: g.Type.Color, Icon: g.Type.Icon}
	}
	for _, c := range g.Children {
		v.Children = append(v.Children, goalView(c, r, actual, loc))
	}
	return v
}

// zeroTime 把「可能没有的日期」摊平成零值，交给 domain.SnapRange 判断这一端要不要算。
func zeroTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// enumTitle 渲染写死词表（时间桶、信心度、时间粒度）的名字；空值没有名字。
func enumTitle(loc i18n.Locale, kind, v string) string {
	if v == "" {
		return ""
	}
	return i18n.Tr(loc, kind+"."+v)
}

// actualSpans 计算每个目标（含子树）任务的实际起止。
func actualSpans(tree []*app.GoalView, tasks []app.TaskSummary) map[string][2]*time.Time {
	byGoal := map[string][2]*time.Time{}
	for _, t := range tasks {
		if t.GoalID == "" {
			continue
		}
		cur := byGoal[t.GoalID]
		cur[0] = minT(cur[0], t.ActualStart)
		cur[1] = maxT(cur[1], t.ActualEnd)
		byGoal[t.GoalID] = cur
	}
	var up func(v *app.GoalView) [2]*time.Time
	up = func(v *app.GoalView) [2]*time.Time {
		cur := byGoal[v.ID]
		for _, c := range v.Children {
			cc := up(c)
			cur[0] = minT(cur[0], cc[0])
			cur[1] = maxT(cur[1], cc[1])
		}
		byGoal[v.ID] = cur
		return cur
	}
	for _, v := range tree {
		up(v)
	}
	return byGoal
}

func minT(a, b *time.Time) *time.Time {
	if a == nil {
		return b
	}
	if b == nil || a.Before(*b) {
		return a
	}
	return b
}
func maxT(a, b *time.Time) *time.Time {
	if a == nil {
		return b
	}
	if b == nil || a.After(*b) {
		return a
	}
	return b
}

type GoalInputV struct {
	Title        *string  `json:"title"`
	Description  *string  `json:"description"`
	OwnerID      *string  `json:"owner_id"`
	ParentID     *string  `json:"parent_id"`
	TeamID       *string  `json:"team_id"`
	Budget       OptFloat `json:"budget"`        // null 表示清空
	PlannedStart OptDate  `json:"planned_start"` // 修改时 null / "" 表示清空
	PlannedEnd   OptDate  `json:"planned_end"`
	Deadline     OptDate  `json:"deadline"`
	Achieved     *bool    `json:"achieved"`
	// "active" | "abandoned"：放弃目标 / 重新开始（"achieved" 请用 achieved 字段）
	Status *string `json:"status"`
	// 目标类型（ADR 0023）："" 表示清空（未分类）
	TypeID *string `json:"type_id"`
	// 路线图三字段（ADR 0021）："" 表示清空（未排期 / 没填 / 去掉成果指标）
	Horizon    *string `json:"horizon"`
	Confidence *string `json:"confidence"`
	Outcome    *string `json:"outcome"`
	// 时间线两字段（ADR 0022）：时间粒度 "" 与 "week" 等价（在时间线上拖条就是送 "week"）；
	// 排序权重传 null 表示清空，改回按日期与创建时间排
	DatePrecision *string  `json:"date_precision"`
	Rank          OptFloat `json:"rank"`
}

// toUpdate 把接口层的目标输入换成应用层的修改输入：null / "" 的日期与 null 的预算表示清空；
// 计划结束同时也是截止日（与创建时一致），显式带了 deadline 时以 deadline 为准。
func (in GoalInputV) toUpdate() (app.UpdateGoalInput, error) {
	ui := app.UpdateGoalInput{Title: in.Title, Description: in.Description, OwnerMemberID: in.OwnerID, TeamID: in.TeamID, TypeID: in.TypeID, ParentID: in.ParentID}
	if in.Budget.Set {
		if in.Budget.V == nil {
			ui.Clear = append(ui.Clear, "budget")
		} else {
			ui.Budget = in.Budget.V
		}
	}
	if in.PlannedStart.Set {
		if in.PlannedStart.T == nil {
			ui.Clear = append(ui.Clear, "planned_start")
		}
		ui.PlannedStart = in.PlannedStart.T
	}
	if in.PlannedEnd.Set {
		if in.PlannedEnd.T == nil {
			ui.Clear = append(ui.Clear, "planned_end", "deadline")
		}
		ui.PlannedEnd, ui.Deadline = in.PlannedEnd.T, in.PlannedEnd.T
	}
	if in.Deadline.Set {
		if in.Deadline.T == nil {
			ui.Clear = append(ui.Clear, "deadline")
		} else {
			ui.Clear = removeStr(ui.Clear, "deadline")
		}
		ui.Deadline = in.Deadline.T
	}
	if in.Achieved != nil {
		st := domain.GoalActive
		if *in.Achieved {
			st = domain.GoalAchieved
		}
		ui.Status = &st
	}
	if in.Horizon != nil {
		h := domain.GoalHorizon(*in.Horizon)
		ui.Horizon = &h
	}
	if in.Confidence != nil {
		c := domain.GoalConfidence(*in.Confidence)
		ui.Confidence = &c
	}
	if in.DatePrecision != nil {
		p := domain.DatePrecision(*in.DatePrecision)
		ui.DatePrecision = &p
	}
	if in.Rank.Set {
		if in.Rank.V == nil {
			ui.Clear = append(ui.Clear, "rank")
		} else {
			ui.Rank = in.Rank.V
		}
	}
	ui.Outcome = in.Outcome
	if in.Status != nil {
		// 只开放「放弃」与「重新开始」；达成走 achieved 字段，草稿不从这里改
		switch *in.Status {
		case string(domain.GoalAbandoned):
			st := domain.GoalAbandoned
			ui.Status = &st
		case string(domain.GoalActive):
			st := domain.GoalActive
			ui.Status = &st
		default:
			return ui, app.Bad("err.goal_status", *in.Status)
		}
	}
	return ui, nil
}

// ---------- 任务 ----------

type TaskRefV struct {
	ID     string    `json:"id"`
	Number int       `json:"number"`
	Title  string    `json:"title"`
	Type   string    `json:"type"`
	State  TaskState `json:"state"`
}

type RelationV struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	From      TaskRefV  `json:"from"`
	To        TaskRefV  `json:"to"`
	CreatedAt time.Time `json:"created_at"`
}

type ArtifactV struct {
	ID         string      `json:"id"`
	Type       string      `json:"type"`
	Title      string      `json:"title"`
	URL        string      `json:"url"`
	AttachedBy ExecutorRef `json:"attached_by"`
	CreatedAt  time.Time   `json:"created_at"`
}

type CommentV struct {
	ID        string      `json:"id"`
	Kind      string      `json:"kind"`
	Author    ExecutorRef `json:"author"`
	Body      string      `json:"body"`
	CreatedAt time.Time   `json:"created_at"`
}

type UsageV struct {
	Model        string `json:"model"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	TotalTokens  int64  `json:"total_tokens"`
	DurationMs   int64  `json:"duration_ms"`
	ToolCalls    int64  `json:"tool_calls"`
}

type RunV struct {
	ID          string      `json:"id"`
	TaskID      string      `json:"task_id"`
	State       string      `json:"state"`
	StateTitle  string      `json:"state_title"`
	Executor    ExecutorRef `json:"executor"`
	StartedAt   time.Time   `json:"started_at"`
	EndedAt     *time.Time  `json:"ended_at"`
	Outcome     string      `json:"outcome"`
	Usage       []UsageV    `json:"usage"`
	TotalTokens int64       `json:"total_tokens"`
	Cost        float64     `json:"cost"`
}

func runView(rr *store.RunRow, tt *domain.TaskType, r refs, loc i18n.Locale) RunV {
	v := RunV{ID: rr.ID, TaskID: rr.TaskID, State: rr.State, StateTitle: rr.State, Executor: r.must(rr.ExecutorID), StartedAt: rr.StartedAt, EndedAt: rr.EndedAt, Cost: rr.Cost, Usage: []UsageV{}}
	if tt != nil {
		if s := tt.Workflow.State(rr.State); s != nil {
			v.StateTitle = s.Title.In(loc)
		}
	}
	switch {
	case rr.EndedAt == nil:
		v.Outcome = "running"
	case rr.Outcome == domain.RunCompleted:
		v.Outcome = "ended"
	default:
		v.Outcome = "cancelled"
	}
	end := time.Now()
	if rr.EndedAt != nil {
		end = *rr.EndedAt
	}
	dur := end.Sub(rr.StartedAt).Milliseconds()
	for _, u := range rr.Usage {
		v.Usage = append(v.Usage, UsageV{Model: u.ModelID, InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, TotalTokens: u.TotalTokens(), DurationMs: dur, ToolCalls: u.ToolCalls})
		v.TotalTokens += u.TotalTokens()
	}
	return v
}

type ParticipantV struct {
	Title    string       `json:"title"`
	Role     string       `json:"role"`
	Executor *ExecutorRef `json:"executor"`
}

type TaskV struct {
	ID                   string                  `json:"id"`
	Number               int                     `json:"number"` // 组织内序号，界面上写成 #123
	GoalID               *string                 `json:"goal_id"`
	Goal                 *GoalRefV               `json:"goal"`
	ParentID             *string                 `json:"parent_id"`
	Type                 string                  `json:"type"`
	TypeTitle            string                  `json:"type_title"`
	TypeVersion          int                     `json:"type_version"`
	Title                string                  `json:"title"`
	Description          string                  `json:"description"`
	State                TaskState               `json:"state"`
	PreviousState        *string                 `json:"previous_state"`
	Creator              ExecutorRef             `json:"creator"`
	Assignee             *ExecutorRef            `json:"assignee"`
	Reviewer             ExecutorRef             `json:"reviewer"`
	Participants         map[string]ParticipantV `json:"participants"`
	RequiredRole         *string                 `json:"required_role"`
	PendingParticipant   *string                 `json:"pending_participant"`
	RequiredCapabilities []string                `json:"required_capabilities"`
	HumanOnly            bool                    `json:"human_only"`
	Relations            []RelationV             `json:"relations"`
	Artifacts            []ArtifactV             `json:"artifacts"`
	Comments             []CommentV              `json:"comments"`
	Runs                 []RunV                  `json:"runs"`
	Subtasks             []TaskRefV              `json:"subtasks"`
	PlannedStart         *string                 `json:"planned_start"`
	PlannedEnd           *string                 `json:"planned_end"`
	ActualStart          *string                 `json:"actual_start"`
	ActualEnd            *string                 `json:"actual_end"`
	Estimate             *float64                `json:"estimate"`
	Priority             string                  `json:"priority"`
	Progress             int                     `json:"progress"`
	Overdue              bool                    `json:"overdue"`
	TotalTokens          int64                   `json:"total_tokens"`
	Cost                 float64                 `json:"cost"`
	Fields               map[string]any          `json:"fields"`
	Points               *int                    `json:"points"`
	Sprint               *SprintRefV             `json:"sprint"`
	// 外部链接与 PR 小标（ADR 0020）
	Links     []app.LinkView `json:"links"`
	PR        *app.TaskPR    `json:"pr,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// GoalRefV 是任务上的目标引用。
type GoalRefV struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type goalTitleFn func(id string) string
type sprintTitleFn func(id string) string

func sprintRef(id string, st sprintTitleFn) *SprintRefV {
	if id == "" {
		return nil
	}
	name := ""
	if st != nil {
		name = st(id)
	}
	return &SprintRefV{ID: id, Name: name}
}

func taskRef(t *domain.Task, tt *domain.TaskType, loc i18n.Locale) TaskRefV {
	return TaskRefV{ID: t.ID, Number: t.Number, Title: t.Title, Type: t.TypeName, State: stateView(tt, t.State, loc)}
}

func relationTypeOut(t domain.RelationType) string {
	if t == domain.RelationRelatesTo {
		return "related"
	}
	return string(t)
}

func relationTypeIn(s string) domain.RelationType {
	if s == "related" {
		return domain.RelationRelatesTo
	}
	return domain.RelationType(s)
}

// taskView 从详情组装任务视图。
func taskView(d *app.TaskDetail, incoming []app.IncomingRelation, related map[string]*domain.Task, types map[string]*domain.TaskType, r refs, goalTitle goalTitleFn, sprintTitle sprintTitleFn, loc i18n.Locale) TaskV {
	t, tt := d.Task, d.Type
	v := TaskV{ID: t.ID, Number: t.Number, GoalID: nullable(t.GoalID), ParentID: nullable(t.ParentID), Type: t.TypeName, TypeTitle: tt.Title.In(loc), TypeVersion: t.TypeVersion, Title: t.Title, Description: t.Description,
		State: stateView(tt, t.State, loc), PreviousState: nullable(t.PreviousState), Creator: r.must(t.CreatorID), Assignee: r.get(t.AssigneeID), Reviewer: r.must(t.ReviewerID),
		Participants: map[string]ParticipantV{}, RequiredRole: nullable(t.RequiredRole), PendingParticipant: nullable(t.PendingSlot), RequiredCapabilities: t.RequiredCapabilities, HumanOnly: t.HumanOnly,
		Relations: []RelationV{}, Artifacts: []ArtifactV{}, Comments: []CommentV{}, Runs: []RunV{}, Subtasks: []TaskRefV{}, Links: d.Links,
		PlannedStart: dateStr(t.PlannedStart), PlannedEnd: dateStr(t.PlannedEnd), ActualStart: dateStr(t.ActualStart), ActualEnd: dateStr(t.ActualEnd),
		Estimate: t.EstimateHours, Priority: priorityName(t.Priority), Progress: d.Progress, Overdue: d.Overdue, Cost: d.Cost, Fields: t.Fields, Points: t.Points, Sprint: sprintRef(t.SprintID, sprintTitle), CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
	if v.RequiredCapabilities == nil {
		v.RequiredCapabilities = []string{}
	}
	if v.Links == nil {
		v.Links = []app.LinkView{}
	}
	// PR 小标：最近更新的那条 PR 链接
	var latest *app.LinkView
	prCount := 0
	for i := range v.Links {
		if v.Links[i].Kind != "pr" {
			continue
		}
		prCount++
		if latest == nil || v.Links[i].UpdatedAt.After(latest.UpdatedAt) {
			latest = &v.Links[i]
		}
	}
	if latest != nil {
		v.PR = &app.TaskPR{Status: latest.Status, StatusTitle: latest.StatusTitle, URL: latest.URL, Title: latest.Title, Count: prCount}
	}
	if v.Fields == nil {
		v.Fields = map[string]any{}
	}
	if t.GoalID != "" {
		v.Goal = &GoalRefV{ID: t.GoalID, Title: goalTitle(t.GoalID)}
	}
	for _, p := range tt.Participants {
		v.Participants[p.Slot] = ParticipantV{Title: p.Title.In(loc), Role: p.Role, Executor: r.get(t.Participants[p.Slot])}
	}
	self := taskRef(t, tt, loc)
	for i, rel := range t.Relations {
		o := related[rel.OtherID]
		if o == nil {
			continue
		}
		v.Relations = append(v.Relations, RelationV{ID: fmt.Sprintf("%s:%s:%s", t.ID, rel.Type, rel.OtherID), Type: relationTypeOut(rel.Type), From: self, To: taskRef(o, types[o.TypeName], loc), CreatedAt: t.CreatedAt.Add(time.Duration(i) * time.Second)})
	}
	for _, in := range incoming {
		v.Relations = append(v.Relations, RelationV{ID: fmt.Sprintf("%s:%s:%s", in.Task.ID, in.Type, t.ID), Type: relationTypeOut(in.Type), From: taskRef(in.Task, types[in.Task.TypeName], loc), To: self, CreatedAt: in.Task.CreatedAt})
	}
	for _, a := range t.Artifacts {
		v.Artifacts = append(v.Artifacts, ArtifactV{ID: a.ID, Type: a.Type, Title: a.Title, URL: a.Ref, AttachedBy: r.must(a.ByID), CreatedAt: a.CreatedAt})
	}
	for _, c := range t.Comments {
		kind := "comment"
		if c.IsNote {
			kind = "note"
		}
		v.Comments = append(v.Comments, CommentV{ID: c.ID, Kind: kind, Author: r.must(c.ByID), Body: c.Text, CreatedAt: c.CreatedAt})
	}
	for _, rr := range d.Runs {
		rv := runView(rr, tt, r, loc)
		v.TotalTokens += rv.TotalTokens
		v.Runs = append(v.Runs, rv)
	}
	for _, s := range d.Subtasks {
		v.Subtasks = append(v.Subtasks, TaskRefV{ID: s.ID, Number: s.Number, Title: s.Title, Type: s.TypeName, State: TaskState{Name: s.State.Name, Title: s.State.Title, Label: s.State.Label}})
	}
	return v
}

// taskListView 把摘要转成列表用的（精简）任务视图。
func taskListView(s app.TaskSummary, r refs, goalTitle goalTitleFn, sprintTitle sprintTitleFn) TaskV {
	v := TaskV{ID: s.ID, Number: s.Number, GoalID: nullable(s.GoalID), ParentID: nullable(s.ParentID), Type: s.TypeName, TypeTitle: s.TypeTitle, Title: s.Title,
		State: TaskState{Name: s.State.Name, Title: s.State.Title, Label: s.State.Label}, Assignee: r.get(s.AssigneeID), Creator: ExecutorRef{}, Reviewer: ExecutorRef{},
		Participants: map[string]ParticipantV{}, RequiredRole: nullable(s.RequiredRole), RequiredCapabilities: []string{}, HumanOnly: s.HumanOnly,
		Relations: []RelationV{}, Artifacts: []ArtifactV{}, Comments: []CommentV{}, Runs: []RunV{}, Subtasks: []TaskRefV{}, Links: []app.LinkView{}, PR: s.PR,
		PlannedStart: dateStr(s.PlannedStart), PlannedEnd: dateStr(s.PlannedEnd), ActualStart: dateStr(s.ActualStart), ActualEnd: dateStr(s.ActualEnd),
		Priority: priorityName(s.Priority), Progress: s.Progress, Overdue: s.Overdue, Cost: s.Cost, Fields: map[string]any{}, Points: s.Points, Sprint: sprintRef(s.SprintID, sprintTitle), CreatedAt: s.CreatedAt, UpdatedAt: s.CreatedAt}
	if s.GoalID != "" {
		v.Goal = &GoalRefV{ID: s.GoalID, Title: goalTitle(s.GoalID)}
	}
	return v
}

type TaskInputV struct {
	Title                *string           `json:"title"`
	Type                 *string           `json:"type"`
	GoalID               *string           `json:"goal_id"`
	ParentID             *string           `json:"parent_id"`
	Description          *string           `json:"description"`
	AssigneeID           *string           `json:"assignee_id"`
	ReviewerID           *string           `json:"reviewer_id"`
	Participants         map[string]string `json:"participants"`
	RequiredCapabilities []string          `json:"required_capabilities"`
	HumanOnly            *bool             `json:"human_only"`
	PlannedStart         OptDate           `json:"planned_start"` // 修改时 null / "" 表示清空
	PlannedEnd           OptDate           `json:"planned_end"`
	Estimate             OptFloat          `json:"estimate"` // 与 estimate_hours 等价；null 表示清空
	EstimateHours        OptFloat          `json:"estimate_hours"`
	Priority             *string           `json:"priority"`
	Fields               map[string]any    `json:"fields"`
	Points               OptInt            `json:"points"`
	SprintID             *string           `json:"sprint_id"`
	Ready                *bool             `json:"ready"`
}

// estimate 取 estimate 与 estimate_hours 里带了的那个（两者等价）。
func (in TaskInputV) estimate() OptFloat {
	if in.Estimate.Set {
		return in.Estimate
	}
	return in.EstimateHours
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (in TaskInputV) toCreate() app.CreateTaskInput {
	out := app.CreateTaskInput{GoalID: str(in.GoalID), ParentID: str(in.ParentID), TypeName: str(in.Type), Title: str(in.Title), Description: str(in.Description), AssigneeID: str(in.AssigneeID), ReviewerID: str(in.ReviewerID),
		Participants: in.Participants, RequiredCapabilities: in.RequiredCapabilities, EstimateHours: in.estimate().V, Fields: in.Fields, Ready: true, Points: in.Points.V, SprintID: str(in.SprintID)}
	if in.HumanOnly != nil {
		out.HumanOnly = *in.HumanOnly
	}
	if in.Ready != nil {
		out.Ready = *in.Ready
	}
	if in.Priority != nil {
		if p, ok := priorityValues[*in.Priority]; ok {
			out.Priority = &p
		}
	}
	if in.PlannedStart.Set {
		out.PlannedStart = in.PlannedStart.T
	}
	if in.PlannedEnd.Set {
		out.PlannedEnd = in.PlannedEnd.T
	}
	return out
}

func (in TaskInputV) toUpdate() app.UpdateTaskInput {
	out := app.UpdateTaskInput{Title: in.Title, Description: in.Description, ReviewerID: in.ReviewerID, HumanOnly: in.HumanOnly, GoalID: in.GoalID, ParentID: in.ParentID,
		Participants: in.Participants, Fields: in.Fields, Points: in.Points.V, SetPoints: in.Points.Set, SprintID: in.SprintID}
	if in.RequiredCapabilities != nil {
		caps := in.RequiredCapabilities
		out.RequiredCapabilities = &caps
	}
	if est := in.estimate(); est.Set {
		if est.V == nil {
			out.Clear = append(out.Clear, "estimate_hours")
		} else {
			out.EstimateHours = est.V
		}
	}
	if in.Priority != nil {
		if p, ok := priorityValues[*in.Priority]; ok {
			out.Priority = &p
		}
	}
	if in.PlannedStart.Set {
		if in.PlannedStart.T == nil {
			out.Clear = append(out.Clear, "planned_start")
		}
		out.PlannedStart = in.PlannedStart.T
	}
	if in.PlannedEnd.Set {
		if in.PlannedEnd.T == nil {
			out.Clear = append(out.Clear, "planned_end")
		}
		out.PlannedEnd = in.PlannedEnd.T
	}
	return out
}

// ---------- 流程可用性 ----------

type TransitionAvailabilityV struct {
	Name      string `json:"name"`
	Title     string `json:"title"`
	To        string `json:"to"`
	Available bool   `json:"available"`
	// NeedsApproval 为真时这一步可以走，但会先记成待确认操作，等人确认（ADR 0003）
	NeedsApproval bool         `json:"needs_approval,omitempty"`
	Reasons       []string     `json:"reasons"`
	Requires      []string     `json:"requires"`
	LabelTo       domain.Label `json:"label_to"`
}

type WorkflowAvailabilityV struct {
	TaskID       string                    `json:"task_id"`
	State        TaskState                 `json:"state"`
	ActiveRun    *ActiveRunV               `json:"active_run"`
	Transitions  []TransitionAvailabilityV `json:"transitions"`
	CanBegin     bool                      `json:"can_begin"`
	CanClaim     bool                      `json:"can_claim"`
	BeginReasons []string                  `json:"begin_reasons"`
	ClaimReasons []string                  `json:"claim_reasons"`
	Progress     int                       `json:"progress"`
}

// ActiveRunV 是进行中的执行记录引用。
type ActiveRunV struct {
	ID         string `json:"id"`
	ExecutorID string `json:"executor_id"`
}

func workflowView(w *app.WorkflowView, tt *domain.TaskType, loc i18n.Locale) WorkflowAvailabilityV {
	v := WorkflowAvailabilityV{TaskID: w.TaskID, State: stateView(tt, w.State.Name, loc), Transitions: []TransitionAvailabilityV{}, CanBegin: w.CanBegin, CanClaim: w.CanClaim, BeginReasons: orEmpty(w.BeginWhyNot), ClaimReasons: orEmpty(w.ClaimWhyNot), Progress: w.Progress}
	if w.ActiveRun != nil {
		v.ActiveRun = &ActiveRunV{ID: w.ActiveRun.ID, ExecutorID: w.ActiveRun.ExecutorID}
	}
	for _, a := range w.Transitions {
		labelTo := domain.LabelPending
		if s := tt.Workflow.State(a.To); s != nil {
			labelTo = s.Label
		}
		v.Transitions = append(v.Transitions, TransitionAvailabilityV{Name: a.Name, Title: a.Title, To: a.To, Available: a.Available, NeedsApproval: a.NeedsApproval, Reasons: orEmpty(a.Reasons), Requires: orEmpty(a.Requires), LabelTo: labelTo})
	}
	return v
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ---------- 任务类型 ----------

type ParticipantDefV struct {
	Title string `json:"title"`
	Role  string `json:"role"`
}

type TransitionDefV struct {
	Name     string       `json:"name"`
	Title    string       `json:"title"`
	From     []string     `json:"from"`
	To       string       `json:"to"`
	By       []string     `json:"by"`
	Requires []string     `json:"requires"`
	Grant    domain.Grant `json:"grant,omitempty"`
	AssignTo *string      `json:"assign_to"`
	// 由外部事件触发（ADR 0020）；不填表示只能由人或 Agent 触发
	TriggeredBy *domain.Trigger `json:"triggered_by,omitempty"`
}

type WorkflowDefV struct {
	Version     int                  `json:"version"`
	Initial     string               `json:"initial"`
	States      map[string]TaskState `json:"states"`
	StateOrder  []string             `json:"state_order"`
	Transitions []TransitionDefV     `json:"transitions"`
}

type TaskTypeV struct {
	Name              string                     `json:"name"`
	Title             string                     `json:"title"`
	Builtin           bool                       `json:"builtin"`
	Participants      map[string]ParticipantDefV `json:"participants"`
	ParticipantOrder  []string                   `json:"participant_order"`
	TaskSchema        map[string]any             `json:"task_schema"`
	ResultSchema      map[string]any             `json:"result_schema"`
	AgentInstructions string                     `json:"agent_instructions"`
	Workflow          WorkflowDefV               `json:"workflow"`
}

func taskTypeView(tt *domain.TaskType, loc i18n.Locale) TaskTypeV {
	v := TaskTypeV{Name: tt.Name, Title: tt.Title.In(loc), Builtin: tt.BuiltIn, Participants: map[string]ParticipantDefV{}, ParticipantOrder: []string{}, TaskSchema: tt.TaskSchema, ResultSchema: tt.ResultSchema, AgentInstructions: tt.AgentInstructions,
		Workflow: WorkflowDefV{Version: tt.Workflow.Version, Initial: tt.Workflow.Initial, States: map[string]TaskState{}, StateOrder: []string{}, Transitions: []TransitionDefV{}}}
	for _, p := range tt.Participants {
		v.Participants[p.Slot] = ParticipantDefV{Title: p.Title.In(loc), Role: p.Role}
		v.ParticipantOrder = append(v.ParticipantOrder, p.Slot)
	}
	for _, s := range tt.Workflow.States {
		v.Workflow.States[s.Name] = TaskState{Name: s.Name, Title: s.Title.In(loc), Label: s.Label, Weight: s.Weight, Claimable: s.Claimable}
		v.Workflow.StateOrder = append(v.Workflow.StateOrder, s.Name)
	}
	for _, t := range tt.Workflow.Transitions {
		d := TransitionDefV{Name: t.Name, Title: t.Title.In(loc), From: t.From, To: t.To, By: t.By, Requires: orEmpty(t.Requires), Grant: t.Grant, TriggeredBy: t.TriggeredBy}
		if t.AssignTo != "" {
			a := t.AssignTo
			d.AssignTo = &a
		}
		v.Workflow.Transitions = append(v.Workflow.Transitions, d)
	}
	return v
}

// ---------- 待领取 ----------

type BacklogItemV struct {
	Task                 TaskV    `json:"task"`
	RequiredRole         *string  `json:"required_role"`
	RequiredCapabilities []string `json:"required_capabilities"`
	CanClaim             bool     `json:"can_claim"`
	Reasons              []string `json:"reasons"`
}

// ---------- 甘特图 ----------

type GanttTaskV struct {
	ID           string       `json:"id"`
	Number       int          `json:"number"`
	Title        string       `json:"title"`
	Type         string       `json:"type"`
	TypeTitle    string       `json:"type_title"`
	State        TaskState    `json:"state"`
	Assignee     *ExecutorRef `json:"assignee"`
	GoalID       *string      `json:"goal_id"`
	PlannedStart *string      `json:"planned_start"`
	PlannedEnd   *string      `json:"planned_end"`
	ActualStart  *string      `json:"actual_start"`
	ActualEnd    *string      `json:"actual_end"`
	Progress     int          `json:"progress"`
	Cost         float64      `json:"cost"`
	Overdue      bool         `json:"overdue"`
}

// GanttGoalV 是甘特图目标行的汇总：横条起止、截止刻度、进度，以及横条上的里程碑菱形（ADR 0016）。
type GanttGoalV struct {
	ID           string            `json:"id"`
	PlannedStart *string           `json:"planned_start"`
	PlannedEnd   *string           `json:"planned_end"`
	Deadline     *string           `json:"deadline"`
	Progress     int               `json:"progress"`
	Milestones   []GanttMilestoneV `json:"milestones"`
}

type GanttRowV struct {
	Key   string       `json:"key"`
	Title string       `json:"title"`
	Depth int          `json:"depth"`
	Goal  *GanttGoalV  `json:"goal"`
	Tasks []GanttTaskV `json:"tasks"`
}

type GanttDependencyV struct {
	FromTaskID string `json:"from_task_id"`
	ToTaskID   string `json:"to_task_id"`
}

type GanttDataV struct {
	Group        string             `json:"group"`
	From         string             `json:"from"`
	To           string             `json:"to"`
	Rows         []GanttRowV        `json:"rows"`
	Dependencies []GanttDependencyV `json:"dependencies"`
}

func ganttView(group string, rows []*app.GanttRow, r refs, from, to string) GanttDataV {
	out := GanttDataV{Group: group, From: from, To: to, Rows: []GanttRowV{}, Dependencies: []GanttDependencyV{}}
	seenDep := map[string]bool{}
	var walk func(row *app.GanttRow, depth int, prefix string)
	walk = func(row *app.GanttRow, depth int, prefix string) {
		key := row.Key
		if key == "" {
			key = "_"
		}
		v := GanttRowV{Key: key, Title: row.Title, Depth: depth, Tasks: []GanttTaskV{}}
		if row.Kind == "goal" && row.Key != "" {
			v.Goal = &GanttGoalV{ID: row.Key, PlannedStart: dateStr(row.Start), PlannedEnd: dateStr(row.End), Deadline: dateStr(row.Deadline), Progress: row.Progress, Milestones: ganttMilestones(row.Milestones)}
		}
		for _, t := range row.Tasks {
			v.Tasks = append(v.Tasks, GanttTaskV{ID: t.ID, Number: t.Number, Title: t.Title, Type: t.TypeName, TypeTitle: t.TypeTitle, State: TaskState{Name: t.State.Name, Title: t.State.Title, Label: t.State.Label}, Assignee: r.get(t.AssigneeID), GoalID: nullable(t.GoalID),
				PlannedStart: dateStr(t.PlannedStart), PlannedEnd: dateStr(t.PlannedEnd), ActualStart: dateStr(t.ActualStart), ActualEnd: dateStr(t.ActualEnd), Progress: t.Progress, Cost: t.Cost, Overdue: t.Overdue})
			for _, b := range t.Blockers {
				k := b + ">" + t.ID
				if !seenDep[k] {
					seenDep[k] = true
					out.Dependencies = append(out.Dependencies, GanttDependencyV{FromTaskID: b, ToTaskID: t.ID})
				}
			}
		}
		out.Rows = append(out.Rows, v)
		for _, c := range row.Children {
			walk(c, depth+1, prefix+row.Title+" › ")
		}
	}
	for _, row := range rows {
		walk(row, 0, "")
	}
	return out
}

// ---------- 待确认操作 ----------

type ProposalTargetV struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Title string `json:"title"`
}

type ProposalV struct {
	ID          string           `json:"id"`
	Agent       ExecutorRef      `json:"agent"`
	Owner       ExecutorRef      `json:"owner"`
	Action      string           `json:"action"`
	ActionTitle string           `json:"action_title"`
	Grant       *string          `json:"grant"`
	Target      *ProposalTargetV `json:"target"`
	Summary     string           `json:"summary"`
	Payload     map[string]any   `json:"payload"`
	Status      string           `json:"status"`
	StatusTitle string           `json:"status_title"`
	DecidedBy   *ExecutorRef     `json:"decided_by"`
	DecidedAt   *time.Time       `json:"decided_at"`
	Reason      *string          `json:"reason"`
	CanDecide   bool             `json:"can_decide"`
	CreatedAt   time.Time        `json:"created_at"`
	ExpiresAt   time.Time        `json:"expires_at"`
}

// proposalView 把应用层的待确认操作转成接口形状（名字与句子已按请求者语言渲染）。
func proposalView(p *app.ProposalView) ProposalV {
	v := ProposalV{
		ID:     p.ID,
		Agent:  ExecutorRef{ID: p.AgentID, Kind: string(domain.ExecutorAgent), Name: p.AgentName, OwnerID: p.OwnerID},
		Owner:  ExecutorRef{ID: p.OwnerID, Kind: string(domain.ExecutorMember), Name: p.OwnerName},
		Action: p.Action, ActionTitle: p.ActionTitle, Grant: nullable(string(p.Grant)),
		Summary: p.SummaryText, Payload: p.Payload, Status: string(p.Status), StatusTitle: p.StatusTitle,
		DecidedAt: p.DecidedAt, Reason: nullable(p.Reason), CanDecide: p.CanDecide,
		CreatedAt: p.CreatedAt, ExpiresAt: p.ExpiresAt,
	}
	if v.Payload == nil {
		v.Payload = map[string]any{}
	}
	if p.TargetKind != "" {
		v.Target = &ProposalTargetV{Kind: p.TargetKind, ID: p.TargetID, Title: p.TargetTitle}
	}
	if p.DecidedBy != "" {
		name := p.DecidedByName
		if name == "" {
			name = p.DecidedBy
		}
		v.DecidedBy = &ExecutorRef{ID: p.DecidedBy, Kind: string(domain.ExecutorMember), Name: name}
	}
	return v
}

// ---------- 动态 ----------

type EventV struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	TaskID     *string        `json:"task_id"`
	TaskTitle  *string        `json:"task_title"`
	TaskNumber *int           `json:"task_number,omitempty"`
	GoalID     *string        `json:"goal_id"`
	Actor      *ExecutorRef   `json:"actor"`
	Summary    string         `json:"summary"`
	Data       map[string]any `json:"data"`
	CreatedAt  time.Time      `json:"created_at"`
}

func eventView(e *store.EventRow, r refs, taskTitle, goalOf map[string]string, roles map[string]i18n.Text, loc i18n.Locale) EventV {
	v := EventV{ID: fmt.Sprint(e.ID), Kind: e.Type, TaskID: nullable(e.TaskID), Actor: r.get(e.ActorID), Data: e.Data, CreatedAt: e.At}
	if v.Data == nil {
		v.Data = map[string]any{}
	}
	if t, ok := taskTitle[e.TaskID]; ok {
		v.TaskTitle = &t
	}
	if g, ok := goalOf[e.TaskID]; ok && g != "" {
		v.GoalID = &g
	} else if g, ok := e.Data["goal_id"].(string); ok && g != "" {
		// 不挂在任务上的动态（目标本身、里程碑）把目标 ID 写在数据里；目标详情的动态面板按 goal_id 过滤
		v.GoalID = &g
	}
	v.Summary = eventSummary(e, r, taskTitle, roles, loc) + approvalNote(e, r, loc)
	return v
}

// textOf 把动态数据里的多语言字段（JSON 对象或字符串）解析成该语言文本。
func textOf(v any, loc i18n.Locale) string {
	switch x := v.(type) {
	case string:
		return x
	case i18n.Text:
		return x.In(loc)
	case map[string]any:
		t := i18n.Text{}
		for k, val := range x {
			if s, ok := val.(string); ok {
				if l := i18n.Normalize(k); l != "" {
					t[l] = s
				}
			}
		}
		return t.In(loc)
	}
	return ""
}

// externalWho 把「系统」换成外部操作者（如「GitHub 用户 alice」），动态里带了来源与登录名时用。
func externalWho(who string, e *store.EventRow, loc i18n.Locale) string {
	src := textOf(e.Data["external_source"], loc)
	name := textOf(e.Data["external_user_name"], loc)
	if e.ActorID != "" || src == "" || name == "" {
		return who
	}
	return i18n.Trf(loc, "external.actor", app.SourceTitle(src, loc), name)
}

func eventSummary(e *store.EventRow, r refs, taskTitle map[string]string, roles map[string]i18n.Text, loc i18n.Locale) string {
	who := i18n.Tr(loc, "ev.system")
	if a := r.get(e.ActorID); a != nil {
		who = a.Name
	}
	task := ""
	if t, ok := taskTitle[e.TaskID]; ok {
		task = "「" + t + "」"
		if loc == i18n.EnUS {
			task = "\"" + t + "\""
		}
	}
	s := func(k string) string { return textOf(e.Data[k], loc) }
	switch e.Type {
	case "TaskCreated":
		return i18n.Trf(loc, "ev.TaskCreated", who, task)
	case "TaskAssigned":
		name := s("to")
		if to := r.get(name); to != nil {
			name = to.Name
		}
		if via := s("via_slot"); via != "" {
			return i18n.Trf(loc, "ev.TaskAssignedVia", task, via, name)
		}
		return i18n.Trf(loc, "ev.TaskAssigned", who, task, name)
	case "TaskClaimed":
		return i18n.Trf(loc, "ev.TaskClaimed", who, task)
	case "TaskSentToBacklog":
		if role := s("required_role"); role != "" {
			return i18n.Trf(loc, "ev.TaskSentToBacklogR", task, roleTitle(roles, role, loc))
		}
		return i18n.Trf(loc, "ev.TaskSentToBacklog", task)
	case "TaskTransitioned":
		to := s("to_title")
		if to == "" {
			to = s("to")
		}
		return i18n.Trf(loc, "ev.TaskTransitioned", who, s("title"), task, to)
	case "RunStarted":
		return i18n.Trf(loc, "ev.RunStarted", who, task)
	case "RunEnded":
		return i18n.Trf(loc, "ev.RunEnded", who, task, i18n.Tr(loc, "outcome."+s("outcome")))
	case "UsageReported":
		return i18n.Trf(loc, "ev.UsageReported", who, task)
	case "ArtifactAttached":
		return i18n.Trf(loc, "ev.ArtifactAttached", who, task, s("title"))
	case "CommentAdded":
		return i18n.Trf(loc, "ev.CommentAdded", who, task, s("text"))
	case "NoteAdded":
		return i18n.Trf(loc, "ev.NoteAdded", who, task)
	case "TasksLinked":
		return i18n.Trf(loc, "ev.TasksLinked", who, task)
	case "RelationRemoved":
		return i18n.Trf(loc, "ev.RelationRemoved", task)
	case "GoalCreated":
		return i18n.Trf(loc, "ev.GoalCreated", who, s("title"))
	case "GoalUpdated":
		return i18n.Trf(loc, "ev.GoalUpdated", who)
	case "GoalFieldChanged":
		return fieldChangeSummary(e, r, loc, who, "goal", i18n.Trf(loc, "ev.obj.goal", s("title")))
	case "TaskFieldChanged":
		return fieldChangeSummary(e, r, loc, who, "task", i18n.Trf(loc, "ev.obj.task", task))
	case "MilestoneCreated", "MilestoneUpdated", "MilestoneReached", "MilestoneUnreached", "MilestoneDeleted":
		var due any = s("due_on")
		if t, err := time.ParseInLocation("2006-01-02", s("due_on"), time.Local); err == nil {
			due = i18n.Date(t)
		}
		return i18n.Trf(loc, "ev."+e.Type, who, s("goal_title"), s("title"), due)
	case "AgentRegistered":
		return i18n.Trf(loc, "ev.AgentRegistered", who, s("name"))
	case "AgentConnected":
		return i18n.Trf(loc, "ev.AgentConnected", who, s("name"), i18n.Tr(loc, "device.client."+s("client")))
	case "PreferencesUpdated":
		cleared, _ := e.Data["cleared"].(bool)
		var fields []string
		if keys, ok := e.Data["fields"].([]any); ok {
			for _, k := range keys {
				fields = append(fields, i18n.Tr(loc, "pref.field."+textOf(k, loc)))
			}
		}
		if s("target") == "role" {
			role := roleTitle(roles, s("role"), loc)
			if cleared {
				return i18n.Trf(loc, "ev.PreferencesRoleCleared", who, role)
			}
			return i18n.Trf(loc, "ev.PreferencesRole", who, role, fields)
		}
		if cleared {
			return i18n.Trf(loc, "ev.PreferencesPersonalCleared", who)
		}
		return i18n.Trf(loc, "ev.PreferencesPersonal", who, fields)
	case "AgentRemoved", "AgentRevoked":
		return i18n.Trf(loc, "ev.AgentRemoved", who)
	case "AgentUpdated":
		return i18n.Trf(loc, "ev.AgentUpdated", who)
	case "TaskUpdated":
		return i18n.Trf(loc, "ev.TaskUpdated", who, task)
	case "TaskTypeSaved":
		return i18n.Trf(loc, "ev.TaskTypeSaved", who, s("name"))
	case "MemberInvited":
		return i18n.Trf(loc, "ev.MemberInvited", who, s("email"))
	case "InvitationRevoked":
		return i18n.Trf(loc, "ev.InvitationRevoked", who, s("email"))
	case "OrgSettingsUpdated":
		return i18n.Trf(loc, "ev.OrgSettingsUpdated", who, i18n.Tr(loc, "visibility."+s("collaboration_visibility")), i18n.Tr(loc, "visibility."+s("finance_visibility")))
	case "WorkspaceLayoutUpdated":
		cleared, _ := e.Data["cleared"].(bool)
		if s("target") != "role" {
			if cleared {
				return i18n.Trf(loc, "ev.WorkspacePersonalCleared", who)
			}
			return i18n.Trf(loc, "ev.WorkspacePersonal", who)
		}
		role := roleTitle(roles, s("role"), loc)
		if cleared {
			return i18n.Trf(loc, "ev.WorkspaceRoleCleared", who, role)
		}
		if p, ok := domain.PresetByKey(s("preset")); ok {
			return i18n.Trf(loc, "ev.WorkspaceRolePreset", who, role, p.Title)
		}
		var titles []i18n.Text
		if keys, ok := e.Data["blocks"].([]any); ok {
			for _, k := range keys {
				if b, ok := domain.BlockByKey(textOf(k, loc)); ok {
					titles = append(titles, b.Title)
				}
			}
		}
		return i18n.Trf(loc, "ev.WorkspaceRoleBlocks", who, role, titles)
	case "GoalTypeCreated", "GoalTypeUpdated", "GoalTypeDeactivated", "GoalTypeReactivated", "GoalTypeDeleted":
		// 目标类型（ADR 0023）：句子里念的是动态里记下的那个名字，之后改名不影响历史
		return i18n.Trf(loc, "ev."+e.Type, who, s("name"))
	case "GoalTypeRenamed":
		return i18n.Trf(loc, "ev.GoalTypeRenamed", who, s("from"), s("name"))
	case "TeamCreated":
		return i18n.Trf(loc, "ev.TeamCreated", who, s("name"))
	case "TeamUpdated":
		return i18n.Trf(loc, "ev.TeamUpdated", who, s("name"))
	case "TeamDeleted":
		return i18n.Trf(loc, "ev.TeamDeleted", who)
	case "TeamBoundaryChanged":
		if on, _ := e.Data["is_boundary"].(bool); on {
			return i18n.Trf(loc, "ev.TeamBoundaryOn", who, s("name"))
		}
		return i18n.Trf(loc, "ev.TeamBoundaryOff", who, s("name"))
	case "MemberJoined":
		return i18n.Trf(loc, "ev.MemberJoined", who)
	case "TeamDeactivated":
		if manual, _ := e.Data["manual"].(bool); manual {
			return i18n.Trf(loc, "ev.TeamDeactivatedManual", who, s("name"))
		}
		return i18n.Trf(loc, "ev.TeamDeactivated", who, s("name"))
	case "TeamMoved":
		if s("parent_id") == "" {
			return i18n.Trf(loc, "ev.TeamMovedTop", who, s("name"))
		}
		return i18n.Trf(loc, "ev.TeamMoved", who, s("name"), s("parent_name"))
	case "MemberSynced", "MemberUpdated", "MemberDeactivated", "MemberActivated", "MemberReactivated", "MemberImported", "MemberTeamCleared", "TeamReactivated":
		return i18n.Trf(loc, "ev."+e.Type, who, s("name"))
	case "MemberRenamed":
		return i18n.Trf(loc, "ev.MemberRenamed", who, s("from"), s("name"))
	case "MemberRolesChanged":
		var titles []string
		if names, ok := e.Data["roles"].([]any); ok {
			for _, n := range names {
				titles = append(titles, roleTitle(roles, textOf(n, loc), loc))
			}
		}
		if len(titles) == 0 {
			return i18n.Trf(loc, "ev.MemberRolesCleared", who, s("name"))
		}
		return i18n.Trf(loc, "ev.MemberRolesChanged", who, s("name"), titles)
	case "MemberTeamChanged":
		if s("mode") == "add" {
			return i18n.Trf(loc, "ev.MemberTeamAdded", who, s("name"), s("team_name"))
		}
		return i18n.Trf(loc, "ev.MemberTeamChanged", who, s("name"), s("team_name"))
	case "TeamMerged", "MemberMerged":
		return i18n.Trf(loc, "ev."+e.Type, who, s("name"), s("into_name"))
	case "DirectoryDecided":
		ext := s("external_name")
		if ext == "" {
			ext = s("external_id")
		}
		switch s("decision") {
		case "merge":
			return i18n.Trf(loc, "ev.DirectoryDecidedMerge", who, ext, s("local_name"))
		case "create":
			return i18n.Trf(loc, "ev.DirectoryDecidedCreate", who, ext)
		case "skip":
			return i18n.Trf(loc, "ev.DirectoryDecidedSkip", who, ext)
		}
		return i18n.Trf(loc, "ev.DirectoryDecidedReconsider", who, ext)
	case "DirectoryUnbound":
		return i18n.Trf(loc, "ev.DirectoryUnbound", who, s("name"))
	case "DirectoryConfigured":
		if names := fieldTitles(s("provider"), e.Data["fields"], loc); names != "" {
			return i18n.Trf(loc, "ev.DirectoryFieldsChanged", who, app.SourceTitle(s("provider"), loc), names)
		}
		return i18n.Trf(loc, "ev.DirectoryConfigured", who, app.SourceTitle(s("provider"), loc))
	// 代码平台与外部事件（ADR 0020）
	case "CodePlatformDisconnected":
		return i18n.Trf(loc, "ev.CodePlatformDisconnected", who, app.SourceTitle(s("provider"), loc))
	case "DirectoryDisconnected":
		return i18n.Trf(loc, "ev.DirectoryDisconnected", who, app.SourceTitle(s("provider"), loc))
	case "CodePlatformConfigured":
		fresh, _ := e.Data["fresh"].(bool)
		switched, _ := e.Data["provider_switched"].(bool)
		if names := fieldTitles(s("provider"), e.Data["fields"], loc); names != "" && !fresh && !switched {
			return i18n.Trf(loc, "ev.CodePlatformFieldsChanged", who, app.SourceTitle(s("provider"), loc), names)
		}
		return i18n.Trf(loc, "ev.CodePlatformConfigured", who, app.SourceTitle(s("provider"), loc))
	case "CodeWebhookSecretRotated", "CodeWebhookSecretRevealed":
		return i18n.Trf(loc, "ev."+e.Type, who, app.SourceTitle(s("provider"), loc))
	case "CodeIdentityBound":
		if login := s("login"); login != "" {
			return i18n.Trf(loc, "ev.CodeIdentityBound", who, app.SourceTitle(s("provider"), loc), login)
		}
		return i18n.Trf(loc, "ev.CodeIdentityUnbound", who, app.SourceTitle(s("provider"), loc))
	case "ExternalLinkAdded":
		return i18n.Trf(loc, "ev.ExternalLinkAdded", externalWho(who, e, loc), task, s("kind_title"), s("title"))
	case "ExternalLinkUpdated":
		return i18n.Trf(loc, "ev.ExternalLinkUpdated", task, s("kind_title"), s("title"), s("status_title"))
	case "ExternalLinkRemoved":
		return i18n.Trf(loc, "ev.ExternalLinkRemoved", who, task, s("title"))
	case "ExternalEventApplied":
		return i18n.Trf(loc, "ev.ExternalEventApplied", app.SourceTitle(s("external_source"), loc), s("external_ref"),
			i18n.Tr(loc, "external.verb."+s("external_event")), task, s("to_title"))
	case "ExternalEventIgnored":
		return i18n.Trf(loc, "ev.ExternalEventIgnored", app.SourceTitle(s("external_source"), loc), s("external_ref"),
			i18n.Tr(loc, "external.verb."+s("external_event")), task, s("reason"))
	case "NotificationChannelConfigured":
		title := s("channel")
		if p, ok := directory.Lookup(s("channel")); ok {
			title = p.Title.In(loc)
		}
		if enabled, _ := e.Data["enabled"].(bool); !enabled {
			return i18n.Trf(loc, "ev.NotificationChannelDisabled", who, title)
		}
		return i18n.Trf(loc, "ev.NotificationChannelConfigured", who, title)
	case "NotificationPolicyChanged":
		var titles []i18n.Text
		if keys, ok := e.Data["allowed_kinds"].([]any); ok {
			for _, k := range keys {
				if t, ok := domain.NotifyKindTitle[textOf(k, loc)]; ok {
					titles = append(titles, t)
				}
			}
		}
		if len(titles) == 0 {
			return i18n.Trf(loc, "ev.NotificationPolicyNone", who)
		}
		return i18n.Trf(loc, "ev.NotificationPolicyChanged", who, titles)
	case "NotificationPreferencesUpdated":
		if cleared, _ := e.Data["cleared"].(bool); cleared {
			return i18n.Trf(loc, "ev.NotificationPreferencesCleared", who)
		}
		return i18n.Trf(loc, "ev.NotificationPreferencesUpdated", who)
	case "DirectoryDetached":
		return i18n.Trf(loc, "ev.DirectoryDetached", who, s("name"), app.SourceTitle(s("old_provider"), loc))
	case "DirectorySyncRan":
		if s("status") == "failed" {
			return i18n.Trf(loc, "ev.DirectorySyncFailed", s("error"))
		}
		n := func(k string) int { v, _ := numOf(e.Data[k]); return v }
		out := i18n.Trf(loc, "ev.DirectorySyncRan", n("added_teams"), n("added_members"), n("updated_teams"), n("updated_members"), n("deactivated_teams"), n("deactivated_members"))
		if k := n("errors"); k > 0 {
			out += i18n.Trf(loc, "ev.DirectorySyncErrors", k)
		}
		return out
	case "SprintCreated", "SprintUpdated", "SprintStarted":
		return i18n.Trf(loc, "ev."+e.Type, who, s("name"))
	case "SprintClosed":
		moved, _ := numOf(e.Data["moved"])
		returned, _ := numOf(e.Data["returned"])
		return i18n.Trf(loc, "ev.SprintClosed", who, s("name"), moved, returned)
	case "TaskAddedToSprint", "TaskRemovedFromSprint":
		return i18n.Trf(loc, "ev."+e.Type, who, task, s("name"))
	case "PointsChanged":
		if p, ok := numOf(e.Data["points"]); ok {
			return i18n.Trf(loc, "ev.PointsChanged", who, task, p)
		}
		return i18n.Trf(loc, "ev.PointsCleared", who, task)
	case "ProposalCreated":
		return i18n.Trf(loc, "ev.ProposalCreated", who, proposalSummary(s("summary")))
	case "ProposalApproved", "ProposalRejected":
		agent := s("agent_id")
		if a := r.get(agent); a != nil {
			agent = a.Name
		}
		if e.Type == "ProposalRejected" {
			return i18n.Trf(loc, "ev.ProposalRejected", who, agent, proposalSummary(s("summary")), s("reason"))
		}
		return i18n.Trf(loc, "ev.ProposalApproved", who, agent, proposalSummary(s("summary")))
	case "ProposalExpired":
		return i18n.Trf(loc, "ev.ProposalExpired", proposalSummary(s("summary")))
	}
	return i18n.Trf(loc, "ev.generic", who, e.Type)
}

// fieldChangeSummary 把一条就地编辑动态（GoalFieldChanged / TaskFieldChanged）渲染成一句话：
// 「某人 把目标「X」的负责人从「李明」改成了「王芳」」。数据里 field 是字段名，from / to 是旧值新值，
// 引用类字段另带 from_title / to_title（当时的名字）。kind 是 goal | task，object 是「目标「X」」/「任务「X」」。
func fieldChangeSummary(e *store.EventRow, r refs, loc i18n.Locale, who, kind, object string) string {
	d := e.Data
	field, _ := d["field"].(string)
	str := func(v any) string {
		switch x := v.(type) {
		case nil:
			return ""
		case string:
			return x
		case float64:
			return i18n.Number(x)
		case bool:
			if x {
				return "true"
			}
			return "false"
		}
		return textOf(v, loc)
	}
	// 引用类字段的名字：优先动态里记下的名字，其次现在的执行者索引，最后原 ID
	name := func(idKey, titleKey string) string {
		if v, ok := d[titleKey]; ok && v != nil {
			switch x := v.(type) {
			case []any:
				parts := make([]string, 0, len(x))
				for _, item := range x {
					parts = append(parts, textOf(item, loc))
				}
				return strings.Join(parts, i18n.Tr(loc, "sep.list"))
			default:
				if t := textOf(x, loc); t != "" {
					return t
				}
			}
		}
		id := str(d[idKey])
		if a := r.get(id); a != nil {
			return a.Name
		}
		return id
	}
	date := func(v any) any {
		if t, err := time.ParseInLocation("2006-01-02", str(v), time.Local); err == nil {
			return i18n.Date(t)
		}
		return str(v)
	}
	label := i18n.Tr(loc, "field."+field)
	from, to := d["from"], d["to"]
	// 空列表（所需能力清空）等同于「没有」
	if arr, ok := from.([]any); ok && len(arr) == 0 {
		from = nil
	}
	if arr, ok := to.([]any); ok && len(arr) == 0 {
		to = nil
	}
	// 值的呈现：quoted 为真时套引号（名字、文字），否则裸写（日期、数字、金额）
	var fromS, toS string
	quoted := true
	switch field {
	case "title":
		return i18n.Trf(loc, "ev.fc.title", who, i18n.Tr(loc, "ev.kind."+kind), str(from), str(to))
	case "description":
		return i18n.Trf(loc, "ev.fc.description", who, object)
	case "parent_id":
		fromN, toN := name("from", "from_title"), name("to", "to_title")
		switch {
		case to == nil || toN == "":
			return i18n.Trf(loc, "ev.fc.parent_top", who, object)
		case from == nil || fromN == "":
			return i18n.Trf(loc, "ev.fc.parent_set", who, object, toN)
		default:
			return i18n.Trf(loc, "ev.fc.parent_moved", who, object, fromN, toN)
		}
	case "human_only":
		if on, _ := to.(bool); on {
			return i18n.Trf(loc, "ev.fc.human_only_on", who, object)
		}
		return i18n.Trf(loc, "ev.fc.human_only_off", who, object)
	case "status":
		switch {
		case str(to) == string(domain.GoalAchieved):
			return i18n.Trf(loc, "ev.fc.status.achieved", who, object)
		case str(to) == string(domain.GoalAbandoned):
			return i18n.Trf(loc, "ev.fc.status.abandoned", who, object)
		case str(to) == string(domain.GoalActive) && str(from) == string(domain.GoalAbandoned):
			return i18n.Trf(loc, "ev.fc.status.resumed", who, object)
		case str(to) == string(domain.GoalActive) && str(from) == string(domain.GoalAchieved):
			return i18n.Trf(loc, "ev.fc.status.unachieved", who, object)
		}
		fromS, toS = i18n.Tr(loc, "goal.status."+str(from)), i18n.Tr(loc, "goal.status."+str(to))
	case "owner_member_id", "team_id", "type_id", "reviewer_id", "goal_id", "required_capabilities", "participants":
		fromS, toS = name("from", "from_title"), name("to", "to_title")
		if field == "participants" {
			label = textOf(d["label"], loc)
		}
	case "fields":
		label = i18n.Trf(loc, "ev.fc.custom_label", str(d["key"]))
		fromS, toS = str(from), str(to)
	case "priority":
		fromS, toS = i18n.Tr(loc, "priority."+str(from)), i18n.Tr(loc, "priority."+str(to))
	case "horizon", "confidence":
		// 时间桶与信心度只存取值，名字按当前语言现渲染（ADR 0021）
		fromS, toS = enumTitle(loc, field, str(from)), enumTitle(loc, field, str(to))
	case "date_precision":
		// 时间粒度的空值是「到周」而不是「没有」（ADR 0022）：从没填过或改回最细时也要念出粒度，
		// 不能说成「清空了时间粒度」
		prec := func(v any) string {
			return enumTitle(loc, field, string(domain.EffectiveDatePrecision(domain.DatePrecision(str(v)))))
		}
		fromS, toS = prec(from), prec(to)
	case "outcome":
		// 成果指标是一句话，写全新的那句就够了，不用把旧句子也念一遍
		if to == nil || str(to) == "" {
			return i18n.Trf(loc, "ev.fc.cleared", who, object, label)
		}
		return i18n.Trf(loc, "ev.fc.rewrote_q", who, object, label, str(to))
	case "deadline", "planned_start", "planned_end":
		quoted = false
		fromS, toS = i18n.Arg(loc, date(from)).(string), i18n.Arg(loc, date(to)).(string)
	case "budget":
		quoted = false
		cur := str(d["currency"])
		if f, ok := from.(float64); ok {
			fromS = i18n.Money{Amount: f, Currency: cur}.Render(loc)
		}
		if f, ok := to.(float64); ok {
			toS = i18n.Money{Amount: f, Currency: cur}.Render(loc)
		}
	case "estimate_hours":
		quoted = false
		if f, ok := from.(float64); ok {
			fromS = i18n.Trf(loc, "unit.hours", i18n.Number(f))
		}
		if f, ok := to.(float64); ok {
			toS = i18n.Trf(loc, "unit.hours", i18n.Number(f))
		}
	case "progress_override":
		quoted = false
		if f, ok := from.(float64); ok {
			fromS = i18n.Number(f) + "%"
		}
		if f, ok := to.(float64); ok {
			toS = i18n.Number(f) + "%"
		}
	default:
		fromS, toS = str(from), str(to)
	}
	suffix := ""
	if quoted {
		suffix = "_q"
	}
	switch {
	case toS == "" && to == nil:
		return i18n.Trf(loc, "ev.fc.cleared", who, object, label)
	case fromS == "" && from == nil:
		return i18n.Trf(loc, "ev.fc.set"+suffix, who, object, label, toS)
	}
	return i18n.Trf(loc, "ev.fc.changed"+suffix, who, object, label, fromS, toS)
}

// approvalNote 是被确认后执行的动态尾巴：「经<确认人>确认」。
func approvalNote(e *store.EventRow, r refs, loc i18n.Locale) string {
	name, _ := e.Data["approved_by_name"].(string)
	if id, _ := e.Data["approved_by"].(string); id != "" {
		if a := r.get(id); a != nil && a.Name != "" && a.Name != id {
			name = a.Name
		}
	}
	if name == "" {
		return ""
	}
	return i18n.Trf(loc, "ev.via_approval", name)
}

// numOf 把动态数据里的数字（JSON 解出来是 float64）转成 int。
func numOf(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		return int(x), true
	case int:
		return x, true
	case int64:
		return int(x), true
	}
	return 0, false
}

// ---------- 统计 ----------

type CostStatV struct {
	Key         string  `json:"key"`
	Title       string  `json:"title"`
	Cost        float64 `json:"cost"`
	TotalTokens int64   `json:"total_tokens"`
	RunCount    int     `json:"run_count"`
}

type CycleStatV struct {
	Type           string  `json:"type"`
	TypeTitle      string  `json:"type_title"`
	Sample         int     `json:"sample"`
	AvgHours       float64 `json:"avg_hours"`
	MedianHours    float64 `json:"median_hours"`
	AvgActiveHours float64 `json:"avg_active_hours"`
}

type ThroughputStatV struct {
	Week       string `json:"week"`
	Created    int    `json:"created"`
	Done       int    `json:"done"`
	Terminated int    `json:"terminated"`
}

type AgentStatV struct {
	Agent       ExecutorRef `json:"agent"`
	Owner       ExecutorRef `json:"owner"`
	Runs        int         `json:"runs"`
	Accepted    int         `json:"accepted"`
	Rejected    int         `json:"rejected"`
	SuccessRate float64     `json:"success_rate"`
	RejectRate  float64     `json:"reject_rate"`
	TotalTokens int64       `json:"total_tokens"`
	Cost        float64     `json:"cost"`
}

// ---------- 错误 ----------

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func errBody(status int, msg string) errorBody {
	var b errorBody
	b.Error.Message = msg
	b.Error.Code = map[int]string{400: "bad_request", 401: "unauthenticated", 403: "forbidden", 404: "not_found", 409: "rejected", 429: "slow_down", 500: "internal"}[status]
	if b.Error.Code == "" {
		b.Error.Code = strings.ToLower(fmt.Sprint(status))
	}
	return b
}

// ---------- 异常与负荷（ADR 0013） ----------

// ExceptionTaskV 是异常列表里的任务。
type ExceptionTaskV struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Type        string     `json:"type"`
	TypeTitle   string     `json:"type_title"`
	State       TaskState  `json:"state"`
	Assignee    *string    `json:"assignee"`
	AssigneeID  *string    `json:"assignee_id"`
	Team        string     `json:"team"`
	TeamID      *string    `json:"team_id"`
	GoalID      *string    `json:"goal_id"`
	GoalTitle   *string    `json:"goal_title"`
	Priority    string     `json:"priority"`
	PlannedEnd  *time.Time `json:"planned_end"`
	DaysOverdue int        `json:"days_overdue"`
	URL         string     `json:"url"`
}

// ExceptionGoalV 是异常列表里的目标。
type ExceptionGoalV struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Owner       *string    `json:"owner"`
	OwnerID     *string    `json:"owner_id"`
	Team        string     `json:"team"`
	TeamID      *string    `json:"team_id"`
	Status      string     `json:"status"`
	StatusTitle string     `json:"status_title"`
	Progress    int        `json:"progress"`
	Deadline    *time.Time `json:"deadline"`
	DaysOverdue int        `json:"days_overdue"`
	Budget      *float64   `json:"budget"`
	Cost        *float64   `json:"cost"`
	OverPct     *float64   `json:"over_pct"`
	URL         string     `json:"url"`
}

// StuckTaskV 是卡在同一个状态太久的任务。
type StuckTaskV struct {
	Task        ExceptionTaskV `json:"task"`
	DaysInState int            `json:"days_in_state"`
}

// ExceptionsV 是 /stats/exceptions 的形状。
type ExceptionsV struct {
	Scope            string           `json:"scope"`
	ScopeTitle       string           `json:"scope_title"`
	OverdueTasks     []ExceptionTaskV `json:"overdue_tasks"`
	OverdueGoals     []ExceptionGoalV `json:"overdue_goals"`
	OverBudgetGoals  []ExceptionGoalV `json:"over_budget_goals"`
	StuckTasks       []StuckTaskV     `json:"stuck_tasks"`
	PendingProposals []ProposalV      `json:"pending_proposals"`
}

func exceptionTaskView(x app.ExceptionTask) ExceptionTaskV {
	return ExceptionTaskV{ID: x.ID, Title: x.Title, Type: x.TypeName, TypeTitle: x.TypeTitle,
		State:    TaskState{Name: x.State.Name, Title: x.State.Title, Label: x.State.Label},
		Assignee: nullable(x.Assignee), AssigneeID: nullable(x.AssigneeID), Team: x.Team, TeamID: nullable(x.TeamID),
		GoalID: nullable(x.GoalID), GoalTitle: nullable(x.GoalTitle), Priority: priorityName(x.Priority),
		PlannedEnd: x.PlannedEnd, DaysOverdue: x.DaysOverdue, URL: "/tasks/" + x.ID + "/"}
}

func exceptionGoalView(x app.ExceptionGoal) ExceptionGoalV {
	return ExceptionGoalV{ID: x.ID, Title: x.Title, Owner: nullable(x.Owner), OwnerID: nullable(x.OwnerID),
		Team: x.Team, TeamID: nullable(x.TeamID), Status: x.Status, StatusTitle: x.StatusTitle, Progress: x.Progress,
		Deadline: x.Deadline, DaysOverdue: x.DaysOverdue, Budget: x.Budget, Cost: x.Cost, OverPct: x.OverPct,
		URL: "/goals/" + x.ID + "/"}
}

func exceptionsView(v *app.ExceptionsView) ExceptionsV {
	out := ExceptionsV{Scope: v.Scope, ScopeTitle: v.ScopeTitle, OverdueTasks: []ExceptionTaskV{}, OverdueGoals: []ExceptionGoalV{},
		OverBudgetGoals: []ExceptionGoalV{}, StuckTasks: []StuckTaskV{}, PendingProposals: []ProposalV{}}
	for _, x := range v.OverdueTasks {
		out.OverdueTasks = append(out.OverdueTasks, exceptionTaskView(x))
	}
	for _, x := range v.OverdueGoals {
		out.OverdueGoals = append(out.OverdueGoals, exceptionGoalView(x))
	}
	for _, x := range v.OverBudgetGoals {
		out.OverBudgetGoals = append(out.OverBudgetGoals, exceptionGoalView(x))
	}
	for _, x := range v.StuckTasks {
		out.StuckTasks = append(out.StuckTasks, StuckTaskV{Task: exceptionTaskView(x.Task), DaysInState: x.DaysInState})
	}
	for _, p := range v.PendingProposals {
		out.PendingProposals = append(out.PendingProposals, proposalView(p))
	}
	return out
}

// LoadV 是 /stats/load 里的一行。
type LoadV struct {
	Executor             ExecutorRef `json:"executor"`
	Team                 string      `json:"team"`
	TeamID               *string     `json:"team_id"`
	OpenTasks            int         `json:"open_tasks"`
	ActiveTasks          int         `json:"active_tasks"`
	PointsOpen           int         `json:"points_open"`
	PlannedHoursThisWeek float64     `json:"planned_hours_this_week"`
	Overdue              int         `json:"overdue"`
	CapacityHint         string      `json:"capacity_hint"`
	ActiveRuns           int         `json:"active_runs"`
	MaxConcurrent        *int        `json:"max_concurrent"`
	// Online 是旧口径，只为兼容保留；对外的状态看 State（CONTEXT.md「Agent 状态」）。
	Online     *bool  `json:"online"`
	State      string `json:"state,omitempty"`
	StateTitle string `json:"state_title,omitempty"`
}

func loadView(x app.LoadRow) LoadV {
	return LoadV{Executor: ExecutorRef{ID: x.ExecutorID, Kind: x.Kind, Name: x.Name}, Team: x.Team, TeamID: nullable(x.TeamID),
		OpenTasks: x.OpenTasks, ActiveTasks: x.ActiveTasks, PointsOpen: x.PointsOpen, PlannedHoursThisWeek: x.PlannedHoursThisWeek,
		Overdue: x.Overdue, CapacityHint: x.CapacityHint, ActiveRuns: x.ActiveRuns, MaxConcurrent: x.MaxConcurrent, Online: x.Online,
		State: x.State, StateTitle: x.StateTitle}
}

// proposalSummary 去掉待确认操作摘要句末的句号：摘要本身是一句完整的话，嵌进动态句子里时由模板负责标点，避免出现「。。」。
func proposalSummary(sum string) string {
	return strings.TrimRight(strings.TrimSpace(sum), "。.")
}
