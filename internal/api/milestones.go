package api

import (
	"net/http"
	"time"

	"github.com/teemo/axiomos/internal/app"
)

// 里程碑（ADR 0016）。形状见 docs/api.md「里程碑」。日期一律 YYYY-MM-DD。

type MilestoneV struct {
	ID          string      `json:"id"`
	GoalID      string      `json:"goal_id"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	DueOn       string      `json:"due_on"`
	ReachedAt   *string     `json:"reached_at"`
	Status      string      `json:"status"` // upcoming | reached | overdue
	ReadyHint   bool        `json:"ready_hint"`
	CreatedBy   ExecutorRef `json:"created_by"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

type MilestoneNextV struct {
	Title string `json:"title"`
	DueOn string `json:"due_on"`
}

type MilestoneSummaryV struct {
	Total   int             `json:"total"`
	Reached int             `json:"reached"`
	Overdue int             `json:"overdue"`
	Next    *MilestoneNextV `json:"next"`
}

// GanttMilestoneV 是甘特图目标行上的菱形。
type GanttMilestoneV struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	DueOn  string `json:"due_on"`
	Status string `json:"status"`
}

type MilestoneInputV struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	DueOn       *Date   `json:"due_on"`
}

func milestoneView(m *app.MilestoneView, r refs) MilestoneV {
	due := m.DueOn
	return MilestoneV{ID: m.ID, GoalID: m.GoalID, Title: m.Title, Description: m.Description, DueOn: *dateStr(&due), ReachedAt: timeStr(m.ReachedAt),
		Status: string(m.Status), ReadyHint: m.ReadyHint, CreatedBy: r.must(m.CreatedBy), CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
}

func milestoneViews(ms []*app.MilestoneView, r refs) []MilestoneV {
	out := make([]MilestoneV, 0, len(ms))
	for _, m := range ms {
		out = append(out, milestoneView(m, r))
	}
	return out
}

func milestoneSummaryView(g *app.GoalView) MilestoneSummaryV {
	s := g.MilestoneSummary
	v := MilestoneSummaryV{Total: s.Total, Reached: s.Reached, Overdue: s.Overdue}
	if s.Next != nil {
		due := s.Next.DueOn
		v.Next = &MilestoneNextV{Title: s.Next.Title, DueOn: *dateStr(&due)}
	}
	return v
}

func ganttMilestones(ms []*app.MilestoneView) []GanttMilestoneV {
	out := make([]GanttMilestoneV, 0, len(ms))
	for _, m := range ms {
		due := m.DueOn
		out = append(out, GanttMilestoneV{ID: m.ID, Title: m.Title, DueOn: *dateStr(&due), Status: string(m.Status)})
	}
	return out
}

// ---------- 路由 ----------

func (s *Server) milestoneRoutes(auth func(string, http.HandlerFunc)) {
	auth("GET /api/v1/goals/{id}/milestones", s.milestones)
	auth("POST /api/v1/goals/{id}/milestones", s.createMilestone)
	auth("PATCH /api/v1/milestones/{id}", s.updateMilestone)
	auth("DELETE /api/v1/milestones/{id}", s.deleteMilestone)
	auth("POST /api/v1/milestones/{id}/reach", s.reachMilestone)
	auth("POST /api/v1/milestones/{id}/unreach", s.unreachMilestone)
}

func (s *Server) milestones(w http.ResponseWriter, r *http.Request) {
	ms, err := s.App.ListMilestones(r.Context(), sessionOf(r), r.PathValue("id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	rf, err := s.refsFor(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, milestoneViews(ms, rf))
}

func (s *Server) createMilestone(w http.ResponseWriter, r *http.Request) {
	var in MilestoneInputV
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	ci := app.CreateMilestoneInput{GoalID: r.PathValue("id"), Title: str(in.Title), Description: str(in.Description)}
	if in.DueOn != nil {
		ci.DueOn = in.DueOn.T
	}
	m, err := s.App.CreateMilestone(r.Context(), sessionOf(r), ci)
	s.respondMilestone(w, r, m, err)
}

func (s *Server) updateMilestone(w http.ResponseWriter, r *http.Request) {
	var in MilestoneInputV
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	ui := app.UpdateMilestoneInput{Title: in.Title, Description: in.Description}
	if in.DueOn != nil {
		ui.DueOn = in.DueOn.T
	}
	m, err := s.App.UpdateMilestone(r.Context(), sessionOf(r), r.PathValue("id"), ui)
	s.respondMilestone(w, r, m, err)
}

func (s *Server) deleteMilestone(w http.ResponseWriter, r *http.Request) {
	if err := s.App.DeleteMilestone(r.Context(), sessionOf(r), r.PathValue("id")); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) reachMilestone(w http.ResponseWriter, r *http.Request) {
	m, err := s.App.ReachMilestone(r.Context(), sessionOf(r), r.PathValue("id"))
	s.respondMilestone(w, r, m, err)
}

func (s *Server) unreachMilestone(w http.ResponseWriter, r *http.Request) {
	m, err := s.App.UnreachMilestone(r.Context(), sessionOf(r), r.PathValue("id"))
	s.respondMilestone(w, r, m, err)
}

func (s *Server) respondMilestone(w http.ResponseWriter, r *http.Request, m *app.MilestoneView, err error) {
	if err != nil {
		writeErr(w, r, err)
		return
	}
	rf, err := s.refsFor(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, milestoneView(m, rf))
}
