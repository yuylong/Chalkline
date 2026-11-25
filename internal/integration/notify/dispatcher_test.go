package notify

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yuylong/Chalkline/internal/store"
)

// mockQueue 记录投递器对队列的全部推进动作。
type mockQueue struct {
	due      []store.OutboxRow
	sent     []int64
	failed   []int64
	debounce map[int64]int // FailOutbox 收到的 backoff
}

func (q *mockQueue) ListDueOutbox(_ context.Context, _ int) ([]store.OutboxRow, error) {
	return q.due, nil
}
func (q *mockQueue) MarkOutboxSent(_ context.Context, id int64) error {
	q.sent = append(q.sent, id)
	return nil
}
func (q *mockQueue) FailOutbox(_ context.Context, id int64, _ string, backoff time.Duration) error {
	if q.debounce == nil {
		q.debounce = map[int64]int{}
	}
	if backoff == 0 {
		q.failed = append(q.failed, id)
	} else {
		q.debounce[id] = int(backoff / time.Second)
	}
	return nil
}

type mockSender struct {
	name   string
	fail   func(ev store.OutboxRow) error
	called []int64
}

func (s *mockSender) Name() string { return s.name }
func (s *mockSender) Send(_ context.Context, ev store.OutboxRow) error {
	s.called = append(s.called, ev.ID)
	if s.fail != nil {
		return s.fail(ev)
	}
	return nil
}

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func ev(id int64, attempts int) store.OutboxRow {
	return store.OutboxRow{ID: id, EventType: "gate.pending", SubjectType: "task",
		SubjectID: fmt.Sprintf("tsk_%d", id), Attempts: attempts,
		Payload: map[string]any{"node": "node_design"}, CreatedAt: time.Now()}
}

// TestDispatchSuccess 成功投递 → 标 sent，全部 sender 都收到。
func TestDispatchSuccess(t *testing.T) {
	q := &mockQueue{due: []store.OutboxRow{ev(1, 0)}}
	a, b := &mockSender{name: "a"}, &mockSender{name: "b"}
	d := &Dispatcher{Queue: q, Senders: []Sender{a, b}, Log: testLogger()}
	d.dispatchOnce(context.Background())
	if len(q.sent) != 1 || q.sent[0] != 1 {
		t.Fatalf("应标 sent: %v", q.sent)
	}
	if len(a.called) != 1 || len(b.called) != 1 {
		t.Fatalf("两个 sender 都应收到: %v %v", a.called, b.called)
	}
}

// TestDispatchNoSenders 未配置通道 → 直接标 sent（事件不滞留 pending）。
func TestDispatchNoSenders(t *testing.T) {
	q := &mockQueue{due: []store.OutboxRow{ev(2, 0)}}
	d := &Dispatcher{Queue: q, Log: testLogger()}
	d.dispatchOnce(context.Background())
	if len(q.sent) != 1 {
		t.Fatalf("无通道时应直接标 sent: %v", q.sent)
	}
}

// TestDispatchRetryBackoff 失败 → 计数并按指数退避（30s/60s/120s/240s）。
func TestDispatchRetryBackoff(t *testing.T) {
	q := &mockQueue{due: []store.OutboxRow{ev(3, 0)}}
	s := &mockSender{name: "w", fail: func(store.OutboxRow) error { return fmt.Errorf("超时") }}
	d := &Dispatcher{Queue: q, Senders: []Sender{s}, Log: testLogger()}
	d.dispatchOnce(context.Background())
	if len(q.sent) != 0 {
		t.Fatalf("失败不应标 sent: %v", q.sent)
	}
	if q.debounce[3] != 30 {
		t.Fatalf("第 1 次失败应退避 30s: %v", q.debounce)
	}

	// 第 2~4 次失败：60s / 120s / 240s。
	for i, want := range []int{60, 120, 240} {
		q.due = []store.OutboxRow{ev(3, i+1)}
		d.dispatchOnce(context.Background())
		if q.debounce[3] != want {
			t.Fatalf("第 %d 次失败应退避 %ds: %v", i+2, want, q.debounce)
		}
	}
}

// TestDispatchMaxAttemptsFailed 达到 5 次尝试（已有 4 次失败）→ 标 failed 不再退避。
func TestDispatchMaxAttemptsFailed(t *testing.T) {
	q := &mockQueue{due: []store.OutboxRow{ev(4, 4)}} // 已失败 4 次
	s := &mockSender{name: "w", fail: func(store.OutboxRow) error { return fmt.Errorf("仍然失败") }}
	d := &Dispatcher{Queue: q, Senders: []Sender{s}, Log: testLogger()}
	d.dispatchOnce(context.Background())
	if len(q.failed) != 1 || q.failed[0] != 4 {
		t.Fatalf("第 5 次失败应标 failed: %v", q.failed)
	}
	if len(q.sent) != 0 {
		t.Fatalf("不应标 sent: %v", q.sent)
	}
}

// TestWebhookSendAndSignature 接收端校验 HMAC-SHA256 签名头；载荷只含元数据。
func TestWebhookSendAndSignature(t *testing.T) {
	var gotBody []byte
	var gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		gotSig = r.Header.Get("X-Chalkline-Signature")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	wh := &Webhook{URL: srv.URL, Secret: "whsec"}
	if err := wh.Send(context.Background(), ev(9, 0)); err != nil {
		t.Fatalf("投递失败: %v", err)
	}
	if want := Sign("whsec", gotBody); gotSig != want {
		t.Fatalf("签名头不符: got %q want %q", gotSig, want)
	}
	if !strings.Contains(string(gotBody), `"event_type":"gate.pending"`) {
		t.Fatalf("载荷应含事件类型: %s", gotBody)
	}
	if strings.Contains(string(gotBody), `"content"`) {
		t.Fatalf("载荷不得携带材料正文: %s", gotBody)
	}
}

// TestWebhookNon2xxFails 非 2xx 响应计失败（触发重试路径）。
func TestWebhookNon2xxFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer srv.Close()
	wh := &Webhook{URL: srv.URL}
	if err := wh.Send(context.Background(), ev(10, 0)); err == nil {
		t.Fatal("502 应视为失败")
	}
}

// TestSMTPSendAgainstStub 用内存 SMTP 桩验证完整投递（HELO→DATA→QUIT）。
func TestSMTPSendAgainstStub(t *testing.T) {
	var mail safeBuffer
	l, err := newInmemorySMTP(&mail)
	if err != nil {
		t.Fatalf("起桩失败: %v", err)
	}
	defer func() { _ = l.Close() }()
	go serveSMTP(l, &mail) //nolint:errcheck // 测试桩

	m := &SMTP{Host: "127.0.0.1", Port: l.Addr().(*net.TCPAddr).Port,
		From: "chalk@example.com", To: []string{"pm@example.com"}, Timeout: 2 * time.Second}
	if err := m.Send(context.Background(), ev(11, 0)); err != nil {
		t.Fatalf("投递失败: %v", err)
	}
	got := mail.String()
	if !strings.Contains(got, "Subject: [Chalkline] 门禁待审批") {
		t.Fatalf("邮件应含事件主题: %q", got)
	}
	if !strings.Contains(got, "gate.pending") {
		t.Fatalf("邮件正文应含事件元数据: %q", got)
	}
}

// TestSMTPIgnoresUnknownEvent 无邮件语义的事件（不在 notifyEvents 内）直接成功跳过。
func TestSMTPIgnoresUnknownEvent(t *testing.T) {
	m := &SMTP{Host: "0.0.0.0", Port: 1, From: "x@y", To: []string{"z@w"}} // 不可达也不该拨号
	e := ev(12, 0)
	e.EventType = "workflow.updated"
	if err := m.Send(context.Background(), e); err != nil {
		t.Fatalf("非邮件事件应跳过: %v", err)
	}
}

// TestSMTPUnconfiguredIsNoop 未配置（Host 空）时不报错（不阻断 webhook 投递）。
func TestSMTPUnconfiguredIsNoop(t *testing.T) {
	m := &SMTP{}
	if err := m.Send(context.Background(), ev(13, 0)); err != nil {
		t.Fatalf("未配置应静默成功: %v", err)
	}
}
