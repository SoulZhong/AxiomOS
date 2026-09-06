package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/teemo/axiomos/internal/app"
)

// ---------- 显示偏好（功能规划第 4 项） ----------

func (s *Server) preferenceRoutes(auth func(string, http.HandlerFunc)) {
	auth("GET /api/v1/me/preferences", s.myPreferences)
	auth("PUT /api/v1/me/preferences", s.setMyPreferences)
	auth("DELETE /api/v1/me/preferences", s.clearMyPreferences)
	auth("GET /api/v1/me/preferences/catalog", s.preferenceCatalog)
	auth("GET /api/v1/org/preferences", s.orgPreferences)
	auth("PUT /api/v1/org/preferences/{role}", s.orgPreferencesPut)
	auth("DELETE /api/v1/org/preferences/{role}", s.orgPreferencesDelete)
}

// readRawJSON 读整个请求体作为部分对象（字段解析交给 domain.ParsePreferencePatch）。
func readRawJSON(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		return nil, app.Bad("err.bad_json", err.Error())
	}
	if len(b) > 0 && !json.Valid(b) {
		return nil, app.Bad("err.bad_json", "invalid")
	}
	return b, nil
}

func (s *Server) myPreferences(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.MyPreferences(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) setMyPreferences(w http.ResponseWriter, r *http.Request) {
	raw, err := readRawJSON(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.App.SetMyPreferences(r.Context(), sessionOf(r), raw)
	respond(w, r, v, err)
}

func (s *Server) clearMyPreferences(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.ClearMyPreferences(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) preferenceCatalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.App.PreferenceCatalog(sessionOf(r)))
}

func (s *Server) orgPreferences(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.OrgPreferences(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) orgPreferencesPut(w http.ResponseWriter, r *http.Request) {
	raw, err := readRawJSON(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.App.SetRolePreferences(r.Context(), sessionOf(r), r.PathValue("role"), raw)
	respond(w, r, v, err)
}

func (s *Server) orgPreferencesDelete(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.ClearRolePreferences(r.Context(), sessionOf(r), r.PathValue("role"))
	respond(w, r, v, err)
}
