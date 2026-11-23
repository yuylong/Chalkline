package workflow

import (
	"errors"
	"testing"

	"github.com/yuylong/Chalkline/internal/domain/identity"
	"github.com/yuylong/Chalkline/internal/domain/workitem"
)

// next_action 投影对应 MCP 工具 chalkline_get_next_action 的返回契约。
func TestNextActionStatuses(t *testing.T) {
	inst := stdInstance(t)
	ref := WorkItemRef{Type: "task", ID: "tsk_7f3a", Title: "工作流引擎状态判定"}

	// 初始：clarify ready → status=ready，start_step 无阻塞。
	rep, err := NextAction(inst, NextActionInputs{WorkItem: ref})
	if err != nil {
		t.Fatalf("投影失败: %v", err)
	}
	if rep.Status != "ready" || len(rep.NextActions) != 1 ||
		rep.NextActions[0].Tool != "chalkline_start_step" || len(rep.NextActions[0].Blockers) != 0 {
		t.Fatalf("初始投影异常: %+v", rep)
	}
	if rep.WorkItem.ID != "tsk_7f3a" {
		t.Errorf("workItem 应回显: %+v", rep.WorkItem)
	}

	// 开始后：in_progress + 缺材料 + 缺审批 → blocked，blockers 逐条可读。
	mustStart(t, inst, "clarify", 1)
	mustSubmit(t, inst, "clarify", 2, NodeInput{})
	mustStart(t, inst, "spec", 1)
	rep, err = NextAction(inst, NextActionInputs{WorkItem: ref})
	if err != nil {
		t.Fatalf("投影失败: %v", err)
	}
	if rep.Status != "blocked" {
		t.Fatalf("缺材料应 blocked: %s", rep.Status)
	}
	if len(rep.NextActions) != 1 || rep.NextActions[0].Tool != "chalkline_submit_step" {
		t.Fatalf("nextActions 异常: %+v", rep.NextActions)
	}
	var hasArtifact, hasApproval bool
	for _, b := range rep.NextActions[0].Blockers {
		switch b.Type {
		case "artifact_missing":
			hasArtifact = b.Kind == workitem.KindSpec
		case "approval_missing":
			hasApproval = b.Required == 1 && b.Current == 0
		}
	}
	if !hasArtifact || !hasApproval {
		t.Fatalf("blockers 应含材料与审批缺失: %+v", rep.NextActions[0].Blockers)
	}
	if len(rep.AllowedActions) == 0 {
		t.Error("应允许 attach_artifact")
	}

	// 材料齐备 → ready（可提交）。
	rep, _ = NextAction(inst, NextActionInputs{WorkItem: ref,
		Inputs: map[string]NodeInput{"spec": {Artifacts: []workitem.ArtifactRef{specArtifact()}}}})
	if rep.Status != "ready" {
		t.Fatalf("材料齐备应 ready: %s（blockers=%+v）", rep.Status, rep.NextActions)
	}

	// 提交后 → awaiting_review，无 nextActions。
	mustSubmit(t, inst, "spec", 2, NodeInput{Artifacts: []workitem.ArtifactRef{specArtifact()}})
	rep, _ = NextAction(inst, NextActionInputs{WorkItem: ref})
	if rep.Status != "awaiting_review" {
		t.Fatalf("已提交应 awaiting_review: %s", rep.Status)
	}
	if len(rep.NextActions) != 0 {
		t.Fatalf("等待评审不应有 nextActions: %+v", rep.NextActions)
	}
	if rep.NextActions == nil || rep.AllowedActions == nil {
		t.Fatal("nextActions/allowedActions 应为空数组而非 null（控制台依赖该契约）")
	}

	// 走到终态 → done。
	if _, _, err := inst.Review("spec", 3, approve("spec", "usr_rv", identity.RoleReviewer),
		nil, NodeInput{Artifacts: []workitem.ArtifactRef{specArtifact()}}, now); err != nil {
		t.Fatalf("review spec: %v", err)
	}
	mustStart(t, inst, "implement", 1)
	mustSubmit(t, inst, "implement", 2, NodeInput{})
	// 终态需 children done：先被阻塞，再放行抵达终态。
	mustStart(t, inst, "done", 1)
	if err := inst.Submit("done", 2, NodeInput{}, now); !errors.Is(err, ErrGateBlocked) {
		t.Fatalf("子任务未 done 时终态提交应阻塞: %v", err)
	}
	if err := inst.Submit("done", 2, NodeInput{ChildrenDone: true}, now); err != nil {
		t.Fatalf("子任务 done 后终态提交: %v", err)
	}
	rep, _ = NextAction(inst, NextActionInputs{WorkItem: ref})
	if _, done := inst.Terminal(); !done {
		t.Fatalf("终态应抵达: %+v", inst.Nodes["done"])
	}
	if rep.Status != "done" {
		t.Fatalf("终态投影应为 done: %s", rep.Status)
	}
}

func TestNextActionParallelReportsMultipleNodes(t *testing.T) {
	// 并行分支时 currentNodes 与 nextActions 返回多个条目（mcp.md 返回契约）。
	def := mustParse(t, `
id: wf-par2
name: 并行
scope: iteration
version: 1
nodes:
  - { id: start, name: 起步, type: normal }
  - { id: left, name: 左, type: normal }
  - { id: right, name: 右, type: normal }
  - { id: join, name: 汇合, type: normal }
  - { id: done, name: 完成, type: terminal }
edges:
  - { from: start, to: left }
  - { from: start, to: right }
  - { from: left, to: join }
  - { from: right, to: join }
  - { from: join, to: done }
`)
	inst, _ := NewInstance(def, now)
	mustStart(t, inst, "start", 1)
	mustSubmit(t, inst, "start", 2, NodeInput{})
	rep, err := NextAction(inst, NextActionInputs{})
	if err != nil {
		t.Fatalf("投影: %v", err)
	}
	if len(rep.CurrentNodes) != 2 || len(rep.NextActions) != 2 {
		t.Fatalf("并行分支应返回 2 个当前节点: %+v", rep)
	}
	if rep.Status != "ready" {
		t.Fatalf("双 ready 应为 ready: %s", rep.Status)
	}
}
