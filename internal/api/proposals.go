package api

import (
	"net/http"
	"strconv"

	"github.com/teemo/axiomos/internal/app"
	"github.com/teemo/axiomos/internal/domain"
)

// 待确认操作（ADR 0003）：Agent 的授权是「需要人确认」时，写操作不会立即生效，
// 而是返回 202 与一条待确认操作，人在这些接口上确认或拒绝。

func (s *Server) proposalRoutes(auth func(string, http.HandlerFunc)) {
	auth("GET /api/v1/proposals", s.proposals)
	auth("GET /api/v1/proposals/count", s.proposalCount)
	auth("GET /api/v1/proposals/{id}", s.proposal)
	auth("POST /api/v1/proposals/{id}/approve", s.approveProposal)
	auth("POST /api/v1/proposals/{id}/reject", s.rejectProposal)
}

func (s *Server) proposals(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := app.ProposalFilter{Status: domain.ProposalStatus(q.Get("status")), AgentID: q.Get("agent"), Mine: q.Get("mine") == "1"}
	if n, _ := strconv.Atoi(q.Get("limit")); n > 0 {
		f.Limit = n
	}
	out, err := s.App.ListProposals(r.Context(), sessionOf(r), f)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	views := []ProposalV{}
	for _, p := range out {
		views = append(views, proposalView(p))
	}
	writeJSON(w, 200, views)
}

func (s *Server) proposal(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.GetProposal(r.Context(), sessionOf(r), r.PathValue("id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, proposalView(v))
}

func (s *Server) approveProposal(w http.ResponseWriter, r *http.Request) {
	// 目标方案（ADR 0026）可以带选择：跳过哪些键、把全部任务改派给谁；别的动作请求体为空或 {}
	var opts app.ApproveOptions
	if r.ContentLength != 0 {
		if err := decode(r, &opts); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	res, err := s.App.ApproveProposalWith(r.Context(), sessionOf(r), r.PathValue("id"), opts)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"proposal": proposalView(res.Proposal), "result": res.Result})
}

func (s *Server) rejectProposal(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason string `json:"reason"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.App.RejectProposal(r.Context(), sessionOf(r), r.PathValue("id"), in.Reason)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, proposalView(v))
}
