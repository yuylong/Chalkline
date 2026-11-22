package planning

import (
	"errors"
	"fmt"
	"time"
)

// IterationStatus 是迭代状态。
type IterationStatus string

const (
	IterationPlanned   IterationStatus = "planned"
	IterationActive    IterationStatus = "active"
	IterationClosed    IterationStatus = "closed"
	IterationCancelled IterationStatus = "cancelled"
)

// Terminal 报告迭代是否处于终态。终态后不可回退（不变量 11）。
func (s IterationStatus) Terminal() bool {
	return s == IterationClosed || s == IterationCancelled
}

// Iteration 是一条完整生命周期，从调研走到总结；粒度由团队定义。
// 生命周期节点即「阶段」：迭代的当前节点就是它当前所处的阶段。
type Iteration struct {
	ID         string
	ProductID  string
	Name       string
	Goal       string
	Number     IterationNumber
	StartsOn   time.Time
	EndsOn     time.Time
	Status     IterationStatus
	WorkflowID string // 生命周期模板，默认取产品默认值
}

// ErrValidation 表示实体违反不变量。
var ErrValidation = errors.New("planning: 实体校验失败")

// New 构造并校验迭代。
func NewIteration(id, productID, name, goal string, number IterationNumber,
	startsOn, endsOn time.Time, workflowID string) (Iteration, error) {
	it := Iteration{ID: id, ProductID: productID, Name: name, Goal: goal, Number: number,
		StartsOn: startsOn, EndsOn: endsOn, Status: IterationPlanned, WorkflowID: workflowID}
	if err := it.Validate(); err != nil {
		return Iteration{}, err
	}
	return it, nil
}

// Validate 检查字段级不变量。
func (it Iteration) Validate() error {
	if it.ID == "" || it.ProductID == "" {
		return fmt.Errorf("%w: id 与 product_id 均不可为空", ErrValidation)
	}
	if _, _, err := it.Number.Parse(); err != nil {
		return fmt.Errorf("%w: %w", ErrValidation, err)
	}
	if it.StartsOn.IsZero() || it.EndsOn.IsZero() {
		return fmt.Errorf("%w: 计划日期不可为空", ErrValidation)
	}
	if it.EndsOn.Before(it.StartsOn) {
		return fmt.Errorf("%w: ends_on 早于 starts_on", ErrValidation)
	}
	return nil
}

// Activate 将 planned 迭代置为 active。同一产品可同时存在多个 active 迭代（不变量 12）。
func (it *Iteration) Activate() error {
	if it.Status != IterationPlanned {
		return fmt.Errorf("%w: 仅 planned 迭代可激活，当前 %q", ErrValidation, it.Status)
	}
	it.Status = IterationActive
	return nil
}

// Close 收口迭代：进入终态前其下未终结任务必须显式转移或取消（不变量 10），
// 终态后不可回退（不变量 11）。hasOpenTasks 由 app 层查询提供。
func (it *Iteration) Close(hasOpenTasks bool) error {
	if it.Status != IterationActive {
		return fmt.Errorf("%w: 仅 active 迭代可收口，当前 %q", ErrValidation, it.Status)
	}
	if hasOpenTasks {
		return fmt.Errorf("%w: 迭代下仍有未终结任务，需先转移或取消（不变量 10）", ErrValidation)
	}
	it.Status = IterationClosed
	return nil
}

// Cancel 取消迭代，同样要求先处理未终结任务。
func (it *Iteration) Cancel(hasOpenTasks bool) error {
	if it.Status.Terminal() {
		return fmt.Errorf("%w: 迭代已处于终态，不可回退（不变量 11）", ErrValidation)
	}
	if hasOpenTasks {
		return fmt.Errorf("%w: 迭代下仍有未终结任务，需先转移或取消（不变量 10）", ErrValidation)
	}
	it.Status = IterationCancelled
	return nil
}
