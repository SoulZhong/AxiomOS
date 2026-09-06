package directory

import (
	"context"
	"sync"

	"github.com/teemo/axiomos/internal/i18n"
)

// T 是 i18n.T 的简写，只给这个文件的声明用。
var T = i18n.T

// FakeProviderKey 是测试提供方的代码名；它只通过 RegisterForTest 登记，不会出现在生产的提供方列表里。
const FakeProviderKey = "fake"

// fakeRootDepartmentID 是内存目录的根部门编号。
const fakeRootDepartmentID = "0"

// UseFake 把一个内存目录挂成测试提供方 fake：凭据字段 app_id 与 app_secret（保密），根部门 "0"。
func UseFake(f *Fake, goodSecret string) Provider {
	return UseFakeProvider(FakeProviderKey, fakeRootDepartmentID, f, goodSecret,
		CredentialField{Key: "app_id", Title: T("App ID", "App ID")},
		CredentialField{Key: "app_secret", Title: T("App Secret", "App Secret"), Secret: true})
}

// UseFakeProvider 用任意代码名、根部门与字段声明登记一个测试提供方（模拟第二个平台时用）。
// 第一个保密字段的值不等于 goodSecret 时，客户端在取令牌时报凭据被拒（与真实客户端的时机一致）。
func UseFakeProvider(key, root string, f *Fake, goodSecret string, fields ...CredentialField) Provider {
	secretKey := ""
	for _, fd := range fields {
		if fd.Secret {
			secretKey = fd.Key
			break
		}
	}
	p := Provider{
		Key:              key,
		Title:            T("测试目录", "Test directory"),
		RootDepartmentID: root,
		Fields:           fields,
		New: func(creds map[string]string, opts Options) (Directory, error) {
			if _, err := newHTTPClient(opts); err != nil {
				return nil, err
			}
			if secretKey != "" && creds[secretKey] != goodSecret {
				bad := NewFake()
				bad.TokenErr = &RejectedError{Code: 10003, Msg: "invalid app_secret"}
				return bad, nil
			}
			return f, nil
		},
	}
	if key != FakeProviderKey {
		p.Title = T("测试目录二", "Second test directory")
	}
	RegisterForTest(p)
	return p
}

// Fake 是测试用的内存目录：直接摆好部门与人员，改一改再同步一次就能验证幂等与增量。
type Fake struct {
	mu     sync.Mutex
	Depts  []Dept
	People []User
	// TokenErr 非空时 Token 失败（模拟凭据错）。
	TokenErr error
	Tenant   string
	// Checks 非空时 Diagnose 在通用检查之后原样附上它们（模拟某个平台的权限缺项与被授权的部门）。
	Checks []Check
}

// NewFake 创建空目录。
func NewFake() *Fake { return &Fake{Tenant: "测试企业"} }

// Token 返回固定令牌。
func (f *Fake) Token(ctx context.Context) (string, error) {
	if f.TokenErr != nil {
		return "", f.TokenErr
	}
	return "fake-token", nil
}

// TenantName 返回企业名。
func (f *Fake) TenantName(ctx context.Context) (string, error) { return f.Tenant, nil }

// Department 读一个部门；根部门返回企业名。
func (f *Fake) Department(ctx context.Context, id string) (*Dept, error) {
	if f.TokenErr != nil {
		return nil, f.TokenErr
	}
	if id == fakeRootDepartmentID {
		return &Dept{ID: fakeRootDepartmentID, Name: f.Tenant}, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, d := range f.Depts {
		if d.ID == id {
			c := d
			return &c, nil
		}
	}
	return nil, &RejectedError{Code: 40013, Msg: "department not found"}
}

// Departments 返回 rootID 下的全部后代。
func (f *Fake) Departments(ctx context.Context, rootID string) ([]Dept, error) {
	if f.TokenErr != nil {
		return nil, f.TokenErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	children := map[string][]Dept{}
	for _, d := range f.Depts {
		children[d.ParentID] = append(children[d.ParentID], d)
	}
	var out []Dept
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		if depth > 64 {
			return
		}
		for _, c := range children[id] {
			out = append(out, c)
			walk(c.ID, depth+1)
		}
	}
	walk(rootID, 0)
	return out, nil
}

// Users 返回某部门的直属人员。
func (f *Fake) Users(ctx context.Context, deptID string) ([]User, error) {
	if f.TokenErr != nil {
		return nil, f.TokenErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []User
	for _, u := range f.People {
		for _, d := range u.DeptIDs {
			if d == deptID {
				out = append(out, u)
				break
			}
		}
	}
	return out, nil
}

// SetDepts / SetUsers 替换整份数据（模拟外部目录变化）。
func (f *Fake) SetDepts(ds []Dept) { f.mu.Lock(); f.Depts = ds; f.mu.Unlock() }
func (f *Fake) SetUsers(us []User) { f.mu.Lock(); f.People = us; f.mu.Unlock() }

// Diagnose 跑通用检查（凭据、部门树），再附上测试摆好的 Checks。
func (f *Fake) Diagnose(ctx context.Context, rootID string) ([]Check, error) {
	out, err := DiagnoseBasic(ctx, T("测试目录", "Test directory"), f, rootID)
	if err != nil {
		return nil, err
	}
	if FirstBlocked(out) != nil {
		return out, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append(out, f.Checks...), nil
}

// SetChecks 替换附加的检查项。
func (f *Fake) SetChecks(cs []Check) { f.mu.Lock(); f.Checks = cs; f.mu.Unlock() }
