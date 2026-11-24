package api

// handlers_events.go：SSE 实时推送端点（M3，docs/modules/console.md「实时性」）。
// 事件来源是 outbox（唯一事实），经进程内 hub 分发；本 handler 只做
// 可见性过滤（不变量 16：非成员看不到其他产品的事件）与协议输出。
// 鉴权走 REST JWT（EventSource 不支持自定义头，控制台用 fetch 流式读取）。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	sseHeartbeat         = 15 * time.Second
	sseVisibilityRefresh = time.Minute // 成员关系快照刷新周期
)

// eventsStream 是 GET /api/v1/events：text/event-stream 长连接。
func (h *handlers) eventsStream(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	if h.deps.Events == nil {
		WriteError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "实时推送未启用", nil)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "当前连接不支持流式输出", nil)
		return
	}

	// SSE 是长连接：解除服务器级 WriteTimeout 对本请求的限制。
	if rc := http.NewResponseController(w); rc != nil {
		_ = rc.SetWriteDeadline(time.Time{})
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // nginx 不缓冲
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	// 可见性快照：成员关系变更在刷新周期内生效（单实例规模下足够，文档已注明）。
	visible := map[string]bool{}
	refresh := func() {
		ids, err := h.svc().ListVisibleProductIDs(r.Context(), actor)
		if err != nil {
			return
		}
		visible = make(map[string]bool, len(ids))
		for _, id := range ids {
			visible[id] = true
		}
	}
	refresh()

	ch, cancel := h.deps.Events.Subscribe()
	defer cancel()

	heartbeat := time.NewTicker(sseHeartbeat)
	defer heartbeat.Stop()
	visRefresh := time.NewTicker(sseVisibilityRefresh)
	defer visRefresh.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-visRefresh.C:
			refresh()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			// 可见性过滤：admin 全量；非 admin 只收自己产品的事件。
			if !actor.IsAdmin {
				if ev.ProductID == "" || !visible[ev.ProductID] {
					continue
				}
			}
			body, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.EventType, body); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
