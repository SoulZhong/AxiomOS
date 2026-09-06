package directory

import (
	"testing"

	"github.com/teemo/axiomos/internal/i18n"
)

// 注册表：生产提供方按文件顺序列出（feishu、wecom），字段声明齐全；测试提供方只 Lookup 得到、不进列表。
func TestRegistry(t *testing.T) {
	ps := Providers()
	if len(ps) < 2 || ps[0].Key != "feishu" || ps[1].Key != "wecom" {
		t.Fatalf("生产提供方顺序应为 feishu、wecom，实际 %+v", keys(ps))
	}
	for _, p := range ps {
		if p.Title.In(i18n.ZhCN) == "" || p.Title.In(i18n.EnUS) == "" || p.RootDepartmentID == "" || len(p.Fields) == 0 || len(p.Prerequisites) == 0 || p.New == nil {
			t.Fatalf("提供方 %s 的声明不完整 %+v", p.Key, p)
		}
		secrets := 0
		for _, f := range p.Fields {
			if f.Secret {
				secrets++
			}
			if f.Title.In(i18n.EnUS) == "" {
				t.Fatalf("字段 %s.%s 缺英文标题", p.Key, f.Key)
			}
		}
		if secrets == 0 {
			t.Fatalf("提供方 %s 应至少有一个保密字段", p.Key)
		}
	}
	if fe, ok := Lookup("feishu"); !ok || fe.RootDepartmentID != "0" || !hasField(fe, "app_id", false) || !hasField(fe, "app_secret", true) {
		t.Fatalf("飞书声明不符 %+v", fe)
	}
	if wc, ok := Lookup("wecom"); !ok || wc.RootDepartmentID != "1" || !hasField(wc, "corp_id", false) || !hasField(wc, "corp_secret", true) {
		t.Fatalf("企业微信声明不符 %+v", wc)
	}
	if _, ok := Lookup("dingtalk"); ok {
		t.Fatal("未接入的平台不应被找到")
	}

	UseFake(NewFake(), "s")
	if _, ok := Lookup(FakeProviderKey); !ok {
		t.Fatal("测试提供方应能 Lookup")
	}
	for _, p := range Providers() {
		if p.Key == FakeProviderKey {
			t.Fatal("测试提供方不应出现在生产列表")
		}
	}
	// 代理地址不合法时建客户端失败
	for _, p := range ps {
		if _, err := p.New(map[string]string{}, Options{ProxyURL: "not a url"}); err == nil {
			t.Fatalf("%s 应拒绝非法代理地址", p.Key)
		}
	}
	// 重复登记生产提供方是编程错误
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("重复登记应 panic")
			}
		}()
		Register(ps[0])
	}()
}

func keys(ps []Provider) []string {
	out := []string{}
	for _, p := range ps {
		out = append(out, p.Key)
	}
	return out
}

func hasField(p Provider, key string, secret bool) bool {
	f, ok := p.Field(key)
	return ok && f.Secret == secret
}
