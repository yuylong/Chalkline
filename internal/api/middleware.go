package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

// contextKey 是中间件注入值的键类型，避免跨包字符串键冲突。
type contextKey string

const requestIDKey contextKey = "request_id"

// RequestID 为每个请求生成 request_id（日志贯穿与问题排查，docs/tech.md）。
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestLogger 输出结构化访问日志（method、path、status、耗时、request_id）。
func RequestLogger(logger *slog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			reqID, _ := r.Context().Value(requestIDKey).(string)
			logger.Info("http_request",
				"request_id", reqID,
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"duration_ms", float64(time.Since(start).Microseconds())/1000.0,
			)
		})
	}
}

// Recoverer 兜住 panic：记日志并返回 500，避免进程退出。
func Recoverer(logger *slog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic_recovered", "recover", rec, "path", r.URL.Path)
					WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "服务内部错误", nil)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
