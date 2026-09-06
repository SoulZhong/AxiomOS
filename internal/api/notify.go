package api

import (
	"net/http"
	"strconv"

	"github.com/teemo/axiomos/internal/app"
)

// 通知外发（ADR 0019，docs/api.md「通知外发」）。组织策略需要 org_settings（权限门在 app 层），个人偏好只对成员本人。

func (s *Server) notifyRoutes(auth func(string, http.HandlerFunc)) {
	auth("GET /api/v1/org/notifications", s.orgNotifyGet)
	auth("PUT /api/v1/org/notifications", s.orgNotifyPut)
	auth("POST /api/v1/org/notifications/test", s.orgNotifyTest)
	auth("GET /api/v1/org/notifications/deliveries", s.orgNotifyDeliveries)
	auth("GET /api/v1/me/notifications", s.myNotifyGet)
	auth("PUT /api/v1/me/notifications", s.myNotifyPut)
	auth("DELETE /api/v1/me/notifications", s.myNotifyDelete)
	auth("GET /api/v1/me/notifications/deliveries", s.myNotifyDeliveries)
}

func (s *Server) orgNotifyGet(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.GetNotifyPolicy(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) orgNotifyPut(w http.ResponseWriter, r *http.Request) {
	var in app.NotifyPolicyInput
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.App.SetNotifyPolicy(r.Context(), sessionOf(r), in)
	respond(w, r, v, err)
}

func (s *Server) orgNotifyTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Channel string `json:"channel"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.App.TestNotifyChannel(r.Context(), sessionOf(r), in.Channel)
	respond(w, r, v, err)
}

func (s *Server) orgNotifyDeliveries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	v, err := s.App.OrgDeliveries(r.Context(), sessionOf(r), limit, q.Get("channel"), q.Get("status"))
	respond(w, r, v, err)
}

func (s *Server) myNotifyGet(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.MyNotifications(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) myNotifyPut(w http.ResponseWriter, r *http.Request) {
	raw, err := readRawJSON(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.App.SetMyNotifications(r.Context(), sessionOf(r), raw)
	respond(w, r, v, err)
}

func (s *Server) myNotifyDelete(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.ClearMyNotifications(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) myNotifyDeliveries(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	v, err := s.App.MyDeliveries(r.Context(), sessionOf(r), limit)
	respond(w, r, v, err)
}
