package api

import (
	"net/http"
	"strconv"

	"github.com/teemo/axiomos/internal/app"
)

// 外部目录同步（ADR 0017，docs/api.md「外部目录同步」）。全部需要 org_settings，权限门在 app 层。
// 提供方（飞书、企业微信…）来自 directory 包的注册表，这一层没有平台名。

func (s *Server) directoryRoutes(auth func(string, http.HandlerFunc)) {
	auth("GET /api/v1/org/directory/providers", s.directoryProviders)
	auth("GET /api/v1/org/directory", s.directoryGet)
	auth("PUT /api/v1/org/directory", s.directoryPut)
	auth("DELETE /api/v1/org/directory", s.directoryDisconnect)
	auth("POST /api/v1/org/directory/test", s.directoryTest)
	auth("GET /api/v1/org/directory/checklist", s.directoryChecklist)
	auth("POST /api/v1/org/directory/sync", s.directorySync)
	auth("GET /api/v1/org/directory/preview", s.directoryPreview)
	auth("GET /api/v1/org/directory/runs", s.directoryRuns)
	// 冲突与对应关系（ADR 0017 补记四）
	auth("PUT /api/v1/org/directory/decisions", s.directoryDecisionsPut)
	auth("DELETE /api/v1/org/directory/decisions/{kind}/{external_id}", s.directoryDecisionDelete)
	auth("GET /api/v1/org/directory/mappings", s.directoryMappings)
	auth("DELETE /api/v1/org/directory/bindings/{kind}/{external_id}", s.directoryBindingDelete)
}

func (s *Server) directoryDecisionsPut(w http.ResponseWriter, r *http.Request) {
	var in app.DecisionsInput
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.App.PutDirectoryDecisions(r.Context(), sessionOf(r), in)
	respond(w, r, v, err)
}

func (s *Server) directoryDecisionDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.App.DeleteDirectoryDecision(r.Context(), sessionOf(r), r.PathValue("kind"), r.PathValue("external_id")); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) directoryMappings(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.DirectoryMappings(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) directoryBindingDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.App.UnbindDirectoryIdentity(r.Context(), sessionOf(r), r.PathValue("kind"), r.PathValue("external_id")); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) directoryProviders(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.ListDirectoryProviders(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) directoryGet(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.GetDirectoryConfig(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) directoryDisconnect(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.DisconnectDirectory(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) directoryPut(w http.ResponseWriter, r *http.Request) {
	var in app.DirectoryConfigInput
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.App.SaveDirectoryConfig(r.Context(), sessionOf(r), in)
	respond(w, r, v, err)
}

func (s *Server) directoryTest(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.TestDirectory(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) directoryChecklist(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.DirectoryChecklist(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) directorySync(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.SyncDirectory(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) directoryPreview(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.PreviewDirectory(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) directoryRuns(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	v, err := s.App.DirectoryRuns(r.Context(), sessionOf(r), limit)
	respond(w, r, v, err)
}
