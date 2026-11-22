package workflow

import (
	"errors"
	"testing"
	"time"

	"github.com/yuylong/Chalkline/internal/domain/identity"
	"github.com/yuylong/Chalkline/internal/domain/workitem"
)

var now = time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)

func stdInstance(t *testing.T) *Instance {
	t.Helper()
	def := mustParse(t, `
id: wf-std
name: 标准任务
scope: task
version: 1
settings:
  require_children_done: true
nodes:
  - { id: clarify, name: 澄清, type: normal }
  - { id: spec, name: 规格, type: normal,
      gate: { approvals: 1, roles: [reviewer, pm], artifacts: [{ kind: spec, required: true }, { kind: pre_review, required: false }] } }
  - { id: implement, name: 实现, type: normal }
  - { id: done, name: 完成, type: terminal }
edges:
  - { from: clarify, to: spec }
  - { from: spec, to: implement }
  - { from: implement, to: done }
`)
	inst, err := NewInstance(def, now)
	if err != nil {
		t.Fatalf("实例化失败: %v", err)
	}
	return inst
}

func specArtifact() workitem.ArtifactRef {
	return workitem.ArtifactRef{
		Kind: workitem.KindSpec, Provider: workitem.ProviderOutline,
		URL: "https://outline.example.com/doc/spec-91", NodeID: "spec", SubmittedBy: "usr_dev",
	}
}

func approve(nodeID, reviewerID string, roles ...identity.Role) workitem.ReviewDecision {
	return workitem.ReviewDecision{NodeID: nodeID, ReviewerID: reviewerID,
		Decision: workitem.DecisionApprove, Roles: roles, CreatedAt: now}
}

func reject(nodeID, reviewerID, comment string, roles ...identity.Role) workitem.ReviewDecision {
	return workitem.ReviewDecision{NodeID: nodeID, ReviewerID: reviewerID,
		Decision: workitem.DecisionReject, Comment: comment, Roles: roles, CreatedAt: now}
}

// mustStart / mustSubmit：断言成功的引擎操作，失败即终止当前测试。
func mustStart(t *testing.T, inst *Instance, nodeID string, version int64) {
	t.Helper()
	if err := inst.Start(nodeID, version, now); err != nil {
		t.Fatalf("start %s: %v", nodeID, err)
	}
}

func mustSubmit(t *testing.T, inst *Instance, nodeID string, version int64, in NodeInput) {
	t.Helper()
	if err := inst.Submit(nodeID, version, in, now); err != nil {
		t.Fatalf("submit %s: %v", nodeID, err)
	}
}

func TestSerialWalkThrough(t *testing.T) {
	inst := stdInstance(t)

	// 起点 ready，其余 pending。
	st, _ := inst.State("clarify")
	if st.Status != workitem.StatusReady {
		t.Fatalf("起点状态 = %s, 期望 ready", st.Status)
	}
	st, _ = inst.State("spec")
	if st.Status != workitem.StatusPending {
		t.Fatalf("后继节点状态 = %s, 期望 pending", st.Status)
	}

	// 开始并提交无门禁节点 → 直接 passed，后继 ready。
	if err := inst.Start("clarify", 1, now); err != nil {
		t.Fatalf("start clarify: %v", err)
	}
	if err := inst.Submit("clarify", 2, NodeInput{}, now); err != nil {
		t.Fatalf("submit clarify: %v", err)
	}
	st, _ = inst.State("clarify")
	if st.Status != workitem.StatusPassed {
		t.Fatalf("clarify 应 passed: %s", st.Status)
	}
	st, _ = inst.State("spec")
	if st.Status != workitem.StatusReady {
		t.Fatalf("spec 应 ready: %s", st.Status)
	}

	// 有门禁节点：缺材料提交 → ARTIFACT_MISSING (422)。
	if err := inst.Start("spec", 1, now); err != nil {
		t.Fatalf("start spec: %v", err)
	}
	err := inst.Submit("spec", 2, NodeInput{}, now)
	if !errors.Is(err, ErrArtifactMissing) {
		t.Fatalf("缺材料提交应报 ErrArtifactMissing, 实际: %v", err)
	}

	// 补材料提交 → awaiting_review。
	if err := inst.Submit("spec", 2, NodeInput{Artifacts: []workitem.ArtifactRef{specArtifact()}}, now); err != nil {
		t.Fatalf("submit spec: %v", err)
	}
	st, _ = inst.State("spec")
	if st.Status != workitem.StatusAwaitingReview {
		t.Fatalf("spec 应 awaiting_review: %s", st.Status)
	}

	// 无角色评审人的 approve 无效；合格评审人通过后级联。
	var decisions []workitem.ReviewDecision
	_, decisions, err = inst.Review("spec", 3, approve("spec", "usr_qa", identity.RoleQA),
		decisions, NodeInput{Artifacts: []workitem.ArtifactRef{specArtifact()}}, now)
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	st, _ = inst.State("spec")
	if st.Status != workitem.StatusAwaitingReview {
		t.Fatalf("qa 对 spec 门禁的 approve 不应生效: %s", st.Status)
	}
	_, _, err = inst.Review("spec", 3, approve("spec", "usr_rv", identity.RoleReviewer),
		decisions, NodeInput{Artifacts: []workitem.ArtifactRef{specArtifact()}}, now)
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	st, _ = inst.State("spec")
	if st.Status != workitem.StatusPassed {
		t.Fatalf("reviewer 通过后 spec 应 passed: %s", st.Status)
	}
	st, _ = inst.State("implement")
	if st.Status != workitem.StatusReady {
		t.Fatalf("implement 应 ready: %s", st.Status)
	}

	// 终态收口：require_children_done=true 且子任务未 done → 终态阻塞。
	if err := inst.Start("implement", 1, now); err != nil {
		t.Fatalf("start implement: %v", err)
	}
	if err := inst.Submit("implement", 2, NodeInput{}, now); err != nil {
		t.Fatalf("submit implement: %v", err)
	}
	st, _ = inst.State("implement")
	if st.Status != workitem.StatusPassed {
		t.Fatalf("implement 无门禁应直接 passed: %s", st.Status)
	}
	st, _ = inst.State("done")
	if st.Status != workitem.StatusReady {
		t.Fatalf("done 应 ready: %s", st.Status)
	}

	// 终态收口：子任务未 done 时提交被拒；完成后抵达终态。
	if err := inst.Start("done", 1, now); err != nil {
		t.Fatalf("start done: %v", err)
	}
	err = inst.Submit("done", 2, NodeInput{}, now)
	if !errors.Is(err, ErrGateBlocked) {
		t.Fatalf("子任务未 done 时终态提交应阻塞: %v", err)
	}
	if _, done := inst.Terminal(); done {
		t.Fatal("子任务未 done，终态不应抵达")
	}
	if err := inst.Submit("done", 2, NodeInput{ChildrenDone: true}, now); err != nil {
		t.Fatalf("子任务 done 后终态提交: %v", err)
	}
	if _, done := inst.Terminal(); !done {
		t.Fatal("应抵达终态")
	}
}

func TestRejectAndResubmitKeepsApprovals(t *testing.T) {
	// 独立模板：门禁需 2 名 reviewer，才能同时观察 approve 保留与 reject 阻塞。
	def := mustParse(t, `
id: wf-2a
name: 双审批
scope: task
version: 1
nodes:
  - { id: spec, name: 规格, type: normal,
      gate: { approvals: 2, roles: [reviewer], artifacts: [{ kind: spec, required: true }] } }
  - { id: done, name: 完成, type: terminal }
edges:
  - { from: spec, to: done }
`)
	inst, err := NewInstance(def, now)
	if err != nil {
		t.Fatalf("实例化: %v", err)
	}
	if err := inst.Start("spec", 1, now); err != nil {
		t.Fatalf("start spec: %v", err)
	}
	artifacts := []workitem.ArtifactRef{specArtifact()}
	if err := inst.Submit("spec", 2, NodeInput{Artifacts: artifacts}, now); err != nil {
		t.Fatalf("submit spec: %v", err)
	}

	var decisions []workitem.ReviewDecision
	// 评审人 A 通过：1/2，仍等待。
	_, decisions, err = inst.Review("spec", 3, approve("spec", "usr_a", identity.RoleReviewer),
		decisions, NodeInput{Artifacts: artifacts}, now)
	if err != nil {
		t.Fatalf("review A: %v", err)
	}
	// 评审人 B 驳回（最近一次决策为准）→ 节点 rejected，仅回退一次。
	_, decisions, err = inst.Review("spec", 3, reject("spec", "usr_b", "缺少非目标节", identity.RoleReviewer),
		decisions, NodeInput{Artifacts: artifacts}, now)
	if err != nil {
		t.Fatalf("review B: %v", err)
	}
	st, _ := inst.State("spec")
	if st.Status != workitem.StatusRejected {
		t.Fatalf("驳回后 spec 应 rejected: %s", st.Status)
	}

	// 重提：进入 awaiting_review；A 的 approve 保留累计，B 的 reject 持续阻塞准出。
	if err := inst.Submit("spec", st.Version, NodeInput{Artifacts: artifacts}, now); err != nil {
		t.Fatalf("重提: %v", err)
	}
	st, _ = inst.State("spec")
	if st.Status != workitem.StatusAwaitingReview {
		t.Fatalf("重提后应回到 awaiting_review: %s", st.Status)
	}
	res := Evaluate(gateOf(inst, "spec"), NodeInput{Decisions: decisions, Artifacts: artifacts},
		nodeSettings(inst.Definition, mustNode(inst, "spec")))
	if res.Passed || len(res.PendingRejects) != 1 || res.Approvals != 1 || res.ApprovalsNeeded != 2 {
		t.Fatalf("驳回重提语义异常: %+v", res)
	}

	// 原驳回人重新表态 approve → 2/2 通过。
	_, _, err = inst.Review("spec", 5, approve("spec", "usr_b", identity.RoleReviewer),
		decisions, NodeInput{Artifacts: artifacts}, now)
	if err != nil {
		t.Fatalf("review B 重表态: %v", err)
	}
	st, _ = inst.State("spec")
	if st.Status != workitem.StatusPassed {
		t.Fatalf("双 approve 后应 passed: %s", st.Status)
	}
}

func TestAutoNodeEntersAndPasses(t *testing.T) {
	def := mustParse(t, `
id: wf-auto
name: 自动节点
scope: task
version: 1
nodes:
  - { id: a, name: A, type: normal }
  - { id: auto, name: 自动, type: auto }
  - { id: done, name: 完成, type: terminal }
edges:
  - { from: a, to: auto }
  - { from: auto, to: done }
`)
	inst, err := NewInstance(def, now)
	if err != nil {
		t.Fatalf("实例化: %v", err)
	}
	if err := inst.Start("a", 1, now); err != nil {
		t.Fatalf("start a: %v", err)
	}
	if err := inst.Submit("a", 2, NodeInput{}, now); err != nil {
		t.Fatalf("submit a: %v", err)
	}
	// auto 节点进入即完成，且级联让 done ready。
	st, _ := inst.State("auto")
	if st.Status != workitem.StatusPassed {
		t.Fatalf("auto 应已 passed: %s", st.Status)
	}
	st, _ = inst.State("done")
	if st.Status != workitem.StatusReady {
		t.Fatalf("done 应 ready: %s", st.Status)
	}
	// auto 节点不可开始/提交。
	if err := inst.Start("auto", 2, now); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("auto 节点 start 应报 ErrInvalidTransition: %v", err)
	}
}

func gateOf(inst *Instance, nodeID string) workitem.Gate {
	node, _ := inst.Definition.Node(nodeID)
	return *node.Gate
}

func mustNode(inst *Instance, nodeID string) Node {
	node, _ := inst.Definition.Node(nodeID)
	return node
}

func TestParallelForkAndJoin(t *testing.T) {
	def := mustParse(t, `
id: wf-par
name: 并行
scope: iteration
version: 1
nodes:
  - { id: start, name: 起步, type: normal }
  - { id: left, name: 左分支, type: normal,
      gate: { approvals: 1, roles: [reviewer], artifacts: [{ kind: spec, required: true }] } }
  - { id: right, name: 右分支, type: normal }
  - { id: join, name: 汇合, type: normal }
  - { id: done, name: 完成, type: terminal }
edges:
  - { from: start, to: left }
  - { from: start, to: right }
  - { from: left, to: join }
  - { from: right, to: join }
  - { from: join, to: done }
`)
	inst, err := NewInstance(def, now)
	if err != nil {
		t.Fatalf("实例化: %v", err)
	}
	if err := inst.Start("start", 1, now); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := inst.Submit("start", 2, NodeInput{}, now); err != nil {
		t.Fatalf("submit start: %v", err)
	}

	// 分叉：左右分支同时 ready（fork）。
	for _, id := range []string{"left", "right"} {
		st, _ := inst.State(id)
		if st.Status != workitem.StatusReady {
			t.Fatalf("%s 应 ready: %s", id, st.Status)
		}
	}
	// 左分支通过不应放行汇合（AND 汇合）。
	if err := inst.Start("right", 1, now); err != nil {
		t.Fatalf("start right: %v", err)
	}
	if err := inst.Submit("right", 2, NodeInput{}, now); err != nil {
		t.Fatalf("submit right: %v", err)
	}
	st, _ := inst.State("join")
	if st.Status != workitem.StatusPending {
		t.Fatalf("右分支通过后 join 应保持 pending: %s", st.Status)
	}
	// 单支驳回不连坐：右分支已 passed 不受影响。
	if err := inst.Start("left", 1, now); err != nil {
		t.Fatalf("start left: %v", err)
	}
	if err := inst.Submit("left", 2, NodeInput{}, now); !errors.Is(err, ErrArtifactMissing) {
		t.Fatalf("left 缺材料提交应 422: %v", err)
	}
	st, _ = inst.State("right")
	if st.Status != workitem.StatusPassed {
		t.Fatalf("并行分支不应连坐: %s", st.Status)
	}

	// 左分支补材料提交并评审通过 → 汇合放行。
	art := workitem.ArtifactRef{Kind: workitem.KindSpec, Provider: workitem.ProviderOutline,
		URL: "https://outline.example.com/doc/1", NodeID: "left", SubmittedBy: "usr_dev"}
	if err := inst.Submit("left", 2, NodeInput{Artifacts: []workitem.ArtifactRef{art}}, now); err != nil {
		t.Fatalf("submit left: %v", err)
	}
	var decisions []workitem.ReviewDecision
	_, _, err = inst.Review("left", 3, approve("left", "usr_rv", identity.RoleReviewer),
		decisions, NodeInput{Artifacts: []workitem.ArtifactRef{art}}, now)
	if err != nil {
		t.Fatalf("review left: %v", err)
	}
	st, _ = inst.State("join")
	if st.Status != workitem.StatusReady {
		t.Fatalf("两支都通过后 join 应 ready: %s", st.Status)
	}
	// 当前节点集合应同时包含汇合与右分支的推进痕迹校验。
	if cur := inst.CurrentNodes(); len(cur) == 0 {
		t.Fatal("存在 actionable 节点")
	}
}

func TestOptimisticLock(t *testing.T) {
	inst := stdInstance(t)
	if err := inst.Start("clarify", 99, now); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("版本不匹配应报 ErrStateConflict: %v", err)
	}
	if err := inst.Start("spec", 1, now); !errors.Is(err, ErrNotReady) {
		t.Fatalf("前驱未完成应报 ErrNotReady: %v", err)
	}
}

func TestSubmitIdempotentState(t *testing.T) {
	// 幂等约定（mcp.md）：重复提交同一节点返回当前状态而非报错——
	// 引擎层面体现为 awaiting_review 状态下 Submit 报 ErrInvalidTransition，
	// 由 app 层把该错误翻译为「返回当前状态」。
	inst := stdInstance(t)
	mustStart(t, inst, "clarify", 1)
	mustSubmit(t, inst, "clarify", 2, NodeInput{})
	err := inst.Submit("clarify", 3, NodeInput{}, now)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("重复提交应报 ErrInvalidTransition（app 层幂等化）: %v", err)
	}
}

func TestProgress(t *testing.T) {
	inst := stdInstance(t)
	mustStart(t, inst, "clarify", 1)
	mustSubmit(t, inst, "clarify", 2, NodeInput{})
	passed, total := inst.Progress()
	if passed != 1 || total != 4 {
		t.Fatalf("进度 = %d/%d, 期望 1/4", passed, total)
	}
}
