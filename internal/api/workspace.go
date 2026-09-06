package api

import (
	"encoding/json"
	"net/http"

	"github.com/teemo/axiomos/internal/app"
)

// ---------- 工作台（ADR 0015） ----------

func (s *Server) workspaceRoutes(auth func(string, http.HandlerFunc)) {
	auth("GET /api/v1/workspace", s.workspace)
	auth("PUT /api/v1/workspace/me", s.workspaceSetMine)
	auth("DELETE /api/v1/workspace/me", s.workspaceClearMine)
	auth("GET /api/v1/workspace/blocks", s.workspaceBlocks)
	auth("GET /api/v1/org/workspace", s.orgWorkspace)
	auth("PUT /api/v1/org/workspace/{role}", s.orgWorkspacePut)
	auth("DELETE /api/v1/org/workspace/{role}", s.orgWorkspaceDelete)
}

func (s *Server) workspace(w http.ResponseWriter, r *http.Request) {
	ws, err := s.App.MyWorkspace(r.Context(), sessionOf(r))
	respond(w, r, ws, err)
}

// decodeBlocks 把请求里的 blocks 读成 []any：接受 ["my_tasks", ...]、[{key, w, h}, ...]（ADR 0015 补记二）
// 与 [{key, x, y, w, h}, ...]（补记三），形状交给 domain.ParseLayout 判断。没给或 null 时返回 nil。
func decodeBlocks(raw json.RawMessage) ([]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var out []any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, app.Bad("err.block_shape")
	}
	return out, nil
}

func (s *Server) workspaceSetMine(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Blocks json.RawMessage `json:"blocks"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	blocks, err := decodeBlocks(in.Blocks)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ws, err := s.App.SetMyWorkspace(r.Context(), sessionOf(r), blocks)
	respond(w, r, ws, err)
}

func (s *Server) workspaceClearMine(w http.ResponseWriter, r *http.Request) {
	ws, err := s.App.ClearMyWorkspace(r.Context(), sessionOf(r))
	respond(w, r, ws, err)
}

func (s *Server) workspaceBlocks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.App.WorkspaceCatalog(sessionOf(r)))
}

func (s *Server) orgWorkspace(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.OrgWorkspaceLayouts(r.Context(), sessionOf(r))
	respond(w, r, out, err)
}

func (s *Server) orgWorkspacePut(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Blocks json.RawMessage `json:"blocks"`
		Preset string          `json:"preset"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	blocks, err := decodeBlocks(in.Blocks)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := s.App.SetRoleWorkspace(r.Context(), sessionOf(r), r.PathValue("role"), blocks, in.Preset)
	respond(w, r, out, err)
}

func (s *Server) orgWorkspaceDelete(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.ClearRoleWorkspace(r.Context(), sessionOf(r), r.PathValue("role"))
	respond(w, r, out, err)
}
