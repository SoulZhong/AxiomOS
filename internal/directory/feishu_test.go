package directory

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 飞书客户端：令牌、分页部门树、人员映射、拒绝与连不上。
func TestFeishu(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["app_id"] != "cli_1" || body["app_secret"] != "good" {
				json.NewEncoder(w).Encode(map[string]any{"code": 10003, "msg": "invalid app_secret"})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "ok", "tenant_access_token": "t-1", "expire": 7200})
		case r.Header.Get("Authorization") != "Bearer t-1":
			json.NewEncoder(w).Encode(map[string]any{"code": 99991663, "msg": "token invalid"})
		case r.URL.Path == "/open-apis/contact/v3/departments/0":
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"department": map[string]any{"name": "示例企业", "open_department_id": "0", "parent_department_id": ""}}})
		case r.URL.Path == "/open-apis/contact/v3/departments/0/children":
			if q.Get("fetch_child") != "true" || q.Get("department_id_type") != "open_department_id" {
				t.Errorf("参数不符 %v", q)
			}
			if q.Get("page_token") == "" {
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"has_more": true, "page_token": "p2", "items": []map[string]any{
					{"name": "产品部", "open_department_id": "od-a", "parent_department_id": "0"},
				}}})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"has_more": false, "items": []map[string]any{
				{"name": "研发组", "open_department_id": "od-b", "parent_department_id": "od-a"},
				{"name": "已删", "open_department_id": "od-x", "parent_department_id": "od-a", "status": map[string]any{"is_deleted": true}},
			}}})
		case r.URL.Path == "/open-apis/contact/v3/users/find_by_department":
			if q.Get("department_id") != "od-b" {
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"has_more": false, "items": []any{}}})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"has_more": false, "items": []map[string]any{
				{"open_id": "ou-1", "name": "小李", "email": "li@x.com", "enterprise_email": "Li@Corp.com", "mobile": "+8613800000000", "department_ids": []string{"od-b"}, "status": map[string]any{}},
				{"open_id": "ou-2", "name": "已离职", "department_ids": []string{"od-b"}, "status": map[string]any{"is_resigned": true}},
				{"open_id": "ou-3", "name": "冻结", "department_ids": []string{"od-b"}, "status": map[string]any{"is_frozen": true}},
			}}})
		case r.URL.Path == "/open-apis/tenant/v2/tenant/query":
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"tenant": map[string]any{"name": "示例企业"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	newClient := func(secret string) *Feishu {
		c, err := NewFeishu(map[string]string{"app_id": "cli_1", "app_secret": secret}, Options{HTTPClient: srv.Client()})
		if err != nil {
			t.Fatal(err)
		}
		c.BaseURL = srv.URL
		return c
	}
	var rj *RejectedError
	if _, err := newClient("wrong").Token(ctx); !errors.As(err, &rj) || rj.Code != 10003 || !strings.Contains(rj.Msg, "invalid app_secret") {
		t.Fatalf("坏凭据应为 RejectedError 并带原话 %v", err)
	}
	c := newClient("good")
	if tok, err := c.Token(ctx); err != nil || tok != "t-1" {
		t.Fatalf("取令牌失败 %q %v", tok, err)
	}
	root, err := c.Department(ctx, "0")
	if err != nil || root.Name != "示例企业" {
		t.Fatalf("根部门不符 %+v %v", root, err)
	}
	ds, err := c.Departments(ctx, "0")
	if err != nil || len(ds) != 3 || ds[0].ID != "od-a" || ds[1].ParentID != "od-a" || !ds[2].Deleted {
		t.Fatalf("分页部门树不符 %+v %v", ds, err)
	}
	us, err := c.Users(ctx, "od-b")
	if err != nil || len(us) != 3 {
		t.Fatalf("人员不符 %+v %v", us, err)
	}
	if us[0].Email != "li@corp.com" || !us[0].Active || us[0].Mobile == "" || us[1].Active || us[2].Active {
		t.Fatalf("人员映射不符 %+v", us)
	}
	if name, err := c.TenantName(ctx); err != nil || name != "示例企业" {
		t.Fatalf("企业名不符 %q %v", name, err)
	}
	srv.Close()
	var un *UnreachableError
	if _, err := newClient("good").Token(ctx); !errors.As(err, &un) {
		t.Fatalf("服务不可达应为 UnreachableError %v", err)
	}
}
