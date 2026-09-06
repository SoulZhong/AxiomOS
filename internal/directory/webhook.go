package directory

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/teemo/axiomos/internal/i18n"
)

// Webhook 是只发消息的提供方（ADR 0019）：组织级一个地址，每条通知 POST 一个 JSON；有密钥时带 HMAC-SHA256 签名头。
// 收件人是谁写在载荷里（recipient），externalUserID 不用。
func init() {
	Register(Provider{
		Key:   "webhook",
		Title: i18n.T("Webhook", "Webhook"),
		MessagingFields: []CredentialField{
			{Key: "url", Title: i18n.T("接收地址", "Receiver URL"), Placeholder: "https://example.com/axiomos/notify",
				Hint: i18n.T("每条通知 POST 一个 JSON 到这里", "Each notification is POSTed here as JSON")},
			{Key: "secret", Title: i18n.T("签名密钥", "Signing secret"), Secret: true,
				Hint: i18n.T("可选；填了就在请求头 X-AxiomOS-Signature 里带 HMAC-SHA256 签名", "Optional; when set, the X-AxiomOS-Signature header carries an HMAC-SHA256 signature")},
		},
		MessagingPrerequisites: []i18n.Text{
			i18n.T("有一个能收 HTTPS POST 的地址（自建服务、飞书 / 企业微信群机器人的转发等）", "An endpoint that accepts HTTPS POST (your own service, a relay to a group bot, etc.)"),
		},
		MessagingConfigured: func(creds map[string]string, secretsSet map[string]bool) bool {
			return strings.TrimSpace(creds["url"]) != ""
		},
		NewMessenger: func(creds map[string]string, opts Options) (Messenger, error) { return NewWebhook(creds, opts) },
	})
}

// Webhook 客户端。
type Webhook struct {
	URL    string
	secret string
	http   *http.Client
}

// NewWebhook 建客户端；creds 需要 url，可选 secret。
func NewWebhook(creds map[string]string, opts Options) (*Webhook, error) {
	hc, err := newHTTPClient(opts)
	if err != nil {
		return nil, err
	}
	u := strings.TrimSpace(creds["url"])
	if err := CheckEgressURL(u, DefaultEgress()); err != nil {
		return nil, err
	}
	return &Webhook{URL: u, secret: creds["secret"], http: hc}, nil
}

// WebhookPayload 是 POST 的 JSON 形状。
type WebhookPayload struct {
	Kind      string           `json:"kind"`
	Title     string           `json:"title"`
	Text      string           `json:"text"`
	URL       string           `json:"url"`
	Recipient WebhookRecipient `json:"recipient"`
	At        time.Time        `json:"at"`
}

// WebhookRecipient 是载荷里的收件人。
type WebhookRecipient struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Signature 算签名：hex(HMAC-SHA256(secret, "<unix 秒>.<body>"))；请求头 X-AxiomOS-Signature: t=<秒>,v1=<hex>。
func Signature(secret string, ts int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(ts, 10)))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// SignatureTolerance 是签名默认的新鲜度窗口：时间戳与现在相差超过它就算过期，防止旧请求被重放。
const SignatureTolerance = 5 * time.Minute

// VerifySignature 校验签名头（给接收方与测试用）：先看时间戳是不是在窗口内，再做常数时间比较。
// tolerance <= 0 时用 SignatureTolerance。
func VerifySignature(secret, header string, body []byte, tolerance time.Duration) bool {
	var ts int64
	var v1 string
	for _, part := range strings.Split(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts, _ = strconv.ParseInt(v, 10, 64)
		case "v1":
			v1 = v
		}
	}
	if ts == 0 || v1 == "" {
		return false
	}
	if tolerance <= 0 {
		tolerance = SignatureTolerance
	}
	if d := time.Since(time.Unix(ts, 0)); d > tolerance || d < -tolerance {
		return false
	}
	return hmac.Equal([]byte(v1), []byte(Signature(secret, ts, body)))
}

// SendDirect POST 一条通知；非 2xx 视为提供方拒绝并带回响应原文。
func (w *Webhook) SendDirect(ctx context.Context, _ string, msg Message) error {
	at := msg.At
	if at.IsZero() {
		at = time.Now()
	}
	body, _ := json.Marshal(WebhookPayload{Kind: msg.Kind, Title: msg.Title, Text: msg.Text, URL: msg.URL,
		Recipient: WebhookRecipient{ID: msg.Recipient.ID, Name: msg.Recipient.Name}, At: at})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", "AxiomOS-Webhook/1")
	req.Header.Set("X-AxiomOS-Kind", msg.Kind)
	if w.secret != "" {
		// 签名时间用发出去的这一刻，不用消息生成时间：排队重试过的消息也要落在接收方的新鲜度窗口里。
		ts := time.Now().Unix()
		req.Header.Set("X-AxiomOS-Signature", "t="+strconv.FormatInt(ts, 10)+",v1="+Signature(w.secret, ts, body))
	}
	resp, err := w.http.Do(req)
	if err != nil {
		return &UnreachableError{Err: err}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &RejectedError{Code: resp.StatusCode, Msg: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, trim(strings.TrimSpace(string(raw)), 200))}
	}
	return nil
}
