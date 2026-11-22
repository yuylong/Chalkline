package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrNotFound 行不存在（app 层映射为 404 NOT_FOUND）。
var ErrNotFound = pgx.ErrNoRows

// rowScanner 是 pgx.Row / pgx.Rows 共有的行扫描接口，
// 让各 scan 函数可同时接受 QueryRow 与 Query 的返回值。
type rowScanner interface {
	Scan(dest ...any) error
}

// Querier 抽象 pool 与事务共有的执行接口；
// app 层用例在事务内把 Querier 传给各查询函数（docs/tech.md 约束 5）。
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// q 返回连接池作为默认 Querier。
func (db *DB) q() Querier { return db.Pool }

// WithTx 在事务内执行 fn；返回错误即回滚，成功提交。
func (db *DB) WithTx(ctx context.Context, fn func(q Querier) error) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: 开启事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // 提交后回滚是 no-op
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// IsDuplicate 报告是否唯一约束冲突（23505）。
func IsDuplicate(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// scanAll 逐行扫描并组切片。
func scanAll[T any](ctx context.Context, q Querier, sql string,
	scan func(rowScanner) (T, error), args ...any) ([]T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("store: 查询失败: %w", err)
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("store: 扫描行失败: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
