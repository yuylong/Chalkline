package app

// current.go：个人「当前迭代」偏好（docs/modules/planning.md「当前迭代」）。
// 控制台登录后的首页直接展示当前迭代看板；用户可在迭代详情页把它设为当前迭代。
// 可见性复用不变量 16 原语：只能选自己可见产品的迭代，取消无需校验。

import (
	"context"

	"github.com/yuylong/Chalkline/internal/store"
)

// CurrentIterationView 是当前迭代视图；未设置时 Iteration 为 null。
type CurrentIterationView struct {
	Iteration *IterationDetail `json:"iteration"`
}

// GetCurrentIteration 返回 actor 的当前迭代；已不可见（成员被移除）时按未设置处理。
func (s *Service) GetCurrentIteration(ctx context.Context, actor Actor) (CurrentIterationView, error) {
	q := s.DB.Q()
	id, err := store.CurrentIterationID(ctx, q, actor.UserID)
	if err != nil {
		return CurrentIterationView{}, err
	}
	if id == "" {
		return CurrentIterationView{}, nil
	}
	detail, err := s.GetIteration(ctx, actor, id)
	if err != nil {
		// 迭代已被清理或 actor 已失去可见性：视为未设置，不向控制台报错。
		if aerr := AsError(err); aerr.Status == 404 {
			return CurrentIterationView{}, nil
		}
		return CurrentIterationView{}, err
	}
	return CurrentIterationView{Iteration: &detail}, nil
}

// SetCurrentIteration 设置当前迭代：须对该迭代所属产品可见（非成员 404）。
// 已收尾/已取消的迭代不可设为当前迭代（避免首页停在终态）。
func (s *Service) SetCurrentIteration(ctx context.Context, actor Actor, iterationID string) (IterationDetail, error) {
	err := s.DB.WithTx(ctx, func(q store.Querier) error {
		it, err := store.IterationByID(ctx, q, iterationID)
		if err != nil {
			return wrapNotFound(err, "迭代不存在或不可见")
		}
		if _, err := s.mustView(ctx, q, actor, it.ProductID); err != nil {
			return err
		}
		if it.Status.Terminal() {
			return Invalid("迭代 %s 已收尾，不能设为当前迭代", it.Number)
		}
		if err := store.UpsertCurrentIteration(ctx, q, actor.UserID, iterationID); err != nil {
			return err
		}
		return audit(ctx, q, actor, "iteration.set_current", "iteration", iterationID,
			map[string]any{"iterationId": iterationID})
	})
	if err != nil {
		return IterationDetail{}, err
	}
	return s.GetIteration(ctx, actor, iterationID)
}

// ClearCurrentIteration 取消当前迭代（幂等）。
func (s *Service) ClearCurrentIteration(ctx context.Context, actor Actor) error {
	return s.DB.WithTx(ctx, func(q store.Querier) error {
		if err := store.ClearCurrentIteration(ctx, q, actor.UserID); err != nil {
			return err
		}
		return audit(ctx, q, actor, "iteration.clear_current", "user", actor.UserID,
			map[string]any{"userId": actor.UserID})
	})
}
