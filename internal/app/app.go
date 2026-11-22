// Package app 是组装层：把 platform、store、api 拼成 chalklined 服务。
// 会话、权限、用例编排从 M1 起在此扩展（docs/structure.md）。
package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/yuylong/Chalkline/internal/api"
	"github.com/yuylong/Chalkline/internal/platform"
	"github.com/yuylong/Chalkline/internal/store"
)

// Version 由构建注入（Makefile: -ldflags -X）。dev 构建为 dev。
var Version = "dev"

// dbState 安全持有数据库句柄（/readyz 与后台初始化协程并发读写）。
type dbState struct {
	mu     sync.RWMutex
	db     *store.DB
	ready  bool
}

func (s *dbState) snapshot() (*store.DB, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db, s.ready
}

func (s *dbState) set(db *store.DB) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db = db
	s.ready = true
}

// Run 引导 chalklined：加载配置 → 起 HTTP → 后台连接数据库并执行迁移与种子 →
// 监听退出信号优雅停机。数据库未就绪时 /healthz 仍 200、/readyz 503，
// 便于 compose 健康检查与问题定位。
func Run(ctx context.Context) error {
	cfg, err := platform.Load()
	if err != nil {
		return err
	}
	logger := platform.NewLogger(cfg.LogLevel)

	state := &dbState{}
	initDone := make(chan error, 1)

	// 数据库初始化：连接 → 迁移 → 种子，失败按间隔重试直到成功或进程退出。
	go func() {
		for {
			db, err := store.New(ctx, cfg.DatabaseURL, logger)
			if err == nil {
				if err = db.Migrate(ctx); err == nil {
					err = db.SeedBuiltinWorkflows(ctx)
				}
				if err != nil {
					db.Close()
				} else {
					state.set(db)
				}
			}
			if err == nil {
				logger.Info("数据库就绪：迁移与种子完成")
				initDone <- nil
				return
			}
			logger.Error("数据库初始化失败，稍后重试", "error", err, "retry_interval", cfg.DBRetryInterval.String())
			select {
			case <-ctx.Done():
				initDone <- ctx.Err()
				return
			case <-time.After(cfg.DBRetryInterval):
			}
		}
	}()

	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      newRouter(logger, state),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("chalkline api 启动", "addr", cfg.Addr, "version", Version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		closeDB(state)
		return err
	case <-ctx.Done():
	}

	// 优雅停机：先停 HTTP 再关数据库。
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP 停机异常", "error", err)
	}
	closeDB(state)
	<-initDone // 等初始化协程退出
	return nil
}

func newRouter(logger *slog.Logger, state *dbState) http.Handler {
	return api.NewRouter(api.Deps{
		Logger:  logger,
		Version: Version,
		Ready: func() bool {
			_, ok := state.snapshot()
			return ok
		},
		Ping: func(r *http.Request) error {
			db, ok := state.snapshot()
			if !ok {
				return errors.New("数据库未就绪")
			}
			return db.Ping(r.Context())
		},
	})
}

func closeDB(state *dbState) {
	if db, ok := state.snapshot(); ok {
		db.Close()
	}
}
