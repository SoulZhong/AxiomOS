package domain

import (
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

// 待确认操作（Proposal）：Agent 发起、但要等人点确认才生效的动作（ADR 0003）。
// 内核只负责判断"这一步要不要人确认"并给出信号；记录与再执行由应用层做。

// ProposalStatus 是待确认操作的状态。
type ProposalStatus string

const (
	ProposalPending  ProposalStatus = "pending"  // 等人确认
	ProposalApproved ProposalStatus = "approved" // 已确认并执行
	ProposalRejected ProposalStatus = "rejected" // 已拒绝
	ProposalExpired  ProposalStatus = "expired"  // 超过有效期，自动作废
	ProposalStale    ProposalStatus = "stale"    // 对象在等待期间变了，不能再答（ADR 0028）
)

// ProposalTTL 是待确认操作的有效期：超过它没人确认就自动过期。
const ProposalTTL = 7 * 24 * time.Hour

// Proposal 是一条待确认操作。
type Proposal struct {
	ID          string         `json:"id"`
	OrgID       string         `json:"org_id"`
	AgentID     string         `json:"agent_id"`
	OwnerID     string         `json:"owner_id"` // Agent 的所有者，默认的确认人
	Action      string         `json:"action"`   // 动作代码名，如 task.transition
	Grant       Grant          `json:"grant,omitempty"`
	TargetKind  string         `json:"target_kind,omitempty"` // task | goal | sprint | task_type | agent
	TargetID    string         `json:"target_id,omitempty"`
	TargetTitle string         `json:"target_title,omitempty"`
	Payload     map[string]any `json:"payload"` // 再执行这个动作所需的全部输入
	Summary     i18n.Text      `json:"summary"` // 完整句子：确认后会发生什么
	// AgentName 是提交时 Agent 的名字（快照）：摘要里的名字冻在那一刻，读的时候用它把摘要换成现在的名字
	AgentName string         `json:"agent_name,omitempty"`
	Status    ProposalStatus `json:"status"`
	DecidedBy string         `json:"decided_by,omitempty"`
	DecidedAt *time.Time     `json:"decided_at,omitempty"`
	Reason    string         `json:"reason,omitempty"` // 拒绝理由（完整句子）
	CreatedAt time.Time      `json:"created_at"`
	ExpiresAt time.Time      `json:"expires_at"`
	// ADR 0028：捆、版本、快照。
	BundleID       string              `json:"bundle_id,omitempty"`      // 同一任务上同一 Agent 连续提出的多条合成一捆
	TargetVersion  int                 `json:"target_version,omitempty"` // 提出时对象的版本，0 表示不校验
	GrantsSnapshot map[Grant]GrantMode `json:"grants_snapshot,omitempty"`
}

// Pending 判断是否还在等人确认。
func (p *Proposal) Pending() bool { return p.Status == ProposalPending }

// ValidateProposal 校验一条待确认操作：动作名不能为空、必须带上再执行所需的输入、有效期必须晚于创建时间。
func ValidateProposal(p *Proposal) []error {
	var errs []error
	bad := func(key string, args ...any) {
		errs = append(errs, &ValidationError{Msg: i18n.M(key, args...)})
	}
	if p == nil {
		bad("val.proposal_no_action")
		return errs
	}
	if p.Action == "" {
		bad("val.proposal_no_action")
	}
	if p.Payload == nil {
		bad("val.proposal_no_payload")
	}
	if p.Summary.IsZero() {
		bad("val.proposal_no_summary")
	}
	if !p.ExpiresAt.After(p.CreatedAt) {
		bad("val.proposal_expiry")
	}
	return errs
}

// ErrNeedsApproval 是内核的信号：这个动作本身允许，但触发者的授权是「需要人确认」，
// 所以内核不做任何改动，由应用层记成一条待确认操作。
type ErrNeedsApproval struct{ Grant Grant }

// Error 用默认语言渲染。
func (e *ErrNeedsApproval) Error() string { return e.Render(i18n.Default) }

// Render 按语言渲染。
func (e *ErrNeedsApproval) Render(l i18n.Locale) string { return e.Reason().Render(l) }

// Reason 返回可翻译的原因。
func (e *ErrNeedsApproval) Reason() Reason {
	return i18n.M("reject.needs_approval", i18n.Key("grant."+string(e.Grant)))
}

// needsApproval 构造信号。
func needsApproval(g Grant) error { return &ErrNeedsApproval{Grant: g} }

// GrantModeOf 返回触发者持有某项授权的生效方式：
// 成员总是「直接生效」；Agent 返回所有者设定的模式，没有这项授权时返回空字符串。
func GrantModeOf(actor *Executor, g Grant) GrantMode {
	if actor == nil || actor.Kind != ExecutorAgent {
		return GrantDirect
	}
	mode, ok := actor.Grants[g]
	if !ok {
		return ""
	}
	if mode == "" {
		return GrantDirect
	}
	return mode
}

// NeedsApproval 判断触发者做这件事是否要先经人确认。
func NeedsApproval(actor *Executor, g Grant) bool {
	return GrantModeOf(actor, g) == GrantWithApproval
}
