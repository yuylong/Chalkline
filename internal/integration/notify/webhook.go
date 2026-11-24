package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/yuylong/Chalkline/internal/store"
)

// Webhook 是通用出站事件通道：POST 事件 JSON 到订阅方，带 HMAC-SHA256 签名头，
// 接收方可校验（docs/domain/integrations.md「安全」）。URL 只来自环境变量（静态通道）
// 或 webhook_subscriptions（admin 配置的订阅，M3）——目标由管理员指定，属受信出站，
// 不走材料适配器的出站白名单；不跟随重定向 + 10s 超时兜底。
type Webhook struct {
	URL    string
	Secret string
	// SubscriptionID 非空时输出 X-Chalkline-Subscription-Id，接收方区分订阅来源。
	SubscriptionID string
	Client         *http.Client
}

func (w *Webhook) Name() string { return "webhook" }

// webhookPayload 是投递载荷：只含 id、类型、主体、payload 与时间，不携带正文
// （docs/domain/integrations.md「事件与投递」）。
type webhookPayload struct {
	ID          int64          `json:"id"`
	EventType   string         `json:"event_type"`
	SubjectType string         `json:"subject_type"`
	SubjectID   string         `json:"subject_id"`
	Payload     map[string]any `json:"payload"`
	CreatedAt   time.Time      `json:"created_at"`
}

// Sign 返回 Webhook 签名头值：sha256=<hmac-sha256(secret, body) 的 hex>。
// 导出供接收方测试与文档对照。
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (w *Webhook) Send(ctx context.Context, ev store.OutboxRow) error {
	if w.URL == "" {
		return fmt.Errorf("webhook URL 未配置")
	}
	body, err := json.Marshal(webhookPayload{
		ID: ev.ID, EventType: ev.EventType, SubjectType: ev.SubjectType,
		SubjectID: ev.SubjectID, Payload: ev.Payload, CreatedAt: ev.CreatedAt,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "chalkline-webhook/1")
	if w.SubscriptionID != "" {
		req.Header.Set("X-Chalkline-Subscription-Id", w.SubscriptionID)
	}
	if w.Secret != "" {
		req.Header.Set("X-Chalkline-Signature", Sign(w.Secret, body))
	}
	client := w.Client
	if client == nil {
		client = webhookClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook 响应状态 %d", resp.StatusCode)
	}
	return nil
}

// webhookClient 不跟随重定向（防 SSRF 跳转绕过），10s 超时。
func webhookClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
