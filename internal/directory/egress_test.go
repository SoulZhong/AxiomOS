package directory

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 出网守卫：默认只许公网，内网 / 回环 / 云元数据 / 带凭据的地址都拒；
// 打开 AXIOMOS_ALLOW_PRIVATE_EGRESS=1（私有化部署）后内网放行，但云元数据永远不放。
func TestCheckEgressURL(t *testing.T) {
	t.Setenv(AllowPrivateEgressEnv, "") // 先按托管的默认口径

	refuse := []struct {
		url    string
		reason EgressReason
	}{
		{"https://127.0.0.1/hook", EgressPrivate},
		{"https://localhost:9/hook", EgressPrivate},
		{"https://10.1.2.3/hook", EgressPrivate},
		{"https://192.168.0.5/hook", EgressPrivate},
		{"https://172.16.0.1/hook", EgressPrivate},
		{"https://100.64.0.1/hook", EgressPrivate},
		{"https://[::1]/hook", EgressPrivate},
		{"https://[fd00::1]/hook", EgressPrivate},
		{"https://169.254.169.254/latest/meta-data/", EgressPrivate},
		{"https://metadata.google.internal/computeMetadata/v1/", EgressPrivate},
		{"https://0.0.0.0/hook", EgressPrivate},
		{"http://example.com/hook", EgressBadScheme},
		{"ftp://example.com/hook", EgressBadScheme},
		{"file:///etc/passwd", EgressBadURL},
		{"https://user:pw@example.com/hook", EgressCredentials},
		{"https://example.com/ho\nok", EgressBadURL},
		{"", EgressBadURL},
		{"not a url", EgressBadURL},
	}
	for _, c := range refuse {
		err := CheckEgressURL(c.url, DefaultEgress())
		e, ok := AsEgressError(err)
		if !ok || e.Reason != c.reason {
			t.Fatalf("%q 应被拒（%s），实际 %v", c.url, c.reason, err)
		}
	}
	// 公网地址放行
	for _, u := range []string{"https://8.8.8.8/hook", "https://hooks.example.com/axiom", "https://api.github.com"} {
		if err := CheckEgressURL(u, DefaultEgress()); err != nil {
			t.Fatalf("%q 应放行，实际 %v", u, err)
		}
	}

	// 私有化部署：显式打开后内网与 http 都放行
	t.Setenv(AllowPrivateEgressEnv, "1")
	for _, u := range []string{"http://127.0.0.1:9/hook", "https://192.168.0.5/hook", "http://gitlab.internal/api/v4"} {
		if err := CheckEgressURL(u, DefaultEgress()); err != nil {
			t.Fatalf("打开内网出网后 %q 应放行，实际 %v", u, err)
		}
	}
	// 云元数据地址即使打开了也不放
	for _, u := range []string{"https://169.254.169.254/", "http://metadata.google.internal/"} {
		if e, ok := AsEgressError(CheckEgressURL(u, DefaultEgress())); !ok || e.Reason != EgressPrivate {
			t.Fatalf("云元数据地址任何时候都该被拒：%s", u)
		}
	}
	// 地址里的凭据、非 http(s) 的 scheme 也照拒
	if _, ok := AsEgressError(CheckEgressURL("https://user:pw@example.com/", DefaultEgress())); !ok {
		t.Fatal("带用户名密码的地址应被拒")
	}
}

// 出网代理：允许 http 与 socks5，但同样不许指向内网、不许带凭据。
func TestCheckProxyURL(t *testing.T) {
	t.Setenv(AllowPrivateEgressEnv, "")
	opts := EgressOpts{AllowHTTP: true}
	if err := CheckProxyURL("http://proxy.example.com:3128", opts); err != nil {
		t.Fatalf("公网 http 代理应放行，实际 %v", err)
	}
	for _, u := range []string{"http://127.0.0.1:3128", "socks5://10.0.0.1:1080", "http://169.254.169.254:80"} {
		if e, ok := AsEgressError(CheckProxyURL(u, opts)); !ok || e.Reason != EgressPrivate {
			t.Fatalf("%q 应被拒", u)
		}
	}
	if e, ok := AsEgressError(CheckProxyURL("http://u:p@proxy.example.com:3128", opts)); !ok || e.Reason != EgressCredentials {
		t.Fatal("代理地址里不该带凭据")
	}
	if e, ok := AsEgressError(CheckProxyURL("ws://proxy.example.com", opts)); !ok || e.Reason != EgressBadScheme {
		t.Fatal("代理只认 http / https / socks5")
	}
	t.Setenv(AllowPrivateEgressEnv, "1")
	if err := CheckProxyURL("http://127.0.0.1:3128", EgressOpts{AllowPrivate: true, AllowHTTP: true}); err != nil {
		t.Fatalf("打开内网出网后本机代理应放行，实际 %v", err)
	}
}

// 连接前的第二道：DNS 解析完、真正连上去之前再按地址判断一次，域名改绑绕不过去。
func TestEgressDialerRechecksResolvedAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer srv.Close()
	strict := &http.Client{Transport: &http.Transport{DialContext: egressDialer(EgressOpts{}).DialContext}, Timeout: 5 * time.Second}
	if _, err := strict.Get(srv.URL); err == nil {
		t.Fatal("解析到回环地址的请求应在连接前被拦下")
	} else if _, ok := AsEgressError(err); !ok {
		t.Fatalf("应是出网守卫拦的，实际 %v", err)
	}
	// 直接拨一次也一样
	if _, err := egressDialer(EgressOpts{}).DialContext(context.Background(), "tcp", "127.0.0.1:9"); err == nil {
		t.Fatal("回环地址不该能拨通")
	}
	loose := &http.Client{Transport: &http.Transport{DialContext: egressDialer(EgressOpts{AllowPrivate: true}).DialContext}, Timeout: 5 * time.Second}
	resp, err := loose.Get(srv.URL)
	if err != nil {
		t.Fatalf("打开内网出网后应能连上，实际 %v", err)
	}
	resp.Body.Close()
}

// 重定向也要过同一道守卫：跳到内网地址的重定向被挡下。
func TestEgressRedirectBlocked(t *testing.T) {
	var target string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/go" {
			http.Redirect(w, r, target, http.StatusFound)
			return
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()
	// 拨号按私有化部署放行（要连得上 httptest），重定向按托管的严口径检查
	c := &http.Client{
		Transport:     &http.Transport{DialContext: egressDialer(EgressOpts{AllowPrivate: true}).DialContext},
		CheckRedirect: egressRedirect(EgressOpts{}),
		Timeout:       5 * time.Second,
	}
	target = "http://169.254.169.254/latest/meta-data/"
	if _, err := c.Get(srv.URL + "/go"); err == nil || !strings.Contains(err.Error(), "egress blocked") {
		t.Fatalf("跳到云元数据的重定向应被拒，实际 %v", err)
	}
	target = "https://10.0.0.9/internal"
	if _, err := c.Get(srv.URL + "/go"); err == nil || !strings.Contains(err.Error(), "egress blocked") {
		t.Fatalf("跳到内网的重定向应被拒，实际 %v", err)
	}
}

// 建客户端时就挡：webhook 接收地址与代码平台的接口地址指向内网时，压根建不出客户端。
func TestProvidersRefusePrivateTargets(t *testing.T) {
	t.Setenv(AllowPrivateEgressEnv, "")
	if _, err := NewWebhook(map[string]string{"url": "https://127.0.0.1:9/hook"}, Options{}); err == nil {
		t.Fatal("指向内网的 webhook 地址应被拒")
	}
	if _, err := NewWebhook(map[string]string{"url": "http://hooks.example.com/x"}, Options{}); err == nil {
		t.Fatal("默认只许 https")
	}
	if _, err := NewGitHub(map[string]string{"token": "t", "api_base": "http://127.0.0.1:8080/api/v3"}, Options{}); err == nil {
		t.Fatal("指向内网的 GitHub 接口地址应被拒")
	}
	if _, err := NewGitLab(map[string]string{"token": "t", "api_base": "https://192.168.1.9/api/v4"}, Options{}); err == nil {
		t.Fatal("指向内网的 GitLab 接口地址应被拒")
	}
	if _, err := NewGitHub(map[string]string{"token": "t"}, Options{}); err != nil {
		t.Fatalf("默认的 github.com 地址应放行，实际 %v", err)
	}
	if _, err := newHTTPClient(Options{ProxyURL: "http://127.0.0.1:3128"}); err == nil {
		t.Fatal("指向内网的出网代理应被拒")
	}
	t.Setenv(AllowPrivateEgressEnv, "1")
	if _, err := NewWebhook(map[string]string{"url": "http://127.0.0.1:9/hook"}, Options{}); err != nil {
		t.Fatalf("打开内网出网后应放行，实际 %v", err)
	}
	if _, err := NewGitLab(map[string]string{"token": "t", "api_base": "https://gitlab.internal/api/v4"}, Options{}); err != nil {
		t.Fatalf("打开内网出网后内网 GitLab 应放行，实际 %v", err)
	}
}

// privateIP 的判断口径。
func TestPrivateIP(t *testing.T) {
	yes := []string{"127.0.0.1", "::1", "10.0.0.1", "172.20.3.4", "192.168.9.9", "169.254.1.1", "fd12::1", "224.0.0.1", "0.0.0.0", "100.100.1.1"}
	for _, s := range yes {
		if !privateIP(net.ParseIP(s)) {
			t.Fatalf("%s 应算内网", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "2001:4860:4860::8888"} {
		if privateIP(net.ParseIP(s)) {
			t.Fatalf("%s 应算公网", s)
		}
	}
}
