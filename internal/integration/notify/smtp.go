package notify

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/yuylong/Chalkline/internal/store"
)

// SMTP 是邮件通道：只对需要人工关注的事件发送（节点进展、门禁待审批/超期/升级、
// 任务指派、迭代收尾）。只发配置的内网 SMTP（docs/domain/integrations.md「安全」）。
// 收件人按事件解析为「相关成员」的邮箱（Recipients，server 组装层注入 app 层解析）；
// To 只是解析不出相关用户时的兜底（CHALKLINE_NOTIFY_EMAILS）。
// M4 起生效配置可经管理台维护：Resolve 非空时每轮投递前重新解析（保存即生效，
// 无需重启）；nil 则用下方静态字段（环境变量直配，测试同）。
type SMTP struct {
	Host     string // CHALKLINE_SMTP_HOST
	Port     int    // CHALKLINE_SMTP_PORT，默认 25
	Username string // 可选
	Password string // 可选
	From     string // CHALKLINE_SMTP_FROM
	// To 是兜底收件人（CHALKLINE_NOTIFY_EMAILS）：仅在 Recipients 未注入或
	// 解析不出任何相关用户时使用，避免通知静默丢失。
	To      []string
	Timeout time.Duration // 默认 10s

	// TLSMode：none（明文，默认）| starttls（连接后 EHLO 升级）| tls（隐式 TLS，465 常用）。
	TLSMode string // CHALKLINE_SMTP_TLS
	// TLSSkipVerify 跳过证书校验（CHALKLINE_SMTP_TLS_SKIP_VERIFY，内网自签场景）。
	TLSSkipVerify bool

	// Resolve 运行时解析生效配置（server 组装层注入 app 层合并结果）。
	Resolve func(ctx context.Context) (SMTPRuntime, bool)
	// Recipients 按事件解析收件人邮箱（server 组装层注入 app.NotifyRecipients）。
	// nil 时退化为静态 To（环境变量直配部署）。
	Recipients func(ctx context.Context, ev store.OutboxRow) ([]string, error)
}

// SMTPRuntime 是运行时解析出的生效发件配置。
type SMTPRuntime struct {
	Enabled       bool
	Host          string
	Port          int
	Username      string
	Password      string
	From          string
	TLSMode       string // none / starttls / tls
	TLSSkipVerify bool
}

// notifyEvents 是发送邮件的事件集合（其余事件只走 webhook）；
// 具体收件人由 app 层按事件语义解析为相关成员。
var notifyEvents = map[string]string{
	"node.ready":       "节点可开始",
	"node.passed":      "节点已通过",
	"node.rejected":    "节点被驳回",
	"gate.pending":     "门禁待审批",
	"gate.overdue":     "评审超期提醒",
	"gate.escalated":   "评审超时升级",
	"task.assigned":    "任务已指派",
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

// tlsMode 归一化 TLS 模式；空或未知值按 none（明文，保持既有行为）。
func (m *SMTP) tlsMode() string { return normalizeTLSMode(m.TLSMode) }

// normalizeTLSMode 归一化 TLS 模式（none / starttls / tls）。
func normalizeTLSMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "starttls":
		return "starttls"
	case "tls":
		return "tls"
	default:
		return "none"
	}
}

func (m *SMTP) Send(ctx context.Context, ev store.OutboxRow) error {
	label, ok := notifyEvents[ev.EventType]
	if !ok {
		return nil // 非邮件事件：视为成功，交给其他通道
	}
	host, port, username, password, from := m.Host, m.port(), m.Username, m.Password, m.From
	tlsMode, skipVerify := m.tlsMode(), m.TLSSkipVerify
	if m.Resolve != nil {
		cfg, ok := m.Resolve(ctx)
		if !ok || !cfg.Enabled {
			return nil // 未启用：静默跳过，不阻断其他通道
		}
		host, port, username, password, from = cfg.Host, cfg.Port, cfg.Username, cfg.Password, cfg.From
		tlsMode, skipVerify = cfg.TLSMode, cfg.TLSSkipVerify
		if port == 0 {
			port = 25
		}
	}
	if host == "" || from == "" {
		// 邮件未配置完整：门禁事件也不该因此阻断 webhook 投递。
		return nil
	}
	recipients, err := m.recipients(ctx, ev)
	if err != nil {
		return err
	}
	if len(recipients) == 0 {
		return nil // 无相关用户且无兜底收件人：静默跳过
	}
	msg, err := m.buildMessage(from, recipients, ev, label)
	if err != nil {
		return err
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	return m.sendMail(ctx, addr, host, username, password, from, tlsMode, skipVerify, recipients, msg)
}

// recipients 解析本封邮件的收件人：注入 Recipients 时按事件解析相关成员邮箱；
// 解析不出（或未注入）时回退静态兜底列表 To。
func (m *SMTP) recipients(ctx context.Context, ev store.OutboxRow) ([]string, error) {
	if m.Recipients != nil {
		list, err := m.Recipients(ctx, ev)
		if err != nil {
			return nil, fmt.Errorf("解析通知收件人失败: %w", err)
		}
		if len(list) > 0 {
			return list, nil
		}
	}
	return m.To, nil
}

// SendTest 发送测试邮件（管理台「发送测试邮件」按钮）：走与事件投递完全相同的
// 拨号 / TLS / 认证路径，收件人由调用方给定（当前登录账号的邮箱），失败直接返回错误。
func (m *SMTP) SendTest(ctx context.Context, cfg SMTPRuntime, to []string) error {
	port := cfg.Port
	if port == 0 {
		port = 25
	}
	msg := m.buildTestMessage(cfg.From, to)
	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", port))
	return m.sendMail(ctx, addr, cfg.Host, cfg.Username, cfg.Password, cfg.From,
		normalizeTLSMode(cfg.TLSMode), cfg.TLSSkipVerify, to, msg)
}

// buildTestMessage 构造测试邮件（固定提示文本，便于收件人一眼识别来源）。
func (m *SMTP) buildTestMessage(from string, to []string) []byte {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	b.WriteString("Subject: [Chalkline] 测试邮件\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString("这是一封来自 Chalkline 控制台「邮箱配置」页的测试邮件。收到本邮件即表示当前发件配置有效。\r\n")
	return []byte(b.String())
}

// buildMessage 构造纯文本邮件（正文为事件元数据 JSON，不携带业务正文）。
func (m *SMTP) buildMessage(from string, to []string, ev store.OutboxRow, label string) ([]byte, error) {
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
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	b.WriteString("Subject: [Chalkline] " + label + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.Write(meta)
	return []byte(b.String()), nil
}

// sendMail 带 deadline 的 SMTP 投递（net/smtp.SendMail 无超时控制，自行实现拨号）。
// tlsMode：none = 明文；starttls = EHLO 后协商升级；tls = 隐式 TLS（连接即 TLS）。
// skipVerify 仅建议内网自签证书场景使用，公网必须校验。
func (m *SMTP) sendMail(ctx context.Context, addr, host, username, password, from string,
	tlsMode string, skipVerify bool, to []string, msg []byte) error {
	tlsCfg := &tls.Config{ServerName: host, InsecureSkipVerify: skipVerify} //nolint:gosec // 内网自签场景由管理台显式开启
	var conn net.Conn
	var err error
	if tlsMode == "tls" {
		d := &tls.Dialer{NetDialer: &net.Dialer{Timeout: m.timeout()}, Config: tlsCfg}
		conn, err = d.DialContext(ctx, "tcp", addr)
	} else {
		d := net.Dialer{Timeout: m.timeout()}
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("smtp 拨号失败: %w", err)
	}
	deadline := time.Now().Add(m.timeout())
	_ = conn.SetDeadline(deadline)
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp 握手失败: %w", err)
	}
	defer func() { _ = c.Quit() }()

	if tlsMode == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("smtp 服务器不支持 STARTTLS")
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("smtp STARTTLS 升级失败: %w", err)
		}
	}
	if username != "" {
		auth := smtp.PlainAuth("", username, password, host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp 认证失败: %w", err)
		}
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("smtp MAIL FROM 失败: %w", err)
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("smtp RCPT TO %s 失败: %w", rcpt, err)
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
