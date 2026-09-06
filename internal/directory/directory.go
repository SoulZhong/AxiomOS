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

// ---------- 发消息（ADR 0019） ----------

// Message 是一条要外发的通知：一句话正文 + 一个直达链接，不含成本与他人信息（ADR 0019 第 4 条）。
// Kind 是事件类型代码名（proposal、review…），Recipient 是收件人在本系统里的编号与姓名（webhook 载荷用）。
type Message struct {
	Kind      string
	Title     string
	Text      string
	URL       string
	Recipient Recipient
	At        time.Time
	// Open 是按钮 / 链接上的字，按收件人语言，如「打开」。
	Open string
}

// Recipient 是收件人。
type Recipient struct {
	ID   string
	Name string
}

// Messenger 是可选能力：以应用身份给一个人发私聊消息。externalUserID 是收件人在提供方那边的编号
// （飞书 open_id、企业微信 userid、邮件地址；webhook 不用它）。发不出去时返回 RejectedError / UnreachableError。
type Messenger interface {
	SendDirect(ctx context.Context, externalUserID string, msg Message) error
}

// MessagingDiagnoser 是可选能力：检查「能以应用身份发消息」，返回一项 key 为 messaging 的检查（todo，不阻塞同步）。
type MessagingDiagnoser interface {
	DiagnoseMessaging(ctx context.Context) Check
}

// MessagingCheckKey 是发消息检查项的键。
const MessagingCheckKey = "messaging"

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

// ---------- 代码平台（ADR 0020） ----------

// Repo 是代码平台上的一个仓库。
type Repo struct {
	ID       string // 提供方的仓库编号
	FullName string // owner/name
	URL      string
	Private  bool
}

// CodeEvent 是从一次 webhook 请求里解出来的一件事。Kind 为空表示"认识这个请求但它不对应六种外部事件"
// （比如 PR 改了标题），这时只更新外部链接、不触发迁移。
type CodeEvent struct {
	Provider   string
	Kind       string // pr_opened | pr_ready | pr_merged | pr_closed | ci_passed | ci_failed，可为空
	DeliveryID string // 提供方的投递编号，去重用
	Repo       string // owner/name
	Number     int    // PR / MR 序号
	Title      string
	Branch     string // 源分支名
	Body       string // PR 描述
	URL        string // PR 网页地址
	ExternalID string // 平台上的唯一编号，如 github:owner/name#12
	Status     string // open | draft | merged | closed | passed | failed
	ActorName  string // 外部操作者的登录名
	// LinkKind 是要挂到任务上的外部链接种类（PR 事件是 pr）；为空表示这次不动链接（如 CI 事件没带 PR）。
	LinkKind string
}

// Ref 是这件事在句子里的说法，如「PR #12」；没有序号时用仓库名。
func (e CodeEvent) Ref() string {
	if e.Number > 0 {
		return fmt.Sprintf("PR #%d", e.Number)
	}
	if e.Branch != "" {
		return e.Repo + " " + e.Branch
	}
	return e.Repo
}

// CodeHost 是可选能力：代码平台（GitHub、GitLab、Gitee）。
// VerifyWebhook 校验签名并把载荷翻译成统一的事件；不是这个平台的请求、签名不对时返回错误。
type CodeHost interface {
	VerifyWebhook(headers http.Header, body []byte, secret string) (CodeEvent, error)
	ListRepos(ctx context.Context) ([]Repo, error)
	EnsureWebhook(ctx context.Context, repo, callbackURL, secret string) error
}

// CodeDiagnoser 是可选能力：代码平台的接入检查（凭据可用 → 能读仓库 → webhook 已建）。
// repos 是组织选中的仓库全名，callbackURL 是本系统的回调地址。
type CodeDiagnoser interface {
	DiagnoseCode(ctx context.Context, repos []string, callbackURL string) ([]Check, error)
}

// ErrBadSignature 表示签名或密钥对不上。
var ErrBadSignature = errors.New("bad webhook signature")

// ErrNotForUs 表示这个请求不是我们要处理的事件（心跳、ping、无关事件类型）。
var ErrNotForUs = errors.New("webhook event not handled")

// ---------- 注册表 ----------

// CredentialField 是提供方声明的一个凭据字段；前端表单按它渲染，应用层按它校验。
type CredentialField struct {
	Key         string    // 存储与接口里的键，如 app_id
	Title       i18n.Text // 显示名，如「App ID」
	Secret      bool      // 保密字段：加密入库，界面只显示是否已设置
	Optional    bool      // 可以留空（如 GitHub 企业版才要填的接口地址）
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
// 一个提供方可以只有部分能力：New 非空表示能当 IM 集成（读组织结构），NewMessenger 非空表示能发消息（ADR 0019）；
// 邮件与 webhook 只有后者，飞书、企业微信两者都有。注册表对只实现部分能力的提供方一视同仁。
type Provider struct {
	Key              string    // 代码名，如 feishu、wecom；也是外部身份与成员来源里存的值
	Title            i18n.Text // 显示名（zh/en）
	RootDepartmentID string    // 提供方的根部门编号（飞书 "0"，企业微信 "1"）；只发消息的提供方为空
	Fields           []CredentialField
	Prerequisites    []i18n.Text // 接入前置条件，逐条给人看
	Tip              GuideStep   // 一句话提醒凭据在哪儿找，可带控制台链接
	New              func(creds map[string]string, opts Options) (Directory, error)
	// ConsoleURL 按凭据算出这个应用在提供方控制台里的首页（可选），前端的「打开控制台」用它。
	ConsoleURL func(creds map[string]string) string

	// MessagingFields 是发消息比读通讯录多要的凭据（企业微信要应用的 AgentId 与 Secret；飞书同一个应用，不用多填）。
	// 只发消息的提供方（邮件、webhook）把全部字段放这里，Fields 留空。
	MessagingFields []CredentialField
	// MessagingPrerequisites 是发消息的前置条件（如飞书要开通「以应用的身份发消息」权限）。
	MessagingPrerequisites []i18n.Text
	// NewMessenger 用全部凭据（Fields + MessagingFields 的值）建一个发消息客户端。
	NewMessenger func(creds map[string]string, opts Options) (Messenger, error)
	// MessagingConfigured 判断发消息的凭据是否齐全（可选）；不给时按 MessagingFields 每个字段都有值判断。
	// 邮件用它表达"组织没填就用服务端环境变量"。
	MessagingConfigured func(creds map[string]string, secretsSet map[string]bool) bool

	// NewCodeHost 用 Fields 的值建一个代码平台客户端（ADR 0020）。非空即表示这是个代码平台提供方；
	// 它不参与 IM 集成（Providers）与通知通道（MessagingProviders），只出现在 CodeHostProviders 里。
	NewCodeHost func(creds map[string]string, opts Options) (CodeHost, error)

	hidden bool // 测试注册的提供方：Lookup 能找到，Providers 不列出
}

// CanSync 判断能否作为 IM 集成读组织结构。
func (p Provider) CanSync() bool { return p.New != nil }

// CanMessage 判断能否发消息。
func (p Provider) CanMessage() bool { return p.NewMessenger != nil }

// CanHostCode 判断是不是代码平台（ADR 0020）。
func (p Provider) CanHostCode() bool { return p.NewCodeHost != nil }

// MessagingField 按键找发消息凭据字段。
func (p Provider) MessagingField(key string) (CredentialField, bool) {
	for _, f := range p.MessagingFields {
		if f.Key == key {
			return f, true
		}
	}
	return CredentialField{}, false
}

// MessagingReady 判断发消息的凭据是否齐全：creds 是非保密字段的值，secretsSet 是保密字段是否已设置。
func (p Provider) MessagingReady(creds map[string]string, secretsSet map[string]bool) bool {
	if p.MessagingConfigured != nil {
		return p.MessagingConfigured(creds, secretsSet)
	}
	for _, f := range p.MessagingFields {
		if f.Secret && !secretsSet[f.Key] || !f.Secret && strings.TrimSpace(creds[f.Key]) == "" {
			return false
		}
	}
	return true
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
	if p.Title.In(i18n.ZhCN) == "" {
		return fmt.Errorf("provider %s must declare Title", p.Key)
	}
	if p.New == nil && p.NewMessenger == nil && p.NewCodeHost == nil {
		return fmt.Errorf("provider %s must declare New (directory), NewMessenger (messaging) or NewCodeHost (code platform)", p.Key)
	}
	if p.New != nil && p.RootDepartmentID == "" {
		return fmt.Errorf("provider %s must declare RootDepartmentID", p.Key)
	}
	seen := map[string]bool{}
	for _, f := range append(append([]CredentialField{}, p.Fields...), p.MessagingFields...) {
		if f.Key == "" || f.Title.In(i18n.ZhCN) == "" || seen[f.Key] {
			return fmt.Errorf("provider %s has a bad or duplicate field %q", p.Key, f.Key)
		}
		seen[f.Key] = true
	}
	return nil
}

// Providers 列出全部能当 IM 集成的生产提供方，顺序即注册顺序（包内文件按文件名初始化：feishu、wecom…）。
// 只发消息的提供方（邮件、webhook）不在这里，见 MessagingProviders。
func Providers() []Provider {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Provider, 0, len(registry))
	for _, p := range registry {
		if !p.hidden && p.CanSync() {
			out = append(out, p)
		}
	}
	return out
}

// CodeHostProviders 列出全部代码平台提供方（ADR 0020），顺序即注册顺序（gitee、github、gitlab 按文件名）。
// 它们不出现在 Providers 与 MessagingProviders 里。
func CodeHostProviders() []Provider {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Provider, 0, len(registry))
	for _, p := range registry {
		if !p.hidden && p.CanHostCode() {
			out = append(out, p)
		}
	}
	return out
}

// MessagingProviders 列出全部能发消息的生产提供方：先是既能同步又能发的 IM 平台（飞书、企业微信），再是只发消息的（邮件、webhook），
// 各自按注册顺序。
func MessagingProviders() []Provider {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Provider, 0, len(registry))
	for _, p := range registry {
		if !p.hidden && p.CanMessage() && p.CanSync() {
			out = append(out, p)
		}
	}
	for _, p := range registry {
		if !p.hidden && p.CanMessage() && !p.CanSync() {
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
// 出网默认只许公网（egress.go）：拨号前按解析出的地址再查一遍，重定向也要过同一道守卫。
func newHTTPClient(opts Options) (*http.Client, error) {
	if opts.HTTPClient != nil {
		return opts.HTTPClient, nil
	}
	eg := DefaultEgress()
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment, TLSHandshakeTimeout: 10 * time.Second,
		DialContext: egressDialer(eg).DialContext}
	if p := strings.TrimSpace(opts.ProxyURL); p != "" {
		if err := CheckProxyURL(p, EgressOpts{AllowPrivate: eg.AllowPrivate, AllowHTTP: true}); err != nil {
			return nil, fmt.Errorf("%w: %q: %v", ErrBadProxy, p, err)
		}
		u, err := url.Parse(strings.TrimSpace(p))
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("%w: %q", ErrBadProxy, p)
		}
		tr.Proxy = http.ProxyURL(u)
	}
	return &http.Client{Transport: tr, Timeout: 30 * time.Second, CheckRedirect: egressRedirect(eg)}, nil
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
