package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 用量钩子脚本：读 Claude Code 的会话记录，按消息 id 去重、按模型累计，带着 ~/.claude.json 里的令牌报到 /me/usage。
func TestUsageHookScript(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("没有 python3")
	}
	var got map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/me/usage" || r.Method != http.MethodPost {
			w.WriteHeader(404)
			return
		}
		auth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"delta_tokens":1}`))
	}))
	defer srv.Close()
	home := t.TempDir()
	// 令牌配在项目级（claude mcp add 默认的 local 作用域）：钩子要从当前目录往上找
	proj := filepath.Join(home, "work", "repo")
	if err := os.MkdirAll(filepath.Join(proj, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"projects":{"`+proj+`":{"mcpServers":{"axiomos":{"type":"http","url":"`+srv.URL+`/mcp","headers":{"Authorization":"Bearer axm_test"}}}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(home, "sess-1.jsonl")
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"hi"}}`,
		`{"type":"assistant","message":{"id":"msg_1","model":"claude-sonnet-5","usage":{"input_tokens":100,"output_tokens":5,"cache_read_input_tokens":40,"cache_creation_input_tokens":10}}}`,
		`{"type":"assistant","message":{"id":"msg_1","model":"claude-sonnet-5","usage":{"input_tokens":100,"output_tokens":30,"cache_read_input_tokens":40,"cache_creation_input_tokens":10}}}`,
		`{"type":"assistant","message":{"id":"msg_2","model":"claude-haiku-4-5","usage":{"input_tokens":7,"output_tokens":3}}}`,
		`not json`,
	}
	if err := os.WriteFile(transcript, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(home, "usage-hook.py")
	if err := os.WriteFile(script, []byte(usageHookScript), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(py, script)
	cmd.Env = append(os.Environ(), "HOME="+home)
	cmd.Stdin = strings.NewReader(`{"session_id":"sess-1","transcript_path":"` + transcript + `","cwd":"` + filepath.Join(proj, "sub") + `","hook_event_name":"Stop"}`)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("钩子应静默成功: %v %s", err, out)
	}
	if auth != "Bearer axm_test" || got == nil || got["session"] != "sess-1" || got["client"] != "claude-code" {
		t.Fatalf("应带令牌报到 /me/usage: auth=%q body=%v", auth, got)
	}
	cum, _ := got["cumulative"].([]any)
	if len(cum) != 2 {
		t.Fatalf("应按模型两条: %v", cum)
	}
	byModel := map[string]map[string]any{}
	for _, c := range cum {
		m := c.(map[string]any)
		byModel[m["model_id"].(string)] = m
	}
	s := byModel["claude-sonnet-5"]
	if s["input_tokens"].(float64) != 100 || s["output_tokens"].(float64) != 30 || s["cache_read_tokens"].(float64) != 40 || s["cache_write_tokens"].(float64) != 10 {
		t.Fatalf("同一条消息的流式多行应只算一次并取最大: %v", s)
	}
	if h := byModel["claude-haiku-4-5"]; h["input_tokens"].(float64) != 7 || h["output_tokens"].(float64) != 3 {
		t.Fatalf("第二个模型: %v", h)
	}
}
