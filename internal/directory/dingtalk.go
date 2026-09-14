package directory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

// 钉钉（企业内部应用）：组织架构同步（ADR 0017）、工作通知（ADR 0019）、外部日历（ADR 0032）都用同一个应用的
// AppKey / AppSecret；发工作通知另要应用的 AgentId。
//
// 老接口在 oapi.dingtalk.com：令牌放查询串，每个响应带 errcode / errmsg，errcode 非 0 一律当作提供方拒绝并把原话带回。
// 日历走新接口 api.dingtalk.com：令牌放请求头，出错时 HTTP 4xx + {code, message}。
func init() {
	Register(Provider{
		Key:              "dingtalk",
		Title:            i18n.T("钉钉", "DingTalk"),
		RootDepartmentID: dingtalkRootDepartmentID,
		Fields: []CredentialField{
			{Key: "app_key", Title: i18n.T("AppKey", "AppKey"), Placeholder: "dingxxxxxxxxxxxxxxxx",
				Hint: i18n.T("钉钉开放平台 → 应用开发 → 企业内部应用 → 该应用 → 凭证与基础信息", "DingTalk Open Platform → App development → Internal apps → the app → Credentials & basic info")},
			{Key: "app_secret", Title: i18n.T("AppSecret", "AppSecret"), Secret: true,
				Hint: i18n.T("与 AppKey 同一页；保存后只显示是否已设置", "Same page as the AppKey; only whether it is set is shown after saving")},
		},
		Tip: GuideStep{Text: i18n.T("AppKey 和 AppSecret 在钉钉开放平台里你创建的企业内部应用的「凭证与基础信息」页；应用要开通通讯录只读权限，并把本系统的出网 IP 加进服务器出口 IP。", "The AppKey and AppSecret are on the “Credentials & basic info” page of your internal app on DingTalk Open Platform; the app needs read-only contact permissions and this system's outbound IP in its server IP allowlist."), URL: dingtalkConsoleApps},
		Prerequisites: []i18n.Text{
			i18n.T("在钉钉开放平台创建一个企业内部应用", "Create an internal enterprise app on DingTalk Open Platform"),
			i18n.T("为它开通通讯录只读权限（通讯录部门信息读权限、通讯录部门成员读权限）；要同步手机号与邮箱再开通「个人手机号信息」「邮箱等个人信息」", "Grant it read-only contact permissions (department info, department members); to sync mobiles and emails also grant the personal mobile and email permissions"),
			i18n.T("把要同步的部门加进应用的通讯录权限范围（或选全部员工）", "Add the departments to sync to the app's contact scope (or choose all employees)"),
			i18n.T("在应用的「开发管理」里把本系统的出网 IP 加进服务器出口 IP", "Under the app's “Development management” add this system's outbound IP to the server IP allowlist"),
		},
		New:        func(creds map[string]string, opts Options) (Directory, error) { return NewDingTalk(creds, opts) },
		ConsoleURL: func(creds map[string]string) string { return dingtalkConsoleApps },
		// 发工作通知（ADR 0019）：同一个应用，多填一个 AgentId。
		MessagingFields: []CredentialField{
			{Key: "agent_id", Title: i18n.T("应用 AgentId", "App AgentId"), Placeholder: "1234567890",
				Hint: i18n.T("与 AppKey 同一页的 AgentId", "The AgentId on the same page as the AppKey")},
		},
		MessagingPrerequisites: []i18n.Text{
			i18n.T("把要接收提醒的人加进应用的可见范围", "Add the people who should receive reminders to the app's visible range"),
		},
		NewMessenger: func(creds map[string]string, opts Options) (Messenger, error) {
			return NewDingTalkMessenger(creds, opts)
		},
		// 外部日历（ADR 0032）：同一个应用，要开通日历只读权限；按成员的钉钉身份读它的主日历。
		CalendarPrerequisites: []i18n.Text{
			i18n.T("为应用开通日历只读权限（读取日程），并把成员加进应用的可见范围", "Grant the app read-only calendar permission and add members to its visible range"),
			i18n.T("成员要先通过 IM 集成同步进来，日程按他的钉钉身份拉取", "Members must be synced through the IM integration first; events are pulled by their DingTalk identity"),
		},
		NewCalendar: func(creds map[string]string, opts Options) (Calendar, error) { return NewDingTalk(creds, opts) },
	})
}

// dingtalkRootDepartmentID 是钉钉根部门的编号。
const dingtalkRootDepartmentID = "1"

// 钉钉开放平台的应用列表页；每个应用的权限管理、开发管理、可见范围都从这里点进去。
const dingtalkConsoleApps = "https://open-dev.dingtalk.com/fe/app#/corp/app"

// DingTalk 是钉钉客户端。用到的接口（均已对照 open.dingtalk.com 文档核对字段名）：
//
//	GET  /gettoken?appkey=&appsecret=                       → {errcode, errmsg, access_token, expires_in}
//	POST /topapi/v2/department/get      {dept_id, language} → result{dept_id, name, parent_id}
//	POST /topapi/v2/department/listsub  {dept_id, language} → result[]{dept_id, name, parent_id}（只有直属子部门，树要自己递归）
//	POST /topapi/v2/user/list           {dept_id, cursor, size} → result{has_more, next_cursor, list[]{userid, unionid, name, mobile, email, org_email, active, dept_id_list[]}}
//	POST /topapi/v2/user/get            {userid}            → result{userid, unionid, ...}（日历接口要 unionid）
//	GET  /auth/scopes                                        → {auth_org_scopes{authed_dept[], authed_user[]}, auth_user_field[]}
//	POST /topapi/message/corpconversation/asyncsend_v2 {agent_id, userid_list, msg} → {task_id}
//	POST /microapp/visible_scopes       {agentId}           → result{isHidden, deptVisibleScopes, userVisibleScopes}
//	GET  api.dingtalk.com/v1.0/calendar/users/{unionId}/calendars/primary/events ?timeMin&timeMax&maxResults&nextToken → {events[], nextToken}
//
// 人员：离职的人不在名单里；未激活（active=false）的人仍算在职。没有敏感字段权限时钉钉不返回手机号与邮箱、
// 或者姓名为空；姓名为空时先用 userid 顶着，同步结果会说明（ADR 0017 补记）。
type DingTalk struct {
	BaseURL    string // 老接口，默认 https://oapi.dingtalk.com
	APIBaseURL string // 新接口，默认 https://api.dingtalk.com
	appKey     string
	appSecret  string
	agentID    string // 发工作通知用的应用 AgentId（只在消息客户端上有）
	http       *http.Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

// NewDingTalk 创建客户端；creds 需要 app_key 与 app_secret。
func NewDingTalk(creds map[string]string, opts Options) (*DingTalk, error) {
	hc, err := newHTTPClient(opts)
	if err != nil {
		return nil, err
	}
	return &DingTalk{BaseURL: "https://oapi.dingtalk.com", APIBaseURL: "https://api.dingtalk.com",
		appKey: strings.TrimSpace(creds["app_key"]), appSecret: strings.TrimSpace(creds["app_secret"]), http: hc}, nil
}

// NewDingTalkMessenger 创建发消息客户端：同一组凭据，多一个 agent_id。
func NewDingTalkMessenger(creds map[string]string, opts Options) (*DingTalk, error) {
	d, err := NewDingTalk(creds, opts)
	if err != nil {
		return nil, err
	}
	d.agentID = strings.TrimSpace(creds["agent_id"])
	return d, nil
}

type dingtalkEnvelope struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// 令牌失效类错误码：40014 不合法的 access_token，42001 access_token 已过期；遇到就重取一次令牌再试。
func dingtalkTokenExpired(code int) bool { return code == 40014 || code == 42001 }

// dingtalkIPNotTrusted 是"服务器 IP 不在出口 IP 白名单里"的错误码；errmsg 里带着我们的出网 IP。
const dingtalkIPNotTrusted = 60020

var dingtalkFromIP = regexp.MustCompile(`([0-9]{1,3}(?:\.[0-9]{1,3}){3})`)

// get 发一次 GET；auth 为真时带 access_token。
func (d *DingTalk) get(ctx context.Context, path string, q url.Values, out any, auth bool) error {
	return d.call(ctx, http.MethodGet, path, q, nil, out, auth)
}

// post 发一次带令牌的 JSON POST。
func (d *DingTalk) post(ctx context.Context, path string, body any, out any) error {
	return d.call(ctx, http.MethodPost, path, nil, body, out, true)
}

func (d *DingTalk) call(ctx context.Context, method, path string, q url.Values, body any, out any, auth bool) error {
	for attempt := 0; attempt < 2; attempt++ {
		q2 := url.Values{}
		for k, v := range q {
			q2[k] = v
		}
		if auth {
			tok, err := d.Token(ctx)
			if err != nil {
				return err
			}
			q2.Set("access_token", tok)
		}
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, err := http.NewRequestWithContext(ctx, method, d.BaseURL+path+"?"+q2.Encode(), rd)
		if err != nil {
			return err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
		}
		resp, err := d.http.Do(req)
		if err != nil {
			return &UnreachableError{Err: err}
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if err != nil {
			return &UnreachableError{Err: err}
		}
		var env dingtalkEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return &RejectedError{Code: resp.StatusCode, Msg: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, trim(string(raw), 200))}
		}
		if env.ErrCode != 0 {
			if auth && attempt == 0 && dingtalkTokenExpired(env.ErrCode) {
				d.mu.Lock()
				d.token = ""
				d.mu.Unlock()
				continue
			}
			return &RejectedError{Code: env.ErrCode, Msg: fmt.Sprintf("%s (errcode %d)", env.ErrMsg, env.ErrCode)}
		}
		if resp.StatusCode >= 400 {
			return &RejectedError{Code: resp.StatusCode, Msg: fmt.Sprintf("HTTP %d", resp.StatusCode)}
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(raw, out)
	}
	return &RejectedError{Code: 42001, Msg: "access_token expired twice"}
}

// callNew 调新接口（api.dingtalk.com）：令牌放请求头；401 时重取一次令牌再试；其他 4xx / 5xx 当作拒绝并带 {code, message} 原话。
func (d *DingTalk) callNew(ctx context.Context, method, path string, q url.Values, out any) error {
	for attempt := 0; attempt < 2; attempt++ {
		tok, err := d.Token(ctx)
		if err != nil {
			return err
		}
		u := d.APIBaseURL + path
		if len(q) > 0 {
			u += "?" + q.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, method, u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("x-acs-dingtalk-access-token", tok)
		resp, err := d.http.Do(req)
		if err != nil {
			return &UnreachableError{Err: err}
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if err != nil {
			return &UnreachableError{Err: err}
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			d.mu.Lock()
			d.token = ""
			d.mu.Unlock()
			continue
		}
		if resp.StatusCode >= 400 {
			var e struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			_ = json.Unmarshal(raw, &e)
			msg := e.Message
			if msg == "" {
				msg = trim(string(raw), 200)
			}
			if e.Code != "" {
				msg = fmt.Sprintf("%s (%s)", msg, e.Code)
			}
			return &RejectedError{Code: resp.StatusCode, Msg: msg}
		}
		return json.Unmarshal(raw, out)
	}
	return &RejectedError{Code: http.StatusUnauthorized, Msg: "access token rejected twice"}
}

// Token 取 access_token，未过期时复用（有效期 expires_in，提前 60 秒作废）。
func (d *DingTalk) Token(ctx context.Context) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.token != "" && time.Now().Before(d.tokenExp) {
		return d.token, nil
	}
	var out struct {
		dingtalkEnvelope
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := d.get(ctx, "/gettoken", url.Values{"appkey": {d.appKey}, "appsecret": {d.appSecret}}, &out, false); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", &RejectedError{Code: -1, Msg: "empty access_token"}
	}
	d.token = out.AccessToken
	ttl := time.Duration(out.ExpiresIn) * time.Second
	if ttl <= 2*time.Minute {
		ttl = 2 * time.Hour
	}
	d.tokenExp = time.Now().Add(ttl - time.Minute)
	return d.token, nil
}

// ---------- 部门 ----------

// 钉钉的部门编号是整数；统一模型里用十进制字符串。
type dingtalkDept struct {
	DeptID   json.Number `json:"dept_id"`
	Name     string      `json:"name"`
	ParentID json.Number `json:"parent_id"`
}

func (x dingtalkDept) toDept() Dept {
	return Dept{ID: x.DeptID.String(), Name: x.Name, ParentID: x.ParentID.String()}
}

// dingtalkDeptID 把统一模型里的部门编号转成钉钉要的整数；空表示根部门。
func dingtalkDeptID(id string) (int64, error) {
	if strings.TrimSpace(id) == "" {
		id = dingtalkRootDepartmentID
	}
	n, err := strconv.ParseInt(strings.TrimSpace(id), 10, 64)
	if err != nil {
		return 0, &RejectedError{Code: 60003, Msg: fmt.Sprintf("department id %q is not a number (errcode 60003)", id)}
	}
	return n, nil
}

// getDept 调 department/get 读一个部门。
func (d *DingTalk) getDept(ctx context.Context, id string) (dingtalkDept, error) {
	n, err := dingtalkDeptID(id)
	if err != nil {
		return dingtalkDept{}, err
	}
	var out struct {
		dingtalkEnvelope
		Result dingtalkDept `json:"result"`
	}
	if err := d.post(ctx, "/topapi/v2/department/get", map[string]any{"dept_id": n, "language": "zh_CN"}, &out); err != nil {
		return dingtalkDept{}, err
	}
	return out.Result, nil
}

// listSub 调 department/listsub 读直属子部门。
func (d *DingTalk) listSub(ctx context.Context, id string) ([]dingtalkDept, error) {
	n, err := dingtalkDeptID(id)
	if err != nil {
		return nil, err
	}
	var out struct {
		dingtalkEnvelope
		Result []dingtalkDept `json:"result"`
	}
	if err := d.post(ctx, "/topapi/v2/department/listsub", map[string]any{"dept_id": n, "language": "zh_CN"}, &out); err != nil {
		return nil, err
	}
	return out.Result, nil
}

// Department 读一个部门。
func (d *DingTalk) Department(ctx context.Context, id string) (*Dept, error) {
	x, err := d.getDept(ctx, id)
	if err != nil {
		return nil, err
	}
	dept := x.toDept()
	if dept.ID == "" {
		dept.ID = strings.TrimSpace(id)
	}
	return &dept, nil
}

// dingtalkMaxDepts 是一次同步最多读的部门数，防止环或异常大的树把同步拖死。
const dingtalkMaxDepts = 5000

// Departments 读根部门下的全部子部门（不含根自身）：接口只给直属子部门，逐层往下读。
func (d *DingTalk) Departments(ctx context.Context, rootID string) ([]Dept, error) {
	if strings.TrimSpace(rootID) == "" {
		rootID = dingtalkRootDepartmentID
	}
	out := []Dept{}
	seen := map[string]bool{rootID: true}
	queue := []string{rootID}
	for len(queue) > 0 && len(out) < dingtalkMaxDepts {
		cur := queue[0]
		queue = queue[1:]
		subs, err := d.listSub(ctx, cur)
		if err != nil {
			return nil, err
		}
		for _, s := range subs {
			dept := s.toDept()
			if dept.ID == "" || seen[dept.ID] {
				continue
			}
			if dept.ParentID == "" || dept.ParentID == "0" {
				dept.ParentID = cur
			}
			seen[dept.ID] = true
			out = append(out, dept)
			queue = append(queue, dept.ID)
		}
	}
	return out, nil
}

// ---------- 人员 ----------

type dingtalkUser struct {
	UserID     string        `json:"userid"`
	UnionID    string        `json:"unionid"`
	Name       string        `json:"name"`
	Mobile     string        `json:"mobile"`
	Email      string        `json:"email"`
	OrgEmail   string        `json:"org_email"`
	Active     bool          `json:"active"`
	DeptIDList []json.Number `json:"dept_id_list"`
}

// listUsers 调 user/list 拿某部门直属人员的原始字段（按 cursor 翻页，每页 100）。
func (d *DingTalk) listUsers(ctx context.Context, deptID string) ([]dingtalkUser, error) {
	n, err := dingtalkDeptID(deptID)
	if err != nil {
		return nil, err
	}
	var all []dingtalkUser
	cursor := int64(0)
	for page := 0; page < 1000; page++ {
		var out struct {
			dingtalkEnvelope
			Result struct {
				HasMore    bool           `json:"has_more"`
				NextCursor int64          `json:"next_cursor"`
				List       []dingtalkUser `json:"list"`
			} `json:"result"`
		}
		if err := d.post(ctx, "/topapi/v2/user/list", map[string]any{"dept_id": n, "cursor": cursor, "size": 100, "language": "zh_CN"}, &out); err != nil {
			return nil, err
		}
		all = append(all, out.Result.List...)
		if !out.Result.HasMore || out.Result.NextCursor <= cursor {
			break
		}
		cursor = out.Result.NextCursor
	}
	return all, nil
}

// Users 读某部门的直属人员。
func (d *DingTalk) Users(ctx context.Context, deptID string) ([]User, error) {
	list, err := d.listUsers(ctx, deptID)
	if err != nil {
		return nil, err
	}
	res := make([]User, 0, len(list))
	for _, u := range list {
		if u.UserID == "" {
			continue
		}
		email := u.OrgEmail
		if email == "" {
			email = u.Email
		}
		name := strings.TrimSpace(u.Name)
		if name == "" {
			name = u.UserID
		}
		deps := make([]string, 0, len(u.DeptIDList))
		for _, x := range u.DeptIDList {
			deps = append(deps, x.String())
		}
		// 离职的人不在名单里，名单里的人都在职（未激活只是还没登录过钉钉）。
		res = append(res, User{ID: u.UserID, Name: name, Email: strings.ToLower(strings.TrimSpace(email)), Mobile: strings.TrimSpace(u.Mobile),
			DeptIDs: deps, Active: true})
	}
	return res, nil
}

// unionID 用 userid 换 unionid（日历接口只认 unionid）。
func (d *DingTalk) unionID(ctx context.Context, userID string) (string, error) {
	var out struct {
		dingtalkEnvelope
		Result dingtalkUser `json:"result"`
	}
	if err := d.post(ctx, "/topapi/v2/user/get", map[string]any{"userid": userID, "language": "zh_CN"}, &out); err != nil {
		return "", err
	}
	if out.Result.UnionID == "" {
		return "", &RejectedError{Code: 60121, Msg: fmt.Sprintf("user %s has no unionid (errcode 60121)", userID)}
	}
	return out.Result.UnionID, nil
}

// AuthorizedDepartments 列出应用通讯录权限范围里的部门编号（选了全部员工时含根部门 1）。
func (d *DingTalk) AuthorizedDepartments(ctx context.Context) ([]string, error) {
	var out struct {
		dingtalkEnvelope
		AuthOrgScopes struct {
			AuthedDept []json.Number `json:"authed_dept"`
			AuthedUser []string      `json:"authed_user"`
		} `json:"auth_org_scopes"`
	}
	if err := d.get(ctx, "/auth/scopes", nil, &out, true); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.AuthOrgScopes.AuthedDept))
	for _, x := range out.AuthOrgScopes.AuthedDept {
		if s := x.String(); s != "" {
			ids = append(ids, s)
		}
	}
	return ids, nil
}

// ---------- 接入检查（ADR 0017 补记二） ----------

// Diagnose 按顺序跑钉钉的接入检查：凭据可用 → 服务器出口 IP → 通讯录权限范围 → 能读到部门树 → 姓名 → 手机号与邮箱。
// gettoken 本身也可能因 IP 不在白名单报 60020，这时凭据算通过、出口 IP 阻塞。
func (d *DingTalk) Diagnose(ctx context.Context, rootID string) ([]Check, error) {
	if strings.TrimSpace(rootID) == "" {
		rootID = dingtalkRootDepartmentID
	}
	credentials := &Check{Key: "credentials", Title: T("凭据可用", "Credentials work")}
	trustedIP := &Check{Key: "trusted_ip", Title: T("服务器 IP 在出口 IP 白名单里", "Server IP is allowlisted")}
	scope := &Check{Key: "scope", Title: T("通讯录权限范围", "Contact scope")}
	structure := &Check{Key: "structure", Title: T("能读到部门树", "Department tree readable")}
	names := &Check{Key: "names", Title: T("人员姓名权限", "People's names")}
	contacts := &Check{Key: "contacts", Title: T("手机号与邮箱权限", "Mobiles and emails")}
	checks := []*Check{credentials, trustedIP, scope, structure, names, contacts}

	ipBlocked := func(msg string) {
		ip := "?"
		if m := dingtalkFromIP.FindStringSubmatch(msg); m != nil {
			ip = m[1]
		}
		trustedIP.Status = CheckBlocked
		trustedIP.Detail = rawText("钉钉说：%s。服务器的出网 IP 是 %s。", "DingTalk said: %s. The server's outbound IP is %s.", msg, ip)
		trustedIP.Fix = rawText("在该应用的「开发管理 → 服务器出口 IP」里加入 %s。", "Under the app's “Development management → Server IP allowlist”, add %s.", ip)
		trustedIP.FixURL = dingtalkConsoleApps
	}

	// 1. 凭据
	if _, err := d.Token(ctx); err != nil {
		var un *UnreachableError
		if errors.As(err, &un) {
			return nil, err
		}
		code, msg, _ := rejectedCode(err)
		if code == dingtalkIPNotTrusted {
			credentials.Status = CheckOK
			credentials.Detail = T("钉钉认出了这组凭据。", "DingTalk recognised these credentials.")
			ipBlocked(msg)
			return finishChecks(checks), nil
		}
		credentials.Status = CheckBlocked
		credentials.Detail = rawText("钉钉拒绝了这组凭据：%s。", "DingTalk rejected these credentials: %s.", msg)
		credentials.Fix = T("到该应用的「凭证与基础信息」页复制 AppKey 与 AppSecret，重新填写。", "Copy the AppKey and AppSecret from the app's “Credentials & basic info” page and enter them again.")
		credentials.FixURL = dingtalkConsoleApps
		return finishChecks(checks), nil
	}
	credentials.Status = CheckOK
	credentials.Detail = T("已取到访问令牌。", "Got an access token.")

	// 2/3/4. 出口 IP、权限范围、部门树：先问范围，再读同步根部门的子部门
	ids, scopeErr := d.AuthorizedDepartments(ctx)
	if scopeErr != nil {
		var un *UnreachableError
		if errors.As(scopeErr, &un) {
			return nil, scopeErr
		}
		if code, msg, _ := rejectedCode(scopeErr); code == dingtalkIPNotTrusted {
			ipBlocked(msg)
			return finishChecks(checks), nil
		}
	}
	subs, err := d.listSub(ctx, rootID)
	if err != nil {
		var un *UnreachableError
		if errors.As(err, &un) {
			return nil, err
		}
		code, msg, _ := rejectedCode(err)
		if code == dingtalkIPNotTrusted {
			ipBlocked(msg)
			return finishChecks(checks), nil
		}
		trustedIP.Status = CheckOK
		trustedIP.Detail = T("服务器 IP 在白名单里。", "The server IP is allowlisted.")
		structure.Status = CheckBlocked
		structure.Detail = rawText("读部门树时钉钉说：%s。", "Reading the department tree, DingTalk said: %s.", msg)
		structure.Fix = T("确认应用已开通「通讯录部门信息读权限」，并且同步根部门在权限范围里。", "Make sure the app has the department read permission and the sync root is inside the contact scope.")
		structure.FixURL = dingtalkConsoleApps
		// 范围：能列出被授权的部门就让人在下一步从里面选
		roots := d.scopeRoots(ctx, ids)
		switch {
		case len(roots) > 0:
			scope.Status = CheckTodo
			scope.Roots = roots
			scope.Detail = rawText("权限范围只包含 %d 个部门：%s。", "The scope covers only %d departments: %s.", len(roots), listDeptNames(roots))
			scope.Fix = T("在该应用的「权限管理 → 通讯录权限范围」改成全部员工，或者在下一步从这些部门里选同步范围。", "Under the app's “Permissions → Contact scope” switch to all employees, or pick the sync roots from these departments in the next step.")
			scope.FixURL = dingtalkConsoleApps
		case scopeErr == nil:
			scope.Status = CheckBlocked
			scope.Detail = T("权限范围里没有任何部门。", "The scope contains no department.")
			scope.Fix = T("在该应用的「权限管理 → 通讯录权限范围」里加入要同步的部门，或者改成全部员工。", "Under the app's “Permissions → Contact scope” add the departments to sync, or switch to all employees.")
			scope.FixURL = dingtalkConsoleApps
		default:
			_, smsg, _ := rejectedCode(scopeErr)
			scope.Status = CheckSkipped
			scope.Detail = rawText("读权限范围时钉钉说：%s。", "Reading the contact scope, DingTalk said: %s.", smsg)
		}
		return finishChecks(checks), nil
	}
	trustedIP.Status = CheckOK
	trustedIP.Detail = T("服务器 IP 在白名单里。", "The server IP is allowlisted.")
	switch {
	case rootID == dingtalkRootDepartmentID:
		scope.Status = CheckOK
		scope.Detail = T("权限范围是全部员工。", "The scope covers all employees.")
	case scopeErr == nil && len(ids) > 0 && !containsStr(ids, rootID) && !containsStr(ids, dingtalkRootDepartmentID):
		roots := d.scopeRoots(ctx, ids)
		scope.Status = CheckTodo
		scope.Roots = roots
		scope.Detail = rawText("同步根部门 %s 不在权限范围里；范围只包含 %d 个部门：%s。", "Sync root %s is outside the scope, which covers only %d departments: %s.", rootID, len(roots), listDeptNames(roots))
		scope.Fix = T("在该应用的「权限管理 → 通讯录权限范围」加入它，或者在下一步从这些部门里重选。", "Under the app's “Permissions → Contact scope” add it, or pick again from these departments in the next step.")
		scope.FixURL = dingtalkConsoleApps
	default:
		scope.Status = CheckOK
		scope.Detail = T("同步范围已选定，且在权限范围里。", "The sync roots are chosen and inside the scope.")
	}
	structure.Status = CheckOK
	example := ""
	for _, s := range subs {
		if strings.TrimSpace(s.Name) != "" {
			example = s.Name
			break
		}
	}
	if example == "" {
		if x, err := d.getDept(ctx, rootID); err == nil {
			example = x.Name
		}
	}
	structure.Detail = rawText("读到了 %d 个直属子部门（如「%s」）。", "Read %d direct sub-departments (e.g. “%s”).", len(subs), example)

	// 5/6. 姓名、手机号与邮箱：从能读的部门里找一个有直属人员的
	candidates := []string{rootID}
	for _, s := range subs {
		if id := s.DeptID.String(); id != "" && len(candidates) < 4 {
			candidates = append(candidates, id)
		}
	}
	var users []dingtalkUser
	var userErr error
	for _, id := range candidates {
		items, err := d.listUsers(ctx, id)
		if err != nil {
			userErr = err
			continue
		}
		if len(items) > 0 {
			users, userErr = items, nil
			break
		}
	}
	switch {
	case len(users) > 0:
		nameless, contactless := 0, 0
		for _, u := range users {
			if strings.TrimSpace(u.Name) == "" {
				nameless++
			}
			if u.Mobile == "" && u.Email == "" && u.OrgEmail == "" {
				contactless++
			}
		}
		if nameless == len(users) {
			names.Status = CheckBlocked
			names.Detail = rawText("读到了 %d 个人，但都没有姓名：应用没有「通讯录部门成员读权限」。", "Read %d people, but none has a name: the app lacks the department member read permission.", len(users))
			names.Fix = T("在该应用的「权限管理」开通「通讯录部门成员读权限」。", "Under the app's “Permissions” grant the department member read permission.")
			names.FixURL = dingtalkConsoleApps
		} else {
			names.Status = CheckOK
			names.Detail = T("人员姓名能读到。", "People's names are readable.")
		}
		if contactless == len(users) {
			contacts.Status = CheckTodo
			contacts.Detail = rawText("读到了 %d 个人，都没有手机号与邮箱：应用没有「个人手机号信息」「邮箱等个人信息」权限。同步进来的人要靠邀请链接激活。", "Read %d people, none with a mobile or email: the app lacks the personal mobile and email permissions. Synced people will have to activate through invitation links.", len(users))
			contacts.Fix = T("在该应用的「权限管理」开通「个人手机号信息」与「邮箱等个人信息」；也可以先不开，同步后手工发邀请链接。", "Under the app's “Permissions” grant the personal mobile and email permissions; or leave it and hand out invitation links manually after syncing.")
			contacts.FixURL = dingtalkConsoleApps
		} else {
			contacts.Status = CheckOK
			contacts.Detail = T("手机号或邮箱能读到，同步时会自动生成邀请。", "Mobiles or emails are readable; invitations are generated on sync.")
		}
	case userErr != nil:
		_, msg, _ := rejectedCode(userErr)
		names.Status, contacts.Status = CheckSkipped, CheckSkipped
		names.Detail = rawText("读人员时钉钉说：%s。", "Reading people, DingTalk said: %s.", msg)
		contacts.Detail = names.Detail
	default:
		names.Status, contacts.Status = CheckSkipped, CheckSkipped
		names.Detail = T("这些部门里没有直属人员，无法判断。", "These departments have no direct members, so this cannot be checked.")
		contacts.Detail = names.Detail
	}
	return finishChecks(checks), nil
}

// scopeRoots 把权限范围里的部门编号变成带名字的部门（最多查 20 个名字），根部门不算。
func (d *DingTalk) scopeRoots(ctx context.Context, ids []string) []Dept {
	var roots []Dept
	for _, id := range ids {
		if id == "" || id == dingtalkRootDepartmentID {
			continue
		}
		dept := Dept{ID: id, Name: id, ParentID: dingtalkRootDepartmentID}
		if len(roots) < 20 {
			if x, err := d.getDept(ctx, id); err == nil && strings.TrimSpace(x.Name) != "" {
				dept.Name = x.Name
				if p := x.ParentID.String(); p != "" {
					dept.ParentID = p
				}
			}
		}
		roots = append(roots, dept)
	}
	return roots
}

// listDeptNames 把前几个部门名拼成「a」、「b」…。
func listDeptNames(roots []Dept) string {
	names := make([]string, 0, 5)
	for _, r := range roots[:min(5, len(roots))] {
		names = append(names, "「"+r.Name+"」")
	}
	listed := strings.Join(names, "、")
	if len(roots) > 5 {
		listed += "…"
	}
	return listed
}

// ---------- 发工作通知（ADR 0019） ----------

// SendDirect 给一个人发工作通知：asyncsend_v2，有链接时用 action_card（标题、正文、一个按钮），没有链接时用 text。
// userid_list 是 userid。
func (d *DingTalk) SendDirect(ctx context.Context, userID string, msg Message) error {
	agent, err := strconv.ParseInt(d.agentID, 10, 64)
	if err != nil || agent <= 0 {
		return &RejectedError{Code: -1, Msg: "missing or invalid agent_id"}
	}
	var payload map[string]any
	if msg.URL != "" {
		open := msg.Open
		if open == "" {
			open = "打开"
		}
		text := msg.Text
		if text == "" {
			text = msg.Title
		}
		payload = map[string]any{"msgtype": "action_card", "action_card": map[string]string{
			"title": msg.Title, "markdown": text, "single_title": open, "single_url": msg.URL}}
	} else {
		text := msg.Title
		if msg.Text != "" && msg.Text != msg.Title {
			text += "\n" + msg.Text
		}
		payload = map[string]any{"msgtype": "text", "text": map[string]string{"content": text}}
	}
	body := map[string]any{"agent_id": agent, "userid_list": userID, "msg": payload}
	var out struct {
		dingtalkEnvelope
		TaskID json.Number `json:"task_id"`
	}
	return d.post(ctx, "/topapi/message/corpconversation/asyncsend_v2", body, &out)
}

// DiagnoseMessaging 检查「能以应用身份发消息」：用 AgentId 读应用的可见范围（microapp/visible_scopes），
// 能读到说明 AgentId 属于这个应用且出口 IP 通过；不真的发消息。
func (d *DingTalk) DiagnoseMessaging(ctx context.Context) Check {
	c := Check{Key: MessagingCheckKey, Title: T("能以应用身份发消息", "Can send messages as the app")}
	agent, err := strconv.ParseInt(d.agentID, 10, 64)
	if err != nil || agent <= 0 {
		c.Status = CheckTodo
		c.Detail = T("还没填应用的 AgentId；没有它发不了工作通知。", "The app's AgentId is not set; work notifications cannot be sent without it.")
		c.Fix = T("在「组织设置 → 通知」的钉钉通道里填应用「凭证与基础信息」页上的 AgentId。", "Under “Organization settings → Notifications”, fill in the AgentId from the app's “Credentials & basic info” page for the DingTalk channel.")
		c.FixURL = dingtalkConsoleApps
		return c
	}
	var out struct {
		dingtalkEnvelope
		Result struct {
			IsHidden          bool     `json:"isHidden"`
			DeptVisibleScopes []int64  `json:"deptVisibleScopes"`
			UserVisibleScopes []string `json:"userVisibleScopes"`
		} `json:"result"`
	}
	err = d.post(ctx, "/microapp/visible_scopes", map[string]any{"agentId": agent}, &out)
	if err == nil {
		c.Status = CheckOK
		if len(out.Result.DeptVisibleScopes) == 0 && len(out.Result.UserVisibleScopes) == 0 {
			c.Detail = T("应用可用；可见范围是全部员工。", "The app works; it is visible to all employees.")
		} else {
			c.Detail = rawText("应用可用；可见范围含 %d 个部门与 %d 个人，不在范围里的人收不到提醒。", "The app works; it is visible to %d departments and %d people, others will not receive reminders.", len(out.Result.DeptVisibleScopes), len(out.Result.UserVisibleScopes))
		}
		return c
	}
	var un *UnreachableError
	if errors.As(err, &un) {
		c.Status = CheckTodo
		c.Detail = rawText("连不上钉钉：%s。", "Could not reach DingTalk: %s.", un.Err.Error())
		c.Fix = T("检查服务器的网络或出网代理设置，然后重新检查。", "Check the server's network or outbound proxy setting, then re-check.")
		return c
	}
	code, msg, _ := rejectedCode(err)
	c.Status = CheckTodo
	if code == dingtalkIPNotTrusted {
		ip := "?"
		if m := dingtalkFromIP.FindStringSubmatch(msg); m != nil {
			ip = m[1]
		}
		c.Detail = rawText("钉钉说：%s。服务器的出网 IP 是 %s。", "DingTalk said: %s. The server's outbound IP is %s.", msg, ip)
		c.Fix = rawText("在该应用的「开发管理 → 服务器出口 IP」里加入 %s。", "Under the app's “Development management → Server IP allowlist”, add %s.", ip)
	} else {
		c.Detail = rawText("用 AgentId 读应用信息时钉钉说：%s。没有它时待确认操作与验收提醒发不到钉钉，只在站内。", "Reading the app by its AgentId, DingTalk said: %s. Without it, reminders stay in-app only.", msg)
		c.Fix = T("到该应用的「凭证与基础信息」页核对 AgentId，重新填写；不需要钉钉提醒也可以先跳过。", "Check the AgentId on the app's “Credentials & basic info” page and enter it again; skip it if DingTalk reminders are not needed.")
	}
	c.FixURL = dingtalkConsoleApps
	return c
}

// ---------- 外部日历（ADR 0032） ----------

type dingtalkEventTime struct {
	Date     string `json:"date"`     // 全天：2006-01-02
	DateTime string `json:"dateTime"` // 否则 RFC 3339
	TimeZone string `json:"timeZone"`
}

type dingtalkEvents struct {
	Events []struct {
		ID                string            `json:"id"`
		Summary           string            `json:"summary"`
		Status            string            `json:"status"` // confirmed | tentative | cancelled
		IsAllDay          bool              `json:"isAllDay"`
		Start             dingtalkEventTime `json:"start"`
		End               dingtalkEventTime `json:"end"`
		OnlineMeetingInfo struct {
			URL string `json:"url"`
		} `json:"onlineMeetingInfo"`
	} `json:"events"`
	NextToken string `json:"nextToken"`
}

// Events 读某个成员主日历里落在区间内的日程：先用 userid 换 unionid，再按 nextToken 翻页。
func (d *DingTalk) Events(ctx context.Context, externalUserID string, from, to time.Time) ([]CalendarEvent, error) {
	uid, err := d.unionID(ctx, externalUserID)
	if err != nil {
		return nil, err
	}
	path := "/v1.0/calendar/users/" + url.PathEscape(uid) + "/calendars/primary/events"
	var out []CalendarEvent
	next := ""
	for page := 0; page < 100; page++ {
		q := url.Values{"timeMin": {from.UTC().Format(time.RFC3339)}, "timeMax": {to.UTC().Format(time.RFC3339)}, "maxResults": {"100"}}
		if next != "" {
			q.Set("nextToken", next)
		}
		var res dingtalkEvents
		if err := d.callNew(ctx, http.MethodGet, path, q, &res); err != nil {
			return nil, err
		}
		for _, it := range res.Events {
			if it.ID == "" || it.Status == "cancelled" {
				continue
			}
			ev := CalendarEvent{ExternalID: it.ID, Title: it.Summary, Busy: true, URL: it.OnlineMeetingInfo.URL}
			if it.IsAllDay || it.Start.Date != "" {
				ev.AllDay = true
				ev.Start, _ = time.ParseInLocation("2006-01-02", it.Start.Date, time.Local)
				ev.End, _ = time.ParseInLocation("2006-01-02", it.End.Date, time.Local)
			} else {
				ev.Start, _ = time.Parse(time.RFC3339, it.Start.DateTime)
				ev.End, _ = time.Parse(time.RFC3339, it.End.DateTime)
			}
			if ev.End.IsZero() || !ev.End.After(from) || !ev.Start.Before(to) {
				continue
			}
			out = append(out, ev)
		}
		if res.NextToken == "" || res.NextToken == next {
			break
		}
		next = res.NextToken
	}
	return out, nil
}
