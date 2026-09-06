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
	"strings"
	"sync"
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

// 飞书作为提供方登记到注册表：凭据是企业自建应用的 App ID 与 App Secret，根部门编号 "0"。
func init() {
	Register(Provider{
		Key:              "feishu",
		Title:            i18n.T("飞书", "Feishu"),
		RootDepartmentID: feishuRootDepartmentID,
		Fields: []CredentialField{
			{Key: "app_id", Title: i18n.T("App ID", "App ID"), Placeholder: "cli_xxxxxxxxxxxxxxxx",
				Hint: i18n.T("飞书开放平台 → 开发者后台 → 应用 → 凭证与基础信息", "Feishu Open Platform → Developer Console → the app → Credentials & Basic Info")},
			{Key: "app_secret", Title: i18n.T("App Secret", "App Secret"), Secret: true,
				Hint: i18n.T("与 App ID 同一页；保存后只显示是否已设置", "Same page as the App ID; only whether it is set is shown after saving")},
		},
		Tip: GuideStep{Text: i18n.T("App ID 和 App Secret 在飞书开放平台里你创建的企业自建应用的「凭证与基础信息」页；应用要开通通讯录只读权限并发布版本。", "The App ID and App Secret are on the “Credentials & Basic Info” page of your custom app on Feishu Open Platform; the app needs read-only contact permissions and a released version."), URL: "https://open.feishu.cn/app"},
		Prerequisites: []i18n.Text{
			i18n.T("在飞书开放平台创建一个企业自建应用", "Create a custom enterprise app on Feishu Open Platform"),
			i18n.T("为它开通通讯录只读权限（获取通讯录基本信息、获取部门基础信息、获取用户基本信息），并发布版本", "Grant it read-only contact permissions (contact, department and user basic info) and publish a version"),
			i18n.T("把要同步的部门加进应用的通讯录权限范围", "Add the departments to sync to the app's contact permission scope"),
		},
		New:        func(creds map[string]string, opts Options) (Directory, error) { return NewFeishu(creds, opts) },
		ConsoleURL: func(creds map[string]string) string { return feishuConsoleURL(creds["app_id"], "baseinfo") },
	})
}

// feishuRootDepartmentID 是飞书根部门的编号。
const feishuRootDepartmentID = "0"

// Feishu 是飞书通讯录客户端。用到的接口（均已对照 open.feishu.cn 文档核对字段名）：
//
//	POST /open-apis/auth/v3/tenant_access_token/internal   {app_id, app_secret} → {code, msg, tenant_access_token, expire}
//	GET  /open-apis/contact/v3/departments/{id}             ?department_id_type=open_department_id → data.department{...}
//	GET  /open-apis/contact/v3/departments/{id}/children    ?fetch_child=true&page_size=50&department_id_type=open_department_id&page_token=
//	     → data.items[]{name, open_department_id, parent_department_id, status.is_deleted}, data.has_more, data.page_token
//	GET  /open-apis/contact/v3/users/find_by_department     ?department_id=&page_size=50&user_id_type=open_id&department_id_type=open_department_id&page_token=
//	     → data.items[]{open_id, name, email, enterprise_email, mobile, department_ids[], status{is_frozen,is_resigned,is_exited}}
//	GET  /open-apis/tenant/v2/tenant/query                  → data.tenant.name（可选，没权限时忽略）
type Feishu struct {
	BaseURL   string
	appID     string
	appSecret string
	http      *http.Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

// NewFeishu 创建客户端；creds 需要 app_id 与 app_secret。
func NewFeishu(creds map[string]string, opts Options) (*Feishu, error) {
	hc, err := newHTTPClient(opts)
	if err != nil {
		return nil, err
	}
	return &Feishu{BaseURL: "https://open.feishu.cn", appID: creds["app_id"], appSecret: creds["app_secret"], http: hc}, nil
}

type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
	// 令牌接口把令牌放在顶层
	TenantAccessToken string `json:"tenant_access_token"`
	Expire            int    `json:"expire"`
}

func (f *Feishu) do(ctx context.Context, method, path string, q url.Values, body any, auth bool) (*envelope, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	u := f.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	if auth {
		tok, err := f.Token(ctx)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := f.http.Do(req)
	if err != nil {
		return nil, &UnreachableError{Err: err}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, &UnreachableError{Err: err}
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, &RejectedError{Code: resp.StatusCode, Msg: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, trim(string(raw), 200))}
	}
	if env.Code != 0 {
		return nil, &RejectedError{Code: env.Code, Msg: fmt.Sprintf("%s (code %d)", env.Msg, env.Code)}
	}
	if resp.StatusCode >= 400 {
		return nil, &RejectedError{Code: resp.StatusCode, Msg: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	return &env, nil
}

// Token 取 tenant_access_token，未过期时复用。
func (f *Feishu) Token(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.token != "" && time.Now().Before(f.tokenExp) {
		return f.token, nil
	}
	env, err := f.do(ctx, http.MethodPost, "/open-apis/auth/v3/tenant_access_token/internal", nil,
		map[string]string{"app_id": f.appID, "app_secret": f.appSecret}, false)
	if err != nil {
		return "", err
	}
	if env.TenantAccessToken == "" {
		return "", &RejectedError{Code: -1, Msg: "empty tenant_access_token"}
	}
	f.token = env.TenantAccessToken
	ttl := time.Duration(env.Expire) * time.Second
	if ttl <= 2*time.Minute {
		ttl = 30 * time.Minute
	}
	f.tokenExp = time.Now().Add(ttl - time.Minute)
	return f.token, nil
}

type feishuDept struct {
	Name               string `json:"name"`
	OpenDepartmentID   string `json:"open_department_id"`
	DepartmentID       string `json:"department_id"`
	ParentDepartmentID string `json:"parent_department_id"`
	Status             struct {
		IsDeleted bool `json:"is_deleted"`
	} `json:"status"`
}

func (d feishuDept) toDept() Dept {
	id := d.OpenDepartmentID
	if id == "" {
		id = d.DepartmentID
	}
	return Dept{ID: id, Name: d.Name, ParentID: d.ParentDepartmentID, Deleted: d.Status.IsDeleted}
}

// Department 读一个部门。
func (f *Feishu) Department(ctx context.Context, id string) (*Dept, error) {
	q := url.Values{"department_id_type": {"open_department_id"}}
	env, err := f.do(ctx, http.MethodGet, "/open-apis/contact/v3/departments/"+url.PathEscape(id), q, nil, true)
	if err != nil {
		return nil, err
	}
	var data struct {
		Department feishuDept `json:"department"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return nil, err
	}
	d := data.Department.toDept()
	if d.ID == "" {
		d.ID = id
	}
	return &d, nil
}

// Departments 读根部门下的全部子部门（fetch_child=true，分页）。
func (f *Feishu) Departments(ctx context.Context, rootID string) ([]Dept, error) {
	if rootID == "" {
		rootID = feishuRootDepartmentID
	}
	var out []Dept
	token := ""
	for page := 0; page < 1000; page++ {
		q := url.Values{"fetch_child": {"true"}, "page_size": {"50"}, "department_id_type": {"open_department_id"}}
		if token != "" {
			q.Set("page_token", token)
		}
		env, err := f.do(ctx, http.MethodGet, "/open-apis/contact/v3/departments/"+url.PathEscape(rootID)+"/children", q, nil, true)
		if err != nil {
			return nil, err
		}
		var data struct {
			HasMore   bool         `json:"has_more"`
			PageToken string       `json:"page_token"`
			Items     []feishuDept `json:"items"`
		}
		if err := json.Unmarshal(env.Data, &data); err != nil {
			return nil, err
		}
		for _, it := range data.Items {
			out = append(out, it.toDept())
		}
		if !data.HasMore || data.PageToken == "" {
			break
		}
		token = data.PageToken
	}
	return out, nil
}

type feishuUser struct {
	OpenID          string   `json:"open_id"`
	Name            string   `json:"name"`
	Email           string   `json:"email"`
	EnterpriseEmail string   `json:"enterprise_email"`
	Mobile          string   `json:"mobile"`
	DepartmentIDs   []string `json:"department_ids"`
	Status          struct {
		IsFrozen   bool `json:"is_frozen"`
		IsResigned bool `json:"is_resigned"`
		IsExited   bool `json:"is_exited"`
	} `json:"status"`
}

// Users 读某部门的直属人员（分页）。
func (f *Feishu) Users(ctx context.Context, deptID string) ([]User, error) {
	if deptID == "" {
		deptID = feishuRootDepartmentID
	}
	var out []User
	token := ""
	for page := 0; page < 1000; page++ {
		q := url.Values{"department_id": {deptID}, "page_size": {"50"}, "user_id_type": {"open_id"}, "department_id_type": {"open_department_id"}}
		if token != "" {
			q.Set("page_token", token)
		}
		env, err := f.do(ctx, http.MethodGet, "/open-apis/contact/v3/users/find_by_department", q, nil, true)
		if err != nil {
			return nil, err
		}
		var data struct {
			HasMore   bool         `json:"has_more"`
			PageToken string       `json:"page_token"`
			Items     []feishuUser `json:"items"`
		}
		if err := json.Unmarshal(env.Data, &data); err != nil {
			return nil, err
		}
		for _, it := range data.Items {
			email := it.EnterpriseEmail
			if email == "" {
				email = it.Email
			}
			out = append(out, User{ID: it.OpenID, Name: it.Name, Email: strings.ToLower(strings.TrimSpace(email)), Mobile: it.Mobile,
				DeptIDs: it.DepartmentIDs, Active: !it.Status.IsResigned && !it.Status.IsFrozen && !it.Status.IsExited})
		}
		if !data.HasMore || data.PageToken == "" {
			break
		}
		token = data.PageToken
	}
	return out, nil
}

// TenantName 读企业名称（需要 tenant:tenant:readonly 权限；没有就返回错误，调用方忽略）。
func (f *Feishu) TenantName(ctx context.Context) (string, error) {
	env, err := f.do(ctx, http.MethodGet, "/open-apis/tenant/v2/tenant/query", nil, nil, true)
	if err != nil {
		return "", err
	}
	var data struct {
		Tenant struct {
			Name string `json:"name"`
		} `json:"tenant"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return "", err
	}
	return data.Tenant.Name, nil
}

// AuthorizedDepartments 读应用的通讯录权限范围里的部门（GET /contact/v3/scopes，分页）。
// 权限范围是"全部成员"时飞书返回根部门 "0"。
func (f *Feishu) AuthorizedDepartments(ctx context.Context) ([]string, error) {
	var out []string
	token := ""
	for {
		q := url.Values{"department_id_type": {"open_department_id"}, "user_id_type": {"open_id"}, "page_size": {"100"}}
		if token != "" {
			q.Set("page_token", token)
		}
		env, err := f.do(ctx, http.MethodGet, "/open-apis/contact/v3/scopes", q, nil, true)
		if err != nil {
			return nil, err
		}
		var data struct {
			DepartmentIDs []string `json:"department_ids"`
			HasMore       bool     `json:"has_more"`
			PageToken     string   `json:"page_token"`
		}
		if err := json.Unmarshal(env.Data, &data); err != nil {
			return nil, err
		}
		out = append(out, data.DepartmentIDs...)
		if !data.HasMore || data.PageToken == "" {
			return out, nil
		}
		token = data.PageToken
	}
}

// ---------- 接入检查（ADR 0017 补记二） ----------

// feishuConsoleURL 是这个应用在飞书开放平台里的某一页：baseinfo 凭证与基础信息、auth 权限管理、version 版本管理与发布。
func feishuConsoleURL(appID, page string) string {
	return "https://open.feishu.cn/app/" + url.PathEscape(appID) + "/" + page
}

// feishuNoDeptAuthority 是"通讯录权限范围不包含这个部门"的错误码。
const feishuNoDeptAuthority = 40004

// feishuPermissionCode 是"应用没有这个接口的权限"一类的错误码：权限没开通、或开通了但版本没发布时，所有读接口都这样报。
func feishuPermissionCode(code int) bool {
	switch code {
	case 99991661, 99991663, 99991664, 99991668, 99991672:
		return true
	}
	return false
}

// probeDepts 读一页（最多 5 个）直属子部门的原始字段，用来看名称有没有被权限裁掉。
func (f *Feishu) probeDepts(ctx context.Context, id string) ([]feishuDept, error) {
	q := url.Values{"fetch_child": {"false"}, "page_size": {"5"}, "department_id_type": {"open_department_id"}}
	env, err := f.do(ctx, http.MethodGet, "/open-apis/contact/v3/departments/"+url.PathEscape(id)+"/children", q, nil, true)
	if err != nil {
		return nil, err
	}
	var data struct {
		Items []feishuDept `json:"items"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return nil, err
	}
	return data.Items, nil
}

// probeUsers 读一页（最多 5 个）直属人员的原始字段。
func (f *Feishu) probeUsers(ctx context.Context, deptID string) ([]feishuUser, error) {
	q := url.Values{"department_id": {deptID}, "page_size": {"5"}, "user_id_type": {"open_id"}, "department_id_type": {"open_department_id"}}
	env, err := f.do(ctx, http.MethodGet, "/open-apis/contact/v3/users/find_by_department", q, nil, true)
	if err != nil {
		return nil, err
	}
	var data struct {
		Items []feishuUser `json:"items"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return nil, err
	}
	return data.Items, nil
}

// Diagnose 按顺序跑飞书的接入检查：凭据可用 → 通讯录权限范围 → 部门名称权限 → 人员姓名权限 → 邮箱权限 → 版本已发布。
// 飞书按权限裁字段而不报错（没有「获取部门基础信息」时部门没有 name，没有「获取用户基本信息」时人没有 name，
// 没有「获取用户邮箱信息」时没有 email，接口都返回 code 0），所以名称类检查只能真的读一页来探测。
// 权限范围只含部分部门时读根部门报 40004，这时从 /contact/v3/scopes 拿被授权的部门列进 Roots，让人从里面挑同步范围。
func (f *Feishu) Diagnose(ctx context.Context, rootID string) ([]Check, error) {
	if rootID == "" {
		rootID = feishuRootDepartmentID
	}
	authURL := feishuConsoleURL(f.appID, "auth")
	credentials := &Check{Key: "credentials", Title: T("凭据可用", "Credentials work")}
	scope := &Check{Key: "scope", Title: T("通讯录权限范围", "Contact scope")}
	deptNames := &Check{Key: "dept_names", Title: T("部门名称权限", "Department names")}
	userNames := &Check{Key: "user_names", Title: T("人员姓名权限", "People's names")}
	userEmails := &Check{Key: "user_emails", Title: T("邮箱权限", "Email addresses")}
	userDepts := &Check{Key: "user_departments", Title: T("人员所属部门权限", "People's department membership")}
	published := &Check{Key: "published", Title: T("版本已发布", "Version released")}
	checks := []*Check{credentials, scope, deptNames, userNames, userEmails, userDepts, published}

	// 1. 凭据
	if _, err := f.Token(ctx); err != nil {
		var un *UnreachableError
		if errors.As(err, &un) {
			return nil, err
		}
		_, msg, _ := rejectedCode(err)
		credentials.Status = CheckBlocked
		credentials.Detail = rawText("飞书拒绝了这组凭据：%s。", "Feishu rejected these credentials: %s.", msg)
		credentials.Fix = T("到应用的「凭证与基础信息」页复制 App ID 与 App Secret，重新填写。", "Copy the App ID and App Secret from the app's “Credentials & Basic Info” page and enter them again.")
		credentials.FixURL = feishuConsoleURL(f.appID, "baseinfo")
		return finishChecks(checks), nil
	}
	credentials.Status = CheckOK
	credentials.Detail = T("已取到访问令牌。", "Got an access token.")

	// 每次读接口都记一笔：全部以"没权限"类错误码失败时，最可能是版本没发布
	reads, permissionFails := 0, 0
	lastMsg := ""
	note := func(err error) {
		reads++
		if code, msg, ok := rejectedCode(err); ok && feishuPermissionCode(code) {
			permissionFails++
			lastMsg = msg
		}
	}

	// 2. 权限范围：读企业根的直属子部门
	sampleRoot := "" // 用来探测名称的部门；空表示没有能读的部门
	var sampleItems []feishuDept
	rootItems, err := f.probeDepts(ctx, feishuRootDepartmentID)
	if err == nil {
		reads++
		scope.Status = CheckOK
		scope.Detail = T("权限范围是全部成员。", "The scope covers all members.")
		sampleRoot, sampleItems = feishuRootDepartmentID, rootItems
		if rootID != feishuRootDepartmentID {
			if items, err := f.probeDepts(ctx, rootID); err == nil {
				sampleRoot, sampleItems = rootID, items
			}
		}
	} else {
		note(err)
		code, msg, _ := rejectedCode(err)
		ids, serr := f.AuthorizedDepartments(ctx)
		if serr != nil {
			note(serr)
		}
		var roots []Dept
		for _, id := range ids {
			if id == "" || id == feishuRootDepartmentID {
				continue
			}
			d := Dept{ID: id, Name: id}
			if len(roots) < 20 {
				if x, err := f.Department(ctx, id); err == nil && x.Name != "" {
					d.Name = x.Name
				}
			}
			roots = append(roots, d)
		}
		switch {
		case len(roots) > 0:
			scope.Roots = roots
			names := make([]string, 0, 5)
			for _, r := range roots[:min(5, len(roots))] {
				names = append(names, "「"+r.Name+"」")
			}
			listed := strings.Join(names, "、")
			if len(roots) > 5 {
				listed += "…"
			}
			chosen := rootID != feishuRootDepartmentID && containsStr(ids, rootID)
			if chosen {
				scope.Status = CheckOK
				scope.Detail = rawText("权限范围只包含 %d 个部门（%s），同步范围已从中选定。", "The scope covers only %d departments (%s); the sync roots have been chosen from them.", len(roots), listed)
				sampleRoot = rootID
			} else {
				scope.Status = CheckTodo
				scope.Detail = rawText("权限范围只包含 %d 个部门：%s。读企业根部门时飞书说：%s。", "The scope covers only %d departments: %s. Reading the company root, Feishu said: %s.", len(roots), listed, msg)
				scope.Fix = T("在「权限管理 → 通讯录权限范围」改成全部成员，或者在下一步从这些部门里选同步范围。", "Under “Permissions → Contact scope” switch to all members, or pick the sync roots from these departments in the next step.")
				scope.FixURL = authURL
				sampleRoot = roots[0].ID
			}
			if items, err := f.probeDepts(ctx, sampleRoot); err == nil {
				sampleItems = items
			} else {
				note(err)
			}
		case code == feishuNoDeptAuthority || serr == nil:
			scope.Status = CheckBlocked
			scope.Detail = rawText("权限范围里没有任何部门。飞书说：%s。", "The scope contains no department. Feishu said: %s.", msg)
			scope.Fix = T("在「权限管理 → 通讯录权限范围」里加入要同步的部门，或者改成全部成员。", "Under “Permissions → Contact scope” add the departments to sync, or switch to all members.")
			scope.FixURL = authURL
		}
	}

	// 3. 部门名称
	if sampleRoot != "" {
		nameless := 0
		for _, d := range sampleItems {
			if strings.TrimSpace(d.Name) == "" {
				nameless++
			}
		}
		switch {
		case len(sampleItems) == 0 && sampleRoot != feishuRootDepartmentID:
			if d, err := f.Department(ctx, sampleRoot); err == nil {
				if strings.TrimSpace(d.Name) != "" {
					deptNames.Status = CheckOK
					deptNames.Detail = rawText("部门名称能读到（如「%s」）。", "Department names are readable (e.g. “%s”).", d.Name)
				} else {
					nameless, sampleItems = 1, []feishuDept{{OpenDepartmentID: sampleRoot}}
				}
			} else {
				note(err)
			}
		case len(sampleItems) == 0:
			deptNames.Status = CheckSkipped
			deptNames.Detail = T("企业根部门下没有子部门，无法判断。", "The company root has no sub-departments, so this cannot be checked.")
		}
		if len(sampleItems) > 0 {
			if nameless == len(sampleItems) {
				deptNames.Status = CheckBlocked
				deptNames.Detail = rawText("读到了 %d 个部门，但都没有名称。缺少的权限：获取部门基础信息（contact:department.base:readonly）。", "Read %d departments, but none has a name. Missing permission: department basic info (contact:department.base:readonly).", len(sampleItems))
				deptNames.Fix = T("在「权限管理」搜索并开通「获取部门基础信息」，然后发布版本。", "Under “Permissions” search for and enable “Get department basic info”, then release a version.")
				deptNames.FixURL = authURL
			} else if deptNames.Status == "" {
				deptNames.Status = CheckOK
				deptNames.Detail = rawText("部门名称能读到（如「%s」）。", "Department names are readable (e.g. “%s”).", firstNamed(sampleItems))
			}
		}

		// 4/5. 人员姓名与邮箱：从能读的部门里找一个有直属人员的
		candidates := []string{sampleRoot}
		for _, d := range sampleItems {
			if id := d.toDept().ID; id != "" && len(candidates) < 4 {
				candidates = append(candidates, id)
			}
		}
		var users []feishuUser
		var userErr error
		for _, id := range candidates {
			items, err := f.probeUsers(ctx, id)
			if err != nil {
				note(err)
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
			nameless, mailless, deptless := 0, 0, 0
			for _, u := range users {
				if strings.TrimSpace(u.Name) == "" {
					nameless++
				}
				if u.Email == "" && u.EnterpriseEmail == "" {
					mailless++
				}
				if len(u.DepartmentIDs) == 0 {
					deptless++
				}
			}
			if deptless == len(users) {
				userDepts.Status = CheckTodo
				userDepts.Detail = rawText("读到了 %d 个人，都没有所属部门字段。缺少的权限：获取用户组织架构信息（contact:user.department:readonly）。没有它时，一个人只会归到被扫描到的那个部门，兼任多个部门的关系会丢。", "Read %d people, none with department membership. Missing permission: user org structure (contact:user.department:readonly). Without it a person is placed only in the department being scanned; multi-department membership is lost.", len(users))
				userDepts.Fix = T("在「权限管理」搜索并开通「获取用户组织架构信息」，然后发布版本；只按单一部门归属也可以先跳过。", "Under “Permissions” search for and enable “Get user org structure info”, then release a version; skip it if single-department membership is enough.")
				userDepts.FixURL = authURL
			} else {
				userDepts.Status = CheckOK
				userDepts.Detail = T("人员的所属部门能读到。", "People's department membership is readable.")
			}
			if nameless == len(users) {
				userNames.Status = CheckBlocked
				userNames.Detail = rawText("读到了 %d 个人，但都没有姓名。缺少的权限：获取用户基本信息（contact:user.base:readonly）。", "Read %d people, but none has a name. Missing permission: user basic info (contact:user.base:readonly).", len(users))
				userNames.Fix = T("在「权限管理」搜索并开通「获取用户基本信息」，然后发布版本。", "Under “Permissions” search for and enable “Get user basic info”, then release a version.")
				userNames.FixURL = authURL
			} else {
				userNames.Status = CheckOK
				userNames.Detail = T("人员姓名能读到。", "People's names are readable.")
			}
			if mailless == len(users) {
				userEmails.Status = CheckTodo
				userEmails.Detail = rawText("读到了 %d 个人，都没有邮箱。缺少的权限：获取用户邮箱信息（contact:user.email:readonly）。同步进来的人要靠邀请链接激活。", "Read %d people, none with an email. Missing permission: user email (contact:user.email:readonly). Synced people will have to activate through invitation links.", len(users))
				userEmails.Fix = T("在「权限管理」搜索并开通「获取用户邮箱信息」，然后发布版本；也可以先不开，同步后手工发邀请链接。", "Under “Permissions” search for and enable “Get user email”, then release a version; or leave it and hand out invitation links manually after syncing.")
				userEmails.FixURL = authURL
			} else {
				userEmails.Status = CheckOK
				userEmails.Detail = T("邮箱能读到，同步时会自动生成邀请。", "Emails are readable; invitations are generated on sync.")
			}
		case userErr != nil:
			_, msg, _ := rejectedCode(userErr)
			userNames.Status, userEmails.Status, userDepts.Status = CheckSkipped, CheckSkipped, CheckSkipped
			userDepts.Detail = rawText("读人员时飞书说：%s。", "Reading people, Feishu said: %s.", msg)
			userNames.Detail = rawText("读人员时飞书说：%s。", "Reading people, Feishu said: %s.", msg)
			userEmails.Detail = userNames.Detail
		default:
			userNames.Status, userEmails.Status, userDepts.Status = CheckSkipped, CheckSkipped, CheckSkipped
			userNames.Detail = T("这些部门里没有直属人员，无法判断。", "These departments have no direct members, so this cannot be checked.")
			userDepts.Detail = userNames.Detail
			userEmails.Detail = userNames.Detail
		}
	}

	// 6. 版本已发布：所有读接口都以"没权限"失败 → 最可能是没发布版本
	switch {
	case reads > 0 && permissionFails == reads:
		published.Status = CheckBlocked
		published.Detail = rawText("所有读接口都被拒绝：%s。通常是应用还没发布版本，或者刚开通的权限还没随新版本发布。", "Every read was refused: %s. Usually the app has no released version yet, or newly granted permissions have not been released.", lastMsg)
		published.Fix = T("在「版本管理与发布」创建版本并发布，等管理员审核通过。", "Under “Version management & release” create a version and release it, then wait for the admin's approval.")
		published.FixURL = feishuConsoleURL(f.appID, "version")
		for _, c := range checks[1:5] {
			if c.Status == "" || c.Status == CheckBlocked {
				c.Status, c.Detail, c.Fix, c.FixURL = CheckSkipped, T("先发布版本再检查。", "Release a version first."), nil, ""
			}
		}
	case reads > permissionFails:
		published.Status = CheckOK
		published.Detail = T("接口能正常返回数据，应用已发布。", "The API returns data, so the app is released.")
	}
	return finishChecks(checks), nil
}

func firstNamed(items []feishuDept) string {
	for _, d := range items {
		if n := strings.TrimSpace(d.Name); n != "" {
			return n
		}
	}
	return ""
}
