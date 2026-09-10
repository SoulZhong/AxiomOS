package directory

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const sampleICS = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\nX-WR-CALNAME:我的日历\r\n" +
	"BEGIN:VEVENT\r\nUID:single@x\r\nSUMMARY:一次性会议\\, 带逗号\r\nDTSTART;TZID=Asia/Shanghai:20260910T090000\r\nDTEND;TZID=Asia/Shanghai:20260910T100000\r\nURL:https://example.com/m/1\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:allday@x\r\nSUMMARY:全天\r\nDTSTART;VALUE=DATE:20260912\r\nDTEND;VALUE=DATE:20260913\r\nTRANSP:TRANSPARENT\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:weekly@x\r\nSUMMARY:周会\r\nDTSTART;TZID=Asia/Shanghai:20260907T140000\r\nDURATION:PT30M\r\nRRULE:FREQ=WEEKLY;BYDAY=MO,WE;COUNT=6\r\nEXDATE;TZID=Asia/Shanghai:20260914T140000\r\n" +
	"BEGIN:VALARM\r\nTRIGGER:-PT10M\r\nACTION:DISPLAY\r\nDESCRIPTION:提醒\r\nEND:VALARM\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:weekly@x\r\nRECURRENCE-ID;TZID=Asia/Shanghai:20260916T140000\r\nSUMMARY:周会（改到下午四点）\r\nDTSTART;TZID=Asia/Shanghai:20260916T160000\r\nDTEND;TZID=Asia/Shanghai:20260916T163000\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:cancelled@x\r\nSUMMARY:取消了的\r\nSTATUS:CANCELLED\r\nDTSTART:20260911T010000Z\r\nDTEND:20260911T020000Z\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:folded@x\r\nSUMMARY:折行的标题第一段\r\n 第二段\r\nDTSTART:20260918T020000Z\r\nDTEND:20260918T030000Z\r\nX-MICROSOFT-CDO-BUSYSTATUS:FREE\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

func sh() *time.Location { l, _ := time.LoadLocation("Asia/Shanghai"); return l }

// 解析：折行、转义、时区、全天、忙闲、取消；展开：周规则 + 排除 + 覆盖实例。
func TestParseICSAndExpand(t *testing.T) {
	f, err := ParseICS(sampleICS)
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "我的日历" || len(f.Events) != 6 {
		t.Fatalf("应解析出日历名与 6 条 VEVENT，实际 %q %d", f.Name, len(f.Events))
	}
	from := time.Date(2026, 9, 7, 0, 0, 0, 0, sh())
	to := time.Date(2026, 9, 30, 0, 0, 0, 0, sh())
	items := f.Instances(from, to)
	byID := map[string]CalendarEvent{}
	for _, it := range items {
		byID[it.ExternalID] = it
	}
	if s := byID["single@x"]; s.Title != "一次性会议, 带逗号" || !s.Start.Equal(time.Date(2026, 9, 10, 9, 0, 0, 0, sh())) || s.URL != "https://example.com/m/1" || !s.Busy {
		t.Fatalf("单次事件不对: %+v", s)
	}
	if a := byID["allday@x"]; !a.AllDay || a.Busy || !a.End.Equal(a.Start.AddDate(0, 0, 1)) {
		t.Fatalf("全天事件不对: %+v", a)
	}
	if _, ok := byID["cancelled@x"]; ok {
		t.Fatal("取消了的不该出现")
	}
	if fo := byID["folded@x"]; fo.Title != "折行的标题第一段第二段" || fo.Busy {
		t.Fatalf("折行 / 空闲标记不对: %+v", fo)
	}
	// 周会：9/7(一) 9/9(三) 9/14(一，排除) 9/16(三，覆盖到 16:00) 9/21(一) 9/23(三)，COUNT=6 数的是规则实例
	var weekly []CalendarEvent
	for _, it := range items {
		if strings.HasPrefix(it.ExternalID, "weekly@x") {
			weekly = append(weekly, it)
		}
	}
	want := []string{"09-07 14:00", "09-09 14:00", "09-16 16:00", "09-21 14:00", "09-23 14:00"}
	if len(weekly) != len(want) {
		t.Fatalf("周会应有 %d 个实例，实际 %d: %+v", len(want), len(weekly), weekly)
	}
	for i, w := range weekly {
		if got := w.Start.In(sh()).Format("01-02 15:04"); got != want[i] {
			t.Fatalf("第 %d 个实例应在 %s，实际 %s", i+1, want[i], got)
		}
		if w.End.Sub(w.Start) != 30*time.Minute {
			t.Fatalf("实例时长应 30 分钟: %+v", w)
		}
	}
	if weekly[2].Title != "周会（改到下午四点）" {
		t.Fatalf("覆盖实例应换标题: %+v", weekly[2])
	}
	// 区间外的不出现
	if n := len(f.Instances(time.Date(2026, 10, 1, 0, 0, 0, 0, sh()), time.Date(2026, 10, 31, 0, 0, 0, 0, sh()))); n != 0 {
		t.Fatalf("十月不该有实例，实际 %d", n)
	}
}

// 各种重复规则的展开。
func TestExpandRRule(t *testing.T) {
	start := time.Date(2026, 1, 31, 10, 0, 0, 0, sh())
	horizon := time.Date(2027, 12, 31, 0, 0, 0, 0, sh())
	fmtAll := func(ts []time.Time) []string {
		var out []string
		for _, x := range ts {
			out = append(out, x.Format("2006-01-02"))
		}
		return out
	}
	cases := []struct {
		rule string
		want []string
	}{
		{"FREQ=DAILY;INTERVAL=2;COUNT=3", []string{"2026-01-31", "2026-02-02", "2026-02-04"}},
		{"FREQ=MONTHLY;COUNT=4", []string{"2026-01-31", "2026-03-31", "2026-05-31", "2026-07-31"}}, // 没有 31 号的月份跳过
		{"FREQ=MONTHLY;BYDAY=-1FR;COUNT=3", []string{"2026-01-31", "2026-02-27", "2026-03-27"}},    // 起点本身永远是第一个，之后每月最后一个周五
		{"FREQ=MONTHLY;BYMONTHDAY=1,15;COUNT=3", []string{"2026-01-31", "2026-02-01", "2026-02-15"}},
		{"FREQ=YEARLY;COUNT=2", []string{"2026-01-31", "2027-01-31"}},
		{"FREQ=WEEKLY;UNTIL=20260215", []string{"2026-01-31", "2026-02-07", "2026-02-14"}},
		{"FREQ=NOPE", []string{"2026-01-31"}},
	}
	for _, c := range cases {
		got := fmtAll(expandRRule(c.rule, start, time.Time{}, horizon))
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: 应 %v，实际 %v", c.rule, c.want, got)
		}
	}
	// 只到 horizon：无限规则不会跑满上限
	if n := len(expandRRule("FREQ=DAILY", start, time.Time{}, start.AddDate(0, 0, 10))); n < 10 || n > 12 {
		t.Fatalf("按 horizon 截断应在 10 个上下，实际 %d", n)
	}
	// 几年前建的每日站会：窗口在今天，照样算得到，且 COUNT 从起点数
	old := time.Date(2019, 1, 7, 9, 30, 0, 0, sh())
	since := time.Date(2026, 9, 1, 0, 0, 0, 0, sh())
	got := expandRRule("FREQ=DAILY;BYDAY=MO,TU,WE,TH,FR", old, since, since.AddDate(0, 0, 30))
	if len(got) < 20 || got[0].Before(since) || got[0].Weekday() == time.Saturday || got[0].Weekday() == time.Sunday {
		t.Fatalf("2019 年起的工作日站会在 2026-09 应有 20 多个实例，实际 %d %v", len(got), got[:min(3, len(got))])
	}
	if n := len(expandRRule("FREQ=DAILY;COUNT=5", old, since, since.AddDate(0, 0, 30))); n != 0 {
		t.Fatalf("COUNT=5 的规则早就数完了，窗口里应为 0，实际 %d", n)
	}
	// 不会算的规则退化成单次
	if got := expandRRule("FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1", start, time.Time{}, horizon); len(got) != 1 {
		t.Fatalf("BYSETPOS 应退化成只有起点一次，实际 %d", len(got))
	}
}

// 网络错误里不能带链接：链接是密钥，错误文本会进动态。
func TestICSErrorHidesURL(t *testing.T) {
	t.Setenv("AXIOMOS_ALLOW_PRIVATE_EGRESS", "1")
	c, err := NewICSCalendar("https://127.0.0.1:1/x.ics?secret=SECRETTOKEN", Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Events(context.Background(), "", time.Now(), time.Now().Add(time.Hour))
	if err == nil || strings.Contains(err.Error(), "SECRETTOKEN") || strings.Contains(err.Error(), "://") {
		t.Fatalf("错误文本不该带链接: %v", err)
	}
	var un *UnreachableError
	if !errors.As(err, &un) {
		t.Fatalf("应是 UnreachableError，实际 %T", err)
	}
}

// 拉取：webcal 转 https、非 2xx 报错、不是 ICS 报错、日历名带回来。
func TestICSCalendarFetch(t *testing.T) {
	t.Setenv("AXIOMOS_ALLOW_PRIVATE_EGRESS", "1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok.ics":
			w.Header().Set("Content-Type", "text/calendar")
			_, _ = w.Write([]byte(sampleICS))
		case "/html":
			_, _ = w.Write([]byte("<html>login</html>"))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	if NormalizeICSURL("webcal://a.b/c.ics") != "https://a.b/c.ics" {
		t.Fatal("webcal 应换成 https")
	}
	c, err := NewICSCalendar(srv.URL+"/ok.ics", Options{HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	evs, err := c.Events(context.Background(), "", time.Date(2026, 9, 1, 0, 0, 0, 0, sh()), time.Date(2026, 10, 1, 0, 0, 0, 0, sh()))
	if err != nil || len(evs) == 0 || c.CalendarName() != "我的日历" {
		t.Fatalf("拉取应成功并带回日历名: %d %q %v", len(evs), c.CalendarName(), err)
	}
	if c2, _ := NewICSCalendar(srv.URL+"/html", Options{HTTPClient: srv.Client()}); c2 != nil {
		if _, err := c2.Events(context.Background(), "", time.Now(), time.Now().Add(time.Hour)); err != ErrNotICS {
			t.Fatalf("不是 ICS 应报 ErrNotICS，实际 %v", err)
		}
	}
	if c3, _ := NewICSCalendar(srv.URL+"/missing", Options{HTTPClient: srv.Client()}); c3 != nil {
		if _, err := c3.Events(context.Background(), "", time.Now(), time.Now().Add(time.Hour)); err == nil || !strings.Contains(err.Error(), "404") {
			t.Fatalf("404 应报错，实际 %v", err)
		}
	}
	if _, err := NewICSCalendar("ftp://x/y.ics", Options{}); err == nil {
		t.Fatal("非 http(s) 链接应被拒")
	}
}
