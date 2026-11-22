package events

import (
	"testing"
	"time"
)

func TestHubFanOut(t *testing.T) {
	h := NewHub()
	a, cancelA := h.Subscribe()
	b, cancelB := h.Subscribe()
	defer cancelB()

	h.Publish(Event{ID: 1, EventType: "node.passed"})
	if ev := <-a; ev.ID != 1 {
		t.Fatalf("订阅者 a 应收到事件 1，实际 %d", ev.ID)
	}
	if ev := <-b; ev.ID != 1 {
		t.Fatalf("订阅者 b 应收到事件 1，实际 %d", ev.ID)
	}

	// 取消后不再接收；重复取消安全。
	cancelA()
	cancelA()
	h.Publish(Event{ID: 2})
	select {
	case ev, ok := <-a:
		if ok {
			t.Fatalf("已取消的订阅不应收到事件，实际 %d", ev.ID)
		}
	case <-time.After(10 * time.Millisecond):
	}
	if ev := <-b; ev.ID != 2 {
		t.Fatalf("订阅者 b 应继续收到事件 2，实际 %d", ev.ID)
	}
}

func TestHubSlowSubscriberDropsOldest(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe()
	defer cancel()

	// 缓冲 16：灌 20 条，慢订阅者应丢最旧、保最新。
	for i := 1; i <= 20; i++ {
		h.Publish(Event{ID: int64(i)})
	}
	count := 0
	var last int64
	for {
		select {
		case ev := <-ch:
			count++
			last = ev.ID
		default:
			if count == 0 {
				t.Fatal("慢订阅者应至少收到缓冲内事件")
			}
			if last != 20 {
				t.Fatalf("最新事件应保留，实际最后收到 %d", last)
			}
			return
		}
	}
}
