package notify

// smtp_recipients_test.go：收件人按事件解析（相关成员邮箱）——解析结果决定
// To 头与 RCPT TO；解析不出时回退静态兜底列表；两者皆空则静默不发。

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/yuylong/Chalkline/internal/store"
)

// newStubSMTP 起一个内存 SMTP 桩，返回投递端口、信体与 RCPT 记录。
func newStubSMTP(t *testing.T) (port int, mail, rcpts *safeBuffer) {
	t.Helper()
	mail, rcpts = &safeBuffer{}, &safeBuffer{}
	l, err := newInmemorySMTP(mail)
	if err != nil {
		t.Fatalf("起桩失败: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go serveSMTP(l, mail, rcpts) //nolint:errcheck // 测试桩
	return l.Addr().(*net.TCPAddr).Port, mail, rcpts
}

// TestSMTPRecipientsFromResolver 注入的解析结果决定收件人（To 头与 RCPT TO）。
func TestSMTPRecipientsFromResolver(t *testing.T) {
	port, mail, rcpts := newStubSMTP(t)
	m := &SMTP{Timeout: 2 * time.Second,
		To:      []string{"ops@example.com"},
		Resolve: staticRuntime(port),
		Recipients: func(context.Context, store.OutboxRow) ([]string, error) {
			return []string{"dev@example.com", "qa@example.com"}, nil
		}}
	if err := m.Send(context.Background(), ev(41, 0)); err != nil {
		t.Fatalf("投递失败: %v", err)
	}
	if got := mail.String(); !strings.Contains(got, "To: dev@example.com, qa@example.com") {
		t.Fatalf("To 头应为解析出的相关用户: %q", got)
	}
	got := rcpts.String()
	for _, want := range []string{"dev@example.com", "qa@example.com"} {
		if !strings.Contains(got, want) {
			t.Fatalf("RCPT TO 应含 %s: %q", want, got)
		}
	}
	if strings.Contains(got, "ops@example.com") {
		t.Fatalf("解析出相关用户时不应再发兜底收件人: %q", got)
	}
}

// TestSMTPRecipientsFallback 解析不出相关用户时回退静态兜底列表。
func TestSMTPRecipientsFallback(t *testing.T) {
	port, mail, _ := newStubSMTP(t)
	m := &SMTP{Timeout: 2 * time.Second,
		To:      []string{"ops@example.com"},
		Resolve: staticRuntime(port),
		Recipients: func(context.Context, store.OutboxRow) ([]string, error) {
			return nil, nil
		}}
	if err := m.Send(context.Background(), ev(42, 0)); err != nil {
		t.Fatalf("投递失败: %v", err)
	}
	if got := mail.String(); !strings.Contains(got, "To: ops@example.com") {
		t.Fatalf("无相关用户时应回退兜底收件人: %q", got)
	}
}

// TestSMTPRecipientsNoneSkips 既无相关用户也无兜底时不发信（不拨号）。
func TestSMTPRecipientsNoneSkips(t *testing.T) {
	m := &SMTP{Host: "0.0.0.0", Port: 1, From: "chalk@example.com",
		Recipients: func(context.Context, store.OutboxRow) ([]string, error) {
			return nil, nil
		}}
	if err := m.Send(context.Background(), ev(43, 0)); err != nil {
		t.Fatalf("无收件人应静默跳过: %v", err)
	}
}

// TestSMTPRecipientsErrorFails 解析失败按投递失败处理（交由投递器退避重试）。
func TestSMTPRecipientsErrorFails(t *testing.T) {
	m := &SMTP{Host: "127.0.0.1", Port: 1, From: "chalk@example.com",
		To: []string{"ops@example.com"},
		Recipients: func(context.Context, store.OutboxRow) ([]string, error) {
			return nil, fmt.Errorf("数据库不可用")
		}}
	if err := m.Send(context.Background(), ev(44, 0)); err == nil {
		t.Fatal("解析收件人失败应返回错误")
	}
}

// staticRuntime 固定解析出一份指向桩服务器的发件配置。
func staticRuntime(port int) func(context.Context) (SMTPRuntime, bool) {
	return func(context.Context) (SMTPRuntime, bool) {
		return SMTPRuntime{Enabled: true, Host: "127.0.0.1", Port: port,
			From: "chalk@example.com"}, true
	}
}
