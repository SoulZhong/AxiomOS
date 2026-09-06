package directory

import (
	"context"
	"crypto/tls"
	"errors"
	"mime"
	"net"
	"net/smtp"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

// 邮件是只发消息的提供方（ADR 0019）：SMTP。服务端可以用环境变量 SMTP_URL / SMTP_FROM 给一个全局默认，
// 组织在通道配置里填了主机就用自己的（密码加密入库）。没有 IM 集成能力，所以不在 Providers() 里。
func init() {
	Register(Provider{
		Key:   "email",
		Title: i18n.T("邮件", "Email"),
		MessagingFields: []CredentialField{
			{Key: "host", Title: i18n.T("SMTP 主机", "SMTP host"), Placeholder: "smtp.example.com",
				Hint: i18n.T("留空表示用服务端配置的邮件服务", "Leave empty to use the server-wide mail service")},
			{Key: "port", Title: i18n.T("端口", "Port"), Placeholder: "587",
				Hint: i18n.T("465 走 TLS，其余端口用 STARTTLS", "465 uses TLS; other ports use STARTTLS")},
			{Key: "username", Title: i18n.T("用户名", "Username")},
			{Key: "password", Title: i18n.T("密码", "Password"), Secret: true,
				Hint: i18n.T("保存后只显示是否已设置", "Only whether it is set is shown after saving")},
			{Key: "from", Title: i18n.T("发件人", "From"), Placeholder: "AxiomOS <no-reply@example.com>"},
		},
		MessagingPrerequisites: []i18n.Text{
			i18n.T("有一个能发信的 SMTP 账号；或者由运维在服务端设置 SMTP_URL 与 SMTP_FROM", "An SMTP account that can send mail, or SMTP_URL and SMTP_FROM set on the server by ops"),
		},
		MessagingConfigured: func(creds map[string]string, secretsSet map[string]bool) bool {
			return strings.TrimSpace(creds["host"]) != "" || EmailDefaults().Host != ""
		},
		NewMessenger: func(creds map[string]string, opts Options) (Messenger, error) { return NewEmail(creds, opts) },
	})
}

// SMTPConfig 是一组 SMTP 参数。
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// EmailDefaults 从环境变量读服务端默认的邮件服务：SMTP_URL 形如 smtp://user:pass@host:587，SMTP_FROM 是发件人。
func EmailDefaults() SMTPConfig {
	var c SMTPConfig
	if raw := strings.TrimSpace(os.Getenv("SMTP_URL")); raw != "" {
		if u, err := url.Parse(raw); err == nil && u.Host != "" {
			c.Host = u.Hostname()
			c.Port, _ = strconv.Atoi(u.Port())
			if u.User != nil {
				c.Username = u.User.Username()
				c.Password, _ = u.User.Password()
			}
			if u.Scheme == "smtps" && c.Port == 0 {
				c.Port = 465
			}
		}
	}
	c.From = strings.TrimSpace(os.Getenv("SMTP_FROM"))
	return c
}

// SMTPSender 是真正把信发出去的一层；测试换成假的。
type SMTPSender interface {
	Send(ctx context.Context, cfg SMTPConfig, to string, raw []byte) error
}

// SendMail 是全局的发信实现，默认走 net/smtp；测试可以替换。
var SendMail SMTPSender = smtpSender{}

// Email 是邮件客户端：把一条通知排成一封纯文本信。
type Email struct {
	Config SMTPConfig
}

// NewEmail 建邮件客户端：组织填了主机就用组织的整套参数，否则用服务端默认；发件人组织没填时用服务端的。
func NewEmail(creds map[string]string, opts Options) (*Email, error) {
	if _, err := newHTTPClient(opts); err != nil {
		return nil, err
	}
	cfg := EmailDefaults()
	if h := strings.TrimSpace(creds["host"]); h != "" {
		port, _ := strconv.Atoi(strings.TrimSpace(creds["port"]))
		cfg = SMTPConfig{Host: h, Port: port, Username: creds["username"], Password: creds["password"], From: cfg.From}
	}
	if f := strings.TrimSpace(creds["from"]); f != "" {
		cfg.From = f
	}
	if cfg.Port == 0 {
		cfg.Port = 587
	}
	if cfg.From == "" && cfg.Username != "" && strings.Contains(cfg.Username, "@") {
		cfg.From = cfg.Username
	}
	if cfg.Host == "" {
		return nil, &RejectedError{Code: -1, Msg: "no smtp host configured"}
	}
	return &Email{Config: cfg}, nil
}

// FormatMail 把一条通知排成一封信：主题是那一句话，正文是那一句话加直达链接。返回完整的 RFC 5322 原文。
func FormatMail(from, to string, msg Message) []byte {
	subject := msg.Title
	if subject == "" {
		subject = msg.Text
	}
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	at := msg.At
	if at.IsZero() {
		at = time.Now()
	}
	b.WriteString("Date: " + at.Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\nX-AxiomOS-Kind: " + msg.Kind + "\r\n\r\n")
	if msg.Text != "" {
		b.WriteString(msg.Text + "\r\n")
	} else {
		b.WriteString(msg.Title + "\r\n")
	}
	if msg.URL != "" {
		open := msg.Open
		if open == "" {
			open = "打开"
		}
		b.WriteString("\r\n" + open + "：" + msg.URL + "\r\n")
	}
	return []byte(b.String())
}

// SendDirect 发一封信到 address。
func (e *Email) SendDirect(ctx context.Context, address string, msg Message) error {
	address = strings.TrimSpace(address)
	if address == "" || !strings.Contains(address, "@") {
		return &RejectedError{Code: -1, Msg: "recipient has no email address"}
	}
	return SendMail.Send(ctx, e.Config, address, FormatMail(e.Config.From, address, msg))
}

// smtpSender 用 net/smtp 发信：465 直接 TLS，其余端口 STARTTLS（服务器支持时）。
type smtpSender struct{}

func (smtpSender) Send(ctx context.Context, cfg SMTPConfig, to string, raw []byte) error {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	var err error
	if cfg.Port == 465 {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: cfg.Host})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return &UnreachableError{Err: err}
	}
	defer conn.Close()
	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return &UnreachableError{Err: err}
	}
	defer c.Close()
	if cfg.Port != 465 {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: cfg.Host}); err != nil {
				return &RejectedError{Code: -1, Msg: err.Error()}
			}
		}
	}
	if cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return &RejectedError{Code: -1, Msg: err.Error()}
		}
	}
	from := cfg.From
	if i := strings.LastIndex(from, "<"); i >= 0 && strings.HasSuffix(from, ">") {
		from = from[i+1 : len(from)-1]
	}
	if err := c.Mail(from); err != nil {
		return &RejectedError{Code: -1, Msg: err.Error()}
	}
	if err := c.Rcpt(to); err != nil {
		return &RejectedError{Code: -1, Msg: err.Error()}
	}
	w, err := c.Data()
	if err != nil {
		return &RejectedError{Code: -1, Msg: err.Error()}
	}
	if _, err := w.Write(raw); err != nil {
		return &UnreachableError{Err: err}
	}
	if err := w.Close(); err != nil {
		return &RejectedError{Code: -1, Msg: err.Error()}
	}
	if err := c.Quit(); err != nil && !errors.Is(err, net.ErrClosed) {
		return nil
	}
	return nil
}
