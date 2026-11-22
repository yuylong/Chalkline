// Package app 是用例编排层：事务边界、跨聚合操作、权限判定（docs/structure.md）。
// 本文件定义对外错误类型：api/mcp 只做错误码转换，不重复判定。
package app

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/yuylong/Chalkline/internal/domain/identity"
	"github.com/yuylong/Chalkline/internal/domain/planning"
	"github.com/yuylong/Chalkline/internal/domain/product"
	"github.com/yuylong/Chalkline/internal/store"
	"github.com/yuylong/Chalkline/internal/workflow"
)

// Error 是对外错误的统一形态，携带 mcp.md 错误码与 HTTP 状态。
type Error struct {
	Status  int
	Code    string
	Message string
	Details map[string]any
}

func (e *Error) Error() string { return e.Message }

// Details 兜底空 map。
func (e *Error) GetDetails() map[string]any {
	if e.Details == nil {
		return map[string]any{}
	}
	return e.Details
}

// 常用构造器。
func NotFound(format string, args ...any) *Error {
	return &Error{Status: http.StatusNotFound, Code: "NOT_FOUND", Message: fmt.Sprintf(format, args...)}
}
func RoleRequired(format string, args ...any) *Error {
	return &Error{Status: http.StatusForbidden, Code: "ROLE_REQUIRED", Message: fmt.Sprintf(format, args...)}
}
func Unauthorized(format string, args ...any) *Error {
	return &Error{Status: http.StatusUnauthorized, Code: "UNAUTHORIZED", Message: fmt.Sprintf(format, args...)}
}
func Invalid(format string, args ...any) *Error {
	return &Error{Status: http.StatusUnprocessableEntity, Code: "INVALID_PARAMETER", Message: fmt.Sprintf(format, args...)}
}
func StageNotAllowedTask(node, stage string) *Error {
	return &Error{Status: http.StatusConflict, Code: "STAGE_NOT_ALLOW_TASK",
		Message: fmt.Sprintf("当前阶段 %s（节点 %s）不允许创建任务，模板未声明 allows_task_creation", stage, node)}
}
func TreeDepthExceeded(what string, depth, max int) *Error {
	return &Error{Status: http.StatusUnprocessableEntity, Code: "TREE_DEPTH_EXCEEDED",
		Message: fmt.Sprintf("%s深度 %d 超过上限 %d", what, depth, max)}
}
func InvalidArtifactRef(err error) *Error {
	return &Error{Status: http.StatusUnprocessableEntity, Code: "INVALID_ARTIFACT_REF", Message: err.Error()}
}
func Internal(err error) *Error {
	return &Error{Status: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "服务内部错误"}
}
func stateConflict() *Error {
	return &Error{Status: http.StatusConflict, Code: "STATE_CONFLICT", Message: workflow.ErrStateConflict.Error()}
}

// AsError 解包 *Error；非本层错误统一 INTERNAL_ERROR（不泄露内部细节）。
func AsError(err error) *Error {
	var aerr *Error
	if errors.As(err, &aerr) {
		return aerr
	}
	return Internal(err)
}

// wrapNotFound 把存储 ErrNotFound 翻译为 404（上下文由调用方补齐）。
func wrapNotFound(err error, notFoundMsg string) error {
	if errors.Is(err, store.ErrNotFound) {
		return NotFound("%s", notFoundMsg)
	}
	return err
}

// MapError 把领域/引擎错误翻译为对外错误（mcp.md 错误码表）。
func MapError(err error) *Error {
	var aerr *Error
	if errors.As(err, &aerr) {
		return aerr
	}
	switch {
	case errors.Is(err, workflow.ErrNotReady):
		return &Error{Status: http.StatusConflict, Code: "NOT_READY", Message: err.Error()}
	case errors.Is(err, workflow.ErrGateBlocked):
		return &Error{Status: http.StatusConflict, Code: "GATE_BLOCKED", Message: err.Error()}
	case errors.Is(err, workflow.ErrInvalidTransition):
		return &Error{Status: http.StatusConflict, Code: "INVALID_TRANSITION", Message: err.Error()}
	case errors.Is(err, workflow.ErrStateConflict):
		return stateConflict()
	case errors.Is(err, workflow.ErrArtifactMissing):
		return &Error{Status: http.StatusUnprocessableEntity, Code: "ARTIFACT_MISSING", Message: err.Error()}
	case errors.Is(err, workflow.ErrNodeNotFound):
		return NotFound("%s", err.Error())
	case errors.Is(err, workflow.ErrInvalidDefinition):
		return Invalid("%s", err.Error())
	case errors.Is(err, identity.ErrValidation),
		errors.Is(err, product.ErrValidation),
		errors.Is(err, planning.ErrValidation):
		return Invalid("%s", err.Error())
	default:
		return Internal(err)
	}
}
