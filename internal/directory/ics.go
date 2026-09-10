package directory

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

// 日历订阅链接（ADR 0032 补记）：成员把自己日历软件给的私密 ICS / webcal 链接贴进来，
// 我们定期拉这份 iCalendar 文件，解析 VEVENT 成只读副本。不需要组织配置、不需要授权——
// Google（「iCal 格式的私密地址」）、Outlook（「发布日历」）、Apple iCloud、飞书（日历设置里的订阅链接）都给这种链接。
//
// 链接本身就是密钥：加密存在成员绑定里，不回显。出网走 egress.go 的守卫：只许 https（webcal 转成 https）。
// 重复事件按 RRULE 在这里展开成实例（FREQ / INTERVAL / COUNT / UNTIL / BYDAY / BYMONTHDAY / BYMONTH），
// EXDATE 排除、RECURRENCE-ID 覆盖单个实例；不支持的规则按单次事件处理。

// ICSKey 是日历订阅链接提供方的代码名。
const ICSKey = "ics"

func init() {
	Register(Provider{
		Key:   ICSKey,
		Title: i18n.T("日历订阅链接", "Calendar subscription link"),
		CalendarFields: []CredentialField{
			{Key: "url", Title: i18n.T("订阅链接", "Subscription link"), Secret: true, Placeholder: "https://…/basic.ics",
				Hint: i18n.T("日历软件里「订阅 / 导出 / 私密地址」给的 .ics 或 webcal 链接；链接就是密钥，只有你自己能看", "The .ics or webcal link from your calendar app's “subscribe / export / secret address”; the link is the secret and only you can see it")},
		},
		CalendarPrerequisites: []i18n.Text{
			i18n.T("Google 日历：设置 → 该日历 → 「iCal 格式的私密地址」", "Google Calendar: Settings → the calendar → “Secret address in iCal format”"),
			i18n.T("Outlook / Exchange：设置 → 日历 → 共享日历 → 发布日历，取 ICS 链接", "Outlook / Exchange: Settings → Calendar → Shared calendars → Publish a calendar, copy the ICS link"),
			i18n.T("飞书：日历设置 → 该日历 → 订阅链接", "Feishu: Calendar settings → the calendar → Subscription link"),
		},
		Tip:                 GuideStep{Text: i18n.T("链接谁拿到都能看你的日历，别贴到别处；换链接就是换密钥。", "Anyone with the link can read your calendar; do not paste it elsewhere. Replacing the link rotates the secret.")},
		PerMemberCalendar:   true,
		SelfServiceCalendar: true,
		NewCalendar: func(creds map[string]string, opts Options) (Calendar, error) {
			return NewICSCalendar(creds["url"], opts)
		},
	})
}

// ICSCalendar 拉一条订阅链接。
type ICSCalendar struct {
	URL  string
	http *http.Client
	name string
}

// CalendarNamer 可选：拉过一次之后能说出这份日历叫什么（X-WR-CALNAME）。
type CalendarNamer interface {
	CalendarName() string
}

// NormalizeICSURL 把 webcal:// 换成 https://，去掉首尾空白。
func NormalizeICSURL(raw string) string {
	s := strings.TrimSpace(raw)
	low := strings.ToLower(s)
	switch {
	case strings.HasPrefix(low, "webcal://"):
		return "https://" + s[len("webcal://"):]
	case strings.HasPrefix(low, "webcals://"):
		return "https://" + s[len("webcals://"):]
	}
	return s
}

// NewICSCalendar 建客户端：链接先过出网守卫（只许 https；私有化部署放开内网时也许 http）。
func NewICSCalendar(raw string, opts Options) (*ICSCalendar, error) {
	u := NormalizeICSURL(raw)
	if err := CheckEgressURL(u, DefaultEgress()); err != nil {
		return nil, err
	}
	hc, err := newHTTPClient(opts)
	if err != nil {
		return nil, err
	}
	return &ICSCalendar{URL: u, http: hc}, nil
}

// CalendarName 最近一次拉取时文件里写的日历名。
func (c *ICSCalendar) CalendarName() string { return c.name }

// Events 拉整份文件，展开后只留落在区间里的。externalUserID 不用：链接本身就是身份。
func (c *ICSCalendar) Events(ctx context.Context, _ string, from, to time.Time) ([]CalendarEvent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/calendar, */*")
	req.Header.Set("User-Agent", "AxiomOS-calendar/1")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, sanitizeICSErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, &RejectedError{Code: resp.StatusCode, Msg: "HTTP " + strconv.Itoa(resp.StatusCode)}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, err
	}
	cal, err := ParseICS(string(body))
	if err != nil {
		return nil, err
	}
	c.name = cal.Name
	return cal.Instances(from, to), nil
}

// sanitizeICSErr 把网络错误里的链接抹掉：链接是密钥，错误文本会进动态与连接状态，谁都看得到。
// Go 的 http.Client 把每个失败都包成 *url.Error，Error() 里带完整地址；出网守卫的拒绝也可能带地址。
func sanitizeICSErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	var eg *EgressError
	if errors.As(err, &eg) {
		return &UnreachableError{Err: &EgressError{Reason: eg.Reason, Host: eg.Host}}
	}
	msg := err.Error()
	if strings.Contains(msg, "://") {
		msg = "network error"
	}
	return &UnreachableError{Err: errors.New(msg)}
}

// ---------- 解析 ----------

// ICSFile 是解析后的一份 iCalendar：日历名与全部 VEVENT。
type ICSFile struct {
	Name   string
	Events []ICSEvent
}

// ICSEvent 是一条 VEVENT（还没展开重复）。
type ICSEvent struct {
	UID          string
	Summary      string
	URL          string
	Start        time.Time
	End          time.Time
	AllDay       bool
	Busy         bool
	Cancelled    bool
	RRule        string
	ExDates      []time.Time
	RecurrenceID *time.Time // 非空：这条是对同 UID 某个实例的覆盖
}

// ErrNotICS 表示内容不是 iCalendar。
var ErrNotICS = fmt.Errorf("not an iCalendar file")

type icsProp struct {
	name   string
	params map[string]string
	value  string
}

// ParseICS 解析文本：先把折行拼回去，再按 BEGIN/END 切出 VEVENT。
func ParseICS(text string) (*ICSFile, error) {
	lines := unfoldICS(text)
	if len(lines) == 0 || !strings.HasPrefix(strings.ToUpper(lines[0]), "BEGIN:VCALENDAR") {
		return nil, ErrNotICS
	}
	out := &ICSFile{}
	var cur *ICSEvent
	depth := 0 // VEVENT 里可能嵌 VALARM，要跳过
	for _, ln := range lines {
		p, ok := parseICSProp(ln)
		if !ok {
			continue
		}
		switch p.name {
		case "BEGIN":
			if strings.EqualFold(p.value, "VEVENT") && cur == nil {
				cur = &ICSEvent{Busy: true}
			} else if cur != nil {
				depth++
			}
			continue
		case "END":
			if cur != nil && depth > 0 {
				depth--
				continue
			}
			if strings.EqualFold(p.value, "VEVENT") && cur != nil {
				if !cur.Start.IsZero() && cur.UID != "" {
					if cur.End.IsZero() {
						if cur.AllDay {
							cur.End = cur.Start.AddDate(0, 0, 1)
						} else {
							cur.End = cur.Start
						}
					}
					out.Events = append(out.Events, *cur)
				}
				cur = nil
			}
			continue
		}
		if cur == nil {
			if p.name == "X-WR-CALNAME" {
				out.Name = p.value
			}
			continue
		}
		if depth > 0 {
			continue
		}
		switch p.name {
		case "UID":
			cur.UID = p.value
		case "SUMMARY":
			cur.Summary = p.value
		case "URL":
			cur.URL = strings.TrimSpace(p.value)
		case "DTSTART":
			if t, allDay, ok := parseICSTime(p); ok {
				cur.Start, cur.AllDay = t, allDay
			}
		case "DTEND":
			if t, _, ok := parseICSTime(p); ok {
				cur.End = t
			}
		case "DURATION":
			if cur.End.IsZero() && !cur.Start.IsZero() {
				if d, ok := parseICSDuration(p.value); ok {
					cur.End = cur.Start.Add(d)
				}
			}
		case "RRULE":
			cur.RRule = p.value
		case "EXDATE":
			for _, v := range strings.Split(p.value, ",") {
				if t, _, ok := parseICSTime(icsProp{name: "EXDATE", params: p.params, value: strings.TrimSpace(v)}); ok {
					cur.ExDates = append(cur.ExDates, t)
				}
			}
		case "RECURRENCE-ID":
			if t, _, ok := parseICSTime(p); ok {
				cur.RecurrenceID = &t
			}
		case "TRANSP":
			if strings.EqualFold(p.value, "TRANSPARENT") {
				cur.Busy = false
			}
		case "X-MICROSOFT-CDO-BUSYSTATUS":
			if strings.EqualFold(p.value, "FREE") {
				cur.Busy = false
			}
		case "STATUS":
			if strings.EqualFold(p.value, "CANCELLED") {
				cur.Cancelled = true
			}
		}
	}
	// DURATION 可能出现在 DTSTART 之前：补一遍
	for i := range out.Events {
		if out.Events[i].End.IsZero() {
			out.Events[i].End = out.Events[i].Start
		}
	}
	return out, nil
}

// unfoldICS 把以空格 / 制表符开头的续行接回上一行，顺便去掉 CR。
func unfoldICS(text string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		ln := strings.TrimRight(sc.Text(), "\r")
		if (strings.HasPrefix(ln, " ") || strings.HasPrefix(ln, "\t")) && len(out) > 0 {
			out[len(out)-1] += ln[1:]
			continue
		}
		out = append(out, ln)
	}
	if sc.Err() != nil {
		return nil // 一行超过 4 MB 或读不下去：当不是 ICS，别拿截断的文件去覆盖副本
	}
	if len(out) > 0 {
		out[0] = strings.TrimPrefix(out[0], "\ufeff") // 文件开头的 BOM
	}
	return out
}

// parseICSProp 拆 NAME;PARAM=VALUE;PARAM2="a,b":value。
func parseICSProp(ln string) (icsProp, bool) {
	// 冒号可能出现在引号里的参数值中，逐字符找第一个不在引号里的冒号
	inQuote := false
	colon := -1
	for i, r := range ln {
		switch r {
		case '"':
			inQuote = !inQuote
		case ':':
			if !inQuote {
				colon = i
			}
		}
		if colon >= 0 {
			break
		}
	}
	if colon < 0 {
		return icsProp{}, false
	}
	head, value := ln[:colon], ln[colon+1:]
	parts := strings.Split(head, ";")
	p := icsProp{name: strings.ToUpper(strings.TrimSpace(parts[0])), params: map[string]string{}, value: unescapeICS(value)}
	for _, kv := range parts[1:] {
		k, v, _ := strings.Cut(kv, "=")
		p.params[strings.ToUpper(strings.TrimSpace(k))] = strings.Trim(strings.TrimSpace(v), `"`)
	}
	if p.name == "" {
		return icsProp{}, false
	}
	return p, true
}

func unescapeICS(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n', 'N':
				b.WriteByte('\n')
			default:
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// icsLocation 按 TZID 找时区；认不出的按本地时区处理（Windows 名字先换成 IANA）。
func icsLocation(tzid string) *time.Location {
	tzid = strings.TrimSpace(tzid)
	if tzid == "" {
		return time.Local
	}
	if loc, err := time.LoadLocation(tzid); err == nil {
		return loc
	}
	if iana, ok := windowsTZ[tzid]; ok {
		if loc, err := time.LoadLocation(iana); err == nil {
			return loc
		}
	}
	// 形如 "(UTC+08:00) Beijing" 或 "GMT+8" 的自定义名字：取不出就当本地
	return time.Local
}

var windowsTZ = map[string]string{
	"China Standard Time":          "Asia/Shanghai",
	"Taipei Standard Time":         "Asia/Taipei",
	"Tokyo Standard Time":          "Asia/Tokyo",
	"Korea Standard Time":          "Asia/Seoul",
	"Singapore Standard Time":      "Asia/Singapore",
	"SE Asia Standard Time":        "Asia/Bangkok",
	"India Standard Time":          "Asia/Kolkata",
	"GMT Standard Time":            "Europe/London",
	"W. Europe Standard Time":      "Europe/Berlin",
	"Romance Standard Time":        "Europe/Paris",
	"Central Europe Standard Time": "Europe/Prague",
	"E. Europe Standard Time":      "Europe/Bucharest",
	"Russian Standard Time":        "Europe/Moscow",
	"Eastern Standard Time":        "America/New_York",
	"Central Standard Time":        "America/Chicago",
	"Mountain Standard Time":       "America/Denver",
	"Pacific Standard Time":        "America/Los_Angeles",
	"AUS Eastern Standard Time":    "Australia/Sydney",
	"UTC":                          "UTC",
}

// parseICSTime 读日期时间：20260910T090000Z / 20260910T090000（浮动，按 TZID 或本地）/ VALUE=DATE:20260910。
// 返回 时间、是否全天、是否读成功。
func parseICSTime(p icsProp) (time.Time, bool, bool) {
	v := strings.TrimSpace(p.value)
	if strings.EqualFold(p.params["VALUE"], "DATE") || (len(v) == 8 && !strings.Contains(v, "T")) {
		t, err := time.ParseInLocation("20060102", v, time.Local)
		return t, true, err == nil
	}
	if strings.HasSuffix(v, "Z") {
		t, err := time.Parse("20060102T150405Z", v)
		return t.In(time.Local), false, err == nil
	}
	t, err := time.ParseInLocation("20060102T150405", v, icsLocation(p.params["TZID"]))
	return t, false, err == nil
}

// parseICSDuration 读 P1D / PT1H30M / P1W / -PT15M。
func parseICSDuration(s string) (time.Duration, bool) {
	s = strings.ToUpper(strings.TrimSpace(s))
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimLeft(s, "+-")
	if !strings.HasPrefix(s, "P") {
		return 0, false
	}
	s = s[1:]
	var d time.Duration
	num := ""
	inTime := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			num += string(r)
		case r == 'T':
			inTime = true
		default:
			n, err := strconv.Atoi(num)
			if err != nil {
				return 0, false
			}
			num = ""
			switch r {
			case 'W':
				d += time.Duration(n) * 7 * 24 * time.Hour
			case 'D':
				d += time.Duration(n) * 24 * time.Hour
			case 'H':
				d += time.Duration(n) * time.Hour
			case 'M':
				if inTime {
					d += time.Duration(n) * time.Minute
				} else {
					d += time.Duration(n) * 30 * 24 * time.Hour
				}
			case 'S':
				d += time.Duration(n) * time.Second
			default:
				return 0, false
			}
		}
	}
	if neg {
		d = -d
	}
	return d, true
}

// ---------- 展开 ----------

// Instances 把全部事件展开成落在 [from, to) 里的实例：重复的按 RRULE 生成，EXDATE 排除，RECURRENCE-ID 覆盖。
func (f *ICSFile) Instances(from, to time.Time) []CalendarEvent {
	overrides := map[string]map[int64]ICSEvent{}
	for _, e := range f.Events {
		if e.RecurrenceID != nil {
			if overrides[e.UID] == nil {
				overrides[e.UID] = map[int64]ICSEvent{}
			}
			overrides[e.UID][e.RecurrenceID.Unix()] = e
		}
	}
	var out []CalendarEvent
	emit := func(e ICSEvent, start, end time.Time, id string) {
		if e.Cancelled || !end.After(from) || !start.Before(to) {
			return
		}
		out = append(out, CalendarEvent{ExternalID: id, Title: e.Summary, Start: start, End: end, AllDay: e.AllDay, Busy: e.Busy, URL: e.URL})
	}
	for _, e := range f.Events {
		if e.RecurrenceID != nil {
			continue
		}
		if e.RRule == "" {
			emit(e, e.Start, e.End, e.UID)
			// 没有规则却带覆盖实例（有的导出把规则丢了）：覆盖实例照单次画
			for key, o := range overrides[e.UID] {
				emit(o, o.Start, o.End, e.UID+"_"+time.Unix(key, 0).UTC().Format("20060102T150405Z"))
			}
			continue
		}
		dur := e.End.Sub(e.Start)
		ex := map[int64]bool{}
		for _, x := range e.ExDates {
			ex[x.Unix()] = true
			if e.AllDay {
				ex[time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, e.Start.Location()).Unix()] = true
			}
		}
		used := map[int64]bool{}
		for _, s := range expandRRule(e.RRule, e.Start, from.Add(-dur), to) {
			key := s.Unix()
			if o, ok := overrides[e.UID][key]; ok {
				used[key] = true
				emit(o, o.Start, o.End, e.UID+"_"+s.UTC().Format("20060102T150405Z"))
				continue
			}
			if ex[key] {
				continue
			}
			emit(e, s, s.Add(dur), e.UID+"_"+s.UTC().Format("20060102T150405Z"))
		}
		// 挪到规则之外的覆盖实例（改了日期的那种）也要出现
		for key, o := range overrides[e.UID] {
			if !used[key] {
				emit(o, o.Start, o.End, e.UID+"_"+time.Unix(key, 0).UTC().Format("20060102T150405Z"))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Start.Equal(out[j].Start) {
			return out[i].Start.Before(out[j].Start)
		}
		return out[i].ExternalID < out[j].ExternalID
	})
	return out
}

type rrule struct {
	freq       string
	interval   int
	count      int
	until      time.Time
	byDay      []rDay // 周几，可带序号（2TU、-1FR）
	byMonthDay []int
	byMonth    []int
}

type rDay struct {
	wd  time.Weekday
	ord int
}

var icsWeekdays = map[string]time.Weekday{"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday, "TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday}

func parseRRule(s string) (rrule, bool) {
	r := rrule{interval: 1}
	for _, kv := range strings.Split(s, ";") {
		k, v, _ := strings.Cut(strings.TrimSpace(kv), "=")
		k, v = strings.ToUpper(k), strings.ToUpper(strings.TrimSpace(v))
		switch k {
		case "BYSETPOS", "BYHOUR", "BYMINUTE", "BYSECOND", "BYWEEKNO", "BYYEARDAY":
			// 不会算的规则：宁可只画起点那一次，也别把人画成天天忙（Outlook 的「每月最后一个工作日」就是 BYSETPOS）
			return r, false
		case "FREQ":
			r.freq = v
		case "INTERVAL":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				r.interval = n
			}
		case "COUNT":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				r.count = n
			}
		case "UNTIL":
			if t, _, ok := parseICSTime(icsProp{value: v, params: map[string]string{}}); ok {
				r.until = t
				if len(v) == 8 {
					r.until = t.AddDate(0, 0, 1).Add(-time.Second) // 只有日期时含当天
				}
			}
		case "BYDAY":
			for _, d := range strings.Split(v, ",") {
				d = strings.TrimSpace(d)
				if len(d) < 2 {
					continue
				}
				wd, ok := icsWeekdays[d[len(d)-2:]]
				if !ok {
					continue
				}
				ord := 0
				if len(d) > 2 {
					ord, _ = strconv.Atoi(d[:len(d)-2])
				}
				r.byDay = append(r.byDay, rDay{wd: wd, ord: ord})
			}
		case "BYMONTHDAY":
			for _, d := range strings.Split(v, ",") {
				if n, err := strconv.Atoi(strings.TrimSpace(d)); err == nil && n != 0 {
					r.byMonthDay = append(r.byMonthDay, n)
				}
			}
		case "BYMONTH":
			for _, d := range strings.Split(v, ",") {
				if n, err := strconv.Atoi(strings.TrimSpace(d)); err == nil && n >= 1 && n <= 12 {
					r.byMonth = append(r.byMonth, n)
				}
			}
		}
	}
	switch r.freq {
	case "DAILY", "WEEKLY", "MONTHLY", "YEARLY":
		return r, true
	}
	return r, false
}

const (
	rruleMaxInstances = 2000   // 窗口里最多返回这么多实例
	rruleMaxSteps     = 200000 // 从 DTSTART 起最多看这么多候选（几十年的每日规则也够）
)

// expandRRule 从 start 起按规则生成实例的开始时间，直到 COUNT / UNTIL / 越过 horizon；只返回不早于 since 的那些
// （COUNT 仍从 DTSTART 数起，几年前建的每日站会照样能算到今天）。第一个实例永远是 DTSTART 本身（RFC 5545 的规矩），之后按 BY* 筛。
func expandRRule(rule string, start, since, horizon time.Time) []time.Time {
	r, ok := parseRRule(rule)
	if !ok {
		return []time.Time{start}
	}
	loc := start.Location()
	h, m, s := start.Clock()
	at := func(y int, mo time.Month, d int) time.Time { return time.Date(y, mo, d, h, m, s, 0, loc) }
	var out []time.Time
	seen := 0 // 规则实例的个数（含窗口之前的），给 COUNT 用
	var last time.Time
	steps := 0 // 候选的个数，硬上限防死循环
	push := func(t time.Time) bool {
		steps++
		if steps > rruleMaxSteps {
			return false
		}
		if t.Before(start) {
			return true
		}
		if !r.until.IsZero() && t.After(r.until) {
			return false
		}
		if seen > 0 && !t.After(last) {
			return true
		}
		seen++
		last = t
		if !t.Before(since) {
			out = append(out, t)
		}
		if r.count > 0 && seen >= r.count {
			return false
		}
		return len(out) < rruleMaxInstances && t.Before(horizon)
	}
	monthOK := func(t time.Time) bool {
		if len(r.byMonth) == 0 {
			return true
		}
		for _, mo := range r.byMonth {
			if int(t.Month()) == mo {
				return true
			}
		}
		return false
	}
	dayOK := func(t time.Time) bool {
		if len(r.byDay) == 0 {
			return true
		}
		for _, d := range r.byDay {
			if d.wd == t.Weekday() {
				return true
			}
		}
		return false
	}
	// 一个月里按 BYDAY / BYMONTHDAY 选出的日子（升序）
	daysInMonth := func(y int, mo time.Month) []time.Time {
		last := time.Date(y, mo+1, 0, 0, 0, 0, 0, loc).Day()
		var days []time.Time
		switch {
		case len(r.byMonthDay) > 0:
			for _, md := range r.byMonthDay {
				d := md
				if d < 0 {
					d = last + 1 + md
				}
				if d >= 1 && d <= last {
					days = append(days, at(y, mo, d))
				}
			}
		case len(r.byDay) > 0:
			for _, bd := range r.byDay {
				var hits []time.Time
				for d := 1; d <= last; d++ {
					if t := at(y, mo, d); t.Weekday() == bd.wd {
						hits = append(hits, t)
					}
				}
				switch {
				case bd.ord == 0:
					days = append(days, hits...)
				case bd.ord > 0 && bd.ord <= len(hits):
					days = append(days, hits[bd.ord-1])
				case bd.ord < 0 && -bd.ord <= len(hits):
					days = append(days, hits[len(hits)+bd.ord])
				}
			}
		default:
			if start.Day() <= last {
				days = append(days, at(y, mo, start.Day()))
			}
		}
		sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
		return days
	}

	seen, last = 1, start
	if !start.Before(since) {
		out = append(out, start)
	}
	if r.count == 1 || (!r.until.IsZero() && start.After(r.until)) {
		return out
	}
	switch r.freq {
	case "DAILY":
		for i := r.interval; i < rruleMaxInstances*r.interval*7; i += r.interval {
			t := start.AddDate(0, 0, i)
			if !monthOK(t) || !dayOK(t) {
				if t.After(horizon) {
					break
				}
				continue
			}
			if !push(t) {
				break
			}
		}
	case "WEEKLY":
		days := r.byDay
		if len(days) == 0 {
			days = []rDay{{wd: start.Weekday()}}
		}
		// 周一为一周之始
		offset := (int(start.Weekday()) + 6) % 7
		weekStart := start.AddDate(0, 0, -offset)
	weekly:
		for w := 0; w < rruleMaxInstances*7; w += r.interval {
			base := weekStart.AddDate(0, 0, 7*w)
			var cands []time.Time
			for _, d := range days {
				off := (int(d.wd) + 6) % 7
				cands = append(cands, at(base.Year(), base.Month(), base.Day()+off))
			}
			sort.Slice(cands, func(i, j int) bool { return cands[i].Before(cands[j]) })
			for _, t := range cands {
				if !monthOK(t) {
					continue
				}
				if !push(t) {
					break weekly
				}
			}
			if base.After(horizon) {
				break
			}
		}
	case "MONTHLY":
	monthly:
		for i := 0; i < rruleMaxInstances*12; i += r.interval {
			first := time.Date(start.Year(), start.Month()+time.Month(i), 1, 0, 0, 0, 0, loc)
			if !monthOK(first) {
				if first.After(horizon) {
					break
				}
				continue
			}
			for _, t := range daysInMonth(first.Year(), first.Month()) {
				if !push(t) {
					break monthly
				}
			}
			if first.After(horizon) {
				break
			}
		}
	case "YEARLY":
	yearly:
		for i := 0; i < rruleMaxInstances; i += r.interval {
			y := start.Year() + i
			months := []time.Month{start.Month()}
			if len(r.byMonth) > 0 {
				months = months[:0]
				for _, mo := range r.byMonth {
					months = append(months, time.Month(mo))
				}
			}
			for _, mo := range months {
				var days []time.Time
				if len(r.byDay) > 0 || len(r.byMonthDay) > 0 {
					days = daysInMonth(y, mo)
				} else {
					last := time.Date(y, mo+1, 0, 0, 0, 0, 0, loc).Day()
					if start.Day() <= last {
						days = []time.Time{at(y, mo, start.Day())}
					}
				}
				for _, t := range days {
					if !push(t) {
						break yearly
					}
				}
			}
			if time.Date(y, 1, 1, 0, 0, 0, 0, loc).After(horizon) {
				break
			}
		}
	}
	return out
}
