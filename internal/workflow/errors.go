package workflow

import "errors"

// 引擎错误与 docs/domain/mcp.md 错误码一一对应；
// HTTP 层（internal/api/apierr）负责映射为 {error:{code,message,details}}。
var (
	// ErrNotReady 前驱节点未完成时调 start_step（NOT_READY / 409）。
	ErrNotReady = errors.New("前驱节点未全部通过，节点不可开始")
	// ErrGateBlocked 门禁未满足时调 submit_step（GATE_BLOCKED / 409）。
	ErrGateBlocked = errors.New("节点门禁未满足")
	// ErrInvalidTransition 节点状态不允许该操作（INVALID_TRANSITION / 409）。
	ErrInvalidTransition = errors.New("节点当前状态不允许该操作")
	// ErrStateConflict 节点状态已被并发修改，乐观锁冲突（STATE_CONFLICT / 409）。
	ErrStateConflict = errors.New("节点状态已被并发修改，请刷新后重试")
	// ErrArtifactMissing 提交时缺必需材料（ARTIFACT_MISSING / 422）。
	ErrArtifactMissing = errors.New("提交缺少门禁必需的材料")
	// ErrNodeNotFound 节点不存在于模板（NOT_FOUND / 404）。
	ErrNodeNotFound = errors.New("节点不存在")
)
