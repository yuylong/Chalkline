package platform

import (
	"log/slog"
	"os"
)

// NewLogger 返回 JSON 结构化日志器（docs/tech.md：log/slog，含 request_id / actor_id）。
func NewLogger(level string) *slog.Logger {
	var lv slog.Level
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lv})
	return slog.New(h)
}
