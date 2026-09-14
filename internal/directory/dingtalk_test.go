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
	"time"
)

// 钉钉客户端：令牌（缓存、失效重取）、逐层读部门树、按 cursor 翻页读人员、字段映射、拒绝与连不上。
func TestDingTalk(t *testing.T) {
	var tokens int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		enc := json.NewEncoder(w)
		if r.URL.Path == "/gettoken" {
			if q.Get("appkey") != "ding1" || q.Get("appsecret") != "good" {
				enc.Encode(map[string]any{"errcode": 40089, "errmsg": "不合法的corpid或corpsecret"})
				return
			}
			n := atomic.AddInt32(&tokens, 1)
			enc.Encode(map[string]any{"errcode": 0, "errmsg": "ok", "access_token": "tok" + string(rune('0'+n)), "expires_in": 7200})
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if q.Get("access_token") == "tok1" && r.URL.Path == "/topapi/v2/user/list" {
			// 模拟令牌中途失效一次：客户端应重取后再试
			enc.Encode(map[string]any{"errcode": 42001, "errmsg": "access_token已过期"})
			return
		}
		if !strings.HasPrefix(q.Get("access_token"), "tok") {
			enc.Encode(map[string]any{"errcode": 40014, "errmsg": "不合法的access_token"})
			return
		}
		deptID, _ := body["dept_id"].(float64)
		switch r.URL.Path {
		case "/topapi/v2/department/get":
			all := map[float64]map[string]any{1: {"dept_id": 1, "name": "示例企业", "parent_id": 0}, 2: {"dept_id": 2, "name": "产品部", "parent_id": 1}}
			d, ok := all[deptID]
			if !ok {
				enc.Encode(map[string]any{"errcode": 60003, "errmsg": "部门不存在"})
				return
			}
			enc.Encode(map[string]any{"errcode": 0, "result": d})
		case "/topapi/v2/department/listsub":
			if r.Method != http.MethodPost {
				t.Errorf("listsub 应 POST")
			}
			subs := map[float64][]map[string]any{
				1: {{"dept_id": 2, "name": "产品部", "parent_id": 1}, {"dept_id": 4, "name": "行政部", "parent_id": 1}},
				2: {{"dept_id": 3, "name": "研发组", "parent_id": 2}},
			}
			enc.Encode(map[string]any{"errcode": 0, "result": subs[deptID]})
		case "/topapi/v2/user/list":
			if deptID != 3 {
				enc.Encode(map[string]any{"errcode": 0, "result": map[string]any{"has_more": false, "list": []any{}}})
				return
			}
			cursor, _ := body["cursor"].(float64)
			if size, _ := body["size"].(float64); size != 100 {
				t.Errorf("每页应 100，实际 %v", size)
			}
			if cursor == 0 {
				enc.Encode(map[string]any{"errcode": 0, "result": map[string]any{"has_more": true, "next_cursor": 100, "list": []map[string]any{
					{"userid": "zhangsan", "unionid": "u-zs", "name": "张三", "mobile": "13800000000", "email": "zs@x.com", "org_email": "ZhangSan@Corp.com", "active": true, "dept_id_list": []int{3, 4}},
					{"userid": "lisi", "name": "李四", "active": false, "dept_id_list": []int{3}},
				}}})
				return
			}
			enc.Encode(map[string]any{"errcode": 0, "result": map[string]any{"has_more": false, "list": []map[string]any{
				{"userid": "noname", "active": true, "dept_id_list": []int{3}},
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	newClient := func(secret string) *DingTalk {
		c, err := NewDingTalk(map[string]string{"app_key": "ding1", "app_secret": secret}, Options{HTTPClient: srv.Client()})
		if err != nil {
			t.Fatal(err)
		}
		c.BaseURL = srv.URL
		return c
	}

	var rj *RejectedError
	if _, err := newClient("wrong").Token(ctx); !errors.As(err, &rj) || rj.Code != 40089 || !strings.Contains(rj.Msg, "不合法") {
		t.Fatalf("坏凭据应为 RejectedError 并带原话，实际 %v", err)
	}
	c := newClient("good")
	if tok, err := c.Token(ctx); err != nil || tok != "tok1" {
		t.Fatalf("取令牌失败 %q %v", tok, err)
	}
	if tok2, _ := c.Token(ctx); tok2 != "tok1" || atomic.LoadInt32(&tokens) != 1 {
		t.Fatal("未过期的令牌应复用")
	}
	root, err := c.Department(ctx, "")
	if err != nil || root.ID != "1" || root.Name != "示例企业" || root.ParentID != "0" {
		t.Fatalf("根部门不符 %+v %v", root, err)
	}
	if _, err := c.Department(ctx, "9"); !errors.As(err, &rj) || rj.Code != 60003 {
		t.Fatalf("不存在的部门应被拒 %v", err)
	}
	if _, err := c.Department(ctx, "od-x"); !errors.As(err, &rj) {
		t.Fatalf("非数字编号应被拒 %v", err)
	}
	ds, err := c.Departments(ctx, "1")
	if err != nil || len(ds) != 3 || ds[0].ID != "2" || ds[1].ID != "4" || ds[2].ID != "3" || ds[2].ParentID != "2" {
		t.Fatalf("部门树应逐层读全 %+v %v", ds, err)
	}
	if sub, err := c.Departments(ctx, "2"); err != nil || len(sub) != 1 || sub[0].ID != "3" {
		t.Fatalf("子树不符 %+v %v", sub, err)
	}

	us, err := c.Users(ctx, "3")
	if err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&tokens) != 2 {
		t.Fatalf("令牌失效后应重取一次，实际取了 %d 次", tokens)
	}
	if len(us) != 3 {
		t.Fatalf("两页应合起来 3 人，实际 %d", len(us))
	}
	zs := us[0]
	if zs.ID != "zhangsan" || zs.Name != "张三" || zs.Email != "zhangsan@corp.com" || zs.Mobile != "13800000000" || !zs.Active || len(zs.DeptIDs) != 2 || zs.DeptIDs[1] != "4" {
		t.Fatalf("张三映射不符 %+v", zs)
	}
	if !us[1].Active {
		t.Fatal("未激活的人仍算在职")
	}
	if us[2].Name != "noname" {
		t.Fatalf("没有姓名时应用 userid 顶着 %+v", us[2])
	}
	if _, ok := any(c).(TenantNamer); ok {
		t.Fatal("钉钉没有读企业名的能力，不应实现 TenantNamer")
	}

	srv.Close()
	var un *UnreachableError
	if _, err := newClient("good").Token(ctx); !errors.As(err, &un) {
		t.Fatalf("服务不可达应为 UnreachableError，实际 %v", err)
	}
}

// 钉钉接入检查：凭据错、出口 IP 不在白名单、权限范围只含部分部门、全部通过但没有手机号邮箱。
func TestDingTalkDiagnose(t *testing.T) {
	mode := "full"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enc := json.NewEncoder(w)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		deptID, _ := body["dept_id"].(float64)
		switch r.URL.Path {
		case "/gettoken":
			switch mode {
			case "badcreds":
				enc.Encode(map[string]any{"errcode": 40089, "errmsg": "不合法的corpid或corpsecret"})
			case "ip":
				enc.Encode(map[string]any{"errcode": 60020, "errmsg": "访问ip不在白名单之中, request ip=203.0.113.9"})
			default:
				enc.Encode(map[string]any{"errcode": 0, "access_token": "t", "expires_in": 7200})
			}
		case "/auth/scopes":
			if mode == "partial" {
				enc.Encode(map[string]any{"errcode": 0, "auth_org_scopes": map[string]any{"authed_dept": []int{2, 5}, "authed_user": []string{}}})
				return
			}
			enc.Encode(map[string]any{"errcode": 0, "auth_org_scopes": map[string]any{"authed_dept": []int{1}, "authed_user": []string{}}})
		case "/topapi/v2/department/listsub":
			if mode == "partial" && deptID == 1 {
				enc.Encode(map[string]any{"errcode": 60011, "errmsg": "无权限访问该部门"})
				return
			}
			enc.Encode(map[string]any{"errcode": 0, "result": []map[string]any{{"dept_id": 2, "name": "产品部", "parent_id": 1}}})
		case "/topapi/v2/department/get":
			names := map[float64]string{2: "产品部", 5: "销售部"}
			enc.Encode(map[string]any{"errcode": 0, "result": map[string]any{"dept_id": deptID, "name": names[deptID], "parent_id": 1}})
		case "/topapi/v2/user/list":
			if deptID != 2 {
				enc.Encode(map[string]any{"errcode": 0, "result": map[string]any{"has_more": false, "list": []any{}}})
				return
			}
			enc.Encode(map[string]any{"errcode": 0, "result": map[string]any{"has_more": false, "list": []map[string]any{
				{"userid": "a", "name": "小李", "active": true, "dept_id_list": []int{2}},
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	d, _ := NewDingTalk(map[string]string{"app_key": "ding1", "app_secret": "s"}, Options{HTTPClient: srv.Client()})
	d.BaseURL = srv.URL

	cs, err := d.Diagnose(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"credentials", "trusted_ip", "scope", "structure", "names"} {
		if c, _ := statusOf(cs, k); c.Status != CheckOK {
			t.Fatalf("全部通过时 %s 应 ok，实际 %+v", k, c)
		}
	}
	if c, _ := statusOf(cs, "contacts"); c.Status != CheckTodo || c.Blocking || !strings.Contains(c.Fix.In("zh-CN"), "个人手机号信息") {
		t.Fatalf("没有手机号邮箱应是待处理且不阻塞 %+v", c)
	}
	if c, _ := statusOf(cs, "scope"); !strings.Contains(c.Detail.In("zh-CN"), "全部员工") {
		t.Fatalf("根部门可读即全部员工 %+v", c)
	}
	if FirstBlocked(cs) != nil {
		t.Fatalf("不应有阻塞项 %+v", cs)
	}

	mode = "partial"
	d.mu.Lock()
	d.token = ""
	d.mu.Unlock()
	cs, err = d.Diagnose(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	sc, _ := statusOf(cs, "scope")
	if sc.Status != CheckTodo || len(sc.Roots) != 2 || sc.Roots[0].Name != "产品部" || sc.Roots[1].Name != "销售部" || !strings.Contains(sc.Fix.In("zh-CN"), "通讯录权限范围") {
		t.Fatalf("范围只含部分部门时应列出可选的根 %+v", sc)
	}
	if roots := ScopeRoots(cs); len(roots) != 2 {
		t.Fatalf("ScopeRoots 应取到 %+v", roots)
	}
	if c, _ := statusOf(cs, "structure"); c.Status != CheckBlocked || !strings.Contains(c.Detail.In("zh-CN"), "无权限访问该部门") {
		t.Fatalf("根部门读不到应阻塞并带原话 %+v", c)
	}
	// 选了被授权的部门当同步根之后：范围通过，人员也从它下面读
	cs, _ = d.Diagnose(ctx, "2")
	if c, _ := statusOf(cs, "scope"); c.Status != CheckOK {
		t.Fatalf("同步根在范围里应通过 %+v", c)
	}
	if c, _ := statusOf(cs, "names"); c.Status != CheckOK {
		t.Fatalf("从被授权部门读到人员 %+v", c)
	}
	// 选了不在范围里的部门
	cs, _ = d.Diagnose(ctx, "7")
	if c, _ := statusOf(cs, "scope"); c.Status != CheckTodo || len(c.Roots) != 2 {
		t.Fatalf("同步根不在范围里应提示重选 %+v", c)
	}

	mode = "ip"
	d.mu.Lock()
	d.token = ""
	d.mu.Unlock()
	cs, _ = d.Diagnose(ctx, "")
	if c, _ := statusOf(cs, "credentials"); c.Status != CheckOK {
		t.Fatalf("IP 不在白名单时凭据算通过 %+v", c)
	}
	if c, _ := statusOf(cs, "trusted_ip"); c.Status != CheckBlocked || !strings.Contains(c.Fix.In("zh-CN"), "203.0.113.9") || !strings.Contains(c.Fix.In("en-US"), "203.0.113.9") {
		t.Fatalf("应指出出网 IP %+v", c)
	}

	mode = "badcreds"
	d.mu.Lock()
	d.token = ""
	d.mu.Unlock()
	cs, _ = d.Diagnose(ctx, "")
	if c, _ := statusOf(cs, "credentials"); c.Status != CheckBlocked || !strings.Contains(c.Detail.In("zh-CN"), "不合法") || c.FixURL == "" {
		t.Fatalf("坏凭据应阻塞并带原话与链接 %+v", c)
	}
	if c, _ := statusOf(cs, "names"); c.Status != CheckSkipped {
		t.Fatalf("后面的检查应未检查 %+v", c)
	}
}

// 钉钉工作通知：action_card 带按钮、没链接发 text、agent_id 缺失算拒绝；自检读应用可见范围。
func TestDingTalkMessaging(t *testing.T) {
	var got map[string]any
	mode := "ok"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		enc := json.NewEncoder(w)
		switch r.URL.Path {
		case "/gettoken":
			if q.Get("appsecret") != "good" {
				enc.Encode(map[string]any{"errcode": 40089, "errmsg": "invalid credential"})
				return
			}
			enc.Encode(map[string]any{"errcode": 0, "access_token": "at-1", "expires_in": 7200})
		case "/microapp/visible_scopes":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if q.Get("access_token") != "at-1" || body["agentId"] != float64(1000002) {
				enc.Encode(map[string]any{"errcode": 60004, "errmsg": "应用不存在"})
				return
			}
			if mode == "ip" {
				enc.Encode(map[string]any{"errcode": 60020, "errmsg": "访问ip不在白名单之中, request ip=203.0.113.9"})
				return
			}
			enc.Encode(map[string]any{"errcode": 0, "result": map[string]any{"isHidden": false, "deptVisibleScopes": []int{2}, "userVisibleScopes": []string{}}})
		case "/topapi/message/corpconversation/asyncsend_v2":
			if r.Method != http.MethodPost || q.Get("access_token") != "at-1" {
				t.Errorf("应带令牌 POST %v", q)
			}
			json.NewDecoder(r.Body).Decode(&got)
			if mode == "noperm" {
				enc.Encode(map[string]any{"errcode": 60011, "errmsg": "无权限"})
				return
			}
			enc.Encode(map[string]any{"errcode": 0, "errmsg": "ok", "task_id": 123})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	d, _ := NewDingTalkMessenger(map[string]string{"app_key": "ding1", "app_secret": "good", "agent_id": "1000002"}, Options{})
	d.BaseURL = srv.URL
	if err := d.SendDirect(ctx, "zhangsan", Message{Title: "待你验收：写文档", Text: "小李「提交」", URL: "https://axiom.example/tasks/t1/", Open: "打开"}); err != nil {
		t.Fatal(err)
	}
	msg, _ := got["msg"].(map[string]any)
	card, _ := msg["action_card"].(map[string]any)
	if got["userid_list"] != "zhangsan" || got["agent_id"] != float64(1000002) || msg["msgtype"] != "action_card" || card["single_url"] != "https://axiom.example/tasks/t1/" || card["single_title"] != "打开" || card["title"] != "待你验收：写文档" || card["markdown"] != "小李「提交」" {
		t.Fatalf("action_card 不对 %+v", got)
	}
	if err := d.SendDirect(ctx, "zhangsan", Message{Title: "只有一句话"}); err != nil {
		t.Fatal(err)
	}
	if msg, _ := got["msg"].(map[string]any); msg["msgtype"] != "text" || !strings.Contains(msg["text"].(map[string]any)["content"].(string), "只有一句话") {
		t.Fatalf("没链接时应发纯文本 %+v", got)
	}
	mode = "noperm"
	var rj *RejectedError
	if err := d.SendDirect(ctx, "zhangsan", Message{Title: "x"}); !errors.As(err, &rj) || rj.Code != 60011 {
		t.Fatalf("发不出去应带原话 %v", err)
	}
	mode = "ok"
	if c := d.DiagnoseMessaging(ctx); c.Status != CheckOK || c.Key != MessagingCheckKey || !strings.Contains(c.Detail.In("zh-CN"), "1 个部门") {
		t.Fatalf("可见范围读到即可发 %+v", c)
	}
	mode = "ip"
	if c := d.DiagnoseMessaging(ctx); c.Status != CheckTodo || c.Blocking || !strings.Contains(c.Fix.In("zh-CN"), "203.0.113.9") {
		t.Fatalf("IP 不在白名单应是待处理并指出 IP %+v", c)
	}
	none, _ := NewDingTalkMessenger(map[string]string{"app_key": "ding1", "app_secret": "good"}, Options{})
	none.BaseURL = srv.URL
	if c := none.DiagnoseMessaging(ctx); c.Status != CheckTodo || !strings.Contains(c.Fix.In("zh-CN"), "AgentId") {
		t.Fatalf("没填 AgentId 应是待处理 %+v", c)
	}
	if err := none.SendDirect(ctx, "zhangsan", Message{Title: "x"}); !errors.As(err, &rj) {
		t.Fatalf("没填 AgentId 发消息应被拒 %v", err)
	}
	wrong, _ := NewDingTalkMessenger(map[string]string{"app_key": "ding1", "app_secret": "good", "agent_id": "42"}, Options{})
	wrong.BaseURL = srv.URL
	if c := wrong.DiagnoseMessaging(ctx); c.Status != CheckTodo || c.Blocking || !strings.Contains(c.Detail.In("zh-CN"), "应用不存在") {
		t.Fatalf("AgentId 错应是待处理并带原话 %+v", c)
	}
}

// 钉钉日历：userid 换 unionid，新接口令牌放请求头、401 重取一次，按 nextToken 翻页，全天与已取消的处理。
func TestDingTalkCalendar(t *testing.T) {
	var tokens int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		enc := json.NewEncoder(w)
		switch {
		case r.URL.Path == "/gettoken":
			n := atomic.AddInt32(&tokens, 1)
			enc.Encode(map[string]any{"errcode": 0, "access_token": "at-" + string(rune('0'+n)), "expires_in": 7200})
		case r.URL.Path == "/topapi/v2/user/get":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			unions := map[any]string{"zhangsan": "u-zs", "lisi": "u-ls"}
			uid, ok := unions[body["userid"]]
			if !ok {
				enc.Encode(map[string]any{"errcode": 60121, "errmsg": "找不到该用户"})
				return
			}
			enc.Encode(map[string]any{"errcode": 0, "result": map[string]any{"userid": body["userid"], "unionid": uid}})
		case r.URL.Path == "/v1.0/calendar/users/u-zs/calendars/primary/events":
			if r.Header.Get("x-acs-dingtalk-access-token") == "at-1" {
				// 第一次用的令牌已失效：客户端应重取后再试
				w.WriteHeader(http.StatusUnauthorized)
				enc.Encode(map[string]any{"code": "InvalidAuthentication", "message": "token expired"})
				return
			}
			if q.Get("timeMin") == "" || q.Get("timeMax") == "" || q.Get("maxResults") != "100" {
				t.Errorf("参数不符 %v", q)
			}
			if q.Get("nextToken") == "" {
				enc.Encode(map[string]any{"nextToken": "p2", "events": []map[string]any{
					{"id": "e1", "summary": "周会", "status": "confirmed", "start": map[string]any{"dateTime": "2026-09-14T10:00:00+08:00"}, "end": map[string]any{"dateTime": "2026-09-14T11:00:00+08:00"}, "onlineMeetingInfo": map[string]any{"url": "https://meeting.example/1"}},
					{"id": "e2", "summary": "已取消", "status": "cancelled", "start": map[string]any{"dateTime": "2026-09-14T12:00:00+08:00"}, "end": map[string]any{"dateTime": "2026-09-14T13:00:00+08:00"}},
				}})
				return
			}
			enc.Encode(map[string]any{"events": []map[string]any{
				{"id": "e3", "summary": "团建", "status": "confirmed", "isAllDay": true, "start": map[string]any{"date": "2026-09-15"}, "end": map[string]any{"date": "2026-09-16"}},
				{"id": "e4", "summary": "区间外", "status": "confirmed", "start": map[string]any{"dateTime": "2026-10-01T10:00:00+08:00"}, "end": map[string]any{"dateTime": "2026-10-01T11:00:00+08:00"}},
			}})
		case strings.HasPrefix(r.URL.Path, "/v1.0/calendar/users/"):
			w.WriteHeader(http.StatusForbidden)
			enc.Encode(map[string]any{"code": "Forbidden.AccessDenied.AccessTokenPermissionDenied", "message": "没有调用该接口的权限"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	d, _ := NewDingTalk(map[string]string{"app_key": "ding1", "app_secret": "good"}, Options{})
	d.BaseURL, d.APIBaseURL = srv.URL, srv.URL
	from := time.Date(2026, 9, 14, 0, 0, 0, 0, time.Local)
	to := from.AddDate(0, 0, 7)
	evs, err := d.Events(ctx, "zhangsan", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&tokens) != 2 {
		t.Fatalf("401 后应重取一次令牌，实际取了 %d 次", tokens)
	}
	if len(evs) != 2 || evs[0].ExternalID != "e1" || evs[0].Title != "周会" || evs[0].URL != "https://meeting.example/1" || evs[0].AllDay || !evs[0].Busy {
		t.Fatalf("会议映射不符 %+v", evs)
	}
	if evs[0].Start.Hour() != 10 || evs[0].End.Sub(evs[0].Start) != time.Hour {
		t.Fatalf("起止不符 %+v", evs[0])
	}
	if !evs[1].AllDay || evs[1].ExternalID != "e3" || evs[1].Start.Day() != 15 {
		t.Fatalf("全天日程不符 %+v", evs[1])
	}
	var rj *RejectedError
	if _, err := d.Events(ctx, "nobody", from, to); !errors.As(err, &rj) || rj.Code != 60121 {
		t.Fatalf("找不到的人应被拒 %v", err)
	}
	// 新接口的拒绝要带 code 与 message 原话
	if _, err := d.Events(ctx, "lisi", from, to); !errors.As(err, &rj) || rj.Code != http.StatusForbidden || !strings.Contains(rj.Msg, "AccessTokenPermissionDenied") || !strings.Contains(rj.Msg, "没有调用该接口的权限") {
		t.Fatalf("新接口拒绝应带原话 %v", err)
	}
}
