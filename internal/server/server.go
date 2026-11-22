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
	"github.com/yuylong/Chalkline/internal/events"
	"github.com/yuylong/Chalkline/internal/integration"
	"github.com/yuylong/Chalkline/internal/integration/notify"
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

	// 集成装配（M2）：出站白名单客户端 + provider 注册表（git/svn/outline/redmine）。
	// 白名单为空时拒绝一切出站：Snapshot 全部降级，Validate 只做本地校验。
	safeClient := integration.NewSafeClient(cfg.OutboundAllowlist)
	artifactGate := integration.NewRegistry(
		integration.NewGitProvider(safeClient),
		integration.NewSVNProvider(safeClient),
		integration.NewOutlineProvider(safeClient, cfg.OutlineBaseURL, cfg.OutlineToken),
		integration.NewRedmineProvider(safeClient, cfg.RedmineBaseURL, cfg.RedmineToken),
	)

	var svcMu sync.RWMutex
	var svc app.API
	getSvc := func() app.API {
		svcMu.RLock()
		defer svcMu.RUnlock()
		return svc
	}

	// M3 实时推送：进程内事件总线（outbox tailer 发布，SSE 连接订阅）。
	eventHub := events.NewHub()

	// 数据库初始化：连接 → 迁移 → 种子 → admin 引导 → 组装 Service。
	go func() {
		for {
			db, derr := store.New(ctx, cfg.DatabaseURL, logger)
			if derr == nil {
				if derr = db.Migrate(ctx); derr == nil {
					if derr = db.SeedBuiltinWorkflows(ctx); derr == nil {
						s := app.NewService(db, logger, jwt, artifactGate, app.Options{
							ReviewSLA:        cfg.ReviewSLA,
							ReviewEscalation: cfg.ReviewEscalation,
						})
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
				startDispatcher(ctx, db, cfg, logger)
				// M3：评审 SLA 监控器（超期提醒与升级链，只写水位与事件，不改状态）。
				if impl, ok := svc.(*app.Service); ok {
					go impl.RunReviewSLA(ctx, cfg.ReviewScanInterval, logger)
				}
				// M3：outbox 事件尾随 → SSE 总线（控制台实时刷新）。
				go tailOutbox(ctx, db, eventHub, cfg.EventsInterval, logger)
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
		Handler:       newRouter(logger, state, getSvc, authmw, mcpHandler, eventHub),
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
	authmw *auth.Middleware, mcpHandler http.Handler, eventHub *events.Hub) http.Handler {
	return api.NewRouter(api.Deps{
		Logger:   logger,
		Version:  Version,
		Svc:      getSvc,
		AuthREST: authmw.AuthenticateREST,
		AuthMCP:  authmw.AuthenticateMCP,
		MCP:      mcpHandler,
		Events:   eventHub,
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

// startDispatcher 启动 outbox 通知投递器（M2 集成 + M3 订阅路由）。无论是否配置通道都启动：
// 无通道且无订阅时事件直接标记完成，避免 outbox 无限滞留 pending（不变量 15）。
func startDispatcher(ctx context.Context, db *store.DB, cfg platform.Config, logger *slog.Logger) {
	senders := []notify.Sender{}
	if cfg.WebhookURL != "" {
		senders = append(senders, &notify.Webhook{URL: cfg.WebhookURL, Secret: cfg.WebhookSecret})
		logger.Info("webhook 通知已启用")
	}
	if cfg.SMTPHost != "" && cfg.SMTPFrom != "" && len(cfg.NotifyEmails) > 0 {
		senders = append(senders, &notify.SMTP{
			Host: cfg.SMTPHost, Port: cfg.SMTPPort,
			Username: cfg.SMTPUsername, Password: cfg.SMTPPassword,
			From: cfg.SMTPFrom, To: cfg.NotifyEmails,
		})
		logger.Info("SMTP 邮件通知已启用", "host", cfg.SMTPHost, "recipients", len(cfg.NotifyEmails))
	}
	go (&notify.Dispatcher{
		Queue: db, Senders: senders, Log: logger, Interval: cfg.OutboxInterval,
		// M3：Webhook 订阅管理——事件按订阅的 event_types 路由到各自 URL，
		// 签名密钥沿用全局 CHALKLINE_WEBHOOK_SECRET（凭据不落库）。
		Subs:          db,
		WebhookSecret: cfg.WebhookSecret,
	}).Run(ctx)
}

// tailOutbox 尾随 outbox 新事件并发布到 SSE 总线（M3 实时推送）。
// 启动时定位到当前最大 id（只推增量）；产品归属在发布前解析，可见性过滤在 api 层。
func tailOutbox(ctx context.Context, db *store.DB, hub *events.Hub, interval time.Duration, logger *slog.Logger) {
	if interval <= 0 {
		interval = 3 * time.Second
	}
	lastID, err := store.MaxOutboxID(ctx, db.Q())
	if err != nil {
		logger.Error("SSE 尾随启动失败：无法定位事件游标", "error", err)
		return
	}
	logger.Info("SSE 事件尾随启动", "interval", interval.String(), "cursor", lastID)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		rows, err := store.ListOutboxAfter(ctx, db.Q(), lastID, 100)
		if err != nil {
			logger.Error("SSE 尾随读取失败", "error", err)
			continue
		}
		for _, ev := range rows {
			productID, err := store.ProductIDOfSubject(ctx, db.Q(), ev.SubjectType, ev.SubjectID)
			if err != nil {
				productID = "" // 主体可能已被删除：仍推送，可见性过滤会兜底
			}
			hub.Publish(events.Event{
				ID: ev.ID, EventType: ev.EventType, SubjectType: ev.SubjectType,
				SubjectID: ev.SubjectID, ProductID: productID, Payload: ev.Payload,
			})
			lastID = ev.ID
		}
	}
}
