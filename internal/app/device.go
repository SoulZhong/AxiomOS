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

// Agent 设备码授权（ADR 0018）：Agent 端拿设备码与验证码 → 人在网页里输入验证码、选授权并批准
// → 系统以批准人为所有者创建 Agent → Agent 端轮询取令牌（只给一次）。设备码绑定的是 Agent 身份，
// 不是人的会话；Agent 的权限仍是所有者权限 ∩ 授权（ADR 0003）。

// DeviceTTL 是设备码的有效期。
const DeviceTTL = 15 * time.Minute

// DeviceInterval 是轮询间隔（秒），比它快会被拒。
const DeviceInterval = 5

// DeviceClients 是接入向导认得的运行环境。
var DeviceClients = []string{"claude-code", "cursor", "codex", "custom"}

// DeviceRequest 是 Agent 端拿到的申请结果。
type DeviceRequest struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// DeviceView 是网页看到的一条申请。
type DeviceView struct {
	UserCode    string     `json:"user_code"`
	Client      string     `json:"client"`
	ClientTitle string     `json:"client_title"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	StatusTitle string     `json:"status_title"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
	DecidedAt   *time.Time `json:"decided_at"`
	Agent       *AgentRef  `json:"agent"`
	ApprovedBy  *AgentRef  `json:"approved_by"`
}

// AgentRef 是 Agent 或成员的简短引用。
type AgentRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// DevicePoll 是 Agent 端轮询的结果。令牌只在第一次成功轮询时给出。
type DevicePoll struct {
	Status       string    `json:"status"` // pending | approved | denied | expired
	Token        string    `json:"token,omitempty"`
	Agent        *AgentRef `json:"agent,omitempty"`
	Organization string    `json:"organization,omitempty"`
	MCPURL       string    `json:"mcp_url"`
	Interval     int       `json:"interval"`
}

// ApproveDeviceInput 是批准时的选择。
type ApproveDeviceInput struct {
	Name          string
	Capabilities  []string
	Grants        map[domain.Grant]domain.GrantMode
	MaxConcurrent int
	Shared        bool
}

// NormalizeUserCode 把用户输入的验证码整理成 ABCD-1234：去空白、大写、补横线。
func NormalizeUserCode(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.NewReplacer(" ", "", "-", "", "_", "").Replace(s)
	if len(s) == 8 {
		return s[:4] + "-" + s[4:]
	}
	return s
}

// ParseGrantChoices 把向导里的三选一（allow | with_approval | deny）换成授权表；deny 的不进表。
func ParseGrantChoices(choices map[string]string) (map[domain.Grant]domain.GrantMode, error) {
	known := []string{}
	for _, g := range domain.AllGrants {
		known = append(known, string(g))
	}
	out := map[domain.Grant]domain.GrantMode{}
	for name, choice := range choices {
		g := domain.Grant(strings.TrimSpace(name))
		if !contains(known, string(g)) {
			return nil, Bad("err.grant_unknown", name, known)
		}
		switch strings.TrimSpace(choice) {
		case "allow", "direct":
			out[g] = domain.GrantDirect
		case "with_approval":
			out[g] = domain.GrantWithApproval
		case "deny", "":
		default:
			return nil, Bad("err.grant_mode", name, choice)
		}
	}
	return out, nil
}

func (a *App) mcpURL() string { return strings.TrimRight(a.PublicURL, "/") + "/mcp" }

func (a *App) verificationURL(userCode string) string {
	return strings.TrimRight(a.PublicURL, "/") + "/agents/connect/?code=" + userCode
}

// RequestDeviceCode 是公开接口：Agent 端申请设备码。
func (a *App) RequestDeviceCode(ctx context.Context, client, name string) (*DeviceRequest, error) {
	client = strings.ToLower(strings.TrimSpace(client))
	if client == "" {
		client = "custom"
	}
	if !contains(DeviceClients, client) {
		return nil, Bad("err.device_client", client)
	}
	name = strings.TrimSpace(name)
	if len([]rune(name)) > 80 {
		return nil, Bad("err.device_name_long")
	}
	d, code, err := a.Store.CreateDeviceCode(ctx, a.Store.Pool, client, name, DeviceTTL)
	if err != nil {
		return nil, err
	}
	return &DeviceRequest{DeviceCode: code, UserCode: d.UserCode, VerificationURL: a.verificationURL(d.UserCode), ExpiresIn: int(DeviceTTL.Seconds()), Interval: DeviceInterval}, nil
}

func deviceStatus(d *store.DeviceCode, now time.Time) string {
	if d.Status == "pending" && now.After(d.ExpiresAt) {
		return "expired"
	}
	return d.Status
}

func (a *App) deviceView(ctx context.Context, sess *Session, d *store.DeviceCode) *DeviceView {
	loc := sess.Loc()
	st := deviceStatus(d, time.Now())
	v := &DeviceView{UserCode: d.UserCode, Client: d.Client, ClientTitle: i18n.Tr(loc, "device.client."+d.Client), Name: d.Name, Status: st, StatusTitle: i18n.Tr(loc, "device.status."+st), CreatedAt: d.CreatedAt, ExpiresAt: d.ExpiresAt, DecidedAt: d.DecidedAt}
	if d.AgentID != "" && d.OrgID == sess.OrgID {
		_ = a.tx(ctx, sess, func(tx pgx.Tx) error {
			if ag, err := a.Store.AgentByID(ctx, tx, d.AgentID); err == nil {
				v.Agent = &AgentRef{ID: ag.ID, Name: ag.Name}
			}
			if d.ApprovedBy != "" {
				if m, err := a.Store.MemberByID(ctx, tx, d.ApprovedBy); err == nil {
					v.ApprovedBy = &AgentRef{ID: m.ID, Name: m.Name}
				}
			}
			return nil
		})
	}
	return v
}

// deviceLookups 限制猜验证码的次数：同一个账号（能拿到客户端地址时连地址一起）在 5 分钟内
// 查错 10 次，就冷却 5 分钟不再受理。验证码只有 8 位，没有这道限制登录用户可以逐个枚举（Codex 审查）。
var deviceLookups = newAttemptLimiter(10, 5*time.Minute, 5*time.Minute)

// deviceLookupKeys 是这次查询要计数的键：账号一个，客户端地址一个。
func deviceLookupKeys(sess *Session) []string {
	if sess == nil {
		return nil
	}
	keys := []string{}
	if sess.AccountID != "" {
		keys = append(keys, "acct:"+sess.AccountID)
	} else if sess.MemberID != "" {
		keys = append(keys, "member:"+sess.MemberID)
	}
	if sess.ClientIP != "" {
		keys = append(keys, "ip:"+sess.ClientIP)
	}
	return keys
}

// lookupDevice 按验证码读一条申请，并把猜错的次数记下来。
func (a *App) lookupDevice(ctx context.Context, sess *Session, userCode string) (*store.DeviceCode, error) {
	keys := deviceLookupKeys(sess)
	if !deviceLookups.Allowed(keys...) {
		return nil, userErr(429, "err.device_lookup_throttled")
	}
	d, err := a.Store.DeviceByUserCode(ctx, a.Store.Pool, NormalizeUserCode(userCode))
	if err != nil || (d.OrgID != "" && d.OrgID != sess.OrgID) {
		deviceLookups.Fail(keys...)
		return nil, NotFound("err.device_missing")
	}
	deviceLookups.Reset(keys...)
	return d, nil
}

// DeviceRequestByUserCode 是网页按验证码查申请（需要登录；申请在被批准前不属于任何组织）。
func (a *App) DeviceRequestByUserCode(ctx context.Context, sess *Session, userCode string) (*DeviceView, error) {
	d, err := a.lookupDevice(ctx, sess, userCode)
	if err != nil {
		return nil, err
	}
	return a.deviceView(ctx, sess, d), nil
}

// pendingDevice 读一条还能批准 / 拒绝的申请。
func (a *App) pendingDevice(ctx context.Context, sess *Session, userCode string) (*store.DeviceCode, error) {
	if sess.IsAgent() {
		return nil, Forbidden("err.device_agent")
	}
	d, err := a.lookupDevice(ctx, sess, userCode)
	if err != nil {
		return nil, err
	}
	switch deviceStatus(d, time.Now()) {
	case "pending":
		return d, nil
	case "expired":
		return nil, Bad("err.device_expired")
	default:
		return nil, Bad("err.device_decided", i18n.Key("device.status."+d.Status))
	}
}

// ApproveDevice 批准接入：以批准人为所有者创建 Agent（走与手工注册同一条路），令牌加密暂存给 Agent 端来取。
func (a *App) ApproveDevice(ctx context.Context, sess *Session, userCode string, in ApproveDeviceInput) (*domain.Agent, *DeviceView, error) {
	if err := refuseDryRun(sess); err != nil {
		return nil, nil, err
	}
	d, err := a.pendingDevice(ctx, sess, userCode)
	if err != nil {
		return nil, nil, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = d.Name
	}
	if name == "" {
		name = i18n.Tr(sess.Loc(), "device.client."+d.Client)
	}
	reg := RegisterAgentInput{Name: name, Runtime: d.Client, Capabilities: in.Capabilities, Grants: in.Grants, MaxConcurrent: in.MaxConcurrent, Shared: in.Shared}
	if err := a.validateRegisterAgent(sess, &reg); err != nil {
		return nil, nil, err
	}
	var ag *domain.Agent
	err = a.tx(ctx, sess, func(tx pgx.Tx) error {
		var token string
		var err error
		ag, token, err = a.createAgent(ctx, tx, sess, reg)
		if err != nil {
			return err
		}
		enc, err := directory.Encrypt(a.SecretKey, token)
		if err != nil {
			return err
		}
		if err := a.Store.DecideDevice(ctx, tx, d.ID, "approved", sess.OrgID, ag.ID, sess.MemberID, enc); err != nil {
			return err
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "AgentConnected", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"agent_id": ag.ID, "name": ag.Name, "client": d.Client, "user_code": d.UserCode}}})
	})
	if err != nil {
		return nil, nil, err
	}
	d, _ = a.Store.DeviceByUserCode(ctx, a.Store.Pool, d.UserCode)
	return ag, a.deviceView(ctx, sess, d), nil
}

// DenyDevice 拒绝接入：不创建任何东西，Agent 端轮询会得到 denied。
func (a *App) DenyDevice(ctx context.Context, sess *Session, userCode string) (*DeviceView, error) {
	if err := refuseDryRun(sess); err != nil {
		return nil, err
	}
	d, err := a.pendingDevice(ctx, sess, userCode)
	if err != nil {
		return nil, err
	}
	if err := a.Store.DecideDevice(ctx, a.Store.Pool, d.ID, "denied", sess.OrgID, "", sess.MemberID, nil); err != nil {
		return nil, err
	}
	d, _ = a.Store.DeviceByUserCode(ctx, a.Store.Pool, d.UserCode)
	return a.deviceView(ctx, sess, d), nil
}

// PollDevice 是公开接口：Agent 端用设备码轮询。轮询快于间隔返回 429；令牌只给一次。
func (a *App) PollDevice(ctx context.Context, deviceCode string) (*DevicePoll, error) {
	deviceCode = strings.TrimSpace(deviceCode)
	if deviceCode == "" {
		return nil, Bad("err.device_code_missing")
	}
	d, err := a.Store.DeviceByCode(ctx, a.Store.Pool, deviceCode)
	if err != nil {
		return nil, NotFound("err.device_code_missing")
	}
	now := time.Now()
	// 限流：检查与记时间是同一条语句，并发的多次轮询里只有一次能过（Codex 审查）。
	ok, err := a.Store.TouchDevicePoll(ctx, a.Store.Pool, d.ID, time.Duration(DeviceInterval)*time.Second)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, userErr(429, "err.device_slow_down", DeviceInterval)
	}
	out := &DevicePoll{Status: deviceStatus(d, now), MCPURL: a.mcpURL(), Interval: DeviceInterval}
	if out.Status != "approved" {
		return out, nil
	}
	if d.OrgID != "" {
		_ = a.Store.WithOrg(ctx, d.OrgID, func(tx pgx.Tx) error {
			if ag, err := a.Store.AgentByID(ctx, tx, d.AgentID); err == nil {
				out.Agent = &AgentRef{ID: ag.ID, Name: ag.Name}
			}
			if org, err := a.Store.OrganizationByID(ctx, tx, d.OrgID); err == nil {
				out.Organization = org.Name
			}
			return nil
		})
	}
	if len(d.TokenEnc) > 0 {
		tok, err := directory.Decrypt(a.SecretKey, d.TokenEnc)
		if err != nil {
			return nil, err
		}
		// 领取是一条带条件的原子更新：并发的两次轮询里只有抢到的那一次拿到令牌，
		// 另一次只回 status: approved（Codex 审查）。
		taken, err := a.Store.TakeDeviceToken(ctx, a.Store.Pool, d.ID)
		if err != nil {
			return nil, err
		}
		if taken {
			out.Token = tok
		}
	}
	return out, nil
}

// ExpireDeviceCodes 由后台巡检调用：过期的申请标为过期，旧记录清理。
func (a *App) ExpireDeviceCodes(ctx context.Context) (int, error) {
	return a.Store.ExpireDeviceCodes(ctx, a.Store.Pool, time.Now())
}

// AgentCheck 是接入向导的连接检查。
type AgentCheck struct {
	// State / StateTitle 是对外的三选一状态（CONTEXT.md「Agent 状态」）。
	State      domain.AgentState `json:"state"`
	StateTitle string            `json:"state_title"`
	// Online 是旧口径（最近有没有心跳），只为兼容旧客户端保留。
	Online       bool       `json:"online"`
	Connected    bool       `json:"connected"` // 曾经收到过它的请求
	LastSeenAt   *time.Time `json:"last_seen_at"`
	LastActiveAt *time.Time `json:"last_active_at"`
	LastTool     string     `json:"last_tool,omitempty"`
	LastToolAt   *time.Time `json:"last_tool_at,omitempty"`
	Hint         string     `json:"hint"`
}

// CheckAgent 用心跳数据回答"它连上了吗"。能看到这个 Agent 的人都能查。
func (a *App) CheckAgent(ctx context.Context, sess *Session, id string) (*AgentCheck, error) {
	var out *AgentCheck
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		ag, err := a.Store.AgentByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if !(sess.SeesAllAgents() || ag.OwnerMemberID == sess.MemberID || ag.Shared) {
			return NotFound("err.not_found")
		}
		now := time.Now()
		loc := sess.Loc()
		runs, err := a.Store.ActiveRunCount(ctx, tx, ag.ID)
		if err != nil {
			return err
		}
		ownerActive := true
		if owner, err := a.Store.MemberByID(ctx, tx, ag.OwnerMemberID); err == nil {
			ownerActive = owner.Active
		}
		st := NewAgentStatus(loc, ag, now, runs, ownerActive)
		out = &AgentCheck{State: st.State, StateTitle: st.Title, Online: ag.Online(now), Connected: ag.LastSeenAt != nil,
			LastSeenAt: ag.LastSeenAt, LastActiveAt: st.LastActiveAt, LastTool: ag.LastTool, LastToolAt: ag.LastToolAt}
		at := ""
		if st.LastActiveAt != nil {
			at = st.LastActiveAt.Local().Format("15:04")
		}
		switch {
		case st.State == domain.AgentInactive:
			out.Hint = i18n.Tr(loc, "agent.check.inactive")
		case ag.LastSeenAt == nil:
			out.Hint = i18n.Tr(loc, "agent.check.never")
		case ag.LastTool != "" && ag.LastToolAt != nil:
			out.Hint = i18n.Trf(loc, "agent.check.state_tool", st.Title, at, ag.LastTool)
		default:
			out.Hint = i18n.Trf(loc, "agent.check.state", st.Title, at)
		}
		return nil
	})
	return out, err
}

// RecordAgentTool 由 MCP 层在每次工具调用后记下工具名（遥测，不产生动态）。
func (a *App) RecordAgentTool(ctx context.Context, sess *Session, tool string) {
	if sess == nil || !sess.IsAgent() {
		return
	}
	_ = a.Store.WithOrg(ctx, sess.OrgID, func(tx pgx.Tx) error { return a.Store.RecordAgentTool(ctx, tx, sess.AgentID, tool) })
}
