package domain

import (
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

// Account 是登录身份。
type Account struct {
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	Name          string    `json:"name"`
	Locale        string    `json:"locale"`
	PlatformAdmin bool      `json:"platform_admin"`
	CreatedAt     time.Time `json:"created_at"`
}

// Role 是组织自定义角色。
type Role struct {
	Name        string    `json:"name"`
	Title       i18n.Text `json:"title"`
	Permissions []string  `json:"permissions"`
	BuiltIn     bool      `json:"builtin"`
}

// Invitation 是加入组织的邀请。
type Invitation struct {
	ID        string   `json:"id"`
	OrgID     string   `json:"org_id"`
	Email     string   `json:"email"`
	Name      string   `json:"name"`
	Roles     []string `json:"roles"`
	InvitedBy string   `json:"invited_by"`
	// TeamID 是邀请时指定要加入的团队；MemberID 是这条邀请对应的待激活成员（手工邀请 / 导入 / 同步预建）。
	TeamID     string     `json:"team_id,omitempty"`
	MemberID   string     `json:"member_id,omitempty"`
	ExpiresAt  time.Time  `json:"expires_at"`
	AcceptedAt *time.Time `json:"accepted_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Visibility 是一条可见性策略的取值（ADR 0013）。这两项策略是组织自己的管理方式，
// 代码里只有默认值，不写死任何一家公司的选择。
type Visibility string

const (
	// VisibilityOrg 全员可见。
	VisibilityOrg Visibility = "org"
	// VisibilityBoundary 按共享边界：可见域是自己所在团队向上最近的边界团队的整棵子树。
	VisibilityBoundary Visibility = "boundary"
	// VisibilityTeamTree 只看自己所属团队及其全部下级团队。
	VisibilityTeamTree Visibility = "team_tree"
)

// DefaultCollaborationVisibility 是协作数据（目标、任务、看板、甘特图、迭代、动态）的默认策略。
const DefaultCollaborationVisibility = VisibilityOrg

// DefaultFinanceVisibility 是财务数据（成本、用量、预算、Agent 成本）的默认策略。
const DefaultFinanceVisibility = VisibilityTeamTree

// Visibilities 是两项策略共同的候选取值。boundary 是另外两者的推广：不标记任何边界
// 等价于 org，每个团队都标记等价于 team_tree。
var Visibilities = []Visibility{VisibilityOrg, VisibilityBoundary, VisibilityTeamTree}

// CollaborationVisibilities 是协作数据可选的策略。
var CollaborationVisibilities = Visibilities

// FinanceVisibilities 是财务数据可选的策略。
var FinanceVisibilities = Visibilities

// ValidVisibility 判断某项策略的取值是否在候选里。
func ValidVisibility(v Visibility, among []Visibility) bool {
	for _, x := range among {
		if x == v {
			return true
		}
	}
	return false
}

// Organization 是组织。
type Organization struct {
	ID            string     `json:"id"`
	Slug          string     `json:"slug"`
	Name          string     `json:"name"`
	OwnerMemberID string     `json:"owner_member_id"`
	Currency      string     `json:"currency"`
	DefaultLocale string     `json:"default_locale"`
	CreatedAt     time.Time  `json:"created_at"`
	DeactivatedAt *time.Time `json:"deactivated_at,omitempty"`

	// 可见性策略（ADR 0013）。空值按默认值解释，老组织升级不会报错。
	CollaborationVisibility Visibility `json:"collaboration_visibility"`
	FinanceVisibility       Visibility `json:"finance_visibility"`
}

// Collaboration 返回协作数据策略（空值回落到默认）。
func (o *Organization) Collaboration() Visibility {
	if ValidVisibility(o.CollaborationVisibility, CollaborationVisibilities) {
		return o.CollaborationVisibility
	}
	return DefaultCollaborationVisibility
}

// Finance 返回财务数据策略（空值回落到默认）。
func (o *Organization) Finance() Visibility {
	if ValidVisibility(o.FinanceVisibility, FinanceVisibilities) {
		return o.FinanceVisibility
	}
	return DefaultFinanceVisibility
}

// 成员与团队的来源（ADR 0017）：手工建的（manual），或从外部目录同步进来的——此时存提供方代码名（feishu、wecom…）。
const SourceManual = "manual"

// 成员状态（ADR 0017）。已停用仍以 Active 布尔为准，Status 是派生给界面看的。
const (
	MemberActive            = "active"
	MemberPendingActivation = "pending_activation"
	MemberInactive          = "inactive"
)

// Member 是组织成员。
type Member struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	AccountID string    `json:"account_id"`
	Name      string    `json:"name"`
	Roles     []string  `json:"roles"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	// Source 是来源：manual 或提供方代码名（ADR 0017）。
	Source string `json:"source"`
	// Status 是激活状态：active | pending_activation | inactive。停用以 Active 为准，这里派生。
	Status string `json:"status"`
}

// DerivedStatus 按 Active 与激活状态算出对外的状态。
func (m *Member) DerivedStatus() string {
	if !m.Active {
		return MemberInactive
	}
	if m.Status == MemberPendingActivation {
		return MemberPendingActivation
	}
	return MemberActive
}

// Team 是团队。
type Team struct {
	ID           string `json:"id"`
	OrgID        string `json:"org_id"`
	ParentID     string `json:"parent_id,omitempty"`
	Name         string `json:"name"`
	LeadMemberID string `json:"lead_member_id,omitempty"`
	// IsBoundary 标记这个团队是共享边界：它的整棵子树内部互相可见，外面看不进来（ADR 0013）。
	IsBoundary bool `json:"is_boundary"`
	// Source 是来源：manual 或提供方代码名；ExternalName 是外部目录里的原名（ADR 0017）。
	Source       string `json:"source"`
	ExternalName string `json:"external_name,omitempty"`
	// Inactive 为真表示这个团队在外部目录里已不存在：不删，历史归口不变。
	Inactive bool `json:"inactive"`
}

// Agent 是注册到组织里的 Agent。
type Agent struct {
	ID            string              `json:"id"`
	OrgID         string              `json:"org_id"`
	OwnerMemberID string              `json:"owner_member_id"`
	Name          string              `json:"name"`
	Runtime       string              `json:"runtime"`
	Capabilities  []string            `json:"capabilities"`
	Grants        map[Grant]GrantMode `json:"grants"`
	MaxConcurrent int                 `json:"max_concurrent"`
	Shared        bool                `json:"shared"`
	LastSeenAt    *time.Time          `json:"last_seen_at,omitempty"`
	RevokedAt     *time.Time          `json:"revoked_at,omitempty"`
	CreatedAt     time.Time           `json:"created_at"`
	// LastTool / LastToolAt 是最近一次调用的 MCP 工具（接入向导的连接检查用）。
	LastTool   string     `json:"last_tool,omitempty"`
	LastToolAt *time.Time `json:"last_tool_at,omitempty"`
}

// AgentState 是 Agent 对外的状态（CONTEXT.md「Agent 状态」）。
// HTTP MCP 的 Agent 不是一条常连着的线：没活干的时候它本来就不说话，
// 所以「最近有没有心跳」不是给人看的信号，只是「此刻忙不忙」的一个来源。
type AgentState string

const (
	// AgentRunning 执行中：名下有打开的执行记录，或刚刚还在说话。
	AgentRunning AgentState = "running"
	// AgentReady 可用：令牌有效、没被注销、所有者在职，只是这会儿没在干活。这是正常的休息状态。
	AgentReady AgentState = "ready"
	// AgentInactive 已停用：已注销，或所有者已停用。
	AgentInactive AgentState = "inactive"
)

// AgentBusyWindow 是「刚刚还在说话」的窗口：这段时间内有过请求就算执行中。
const AgentBusyWindow = 3 * time.Minute

// AgentStates 是三个状态的固定顺序（界面上的筛选与统计按它排）。
var AgentStates = []AgentState{AgentRunning, AgentReady, AgentInactive}

// State 算出 Agent 的状态：activeRuns 是它名下打开的执行记录数，ownerActive 是所有者是否在职。
func (a *Agent) State(now time.Time, activeRuns int, ownerActive bool) AgentState {
	switch {
	case a.RevokedAt != nil || !ownerActive:
		return AgentInactive
	case activeRuns > 0 || (a.LastSeenAt != nil && now.Sub(*a.LastSeenAt) < AgentBusyWindow):
		return AgentRunning
	default:
		return AgentReady
	}
}

// LastActiveAt 是最近一次活动：心跳与最近一次工具调用里更晚的那个（都没有时为空）。
func (a *Agent) LastActiveAt() *time.Time {
	switch {
	case a.LastToolAt == nil:
		return a.LastSeenAt
	case a.LastSeenAt == nil:
		return a.LastToolAt
	case a.LastToolAt.After(*a.LastSeenAt):
		return a.LastToolAt
	default:
		return a.LastSeenAt
	}
}

// Online 判断 Agent 最近是否有心跳。内部口径，不再是对外的产品信号（见 State）。
func (a *Agent) Online(now time.Time) bool {
	return a.RevokedAt == nil && a.LastSeenAt != nil && now.Sub(*a.LastSeenAt) < AgentBusyWindow
}

// GoalStatus 是目标的状态。
type GoalStatus string

const (
	GoalDraft     GoalStatus = "draft"
	GoalActive    GoalStatus = "active"
	GoalAchieved  GoalStatus = "achieved"
	GoalAbandoned GoalStatus = "abandoned"
)

// Goal 是目标。
type Goal struct {
	ID       string `json:"id"`
	OrgID    string `json:"org_id"`
	Version  int    `json:"version"` // 每次写入加一（ADR 0028）
	ParentID string `json:"parent_id,omitempty"`
	TeamID   string `json:"team_id,omitempty"`
	// TypeID 是目标类型（ADR 0023），可空：空表示「未分类」。类型只做分类与显示，不带任何行为。
	TypeID           string     `json:"type_id,omitempty"`
	OwnerMemberID    string     `json:"owner_member_id"`
	Title            string     `json:"title"`
	Description      string     `json:"description"`
	Status           GoalStatus `json:"status"`
	Budget           *float64   `json:"budget,omitempty"`
	Deadline         *time.Time `json:"deadline,omitempty"`
	PlannedStart     *time.Time `json:"planned_start,omitempty"`
	PlannedEnd       *time.Time `json:"planned_end,omitempty"`
	ProgressOverride *int       `json:"progress_override,omitempty"`
	// 路线图三字段（ADR 0021）：时间桶、信心度、一句话成果指标，都可空。
	Horizon    GoalHorizon    `json:"horizon,omitempty"`
	Confidence GoalConfidence `json:"confidence,omitempty"`
	Outcome    string         `json:"outcome,omitempty"`
	// 时间线两字段（ADR 0022）：时间粒度（空等于到周）与排序权重（空表示没手工排过）。
	DatePrecision DatePrecision `json:"date_precision,omitempty"`
	Rank          *float64      `json:"rank,omitempty"`
	Visibility    string        `json:"visibility"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

// Price 是价格表里的一条：每百万 token 的价格。
type Price struct {
	ModelID              string  `json:"model_id"`
	InputPerMillion      float64 `json:"input_per_million"`
	OutputPerMillion     float64 `json:"output_per_million"`
	CacheReadPerMillion  float64 `json:"cache_read_per_million"`
	CacheWritePerMillion float64 `json:"cache_write_per_million"`
	Currency             string  `json:"currency"`
}

// CostOf 按价格折算一条用量的金额（价格表货币）。
func (p Price) CostOf(u Usage) float64 {
	m := 1_000_000.0
	return float64(u.InputTokens)/m*p.InputPerMillion + float64(u.OutputTokens)/m*p.OutputPerMillion +
		float64(u.CacheReadTokens)/m*p.CacheReadPerMillion + float64(u.CacheWriteTokens)/m*p.CacheWritePerMillion
}

// Notification 是站内通知。
type Notification struct {
	ID        int64      `json:"id"`
	MemberID  string     `json:"member_id"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	TaskID    string     `json:"task_id,omitempty"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}
