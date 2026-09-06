package directory

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/teemo/axiomos/internal/i18n"
)

// 代码平台提供方（ADR 0020）共用的东西：一个薄 HTTP 客户端、签名工具、三项接入检查的骨架。
// 每个平台一个文件（github.go、gitlab.go、gitee.go），只写自己的接口路径与载荷形状。

// codeAPI 是代码平台共用的 HTTP 小客户端：拼地址、带鉴权头、把非 2xx 变成 RejectedError。
type codeAPI struct {
	base string
	http *http.Client
	auth func(*http.Request)
}

func (c *codeAPI) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.base, "/")+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.auth != nil {
		c.auth(req)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return &UnreachableError{Err: err}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &RejectedError{Code: resp.StatusCode, Msg: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, trim(oneLine(string(raw)), 200))}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return &RejectedError{Code: resp.StatusCode, Msg: "响应不是预期的 JSON：" + trim(strings.TrimSpace(string(raw)), 120)}
	}
	return nil
}

// oneLine 把提供方的原话压成一行（错误句子要能整句念出来）。
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// hmacHex 算 hex(HMAC-SHA256(secret, body))，GitHub 与 Gitee 的签名都基于它。
func hmacHex(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// BodyDigest 是 webhook 载荷的指纹；提供方没给投递编号时拿它去重。
func BodyDigest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])[:32]
}

// hookState 是一个仓库上指向我们的 webhook 的状态。
type hookState struct {
	Found  bool // 有一条指向回调地址的 webhook
	Signed bool // 它配了密钥（签名能对上）
}

// diagnoseCode 是代码平台共用的三项接入检查：凭据可用 → 能读到仓库 → webhook 已建且带密钥。
// list 读仓库列表；hook 查某个仓库上的 webhook 状态（提供方各自实现）。
func diagnoseCode(ctx context.Context, title i18n.Text, fixURL string, repos []string, callbackURL string,
	list func(context.Context) ([]Repo, error),
	hook func(ctx context.Context, repo string) (hookState, error)) ([]Check, error) {
	credentials := &Check{Key: "credentials", Title: i18n.T("凭据可用", "Credentials work")}
	repoCheck := &Check{Key: "repos", Title: i18n.T("能读到仓库", "Repositories readable")}
	hookCheck := &Check{Key: "webhook", Title: i18n.T("回调已建好", "Webhook installed")}
	checks := []*Check{credentials, repoCheck, hookCheck}

	all, err := list(ctx)
	if err != nil {
		if isUnreachable(err) {
			return nil, err
		}
		_, msg, _ := rejectedCode(err)
		credentials.Status = CheckBlocked
		credentials.Detail = rawText("%s拒绝了这个令牌：%s。", "%s rejected this token: %s.", title.In(i18n.ZhCN), msg)
		credentials.Detail[i18n.EnUS] = fmt.Sprintf("%s rejected this token: %s.", title.In(i18n.EnUS), msg)
		credentials.Fix = i18n.T("重新填写访问令牌，并确认它有读取仓库与管理 webhook 的权限。", "Enter the access token again and make sure it can read repositories and manage webhooks.")
		credentials.FixURL = fixURL
		return finishChecks(checks), nil
	}
	credentials.Status = CheckOK
	credentials.Detail = i18n.T("令牌可用。", "The token works.")
	repoCheck.Status = CheckOK
	repoCheck.Detail = rawText("读到了 %d 个仓库。", "Read %d repositories.", len(all))
	if len(all) == 0 {
		repoCheck.Status = CheckBlocked
		repoCheck.Detail = i18n.T("这个令牌一个仓库都读不到。", "This token can see no repositories.")
		repoCheck.Fix = i18n.T("换一个有仓库读取权限的令牌。", "Use a token that can read repositories.")
		repoCheck.FixURL = fixURL
		return finishChecks(checks), nil
	}
	if len(repos) == 0 {
		hookCheck.Status = CheckTodo
		hookCheck.Detail = i18n.T("还没有选要接的仓库。", "No repositories are selected yet.")
		hookCheck.Fix = i18n.T("在下面挑几个仓库，系统会自动去建回调。", "Pick a few repositories below and the webhook will be created for you.")
		return finishChecks(checks), nil
	}
	var missing, unsigned []string
	for _, r := range repos {
		st, err := hook(ctx, r)
		if err != nil {
			if isUnreachable(err) {
				return nil, err
			}
			missing = append(missing, r)
			continue
		}
		if !st.Found {
			missing = append(missing, r)
		} else if !st.Signed {
			unsigned = append(unsigned, r)
		}
	}
	switch {
	case len(missing) > 0:
		hookCheck.Status = CheckBlocked
		hookCheck.Detail = rawText("这些仓库上还没有指向本系统的回调：%s。", "These repositories have no webhook pointing at AxiomOS yet: %s.", strings.Join(missing, "、"))
		hookCheck.Detail[i18n.EnUS] = fmt.Sprintf("These repositories have no webhook pointing at AxiomOS yet: %s.", strings.Join(missing, ", "))
		hookCheck.Fix = rawText("再保存一次仓库选择，系统会去建；也可以手工在仓库设置里把回调地址填成 %s。", "Save the repository selection again to have it created, or add a webhook with this URL by hand: %s.", callbackURL)
		hookCheck.Fix[i18n.EnUS] = fmt.Sprintf("Save the repository selection again to have it created, or add a webhook with this URL by hand: %s.", callbackURL)
	case len(unsigned) > 0:
		hookCheck.Status = CheckTodo
		hookCheck.Detail = rawText("这些仓库的回调没有配签名密钥：%s。", "The webhook on these repositories has no signing secret: %s.", strings.Join(unsigned, "、"))
		hookCheck.Detail[i18n.EnUS] = fmt.Sprintf("The webhook on these repositories has no signing secret: %s.", strings.Join(unsigned, ", "))
		hookCheck.Fix = i18n.T("再保存一次仓库选择，系统会把密钥补上；没有密钥的回调会被拒收。", "Save the repository selection again to fill the secret in; requests without a valid signature are rejected.")
	default:
		hookCheck.Status = CheckOK
		hookCheck.Detail = rawText("%d 个仓库的回调都在，签名密钥也对。", "All %d repositories have a webhook with the right signing secret.", len(repos))
	}
	return finishChecks(checks), nil
}

func isUnreachable(err error) bool {
	_, ok := err.(*UnreachableError)
	return ok
}
