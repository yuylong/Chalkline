package workflow

import (
	"fmt"
	"sort"
	"time"

	"github.com/yuylong/Chalkline/internal/domain/workitem"
)

// NodeState 是节点运行态。携带乐观锁版本号：并发修改冲突时拒绝写入并返回
// ErrStateConflict（docs/domain/workflow.md「并发控制」）。
type NodeState struct {
	NodeID      string              `json:"node_id"`
	Status      workitem.NodeStatus `json:"status"`
	Version     int64               `json:"version"` // 每次状态变更 +1
	EnteredAt   time.Time           `json:"entered_at"`
	CompletedAt time.Time           `json:"completed_at"`
}

// Instance 是工作项的运行态。实例绑定某个模板版本；
// 工作项的业务状态（如任务的 open/done）是节点状态的投影，不单独维护。
type Instance struct {
	Definition Definition            `json:"definition"`
	Nodes      map[string]*NodeState `json:"nodes"`
	// TerminalNodeID 已抵达的终态节点（空即未抵达）。
	TerminalNodeID string `json:"terminal_node_id"`
}

// NodeInput 是引擎判定某个节点所需的全部外部输入（材料、审批、收口状态）。
// 引擎不落库：这些数据由 app 层从存储读出后传入，保持判定纯函数化。
type NodeInput struct {
	// Decisions 该节点全部历史审批（时间升序）；同一评审人以最近一次为准。
	Decisions []workitem.ReviewDecision
	// Artifacts 该节点当前材料集合（含 awaiting_review 期间补挂的）。
	Artifacts []workitem.ArtifactRef
	// ChildrenDone 全部子任务是否 done（父任务终态收口，settings.require_children_done）。
	ChildrenDone bool
	// IterationTasksDone 迭代下全部任务是否终结（require_iteration_tasks_done）。
	IterationTasksDone bool
}

// NewInstance 用模板版本实例化：全部节点 pending，起点进入 ready，
// auto 节点进入即完成（级联推进直到需要人）。
func NewInstance(def Definition, now time.Time) (*Instance, error) {
	if err := def.Validate(); err != nil {
		return nil, err
	}
	inst := &Instance{Definition: def, Nodes: map[string]*NodeState{}}
	for _, n := range def.Nodes {
		inst.Nodes[n.ID] = &NodeState{NodeID: n.ID, Status: workitem.StatusPending, Version: 1}
	}
	for _, id := range def.StartNodes() {
		inst.enterReadyOrPass(id, now)
	}
	inst.cascade(def.StartNodes(), now)
	return inst, nil
}

// State 返回节点运行态。
func (i *Instance) State(nodeID string) (*NodeState, error) {
	st, ok := i.Nodes[nodeID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNodeNotFound, nodeID)
	}
	return st, nil
}

// readyToStart 判定：所有前驱节点 passed 且自身未被跳过（docs/domain/workflow.md）。
func (i *Instance) readyToStart(nodeID string) bool {
	for _, p := range i.Definition.Predecessors(nodeID) {
		if i.Nodes[p].Status != workitem.StatusPassed {
			return false
		}
	}
	return true
}

// Start 开始节点：ready → in_progress。仅 ready 状态可开始。
// expectedVersion 用于乐观锁校验。
func (i *Instance) Start(nodeID string, expectedVersion int64, now time.Time) error {
	st, err := i.State(nodeID)
	if err != nil {
		return err
	}
	if st.Version != expectedVersion {
		return ErrStateConflict
	}
	if st.Status != workitem.StatusReady {
		if st.Status == workitem.StatusPending {
			// 前驱未完成：语义上属于 NOT_READY 而非非法迁移。
			return fmt.Errorf("%w: 节点 %s 前驱未全部通过", ErrNotReady, nodeID)
		}
		return fmt.Errorf("%w: 节点 %s 状态为 %s，需 ready", ErrInvalidTransition, nodeID, st.Status)
	}
	st.Status = workitem.StatusInProgress
	st.EnteredAt = now
	st.Version++
	return nil
}

// Submit 提交节点：校验必需材料随提交齐备（缺则 ErrArtifactMissing / 422）；
// 无门禁的节点提交即 passed；有门禁的进入 awaiting_review，
// 随后立即重算——驳回重提且累计审批已够时可直接通过。
func (i *Instance) Submit(nodeID string, expectedVersion int64, in NodeInput, now time.Time) error {
	st, err := i.State(nodeID)
	if err != nil {
		return err
	}
	if st.Version != expectedVersion {
		return ErrStateConflict
	}
	if !st.Status.CanSubmit() {
		return fmt.Errorf("%w: 节点 %s 状态为 %s，不可提交", ErrInvalidTransition, nodeID, st.Status)
	}
	node, ok := i.Definition.Node(nodeID)
	if !ok {
		return fmt.Errorf("%w: %s", ErrNodeNotFound, nodeID)
	}
	if node.Gate != nil {
		if missing := missingRequiredArtifacts(*node.Gate, in.Artifacts); len(missing) > 0 {
			return fmt.Errorf("%w: 节点 %s 缺少 %v", ErrArtifactMissing, nodeID, missing)
		}
	}
	// 收口校验：无论终态节点是否显式挂门禁，收口要求都必须满足
	//（docs/domain/workflow.md「父任务收口」「迭代收口」）。
	s := nodeSettings(i.Definition, node)
	if s.requireChildrenDone && !in.ChildrenDone {
		return fmt.Errorf("%w: 节点 %s 需全部子任务 done 后才可提交", ErrGateBlocked, nodeID)
	}
	if s.requireIterationTasksDone && !in.IterationTasksDone {
		return fmt.Errorf("%w: 节点 %s 需迭代下全部任务终结后才可提交", ErrGateBlocked, nodeID)
	}
	i.passOrAwait(node, st, in, now)
	return nil
}

// passOrAwait 是提交后的共同推进：无门禁直接 passed（markPassed 内递增版本）；
// 有门禁进入 awaiting_review（版本 +1）并立刻按当前输入重算准出。
func (i *Instance) passOrAwait(node Node, st *NodeState, in NodeInput, now time.Time) {
	st.EnteredAt = now
	if node.Gate == nil {
		i.markPassed(st, now)
		i.cascade([]string{node.ID}, now)
		return
	}
	st.Status = workitem.StatusAwaitingReview
	st.Version++
	res := Evaluate(*node.Gate, in, nodeSettings(i.Definition, node))
	if res.Passed {
		i.markPassed(st, now)
		i.cascade([]string{node.ID}, now)
	}
}

// Review 记录评审决策（upsert：同一评审人以最近一次为准，天然幂等）并重算节点：
//   - 任一合格评审人最近一次决策为 reject → 节点回退 rejected（只回退一次，不重复触发）；
//   - 否则按准出判定：满足即 passed 并级联；不满足维持 awaiting_review。
//
// expectedVersion 用于乐观锁校验；返回合并后的决策列表，由 app 层持久化，引擎自身不写库。
func (i *Instance) Review(nodeID string, expectedVersion int64, decision workitem.ReviewDecision,
	existing []workitem.ReviewDecision, in NodeInput, now time.Time) (GateResult, []workitem.ReviewDecision, error) {
	if err := decision.Validate(); err != nil {
		return GateResult{}, nil, fmt.Errorf("workflow: %w", err)
	}
	if decision.NodeID != nodeID {
		return GateResult{}, nil, fmt.Errorf("workflow: 决策归属节点 %s 与目标节点 %s 不一致", decision.NodeID, nodeID)
	}
	st, err := i.State(nodeID)
	if err != nil {
		return GateResult{}, nil, err
	}
	if st.Version != expectedVersion {
		return GateResult{}, nil, ErrStateConflict
	}
	if st.Status != workitem.StatusAwaitingReview && st.Status != workitem.StatusRejected {
		return GateResult{}, nil, fmt.Errorf("%w: 节点 %s 状态为 %s，不可评审",
			ErrInvalidTransition, nodeID, st.Status)
	}
	merged := mergeDecisions(existing, decision)
	node, ok := i.Definition.Node(nodeID)
	if !ok {
		return GateResult{}, nil, fmt.Errorf("%w: %s", ErrNodeNotFound, nodeID)
	}
	if node.Gate == nil {
		return GateResult{}, nil, fmt.Errorf("%w: 节点 %s 无门禁，不可评审", ErrInvalidTransition, nodeID)
	}
	in.Decisions = merged
	res := Evaluate(*node.Gate, in, nodeSettings(i.Definition, node))

	switch {
	case len(res.PendingRejects) > 0 && st.Status == workitem.StatusAwaitingReview:
		// 任一 reject → 节点回到待修改（rejected），驳回理由由 app 层写审计与通知。
		st.Status = workitem.StatusRejected
		st.Version++
	case res.Passed:
		i.markPassed(st, now)
		i.cascade([]string{nodeID}, now)
	default:
		// 审批不足或材料缺失：状态不变（驳回重提后不因再次不足而重复回退）。
	}
	return res, merged, nil
}

// mergeDecisions upsert：移除同一评审人旧记录后追加，保持时间序。
func mergeDecisions(existing []workitem.ReviewDecision, decision workitem.ReviewDecision) []workitem.ReviewDecision {
	out := make([]workitem.ReviewDecision, 0, len(existing)+1)
	for _, d := range existing {
		if d.ReviewerID != decision.ReviewerID {
			out = append(out, d)
		}
	}
	return append(out, decision)
}

// markPassed 置节点 passed；终态节点同时记录结论。
func (i *Instance) markPassed(st *NodeState, now time.Time) {
	st.Status = workitem.StatusPassed
	st.CompletedAt = now
	st.Version++
	if n, ok := i.Definition.Node(st.NodeID); ok && n.Type == NodeTerminal {
		i.TerminalNodeID = st.NodeID
	}
}

// enterReadyOrPass 让满足就绪条件的节点进入 ready（auto 节点进入即完成）。
func (i *Instance) enterReadyOrPass(nodeID string, now time.Time) {
	st := i.Nodes[nodeID]
	node, _ := i.Definition.Node(nodeID)
	st.EnteredAt = now
	if node.Type == NodeAuto {
		i.markPassed(st, now)
		return
	}
	if st.Status == workitem.StatusPending {
		st.Status = workitem.StatusReady
	}
}

// cascade 从给定节点集向后推进：前驱齐备的后继进入 ready，
// auto 节点即时 passed，汇合节点需全部前驱 passed（AND 汇合），
// 分叉则多条后继同时就绪（fork）。循环直到状态不再变化。
func (i *Instance) cascade(from []string, now time.Time) {
	queue := append([]string(nil), from...)
	seen := map[string]bool{}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if seen[cur] {
			continue
		}
		seen[cur] = true
		for _, nextID := range i.Definition.Successors(cur) {
			st := i.Nodes[nextID]
			if st.Status != workitem.StatusPending || !i.readyToStart(nextID) {
				continue
			}
			i.enterReadyOrPass(nextID, now)
			if st.Status == workitem.StatusPassed {
				queue = append(queue, nextID) // auto 节点继续向后级联
			}
		}
	}
}

// Terminal 返回终态结论；未抵达时 ok 为 false。
func (i *Instance) Terminal() (outcome workitem.Outcome, ok bool) {
	if i.TerminalNodeID == "" {
		return "", false
	}
	node, _ := i.Definition.Node(i.TerminalNodeID)
	return node.Outcome, true
}

// Progress 返回 passed 节点数与可达节点总数（状态投影：进度百分比）。
func (i *Instance) Progress() (passed, total int) {
	reachable := i.Definition.reachableFrom(i.Definition.StartNodes(), succMap(i.Definition))
	for id := range reachable {
		total++
		if i.Nodes[id].Status == workitem.StatusPassed {
			passed++
		}
	}
	return passed, total
}

func succMap(d Definition) map[string][]string {
	m := map[string][]string{}
	for _, e := range d.Edges {
		m[e.From] = append(m[e.From], e.To)
	}
	return m
}

// CurrentNodes 返回「当前节点」集合：最近进入且未完成（actionable）的节点，
// 按 id 排序保证稳定（并行分支时是集合，串行模板长度恒为 1）。
func (i *Instance) CurrentNodes() []string {
	var out []string
	for id, st := range i.Nodes {
		if st.Status.Actionable() {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// missingRequiredArtifacts 返回缺失的必填材料类型。
func missingRequiredArtifacts(gate workitem.Gate, artifacts []workitem.ArtifactRef) []workitem.ArtifactKind {
	present := map[workitem.ArtifactKind]bool{}
	for _, a := range artifacts {
		if err := a.ValidateRef(); err == nil {
			present[a.Kind] = true
		}
	}
	var missing []workitem.ArtifactKind
	for _, req := range gate.Artifacts {
		if req.Required && !present[req.Kind] {
			missing = append(missing, req.Kind)
		}
	}
	return missing
}

// nodeSettings 汇总节点的收口要求。
func nodeSettings(def Definition, node Node) evalSettings {
	return evalSettings{
		requireChildrenDone:       def.Settings.RequireChildrenDone && node.Type == NodeTerminal,
		requireIterationTasksDone: node.RequireIterationTasksDone,
	}
}

// evalSettings 是 Evaluate 需要的模板侧收口配置。
type evalSettings struct {
	requireChildrenDone       bool
	requireIterationTasksDone bool
}

// RejectDetail 是未决驳回明细：理由与时间随评审人一并透出，
// 让 Agent 不必人工转述就能读到驳回原因（mcp.md blocker.comment）。
type RejectDetail struct {
	ReviewerID string    `json:"reviewer_id"`
	Comment    string    `json:"comment,omitempty"`
	At         time.Time `json:"at,omitempty"`
}

// GateResult 是门禁裁决结论：Passed 即准出；
// 各缺失项与阻塞人供 get_next_action 组装 blockers 明细。
type GateResult struct {
	Passed           bool                    `json:"passed"`
	PendingRejects   []string                `json:"pending_rejects"` // 最近一次决策为 reject 的评审人
	Approvals        int                     `json:"approvals"`       // 有效 approve 数（不同评审人）
	ApprovalsNeeded  int                     `json:"approvals_needed"`
	MissingArtifacts []workitem.ArtifactKind `json:"missing_artifacts"`
	ChildrenBlocked  bool                    `json:"children_blocked"`  // 子任务未全部 done
	IterationBlocked bool                    `json:"iteration_blocked"` // 迭代下存在未终结任务
	// PendingRejectDetails 与 PendingRejects 一一对应（同序），携带驳回理由与时间。
	PendingRejectDetails []RejectDetail `json:"pending_reject_details,omitempty"`
}

// Evaluate 是门禁准出判定（纯函数，docs/domain/workflow.md「状态与判定算法」）：
//
//	passed = 无未决 reject AND 有效 approve 数 ≥ gate.approvals AND 必需材料齐备
//	  （并叠加收口要求：require_children_done / require_iteration_tasks_done）
//
// 「有效」指评审人持有门禁 roles 之一（admin 恒可，见 identity.Roles.HasAny）。
func Evaluate(gate workitem.Gate, in NodeInput, s evalSettings) GateResult {
	res := GateResult{ApprovalsNeeded: gate.Approvals}

	// 同一评审人只计一次有效意见：以最近一次决策为准。
	latest := map[string]workitem.ReviewDecision{}
	order := []string{}
	for _, d := range in.Decisions {
		if _, seen := latest[d.ReviewerID]; !seen {
			order = append(order, d.ReviewerID)
		}
		latest[d.ReviewerID] = d
	}
	approved := map[string]bool{}
	for _, rid := range order {
		d := latest[rid]
		if !d.Roles.HasAny(gate.Roles...) {
			continue // 无该门禁角色的决策不计入（服务端判定，AGENT.md 红线 2）
		}
		switch d.Decision {
		case workitem.DecisionApprove:
			approved[rid] = true
		case workitem.DecisionReject:
			res.PendingRejects = append(res.PendingRejects, rid)
			res.PendingRejectDetails = append(res.PendingRejectDetails, RejectDetail{
				ReviewerID: rid, Comment: d.Comment, At: d.CreatedAt,
			})
		}
	}
	res.Approvals = len(approved)
	if gate.Mode == workitem.GateModeAny {
		res.ApprovalsNeeded = 1
	}

	res.MissingArtifacts = missingRequiredArtifacts(gate, in.Artifacts)
	res.ChildrenBlocked = s.requireChildrenDone && !in.ChildrenDone
	res.IterationBlocked = s.requireIterationTasksDone && !in.IterationTasksDone

	res.Passed = len(res.PendingRejects) == 0 &&
		res.Approvals >= res.ApprovalsNeeded &&
		len(res.MissingArtifacts) == 0 &&
		!res.ChildrenBlocked &&
		!res.IterationBlocked
	return res
}
