package app

// scale.go：多产品规模化管理（M3，docs/modules/catalog.md「规模化」）。
// 产品列表搜索/过滤/分页、归档守卫、跨产品概览。可见性判定复用不变量 16 原语。

import (
	"context"
	"time"

	"github.com/yuylong/Chalkline/internal/domain/product"
	"github.com/yuylong/Chalkline/internal/store"
)

// ProductListQuery 是产品列表检索条件（REST / MCP 共用）。
type ProductListQuery struct {
	Q      string // 名称 / key 模糊匹配
	Status string // active | archived；空 = 全部
	Limit  int
	Offset int
}

// ProductListResult 是分页产品列表。
type ProductListResult struct {
	Items []ProductView `json:"items"`
	Total int           `json:"total"`
}

// ListProducts 分页列可见产品（M3：搜索 + 状态过滤 + 计数；mcp.md 列表分页约定）。
func (s *Service) ListProducts(ctx context.Context, actor Actor, in ProductListQuery) (ProductListResult, error) {
	if in.Status != "" && product.Status(in.Status) != product.StatusActive &&
		product.Status(in.Status) != product.StatusArchived {
		return ProductListResult{}, Invalid("status %q 非法，应为 active / archived", in.Status)
	}
	q := s.DB.Q()
	rows, err := store.ListVisibleProductsPaged(ctx, q, actor.UserID, actor.IsAdmin, store.ProductListFilter{
		Q: in.Q, Status: in.Status, Limit: in.Limit, Offset: in.Offset,
	})
	if err != nil {
		return ProductListResult{}, err
	}
	total, err := store.CountVisibleProducts(ctx, q, actor.UserID, actor.IsAdmin, store.ProductListFilter{
		Q: in.Q, Status: in.Status,
	})
	if err != nil {
		return ProductListResult{}, err
	}
	items := make([]ProductView, 0, len(rows))
	for _, p := range rows {
		items = append(items, viewProduct(p))
	}
	return ProductListResult{Items: items, Total: total}, nil
}

// ListVisibleProductIDs 列 actor 可见产品 id（SSE 过滤与概览统计用；admin 全量）。
func (s *Service) ListVisibleProductIDs(ctx context.Context, actor Actor) ([]string, error) {
	q := s.DB.Q()
	if actor.IsAdmin {
		products, err := store.ListVisibleProducts(ctx, q, actor.UserID, true)
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(products))
		for _, p := range products {
			ids = append(ids, p.ID)
		}
		return ids, nil
	}
	return store.ProductIDsOfMember(ctx, q, actor.UserID)
}

// OverviewView 是跨产品概览（控制台首页规模视图）。
type OverviewView struct {
	Products         int `json:"products"`
	ActiveIterations int `json:"activeIterations"`
	OpenTasks        int `json:"openTasks"`
	PendingGates     int `json:"pendingGates"`
	OverdueGates     int `json:"overdueGates"`
}

// GetOverview 跨产品概览：按 actor 可见产品集合统计（admin 全量）。
func (s *Service) GetOverview(ctx context.Context, actor Actor) (OverviewView, error) {
	ids, err := s.ListVisibleProductIDs(ctx, actor)
	if err != nil {
		return OverviewView{}, err
	}
	counts, err := store.CountOverview(ctx, s.DB.Q(), ids, s.Now().Add(-s.reviewSLA()))
	if err != nil {
		return OverviewView{}, err
	}
	return OverviewView{
		Products:         len(ids),
		ActiveIterations: int(counts.ActiveIterations),
		OpenTasks:        int(counts.OpenTasks),
		PendingGates:     int(counts.PendingGates),
		OverdueGates:     int(counts.OverdueGates),
	}, nil
}

// reviewSLA 评审超期阈值（未配置时默认 24h）。
func (s *Service) reviewSLA() time.Duration {
	if s.ReviewSLA > 0 {
		return s.ReviewSLA
	}
	return 24 * time.Hour
}
