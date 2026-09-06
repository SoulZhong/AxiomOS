package directory

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/teemo/axiomos/internal/i18n"
)

// Gitee 作为代码平台提供方登记到注册表（ADR 0020）：凭据是一个私人令牌。
//
// 用到的接口：
//
//	GET  https://gitee.com/api/v5/user/repos?per_page=100    → [{id, full_name, html_url, private}]
//	GET  https://gitee.com/api/v5/repos/{owner}/{repo}/hooks → [{id, url, password}]
//	POST /repos/{owner}/{repo}/hooks                          建回调
//	PATCH /repos/{owner}/{repo}/hooks/{id}                    改回调
//
// webhook 请求头：X-Gitee-Event（Merge Request Hook）、X-Gitee-Token（密码模式就是密钥本身；
// 签名模式是 base64(HMAC-SHA256(secret, "<X-Gitee-Timestamp>\n<secret>"))），两种都认。
// Gitee 没有统一的流水线回调，所以它只产生 PR 类事件，不产生检查通过 / 失败。
func init() {
	Register(Provider{
		Key:   "gitee",
		Title: i18n.T("Gitee", "Gitee"),
		Fields: []CredentialField{
			{Key: "token", Title: i18n.T("私人令牌", "Private token"), Secret: true, Placeholder: "xxxxxxxxxxxxxxxxxxxx",
				Hint: i18n.T("Gitee → 设置 → 私人令牌，勾上 projects 与 hook", "Gitee → Settings → Private access tokens, with the projects and hook scopes")},
		},
		Tip: GuideStep{Text: i18n.T("私人令牌在 Gitee 的「设置 → 私人令牌」页生成，要勾上仓库（projects）与 webhook（hook）两项权限。", "Create the private token under Gitee's “Settings → Private access tokens” with the projects and hook scopes."),
			URL: "https://gitee.com/personal_access_tokens"},
		Prerequisites: []i18n.Text{
			i18n.T("有一个能读到目标仓库的 Gitee 账号", "A Gitee account that can see the repositories"),
			i18n.T("生成一个包含 projects 与 hook 权限的私人令牌", "Create a private token with the projects and hook scopes"),
			i18n.T("本系统的地址要能被 Gitee 访问到", "AxiomOS must be reachable from Gitee"),
		},
		ConsoleURL:  func(map[string]string) string { return "https://gitee.com/personal_access_tokens" },
		NewCodeHost: func(creds map[string]string, opts Options) (CodeHost, error) { return NewGitee(creds, opts) },
	})
}

// Gitee 是 Gitee 客户端。
type Gitee struct{ api *codeAPI }

// NewGitee 建客户端；creds 需要 token。
func NewGitee(creds map[string]string, opts Options) (*Gitee, error) {
	hc, err := newHTTPClient(opts)
	if err != nil {
		return nil, err
	}
	tok := strings.TrimSpace(creds["token"])
	return &Gitee{api: &codeAPI{base: "https://gitee.com/api/v5", http: hc, auth: func(r *http.Request) {
		if tok != "" {
			r.Header.Set("Authorization", "token "+tok)
		}
	}}}, nil
}

type giteeRepo struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
	HTMLURL  string `json:"html_url"`
	Private  bool   `json:"private"`
}

// ListRepos 读当前令牌能看到的仓库（最多三页 300 个）。
func (g *Gitee) ListRepos(ctx context.Context) ([]Repo, error) {
	var out []Repo
	for page := 1; page <= 3; page++ {
		var rs []giteeRepo
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

type giteeHookRow struct {
	ID       int64  `json:"id"`
	URL      string `json:"url"`
	Password string `json:"password"`
}

func (g *Gitee) hooks(ctx context.Context, repo string) ([]giteeHookRow, error) {
	var rows []giteeHookRow
	err := g.api.do(ctx, http.MethodGet, "/repos/"+repo+"/hooks?per_page=100", nil, &rows)
	return rows, err
}

// EnsureWebhook 让这个仓库上恰好有一条指向 callbackURL 的回调，并带上我们的密钥（密码模式）。
func (g *Gitee) EnsureWebhook(ctx context.Context, repo, callbackURL, secret string) error {
	rows, err := g.hooks(ctx, repo)
	if err != nil {
		return err
	}
	body := map[string]any{"url": callbackURL, "password": secret, "merge_requests_events": true, "push_events": false}
	for _, h := range rows {
		if h.URL == callbackURL {
			return g.api.do(ctx, http.MethodPatch, fmt.Sprintf("/repos/%s/hooks/%d", repo, h.ID), body, nil)
		}
	}
	return g.api.do(ctx, http.MethodPost, "/repos/"+repo+"/hooks", body, nil)
}

// DiagnoseCode 跑代码平台的三项接入检查。
func (g *Gitee) DiagnoseCode(ctx context.Context, repos []string, callbackURL string) ([]Check, error) {
	return diagnoseCode(ctx, i18n.T("Gitee", "Gitee"), "https://gitee.com/personal_access_tokens", repos, callbackURL, g.ListRepos,
		func(ctx context.Context, repo string) (hookState, error) {
			rows, err := g.hooks(ctx, repo)
			if err != nil {
				return hookState{}, err
			}
			for _, h := range rows {
				if h.URL == callbackURL {
					return hookState{Found: true, Signed: true}, nil
				}
			}
			return hookState{}, nil
		})
}

// ---------- webhook ----------

type giteePayload struct {
	Action      string `json:"action"`
	PullRequest *struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		Merged  bool   `json:"merged"`
		State   string `json:"state"`
		Draft   bool   `json:"draft"`
		Head    struct {
			Ref string `json:"ref"`
		} `json:"head"`
	} `json:"pull_request"`
	Repository struct {
		FullName        string `json:"full_name"`
		PathWithSpace   string `json:"path_with_namespace"`
		HumanName       string `json:"human_name"`
		FullNameFromURL string `json:"url"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
		Name  string `json:"name"`
	} `json:"sender"`
	HookID int64 `json:"hook_id"`
}

// giteeSignature 是 Gitee 签名模式的期望值：base64(HMAC-SHA256(secret, "<timestamp>\n<secret>"))。
func giteeSignature(secret, timestamp string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "\n" + secret))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// VerifyWebhook 校验 Gitee 的密钥头（密码模式或签名模式）并把载荷翻译成统一的事件。
func (g *Gitee) VerifyWebhook(h http.Header, body []byte, secret string) (CodeEvent, error) {
	if secret != "" {
		got := h.Get("X-Gitee-Token")
		ok := hmac.Equal([]byte(got), []byte(secret))
		if ts := h.Get("X-Gitee-Timestamp"); !ok && ts != "" {
			ok = hmac.Equal([]byte(got), []byte(giteeSignature(secret, ts)))
		}
		if !ok {
			return CodeEvent{}, ErrBadSignature
		}
	}
	var p giteePayload
	if err := json.Unmarshal(body, &p); err != nil {
		return CodeEvent{}, fmt.Errorf("gitee: 载荷不是 JSON: %w", err)
	}
	repo := p.Repository.FullName
	if repo == "" {
		repo = p.Repository.PathWithSpace
	}
	actor := p.Sender.Login
	if actor == "" {
		actor = p.Sender.Name
	}
	ev := CodeEvent{Provider: "gitee", DeliveryID: h.Get("X-Gitee-Timestamp"), Repo: repo, ActorName: actor}
	if ev.DeliveryID == "" {
		ev.DeliveryID = BodyDigest(body)
	} else {
		ev.DeliveryID += ":" + BodyDigest(body)[:8]
	}
	if h.Get("X-Gitee-Event") != "Merge Request Hook" || p.PullRequest == nil {
		return ev, ErrNotForUs
	}
	pr := p.PullRequest
	ev.Number, ev.Title, ev.Body, ev.URL, ev.Branch = pr.Number, pr.Title, pr.Body, pr.HTMLURL, pr.Head.Ref
	ev.ExternalID = fmt.Sprintf("gitee:%s#%d", ev.Repo, pr.Number)
	ev.LinkKind = "pr"
	ev.Status = "open"
	if pr.Draft {
		ev.Status = "draft"
	}
	switch p.Action {
	case "open", "reopen":
		ev.Kind = "pr_opened"
	case "merge":
		ev.Kind, ev.Status = "pr_merged", "merged"
	case "close":
		if pr.Merged {
			ev.Kind, ev.Status = "pr_merged", "merged"
		} else {
			ev.Kind, ev.Status = "pr_closed", "closed"
		}
	}
	return ev, nil
}
