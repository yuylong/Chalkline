// Package api 是 HTTP 入口：路由、中间件、健康检查与统一错误包络。
// 只做协议转换——没有任何业务规则；会话、项目对象、权限判定都在 app 层（docs/structure.md）。
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Deps 是路由所需依赖；M1 起由 app 层注入会话与各模块用例。
type Deps struct {
	Logger  *slog.Logger
	Version string
	// Ready 报告数据库是否已连接并完成迁移与种子。
	Ready func() bool
	// Ping 探活数据库连接（Ready 为 true 时有效）。
	Ping func(r *http.Request) error
}

// NewRouter 组装全部 HTTP 路由。
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(RequestID)
	r.Use(RequestLogger(d.Logger))
	r.Use(Recoverer(d.Logger))

	r.Get("/healthz", healthz) // 存活：进程在即 200
	r.Get("/readyz", readyz(d)) // 就绪：数据库连通 + 迁移完成
	r.Route("/api/v1", func(api chi.Router) {
		api.Get("/info", info(d))
	})

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
			"name":     "chalkline",
			"version":  d.Version,
			"api":      "v1",
			"milestone": "M0",
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
