// Package i18n 提供多语言支持：语言代码、多语言文本、消息词条。
// 内核只产生带参数的消息键，输出层按请求者语言渲染。
package i18n

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Locale 是语言代码。
type Locale string

const (
	ZhCN Locale = "zh-CN"
	EnUS Locale = "en-US"
)

// Default 是系统默认语言。
const Default = ZhCN

// Supported 是支持的语言。
var Supported = []Locale{ZhCN, EnUS}

// Normalize 把任意写法归一到支持的语言；不认识返回空。
func Normalize(s string) Locale {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case s == "":
		return ""
	case strings.HasPrefix(s, "zh"):
		return ZhCN
	case strings.HasPrefix(s, "en"):
		return EnUS
	}
	return ""
}

// ParseAcceptLanguage 从 Accept-Language 头里挑第一个支持的语言。
func ParseAcceptLanguage(h string) Locale {
	for _, part := range strings.Split(h, ",") {
		tag := strings.TrimSpace(strings.Split(part, ";")[0])
		if l := Normalize(tag); l != "" {
			return l
		}
	}
	return ""
}

// Text 是多语言文本：语言 → 字符串。JSON 里可以是字符串（视为 zh-CN）或对象。
type Text map[Locale]string

// T 构造一条中英文本。
func T(zh, en string) Text { return Text{ZhCN: zh, EnUS: en} }

// In 取某语言的文本，缺失时依次回退到默认语言、任意语言。
func (t Text) In(l Locale) string {
	if t == nil {
		return ""
	}
	if s, ok := t[l]; ok && s != "" {
		return s
	}
	if s, ok := t[Default]; ok && s != "" {
		return s
	}
	for _, s := range t {
		if s != "" {
			return s
		}
	}
	return ""
}

// IsZero 判断是否为空。
func (t Text) IsZero() bool { return len(t) == 0 }

func (t Text) MarshalJSON() ([]byte, error) {
	if t == nil {
		return []byte("null"), nil
	}
	m := map[string]string{}
	for k, v := range t {
		m[string(k)] = v
	}
	return json.Marshal(m)
}

func (t *Text) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*t = Text{Default: s}
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		return fmt.Errorf("多语言文本应为字符串或 {语言: 文本}")
	}
	out := Text{}
	for k, v := range m {
		if l := Normalize(k); l != "" {
			out[l] = v
		}
	}
	*t = out
	return nil
}

// Key 是词条键，作为消息参数时会被翻译。
type Key string

// Msg 是一条待渲染的消息：词条键 + 参数。参数可以是普通值、Text、Key 或 Msg。
type Msg struct {
	Key  string
	Args []any
}

// M 构造消息。
func M(key string, args ...any) Msg { return Msg{Key: key, Args: args} }

// Render 按语言渲染消息。
func (m Msg) Render(l Locale) string {
	tmpl := lookup(m.Key, l)
	if tmpl == "" {
		return m.Key
	}
	args := make([]any, len(m.Args))
	for i, a := range m.Args {
		args[i] = Arg(l, a)
	}
	return fmt.Sprintf(tmpl, args...)
}

// Renderable 是能按语言渲染自己的参数。
type Renderable interface{ Render(Locale) string }

// Arg 把一个参数解析成该语言下的字符串。
func Arg(l Locale, a any) any {
	switch v := a.(type) {
	case Renderable:
		return v.Render(l)
	case Text:
		return v.In(l)
	case Key:
		return Tr(l, string(v))
	case Msg:
		return v.Render(l)
	case []string:
		return strings.Join(v, Tr(l, "sep.list"))
	case []Text:
		parts := make([]string, len(v))
		for i, t := range v {
			parts[i] = t.In(l)
		}
		return strings.Join(parts, Tr(l, "sep.list"))
	}
	return a
}

// Tr 翻译无参数词条。
func Tr(l Locale, key string) string {
	if s := lookup(key, l); s != "" {
		return s
	}
	return key
}

// Trf 翻译带参数词条。
func Trf(l Locale, key string, args ...any) string { return M(key, args...).Render(l) }

func lookup(key string, l Locale) string {
	e, ok := catalog[key]
	if !ok {
		return ""
	}
	if s := e[l]; s != "" {
		return s
	}
	return e[Default]
}

// Has 判断词条是否存在。
func Has(key string) bool { _, ok := catalog[key]; return ok }
