package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/yuylong/Chalkline/internal/workflow"
)

// 统一错误包络（docs/domain/mcp.md）：
//
//	{"error": {"code": "NOT_FOUND", "message": "任务不存在", "details": {...}}}
//
// HTTP 层只负责映射与序列化；判定全部在服务端完成，客户端不承担业务规则。
type apiError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type errorEnvelope struct {
	Error apiError `json:"error"`
}

// WriteError 按状态码与业务错误码输出统一包络。
func WriteError(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorEnvelope{Error: apiError{Code: code, Message: message, Details: details}})
}

// httpStatusOf 把引擎错误映射为 HTTP 状态与错误码（mcp.md 错误码表）。
func httpStatusOf(err error) (status int, code string) {
	switch {
	case errors.Is(err, workflow.ErrNotReady):
		return http.StatusConflict, "NOT_READY"
	case errors.Is(err, workflow.ErrGateBlocked):
		return http.StatusConflict, "GATE_BLOCKED"
	case errors.Is(err, workflow.ErrInvalidTransition):
		return http.StatusConflict, "INVALID_TRANSITION"
	case errors.Is(err, workflow.ErrStateConflict):
		return http.StatusConflict, "STATE_CONFLICT"
	case errors.Is(err, workflow.ErrArtifactMissing):
		return http.StatusUnprocessableEntity, "ARTIFACT_MISSING"
	case errors.Is(err, workflow.ErrNodeNotFound):
		return http.StatusNotFound, "NOT_FOUND"
	default:
		return http.StatusInternalServerError, "INTERNAL_ERROR"
	}
}

// WriteDomainError 输出领域/引擎错误的统一包络。
func WriteDomainError(w http.ResponseWriter, err error) {
	status, code := httpStatusOf(err)
	WriteError(w, status, code, err.Error(), nil)
}
