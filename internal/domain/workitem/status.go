// Package workitem 是工作项状态与门禁的通用抽象，
// 被迭代（iteration 作用域）与任务（task 作用域）两层工作流共用。
// 对应 docs/domain/workflow.md 的节点状态机与门禁语义。
package workitem

// Scope 是工作流作用域：迭代承载生命周期，任务承载具体工作。
type Scope string

const (
	ScopeIteration Scope = "iteration"
	ScopeTask      Scope = "task"
)

func (s Scope) Valid() bool { return s == ScopeIteration || s == ScopeTask }

// NodeStatus 是节点状态机的取值（docs/domain/workflow.md）：
// pending → ready → in_progress → awaiting_review → passed / rejected。
type NodeStatus string

const (
	// StatusPending 前驱未全部通过。
	StatusPending NodeStatus = "pending"
	// StatusReady 前驱全部 passed，可开始。
	StatusReady NodeStatus = "ready"
	// StatusInProgress 已开始，尚未提交。
	StatusInProgress NodeStatus = "in_progress"
	// StatusAwaitingReview 已提交，等待门禁评审。
	StatusAwaitingReview NodeStatus = "awaiting_review"
	// StatusPassed 门禁满足（或无门禁直接提交通过），准出。
	StatusPassed NodeStatus = "passed"
	// StatusRejected 被评审驳回，回到待修改状态；重提不清零既有 approve。
	StatusRejected NodeStatus = "rejected"
)

// Actionable 报告该状态是否为「当前节点」：最近进入且未完成。
// 投影「当前节点 / 当前阶段」即全部 actionable 节点的集合（并行分支为集合）。
func (s NodeStatus) Actionable() bool {
	return s == StatusReady || s == StatusInProgress ||
		s == StatusAwaitingReview || s == StatusRejected
}

// CanSubmit 报告该状态下能否提交节点。
func (s NodeStatus) CanSubmit() bool {
	return s == StatusInProgress || s == StatusRejected
}

// Outcome 是终态节点对应的业务结论（多终态时必填，如 done / cancelled）。
type Outcome string

const (
	OutcomeDone      Outcome = "done"
	OutcomeCancelled Outcome = "cancelled"
)

func (o Outcome) Valid() bool { return o == OutcomeDone || o == OutcomeCancelled }
