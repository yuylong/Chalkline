package planning

import (
	"fmt"
	"time"
)

// TaskKind 是任务类型。调研、设计、测试、总结不是编码任务的附属品，
// 而是独立类型，各有自己的流程与必交材料（docs/domain/model.md）。
type TaskKind string

const (
	KindResearch TaskKind = "research"
	KindDesign   TaskKind = "design"
	KindDev      TaskKind = "dev"
	KindTest     TaskKind = "test"
	KindOps      TaskKind = "ops"
	KindSummary  TaskKind = "summary"
)

func (k TaskKind) Valid() bool {
	switch k {
	case KindResearch, KindDesign, KindDev, KindTest, KindOps, KindSummary:
		return true
	}
	return false
}

// Priority 是任务优先级。
type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityNormal Priority = "normal"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"
)

func (p Priority) Valid() bool {
	switch p {
	case PriorityLow, PriorityNormal, PriorityHigh, PriorityUrgent:
		return true
	}
	return false
}

// TaskStatus 是任务业务状态。它是工作流节点状态的投影，由工作流终态驱动，
// 任何入口不得直接 UPDATE（AGENT.md 红线 1）。
type TaskStatus string

const (
	TaskOpen      TaskStatus = "open"
	TaskDone      TaskStatus = "done"
	TaskCancelled TaskStatus = "cancelled"
)

// Terminal 报告任务是否已终结；不变量 9 / 10 的「终结」即 done / cancelled。
func (s TaskStatus) Terminal() bool { return s == TaskDone || s == TaskCancelled }

// Task 是工作项，挂在迭代下、关联模块，支持多级拆解，有唯一负责人。
type Task struct {
	ID          string
	ProductID   string
	IterationID string
	ParentID    string // 为空即顶层任务；父子关系不可变（不变量 3）
	Number      TaskNumber
	Title       string
	Description string
	Kind        TaskKind
	AssigneeID  string // 唯一负责人，可为空（待认领）
	Priority    Priority
	Status      TaskStatus
	WorkflowID  string // 子任务默认继承父任务模板，可按 kind 覆盖
	CreatedAt   time.Time
}

// NewTask 构造并校验任务。parent 非空时强制：同产品、同迭代、深度上限、编号层级匹配（不变量 2）。
func NewTask(id, productID, iterationID string, parent *Task, number TaskNumber, title string,
	kind TaskKind, priority Priority, workflowID string) (Task, error) {
	t := Task{ID: id, ProductID: productID, IterationID: iterationID, Number: number,
		Title: title, Kind: kind, Priority: priority, Status: TaskOpen, WorkflowID: workflowID}
	if parent != nil {
		t.ParentID = parent.ID
		if err := ensureDecomposable(parent, &t); err != nil {
			return Task{}, err
		}
	}
	if err := t.Validate(); err != nil {
		return Task{}, err
	}
	return t, nil
}

// ensureDecomposable 强制任务拆解规则：子任务必须与父任务同产品、同迭代，
// 深度 ≤ 3，且编号必须是父编号向下延伸一级。
func ensureDecomposable(parent *Task, child *Task) error {
	if parent.ProductID != child.ProductID {
		return fmt.Errorf("%w: 子任务与父任务必须同产品（不变量 2）", ErrValidation)
	}
	if parent.IterationID != child.IterationID {
		return fmt.Errorf("%w: 子任务与父任务必须同迭代，跨迭代需新建一级任务", ErrValidation)
	}
	parentDepth, err := parent.Number.Depth()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrValidation, err)
	}
	if parentDepth >= MaxTaskDepth {
		return fmt.Errorf("%w: 父任务 %s 已在第 %d 层，不可再拆（上限 %d）",
			ErrValidation, parent.Number, parentDepth, MaxTaskDepth)
	}
	if err := childNumberExtendsParent(parent.Number, child.Number); err != nil {
		return fmt.Errorf("%w: %w", ErrValidation, err)
	}
	return nil
}

// childNumberExtendsParent 校验子编号是父编号追加一级序号。
func childNumberExtendsParent(parent, child TaskNumber) error {
	pk, pseqs, err := parent.Parse()
	if err != nil {
		return err
	}
	ck, cseqs, err := child.Parse()
	if err != nil {
		return err
	}
	if pk != ck {
		return fmt.Errorf("子编号 %s 与父编号 %s 产品 key 不一致", child, parent)
	}
	if len(cseqs) != len(pseqs)+1 {
		return fmt.Errorf("子编号 %s 应为父编号 %s 追加一级", child, parent)
	}
	for i := range pseqs {
		if pseqs[i] != cseqs[i] {
			return fmt.Errorf("子编号 %s 与父编号 %s 前缀不匹配", child, parent)
		}
	}
	return nil
}

// Validate 检查字段级不变量。
func (t Task) Validate() error {
	if t.ID == "" || t.ProductID == "" || t.IterationID == "" {
		return fmt.Errorf("%w: id、product_id、iteration_id 均不可为空", ErrValidation)
	}
	if err := t.Number.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrValidation, err)
	}
	if t.Title == "" {
		return fmt.Errorf("%w: title 为空", ErrValidation)
	}
	if !t.Kind.Valid() {
		return fmt.Errorf("%w: 未知任务类型 %q", ErrValidation, t.Kind)
	}
	if !t.Priority.Valid() {
		return fmt.Errorf("%w: 未知优先级 %q", ErrValidation, t.Priority)
	}
	return nil
}

// Reassign 转派负责人。移除成员前其未终结任务必须转派（不变量 17）。
func (t *Task) Reassign(assigneeID string) error {
	if t.Status.Terminal() {
		return fmt.Errorf("%w: 已终结任务不可转派", ErrValidation)
	}
	if assigneeID == "" {
		return fmt.Errorf("%w: 转派目标为空", ErrValidation)
	}
	t.AssigneeID = assigneeID
	return nil
}

// EnsureChildrenDone 强制不变量 9：任务进入终态前全部子任务必须 done
// （可由模板 require_children_done 关闭）。
func EnsureChildrenDone(children []Task, required bool) error {
	if !required {
		return nil
	}
	for _, c := range children {
		if c.Status != TaskDone {
			return fmt.Errorf("%w: 子任务 %s 尚未 done，父任务不可进入终态（不变量 9）",
				ErrValidation, c.Number)
		}
	}
	return nil
}

// EnsureIterationClosedCleanly 强制不变量 10：迭代进入终态前，
// 其下未终结任务必须显式转移或取消，不允许静默丢弃。
func EnsureIterationClosedCleanly(tasks []Task) error {
	for _, t := range tasks {
		if !t.Status.Terminal() {
			return fmt.Errorf("%w: 任务 %s 未终结，需显式转移或取消（不变量 10）",
				ErrValidation, t.Number)
		}
	}
	return nil
}

// TaskModuleLink 是任务与模块的多对多关联（一个任务可跨多个模块）。
type TaskModuleLink struct {
	TaskID   string
	ModuleID string
}

// EnsureModuleSameProduct 强制不变量 2：TaskModule 中的模块必须与任务同产品。
func EnsureModuleSameProduct(taskProductID, moduleProductID string) error {
	if taskProductID != moduleProductID {
		return fmt.Errorf("%w: 模块与任务必须同产品（不变量 2）", ErrValidation)
	}
	return nil
}
