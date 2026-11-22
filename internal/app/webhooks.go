package app

// webhooks.go：Webhook 订阅管理（M3 开放 API 与事件订阅，docs/modules/integration.md）。
// 订阅只存 URL / 事件类型过滤 / 状态；签名密钥沿用全局 CHALKLINE_WEBHOOK_SECRET
//（凭据不落库，AGENT 红线 8）。管理权限收敛到 admin（对外开放策略见 integrations.md）。

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/yuylong/Chalkline/internal/store"
)

// WebhookView 是订阅视图。
type WebhookView struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	URL            string   `json:"url"`
	EventTypes     []string `json:"eventTypes"` // 空 = 订阅全部事件
	Status         string   `json:"status"`
	CreatedBy      string   `json:"createdBy"`
	CreatedAt      string   `json:"createdAt"`
	LastDeliveryAt string   `json:"lastDeliveryAt,omitempty"`
	LastDeliveryOK *bool    `json:"lastDeliveryOk,omitempty"`
	LastError      string   `json:"lastError,omitempty"`
}

func viewWebhook(w store.WebhookRow) WebhookView {
	v := WebhookView{
		ID: w.ID, Name: w.Name, URL: w.URL, EventTypes: w.EventTypes,
		Status: w.Status, CreatedBy: w.CreatedBy,
		CreatedAt: w.CreatedAt.UTC().Format(time.RFC3339),
	}
	if w.LastDeliveryAt != nil {
		v.LastDeliveryAt = w.LastDeliveryAt.UTC().Format(time.RFC3339)
	}
	v.LastDeliveryOK = w.LastDeliveryOK
	if w.LastError != nil {
		v.LastError = *w.LastError
	}
	if v.EventTypes == nil {
		v.EventTypes = []string{}
	}
	return v
}

// CreateWebhookInput 建订阅入参。
type CreateWebhookInput struct {
	Name       string
	URL        string
	EventTypes []string
}

// UpdateWebhookInput 改订阅入参（nil 即不改）。
type UpdateWebhookInput struct {
	Name       *string
	URL        *string
	EventTypes *[]string
	Status     *string // active | paused
}

// requireAdmin 订阅管理收敛到 admin。
func requireAdmin(actor Actor) error {
	if !actor.IsAdmin {
		return RoleRequired("Webhook 订阅管理需要 admin 角色")
	}
	return nil
}

// validateWebhookURL 订阅 URL 须为 http(s) 绝对地址；私网拦截由出站白名单在投递时兜底。
func validateWebhookURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return Invalid("webhook URL %q 非法，需为绝对地址", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return Invalid("webhook URL %q 仅支持 http/https", raw)
	}
	return nil
}

// validateEventTypes 事件类型白名单（docs/domain/integrations.md「事件与投递」）。
var webhookEventTypes = map[string]bool{
	"node.ready": true, "node.passed": true, "node.rejected": true,
	"gate.pending": true, "gate.overdue": true, "gate.escalated": true,
	"task.assigned": true, "iteration.closed": true,
}

func validateEventTypes(types []string) error {
	for _, t := range types {
		if !webhookEventTypes[t] {
			return Invalid("未知事件类型 %q（可选：%s）", t, strings.Join(webhookEventNames(), " / "))
		}
	}
	return nil
}

func webhookEventNames() []string {
	out := make([]string, 0, len(webhookEventTypes))
	for k := range webhookEventTypes {
		out = append(out, k)
	}
	return out
}

// CreateWebhook 建订阅（admin）。
func (s *Service) CreateWebhook(ctx context.Context, actor Actor, in CreateWebhookInput) (WebhookView, error) {
	if err := requireAdmin(actor); err != nil {
		return WebhookView{}, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return WebhookView{}, Invalid("订阅名称不可为空")
	}
	if err := validateWebhookURL(in.URL); err != nil {
		return WebhookView{}, err
	}
	if err := validateEventTypes(in.EventTypes); err != nil {
		return WebhookView{}, err
	}
	row := store.WebhookRow{
		ID: s.newID("whk"), Name: in.Name, URL: in.URL,
		EventTypes: in.EventTypes, Status: "active", CreatedBy: actor.UserID,
	}
	if row.EventTypes == nil {
		row.EventTypes = []string{}
	}
	err := s.DB.WithTx(ctx, func(q store.Querier) error {
		if err := store.InsertWebhook(ctx, q, row); err != nil {
			return err
		}
		return audit(ctx, q, actor, "webhook.create", "webhook", row.ID,
			map[string]any{"name": in.Name, "url": in.URL, "event_types": in.EventTypes})
	})
	if err != nil {
		return WebhookView{}, err
	}
	return viewWebhook(row), nil
}

// ListWebhooks 列全部订阅（admin）。
func (s *Service) ListWebhooks(ctx context.Context, actor Actor) ([]WebhookView, error) {
	if err := requireAdmin(actor); err != nil {
		return nil, err
	}
	rows, err := store.ListWebhooks(ctx, s.DB.Q())
	if err != nil {
		return nil, err
	}
	out := make([]WebhookView, 0, len(rows))
	for _, w := range rows {
		out = append(out, viewWebhook(w))
	}
	return out, nil
}

// UpdateWebhook 改订阅（admin）。
func (s *Service) UpdateWebhook(ctx context.Context, actor Actor, id string, in UpdateWebhookInput) (WebhookView, error) {
	if err := requireAdmin(actor); err != nil {
		return WebhookView{}, err
	}
	if in.URL != nil {
		if err := validateWebhookURL(*in.URL); err != nil {
			return WebhookView{}, err
		}
	}
	if in.EventTypes != nil {
		if err := validateEventTypes(*in.EventTypes); err != nil {
			return WebhookView{}, err
		}
	}
	if in.Status != nil && *in.Status != "active" && *in.Status != "paused" {
		return WebhookView{}, Invalid("status %q 非法，应为 active / paused", *in.Status)
	}
	var out WebhookView
	err := s.DB.WithTx(ctx, func(q store.Querier) error {
		if err := store.UpdateWebhook(ctx, q, id, store.WebhookUpdate{
			Name: in.Name, URL: in.URL, EventTypes: in.EventTypes, Status: in.Status,
		}); err != nil {
			return wrapNotFound(err, "订阅不存在")
		}
		if err := audit(ctx, q, actor, "webhook.update", "webhook", id, map[string]any{
			"status": in.Status, "name": in.Name}); err != nil {
			return err
		}
		row, err := store.WebhookByID(ctx, q, id)
		if err != nil {
			return err
		}
		out = viewWebhook(row)
		return nil
	})
	return out, err
}

// DeleteWebhook 删订阅（admin）。
func (s *Service) DeleteWebhook(ctx context.Context, actor Actor, id string) error {
	if err := requireAdmin(actor); err != nil {
		return err
	}
	return s.DB.WithTx(ctx, func(q store.Querier) error {
		if err := store.DeleteWebhook(ctx, q, id); err != nil {
			return wrapNotFound(err, "订阅不存在")
		}
		return audit(ctx, q, actor, "webhook.delete", "webhook", id, nil)
	})
}
