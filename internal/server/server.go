// Package server 是 chalklined 的组装引导：platform、store、app、api、mcp 拼装成服务进程。
// 它是唯一同时 import api 与 app 的包（docs/structure.md 分层边界的顶层组装点）。
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/yuylong/Chalkline/internal/api"
	"github.com/yuylong/Chalkline/internal/app"
	"github.com/yuylong/Chalkline/internal/auth"
	"github.com/yuylong/Chalkline/internal/domain/identity"
	"github.com/yuylong/Chalkline/internal/mcp"
	"github.com/yuylong/Chalkline/internal/platform"
	"github.com/yuylong/Chalkline/internal/store"
)

// Version 由构建注入（Makefile: -ldflags -X）。dev 构建为 dev。
var Version = "dev"

// dbState 安全持有数据库句柄（/readyz 与后台初始化协程并发读写）。
type dbState struct {
	mu    sync.RWMutex
	db    *store.DB
	ready bool
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

// Run 引导 chalklined：配置 → HTTP → 后台连接数据库（迁移+种子+admin 引导）→ 优雅停机。
// 数据库未就绪时 /healthz 仍 200、/readyz 503，业务端点统一 503（compose 健康检查依赖）。
func Run(ctx context.Context) error {
	cfg, err := platform.Load()
	if err != nil {
		return err
	}
	logger := platform.NewLogger(cfg.LogLevel)

	jwt := auth.NewJWTManager(cfg.JWTSecret)
	if jwt.EphemeralKey() {
		logger.Warn("CHALKLINE_JWT_SECRET 未设置，使用进程内临时密钥：重启后登录会话全部失效（生产必须配置）")
	}

	state := &dbState{}
	initDone := make(chan error, 1)

	var svcMu sync.RWMutex
	var svc app.API
	getSvc := func() app.API {
		svcMu.RLock()
		defer svcMu.RUnlock()
		return svc
	}

	// 数据库初始化：连接 → 迁移 → 种子 → admin 引导 → 组装 Service。
	go func() {
		for {
			db, derr := store.New(ctx, cfg.DatabaseURL, logger)
			if derr == nil {
				if derr = db.Migrate(ctx); derr == nil {
					if derr = db.SeedBuiltinWorkflows(ctx); derr == nil {
						s := app.NewService(db, logger, jwt)
						created, berr := s.BootstrapAdmin(ctx, cfg.AdminUsername, cfg.AdminPassword)
						if berr != nil {
							derr = berr
						} else {
							state.set(db)
							svcMu.Lock()
							svc = s
							svcMu.Unlock()
							if created {
								logger.Info("首启 admin 已引导", "username", cfg.AdminUsername)
							}
						}
					}
				}
				if derr != nil {
					db.Close()
				}
			}
			if derr == nil {
				logger.Info("数据库就绪：迁移、种子与引导完成")
				initDone <- nil
				return
			}
			logger.Error("数据库初始化失败，稍后重试", "error", derr, "retry_interval", cfg.DBRetryInterval.String())
			select {
			case <-ctx.Done():
				initDone <- ctx.Err()
				return
			case <-time.After(cfg.DBRetryInterval):
			}
		}
	}()

	authmw := &auth.Middleware{JWT: jwt, Users: lazyIdentityProvider{state: state}}
	mcpHandler := mcp.NewHandler(mcp.Deps{Logger: logger, Svc: getSvc})

	srv := &http.Server{
		Addr:          cfg.Addr,
		Handler:       newRouter(logger, state, getSvc, authmw, mcpHandler),
		ReadTimeout:   cfg.ReadTimeout,
		WriteTimeout:  cfg.WriteTimeout,
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

// lazyIdentityProvider 数据库就绪后提供身份查询（鉴权中间件的依赖）。
type lazyIdentityProvider struct{ state *dbState }

func (p lazyIdentityProvider) UserByID(ctx context.Context, id string) (identity.User, error) {
	db, ok := p.state.snapshot()
	if !ok {
		return identity.User{}, auth.ErrUnauthorized
	}
	return store.UserByID(ctx, db.Q(), id)
}

func (p lazyIdentityProvider) ResolvePAT(ctx context.Context, tokenHash string) (auth.PATInfo, error) {
	db, ok := p.state.snapshot()
	if !ok {
		return auth.PATInfo{}, auth.ErrUnauthorized
	}
	row, err := store.ResolvePAT(ctx, db.Q(), tokenHash)
	if err != nil {
		return auth.PATInfo{}, err
	}
	return auth.PATInfo{UserID: row.UserID, Expires: row.ExpiresAt, Revoked: row.RevokedAt != nil}, nil
}

func (p lazyIdentityProvider) TouchPAT(ctx context.Context, tokenHash string) error {
	db, ok := p.state.snapshot()
	if !ok {
		return nil // 未就绪时尽力而为
	}
	return store.TouchPAT(ctx, db.Q(), tokenHash)
}

func newRouter(logger *slog.Logger, state *dbState, getSvc func() app.API,
	authmw *auth.Middleware, mcpHandler http.Handler) http.Handler {
	return api.NewRouter(api.Deps{
		Logger:   logger,
		Version:  Version,
		Svc:      getSvc,
		AuthREST: authmw.AuthenticateREST,
		AuthMCP:  authmw.AuthenticateMCP,
		MCP:      mcpHandler,
		Ready: func() bool {
			_, ok := state.snapshot()
			return ok && getSvc() != nil
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
