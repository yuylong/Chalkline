package store

// webhooks.go：Webhook 订阅管理（M3 开放 API 与事件订阅，docs/modules/integration.md）。
// 订阅行只存 URL / 事件类型过滤 / 状态与最近投递结果；签名密钥沿用全局
// CHALKLINE_WEBHOOK_SECRET，不落库（AGENT 红线 8：凭据不落库）。

import (
	"context"
	"time"
)

// WebhookRow 是 webhook_subscriptions 行。
type WebhookRow struct {
	ID             string
	Name           string
	URL            string
	EventTypes     []string
	Status         string // active | paused
	CreatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastDeliveryAt *time.Time
	LastDeliveryOK *bool
	LastError      *string
}

const webhookCols = `id, name, url, event_types, status, created_by, created_at, updated_at, last_delivery_at, last_delivery_ok, last_error`

func scanWebhook(row rowScanner) (WebhookRow, error) {
	var w WebhookRow
	err := row.Scan(&w.ID, &w.Name, &w.URL, &w.EventTypes, &w.Status, &w.CreatedBy,
		&w.CreatedAt, &w.UpdatedAt, &w.LastDeliveryAt, &w.LastDeliveryOK, &w.LastError)
	return w, err
}

// InsertWebhook 写入新订阅。
func InsertWebhook(ctx context.Context, q Querier, w WebhookRow) error {
	_, err := q.Exec(ctx,
		`INSERT INTO webhook_subscriptions (id, name, url, event_types, status, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		w.ID, w.Name, w.URL, w.EventTypes, w.Status, w.CreatedBy)
	return err
}

// WebhookByID 取订阅。
func WebhookByID(ctx context.Context, q Querier, id string) (WebhookRow, error) {
	return scanWebhook(q.QueryRow(ctx, `SELECT `+webhookCols+` FROM webhook_subscriptions WHERE id = $1`, id))
}

// ListWebhooks 全部订阅（管理台用，按创建时间倒序）。
func ListWebhooks(ctx context.Context, q Querier) ([]WebhookRow, error) {
	return scanAll(ctx, q,
		`SELECT `+webhookCols+` FROM webhook_subscriptions ORDER BY created_at DESC`, scanWebhook)
}

// WebhookUpdate 是订阅可更新字段（nil 即不改）。
type WebhookUpdate struct {
	Name       *string
	URL        *string
	EventTypes *[]string
	Status     *string
}

// UpdateWebhook 更新订阅。
func UpdateWebhook(ctx context.Context, q Querier, id string, u WebhookUpdate) error {
	tag, err := q.Exec(ctx,
		`UPDATE webhook_subscriptions SET
		   name = COALESCE($2, name),
		   url = COALESCE($3, url),
		   event_types = COALESCE($4, event_types),
		   status = COALESCE($5, status),
		   updated_at = now()
		 WHERE id = $1`,
		id, u.Name, u.URL, u.EventTypes, u.Status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteWebhook 删除订阅。
func DeleteWebhook(ctx context.Context, q Querier, id string) error {
	tag, err := q.Exec(ctx, `DELETE FROM webhook_subscriptions WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkWebhookDelivery 记录一次订阅投递结果（尽力而为，不阻断事件流转）。
func MarkWebhookDelivery(ctx context.Context, q Querier, id string, ok bool, lastErr string) error {
	_, err := q.Exec(ctx,
		`UPDATE webhook_subscriptions
		 SET last_delivery_at = now(), last_delivery_ok = $2, last_error = $3, updated_at = now()
		 WHERE id = $1`, id, ok, lastErr)
	return err
}

// ---- notify 投递器侧（Queue 接口的订阅扩展）----

// ActiveWebhookSubscription 是投递器需要的最小订阅形态。
type ActiveWebhookSubscription struct {
	ID         string
	URL        string
	EventTypes []string
}

// ListActiveWebhookSubscriptions 取全部 active 订阅（投递器每轮调用）。
func ListActiveWebhookSubscriptions(ctx context.Context, q Querier) ([]ActiveWebhookSubscription, error) {
	return scanAll(ctx, q,
		`SELECT id, url, event_types FROM webhook_subscriptions WHERE status = 'active' ORDER BY id`,
		func(row rowScanner) (ActiveWebhookSubscription, error) {
			var s ActiveWebhookSubscription
			err := row.Scan(&s.ID, &s.URL, &s.EventTypes)
			return s, err
		})
}

// ---- *DB 方法形态：实现 notify.SubscriptionSource（server 装配用）----

// ListActiveWebhookSubscriptions 实现 notify.SubscriptionSource。
func (db *DB) ListActiveWebhookSubscriptions(ctx context.Context) ([]ActiveWebhookSubscription, error) {
	return ListActiveWebhookSubscriptions(ctx, db.Q())
}

// MarkWebhookDelivery 实现 notify.SubscriptionSource。
func (db *DB) MarkWebhookDelivery(ctx context.Context, id string, ok bool, lastErr string) error {
	return MarkWebhookDelivery(ctx, db.Q(), id, ok, lastErr)
}
