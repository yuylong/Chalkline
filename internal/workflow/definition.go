// Package workflow 是工作流引擎：定义解析、DAG 校验、状态判定与门禁裁决。
// 引擎是纯函数式的状态判定（docs/tech.md 架构决策），不落库、不感知 HTTP。
// 只支持节点、有向边、门禁三种要素，不引入脚本、条件表达式、子流程。
package workflow

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/yuylong/Chalkline/internal/domain/workitem"
	"gopkg.in/yaml.v3"
)

// NodeType 是节点类型（docs/domain/workflow.md）：
// normal 需要有人推进；auto 进入即完成；terminal 终态不可回退。
type NodeType string

const (
	NodeNormal   NodeType = "normal"
	NodeAuto     NodeType = "auto"
	NodeTerminal NodeType = "terminal"
)

func (t NodeType) Valid() bool {
	return t == NodeNormal || t == NodeAuto || t == NodeTerminal
}

// Settings 是模板级设置。
type Settings struct {
	// RequireChildrenDone 为 true 时父任务终态节点在全部子任务 done 前保持阻塞。
	RequireChildrenDone bool `yaml:"require_children_done" json:"require_children_done"`
	// AllowSkipNodes 是否允许跳过节点（M0 仅解析与保存，跳过语义后续版本实现）。
	AllowSkipNodes bool `yaml:"allow_skip_nodes" json:"allow_skip_nodes"`
}

// Node 是模板中的节点。
type Node struct {
	ID   string   `yaml:"id" json:"id"`
	Name string   `yaml:"name" json:"name"`
	Type NodeType `yaml:"type" json:"type"`
	// Outcome 多终态时必填（done / cancelled）；单终态缺省为 done。
	Outcome workitem.Outcome `yaml:"outcome,omitempty" json:"outcome,omitempty"`
	// Gate 准出门禁；省略即不拦截，提交直接通过。
	Gate *workitem.Gate `yaml:"gate,omitempty" json:"gate,omitempty"`
	// AllowsTaskCreation 仅 iteration 作用域有效：该阶段允许创建任务（声明式白名单）。
	AllowsTaskCreation bool `yaml:"allows_task_creation,omitempty" json:"allows_task_creation,omitempty"`
	// RequireIterationTasksDone 仅 iteration 作用域有效：
	// 该节点 passed 需迭代下全部任务已终结（不变量 18）。
	RequireIterationTasksDone bool `yaml:"require_iteration_tasks_done,omitempty" json:"require_iteration_tasks_done,omitempty"`
	// DueInDays 期望耗时（天），超期控制台标红，不阻断。
	DueInDays *int `yaml:"due_in_days,omitempty" json:"due_in_days,omitempty"`
}

// Edge 是有向边。并行不需要新语法——分叉与汇合都是边的效果。
type Edge struct {
	From string `yaml:"from" json:"from"`
	To   string `yaml:"to" json:"to"`
}

// Definition 是一份工作流模板。流程定义存于数据库（JSON），
// 作者用 YAML 编辑，平台校验后转换（docs/domain/workflow.md）。
type Definition struct {
	ID       string         `yaml:"id" json:"id"`
	Name     string         `yaml:"name" json:"name"`
	Scope    workitem.Scope `yaml:"scope" json:"scope"`
	Version  int            `yaml:"version" json:"version"`
	Settings Settings       `yaml:"settings" json:"settings"`
	Nodes    []Node         `yaml:"nodes" json:"nodes"`
	Edges    []Edge         `yaml:"edges" json:"edges"`
}

// ErrInvalidDefinition 表示模板定义非法，错误消息可直接展示。
var ErrInvalidDefinition = errors.New("workflow: 模板定义非法")

// ParseYAML 解析并校验 YAML 模板。校验失败即拒绝入库。
func ParseYAML(data []byte) (Definition, error) {
	var def Definition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return Definition{}, fmt.Errorf("%w: YAML 解析失败: %w", ErrInvalidDefinition, err)
	}
	if err := def.Validate(); err != nil {
		return Definition{}, err
	}
	return def, nil
}

// ParseJSON 解析并校验 JSON 模板（数据库存储格式）。
func ParseJSON(data []byte) (Definition, error) {
	var def Definition
	if err := json.Unmarshal(data, &def); err != nil {
		return Definition{}, fmt.Errorf("%w: JSON 解析失败: %w", ErrInvalidDefinition, err)
	}
	if err := def.Validate(); err != nil {
		return Definition{}, err
	}
	return def, nil
}

// Normalize 归一化：gate mode 默认 all；单终态 outcome 默认 done。
func (d *Definition) Normalize() {
	for i := range d.Nodes {
		if d.Nodes[i].Gate != nil {
			d.Nodes[i].Gate.Normalize()
		}
	}
	terminals := d.TerminalNodes()
	if len(terminals) == 1 && terminals[0].Outcome == "" {
		for i := range d.Nodes {
			if d.Nodes[i].ID == terminals[0].ID {
				d.Nodes[i].Outcome = workitem.OutcomeDone
			}
		}
	}
}
