package notify

// smtp_resolve_test.go：M4 邮箱配置——Resolve 运行时解析生效（保存即生效、
// 未启用静默跳过）。

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// TestSMTPResolveRuntime 管理台保存的配置经 Resolve 生效：发件人取解析值。
func TestSMTPResolveRuntime(t *testing.T) {
	var mail safeBuffer
	l, err := newInmemorySMTP(&mail)
	if err != nil {
		t.Fatalf("起桩失败: %v", err)
	}
	defer func() { _ = l.Close() }()
	go serveSMTP(l, &mail) //nolint:errcheck // 测试桩

	m := &SMTP{To: []string{"pm@example.com"}, Timeout: 2 * time.Second,
		Resolve: func(context.Context) (SMTPRuntime, bool) {
			return SMTPRuntime{Enabled: true, Host: "127.0.0.1",
				Port: l.Addr().(*net.TCPAddr).Port, From: "chalk@example.com"}, true
		}}
	if err := m.Send(context.Background(), ev(21, 0)); err != nil {
		t.Fatalf("投递失败: %v", err)
	}
	got := mail.String()
	if !strings.Contains(got, "From: chalk@example.com") {
		t.Fatalf("发件人应取解析值: %q", got)
	}
	if !strings.Contains(got, "Subject: [Chalkline] 门禁待审批") {
		t.Fatalf("邮件应含事件主题: %q", got)
	}
}

// TestSMTPResolveDisabled 未启用（enabled=false）时静默跳过，不拨号。
func TestSMTPResolveDisabled(t *testing.T) {
	m := &SMTP{To: []string{"pm@example.com"},
		Resolve: func(context.Context) (SMTPRuntime, bool) {
			return SMTPRuntime{Enabled: false}, true
		}}
	if err := m.Send(context.Background(), ev(22, 0)); err != nil {
		t.Fatalf("未启用应静默成功: %v", err)
	}
}

// TestSMTPResolveIncomplete 配置不完整（host 空）时静默跳过，不报错。
func TestSMTPResolveIncomplete(t *testing.T) {
	m := &SMTP{To: []string{"pm@example.com"},
		Resolve: func(context.Context) (SMTPRuntime, bool) {
			return SMTPRuntime{Enabled: true, From: "chalk@example.com"}, true
		}}
	if err := m.Send(context.Background(), ev(23, 0)); err != nil {
		t.Fatalf("配置不完整应静默成功: %v", err)
	}
}

// TestSMTPSendTest 测试邮件走完整投递路径：主题与收件人按入参落盘。
func TestSMTPSendTest(t *testing.T) {
	var mail safeBuffer
	l, err := newInmemorySMTP(&mail)
	if err != nil {
		t.Fatalf("起桩失败: %v", err)
	}
	defer func() { _ = l.Close() }()
	go serveSMTP(l, &mail) //nolint:errcheck // 测试桩

	m := &SMTP{Timeout: 2 * time.Second}
	cfg := SMTPRuntime{Enabled: true, Host: "127.0.0.1",
		Port: l.Addr().(*net.TCPAddr).Port, From: "chalk@example.com", TLSMode: "none"}
	if err := m.SendTest(context.Background(), cfg, []string{"pm@example.com"}); err != nil {
		t.Fatalf("测试邮件发送失败: %v", err)
	}
	got := mail.String()
	if !strings.Contains(got, "Subject: [Chalkline] 测试邮件") {
		t.Fatalf("测试邮件应含固定主题: %q", got)
	}
	if !strings.Contains(got, "To: pm@example.com") {
		t.Fatalf("测试邮件应发给指定收件人: %q", got)
	}
}
