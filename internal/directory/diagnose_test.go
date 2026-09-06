package directory

import (
	"context"

	"encoding/json"
	"github.com/teemo/axiomos/internal/i18n"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func statusOf(cs []Check, key string) (Check, bool) {
	for _, c := range cs {
		if c.Key == key {
			return c, true
		}
	}
	return Check{}, false
}

// 飞书接入检查：四种真实遇到过的形态。
//   - partial：权限范围只含部分部门（根被 40004 拒绝、scopes 列出部门），部门与人员读得到但没有名称、没有邮箱
//   - full：全部成员、字段齐全
//   - badcreds：凭据被拒
//   - unpublished：令牌能取，但所有读接口都报 99991672
func TestFeishuDiagnose(t *testing.T) {
	mode := "partial"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enc := json.NewEncoder(w)
		q := r.URL.Query()
		if r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal" {
			if mode == "badcreds" {
				enc.Encode(map[string]any{"code": 10003, "msg": "invalid app_secret"})
				return
			}
			enc.Encode(map[string]any{"code": 0, "tenant_access_token": "t", "expire": 7200})
			return
		}
		if mode == "unpublished" {
			enc.Encode(map[string]any{"code": 99991672, "msg": "Access denied. You do not have permission"})
			return
		}
		name := func(s string) string {
			if mode == "partial" {
				return ""
			}
			return s
		}
		switch {
		case r.URL.Path == "/open-apis/contact/v3/departments/0/children":
			if mode == "partial" {
				enc.Encode(map[string]any{"code": 40004, "msg": "no dept authority error"})
				return
			}
			enc.Encode(map[string]any{"code": 0, "data": map[string]any{"items": []map[string]any{{"name": "产品部", "open_department_id": "od-a", "parent_department_id": "0"}}}})
		case r.URL.Path == "/open-apis/contact/v3/scopes":
			enc.Encode(map[string]any{"code": 0, "data": map[string]any{"department_ids": []string{"od-a", "od-b"}, "has_more": false}})
		case strings.HasPrefix(r.URL.Path, "/open-apis/contact/v3/departments/od-") && strings.HasSuffix(r.URL.Path, "/children"):
			enc.Encode(map[string]any{"code": 0, "data": map[string]any{"items": []map[string]any{
				{"name": name("研发组"), "open_department_id": "od-a1", "parent_department_id": "od-a"},
				{"name": name("测试组"), "open_department_id": "od-a2", "parent_department_id": "od-a"},
			}}})
		case strings.HasPrefix(r.URL.Path, "/open-apis/contact/v3/departments/od-"):
			id := strings.TrimPrefix(r.URL.Path, "/open-apis/contact/v3/departments/")
			enc.Encode(map[string]any{"code": 0, "data": map[string]any{"department": map[string]any{"name": name("部门" + id), "open_department_id": id}}})
		case r.URL.Path == "/open-apis/contact/v3/users/find_by_department":
			if q.Get("department_id") == "0" {
				enc.Encode(map[string]any{"code": 0, "data": map[string]any{"items": []any{}}})
				return
			}
			u := map[string]any{"open_id": "ou-1", "name": name("小李"), "department_ids": []string{q.Get("department_id")}}
			if mode == "full" {
				u["enterprise_email"] = "li@corp.com"
			}
			enc.Encode(map[string]any{"code": 0, "data": map[string]any{"items": []map[string]any{u}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	newClient := func() *Feishu {
		c, _ := NewFeishu(map[string]string{"app_id": "cli_1", "app_secret": "s"}, Options{HTTPClient: srv.Client()})
		c.BaseURL = srv.URL
		return c
	}
	keys := func(cs []Check) string {
		var ks []string
		for _, c := range cs {
			ks = append(ks, c.Key+"="+string(c.Status))
		}
		return strings.Join(ks, " ")
	}

	// partial
	cs, err := newClient().Diagnose(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := keys(cs); got != "credentials=ok scope=todo dept_names=blocked user_names=blocked user_emails=todo user_departments=ok published=ok" {
		t.Fatalf("部分权限范围的形态不符: %s", got)
	}
	scope, _ := statusOf(cs, "scope")
	if len(scope.Roots) != 2 || scope.Roots[0].ID != "od-a" || scope.Roots[0].Name != "od-a" || scope.Blocking || !strings.Contains(scope.Detail[ZhCN()], "只包含 2 个部门") || !strings.Contains(scope.Fix[ZhCN()], "通讯录权限范围") || scope.FixURL != "https://open.feishu.cn/app/cli_1/auth" {
		t.Fatalf("权限范围检查不符 %+v", scope)
	}
	dn, _ := statusOf(cs, "dept_names")
	if !dn.Blocking || !strings.Contains(dn.Detail[ZhCN()], "contact:department.base:readonly") || strings.Contains(dn.Fix[ZhCN()], "readonly") || !strings.Contains(dn.Fix[ZhCN()], "获取部门基础信息") {
		t.Fatalf("部门名称检查不符 %+v", dn)
	}
	ue, _ := statusOf(cs, "user_emails")
	if ue.Blocking || !strings.Contains(ue.Detail[ZhCN()], "邀请链接") {
		t.Fatalf("邮箱检查应为待处理且提到邀请链接 %+v", ue)
	}
	// 已把授权部门选成同步根：权限范围通过
	cs, _ = newClient().Diagnose(ctx, "od-b")
	if scope, _ := statusOf(cs, "scope"); scope.Status != CheckOK || len(scope.Roots) != 2 {
		t.Fatalf("选了授权部门后权限范围应通过并仍列出部门 %+v", scope)
	}

	mode = "full"
	cs, _ = newClient().Diagnose(ctx, "")
	if got := keys(cs); got != "credentials=ok scope=ok dept_names=ok user_names=ok user_emails=ok user_departments=ok published=ok" || FirstBlocked(cs) != nil {
		t.Fatalf("全部成员且字段齐全应全绿: %s", got)
	}
	if dn, _ := statusOf(cs, "dept_names"); !strings.Contains(dn.Detail[ZhCN()], "产品部") {
		t.Fatalf("通过时应举例部门名 %+v", dn)
	}

	mode = "badcreds"
	cs, _ = newClient().Diagnose(ctx, "")
	if got := keys(cs); got != "credentials=blocked scope=skipped dept_names=skipped user_names=skipped user_emails=skipped user_departments=skipped published=skipped" {
		t.Fatalf("凭据被拒的形态不符: %s", got)
	}
	if c, _ := statusOf(cs, "credentials"); !strings.Contains(c.Detail[ZhCN()], "invalid app_secret") || c.FixURL != "https://open.feishu.cn/app/cli_1/baseinfo" || !c.Blocking {
		t.Fatalf("凭据检查不符 %+v", c)
	}

	mode = "unpublished"
	cs, _ = newClient().Diagnose(ctx, "")
	if got := keys(cs); got != "credentials=ok scope=skipped dept_names=skipped user_names=skipped user_emails=skipped user_departments=skipped published=blocked" {
		t.Fatalf("未发布的形态不符: %s", got)
	}
	if c, _ := statusOf(cs, "published"); c.FixURL != "https://open.feishu.cn/app/cli_1/version" || !strings.Contains(c.Fix[ZhCN()], "版本管理与发布") {
		t.Fatalf("版本检查不符 %+v", c)
	}
	// 连不上：返回错误而不是检查项
	srv.Close()
	if _, err := newClient().Diagnose(ctx, ""); err == nil {
		t.Fatal("连不上应返回错误")
	}
	if p, _ := Lookup("feishu"); p.ConsoleURL(map[string]string{"app_id": "cli_1"}) != "https://open.feishu.cn/app/cli_1/baseinfo" {
		t.Fatal("控制台首页不符")
	}
}

// 企业微信接入检查：可信 IP（60020，从原话里抠出 IP）、没有敏感字段权限（人没有姓名）、全通。
func TestWeComDiagnose(t *testing.T) {
	mode := "ip"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enc := json.NewEncoder(w)
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			if mode == "badcreds" {
				enc.Encode(map[string]any{"errcode": 40013, "errmsg": "invalid corpid"})
				return
			}
			enc.Encode(map[string]any{"errcode": 0, "access_token": "tok", "expires_in": 7200})
		case "/cgi-bin/department/list":
			if mode == "ip" {
				enc.Encode(map[string]any{"errcode": 60020, "errmsg": "not allow to access from your ip, hint: [1700000000_1_abc], from ip: 203.0.113.9, more info at https://open.work.weixin.qq.com/devtool/query?e=60020"})
				return
			}
			enc.Encode(map[string]any{"errcode": 0, "department": []map[string]any{{"id": 1, "name": "示例企业", "parentid": 0}, {"id": 2, "name": "产品部", "parentid": 1}}})
		case "/cgi-bin/user/list":
			u := map[string]any{"userid": "zhangsan", "department": []int{2}, "status": 1}
			if mode == "full" {
				u["name"] = "张三"
			}
			enc.Encode(map[string]any{"errcode": 0, "userlist": []map[string]any{u}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	newClient := func() *WeCom {
		c, _ := NewWeCom(map[string]string{"corp_id": "ww1", "corp_secret": "s"}, Options{HTTPClient: srv.Client()})
		c.BaseURL = srv.URL
		return c
	}
	cs, err := newClient().Diagnose(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	ip, _ := statusOf(cs, "trusted_ip")
	if ip.Status != CheckBlocked || !strings.Contains(ip.Detail[ZhCN()], "203.0.113.9") || !strings.Contains(ip.Fix[ZhCN()], "203.0.113.9") || !strings.Contains(ip.Fix[ZhCN()], "通讯录同步") {
		t.Fatalf("可信 IP 检查不符 %+v", ip)
	}
	if st, _ := statusOf(cs, "structure"); st.Status != CheckSkipped {
		t.Fatalf("IP 被挡时部门树应跳过 %+v", st)
	}

	mode = "nameless"
	cs, _ = newClient().Diagnose(ctx, "")
	if n, _ := statusOf(cs, "names"); n.Status != CheckBlocked || !strings.Contains(n.Fix[ZhCN()], "敏感字段") {
		t.Fatalf("没有姓名应阻塞 %+v", n)
	}
	if st, _ := statusOf(cs, "structure"); st.Status != CheckOK || !strings.Contains(st.Detail[ZhCN()], "示例企业") {
		t.Fatalf("部门树应通过 %+v", st)
	}

	mode = "full"
	cs, _ = newClient().Diagnose(ctx, "")
	if FirstBlocked(cs) != nil || len(cs) != 4 {
		t.Fatalf("全通形态不符 %+v", cs)
	}

	mode = "badcreds"
	cs, _ = newClient().Diagnose(ctx, "")
	if c, _ := statusOf(cs, "credentials"); c.Status != CheckBlocked || c.FixURL != "https://work.weixin.qq.com/wework_admin/frame#profile" {
		t.Fatalf("凭据检查不符 %+v", c)
	}
}

// ZhCN 是 i18n.ZhCN 的简写。
func ZhCN() i18n.Locale { return i18n.ZhCN }
