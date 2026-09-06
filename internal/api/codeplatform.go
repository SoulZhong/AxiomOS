package api

import (
	"io"
	"net/http"

	"github.com/teemo/axiomos/internal/app"
)

// 代码平台与外部事件（ADR 0020，docs/api.md「代码平台与外部事件」）。
// 组织配置需要 org_settings（权限门在 app 层）；回调入口是公开的，靠组织自己的签名密钥验真。
// 提供方（GitHub、GitLab、Gitee）来自 directory 包的注册表，这一层没有平台名。

func (s *Server) codePlatformRoutes(auth func(string, http.HandlerFunc), pub func(string, http.HandlerFunc)) {
	auth("GET /api/v1/org/code-platform", s.codePlatformGet)
	auth("PUT /api/v1/org/code-platform", s.codePlatformPut)
	auth("DELETE /api/v1/org/code-platform", s.codePlatformDisconnect)
	auth("POST /api/v1/org/code-platform/test", s.codePlatformTest)
	auth("GET /api/v1/org/code-platform/checklist", s.codePlatformChecklist)
	auth("GET /api/v1/org/code-platform/repos", s.codePlatformRepos)
	auth("GET /api/v1/me/code-identity", s.codeIdentityGet)
	auth("PUT /api/v1/me/code-identity", s.codeIdentityPut)
	auth("GET /api/v1/tasks/{id}/links", s.taskLinks)
	auth("POST /api/v1/tasks/{id}/links", s.taskLinkAdd)
	auth("DELETE /api/v1/tasks/{id}/links/{link_id}", s.taskLinkDelete)
	pub("POST /api/v1/hooks/code/{provider}/{org_id}", s.codeWebhook)
}

// codePlatformGet 读配置。回调密钥平时不返回，只在显式要看的那一次（?reveal=secret）带上，
// 那一次不许缓存，并在动态里记一条「查看了回调密钥」。
func (s *Server) codePlatformGet(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("reveal") == "secret" {
		v, err := s.App.RevealCodeWebhookSecret(r.Context(), sessionOf(r))
		if err == nil {
			w.Header().Set("Cache-Control", "no-store")
		}
		respond(w, r, v, err)
		return
	}
	v, err := s.App.GetCodePlatform(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) codePlatformDisconnect(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.DisconnectCodePlatform(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) codePlatformPut(w http.ResponseWriter, r *http.Request) {
	var in app.CodePlatformInput
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.App.SaveCodePlatform(r.Context(), sessionOf(r), in)
	respond(w, r, v, err)
}

func (s *Server) codePlatformTest(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.TestCodePlatform(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) codePlatformChecklist(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.CodePlatformChecklist(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) codePlatformRepos(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.CodePlatformRepos(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) codeIdentityGet(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.MyCodeIdentity(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) codeIdentityPut(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Login string `json:"login"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.App.BindMyCodeIdentity(r.Context(), sessionOf(r), in.Login)
	respond(w, r, v, err)
}

func (s *Server) taskLinks(w http.ResponseWriter, r *http.Request) {
	id, err := s.App.ResolveTaskRef(r.Context(), sessionOf(r), r.PathValue("id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.App.TaskLinks(r.Context(), sessionOf(r), id)
	respond(w, r, v, err)
}

func (s *Server) taskLinkAdd(w http.ResponseWriter, r *http.Request) {
	var in app.LinkInput
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	id, err := s.App.ResolveTaskRef(r.Context(), sessionOf(r), r.PathValue("id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.App.AddTaskLink(r.Context(), sessionOf(r), id, in)
	respond(w, r, v, err)
}

func (s *Server) taskLinkDelete(w http.ResponseWriter, r *http.Request) {
	id, err := s.App.ResolveTaskRef(r.Context(), sessionOf(r), r.PathValue("id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.App.RemoveTaskLink(r.Context(), sessionOf(r), id, r.PathValue("link_id")); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// codeWebhook 是代码平台的回调入口（公开）：验签后立刻返回 200，代码平台不用等我们做完。
// 签名不对返回 401；组织没配这个平台返回 404。
func (s *Server) codeWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		writeErr(w, r, app.Bad("err.bad_json", err.Error()))
		return
	}
	res, err := s.App.HandleCodeWebhook(r.Context(), r.PathValue("provider"), r.PathValue("org_id"), r.Header, body)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
