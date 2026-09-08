package app

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/directory"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 代码平台与外部事件（ADR 0020）：GitHub / GitLab / Gitee 走与 IM 集成同一套提供方注册表与接入向导。
// 这里负责配置（凭据按提供方声明校验、保密字段加密入库）、接入检查、仓库选择与建回调、
// webhook 入口（验签 → 去重 → 认任务 → 建 / 更新外部链接 → 让内核决定要不要迁移），以及任务上的手工外部链接。
// 提供方来自 directory 包的注册表，这一层没有任何平台名的分支。

// IdentityGitUser 是外部身份里代码平台登录名的种类（复用 external_identities）。
const IdentityGitUser = "git_user"

// 外部链接的种类。
var linkKinds = []string{"pr", "issue", "doc", "design", "other"}

// LinkKindTitle 返回外部链接种类的名称。
func LinkKindTitle(kind string, loc i18n.Locale) string { return i18n.Tr(loc, "link.kind."+kind) }

// LinkStatusTitle 返回外部链接状态的名称（空状态返回空串）。
func LinkStatusTitle(status string, loc i18n.Locale) string {
	if status == "" {
		return ""
	}
	return i18n.Tr(loc, "link.status."+status)
}

// ---------- 视图 ----------

// CodeRepoView 是一个仓库。
type CodeRepoView struct {
	ID        string `json:"id"`
	FullName  string `json:"full_name"`
	Enabled   bool   `json:"enabled"`
	HookOK    bool   `json:"hook_ok"`
	HookError string `json:"hook_error,omitempty"`
}

// CodePlatformView 是 GET /org/code-platform 的返回。凭据里的保密字段永不回显；
// 回调密钥（本系统生成、要人复制到代码平台里）平时也只回 webhook_secret_set，
// 显式要看的那一次（GET /org/code-platform?reveal=secret）才带上 webhook_secret 并记一条动态。
type CodePlatformView struct {
	Provider         *string           `json:"provider"`
	ProviderTitle    string            `json:"provider_title"`
	Configured       bool              `json:"configured"`
	Credentials      map[string]string `json:"credentials"`
	SecretsSet       map[string]bool   `json:"secrets_set"`
	Providers        []ProviderView    `json:"providers"`
	Repos            []CodeRepoView    `json:"repos"`
	WebhookURL       string            `json:"webhook_url"`
	WebhookSecret    string            `json:"webhook_secret,omitempty"`
	WebhookSecretSet bool              `json:"webhook_secret_set"`
	ProxyURL         string            `json:"proxy_url"`
	ConsoleURL       string            `json:"console_url,omitempty"`
	Events           []CodeEventView   `json:"events"`
}

// CodeEventView 是一种外部事件（前端在流程编辑器里按它列候选）。
type CodeEventView struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

// CodePlatformInput 是 PUT /org/code-platform 的输入。
type CodePlatformInput struct {
	Provider    *string           `json:"provider"`
	Credentials map[string]string `json:"credentials"`
	// Repos 是要接的仓库全名（owner/name）；给了它就整体替换，并逐个去建回调。
	Repos    *[]string `json:"repos"`
	ProxyURL *string   `json:"proxy_url"`
	// RotateWebhookSecret 为真时换一把新的回调密钥；换完要把新密钥填回代码平台的回调设置里。
	RotateWebhookSecret bool `json:"rotate_webhook_secret"`
}

// CodePlatformTestResult 是连通性测试的结果。
type CodePlatformTestResult struct {
	OK       bool     `json:"ok"`
	Repos    int      `json:"repos"`
	Error    string   `json:"error,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// CodeChecklistView 是 GET /org/code-platform/checklist 的返回：检查项 + 向导状态（形状与 IM 集成一致）。
type CodeChecklistView struct {
	Provider      string      `json:"provider"`
	ProviderTitle string      `json:"provider_title"`
	ConsoleURL    string      `json:"console_url,omitempty"`
	WebhookURL    string      `json:"webhook_url"`
	Checks        []CheckView `json:"checks"`
	Ready         bool        `json:"ready"`
	Next          NextStep    `json:"next"`
	Repos         []string    `json:"repos"`
}

// CodeIdentityView 是 GET /me/code-identity 的返回。
type CodeIdentityView struct {
	Provider      string `json:"provider"`
	ProviderTitle string `json:"provider_title"`
	Login         string `json:"login"`
	Bound         bool   `json:"bound"`
}

// LinkView 是任务上的一条外部链接。
type LinkView struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	KindTitle   string    `json:"kind_title"`
	Provider    string    `json:"provider"`
	URL         string    `json:"url"`
	Title       string    `json:"title"`
	Status      string    `json:"status,omitempty"`
	StatusTitle string    `json:"status_title,omitempty"`
	ActorName   string    `json:"actor_name,omitempty"`
	ExternalID  string    `json:"external_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func linkView(l *store.ExternalLink, loc i18n.Locale) LinkView {
	return LinkView{ID: l.ID, Kind: l.Kind, KindTitle: LinkKindTitle(l.Kind, loc), Provider: l.Provider, URL: l.URL, Title: l.Title,
		Status: l.Status, StatusTitle: LinkStatusTitle(l.Status, loc), ActorName: l.ActorName, ExternalID: l.ExternalID,
		CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt}
}

// ---------- 配置 ----------

// WebhookURL 是某个组织在某个代码平台上要填的回调地址。
func (a *App) WebhookURL(provider, orgID string) string {
	return strings.TrimRight(a.PublicURL, "/") + "/api/v1/hooks/code/" + provider + "/" + orgID
}

// codeSecretsSetOf 只看密文里有哪些键。
func (a *App) codeSecretsSetOf(cfg *store.CodePlatformConfig) map[string]bool {
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

// codePlatformConfigured 判断凭据是否齐全：每个必填字段都有值（保密字段看是否已设置）。
func codePlatformConfigured(p directory.Provider, cfg *store.CodePlatformConfig, secretsSet map[string]bool) bool {
	if cfg == nil || cfg.Provider != p.Key {
		return false
	}
	for _, f := range p.Fields {
		if f.Optional {
			continue
		}
		if f.Secret && !secretsSet[f.Key] || !f.Secret && strings.TrimSpace(cfg.Credentials[f.Key]) == "" {
			return false
		}
	}
	return true
}

// webhookSecret 解出组织的回调签名密钥。
func (a *App) webhookSecret(cfg *store.CodePlatformConfig) string {
	if cfg == nil || len(cfg.WebhookSecretEnc) == 0 {
		return ""
	}
	s, err := directory.Decrypt(a.SecretKey, cfg.WebhookSecretEnc)
	if err != nil {
		return ""
	}
	return s
}

func (a *App) codePlatformView(cfg *store.CodePlatformConfig, repos []store.CodeRepo, loc i18n.Locale, reveal bool) *CodePlatformView {
	v := &CodePlatformView{Credentials: map[string]string{}, SecretsSet: map[string]bool{}, Providers: []ProviderView{}, Repos: []CodeRepoView{}, Events: []CodeEventView{}}
	for _, k := range domain.ExternalEvents {
		v.Events = append(v.Events, CodeEventView{Key: k, Title: domain.ExternalEventTitle[k].In(loc)})
	}
	secretsSet := a.codeSecretsSetOf(cfg)
	var dirCfg *store.DirectoryConfig
	if cfg != nil && cfg.Provider != "" {
		p := cfg.Provider
		v.Provider = &p
		v.ProxyURL = cfg.ProxyURL
		v.SecretsSet = secretsSet
		v.WebhookURL = a.WebhookURL(cfg.Provider, cfg.OrgID)
		secret := a.webhookSecret(cfg)
		v.WebhookSecretSet = secret != ""
		// 回调密钥默认不回显：要看它得显式来一次 GET /org/code-platform?reveal=secret，
		// 那一次不缓存并记一条动态（Codex 审查）。
		if reveal {
			v.WebhookSecret = secret
		}
		if prov, ok := directory.Lookup(cfg.Provider); ok {
			v.ProviderTitle = prov.Title.In(loc)
			v.Configured = codePlatformConfigured(prov, cfg, secretsSet)
			if prov.ConsoleURL != nil {
				v.ConsoleURL = prov.ConsoleURL(cfg.Credentials)
			}
			for _, f := range prov.Fields {
				if !f.Secret {
					v.Credentials[f.Key] = cfg.Credentials[f.Key]
				}
			}
		} else {
			v.ProviderTitle = cfg.Provider
		}
		dirCfg = &store.DirectoryConfig{Provider: cfg.Provider, Credentials: cfg.Credentials}
	}
	for _, p := range directory.CodeHostProviders() {
		v.Providers = append(v.Providers, providerView(p, dirCfg, secretsSet, loc))
	}
	for _, r := range repos {
		v.Repos = append(v.Repos, CodeRepoView{ID: r.RepoID, FullName: r.FullName, Enabled: r.Enabled, HookOK: r.HookOK, HookError: r.HookError})
	}
	return v
}

// loadCodePlatform 读配置与仓库；没有配置时 cfg 为 nil。
func (a *App) loadCodePlatform(ctx context.Context, tx pgx.Tx, orgID string) (*store.CodePlatformConfig, []store.CodeRepo, error) {
	cfg, err := a.Store.CodePlatformConfig(ctx, tx, orgID)
	if err == store.ErrNotFound {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	repos, err := a.Store.ListCodeRepos(ctx, tx, orgID, cfg.Provider)
	if err != nil {
		return nil, nil, err
	}
	return cfg, repos, nil
}

// GetCodePlatform 读配置（GET /org/code-platform）。回调密钥不在这里回显。
func (a *App) GetCodePlatform(ctx context.Context, sess *Session) (*CodePlatformView, error) {
	return a.getCodePlatform(ctx, sess, false)
}

// RevealCodeWebhookSecret 读配置并回显回调密钥（GET /org/code-platform?reveal=secret）。
// 接口层要给这一次响应加 Cache-Control: no-store；这里记一条动态，谁看过密钥有据可查。
func (a *App) RevealCodeWebhookSecret(ctx context.Context, sess *Session) (*CodePlatformView, error) {
	return a.getCodePlatform(ctx, sess, true)
}

func (a *App) getCodePlatform(ctx context.Context, sess *Session, reveal bool) (*CodePlatformView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	var out *CodePlatformView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		cfg, repos, err := a.loadCodePlatform(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		out = a.codePlatformView(cfg, repos, sess.Loc(), reveal)
		if reveal && out.WebhookSecret != "" {
			return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "CodeWebhookSecretRevealed", ActorID: sess.Actor.ID, At: time.Now(),
				Data: map[string]any{"provider": cfg.Provider}}})
		}
		return nil
	})
	return out, err
}

// SaveCodePlatform 写配置（PUT /org/code-platform）：凭据按提供方声明逐字段校验，保密字段加密入库；
// 第一次保存时生成回调签名密钥；给了仓库列表就整体替换并逐个去建回调。动态 CodePlatformConfigured。
// DisconnectCodePlatform 断开代码平台：删掉配置与仓库选择，回到未接入。
// 已经挂上的外部链接与历史动态保留（它们记录的是发生过的事，ADR 0002）；回调再进来会被当成未配置拒掉。
func (a *App) DisconnectCodePlatform(ctx context.Context, sess *Session) (*CodePlatformView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	var prev string
	if err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		c, err := a.Store.CodePlatformConfig(ctx, tx, sess.OrgID)
		if err == store.ErrNotFound {
			return nil
		}
		if err != nil {
			return err
		}
		prev = c.Provider
		if err := a.Store.DeleteCodePlatformConfig(ctx, tx, sess.OrgID); err != nil {
			return err
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "CodePlatformDisconnected", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"provider": prev}}})
	}); err != nil {
		return nil, err
	}
	return a.GetCodePlatform(ctx, sess)
}

func (a *App) SaveCodePlatform(ctx context.Context, sess *Session, in CodePlatformInput) (*CodePlatformView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	// 出网地址在存进库之前先过一遍守卫：默认只许公网（egress.go）。
	if in.ProxyURL != nil && strings.TrimSpace(*in.ProxyURL) != "" {
		if err := checkProxy(*in.ProxyURL); err != nil {
			return nil, err
		}
	}
	for k, v := range in.Credentials {
		if !isURLField(k) || strings.TrimSpace(v) == "" {
			continue
		}
		if err := checkEgress(v); err != nil {
			return nil, err
		}
	}
	// 第一段事务：存配置。建回调要出网，不能占着事务。
	var cfg *store.CodePlatformConfig
	var prov directory.Provider
	var secretsChanged, fieldsChanged []string
	var switched bool
	prevProxy := ""
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		c, err := a.Store.CodePlatformConfig(ctx, tx, sess.OrgID)
		fresh := err == store.ErrNotFound
		if fresh {
			c = &store.CodePlatformConfig{OrgID: sess.OrgID, Credentials: map[string]string{}}
		} else if err != nil {
			return err
		}
		prevProxy = c.ProxyURL
		key := c.Provider
		if in.Provider != nil && strings.TrimSpace(*in.Provider) != "" {
			key = strings.ToLower(strings.TrimSpace(*in.Provider))
		}
		if key == "" {
			return Bad("err.code_provider_required")
		}
		p, ok := directory.Lookup(key)
		if !ok || !p.CanHostCode() {
			return Bad("err.code_provider", key)
		}
		prov = p
		switched = !fresh && c.Provider != key
		if fresh || switched {
			c.Provider, c.Credentials, c.SecretsEnc = key, map[string]string{}, nil
		}
		secrets, err := directory.DecryptSecrets(a.SecretKey, c.SecretsEnc)
		if err != nil {
			secrets = map[string]string{}
		}
		creds := map[string]string{}
		for _, f := range p.Fields {
			val := strings.TrimSpace(in.Credentials[f.Key])
			if f.Secret {
				if val != "" {
					secrets[f.Key] = val
					secretsChanged = append(secretsChanged, f.Key)
				}
				if secrets[f.Key] == "" && !f.Optional {
					return Bad("err.directory_secret_required", f.Title)
				}
				continue
			}
			if val == "" {
				val = c.Credentials[f.Key]
			} else if val != c.Credentials[f.Key] {
				fieldsChanged = append(fieldsChanged, f.Key)
			}
			if val == "" && !f.Optional {
				return Bad("err.directory_field_required", f.Title)
			}
			creds[f.Key] = val
		}
		for k := range secrets {
			if f, ok := p.Field(k); !ok || !f.Secret {
				delete(secrets, k)
			}
		}
		c.Credentials = creds
		if c.SecretsEnc, err = directory.EncryptSecrets(a.SecretKey, secrets); err != nil {
			return err
		}
		rotated := len(c.WebhookSecretEnc) == 0 || switched || in.RotateWebhookSecret
		if rotated {
			enc, err := directory.Encrypt(a.SecretKey, store.NewID("whs"))
			if err != nil {
				return err
			}
			c.WebhookSecretEnc = enc
		}
		if in.ProxyURL != nil {
			c.ProxyURL = strings.TrimSpace(*in.ProxyURL)
		}
		if err := a.Store.UpsertCodePlatformConfig(ctx, tx, c); err != nil {
			return err
		}
		cfg = c
		if secretsChanged == nil {
			secretsChanged = []string{}
		}
		// 动态只记改了哪些字段的名字，不记值：接口地址、用户名、代理地址都可能暴露内网拓扑（Codex 审查）。
		fields := append(append([]string{}, fieldsChanged...), secretsChanged...)
		if in.ProxyURL != nil && strings.TrimSpace(*in.ProxyURL) != prevProxy {
			fields = append(fields, "proxy_url")
		}
		sort.Strings(fields)
		events := []domain.Event{{Type: "CodePlatformConfigured", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"provider": c.Provider, "provider_switched": switched, "fields": fields, "fresh": fresh}}}
		if in.RotateWebhookSecret && !fresh && !switched {
			events = append(events, domain.Event{Type: "CodeWebhookSecretRotated", ActorID: sess.Actor.ID, At: time.Now(),
				Data: map[string]any{"provider": c.Provider}})
		}
		return a.insertEvents(ctx, tx, sess, events)
	})
	if err != nil {
		return nil, err
	}
	// 第二段：选仓库并建回调（出网，事务外）。
	if in.Repos != nil {
		if err := a.syncCodeRepos(ctx, sess, cfg, prov, *in.Repos); err != nil {
			return nil, err
		}
	}
	var out *CodePlatformView
	err = a.tx(ctx, sess, func(tx pgx.Tx) error {
		c, repos, err := a.loadCodePlatform(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		out = a.codePlatformView(c, repos, sess.Loc(), false)
		return nil
	})
	return out, err
}

// syncCodeRepos 把选中的仓库存下来，并逐个去代码平台上建 / 补回调；建不上不算失败，记在仓库行上。
func (a *App) syncCodeRepos(ctx context.Context, sess *Session, cfg *store.CodePlatformConfig, prov directory.Provider, wanted []string) error {
	host, err := a.newCodeHost(cfg, prov)
	if err != nil {
		return err
	}
	callback := a.WebhookURL(cfg.Provider, cfg.OrgID)
	secret := a.webhookSecret(cfg)
	available, listErr := host.ListRepos(ctx)
	byName := map[string]directory.Repo{}
	for _, r := range available {
		byName[r.FullName] = r
	}
	rows := []store.CodeRepo{}
	seen := map[string]bool{}
	for _, name := range wanted {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		row := store.CodeRepo{OrgID: cfg.OrgID, Provider: cfg.Provider, RepoID: name, FullName: name, Enabled: true}
		if r, ok := byName[name]; ok && r.ID != "" {
			row.RepoID = r.ID
		}
		if listErr == nil {
			if _, ok := byName[name]; !ok {
				row.HookError = i18n.Trf(sess.Loc(), "err.code_repo_unknown", name)
				rows = append(rows, row)
				continue
			}
		}
		if err := host.EnsureWebhook(ctx, name, callback, secret); err != nil {
			row.HookError = providerMsg(prov, err).Render(sess.Loc())
		} else {
			row.HookOK = true
		}
		rows = append(rows, row)
	}
	return a.tx(ctx, sess, func(tx pgx.Tx) error {
		keep := make([]string, 0, len(rows))
		for _, r := range rows {
			if err := a.Store.UpsertCodeRepo(ctx, tx, r); err != nil {
				return err
			}
			keep = append(keep, r.RepoID)
		}
		return a.Store.DeleteCodeReposExcept(ctx, tx, cfg.OrgID, cfg.Provider, keep)
	})
}

// newCodeHost 用配置里的凭据建一个代码平台客户端。
func (a *App) newCodeHost(cfg *store.CodePlatformConfig, prov directory.Provider) (directory.CodeHost, error) {
	creds := map[string]string{}
	for k, v := range cfg.Credentials {
		creds[k] = v
	}
	secrets, err := directory.DecryptSecrets(a.SecretKey, cfg.SecretsEnc)
	if err != nil {
		return nil, Bad("err.directory_secret_key")
	}
	for k, v := range secrets {
		creds[k] = v
	}
	return prov.NewCodeHost(creds, directory.Options{ProxyURL: cfg.ProxyURL})
}

// codeHostOf 读配置、建客户端；没配置时返回可读的拒绝理由。
func (a *App) codeHostOf(ctx context.Context, sess *Session) (directory.CodeHost, *store.CodePlatformConfig, directory.Provider, []store.CodeRepo, error) {
	var cfg *store.CodePlatformConfig
	var repos []store.CodeRepo
	err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		cfg, repos, err = a.loadCodePlatform(ctx, tx, sess.OrgID)
		return
	})
	if err != nil {
		return nil, nil, directory.Provider{}, nil, err
	}
	if cfg == nil || cfg.Provider == "" {
		return nil, nil, directory.Provider{}, nil, Bad("err.code_not_configured")
	}
	prov, ok := directory.Lookup(cfg.Provider)
	if !ok || !prov.CanHostCode() {
		return nil, nil, directory.Provider{}, nil, Bad("err.code_provider", cfg.Provider)
	}
	host, err := a.newCodeHost(cfg, prov)
	if err != nil {
		return nil, nil, directory.Provider{}, nil, err
	}
	return host, cfg, prov, repos, nil
}

// enabledRepoNames 返回启用的仓库全名。
func enabledRepoNames(repos []store.CodeRepo) []string {
	out := []string{}
	for _, r := range repos {
		if r.Enabled {
			out = append(out, r.FullName)
		}
	}
	return out
}

// diagnoseCode 让代码平台跑接入检查；连不上时合成一项阻塞的「连接提供方」。
func (a *App) diagnoseCode(ctx context.Context, host directory.CodeHost, cfg *store.CodePlatformConfig, prov directory.Provider, repos []store.CodeRepo) []directory.Check {
	dg, ok := host.(directory.CodeDiagnoser)
	if !ok {
		return nil
	}
	checks, err := dg.DiagnoseCode(ctx, enabledRepoNames(repos), a.WebhookURL(cfg.Provider, cfg.OrgID))
	if err != nil {
		return []directory.Check{{Key: "connection", Title: textOf("directory.check.connection"), Status: directory.CheckBlocked, Blocking: true,
			Detail: textOf("err.directory_unreachable", prov.Title, err.Error()), Fix: textOf("directory.check.connection.fix")}}
	}
	return checks
}

// CodePlatformChecklist 现场跑一遍接入检查并汇总成向导状态（GET /org/code-platform/checklist）。
func (a *App) CodePlatformChecklist(ctx context.Context, sess *Session) (*CodeChecklistView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	loc := sess.Loc()
	view := &CodeChecklistView{Checks: []CheckView{}, Repos: []string{}}
	host, cfg, prov, repos, err := a.codeHostOf(ctx, sess)
	if err != nil {
		var ue *UserError
		if errors.As(err, &ue) && len(ue.Reasons) == 1 && ue.Reasons[0].Key == "err.code_not_configured" {
			view.Next = NextStep{Step: "credentials", Text: i18n.Tr(loc, "code.next.credentials")}
			return view, nil
		}
		return nil, err
	}
	view.Provider, view.ProviderTitle = prov.Key, prov.Title.In(loc)
	view.WebhookURL = a.WebhookURL(cfg.Provider, cfg.OrgID)
	view.Repos = enabledRepoNames(repos)
	if prov.ConsoleURL != nil {
		view.ConsoleURL = prov.ConsoleURL(cfg.Credentials)
	}
	checks := a.diagnoseCode(ctx, host, cfg, prov, repos)
	for _, c := range checks {
		view.Checks = append(view.Checks, checkView(c, loc))
	}
	blocked := directory.FirstBlocked(checks)
	view.Ready = blocked == nil
	switch {
	case blocked != nil:
		text := blocked.Fix.In(loc)
		if text == "" {
			text = blocked.Detail.In(loc)
		}
		view.Next = NextStep{Step: "checks", Text: text}
	case len(view.Repos) == 0:
		view.Next = NextStep{Step: "repos", Text: i18n.Tr(loc, "code.next.repos")}
	default:
		view.Next = NextStep{Step: "done", Text: i18n.Tr(loc, "code.next.done")}
	}
	return view, nil
}

// TestCodePlatform 用当前凭据跑同一份接入检查并摘要（POST /org/code-platform/test）。
func (a *App) TestCodePlatform(ctx context.Context, sess *Session) (*CodePlatformTestResult, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	host, cfg, prov, repos, err := a.codeHostOf(ctx, sess)
	if err != nil {
		return nil, err
	}
	loc := sess.Loc()
	res := &CodePlatformTestResult{}
	checks := a.diagnoseCode(ctx, host, cfg, prov, repos)
	for _, c := range checks {
		if c.Status == directory.CheckTodo {
			res.Warnings = append(res.Warnings, strings.TrimSpace(c.Detail.In(loc)+" "+c.Fix.In(loc)))
		}
	}
	if b := directory.FirstBlocked(checks); b != nil {
		res.Error = strings.TrimSpace(b.Detail.In(loc) + " " + b.Fix.In(loc))
		return res, nil
	}
	if rs, err := host.ListRepos(ctx); err == nil {
		res.Repos = len(rs)
	}
	res.OK = true
	return res, nil
}

// CodePlatformRepos 现场读一遍代码平台上的仓库，并标出已经选中的（GET /org/code-platform/repos）。
func (a *App) CodePlatformRepos(ctx context.Context, sess *Session) ([]CodeRepoView, error) {
	if err := a.requireOrgSettings(sess); err != nil {
		return nil, err
	}
	host, _, prov, repos, err := a.codeHostOf(ctx, sess)
	if err != nil {
		return nil, err
	}
	all, err := host.ListRepos(ctx)
	if err != nil {
		return nil, Bad("err.code_repos_failed", providerMsg(prov, err))
	}
	chosen := map[string]store.CodeRepo{}
	for _, r := range repos {
		chosen[r.FullName] = r
	}
	out := []CodeRepoView{}
	for _, r := range all {
		v := CodeRepoView{ID: r.ID, FullName: r.FullName}
		if c, ok := chosen[r.FullName]; ok {
			v.Enabled, v.HookOK, v.HookError = c.Enabled, c.HookOK, c.HookError
		}
		out = append(out, v)
	}
	return out, nil
}

// ---------- 个人的代码平台身份 ----------

// MyCodeIdentity 读我绑定的代码平台登录名（GET /me/code-identity）。
func (a *App) MyCodeIdentity(ctx context.Context, sess *Session) (*CodeIdentityView, error) {
	if sess.IsAgent() {
		return nil, Forbidden("err.code_identity_agent")
	}
	v := &CodeIdentityView{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		cfg, _, err := a.loadCodePlatform(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		if cfg != nil {
			v.Provider = cfg.Provider
			if p, ok := directory.Lookup(cfg.Provider); ok {
				v.ProviderTitle = p.Title.In(sess.Loc())
			}
		}
		ids, err := a.Store.ListExternalIdentities(ctx, tx, v.Provider)
		if err != nil {
			return err
		}
		for _, x := range ids {
			if x.Kind == IdentityGitUser && x.LocalID == sess.MemberID {
				v.Login, v.Bound = x.ExternalID, true
			}
		}
		return nil
	})
	return v, err
}

// BindMyCodeIdentity 绑定（login 非空）或解绑（login 为空）我的代码平台登录名（PUT /me/code-identity）。
// 动态 CodeIdentityBound。
func (a *App) BindMyCodeIdentity(ctx context.Context, sess *Session, login string) (*CodeIdentityView, error) {
	if sess.IsAgent() {
		return nil, Forbidden("err.code_identity_agent")
	}
	login = strings.TrimSpace(login)
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		cfg, _, err := a.loadCodePlatform(ctx, tx, sess.OrgID)
		if err != nil {
			return err
		}
		if cfg == nil || cfg.Provider == "" {
			return Bad("err.code_not_configured")
		}
		ids, err := a.Store.ListExternalIdentities(ctx, tx, cfg.Provider)
		if err != nil {
			return err
		}
		for _, x := range ids {
			if x.Kind != IdentityGitUser {
				continue
			}
			if x.LocalID == sess.MemberID && x.ExternalID != login {
				if _, err := a.Store.DeleteExternalIdentity(ctx, tx, cfg.Provider, IdentityGitUser, x.ExternalID); err != nil {
					return err
				}
			}
			if login != "" && x.ExternalID == login && x.LocalID != sess.MemberID {
				return Bad("err.code_login_taken", login)
			}
		}
		if login != "" {
			if err := a.Store.UpsertExternalIdentity(ctx, tx, store.ExternalIdentity{OrgID: sess.OrgID, Provider: cfg.Provider,
				Kind: IdentityGitUser, ExternalID: login, LocalID: sess.MemberID}); err != nil {
				return err
			}
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "CodeIdentityBound", ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"provider": cfg.Provider, "login": login}}})
	})
	if err != nil {
		return nil, err
	}
	return a.MyCodeIdentity(ctx, sess)
}

// ---------- 任务上的外部链接 ----------

// LinkInput 是 POST /tasks/{id}/links 的输入。
type LinkInput struct {
	Kind  string `json:"kind"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

// TaskLinks 读一个任务的外部链接。
func (a *App) TaskLinks(ctx context.Context, sess *Session, taskID string) ([]LinkView, error) {
	out := []LinkView{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		if _, err := a.loadContext(ctx, tx, sess, taskID); err != nil {
			return err
		}
		ls, err := a.Store.LinksOfTask(ctx, tx, taskID)
		if err != nil {
			return err
		}
		for _, l := range ls {
			out = append(out, linkView(l, sess.Loc()))
		}
		return nil
	})
	return out, err
}

// AddTaskLink 给任务挂一条手工外部链接（POST /tasks/{id}/links）。
// Agent 与评论同一套规则：要有「执行任务」授权，是「需要人确认」时记一条待确认操作。
func (a *App) AddTaskLink(ctx context.Context, sess *Session, taskID string, in LinkInput) (*LinkView, error) {
	return idempotent(ctx, a, sess, "add_external_link", map[string]any{"task_id": taskID, "kind": in.Kind, "url": in.URL, "title": in.Title}, func() (*LinkView, error) {
		return a.addTaskLink(ctx, sess, taskID, in)
	})
}

func (a *App) addTaskLink(ctx context.Context, sess *Session, taskID string, in LinkInput) (*LinkView, error) {
	kind := strings.TrimSpace(in.Kind)
	if kind == "" {
		kind = "other"
	}
	if !contains(linkKinds, kind) {
		return nil, Bad("err.link_kind", kind)
	}
	link := strings.TrimSpace(in.URL)
	u, err := url.Parse(link)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || hasControlChars(link) {
		return nil, Bad("err.link_url", in.URL)
	}
	if u.User != nil {
		return nil, Bad("err.link_url_credentials")
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = link
	}
	var out *LinkView
	var pv *ProposalView
	err = a.tx(ctx, sess, func(tx pgx.Tx) error {
		c, err := a.loadContext(ctx, tx, sess, taskID)
		if err != nil {
			return err
		}
		if sess.Actor.Kind == domain.ExecutorAgent {
			if !sess.Actor.HasGrant(domain.GrantExecute) {
				return &domain.Rejection{Reasons: []domain.Reason{i18n.M("reject.agent_no_grant", i18n.Key("grant.execute"))}}
			}
			if domain.NeedsApproval(sess.Actor, domain.GrantExecute) {
				summary := i18n.M("proposal.summary.task.external_link", c.Task.Title, title)
				if sess.Write.DryRun {
					return dryRunPending(sess, a.executorName(ctx, tx, sess.MemberID), summary)
				}
				pv, err = a.createProposal(ctx, tx, sess, proposalDraft{Action: ActionTaskExternalLink, Grant: domain.GrantExecute,
					TargetKind: "task", TargetID: c.Task.ID, TargetTitle: c.Task.Title,
					Payload: map[string]any{"task_id": c.Task.ID, "kind": kind, "url": link, "title": title},
					Summary: summary})
				return err
			}
		}
		// 只看不做（ADR 0025）：地址、种类、权限都校验过了，写库之前停住。
		if sess.Write.DryRun {
			return dryRun(sess, i18n.M("will.task.external_link", taskRefText(c.Task), title))
		}
		l := &store.ExternalLink{OrgID: sess.OrgID, TaskID: c.Task.ID, Kind: kind, URL: link, Title: title, CreatedBy: sess.Actor.ID}
		created, err := a.Store.UpsertLink(ctx, tx, l)
		if err != nil {
			return err
		}
		typ := "ExternalLinkUpdated"
		if created {
			typ = "ExternalLinkAdded"
		}
		v := linkView(l, sess.Loc())
		out = &v
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: typ, TaskID: c.Task.ID, ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"kind": kind, "url": link, "title": title, "kind_title": bothText(LinkKindTitle, kind)}}})
	})
	if err != nil {
		return nil, err
	}
	if pv != nil {
		return nil, pending(sess, pv)
	}
	return out, nil
}

// RemoveTaskLink 摘掉一条外部链接（DELETE /tasks/{id}/links/{link_id}）。动态 ExternalLinkRemoved。
// Agent 与挂链接同一套规则：要有「执行任务」授权，是「需要人确认」时先记一条待确认操作。
func (a *App) RemoveTaskLink(ctx context.Context, sess *Session, taskID, linkID string) error {
	_, err := idempotent(ctx, a, sess, "remove_external_link", map[string]any{"task_id": taskID, "link_id": linkID}, func() (map[string]any, error) {
		if err := a.removeTaskLink(ctx, sess, taskID, linkID); err != nil {
			return nil, err
		}
		return map[string]any{"task_id": taskID, "removed": linkID}, nil
	})
	return err
}

func (a *App) removeTaskLink(ctx context.Context, sess *Session, taskID, linkID string) error {
	var pv *ProposalView
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		c, err := a.loadContext(ctx, tx, sess, taskID)
		if err != nil {
			return err
		}
		if sess.Actor.Kind == domain.ExecutorAgent && !sess.Actor.HasGrant(domain.GrantExecute) {
			return &domain.Rejection{Reasons: []domain.Reason{i18n.M("reject.agent_no_grant", i18n.Key("grant.execute"))}}
		}
		l, err := a.Store.LinkByID(ctx, tx, linkID)
		if err != nil || l.TaskID != taskID {
			return NotFound("err.link_missing")
		}
		if sess.Actor.Kind == domain.ExecutorAgent && domain.NeedsApproval(sess.Actor, domain.GrantExecute) {
			summary := i18n.M("proposal.summary.task.external_link_remove", c.Task.Title, l.Title)
			if sess.Write.DryRun {
				return dryRunPending(sess, a.executorName(ctx, tx, sess.MemberID), summary)
			}
			pv, err = a.createProposal(ctx, tx, sess, proposalDraft{Action: ActionTaskExternalLinkRemove, Grant: domain.GrantExecute,
				TargetKind: "task", TargetID: c.Task.ID, TargetTitle: c.Task.Title,
				Payload: map[string]any{"task_id": c.Task.ID, "link_id": l.ID, "title": l.Title}, Summary: summary})
			return err
		}
		if sess.Write.DryRun {
			return dryRun(sess, i18n.M("will.task.external_link_remove", taskRefText(c.Task), l.Title))
		}
		ok, err := a.Store.DeleteLink(ctx, tx, taskID, linkID)
		if err != nil {
			return err
		}
		if !ok {
			return NotFound("err.link_missing")
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "ExternalLinkRemoved", TaskID: c.Task.ID, ActorID: sess.Actor.ID, At: time.Now(),
			Data: map[string]any{"kind": l.Kind, "url": l.URL, "title": l.Title}}})
	})
	if err != nil {
		return err
	}
	if pv != nil {
		return pending(sess, pv)
	}
	return nil
}

// ---------- webhook 入口 ----------

// WebhookResult 是一次回调处理的结果（接口层只回 200，这里给测试与日志看）。
type WebhookResult struct {
	Provider  string   `json:"provider"`
	Event     string   `json:"event"`
	Duplicate bool     `json:"duplicate"`
	Tasks     []string `json:"tasks"`
	Applied   []string `json:"applied"`
	Ignored   []string `json:"ignored"`
	// Note 是"收下了但什么都没做"的原因（仓库没接进来等），一句人话。
	Note string `json:"note,omitempty"`
}

// taskNumberRe 从 PR 标题与分支名里找 #123。
var taskNumberRe = regexp.MustCompile(`#(\d+)`)

// HandleCodeWebhook 处理一次代码平台回调：验签 → 去重 → 认任务 → 建 / 更新外部链接 → 让内核决定要不要迁移。
// 签名不对返回 401 类错误；其余情况一律 200（代码平台不该因为我们这边的判断而重试）。
func (a *App) HandleCodeWebhook(ctx context.Context, providerKey, orgID string, headers http.Header, body []byte) (*WebhookResult, error) {
	prov, ok := directory.Lookup(providerKey)
	if !ok || !prov.CanHostCode() {
		return nil, NotFound("err.code_provider", providerKey)
	}
	var cfg *store.CodePlatformConfig
	var repos []store.CodeRepo
	if err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		c, err := a.Store.CodePlatformConfig(ctx, tx, orgID)
		if err == store.ErrNotFound {
			return NotFound("err.code_not_configured")
		}
		if err != nil {
			return err
		}
		cfg = c
		repos, err = a.Store.ListCodeRepos(ctx, tx, orgID, c.Provider)
		return err
	}); err != nil {
		return nil, wrapErr(err)
	}
	if cfg.Provider != providerKey {
		return nil, NotFound("err.code_not_configured")
	}
	host, err := a.newCodeHost(cfg, prov)
	if err != nil {
		return nil, err
	}
	ev, err := host.VerifyWebhook(headers, body, a.webhookSecret(cfg))
	if errors.Is(err, directory.ErrBadSignature) {
		return nil, Unauthorized("err.webhook_signature")
	}
	res := &WebhookResult{Provider: providerKey, Event: ev.Kind, Tasks: []string{}, Applied: []string{}, Ignored: []string{}}
	if errors.Is(err, directory.ErrNotForUs) {
		return res, nil
	}
	if err != nil {
		return nil, Bad("err.webhook_payload", err.Error())
	}
	// 验过签也只认接进来的仓库：密钥万一泄露，也不能拿一个没接入的仓库伪造事件推进任务（Codex 审查）。
	if !repoEnabled(repos, ev.Repo) {
		res.Note = i18n.Trf(i18n.Default, "code.webhook.repo_off", ev.Repo)
		return res, nil
	}
	// 自动建的外部链接必须落在这个平台自己的域名上，不能由载荷指到任意站点（Codex 审查）。
	if ev.URL != "" && !sameHost(ev.URL, platformWebHost(prov, cfg)) {
		res.Note = i18n.Trf(i18n.Default, "code.webhook.link_host", ev.URL)
		ev.URL, ev.LinkKind = "", ""
	}
	sess := a.externalSession(ctx, orgID, ev)
	// 去重：同一投递只处理一次
	dup := false
	if err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		dup, err = a.Store.SeenWebhook(ctx, tx, orgID, providerKey, ev.DeliveryID)
		return
	}); err != nil {
		return nil, err
	}
	if dup {
		res.Duplicate = true
		return res, nil
	}
	// 上面那条记录是"占位"，处理中途出错时要撤销，否则代码平台重投会被当成重复而永远丢掉这个事件。
	release := func() {
		_ = a.tx(ctx, sess, func(tx pgx.Tx) error {
			return a.Store.ReleaseWebhook(ctx, tx, orgID, providerKey, ev.DeliveryID)
		})
	}
	if a.failCodeEvent { // 测试用：模拟处理中途出错
		release()
		return nil, Bad("err.webhook_payload", "test failure")
	}
	tasks, err := a.matchTasks(ctx, sess, ev)
	if err != nil {
		release()
		return nil, err
	}
	for _, t := range tasks {
		res.Tasks = append(res.Tasks, t.ID)
		applied, err := a.applyCodeEvent(ctx, sess, t.ID, ev)
		if err != nil {
			release()
			return nil, err
		}
		if applied != "" {
			res.Applied = append(res.Applied, t.ID+":"+applied)
		} else if ev.Kind != "" {
			res.Ignored = append(res.Ignored, t.ID)
		}
	}
	return res, nil
}

// hasControlChars 说一段文字里有没有控制字符（换行、制表、NUL 等）；地址里不该有。
func hasControlChars(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// repoEnabled 说这个仓库是不是组织接进来并且还启用着的。
func repoEnabled(repos []store.CodeRepo, fullName string) bool {
	name := strings.TrimSpace(fullName)
	if name == "" {
		return false
	}
	for _, r := range repos {
		if r.Enabled && strings.EqualFold(r.FullName, name) {
			return true
		}
	}
	return false
}

// platformWebHost 是这个代码平台的网页域名（从提供方的控制台地址反推；企业版跟着接口地址走）。
// 拿不到时返回空串，表示不做域名限制。
func platformWebHost(prov directory.Provider, cfg *store.CodePlatformConfig) string {
	if prov.ConsoleURL == nil || cfg == nil {
		return ""
	}
	u, err := url.Parse(prov.ConsoleURL(cfg.Credentials))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// sameHost 判断一个地址是不是落在 host（或它的子域）上；host 为空表示不限制。
func sameHost(raw, host string) bool {
	if host == "" {
		return true
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == host || strings.HasSuffix(h, "."+host)
}

// externalSession 是处理外部事件用的会话：执行者是「外部事件」，看得见全组织（回调不带人的身份）。
func (a *App) externalSession(ctx context.Context, orgID string, ev directory.CodeEvent) *Session {
	sess := a.systemSession(ctx, orgID)
	name := ev.ActorName
	if name == "" {
		name = ev.Provider
	}
	sess.Actor = &domain.Executor{ID: "", Kind: domain.ExecutorExternal, Name: name}
	sess.collabAll, sess.financeAll = true, true
	return sess
}

// matchTasks 按 ADR 0020 的规则认任务：PR 标题或分支名里的 #123（组织内序号），
// 或描述里出现的本系统任务链接；另外，这个 PR 已经挂过的任务永远算数（CI 事件靠它找到任务）。
func (a *App) matchTasks(ctx context.Context, sess *Session, ev directory.CodeEvent) ([]*domain.Task, error) {
	numbers := map[int]bool{}
	for _, s := range []string{ev.Title, ev.Branch} {
		for _, m := range taskNumberRe.FindAllStringSubmatch(s, -1) {
			if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
				numbers[n] = true
			}
		}
	}
	ids := map[string]bool{}
	for _, id := range taskIDsInText(a.PublicURL, ev.Body) {
		ids[id] = true
	}
	var out []*domain.Task
	seen := map[string]bool{}
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		add := func(t *domain.Task) {
			if t != nil && !seen[t.ID] {
				seen[t.ID] = true
				out = append(out, t)
			}
		}
		for n := range numbers {
			if t, err := a.Store.TaskByNumber(ctx, tx, n); err == nil {
				add(t)
			}
		}
		for id := range ids {
			if t, err := a.Store.TaskByID(ctx, tx, id); err == nil {
				add(t)
			}
		}
		if ev.ExternalID != "" {
			ls, err := a.Store.LinksByExternalID(ctx, tx, ev.ExternalID)
			if err != nil {
				return err
			}
			for _, l := range ls {
				if t, err := a.Store.TaskByID(ctx, tx, l.TaskID); err == nil {
					add(t)
				}
			}
		}
		return nil
	})
	return out, err
}

// taskIDsInText 从一段文字里找本系统的任务链接，返回任务编号或序号。
func taskIDsInText(publicURL, text string) []string {
	base := strings.TrimRight(publicURL, "/") + "/tasks/"
	var out []string
	rest := text
	for {
		i := strings.Index(rest, base)
		if i < 0 {
			return out
		}
		rest = rest[i+len(base):]
		end := 0
		for end < len(rest) && (rest[end] == '_' || rest[end] == '-' || isAlnum(rest[end])) {
			end++
		}
		if end > 0 {
			out = append(out, rest[:end])
		}
	}
}

func isAlnum(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// applyCodeEvent 在一个任务上落这件外部事件：更新外部链接、必要时补上「代码 PR」交付物，再让内核决定要不要迁移。
// 返回被走掉的步骤名；没迁移时返回空串。
func (a *App) applyCodeEvent(ctx context.Context, sess *Session, taskID string, ev directory.CodeEvent) (string, error) {
	applied := ""
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		c, err := a.loadContext(ctx, tx, sess, taskID)
		if err != nil {
			return err
		}
		var events []domain.Event
		now := time.Now()
		ref := ev.Ref()
		// 1. 外部链接：建或更新，并写一条动态
		if ev.LinkKind != "" && ev.URL != "" {
			title := ev.Title
			if title == "" {
				title = ref
			}
			l := &store.ExternalLink{OrgID: sess.OrgID, TaskID: taskID, Provider: ev.Provider, Kind: ev.LinkKind, URL: ev.URL,
				Title: title, ExternalID: ev.ExternalID, Status: ev.Status, ActorName: ev.ActorName}
			created, err := a.Store.UpsertLink(ctx, tx, l)
			if err != nil {
				return err
			}
			typ := "ExternalLinkUpdated"
			if created {
				typ = "ExternalLinkAdded"
			}
			events = append(events, domain.Event{Type: typ, TaskID: taskID, At: now, Data: map[string]any{
				"kind": ev.LinkKind, "url": ev.URL, "title": title, "status": ev.Status,
				"status_title":       bothText(LinkStatusTitle, ev.Status),
				"kind_title":         bothText(LinkKindTitle, ev.LinkKind),
				"external_source":    ev.Provider,
				"external_user_name": ev.ActorName,
			}})
			// 内置流程里「开发完成」「修复完成」以「代码 PR」交付物为前提；PR 一挂上来就把它补上，
			// 否则 PR 合并事件永远只能写一条「前提不满足」的说明。
			if ev.LinkKind == "pr" && !hasArtifact(c.Task, "pr") {
				art := domain.Artifact{ID: store.NewID("art"), Type: "pr", Title: title, Ref: ev.URL, CreatedAt: now}
				c.Task.Artifacts = append(c.Task.Artifacts, art)
				if err := a.Store.InsertArtifact(ctx, tx, sess.OrgID, taskID, art); err != nil {
					return err
				}
				events = append(events, domain.Event{Type: "ArtifactAttached", TaskID: taskID, At: now,
					Data: map[string]any{"type": "pr", "title": title, "external_source": ev.Provider}})
			}
		}
		if len(events) > 0 {
			if err := a.insertEvents(ctx, tx, sess, events); err != nil {
				return err
			}
		}
		// 2. 让内核决定要不要迁移
		if ev.Kind == "" {
			return nil
		}
		tg := domain.ExternalTrigger{Source: domain.SourceGit, Event: ev.Kind, Provider: ev.Provider, UserName: ev.ActorName, Ref: ref, URL: ev.URL}
		tg.MemberID = a.memberOfGitUser(ctx, tx, ev.Provider, ev.ActorName)
		o := domain.ApplyExternal(c, tg)
		for _, e := range o.Events {
			if e.Type == "ExternalEventApplied" {
				applied, _ = e.Data["transition"].(string)
			}
		}
		if err := a.persist(ctx, tx, sess, c, o); err != nil {
			return err
		}
		if applied == "" {
			return nil
		}
		return a.notifyBlocked(ctx, tx, sess, c, ev)
	})
	return applied, err
}

// bothText 把一个按语言取名的函数摊成中英两条，好放进动态数据里。
func bothText(f func(string, i18n.Locale) string, key string) i18n.Text {
	return i18n.Text{i18n.ZhCN: f(key, i18n.ZhCN), i18n.EnUS: f(key, i18n.EnUS)}
}

func hasArtifact(t *domain.Task, typ string) bool {
	for _, a := range t.Artifacts {
		if a.Type == typ {
			return true
		}
	}
	return false
}

// memberOfGitUser 把外部登录名解析成本系统成员（没绑定时为空）。
func (a *App) memberOfGitUser(ctx context.Context, tx pgx.Tx, provider, login string) string {
	if login == "" {
		return ""
	}
	ids, err := a.Store.ListExternalIdentities(ctx, tx, provider)
	if err != nil {
		return ""
	}
	for _, x := range ids {
		if x.Kind == IdentityGitUser && strings.EqualFold(x.ExternalID, login) {
			return x.LocalID
		}
	}
	return ""
}

// notifyBlocked 外部事件把任务推进「等待中」的状态、且这件事本身是坏消息（检查失败）时，
// 给负责人一条「任务被阻塞」（ADR 0019 的通道与个人规则照旧生效）。
func (a *App) notifyBlocked(ctx context.Context, tx pgx.Tx, sess *Session, c *domain.Context, ev directory.CodeEvent) error {
	if !domain.IsBlockingEvent(ev.Kind) {
		return nil
	}
	st := c.Type.Workflow.State(c.Task.State)
	if st == nil || st.Label != domain.LabelWaiting {
		return nil
	}
	memberID := c.Task.AssigneeID
	if memberID == "" {
		return nil
	}
	if ag, err := a.Store.AgentByID(ctx, tx, memberID); err == nil {
		memberID = ag.OwnerMemberID
	}
	loc := a.localeOfMember(ctx, tx, sess.OrgID, memberID)
	title := i18n.Trf(loc, "notif.blocked", c.Task.Title)
	body := i18n.Trf(loc, "notif.blocked_body", providerTitle(ev.Provider, loc), domain.ExternalEventTitle[ev.Kind].In(loc), st.Title.In(loc))
	return a.notifyAndDeliver(ctx, tx, sess.OrgID, domain.NotifyBlocked,
		domain.Notification{MemberID: memberID, Title: title, Body: body, TaskID: c.Task.ID}, "")
}

// providerTitle 返回提供方的显示名（不认识时返回代码名）。
func providerTitle(key string, loc i18n.Locale) string {
	if p, ok := directory.Lookup(key); ok {
		return p.Title.In(loc)
	}
	return key
}
