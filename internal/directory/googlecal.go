package directory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

// Google Calendar 作为外部日历提供方（ADR 0032）：组织登记一个 OAuth 客户端，成员各自授权一次；
// 只读拉主日历的事件。用到的接口：
//
//	GET  https://accounts.google.com/o/oauth2/v2/auth?client_id&redirect_uri&response_type=code&scope&access_type=offline&prompt=consent&state
//	POST https://oauth2.googleapis.com/token   (code → refresh_token；refresh_token → access_token)
//	GET  https://www.googleapis.com/oauth2/v3/userinfo  → {email}
//	GET  https://www.googleapis.com/calendar/v3/calendars/primary/events?timeMin&timeMax&singleEvents=true&orderBy=startTime&maxResults=250
func init() {
	Register(Provider{
		Key:   "googlecal",
		Title: i18n.T("Google 日历", "Google Calendar"),
		CalendarFields: []CredentialField{
			{Key: "client_id", Title: i18n.T("OAuth 客户端 ID", "OAuth client ID"), Placeholder: "xxxx.apps.googleusercontent.com",
				Hint: i18n.T("Google Cloud Console → API 和服务 → 凭据 → OAuth 2.0 客户端（网页应用）", "Google Cloud Console → APIs & Services → Credentials → OAuth 2.0 client (web application)")},
			{Key: "client_secret", Title: i18n.T("OAuth 客户端密钥", "OAuth client secret"), Secret: true,
				Hint: i18n.T("与客户端 ID 同一处；保存后只显示是否已设置", "Same place as the client ID; only whether it is set is shown after saving")},
		},
		CalendarPrerequisites: []i18n.Text{
			i18n.T("在 Google Cloud Console 启用 Google Calendar API", "Enable the Google Calendar API in Google Cloud Console"),
			i18n.T("创建一个「网页应用」类型的 OAuth 客户端，把本系统的回调地址加进已授权的重定向 URI", "Create a “web application” OAuth client and add this system's callback URL to the authorized redirect URIs"),
			i18n.T("成员在个人设置里各自连接一次自己的 Google 日历", "Each member connects their own Google Calendar once in personal settings"),
		},
		Tip:               GuideStep{Text: i18n.T("OAuth 客户端在 Google Cloud Console 的「API 和服务 → 凭据」里；本系统只申请日历只读权限。", "The OAuth client lives under “APIs & Services → Credentials” in Google Cloud Console; this system requests read-only calendar access only."), URL: "https://console.cloud.google.com/apis/credentials"},
		PerMemberCalendar: true,
		NewCalendar:       func(creds map[string]string, opts Options) (Calendar, error) { return NewGoogleCalendar(creds, opts) },
	})
}

// GoogleCalendar 是一个成员的 Google 日历客户端：refresh_token 是成员自己授权得到的。
type GoogleCalendar struct {
	AuthBase     string // https://accounts.google.com
	TokenBase    string // https://oauth2.googleapis.com
	APIBase      string // https://www.googleapis.com
	clientID     string
	clientSecret string
	refreshToken string
	http         *http.Client
}

// NewGoogleCalendar 建客户端；creds 要 client_id、client_secret，成员的 refresh_token 由应用层并进来（没有时只能出授权地址、换令牌）。
func NewGoogleCalendar(creds map[string]string, opts Options) (*GoogleCalendar, error) {
	hc, err := newHTTPClient(opts)
	if err != nil {
		return nil, err
	}
	return &GoogleCalendar{AuthBase: "https://accounts.google.com", TokenBase: "https://oauth2.googleapis.com", APIBase: "https://www.googleapis.com",
		clientID: creds["client_id"], clientSecret: creds["client_secret"], refreshToken: creds["refresh_token"], http: hc}, nil
}

const googleCalendarScope = "https://www.googleapis.com/auth/calendar.readonly https://www.googleapis.com/auth/userinfo.email"

// AuthURL 让成员去授权的地址：离线访问 + 每次都要 consent，才拿得到刷新令牌。
func (g *GoogleCalendar) AuthURL(redirectURL, state string) string {
	q := url.Values{"client_id": {g.clientID}, "redirect_uri": {redirectURL}, "response_type": {"code"}, "scope": {googleCalendarScope},
		"access_type": {"offline"}, "prompt": {"consent"}, "state": {state}}
	return g.AuthBase + "/o/oauth2/v2/auth?" + q.Encode()
}

type googleToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func (g *GoogleCalendar) token(ctx context.Context, form url.Values) (*googleToken, error) {
	form.Set("client_id", g.clientID)
	form.Set("client_secret", g.clientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.TokenBase+"/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.http.Do(req)
	if err != nil {
		return nil, &UnreachableError{Err: err}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var tk googleToken
	if err := json.Unmarshal(raw, &tk); err != nil || tk.Error != "" || tk.AccessToken == "" {
		msg := tk.ErrorDesc
		if msg == "" {
			msg = trim(string(raw), 200)
		}
		return nil, &RejectedError{Code: resp.StatusCode, Msg: fmt.Sprintf("%s (%s)", msg, tk.Error)}
	}
	return &tk, nil
}

// Exchange 用授权码换刷新令牌，并读出账号邮箱。
func (g *GoogleCalendar) Exchange(ctx context.Context, code, redirectURL string) (string, string, error) {
	tk, err := g.token(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURL}})
	if err != nil {
		return "", "", err
	}
	if tk.RefreshToken == "" {
		return "", "", &RejectedError{Code: 400, Msg: "no refresh token returned; the consent screen must be shown"}
	}
	email := ""
	var info struct {
		Email string `json:"email"`
	}
	if err := g.getJSON(ctx, tk.AccessToken, g.APIBase+"/oauth2/v3/userinfo", &info); err == nil {
		email = info.Email
	}
	return tk.RefreshToken, email, nil
}

func (g *GoogleCalendar) getJSON(ctx context.Context, access, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	resp, err := g.http.Do(req)
	if err != nil {
		return &UnreachableError{Err: err}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 400 {
		return &RejectedError{Code: resp.StatusCode, Msg: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, trim(string(raw), 200))}
	}
	return json.Unmarshal(raw, out)
}

type googleEvents struct {
	Items []struct {
		ID      string `json:"id"`
		Summary string `json:"summary"`
		Status  string `json:"status"`
		Start   struct {
			DateTime string `json:"dateTime"`
			Date     string `json:"date"`
		} `json:"start"`
		End struct {
			DateTime string `json:"dateTime"`
			Date     string `json:"date"`
		} `json:"end"`
		Transparency string `json:"transparency"`
		HTMLLink     string `json:"htmlLink"`
	} `json:"items"`
	NextPageToken string `json:"nextPageToken"`
}

// Events 读主日历在区间里的事件（循环事件已展开）。externalUserID 不用：令牌本身就是身份。
func (g *GoogleCalendar) Events(ctx context.Context, _ string, from, to time.Time) ([]CalendarEvent, error) {
	if g.refreshToken == "" {
		return nil, &RejectedError{Code: 401, Msg: "member has not connected Google Calendar"}
	}
	tk, err := g.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {g.refreshToken}})
	if err != nil {
		return nil, err
	}
	var out []CalendarEvent
	page := ""
	for i := 0; i < 20; i++ {
		q := url.Values{"timeMin": {from.UTC().Format(time.RFC3339)}, "timeMax": {to.UTC().Format(time.RFC3339)}, "singleEvents": {"true"}, "orderBy": {"startTime"}, "maxResults": {"250"}}
		if page != "" {
			q.Set("pageToken", page)
		}
		var res googleEvents
		if err := g.getJSON(ctx, tk.AccessToken, g.APIBase+"/calendar/v3/calendars/primary/events?"+q.Encode(), &res); err != nil {
			return nil, err
		}
		for _, it := range res.Items {
			if it.Status == "cancelled" || it.ID == "" {
				continue
			}
			ev := CalendarEvent{ExternalID: it.ID, Title: it.Summary, Busy: it.Transparency != "transparent", URL: it.HTMLLink}
			if it.Start.Date != "" {
				ev.AllDay = true
				ev.Start, _ = time.ParseInLocation("2006-01-02", it.Start.Date, time.Local)
				ev.End, _ = time.ParseInLocation("2006-01-02", it.End.Date, time.Local)
			} else {
				ev.Start, _ = time.Parse(time.RFC3339, it.Start.DateTime)
				ev.End, _ = time.Parse(time.RFC3339, it.End.DateTime)
			}
			if ev.End.IsZero() {
				continue
			}
			out = append(out, ev)
		}
		if res.NextPageToken == "" {
			break
		}
		page = res.NextPageToken
	}
	return out, nil
}
