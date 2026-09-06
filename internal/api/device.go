package api

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"net/http"
	"strings"
	"text/template"
	"time"

	"github.com/teemo/axiomos/internal/app"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
)

// ---------- Agent 设备码授权（ADR 0018） ----------

//go:embed connect.sh.tmpl
var connectScript string

var connectTmpl = template.Must(template.New("connect.sh").Parse(connectScript))

func (s *Server) deviceRoutes(auth, pub func(string, http.HandlerFunc)) {
	pub("POST /api/v1/agent-auth/device", s.deviceRequest)
	pub("POST /api/v1/agent-auth/token", s.deviceToken)
	pub("GET /api/v1/agent-auth/connect.sh", s.connectScript)
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
	writeJSON(w, 200, map[string]any{"agent": agentView(ag, rf, time.Now(), true), "request": dv})
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
	"merged":       i18n.T("已合并进", "Merged into"),
	"no_claude":    i18n.T("没找到 claude 命令。把下面这段加进 Claude Code 的 MCP 配置（~/.claude.json 的 mcpServers，或项目里的 .mcp.json）：", "The claude command was not found. Add the following to Claude Code's MCP config (mcpServers in ~/.claude.json, or .mcp.json in the project):"),
	"no_python":    i18n.T("已有配置文件但没有 python3 帮忙合并。把下面这段加进", "The config file exists but python3 is unavailable to merge. Add the following to"),
	"codex_exists": i18n.T("里已经有 [mcp_servers.axiomos]，请把它换成：", "already contains [mcp_servers.axiomos]; replace it with:"),
	"custom":       i18n.T("把下面这段加进你的 MCP 客户端配置：", "Add the following to your MCP client's configuration:"),
	"try_claude":   i18n.T("在 Claude Code 里说「看看我在 AxiomOS 上有什么任务」试试。", "In Claude Code, try: \"What tasks do I have in AxiomOS?\""),
	"try_cursor":   i18n.T("重启 Cursor 后，在对话里让它看看 AxiomOS 上的任务。", "Restart Cursor, then ask it about your tasks in AxiomOS."),
	"try_codex":    i18n.T("重启 Codex 后，让它看看 AxiomOS 上的任务。", "Restart Codex, then ask it about your tasks in AxiomOS."),
	"done":         i18n.T("接入完成。回到网页的接入向导，它会显示这个 Agent 已在线。", "Done. Go back to the web wizard; it will show this agent as online."),
	"checking":     i18n.T("正在做一次连接检查（调用 whoami）……", "Running a connection check (calling whoami)..."),
}

func (s *Server) connectScript(w http.ResponseWriter, r *http.Request) {
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
	if err := connectTmpl.Execute(&buf, map[string]any{"BaseURL": base, "Client": client, "ClientTitle": i18n.Tr(loc, "device.client."+client), "T": t}); err != nil {
		writeErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	_, _ = w.Write(buf.Bytes())
}
