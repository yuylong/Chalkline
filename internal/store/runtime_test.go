package store

// runtime_test.go：实例行 ↔ 引擎实例的纯转换单测（无需数据库）。

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/yuylong/Chalkline/internal/domain/identity"
	"github.com/yuylong/Chalkline/internal/domain/workitem"
	"github.com/yuylong/Chalkline/internal/workflow"
)

const taskDefJSON = `{
  "id": "wf-task-standard",
  "name": "标准任务流",
  "scope": "task",
  "version": 1,
  "nodes": [
    {"id": "clarify", "name": "澄清", "type": "normal"},
    {"id": "spec", "name": "规格", "type": "normal", "gate": {"approvals": 1, "roles": ["reviewer"], "mode": "all"}},
    {"id": "done", "name": "完成", "type": "terminal"}
  ],
  "edges": [
    {"from": "clarify", "to": "spec"},
    {"from": "spec", "to": "done"}
  ]
}`

func TestRowsToInstanceRoundTrip(t *testing.T) {
	var defJSON json.RawMessage = []byte(taskDefJSON)
	def, err := workflow.ParseJSON(defJSON)
	if err != nil {
		t.Fatalf("模板非法: %v", err)
	}
	inst, err := workflow.NewInstance(def, time.Unix(1000, 0))
	if err != nil {
		t.Fatalf("实例化失败: %v", err)
	}

	row := InstanceRow{
		ID: "wfi_1", WorkflowID: def.ID, WorkflowVersion: def.Version,
		WorkItemType: workitem.ScopeTask, WorkItemID: "tsk_1",
		DefinitionJSON: defJSON, TerminalNodeID: inst.TerminalNodeID,
	}
	states := make([]workflow.NodeState, 0, len(inst.Nodes))
	for _, st := range inst.Nodes {
		states = append(states, *st)
	}

	got, err := RowsToInstance(row, states)
	if err != nil {
		t.Fatalf("还原失败: %v", err)
	}
	if got.TerminalNodeID != inst.TerminalNodeID {
		t.Fatalf("终态不一致")
	}
	if len(got.Nodes) != len(inst.Nodes) {
		t.Fatalf("节点数不一致")
	}
	for id, st := range inst.Nodes {
		g := got.Nodes[id]
		if g.Status != st.Status || g.Version != st.Version {
			t.Fatalf("节点 %s 状态不一致: %s/%d vs %s/%d", id, g.Status, g.Version, st.Status, st.Version)
		}
	}
}

func TestRowsToInstanceRejectsMissingNodeState(t *testing.T) {
	var defJSON json.RawMessage = []byte(taskDefJSON)
	row := InstanceRow{DefinitionJSON: defJSON}
	states := []workflow.NodeState{
		{NodeID: "clarify", Status: workitem.StatusReady, Version: 1},
	}
	if _, err := RowsToInstance(row, states); err == nil {
		t.Fatalf("缺节点状态应报错")
	}
}

func TestRowsToInputsGroupsByNode(t *testing.T) {
	approvals := []workitem.ReviewDecision{
		{NodeID: "spec", ReviewerID: "u1", Decision: workitem.DecisionReject, Comment: "改", Roles: identity.Roles{identity.RoleReviewer}},
		{NodeID: "spec", ReviewerID: "u1", Decision: workitem.DecisionApprove, Roles: identity.Roles{identity.RoleReviewer}},
		{NodeID: "done", ReviewerID: "u2", Decision: workitem.DecisionApprove, Roles: identity.Roles{identity.RoleAdmin}},
	}
	artifacts := []workitem.ArtifactRef{
		{Kind: workitem.KindSpec, Provider: workitem.ProviderOutline, URL: "https://o/d/1", NodeID: "spec", SubmittedBy: "u3"},
		{Kind: workitem.KindPreReview, Provider: workitem.ProviderInline, Content: "自查", NodeID: "spec", SubmittedBy: "u3"},
	}
	inputs := RowsToInputs(approvals, artifacts)

	spec := inputs["spec"]
	if len(spec.Decisions) != 2 {
		t.Fatalf("spec 决策数应为 2（保留历史，引擎取最近），实际 %d", len(spec.Decisions))
	}
	if len(spec.Artifacts) != 2 {
		t.Fatalf("spec 材料数应为 2，实际 %d", len(spec.Artifacts))
	}
	if len(inputs["done"].Decisions) != 1 {
		t.Fatalf("done 决策数应为 1")
	}
	// 无审批/材料的节点不建条目：零值补齐由 app.GetNextAction 按模板节点兜底。
}
