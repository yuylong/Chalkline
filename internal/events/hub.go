// Package events 是进程内事件总线：把 outbox 领域事件实时分发给 SSE 连接
//（M3 实时推送，docs/modules/console.md「实时性」收敛为 SSE）。
// 本包只做分发，不做持久化与可见性判定——事件来源是 outbox 表（唯一事实），
// 可见性过滤在 api 层按成员关系完成（不变量 16）。
package events

import "sync"

// Event 是推送给控制台的一条领域事件（outbox 行的投影 + 产品归属）。
type Event struct {
	ID          int64          `json:"id"`
	EventType   string         `json:"eventType"`
	SubjectType string         `json:"subjectType"`
	SubjectID   string         `json:"subjectId"`
	ProductID   string         `json:"productId,omitempty"` // 非工作项主体为空
	Payload     map[string]any `json:"payload"`
}

// Hub 是 fan-out 广播器：一个发布者（outbox tailer），N 个 SSE 连接。
type Hub struct {
	mu   sync.RWMutex
	subs map[chan Event]struct{}
}

// NewHub 建总线。
func NewHub() *Hub {
	return &Hub{subs: make(map[chan Event]struct{})}
}

// Subscribe 注册一个订阅者，返回通道与取消函数（重复取消安全）。
func (h *Hub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 16)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

// Publish 广播一条事件；订阅者消费慢时丢弃最旧一条（控制台刷新是幂等查询，
// 丢事件只意味着少一次提前刷新，轮询兜底仍在）。
func (h *Hub) Publish(ev Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs {
		select {
		case ch <- ev:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- ev:
			default:
			}
		}
	}
}
