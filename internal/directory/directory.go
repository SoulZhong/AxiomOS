// Package directory 是外部目录（ADR 0017）的提供方客户端。每个平台一个文件（feishu.go、wecom.go…），
// 实现同一个 Directory 接口并在包初始化时 Register 到注册表；应用层、存储层、接口层只认注册表，
// 不出现任何平台名的分支（ADR 0017 补记）。它只负责"把外部目录读出来"，不碰数据库；对照与写入在 app 层。
//
// 新增一个平台 = 新增一个文件（含 init 里的 Register）+ 词汇表一条说明，其余代码不改。
package directory

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

// Dept 是外部目录里的部门（统一模型）。
type Dept struct {
	ID       string // 提供方的部门编号
	Name     string
	ParentID string // 父部门编号；顶层部门的父是提供方的根部门编号
	Deleted  bool
}

// User 是外部目录里的人（统一模型）。
type User struct {
	ID      string // 提供方的人员编号
	Name    string
	Email   string
	Mobile  string
	DeptIDs []string // 所属部门编号
	Active  bool     // 仍在企业里、未被禁用
}

// Directory 是提供方客户端要实现的接口。
type Directory interface {
	// Token 取一次访问令牌，用来验证凭据。
	Token(ctx context.Context) (string, error)
	// Department 读一个部门。
	Department(ctx context.Context, id string) (*Dept, error)
	// Departments 读某部门下的全部子部门（含多级，不含自身）。
	Departments(ctx context.Context, rootID string) ([]Dept, error)
	// Users 读某部门的直属人员。
	Users(ctx context.Context, deptID string) ([]User, error)
}

// ScopeLister 是可选能力：列出应用被授权访问的部门编号。
// 有的平台（飞书）允许把应用的通讯录权限范围限定在部分部门，这时读根部门会被拒绝，
// 只能从这里拿到"能读哪些部门"。
type ScopeLister interface {
	AuthorizedDepartments(ctx context.Context) ([]string, error)
}

// CheckStatus 是一项接入检查的结果。
type CheckStatus string

const (
	CheckOK      CheckStatus = "ok"      // 通过
	CheckTodo    CheckStatus = "todo"    // 有缺项但不挡同步（比如读不到邮箱）
	CheckBlocked CheckStatus = "blocked" // 不修好就不能同步
	CheckSkipped CheckStatus = "skipped" // 前面的检查没过，这项没法判断
)

// Check 是提供方声明的一项接入检查（ADR 0017 补记二）。Fix 是一句"去哪儿点什么"，FixURL 尽量直达
// 这个应用在提供方控制台里的那一页。权限的英文代号只放进 Detail，不进 Title 与 Fix。
type Check struct {
	Key      string      // 代码名，如 credentials、scope
	Title    i18n.Text   // 一眼能看懂的标题，如「凭据可用」
	Status   CheckStatus //
	Detail   i18n.Text   // 我们实际观察到了什么，可带提供方原话
	Fix      i18n.Text   // 一句怎么修；通过时为空
	FixURL   string      // 控制台里的那一页
	Blocking bool        // 为真且 Status 为 blocked 时同步被拒
	// Roots 只在权限范围检查里出现：应用实际被授权的部门，向导让人从里面挑同步范围。
	Roots []Dept
}

// Diagnoser 是可选能力：按顺序跑一组接入检查。rootID 是配置里的同步根部门（空表示提供方的根）。
// 只在连不上提供方时返回错误；凭据错、权限缺都表达为 blocked 的检查项。
type Diagnoser interface {
	Diagnose(ctx context.Context, rootID string) ([]Check, error)
}

// TenantNamer 是可选能力：读租户（企业）名称。没有这个能力的提供方不实现它，调用方跳过。
type TenantNamer interface {
	TenantName(ctx context.Context) (string, error)
}

// RejectedError 是提供方明确拒绝（凭据错、权限不足等），带提供方返回的原话。
type RejectedError struct {
	Code int
	Msg  string
}

func (e *RejectedError) Error() string {
	return fmt.Sprintf("provider rejected (%d): %s", e.Code, e.Msg)
}

// UnreachableError 是网络层面连不上。
type UnreachableError struct{ Err error }

func (e *UnreachableError) Error() string { return "provider unreachable: " + e.Err.Error() }
func (e *UnreachableError) Unwrap() error { return e.Err }

// ---------- 注册表 ----------

// CredentialField 是提供方声明的一个凭据字段；前端表单按它渲染，应用层按它校验。
type CredentialField struct {
	Key         string    // 存储与接口里的键，如 app_id
	Title       i18n.Text // 显示名，如「App ID」
	Secret      bool      // 保密字段：加密入库，界面只显示是否已设置
	Placeholder string
	Hint        i18n.Text // 到哪里去拿这个值
}

// Options 是创建客户端时与凭据无关的选项。
type Options struct {
	// ProxyURL 非空时经它出网（私有化部署，ADR 0014）。
	ProxyURL string
	// HTTPClient 非空时直接使用（测试注入）；否则按 ProxyURL 建一个。
	HTTPClient *http.Client
}

// Provider 是一个已接入的平台的声明：怎么叫、要哪些凭据、根部门编号是什么、怎么建客户端。
type Provider struct {
	Key              string    // 代码名，如 feishu、wecom；也是外部身份与成员来源里存的值
	Title            i18n.Text // 显示名（zh/en）
	RootDepartmentID string    // 提供方的根部门编号（飞书 "0"，企业微信 "1"）
	Fields           []CredentialField
	Prerequisites    []i18n.Text // 接入前置条件，逐条给人看
	Tip              GuideStep   // 一句话提醒凭据在哪儿找，可带控制台链接
	New              func(creds map[string]string, opts Options) (Directory, error)
	// ConsoleURL 按凭据算出这个应用在提供方控制台里的首页（可选），前端的「打开控制台」用它。
	ConsoleURL func(creds map[string]string) string

	hidden bool // 测试注册的提供方：Lookup 能找到，Providers 不列出
}

// Field 按键找凭据字段。
func (p Provider) Field(key string) (CredentialField, bool) {
	for _, f := range p.Fields {
		if f.Key == key {
			return f, true
		}
	}
	return CredentialField{}, false
}

var (
	regMu    sync.RWMutex
	registry []Provider
)

// Register 登记一个提供方；包初始化时调用。键重复或声明不完整是编程错误，直接 panic。
func Register(p Provider) {
	if err := validateProvider(p); err != nil {
		panic("directory: " + err.Error())
	}
	regMu.Lock()
	defer regMu.Unlock()
	for _, x := range registry {
		if x.Key == p.Key {
			panic("directory: provider " + p.Key + " registered twice")
		}
	}
	registry = append(registry, p)
}

// RegisterForTest 登记（或替换）一个只供测试用的提供方：Lookup 能找到它，Providers 不会列出它，
// 所以不会漏进生产的提供方列表。测试里用它把内存目录（Fake）挂到应用层。
func RegisterForTest(p Provider) {
	if err := validateProvider(p); err != nil {
		panic("directory: " + err.Error())
	}
	p.hidden = true
	regMu.Lock()
	defer regMu.Unlock()
	for i, x := range registry {
		if x.Key == p.Key {
			if !x.hidden {
				panic("directory: cannot replace production provider " + p.Key)
			}
			registry[i] = p
			return
		}
	}
	registry = append(registry, p)
}

func validateProvider(p Provider) error {
	if strings.TrimSpace(p.Key) == "" || p.Key != strings.ToLower(strings.TrimSpace(p.Key)) {
		return fmt.Errorf("provider key %q must be a non-empty lower-case code", p.Key)
	}
	if p.Title.In(i18n.ZhCN) == "" || p.RootDepartmentID == "" || p.New == nil {
		return fmt.Errorf("provider %s must declare Title, RootDepartmentID and New", p.Key)
	}
	seen := map[string]bool{}
	for _, f := range p.Fields {
		if f.Key == "" || f.Title.In(i18n.ZhCN) == "" || seen[f.Key] {
			return fmt.Errorf("provider %s has a bad or duplicate field %q", p.Key, f.Key)
		}
		seen[f.Key] = true
	}
	return nil
}

// Providers 列出全部生产提供方，顺序即注册顺序（包内文件按文件名初始化：feishu、wecom…）。
func Providers() []Provider {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Provider, 0, len(registry))
	for _, p := range registry {
		if !p.hidden {
			out = append(out, p)
		}
	}
	return out
}

// Lookup 按代码名找提供方（含测试注册的）。
func Lookup(key string) (Provider, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	for _, p := range registry {
		if p.Key == key {
			return p, true
		}
	}
	return Provider{}, false
}

// ---------- 提供方共用的小工具 ----------

// ErrBadProxy 表示代理地址不合法。
var ErrBadProxy = errors.New("bad proxy url")

// newHTTPClient 按选项建 HTTP 客户端：优先用注入的，否则按 ProxyURL（或环境变量）出网。
func newHTTPClient(opts Options) (*http.Client, error) {
	if opts.HTTPClient != nil {
		return opts.HTTPClient, nil
	}
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment, TLSHandshakeTimeout: 10 * time.Second}
	if p := strings.TrimSpace(opts.ProxyURL); p != "" {
		u, err := url.Parse(p)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("%w: %q", ErrBadProxy, p)
		}
		tr.Proxy = http.ProxyURL(u)
	}
	return &http.Client{Transport: tr, Timeout: 30 * time.Second}, nil
}

func trim(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// GuideStep 是给人看的一句提醒，可选一个打开控制台的链接。
type GuideStep struct {
	Text i18n.Text
	URL  string
}

// ---------- 接入检查共用的小工具 ----------

// rejectedCode 拆出提供方拒绝的错误码与原话；不是拒绝类错误时 ok 为假。
func rejectedCode(err error) (code int, msg string, ok bool) {
	var rj *RejectedError
	if errors.As(err, &rj) {
		return rj.Code, rj.Msg, true
	}
	if err != nil {
		return 0, err.Error(), false
	}
	return 0, "", false
}

// rawText 用 Sprintf 拼一条带提供方原话的中英文本。
func rawText(zh, en string, args ...any) i18n.Text {
	return i18n.T(fmt.Sprintf(zh, args...), fmt.Sprintf(en, args...))
}

// finishChecks 收尾：没有结论的检查标成 skipped，blocked 的标 Blocking，并把指针列表摊平。
func finishChecks(checks []*Check) []Check {
	out := make([]Check, 0, len(checks))
	for _, c := range checks {
		if c.Status == "" {
			c.Status = CheckSkipped
			if c.Detail.IsZero() {
				c.Detail = i18n.T("前面的检查没通过，这项没法判断。", "An earlier check failed, so this one could not be evaluated.")
			}
		}
		c.Blocking = c.Status == CheckBlocked
		out = append(out, *c)
	}
	return out
}

// FirstBlocked 返回第一项阻塞的检查；没有则为 nil。
func FirstBlocked(checks []Check) *Check {
	for i := range checks {
		if checks[i].Status == CheckBlocked {
			return &checks[i]
		}
	}
	return nil
}

// ScopeRoots 返回权限范围检查列出的被授权部门（提供方没有这项检查时为空）。
func ScopeRoots(checks []Check) []Dept {
	for _, c := range checks {
		if c.Key == "scope" {
			return c.Roots
		}
	}
	return nil
}

// DiagnoseBasic 是没有实现 Diagnoser 的提供方的通用检查：凭据可用、能读到部门树。
// 连不上时返回错误；凭据被拒表达为 blocked。
func DiagnoseBasic(ctx context.Context, title i18n.Text, dir Directory, rootID string) ([]Check, error) {
	credentials := &Check{Key: "credentials", Title: i18n.T("凭据可用", "Credentials work")}
	structure := &Check{Key: "structure", Title: i18n.T("能读到部门树", "Department tree readable")}
	checks := []*Check{credentials, structure}
	if _, err := dir.Token(ctx); err != nil {
		var un *UnreachableError
		if errors.As(err, &un) {
			return nil, err
		}
		_, msg, _ := rejectedCode(err)
		credentials.Status = CheckBlocked
		credentials.Detail = rawText("%s拒绝了这组凭据：%s。", "%s rejected these credentials: %s.", title.In(i18n.ZhCN), msg)
		credentials.Detail[i18n.EnUS] = fmt.Sprintf("%s rejected these credentials: %s.", title.In(i18n.EnUS), msg)
		credentials.Fix = i18n.T("重新填写凭据。", "Enter the credentials again.")
		return finishChecks(checks), nil
	}
	credentials.Status = CheckOK
	credentials.Detail = i18n.T("已取到访问令牌。", "Got an access token.")
	ds, err := dir.Departments(ctx, rootID)
	if err != nil {
		var un *UnreachableError
		if errors.As(err, &un) {
			return nil, err
		}
		_, msg, _ := rejectedCode(err)
		structure.Status = CheckBlocked
		structure.Detail = rawText("读部门树时%s说：%s。", "Reading the department tree, %s said: %s.", title.In(i18n.ZhCN), msg)
		structure.Detail[i18n.EnUS] = fmt.Sprintf("Reading the department tree, %s said: %s.", title.In(i18n.EnUS), msg)
		structure.Fix = i18n.T("检查应用是否有读取通讯录的权限。", "Check that the app may read the contacts.")
		return finishChecks(checks), nil
	}
	structure.Status = CheckOK
	structure.Detail = rawText("读到了 %d 个部门。", "Read %d departments.", len(ds))
	return finishChecks(checks), nil
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
