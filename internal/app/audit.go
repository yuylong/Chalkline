package app

// audit.go：审计留痕检索（M3，AGENT.md「审计留痕检索」）。
// 权限：admin 可全局检索；非 admin 必须指定产品且为该产品成员（产品内开放、
// 跨产品隔离，不变量 16——非成员对产品的审计同样返回 404，不暴露存在性）。

import (
	"context"
	"time"

	"github.com/yuylong/Chalkline/internal/store"
)

// AuditQuery 是审计检索入参（REST / 控制台共用）。
type AuditQuery struct {
	ProductID   string
	ActorID     string
	Action      string
	SubjectType string
	SubjectID   string
	From        *time.Time
	To          *time.Time
	Limit       int
	Offset      int
}

// AuditLogView 是审计条目视图。
type AuditLogView struct {
	ID          int64          `json:"id"`
	ActorID     string         `json:"actorId"`
	ActorName   string         `json:"actorName"`
	Action      string         `json:"action"`
	SubjectType string         `json:"subjectType"`
	SubjectID   string         `json:"subjectId"`
	Payload     map[string]any `json:"payload"`
	CreatedAt   string         `json:"createdAt"`
}

// AuditListResult 是审计检索分页结果。
type AuditListResult struct {
	Items []AuditLogView `json:"items"`
	Total int            `json:"total"`
}

// SearchAuditLogs 检索审计：admin 全局；非 admin 须带 ProductID 且为成员。
func (s *Service) SearchAuditLogs(ctx context.Context, actor Actor, f AuditQuery) (AuditListResult, error) {
	q := s.DB.Q()
	if f.ProductID != "" {
		if _, err := s.mustView(ctx, q, actor, f.ProductID); err != nil {
			return AuditListResult{}, err
		}
	} else if !actor.IsAdmin {
		return AuditListResult{}, RoleRequired("非 admin 检索须指定 productId（产品内可见）")
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	rows, err := store.ListAuditLogs(ctx, q, store.AuditFilter{
		ActorID: f.ActorID, Action: f.Action, SubjectType: f.SubjectType,
		SubjectID: f.SubjectID, ProductID: f.ProductID, From: f.From, To: f.To,
		Limit: f.Limit, Offset: f.Offset,
	})
	if err != nil {
		return AuditListResult{}, err
	}
	total, err := store.CountAuditLogs(ctx, q, store.AuditFilter{
		ActorID: f.ActorID, Action: f.Action, SubjectType: f.SubjectType,
		SubjectID: f.SubjectID, ProductID: f.ProductID, From: f.From, To: f.To,
	})
	if err != nil {
		return AuditListResult{}, err
	}
	items := make([]AuditLogView, 0, len(rows))
	for _, r := range rows {
		items = append(items, AuditLogView{
			ID: r.ID, ActorID: r.ActorID, ActorName: r.ActorName, Action: r.Action,
			SubjectType: r.SubjectType, SubjectID: r.SubjectID, Payload: r.Payload,
			CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return AuditListResult{Items: items, Total: total}, nil
}
