package directory

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// 企业微信客户端：令牌（缓存、过期重取）、部门树、人员映射、拒绝与连不上。
func TestWeCom(t *testing.T) {
	var tokens int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			if q.Get("corpid") != "ww1" || q.Get("corpsecret") != "good" {
				json.NewEncoder(w).Encode(map[string]any{"errcode": 40013, "errmsg": "invalid corpid, hint: [1700000000_1_abc]"})
				return
			}
			n := atomic.AddInt32(&tokens, 1)
			json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "errmsg": "ok", "access_token": "tok" + string(rune('0'+n)), "expires_in": 7200})
			return
		}
		if q.Get("access_token") == "tok1" && q.Get("expired") == "" && r.URL.Path == "/cgi-bin/user/list" && q.Get("department_id") == "3" {
			// 模拟令牌在中途失效一次：客户端应重取后再试
			json.NewEncoder(w).Encode(map[string]any{"errcode": 42001, "errmsg": "access_token expired"})
			return
		}
		if !strings.HasPrefix(q.Get("access_token"), "tok") {
			json.NewEncoder(w).Encode(map[string]any{"errcode": 40014, "errmsg": "invalid access_token"})
			return
		}
		switch r.URL.Path {
		case "/cgi-bin/department/list":
			all := []map[string]any{
				{"id": 1, "name": "示例企业", "parentid": 0, "order": 1},
				{"id": 2, "name": "产品部", "parentid": 1, "order": 2},
				{"id": 3, "name": "研发组", "parentid": 2, "order": 3},
				{"id": 4, "name": "行政部", "parentid": 1, "order": 4},
			}
			id := q.Get("id")
			out := []map[string]any{}
			for _, d := range all {
				// 简化：id=2 时返回 2 及其后代 3
				switch id {
				case "2":
					if d["id"] == 2 || d["id"] == 3 {
						out = append(out, d)
					}
				case "9":
					// 不存在的部门
				default:
					out = append(out, d)
				}
			}
			if id == "9" {
				json.NewEncoder(w).Encode(map[string]any{"errcode": 60003, "errmsg": "department not found"})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "errmsg": "ok", "department": out})
		case "/cgi-bin/user/list":
			if q.Get("fetch_child") != "0" {
				t.Errorf("直属人员应 fetch_child=0，实际 %s", q.Get("fetch_child"))
			}
			users := map[string][]map[string]any{
				"3": {
					{"userid": "zhangsan", "name": "张三", "mobile": "13800000000", "email": "zs@x.com", "biz_mail": "ZhangSan@Corp.com", "department": []int{3, 4}, "status": 1},
					{"userid": "lisi", "name": "李四", "department": []int{3}, "status": 4},
					{"userid": "wangwu", "name": "王五", "email": "ww@x.com", "department": []int{3}, "status": 2},
					{"userid": "zhaoliu", "name": "赵六", "department": []int{3}, "status": 5},
					{"userid": "noname", "department": []int{3}, "status": 1},
				},
			}
			json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "errmsg": "ok", "userlist": users[q.Get("department_id")]})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	newClient := func(secret string) *WeCom {
		c, err := NewWeCom(map[string]string{"corp_id": "ww1", "corp_secret": secret}, Options{HTTPClient: srv.Client()})
		if err != nil {
			t.Fatal(err)
		}
		c.BaseURL = srv.URL
		return c
	}

	// 凭据错：提供方原话进 RejectedError
	bad := newClient("wrong")
	_, err := bad.Token(ctx)
	var rj *RejectedError
	if !errors.As(err, &rj) || rj.Code != 40013 || !strings.Contains(rj.Msg, "invalid corpid") {
		t.Fatalf("坏凭据应为 RejectedError 并带原话，实际 %v", err)
	}

	c := newClient("good")
	tok, err := c.Token(ctx)
	if err != nil || tok != "tok1" {
		t.Fatalf("取令牌失败 %q %v", tok, err)
	}
	if tok2, _ := c.Token(ctx); tok2 != "tok1" || atomic.LoadInt32(&tokens) != 1 {
		t.Fatal("未过期的令牌应复用")
	}

	// 根部门：department/list 返回自身及后代，取自身
	root, err := c.Department(ctx, "1")
	if err != nil || root.Name != "示例企业" || root.ParentID != "0" {
		t.Fatalf("根部门不符 %+v %v", root, err)
	}
	ds, err := c.Departments(ctx, "1")
	if err != nil || len(ds) != 3 || ds[0].ID != "2" || ds[0].ParentID != "1" || ds[1].ID != "3" || ds[1].ParentID != "2" {
		t.Fatalf("部门树不符 %+v %v", ds, err)
	}
	sub, err := c.Departments(ctx, "2")
	if err != nil || len(sub) != 1 || sub[0].ID != "3" {
		t.Fatalf("子树不符 %+v %v", sub, err)
	}
	if _, err := c.Department(ctx, "9"); !errors.As(err, &rj) || rj.Code != 60003 {
		t.Fatalf("不存在的部门应被拒 %v", err)
	}

	// 人员：状态映射、邮箱取企业邮箱并小写、部门编号转字符串、无姓名用 userid 顶着；期间令牌失效一次并自动重取
	us, err := c.Users(ctx, "3")
	if err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&tokens) != 2 {
		t.Fatalf("令牌失效后应重取一次，实际取了 %d 次", tokens)
	}
	if len(us) != 5 {
		t.Fatalf("应有 5 人，实际 %d", len(us))
	}
	byID := map[string]User{}
	for _, u := range us {
		byID[u.ID] = u
	}
	zs := byID["zhangsan"]
	if zs.Name != "张三" || zs.Email != "zhangsan@corp.com" || zs.Mobile != "13800000000" || !zs.Active || len(zs.DeptIDs) != 2 || zs.DeptIDs[1] != "4" {
		t.Fatalf("张三映射不符 %+v", zs)
	}
	if !byID["lisi"].Active {
		t.Fatal("未激活（4）应算在职")
	}
	if byID["wangwu"].Active || byID["wangwu"].Email != "ww@x.com" {
		t.Fatalf("已禁用（2）应算不在职 %+v", byID["wangwu"])
	}
	if byID["zhaoliu"].Active {
		t.Fatal("退出企业（5）应算不在职")
	}
	if byID["noname"].Name != "noname" {
		t.Fatalf("没有姓名权限时应用 userid 顶着 %+v", byID["noname"])
	}
	if _, ok := any(c).(TenantNamer); ok {
		t.Fatal("企业微信没有读企业名的能力，不应实现 TenantNamer")
	}

	// 连不上
	srv.Close()
	_, err = newClient("good").Token(ctx)
	var un *UnreachableError
	if !errors.As(err, &un) {
		t.Fatalf("服务不可达应为 UnreachableError，实际 %v", err)
	}
}
