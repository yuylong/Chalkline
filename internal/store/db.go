// Package store 是数据访问层：pgx 连接池、goose 迁移、内置模板种子。
// 查询一律 sqlc 生成（M1 起随首个查询引入）；本包 M0 只提供基础设施。
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/pressly/goose/v3"
	"github.com/yuylong/Chalkline/internal/workflow"
	"github.com/yuylong/Chalkline/migrations"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql 驱动（goose 迁移用）
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB 持有连接池；M1 起在其上叠加 sqlc 生成的 Queries。
type DB struct {
	Pool   *pgxpool.Pool
	logger *slog.Logger
}

// New 建立连接池并验证连通。
func New(ctx context.Context, url string, logger *slog.Logger) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("store: 解析连接串失败: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("store: 建立连接池失败: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: 数据库连通失败: %w", err)
	}
	return &DB{Pool: pool, logger: logger}, nil
}

// Close 关闭连接池。
func (db *DB) Close() { db.Pool.Close() }

// Ping 验证数据库当前可用（/readyz 用）。
func (db *DB) Ping(ctx context.Context) error {
	return db.Pool.Ping(ctx)
}

// Migrate 应用全部内嵌迁移（goose，postgres 方言）。幂等：已应用的脚本自动跳过。
func (db *DB) Migrate(ctx context.Context) error {
	sqlDB, err := sql.Open("pgx", db.Pool.Config().ConnString())
	if err != nil {
		return fmt.Errorf("store: 打开迁移连接失败: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("store: 设置迁移方言失败: %w", err)
	}
	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		return fmt.Errorf("store: 迁移失败: %w", err)
	}
	return nil
}

// SeedBuiltinWorkflows 首启种子：内置模板写入 workflows 表，已存在则跳过
//（内置模板入库后可编辑，种子不覆盖用户的修改）。
func (db *DB) SeedBuiltinWorkflows(ctx context.Context) error {
	builtins, err := workflow.LoadBuiltins()
	if err != nil {
		return fmt.Errorf("store: 内置模板加载失败: %w", err)
	}
	const insert = `
INSERT INTO workflows (id, scope, name, version, status, definition)
VALUES ($1, $2, $3, $4, 'active', $5)
ON CONFLICT (id) DO NOTHING`
	for _, def := range builtins {
		defJSON, err := json.Marshal(def)
		if err != nil {
			return fmt.Errorf("store: 模板 %s 序列化失败: %w", def.ID, err)
		}
		_, err = db.Pool.Exec(ctx, insert, def.ID, def.Scope, def.Name, def.Version, defJSON)
		if err != nil {
			return fmt.Errorf("store: 模板 %s 种子写入失败: %w", def.ID, err)
		}
	}
	db.logger.Info("内置工作流模板已就绪", "count", len(builtins))
	return nil
}
