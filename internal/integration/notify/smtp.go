package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/yuylong/Chalkline/internal/store"
)

// SMTP 是邮件通道：只对需要人工关注的门禁事件发送（gate.pending / node.rejected /
// iteration.closed），收件人由 CHALKLINE_NOTIFY_EMAILS 配置。
// 只发配置的内网 SMTP（docs/domain/integrations.md「安全」）；凭据不落库。
type SMTP struct {
	Host     string // CHALKLINE_SMTP_HOST
	Port     int    // CHALKLINE_SMTP_PORT，默认 25
	Username string // 可选
	Password string // 可选
	From     string // CHALKLINE_SMTP_FROM
	To       []string
	Timeout  time.Duration // 默认 10s
}

// notifyEvents 是发送邮件的事件集合；其余事件只走 webhook。
var notifyEvents = map[string]string{
	"gate.pending":     "门禁待审批",
	"node.rejected":    "节点被驳回",
	"iteration.closed": "迭代已收尾",
}

func (m *SMTP) Name() string { return "smtp" }

// Interested 报告事件是否需要发邮件（由 Dispatcher 侧在构造时用于跳过）。
func (m *SMTP) Interested(eventType string) bool { return notifyEvents[eventType] != "" }

func (m *SMTP) port() int {
	if m.Port > 0 {
		return m.Port
	}
	return 25
}

func (m *SMTP) timeout() time.Duration {
	if m.Timeout > 0 {
		return m.Timeout
	}
	return 10 * time.Second
}

func (m *SMTP) Send(ctx context.Context, ev store.OutboxRow) error {
	label, ok := notifyEvents[ev.EventType]
	if !ok {
		return nil // 非邮件事件：视为成功，交给其他通道
	}
	if m.Host == "" || len(m.To) == 0 || m.From == "" {
		// 邮件未配置：门禁事件也不该因此阻断 webhook 投递。
		return nil
	}
	msg, err := m.buildMessage(ev, label)
	if err != nil {
		return err
	}
	addr := net.JoinHostPort(m.Host, fmt.Sprintf("%d", m.port()))
	return m.sendMail(ctx, addr, msg)
}

// buildMessage 构造纯文本邮件（正文为事件元数据 JSON，不携带业务正文）。
func (m *SMTP) buildMessage(ev store.OutboxRow, label string) ([]byte, error) {
	meta, err := json.Marshal(map[string]any{
		"event_type":   ev.EventType,
		"subject_type": ev.SubjectType,
		"subject_id":   ev.SubjectID,
		"payload":      ev.Payload,
		"created_at":   ev.CreatedAt.Format(time.RFC3339),
	})
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("From: " + m.From + "\r\n")
	b.WriteString("To: " + strings.Join(m.To, ", ") + "\r\n")
	b.WriteString("Subject: [Chalkline] " + label + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.Write(meta)
	return []byte(b.String()), nil
}

// sendMail 带 deadline 的 SMTP 投递（net/smtp.SendMail 无超时控制，自行实现拨号）。
func (m *SMTP) sendMail(ctx context.Context, addr string, msg []byte) error {
	d := net.Dialer{Timeout: m.timeout()}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp 拨号失败: %w", err)
	}
	deadline := time.Now().Add(m.timeout())
	_ = conn.SetDeadline(deadline)
	c, err := smtp.NewClient(conn, m.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp 握手失败: %w", err)
	}
	defer func() { _ = c.Quit() }()

	var auth smtp.Auth
	if m.Username != "" {
		auth = smtp.PlainAuth("", m.Username, m.Password, m.Host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp 认证失败: %w", err)
		}
	}
	if err := c.Mail(m.From); err != nil {
		return fmt.Errorf("smtp MAIL FROM 失败: %w", err)
	}
	for _, to := range m.To {
		if err := c.Rcpt(to); err != nil {
			return fmt.Errorf("smtp RCPT TO %s 失败: %w", to, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA 失败: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp 写信失败: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp 结束信体失败: %w", err)
	}
	return nil
}
