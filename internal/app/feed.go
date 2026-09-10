package app

// 对外订阅源（ADR 0032 补记）：每个成员一条私密的 iCalendar 链接，把他负责的任务（计划起止与截止）、目标、里程碑
// 发布成 VEVENT，让人在自己的日历软件里订阅。只读、按拉取时现算、不含外部日历同步来的会议（那本来就是他日历里的）。
// 链接里的 token 就是密钥：谁拿到都能读，所以能随时换、随时停。

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 订阅源的窗口：过去 30 天到未来 180 天。
const (
	feedBack  = 30 * 24 * time.Hour
	feedAhead = 180 * 24 * time.Hour
)

// MyFeedView 是成员自己看到的订阅源。
type MyFeedView struct {
	Enabled   bool       `json:"enabled"`
	URL       string     `json:"url,omitempty"`        // https 链接（Google、Outlook 用）
	WebcalURL string     `json:"webcal_url,omitempty"` // webcal 链接（Apple 日历点一下就订阅）
	CreatedAt *time.Time `json:"created_at,omitempty"`
}

func (a *App) feedView(f *store.CalendarFeed) *MyFeedView {
	if f == nil {
		return &MyFeedView{}
	}
	u := strings.TrimRight(a.PublicURL, "/") + "/api/v1/feeds/" + f.Token + ".ics"
	at := f.CreatedAt
	return &MyFeedView{Enabled: true, URL: u, WebcalURL: "webcal://" + strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://"), CreatedAt: &at}
}

// MyFeed 看自己的订阅源。
func (a *App) MyFeed(ctx context.Context, sess *Session) (*MyFeedView, error) {
	if sess.IsAgent() {
		return nil, Forbidden("err.calendar_member_only")
	}
	var f *store.CalendarFeed
	err := a.tx(ctx, sess, func(tx pgx.Tx) (err error) {
		f, err = a.Store.CalendarFeedOf(ctx, tx, sess.MemberID)
		if err == store.ErrNotFound {
			f, err = nil, nil
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return a.feedView(f), nil
}

func newFeedToken() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "fd" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)), nil
}

// ResetMyFeed 生成（没有时）或换掉（已有时）自己的订阅源；旧链接立刻失效。
func (a *App) ResetMyFeed(ctx context.Context, sess *Session) (*MyFeedView, error) {
	if sess.IsAgent() {
		return nil, Forbidden("err.calendar_member_only")
	}
	if sess.Write.DryRun {
		return nil, dryRun(sess, i18n.M("will.feed.reset"))
	}
	token, err := newFeedToken()
	if err != nil {
		return nil, err
	}
	var f *store.CalendarFeed
	err = a.tx(ctx, sess, func(tx pgx.Tx) error {
		if old, err := a.Store.CalendarFeedOf(ctx, tx, sess.MemberID); err == nil {
			a.feedCache.Delete(old.Token)
		}
		if err := a.Store.SetCalendarFeed(ctx, tx, sess.OrgID, sess.MemberID, token); err != nil {
			return err
		}
		var err error
		if f, err = a.Store.CalendarFeedOf(ctx, tx, sess.MemberID); err != nil {
			return err
		}
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "CalendarFeedReset", ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{}}})
	})
	if err != nil {
		return nil, err
	}
	return a.feedView(f), nil
}

// DeleteMyFeed 停用自己的订阅源。
func (a *App) DeleteMyFeed(ctx context.Context, sess *Session) error {
	if sess.IsAgent() {
		return Forbidden("err.calendar_member_only")
	}
	if sess.Write.DryRun {
		return dryRun(sess, i18n.M("will.feed.delete"))
	}
	return a.tx(ctx, sess, func(tx pgx.Tx) error {
		f, err := a.Store.CalendarFeedOf(ctx, tx, sess.MemberID)
		if err == store.ErrNotFound {
			return NotFound("err.feed_missing")
		} else if err != nil {
			return err
		}
		if err := a.Store.DeleteCalendarFeed(ctx, tx, sess.MemberID); err != nil {
			return err
		}
		a.feedCache.Delete(f.Token)
		return a.insertEvents(ctx, tx, sess, []domain.Event{{Type: "CalendarFeedRemoved", ActorID: sess.Actor.ID, At: time.Now(), Data: map[string]any{}}})
	})
}

// FeedICS 按 token 出一份 iCalendar（公开接口，不需要会话）：token 认出是谁，日程按他本人的可见范围算。
func (a *App) FeedICS(ctx context.Context, token string) ([]byte, error) {
	token = strings.TrimSuffix(strings.TrimSpace(token), ".ics")
	if token == "" {
		return nil, NotFound("err.feed_missing")
	}
	// 公开接口没有会话：同一 token 五分钟内直接给缓存，链接泄露被反复拉也不会把库拖垮
	if c, ok := a.feedCache.Load(token); ok {
		if fc := c.(feedCached); time.Since(fc.at) < feedCacheTTL {
			return fc.body, nil
		}
	}
	f, err := a.Store.CalendarFeedByToken(ctx, a.Store.Pool, token)
	if err == store.ErrNotFound {
		return nil, NotFound("err.feed_missing")
	} else if err != nil {
		return nil, err
	}
	sess, err := a.memberSession(ctx, f.OrgID, f.MemberID)
	if err != nil {
		return nil, NotFound("err.feed_missing")
	}
	now := time.Now()
	from := now.Add(-feedBack)
	to := now.Add(feedAhead)
	from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.Local)
	to = time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.Local)
	v, err := a.scheduleOf(ctx, sess, sess.MemberID, from, to)
	if err != nil {
		return nil, err
	}
	body := renderFeed(v, sess, strings.TrimRight(a.PublicURL, "/"), now)
	a.feedCache.Store(token, feedCached{body: body, at: now})
	return body, nil
}

// feedCached 是订阅源的一份短缓存。
type feedCached struct {
	body []byte
	at   time.Time
}

const feedCacheTTL = 5 * time.Minute

// SweepFeedCache 清掉过期的订阅源缓存（后台巡检调用）。
func (a *App) SweepFeedCache(now time.Time) {
	a.feedCache.Range(func(k, v any) bool {
		if fc, ok := v.(feedCached); ok && now.Sub(fc.at) > feedCacheTTL {
			a.feedCache.Delete(k)
		}
		return true
	})
}

// renderFeed 把日程项写成 iCalendar：任务与目标是一段（全天，DTEND 不含），截止日与里程碑是一天。
func renderFeed(v *ScheduleView, sess *Session, base string, now time.Time) []byte {
	loc := sess.Loc()
	var b strings.Builder
	line := func(s string) { b.WriteString(foldICS(s)); b.WriteString("\r\n") }
	line("BEGIN:VCALENDAR")
	line("VERSION:2.0")
	line("PRODID:-//AxiomOS//schedule//" + strings.ToUpper(string(loc)))
	line("CALSCALE:GREGORIAN")
	line("METHOD:PUBLISH")
	line("X-WR-CALNAME:" + escapeICS("AxiomOS · "+sess.Actor.Name))
	line("X-PUBLISHED-TTL:PT15M")
	stamp := now.UTC().Format("20060102T150405Z")
	for _, it := range v.Items {
		if it.Kind == "event" {
			continue
		}
		start, end := it.StartDate, it.EndDate
		if start == "" {
			start, end = it.Date, it.Date
		}
		if start == "" {
			continue
		}
		sd, err1 := time.ParseInLocation("2006-01-02", start, time.Local)
		ed, err2 := time.ParseInLocation("2006-01-02", end, time.Local)
		if err1 != nil || err2 != nil {
			continue
		}
		var summary, url string
		switch it.Kind {
		case "task":
			summary = fmt.Sprintf("#%d %s", it.Number, it.Title)
			if it.Deadline {
				summary = i18n.Tr(loc, "feed.deadline") + " " + summary
			}
			url = base + "/tasks/" + it.ID + "/"
		case "goal":
			summary = i18n.Tr(loc, "feed.goal") + " " + it.Title
			url = base + "/goals/" + it.ID + "/"
		case "milestone":
			summary = i18n.Tr(loc, "feed.milestone") + " " + it.Title
			url = base + "/goals/" + it.GoalID + "/"
		default:
			continue
		}
		desc := []string{}
		if it.State != nil {
			desc = append(desc, it.State.Title)
		}
		if it.Kind == "task" && it.Progress > 0 {
			desc = append(desc, fmt.Sprintf("%d%%", it.Progress))
		}
		line("BEGIN:VEVENT")
		line("UID:" + it.Kind + "-" + it.ID + "@axiomos")
		line("DTSTAMP:" + stamp)
		line("DTSTART;VALUE=DATE:" + sd.Format("20060102"))
		line("DTEND;VALUE=DATE:" + ed.AddDate(0, 0, 1).Format("20060102"))
		line("SUMMARY:" + escapeICS(summary))
		if len(desc) > 0 {
			line("DESCRIPTION:" + escapeICS(strings.Join(desc, " · ")))
		}
		line("URL:" + escapeICS(url))
		line("TRANSP:TRANSPARENT")
		line("END:VEVENT")
	}
	line("END:VCALENDAR")
	return []byte(b.String())
}

func escapeICS(s string) string {
	r := strings.NewReplacer("\r", "", `\`, `\\`, ";", `\;`, ",", `\,`, "\n", `\n`)
	return r.Replace(s)
}

// foldICS 按 RFC 5545 每 75 字节折行（在 UTF-8 字符边界上折）。
func foldICS(s string) string {
	const width = 75
	if len(s) <= width {
		return s
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		rl := len(string(r))
		if n+rl > width {
			b.WriteString("\r\n ")
			n = 1
		}
		b.WriteRune(r)
		n += rl
	}
	return b.String()
}
