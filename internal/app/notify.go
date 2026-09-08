package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/directory"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 通知外发（ADR 0019）：站内「待我处理」是事实源；组织决定允许哪些通道（飞书、企业微信、邮件、webhook）与哪些事件类型可外发；
// 个人在允许范围内为每种事件选通道与安静时段。外发只对新产生的事项生效：站内通知落库时按个人规则排进投递表，
// 后台每 15 秒把到点的投递发出去（3 次、退避、安静时段顺延），失败不阻塞业务写入。
// 通道是提供方的一种能力：IM 通道复用 IM 集成的凭据与接入检查（directory.Messenger），邮件与 webhook 是只发消息的提供方。

// 投递重试：最多 3 次，两次之间的间隔。
var (
	DeliveryMaxAttempts = 3
	DeliveryBackoff     = []time.Duration{30 * time.Second, 2 * time.Minute}
	// DegradedStreak 是连续失败多少次算「异常」（检查清单亮黄）。
	DegradedStreak = 3
	// dueReminderWindow 是逾期 / 到期提醒只对多"新"的事项生效：逾期或到期超过这些天的老事项不再补发。
	dueReminderWindow = 3
	// deliveryLockKey 是投递巡检的会话级建议锁：多实例部署时只有拿到锁的那个实例发。
	deliveryLockKey = int64(7019)
)

// ---------- 视图 ----------

// KindView 是一种事件类型。
type KindView struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

// ChannelHealthView 是一个通道最近的投递情况。
type ChannelHealthView struct {
	Status    string     `json:"status"` // ok | degraded
	Streak    int        `json:"streak"`
	LastError string     `json:"last_error"`
	LastAt    *time.Time `json:"last_at,omitempty"`
}

// NotifyChannelView 是组织的一个通道。Configured 是凭据齐全；Enabled 是组织开关；Available = 两者都真。
// IM 通道（飞书、企业微信）只在它是组织当前的 IM 集成提供方时可配置；Hint 说明为什么不可用。
type NotifyChannelView struct {
	Key           string            `json:"key"`
	Title         string            `json:"title"`
	Enabled       bool              `json:"enabled"`
	Configured    bool              `json:"configured"`
	Available     bool              `json:"available"`
	IM            bool              `json:"im"`
	Config        map[string]string `json:"config"`
	SecretsSet    map[string]bool   `json:"secrets_set"`
	Fields        []FieldView       `json:"fields"`
	Prerequisites []string          `json:"prerequisites"`
	Hint          string            `json:"hint,omitempty"`
	Health        ChannelHealthView `json:"health"`
}

// NotifyPolicyView 是 GET /org/notifications 的返回。
type NotifyPolicyView struct {
	Channels     map[string]NotifyChannelView `json:"channels"`
	ChannelOrder []string                     `json:"channel_order"`
	AllowedKinds []string                     `json:"allowed_kinds"`
	Kinds        []KindView                   `json:"kinds"`
	Health       map[string]ChannelHealthView `json:"health"`
}

// NotifyChannelInput 是 PUT /org/notifications 里一个通道的改动：enabled 省略不改；config 里保密字段省略或为空表示不改。
type NotifyChannelInput struct {
	Enabled *bool             `json:"enabled"`
	Config  map[string]string `json:"config"`
}

// NotifyPolicyInput 是 PUT /org/notifications 的输入。
type NotifyPolicyInput struct {
	Channels     map[string]NotifyChannelInput `json:"channels"`
	AllowedKinds *[]string                     `json:"allowed_kinds"`
}

// AvailableChannelView 是个人能选的一个通道；Bound 只对 IM 通道有意义（这个人绑没绑 IM 身份）。
type AvailableChannelView struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	IM    bool   `json:"im"`
	Bound bool   `json:"bound"`
	Hint  string `json:"hint,omitempty"`
}

// MyNotifyView 是 GET /me/notifications 的返回：解析后的规则 + 来源 + 可选通道。
type MyNotifyView struct {
	Rules             map[string][]string    `json:"rules"`
	Sources           map[string]string      `json:"sources"`
	QuietHours        *domain.QuietHours     `json:"quiet_hours"`
	AvailableChannels []AvailableChannelView `json:"available_channels"`
	Kinds             []KindView             `json:"kinds"`
	AllowedKinds      []string               `json:"allowed_kinds"`
	Defaults          map[string][]string    `json:"defaults"`
	Source            string                 `json:"source"` // personal | default
}

// DeliveryRecipient 是投递的收件人。
type DeliveryRecipient struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// DeliveryView 是一条投递记录。
type DeliveryView struct {
	ID           int64              `json:"id"`
	Kind         string             `json:"kind"`
	KindTitle    string             `json:"kind_title"`
	Channel      string             `json:"channel"`
	ChannelTitle string             `json:"channel_title"`
	Status       string             `json:"status"`
	StatusTitle  string             `json:"status_title"`
	Title        string             `json:"title"`
	Text         string             `json:"text"`
	URL          string             `json:"url"`
	Error        string             `json:"error"`
	Attempts     int                `json:"attempts"`
	CreatedAt    time.Time          `json:"created_at"`
	SentAt       *time.Time         `json:"sent_at,omitempty"`
	Recipient    *DeliveryRecipient `json:"recipient,omitempty"`
}

// NotifyTestResult 是 POST /org/notifications/test 的返回：一句投递结果。
type NotifyTestResult struct {
	OK       bool          `json:"ok"`
	Message  string        `json:"message"`
	Delivery *DeliveryView `json:"delivery,omitempty"`
}

func kindViews(loc i18n.Locale) []KindView {
	out := make([]KindView, 0, len(domain.NotifyKinds))
	for _, k := range domain.NotifyKinds {
		out = append(out, KindView{Key: k, Title: domain.NotifyKindTitle[k].In(loc)})
	}
	return out
}

func notifyError(err error) error {
	var ne *domain.NotifyError
	if errors.As(err, &ne) {
		return &UserError{Status: 400, Reasons: []i18n.Msg{ne.Msg}}
	}
	return err
}

// channelTitle 是通道的显示名：提供方标题。
func channelTitle(key string, loc i18n.Locale) string {
	if p, ok := directory.Lookup(key); ok {
		return p.Title.In(loc)
	}
	return key
}

// ---------- 组织的通道与策略（一次装载） ----------

// notifyChannel 是装载后的一个通道：提供方声明 + 组织配置 + 解出来的凭据。
type notifyChannel struct {
	prov       directory.Provider
	row        *store.NotifyChannel
	creds      map[string]string // 全部凭据（IM 通道含 IM 集成的凭据；保密字段已解密）
	secretsSet map[string]bool
	enabled    bool
	configured bool
	hint       i18n.Msg // 不可配置的原因
	im         bool
	messenger  directory.Messenger
	msgErr     error
}

func (c *notifyChannel) available() bool { return c.enabled && c.configured }

// notifySetup 是一个组织的外发设置：通道、允许的事件类型、当前 IM 提供方。
type notifySetup struct {
	orgID        string
	channels     map[string]*notifyChannel
	order        []string
	allowedKinds []string
	imProvider   string // 组织当前的 IM 集成提供方（可能为空）
	dirCfg       *store.DirectoryConfig
	dirSecrets   map[string]string
}

// availableChannels 是组织开了且配置齐全的通道，按注册顺序。
func (s *notifySetup) availableChannels() []string {
	out := []string{}
	for _, k := range s.order {
		if s.channels[k].available() {
			out = append(out, k)
		}
	}
	return out
}

// messengerFor 建（并缓存）一个通道的发消息客户端。
func (s *notifySetup) messengerFor(key string) (directory.Messenger, error) {
	c := s.channels[key]
	if c == nil {
		return nil, fmt.Errorf("unknown channel %s", key)
	}
	if c.messenger == nil && c.msgErr == nil {
		proxy := ""
		if s.dirCfg != nil {
			proxy = s.dirCfg.ProxyURL
		}
		c.messenger, c.msgErr = c.prov.NewMessenger(c.creds, directory.Options{ProxyURL: proxy})
	}
	return c.messenger, c.msgErr
}

// loadNotifySetup 在事务里装载组织的外发设置。
func (a *App) loadNotifySetup(ctx context.Context, tx pgx.Tx, orgID string) (*notifySetup, error) {
	s := &notifySetup{orgID: orgID, channels: map[string]*notifyChannel{}}
	rows, err := a.Store.ListNotifyChannels(ctx, tx)
	if err != nil {
		return nil, err
	}
	if kinds, ok, err := a.Store.NotifyPolicy(ctx, tx); err != nil {
		return nil, err
	} else if ok {
		s.allowedKinds = kinds
	} else {
		s.allowedKinds = append([]string{}, domain.NotifyKinds...)
	}
	if cfg, err := a.loadDirectoryConfig(ctx, tx, orgID); err == nil {
		s.dirCfg = cfg
		if secrets, err := directory.DecryptSecrets(a.SecretKey, cfg.SecretsEnc); err == nil {
			s.dirSecrets = secrets
			set := map[string]bool{}
			for k, v := range secrets {
				set[k] = v != ""
			}
			if p, ok := directory.Lookup(cfg.Provider); ok && directoryConfigured(p, cfg, set) {
				s.imProvider = p.Key
			}
		}
	} else if err != store.ErrNotFound {
		return nil, err
	}
	provs := directory.MessagingProviders()
	if s.imProvider != "" {
		// 测试登记的提供方不在生产列表里；组织当前的 IM 提供方只要能发消息就一定要有它的通道
		found := false
		for _, p := range provs {
			found = found || p.Key == s.imProvider
		}
		if p, ok := directory.Lookup(s.imProvider); ok && !found && p.CanMessage() {
			provs = append([]directory.Provider{p}, provs...)
		}
	}
	for _, p := range provs {
		c := &notifyChannel{prov: p, row: rows[p.Key], creds: map[string]string{}, secretsSet: map[string]bool{}, im: p.CanSync(), enabled: true}
		if c.row != nil {
			c.enabled = c.row.Enabled
			for k, v := range c.row.Config {
				c.creds[k] = v
			}
			if secrets, err := directory.DecryptSecrets(a.SecretKey, c.row.SecretsEnc); err == nil {
				for k, v := range secrets {
					c.creds[k] = v
					c.secretsSet[k] = v != ""
				}
			}
		}
		if c.im {
			// IM 通道：凭据来自 IM 集成，只在它是当前提供方且接入齐全时可配置
			if s.imProvider == p.Key {
				for k, v := range s.dirCfg.Credentials {
					c.creds[k] = v
				}
				for k, v := range s.dirSecrets {
					c.creds[k] = v
				}
				c.configured = p.MessagingReady(c.creds, c.secretsSet)
				if !c.configured {
					c.hint = i18n.M("notify.hint.im_fields", p.Title)
				}
			} else {
				c.hint = i18n.M("notify.hint.im_not_connected", p.Title)
			}
		} else {
			c.configured = p.MessagingReady(c.creds, c.secretsSet)
			if !c.configured {
				c.hint = i18n.M("notify.hint.not_configured", p.Title)
			}
		}
		s.channels[p.Key] = c
		s.order = append(s.order, p.Key)
	}
	return s, nil
}

// ---------- 个人规则的解析 ----------

// memberNotify 是一个成员解析后的通知规则。
type memberNotify struct {
	rules    map[string][]string
	sources  map[string]string
	quiet    *domain.QuietHours
	personal *domain.NotifyPrefs
	bound    bool
	defaults map[string][]string
}

// resolveMemberNotify 解析一个成员的规则：个人 → 默认（待确认操作与等我验收走 IM 私聊，绑定了才算；否则邮件）。
func (a *App) resolveMemberNotify(ctx context.Context, tx pgx.Tx, s *notifySetup, memberID string) (*memberNotify, error) {
	m := &memberNotify{}
	if s.imProvider != "" {
		ext, err := a.Store.MemberExternalID(ctx, tx, s.imProvider, memberID)
		if err != nil {
			return nil, err
		}
		m.bound = ext != ""
	}
	chans := s.availableChannels()
	m.defaults = domain.DefaultNotifyRules(s.imProvider, m.bound, chans, s.allowedKinds)
	if p, err := a.Store.NotifyPrefs(ctx, tx, memberID); err == nil {
		m.personal = p
		m.quiet = p.QuietHours
	} else if err != store.ErrNotFound {
		return nil, err
	}
	var personalRules map[string][]string
	if m.personal != nil {
		personalRules = m.personal.Rules
	}
	m.rules, m.sources = domain.ResolveNotifyRules(personalRules, m.defaults, chans, s.allowedKinds)
	return m, nil
}

// ---------- 排队（在产生事项的事务里调用） ----------

// deliveryLink 拼直达链接：任务 → /tasks/{id}/，目标 → /goals/{id}/，其余 → 首页（待确认操作在「待我处理」里）。
func (a *App) deliveryLink(taskID, goalID string) string {
	base := strings.TrimRight(a.PublicURL, "/")
	switch {
	case taskID != "":
		return base + "/tasks/" + taskID + "/"
	case goalID != "":
		return base + "/goals/" + goalID + "/"
	}
	return base + "/"
}

// enqueueNotify 按收件人的规则把一个事项排进投递表（同一事项同一通道只一次）。title / body 已按收件人语言渲染。
// 只在同一事务里插几行，失败就随事务回滚——外发本身永远在后台，不阻塞这次写入。
func (a *App) enqueueNotify(ctx context.Context, tx pgx.Tx, orgID, kind, itemID string, notificationID *int64, memberID, title, body, link string, now time.Time) error {
	if !domain.IsNotifyKind(kind) || memberID == "" {
		return nil
	}
	s, err := a.loadNotifySetup(ctx, tx, orgID)
	if err != nil {
		return err
	}
	if !contains(s.allowedKinds, kind) {
		return nil
	}
	m, err := a.resolveMemberNotify(ctx, tx, s, memberID)
	if err != nil {
		return err
	}
	nextAt := now
	if until := domain.QuietUntil(m.quiet, now); !until.IsZero() {
		nextAt = until
	}
	for _, ch := range m.rules[kind] {
		d := &store.Delivery{OrgID: orgID, ItemID: itemID, NotificationID: notificationID, Kind: kind, MemberID: memberID, Channel: ch,
			Title: title, Body: body, URL: link, NextAt: nextAt}
		if _, err := a.Store.EnqueueDelivery(ctx, tx, d); err != nil {
			return err
		}
	}
	return nil
}

// notifyAndDeliver 落一条站内通知并按规则排外发（kind 为空或不在目录里时只落站内）。link 为空时按通知的任务拼直达链接。
func (a *App) notifyAndDeliver(ctx context.Context, tx pgx.Tx, orgID, kind string, n domain.Notification, link string) error {
	id, err := a.Store.InsertNotificationID(ctx, tx, orgID, n)
	if err != nil {
		return err
	}
	if !domain.IsNotifyKind(kind) {
		return nil
	}
	if link == "" {
		link = a.deliveryLink(n.TaskID, "")
	}
	return a.enqueueNotify(ctx, tx, orgID, kind, fmt.Sprintf("notification:%d", id), &id, n.MemberID, n.Title, n.Body, link, time.Now())
}

// EnqueueDueReminders 给刚逾期的任务与刚到期的里程碑排外发提醒（由后台每分钟调用；同一事项只提醒一次）。
// 站内的「待我处理」本来就按日期派生出逾期组，这里不另落站内通知。只看最近几天内发生的，不补发老账。
func (a *App) EnqueueDueReminders(ctx context.Context, orgID string, now time.Time) error {
	return a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		s, err := a.loadNotifySetup(ctx, tx, orgID)
		if err != nil {
			return err
		}
		wantOverdue, wantMilestone := contains(s.allowedKinds, domain.NotifyOverdue), contains(s.allowedKinds, domain.NotifyMilestoneDue)
		if !wantOverdue && !wantMilestone {
			return nil
		}
		today := startOfDay(now)
		ownerOf := func(id string) string {
			if ag, err := a.Store.AgentByID(ctx, tx, id); err == nil {
				return ag.OwnerMemberID
			}
			return id
		}
		if wantOverdue {
			tasks, err := a.Store.AllTasks(ctx, tx)
			if err != nil {
				return err
			}
			types, err := a.Store.TaskTypesFor(ctx, tx, tasks)
			if err != nil {
				return err
			}
			for _, t := range tasks {
				if t.AssigneeID == "" || t.PlannedEnd == nil {
					continue
				}
				tt := types[t.TypeName]
				if tt == nil {
					continue
				}
				if st := tt.Workflow.State(t.State); st == nil || st.Label.IsTerminal() {
					continue
				}
				days := daysOverdue(t.PlannedEnd, today)
				if days <= 0 || days > dueReminderWindow {
					continue
				}
				to := ownerOf(t.AssigneeID)
				loc := a.localeOfMember(ctx, tx, orgID, to)
				title := i18n.Trf(loc, "notify.msg.overdue", t.Number, t.Title, days)
				if err := a.enqueueNotify(ctx, tx, orgID, domain.NotifyOverdue, "task:"+t.ID+":overdue", nil, to, title, "", a.deliveryLink(t.ID, ""), now); err != nil {
					return err
				}
			}
		}
		if wantMilestone {
			goals, err := a.Store.ListGoals(ctx, tx)
			if err != nil {
				return err
			}
			ids := make([]string, 0, len(goals))
			byID := map[string]*domain.Goal{}
			for _, g := range goals {
				ids = append(ids, g.ID)
				byID[g.ID] = g
			}
			ms, err := a.Store.MilestonesByGoals(ctx, tx, ids)
			if err != nil {
				return err
			}
			for _, m := range ms {
				g := byID[m.GoalID]
				if g == nil || m.Reached() || g.OwnerMemberID == "" {
					continue
				}
				due := calendarDay(m.DueOn) // 里程碑日期也是日历日，同 daysOverdue
				if due.After(today) {
					continue
				}
				days := int(today.Sub(due).Hours()/24 + 0.5)
				if days > dueReminderWindow {
					continue
				}
				loc := a.localeOfMember(ctx, tx, orgID, g.OwnerMemberID)
				key := "notify.msg.milestone_today"
				if days > 0 {
					key = "notify.msg.milestone_overdue"
				}
				title := i18n.Trf(loc, key, g.Title, m.Title, i18n.Date(m.DueOn))
				if err := a.enqueueNotify(ctx, tx, orgID, domain.NotifyMilestoneDue, "milestone:"+m.ID+":due", nil, g.OwnerMemberID, title, "", a.deliveryLink("", g.ID), now); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// ---------- 投递巡检 ----------

// DeliveryStats 是一次巡检的结果。
type DeliveryStats struct {
	Sent, Retried, Failed, Skipped, Deferred int
}

// plannedSend 是一条准备好的投递：客户端、外部编号、消息。
type plannedSend struct {
	d      *store.Delivery
	ms     directory.Messenger
	to     string
	msg    directory.Message
	skip   string // 非空表示跳过的理由
	defer_ time.Time
}

// prepareSends 在组织事务里为到点的投递准备发送材料。
func (a *App) prepareSends(ctx context.Context, tx pgx.Tx, orgID string, ds []*store.Delivery, now time.Time) ([]*plannedSend, error) {
	s, err := a.loadNotifySetup(ctx, tx, orgID)
	if err != nil {
		return nil, err
	}
	names, err := a.Store.ExecutorNames(ctx, tx)
	if err != nil {
		return nil, err
	}
	var out []*plannedSend
	for _, d := range ds {
		p := &plannedSend{d: d}
		out = append(out, p)
		loc := a.localeOfMember(ctx, tx, orgID, d.MemberID)
		ch := s.channels[d.Channel]
		if ch == nil || !ch.available() && d.Kind != "test" {
			p.skip = i18n.Trf(loc, "notify.skip.channel_off", channelTitle(d.Channel, loc))
			continue
		}
		if d.Kind != "test" {
			if m, err := a.resolveMemberNotify(ctx, tx, s, d.MemberID); err != nil {
				return nil, err
			} else if until := domain.QuietUntil(m.quiet, now); !until.IsZero() {
				p.defer_ = until
				continue
			}
		}
		switch {
		case ch.im:
			ext, err := a.Store.MemberExternalID(ctx, tx, d.Channel, d.MemberID)
			if err != nil {
				return nil, err
			}
			if ext == "" {
				p.skip = i18n.Trf(loc, "notify.skip.not_bound", ch.prov.Title, ch.prov.Title)
				continue
			}
			p.to = ext
		case d.Channel == domain.ChannelEmail:
			if mem, err := a.Store.MemberByID(ctx, tx, d.MemberID); err == nil {
				if acc, err := a.Store.AccountByID(ctx, tx, mem.AccountID); err == nil {
					p.to = acc.Email
				}
			}
			if p.to == "" || strings.HasSuffix(p.to, ".invalid") {
				p.skip = i18n.Tr(loc, "notify.skip.no_email")
				continue
			}
		default:
			p.to = d.MemberID
		}
		ms, err := s.messengerFor(d.Channel)
		if err != nil {
			p.skip = i18n.Trf(loc, "notify.skip.bad_config", ch.prov.Title, err.Error())
			continue
		}
		p.ms = ms
		p.msg = directory.Message{Kind: d.Kind, Title: d.Title, Text: d.Body, URL: d.URL, At: now,
			Recipient: directory.Recipient{ID: d.MemberID, Name: names[d.MemberID]}, Open: i18n.Tr(loc, "notify.open")}
	}
	return out, nil
}

// providerError 把提供方错误压成一句原话。
func providerError(err error) string {
	var rj *directory.RejectedError
	if errors.As(err, &rj) {
		return rj.Msg
	}
	var un *directory.UnreachableError
	if errors.As(err, &un) {
		return "unreachable: " + un.Err.Error()
	}
	return err.Error()
}

// finishSend 按发送结果更新投递：成功 → sent；失败 → 次数 +1，没到上限就退避后重试，到了就 failed。
func finishSend(d *store.Delivery, err error, now time.Time, st *DeliveryStats) {
	if err == nil {
		d.Status, d.Error = store.DeliverySent, ""
		t := now
		d.SentAt = &t
		d.Attempts++
		st.Sent++
		return
	}
	d.Attempts++
	d.Error = providerError(err)
	if d.Attempts >= DeliveryMaxAttempts {
		d.Status = store.DeliveryFailed
		st.Failed++
		return
	}
	back := DeliveryBackoff[len(DeliveryBackoff)-1]
	if d.Attempts-1 < len(DeliveryBackoff) {
		back = DeliveryBackoff[d.Attempts-1]
	}
	d.NextAt = now.Add(back)
	st.Retried++
}

// deliverBatch 处理一个组织的一批到点投递：准备 → 发送（事务之外）→ 写回。
func (a *App) deliverBatch(ctx context.Context, orgID string, ds []*store.Delivery, now time.Time, st *DeliveryStats) error {
	var plans []*plannedSend
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) (err error) {
		plans, err = a.prepareSends(ctx, tx, orgID, ds, now)
		return err
	}); err != nil {
		return err
	}
	for _, p := range plans {
		switch {
		case !p.defer_.IsZero():
			p.d.NextAt = p.defer_
			st.Deferred++
		case p.skip != "":
			p.d.Status, p.d.Error = store.DeliverySkipped, p.skip
			st.Skipped++
		default:
			sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := p.ms.SendDirect(sendCtx, p.to, p.msg)
			cancel()
			finishSend(p.d, err, now, st)
		}
	}
	return a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		for _, p := range plans {
			if err := a.Store.UpdateDelivery(ctx, tx, p.d); err != nil {
				return err
			}
		}
		return nil
	})
}

// RunNotificationDeliveries 把到点的投递发出去（由后台每 15 秒调用）。多实例时用数据库建议锁保证只有一个实例在发。
func (a *App) RunNotificationDeliveries(ctx context.Context, now time.Time) (DeliveryStats, error) {
	var st DeliveryStats
	conn, err := a.Store.Pool.Acquire(ctx)
	if err != nil {
		return st, err
	}
	defer conn.Release()
	var locked bool
	if err := conn.QueryRow(ctx, `select pg_try_advisory_lock($1)`, deliveryLockKey).Scan(&locked); err != nil {
		return st, err
	}
	if !locked {
		return st, nil
	}
	defer conn.Exec(ctx, `select pg_advisory_unlock($1)`, deliveryLockKey)
	due, err := a.Store.DueDeliveries(ctx, conn, now, 200)
	if err != nil {
		return st, err
	}
	byOrg := map[string][]*store.Delivery{}
	var orgs []string
	for _, d := range due {
		if _, ok := byOrg[d.OrgID]; !ok {
			orgs = append(orgs, d.OrgID)
		}
		byOrg[d.OrgID] = append(byOrg[d.OrgID], d)
	}
	var firstErr error
	for _, org := range orgs {
		if err := a.deliverBatch(ctx, org, byOrg[org], now, &st); err != nil {
			log.Printf("通知外发 %s: %v", org, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return st, firstErr
}

// ---------- 组织策略 ----------

func (a *App) healthViews(ctx context.Context, tx pgx.Tx, order []string) (map[string]ChannelHealthView, error) {
	raw, err := a.Store.DeliveryHealth(ctx, tx)
	if err != nil {
		return nil, err
	}
	out := map[string]ChannelHealthView{}
	for _, key := range order {
		v := ChannelHealthView{Status: "ok"}
		if h := raw[key]; h != nil {
			v.Streak, v.LastError, v.LastAt = h.Streak, h.LastError, h.LastAt
			if h.Streak >= DegradedStreak {
				v.Status = "degraded"
			}
		}
		out[key] = v
	}
	return out, nil
}

func (a *App) notifyPolicyView(ctx context.Context, tx pgx.Tx, s *notifySetup, loc i18n.Locale) (*NotifyPolicyView, error) {
	health, err := a.healthViews(ctx, tx, s.order)
	if err != nil {
		return nil, err
	}
	v := &NotifyPolicyView{Channels: map[string]NotifyChannelView{}, ChannelOrder: s.order, AllowedKinds: s.allowedKinds, Kinds: kindViews(loc), Health: health}
	for _, key := range s.order {
		c := s.channels[key]
		cv := NotifyChannelView{Key: key, Title: c.prov.Title.In(loc), Enabled: c.enabled, Configured: c.configured, Available: c.available(), IM: c.im,
			Config: map[string]string{}, SecretsSet: map[string]bool{}, Fields: []FieldView{}, Prerequisites: []string{}, Health: health[key]}
		if c.hint.Key != "" {
			cv.Hint = c.hint.Render(loc)
		}
		for _, f := range c.prov.MessagingFields {
			fv := FieldView{Key: f.Key, Title: f.Title.In(loc), Secret: f.Secret, Placeholder: f.Placeholder, Hint: f.Hint.In(loc)}
			if f.Secret {
				fv.Set = c.secretsSet[f.Key]
				cv.SecretsSet[f.Key] = fv.Set
			} else if c.row != nil {
				fv.Value = c.row.Config[f.Key]
				fv.Set = fv.Value != ""
				cv.Config[f.Key] = fv.Value
			}
			cv.Fields = append(cv.Fields, fv)
		}
		for _, t := range c.prov.MessagingPrerequisites {
			cv.Prerequisites = append(cv.Prerequisites, t.In(loc))
		}
		v.Channels[key] = cv
	}
	return v, nil
}

// GetNotifyPolicy 读组织的通道与允许范围（需要「组织设置」权限）。
func (a *App) GetNotifyPolicy(ctx context.Context, sess *Session) (*NotifyPolicyView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	var out *NotifyPolicyView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		s, err := a.loadNotifySetup(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		out, err = a.notifyPolicyView(ctx, tx, s, sess.Loc())
		return err
	})
	return out, err
}

// SetNotifyPolicy 改组织的通道配置与允许范围。每个改了的通道一条动态 NotificationChannelConfigured，
// 允许范围改了一条 NotificationPolicyChanged。
func (a *App) SetNotifyPolicy(ctx context.Context, sess *Session, in NotifyPolicyInput) (*NotifyPolicyView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	var kinds []string
	if in.AllowedKinds != nil {
		var err error
		if kinds, err = domain.NormalizeKinds(*in.AllowedKinds); err != nil {
			return nil, notifyError(err)
		}
	}
	for key := range in.Channels {
		if p, ok := directory.Lookup(key); !ok || !p.CanMessage() {
			return nil, Bad("err.notify_channel_unknown", key)
		}
	}
	if len(in.Channels) == 0 && in.AllowedKinds == nil {
		return nil, Bad("err.notify_body_empty")
	}
	var out *NotifyPolicyView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		s, err := a.loadNotifySetup(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		now := time.Now()
		var events []domain.Event
		for _, key := range s.order {
			ci, ok := in.Channels[key]
			if !ok {
				continue
			}
			c := s.channels[key]
			row := &store.NotifyChannel{OrgID: sess.OrgID, Channel: key, Enabled: c.enabled, Config: map[string]string{}}
			secrets := map[string]string{}
			if c.row != nil {
				for k, v := range c.row.Config {
					row.Config[k] = v
				}
				if m, err := directory.DecryptSecrets(a.SecretKey, c.row.SecretsEnc); err == nil {
					secrets = m
				}
			}
			if ci.Enabled != nil {
				row.Enabled = *ci.Enabled
			}
			changed := []string{}
			for k, v := range ci.Config {
				f, ok := c.prov.MessagingField(k)
				if !ok {
					return Bad("err.notify_field_unknown", channelTitle(key, sess.Loc()), k)
				}
				v = strings.TrimSpace(v)
				// 出网地址存进库之前先过一遍守卫：默认只许公网（egress.go）
				if !f.Secret && isURLField(k) && v != "" {
					if err := checkEgress(v); err != nil {
						return err
					}
				}
				if f.Secret {
					if v == "" {
						continue
					}
					secrets[k] = v
				} else {
					row.Config[k] = v
				}
				changed = append(changed, k)
			}
			if row.SecretsEnc, err = directory.EncryptSecrets(a.SecretKey, secrets); err != nil {
				return err
			}
			if err := a.Store.UpsertNotifyChannel(ctx, tx, row); err != nil {
				return err
			}
			sort.Strings(changed)
			events = append(events, domain.Event{Type: "NotificationChannelConfigured", ActorID: sess.Actor.ID, At: now,
				Data: map[string]any{"channel": key, "enabled": row.Enabled, "fields": changed}})
		}
		if in.AllowedKinds != nil {
			if err := a.Store.PutNotifyPolicy(ctx, tx, sess.OrgID, kinds); err != nil {
				return err
			}
			events = append(events, domain.Event{Type: "NotificationPolicyChanged", ActorID: sess.Actor.ID, At: now,
				Data: map[string]any{"allowed_kinds": kinds}})
		}
		if err := a.insertEvents(ctx, tx, sess, events); err != nil {
			return err
		}
		if s, err = a.loadNotifySetup(ctx, tx, sess.OrgID); err != nil {
			return err
		}
		out, err = a.notifyPolicyView(ctx, tx, s, sess.Loc())
		return err
	})
	return out, err
}

// TestNotifyChannel 用一个通道给调用者本人发一条测试消息，同步等结果，返回一句话；也记一条投递（kind = test）。
func (a *App) TestNotifyChannel(ctx context.Context, sess *Session, channel string) (*NotifyTestResult, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	if err := refuseDryRun(sess); err != nil {
		return nil, err
	}
	loc := sess.Loc()
	p, ok := directory.Lookup(channel)
	if !ok || !p.CanMessage() {
		return nil, Bad("err.notify_channel_unknown", channel)
	}
	now := time.Now()
	d := &store.Delivery{OrgID: sess.OrgID, ItemID: fmt.Sprintf("test:%s", store.NewID("t")), Kind: "test", MemberID: sess.MemberID, Channel: channel,
		Title: i18n.Tr(loc, "notify.test.title"), Body: i18n.Trf(loc, "notify.test.body", sess.Actor.Name), URL: a.deliveryLink("", ""), NextAt: now}
	var plans []*plannedSend
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		s, err := a.loadNotifySetup(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		if c := s.channels[channel]; !c.configured {
			return Bad("err.notify_channel_not_configured", p.Title, c.hint.Render(loc))
		}
		if _, err := a.Store.EnqueueDelivery(ctx, tx, d); err != nil {
			return err
		}
		plans, err = a.prepareSends(ctx, tx, sess.OrgID, []*store.Delivery{d}, now)
		return err
	})
	if err != nil {
		return nil, err
	}
	res := &NotifyTestResult{}
	pl := plans[0]
	var st DeliveryStats
	if pl.skip != "" {
		d.Status, d.Error = store.DeliverySkipped, pl.skip
		res.Message = pl.skip
	} else {
		sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		serr := pl.ms.SendDirect(sendCtx, pl.to, pl.msg)
		cancel()
		finishSend(d, serr, now, &st)
		d.Attempts = 1 // 测试不重试
		if d.Status == store.DeliveryQueued {
			d.Status = store.DeliveryFailed
		}
		if serr == nil {
			res.OK = true
			res.Message = i18n.Trf(loc, "notify.test.sent", p.Title)
		} else {
			res.Message = i18n.Trf(loc, "notify.test.failed", p.Title, d.Error)
		}
	}
	err = a.tx(ctx, sess, func(tx pgx.Tx) error {
		return a.Store.UpdateDelivery(ctx, tx, d)
	})
	if err != nil {
		return nil, err
	}
	v := deliveryView(d, loc, nil)
	res.Delivery = &v
	return res, nil
}

func deliveryView(d *store.Delivery, loc i18n.Locale, names map[string]string) DeliveryView {
	v := DeliveryView{ID: d.ID, Kind: d.Kind, Channel: d.Channel, ChannelTitle: channelTitle(d.Channel, loc), Status: d.Status, StatusTitle: i18n.Tr(loc, "notify.status."+d.Status),
		Title: d.Title, Text: d.Body, URL: d.URL, Error: d.Error, Attempts: d.Attempts, CreatedAt: d.CreatedAt, SentAt: d.SentAt}
	if t, ok := domain.NotifyKindTitle[d.Kind]; ok {
		v.KindTitle = t.In(loc)
	} else {
		v.KindTitle = i18n.Tr(loc, "notify.kind."+d.Kind)
	}
	if names != nil {
		v.Recipient = &DeliveryRecipient{ID: d.MemberID, Name: names[d.MemberID]}
	}
	return v
}

// OrgDeliveries 列组织最近的投递（需要「组织设置」权限）。
func (a *App) OrgDeliveries(ctx context.Context, sess *Session, limit int, channel, status string) ([]DeliveryView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	out := []DeliveryView{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		rows, err := a.Store.ListDeliveries(ctx, tx, store.DeliveryFilter{Channel: channel, Status: status, Limit: limit})
		if err != nil {
			return err
		}
		names, err := a.Store.ExecutorNames(ctx, tx)
		if err != nil {
			return err
		}
		for _, d := range rows {
			out = append(out, deliveryView(d, sess.Loc(), names))
		}
		return nil
	})
	return out, err
}

// ---------- 个人偏好 ----------

func (a *App) myNotifyView(ctx context.Context, tx pgx.Tx, s *notifySetup, memberID string, loc i18n.Locale) (*MyNotifyView, error) {
	m, err := a.resolveMemberNotify(ctx, tx, s, memberID)
	if err != nil {
		return nil, err
	}
	v := &MyNotifyView{Rules: m.rules, Sources: m.sources, QuietHours: m.quiet, AvailableChannels: []AvailableChannelView{}, Kinds: kindViews(loc), AllowedKinds: s.allowedKinds, Defaults: m.defaults, Source: "default"}
	if m.personal != nil && !m.personal.IsEmpty() {
		v.Source = "personal"
	}
	for _, key := range s.availableChannels() {
		c := s.channels[key]
		av := AvailableChannelView{Key: key, Title: c.prov.Title.In(loc), IM: c.im, Bound: !c.im || m.bound}
		if c.im && !m.bound {
			av.Hint = i18n.Trf(loc, "notify.hint.not_bound", c.prov.Title, c.prov.Title)
		}
		v.AvailableChannels = append(v.AvailableChannels, av)
	}
	return v, nil
}

// MyNotifications 返回我的通知偏好（解析后）。
func (a *App) MyNotifications(ctx context.Context, sess *Session) (*MyNotifyView, error) {
	if err := requireHumanPreferences(sess); err != nil {
		return nil, err
	}
	var out *MyNotifyView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		s, err := a.loadNotifySetup(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		out, err = a.myNotifyView(ctx, tx, s, sess.MemberID, sess.Loc())
		return err
	})
	return out, err
}

// SetMyNotifications 部分修改我的通知偏好：rules 里出现的事件类型被设置（空列表 = 只站内），quiet_hours 出现（含 null）即设置。
// 只能选组织允许的通道与事件类型。
func (a *App) SetMyNotifications(ctx context.Context, sess *Session, raw []byte) (*MyNotifyView, error) {
	if err := requireHumanPreferences(sess); err != nil {
		return nil, err
	}
	in, err := domain.ParseNotifyPrefsPatch(raw)
	if err != nil {
		return nil, notifyError(err)
	}
	var out *MyNotifyView
	err = a.tx(ctx, sess, func(tx pgx.Tx) error {
		s, err := a.loadNotifySetup(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		loc := sess.Loc()
		if err := domain.CheckRulesAllowed(in.Rules, s.availableChannels(), s.allowedKinds, loc, func(k string) string { return channelTitle(k, loc) }); err != nil {
			return notifyError(err)
		}
		base := domain.NotifyPrefs{}
		if p, err := a.Store.NotifyPrefs(ctx, tx, sess.MemberID); err == nil {
			base = *p
		} else if err != store.ErrNotFound {
			return err
		}
		merged := domain.MergeNotifyPrefs(base, in)
		if err := a.Store.PutNotifyPrefs(ctx, tx, sess.OrgID, sess.MemberID, merged); err != nil {
			return err
		}
		fields := domain.SortedKeys(in.Rules)
		if in.QuietSet {
			fields = append(fields, "quiet_hours")
		}
		if err := a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "NotificationPreferencesUpdated", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"member": sess.MemberID, "fields": fields}}}); err != nil {
			return err
		}
		out, err = a.myNotifyView(ctx, tx, s, sess.MemberID, loc)
		return err
	})
	return out, err
}

// ClearMyNotifications 一键重置：清掉个人记录，回到默认。
func (a *App) ClearMyNotifications(ctx context.Context, sess *Session) (*MyNotifyView, error) {
	if err := requireHumanPreferences(sess); err != nil {
		return nil, err
	}
	var out *MyNotifyView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		if err := a.Store.DeleteNotifyPrefs(ctx, tx, sess.MemberID); err != nil {
			return err
		}
		if err := a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "NotificationPreferencesUpdated", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"member": sess.MemberID, "cleared": true}}}); err != nil {
			return err
		}
		s, err := a.loadNotifySetup(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		out, err = a.myNotifyView(ctx, tx, s, sess.MemberID, sess.Loc())
		return err
	})
	return out, err
}

// MyDeliveries 列我最近的投递。
func (a *App) MyDeliveries(ctx context.Context, sess *Session, limit int) ([]DeliveryView, error) {
	if err := requireHumanPreferences(sess); err != nil {
		return nil, err
	}
	out := []DeliveryView{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		rows, err := a.Store.ListDeliveries(ctx, tx, store.DeliveryFilter{MemberID: sess.MemberID, Limit: limit})
		if err != nil {
			return err
		}
		for _, d := range rows {
			out = append(out, deliveryView(d, sess.Loc(), nil))
		}
		return nil
	})
	return out, err
}

// ---------- 接入检查里的「能以应用身份发消息」 ----------

// messagingCheck 给 IM 集成的检查清单补一项 messaging：用通道凭据建发消息客户端让提供方自检；
// 连续失败达到阈值时把最近一次错误原话写进 detail 并标为待处理（ADR 0019 第 3 条）。
func (a *App) messagingCheck(ctx context.Context, tx pgx.Tx, orgID string, prov directory.Provider) *directory.Check {
	if !prov.CanMessage() {
		return nil
	}
	s, err := a.loadNotifySetup(ctx, tx, orgID)
	if err != nil {
		return nil
	}
	c := s.channels[prov.Key]
	if c == nil {
		return nil
	}
	check := directory.Check{Key: directory.MessagingCheckKey, Title: textOf("directory.check.messaging"), Status: directory.CheckTodo}
	ms, err := s.messengerFor(prov.Key)
	if err != nil {
		check.Detail = textOf("directory.check.messaging.bad_config", prov.Title, err.Error())
		check.Fix = textOf("directory.check.messaging.fix_config")
		return &check
	}
	if dg, ok := ms.(directory.MessagingDiagnoser); ok {
		check = dg.DiagnoseMessaging(ctx)
	} else {
		check.Status = directory.CheckOK
		check.Detail = textOf("directory.check.messaging.assumed", prov.Title)
	}
	if !c.enabled {
		check.Status = directory.CheckTodo
		check.Detail = textOf("directory.check.messaging.disabled", prov.Title)
		check.Fix = textOf("directory.check.messaging.fix_enable")
	}
	if health, err := a.Store.DeliveryHealth(ctx, tx); err == nil {
		if h := health[prov.Key]; h != nil && h.Streak >= DegradedStreak {
			check.Status = directory.CheckTodo
			check.Detail = textOf("directory.check.messaging.degraded", h.Streak, h.LastError)
			if check.Fix.IsZero() {
				check.Fix = textOf("directory.check.messaging.fix_degraded")
			}
		}
	}
	check.Blocking = false
	return &check
}
