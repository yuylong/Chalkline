package notify

// dispatcher_subs_test.go：M3 订阅路由——event_types 过滤、按订阅投递、
// 订阅失败不阻断事件（尽力而为，记在订阅行）。

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yuylong/Chalkline/internal/store"
)

type mockSubs struct {
	subs     []store.ActiveWebhookSubscription
	marked   []string // id:ok
	failURLs map[string]bool
}

func (m *mockSubs) ListActiveWebhookSubscriptions(context.Context) ([]store.ActiveWebhookSubscription, error) {
	return m.subs, nil
}
func (m *mockSubs) MarkWebhookDelivery(_ context.Context, id string, ok bool, _ string) error {
	m.marked = append(m.marked, fmt.Sprintf("%s:%v", id, ok))
	return nil
}

func TestSubscriptionMatches(t *testing.T) {
	if !subscriptionMatches(nil, "gate.pending") {
		t.Fatal("空 event_types 应订阅全部事件")
	}
	if !subscriptionMatches([]string{"gate.pending", "node.passed"}, "node.passed") {
		t.Fatal("命中列表应匹配")
	}
	if subscriptionMatches([]string{"node.ready"}, "gate.pending") {
		t.Fatal("未命中列表不应匹配")
	}
}

// TestDispatchSubscriptions 订阅按 event_types 收到各自事件；事件本身标 sent。
func TestDispatchSubscriptions(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("X-Chalkline-Subscription-Id") == "" {
			t.Error("订阅投递应带 X-Chalkline-Subscription-Id 头")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	q := &mockQueue{due: []store.OutboxRow{ev(11, 0)}}
	subs := &mockSubs{subs: []store.ActiveWebhookSubscription{
		{ID: "whk_1", URL: srv.URL, EventTypes: nil},
		{ID: "whk_2", URL: srv.URL, EventTypes: []string{"node.ready"}},
	}}
	d := &Dispatcher{Queue: q, Log: testLogger(), Subs: subs, WebhookSecret: "s"}
	d.dispatchOnce(context.Background())

	// whk_2 订阅 node.ready，本事件是 gate.pending → 不投递。
	if hits != 1 {
		t.Fatalf("只有 whk_1 应投递，实际命中 %d 次", hits)
	}
	if len(subs.marked) != 1 || subs.marked[0] != "whk_1:true" {
		t.Fatalf("whk_1 应投递成功: %v", subs.marked)
	}
	if len(q.sent) != 1 || q.sent[0] != 11 {
		t.Fatalf("订阅投递后事件应标 sent: %v", q.sent)
	}
}

// TestDispatchSubscriptionFailureIsBestEffort 订阅 URL 失败只记 last_error，
// 不让事件重试（静态通道语义不变）。
func TestDispatchSubscriptionFailureIsBestEffort(t *testing.T) {
	q := &mockQueue{due: []store.OutboxRow{ev(12, 0)}}
	subs := &mockSubs{subs: []store.ActiveWebhookSubscription{
		{ID: "whk_bad", URL: ""}, // 空 URL → Send 失败
	}}
	d := &Dispatcher{Queue: q, Log: testLogger(), Subs: subs}
	d.dispatchOnce(context.Background())

	if len(subs.marked) != 1 || subs.marked[0] != "whk_bad:false" {
		t.Fatalf("失败应记在订阅行: %v", subs.marked)
	}
	if len(q.sent) != 1 || q.sent[0] != 12 {
		t.Fatalf("订阅失败不应阻断事件: %v", q.sent)
	}
	if len(q.failed) != 0 {
		t.Fatalf("订阅失败不应触发事件重试: %v", q.failed)
	}
}
