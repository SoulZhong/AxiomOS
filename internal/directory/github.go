package directory

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/teemo/axiomos/internal/i18n"
)

// GitHub 作为代码平台提供方登记到注册表（ADR 0020）：凭据是一个访问令牌，
// 企业版（GHES）另填接口地址。它不参与 IM 集成，也不发通知。
//
// 用到的接口：
//
//	GET  {api}/user/repos?per_page=100&sort=full_name       → [{id, full_name, html_url, private}]
//	GET  {api}/repos/{owner}/{repo}/hooks                   → [{id, config{url, secret}, events, active}]
//	POST {api}/repos/{owner}/{repo}/hooks                   建回调
//	PATCH{api}/repos/{owner}/{repo}/hooks/{id}              改回调（补密钥）
//
// webhook 请求头：X-GitHub-Event（pull_request / check_suite / ping）、X-GitHub-Delivery（投递编号）、
// X-Hub-Signature-256（sha256=hex(HMAC-SHA256(secret, body))）。
func init() {
	Register(Provider{
		Key:   "github",
		Title: i18n.T("GitHub", "GitHub"),
		Fields: []CredentialField{
			{Key: "token", Title: i18n.T("访问令牌", "Access token"), Secret: true, Placeholder: "ghp_xxxxxxxxxxxx",
				Hint: i18n.T("GitHub → Settings → Developer settings → Personal access tokens，勾上 repo 与 admin:repo_hook", "GitHub → Settings → Developer settings → Personal access tokens, with the repo and admin:repo_hook scopes")},
			{Key: "api_base", Title: i18n.T("接口地址", "API base URL"), Optional: true, Placeholder: "https://api.github.com",
				Hint: i18n.T("用 github.com 时留空；GitHub 企业版填 https://你的域名/api/v3", "Leave empty for github.com; for GitHub Enterprise use https://your-host/api/v3")},
		},
		Tip: GuideStep{Text: i18n.T("令牌在 GitHub 的「Developer settings → Personal access tokens」页生成，要能读仓库并管理仓库的 webhook。", "Create the token under GitHub's “Developer settings → Personal access tokens”; it must be able to read repositories and manage repository webhooks."),
			URL: "https://github.com/settings/tokens"},
		Prerequisites: []i18n.Text{
			i18n.T("有一个能读到目标仓库的 GitHub 账号或组织", "A GitHub account or organization that can see the repositories"),
			i18n.T("生成一个访问令牌，权限包含 repo 与 admin:repo_hook", "Create an access token with the repo and admin:repo_hook scopes"),
			i18n.T("本系统的地址要能被 GitHub 访问到（回调是 GitHub 主动请求）", "AxiomOS must be reachable from GitHub, which calls the webhook URL"),
		},
		ConsoleURL:  func(creds map[string]string) string { return githubWebBase(creds["api_base"]) + "/settings/tokens" },
		NewCodeHost: func(creds map[string]string, opts Options) (CodeHost, error) { return NewGitHub(creds, opts) },
	})
}

// GitHub 是 GitHub / GitHub 企业版的客户端。
type GitHub struct {
	api   *codeAPI
	token string
	web   string
}

// NewGitHub 建客户端；creds 需要 token，可选 api_base。
func NewGitHub(creds map[string]string, opts Options) (*GitHub, error) {
	hc, err := newHTTPClient(opts)
	if err != nil {
		return nil, err
	}
	base := strings.TrimSpace(creds["api_base"])
	if base == "" {
		base = "https://api.github.com"
	}
	if err := CheckEgressURL(base, DefaultEgress()); err != nil {
		return nil, err
	}
	tok := strings.TrimSpace(creds["token"])
	g := &GitHub{token: tok, web: githubWebBase(base)}
	g.api = &codeAPI{base: base, http: hc, auth: func(r *http.Request) {
		if tok != "" {
			r.Header.Set("Authorization", "Bearer "+tok)
		}
		r.Header.Set("Accept", "application/vnd.github+json")
		r.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}}
	return g, nil
}

// githubWebBase 从接口地址反推网页地址（github.com 或企业版的域名）。
func githubWebBase(apiBase string) string {
	b := strings.TrimSpace(apiBase)
	if b == "" || strings.Contains(b, "api.github.com") {
		return "https://github.com"
	}
	if i := strings.Index(b, "/api/"); i > 0 {
		return b[:i]
	}
	return strings.TrimRight(b, "/")
}

type ghRepo struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
	HTMLURL  string `json:"html_url"`
	Private  bool   `json:"private"`
}

// ListRepos 读当前令牌能看到的仓库（最多三页 300 个）。
func (g *GitHub) ListRepos(ctx context.Context) ([]Repo, error) {
	var out []Repo
	for page := 1; page <= 3; page++ {
		var rs []ghRepo
		if err := g.api.do(ctx, http.MethodGet, fmt.Sprintf("/user/repos?per_page=100&sort=full_name&page=%d", page), nil, &rs); err != nil {
			return nil, err
		}
		for _, r := range rs {
			out = append(out, Repo{ID: strconv.FormatInt(r.ID, 10), FullName: r.FullName, URL: r.HTMLURL, Private: r.Private})
		}
		if len(rs) < 100 {
			break
		}
	}
	return out, nil
}

type ghHookRow struct {
	ID     int64 `json:"id"`
	Active bool  `json:"active"`
	Config struct {
		URL    string `json:"url"`
		Secret string `json:"secret"`
	} `json:"config"`
}

var githubHookEvents = []string{"pull_request", "check_suite"}

func (g *GitHub) hooks(ctx context.Context, repo string) ([]ghHookRow, error) {
	var rows []ghHookRow
	err := g.api.do(ctx, http.MethodGet, "/repos/"+repo+"/hooks?per_page=100", nil, &rows)
	return rows, err
}

// EnsureWebhook 让这个仓库上恰好有一条指向 callbackURL 的回调，并带上我们的密钥。
func (g *GitHub) EnsureWebhook(ctx context.Context, repo, callbackURL, secret string) error {
	rows, err := g.hooks(ctx, repo)
	if err != nil {
		return err
	}
	cfg := map[string]any{"url": callbackURL, "content_type": "json", "secret": secret, "insecure_ssl": "0"}
	for _, h := range rows {
		if h.Config.URL == callbackURL {
			return g.api.do(ctx, http.MethodPatch, fmt.Sprintf("/repos/%s/hooks/%d", repo, h.ID),
				map[string]any{"active": true, "events": githubHookEvents, "config": cfg}, nil)
		}
	}
	return g.api.do(ctx, http.MethodPost, "/repos/"+repo+"/hooks",
		map[string]any{"name": "web", "active": true, "events": githubHookEvents, "config": cfg}, nil)
}

// DiagnoseCode 跑代码平台的三项接入检查。
func (g *GitHub) DiagnoseCode(ctx context.Context, repos []string, callbackURL string) ([]Check, error) {
	return diagnoseCode(ctx, i18n.T("GitHub", "GitHub"), g.web+"/settings/tokens", repos, callbackURL, g.ListRepos,
		func(ctx context.Context, repo string) (hookState, error) {
			rows, err := g.hooks(ctx, repo)
			if err != nil {
				return hookState{}, err
			}
			for _, h := range rows {
				if h.Config.URL == callbackURL {
					// GitHub 不回显密钥，只回显 "********"；有值就算配过
					return hookState{Found: true, Signed: h.Config.Secret != ""}, nil
				}
			}
			return hookState{}, nil
		})
}

// ---------- webhook ----------

type ghPR struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
	Draft   bool   `json:"draft"`
	Merged  bool   `json:"merged"`
	State   string `json:"state"`
	Head    struct {
		Ref string `json:"ref"`
	} `json:"head"`
}

type ghPayload struct {
	Action      string `json:"action"`
	PullRequest *ghPR  `json:"pull_request"`
	Repository  struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
	CheckSuite *struct {
		Conclusion   string `json:"conclusion"`
		HeadBranch   string `json:"head_branch"`
		PullRequests []ghPR `json:"pull_requests"`
	} `json:"check_suite"`
}

// VerifyWebhook 校验 GitHub 的签名并把载荷翻译成统一的事件。
func (g *GitHub) VerifyWebhook(h http.Header, body []byte, secret string) (CodeEvent, error) {
	if secret != "" {
		got := h.Get("X-Hub-Signature-256")
		want := "sha256=" + hmacHex(secret, body)
		if !hmac.Equal([]byte(got), []byte(want)) {
			return CodeEvent{}, ErrBadSignature
		}
	}
	var p ghPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return CodeEvent{}, fmt.Errorf("github: 载荷不是 JSON: %w", err)
	}
	ev := CodeEvent{Provider: "github", DeliveryID: h.Get("X-GitHub-Delivery"), Repo: p.Repository.FullName, ActorName: p.Sender.Login}
	if ev.DeliveryID == "" {
		ev.DeliveryID = BodyDigest(body)
	}
	switch h.Get("X-GitHub-Event") {
	case "pull_request":
		if p.PullRequest == nil {
			return ev, ErrNotForUs
		}
		pr := p.PullRequest
		ev.Number, ev.Title, ev.Body, ev.URL, ev.Branch = pr.Number, pr.Title, pr.Body, pr.HTMLURL, pr.Head.Ref
		ev.ExternalID = fmt.Sprintf("github:%s#%d", ev.Repo, pr.Number)
		ev.LinkKind = "pr"
		ev.Kind, ev.Status = githubPRKind(p.Action, pr)
		return ev, nil
	case "check_suite":
		cs := p.CheckSuite
		if cs == nil || p.Action != "completed" {
			return ev, ErrNotForUs
		}
		ev.Branch = cs.HeadBranch
		if len(cs.PullRequests) > 0 {
			ev.Number = cs.PullRequests[0].Number
			ev.ExternalID = fmt.Sprintf("github:%s#%d", ev.Repo, ev.Number)
		}
		switch cs.Conclusion {
		case "success":
			ev.Kind, ev.Status = "ci_passed", "passed"
		case "failure", "timed_out", "startup_failure":
			ev.Kind, ev.Status = "ci_failed", "failed"
		default:
			return ev, ErrNotForUs
		}
		return ev, nil
	case "ping":
		return ev, ErrNotForUs
	}
	return ev, ErrNotForUs
}

// githubPRKind 把 pull_request 的动作映射成六种外部事件之一（映射不到时 kind 为空，只更新链接）。
func githubPRKind(action string, pr *ghPR) (kind, status string) {
	status = "open"
	if pr.Draft {
		status = "draft"
	}
	switch action {
	case "opened", "reopened":
		return "pr_opened", status
	case "ready_for_review":
		return "pr_ready", "open"
	case "converted_to_draft":
		return "", "draft"
	case "closed":
		if pr.Merged {
			return "pr_merged", "merged"
		}
		return "pr_closed", "closed"
	}
	return "", status
}
