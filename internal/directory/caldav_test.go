package directory

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 一个最小的 CalDAV 假服务器：发现三步 + calendar-query；要账号密码；一本日历装 VEVENT，一本只装 VTODO。
func fakeCalDAV(t *testing.T) *httptest.Server {
	t.Helper()
	ms := func(body string) string {
		return `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">` + body + `</d:multistatus>`
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "zhong" || p != "pw-1" {
			w.WriteHeader(401)
			return
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/xml")
		switch {
		case r.Method == "PROPFIND" && r.URL.Path == "/":
			w.WriteHeader(207)
			_, _ = w.Write([]byte(ms(`<d:response><d:href>/</d:href><d:propstat><d:prop><d:current-user-principal><d:href>/principals/zhong/</d:href></d:current-user-principal></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`)))
		case r.Method == "PROPFIND" && r.URL.Path == "/principals/zhong/":
			w.WriteHeader(207)
			_, _ = w.Write([]byte(ms(`<d:response><d:href>/principals/zhong/</d:href><d:propstat><d:prop><c:calendar-home-set><d:href>/calendars/zhong/</d:href></c:calendar-home-set></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`)))
		case r.Method == "PROPFIND" && r.URL.Path == "/calendars/zhong/":
			if r.Header.Get("Depth") != "1" {
				w.WriteHeader(400)
				return
			}
			w.WriteHeader(207)
			_, _ = w.Write([]byte(ms(`<d:response><d:href>/calendars/zhong/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>` +
				`<d:response><d:href>/calendars/zhong/work/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/><c:calendar/></d:resourcetype><d:displayname>工作</d:displayname><c:supported-calendar-component-set><c:comp name="VEVENT"/></c:supported-calendar-component-set></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>` +
				`<d:response><d:href>/calendars/zhong/todo/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/><c:calendar/></d:resourcetype><d:displayname>待办</d:displayname><c:supported-calendar-component-set><c:comp name="VTODO"/></c:supported-calendar-component-set></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`)))
		case r.Method == "REPORT" && r.URL.Path == "/calendars/zhong/work/":
			if !strings.Contains(string(body), "time-range") {
				w.WriteHeader(400)
				return
			}
			w.WriteHeader(207)
			_, _ = w.Write([]byte(ms(`<d:response><d:href>/calendars/zhong/work/a.ics</d:href><d:propstat><d:prop><d:getetag>"1"</d:getetag><c:calendar-data>BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
UID:a@wecom
SUMMARY:周会 &amp; 评审
DTSTART;TZID=Asia/Shanghai:20260914T100000
DTEND;TZID=Asia/Shanghai:20260914T110000
RRULE:FREQ=WEEKLY;COUNT=3
END:VEVENT
END:VCALENDAR</c:calendar-data></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`)))
		case r.Method == "REPORT" && r.URL.Path == "/calendars/zhong/todo/":
			t.Errorf("只装 VTODO 的日历不该被查询")
			w.WriteHeader(400)
		default:
			w.WriteHeader(404)
		}
	}))
}

func TestCalDAVDiscoverAndQuery(t *testing.T) {
	t.Setenv("AXIOMOS_ALLOW_PRIVATE_EGRESS", "1")
	srv := fakeCalDAV(t)
	defer srv.Close()
	c, err := NewCalDAV(map[string]string{"server": srv.URL, "username": "zhong", "password": "pw-1"}, Options{HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 9, 14, 0, 0, 0, 0, sh())
	evs, err := c.Events(context.Background(), "", from, from.AddDate(0, 0, 30))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 3 || c.CalendarName() != "工作" {
		t.Fatalf("应展开成 3 个实例、日历名「工作」，实际 %d %q", len(evs), c.CalendarName())
	}
	if evs[0].Title != "周会 & 评审" || !strings.HasPrefix(evs[0].ExternalID, "calendars/zhong/work#a@wecom") {
		t.Fatalf("标题应还原 XML 转义、ID 带日历前缀: %+v", evs[0])
	}
	// 账号密码不对 → 401
	bad, _ := NewCalDAV(map[string]string{"server": srv.URL, "username": "zhong", "password": "nope"}, Options{HTTPClient: srv.Client()})
	if _, err := bad.Events(context.Background(), "", from, from.AddDate(0, 0, 7)); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("密码不对应报 401，实际 %v", err)
	}
	// 服务器地址不带协议时补 https；非法地址被拒
	if c2, err := NewCalDAV(map[string]string{"server": "caldav.wecom.work", "username": "u", "password": "p"}, Options{}); err != nil || c2.Server != "https://caldav.wecom.work" {
		t.Fatalf("应补成 https://caldav.wecom.work，实际 %v %v", c2, err)
	}
	if _, err := NewCalDAV(map[string]string{"server": "ftp://x", "username": "u", "password": "p"}, Options{}); err == nil {
		t.Fatal("非 http(s) 应被拒")
	}
}

// 填的是网页地址：回的不是 WebDAV 应答。
func TestCalDAVNotDAV(t *testing.T) {
	t.Setenv("AXIOMOS_ALLOW_PRIVATE_EGRESS", "1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>hi</html>")) }))
	defer srv.Close()
	c, _ := NewCalDAV(map[string]string{"server": srv.URL, "username": "u", "password": "p"}, Options{HTTPClient: srv.Client()})
	if _, err := c.Events(context.Background(), "", time.Now(), time.Now().Add(time.Hour)); err != ErrNotCalDAV {
		t.Fatalf("应报 ErrNotCalDAV，实际 %v", err)
	}
}

// 企业微信那种形状：根路径对谁都 403，/.well-known/caldav 跳转到真正的入口；PROPFIND 跟跳转时不能变成 GET。
func TestCalDAVWellKnownRedirect(t *testing.T) {
	t.Setenv("AXIOMOS_ALLOW_PRIVATE_EGRESS", "1")
	ms := func(b string) string {
		return `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">` + b + `</d:multistatus>`
	}
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		if u, p, ok := r.BasicAuth(); !ok || u != "a" || p != "b" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/":
			w.WriteHeader(403)
		case "/.well-known/caldav":
			w.Header().Set("Location", "/dav/")
			w.WriteHeader(301)
		case "/dav/":
			w.WriteHeader(207)
			_, _ = w.Write([]byte(ms(`<d:response><d:href>/dav/</d:href><d:propstat><d:prop><d:current-user-principal><d:href>/dav/p/</d:href></d:current-user-principal></d:prop></d:propstat></d:response>`)))
		case "/dav/p/":
			w.WriteHeader(207)
			_, _ = w.Write([]byte(ms(`<d:response><d:href>/dav/p/</d:href><d:propstat><d:prop><c:calendar-home-set><d:href>/dav/h/</d:href></c:calendar-home-set></d:prop></d:propstat></d:response>`)))
		case "/dav/h/":
			w.WriteHeader(207)
			_, _ = w.Write([]byte(ms(`<d:response><d:href>/dav/h/c1/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/><c:calendar/></d:resourcetype><d:displayname>企微</d:displayname></d:prop></d:propstat></d:response>`)))
		case "/dav/h/c1/":
			w.WriteHeader(207)
			_, _ = w.Write([]byte(ms(``)))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c, _ := NewCalDAV(map[string]string{"server": srv.URL, "username": "a", "password": "b"}, Options{HTTPClient: srv.Client()})
	evs, err := c.Events(context.Background(), "", time.Now(), time.Now().Add(24*time.Hour))
	if err != nil || len(evs) != 0 || c.CalendarName() != "企微" {
		t.Fatalf("应经 well-known 跳转找到日历「企微」: %v %d %q\n%v", err, len(evs), c.CalendarName(), methods)
	}
	for _, m := range methods {
		if strings.HasPrefix(m, "GET ") {
			t.Fatalf("跟跳转时 PROPFIND 变成了 GET: %v", methods)
		}
	}
	// 根路径 403 不算密码错：密码真错时 well-known 就回 401
	bad, _ := NewCalDAV(map[string]string{"server": srv.URL, "username": "a", "password": "x"}, Options{HTTPClient: srv.Client()})
	if _, err := bad.Events(context.Background(), "", time.Now(), time.Now().Add(time.Hour)); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("密码错应报 401，实际 %v", err)
	}
}
