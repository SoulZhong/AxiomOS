package app

// 外部日历（ADR 0032）：组织级连接（一家提供方一行）、成员绑定、只读同步、Google 的成员授权。
// 全部组织设置类操作要「组织设置」权限；成员自己的绑定不用。

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/directory"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 同步窗口：过去 7 天到未来 60 天；后台每 15 分钟一轮。
const (
	calendarSyncBack     = 7 * 24 * time.Hour
	calendarSyncAhead    = 60 * 24 * time.Hour
	calendarSyncInterval = 15 * time.Minute
	googleStateTTL       = 15 * time.Minute
)

// CalendarInput 是保存一家提供方连接的输入。
type CalendarInput struct {
	Provider    string            `json:"provider"`
	Credentials map[string]string `json:"credentials"`
	ProxyURL    *string           `json:"proxy_url"`
	Enabled     *bool             `json:"enabled"`
}

// CalendarView 是一家提供方的连接状态（给组织设置页）。
type CalendarView struct {
	Provider      string            `json:"provider"`
	ProviderTitle string            `json:"provider_title"`
	Configured    bool              `json:"configured"`
	Enabled       bool              `json:"enabled"`
	PerMember     bool              `json:"per_member"` // 成员各自授权（Google）
	Fields        []FieldView       `json:"fields"`
	Prerequisites []string          `json:"prerequisites"`
	Tip           *GuideView        `json:"tip,omitempty"`
	Credentials   map[string]string `json:"credentials"`
	SecretsSet    map[string]bool   `json:"secrets_set"`
	ProxyURL      string            `json:"proxy_url"`
	// 借用 IM 集成的凭据：飞书 / 企业微信同一个应用，这里没填的字段沿用那边
	InheritsDirectory bool       `json:"inherits_directory"`
	LastSyncAt        *time.Time `json:"last_sync_at,omitempty"`
	LastStatus        string     `json:"last_status"`
	LastStatusTitle   string     `json:"last_status_title"`
	LastError         string     `json:"last_error,omitempty"`
	ConnectedMembers  int        `json:"connected_members"`
	RedirectURL       string     `json:"redirect_url,omitempty"` // Google：要填进 OAuth 客户端的回调地址
}

// CalendarsView 是组织设置「外部日历」页的整份数据。
type CalendarsView struct {
	Providers []CalendarView `json:"providers"`
}

// MyCalendarView 是成员自己看到的某家提供方的连接情况。
type MyCalendarView struct {
	Provider      string     `json:"provider"`
	ProviderTitle string     `json:"provider_title"`
	OrgConfigured bool       `json:"org_configured"`
	PerMember     bool       `json:"per_member"`
	Connected     bool       `json:"connected"`
	Email         string     `json:"email,omitempty"`
	ExternalID    string     `json:"external_user_id,omitempty"`
	ConnectedAt   *time.Time `json:"connected_at,omitempty"`
	AuthURL       string     `json:"auth_url,omitempty"` // Google：去授权的地址（每次请求现算，带一次性 state）
	// 自助的提供方（日历订阅链接）：成员自己填 Fields 就能连，不用组织配置
	SelfService   bool        `json:"self_service"`
	Fields        []FieldView `json:"fields,omitempty"`
	Prerequisites []string    `json:"prerequisites,omitempty"`
	Tip           *GuideView  `json:"tip,omitempty"`
	// 这个成员自己的同步情况（有绑定行的才有）
	Label           string     `json:"label,omitempty"`
	LastSyncAt      *time.Time `json:"last_sync_at,omitempty"`
	LastStatus      string     `json:"last_status,omitempty"`
	LastStatusTitle string     `json:"last_status_title,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
}

// CalendarTestResult 是接入检查的结果。
type CalendarTestResult struct {
	OK     bool        `json:"ok"`
	Checks []CheckView `json:"checks"`
	Error  string      `json:"error,omitempty"`
}

// CalendarSyncResult 是一次同步的结果。
type CalendarSyncResult struct {
	Provider string   `json:"provider"`
	Members  int      `json:"members"`
	Events   int      `json:"events"`
	Errors   []string `json:"errors"`
	Status   string   `json:"status"`
}

// calendarProvidersFor 生产列表 + 组织已连接的（测试提供方只在连上之后才出现在列表里）。
func (a *App) calendarProvidersFor(ctx context.Context, tx pgx.Tx) ([]directory.Provider, map[string]*store.CalendarConfig, error) {
	cfgs, err := a.Store.ListCalendarConfigs(ctx, tx)
	if err != nil {
		return nil, nil, err
	}
	byKey := map[string]*store.CalendarConfig{}
	for _, c := range cfgs {
		byKey[c.Provider] = c
	}
	provs := directory.CalendarProviders()
	seen := map[string]bool{}
	for _, p := range provs {
		seen[p.Key] = true
	}
	for _, c := range cfgs {
		if seen[c.Provider] {
			continue
		}
		if p, ok := calendarProvider(c.Provider); ok {
			provs = append(provs, p)
			seen[p.Key] = true
		}
	}
	return provs, byKey, nil
}

func calendarProvider(key string) (directory.Provider, bool) {
	p, ok := directory.Lookup(strings.ToLower(strings.TrimSpace(key)))
	if !ok || !p.CanCalendar() {
		return directory.Provider{}, false
	}
	return p, true
}

// calendarFieldsOf 一家提供方要填的全部字段：读通讯录的字段（飞书 / 企业微信借用 IM 集成的）+ 日历专用字段。
func calendarFieldsOf(p directory.Provider) []directory.CredentialField {
	return append(append([]directory.CredentialField{}, p.Fields...), p.CalendarFields...)
}

// calendarCreds 把一家提供方可用的凭据拼齐：IM 集成里同一提供方的凭据打底，日历连接里填的覆盖。
// 返回明文凭据（含解密后的保密字段）与「哪些保密字段已设置」。
func (a *App) calendarCreds(ctx context.Context, tx pgx.Tx, orgID string, p directory.Provider, cfg *store.CalendarConfig) (map[string]string, map[string]bool, bool, error) {
	creds, set := map[string]string{}, map[string]bool{}
	inherits := false
	if p.CanSync() {
		if dcfg, err := a.loadDirectoryConfig(ctx, tx, orgID); err == nil && dcfg.Provider == p.Key {
			inherits = true
			for k, v := range dcfg.Credentials {
				creds[k] = v
			}
			if m, err := directory.DecryptSecrets(a.SecretKey, dcfg.SecretsEnc); err == nil {
				for k, v := range m {
					if v != "" {
						creds[k], set[k] = v, true
					}
				}
			}
		} else if err != nil && err != store.ErrNotFound {
			return nil, nil, false, err
		}
	}
	if cfg != nil {
		for k, v := range cfg.Credentials {
			if v != "" {
				creds[k] = v
			}
		}
		m, err := directory.DecryptSecrets(a.SecretKey, cfg.SecretsEnc)
		if err != nil {
			return nil, nil, false, Bad("err.directory_secret_key")
		}
		for k, v := range m {
			if v != "" {
				creds[k], set[k] = v, true
			}
		}
	}
	return creds, set, inherits, nil
}

// calendarConfigured 判断凭据是否齐全。
func calendarConfigured(p directory.Provider, creds map[string]string) bool {
	for _, f := range calendarFieldsOf(p) {
		if !f.Optional && creds[f.Key] == "" {
			return false
		}
	}
	return true
}

func (a *App) calendarView(ctx context.Context, tx pgx.Tx, sess *Session, p directory.Provider, cfg *store.CalendarConfig) (CalendarView, error) {
	loc := sess.Loc()
	creds, set, inherits, err := a.calendarCreds(ctx, tx, sess.OrgID, p, cfg)
	if err != nil {
		return CalendarView{}, err
	}
	v := CalendarView{Provider: p.Key, ProviderTitle: p.Title.In(loc), PerMember: p.PerMemberCalendar, Fields: []FieldView{}, Prerequisites: []string{},
		Credentials: map[string]string{}, SecretsSet: map[string]bool{}, InheritsDirectory: inherits, Configured: cfg != nil && calendarConfigured(p, creds)}
	if len(p.Tip.Text) > 0 {
		v.Tip = &GuideView{Text: p.Tip.Text.In(loc), URL: p.Tip.URL}
	}
	for _, f := range calendarFieldsOf(p) {
		fv := FieldView{Key: f.Key, Title: f.Title.In(loc), Secret: f.Secret, Optional: f.Optional, Placeholder: f.Placeholder, Hint: f.Hint.In(loc)}
		if f.Secret {
			fv.Set = set[f.Key]
			v.SecretsSet[f.Key] = fv.Set
		} else {
			fv.Value = creds[f.Key]
			fv.Set = fv.Value != ""
			v.Credentials[f.Key] = fv.Value
		}
		v.Fields = append(v.Fields, fv)
	}
	for _, t := range p.CalendarPrerequisites {
		v.Prerequisites = append(v.Prerequisites, t.In(loc))
	}
	if cfg != nil {
		v.Enabled, v.ProxyURL, v.LastSyncAt, v.LastStatus, v.LastError = cfg.Enabled, cfg.ProxyURL, cfg.LastSyncAt, cfg.LastStatus, cfg.LastError
		if cfg.LastStatus != "" {
			v.LastStatusTitle = i18n.Tr(loc, "directory.status."+cfg.LastStatus)
		}
	}
	if ids, err := a.Store.ListCalendarIdentities(ctx, tx, p.Key); err == nil {
		v.ConnectedMembers = len(ids)
	}
	if p.PerMemberCalendar {
		v.RedirectURL = a.googleRedirectURL()
	}
	return v, nil
}

// ListCalendars 组织设置「外部日历」：每家提供方一份状态。
func (a *App) ListCalendars(ctx context.Context, sess *Session) (*CalendarsView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	out := &CalendarsView{Providers: []CalendarView{}}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		provs, byKey, err := a.calendarProvidersFor(ctx, tx)
		if err != nil {
			return err
		}
		for _, p := range provs {
			if p.SelfServiceCalendar {
				continue // 成员自己连的，组织设置页不管
			}
			v, err := a.calendarView(ctx, tx, sess, p, byKey[p.Key])
			if err != nil {
				return err
			}
			out.Providers = append(out.Providers, v)
		}
		return nil
	})
	return out, err
}

// SaveCalendar 保存一家提供方的连接：凭据（保密字段留空表示不改）、出网代理、启用与否。
func (a *App) SaveCalendar(ctx context.Context, sess *Session, in CalendarInput) (*CalendarView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	p, ok := calendarProvider(in.Provider)
	if !ok {
		return nil, Bad("err.calendar_provider", in.Provider)
	}
	if in.ProxyURL != nil && strings.TrimSpace(*in.ProxyURL) != "" {
		if err := checkProxy(*in.ProxyURL); err != nil {
			return nil, err
		}
	}
	for k, v := range in.Credentials {
		if isURLField(k) && strings.TrimSpace(v) != "" {
			if err := checkEgress(v); err != nil {
				return nil, err
			}
		}
	}
	var out CalendarView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		cfg, err := a.Store.CalendarConfig(ctx, tx, p.Key)
		fresh := err == store.ErrNotFound
		if fresh {
			cfg = &store.CalendarConfig{OrgID: sess.OrgID, Provider: p.Key, Credentials: map[string]string{}, Enabled: true}
		} else if err != nil {
			return err
		}
		// 只看不做（ADR 0025）
		if sess.Write.DryRun {
			return dryRun(sess, i18n.M("will.calendar.save", p.Title))
		}
		secrets, err := directory.DecryptSecrets(a.SecretKey, cfg.SecretsEnc)
		if err != nil {
			secrets = map[string]string{}
		}
		var changed []string
		creds := map[string]string{}
		for k, v := range cfg.Credentials {
			creds[k] = v
		}
		for _, f := range calendarFieldsOf(p) {
			val := strings.TrimSpace(in.Credentials[f.Key])
			if f.Secret {
				if val != "" {
					secrets[f.Key] = val
					changed = append(changed, f.Key)
				}
				continue
			}
			if _, given := in.Credentials[f.Key]; !given {
				continue
			}
			if val != creds[f.Key] {
				changed = append(changed, f.Key)
			}
			creds[f.Key] = val
		}
		cfg.Credentials = creds
		if cfg.SecretsEnc, err = directory.EncryptSecrets(a.SecretKey, secrets); err != nil {
			return err
		}
		if in.ProxyURL != nil {
			if v := strings.TrimSpace(*in.ProxyURL); v != cfg.ProxyURL {
				cfg.ProxyURL = v
				changed = append(changed, "proxy_url")
			}
		}
		if in.Enabled != nil && *in.Enabled != cfg.Enabled {
			cfg.Enabled = *in.Enabled
			changed = append(changed, "enabled")
		}
		if err := a.Store.UpsertCalendarConfig(ctx, tx, cfg); err != nil {
			return err
		}
		sort.Strings(changed)
		if changed == nil {
			changed = []string{}
		}
		// 动态只记改了哪些字段的名字，不记值
		if err := a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "CalendarConfigured", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"provider": p.Key, "fields": changed, "fresh": fresh}}}); err != nil {
			return err
		}
		out, err = a.calendarView(ctx, tx, sess, p, cfg)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DisconnectCalendar 断开一家提供方：连接、成员绑定、同步来的事件一起清掉。
func (a *App) DisconnectCalendar(ctx context.Context, sess *Session, provider string) error {
	if err := a.requireOrgSettings(sess); err != nil {
		return err
	}
	p, ok := calendarProvider(provider)
	if !ok {
		return Bad("err.calendar_provider", provider)
	}
	return a.tx(ctx, sess, func(tx pgx.Tx) error {
		if _, err := a.Store.CalendarConfig(ctx, tx, p.Key); err == store.ErrNotFound {
			return NotFound("err.calendar_not_configured", p.Title)
		} else if err != nil {
			return err
		}
		if sess.Write.DryRun {
			return dryRun(sess, i18n.M("will.calendar.disconnect", p.Title))
		}
		if err := a.Store.DeleteCalendarConfig(ctx, tx, p.Key); err != nil {
			return err
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "CalendarDisconnected", ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{"provider": p.Key}}})
	})
}

// calendarClient 用拼齐的凭据建客户端；memberCreds 是成员自己的（Google 的刷新令牌）。
func (a *App) calendarClient(ctx context.Context, tx pgx.Tx, orgID string, p directory.Provider, cfg *store.CalendarConfig, memberCreds map[string]string) (directory.Calendar, error) {
	creds, _, _, err := a.calendarCreds(ctx, tx, orgID, p, cfg)
	if err != nil {
		return nil, err
	}
	for k, v := range memberCreds {
		creds[k] = v
	}
	if !calendarConfigured(p, creds) {
		return nil, Bad("err.calendar_not_configured", p.Title)
	}
	proxy := ""
	if cfg != nil {
		proxy = cfg.ProxyURL
	}
	cal, err := p.NewCalendar(creds, directory.Options{ProxyURL: proxy})
	if err != nil {
		return nil, Bad("err.directory_proxy", proxy)
	}
	return cal, nil
}

// TestCalendar 接入检查：提供方逐项给结论；没有诊断能力的提供方就拉一次今天的日程试试。
func (a *App) TestCalendar(ctx context.Context, sess *Session, provider string) (*CalendarTestResult, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	p, ok := calendarProvider(provider)
	if !ok {
		return nil, Bad("err.calendar_provider", provider)
	}
	loc := sess.Loc()
	var cal directory.Calendar
	if err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		cfg, err := a.Store.CalendarConfig(ctx, tx, p.Key)
		if err != nil && err != store.ErrNotFound {
			return err
		}
		if err == store.ErrNotFound {
			cfg = nil
		}
		cal, err = a.calendarClient(ctx, tx, sess.OrgID, p, cfg, nil)
		return err
	}); err != nil {
		return nil, err
	}
	res := &CalendarTestResult{Checks: []CheckView{}}
	if d, ok := cal.(directory.CalendarDiagnoser); ok {
		checks, err := d.DiagnoseCalendar(ctx)
		if err != nil {
			res.Error = providerMsg(p, err).Render(loc)
			return res, nil
		}
		res.OK = true
		for _, c := range checks {
			if c.Status == directory.CheckBlocked {
				res.OK = false
			}
			res.Checks = append(res.Checks, checkView(c, loc))
		}
		return res, nil
	}
	if p.PerMemberCalendar {
		// 成员各自授权的提供方：组织级只能检查客户端配置齐不齐
		res.OK = true
		res.Checks = append(res.Checks, CheckView{Key: "client", Title: i18n.Tr(loc, "calendar.check.client"), Status: "ok", Detail: i18n.Tr(loc, "calendar.check.client_ok")})
		return res, nil
	}
	now := time.Now()
	if _, err := cal.Events(ctx, "", now, now.Add(time.Hour)); err != nil {
		res.Error = providerMsg(p, err).Render(loc)
		return res, nil
	}
	res.OK = true
	return res, nil
}

// ---------- 成员绑定 ----------

// MyCalendars 成员自己看各家提供方连没连。
func (a *App) MyCalendars(ctx context.Context, sess *Session) ([]MyCalendarView, error) {
	if sess.IsAgent() {
		return nil, Forbidden("err.calendar_member_only")
	}
	loc := sess.Loc()
	out := []MyCalendarView{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		provs, byKey, err := a.calendarProvidersFor(ctx, tx)
		if err != nil {
			return err
		}
		for _, p := range provs {
			v := MyCalendarView{Provider: p.Key, ProviderTitle: p.Title.In(loc), PerMember: p.PerMemberCalendar, SelfService: p.SelfServiceCalendar}
			cfg := byKey[p.Key]
			if p.SelfServiceCalendar {
				// 不用组织配置：字段、前置说明、提醒都给成员自己看
				v.OrgConfigured = true
				for _, f := range p.CalendarFields {
					v.Fields = append(v.Fields, FieldView{Key: f.Key, Title: f.Title.In(loc), Secret: f.Secret, Optional: f.Optional, Placeholder: f.Placeholder, Hint: f.Hint.In(loc)})
				}
				for _, x := range p.CalendarPrerequisites {
					v.Prerequisites = append(v.Prerequisites, x.In(loc))
				}
				if txt := p.Tip.Text.In(loc); txt != "" {
					v.Tip = &GuideView{Text: txt, URL: p.Tip.URL}
				}
			} else if cfg != nil {
				creds, _, _, err := a.calendarCreds(ctx, tx, sess.OrgID, p, cfg)
				if err == nil {
					v.OrgConfigured = cfg.Enabled && calendarConfigured(p, creds)
				}
			}
			if id, err := a.Store.CalendarIdentityOf(ctx, tx, p.Key, sess.MemberID); err == nil {
				v.Connected, v.Email, v.ExternalID, v.Label = true, id.Email, id.ExternalUserID, id.Label
				at := id.ConnectedAt
				v.ConnectedAt = &at
				v.LastSyncAt, v.LastStatus, v.LastError = id.LastSyncAt, id.LastStatus, id.LastError
				if id.LastStatus != "" {
					v.LastStatusTitle = i18n.Tr(loc, "directory.status."+id.LastStatus)
				}
			} else if !p.PerMemberCalendar {
				// 飞书 / 企业微信：沿用外部目录的身份
				if ext := a.externalUserOf(ctx, tx, p.Key, sess.MemberID); ext != "" {
					v.Connected, v.ExternalID = true, ext
				}
			}
			if p.PerMemberCalendar && v.OrgConfigured && !v.Connected {
				if u, err := a.googleAuthURL(ctx, tx, sess, p, cfg); err == nil {
					v.AuthURL = u
				}
			}
			out = append(out, v)
		}
		return nil
	})
	return out, err
}

// externalUserOf 成员在某家 IM 提供方的外部身份（外部目录同步时记下的）。
func (a *App) externalUserOf(ctx context.Context, tx pgx.Tx, provider, memberID string) string {
	ids, err := a.Store.ListExternalIdentities(ctx, tx, provider)
	if err != nil {
		return ""
	}
	for _, x := range ids {
		if x.Kind == "member" && x.LocalID == memberID {
			return x.ExternalID
		}
	}
	return ""
}

// DisconnectMyCalendar 成员解除自己在某家提供方的绑定（Google 的令牌一并删）。
func (a *App) DisconnectMyCalendar(ctx context.Context, sess *Session, provider string) error {
	if sess.IsAgent() {
		return Forbidden("err.calendar_member_only")
	}
	p, ok := calendarProvider(provider)
	if !ok {
		return Bad("err.calendar_provider", provider)
	}
	return a.tx(ctx, sess, func(tx pgx.Tx) error {
		if _, err := a.Store.CalendarIdentityOf(ctx, tx, p.Key, sess.MemberID); err == store.ErrNotFound {
			return NotFound("err.calendar_not_connected", p.Title)
		} else if err != nil {
			return err
		}
		if sess.Write.DryRun {
			return dryRun(sess, i18n.M("will.calendar.me_disconnect", p.Title))
		}
		if err := a.Store.DeleteCalendarIdentity(ctx, tx, p.Key, sess.MemberID); err != nil {
			return err
		}
		// 自助提供方的连接行是第一个人连上时自动建的：最后一个人断开就收掉，后台巡检不再空跑（一条语句判空，不会误删别人的绑定）
		if p.SelfServiceCalendar {
			if err := a.Store.DeleteCalendarConfigIfUnused(ctx, tx, p.Key); err != nil {
				return err
			}
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "CalendarIdentityRemoved", ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{"provider": p.Key}}})
	})
}

// ConnectMyCalendar 成员自己连一家自助的提供方（日历订阅链接）：链接过出网守卫、先拉一次证明能读，
// 然后加密存进绑定、立刻同步。没有组织级连接行时建一行（空凭据、启用），后台巡检靠它到点。
func (a *App) ConnectMyCalendar(ctx context.Context, sess *Session, provider string, creds map[string]string) (*MyCalendarView, error) {
	if sess.IsAgent() {
		return nil, Forbidden("err.calendar_member_only")
	}
	p, ok := calendarProvider(provider)
	if !ok || !p.SelfServiceCalendar {
		return nil, Bad("err.calendar_provider", provider)
	}
	clean := map[string]string{}
	for _, f := range p.CalendarFields {
		v := strings.TrimSpace(creds[f.Key])
		switch f.Key {
		case "url":
			v = directory.NormalizeICSURL(v)
		case "server":
			if v != "" && !strings.Contains(v, "://") {
				v = "https://" + v
			}
		}
		if v == "" && !f.Optional {
			return nil, Bad("err.calendar_field_required", f.Title)
		}
		clean[f.Key] = v
	}
	// 服务器会去访问的地址（订阅链接、CalDAV 服务器）都要过出网守卫
	for _, key := range []string{"url", "server"} {
		if u := clean[key]; u != "" {
			if err := directory.CheckEgressURL(u, directory.DefaultEgress()); err != nil {
				return nil, egressErr(err, u)
			}
		}
	}
	if sess.Write.DryRun {
		return nil, dryRun(sess, i18n.M("will.calendar.me_connect", p.Title))
	}
	cal, err := p.NewCalendar(clean, directory.Options{})
	if err != nil {
		return nil, Bad("err.calendar_link_bad", p.Title)
	}
	now := time.Now()
	if _, err := cal.Events(ctx, "", now.Add(-24*time.Hour), now.Add(7*24*time.Hour)); err != nil {
		if errors.Is(err, directory.ErrNotICS) {
			return nil, Bad("err.calendar_link_not_ics")
		}
		if errors.Is(err, directory.ErrNotCalDAV) {
			return nil, Bad("err.calendar_caldav_not_dav")
		}
		var un *directory.UnreachableError
		if errors.As(err, &un) {
			return nil, Bad("err.calendar_self_unreachable", un.Err.Error())
		}
		var rj *directory.RejectedError
		if errors.As(err, &rj) {
			switch {
			case p.Key == directory.CalDAVKey && rj.Code == 401:
				return nil, Bad("err.calendar_caldav_auth")
			case p.Key == directory.CalDAVKey && rj.Code == 403:
				return nil, Bad("err.calendar_caldav_forbidden")
			case p.Key == directory.CalDAVKey && rj.Code == 404:
				return nil, Bad("err.calendar_caldav_none")
			}
			return nil, Bad("err.calendar_link_http", rj.Code)
		}
		return nil, &UserError{Status: 400, Reasons: []i18n.Msg{providerMsg(p, err)}}
	}
	label := ""
	if n, ok := cal.(directory.CalendarNamer); ok {
		label = n.CalendarName()
	}
	enc, err := directory.EncryptSecrets(a.SecretKey, clean)
	if err != nil {
		return nil, err
	}
	if err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		if _, err := a.Store.CalendarConfig(ctx, tx, p.Key); err == store.ErrNotFound {
			if err := a.Store.UpsertCalendarConfig(ctx, tx, &store.CalendarConfig{OrgID: sess.OrgID, Provider: p.Key, Credentials: map[string]string{}, Enabled: true}); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if err := a.Store.UpsertCalendarIdentity(ctx, tx, &store.CalendarIdentity{OrgID: sess.OrgID, Provider: p.Key, MemberID: sess.MemberID, SecretsEnc: enc, Label: label}); err != nil {
			return err
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "CalendarIdentityBound", ActorID: sess.Actor.ID, At: now, Data: map[string]any{"provider": p.Key}}})
	}); err != nil {
		return nil, err
	}
	if err := a.syncCalendarMember(ctx, sess, p, sess.MemberID); err != nil {
		_ = a.markIdentitySync(ctx, sess, p, sess.MemberID, "failed", providerMsg(p, err).Render(sess.Loc()))
	}
	views, err := a.MyCalendars(ctx, sess)
	if err != nil {
		return nil, err
	}
	for i := range views {
		if views[i].Provider == p.Key {
			return &views[i], nil
		}
	}
	return nil, NotFound("err.calendar_provider", provider)
}

// markIdentitySync 记下某个成员这次同步的结果（个人设置里显示）。
func (a *App) markIdentitySync(ctx context.Context, sess *Session, p directory.Provider, memberID, status, errText string) error {
	return a.tx(ctx, sess, func(tx pgx.Tx) error {
		return a.Store.MarkCalendarIdentitySync(ctx, tx, p.Key, memberID, status, errText, time.Now())
	})
}

// ---------- Google：成员各自授权 ----------

// googleState 是一次授权的一次性 state：谁在授权、哪个组织，十五分钟过期。
type googleState struct {
	OrgID, MemberID, Provider string
	ExpiresAt                 time.Time
}

var googleStates sync.Map

func (a *App) googleRedirectURL() string {
	return strings.TrimRight(a.PublicURL, "/") + "/api/v1/me/calendars/google/callback"
}

func (a *App) googleAuthURL(ctx context.Context, tx pgx.Tx, sess *Session, p directory.Provider, cfg *store.CalendarConfig) (string, error) {
	cal, err := a.calendarClient(ctx, tx, sess.OrgID, p, cfg, map[string]string{"refresh_token": "-"})
	if err != nil {
		return "", err
	}
	oc, ok := cal.(directory.OAuthCalendar)
	if !ok {
		return "", Bad("err.calendar_provider", p.Key)
	}
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	state := base64.RawURLEncoding.EncodeToString(buf)
	googleStates.Store(state, googleState{OrgID: sess.OrgID, MemberID: sess.MemberID, Provider: p.Key, ExpiresAt: time.Now().Add(googleStateTTL)})
	return oc.AuthURL(a.googleRedirectURL(), state), nil
}

// FinishGoogleAuth 回调：用 state 认出是谁，用 code 换刷新令牌并加密存下，返回该去的页面。
func (a *App) FinishGoogleAuth(ctx context.Context, state, code string) (string, error) {
	v, ok := googleStates.LoadAndDelete(state)
	st, _ := v.(googleState)
	if !ok || time.Now().After(st.ExpiresAt) {
		return "", Bad("err.calendar_state")
	}
	p, ok := calendarProvider(st.Provider)
	if !ok {
		return "", Bad("err.calendar_provider", st.Provider)
	}
	sess, err := a.memberSession(ctx, st.OrgID, st.MemberID)
	if err != nil {
		return "", err
	}
	var refresh, email string
	if err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		cfg, err := a.Store.CalendarConfig(ctx, tx, p.Key)
		if err != nil {
			return NotFound("err.calendar_not_configured", p.Title)
		}
		cal, err := a.calendarClient(ctx, tx, sess.OrgID, p, cfg, map[string]string{"refresh_token": "-"})
		if err != nil {
			return err
		}
		oc, ok := cal.(directory.OAuthCalendar)
		if !ok {
			return Bad("err.calendar_provider", p.Key)
		}
		refresh, email, err = oc.Exchange(ctx, code, a.googleRedirectURL())
		if err != nil {
			return &UserError{Status: 400, Reasons: []i18n.Msg{providerMsg(p, err)}}
		}
		enc, err := directory.EncryptSecrets(a.SecretKey, map[string]string{"refresh_token": refresh})
		if err != nil {
			return err
		}
		if err := a.Store.UpsertCalendarIdentity(ctx, tx, &store.CalendarIdentity{OrgID: sess.OrgID, Provider: p.Key, MemberID: sess.MemberID, Email: email, SecretsEnc: enc}); err != nil {
			return err
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "CalendarIdentityBound", ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{"provider": p.Key}}})
	}); err != nil {
		return "", err
	}
	// 连上后立刻同步一次这个成员的
	_ = a.syncCalendarMember(ctx, sess, p, sess.MemberID)
	return strings.TrimRight(a.PublicURL, "/") + "/me/?calendar=connected", nil
}

// SweepGoogleStates 清掉过期的授权 state（后台巡检）。
func (a *App) SweepGoogleStates(now time.Time) {
	googleStates.Range(func(k, v any) bool {
		if st, ok := v.(googleState); ok && now.After(st.ExpiresAt) {
			googleStates.Delete(k)
		}
		return true
	})
}

// ---------- 同步 ----------

// SyncCalendar 手动同步一家提供方（组织设置）。
func (a *App) SyncCalendar(ctx context.Context, sess *Session, provider string) (*CalendarSyncResult, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	p, ok := calendarProvider(provider)
	if !ok {
		return nil, Bad("err.calendar_provider", provider)
	}
	if err := refuseDryRun(sess); err != nil {
		return nil, err
	}
	return a.syncCalendar(ctx, sess, p)
}

// calendarMembers 这家提供方要同步哪些成员：成员显式绑定的 + 外部目录里对上的（飞书 / 企业微信）。
// 返回 成员 → {外部身份, 成员自己的凭据}。
type calendarTarget struct {
	ExternalID string
	Creds      map[string]string
}

func (a *App) calendarTargets(ctx context.Context, tx pgx.Tx, p directory.Provider) (map[string]calendarTarget, error) {
	out := map[string]calendarTarget{}
	ids, err := a.Store.ListCalendarIdentities(ctx, tx, p.Key)
	if err != nil {
		return nil, err
	}
	for _, x := range ids {
		t := calendarTarget{ExternalID: x.ExternalUserID, Creds: map[string]string{}}
		if len(x.SecretsEnc) > 0 {
			if m, err := directory.DecryptSecrets(a.SecretKey, x.SecretsEnc); err == nil {
				t.Creds = m
			}
		}
		out[x.MemberID] = t
	}
	if !p.PerMemberCalendar {
		ext, err := a.Store.ListExternalIdentities(ctx, tx, p.Key)
		if err != nil {
			return nil, err
		}
		for _, x := range ext {
			if x.Kind != "member" {
				continue
			}
			if _, has := out[x.LocalID]; !has {
				out[x.LocalID] = calendarTarget{ExternalID: x.ExternalID, Creds: map[string]string{}}
			}
		}
	}
	return out, nil
}

// syncCalendar 同步一家提供方的全部成员：每个成员各自一笔事务，一个人失败不影响别人；结果记在连接上。
func (a *App) syncCalendar(ctx context.Context, sess *Session, p directory.Provider) (*CalendarSyncResult, error) {
	res := &CalendarSyncResult{Provider: p.Key, Errors: []string{}}
	var cfg *store.CalendarConfig
	var targets map[string]calendarTarget
	if err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		if cfg, err = a.Store.CalendarConfig(ctx, tx, p.Key); err == store.ErrNotFound {
			return NotFound("err.calendar_not_configured", p.Title)
		} else if err != nil {
			return err
		}
		if !cfg.Enabled {
			return Bad("err.calendar_disabled", p.Title)
		}
		targets, err = a.calendarTargets(ctx, tx, p)
		return err
	}); err != nil {
		return nil, err
	}
	members := make([]string, 0, len(targets))
	for m := range targets {
		members = append(members, m)
	}
	sort.Strings(members)
	loc := sess.Loc()
	for _, m := range members {
		n, err := a.syncOne(ctx, sess, p, cfg, m, targets[m])
		res.Members++
		res.Events += n
		if err != nil {
			name := m
			_ = a.tx(ctx, sess, func(tx pgx.Tx) error { name = a.executorName(ctx, tx, m); return nil })
			msg := providerMsg(p, err).Render(loc)
			_ = a.markIdentitySync(ctx, sess, p, m, "failed", msg)
			if p.SelfServiceCalendar {
				// 自助的：原因只记在本人的绑定上，组织级的动态只说谁失败了
				msg = i18n.Tr(loc, "directory.status.failed")
			}
			res.Errors = append(res.Errors, name+"："+msg)
		} else {
			_ = a.markIdentitySync(ctx, sess, p, m, "ok", "")
		}
	}
	res.Status = "ok"
	if len(res.Errors) > 0 {
		res.Status = "failed"
	}
	errText := strings.Join(res.Errors, "；")
	if err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		if err := a.Store.MarkCalendarSync(ctx, tx, p.Key, res.Status, errText, time.Now()); err != nil {
			return err
		}
		return a.Store.InsertEvents(ctx, tx, sess.OrgID, []domain.Event{{Type: "CalendarSyncRan", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"provider": p.Key, "status": res.Status, "members": res.Members, "events": res.Events, "error": errText}}})
	}); err != nil {
		return nil, err
	}
	return res, nil
}

// syncCalendarMember 只同步一个成员（连上 Google 之后立刻拉一次）。
func (a *App) syncCalendarMember(ctx context.Context, sess *Session, p directory.Provider, memberID string) error {
	var cfg *store.CalendarConfig
	var targets map[string]calendarTarget
	if err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		if cfg, err = a.Store.CalendarConfig(ctx, tx, p.Key); err != nil {
			return err
		}
		targets, err = a.calendarTargets(ctx, tx, p)
		return err
	}); err != nil {
		return err
	}
	t, ok := targets[memberID]
	if !ok {
		return nil
	}
	_, err := a.syncOne(ctx, sess, p, cfg, memberID, t)
	if err == nil {
		_ = a.markIdentitySync(ctx, sess, p, memberID, "ok", "")
	}
	return err
}

func (a *App) syncOne(ctx context.Context, sess *Session, p directory.Provider, cfg *store.CalendarConfig, memberID string, t calendarTarget) (int, error) {
	now := time.Now()
	from, to := now.Add(-calendarSyncBack), now.Add(calendarSyncAhead)
	var cal directory.Calendar
	if err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		cal, err = a.calendarClient(ctx, tx, sess.OrgID, p, cfg, t.Creds)
		return err
	}); err != nil {
		return 0, err
	}
	events, err := cal.Events(ctx, t.ExternalID, from, to)
	if err != nil {
		return 0, err
	}
	rows := make([]store.CalendarEventRow, 0, len(events))
	for _, e := range events {
		if e.ExternalID == "" || !e.End.After(e.Start) && !e.AllDay {
			continue
		}
		rows = append(rows, store.CalendarEventRow{ExternalID: e.ExternalID, Title: e.Title, StartsAt: e.Start, EndsAt: e.End, AllDay: e.AllDay, Busy: e.Busy, URL: e.URL})
	}
	err = a.tx(ctx, sess, func(tx pgx.Tx) error {
		return a.Store.ReplaceCalendarEvents(ctx, tx, sess.OrgID, p.Key, memberID, from, to, rows)
	})
	return len(rows), err
}

// RunScheduledCalendarSyncs 给每家到点的连接跑一次同步（后台巡检每分钟调用，实际每 15 分钟一轮）。
func (a *App) RunScheduledCalendarSyncs(ctx context.Context, now time.Time) []CalendarSyncResult {
	due, err := a.Store.ScheduledCalendars(ctx, a.Store.Pool)
	if err != nil {
		return nil
	}
	var out []CalendarSyncResult
	for orgID, provs := range due {
		sess := a.systemSession(ctx, orgID)
		for _, key := range provs {
			p, ok := calendarProvider(key)
			if !ok {
				continue
			}
			var last *time.Time
			_ = a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
				if cfg, err := a.Store.CalendarConfig(ctx, tx, key); err == nil {
					last = cfg.LastSyncAt
				}
				return nil
			})
			if last != nil && now.Sub(*last) < calendarSyncInterval {
				continue
			}
			res, err := a.syncCalendar(ctx, sess, p)
			if err != nil {
				var ue *UserError
				if !errors.As(err, &ue) {
					continue
				}
				res = &CalendarSyncResult{Provider: key, Status: "failed", Errors: []string{ue.Error()}}
			}
			r := *res
			r.Errors = append([]string{orgID}, r.Errors...)
			out = append(out, r)
		}
	}
	return out
}
