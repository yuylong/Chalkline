package app

// workflowsvc.go：工作流模板管理与实例操作用例（docs/modules/workflow.md）。
// 引擎判定已在 M0 交付且不改；本文件做「加载 → 引擎 → 写回」的持久化编排：
// 事务、乐观锁、审计、outbox 事件、材料替换重置审批、迭代状态投影。

import (
	"context"
	"errors"
	"time"

	"github.com/yuylong/Chalkline/internal/domain/identity"
	"github.com/yuylong/Chalkline/internal/domain/planning"
	"github.com/yuylong/Chalkline/internal/domain/product"
	"github.com/yuylong/Chalkline/internal/domain/workitem"
	"github.com/yuylong/Chalkline/internal/store"
	"github.com/yuylong/Chalkline/internal/workflow"
	"gopkg.in/yaml.v3"
)

// ---- 模板管理 ----

// WorkflowView 是模板视图。
type WorkflowView struct {
	ID        string `json:"id"`
	Scope      string `json:"scope"`
	Name       string `json:"name"`
	Version    int    `json:"version"`
	Status     string `json:"status"`
	Definition string `json:"definition,omitempty"` // JSON 文本（详情用）
}

func viewWorkflow(w store.WorkflowRow, withDef bool) WorkflowView {
	v := WorkflowView{ID: w.ID, Scope: string(w.Scope), Name: w.Name, Version: w.Version, Status: w.Status}
	if withDef {
		v.Definition = string(w.Definition)
	}
	return v
}

// ListWorkflows 模板列表（登录即可读，含内置）。
func (s *Service) ListWorkflows(ctx context.Context, _ Actor, scope *workitem.Scope) ([]WorkflowView, error) {
	rows, err := store.ListWorkflows(ctx, s.DB.q(), scope)
	if err != nil {
		return nil, err
	}
	out := make([]WorkflowView, 0, len(rows))
	for _, w := range rows {
		out = append(out, viewWorkflow(w, false))
	}
	return out, nil
}

// GetWorkflow 模板详情。
func (s *Service) GetWorkflow(ctx context.Context, _ Actor, id string) (WorkflowView, error) {
	w, err := store.WorkflowByID(ctx, s.DB.q(), id)
	if err != nil {
		return WorkflowView{}, wrapNotFound(err, "模板不存在")
	}
	return viewWorkflow(w, true), nil
}

// parseTemplateYAML 解析模板草稿：补默认版本，校验失败即拒绝。
func parseTemplateYAML(data []byte) (workflow.Definition, error) {
	var def workflow.Definition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return def, Invalid("YAML 解析失败: %v", err)
	}
	if def.Version == 0 {
		def.Version = 1
	}
	def.Normalize()
	if err := def.Validate(); err != nil {
		return def, Invalid("%s", err.Error())
	}
	return def, nil
}

// CreateWorkflow 新建模板（pm / admin）；模板创建即 active，可直接被产品引用。
func (s *Service) CreateWorkflow(ctx context.Context, actor Actor, yamlText string) (WorkflowView, error) {
	def, err := parseTemplateYAML([]byte(yamlText))
	if err != nil {
		return WorkflowView{}, err
	}
	if def.ID == "" {
		def.ID = s.newID("wf")
	}
	err = s.DB.WithTx(ctx, func(q store.Querier) error {
		if err := store.InsertWorkflow(ctx, q, def, "active"); err != nil {
			if store.IsDuplicate(err) {
				return Invalid("模板 id 或 (scope,name,version) 已存在")
			}
			return err
		}
		return audit(ctx, q, actor, "workflow.create", "workflow", def.ID,
			map[string]any{"scope": string(def.Scope), "name": def.Name})
	})
	if err != nil {
		return WorkflowView{}, err
	}
	return WorkflowView{ID: def.ID, Scope: string(def.Scope), Name: def.Name, Version: def.Version, Status: "active"}, nil
}

// UpdateWorkflow 改模板即升版本（运行中实例不受影响）；pm / admin。
func (s *Service) UpdateWorkflow(ctx context.Context, actor Actor, id, yamlText string) (WorkflowView, error) {
	def, err := parseTemplateYAML([]byte(yamlText))
	if err != nil {
		return WorkflowView{}, err
	}
	var out WorkflowView
	err = s.DB.WithTx(ctx, func(q store.Querier) error {
		row, err := store.WorkflowByID(ctx, q, id)
		if err != nil {
			return wrapNotFound(err, "模板不存在")
		}
		def.ID = id // id 不可改；scope 亦沿用原值防错挂
		def.Scope = row.Scope
		version, err := store.BumpWorkflowVersion(ctx, q, id, row.Version, def)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return stateConflict()
			}
			return err
		}
		def.Version = version
		if err := audit(ctx, q, actor, "workflow.update", "workflow", id,
			map[string]any{"version": version}); err != nil {
			return err
		}
		out = WorkflowView{ID: id, Scope: string(def.Scope), Name: def.Name, Version: version, Status: row.Status}
		return nil
	})
	return out, err
}

// ArchiveWorkflow 软删模板；pm / admin。
func (s *Service) ArchiveWorkflow(ctx context.Context, actor Actor, id string) error {
	return s.DB.WithTx(ctx, func(q store.Querier) error {
		if err := store.ArchiveWorkflow(ctx, q, id); err != nil {
			return wrapNotFound(err, "模板不存在")
		}
		return audit(ctx, q, actor, "workflow.archive", "workflow", id, nil)
	})
}

// ---- 实例操作 ----

// loadedInstance 是一次操作加载的全部运行态。
type loadedInstance struct {
	Row    store.InstanceRow
	Inst   *workflow.Instance
	Inputs map[string]workflow.NodeInput
}

// loadInstance 加载实例与逐节点输入（审批 + 材料）。
func (s *Service) loadInstance(ctx context.Context, q store.Querier, wtype workitem.Scope, workItemID string) (loadedInstance, error) {
	var li loadedInstance
	row, err := store.InstanceByWorkItem(ctx, q, wtype, workItemID)
	if err != nil {
		return li, wrapNotFound(err, "工作项不存在或不可见")
	}
	li.Row = row
	states, err := store.ListNodeStates(ctx, q, row.ID)
	if err != nil {
		return li, err
	}
	inst, err := store.RowsToInstance(row, states)
	if err != nil {
		return li, err
	}
	li.Inst = inst
	approvals, err := store.ListApprovals(ctx, q, row.ID)
	if err != nil {
		return li, err
	}
	artifacts, err := store.ListArtifacts(ctx, q, row.ID)
	if err != nil {
		return li, err
	}
	refs := make([]workitem.ArtifactRef, 0, len(artifacts))
	for _, a := range artifacts {
		refs = append(refs, a.Ref)
	}
	li.Inputs = store.RowsToInputs(approvals, refs)
	return li, nil
}

// workItemCtx 定位工作项的产品与写权限（start/submit/attach）。
func (s *Service) workItemCtx(ctx context.Context, q store.Querier, wtype workitem.Scope, workItemID string) (memberContext, error) {
	var productID string
	var assignee string
	switch wtype {
	case workitem.ScopeTask:
		t, err := store.TaskByID(ctx, q, workItemID)
		if err != nil {
			return memberContext{}, wrapNotFound(err, "任务不存在或不可见")
		}
		productID, assignee = t.ProductID, t.AssigneeID
	case workitem.ScopeIteration:
		it, err := store.IterationByID(ctx, q, workItemID)
		if err != nil {
			return memberContext{}, wrapNotFound(err, "迭代不存在或不可见")
		}
		productID = it.ProductID
	default:
		return memberContext{}, Invalid("工作项类型非法")
	}
	mc, err := s.mustView(ctx, q, Actor{UserID: ""}, productID) // 占位，实际 actor 由调用方传入
	_ = mc
	return mc, err
}

// NodeOpResult 是节点操作的统一产物。
type NodeOpResult struct {
	Node       NodeStateView `json:"node"`
	Gate       *GateResultView `json:"gate,omitempty"`
	Instance   InstanceView  `json:"instance"`
}

// GateResultView 是门禁裁决视图（GATE_BLOCKED 细节与评审人列表）。
type GateResultView struct {
	Passed           bool     `json:"passed"`
	Approvals        int      `json:"approvals"`
	ApprovalsNeeded  int      `json:"approvalsNeeded"`
	PendingRejects   []string `json:"pendingRejects,omitempty"`
	MissingArtifacts []string `json:"missingArtifacts,omitempty"`
	ChildrenBlocked  bool     `json:"childrenBlocked,omitempty"`
	IterationBlocked bool     `json:"iterationBlocked,omitempty"`
	EligibleReviewers []string `json:"eligibleReviewers,omitempty"`
}

// StartStep 开始当前可执行节点（任务负责人 / owner / pm / admin；迭代为成员+pm 或 owner/admin）。
func (s *Service) StartStep(ctx context.Context, actor Actor, wtype workitem.Scope, workItemID, nodeID string, expectedVersion int64) (NodeOpResult, error) {
	var out NodeOpResult
	err := s.DB.WithTx(ctx, func(q store.Querier) error {
		if _, _, err := s.stepPermission(ctx, q, actor, wtype, workItemID); err != nil {
			return err
		}
		li, err := s.loadInstance(ctx, q, wtype, workItemID)
		if err != nil {
			return err
		}
		now := s.Now()
		if err := li.Inst.Start(nodeID, expectedVersion, now); err != nil {
			return MapError(err)
		}
		if err := store.SaveNodeStates(ctx, q, li.Row.ID, li.Inst); err != nil {
			return err
		}
		if err := audit(ctx, q, actor, "workflow.start_step", string(wtype), workItemID,
			map[string]any{"node": nodeID}); err != nil {
			return err
		}
		if err := s.syncIterationStatus(ctx, q, wtype, workItemID, li.Inst); err != nil {
			return err
		}
		return s.fillNodeOpResult(ctx, q, li, nodeID, &out)
	})
	return out, err
}

// ArtifactInput 是材料引用入参（provider+url/externalId 或 inline content）。
type ArtifactInput struct {
	Kind       string `json:"kind"`
	Provider    string `json:"provider"`
	URL        string `json:"url,omitempty"`
	ExternalID string `json:"externalId,omitempty"`
	Title      string `json:"title,omitempty"`
	Content    string `json:"content,omitempty"` // 仅 inline
}

func (in ArtifactInput) toRef(nodeID, submittedBy string) (workitem.ArtifactRef, error) {
	ref := workitem.ArtifactRef{
		Kind: workitem.ArtifactKind(in.Kind), Provider: workitem.ArtifactProvider(in.Provider),
		URL: in.URL, ExternalID: in.ExternalID, Title: in.Title, Content: in.Content,
		NodeID: nodeID, SubmittedBy: submittedBy,
	}
	if err := ref.ValidateRef(); err != nil {
		return ref, InvalidArtifactRef(err)
	}
	return ref, nil
}

// SubmitStep 提交节点，携带材料与说明；重复提交同节点返回当前状态（幂等，mcp.md 约定）。
func (s *Service) SubmitStep(ctx context.Context, actor Actor, wtype workitem.Scope, workItemID, nodeID string,
	expectedVersion int64, artifacts []ArtifactInput, note string) (NodeOpResult, error) {
	var out NodeOpResult
	err := s.DB.WithTx(ctx, func(q store.Querier) error {
		mc, assignee, err := s.stepPermission(ctx, q, actor, wtype, workItemID)
		if err != nil {
			return err
		}
		_ = mc
		_ = assignee

		li, err := s.loadInstance(ctx, q, wtype, workItemID)
		if err != nil {
			return err
		}

		// 材料引用校验（不变量 14：provider 与 kind 匹配）。
		inRefs := make([]workitem.ArtifactRef, 0, len(artifacts))
		for _, a := range artifacts {
			ref, err := a.toRef(nodeID, actor.UserID)
			if err != nil {
				return err
			}
			inRefs = append(inRefs, ref)
		}

		// 替换判定：同 kind 已有材料且内容不同 → 作废该节点既有审批（材料变更语义）。
		existing := li.Inputs[nodeID].Artifacts
		replaced := false
		effective := make([]workitem.ArtifactRef, len(existing))
		copy(effective, existing)
		for _, ref := range inRefs {
			for i, e := range effective {
				if e.Kind == ref.Kind {
					effective[i] = ref
					if artifactDiffers(e, ref) {
						replaced = true
					}
					break
				}
			}
			if !hasKind(effective, ref.Kind) {
				effective = append(effective, ref)
			}
		}

		in := li.Inputs[nodeID]
		in.Artifacts = effective
		if wtype == workitem.ScopeTask {
			open, err := store.CountUnfinishedChildren(ctx, q, workItemID)
			if err != nil {
				return err
			}
			in.ChildrenDone = open == 0
		}
		if wtype == workitem.ScopeIteration {
			open, err := store.CountUnfinishedTasksInIteration(ctx, q, workItemID)
			if err != nil {
				return err
			}
			in.IterationTasksDone = open == 0
		}

		now := s.Now()
		if err := li.Inst.Submit(nodeID, expectedVersion, in, now); err != nil {
			// 幂等：awaiting_review 重复提交返回当前状态而非报错（mcp.md）。
			if errors.Is(err, workflow.ErrInvalidTransition) {
				if st, serr := li.Inst.State(nodeID); serr == nil && st.Status == workitem.StatusAwaitingReview {
					return s.fillNodeOpResult(ctx, q, li, nodeID, &out)
				}
			}
			return MapError(err)
		}

		// 材料落库（每节点每 kind 一份，替换即作废审批）。
		for _, ref := range inRefs {
			row := store.ArtifactRow{ID: s.newID("art"), InstanceID: li.Row.ID, Ref: ref}
			if err := store.ReplaceArtifact(ctx, q, row, replaced); err != nil {
				return err
			}
		}

		if err := store.SaveNodeStates(ctx, q, li.Row.ID, li.Inst); err != nil {
			return err
		}
		if err := audit(ctx, q, actor, "workflow.submit_step", string(wtype), workItemID,
			map[string]any{"node": nodeID, "note": note, "artifacts": len(inRefs)}); err != nil {
			return err
		}

		// 事件：passed → node.passed；awaiting → gate.pending。
		st, _ := li.Inst.State(nodeID)
		switch st.Status {
		case workitem.StatusPassed:
			if err := store.InsertOutbox(ctx, q, "node.passed", string(wtype), workItemID,
				map[string]any{"node": nodeID}); err != nil {
				return err
			}
		case workitem.StatusAwaitingReview:
			if err := store.InsertOutbox(ctx, q, "gate.pending", string(wtype), workItemID,
				map[string]any{"node": nodeID}); err != nil {
				return err
			}
		}
		if err := s.syncIterationStatus(ctx, q, wtype, workItemID, li.Inst); err != nil {
			return err
		}
		return s.fillNodeOpResult(ctx, q, li, nodeID, &out)
	})
	return out, err
}

// AttachArtifact 单独挂接材料：提交前补充或 awaiting_review 期间补挂；
// 替换已进入评审的材料会作废该节点既有审批（workflow.md「材料变更」）。
func (s *Service) AttachArtifact(ctx context.Context, actor Actor, wtype workitem.Scope, workItemID, nodeID string, in ArtifactInput) (NodeOpResult, error) {
	var out NodeOpResult
	err := s.DB.WithTx(ctx, func(q store.Querier) error {
		if _, _, err := s.stepPermission(ctx, q, actor, wtype, workItemID); err != nil {
			return err
		}
		li, err := s.loadInstance(ctx, q, wtype, workItemID)
		if err != nil {
			return err
		}
		st, err := li.Inst.State(nodeID)
		if err != nil {
			return MapError(err)
		}
		if !st.Status.Actionable() {
			return MapError(workflow.ErrInvalidTransition)
		}

		ref, err := in.toRef(nodeID, actor.UserID)
		if err != nil {
			return err
		}
		// 已进入评审的材料被替换 → 作废审批；提交前替换不涉及既有审批。
		reset := st.Status == workitem.StatusAwaitingReview
		existing := li.Inputs[nodeID].Artifacts
		for _, e := range existing {
			if e.Kind == ref.Kind && artifactDiffers(e, ref) {
				reset = true
			}
		}
		row := store.ArtifactRow{ID: s.newID("art"), InstanceID: li.Row.ID, Ref: ref}
		if err := store.ReplaceArtifact(ctx, q, row, reset); err != nil {
			return err
		}
		if err := audit(ctx, q, actor, "workflow.attach_artifact", string(wtype), workItemID,
			map[string]any{"node": nodeID, "kind": in.Kind, "provider": in.Provider, "replaced": reset}); err != nil {
			return err
		}
		// 重新加载以反映新材料。
		li, err = s.loadInstance(ctx, q, wtype, workItemID)
		if err != nil {
			return err
		}
		return s.fillNodeOpResult(ctx, q, li, nodeID, &out)
	})
	return out, err
}

// artifactDiffers 判定材料是否实质变更（URL/外部ID/标题/内容）。
func artifactDiffers(a, b workitem.ArtifactRef) bool {
	return a.URL != b.URL || a.ExternalID != b.ExternalID ||
		a.Title != b.Title || a.Content != b.Content
}

func hasKind(refs []workitem.ArtifactRef, kind workitem.ArtifactKind) bool {
	for _, r := range refs {
		if r.Kind == kind {
			return true
		}
	}
	return false
}

// ReviewStep 门禁确认/驳回：产品内持 gate.roles 之一的非 guest 成员（admin 恒可）；
// 提交人本人为合格评审人时允许自审（不变量 8），同样走留痕流程。
func (s *Service) ReviewStep(ctx context.Context, actor Actor, wtype workitem.Scope, workItemID, nodeID string,
	decision workitem.Decision, comment string, expectedVersion int64) (NodeOpResult, error) {
	var out NodeOpResult
	err := s.DB.WithTx(ctx, func(q store.Querier) error {
		mc, _, err := s.stepPermission(ctx, q, actor, wtype, workItemID)
		if err != nil {
			return err
		}
		_ = mc

		li, err := s.loadInstance(ctx, q, wtype, workItemID)
		if err != nil {
			return err
		}
		node, ok := li.Inst.Definition.Node(nodeID)
		if !ok {
			return MapError(workflow.ErrNodeNotFound)
		}
		if node.Gate == nil {
			return MapError(workflow.ErrInvalidTransition)
		}
		// 资格判定（服务端，AGENT 红线 2）：全局角色 + 产品非 guest 成员。
		if !actor.Roles.HasAny(node.Gate.Roles...) {
			return RoleRequired("需要角色 %v", roleNames(node.Gate.Roles))
		}
		if !mc.IsAdmin {
			switch mc.Role {
			case "":
				return NotFound("工作项不存在或不可见")
			case product.MemberGuest:
				return RoleRequired("guest 不可审批（不变量 16）")
			}
		}

		d := workitem.ReviewDecision{
			NodeID: nodeID, ReviewerID: actor.UserID, Decision: decision, Comment: comment,
			Roles: actor.Roles, CreatedAt: s.Now(),
		}
		if d.Decision == workitem.DecisionReject && comment == "" {
			return Invalid("驳回必须填写理由")
		}
		in := li.Inputs[nodeID]
		res, _, err := li.Inst.Review(nodeID, expectedVersion, d, in.Decisions, in, s.Now())
		if err != nil {
			return MapError(err)
		}

		if err := store.UpsertApproval(ctx, q, li.Row.ID, d); err != nil {
			return err
		}
		if err := store.SaveNodeStates(ctx, q, li.Row.ID, li.Inst); err != nil {
			return err
		}
		if err := audit(ctx, q, actor, "gate.review", string(wtype), workItemID,
			map[string]any{"node": nodeID, "decision": string(decision), "comment": comment}); err != nil {
			return err
		}
		st, _ := li.Inst.State(nodeID)
		switch {
		case st.Status == workitem.StatusRejected:
			_ = store.InsertOutbox(ctx, q, "node.rejected", string(wtype), workItemID,
				map[string]any{"node": nodeID, "reviewer": actor.UserID})
		case st.Status == workitem.StatusPassed:
			_ = store.InsertOutbox(ctx, q, "node.passed", string(wtype), workItemID,
				map[string]any{"node": nodeID, "reviewer": actor.UserID})
		}
		if err := s.syncIterationStatus(ctx, q, wtype, workItemID, li.Inst); err != nil {
			return err
		}
		return s.fillNodeOpResultWithGate(ctx, q, li, nodeID, res, &out)
	})
	return out, err
}

func productGuestOnly(mc memberContext) productGuestRole { return "" }

type productGuestRole string

// roleNames 角色串。
func roleNames(roles []identity.Role) []string {
	out := make([]string, len(roles))
	for i, r := range roles {
		out[i] = string(r)
	}
	return out
}

// stepPermission 统一节点写权限：任务=负责人/owner/pm/admin；迭代=成员+pm/owner/admin。
func (s *Service) stepPermission(ctx context.Context, q store.Querier, actor Actor, wtype workitem.Scope, workItemID string) (memberContext, string, error) {
	switch wtype {
	case workitem.ScopeTask:
		t, err := store.TaskByID(ctx, q, workItemID)
		if err != nil {
			return memberContext{}, "", wrapNotFound(err, "任务不存在或不可见")
		}
		mc, err := s.mustView(ctx, q, actor, t.ProductID)
		if err != nil {
			return memberContext{}, "", err
		}
		if err := canWorkTask(mc, actor, t.AssigneeID); err != nil {
			return memberContext{}, "", err
		}
		return mc, t.AssigneeID, nil
	case workitem.ScopeIteration:
		it, err := store.IterationByID(ctx, q, workItemID)
		if err != nil {
			return memberContext{}, "", wrapNotFound(err, "迭代不存在或不可见")
		}
		mc, err := s.mustView(ctx, q, actor, it.ProductID)
		if err != nil {
			return memberContext{}, "", err
		}
		if err := canCreateInProduct(mc, actor); err != nil {
			return memberContext{}, "", err
		}
		return mc, "", nil
	}
	return memberContext{}, "", Invalid("工作项类型非法")
}

// fillNodeOpResult 组装操作返回（无门禁裁决时）。
func (s *Service) fillNodeOpResult(ctx context.Context, q store.Querier, li loadedInstance, nodeID string, out *NodeOpResult) error {
	return s.fillNodeOpResultWithGate(ctx, q, li, nodeID, workflow.GateResult{}, out)
}

func (s *Service) fillNodeOpResultWithGate(ctx context.Context, q store.Querier, li loadedInstance, nodeID string, res workflow.GateResult, out *NodeOpResult) error {
	st, err := li.Inst.State(nodeID)
	if err != nil {
		return MapError(err)
	}
	node, _ := li.Inst.Definition.Node(nodeID)
	nv := NodeStateView{ID: nodeID, Name: node.Name, Status: string(st.Status), Version: st.Version}
	if !st.EnteredAt.IsZero() {
		nv.EnteredAt = st.EnteredAt.UTC().Format(time.RFC3339)
	}
	if !st.CompletedAt.IsZero() {
		nv.CompletedAt = st.CompletedAt.UTC().Format(time.RFC3339)
	}
	out.Node = nv

	iv, err := s.instanceView(ctx, q, li.Row)
	if err != nil {
		return err
	}
	out.Instance = iv

	// 门禁视图（含合格评审人）。
	node2, _ := li.Inst.Definition.Node(nodeID)
	if node2.Gate != nil {
		productID, err := s.productOf(ctx, q, li.Row.WorkItemType, li.Row.WorkItemID)
		if err == nil {
			candidates, err := store.ListEligibleReviewers(ctx, q, productID, roleNames(node2.Gate.Roles))
			if err == nil {
				ids := make([]string, 0, len(candidates))
				for _, c := range candidates {
					ids = append(ids, c.UserID)
				}
				out.Gate = &GateResultView{
					Passed: res.Passed, Approvals: res.Approvals, ApprovalsNeeded: res.ApprovalsNeeded,
					EligibleReviewers: ids,
				}
				for _, m := range res.MissingArtifacts {
					out.Gate.MissingArtifacts = append(out.Gate.MissingArtifacts, string(m))
				}
				out.Gate.PendingRejects = res.PendingRejects
				out.Gate.ChildrenBlocked = res.ChildrenBlocked
				out.Gate.IterationBlocked = res.IterationBlocked
			}
		}
	}
	return nil
}

// productOf 取工作项所属产品。
func (s *Service) productOf(ctx context.Context, q store.Querier, wtype workitem.Scope, workItemID string) (string, error) {
	switch wtype {
	case workitem.ScopeTask:
		t, err := store.TaskByID(ctx, q, workItemID)
		if err != nil {
			return "", err
		}
		return t.ProductID, nil
	case workitem.ScopeIteration:
		it, err := store.IterationByID(ctx, q, workItemID)
		if err != nil {
			return "", err
		}
		return it.ProductID, nil
	}
	return "", Invalid("工作项类型非法")
}

// syncIterationStatus 迭代状态投影：首个节点推进 → active；终态 → closed / cancelled。
func (s *Service) syncIterationStatus(ctx context.Context, q store.Querier, wtype workitem.Scope, workItemID string, inst *workflow.Instance) error {
	if wtype != workitem.ScopeIteration {
		return nil
	}
	it, err := store.IterationByID(ctx, q, workItemID)
	if err != nil {
		return err
	}
	dom := iterationFromRow(it)

	if outcome, ok := inst.Terminal(); ok {
		var next planning.IterationStatus
		switch outcome {
		case workitem.OutcomeDone:
			if dom.Status == planning.IterationPlanned {
				if err := dom.Activate(); err != nil {
					return MapError(err)
				}
			}
			if err := dom.Close(false); err != nil {
				return MapError(err)
			}
			next = dom.Status
		case workitem.OutcomeCancelled:
			if err := dom.Cancel(false); err != nil {
				return MapError(err)
			}
			next = dom.Status
		default:
			return nil
		}
		if err := store.UpdateIterationStatus(ctx, q, workItemID, next); err != nil {
			return err
		}
		return store.InsertOutbox(ctx, q, "iteration.closed", "iteration", workItemID,
			map[string]any{"status": string(next), "number": it.Number})
	}

	// 首次推进 → active（迭代当前节点即阶段，状态是节点投影）。
	if dom.Status == planning.IterationPlanned {
		for _, st := range inst.Nodes {
			if st.Status == workitem.StatusInProgress || st.Status == workitem.StatusPassed ||
				st.Status == workitem.StatusAwaitingReview || st.Status == workitem.StatusRejected {
				if err := dom.Activate(); err != nil {
					return MapError(err)
				}
				if err := store.UpdateIterationStatus(ctx, q, workItemID, dom.Status); err != nil {
					return err
				}
				return nil
			}
		}
	}
	return nil
}

// ---- 查询投影 ----

// GetNextAction 结构化下一步（mcp.md 契约）：服务端事实，Agent 不得跳过 blockers。
func (s *Service) GetNextAction(ctx context.Context, actor Actor, wtype workitem.Scope, workItemID string) (workflow.NextActionReport, error) {
	q := s.DB.q()
	// 可见性。
	productID, err := s.productOf(ctx, q, wtype, workItemID)
	if err != nil {
		return workflow.NextActionReport{}, wrapNotFound(err, "工作项不存在或不可见")
	}
	mc, err := s.mustView(ctx, q, actor, productID)
	if err != nil {
		return workflow.NextActionReport{}, err
	}

	li, err := s.loadInstance(ctx, q, wtype, workItemID)
	if err != nil {
		return workflow.NextActionReport{}, err
	}

	// 工作项标识。
	ref := workflow.WorkItemRef{Type: string(wtype), ID: workItemID}
	switch wtype {
	case workitem.ScopeTask:
		t, err := store.TaskByID(ctx, q, workItemID)
		if err != nil {
			return workflow.NextActionReport{}, wrapNotFound(err, "任务不存在或不可见")
		}
		ref.Title = t.Number + " " + t.Title
	case workitem.ScopeIteration:
		it, err := store.IterationByID(ctx, q, workItemID)
		if err != nil {
			return workflow.NextActionReport{}, wrapNotFound(err, "迭代不存在或不可见")
		}
		ref.Title = it.Number + " " + it.Name
	}

	// 收口输入。
	inputs := map[string]workflow.NodeInput{}
	for nodeID, in := range li.Inputs {
		if wtype == workitem.ScopeTask {
			open, err := store.CountUnfinishedChildren(ctx, q, workItemID)
			if err != nil {
				return workflow.NextActionReport{}, err
			}
			in.ChildrenDone = open == 0
		}
		if wtype == workitem.ScopeIteration {
			open, err := store.CountUnfinishedTasksInIteration(ctx, q, workItemID)
			if err != nil {
				return workflow.NextActionReport{}, err
			}
			in.IterationTasksDone = open == 0
		}
		inputs[nodeID] = in
	}
	// 模板中存在但尚无输入的节点也要有默认输入（Evaluate 需零值）。
	for _, n := range li.Inst.Definition.Nodes {
		if _, ok := inputs[n.ID]; !ok {
			inputs[n.ID] = workflow.NodeInput{}
		}
	}

	report, err := workflow.NextAction(li.Inst, workflow.NextActionInputs{
		WorkItem: ref, Inputs: inputs, EligibleReviewers: map[string][]string{},
	})
	if err != nil {
		return report, MapError(err)
	}

	// 权限过滤（mcp.md：对无权执行的成员不返回该工具）。
	if !s.canOperateWorkItem(ctx, q, actor, mc, wtype, workItemID) {
		report.NextActions = nil
		report.AllowedActions = nil
		switch report.Status {
		case "done":
		case "awaiting_review":
		default:
			report.Status = "in_progress"
		}
	}
	return report, nil
}

// canOperateWorkItem 判定 actor 能否推进该工作项（用于 next_action 过滤）。
func (s *Service) canOperateWorkItem(ctx context.Context, q store.Querier, actor Actor, mc memberContext, wtype workitem.Scope, workItemID string) bool {
	switch wtype {
	case workitem.ScopeTask:
		t, err := store.TaskByID(ctx, q, workItemID)
		if err != nil {
			return false
		}
		return canWorkTask(mc, actor, t.AssigneeID) == nil
	case workitem.ScopeIteration:
		return canCreateInProduct(mc, actor) == nil
	}
	return false
}

// PendingReviewView 是待我审批的门禁条目。
type PendingReviewView struct {
	WorkItemType string   `json:"workItemType"`
	WorkItemID   string   `json:"workItemId"`
	Title        string    `json:"title"`
	NodeID       string   `json:"nodeId"`
	NodeName     string   `json:"nodeName"`
	ApprovalsNeeded int   `json:"approvalsNeeded"`
	CurrentApprovals int  `json:"currentApprovals"`
	Roles        []string `json:"roles"`
}

// ListPendingReviews 我是评审人、待我确认的门禁（chalkline_get_pending_reviews）。
func (s *Service) ListPendingReviews(ctx context.Context, actor Actor) ([]PendingReviewView, error) {
	q := s.DB.q()
	productIDs, err := store.ProductIDsOfMember(ctx, q, actor.UserID)
	if err != nil {
		return nil, err
	}
	if actor.IsAdmin {
		products, err := store.ListVisibleProducts(ctx, q, actor.UserID, true)
		if err != nil {
			return nil, err
		}
		productIDs = make([]string, 0, len(products))
		for _, p := range products {
			productIDs = append(productIDs, p.ID)
		}
	}
	awaiting, err := store.ListAwaitingNodes(ctx, q, productIDs)
	if err != nil {
		return nil, err
	}
	out := []PendingReviewView{}
	for _, a := range awaiting {
		instRow, err := store.InstanceByWorkItem(ctx, q, a.WorkItemType, a.WorkItemID)
		if err != nil {
			continue
		}
		inst, err := store.RowsToInstance(instRow, mustStates(ctx, q, instRow.ID))
		if err != nil {
			continue
		}
		node, ok := inst.Definition.Node(a.NodeID)
		if !ok || node.Gate == nil {
			continue
		}
		if !actor.Roles.HasAny(node.Gate.Roles...) {
			continue // 资格过滤在服务端
		}
		title := a.WorkItemID
		switch a.WorkItemType {
		case workitem.ScopeTask:
			if t, err := store.TaskByID(ctx, q, a.WorkItemID); err == nil {
				title = t.Number + " " + t.Title
			}
		case workitem.ScopeIteration:
			if it, err := store.IterationByID(ctx, q, a.WorkItemID); err == nil {
				title = it.Number + " " + it.Name
			}
		}
		approvals, err := store.ListApprovals(ctx, q, a.InstanceID)
		if err != nil {
			continue
		}
		current := 0
		latest := map[string]workitem.Decision{}
		for _, d := range approvals {
			if d.NodeID == a.NodeID {
				latest[d.ReviewerID] = d.Decision
			}
		}
		for _, dec := range latest {
			if dec == workitem.DecisionApprove {
				current++
			}
		}
		out = append(out, PendingReviewView{
			WorkItemType: string(a.WorkItemType), WorkItemID: a.WorkItemID, Title: title,
			NodeID: a.NodeID, NodeName: node.Name,
			ApprovalsNeeded: node.Gate.Approvals, CurrentApprovals: current,
			Roles: roleNames(node.Gate.Roles),
		})
	}
	return out, nil
}

func mustStates(ctx context.Context, q store.Querier, instanceID string) []workflow.NodeState {
	states, err := store.ListNodeStates(ctx, q, instanceID)
	if err != nil {
		return nil
	}
	return states
}

// GetInstanceViewByWorkItem REST/MCP 共用的实例状态查询。
func (s *Service) GetInstanceViewByWorkItem(ctx context.Context, actor Actor, wtype workitem.Scope, workItemID string) (InstanceView, error) {
	q := s.DB.q()
	productID, err := s.productOf(ctx, q, wtype, workItemID)
	if err != nil {
		return InstanceView{}, wrapNotFound(err, "工作项不存在或不可见")
	}
	if _, err := s.mustView(ctx, q, actor, productID); err != nil {
		return InstanceView{}, err
	}
	instRow, err := store.InstanceByWorkItem(ctx, q, wtype, workItemID)
	if err != nil {
		return InstanceView{}, wrapNotFound(err, "实例不存在")
	}
	return s.instanceView(ctx, q, instRow)
}
