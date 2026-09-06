package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/teemo/axiomos/internal/app"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 看板与迭代的接口（docs/api.md「看板与迭代」）。

// OptInt 区分"没传"、"传了 null"、"传了数字"。
type OptInt struct {
	Set bool
	V   *int
}

func (o *OptInt) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.V = nil
		return nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	o.V = &n
	return nil
}

// OptFloat 同 OptInt：区分「没带」「带了 null（清空）」「带了值」。
type OptFloat struct {
	Set bool
	V   *float64
}

func (o *OptFloat) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.V = nil
		return nil
	}
	var n float64
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	o.V = &n
	return nil
}

// ---------- 视图 ----------

type TeamRefV struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type SprintRefV struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type SprintV struct {
	ID          string      `json:"id"`
	Team        *TeamRefV   `json:"team"`
	Name        string      `json:"name"`
	Goal        string      `json:"goal"`
	StartsOn    string      `json:"starts_on"`
	EndsOn      string      `json:"ends_on"`
	Status      string      `json:"status"`
	StatusTitle string      `json:"status_title"`
	TaskCount   int         `json:"task_count"`
	PointsTotal int         `json:"points_total"`
	PointsDone  int         `json:"points_done"`
	TasksDone   int         `json:"tasks_done"`
	CreatedBy   ExecutorRef `json:"created_by"`
	StartedAt   *string     `json:"started_at"`
	ClosedAt    *string     `json:"closed_at"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

type VelocityV struct {
	Sprints       []domain.VelocitySample `json:"sprints"`
	AveragePoints float64                 `json:"average_points"`
}

type SprintDetailV struct {
	SprintV
	Tasks    []TaskV         `json:"tasks"`
	Burndown domain.Burndown `json:"burndown"`
	Velocity VelocityV       `json:"velocity"`
}

func sprintView(v *app.SprintView, r refs, loc i18n.Locale) SprintV {
	s := v.Sprint
	out := SprintV{ID: s.ID, Name: s.Name, Goal: s.Goal, StartsOn: s.StartsOn.Format("2006-01-02"), EndsOn: s.EndsOn.Format("2006-01-02"), Status: string(s.Status), StatusTitle: domain.SprintStatusTitle(s.Status, loc),
		TaskCount: v.Stats.TaskCount, PointsTotal: v.Stats.PointsTotal, PointsDone: v.Stats.PointsDone, TasksDone: v.Stats.TasksDone, CreatedBy: r.must(s.CreatedBy), StartedAt: timeStr(s.StartedAt), ClosedAt: timeStr(s.ClosedAt), CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt}
	if s.TeamID != "" {
		out.Team = &TeamRefV{ID: s.TeamID, Title: v.TeamName}
	}
	return out
}

func velocityView(v app.VelocityView) VelocityV {
	out := VelocityV{Sprints: v.Sprints, AveragePoints: v.AveragePoints}
	if out.Sprints == nil {
		out.Sprints = []domain.VelocitySample{}
	}
	return out
}

type SprintInputV struct {
	Name     *string `json:"name"`
	Goal     *string `json:"goal"`
	TeamID   *string `json:"team_id"`
	StartsOn *Date   `json:"starts_on"`
	EndsOn   *Date   `json:"ends_on"`
}

// 看板

type BoardCardV struct {
	TaskV
	CanMoveTo []string      `json:"can_move_to"`
	Moves     []domain.Move `json:"moves"`
	Lane      *string       `json:"lane,omitempty"`
}

type BoardColumnV struct {
	State     TaskState    `json:"state"`
	WIPLimit  *int         `json:"wip_limit,omitempty"`
	Count     int          `json:"count"`
	OverLimit bool         `json:"over_limit"`
	Cards     []BoardCardV `json:"cards"`
}

type BoardTypeRefV struct {
	Name  string `json:"name"`
	Title string `json:"title"`
}

type BoardV struct {
	Type    *BoardTypeRefV  `json:"type"`
	Columns []BoardColumnV  `json:"columns"`
	Lanes   []app.BoardLane `json:"lanes,omitempty"`
}

// ---------- 处理器 ----------

func (s *Server) sprintTitles(r *http.Request) sprintTitleFn {
	m, _ := s.App.SprintNames(r.Context(), sessionOf(r))
	return func(id string) string { return m[id] }
}

func (s *Server) board(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := app.BoardFilter{TypeName: q.Get("type"), GoalID: q.Get("goal"), TeamID: q.Get("team"), AssigneeID: q.Get("assignee"), SprintID: q.Get("sprint"), Lane: q.Get("lane")}
	b, err := s.App.Board(r.Context(), sessionOf(r), f)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	rf, err := s.refsFor(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	loc := sessionOf(r).Loc()
	gt, st := s.goalTitles(r), s.sprintTitles(r)
	out := BoardV{Columns: []BoardColumnV{}, Lanes: b.Lanes}
	if b.Type != nil {
		out.Type = &BoardTypeRefV{Name: b.Type.Name, Title: b.Type.Title.In(loc)}
	}
	for _, c := range b.Columns {
		col := BoardColumnV{State: TaskState{Name: c.State.Name, Title: c.State.Title, Label: c.State.Label, Claimable: c.Claimable}, Count: c.Count, OverLimit: c.OverLimit, Cards: []BoardCardV{}}
		if c.WIPLimit > 0 {
			l := c.WIPLimit
			col.WIPLimit = &l
		}
		for _, card := range c.Cards {
			cv := BoardCardV{TaskV: taskListView(card.TaskSummary, rf, gt, st), CanMoveTo: orEmpty(card.CanMoveTo), Moves: card.Moves}
			if cv.Moves == nil {
				cv.Moves = []domain.Move{}
			}
			if f.Lane == "goal" || f.Lane == "assignee" {
				l := card.Lane
				cv.Lane = &l
			}
			col.Cards = append(col.Cards, cv)
		}
		out.Columns = append(out.Columns, col)
	}
	writeJSON(w, 200, out)
}

func (s *Server) sprints(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.SprintFilter{TeamID: q.Get("team"), Status: domain.SprintStatus(q.Get("status"))}
	if n, _ := strconv.Atoi(q.Get("limit")); n > 0 {
		f.Limit = n
	}
	out, err := s.App.ListSprints(r.Context(), sessionOf(r), f)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	rf, err := s.refsFor(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	views := []SprintV{}
	for _, v := range out {
		views = append(views, sprintView(v, rf, sessionOf(r).Loc()))
	}
	writeJSON(w, 200, views)
}

func (s *Server) sprintByID(r *http.Request, id string) (SprintV, error) {
	d, err := s.App.GetSprint(r.Context(), sessionOf(r), id)
	if err != nil {
		return SprintV{}, err
	}
	rf, err := s.refsFor(r)
	if err != nil {
		return SprintV{}, err
	}
	return sprintView(&d.SprintView, rf, sessionOf(r).Loc()), nil
}

func (s *Server) createSprint(w http.ResponseWriter, r *http.Request) {
	var in SprintInputV
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	ci := app.CreateSprintInput{Name: str(in.Name), Goal: str(in.Goal), TeamID: str(in.TeamID)}
	if in.StartsOn != nil {
		ci.StartsOn = in.StartsOn.T
	}
	if in.EndsOn != nil {
		ci.EndsOn = in.EndsOn.T
	}
	sp, err := s.App.CreateSprint(r.Context(), sessionOf(r), ci)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.sprintByID(r, sp.ID)
	respond(w, r, v, err)
}

func (s *Server) sprint(w http.ResponseWriter, r *http.Request) {
	sess := sessionOf(r)
	d, err := s.App.GetSprint(r.Context(), sess, r.PathValue("id"))
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
	out := SprintDetailV{SprintV: sprintView(&d.SprintView, rf, sess.Loc()), Tasks: []TaskV{}, Burndown: d.Burndown, Velocity: velocityView(d.Velocity)}
	for _, t := range d.Tasks {
		out.Tasks = append(out.Tasks, taskListView(t, rf, gt, st))
	}
	writeJSON(w, 200, out)
}

func (s *Server) updateSprint(w http.ResponseWriter, r *http.Request) {
	var in SprintInputV
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	ui := app.UpdateSprintInput{Name: in.Name, Goal: in.Goal}
	if in.StartsOn != nil {
		ui.StartsOn = in.StartsOn.T
	}
	if in.EndsOn != nil {
		ui.EndsOn = in.EndsOn.T
	}
	if _, err := s.App.UpdateSprint(r.Context(), sessionOf(r), r.PathValue("id"), ui); err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.sprintByID(r, r.PathValue("id"))
	respond(w, r, v, err)
}

func (s *Server) startSprint(w http.ResponseWriter, r *http.Request) {
	if _, err := s.App.StartSprint(r.Context(), sessionOf(r), r.PathValue("id")); err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.sprintByID(r, r.PathValue("id"))
	respond(w, r, v, err)
}

func (s *Server) closeSprint(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Unfinished   string `json:"unfinished"`
		NextSprintID string `json:"next_sprint_id"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	res, err := s.App.CloseSprint(r.Context(), sessionOf(r), r.PathValue("id"), in.Unfinished, in.NextSprintID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.sprintByID(r, r.PathValue("id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"sprint": v, "moved": res.Moved, "returned": res.Returned})
}

func (s *Server) addSprintTasks(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TaskIDs []string `json:"task_ids"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	n, err := s.App.AddTasksToSprint(r.Context(), sessionOf(r), r.PathValue("id"), in.TaskIDs)
	respond(w, r, map[string]any{"added": n}, err)
}

func (s *Server) removeSprintTask(w http.ResponseWriter, r *http.Request) {
	if err := s.App.RemoveTaskFromSprint(r.Context(), sessionOf(r), r.PathValue("id"), r.PathValue("task_id")); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) velocity(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.Velocity(r.Context(), sessionOf(r), r.URL.Query().Get("team"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, velocityView(*v))
}
