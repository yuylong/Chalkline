package store

// current.go：个人「当前迭代」偏好（docs/modules/planning.md「当前迭代」）。
// 每人至多一条记录；可见性判定在 app 层，本层只负责读写。

import (
	"context"
	"errors"
)

// CurrentIterationID 取用户当前迭代 id；未设置返回空串。
func CurrentIterationID(ctx context.Context, q Querier, userID string) (string, error) {
	var id string
	err := q.QueryRow(ctx,
		`SELECT iteration_id FROM user_current_iterations WHERE user_id = $1`, userID).Scan(&id)
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return id, nil
}

// UpsertCurrentIteration 设置当前迭代（每人一条，冲突即覆盖）。
func UpsertCurrentIteration(ctx context.Context, q Querier, userID, iterationID string) error {
	_, err := q.Exec(ctx,
		`INSERT INTO user_current_iterations (user_id, iteration_id)
		 VALUES ($1, $2)
		 ON CONFLICT (user_id) DO UPDATE
		   SET iteration_id = EXCLUDED.iteration_id, updated_at = now()`,
		userID, iterationID)
	return err
}

// ClearCurrentIteration 取消当前迭代（无记录时为幂等成功）。
func ClearCurrentIteration(ctx context.Context, q Querier, userID string) error {
	_, err := q.Exec(ctx, `DELETE FROM user_current_iterations WHERE user_id = $1`, userID)
	return err
}
