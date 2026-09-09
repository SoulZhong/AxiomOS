// Package api 是给网页用的 HTTP 接口。所有路径在 /api/v1 下，只通过 app 层操作系统。
// 返回的形状与 web/src/lib/api.ts 一致（见 views.go）。
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/teemo/axiomos/internal/app"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

const sessionCookie = "axiomos_session"

type Server struct {
	App          *app.App
	AllowOrigins []string // 开发时的前端来源，如 http://localhost:3000
	SecureCookie bool
}

type ctxKey int

const sessKey ctxKey = 1

func sessionOf(r *http.Request) *app.Session {
	s, _ := r.Context().Value(sessKey).(*app.Session)
	return s
}

// Handler 组装路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	pub := func(pattern string, h http.HandlerFunc) { mux.HandleFunc(pattern, h) }
	auth := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, s.requireAuth(h)) }

	pub("POST /api/v1/auth/login", s.login)
	pub("POST /api/v1/auth/logout", s.logout)
	auth("GET /api/v1/auth/me", s.me)
	auth("PATCH /api/v1/auth/me", s.updateMe)

	auth("GET /api/v1/goals", s.goals)
	auth("POST /api/v1/goals", s.createGoal)
	auth("PUT /api/v1/goals/horizon", s.goalsHorizonBulk)
	auth("PUT /api/v1/goals/rank", s.goalsRank)
	auth("GET /api/v1/goals/{id}", s.goal)
	auth("PATCH /api/v1/goals/{id}", s.updateGoal)
	auth("DELETE /api/v1/goals/{id}", s.deleteGoal)
	s.milestoneRoutes(auth)

	auth("GET /api/v1/tasks", s.tasks)
	auth("POST /api/v1/tasks", s.createTask)
	auth("GET /api/v1/tasks/mine", s.myTasks)
	auth("GET /api/v1/task-by-number/{n}", s.taskByNumber)
	auth("GET /api/v1/tasks/{id}", s.task)
	auth("PATCH /api/v1/tasks/{id}", s.updateTask)
	auth("GET /api/v1/tasks/{id}/workflow", s.workflow)
	auth("GET /api/v1/tasks/{id}/brief", s.brief)
	auth("POST /api/v1/tasks/{id}/transitions/{name}", s.transition)
	auth("POST /api/v1/tasks/{id}/claim", s.claim)
	auth("POST /api/v1/tasks/{id}/begin", s.begin)
	auth("POST /api/v1/tasks/{id}/assign", s.assign)
	auth("POST /api/v1/tasks/{id}/artifacts", s.artifact)
	auth("POST /api/v1/tasks/{id}/comments", s.comment)
	auth("POST /api/v1/tasks/{id}/relations", s.relation)
	auth("DELETE /api/v1/tasks/{id}/relations/{type}/{other_id}", s.unlink)
	// 委托与撤回（ADR 0028）
	auth("GET /api/v1/tasks/{id}/mandates", s.taskMandates)
	auth("POST /api/v1/tasks/{id}/mandates", s.issueMandate)
	auth("DELETE /api/v1/mandates/{id}", s.revokeMandate)
	auth("POST /api/v1/tasks/{id}/revert", s.revertTask)
	auth("POST /api/v1/tasks/{id}/heartbeat", s.heartbeat)

	auth("GET /api/v1/backlog", s.backlog)
	auth("GET /api/v1/gantt", s.gantt)

	auth("GET /api/v1/board", s.board)
	auth("GET /api/v1/sprints", s.sprints)
	auth("POST /api/v1/sprints", s.createSprint)
	auth("GET /api/v1/sprints/velocity", s.velocity)
	auth("GET /api/v1/sprints/{id}", s.sprint)
	auth("PATCH /api/v1/sprints/{id}", s.updateSprint)
	auth("POST /api/v1/sprints/{id}/start", s.startSprint)
	auth("POST /api/v1/sprints/{id}/close", s.closeSprint)
	auth("POST /api/v1/sprints/{id}/tasks", s.addSprintTasks)
	auth("DELETE /api/v1/sprints/{id}/tasks/{task_id}", s.removeSprintTask)

	auth("GET /api/v1/agents", s.agents)
	auth("POST /api/v1/agents", s.registerAgent)
	auth("PATCH /api/v1/agents/{id}", s.updateAgent)
	auth("DELETE /api/v1/agents/{id}", s.revokeAgent)

	auth("GET /api/v1/members", s.members)
	auth("GET /api/v1/capabilities", s.capabilities)
	auth("GET /api/v1/task-types", s.taskTypes)
	auth("GET /api/v1/task-types/{name}", s.taskType)
	auth("PUT /api/v1/task-types/{name}", s.saveTaskType)

	auth("GET /api/v1/stats/cost", s.statsCost)
	auth("GET /api/v1/stats/cycle", s.statsCycle)
	auth("GET /api/v1/stats/throughput", s.statsThroughput)
	auth("GET /api/v1/stats/agents", s.statsAgents)
	auth("GET /api/v1/stats/overview", s.statsOverview)
	auth("GET /api/v1/stats/exceptions", s.statsExceptions)
	auth("GET /api/v1/stats/load", s.statsLoad)

	s.proposalRoutes(auth)
	s.workspaceRoutes(auth)
	s.preferenceRoutes(auth)
	s.deviceRoutes(auth, pub)
	s.inboxRoutes(auth)
	s.notifyRoutes(auth)
	s.codePlatformRoutes(auth, pub)
	s.calendarRoutes(auth, pub)

	auth("GET /api/v1/events", s.events)
	auth("GET /api/v1/notifications", s.notifications)

	s.orgRoutes(mux, auth, pub)
	s.adminRoutes(mux, pub)

	return s.cors(mux)
}

// ---------- 中间件与工具 ----------

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		for _, o := range s.AllowOrigins {
			if o == origin {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
				w.Header().Set("Vary", "Origin")
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Authenticate 从 Cookie 或 Bearer 令牌解析身份（MCP 也用它）。
func (s *Server) Authenticate(r *http.Request) (*app.Session, error) {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		tok := strings.TrimPrefix(h, "Bearer ")
		if strings.HasPrefix(tok, "axm_") {
			return s.App.SessionFromAgentToken(r.Context(), tok)
		}
		return s.App.SessionFromToken(r.Context(), tok)
	}
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		return s.App.SessionFromToken(r.Context(), c.Value)
	}
	return nil, app.Unauthorized("err.login_required")
}

func (s *Server) requireAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := s.Authenticate(r)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		// 范围（ADR 0013）：所有列表与统计接口都接受 ?scope=，挂在会话上带进 app 层
		if sc := r.URL.Query().Get("scope"); sc != "" {
			sess = sess.WithScope(sc)
		}
		// 只看不做与幂等键（ADR 0025）：写接口接受 ?dry_run=1 与 ?idempotency_key=
		// （幂等键也可以放在 Idempotency-Key 头里）。读接口收到也无妨，它们本来就不写。
		sess = sess.WithWrite(writeOptionsOf(r))
		sess.ClientIP = clientIP(r)
		next(w, r.WithContext(context.WithValue(r.Context(), sessKey, sess)))
	})
}

// clientIP 取请求的来源地址：优先反向代理给的 X-Forwarded-For 第一段，否则 RemoteAddr。
// 只用来给进程内的尝试次数限制分桶，不做鉴权。
func clientIP(r *http.Request) string {
	if h := r.Header.Get("X-Forwarded-For"); h != "" {
		if first, _, _ := strings.Cut(h, ","); strings.TrimSpace(first) != "" {
			return strings.TrimSpace(first)
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// localeOf 取请求语言：已登录按成员设置，否则按 Accept-Language。
func localeOf(r *http.Request) i18n.Locale {
	if s := sessionOf(r); s != nil {
		return s.Loc()
	}
	if l := i18n.ParseAcceptLanguage(r.Header.Get("Accept-Language")); l != "" {
		return l
	}
	return i18n.Default
}

// writeOptionsOf 从请求里取出这次写操作的两个开关。
func writeOptionsOf(r *http.Request) app.WriteOptions {
	q := r.URL.Query()
	o := app.WriteOptions{IdempotencyKey: strings.TrimSpace(q.Get("idempotency_key"))}
	switch strings.ToLower(strings.TrimSpace(q.Get("dry_run"))) {
	case "1", "true", "yes":
		o.DryRun = true
	}
	if o.IdempotencyKey == "" {
		o.IdempotencyKey = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	}
	return o
}

func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	loc := localeOf(r)
	// 只看不做（ADR 0025）：不是错误，是"我将要做什么"的一句话，一行也没写
	if d, ok := app.AsDryRun(err); ok {
		writeJSON(w, 200, map[string]any{"dry_run": true, "will": d.Will})
		return
	}
	// 命中幂等键（ADR 0025）：没有再写一次，返回的是第一次的结果
	if rp, ok := app.AsRepeated(err); ok {
		writeJSON(w, 200, map[string]any{"repeated": true, "message": rp.Message,
			"result": json.RawMessage(rp.Result), "first_result_ref": rp.Ref, "first_called_at": rp.CreatedAt})
		return
	}
	// 命中「需要人确认」的授权：这次写操作没有执行，而是记成了一条待确认操作（ADR 0003）
	if pp, ok := app.AsProposalPending(err); ok {
		writeJSON(w, http.StatusAccepted, map[string]any{"proposal": proposalView(pp.Proposal), "message": pp.Message})
		return
	}
	var ue *app.UserError
	if errors.As(err, &ue) {
		writeJSON(w, ue.Status, errBody(ue.Status, ue.Render(loc)))
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, 404, errBody(404, i18n.Tr(loc, "err.not_found")))
		return
	}
	log.Printf("内部错误: %v", err)
	writeJSON(w, 500, errBody(500, i18n.Tr(loc, "err.internal")))
}

func removeStr(list []string, s string) []string {
	out := list[:0]
	for _, x := range list {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

func decode(r *http.Request, v any) error {
	if r.Body == nil {
		return nil
	}
	if err := json.NewDecoder(r.Body).Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return app.Bad("err.bad_json", err.Error())
	}
	return nil
}

func respond(w http.ResponseWriter, r *http.Request, v any, err error) {
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, v)
}

// refsFor 读取执行者索引。
func (s *Server) refsFor(r *http.Request) (refs, error) {
	idx, err := s.App.ExecutorIndex(r.Context(), sessionOf(r))
	if err != nil {
		return nil, err
	}
	return buildRefs(idx), nil
}

func (s *Server) goalTitles(r *http.Request) goalTitleFn {
	m, _ := s.App.GoalTitles(r.Context(), sessionOf(r))
	return func(id string) string { return m[id] }
}

// ---------- 登录 ----------

func (s *Server) sessionView(r *http.Request, sess *app.Session) (SessionV, error) {
	me, err := s.App.Me(r.Context(), sess)
	if err != nil {
		return SessionV{}, err
	}
	d, err := s.App.Directory(r.Context(), sess)
	if err != nil {
		return SessionV{}, err
	}
	roles, err := s.App.ListRoles(r.Context(), sess)
	if err != nil {
		return SessionV{}, err
	}
	return sessionView(me, d.Emails[me.Member.ID], d.Teams, d.TeamOf, d.Caps, roles, sess.Loc()), nil
}

func (s *Server) updateMe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Locale string `json:"locale"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	sess := sessionOf(r)
	if in.Locale != "" {
		if err := s.App.SetMyLocale(r.Context(), sess, in.Locale); err != nil {
			writeErr(w, r, err)
			return
		}
		sess.Locale = i18n.Normalize(in.Locale)
	}
	v, err := s.sessionView(r, sess)
	respond(w, r, v, err)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Org      string `json:"org"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	token, sess, err := s.App.Login(r.Context(), in.Email, in.Password, in.Org)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: s.SecureCookie, SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(30 * 24 * time.Hour)})
	v, err := s.sessionView(r, sess)
	respond(w, r, v, err)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.App.Logout(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	v, err := s.sessionView(r, sessionOf(r))
	respond(w, r, v, err)
}

// ---------- 目标 ----------

func (s *Server) goalTree(r *http.Request) ([]GoalV, error) {
	// 路线图按时间桶筛选（ADR 0021）：?horizon=now|next|later|none，none 是还没排期
	// 按类型筛选（ADR 0023）：?type=<类型编号>|none，none 是未分类
	tree, err := s.App.GoalTreeFiltered(r.Context(), sessionOf(r), app.GoalFilter{Horizon: r.URL.Query().Get("horizon"), Type: r.URL.Query().Get("type")})
	if err != nil {
		return nil, err
	}
	rf, err := s.refsFor(r)
	if err != nil {
		return nil, err
	}
	tasks, err := s.App.ListTaskSummaries(r.Context(), sessionOf(r), store.TaskFilter{})
	if err != nil {
		return nil, err
	}
	spans := actualSpans(tree, tasks)
	loc := sessionOf(r).Loc()
	out := []GoalV{}
	for _, g := range tree {
		out = append(out, goalView(g, rf, spans, loc))
	}
	return out, nil
}

// goalsHorizonBulk 批量把目标放进同一个时间桶：路线图上拖一次卡片就是一次请求（ADR 0021）。
func (s *Server) goalsHorizonBulk(w http.ResponseWriter, r *http.Request) {
	var in app.BulkGoalHorizonInput
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := s.App.BulkGoalHorizon(r.Context(), sessionOf(r), in)
	respond(w, r, out, err)
}

// goalsRank 按给定顺序重排一串目标：时间线上把一条泳道里的目标上下拖一次就是一次请求（ADR 0022）。
func (s *Server) goalsRank(w http.ResponseWriter, r *http.Request) {
	var in app.GoalRankInput
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := s.App.SetGoalRanks(r.Context(), sessionOf(r), in)
	respond(w, r, out, err)
}

func (s *Server) goals(w http.ResponseWriter, r *http.Request) {
	out, err := s.goalTree(r)
	respond(w, r, out, err)
}

func (s *Server) goalByID(r *http.Request, id string) (GoalV, error) {
	tree, err := s.goalTree(r)
	if err != nil {
		return GoalV{}, err
	}
	var find func(vs []GoalV) *GoalV
	find = func(vs []GoalV) *GoalV {
		for i := range vs {
			if vs[i].ID == id {
				return &vs[i]
			}
			if f := find(vs[i].Children); f != nil {
				return f
			}
		}
		return nil
	}
	if g := find(tree); g != nil {
		return *g, nil
	}
	return GoalV{}, app.NotFound("目标不存在")
}

func (s *Server) createGoal(w http.ResponseWriter, r *http.Request) {
	var in GoalInputV
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	ci := app.CreateGoalInput{ParentID: str(in.ParentID), TeamID: str(in.TeamID), TypeID: str(in.TypeID), OwnerMemberID: str(in.OwnerID), Title: str(in.Title), Description: str(in.Description), Budget: in.Budget.V,
		Horizon: domain.GoalHorizon(str(in.Horizon)), Confidence: domain.GoalConfidence(str(in.Confidence)), Outcome: str(in.Outcome),
		DatePrecision: domain.DatePrecision(str(in.DatePrecision)), Rank: in.Rank.V}
	if in.PlannedStart.Set {
		ci.PlannedStart = in.PlannedStart.T
	}
	if in.PlannedEnd.Set {
		ci.PlannedEnd = in.PlannedEnd.T
	}
	if in.Deadline.Set {
		ci.Deadline = in.Deadline.T
	} else if in.PlannedEnd.Set {
		ci.Deadline = in.PlannedEnd.T
	}
	g, err := s.App.CreateGoal(r.Context(), sessionOf(r), ci)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.goalByID(r, g.ID)
	respond(w, r, v, err)
}

func (s *Server) goal(w http.ResponseWriter, r *http.Request) {
	v, err := s.goalByID(r, r.PathValue("id"))
	respond(w, r, v, err)
}

func (s *Server) deleteGoal(w http.ResponseWriter, r *http.Request) {
	if err := s.App.DeleteGoal(r.Context(), sessionOf(r), r.PathValue("id")); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) updateGoal(w http.ResponseWriter, r *http.Request) {
	var in GoalInputV
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	ui, err := in.toUpdate()
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if _, err := s.App.UpdateGoal(r.Context(), sessionOf(r), r.PathValue("id"), ui); err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.goalByID(r, r.PathValue("id"))
	respond(w, r, v, err)
}

// ---------- 任务 ----------

func (s *Server) taskDetail(r *http.Request, id string) (TaskV, error) {
	sess := sessionOf(r)
	d, err := s.App.GetTaskDetail(r.Context(), sess, id)
	if err != nil {
		return TaskV{}, err
	}
	related, incoming, types, err := s.App.TaskRelationsContext(r.Context(), sess, d.Task)
	if err != nil {
		return TaskV{}, err
	}
	rf, err := s.refsFor(r)
	if err != nil {
		return TaskV{}, err
	}
	return taskView(d, incoming, related, types, rf, s.goalTitles(r), s.sprintTitles(r), sess.Loc()), nil
}

func (s *Server) myExecutorIDs(r *http.Request) []string {
	sess := sessionOf(r)
	ids := []string{sess.Actor.ID, sess.MemberID}
	if me, err := s.App.Me(r.Context(), sess); err == nil {
		for _, ag := range me.Agents {
			ids = append(ids, ag.ID)
		}
	}
	return ids
}

func (s *Server) tasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.TaskFilter{GoalID: q.Get("goal"), TypeName: q.Get("type"), ParentID: q.Get("parent"), SprintID: q.Get("sprint")}
	if q.Get("sprint") == "none" {
		f.SprintID, f.NoSprint = "", true
	}
	if n, _ := strconv.Atoi(q.Get("limit")); n > 0 {
		f.Limit = n
	}
	labelFilter := ""
	if st := q.Get("state"); st != "" {
		if domain.Label(st) == domain.LabelPending || domain.Label(st) == domain.LabelActive || domain.Label(st) == domain.LabelWaiting || domain.Label(st) == domain.LabelTerminalSuccess || domain.Label(st) == domain.LabelTerminalFailure {
			labelFilter = st
		} else {
			f.State = st
		}
	}
	mine := s.myExecutorIDs(r)
	if a := q.Get("assignee"); a == "me" {
		f.Executors = mine
	} else if a != "" {
		f.AssigneeID = a
	}
	out, err := s.App.ListTaskSummaries(r.Context(), sessionOf(r), f)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	rf, err := s.refsFor(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	isMine := func(id string) bool {
		for _, m := range mine {
			if m == id {
				return true
			}
		}
		return false
	}
	gt, st := s.goalTitles(r), s.sprintTitles(r)
	views := []TaskV{}
	for _, t := range out {
		if labelFilter != "" && string(t.State.Label) != labelFilter {
			continue
		}
		if c := q.Get("creator"); c == "me" && !isMine(t.CreatorID) || c != "" && c != "me" && c != t.CreatorID {
			continue
		}
		if c := q.Get("reviewer"); c == "me" && !isMine(t.ReviewerID) || c != "" && c != "me" && c != t.ReviewerID {
			continue
		}
		views = append(views, taskListView(t, rf, gt, st))
	}
	writeJSON(w, 200, views)
}

func (s *Server) myTasks(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.MyTasks(r.Context(), sessionOf(r))
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
	views := []TaskV{}
	for _, t := range out {
		views = append(views, taskListView(t, rf, gt, st))
	}
	writeJSON(w, 200, views)
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var in TaskInputV
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	t, err := s.App.CreateTask(r.Context(), sessionOf(r), in.toCreate())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.taskDetail(r, t.ID)
	respond(w, r, v, err)
}

// task 读任务详情；{id} 也接受序号（123 或 #123）。
func (s *Server) task(w http.ResponseWriter, r *http.Request) {
	id, err := s.App.ResolveTaskRef(r.Context(), sessionOf(r), r.PathValue("id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.taskDetail(r, id)
	respond(w, r, v, err)
}

// taskByNumber 按组织内序号读任务详情。
func (s *Server) taskByNumber(w http.ResponseWriter, r *http.Request) {
	n := strings.TrimPrefix(r.PathValue("n"), "#")
	id, err := s.App.ResolveTaskRef(r.Context(), sessionOf(r), "#"+n)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if id == "#"+n {
		writeErr(w, r, app.NotFound("err.task_number", n))
		return
	}
	v, err := s.taskDetail(r, id)
	respond(w, r, v, err)
}

func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) {
	var in TaskInputV
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	if _, err := s.App.UpdateTask(r.Context(), sessionOf(r), r.PathValue("id"), in.toUpdate()); err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.taskDetail(r, r.PathValue("id"))
	respond(w, r, v, err)
}

func (s *Server) workflow(w http.ResponseWriter, r *http.Request) {
	d, err := s.App.GetTaskDetail(r.Context(), sessionOf(r), r.PathValue("id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, workflowView(d.Workflow, d.Type, sessionOf(r).Loc()))
}

func (s *Server) brief(w http.ResponseWriter, r *http.Request) {
	b, err := s.App.Brief(r.Context(), sessionOf(r), r.PathValue("id"))
	respond(w, r, b, err)
}

func (s *Server) afterTaskCommand(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.taskDetail(r, r.PathValue("id"))
	respond(w, r, v, err)
}

func (s *Server) transition(w http.ResponseWriter, r *http.Request) {
	var p app.TransitionPayload
	if err := decode(r, &p); err != nil {
		writeErr(w, r, err)
		return
	}
	_, err := s.App.Transition(r.Context(), sessionOf(r), r.PathValue("id"), r.PathValue("name"), p)
	s.afterTaskCommand(w, r, err)
}

func (s *Server) claim(w http.ResponseWriter, r *http.Request) {
	_, err := s.App.Claim(r.Context(), sessionOf(r), r.PathValue("id"))
	s.afterTaskCommand(w, r, err)
}

func (s *Server) begin(w http.ResponseWriter, r *http.Request) {
	_, err := s.App.Begin(r.Context(), sessionOf(r), r.PathValue("id"))
	s.afterTaskCommand(w, r, err)
}

func (s *Server) assign(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ExecutorID *string `json:"executor_id"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	_, err := s.App.Assign(r.Context(), sessionOf(r), r.PathValue("id"), str(in.ExecutorID))
	s.afterTaskCommand(w, r, err)
}

func (s *Server) artifact(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Type  string `json:"type"`
		Title string `json:"title"`
		URL   string `json:"url"`
		Ref   string `json:"ref"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	if in.Ref == "" {
		in.Ref = in.URL
	}
	t, err := s.App.AddArtifact(r.Context(), sessionOf(r), r.PathValue("id"), domain.Artifact{Type: in.Type, Title: in.Title, Ref: in.Ref})
	if err != nil {
		writeErr(w, r, err)
		return
	}
	rf, _ := s.refsFor(r)
	a := t.Artifacts[len(t.Artifacts)-1]
	writeJSON(w, 200, ArtifactV{ID: a.ID, Type: a.Type, Title: a.Title, URL: a.Ref, AttachedBy: rf.must(a.ByID), CreatedAt: a.CreatedAt})
}

func (s *Server) comment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Body   string `json:"body"`
		Text   string `json:"text"`
		Kind   string `json:"kind"`
		IsNote bool   `json:"is_note"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	if in.Body == "" {
		in.Body = in.Text
	}
	isNote := in.IsNote || in.Kind == "note"
	t, err := s.App.AddComment(r.Context(), sessionOf(r), r.PathValue("id"), in.Body, isNote)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	rf, _ := s.refsFor(r)
	c := t.Comments[len(t.Comments)-1]
	kind := "comment"
	if c.IsNote {
		kind = "note"
	}
	writeJSON(w, 200, CommentV{ID: c.ID, Kind: kind, Author: rf.must(c.ByID), Body: c.Text, CreatedAt: c.CreatedAt})
}

// unlink 解除本任务指向另一个任务的一条关联（DELETE /tasks/{id}/relations/{type}/{other_id}）。
func (s *Server) unlink(w http.ResponseWriter, r *http.Request) {
	sess := sessionOf(r)
	id, err := s.App.ResolveTaskRef(r.Context(), sess, r.PathValue("id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	other, err := s.App.ResolveTaskRef(r.Context(), sess, r.PathValue("other_id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if _, err := s.App.Unlink(r.Context(), sess, id, relationTypeIn(r.PathValue("type")), other); err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.taskDetail(r, id)
	respond(w, r, v, err)
}

func (s *Server) relation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Type       string `json:"type"`
		FromTaskID string `json:"from_task_id"`
		ToTaskID   string `json:"to_task_id"`
		OtherID    string `json:"other_id"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	id := r.PathValue("id")
	from, to := in.FromTaskID, in.ToTaskID
	if from == "" && to == "" {
		from, to = id, in.OtherID
	}
	if from != id && to != id {
		writeErr(w, r, app.Bad("err.relation_end"))
		return
	}
	typ := relationTypeIn(in.Type)
	fromTask, err := s.App.Link(r.Context(), sessionOf(r), from, typ, to)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	toTask, err := s.App.GetTask(r.Context(), sessionOf(r), to)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ft, _ := s.App.GetTaskType(r.Context(), sessionOf(r), fromTask.TypeName)
	tt, _ := s.App.GetTaskType(r.Context(), sessionOf(r), toTask.TypeName)
	loc := sessionOf(r).Loc()
	writeJSON(w, 200, RelationV{ID: from + ":" + string(typ) + ":" + to, Type: relationTypeOut(typ), From: taskRef(fromTask, ft, loc), To: taskRef(toTask, tt, loc), CreatedAt: time.Now()})
}

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Usage []domain.Usage `json:"usage"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	_, err := s.App.Heartbeat(r.Context(), sessionOf(r), r.PathValue("id"), in.Usage)
	respond(w, r, map[string]any{"ok": true}, err)
}

func (s *Server) backlog(w http.ResponseWriter, r *http.Request) {
	sess := sessionOf(r)
	out, err := s.App.Backlog(r.Context(), sess)
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
	items := []BacklogItemV{}
	for _, t := range out {
		item := BacklogItemV{Task: taskListView(t, rf, gt, st), RequiredRole: nullable(t.RequiredRole), RequiredCapabilities: []string{}, Reasons: []string{}}
		if full, err := s.App.GetTask(r.Context(), sess, t.ID); err == nil {
			item.RequiredCapabilities = orEmpty(full.RequiredCapabilities)
			item.Task.RequiredCapabilities = item.RequiredCapabilities
		}
		if wf, err := s.App.Workflow(r.Context(), sess, t.ID); err == nil {
			item.CanClaim = wf.CanClaim
			item.Reasons = orEmpty(wf.ClaimWhyNot)
		}
		items = append(items, item)
	}
	writeJSON(w, 200, items)
}

func (s *Server) gantt(w http.ResponseWriter, r *http.Request) {
	group := r.URL.Query().Get("group")
	if group == "" {
		group = "goal"
	}
	rows, err := s.App.Gantt(r.Context(), sessionOf(r), group)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	rf, err := s.refsFor(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if from == "" || to == "" {
		var lo, hi *time.Time
		for _, row := range rows {
			lo, hi = minT(lo, row.Start), maxT(hi, row.End)
		}
		now := time.Now()
		if lo == nil {
			l := now.AddDate(0, 0, -14)
			lo = &l
		}
		if hi == nil {
			h := now.AddDate(0, 0, 30)
			hi = &h
		}
		if from == "" {
			from = lo.Format("2006-01-02")
		}
		if to == "" {
			to = hi.Format("2006-01-02")
		}
	}
	writeJSON(w, 200, ganttView(group, rows, rf, from, to))
}

// ---------- Agent ----------

func (s *Server) agents(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.ListAgents(r.Context(), sessionOf(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	rf, err := s.refsFor(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	now := time.Now()
	sess := sessionOf(r)
	states := s.agentStates(r, out...)
	views := []AgentV{}
	for _, a := range out {
		views = append(views, agentView(a, rf, now, sess.CanManageAgent(a), states[a.ID]))
	}
	writeJSON(w, 200, views)
}

// agentStates 取这些 Agent 的状态（CONTEXT.md「Agent 状态」）；算不出来时按「没有执行记录、所有者在职」兜底，
// 这样一行也不会变成空状态。
func (s *Server) agentStates(r *http.Request, agents ...*domain.Agent) map[string]app.AgentStatus {
	sess := sessionOf(r)
	m, err := s.App.AgentStates(r.Context(), sess, agents)
	if err != nil || m == nil {
		m = map[string]app.AgentStatus{}
	}
	now := time.Now()
	for _, ag := range agents {
		if _, ok := m[ag.ID]; !ok {
			m[ag.ID] = app.NewAgentStatus(sess.Loc(), ag, now, 0, true)
		}
	}
	return m
}

func (s *Server) registerAgent(w http.ResponseWriter, r *http.Request) {
	var in AgentInputV
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	ag, token, err := s.App.RegisterAgent(r.Context(), sessionOf(r), in.toApp())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	rf, _ := s.refsFor(r)
	writeJSON(w, 200, map[string]any{"agent": agentView(ag, rf, time.Now(), true, s.agentStates(r, ag)[ag.ID]), "token": token})
}

func (s *Server) updateAgent(w http.ResponseWriter, r *http.Request) {
	var in AgentInputV
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	ag, err := s.App.UpdateAgent(r.Context(), sessionOf(r), r.PathValue("id"), in.toApp())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	rf, _ := s.refsFor(r)
	writeJSON(w, 200, agentView(ag, rf, time.Now(), true, s.agentStates(r, ag)[ag.ID]))
}

func (s *Server) revokeAgent(w http.ResponseWriter, r *http.Request) {
	if err := s.App.RevokeAgent(r.Context(), sessionOf(r), r.PathValue("id")); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- 其他 ----------

func (s *Server) members(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.ListMembers(r.Context(), sessionOf(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	d, err := s.App.Directory(r.Context(), sessionOf(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	views := []MemberV{}
	for _, m := range out {
		views = append(views, memberView(m, d.Emails[m.ID], d.TeamOf))
	}
	writeJSON(w, 200, views)
}

func (s *Server) capabilities(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.Capabilities(r.Context(), sessionOf(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	loc := sessionOf(r).Loc()
	m := map[string]string{}
	for k, t := range out {
		m[k] = t.In(loc)
	}
	writeJSON(w, 200, m)
}

func (s *Server) taskTypes(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.ListTaskTypes(r.Context(), sessionOf(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	views := []TaskTypeV{}
	for _, tt := range out {
		views = append(views, taskTypeView(tt, sessionOf(r).Loc()))
	}
	writeJSON(w, 200, views)
}

func (s *Server) taskType(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.GetTaskType(r.Context(), sessionOf(r), r.PathValue("name"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, taskTypeView(out, sessionOf(r).Loc()))
}

func (s *Server) saveTaskType(w http.ResponseWriter, r *http.Request) {
	var tt domain.TaskType
	if err := decode(r, &tt); err != nil {
		writeErr(w, r, err)
		return
	}
	tt.Name = r.PathValue("name")
	out, err := s.App.SaveTaskType(r.Context(), sessionOf(r), &tt)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, taskTypeView(out, sessionOf(r).Loc()))
}

func (s *Server) statsCost(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.CostStats(r.Context(), sessionOf(r), r.URL.Query().Get("group"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	views := []CostStatV{}
	for _, b := range out {
		views = append(views, CostStatV{Key: b.Key, Title: b.Title, Cost: b.Value, TotalTokens: b.Tokens, RunCount: b.Count})
	}
	writeJSON(w, 200, views)
}

func (s *Server) statsCycle(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.CycleDetails(r.Context(), sessionOf(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	views := []CycleStatV{}
	for _, c := range out {
		views = append(views, CycleStatV{Type: c.TypeName, TypeTitle: c.TypeTitle, Sample: c.Sample, AvgHours: c.AvgHours, MedianHours: c.MedianHours, AvgActiveHours: c.AvgActiveHours})
	}
	writeJSON(w, 200, views)
}

func (s *Server) statsThroughput(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.WeekStats(r.Context(), sessionOf(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	views := []ThroughputStatV{}
	for _, x := range out {
		views = append(views, ThroughputStatV{Week: x.Week, Created: x.Created, Done: x.Done, Terminated: x.Terminated})
	}
	writeJSON(w, 200, views)
}

func (s *Server) statsAgents(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.AgentQualities(r.Context(), sessionOf(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	rf, _ := s.refsFor(r)
	views := []AgentStatV{}
	for _, q := range out {
		v := AgentStatV{Agent: rf.must(q.AgentID), Owner: rf.must(q.OwnerID), Runs: q.Runs, Accepted: q.Accepted, Rejected: q.Rejected, TotalTokens: q.Tokens, Cost: q.Cost}
		if n := q.Accepted + q.Rejected; n > 0 {
			v.SuccessRate = float64(q.Accepted) / float64(n)
			v.RejectRate = float64(q.Rejected) / float64(n)
		}
		views = append(views, v)
	}
	writeJSON(w, 200, views)
}

func (s *Server) statsOverview(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.Overview(r.Context(), sessionOf(r), r.URL.Query().Get("period"))
	respond(w, r, v, err)
}

func (s *Server) statsExceptions(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.Exceptions(r.Context(), sessionOf(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, exceptionsView(v))
}

func (s *Server) statsLoad(w http.ResponseWriter, r *http.Request) {
	rows, err := s.App.Load(r.Context(), sessionOf(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out := []LoadV{}
	for _, x := range rows {
		out = append(out, loadView(x))
	}
	writeJSON(w, 200, out)
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	out, err := s.App.Events(r.Context(), sessionOf(r), r.URL.Query().Get("task"), limit)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	rf, _ := s.refsFor(r)
	tasks, _ := s.App.ListTaskSummaries(r.Context(), sessionOf(r), store.TaskFilter{})
	titles, goalOf, numbers := map[string]string{}, map[string]string{}, map[string]int{}
	for _, t := range tasks {
		titles[t.ID] = t.Title
		goalOf[t.ID] = t.GoalID
		numbers[t.ID] = t.Number
	}
	roles := map[string]i18n.Text{}
	if rs, err := s.App.ListRoles(r.Context(), sessionOf(r)); err == nil {
		for _, ro := range rs {
			roles[ro.Name] = ro.Title
		}
	}
	views := []EventV{}
	for _, e := range out {
		if e.Type == "AgentRevoked" {
			e.Type = "AgentRemoved"
		}
		v := eventView(e, rf, titles, goalOf, roles, sessionOf(r).Loc())
		if n, ok := numbers[e.TaskID]; ok && n > 0 {
			v.TaskNumber = &n
		}
		views = append(views, v)
	}
	writeJSON(w, 200, views)
}

func (s *Server) notifications(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.Notifications(r.Context(), sessionOf(r), r.URL.Query().Get("read") == "1")
	respond(w, r, out, err)
}

// taskMandates 列出任务上的委托（GET /tasks/{id}/mandates）。
func (s *Server) taskMandates(w http.ResponseWriter, r *http.Request) {
	sess := sessionOf(r)
	id, err := s.App.ResolveTaskRef(r.Context(), sess, r.PathValue("id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ms, err := s.App.TaskMandates(r.Context(), sess, id)
	respond(w, r, ms, err)
}

// issueMandate 人把任务委托给它的负责人 Agent（POST /tasks/{id}/mandates）。
func (s *Server) issueMandate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AgentID string `json:"agent_id"`
		app.MandateOptions
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	sess := sessionOf(r)
	id, err := s.App.ResolveTaskRef(r.Context(), sess, r.PathValue("id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	m, err := s.App.IssueMandate(r.Context(), sess, id, in.AgentID, in.MandateOptions)
	respond(w, r, m, err)
}

// revokeMandate 收回委托（DELETE /mandates/{id}，请求体可带 reason）。
func (s *Server) revokeMandate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength != 0 {
		if err := decode(r, &in); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	m, err := s.App.RevokeMandate(r.Context(), sessionOf(r), r.PathValue("id"), in.Reason)
	respond(w, r, m, err)
}

// revertTask 撤回委托内最近一步推进（POST /tasks/{id}/revert，请求体 {reason}）。
func (s *Server) revertTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason string `json:"reason"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	sess := sessionOf(r)
	id, err := s.App.ResolveTaskRef(r.Context(), sess, r.PathValue("id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if _, err := s.App.RevertTask(r.Context(), sess, id, in.Reason); err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.taskDetail(r, id)
	respond(w, r, v, err)
}
