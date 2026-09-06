package directory

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
)

// 出网守卫：所有由组织自己填的地址（通知 webhook、代码平台接口地址、出网代理）都要先过这里。
//
// 规则是「默认只许公网」：多租户托管时，组织管理员不该能让服务器去访问内网地址或云元数据接口；
// 私有化部署要指向内网 GitLab 或内网接收端时，由部署方设置环境变量 AXIOMOS_ALLOW_PRIVATE_EGRESS=1
// 显式打开（ADR 0014 的可配边界：允许什么由部署方决定，判断规则写死）。
//
// 两道关卡：保存与建客户端时按域名先查一遍（CheckEgressURL），真正连接前再按解析出的地址查一遍
// （net.Dialer.Control），DNS 改绑（rebinding）绕不过去；重定向到不允许的地址也会被挡下。

// AllowPrivateEgressEnv 是打开内网出网的环境变量名。
const AllowPrivateEgressEnv = "AXIOMOS_ALLOW_PRIVATE_EGRESS"

// PrivateEgressAllowed 是这台服务器允不允许往内网出网。
func PrivateEgressAllowed() bool {
	return strings.TrimSpace(os.Getenv(AllowPrivateEgressEnv)) == "1"
}

// EgressOpts 是一次出网检查的口径。
type EgressOpts struct {
	// AllowPrivate 为真时放行内网、回环地址，并允许 http（私有化部署）。
	AllowPrivate bool
	// AllowHTTP 为真时即使不放行内网也允许 http（出网代理用）。
	AllowHTTP bool
}

// DefaultEgress 按环境变量给出本机的口径。
func DefaultEgress() EgressOpts { return EgressOpts{AllowPrivate: PrivateEgressAllowed()} }

// EgressReason 是地址被拒的原因，应用层按它挑一句中文。
type EgressReason string

const (
	EgressBadURL      EgressReason = "url"         // 根本不是一个地址
	EgressBadScheme   EgressReason = "scheme"      // 不是 http / https，或没开内网时用了 http
	EgressCredentials EgressReason = "credentials" // 地址里带了用户名密码
	EgressPrivate     EgressReason = "private"     // 指向内网 / 回环 / 云元数据
)

// EgressError 是被出网守卫拒掉的地址。
type EgressError struct {
	Reason EgressReason
	URL    string
	Host   string
}

func (e *EgressError) Error() string {
	switch e.Reason {
	case EgressCredentials:
		return fmt.Sprintf("egress blocked: url has embedded credentials (%s)", e.Host)
	case EgressBadScheme:
		return fmt.Sprintf("egress blocked: scheme not allowed (%s)", e.URL)
	case EgressPrivate:
		return fmt.Sprintf("egress blocked: %s resolves to a private or metadata address", e.Host)
	default:
		return fmt.Sprintf("egress blocked: not a valid url (%s)", e.URL)
	}
}

// AsEgressError 拆出出网守卫的拒绝原因。
func AsEgressError(err error) (*EgressError, bool) {
	var e *EgressError
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// 云元数据地址：任何情况下都不允许，连私有化部署也不行。
var metadataHosts = []string{"metadata.google.internal", "metadata.goog"}

var metadataIPs = []string{"169.254.169.254", "fd00:ec2::254"}

func isMetadataHost(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	for _, m := range metadataHosts {
		if h == m || strings.HasSuffix(h, "."+m) {
			return true
		}
	}
	return false
}

func isMetadataIP(ip net.IP) bool {
	for _, s := range metadataIPs {
		if m := net.ParseIP(s); m != nil && m.Equal(ip) {
			return true
		}
	}
	return false
}

// cgnat 是运营商级 NAT 段 100.64.0.0/10，按内网对待。
var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// privateIP 判断一个地址算不算内网：回环、私有段、链路本地、组播、未指定、唯一本地（fc00::/7）、CGNAT。
func privateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return true
	}
	if v4 := ip.To4(); v4 != nil && cgnat.Contains(v4) {
		return true
	}
	return false
}

// checkEgressIP 检查一个已经解析出来的地址。
func checkEgressIP(ip net.IP, opts EgressOpts) error {
	if isMetadataIP(ip) {
		return &EgressError{Reason: EgressPrivate, Host: ip.String()}
	}
	if opts.AllowPrivate {
		return nil
	}
	if privateIP(ip) {
		return &EgressError{Reason: EgressPrivate, Host: ip.String()}
	}
	return nil
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// CheckEgressURL 检查一个要出网的地址：必须是 http(s)（没开内网时只许 https）、不能带用户名密码、
// 不能指向内网 / 回环 / 云元数据。域名解析不出来时先放行，连接前的那一道还会再查。
func CheckEgressURL(raw string, opts EgressOpts) error {
	s := strings.TrimSpace(raw)
	if s == "" || hasControl(s) {
		return &EgressError{Reason: EgressBadURL, URL: raw}
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return &EgressError{Reason: EgressBadURL, URL: raw}
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return &EgressError{Reason: EgressBadScheme, URL: raw}
	}
	if u.User != nil {
		return &EgressError{Reason: EgressCredentials, URL: raw, Host: u.Hostname()}
	}
	// 先判内网再判 scheme：内网地址给的那句话更有用（说清怎么放开），别被"要用 https"盖住。
	if err := checkEgressHost(u.Hostname(), raw, opts); err != nil {
		return err
	}
	if scheme == "http" && !opts.AllowPrivate && !opts.AllowHTTP {
		return &EgressError{Reason: EgressBadScheme, URL: raw}
	}
	return nil
}

// CheckProxyURL 检查出网代理地址：代理常常是 http 或 socks5，其余规则与 CheckEgressURL 相同。
func CheckProxyURL(raw string, opts EgressOpts) error {
	s := strings.TrimSpace(raw)
	if s == "" || hasControl(s) {
		return &EgressError{Reason: EgressBadURL, URL: raw}
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return &EgressError{Reason: EgressBadURL, URL: raw}
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5", "socks5h":
	default:
		return &EgressError{Reason: EgressBadScheme, URL: raw}
	}
	if u.User != nil {
		return &EgressError{Reason: EgressCredentials, URL: raw, Host: u.Hostname()}
	}
	return checkEgressHost(u.Hostname(), raw, opts)
}

func checkEgressHost(host, raw string, opts EgressOpts) error {
	if host == "" {
		return &EgressError{Reason: EgressBadURL, URL: raw}
	}
	if isMetadataHost(host) {
		return &EgressError{Reason: EgressPrivate, URL: raw, Host: host}
	}
	if ip := net.ParseIP(host); ip != nil {
		if err := checkEgressIP(ip, opts); err != nil {
			return &EgressError{Reason: EgressPrivate, URL: raw, Host: host}
		}
		return nil
	}
	if opts.AllowPrivate {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		// 这台机器现在解析不出来：先放行，真正连接前的那一道会按解析结果再查一次。
		return nil
	}
	for _, a := range addrs {
		if err := checkEgressIP(a.IP, opts); err != nil {
			return &EgressError{Reason: EgressPrivate, URL: raw, Host: host}
		}
	}
	return nil
}

// egressDialer 是连接前再查一次的拨号器：DNS 解析完、真正连上去之前按解析出的地址判断，
// 域名改绑（DNS rebinding）绕不过去。
func egressDialer(opts EgressOpts) *net.Dialer {
	return &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return &EgressError{Reason: EgressBadURL, URL: address}
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return &EgressError{Reason: EgressBadURL, URL: address}
			}
			return checkEgressIP(ip, opts)
		},
	}
}

// egressRedirect 让重定向也过一遍守卫：跳到内网地址的重定向被挡下。
func egressRedirect(opts EgressOpts) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("stopped after 5 redirects")
		}
		return CheckEgressURL(req.URL.String(), opts)
	}
}
