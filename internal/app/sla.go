package app

// sla.go：评审超期提醒与升级链（M3，README 路线图「评审超期提醒与升级链」）。
// 后台监控 awaiting_review 节点：超过 SLA 提醒评审人（gate.overdue），
// 超过升级阈值升级给产品 owner / admin（gate.escalated）。
// 红线 1：监控器只写 gate_reminders 水位与 outbox 事件，不改任何节点状态。

import (
	"context"
	"log/slog"
	"time"

	"github.com/yuylong/Chalkline/internal/store"
)

// systemActor 是后台监控器的审计身份（不变量 13：写操作带 actor_id）。
var systemActor = Actor{UserID: "system"}

// reviewEscalation 升级阈值（未配置时默认 3×SLA）。
func (s *Service) reviewEscalation() time.Duration {
	if s.ReviewEscalation > 0 {
		return s.ReviewEscalation
	}
	return 3 * s.reviewSLA()
}

// SLAReminderLevel 依超期时长判定提醒级别：0=未超期；1=提醒评审人；2=升级 owner/admin。
// 纯函数，可单测。
func SLAReminderLevel(age, sla, escalation time.Duration) int {
	switch {
	case age >= escalation:
		return 2
	case age >= sla:
		return 1
	default:
		return 0
	}
}

// RunReviewSLA 阻塞轮询直到 ctx 结束（由 server 在数据库就绪后启动）。
// 每轮：扫超期节点 → 对未发过的级别写 gate_reminders + outbox 事件 + 审计。
func (s *Service) RunReviewSLA(ctx context.Context, interval time.Duration, logger *slog.Logger) {
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	sla, escalation := s.reviewSLA(), s.reviewEscalation()
	logger.Info("评审 SLA 监控器启动",
		"interval", interval.String(), "sla", sla.String(), "escalation", escalation.String())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		s.scanOnce(ctx, sla, escalation, logger)
		select {
		case <-ctx.Done():
			logger.Info("评审 SLA 监控器停止")
			return
		case <-ticker.C:
		}
	}
}

// scanOnce 处理一轮超期节点；单条失败记日志不中断。
func (s *Service) scanOnce(ctx context.Context, sla, escalation time.Duration, logger *slog.Logger) {
	now := s.Now()
	rows, err := store.ListOverdueAwaiting(ctx, s.DB.Q(), now.Add(-sla))
	if err != nil {
		logger.Error("SLA 扫描读取失败", "error", err)
		return
	}
	for _, r := range rows {
		age := now.Sub(r.EnteredAt)
		level := SLAReminderLevel(age, sla, escalation)
		// 升级链保序：跳级（扫描间隔内直接越过升级阈值）时先补发 level 1 再发 level 2。
		for lv := 1; lv <= level; lv++ {
			if err := s.remindOnce(ctx, r, lv, age); err != nil {
				logger.Error("SLA 提醒写入失败", "error", err,
					"instance", r.InstanceID, "node", r.NodeID, "level", lv)
			}
		}
	}
}

// remindOnce 发一个级别的提醒（幂等：gate_reminders 水位去重）。
func (s *Service) remindOnce(ctx context.Context, r store.OverdueAwaitingRow, level int, age time.Duration) error {
	return s.DB.WithTx(ctx, func(q store.Querier) error {
		exists, err := store.GateReminderExists(ctx, q, r.InstanceID, r.NodeID, level)
		if err != nil {
			return err
		}
		if exists {
			return nil
		}
		if err := store.InsertGateReminder(ctx, q, r.InstanceID, r.NodeID, level); err != nil {
			return err
		}
		eventType := "gate.overdue"
		if level == 2 {
			eventType = "gate.escalated"
		}
		if err := store.InsertOutbox(ctx, q, eventType, string(r.WorkItemType), r.WorkItemID,
			map[string]any{
				"node": r.NodeID, "product_id": r.ProductID,
				"level": level, "hours_overdue": age.Hours(),
			}); err != nil {
			return err
		}
		return audit(ctx, q, systemActor, eventType, string(r.WorkItemType), r.WorkItemID,
			map[string]any{"node": r.NodeID, "level": level, "hours_overdue": age.Hours()})
	})
}
