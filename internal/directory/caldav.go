package directory

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

// CalDAV 账号（ADR 0032 补记二）：第二种自助的日历提供方。企业微信（caldav.wecom.work）、Apple iCloud
// 这类不给订阅链接、只给「服务器 + 账号 + 密码」的日历，成员在个人设置里自己填就能连，不用管理员。
// 只读：PROPFIND 找到账号下的日历，REPORT calendar-query 按时间段拉 VEVENT，拿到的还是 iCalendar 文本，
// 交给 ics.go 的解析与重复规则展开。密码整组加密存在成员绑定里，不回显；服务器地址过出网守卫。
//
// 用到的 WebDAV / CalDAV 请求（RFC 4791）：
//
//	PROPFIND <服务器>            Depth: 0  → current-user-principal
//	PROPFIND <principal>         Depth: 0  → calendar-home-set
//	PROPFIND <home>              Depth: 1  → 每本日历的 resourcetype / displayname / supported-calendar-component-set
//	REPORT   <日历>              Depth: 1  → calendar-query（VEVENT，time-range）→ calendar-data
//
// 填的是某一本日历的地址时跳过发现，直接 REPORT。

// CalDAVKey 是 CalDAV 提供方的代码名。
const CalDAVKey = "caldav"

func init() {
	Register(Provider{
		Key:   CalDAVKey,
		Title: i18n.T("CalDAV 账号", "CalDAV account"),
		CalendarFields: []CredentialField{
			{Key: "server", Title: i18n.T("服务器", "Server"), Placeholder: "https://caldav.wecom.work",
				Hint: i18n.T("企业微信：日程 → 同步至其他日历 里给的服务器；Apple iCloud：https://caldav.icloud.com", "WeCom: the server shown under Schedule → Sync to other calendars; Apple iCloud: https://caldav.icloud.com")},
			{Key: "username", Title: i18n.T("账号", "Account"), Placeholder: "",
				Hint: i18n.T("同一页给的「帐号 / 用户名」；iCloud 是 Apple ID 邮箱", "The “account / username” from the same page; for iCloud, your Apple ID email")},
			{Key: "password", Title: i18n.T("密码", "Password"), Secret: true,
				Hint: i18n.T("企业微信每次现取一个新的；iCloud 要在 appleid.apple.com 生成「App 专用密码」，不是登录密码", "WeCom issues a fresh one each time; iCloud needs an app-specific password from appleid.apple.com, not your login password")},
		},
		CalendarPrerequisites: []i18n.Text{
			i18n.T("企业微信：日程 → 右上角 … → 同步至其他日历，复制服务器、帐号，点「获取密码」", "WeCom: Schedule → … → Sync to other calendars; copy the server and account, then “Get password”"),
			i18n.T("Apple iCloud：服务器填 https://caldav.icloud.com，账号是 Apple ID，密码用 App 专用密码", "Apple iCloud: server https://caldav.icloud.com, account is your Apple ID, password is an app-specific password"),
			i18n.T("Google 日历不支持账号密码，用「日历订阅链接」或组织级的 Google 日历", "Google Calendar does not accept passwords here; use the subscription link or the organization's Google Calendar connection"),
		},
		Tip:                 GuideStep{Text: i18n.T("密码只用来只读拉取，加密存放、不回显；企业微信的密码若失效，同步会显示失败，重新取一个再填即可。", "The password is used only for read-only fetching, stored encrypted and never shown; if a WeCom password stops working the sync shows a failure — fetch a new one and paste it again.")},
		PerMemberCalendar:   true,
		SelfServiceCalendar: true,
		NewCalendar:         func(creds map[string]string, opts Options) (Calendar, error) { return NewCalDAV(creds, opts) },
	})
}

// CalDAV 是一个账号的只读客户端。
type CalDAV struct {
	Server   string
	username string
	password string
	http     *http.Client
	names    []string
}

// NewCalDAV 建客户端：服务器地址先过出网守卫（只许 https；私有化部署放开内网时也许 http）。
func NewCalDAV(creds map[string]string, opts Options) (*CalDAV, error) {
	server := strings.TrimSpace(creds["server"])
	if server != "" && !strings.Contains(server, "://") {
		server = "https://" + server
	}
	if err := CheckEgressURL(server, DefaultEgress()); err != nil {
		return nil, err
	}
	hc, err := newHTTPClient(opts)
	if err != nil {
		return nil, err
	}
	return &CalDAV{Server: server, username: strings.TrimSpace(creds["username"]), password: creds["password"], http: hc}, nil
}

// CalendarName 最近一次拉取时找到的日历名（几本用「、」连起来）。
func (c *CalDAV) CalendarName() string { return strings.Join(c.names, "、") }

// Events 找到账号下全部日历，按时间段拉 VEVENT。externalUserID 不用：账号本身就是身份。
func (c *CalDAV) Events(ctx context.Context, _ string, from, to time.Time) ([]CalendarEvent, error) {
	cals, err := c.calendars(ctx)
	if err != nil {
		return nil, err
	}
	if len(cals) == 0 {
		return nil, &RejectedError{Code: 404, Msg: "no calendars"}
	}
	c.names = c.names[:0]
	var out []CalendarEvent
	for _, cal := range cals {
		c.names = append(c.names, cal.name)
		evs, err := c.query(ctx, cal.href, from, to)
		if err != nil {
			return nil, err
		}
		prefix := strings.Trim(cal.href.Path, "/")
		for _, e := range evs {
			e.ExternalID = prefix + "#" + e.ExternalID
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Start.Equal(out[j].Start) {
			return out[i].Start.Before(out[j].Start)
		}
		return out[i].ExternalID < out[j].ExternalID
	})
	return out, nil
}

type davCalendar struct {
	href *url.URL
	name string
}

// ---------- 发现 ----------

const (
	propfindPrincipal = `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:current-user-principal/><d:resourcetype/><d:displayname/></d:prop></d:propfind>`
	propfindHome      = `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><c:calendar-home-set/></d:prop></d:propfind>`
	propfindCalendars = `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><d:resourcetype/><d:displayname/><c:supported-calendar-component-set/></d:prop></d:propfind>`
)

// calendars 走标准发现：服务器 → principal → 日历主目录 → 逐本日历；填的本身是一本日历时直接用它。
func (c *CalDAV) calendars(ctx context.Context) ([]davCalendar, error) {
	base, err := url.Parse(c.Server)
	if err != nil {
		return nil, &RejectedError{Code: 400, Msg: "bad server url"}
	}
	if base.Path == "" {
		base.Path = "/"
	}
	// 第一步：填的地址本身是不是一本日历
	ms, err := c.dav(ctx, "PROPFIND", base, "0", propfindPrincipal)
	if err != nil {
		return nil, err
	}
	var principal *url.URL
	for _, r := range ms.Responses {
		for _, ps := range r.Propstat {
			if ps.Prop.ResourceType.Calendar != nil {
				name := ps.Prop.DisplayName
				if name == "" {
					name = strings.Trim(base.Path, "/")
				}
				return []davCalendar{{href: base, name: name}}, nil
			}
			if h := strings.TrimSpace(ps.Prop.CurrentUserPrincipal.Href); h != "" && principal == nil {
				principal = resolveHref(base, h)
			}
		}
	}
	if principal == nil {
		return nil, &RejectedError{Code: 404, Msg: "no principal"}
	}
	// 第二步：日历主目录
	ms, err = c.dav(ctx, "PROPFIND", principal, "0", propfindHome)
	if err != nil {
		return nil, err
	}
	var home *url.URL
	for _, r := range ms.Responses {
		for _, ps := range r.Propstat {
			for _, h := range ps.Prop.CalendarHomeSet.Href {
				if h = strings.TrimSpace(h); h != "" && home == nil {
					home = resolveHref(principal, h)
				}
			}
		}
	}
	if home == nil {
		return nil, &RejectedError{Code: 404, Msg: "no calendar home"}
	}
	// 第三步：主目录下每一本装 VEVENT 的日历
	ms, err = c.dav(ctx, "PROPFIND", home, "1", propfindCalendars)
	if err != nil {
		return nil, err
	}
	var out []davCalendar
	for _, r := range ms.Responses {
		for _, ps := range r.Propstat {
			if ps.Prop.ResourceType.Calendar == nil {
				continue
			}
			if comps := ps.Prop.Comps.Comp; len(comps) > 0 {
				ok := false
				for _, x := range comps {
					if strings.EqualFold(x.Name, "VEVENT") {
						ok = true
					}
				}
				if !ok {
					continue
				}
			}
			href := resolveHref(home, strings.TrimSpace(r.Href))
			name := ps.Prop.DisplayName
			if name == "" {
				name = strings.Trim(href.Path, "/")
			}
			out = append(out, davCalendar{href: href, name: name})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].href.Path < out[j].href.Path })
	return out, nil
}

// query 一本日历里落在区间的 VEVENT（服务器按 time-range 筛，重复的给主记录，这边展开）。
func (c *CalDAV) query(ctx context.Context, cal *url.URL, from, to time.Time) ([]CalendarEvent, error) {
	body := `<?xml version="1.0" encoding="utf-8"?><c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><d:getetag/><c:calendar-data/></d:prop>` +
		`<c:filter><c:comp-filter name="VCALENDAR"><c:comp-filter name="VEVENT"><c:time-range start="` + from.UTC().Format("20060102T150405Z") + `" end="` + to.UTC().Format("20060102T150405Z") + `"/></c:comp-filter></c:comp-filter></c:filter></c:calendar-query>`
	ms, err := c.dav(ctx, "REPORT", cal, "1", body)
	if err != nil {
		return nil, err
	}
	var out []CalendarEvent
	for _, r := range ms.Responses {
		for _, ps := range r.Propstat {
			data := strings.TrimSpace(ps.Prop.CalendarData)
			if data == "" {
				continue
			}
			f, err := ParseICS(data)
			if err != nil {
				continue
			}
			out = append(out, f.Instances(from, to)...)
		}
	}
	return out, nil
}

// ---------- 传输 ----------

type davMultistatus struct {
	XMLName   xml.Name      `xml:"DAV: multistatus"`
	Responses []davResponse `xml:"DAV: response"`
}

type davResponse struct {
	Href     string        `xml:"DAV: href"`
	Propstat []davPropstat `xml:"DAV: propstat"`
}

type davPropstat struct {
	Status string  `xml:"DAV: status"`
	Prop   davProp `xml:"DAV: prop"`
}

type davProp struct {
	CurrentUserPrincipal struct {
		Href string `xml:"DAV: href"`
	} `xml:"DAV: current-user-principal"`
	CalendarHomeSet struct {
		Href []string `xml:"DAV: href"`
	} `xml:"urn:ietf:params:xml:ns:caldav calendar-home-set"`
	ResourceType struct {
		Calendar *struct{} `xml:"urn:ietf:params:xml:ns:caldav calendar"`
	} `xml:"DAV: resourcetype"`
	DisplayName string `xml:"DAV: displayname"`
	Comps       struct {
		Comp []struct {
			Name string `xml:"name,attr"`
		} `xml:"urn:ietf:params:xml:ns:caldav comp"`
	} `xml:"urn:ietf:params:xml:ns:caldav supported-calendar-component-set"`
	CalendarData string `xml:"urn:ietf:params:xml:ns:caldav calendar-data"`
}

// dav 发一个 WebDAV 请求，2xx（多半是 207）时解析 multistatus；401 / 403 是账号密码不对，其余非 2xx 照状态码报。
// 网络错误抹掉地址（里面可能带账号）。
func (c *CalDAV) dav(ctx context.Context, method string, u *url.URL, depth, body string) (*davMultistatus, error) {
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewBufferString(body))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.Header.Set("Depth", depth)
	req.Header.Set("User-Agent", "AxiomOS-calendar/1")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, sanitizeICSErr(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, sanitizeICSErr(err)
	}
	switch {
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return nil, &RejectedError{Code: resp.StatusCode, Msg: "unauthorized"}
	case resp.StatusCode/100 != 2:
		return nil, &RejectedError{Code: resp.StatusCode, Msg: "HTTP " + http.StatusText(resp.StatusCode)}
	}
	var ms davMultistatus
	if len(bytes.TrimSpace(raw)) == 0 {
		return &ms, nil
	}
	if err := xml.Unmarshal(raw, &ms); err != nil {
		return nil, ErrNotCalDAV
	}
	return &ms, nil
}

// ErrNotCalDAV 表示服务器回的不是 WebDAV 应答（多半填成了网页地址）。
var ErrNotCalDAV = errors.New("not a CalDAV server")

// resolveHref 把应答里的 href（相对路径或绝对地址）接到请求地址上。
func resolveHref(base *url.URL, href string) *url.URL {
	ref, err := url.Parse(href)
	if err != nil {
		return base
	}
	return base.ResolveReference(ref)
}
