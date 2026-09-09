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
	"strings"
	"sync"
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

// 企业微信作为提供方登记到注册表：凭据是企业 ID（CorpID）与「通讯录同步」的 Secret，根部门编号 "1"。
func init() {
	Register(Provider{
		Key:              "wecom",
		Title:            i18n.T("企业微信", "WeCom"),
		RootDepartmentID: wecomRootDepartmentID,
		Fields: []CredentialField{
			{Key: "corp_id", Title: i18n.T("企业 ID", "Corp ID"), Placeholder: "wwxxxxxxxxxxxxxxxx",
				Hint: i18n.T("企业微信管理后台 → 我的企业 → 企业信息 → 企业 ID", "WeCom admin console → My Company → Company Info → Corp ID")},
			{Key: "corp_secret", Title: i18n.T("通讯录同步 Secret", "Contacts sync Secret"), Secret: true,
				Hint: i18n.T("管理后台 → 管理工具 → 通讯录同步 → Secret；保存后只显示是否已设置", "Admin console → Management Tools → Contacts Sync → Secret; only whether it is set is shown after saving")},
		},
		Tip: GuideStep{Text: i18n.T("企业 ID 在企业微信管理后台「我的企业 → 企业信息」最底部；通讯录同步 Secret 在「管理工具 → 通讯录同步」开启 API 接口同步后显示，并要把服务器出网 IP 加入企业可信 IP。", "The Corp ID is at the bottom of “My Company → Company Info” in the WeCom admin console; the contacts-sync Secret appears under “Management Tools → Contacts Sync” after enabling API sync, and the server’s outbound IP must be added to the trusted IPs."), URL: "https://work.weixin.qq.com/wework_admin/frame"},
		Prerequisites: []i18n.Text{
			i18n.T("在企业微信管理后台的「管理工具 → 通讯录同步」里开启 API 接口同步，取得 Secret", "In the WeCom admin console open Management Tools → Contacts Sync, enable API sync and copy the Secret"),
			i18n.T("把本系统的出网 IP 加进通讯录同步的可信 IP 列表", "Add this system's outbound IP to the contacts-sync trusted IP list"),
			i18n.T("新建的应用可能拿不到姓名、手机、邮箱等敏感字段；拿不到时只同步能拿到的字段，并在同步结果里说明", "Newly created apps may not receive sensitive fields (name, mobile, email); then only the available fields are synced and the sync result says so"),
		},
		New:        func(creds map[string]string, opts Options) (Directory, error) { return NewWeCom(creds, opts) },
		ConsoleURL: func(creds map[string]string) string { return wecomConsoleHome },
		// 发消息（ADR 0019）：通讯录同步的 Secret 发不了消息，要用一个自建应用的 AgentId 与 Secret。
		MessagingFields: []CredentialField{
			{Key: "agent_id", Title: i18n.T("应用 AgentId", "App AgentId"), Placeholder: "1000002",
				Hint: i18n.T("管理后台 → 应用管理 → 自建应用 → 该应用页面上的 AgentId", "Admin console → App Management → the custom app → AgentId on its page")},
			{Key: "app_secret", Title: i18n.T("应用 Secret", "App Secret"), Secret: true,
				Hint: i18n.T("同一页的 Secret（不是通讯录同步的 Secret）；保存后只显示是否已设置", "The Secret on the same page (not the contacts-sync Secret); only whether it is set is shown after saving")},
		},
		MessagingPrerequisites: []i18n.Text{
			i18n.T("在企业微信管理后台创建一个自建应用，把要接收提醒的人加进它的可见范围", "Create a custom app in the WeCom admin console and add the people who should receive reminders to its visible range"),
			i18n.T("把本系统的出网 IP 加进该应用的企业可信 IP", "Add this system's outbound IP to that app's trusted IPs"),
		},
		NewMessenger: func(creds map[string]string, opts Options) (Messenger, error) { return NewWeComMessenger(creds, opts) },
		// 外部日历（ADR 0032）：用自建应用的 Secret 读一个企业日历里的日程，按参与人对到成员。
		CalendarFields: []CredentialField{
			{Key: "cal_id", Title: i18n.T("企业日历 ID", "Company calendar ID"), Placeholder: "wcxxxxxxxxxxxxxxxx",
				Hint: i18n.T("管理后台 → 应用管理 → 日程 → 企业日历；成员的会议要在这本日历里", "Admin console → App Management → Schedule → company calendars; members' meetings must live in this calendar")},
		},
		CalendarPrerequisites: []i18n.Text{
			i18n.T("在企业微信管理后台把「日程」的接口权限授予该自建应用", "Grant the custom app access to the Schedule API in the WeCom admin console"),
			i18n.T("成员要先通过 IM 集成同步进来，日程按他的企业微信账号对应", "Members must be synced through the IM integration first; events are matched by their WeCom account"),
		},
		NewCalendar: func(creds map[string]string, opts Options) (Calendar, error) { return NewWeComCalendar(creds, opts) },
	})
}

// NewWeComCalendar 用自建应用的 Secret 与企业日历 ID 建日历客户端。
func NewWeComCalendar(creds map[string]string, opts Options) (*WeCom, error) {
	w, err := NewWeComMessenger(creds, opts)
	if err != nil {
		return nil, err
	}
	w.calID = creds["cal_id"]
	return w, nil
}

// wecomRootDepartmentID 是企业微信根部门的编号。
const wecomRootDepartmentID = "1"

// WeCom 是企业微信通讯录客户端（自建应用 / 通讯录同步 Secret）。用到的接口：
//
//	GET /cgi-bin/gettoken?corpid=&corpsecret=                       → {errcode, errmsg, access_token, expires_in}
//	GET /cgi-bin/department/list?access_token=&id=                  → department[]{id, name, parentid, order}（含该部门及全部后代；根部门 1 的父是 0）
//	GET /cgi-bin/user/list?access_token=&department_id=&fetch_child= → userlist[]{userid, name, mobile, email, biz_mail, department[], status}
//
// 每个响应都带 errcode / errmsg；errcode 非 0 一律当作提供方拒绝，把 errmsg 原话带回。
// status：1 已激活、2 已禁用、4 未激活、5 退出企业 → 1 与 4 算在职，2 与 5 不算。
type WeCom struct {
	calID      string // 外部日历：企业日历 ID
	BaseURL    string
	corpID     string
	corpSecret string
	agentID    string // 发消息用的自建应用 AgentId（只在消息客户端上有）
	http       *http.Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

// NewWeCom 创建客户端；creds 需要 corp_id 与 corp_secret。
func NewWeCom(creds map[string]string, opts Options) (*WeCom, error) {
	hc, err := newHTTPClient(opts)
	if err != nil {
		return nil, err
	}
	return &WeCom{BaseURL: "https://qyapi.weixin.qq.com", corpID: creds["corp_id"], corpSecret: creds["corp_secret"], http: hc}, nil
}

type wecomEnvelope struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// NewWeComMessenger 创建发消息客户端：令牌用自建应用的 Secret（app_secret）换，touser 用 userid，agentid 是应用的 AgentId。
func NewWeComMessenger(creds map[string]string, opts Options) (*WeCom, error) {
	w, err := NewWeCom(creds, opts)
	if err != nil {
		return nil, err
	}
	if s := strings.TrimSpace(creds["app_secret"]); s != "" {
		w.corpSecret = s
	}
	w.agentID = strings.TrimSpace(creds["agent_id"])
	return w, nil
}

// 令牌失效类错误码：40014 不合法的 access_token，42001 access_token 已过期；遇到就重取一次令牌再试。
func wecomTokenExpired(code int) bool { return code == 40014 || code == 42001 }

// get 发一次 GET，把 JSON 解到 out（out 里要嵌 wecomEnvelope）；auth 为真时带 access_token。
func (w *WeCom) get(ctx context.Context, path string, q url.Values, out any, auth bool) error {
	return w.call(ctx, http.MethodGet, path, q, nil, out, auth)
}

// post 发一次 JSON POST（发消息用）。
func (w *WeCom) post(ctx context.Context, path string, body any, out any) error {
	return w.call(ctx, http.MethodPost, path, nil, body, out, true)
}

func (w *WeCom) call(ctx context.Context, method, path string, q url.Values, body any, out any, auth bool) error {
	for attempt := 0; attempt < 2; attempt++ {
		q2 := url.Values{}
		for k, v := range q {
			q2[k] = v
		}
		if auth {
			tok, err := w.Token(ctx)
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
		req, err := http.NewRequestWithContext(ctx, method, w.BaseURL+path+"?"+q2.Encode(), rd)
		if err != nil {
			return err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
		}
		resp, err := w.http.Do(req)
		if err != nil {
			return &UnreachableError{Err: err}
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if err != nil {
			return &UnreachableError{Err: err}
		}
		var env wecomEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return &RejectedError{Code: resp.StatusCode, Msg: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, trim(string(raw), 200))}
		}
		if env.ErrCode != 0 {
			if auth && attempt == 0 && wecomTokenExpired(env.ErrCode) {
				w.mu.Lock()
				w.token = ""
				w.mu.Unlock()
				continue
			}
			return &RejectedError{Code: env.ErrCode, Msg: fmt.Sprintf("%s (errcode %d)", env.ErrMsg, env.ErrCode)}
		}
		if resp.StatusCode >= 400 {
			return &RejectedError{Code: resp.StatusCode, Msg: fmt.Sprintf("HTTP %d", resp.StatusCode)}
		}
		return json.Unmarshal(raw, out)
	}
	return &RejectedError{Code: 42001, Msg: "access_token expired twice"}
}

// Token 取 access_token，未过期时复用（有效期 expires_in，提前 60 秒作废）。
func (w *WeCom) Token(ctx context.Context) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.token != "" && time.Now().Before(w.tokenExp) {
		return w.token, nil
	}
	var out struct {
		wecomEnvelope
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := w.get(ctx, "/cgi-bin/gettoken", url.Values{"corpid": {w.corpID}, "corpsecret": {w.corpSecret}}, &out, false); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", &RejectedError{Code: -1, Msg: "empty access_token"}
	}
	w.token = out.AccessToken
	ttl := time.Duration(out.ExpiresIn) * time.Second
	if ttl <= 2*time.Minute {
		ttl = 2 * time.Hour
	}
	w.tokenExp = time.Now().Add(ttl - time.Minute)
	return w.token, nil
}

// 企业微信的部门编号是整数；统一模型里用十进制字符串。
type wecomDept struct {
	ID       json.Number `json:"id"`
	Name     string      `json:"name"`
	ParentID json.Number `json:"parentid"`
}

func (d wecomDept) toDept() Dept {
	return Dept{ID: d.ID.String(), Name: d.Name, ParentID: d.ParentID.String()}
}

// list 调 department/list：返回 id 及其全部后代。
func (w *WeCom) list(ctx context.Context, id string) ([]wecomDept, error) {
	var out struct {
		wecomEnvelope
		Department []wecomDept `json:"department"`
	}
	q := url.Values{}
	if id != "" {
		q.Set("id", id)
	}
	if err := w.get(ctx, "/cgi-bin/department/list", q, &out, true); err != nil {
		return nil, err
	}
	return out.Department, nil
}

// Department 读一个部门（department/list 返回自身及后代，取自身那条）。
func (w *WeCom) Department(ctx context.Context, id string) (*Dept, error) {
	if id == "" {
		id = wecomRootDepartmentID
	}
	ds, err := w.list(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, d := range ds {
		if d.ID.String() == id {
			x := d.toDept()
			return &x, nil
		}
	}
	return nil, &RejectedError{Code: 60003, Msg: fmt.Sprintf("department %s not found (errcode 60003)", id)}
}

// Departments 读根部门下的全部子部门（不含根自身；接口一次返回全部后代，不分页）。
func (w *WeCom) Departments(ctx context.Context, rootID string) ([]Dept, error) {
	if rootID == "" {
		rootID = wecomRootDepartmentID
	}
	ds, err := w.list(ctx, rootID)
	if err != nil {
		return nil, err
	}
	out := []Dept{}
	for _, d := range ds {
		if d.ID.String() == rootID {
			continue
		}
		out = append(out, d.toDept())
	}
	return out, nil
}

type wecomUser struct {
	UserID     string        `json:"userid"`
	Name       string        `json:"name"`
	Mobile     string        `json:"mobile"`
	Email      string        `json:"email"`
	BizMail    string        `json:"biz_mail"`
	Department []json.Number `json:"department"`
	Status     int           `json:"status"`
}

// listUsers 调 user/list 拿原始字段；fetchChild 为真时含全部后代部门的人。
func (w *WeCom) listUsers(ctx context.Context, deptID string, fetchChild bool) ([]wecomUser, error) {
	var out struct {
		wecomEnvelope
		UserList []wecomUser `json:"userlist"`
	}
	fc := "0"
	if fetchChild {
		fc = "1"
	}
	if err := w.get(ctx, "/cgi-bin/user/list", url.Values{"department_id": {deptID}, "fetch_child": {fc}}, &out, true); err != nil {
		return nil, err
	}
	return out.UserList, nil
}

// Users 读某部门的直属人员（fetch_child=0；接口不分页）。
func (w *WeCom) Users(ctx context.Context, deptID string) ([]User, error) {
	if deptID == "" {
		deptID = wecomRootDepartmentID
	}
	list, err := w.listUsers(ctx, deptID, false)
	if err != nil {
		return nil, err
	}
	res := make([]User, 0, len(list))
	for _, u := range list {
		if u.UserID == "" {
			continue
		}
		email := u.BizMail
		if email == "" {
			email = u.Email
		}
		name := strings.TrimSpace(u.Name)
		if name == "" {
			// 没有敏感字段权限时企业微信不返回姓名：先用 userid 顶着，同步结果会说明（ADR 0017 补记）
			name = u.UserID
		}
		deps := make([]string, 0, len(u.Department))
		for _, d := range u.Department {
			deps = append(deps, d.String())
		}
		res = append(res, User{ID: u.UserID, Name: name, Email: strings.ToLower(strings.TrimSpace(email)), Mobile: u.Mobile,
			DeptIDs: deps, Active: u.Status == 1 || u.Status == 4})
	}
	return res, nil
}

// ---------- 接入检查（ADR 0017 补记二） ----------

// 企业微信管理后台的页面：首页、我的企业 → 企业信息、管理工具 → 通讯录同步。
const (
	wecomConsoleHome     = "https://work.weixin.qq.com/wework_admin/frame"
	wecomConsoleProfile  = "https://work.weixin.qq.com/wework_admin/frame#profile"
	wecomConsoleContacts = "https://work.weixin.qq.com/wework_admin/frame#apps/contactsApi"
	wecomConsoleApps     = "https://work.weixin.qq.com/wework_admin/frame#apps"
)

// wecomIPNotTrusted 是"不在企业可信 IP 里"的错误码；errmsg 里带着我们的出网 IP。
const wecomIPNotTrusted = 60020

var wecomFromIP = regexp.MustCompile(`from ip:?\s*([0-9a-fA-F.:]+)`)

// Diagnose 按顺序跑企业微信的接入检查：凭据可用 → 可信 IP → 能读到部门树 → 姓名等敏感字段权限。
// gettoken 本身也可能因 IP 不可信报 60020，这时凭据算通过、可信 IP 阻塞。
func (w *WeCom) Diagnose(ctx context.Context, rootID string) ([]Check, error) {
	if rootID == "" {
		rootID = wecomRootDepartmentID
	}
	credentials := &Check{Key: "credentials", Title: T("凭据可用", "Credentials work")}
	trustedIP := &Check{Key: "trusted_ip", Title: T("服务器 IP 在可信 IP 里", "Server IP is trusted")}
	structure := &Check{Key: "structure", Title: T("能读到部门树", "Department tree readable")}
	names := &Check{Key: "names", Title: T("姓名等敏感字段权限", "Sensitive fields (names)")}
	checks := []*Check{credentials, trustedIP, structure, names}

	ipBlocked := func(msg string) {
		ip := "?"
		if m := wecomFromIP.FindStringSubmatch(msg); m != nil {
			ip = m[1]
		}
		trustedIP.Status = CheckBlocked
		trustedIP.Detail = rawText("企业微信说：%s。服务器的出网 IP 是 %s。", "WeCom said: %s. The server's outbound IP is %s.", msg, ip)
		trustedIP.Fix = rawText("在「管理工具 → 通讯录同步」的企业可信 IP 里加入 %s。", "Under “Management Tools → Contacts Sync”, add %s to the trusted IPs.", ip)
		trustedIP.FixURL = wecomConsoleContacts
	}

	// 1. 凭据
	if _, err := w.Token(ctx); err != nil {
		var un *UnreachableError
		if errors.As(err, &un) {
			return nil, err
		}
		code, msg, _ := rejectedCode(err)
		if code == wecomIPNotTrusted {
			credentials.Status = CheckOK
			credentials.Detail = T("企业微信认出了这组凭据。", "WeCom recognised these credentials.")
			ipBlocked(msg)
			return finishChecks(checks), nil
		}
		credentials.Status = CheckBlocked
		credentials.Detail = rawText("企业微信拒绝了这组凭据：%s。", "WeCom rejected these credentials: %s.", msg)
		credentials.Fix = T("到「我的企业 → 企业信息」页最底部复制企业 ID，到「管理工具 → 通讯录同步」复制 Secret，重新填写。", "Copy the Corp ID from the bottom of “My Company → Company Info” and the Secret from “Management Tools → Contacts Sync”, then enter them again.")
		credentials.FixURL = wecomConsoleProfile
		return finishChecks(checks), nil
	}
	credentials.Status = CheckOK
	credentials.Detail = T("已取到访问令牌。", "Got an access token.")

	// 2/3. 可信 IP 与部门树：同一个接口
	depts, err := w.list(ctx, rootID)
	if err != nil {
		var un *UnreachableError
		if errors.As(err, &un) {
			return nil, err
		}
		code, msg, _ := rejectedCode(err)
		if code == wecomIPNotTrusted {
			ipBlocked(msg)
			return finishChecks(checks), nil
		}
		trustedIP.Status = CheckOK
		trustedIP.Detail = T("服务器 IP 在可信 IP 列表里。", "The server IP is in the trusted list.")
		structure.Status = CheckBlocked
		structure.Detail = rawText("读部门树时企业微信说：%s。", "Reading the department tree, WeCom said: %s.", msg)
		structure.Fix = T("确认「管理工具 → 通讯录同步」已开启 API 接口同步，且填的是它的 Secret。", "Make sure API sync is enabled under “Management Tools → Contacts Sync” and that its Secret is the one entered.")
		structure.FixURL = wecomConsoleContacts
		return finishChecks(checks), nil
	}
	trustedIP.Status = CheckOK
	trustedIP.Detail = T("服务器 IP 在可信 IP 列表里。", "The server IP is in the trusted list.")
	structure.Status = CheckOK
	example := ""
	for _, d := range depts {
		if strings.TrimSpace(d.Name) != "" {
			example = d.Name
			break
		}
	}
	structure.Detail = rawText("读到了 %d 个部门（如「%s」）。", "Read %d departments (e.g. “%s”).", len(depts), example)

	// 4. 敏感字段：读一份人员看有没有姓名
	users, err := w.listUsers(ctx, rootID, true)
	switch {
	case err != nil:
		_, msg, _ := rejectedCode(err)
		names.Status = CheckSkipped
		names.Detail = rawText("读人员时企业微信说：%s。", "Reading people, WeCom said: %s.", msg)
	case len(users) == 0:
		names.Status = CheckSkipped
		names.Detail = T("这些部门里没有人员，无法判断。", "These departments have no members, so this cannot be checked.")
	default:
		nameless := 0
		for _, u := range users {
			if strings.TrimSpace(u.Name) == "" {
				nameless++
			}
		}
		if nameless == len(users) {
			names.Status = CheckBlocked
			names.Detail = rawText("读到了 %d 个人，但都没有姓名：应用没有敏感字段的读取权限。", "Read %d people, but none has a name: the app may not read sensitive fields.", len(users))
			names.Fix = T("在「管理工具 → 通讯录同步」里开通姓名、手机、邮箱等敏感字段的读取权限。", "Under “Management Tools → Contacts Sync”, allow reading sensitive fields such as name, mobile and email.")
			names.FixURL = wecomConsoleContacts
		} else {
			names.Status = CheckOK
			names.Detail = T("姓名能读到。", "Names are readable.")
		}
	}
	return finishChecks(checks), nil
}

// ---------- 发消息（ADR 0019） ----------

// SendDirect 给一个人发应用消息：POST /cgi-bin/message/send，有链接时用 textcard（标题、一句话、按钮「详情」），
// 没有链接时用 text。touser 是 userid。
func (w *WeCom) SendDirect(ctx context.Context, userID string, msg Message) error {
	if w.agentID == "" {
		return &RejectedError{Code: -1, Msg: "missing agent_id"}
	}
	body := map[string]any{"touser": userID, "agentid": w.agentID}
	if msg.URL != "" {
		open := msg.Open
		if open == "" {
			open = "打开"
		}
		desc := msg.Text
		if desc == "" {
			desc = msg.Title
		}
		body["msgtype"] = "textcard"
		body["textcard"] = map[string]string{"title": msg.Title, "description": desc, "url": msg.URL, "btntxt": open}
	} else {
		text := msg.Title
		if msg.Text != "" && msg.Text != msg.Title {
			text += "\n" + msg.Text
		}
		body["msgtype"] = "text"
		body["text"] = map[string]string{"content": text}
	}
	var out struct {
		wecomEnvelope
		InvalidUser string `json:"invaliduser"`
	}
	if err := w.post(ctx, "/cgi-bin/message/send", body, &out); err != nil {
		return err
	}
	if out.InvalidUser != "" {
		return &RejectedError{Code: 81013, Msg: fmt.Sprintf("invaliduser: %s（这个人不在应用的可见范围里）", out.InvalidUser)}
	}
	return nil
}

// DiagnoseMessaging 检查「能以应用身份发消息」：用应用的 Secret 读它自己的信息（agent/get），
// 能读到说明 AgentId 与 Secret 配对且可信 IP 通过；不真的发消息。
func (w *WeCom) DiagnoseMessaging(ctx context.Context) Check {
	c := Check{Key: MessagingCheckKey, Title: T("能以应用身份发消息", "Can send messages as the app")}
	if w.agentID == "" {
		c.Status = CheckTodo
		c.Detail = T("还没填自建应用的 AgentId 与 Secret；通讯录同步的 Secret 发不了消息。", "The custom app's AgentId and Secret are not set; the contacts-sync Secret cannot send messages.")
		c.Fix = T("在「组织设置 → 通知」的企业微信通道里填自建应用的 AgentId 与 Secret。", "Under “Organization settings → Notifications”, fill in the custom app's AgentId and Secret for the WeCom channel.")
		c.FixURL = wecomConsoleApps
		return c
	}
	var out struct {
		wecomEnvelope
		Name string `json:"name"`
	}
	err := w.get(ctx, "/cgi-bin/agent/get", url.Values{"agentid": {w.agentID}}, &out, true)
	if err == nil {
		c.Status = CheckOK
		c.Detail = rawText("应用「%s」的凭据可用，可以发消息。", "The credentials of app “%s” work; messages can be sent.", out.Name)
		return c
	}
	var un *UnreachableError
	if errors.As(err, &un) {
		c.Status = CheckTodo
		c.Detail = rawText("连不上企业微信：%s。", "Could not reach WeCom: %s.", un.Err.Error())
		c.Fix = T("检查服务器的网络或出网代理设置，然后重新检查。", "Check the server's network or outbound proxy setting, then re-check.")
		return c
	}
	code, msg, _ := rejectedCode(err)
	c.Status = CheckTodo
	if code == wecomIPNotTrusted {
		ip := "?"
		if m := wecomFromIP.FindStringSubmatch(msg); m != nil {
			ip = m[1]
		}
		c.Detail = rawText("企业微信说：%s。服务器的出网 IP 是 %s。", "WeCom said: %s. The server's outbound IP is %s.", msg, ip)
		c.Fix = rawText("在该应用页面的企业可信 IP 里加入 %s。", "On the app's page, add %s to the trusted IPs.", ip)
	} else {
		c.Detail = rawText("用应用的 Secret 读应用信息时企业微信说：%s。没有它时待确认操作与验收提醒发不到企业微信，只在站内。", "Reading the app with its Secret, WeCom said: %s. Without it, reminders stay in-app only.", msg)
		c.Fix = T("到「应用管理 → 自建应用」核对 AgentId 与 Secret，重新填写；不需要 IM 提醒也可以先跳过。", "Under “App Management → Custom apps” check the AgentId and Secret and enter them again; skip it if IM reminders are not needed.")
	}
	c.FixURL = wecomConsoleApps
	return c
}

// ---------- 外部日历（ADR 0032） ----------
//
//	POST /cgi-bin/oa/schedule/get_by_calendar  {cal_id, offset, limit} → {schedule_list:[{schedule_id, summary, start_time, end_time, status, attendees:[{userid}], organizer}]}

type wecomSchedules struct {
	wecomEnvelope
	ScheduleList []struct {
		ScheduleID string `json:"schedule_id"`
		Summary    string `json:"summary"`
		StartTime  int64  `json:"start_time"`
		EndTime    int64  `json:"end_time"`
		Status     int    `json:"status"` // 1 正常 2 已取消
		Organizer  string `json:"organizer"`
		Attendees  []struct {
			UserID string `json:"userid"`
		} `json:"attendees"`
	} `json:"schedule_list"`
}

// Events 读企业日历里这个成员参与的日程（组织者或参与人）。
func (w *WeCom) Events(ctx context.Context, externalUserID string, from, to time.Time) ([]CalendarEvent, error) {
	if w.calID == "" {
		return nil, &RejectedError{Code: 400, Msg: "cal_id not configured"}
	}
	var out []CalendarEvent
	for offset := 0; offset < 5000; offset += 500 {
		var res wecomSchedules
		if err := w.post(ctx, "/cgi-bin/oa/schedule/get_by_calendar", map[string]any{"cal_id": w.calID, "offset": offset, "limit": 500}, &res); err != nil {
			return nil, err
		}
		for _, it := range res.ScheduleList {
			mine := it.Organizer == externalUserID
			for _, a := range it.Attendees {
				if a.UserID == externalUserID {
					mine = true
				}
			}
			if !mine || it.Status == 2 {
				continue
			}
			start, end := time.Unix(it.StartTime, 0), time.Unix(it.EndTime, 0)
			if !end.After(from) || !start.Before(to) {
				continue
			}
			out = append(out, CalendarEvent{ExternalID: it.ScheduleID, Title: it.Summary, Start: start, End: end, Busy: true})
		}
		if len(res.ScheduleList) < 500 {
			break
		}
	}
	return out, nil
}
