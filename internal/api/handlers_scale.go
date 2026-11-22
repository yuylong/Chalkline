package api

// handlers_scale.go：M3 规模化端点——审计留痕检索与 Webhook 订阅管理。
// 只做参数转换与错误映射；权限与过滤规则在 app 层（docs/structure.md）。

import (
	"net/http"
	"time"

	"github.com/yuylong/Chalkline/internal/app"
)

// parseTimeParam 解析可选 RFC3339 时间参数。
func parseTimeParam(r *http.Request, key string) (*time.Time, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil, app.Invalid("参数 %s 需为 ISO 8601（RFC3339）时间", key)
	}
	return &t, nil
}

// auditLogs 是 GET /api/v1/audit-logs（admin 全局）与
// GET /api/v1/products/{pid}/audit-logs（产品成员）共用 handler。
func (h *handlers) auditLogs(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	from, err := parseTimeParam(r, "from")
	if err != nil {
		writeAppError(w, err)
		return
	}
	to, err := parseTimeParam(r, "to")
	if err != nil {
		writeAppError(w, err)
		return
	}
	// 产品路径参数优先（产品内检索）；全局检索须 admin（app 层判定）。
	productID := pathParam(r, "id")
	if productID == "" {
		productID = r.URL.Query().Get("productId")
	}
	result, err := h.svc().SearchAuditLogs(r.Context(), actor, app.AuditQuery{
		ProductID:   productID,
		ActorID:     r.URL.Query().Get("actorId"),
		Action:      r.URL.Query().Get("action"),
		SubjectType: r.URL.Query().Get("subjectType"),
		SubjectID:   r.URL.Query().Get("subjectId"),
		From:        from,
		To:          to,
		Limit:       queryInt(r, "limit", 0),
		Offset:      queryInt(r, "offset", 0),
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ---- Webhook 订阅管理（admin）----

type createWebhookReq struct {
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	EventTypes []string `json:"eventTypes"`
}

func (h *handlers) createWebhook(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req createWebhookReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().CreateWebhook(r.Context(), actor, app.CreateWebhookInput{
		Name: req.Name, URL: req.URL, EventTypes: req.EventTypes,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (h *handlers) listWebhooks(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	items, err := h.svc().ListWebhooks(r.Context(), actor)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type updateWebhookReq struct {
	Name       *string   `json:"name"`
	URL        *string   `json:"url"`
	EventTypes *[]string `json:"eventTypes"`
	Status     *string   `json:"status"`
}

func (h *handlers) updateWebhook(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req updateWebhookReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().UpdateWebhook(r.Context(), actor, pathParam(r, "id"), app.UpdateWebhookInput{
		Name: req.Name, URL: req.URL, EventTypes: req.EventTypes, Status: req.Status,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *handlers) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	if err := h.svc().DeleteWebhook(r.Context(), actor, pathParam(r, "id")); err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
