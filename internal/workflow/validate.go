package workflow

import (
	"fmt"
	"sort"

	"github.com/yuylong/Chalkline/internal/domain/planning"
	"github.com/yuylong/Chalkline/internal/domain/workitem"
)

// Validate 校验模板定义的全部结构约束。所有规则失败都拒绝入库。
func (d Definition) Validate() error {
	d.Normalize()
	if d.ID == "" {
		return fmt.Errorf("%w: id 为空", ErrInvalidDefinition)
	}
	if d.Name == "" {
		return fmt.Errorf("%w: name 为空", ErrInvalidDefinition)
	}
	if !d.Scope.Valid() {
		return fmt.Errorf("%w: scope 非法 %q（iteration | task）", ErrInvalidDefinition, d.Scope)
	}
	if d.Version < 1 {
		return fmt.Errorf("%w: version 需 ≥ 1，当前 %d", ErrInvalidDefinition, d.Version)
	}
	if len(d.Nodes) == 0 {
		return fmt.Errorf("%w: 至少需要一个节点", ErrInvalidDefinition)
	}

	// 节点 id 唯一、类型与门禁合法、作用域专属属性不越界。
	ids := map[string]bool{}
	for _, n := range d.Nodes {
		if n.ID == "" {
			return fmt.Errorf("%w: 存在 id 为空的节点", ErrInvalidDefinition)
		}
		if ids[n.ID] {
			return fmt.Errorf("%w: 节点 id %q 重复", ErrInvalidDefinition, n.ID)
		}
		ids[n.ID] = true
		if n.Name == "" {
			return fmt.Errorf("%w: 节点 %s 缺少名称", ErrInvalidDefinition, n.ID)
		}
		if !n.Type.Valid() {
			return fmt.Errorf("%w: 节点 %s 类型非法 %q", ErrInvalidDefinition, n.ID, n.Type)
		}
		if n.Gate != nil {
			if err := n.Gate.Validate(); err != nil {
				return fmt.Errorf("%w: 节点 %s 门禁非法: %w", ErrInvalidDefinition, n.ID, err)
			}
		}
		if n.DueInDays != nil && *n.DueInDays < 0 {
			return fmt.Errorf("%w: 节点 %s due_in_days 需 ≥ 0", ErrInvalidDefinition, n.ID)
		}
		if d.Scope == workitem.ScopeTask {
			// allows_task_creation / allows_task_kinds / require_iteration_tasks_done
			// 仅 iteration 作用域有效。
			if n.AllowsTaskCreation {
				return fmt.Errorf("%w: 节点 %s 声明 allows_task_creation，但该属性仅 iteration 作用域有效",
					ErrInvalidDefinition, n.ID)
			}
			if len(n.AllowsTaskKinds) > 0 {
				return fmt.Errorf("%w: 节点 %s 声明 allows_task_kinds，但该属性仅 iteration 作用域有效",
					ErrInvalidDefinition, n.ID)
			}
			if n.RequireIterationTasksDone {
				return fmt.Errorf("%w: 节点 %s 声明 require_iteration_tasks_done，但该属性仅 iteration 作用域有效",
					ErrInvalidDefinition, n.ID)
			}
		} else {
			// 白名单里的任务类型必须合法（未知类型直接拒绝入库）。
			for _, k := range n.AllowsTaskKinds {
				if !planning.TaskKind(k).Valid() {
					return fmt.Errorf("%w: 节点 %s allows_task_kinds 含未知任务类型 %q",
						ErrInvalidDefinition, n.ID, k)
				}
			}
		}
	}

	// 边引用存在、无自环、无重复边。
	edgeSeen := map[string]bool{}
	succ := map[string][]string{}
	pred := map[string][]string{}
	outDeg := map[string]int{}
	inDeg := map[string]int{}
	for _, e := range d.Edges {
		if !ids[e.From] || !ids[e.To] {
			return fmt.Errorf("%w: 边 %s→%s 引用了不存在的节点", ErrInvalidDefinition, e.From, e.To)
		}
		if e.From == e.To {
			return fmt.Errorf("%w: 边 %s→%s 是自环", ErrInvalidDefinition, e.From, e.To)
		}
		key := e.From + "\x00" + e.To
		if edgeSeen[key] {
			return fmt.Errorf("%w: 边 %s→%s 重复", ErrInvalidDefinition, e.From, e.To)
		}
		edgeSeen[key] = true
		succ[e.From] = append(succ[e.From], e.To)
		pred[e.To] = append(pred[e.To], e.From)
		outDeg[e.From]++
		inDeg[e.To]++
	}

	// 终态节点：不可有出边；多终态时必须各自声明 outcome 且互不重复。
	terminals := d.TerminalNodes()
	if len(terminals) == 0 {
		return fmt.Errorf("%w: 模板缺少 terminal 节点", ErrInvalidDefinition)
	}
	outcomes := map[workitem.Outcome]bool{}
	for _, t := range terminals {
		if outDeg[t.ID] > 0 {
			return fmt.Errorf("%w: 终态节点 %s 不可有出边", ErrInvalidDefinition, t.ID)
		}
		if len(terminals) > 1 && t.Outcome == "" {
			return fmt.Errorf("%w: 存在多个终态节点，%s 必须声明 outcome", ErrInvalidDefinition, t.ID)
		}
		if t.Outcome != "" {
			if !t.Outcome.Valid() {
				return fmt.Errorf("%w: 节点 %s outcome 非法 %q", ErrInvalidDefinition, t.ID, t.Outcome)
			}
			if outcomes[t.Outcome] {
				return fmt.Errorf("%w: 终态 outcome %q 重复", ErrInvalidDefinition, t.Outcome)
			}
			outcomes[t.Outcome] = true
		}
	}

	// 非终态节点必须有出边，否则流程无法到达终态。
	for _, n := range d.Nodes {
		if n.Type != NodeTerminal && outDeg[n.ID] == 0 {
			return fmt.Errorf("%w: 非终态节点 %s 没有出边", ErrInvalidDefinition, n.ID)
		}
	}

	// 起点集合：无入边的节点。至少一个，且所有节点都要从起点可达。
	var starts []string
	for _, n := range d.Nodes {
		if inDeg[n.ID] == 0 {
			starts = append(starts, n.ID)
		}
	}
	if len(starts) == 0 {
		return fmt.Errorf("%w: 模板没有起点（每个节点都有入边）", ErrInvalidDefinition)
	}
	reachable := d.reachableFrom(starts, succ)
	for _, n := range d.Nodes {
		if !reachable[n.ID] {
			return fmt.Errorf("%w: 节点 %s 从起点不可达", ErrInvalidDefinition, n.ID)
		}
	}

	// DAG：禁止环（模板是有向无环图，分叉/汇合都由边表达）。
	if cycle := findCycle(d, succ); cycle != "" {
		return fmt.Errorf("%w: 模板存在环，起点 %s", ErrInvalidDefinition, cycle)
	}
	return nil
}

// TerminalNodes 返回全部终态节点，按 id 排序保证稳定。
func (d Definition) TerminalNodes() []Node {
	var out []Node
	for _, n := range d.Nodes {
		if n.Type == NodeTerminal {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Node 按 id 查找节点；不存在返回 false。
func (d Definition) Node(id string) (Node, bool) {
	for _, n := range d.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return Node{}, false
}

// StartNodes 返回无入边的起点集合。
func (d Definition) StartNodes() []string {
	inDeg := map[string]int{}
	for _, e := range d.Edges {
		inDeg[e.To]++
	}
	var out []string
	for _, n := range d.Nodes {
		if inDeg[n.ID] == 0 {
			out = append(out, n.ID)
		}
	}
	sort.Strings(out)
	return out
}

// Successors / Predecessors 返回直接后继 / 前驱。
func (d Definition) Successors(id string) []string {
	var out []string
	for _, e := range d.Edges {
		if e.From == id {
			out = append(out, e.To)
		}
	}
	return out
}

func (d Definition) Predecessors(id string) []string {
	var out []string
	for _, e := range d.Edges {
		if e.To == id {
			out = append(out, e.From)
		}
	}
	return out
}

// reachableFrom 从起点集合做 BFS，返回可达节点集合。
func (d Definition) reachableFrom(starts []string, succ map[string][]string) map[string]bool {
	seen := map[string]bool{}
	queue := append([]string(nil), starts...)
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if seen[cur] {
			continue
		}
		seen[cur] = true
		queue = append(queue, succ[cur]...)
	}
	return seen
}

// findCycle 用 DFS 找环，返回环上任意一个起点 id；无环返回空串。
func findCycle(d Definition, succ map[string][]string) string {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var visit func(id string) string
	visit = func(id string) string {
		color[id] = gray
		for _, next := range succ[id] {
			switch color[next] {
			case gray:
				return next
			case white:
				if hit := visit(next); hit != "" {
					return hit
				}
			}
		}
		color[id] = black
		return ""
	}
	for _, n := range d.Nodes {
		if color[n.ID] == white {
			if hit := visit(n.ID); hit != "" {
				return hit
			}
		}
	}
	return ""
}
