package workflow

import (
	"fmt"
	"time"

	"github.com/yuylong/Chalkline/internal/domain/identity"
	"github.com/yuylong/Chalkline/internal/domain/workitem"
)

// 下一步是查出来的，不是猜出来的（docs/domain/mcp.md）。
// 本文件把「实例状态 + 材料与审批输入」投影成 Agent 可直接消费的结构化结论；
// 权限过滤（谁有资格看到哪个动作）由 M1 的 app 层叠加，引擎只做状态事实。

// WorkItemRef 是工作项的轻量标识，由调用方补充（引擎不感知迭代/任务实体）。
type WorkItemRef struct {
	Type  string `json:"type"` // task | iteration
	ID    string `json:"id"`
	Title string `json:"title"`
}

// CurrentNodeView 是「当前节点」视图条目；并行分支时是集合。
type CurrentNodeView struct {
	ID     string              `json:"id"`
	Name   string              `json:"name"`
	Status workitem.NodeStatus `json:"status"`
}

// Blocker 是阻塞项明细，type ∈ artifact_missing | approval_missing | pending_reject |
// children_not_done | iteration_tasks_not_done。
type Blocker struct {
	Type       string                `json:"type"`
	Kind       workitem.ArtifactKind `json:"kind,omitempty"`
	Required   int                   `json:"required,omitempty"`
	Current    int                   `json:"current,omitempty"`
	Roles      []identity.Role       `json:"roles,omitempty"`
	ReviewerID string                `json:"reviewer_id,omitempty"`
	// Comment 是评审人最近一次驳回的理由原文（pending_reject 专用）。
	// Agent 靠它做针对性修订，不必由人转述（mcp.md blocker 表）。
	Comment string `json:"comment,omitempty"`
	// RejectedAt 是驳回时间（RFC3339，pending_reject 专用）。
	RejectedAt string `json:"rejected_at,omitempty"`
	Message    string `json:"message"`
}

// ActionView 是单个节点的下一步动作。
type ActionView struct {
	NodeID   string    `json:"nodeId"`
	Tool     string    `json:"tool"`
	Blockers []Blocker `json:"blockers"`
}

// NextActionReport 对应 MCP 工具 chalkline_get_next_action 的返回契约。
type NextActionReport struct {
	WorkItem       WorkItemRef       `json:"workItem"`
	Status         string            `json:"status"` // ready | in_progress | awaiting_review | blocked | done
	CurrentNodes   []CurrentNodeView `json:"currentNodes"`
	NextActions    []ActionView      `json:"nextActions"`
	AllowedActions []string          `json:"allowedActions"`
}

// NextActionInputs 汇总投影所需的外部输入。
type NextActionInputs struct {
	WorkItem WorkItemRef
	// Inputs 每个节点的材料与审批输入（含收口状态）。
	Inputs map[string]NodeInput
	// EligibleReviewers 每个节点的合格评审人（app 层按产品成员与角色计算后传入）。
	EligibleReviewers map[string][]string
}

// 可提交的阻塞类型：材料缺失与收口未完成阻断提交；
// approval_missing / pending_reject 是提交后由人处理的预览信息，不阻断提交本身。
func (b Blocker) blocksSubmit() bool {
	return b.Type == "artifact_missing" ||
		b.Type == "children_not_done" ||
		b.Type == "iteration_tasks_not_done"
}

// NextAction 生成结构化结论。判定规则（服务端事实，Agent 不得跳过）：
//   - done：终态已抵达；
//   - ready：存在可立即执行的动作（start ready 节点，或材料齐备可提交/重提）；
//   - awaiting_review：仅剩等待评审的节点；
//   - blocked：存在因材料缺失或收口未完成而无法提交的节点（blockers 逐条处理）；
//   - in_progress：防御性兜底（串行流中正常不会出现）。
func NextAction(inst *Instance, in NextActionInputs) (NextActionReport, error) {
	// 切片初始化为空而非 nil：序列化后必须是 [] 而不是 null，
	// 否则控制台在 awaiting_review 时读 nextActions 会崩（mcp.md 返回契约）。
	report := NextActionReport{
		WorkItem:       in.WorkItem,
		CurrentNodes:   []CurrentNodeView{},
		NextActions:    []ActionView{},
		AllowedActions: []string{},
	}

	if _, done := inst.Terminal(); done {
		report.Status = "done"
		return report, nil
	}

	hasAwaiting := false
	submittable := false // 存在可提交（材料齐备）的节点
	attachable := false
	for _, id := range inst.CurrentNodes() {
		st, err := inst.State(id)
		if err != nil {
			return report, err
		}
		node, _ := inst.Definition.Node(id)
		report.CurrentNodes = append(report.CurrentNodes,
			CurrentNodeView{ID: id, Name: node.Name, Status: st.Status})

		switch st.Status {
		case workitem.StatusReady:
			report.NextActions = append(report.NextActions,
				ActionView{NodeID: id, Tool: "chalkline_start_step", Blockers: []Blocker{}})
			submittable = true
			report.AllowedActions = appendUnique(report.AllowedActions, "chalkline_start_step")
		case workitem.StatusInProgress, workitem.StatusRejected:
			action := submitAction(inst, id, node, in)
			executable := true
			for _, b := range action.Blockers {
				if b.blocksSubmit() {
					executable = false
				}
			}
			if executable {
				submittable = true
			}
			report.NextActions = append(report.NextActions, action)
			attachable = true
		case workitem.StatusAwaitingReview:
			hasAwaiting = true
			attachable = true // awaiting_review 期间允许补挂材料
		}
	}
	if attachable {
		report.AllowedActions = appendUnique(report.AllowedActions, "chalkline_attach_artifact")
	}

	switch {
	case submittable:
		report.Status = "ready"
	case hasAwaiting:
		report.Status = "awaiting_review"
	case len(report.NextActions) > 0:
		report.Status = "blocked"
	case len(report.CurrentNodes) > 0:
		report.Status = "in_progress"
	default:
		report.Status = "blocked" // 无当前节点且未到终态（防御分支）
	}
	return report, nil
}

// submitAction 组装提交动作与阻塞明细（含审批预览与驳回待表态）。
func submitAction(inst *Instance, nodeID string, node Node, in NextActionInputs) ActionView {
	action := ActionView{NodeID: nodeID, Tool: "chalkline_submit_step"}
	if node.Gate == nil {
		return action // 无门禁：提交即通过，无阻塞项
	}
	input := in.Inputs[nodeID]
	res := Evaluate(*node.Gate, input, nodeSettings(inst.Definition, node))

	for _, kind := range res.MissingArtifacts {
		action.Blockers = append(action.Blockers, Blocker{
			Type:    "artifact_missing",
			Kind:    kind,
			Message: fmt.Sprintf("需上传 %s 材料", kind),
		})
	}
	if res.Approvals < res.ApprovalsNeeded {
		action.Blockers = append(action.Blockers, Blocker{
			Type:     "approval_missing",
			Required: res.ApprovalsNeeded,
			Current:  res.Approvals,
			Roles:    node.Gate.Roles,
			Message: fmt.Sprintf("需 %d 名 %v 角色评审确认，当前 %d",
				res.ApprovalsNeeded, roleNames(node.Gate.Roles), res.Approvals),
		})
	}
	for _, rd := range res.PendingRejectDetails {
		msg := fmt.Sprintf("评审人 %s 最近一次决策为驳回，需其重新表态", rd.ReviewerID)
		if rd.Comment != "" {
			msg = fmt.Sprintf("评审人 %s 驳回：%s", rd.ReviewerID, rd.Comment)
		}
		b := Blocker{
			Type:       "pending_reject",
			ReviewerID: rd.ReviewerID,
			Comment:    rd.Comment,
			Message:    msg,
		}
		if !rd.At.IsZero() {
			b.RejectedAt = rd.At.UTC().Format(time.RFC3339)
		}
		action.Blockers = append(action.Blockers, b)
	}
	if res.ChildrenBlocked {
		action.Blockers = append(action.Blockers, Blocker{
			Type:    "children_not_done",
			Message: "子任务未全部 done，终态节点保持阻塞",
		})
	}
	if res.IterationBlocked {
		action.Blockers = append(action.Blockers, Blocker{
			Type:    "iteration_tasks_not_done",
			Message: "迭代下存在未终结任务，节点保持阻塞",
		})
	}
	return action
}

func roleNames(roles []identity.Role) []string {
	out := make([]string, len(roles))
	for i, r := range roles {
		out[i] = string(r)
	}
	return out
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}
