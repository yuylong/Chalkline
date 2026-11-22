// Package api 是 HTTP 入口：路由、中间件、健康检查与统一错误包络。
// 只做协议转换——没有任何业务规则；会话、权限、用例编排都在 app 层（docs/structure.md）。
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/yuylong/Chalkline/internal/app"
	"github.com/yuylong/Chalkline/internal/auth"
	"github.com/yuylong/Chalkline/internal/domain/identity"
	"github.com/yuylong/Chalkline/internal/events"
)

// Deps 是路由所需依赖，由 server 组装层注入。
type Deps struct {
	Logger  *slog.Logger
	Version string
	// Svc 按请求取用例集合（数据库就绪后非 nil；未就绪由 readyGuard 拦截）。
	Svc func() app.API
	// AuthREST 鉴权中间件（JWT）。
	AuthREST func(http.Handler) http.Handler
	// AuthMCP MCP 端点鉴权中间件（PAT）。
	AuthMCP func(http.Handler) http.Handler
	// MCP 交给路由挂载（nil 则 /mcp 返回 501）。
	MCP http.Handler
	// Events 是进程内事件总线（M3 SSE 实时推送；nil 则 /api/v1/events 返回 501）。
	Events *events.Hub
	// Ready 报告数据库是否已连接并完成迁移与种子。
	Ready func() bool
	// Ping 探活数据库连接（Ready 为 true 时有效）。
	Ping func(r *http.Request) error
}

// NewRouter 组装全部 HTTP 路由（M1：auth / users / products / planning / workflows / mcp）。
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(RequestID)
	r.Use(RequestLogger(d.Logger))
	r.Use(Recoverer(d.Logger))

	r.Get("/healthz", healthz) // 存活：进程在即 200
	r.Get("/readyz", readyz(d)) // 就绪：数据库连通 + 迁移完成

	h := &handlers{deps: d}

	r.Route("/api/v1", func(apiRouter chi.Router) {
		apiRouter.Get("/info", info(d))

		// 公开端点（仍需数据库就绪）。
		apiRouter.Group(func(pub chi.Router) {
			pub.Use(readyGuard(d))
			pub.Post("/auth/login", h.login) // 用户名密码换 JWT
		})

		apiRouter.Group(func(g chi.Router) {
			g.Use(readyGuard(d))
			g.Use(d.AuthREST)

			// M3 实时推送：SSE 事件流（JWT 鉴权，长连接）。
			g.Get("/events", h.eventsStream)

			// identity：PAT 与本人信息
			g.Get("/auth/me", h.me)
			g.Post("/auth/pat", h.createPAT)
			g.Get("/auth/pats", h.listPATs)
			g.Delete("/auth/pats/{id}", h.revokePAT)

			// catalog：产品 / 成员 / 模块 / 文档
			g.Get("/products", h.listProducts)
			g.Post("/products", h.createProduct)
			g.Get("/products/{id}", h.getProduct)
			g.Patch("/products/{id}", h.updateProduct)
			g.Get("/overview", h.getOverview)
			g.Get("/products/{id}/audit-logs", h.auditLogs)
			g.Get("/products/{id}/members", h.listMembers)
			g.Post("/products/{id}/members", h.upsertMember)
			g.Delete("/products/{id}/members/{uid}", h.removeMember)
			g.Get("/products/{id}/modules", h.listModules)
			g.Post("/products/{id}/modules", h.createModule)
			g.Patch("/products/{id}/modules/{mid}", h.updateModule)
			g.Put("/documents/{ownerType}/{ownerID}", h.upsertDocument)
			g.Get("/documents/{ownerType}/{ownerID}", h.getDocument)

			// planning：迭代 / 任务
			g.Get("/products/{pid}/iterations", h.listIterations)
			g.Post("/products/{pid}/iterations", h.createIteration)
			g.Get("/iterations/{id}", h.getIteration)
			g.Post("/iterations/{id}/tasks", h.createTask)
			g.Get("/tasks", h.listTasks)
			g.Get("/tasks/{id}", h.getTask)
			g.Patch("/tasks/{id}", h.updateTask)
			g.Post("/tasks/{id}/children", h.decomposeTask)
			g.Post("/tasks/{id}/modules", h.linkModule)

			// workflow：模板 / 实例查询 / 待办
			g.Get("/workflows", h.listWorkflows)
			g.Post("/workflows", h.createWorkflow)
			g.Post("/workflows/import", h.importWorkflow)
			g.Get("/workflows/{id}", h.getWorkflow)
			g.Patch("/workflows/{id}", h.updateWorkflow)
			g.Post("/workflows/{id}/archive", h.archiveWorkflow)
			g.Get("/workflows/{id}/export", h.exportWorkflow)
			g.Get("/tasks/{id}/workflow", h.taskWorkflow)
			g.Get("/iterations/{id}/workflow", h.iterationWorkflow)
			g.Get("/reviews/pending", h.pendingReviews)

			// workflow：节点操作（与 MCP 工具同一套用例与错误码）
			g.Post("/tasks/{id}/steps/start", h.taskStartStep)
			g.Post("/tasks/{id}/steps/submit", h.taskSubmitStep)
			g.Post("/tasks/{id}/steps/review", h.taskReviewStep)
			g.Post("/tasks/{id}/artifacts", h.taskAttachArtifact)
			g.Get("/tasks/{id}/next-action", h.taskNextAction)
			g.Post("/iterations/{id}/steps/start", h.iterationStartStep)
			g.Post("/iterations/{id}/steps/submit", h.iterationSubmitStep)
			g.Post("/iterations/{id}/steps/review", h.iterationReviewStep)
			g.Get("/iterations/{id}/next-action", h.iterationNextAction)

			// identity：用户管理（admin）
			g.Group(func(admin chi.Router) {
				admin.Use(auth.RequireRole(identity.RoleAdmin))
				admin.Get("/users", h.listUsers)
				admin.Post("/users", h.createUser)
				admin.Get("/users/{id}", h.getUser)
				admin.Patch("/users/{id}", h.updateUser)
				admin.Post("/users/{id}/password", h.setUserPassword)

				// M3：审计全局检索与 Webhook 订阅管理（admin）
				admin.Get("/audit-logs", h.auditLogs)
				admin.Get("/webhooks", h.listWebhooks)
				admin.Post("/webhooks", h.createWebhook)
				admin.Patch("/webhooks/{id}", h.updateWebhook)
				admin.Delete("/webhooks/{id}", h.deleteWebhook)
			})
		})
	})

	if d.MCP != nil {
		r.With(d.AuthMCP).Post("/mcp", d.MCP.ServeHTTP)
	} else {
		r.Post("/mcp", func(w http.ResponseWriter, _ *http.Request) {
			WriteError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "MCP 未启用", nil)
		})
	}

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotFound, "NOT_FOUND", "路径不存在", nil)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusMethodNotAllowed, "INVALID_METHOD", "方法不支持", nil)
	})
	return r
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func readyz(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !d.Ready() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		if err := d.Ping(r); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}

func info(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"name":      "chalkline",
			"version":   d.Version,
			"api":       "v1",
			"milestone": "M3",
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
