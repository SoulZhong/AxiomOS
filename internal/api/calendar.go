package api

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/teemo/axiomos/internal/app"
)

// 日程与外部日历（ADR 0032，docs/api.md「日程」）。组织级连接全部需要 org_settings，权限门在 app 层；
// 成员自己的绑定与日程不用。提供方来自 directory 包的注册表，这一层没有平台名。

func (s *Server) calendarRoutes(auth, pub func(string, http.HandlerFunc)) {
	auth("GET /api/v1/schedule", s.schedule)
	auth("GET /api/v1/org/calendars", s.calendarsGet)
	auth("PUT /api/v1/org/calendars/{provider}", s.calendarPut)
	auth("DELETE /api/v1/org/calendars/{provider}", s.calendarDisconnect)
	auth("POST /api/v1/org/calendars/{provider}/test", s.calendarTest)
	auth("POST /api/v1/org/calendars/{provider}/sync", s.calendarSync)
	auth("GET /api/v1/me/calendars", s.myCalendars)
	auth("DELETE /api/v1/me/calendars/{provider}", s.myCalendarDisconnect)
	// Google 授权回调：浏览器带着 state 与 code 回来，state 认出是谁，不需要会话
	pub("GET /api/v1/me/calendars/google/callback", s.googleCallback)
}

// schedule 取日程（GET /schedule?from=&to=&who=me|<member_id>|<team_id>|<org_id>）。默认本周。
func (s *Server) schedule(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	weekFrom, weekTo := app.CurrentWeek(time.Now())
	from, err := parseDayParam(q.Get("from"), weekFrom)
	if err != nil {
		writeErr(w, r, app.Bad("err.bad_date", q.Get("from")))
		return
	}
	// 只给了 from 时，取从它起的七天；都没给就是本周（周一到周日）
	defTo := weekTo
	if q.Get("from") != "" {
		defTo = from.AddDate(0, 0, 6)
	}
	to, err := parseDayParam(q.Get("to"), defTo)
	if err != nil {
		writeErr(w, r, app.Bad("err.bad_date", q.Get("to")))
		return
	}
	v, err := s.App.ScheduleOf(r.Context(), sessionOf(r), strings.TrimSpace(q.Get("who")), from, to)
	respond(w, r, v, err)
}

func parseDayParam(v string, def time.Time) (time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return def, nil
	}
	return time.ParseInLocation("2006-01-02", v, time.Local)
}

func (s *Server) calendarsGet(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.ListCalendars(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) calendarPut(w http.ResponseWriter, r *http.Request) {
	var in app.CalendarInput
	if err := decode(r, &in); err != nil {
		writeErr(w, r, err)
		return
	}
	in.Provider = r.PathValue("provider")
	v, err := s.App.SaveCalendar(r.Context(), sessionOf(r), in)
	respond(w, r, v, err)
}

func (s *Server) calendarDisconnect(w http.ResponseWriter, r *http.Request) {
	if err := s.App.DisconnectCalendar(r.Context(), sessionOf(r), r.PathValue("provider")); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) calendarTest(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.TestCalendar(r.Context(), sessionOf(r), r.PathValue("provider"))
	respond(w, r, v, err)
}

func (s *Server) calendarSync(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.SyncCalendar(r.Context(), sessionOf(r), r.PathValue("provider"))
	respond(w, r, v, err)
}

func (s *Server) myCalendars(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.MyCalendars(r.Context(), sessionOf(r))
	respond(w, r, v, err)
}

func (s *Server) myCalendarDisconnect(w http.ResponseWriter, r *http.Request) {
	if err := s.App.DisconnectMyCalendar(r.Context(), sessionOf(r), r.PathValue("provider")); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// googleCallback Google 授权回来：换令牌、存绑定，然后把浏览器送回个人设置页；失败也送回去并带上原因。
func (s *Server) googleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	base := strings.TrimRight(s.App.PublicURL, "/") + "/settings/?tab=me"
	if e := q.Get("error"); e != "" {
		http.Redirect(w, r, base+"&calendar=denied", http.StatusSeeOther)
		return
	}
	target, err := s.App.FinishGoogleAuth(r.Context(), q.Get("state"), q.Get("code"))
	if err != nil {
		msg := err.Error()
		if ue, ok := err.(*app.UserError); ok {
			msg = ue.Render(localeOf(r))
		}
		http.Redirect(w, r, base+"&calendar=failed&reason="+url.QueryEscape(msg), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
