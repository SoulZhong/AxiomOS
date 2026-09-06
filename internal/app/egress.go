package app

import (
	"strings"
	"sync"
	"time"

	"github.com/teemo/axiomos/internal/directory"
)

// 出网地址的保存时校验：组织自己填的地址（通知 webhook 的接收地址、代码平台的接口地址、出网代理）
// 在存进库之前先过一遍 internal/directory 的出网守卫，指向内网时当场给一句人话，
// 不用等到第一次投递失败才发现。规则本身写在 directory/egress.go，这里只负责翻成中文句子。

// urlFieldKeys 是凭据声明里"值是一个出网地址"的字段名。提供方声明里没有这个标记，
// 这里按约定认：接口地址（api_base）与接收地址（url）。
var urlFieldKeys = []string{"api_base", "url", "base_url", "endpoint"}

// isURLField 判断一个凭据字段的值要不要过出网守卫。
func isURLField(key string) bool {
	for _, k := range urlFieldKeys {
		if key == k {
			return true
		}
	}
	return false
}

// checkEgress 校验一个出网地址；不合格时返回可以直接展示的整句。
func checkEgress(raw string) error {
	return egressErr(directory.CheckEgressURL(raw, directory.DefaultEgress()), raw)
}

// checkProxy 校验出网代理地址（代理可以是 http 或 socks5）。
func checkProxy(raw string) error {
	opts := directory.DefaultEgress()
	opts.AllowHTTP = true
	return egressErr(directory.CheckProxyURL(raw, opts), raw)
}

// egressErr 把守卫的拒绝原因翻成一句中文。
func egressErr(err error, raw string) error {
	if err == nil {
		return nil
	}
	e, ok := directory.AsEgressError(err)
	if !ok {
		return Bad("err.egress_url", strings.TrimSpace(raw))
	}
	switch e.Reason {
	case directory.EgressPrivate:
		return Bad("err.egress_private")
	case directory.EgressCredentials:
		return Bad("err.egress_credentials")
	case directory.EgressBadScheme:
		return Bad("err.egress_scheme", strings.TrimSpace(raw))
	default:
		return Bad("err.egress_url", strings.TrimSpace(raw))
	}
}

// ---------- 进程内的尝试次数限制 ----------

// attemptLimiter 是一个不依赖外部组件的失败计数器：同一个键在窗口内失败够多次就冷却一段时间。
// 用来挡住「登录后逐个猜验证码」这类枚举（ADR 0018 的验证码只有 8 位）。
type attemptLimiter struct {
	mu      sync.Mutex
	max     int           // 窗口内允许的失败次数
	window  time.Duration // 统计窗口
	cool    time.Duration // 超了以后冷却多久
	hits    map[string]*attemptRecord
	swept   time.Time
	nowFunc func() time.Time
}

type attemptRecord struct {
	fails   []time.Time
	blocked time.Time
}

func newAttemptLimiter(max int, window, cool time.Duration) *attemptLimiter {
	return &attemptLimiter{max: max, window: window, cool: cool, hits: map[string]*attemptRecord{}}
}

func (l *attemptLimiter) now() time.Time {
	if l.nowFunc != nil {
		return l.nowFunc()
	}
	return time.Now()
}

// Allowed 说这个键现在还能不能试。
func (l *attemptLimiter) Allowed(keys ...string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	for _, k := range keys {
		if k == "" {
			continue
		}
		if r := l.hits[k]; r != nil && now.Before(r.blocked) {
			return false
		}
	}
	return true
}

// Fail 记一次失败；够 max 次就开始冷却。
func (l *attemptLimiter) Fail(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	for _, k := range keys {
		if k == "" {
			continue
		}
		r := l.hits[k]
		if r == nil {
			r = &attemptRecord{}
			l.hits[k] = r
		}
		kept := r.fails[:0]
		for _, t := range r.fails {
			if now.Sub(t) < l.window {
				kept = append(kept, t)
			}
		}
		r.fails = append(kept, now)
		if len(r.fails) >= l.max {
			r.blocked = now.Add(l.cool)
			r.fails = nil
		}
	}
}

// Reset 清掉一个键的失败记录（试对了）。
func (l *attemptLimiter) Reset(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range keys {
		if k == "" {
			continue
		}
		if r := l.hits[k]; r != nil && l.now().After(r.blocked) {
			delete(l.hits, k)
		}
	}
}

// sweep 定期清掉过期的记录，map 不会一直长大。调用者持锁。
func (l *attemptLimiter) sweep(now time.Time) {
	if now.Sub(l.swept) < l.window {
		return
	}
	l.swept = now
	for k, r := range l.hits {
		if now.Before(r.blocked) {
			continue
		}
		fresh := false
		for _, t := range r.fails {
			if now.Sub(t) < l.window {
				fresh = true
				break
			}
		}
		if !fresh {
			delete(l.hits, k)
		}
	}
}
