package api

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"text/template"
	"time"
	"unicode"

	"github.com/teemo/axiomos/internal/app"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
)

// ---------- Agent 设备码授权（ADR 0018） ----------

//go:embed usage-hook.py
var usageHookScript string

//go:embed usage-hook.ps1
var usageHookScriptPS string

//go:embed connect.sh.tmpl
var connectScript string

//go:embed connect.ps1.tmpl
var connectScriptPS string

var connectTmplPS = template.Must(template.New("connect.ps1").Parse(connectScriptPS))

var connectTmpl = template.Must(template.New("connect.sh").Parse(connectScript))

func (s *Server) deviceRoutes(auth, pub func(string, http.HandlerFunc)) {
	pub("POST /api/v1/agent-auth/device", s.deviceRequest)
	pub("POST /api/v1/agent-auth/token", s.deviceToken)
	pub("GET /api/v1/agent-auth/connect.sh", s.connectScript)
	// 用量钩子（ADR 0030）：接入脚本把它装进 ~/.axiomos，Claude Code 每轮结束时跑一下
	pub("GET /api/v1/agent-auth/usage-hook.py", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/x-python; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(usageHookScript))
	})
	// Windows 版：PowerShell 5.1+ 自带，不依赖 python
	pub("GET /api/v1/agent-auth/usage-hook.ps1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(usageHookScriptPS))
	})
	pub("GET /api/v1/agent-auth/connect.ps1", s.connectScriptPS)
	// 接入链接（ADR 0024）：短地址 /connect 另挂在根 mux 上（见 cmd/axiomd 与 ConnectAlias）
	pub("GET /api/v1/agent-auth/onboard", s.onboard)
	pub("GET /connect", s.onboard)
	auth("GET /api/v1/agent-auth/device/{user_code}", s.deviceGet)
	auth("POST /api/v1/agent-auth/device/{user_code}/approve", s.deviceApprove)
	auth("POST /api/v1/agent-auth/device/{user_code}/deny", s.deviceDeny)
	auth("GET /api/v1/agents/{id}/check", s.agentCheck)
}

func (s *Server) deviceRequest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Client string `json:"client"`
		Name   string `json:"name"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.App.RequestDeviceCode(r.Context(), in.Client, in.Name)
	respond(w, r, v, err)
}

func (s *Server) deviceToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DeviceCode string `json:"device_code"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.App.PollDevice(r.Context(), in.DeviceCode)
	respond(w, r, v, err)
}

func (s *Server) deviceGet(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.DeviceRequestByUserCode(r.Context(), sessionOf(r), r.PathValue("user_code"))
	respond(w, r, v, err)
}

// DeviceApproveInputV 是批准时的请求体。grants 两种写法都认：
// {"execute": "allow" | "with_approval" | "deny", ...}（向导）或 [{name, mode}]（与 POST /agents 相同）。
type DeviceApproveInputV struct {
	Name           string          `json:"name"`
	Capabilities   []string        `json:"capabilities"`
	Grants         json.RawMessage `json:"grants"`
	MaxConcurrency int             `json:"max_concurrency"`
	MaxConcurrent  int             `json:"max_concurrent"`
	Shared         bool            `json:"shared"`
}

func (in DeviceApproveInputV) toApp() (app.ApproveDeviceInput, error) {
	out := app.ApproveDeviceInput{Name: in.Name, Capabilities: in.Capabilities, MaxConcurrent: in.MaxConcurrency, Shared: in.Shared}
	if out.MaxConcurrent == 0 {
		out.MaxConcurrent = in.MaxConcurrent
	}
	if len(in.Grants) == 0 || string(in.Grants) == "null" {
		return out, nil
	}
	var choices map[string]string
	if err := json.Unmarshal(in.Grants, &choices); err == nil {
		g, err := app.ParseGrantChoices(choices)
		if err != nil {
			return out, err
		}
		out.Grants = g
		return out, nil
	}
	var list []GrantV
	if err := json.Unmarshal(in.Grants, &list); err != nil {
		return out, app.Bad("err.bad_json", "grants")
	}
	out.Grants = map[domain.Grant]domain.GrantMode{}
	for _, g := range list {
		mode := g.Mode
		if mode == "" {
			mode = domain.GrantDirect
		}
		out.Grants[g.Name] = mode
	}
	return out, nil
}

func (s *Server) deviceApprove(w http.ResponseWriter, r *http.Request) {
	var in DeviceApproveInputV
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	ai, err := in.toApp()
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ag, dv, err := s.App.ApproveDevice(r.Context(), sessionOf(r), r.PathValue("user_code"), ai)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	rf, _ := s.refsFor(r)
	writeJSON(w, 200, map[string]any{"agent": agentView(ag, rf, time.Now(), true, s.agentStates(r, ag)[ag.ID]), "request": dv})
}

func (s *Server) deviceDeny(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.DenyDevice(r.Context(), sessionOf(r), r.PathValue("user_code"))
	respond(w, r, v, err)
}

func (s *Server) agentCheck(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.CheckAgent(r.Context(), sessionOf(r), r.PathValue("id"))
	respond(w, r, v, err)
}

// 接入脚本里的句子（按 Accept-Language 或 ?lang= 选语言）。
var connectText = map[string]i18n.Text{
	"need_curl":    i18n.T("需要 curl 才能接入。", "curl is required to connect."),
	"requesting":   i18n.T("正在向 AxiomOS 申请接入……", "Requesting access from AxiomOS..."),
	"request_fail": i18n.T("连不上 AxiomOS，请检查地址与网络。", "Could not reach AxiomOS; check the address and your network."),
	"open":         i18n.T("打开", "Open"),
	"enter":        i18n.T("输入验证码", "enter the code"),
	"approve":      i18n.T("批准这个 Agent。", "and approve this agent."),
	"waiting":      i18n.T("等待批准（15 分钟内有效）……", "Waiting for approval (valid for 15 minutes)..."),
	"denied":       i18n.T("接入被拒绝了。", "The request was denied."),
	"expired":      i18n.T("验证码已过期，请重新执行这条命令。", "The code has expired; run this command again."),
	"no_token":     i18n.T("没有拿到令牌，请重新执行这条命令。", "No token was received; run this command again."),
	"approved":     i18n.T("已批准，Agent 名称：", "Approved. Agent name:"),
	"written":      i18n.T("已写入", "Written to"),
	"hook_written": i18n.T("已装上用量钩子：每轮结束自动把 token 用量报到 AxiomOS（", "Usage hook installed: token usage is reported to AxiomOS after every turn ("),
	"hook_failed":  i18n.T("用量钩子没装上（下载或写 settings.json 失败）。想让 AxiomOS 自动记 token，重跑本脚本。", "The usage hook was not installed (download or writing settings.json failed). To let AxiomOS record tokens automatically, rerun this script."),
	"hook_manual":  i18n.T("没有 python3，用量钩子没装。想让 AxiomOS 自动记 token，装好 python3 后重跑本脚本。", "python3 is missing, so the usage hook was not installed. To let AxiomOS record tokens automatically, install python3 and rerun this script."),
	"merged":       i18n.T("已合并进", "Merged into"),
	"no_claude":    i18n.T("没找到 claude 命令。把下面这段加进 Claude Code 的 MCP 配置（~/.claude.json 的 mcpServers，或项目里的 .mcp.json）：", "The claude command was not found. Add the following to Claude Code's MCP config (mcpServers in ~/.claude.json, or .mcp.json in the project):"),
	"no_python":    i18n.T("已有配置文件但没有 python3 帮忙合并。把下面这段加进", "The config file exists but python3 is unavailable to merge. Add the following to"),
	"codex_exists": i18n.T("里已经有 [mcp_servers.axiomos]，请把它换成：", "already contains [mcp_servers.axiomos]; replace it with:"),
	"replaced":     i18n.T("已替换掉旧的 [mcp_servers.axiomos]：", "Replaced the old [mcp_servers.axiomos] in"),
	"custom":       i18n.T("把下面这段加进你的 MCP 客户端配置：", "Add the following to your MCP client's configuration:"),
	"try_claude":   i18n.T("重启 Claude Code（或在 /mcp 里重连）后，说「看看我在 AxiomOS 上有什么任务」试试。配置是全局的，任何目录里都能用。", "Restart Claude Code (or reconnect in /mcp), then try: \"What tasks do I have in AxiomOS?\" The config is user-wide and works from any directory."),
	"try_cursor":   i18n.T("重启 Cursor 后，在对话里让它看看 AxiomOS 上的任务。", "Restart Cursor, then ask it about your tasks in AxiomOS."),
	"try_codex":    i18n.T("重启 Codex 后，让它看看 AxiomOS 上的任务。", "Restart Codex, then ask it about your tasks in AxiomOS."),
	"done":         i18n.T("接入完成。回到网页的接入向导，它会显示这个 Agent 已可用。", "Done. Go back to the web wizard; it will show this agent as ready."),
	"checking":     i18n.T("正在做一次连接检查（调用 whoami）……", "Running a connection check (calling whoami)..."),
}

// connectScript 是 POSIX sh 版接入脚本（macOS / Linux / WSL）。
func (s *Server) connectScript(w http.ResponseWriter, r *http.Request) {
	s.connectScriptWith(w, r, connectTmpl, "text/x-shellscript; charset=utf-8", func(mcpURL, token string) map[string]string {
		return map[string]string{
			"JSON":       cfgJSON,
			"CursorJSON": cfgCursorJSON,
			"CodexTOML":  cfgCodexTOML,
			"ClaudeCLI":  fmt.Sprintf(cfgClaudeCLI, `"`+mcpURL+`"`, token),
		}
	})
}

// connectScriptPS 是 Windows PowerShell 版：同一批配置写法、同一批句子，只是换成 PowerShell 的写法。
// 用法 irm '<PUBLIC_URL>/api/v1/agent-auth/connect.ps1?client=claude-code' | iex。
func (s *Server) connectScriptPS(w http.ResponseWriter, r *http.Request) {
	s.connectScriptWith(w, r, connectTmplPS, "text/plain; charset=utf-8", func(mcpURL, token string) map[string]string {
		// PowerShell 的 here-string 里 $MCP / $TOKEN 自己会展开，所以配置写法直接还原成多行文本
		return map[string]string{
			"JSON":       renderConfig(cfgJSON, mcpURL, token),
			"CursorJSON": renderConfig(cfgCursorJSON, mcpURL, token),
			"CodexTOML":  renderConfig(cfgCodexTOML, mcpURL, token),
			"ClaudeCLI":  fmt.Sprintf(cfgClaudeCLI, mcpURL, token),
		}
	})
}

// connectScriptWith 渲染一份接入脚本：校验 client、选语言、把配置写法与句子填进模板。
// cfgOf 拿到的是脚本里代表 MCP 地址与令牌的变量名（$MCP、$TOKEN），不是真值——脚本运行时才有。
func (s *Server) connectScriptWith(w http.ResponseWriter, r *http.Request, tmpl *template.Template, contentType string, cfgOf func(mcpURL, token string) map[string]string) {
	client := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("client")))
	if client == "" {
		client = "claude-code"
	}
	known := false
	for _, c := range app.DeviceClients {
		if c == client {
			known = true
		}
	}
	if !known {
		writeErr(w, r, app.Bad("err.device_client", client))
		return
	}
	loc := i18n.Normalize(r.URL.Query().Get("lang"))
	if loc == "" {
		loc = localeOf(r)
	}
	t := map[string]string{}
	for k, v := range connectText {
		t[k] = v.In(loc)
	}
	base := strings.TrimRight(s.App.PublicURL, "/")
	var buf bytes.Buffer
	// 配置写法与接入说明共用同一批常量，两条路不会各写各的（ADR 0024）
	cfg := cfgOf("$MCP", "$TOKEN")
	if err := tmpl.Execute(&buf, map[string]any{"BaseURL": base, "Client": client, "ClientTitle": i18n.Tr(loc, "device.client."+client), "T": t, "Cfg": cfg}); err != nil {
		writeErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	_, _ = w.Write(buf.Bytes())
}

// ---------- 接入链接（ADR 0024） ----------

// 各客户端 MCP 配置的写法，全系统只此一份：接入脚本（connect.sh.tmpl）与接入说明
// （onboard.*.md.tmpl）都从这里取，两条路不会各写各的。printf 格式，第一个 %s 是 MCP 地址，
// 第二个是令牌；`\n` 是给 shell 的 printf 用的转义，说明书那边会还原成真的换行。
const (
	cfgJSON       = `{\n  "mcpServers": {\n    "axiomos": {\n      "type": "http",\n      "url": "%s",\n      "headers": { "Authorization": "Bearer %s" }\n    }\n  }\n}`
	cfgCursorJSON = `{\n  "mcpServers": {\n    "axiomos": {\n      "url": "%s",\n      "headers": { "Authorization": "Bearer %s" }\n    }\n  }\n}`
	cfgCodexTOML  = `\n[mcp_servers.axiomos]\nurl = "%s"\nhttp_headers = { Authorization = "Bearer %s" }\n`
	// --scope user：写进 ~/.claude.json 的用户级配置，任何目录里都能用。默认的 local 只对当前项目目录生效，
	// 换个目录打开 Claude Code 就没有 axiomos 了（接入现状核对 2026-09-08 的第一条卡点）。
	cfgClaudeCLI = `claude mcp add --transport http --scope user axiomos %s --header "Authorization: Bearer %s"`
)

// renderConfig 把上面的写法还原成给人（和 Agent）看的多行文本。
func renderConfig(shape, mcpURL, token string) string {
	return strings.TrimSpace(fmt.Sprintf(strings.ReplaceAll(shape, `\n`, "\n"), mcpURL, token))
}

//go:embed onboard.zh.md.tmpl
var onboardZh string

//go:embed onboard.en.md.tmpl
var onboardEn string

var onboardTmpl = map[i18n.Locale]*template.Template{
	i18n.ZhCN: template.Must(template.New("onboard.zh.md").Parse(onboardZh)),
	i18n.EnUS: template.Must(template.New("onboard.en.md").Parse(onboardEn)),
}

// onboardNameMax 是「建议的 Agent 名称」在说明书里的长度上限；超过就当没给。
const onboardNameMax = 40

// onboardName 收拾 ?name=：它只是 JSON 例子里的一个字符串，不能变成说明书里的一句话。
// 去掉换行、控制字符和会撑破 JSON / 代码块的字符；收拾完是空的或太长，就静静地丢掉（ADR 0024 第 3 条）。
func onboardName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsControl(r), r == '\u2028', r == '\u2029', r == '\ufeff':
			continue
		case r == '"', r == '\\', r == '`':
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if len([]rune(out)) > onboardNameMax {
		return ""
	}
	return out
}

// jsonString 把一段文本变成 JSON 字符串字面量（不转义 HTML，中文原样）。
func jsonString(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSpace(buf.String())
}

// onboardWantsHTML 判断这次请求要不要跳到网页版：只有明确要 text/html（浏览器）才跳；
// curl 的 */*、Agent、空 Accept 一律给 Markdown 说明书。
func onboardWantsHTML(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format"))) {
	case "md", "markdown", "text":
		return false
	case "html":
		return true
	}
	accept := strings.ToLower(r.Header.Get("Accept"))
	if strings.Contains(accept, "text/markdown") || strings.Contains(accept, "text/plain") {
		return false
	}
	return strings.Contains(accept, "text/html")
}

// ConnectAlias 是短地址 /connect 的处理器，挂在根 mux 上（见 cmd/axiomd）。
func (s *Server) ConnectAlias() http.Handler { return s.cors(http.HandlerFunc(s.onboard)) }

// onboard 是接入链接（ADR 0024）：人把它贴给自己的 Agent，Agent 读到一份照着做就能接入的说明。
// 公开、不需要登录、不含任何组织数据；浏览器打开时跳到前端的 /connect/ 页。
func (s *Server) onboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Accept, Accept-Language")
	base := strings.TrimRight(s.App.PublicURL, "/")
	q := r.URL.Query()
	if onboardWantsHTML(r) {
		to := base + "/connect/"
		keep := url.Values{}
		for k, v := range q {
			if k != "format" {
				keep[k] = v
			}
		}
		if len(keep) > 0 {
			to += "?" + keep.Encode()
		}
		http.Redirect(w, r, to, http.StatusSeeOther)
		return
	}
	client := strings.ToLower(strings.TrimSpace(q.Get("client")))
	if client != "" && !contains(app.DeviceClients, client) {
		writeErr(w, r, app.Bad("err.device_client", client))
		return
	}
	loc := i18n.Normalize(q.Get("lang"))
	if loc == "" {
		loc = localeOf(r)
	}
	tmpl := onboardTmpl[loc]
	if tmpl == nil {
		tmpl = onboardTmpl[i18n.Default]
	}

	mcpURL := base + "/mcp"
	tokenHint := i18n.T("axm_你的令牌", "axm_YOUR_TOKEN").In(loc)
	show := map[string]bool{}
	for _, c := range app.DeviceClients {
		show[strings.ReplaceAll(c, "-", "_")] = client == "" || c == client
	}
	clientParam := client
	if clientParam == "" {
		clientParam = "claude-code"
	}
	name := onboardName(q.Get("name"))
	if name == "" {
		name = i18n.T("我的开发 Agent", "My dev agent").In(loc)
	}
	data := map[string]any{
		"BaseURL":     base,
		"MCPURL":      mcpURL,
		"One":         client != "",
		"Client":      client,
		"ClientParam": clientParam,
		"ClientTitle": i18n.Tr(loc, "device.client."+clientParam),
		"Show":        show,
		"TokenHint":   tokenHint,
		// 用户给的 name 只出现在这一个 JSON 例子里，且是编码过的字符串字面量。
		"RequestBody": fmt.Sprintf(`{"client": %s, "name": %s}`, jsonString(clientParam), jsonString(name)),
		"Cfg": map[string]string{
			"ClaudeCLI":  renderConfig(cfgClaudeCLI, mcpURL, tokenHint),
			"JSON":       renderConfig(cfgJSON, mcpURL, tokenHint),
			"CursorJSON": renderConfig(cfgCursorJSON, mcpURL, tokenHint),
			"CodexTOML":  renderConfig(cfgCodexTOML, mcpURL, tokenHint),
		},
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		writeErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(buf.Bytes())
}

// contains 判断字符串在不在列表里（app 层同名函数不导出）。
func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
