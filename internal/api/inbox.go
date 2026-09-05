package api

import (
	"net/http"

	"github.com/teemo/axiomos/internal/app"
)

// 待我处理（DESIGN.md §12）：只看本人，不受 scope 影响。三个接口的形状见 docs/api.md。

func (s *Server) inboxRoutes(auth func(string, http.HandlerFunc)) {
	auth("GET /api/v1/inbox", s.inbox)
	auth("GET /api/v1/inbox/count", s.inboxCount)
	auth("POST /api/v1/notifications/read", s.notificationsRead)
}

// InboxTaskV 是任务类组里的一条：任务精简对象加 days_overdue。
type InboxTaskV struct {
	TaskV
	DaysOverdue int `json:"days_overdue"`
}

// InboxQuestionV 是等我答复的一条提问。
type InboxQuestionV struct {
	Task    TaskV    `json:"task"`
	Comment CommentV `json:"comment"`
}

// InboxGroupV 是一组事项；items 的形状按 kind 不同。
type InboxGroupV struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Count int    `json:"count"`
	Items []any  `json:"items"`
}

// InboxV 是整个「待我处理」。全空时 empty 是那句话。
type InboxV struct {
	Count  int           `json:"count"`
	Groups []InboxGroupV `json:"groups"`
	Empty  string        `json:"empty,omitempty"`
}

func (s *Server) inbox(w http.ResponseWriter, r *http.Request) {
	in, err := s.App.Inbox(r.Context(), sessionOf(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	rf, err := s.refsFor(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	gt, st := s.goalTitles(r), s.sprintTitles(r)
	out := InboxV{Count: in.Count, Groups: []InboxGroupV{}, Empty: in.Empty}
	for _, g := range in.Groups {
		gv := InboxGroupV{Kind: g.Kind, Title: g.Title, Count: g.Count, Items: []any{}}
		for _, t := range g.Tasks {
			gv.Items = append(gv.Items, InboxTaskV{TaskV: taskListView(t.Task, rf, gt, st), DaysOverdue: t.DaysOverdue})
		}
		for _, p := range g.Proposals {
			gv.Items = append(gv.Items, proposalView(p))
		}
		for _, q := range g.Questions {
			gv.Items = append(gv.Items, InboxQuestionV{Task: taskListView(q.Task, rf, gt, st),
				Comment: CommentV{ID: q.Comment.ID, Kind: "comment", Author: rf.must(q.Comment.ByID), Body: q.Comment.Text, CreatedAt: q.Comment.CreatedAt}})
		}
		for _, n := range g.Notifications {
			gv.Items = append(gv.Items, n)
		}
		out.Groups = append(out.Groups, gv)
	}
	writeJSON(w, 200, out)
}

func (s *Server) inboxCount(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.InboxCount(r.Context(), sessionOf(r))
	respond(w, r, out, err)
}

func (s *Server) notificationsRead(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	n, err := s.App.MarkNotificationsRead(r.Context(), sessionOf(r), in.IDs)
	respond(w, r, map[string]any{"count": n}, err)
}

// proposalCount 保留 `/proposals/count` 的兼容形状 `{pending}`：成员取「待我处理」里等我确认的条数；
// Agent 没有待我处理，沿用全组织的待确认条数。
func (s *Server) proposalCount(w http.ResponseWriter, r *http.Request) {
	sess := sessionOf(r)
	if sess.IsAgent() {
		n, err := s.App.PendingProposalCount(r.Context(), sess)
		respond(w, r, map[string]any{"pending": n}, err)
		return
	}
	c, err := s.App.InboxCount(r.Context(), sess)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"pending": c.ByKind[app.InboxProposals]})
}
