package directory

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/teemo/axiomos/internal/i18n"
)

// GitLab 作为代码平台提供方登记到注册表（ADR 0020）：凭据是一个访问令牌，私有化部署另填接口地址。
//
// 用到的接口：
//
//	GET  {api}/projects?membership=true&per_page=100        → [{id, path_with_namespace, web_url, visibility}]
//	GET  {api}/projects/{编码后的全名}/hooks                 → [{id, url, token, merge_requests_events, pipeline_events}]
//	POST {api}/projects/{编码后的全名}/hooks                 建回调
//	PUT  {api}/projects/{编码后的全名}/hooks/{id}            改回调
//
// webhook 请求头：X-Gitlab-Event（Merge Request Hook / Pipeline Hook）、X-Gitlab-Event-UUID（投递编号）、
// X-Gitlab-Token（就是密钥本身，逐字节比对）。
func init() {
	Register(Provider{
		Key:   "gitlab",
		Title: i18n.T("GitLab", "GitLab"),
		Fields: []CredentialField{
			{Key: "token", Title: i18n.T("访问令牌", "Access token"), Secret: true, Placeholder: "glpat-xxxxxxxxxxxx",
				Hint: i18n.T("GitLab → 用户设置 → 访问令牌，勾上 api 范围", "GitLab → User settings → Access tokens, with the api scope")},
			{Key: "api_base", Title: i18n.T("接口地址", "API base URL"), Optional: true, Placeholder: "https://gitlab.com/api/v4",
				Hint: i18n.T("用 gitlab.com 时留空；自建的填 https://你的域名/api/v4", "Leave empty for gitlab.com; for a self-hosted instance use https://your-host/api/v4")},
		},
		Tip: GuideStep{Text: i18n.T("令牌在 GitLab 的「用户设置 → 访问令牌」页生成，范围勾 api（它同时包含读仓库与管理回调）。", "Create the token under GitLab's “User settings → Access tokens” with the api scope, which covers both reading repositories and managing webhooks."),
			URL: "https://gitlab.com/-/user_settings/personal_access_tokens"},
		Prerequisites: []i18n.Text{
			i18n.T("有一个能读到目标项目的 GitLab 账号", "A GitLab account that can see the projects"),
			i18n.T("生成一个范围为 api 的访问令牌", "Create an access token with the api scope"),
			i18n.T("本系统的地址要能被 GitLab 访问到", "AxiomOS must be reachable from GitLab"),
		},
		ConsoleURL: func(creds map[string]string) string {
			return gitlabWebBase(creds["api_base"]) + "/-/user_settings/personal_access_tokens"
		},
		NewCodeHost: func(creds map[string]string, opts Options) (CodeHost, error) { return NewGitLab(creds, opts) },
	})
}

// GitLab 是 GitLab 客户端。
type GitLab struct {
	api *codeAPI
	web string
}

// NewGitLab 建客户端；creds 需要 token，可选 api_base。
func NewGitLab(creds map[string]string, opts Options) (*GitLab, error) {
	hc, err := newHTTPClient(opts)
	if err != nil {
		return nil, err
	}
	base := strings.TrimSpace(creds["api_base"])
	if base == "" {
		base = "https://gitlab.com/api/v4"
	}
	if err := CheckEgressURL(base, DefaultEgress()); err != nil {
		return nil, err
	}
	tok := strings.TrimSpace(creds["token"])
	g := &GitLab{web: gitlabWebBase(base)}
	g.api = &codeAPI{base: base, http: hc, auth: func(r *http.Request) {
		if tok != "" {
			r.Header.Set("PRIVATE-TOKEN", tok)
		}
	}}
	return g, nil
}

func gitlabWebBase(apiBase string) string {
	b := strings.TrimSpace(apiBase)
	if b == "" {
		return "https://gitlab.com"
	}
	if i := strings.Index(b, "/api/"); i > 0 {
		return b[:i]
	}
	return strings.TrimRight(b, "/")
}

type glProject struct {
	ID                int64  `json:"id"`
	PathWithNamespace string `json:"path_with_namespace"`
	WebURL            string `json:"web_url"`
	Visibility        string `json:"visibility"`
}

// ListRepos 读当前令牌所属账号参与的项目（最多三页 300 个）。
func (g *GitLab) ListRepos(ctx context.Context) ([]Repo, error) {
	var out []Repo
	for page := 1; page <= 3; page++ {
		var ps []glProject
		if err := g.api.do(ctx, http.MethodGet, fmt.Sprintf("/projects?membership=true&simple=true&order_by=path&sort=asc&per_page=100&page=%d", page), nil, &ps); err != nil {
			return nil, err
		}
		for _, p := range ps {
			out = append(out, Repo{ID: strconv.FormatInt(p.ID, 10), FullName: p.PathWithNamespace, URL: p.WebURL, Private: p.Visibility != "public"})
		}
		if len(ps) < 100 {
			break
		}
	}
	return out, nil
}

type glHookRow struct {
	ID    int64  `json:"id"`
	URL   string `json:"url"`
	Token string `json:"token"`
}

func (g *GitLab) hooks(ctx context.Context, repo string) ([]glHookRow, error) {
	var rows []glHookRow
	err := g.api.do(ctx, http.MethodGet, "/projects/"+url.PathEscape(repo)+"/hooks?per_page=100", nil, &rows)
	return rows, err
}

// EnsureWebhook 让这个项目上恰好有一条指向 callbackURL 的回调，并带上我们的密钥。
func (g *GitLab) EnsureWebhook(ctx context.Context, repo, callbackURL, secret string) error {
	rows, err := g.hooks(ctx, repo)
	if err != nil {
		return err
	}
	body := map[string]any{"url": callbackURL, "token": secret, "merge_requests_events": true, "pipeline_events": true,
		"push_events": false, "enable_ssl_verification": strings.HasPrefix(callbackURL, "https://")}
	for _, h := range rows {
		if h.URL == callbackURL {
			return g.api.do(ctx, http.MethodPut, fmt.Sprintf("/projects/%s/hooks/%d", url.PathEscape(repo), h.ID), body, nil)
		}
	}
	return g.api.do(ctx, http.MethodPost, "/projects/"+url.PathEscape(repo)+"/hooks", body, nil)
}

// DiagnoseCode 跑代码平台的三项接入检查。
func (g *GitLab) DiagnoseCode(ctx context.Context, repos []string, callbackURL string) ([]Check, error) {
	return diagnoseCode(ctx, i18n.T("GitLab", "GitLab"), g.web+"/-/user_settings/personal_access_tokens", repos, callbackURL, g.ListRepos,
		func(ctx context.Context, repo string) (hookState, error) {
			rows, err := g.hooks(ctx, repo)
			if err != nil {
				return hookState{}, err
			}
			for _, h := range rows {
				if h.URL == callbackURL {
					// GitLab 不回显密钥；只要回调在，就当它是我们建的（保存仓库选择时总会把密钥写一遍）
					return hookState{Found: true, Signed: true}, nil
				}
			}
			return hookState{}, nil
		})
}

// ---------- webhook ----------

type glPayload struct {
	ObjectKind string `json:"object_kind"`
	User       struct {
		Username string `json:"username"`
		Name     string `json:"name"`
	} `json:"user"`
	Project struct {
		PathWithNamespace string `json:"path_with_namespace"`
		WebURL            string `json:"web_url"`
	} `json:"project"`
	ObjectAttributes struct {
		IID             int    `json:"iid"`
		Title           string `json:"title"`
		Description     string `json:"description"`
		SourceBranch    string `json:"source_branch"`
		URL             string `json:"url"`
		State           string `json:"state"`
		Action          string `json:"action"`
		Draft           bool   `json:"draft"`
		WorkInProgress  bool   `json:"work_in_progress"`
		Status          string `json:"status"` // 流水线用
		Ref             string `json:"ref"`    // 流水线用
		DetailedStatus  string `json:"detailed_status"`
		MergeStatusText string `json:"merge_status"`
	} `json:"object_attributes"`
	MergeRequest *struct {
		IID          int    `json:"iid"`
		Title        string `json:"title"`
		SourceBranch string `json:"source_branch"`
		URL          string `json:"url"`
	} `json:"merge_request"`
	Changes struct {
		Draft *struct {
			Previous bool `json:"previous"`
			Current  bool `json:"current"`
		} `json:"draft"`
	} `json:"changes"`
}

// VerifyWebhook 校验 GitLab 的密钥头并把载荷翻译成统一的事件。
func (g *GitLab) VerifyWebhook(h http.Header, body []byte, secret string) (CodeEvent, error) {
	if secret != "" {
		if !hmac.Equal([]byte(h.Get("X-Gitlab-Token")), []byte(secret)) {
			return CodeEvent{}, ErrBadSignature
		}
	}
	var p glPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return CodeEvent{}, fmt.Errorf("gitlab: 载荷不是 JSON: %w", err)
	}
	ev := CodeEvent{Provider: "gitlab", DeliveryID: h.Get("X-Gitlab-Event-UUID"), Repo: p.Project.PathWithNamespace, ActorName: p.User.Username}
	if ev.ActorName == "" {
		ev.ActorName = p.User.Name
	}
	if ev.DeliveryID == "" {
		ev.DeliveryID = BodyDigest(body)
	}
	a := p.ObjectAttributes
	switch h.Get("X-Gitlab-Event") {
	case "Merge Request Hook":
		ev.Number, ev.Title, ev.Body, ev.URL, ev.Branch = a.IID, a.Title, a.Description, a.URL, a.SourceBranch
		ev.ExternalID = fmt.Sprintf("gitlab:%s#%d", ev.Repo, a.IID)
		ev.LinkKind = "pr"
		draft := a.Draft || a.WorkInProgress
		ev.Status = "open"
		if draft {
			ev.Status = "draft"
		}
		switch a.Action {
		case "open", "reopen":
			ev.Kind = "pr_opened"
		case "merge":
			ev.Kind, ev.Status = "pr_merged", "merged"
		case "close":
			ev.Kind, ev.Status = "pr_closed", "closed"
		case "update":
			if d := p.Changes.Draft; d != nil && d.Previous && !d.Current {
				ev.Kind, ev.Status = "pr_ready", "open"
			}
		}
		return ev, nil
	case "Pipeline Hook":
		ev.Branch = a.Ref
		if p.MergeRequest != nil {
			ev.Number, ev.URL, ev.Branch = p.MergeRequest.IID, p.MergeRequest.URL, p.MergeRequest.SourceBranch
			ev.ExternalID = fmt.Sprintf("gitlab:%s#%d", ev.Repo, p.MergeRequest.IID)
		}
		switch a.Status {
		case "success":
			ev.Kind, ev.Status = "ci_passed", "passed"
		case "failed":
			ev.Kind, ev.Status = "ci_failed", "failed"
		default:
			return ev, ErrNotForUs
		}
		return ev, nil
	}
	return ev, ErrNotForUs
}
