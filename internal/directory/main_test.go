package directory

import (
	"os"
	"testing"
)

// 测试里的提供方都跑在 httptest 的 127.0.0.1 上，而出网守卫默认只许公网（egress.go）。
// 测试进程按私有化部署的方式打开内网出网；要验「默认不许」的用例自己用 t.Setenv 关掉。
func TestMain(m *testing.M) {
	os.Setenv(AllowPrivateEgressEnv, "1")
	os.Exit(m.Run())
}
