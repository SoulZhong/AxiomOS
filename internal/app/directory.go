package app

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/directory"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 外部目录同步（ADR 0017）：外部目录（飞书、企业微信…）是团队树与人员名单的来源。
// 这里负责：配置（凭据按提供方声明校验、保密字段加密入库）、连通性测试、拉取、对照（内核 domain.PlanDirectory）、
// 写入、运行记录与动态。提供方来自 directory 包的注册表，这一层没有任何平台名的分支（ADR 0017 补记）。

// 同步频率。
var directorySchedules = []string{"manual", "hourly", "daily"}

// DirectoryConfigInput 是 PUT /org/directory 的输入。
// provider 省略表示沿用现有提供方（首次保存必填）；credentials 里保密字段省略或为空表示不改（首次保存必填）。
type DirectoryConfigInput struct {
	Provider         *string           `json:"provider"`
	Credentials      map[string]string `json:"credentials"`
	RootDepartmentID *string           `json:"root_department_id"`
	// RootDepartmentIDs 是同步根部门列表（ADR 0017 补记二）：空列表表示整个企业；给了它就忽略 root_department_id。
	RootDepartmentIDs *[]string `json:"root_department_ids"`
	DefaultRole       *string   `json:"default_role"`
	Schedule          *string   `json:"schedule"`
	ProxyURL          *string   `json:"proxy_url"`
}

// DirectoryRunView 是一次同步的展示。
type DirectoryRunView struct {
	ID          string     `json:"id"`
	Provider    string     `json:"provider"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	Status      string     `json:"status"`
	StatusTitle string     `json:"status_title"`
	domain.DirectoryCounts
	Errors []string `json:"errors"`
	// Invitations 是这次同步给有邮箱的新成员生成的邀请链接，只在同步返回里出现一次（与 POST /org/invitations 一致）。
	Invitations []InvitationView `json:"invitations,omitempty"`
}

func runView(r *store.DirectoryRun, loc i18n.Locale) *DirectoryRunView {
	if r == nil {
		return nil
	}
	v := &DirectoryRunView{ID: r.ID, Provider: r.Provider, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, Status: r.Status, StatusTitle: i18n.Tr(loc, "directory.status."+r.Status), DirectoryCounts: r.Counts, Errors: r.Errors}
	if v.Errors == nil {
		v.Errors = []string{}
	}
	return v
}

// ProviderView 是一个提供方的声明：前端按它渲染凭据表单与前置条件。
// GuideView 是一句给人看的提醒（凭据在哪儿找），可带控制台链接。
type GuideView struct {
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
}

type ProviderView struct {
	Key              string      `json:"key"`
	Title            string      `json:"title"`
	RootDepartmentID string      `json:"root_department_id"`
	Fields           []FieldView `json:"fields"`
	Prerequisites    []string    `json:"prerequisites"`
	Tip              *GuideView  `json:"tip,omitempty"`
}

// FieldView 是一个凭据字段。Set / Value 只对当前提供方有意义：保密字段只给 Set，非保密字段给 Value。
type FieldView struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Secret      bool   `json:"secret"`
	Optional    bool   `json:"optional"`
	Placeholder string `json:"placeholder"`
	Hint        string `json:"hint"`
	Set         bool   `json:"set"`
	Value       string `json:"value,omitempty"`
}

func providerView(p directory.Provider, cfg *store.DirectoryConfig, secretsSet map[string]bool, loc i18n.Locale) ProviderView {
	v := ProviderView{Key: p.Key, Title: p.Title.In(loc), RootDepartmentID: p.RootDepartmentID, Fields: []FieldView{}, Prerequisites: []string{}}
	if len(p.Tip.Text) > 0 {
		v.Tip = &GuideView{Text: p.Tip.Text.In(loc), URL: p.Tip.URL}
	}
	for _, f := range p.Fields {
		fv := FieldView{Key: f.Key, Title: f.Title.In(loc), Secret: f.Secret, Optional: f.Optional, Placeholder: f.Placeholder, Hint: f.Hint.In(loc)}
		if cfg != nil && cfg.Provider == p.Key {
			if f.Secret {
				fv.Set = secretsSet[f.Key]
			} else {
				fv.Value = cfg.Credentials[f.Key]
				fv.Set = fv.Value != ""
			}
		}
		v.Fields = append(v.Fields, fv)
	}
	for _, t := range p.Prerequisites {
		v.Prerequisites = append(v.Prerequisites, t.In(loc))
	}
	return v
}

// ListDirectoryProviders 列出可接入的提供方（GET /org/directory/providers）。
func (a *App) ListDirectoryProviders(ctx context.Context, sess *Session) ([]ProviderView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	out := []ProviderView{}
	for _, p := range directory.Providers() {
		out = append(out, providerView(p, nil, nil, sess.Loc()))
	}
	return out, nil
}

// DirectoryView 是 GET /org/directory 的返回；永远不包含保密字段的值。
type DirectoryView struct {
	Provider          *string           `json:"provider"`
	ProviderTitle     string            `json:"provider_title"`
	Configured        bool              `json:"configured"`
	Credentials       map[string]string `json:"credentials"` // 非保密字段的值
	SecretsSet        map[string]bool   `json:"secrets_set"` // 保密字段是否已设置
	RootDepartmentID  string            `json:"root_department_id"`
	RootDepartmentIDs []string          `json:"root_department_ids"` // 空表示整个企业
	DefaultRole       string            `json:"default_role"`
	Schedule          string            `json:"schedule"`
	ScheduleTitle     string            `json:"schedule_title"`
	Schedules         []ScheduleOption  `json:"schedules"`
	ProxyURL          string            `json:"proxy_url"`
	Providers         []ProviderView    `json:"providers"`
	LastRun           *DirectoryRunView `json:"last_run"`
}

// ScheduleOption 是同步频率的一个候选。
type ScheduleOption struct {
	Value string `json:"value"`
	Title string `json:"title"`
}

// secretsSetOf 只看密文里有哪些键，不解密内容以外的东西给界面。
func (a *App) secretsSetOf(cfg *store.DirectoryConfig) map[string]bool {
	out := map[string]bool{}
	if cfg == nil {
		return out
	}
	if m, err := directory.DecryptSecrets(a.SecretKey, cfg.SecretsEnc); err == nil {
		for k, v := range m {
			out[k] = v != ""
		}
	}
	return out
}

// directoryConfigured 判断凭据是否齐全：每个声明的字段都有值（保密字段看是否已设置）。
func directoryConfigured(p directory.Provider, cfg *store.DirectoryConfig, secretsSet map[string]bool) bool {
	if cfg == nil || cfg.Provider != p.Key {
		return false
	}
	for _, f := range p.Fields {
		if f.Secret && !secretsSet[f.Key] || !f.Secret && strings.TrimSpace(cfg.Credentials[f.Key]) == "" {
			return false
		}
	}
	return true
}

func (a *App) directoryView(cfg *store.DirectoryConfig, last *store.DirectoryRun, loc i18n.Locale) *DirectoryView {
	v := &DirectoryView{Schedule: "manual", Schedules: []ScheduleOption{}, Credentials: map[string]string{}, SecretsSet: map[string]bool{}, Providers: []ProviderView{}, RootDepartmentIDs: []string{}}
	for _, s := range directorySchedules {
		v.Schedules = append(v.Schedules, ScheduleOption{Value: s, Title: i18n.Tr(loc, "directory.schedule."+s)})
	}
	secretsSet := a.secretsSetOf(cfg)
	if cfg != nil {
		p := cfg.Provider
		v.Provider = &p
		v.RootDepartmentID, v.DefaultRole, v.Schedule, v.ProxyURL = cfg.RootDepartmentID, cfg.DefaultRole, cfg.Schedule, cfg.ProxyURL
		v.RootDepartmentIDs = append(v.RootDepartmentIDs, cfg.RootDepartmentIDs...)
		v.SecretsSet = secretsSet
		if prov, ok := directory.Lookup(cfg.Provider); ok {
			v.ProviderTitle = prov.Title.In(loc)
			v.Configured = directoryConfigured(prov, cfg, secretsSet)
			for _, f := range prov.Fields {
				if !f.Secret {
					v.Credentials[f.Key] = cfg.Credentials[f.Key]
				}
			}
		} else {
			v.ProviderTitle = cfg.Provider
		}
	}
	for _, p := range directory.Providers() {
		v.Providers = append(v.Providers, providerView(p, cfg, secretsSet, loc))
	}
	if v.Schedule == "" {
		v.Schedule = "manual"
	}
	v.ScheduleTitle = i18n.Tr(loc, "directory.schedule."+v.Schedule)
	v.LastRun = runView(last, loc)
	return v
}

// loadDirectoryConfig 读配置；读到 0009 期旧格式的行（app_id / app_secret_enc）时就地升级成凭据对象并写回（懒升级，只发生一次）。
// 旧密文解不开（服务端密钥换过）时只搬 app_id，保密字段留空让人重填。
func (a *App) loadDirectoryConfig(ctx context.Context, tx pgx.Tx, orgID string) (*store.DirectoryConfig, error) {
	cfg, err := a.Store.DirectoryConfig(ctx, tx, orgID)
	if err != nil {
		return nil, err
	}
	if !cfg.NeedsUpgrade() {
		return cfg, nil
	}
	if cfg.LegacyAppID != "" {
		cfg.Credentials["app_id"] = cfg.LegacyAppID
	}
	if len(cfg.LegacyAppSecretEnc) > 0 {
		if plain, err := directory.Decrypt(a.SecretKey, cfg.LegacyAppSecretEnc); err == nil && plain != "" {
			if cfg.SecretsEnc, err = directory.EncryptSecrets(a.SecretKey, map[string]string{"app_secret": plain}); err != nil {
				return nil, err
			}
		}
	}
	if err := a.Store.UpsertDirectoryConfig(ctx, tx, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// GetDirectoryConfig 读取配置（不含保密字段的值）。
func (a *App) GetDirectoryConfig(ctx context.Context, sess *Session) (*DirectoryView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	var out *DirectoryView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		cfg, err := a.loadDirectoryConfig(ctx, tx, sess.OrgID)
		if err != nil && err != store.ErrNotFound {
			return err
		}
		last, err := a.Store.LastDirectoryRun(ctx, tx)
		if err != nil && err != store.ErrNotFound {
			return err
		}
		out = a.directoryView(cfg, last, sess.Loc())
		return nil
	})
	return out, err
}

// SaveDirectoryConfig 写配置：凭据按提供方声明逐字段校验，保密字段加密入库；产生动态 DirectoryConfigured（不含任何保密值）。
// 换提供方等于换来源：凭据与根部门重置为新提供方的，旧外部身份保留但不再匹配（ADR 0017 补记）。
// DisconnectDirectory 断开 IM 集成：删掉配置，回到未接入。
// 已经同步进来的团队与成员、外部身份、历次同步记录都保留；要改成手工维护就去「对应关系」里解绑。
func (a *App) DisconnectDirectory(ctx context.Context, sess *Session) (*DirectoryView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	if err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		c, err := a.Store.DirectoryConfig(ctx, tx, sess.OrgID)
		if err == store.ErrNotFound {
			return nil
		}
		if err != nil {
			return err
		}
		if err := a.Store.DeleteDirectoryConfig(ctx, tx, sess.OrgID); err != nil {
			return err
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "DirectoryDisconnected", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"provider": c.Provider}}})
	}); err != nil {
		return nil, err
	}
	return a.GetDirectoryConfig(ctx, sess)
}

func (a *App) SaveDirectoryConfig(ctx context.Context, sess *Session, in DirectoryConfigInput) (*DirectoryView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	if in.Schedule != nil && !contains(directorySchedules, strings.TrimSpace(*in.Schedule)) {
		return nil, Bad("err.directory_schedule", *in.Schedule)
	}
	if in.ProxyURL != nil && strings.TrimSpace(*in.ProxyURL) != "" {
		u, err := url.Parse(strings.TrimSpace(*in.ProxyURL))
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, Bad("err.directory_proxy", *in.ProxyURL)
		}
	}
	var out *DirectoryView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		cfg, err := a.loadDirectoryConfig(ctx, tx, sess.OrgID)
		fresh := err == store.ErrNotFound
		if fresh {
			cfg = &store.DirectoryConfig{OrgID: sess.OrgID, Schedule: "manual", Credentials: map[string]string{}}
		} else if err != nil {
			return err
		}
		// 提供方
		key := cfg.Provider
		if in.Provider != nil && strings.TrimSpace(*in.Provider) != "" {
			key = strings.ToLower(strings.TrimSpace(*in.Provider))
		}
		if key == "" {
			return Bad("err.directory_provider_required")
		}
		prov, ok := directory.Lookup(key)
		if !ok {
			return Bad("err.directory_provider", key)
		}
		switched := !fresh && cfg.Provider != key
		if fresh || switched {
			cfg.Provider = key
			cfg.Credentials = map[string]string{}
			cfg.SecretsEnc = nil
			cfg.RootDepartmentID = prov.RootDepartmentID
			cfg.RootDepartmentIDs = []string{}
		}
		// 凭据：按声明逐字段
		secrets, err := directory.DecryptSecrets(a.SecretKey, cfg.SecretsEnc)
		if err != nil {
			// 服务端密钥解不开旧密文：只能全部重填
			secrets = map[string]string{}
			for _, f := range prov.Fields {
				if f.Secret && strings.TrimSpace(in.Credentials[f.Key]) == "" {
					return Bad("err.directory_secret_key")
				}
			}
		}
		var secretsChanged, fieldsChanged []string
		creds := map[string]string{}
		for _, f := range prov.Fields {
			val := strings.TrimSpace(in.Credentials[f.Key])
			if f.Secret {
				if val != "" {
					secrets[f.Key] = val
					secretsChanged = append(secretsChanged, f.Key)
				}
				if secrets[f.Key] == "" {
					return Bad("err.directory_secret_required", f.Title)
				}
				continue
			}
			if val == "" {
				val = cfg.Credentials[f.Key]
			} else {
				if isURLField(f.Key) {
					if err := checkEgress(val); err != nil {
						return err
					}
				}
				if val != cfg.Credentials[f.Key] {
					fieldsChanged = append(fieldsChanged, f.Key)
				}
			}
			if val == "" {
				return Bad("err.directory_field_required", f.Title)
			}
			creds[f.Key] = val
		}
		// 只保留声明过的键
		for k := range secrets {
			if f, ok := prov.Field(k); !ok || !f.Secret {
				delete(secrets, k)
			}
		}
		cfg.Credentials = creds
		if cfg.SecretsEnc, err = directory.EncryptSecrets(a.SecretKey, secrets); err != nil {
			return err
		}
		// 同步根：给了列表用列表；只给单个时当作长度为一的列表；提供方的根等于"整个企业"，不进列表
		if in.RootDepartmentIDs != nil {
			cfg.RootDepartmentIDs = cleanRoots(*in.RootDepartmentIDs, prov.RootDepartmentID)
		} else if in.RootDepartmentID != nil {
			cfg.RootDepartmentIDs = cleanRoots([]string{*in.RootDepartmentID}, prov.RootDepartmentID)
		}
		cfg.RootDepartmentID = prov.RootDepartmentID
		if len(cfg.RootDepartmentIDs) > 0 {
			cfg.RootDepartmentID = cfg.RootDepartmentIDs[0]
		}
		if in.DefaultRole != nil {
			role := strings.TrimSpace(*in.DefaultRole)
			if role != "" {
				roles, err := a.Store.ListRoles(ctx, tx)
				if err != nil {
					return err
				}
				known := false
				for _, r := range roles {
					known = known || r.Name == role
				}
				if !known {
					return Bad("err.role_unknown", role)
				}
			}
			cfg.DefaultRole = role
		}
		if in.Schedule != nil {
			cfg.Schedule = strings.TrimSpace(*in.Schedule)
		}
		proxyChanged := false
		if in.ProxyURL != nil && strings.TrimSpace(*in.ProxyURL) != cfg.ProxyURL {
			if p := strings.TrimSpace(*in.ProxyURL); p != "" {
				if err := checkProxy(p); err != nil {
					return err
				}
			}
			cfg.ProxyURL = strings.TrimSpace(*in.ProxyURL)
			proxyChanged = true
		}
		if err := a.Store.UpsertDirectoryConfig(ctx, tx, cfg); err != nil {
			return err
		}
		last, err := a.Store.LastDirectoryRun(ctx, tx)
		if err != nil && err != store.ErrNotFound {
			return err
		}
		out = a.directoryView(cfg, last, sess.Loc())
		// 动态只记改了哪些字段的名字，不记值：接口地址、用户名与代理地址都可能暴露内网拓扑（Codex 审查）。
		fields := append(append([]string{}, fieldsChanged...), secretsChanged...)
		if proxyChanged {
			fields = append(fields, "proxy_url")
		}
		sort.Strings(fields)
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "DirectoryConfigured", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"provider": cfg.Provider, "provider_switched": switched, "fields": fields,
				"root_department_id": cfg.RootDepartmentID, "root_department_ids": cfg.RootDepartmentIDs, "default_role": cfg.DefaultRole, "schedule": cfg.Schedule}}})
	})
	return out, err
}

// directoryClient 读配置、解密凭据、按提供方声明建客户端。
func (a *App) directoryClient(ctx context.Context, tx pgx.Tx, orgID string) (directory.Directory, *store.DirectoryConfig, directory.Provider, error) {
	cfg, err := a.loadDirectoryConfig(ctx, tx, orgID)
	if err == store.ErrNotFound {
		return nil, nil, directory.Provider{}, Bad("err.directory_not_configured")
	}
	if err != nil {
		return nil, nil, directory.Provider{}, err
	}
	prov, ok := directory.Lookup(cfg.Provider)
	if !ok {
		return nil, nil, directory.Provider{}, Bad("err.directory_provider", cfg.Provider)
	}
	secrets, err := directory.DecryptSecrets(a.SecretKey, cfg.SecretsEnc)
	if err != nil {
		return nil, nil, directory.Provider{}, Bad("err.directory_secret_key")
	}
	secretsSet := map[string]bool{}
	for k, v := range secrets {
		secretsSet[k] = v != ""
	}
	if !directoryConfigured(prov, cfg, secretsSet) {
		return nil, nil, directory.Provider{}, Bad("err.directory_not_configured")
	}
	creds := map[string]string{}
	for k, v := range cfg.Credentials {
		creds[k] = v
	}
	for k, v := range secrets {
		creds[k] = v
	}
	dir, err := prov.New(creds, directory.Options{ProxyURL: cfg.ProxyURL})
	if err != nil {
		return nil, nil, directory.Provider{}, Bad("err.directory_proxy", cfg.ProxyURL)
	}
	return dir, cfg, prov, nil
}

// providerMsg 把提供方错误翻成给人看的句子（带提供方名字）。
func providerMsg(prov directory.Provider, err error) i18n.Msg {
	var rj *directory.RejectedError
	if errors.As(err, &rj) {
		return i18n.M("err.directory_rejected", prov.Title, rj.Msg)
	}
	var un *directory.UnreachableError
	if errors.As(err, &un) {
		return i18n.M("err.directory_unreachable", prov.Title, un.Err.Error())
	}
	return i18n.M("err.directory_unreachable", prov.Title, err.Error())
}

// cleanRoots 整理同步根列表：去空白、去重、去掉提供方的根（它表示整个企业，不需要列出）。
func cleanRoots(ids []string, providerRoot string) []string {
	out := []string{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || id == providerRoot || contains(out, id) {
			continue
		}
		out = append(out, id)
	}
	return out
}

// syncRoots 是实际生效的同步根列表：配置里的列表；为空时回退到旧的单个根（不是提供方根时）；再为空表示整个企业。
func syncRoots(cfg *store.DirectoryConfig, prov directory.Provider) []string {
	if len(cfg.RootDepartmentIDs) > 0 {
		return cfg.RootDepartmentIDs
	}
	if cfg.RootDepartmentID != "" && cfg.RootDepartmentID != prov.RootDepartmentID {
		return []string{cfg.RootDepartmentID}
	}
	return nil
}

// checkRoot 是拿去做接入检查的根：第一个同步根，没有就是提供方的根。
func checkRoot(cfg *store.DirectoryConfig, prov directory.Provider) string {
	if roots := syncRoots(cfg, prov); len(roots) > 0 {
		return roots[0]
	}
	return prov.RootDepartmentID
}

// textOf 把一条词条渲染成中英文本，给应用层自己造的检查项用。
func textOf(key string, args ...any) i18n.Text {
	t := i18n.Text{}
	for _, l := range i18n.Supported {
		t[l] = i18n.M(key, args...).Render(l)
	}
	return t
}

// diagnose 让提供方跑接入检查（ADR 0017 补记二）。提供方没声明检查项时用通用的两项；连不上时合成一项阻塞的「连接提供方」。
func (a *App) diagnose(ctx context.Context, dir directory.Directory, cfg *store.DirectoryConfig, prov directory.Provider) []directory.Check {
	root := checkRoot(cfg, prov)
	var checks []directory.Check
	var err error
	if dg, ok := dir.(directory.Diagnoser); ok {
		checks, err = dg.Diagnose(ctx, root)
	} else {
		checks, err = directory.DiagnoseBasic(ctx, prov.Title, dir, root)
	}
	if err != nil {
		return []directory.Check{{Key: "connection", Title: textOf("directory.check.connection"), Status: directory.CheckBlocked, Blocking: true,
			Detail: textOf("err.directory_unreachable", prov.Title, err.Error()), Fix: textOf("directory.check.connection.fix")}}
	}
	return checks
}

// blockedError 是同步与预览被检查项挡住时的拒绝理由：第一项阻塞检查观察到了什么、怎么修。
func blockedError(checks []directory.Check) error {
	b := directory.FirstBlocked(checks)
	if b == nil {
		return nil
	}
	text := i18n.Text{}
	for _, l := range i18n.Supported {
		text[l] = strings.TrimSpace(b.Detail.In(l) + " " + b.Fix.In(l))
	}
	return Bad("err.directory_blocked", text)
}

// CheckView 是一项接入检查。
type CheckView struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	Status   string `json:"status"` // ok | todo | blocked | skipped
	Detail   string `json:"detail"`
	Fix      string `json:"fix"`
	FixURL   string `json:"fix_url"`
	Blocking bool   `json:"blocking"`
}

// NextStep 是向导当前该做的一步。
type NextStep struct {
	Step string `json:"step"` // credentials | checks | scope | preview | sync | schedule
	Text string `json:"text"`
}

// ChecklistView 是 GET /org/directory/checklist 的返回：检查项 + 向导状态。
type ChecklistView struct {
	Provider          string          `json:"provider"`
	ProviderTitle     string          `json:"provider_title"`
	ConsoleURL        string          `json:"console_url,omitempty"`
	Checks            []CheckView     `json:"checks"`
	Ready             bool            `json:"ready"` // 没有阻塞项
	Next              NextStep        `json:"next"`
	SuggestedRoots    []SuggestedRoot `json:"suggested_roots"` // 权限范围只含部分部门时，可选作同步根的部门
	RootDepartmentID  string          `json:"root_department_id"`
	RootDepartmentIDs []string        `json:"root_department_ids"`
}

func checkView(c directory.Check, loc i18n.Locale) CheckView {
	return CheckView{Key: c.Key, Title: c.Title.In(loc), Status: string(c.Status), Detail: c.Detail.In(loc), Fix: c.Fix.In(loc), FixURL: c.FixURL, Blocking: c.Blocking}
}

// DirectoryChecklist 现场跑一遍接入检查并汇总成向导状态（不缓存：每次都问提供方）。
func (a *App) DirectoryChecklist(ctx context.Context, sess *Session) (*ChecklistView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	loc := sess.Loc()
	view := &ChecklistView{Checks: []CheckView{}, SuggestedRoots: []SuggestedRoot{}, RootDepartmentIDs: []string{}}
	var dir directory.Directory
	var cfg *store.DirectoryConfig
	var prov directory.Provider
	var last *store.DirectoryRun
	err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		if last, err = a.Store.LastDirectoryRun(ctx, tx); err != nil && err != store.ErrNotFound {
			return err
		}
		if c, err := a.loadDirectoryConfig(ctx, tx, sess.OrgID); err == nil {
			if p, ok := directory.Lookup(c.Provider); ok {
				view.Provider, view.ProviderTitle, view.RootDepartmentID = p.Key, p.Title.In(loc), c.RootDepartmentID
				view.RootDepartmentIDs = append(view.RootDepartmentIDs, c.RootDepartmentIDs...)
				if p.ConsoleURL != nil {
					view.ConsoleURL = p.ConsoleURL(c.Credentials)
				}
			}
		} else if err != store.ErrNotFound {
			return err
		}
		dir, cfg, prov, err = a.directoryClient(ctx, tx, sess.OrgID)
		return err
	})
	if err != nil {
		var ue *UserError
		if errors.As(err, &ue) && len(ue.Reasons) == 1 && ue.Reasons[0].Key == "err.directory_not_configured" {
			view.Next = NextStep{Step: "credentials", Text: i18n.Tr(loc, "directory.next.credentials")}
			return view, nil
		}
		return nil, err
	}
	checks := a.diagnose(ctx, dir, cfg, prov)
	// 「能以应用身份发消息」（ADR 0019）：待处理、不阻塞；用通知通道的凭据让提供方自检，连续失败时带最近一次错误
	if b := directory.FirstBlocked(checks); b == nil || b.Key != "connection" {
		_ = a.tx(ctx, sess, func(tx pgx.Tx) error {
			if mc := a.messagingCheck(ctx, tx, sess.OrgID, prov); mc != nil {
				checks = append(checks, *mc)
			}
			return nil
		})
	}
	for _, c := range checks {
		view.Checks = append(view.Checks, checkView(c, loc))
	}
	for _, d := range directory.ScopeRoots(checks) {
		view.SuggestedRoots = append(view.SuggestedRoots, SuggestedRoot{ID: d.ID, Name: d.Name})
	}
	blocked := directory.FirstBlocked(checks)
	view.Ready = blocked == nil
	scopeTodo := false
	for _, c := range checks {
		scopeTodo = scopeTodo || c.Key == "scope" && c.Status == directory.CheckTodo
	}
	switch {
	case blocked != nil:
		text := blocked.Fix.In(loc)
		if text == "" {
			text = blocked.Detail.In(loc)
		}
		view.Next = NextStep{Step: "checks", Text: text}
	case scopeTodo && len(syncRoots(cfg, prov)) == 0:
		view.Next = NextStep{Step: "scope", Text: i18n.Tr(loc, "directory.next.scope")}
	case last == nil || last.Status == "failed":
		view.Next = NextStep{Step: "preview", Text: i18n.Tr(loc, "directory.next.preview")}
	case cfg.Schedule == "" || cfg.Schedule == "manual":
		view.Next = NextStep{Step: "schedule", Text: i18n.Tr(loc, "directory.next.schedule")}
	default:
		view.Next = NextStep{Step: "sync", Text: i18n.Tr(loc, "directory.next.sync")}
	}
	return view, nil
}

// DirectoryTestResult 是连通性测试的结果：接入检查的摘要。
type DirectoryTestResult struct {
	OK             bool            `json:"ok"`
	TenantName     string          `json:"tenant_name,omitempty"`
	DepartmentName string          `json:"department_name,omitempty"`
	Error          string          `json:"error,omitempty"`
	SuggestedRoots []SuggestedRoot `json:"suggested_roots,omitempty"` // 权限范围只含部分部门时，应用实际被授权的部门，可选作同步根
	Warnings       []string        `json:"warnings,omitempty"`        // 连上了但有不影响同步的缺项（比如读不到邮箱）
}

// SuggestedRoot 是一个可以拿来当同步根部门的已授权部门。
type SuggestedRoot struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// authorizedRoots 在根部门被拒绝时问提供方"你到底让我读哪些部门"，最多带回前 20 个的名字。
func authorizedRoots(ctx context.Context, dir directory.Directory, providerRoot string) []SuggestedRoot {
	sl, ok := dir.(directory.ScopeLister)
	if !ok {
		return nil
	}
	ids, err := sl.AuthorizedDepartments(ctx)
	if err != nil {
		return nil
	}
	var out []SuggestedRoot
	for _, id := range ids {
		if id == "" || id == providerRoot {
			continue
		}
		name := id
		if len(out) < 20 {
			if d, err := dir.Department(ctx, id); err == nil && d.Name != "" {
				name = d.Name
			}
		}
		out = append(out, SuggestedRoot{ID: id, Name: name})
	}
	return out
}

// TestDirectory 用当前凭据跑一遍接入检查并摘要：有阻塞项 → error 是它的说明与修法；待处理项进 warnings。
// 与 GET /org/directory/checklist 用的是同一份检查，两边永远一致。
func (a *App) TestDirectory(ctx context.Context, sess *Session) (*DirectoryTestResult, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	var dir directory.Directory
	var cfg *store.DirectoryConfig
	var prov directory.Provider
	err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		dir, cfg, prov, err = a.directoryClient(ctx, tx, sess.OrgID)
		return
	})
	if err != nil {
		return nil, err
	}
	loc := sess.Loc()
	res := &DirectoryTestResult{}
	checks := a.diagnose(ctx, dir, cfg, prov)
	for _, d := range directory.ScopeRoots(checks) {
		res.SuggestedRoots = append(res.SuggestedRoots, SuggestedRoot{ID: d.ID, Name: d.Name})
	}
	for _, c := range checks {
		if c.Status == directory.CheckTodo {
			res.Warnings = append(res.Warnings, strings.TrimSpace(c.Detail.In(loc)+" "+c.Fix.In(loc)))
		}
	}
	if b := directory.FirstBlocked(checks); b != nil {
		res.Error = strings.TrimSpace(b.Detail.In(loc) + " " + b.Fix.In(loc))
		return res, nil
	}
	if d, err := dir.Department(ctx, checkRoot(cfg, prov)); err == nil {
		res.DepartmentName = d.Name
	}
	if tn, ok := dir.(directory.TenantNamer); ok {
		if name, err := tn.TenantName(ctx); err == nil {
			res.TenantName = name
		}
	}
	res.OK = true
	return res, nil
}

// pulled 是从外部目录拉下来的一份快照。
type pulled struct {
	depts       []domain.ExternalDept
	users       []domain.ExternalUser
	includeRoot bool
	notes       []i18n.Msg // 非致命问题（某个部门的人员读不出来等）
}

// pullDirectory 拉部门树与人员。部门树拉不到是致命的；某个部门的人员拉不到只记一条继续。
func (a *App) pullDirectory(ctx context.Context, dir directory.Directory, cfg *store.DirectoryConfig, prov directory.Provider) (*pulled, error) {
	roots := syncRoots(cfg, prov)
	if len(roots) > 1 {
		// 多个同步根（ADR 0017 补记二）：每个各自作为顶层团队
		refs := make([]SuggestedRoot, 0, len(roots))
		for _, r := range roots {
			refs = append(refs, SuggestedRoot{ID: r, Name: r})
		}
		return a.pullAuthorizedRoots(ctx, dir, prov, refs, i18n.M("directory.note.multi_roots", len(roots)))
	}
	root := prov.RootDepartmentID
	if len(roots) == 1 {
		root = roots[0]
	}
	raw, err := dir.Departments(ctx, root)
	if err != nil {
		// 企业根被拒绝、但应用有一组被授权的部门：把每个授权部门当作一个同步根，各自成为顶层团队。
		if root == prov.RootDepartmentID {
			if roots := authorizedRoots(ctx, dir, prov.RootDepartmentID); len(roots) > 0 {
				return a.pullAuthorizedRoots(ctx, dir, prov, roots, i18n.M("directory.note.partial_scope", len(roots)))
			}
		}
		return nil, err
	}
	p := &pulled{}
	if root != prov.RootDepartmentID {
		// 同步根不是企业根：根部门自己也是一个团队
		if d, err := dir.Department(ctx, root); err == nil && !d.Deleted {
			p.depts = append(p.depts, domain.ExternalDept{ID: d.ID, Name: d.Name, ParentID: ""})
			p.includeRoot = true
		}
	}
	for _, d := range raw {
		if d.Deleted || d.ID == "" {
			continue
		}
		p.depts = append(p.depts, domain.ExternalDept{ID: d.ID, Name: d.Name, ParentID: d.ParentID})
	}
	// 人员：根 + 每个部门的直属人员，按外部编号去重、合并部门
	byID := map[string]*domain.ExternalUser{}
	var order []string
	scan := []domain.ExternalDept{{ID: root, Name: ""}}
	if p.includeRoot {
		scan = nil
	}
	scan = append(scan, p.depts...)
	for _, d := range scan {
		users, err := dir.Users(ctx, d.ID)
		if err != nil {
			p.notes = append(p.notes, i18n.M("directory.note.users_failed", d.Name, providerMsg(prov, err)))
			continue
		}
		for _, u := range users {
			if !u.Active || u.ID == "" {
				continue
			}
			x, ok := byID[u.ID]
			if !ok {
				x = &domain.ExternalUser{ID: u.ID, Name: u.Name, Email: u.Email, Mobile: u.Mobile}
				byID[u.ID] = x
				order = append(order, u.ID)
			}
			deps := u.DeptIDs
			if len(deps) == 0 {
				deps = []string{d.ID}
			}
			for _, dep := range deps {
				if !contains(x.DeptIDs, dep) {
					x.DeptIDs = append(x.DeptIDs, dep)
				}
			}
		}
	}
	for _, id := range order {
		p.users = append(p.users, *byID[id])
	}
	return p, nil
}

// loadDirectoryState 读本系统现状。
func (a *App) loadDirectoryState(ctx context.Context, tx pgx.Tx, orgID string, prov directory.Provider, cfg *store.DirectoryConfig, includeRoot bool) (domain.DirectoryState, error) {
	provider := prov.Key
	st := domain.DirectoryState{Emails: map[string]string{}, Mobiles: map[string]string{}, TeamIdentity: map[string]string{}, MemberIdentity: map[string]string{},
		TeamDecisions: map[string]domain.Decision{}, MemberDecisions: map[string]domain.Decision{}, RootDeptID: checkRoot(cfg, prov), IncludeRoot: includeRoot}
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
	if st.MemberTeams, err = a.Store.TeamMemberships(ctx, tx); err != nil {
		return st, err
	}
	ids, err := a.Store.ListExternalIdentities(ctx, tx, provider)
	if err != nil {
		return st, err
	}
	for _, x := range ids {
		switch x.Kind {
		case store.IdentityTeam:
			st.TeamIdentity[x.ExternalID] = x.LocalID
		case store.IdentityMember:
			st.MemberIdentity[x.ExternalID] = x.LocalID
		}
	}
	// 已做过的决定（ADR 0017 补记四）：合并 / 新建 / 跳过
	decisions, err := a.Store.ListDirectoryDecisions(ctx, tx, provider)
	if err != nil {
		return st, err
	}
	for _, d := range decisions {
		dec := domain.Decision{Decision: d.Decision, LocalID: d.LocalID}
		switch d.Kind {
		case store.IdentityTeam:
			st.TeamDecisions[d.ExternalID] = dec
		case store.IdentityMember:
			st.MemberDecisions[d.ExternalID] = dec
		}
	}
	if org, err := a.Store.OrganizationByID(ctx, tx, orgID); err == nil {
		st.OwnerMemberID = org.OwnerMemberID
	}
	return st, nil
}

// staleRef 是一个来自旧提供方的团队或成员：切换提供方后它不再被匹配，将改为手工维护（ADR 0017 补记）。
type staleRef struct {
	kind     string // member | team
	id, name string
	provider string // 旧提供方代码名
}

// staleSources 找出来源仍是旧提供方、且没有被当前提供方认领的团队与成员。已经是手工维护的不算。
func (a *App) staleSources(ctx context.Context, tx pgx.Tx, prov directory.Provider, st domain.DirectoryState) ([]staleRef, error) {
	foreign, err := a.Store.ListForeignIdentities(ctx, tx, prov.Key)
	if err != nil {
		return nil, err
	}
	claimed := map[string]bool{}
	for _, id := range st.TeamIdentity {
		claimed[id] = true
	}
	for _, id := range st.MemberIdentity {
		claimed[id] = true
	}
	var out []staleRef
	seen := map[string]bool{}
	for _, x := range foreign {
		if claimed[x.LocalID] || seen[x.LocalID] {
			continue
		}
		seen[x.LocalID] = true
		switch x.Kind {
		case store.IdentityTeam:
			for _, t := range st.Teams {
				if t.ID == x.LocalID && t.Source != domain.SourceManual {
					out = append(out, staleRef{kind: x.Kind, id: t.ID, name: t.Name, provider: x.Provider})
				}
			}
		case store.IdentityMember:
			for _, m := range st.Members {
				if m.ID == x.LocalID && m.Source != domain.SourceManual {
					out = append(out, staleRef{kind: x.Kind, id: m.ID, name: m.Name, provider: x.Provider})
				}
			}
		}
	}
	return out, nil
}

func (s staleRef) note() i18n.Msg {
	title := i18n.Text{i18n.ZhCN: s.provider}
	if p, ok := directory.Lookup(s.provider); ok {
		title = p.Title
	}
	if s.kind == store.IdentityTeam {
		return i18n.M("directory.note.stale_team", s.name, title)
	}
	return i18n.M("directory.note.stale_member", s.name, title)
}

// DirectoryPreview 是「同步会做什么」的对照结果。
type DirectoryPreview struct {
	Teams               []domain.TeamPlan `json:"teams"`
	MembersTotal        int               `json:"members_total"`
	MembersNew          int               `json:"members_new"`
	MembersExisting     int               `json:"members_existing"`
	MembersToDeactivate []MemberRef       `json:"members_to_deactivate"`
	TeamsToDeactivate   []MemberRef       `json:"teams_to_deactivate"`
	Notes               []string          `json:"notes"`
	// 冲突（ADR 0017 补记四）：Confirmations 是还没决定的候选；Decided 是已决定的（合并 / 新建）；Skipped 是决定跳过的；
	// BlockedByConfirmations 大于零时同步不执行。
	Confirmations          []ConfirmationView `json:"confirmations"`
	Decided                []ConfirmationView `json:"decided"`
	Skipped                []SkippedView      `json:"skipped"`
	BlockedByConfirmations int                `json:"blocked_by_confirmations"`
}

// MemberRef 是列表里的一个人或团队。
type MemberRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PreviewDirectory 不落库地拉取并对照。用的是与 SyncDirectory 同一份计划。
func (a *App) PreviewDirectory(ctx context.Context, sess *Session) (*DirectoryPreview, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	var dir directory.Directory
	var cfg *store.DirectoryConfig
	var prov directory.Provider
	if err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		dir, cfg, prov, err = a.directoryClient(ctx, tx, sess.OrgID)
		return
	}); err != nil {
		return nil, err
	}
	// 接入检查有阻塞项时不预览：无名团队不允许进来（ADR 0017 补记二）
	if err := blockedError(a.diagnose(ctx, dir, cfg, prov)); err != nil {
		return nil, err
	}
	p, err := a.pullDirectory(ctx, dir, cfg, prov)
	if err != nil {
		return nil, &UserError{Status: 502, Reasons: []i18n.Msg{providerMsg(prov, err)}}
	}
	out := &DirectoryPreview{Teams: []domain.TeamPlan{}, MembersToDeactivate: []MemberRef{}, TeamsToDeactivate: []MemberRef{}, Notes: []string{},
		Confirmations: []ConfirmationView{}, Decided: []ConfirmationView{}, Skipped: []SkippedView{}}
	err = a.tx(ctx, sess, func(tx pgx.Tx) error {
		st, err := a.loadDirectoryState(ctx, tx, sess.OrgID, prov, cfg, p.includeRoot)
		if err != nil {
			return err
		}
		plan := domain.PlanDirectory(st, p.depts, p.users)
		out.Teams = append(out.Teams, plan.Teams...)
		out.MembersTotal = len(plan.Members)
		for _, m := range plan.Members {
			switch m.Action {
			case domain.PlanCreate:
				out.MembersNew++
			case domain.PlanConfirm:
			default:
				out.MembersExisting++
			}
		}
		decisions, err := a.Store.ListDirectoryDecisions(ctx, tx, prov.Key)
		if err != nil {
			return err
		}
		out.Confirmations, out.Decided, out.Skipped = a.conflictViews(plan, st, p, decisions, sess.Loc())
		out.BlockedByConfirmations = len(out.Confirmations)
		names := map[string]string{}
		for _, m := range st.Members {
			names[m.ID] = m.Name
		}
		for _, id := range plan.DeactivateMembers {
			out.MembersToDeactivate = append(out.MembersToDeactivate, MemberRef{ID: id, Name: names[id]})
		}
		for _, t := range st.Teams {
			if contains(plan.DeactivateTeams, t.ID) {
				out.TeamsToDeactivate = append(out.TeamsToDeactivate, MemberRef{ID: t.ID, Name: t.Name})
			}
		}
		if plan.SkippedOwner != "" {
			out.Notes = append(out.Notes, i18n.M("directory.note.owner_missing", names[plan.SkippedOwner]).Render(sess.Loc()))
		}
		stale, err := a.staleSources(ctx, tx, prov, st)
		if err != nil {
			return err
		}
		for _, sr := range stale {
			out.Notes = append(out.Notes, sr.note().Render(sess.Loc()))
		}
		return nil
	})
	for _, n := range p.notes {
		out.Notes = append(out.Notes, n.Render(sess.Loc()))
	}
	return out, err
}

// SyncDirectory 立即同步（需要组织设置权限）。
func (a *App) SyncDirectory(ctx context.Context, sess *Session) (*DirectoryRunView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	return a.syncDirectory(ctx, sess, true)
}

// errConfirmFirst 是"还有候选没决定"的哨兵：手动同步报 400，定时同步记一次失败的运行。
var errConfirmFirst = errors.New("directory: confirmations pending")

// syncDirectory 是同步本体：拉取 → 对照 → 写入（每一项在自己的保存点里，失败不影响其他项）→ 记录运行与动态。
func (a *App) syncDirectory(ctx context.Context, sess *Session, manual bool) (*DirectoryRunView, error) {
	if _, busy := a.syncing.LoadOrStore(sess.OrgID, true); busy {
		return nil, Bad("err.directory_busy")
	}
	defer a.syncing.Delete(sess.OrgID)
	loc := sess.Loc()
	started := time.Now()

	var dir directory.Directory
	var cfg *store.DirectoryConfig
	var prov directory.Provider
	if err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		dir, cfg, prov, err = a.directoryClient(ctx, tx, sess.OrgID)
		return
	}); err != nil {
		return nil, err
	}
	source := prov.Key // 同步进来的团队与成员的来源 = 提供方代码名
	run := &store.DirectoryRun{OrgID: sess.OrgID, Provider: cfg.Provider, StartedAt: started, Errors: []string{}}
	finish := func(status string) {
		now := time.Now()
		run.FinishedAt, run.Status = &now, status
	}
	// failed 记一次失败的运行与动态（不报 HTTP 错：手动与定时同步都能在运行记录里看到原因）
	failed := func(msg string) (*DirectoryRunView, error) {
		run.Errors = append(run.Errors, msg)
		finish("failed")
		if err := a.tx(ctx, sess, func(tx pgx.Tx) error {
			if err := a.Store.InsertDirectoryRun(ctx, tx, run); err != nil {
				return err
			}
			return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "DirectorySyncRan", ActorID: sess.Actor.ID, At: time.Now(),
				Data: map[string]any{"provider": cfg.Provider, "status": "failed", "error": msg, "run_id": run.ID}}})
		}); err != nil {
			return nil, err
		}
		return runView(run, loc), nil
	}

	// 接入检查有阻塞项时不同步（ADR 0017 补记二）：无名团队不允许进来。记为一次失败的运行，原因是那项检查的说明与修法
	if err := blockedError(a.diagnose(ctx, dir, cfg, prov)); err != nil {
		var ue *UserError
		if errors.As(err, &ue) {
			return failed(ue.Render(loc))
		}
		return nil, err
	}

	p, err := a.pullDirectory(ctx, dir, cfg, prov)
	if err != nil {
		return failed(providerMsg(prov, err).Render(loc))
	}
	for _, n := range p.notes {
		run.Errors = append(run.Errors, n.Render(loc))
	}

	var invitations []InvitationView
	confirmLeft := 0
	err = a.tx(ctx, sess, func(tx pgx.Tx) error {
		st, err := a.loadDirectoryState(ctx, tx, sess.OrgID, prov, cfg, p.includeRoot)
		if err != nil {
			return err
		}
		plan := domain.PlanDirectory(st, p.depts, p.users)
		// 有未决定的候选时不同步（ADR 0017 补记四）：先去预览里确认
		if confirmLeft = plan.Confirmations(); confirmLeft > 0 {
			return errConfirmFirst
		}
		stale, err := a.staleSources(ctx, tx, prov, st)
		if err != nil {
			return err
		}
		teamByID := map[string]*domain.Team{}
		for _, t := range st.Teams {
			teamByID[t.ID] = t
		}
		memberByID := map[string]*domain.Member{}
		for _, m := range st.Members {
			memberByID[m.ID] = m
		}
		var counts domain.DirectoryCounts
		// step 在保存点里执行一项；失败记一条继续
		step := func(fail func(error) i18n.Msg, fn func(sp pgx.Tx) error) {
			sp, err := tx.Begin(ctx)
			if err != nil {
				run.Errors = append(run.Errors, fail(err).Render(loc))
				return
			}
			if err := fn(sp); err != nil {
				_ = sp.Rollback(ctx)
				run.Errors = append(run.Errors, fail(err).Render(loc))
				return
			}
			if err := sp.Commit(ctx); err != nil {
				run.Errors = append(run.Errors, fail(err).Render(loc))
			}
		}
		roles := []string{}
		if cfg.DefaultRole != "" {
			roles = []string{cfg.DefaultRole}
		}

		// ---- 团队：父在前 ----
		for _, tp := range plan.Teams {
			tp := tp
			step(func(e error) i18n.Msg { return i18n.M("directory.note.team_failed", tp.Name, e.Error()) }, func(sp pgx.Tx) error {
				parent := ""
				if tp.ParentExternalID != "" {
					parent = st.TeamIdentity[tp.ParentExternalID]
				}
				switch tp.Action {
				case domain.PlanCreate:
					t := &domain.Team{OrgID: sess.OrgID, Name: tp.Name, ParentID: parent, Source: source, ExternalName: tp.Name}
					if err := a.Store.CreateTeam(ctx, sp, t); err != nil {
						return err
					}
					if err := a.Store.UpsertExternalIdentity(ctx, sp, store.ExternalIdentity{OrgID: sess.OrgID, Provider: cfg.Provider, Kind: store.IdentityTeam, ExternalID: tp.ExternalID, LocalID: t.ID}); err != nil {
						return err
					}
					st.TeamIdentity[tp.ExternalID] = t.ID
					teamByID[t.ID] = t
					counts.AddedTeams++
					return a.insertEvents(ctx, sp, sess, []domain.Event{{Type: "TeamCreated", ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{"team_id": t.ID, "name": t.Name, "source": source}}})
				case domain.PlanUpdate:
					t := teamByID[tp.LocalID]
					if t == nil {
						return store.ErrNotFound
					}
					// 以前的同步建过重复团队、现在决定合并到已有：先把重复团队整体并入（ADR 0017 补记四）
					if loser := teamByID[tp.MergeFrom]; loser != nil && loser.ID != t.ID && !loser.Inactive {
						if err := a.mergeTeamTx(ctx, sp, sess, st.Teams, loser, t); err != nil {
							return err
						}
					}
					t.Name, t.ExternalName, t.ParentID, t.Source, t.Inactive = tp.Name, tp.Name, parent, source, false
					if err := a.Store.UpdateTeam(ctx, sp, t); err != nil {
						return err
					}
					if tp.Bind {
						if err := a.Store.UpsertExternalIdentity(ctx, sp, store.ExternalIdentity{OrgID: sess.OrgID, Provider: cfg.Provider, Kind: store.IdentityTeam, ExternalID: tp.ExternalID, LocalID: t.ID}); err != nil {
							return err
						}
						st.TeamIdentity[tp.ExternalID] = t.ID
					}
					counts.UpdatedTeams++
					return a.insertEvents(ctx, sp, sess, []domain.Event{{Type: "TeamUpdated", ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{"team_id": t.ID, "name": t.Name, "source": source}}})
				}
				return nil
			})
		}
		for _, id := range plan.DeactivateTeams {
			t := teamByID[id]
			if t == nil {
				continue
			}
			step(func(e error) i18n.Msg { return i18n.M("directory.note.team_failed", t.Name, e.Error()) }, func(sp pgx.Tx) error {
				t.Inactive = true
				if err := a.Store.UpdateTeam(ctx, sp, t); err != nil {
					return err
				}
				counts.DeactivatedTeams++
				return a.insertEvents(ctx, sp, sess, []domain.Event{{Type: "TeamDeactivated", ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{"team_id": t.ID, "name": t.Name}}})
			})
		}

		// ---- 成员 ----
		teamsOf := func(deptIDs []string) []string {
			out := []string{}
			for _, d := range deptIDs {
				if local, ok := st.TeamIdentity[d]; ok && !contains(out, local) {
					out = append(out, local)
				}
			}
			return out
		}
		for _, mp := range plan.Members {
			mp := mp
			step(func(e error) i18n.Msg { return i18n.M("directory.note.member_failed", mp.Name, e.Error()) }, func(sp pgx.Tx) error {
				switch mp.Action {
				case domain.PlanCreate:
					email := mp.Email
					if email == "" {
						email = strings.ToLower(mp.ExternalID) + "@" + source + ".invalid"
					}
					acc, _, err := a.Store.AccountByEmail(ctx, sp, email)
					if err == store.ErrNotFound {
						// 没有可用密码的账号：要通过邀请链接设密码才能登录（ADR 0017）
						acc, err = a.Store.CreateAccount(ctx, sp, email, "!"+store.NewID("nopw"), mp.Name)
					}
					if err != nil {
						return err
					}
					if _, err := a.Store.MemberByAccount(ctx, sp, sess.OrgID, acc.ID); err == nil {
						// 账号已是成员（邮箱匹配时应已在计划里认出，这里兜底）
						return nil
					}
					m := &domain.Member{OrgID: sess.OrgID, AccountID: acc.ID, Name: mp.Name, Roles: roles, Active: true, Source: source, Status: domain.MemberPendingActivation}
					if err := a.Store.CreateMemberFull(ctx, sp, m); err != nil {
						return err
					}
					if err := a.Store.UpsertExternalIdentity(ctx, sp, store.ExternalIdentity{OrgID: sess.OrgID, Provider: cfg.Provider, Kind: store.IdentityMember, ExternalID: mp.ExternalID, LocalID: m.ID}); err != nil {
						return err
					}
					if err := a.Store.SetMemberTeams(ctx, sp, sess.OrgID, m.ID, teamsOf(mp.DeptIDs)); err != nil {
						return err
					}
					counts.AddedMembers++
					events := []domain.Event{{Type: "MemberSynced", ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{"member_id": m.ID, "name": m.Name, "source": source}}}
					if mp.Email != "" {
						inv := &domain.Invitation{OrgID: sess.OrgID, Email: mp.Email, Name: mp.Name, Roles: roles, InvitedBy: sess.MemberID, MemberID: m.ID, ExpiresAt: time.Now().Add(14 * 24 * time.Hour)}
						token, err := a.Store.CreateInvitation(ctx, sp, inv)
						if err != nil {
							return err
						}
						invitations = append(invitations, InvitationView{Invitation: inv, URL: a.PublicURL + "/invite/" + token + "/"})
						events = append(events, domain.Event{Type: "MemberInvited", ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{"email": mp.Email, "member_id": m.ID}})
					} else {
						run.Errors = append(run.Errors, i18n.M("directory.note.no_email", mp.Name).Render(loc))
					}
					return a.insertEvents(ctx, sp, sess, events)
				case domain.PlanUpdate:
					m := memberByID[mp.LocalID]
					if m == nil {
						return store.ErrNotFound
					}
					// 以前的同步建过重复成员、现在决定合并到已有：先把重复成员并入（ADR 0017 补记四）
					if loser := memberByID[mp.MergeFrom]; loser != nil && loser.ID != m.ID && loser.Active {
						if err := a.mergeMemberTx(ctx, sp, sess, st.OwnerMemberID, loser, m); err != nil {
							return err
						}
					}
					// 冲突时外部目录赢（ADR 0017）：名字按外部目录，来源改为当前提供方；角色、Agent、任务、目标由本系统管，同步不碰
					m.Name, m.Source = mp.Name, source
					events := []domain.Event{}
					if mp.Reactivate {
						m.Active = true
						m.Status = domain.MemberPendingActivation
					}
					if err := a.Store.UpdateMember(ctx, sp, m); err != nil {
						return err
					}
					if mp.Bind {
						if err := a.Store.UpsertExternalIdentity(ctx, sp, store.ExternalIdentity{OrgID: sess.OrgID, Provider: cfg.Provider, Kind: store.IdentityMember, ExternalID: mp.ExternalID, LocalID: m.ID}); err != nil {
							return err
						}
					}
					if err := a.Store.SetMemberTeams(ctx, sp, sess.OrgID, m.ID, teamsOf(mp.DeptIDs)); err != nil {
						return err
					}
					counts.UpdatedMembers++
					events = append(events, domain.Event{Type: "MemberUpdated", ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{"member_id": m.ID, "name": m.Name, "source": source, "reactivated": mp.Reactivate}})
					return a.insertEvents(ctx, sp, sess, events)
				}
				return nil
			})
		}
		for _, id := range plan.DeactivateMembers {
			m := memberByID[id]
			if m == nil {
				continue
			}
			step(func(e error) i18n.Msg { return i18n.M("directory.note.member_failed", m.Name, e.Error()) }, func(sp pgx.Tx) error {
				m.Active = false
				if err := a.Store.UpdateMember(ctx, sp, m); err != nil {
					return err
				}
				// 沿用现有停用逻辑：吊销其 Agent、任务回待领取
				if err := a.deactivateMemberEffects(ctx, sp, sess, m); err != nil {
					return err
				}
				counts.DeactivatedMembers++
				return a.insertEvents(ctx, sp, sess, []domain.Event{{Type: "MemberDeactivated", ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{"member_id": m.ID, "name": m.Name, "source": source}}})
			})
		}
		if plan.SkippedOwner != "" {
			if m := memberByID[plan.SkippedOwner]; m != nil {
				run.Errors = append(run.Errors, i18n.M("directory.note.owner_missing", m.Name).Render(loc))
			}
		}
		// ---- 旧提供方留下的团队与成员：改为手工维护，不停用（ADR 0017 补记）----
		for _, sr := range stale {
			sr := sr
			run.Errors = append(run.Errors, sr.note().Render(loc))
			step(func(e error) i18n.Msg {
				if sr.kind == store.IdentityTeam {
					return i18n.M("directory.note.team_failed", sr.name, e.Error())
				}
				return i18n.M("directory.note.member_failed", sr.name, e.Error())
			}, func(sp pgx.Tx) error {
				if sr.kind == store.IdentityTeam {
					t := teamByID[sr.id]
					if t == nil {
						return nil
					}
					t.Source, t.ExternalName = domain.SourceManual, ""
					if err := a.Store.UpdateTeam(ctx, sp, t); err != nil {
						return err
					}
				} else {
					m := memberByID[sr.id]
					if m == nil {
						return nil
					}
					m.Source = domain.SourceManual
					if err := a.Store.UpdateMember(ctx, sp, m); err != nil {
						return err
					}
				}
				return a.insertEvents(ctx, sp, sess, []domain.Event{{Type: "DirectoryDetached", ActorID: sess.Actor.ID, At: time.Now(),
					Data: map[string]any{"kind": sr.kind, "id": sr.id, "name": sr.name, "old_provider": sr.provider}}})
			})
		}

		run.Counts = counts
		status := "ok"
		if len(run.Errors) > 0 {
			status = "partial"
		}
		finish(status)
		if err := a.Store.InsertDirectoryRun(ctx, tx, run); err != nil {
			return err
		}
		data := map[string]any{"provider": cfg.Provider, "status": status, "run_id": run.ID, "errors": len(run.Errors),
			"added_teams": counts.AddedTeams, "updated_teams": counts.UpdatedTeams, "deactivated_teams": counts.DeactivatedTeams,
			"added_members": counts.AddedMembers, "updated_members": counts.UpdatedMembers, "deactivated_members": counts.DeactivatedMembers}
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "DirectorySyncRan", ActorID: sess.Actor.ID, At: time.Now(), Data: data}})
	})
	if errors.Is(err, errConfirmFirst) {
		if manual {
			return nil, Bad("err.directory_confirm_first", confirmLeft)
		}
		return failed(i18n.M("err.directory_confirm_first", confirmLeft).Render(loc))
	}
	if err != nil {
		return nil, err
	}
	v := runView(run, loc)
	v.Invitations = invitations
	return v, nil
}

// SourceText 是来源的多语言显示名（作为拒绝理由的参数用，按读者语言渲染）：manual →「手工」，提供方代码名 → 提供方名字。
func SourceText(source string) i18n.Text {
	if source == "" || source == domain.SourceManual {
		return i18n.T("手工", "Manual")
	}
	if p, ok := directory.Lookup(source); ok {
		return p.Title
	}
	return i18n.Text{i18n.Default: source}
}

// SourceTitle 把成员 / 团队的来源翻成显示名：manual →「手工」，提供方代码名 → 提供方名字，认不出的照原样。
func SourceTitle(source string, loc i18n.Locale) string {
	if source == "" || source == domain.SourceManual {
		return i18n.Tr(loc, "directory.source.manual")
	}
	if p, ok := directory.Lookup(source); ok {
		return p.Title.In(loc)
	}
	return source
}

// DirectoryRuns 列出历次同步。
func (a *App) DirectoryRuns(ctx context.Context, sess *Session, limit int) ([]*DirectoryRunView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	out := []*DirectoryRunView{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		runs, err := a.Store.ListDirectoryRuns(ctx, tx, limit)
		if err != nil {
			return err
		}
		for _, r := range runs {
			out = append(out, runView(r, sess.Loc()))
		}
		return nil
	})
	return out, err
}

// ---------- 定时同步 ----------

// ScheduledSyncResult 是巡检里一个组织的结果。
type ScheduledSyncResult struct {
	OrgID string
	Run   *DirectoryRunView
	Err   error
}

// systemSession 是后台任务用的会话：动态的执行者显示为「系统」。
func (a *App) systemSession(ctx context.Context, orgID string) *Session {
	sess := &Session{OrgID: orgID, Actor: &domain.Executor{ID: "", Kind: domain.ExecutorMember, Name: "系统"}, Permissions: map[string]bool{}}
	if org, err := a.Store.OrganizationByID(ctx, a.Store.Pool, orgID); err == nil {
		sess.Locale = i18n.Normalize(org.DefaultLocale)
	}
	return sess
}

// RunScheduledDirectorySyncs 给每小时 / 每天到点的组织跑一次同步（由后台巡检每分钟调用）。
func (a *App) RunScheduledDirectorySyncs(ctx context.Context, now time.Time) []ScheduledSyncResult {
	var out []ScheduledSyncResult
	scheduled, err := a.Store.ScheduledDirectories(ctx, a.Store.Pool)
	if err != nil {
		return []ScheduledSyncResult{{Err: err}}
	}
	for orgID, schedule := range scheduled {
		interval := time.Hour
		if schedule == "daily" {
			interval = 24 * time.Hour
		}
		due := true
		if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
			last, err := a.Store.LastDirectoryRun(ctx, tx)
			if err == store.ErrNotFound {
				return nil
			}
			if err != nil {
				return err
			}
			due = now.Sub(last.StartedAt) >= interval
			return nil
		}); err != nil {
			out = append(out, ScheduledSyncResult{OrgID: orgID, Err: err})
			continue
		}
		if !due {
			continue
		}
		run, err := a.syncDirectory(ctx, a.systemSession(ctx, orgID), false)
		out = append(out, ScheduledSyncResult{OrgID: orgID, Run: run, Err: err})
	}
	return out
}

// pullAuthorizedRoots 把多个部门各自作为同步根拉下来：配置里选了多个同步根，或飞书权限范围只含部分部门时。
// note 是附在结果里的一句说明（按哪些根同步了）。
func (a *App) pullAuthorizedRoots(ctx context.Context, dir directory.Directory, prov directory.Provider, roots []SuggestedRoot, note i18n.Msg) (*pulled, error) {
	p := &pulled{includeRoot: true}
	seen := map[string]bool{}
	byID := map[string]*domain.ExternalUser{}
	var order []string
	addUsers := func(d domain.ExternalDept) {
		users, err := dir.Users(ctx, d.ID)
		if err != nil {
			p.notes = append(p.notes, i18n.M("directory.note.users_failed", d.Name, providerMsg(prov, err)))
			return
		}
		for _, u := range users {
			if !u.Active || u.ID == "" {
				continue
			}
			x, ok := byID[u.ID]
			if !ok {
				x = &domain.ExternalUser{ID: u.ID, Name: u.Name, Email: u.Email, Mobile: u.Mobile}
				byID[u.ID] = x
				order = append(order, u.ID)
			}
			deps := u.DeptIDs
			if len(deps) == 0 {
				deps = []string{d.ID}
			}
			for _, dep := range deps {
				if !contains(x.DeptIDs, dep) {
					x.DeptIDs = append(x.DeptIDs, dep)
				}
			}
		}
	}
	for _, r := range roots {
		if seen[r.ID] {
			continue
		}
		d, err := dir.Department(ctx, r.ID)
		if err != nil || d.Deleted {
			continue
		}
		seen[d.ID] = true
		top := domain.ExternalDept{ID: d.ID, Name: d.Name, ParentID: ""}
		p.depts = append(p.depts, top)
		addUsers(top)
		children, err := dir.Departments(ctx, d.ID)
		if err != nil {
			p.notes = append(p.notes, i18n.M("directory.note.users_failed", d.Name, providerMsg(prov, err)))
			continue
		}
		for _, c := range children {
			if c.Deleted || c.ID == "" || seen[c.ID] {
				continue
			}
			seen[c.ID] = true
			cd := domain.ExternalDept{ID: c.ID, Name: c.Name, ParentID: c.ParentID}
			p.depts = append(p.depts, cd)
			addUsers(cd)
		}
	}
	p.notes = append(p.notes, note)
	for _, id := range order {
		p.users = append(p.users, *byID[id])
	}
	return p, nil
}
