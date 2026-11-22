// Package notify 消费 outbox_events 异步投递通知（docs/modules/integration.md）。
// 投递失败指数退避重试最多 5 次，超过标 failed；任何失败不阻断流程（不变量 15）。
package notify

import (
	"context"
	"log/slog"
	"time"

	"github.com/yuylong/Chalkline/internal/store"
)

// Sender 是一种投递通道（webhook / smtp）。实现必须自身幂等安全：
// 同一事件可能因重试被多次调用。
type Sender interface {
	Name() string
	Send(ctx context.Context, ev store.OutboxRow) error
}

// Queue 是 outbox 队列的存储访问（生产由 *store.DB 实现，测试可 mock）。
type Queue interface {
	ListDueOutbox(ctx context.Context, limit int) ([]store.OutboxRow, error)
	MarkOutboxSent(ctx context.Context, id int64) error
	FailOutbox(ctx context.Context, id int64, lastErr string, backoff time.Duration) error
}

// SubscriptionSource 是 Webhook 订阅的存储访问（M3 订阅管理；nil 则只有静态通道）。
// 生产由 *store.DB 实现。
type SubscriptionSource interface {
	ListActiveWebhookSubscriptions(ctx context.Context) ([]store.ActiveWebhookSubscription, error)
	MarkWebhookDelivery(ctx context.Context, id string, ok bool, lastErr string) error
}

// Dispatcher 是后台投递器：轮询 due 事件 → 逐条经全部 Sender 发送。
// 任一 Sender 失败即计一次失败（整条事件重试），保证接收方看到的事件序完整。
// M3 起 Subs 非空时，事件另按订阅（event_types 过滤）逐个投递到订阅方 URL；
// 订阅投递是尽力而为：失败记在订阅行（last_error），不阻断事件流转。
type Dispatcher struct {
	Queue       Queue
	Senders     []Sender
	Log         *slog.Logger
	Interval    time.Duration // 轮询间隔，默认 5s
	MaxAttempts int           // 最大尝试次数，默认 5
	BatchLimit  int           // 每轮最多处理条数，默认 50
	BackoffBase time.Duration // 退避基数：第 n 次失败后等待 base * 2^(n-1)

	// Subs 是 Webhook 订阅源（M3；nil 则不投订阅）。
	Subs SubscriptionSource
	// WebhookSecret 是订阅投递的 HMAC 签名密钥（全局 CHALKLINE_WEBHOOK_SECRET，
	// 凭据不落库；订阅行只存 URL 与事件过滤）。
	WebhookSecret string
}

func (d *Dispatcher) interval() time.Duration {
	if d.Interval > 0 {
		return d.Interval
	}
	return 5 * time.Second
}

func (d *Dispatcher) maxAttempts() int {
	if d.MaxAttempts > 0 {
		return d.MaxAttempts
	}
	return 5
}

func (d *Dispatcher) batchLimit() int {
	if d.BatchLimit > 0 {
		return d.BatchLimit
	}
	return 50
}

func (d *Dispatcher) backoffBase() time.Duration {
	if d.BackoffBase > 0 {
		return d.BackoffBase
	}
	return 30 * time.Second
}

// Run 阻塞轮询直到 ctx 结束（由 server 在数据库就绪后启动）。
func (d *Dispatcher) Run(ctx context.Context) {
	d.Log.Info("outbox 投递器启动",
		"interval", d.interval().String(), "max_attempts", d.maxAttempts(),
		"senders", len(d.Senders))
	ticker := time.NewTicker(d.interval())
	defer ticker.Stop()
	for {
		d.dispatchOnce(ctx)
		select {
		case <-ctx.Done():
			d.Log.Info("outbox 投递器停止")
			return
		case <-ticker.C:
		}
	}
}

// dispatchOnce 处理一轮 due 事件；统计写日志，错误不中断循环。
func (d *Dispatcher) dispatchOnce(ctx context.Context) {
	events, err := d.Queue.ListDueOutbox(ctx, d.batchLimit())
	if err != nil {
		d.Log.Error("outbox 读取失败", "error", err)
		return
	}
	for _, ev := range events {
		d.dispatchOne(ctx, ev)
	}
}

func (d *Dispatcher) dispatchOne(ctx context.Context, ev store.OutboxRow) {
	hasSubs := d.Subs != nil
	if len(d.Senders) == 0 && !hasSubs {
		// 无通道（未配置 webhook/smtp 且无订阅）：直接标记完成，避免事件无限滞留 pending。
		if err := d.Queue.MarkOutboxSent(ctx, ev.ID); err != nil {
			d.Log.Error("outbox 标记完成失败", "id", ev.ID, "error", err)
		}
		return
	}
	for _, s := range d.Senders {
		if err := s.Send(ctx, ev); err != nil {
			d.fail(ctx, ev, s.Name()+": "+err.Error())
			return
		}
	}
	if hasSubs {
		d.deliverSubscriptions(ctx, ev)
	}
	if err := d.Queue.MarkOutboxSent(ctx, ev.ID); err != nil {
		d.Log.Error("outbox 标记完成失败", "id", ev.ID, "error", err)
		return
	}
	d.Log.Debug("outbox 事件已投递", "id", ev.ID, "event_type", ev.EventType)
}

// deliverSubscriptions 按订阅投递：event_types 为空即全订阅；
// 单个订阅失败只记在该订阅行，不影响事件与其他订阅（尽力而为）。
func (d *Dispatcher) deliverSubscriptions(ctx context.Context, ev store.OutboxRow) {
	subs, err := d.Subs.ListActiveWebhookSubscriptions(ctx)
	if err != nil {
		d.Log.Error("webhook 订阅读取失败", "error", err)
		return
	}
	for _, sub := range subs {
		if !subscriptionMatches(sub.EventTypes, ev.EventType) {
			continue
		}
		wh := &Webhook{URL: sub.URL, Secret: d.WebhookSecret, SubscriptionID: sub.ID}
		sendErr := wh.Send(ctx, ev)
		lastErr := ""
		if sendErr != nil {
			lastErr = sendErr.Error()
			d.Log.Warn("webhook 订阅投递失败", "subscription", sub.ID,
				"event_type", ev.EventType, "error", sendErr)
		}
		if err := d.Subs.MarkWebhookDelivery(ctx, sub.ID, sendErr == nil, lastErr); err != nil {
			d.Log.Error("webhook 投递记录失败", "subscription", sub.ID, "error", err)
		}
	}
}

// subscriptionMatches 事件类型过滤（空列表 = 订阅全部）。
func subscriptionMatches(eventTypes []string, eventType string) bool {
	if len(eventTypes) == 0 {
		return true
	}
	for _, t := range eventTypes {
		if t == eventType {
			return true
		}
	}
	return false
}

// fail 记一次失败：未达上限则按指数退避延后重试，达到上限标 failed。
func (d *Dispatcher) fail(ctx context.Context, ev store.OutboxRow, lastErr string) {
	attempts := ev.Attempts + 1
	if attempts >= d.maxAttempts() {
		if err := d.Queue.FailOutbox(ctx, ev.ID, lastErr, 0); err != nil {
			d.Log.Error("outbox 标记失败态出错", "id", ev.ID, "error", err)
		}
		d.Log.Warn("outbox 事件投递失败已达上限，标记 failed",
			"id", ev.ID, "event_type", ev.EventType, "attempts", attempts, "last_error", lastErr)
		return
	}
	backoff := d.backoffBase() << (attempts - 1) // 30s, 60s, 120s, 240s
	if err := d.Queue.FailOutbox(ctx, ev.ID, lastErr, backoff); err != nil {
		d.Log.Error("outbox 失败记录出错", "id", ev.ID, "error", err)
		return
	}
	d.Log.Warn("outbox 事件投递失败，稍后重试",
		"id", ev.ID, "event_type", ev.EventType, "attempts", attempts,
		"backoff", backoff.String(), "last_error", lastErr)
}
