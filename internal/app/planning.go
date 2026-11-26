package app

// planning.go：迭代与任务用例（docs/modules/planning.md）。
// 任务状态是工作流终态投影（不落 status 列）；编号经 counters 原子分配。

import (
	"context"
	"encoding/json"
	"time"

	"github.com/yuylong/Chalkline/internal/domain/planning"
	"github.com/yuylong/Chalkline/internal/domain/workitem"
	"github.com/yuylong/Chalkline/internal/store"
	"github.com/yuylong/Chalkline/internal/workflow"
)

// IterationView 是迭代视图。
type IterationView struct {
	ID         string `json:"id"`
	ProductID  string `json:"productId"`
	Name       string `json:"name"`
	Goal       string `json:"goal"`
	Number     string `json:"number"`
	StartsOn   string `json:"startsOn"`
	EndsOn     string `json:"endsOn"`
	Status     string `json:"status"`
	WorkflowID string `json:"workflowId"`
	CreatedBy  string `json:"createdBy"`
}

func viewIteration(it store.IterationRow) IterationView {
	return IterationView{
		ID: it.ID, ProductID: it.ProductID, Name: it.Name, Goal: it.Goal,
		Number: it.Number, StartsOn: it.StartsOn.Format("2006-01-02"),
		EndsOn: it.EndsOn.Format("2006-01-02"), Status: string(it.Status),
		WorkflowID: it.WorkflowID, CreatedBy: it.CreatedBy,
	}
}

// CreateIterationInput 建迭代入参。
type CreateIterationInput struct {
	Name       string
	Goal       string
	StartsOn   time.Time
	EndsOn     time.Time
	WorkflowID string // 可选，缺省取产品默认生命周期模板
	// Providers 是迭代级 provider 绑定：成果物必选且唯一，流程记录至多一个，代码库可多个。
	Providers []IterationProviderInput
}

// CreateIteration 建迭代：产品成员持 pm 或 admin；分配 CHK-IT<n>；
// 实例化生命周期模板（起点 ready），状态 planned。
func (s *Service) CreateIteration(ctx context.Context, actor Actor, productID string, in CreateIterationInput) (IterationView, error) {
	var out IterationView
	err := s.DB.WithTx(ctx, func(q store.Querier) error {
		p, err := store.ProductByID(ctx, q, productID)
		if err != nil {
			return wrapNotFound(err, "产品不存在或不可见")
		}
		mc, err := s.mustView(ctx, q, actor, productID)
		if err != nil {
			return err
		}
		if err := canCreateInProduct(mc, actor); err != nil {
			return err
		}
		if p.Status != "active" {
			return Invalid("产品已归档，不可再建迭代")
		}

		wfID := in.WorkflowID
		if wfID == "" {
			wfID = p.DefaultIterationWorkflowID
		}
		wf, err := store.WorkflowByID(ctx, q, wfID)
		if err != nil {
			return wrapNotFound(err, "生命周期模板不存在")
		}
		if wf.Scope != workitem.ScopeIteration {
			return Invalid("模板 %s 不是 iteration 作用域", wfID)
		}
		if wf.Status == "archived" {
			return Invalid("模板 %s 已归档", wfID)
		}
		def, err := workflow.ParseJSON(wf.Definition)
		if err != nil {
			return MapError(err)
		}

		seq, err := store.NextCounter(ctx, q, "product:"+productID+":iteration")
		if err != nil {
			return err
		}
		number := planning.FormatIterationNumber(p.Key, int(seq))
		now := s.Now()
		dom, err := planning.NewIteration(s.newID("itr"), productID, in.Name, in.Goal,
			number, in.StartsOn, in.EndsOn, wfID)
		if err != nil {
			return MapError(err)
		}
		row := store.IterationRow{
			ID: dom.ID, ProductID: dom.ProductID, Name: dom.Name, Goal: dom.Goal,
			Number: string(dom.Number), StartsOn: dom.StartsOn, EndsOn: dom.EndsOn,
			Status: dom.Status, WorkflowID: wfID, CreatedBy: actor.UserID,
		}
		if err := store.InsertIteration(ctx, q, row); err != nil {
			if store.IsDuplicate(err) {
				return Invalid("迭代编号 %s 已存在", number)
			}
			return err
		}

		inst, err := workflow.NewInstance(def, now)
		if err != nil {
			return MapError(err)
		}
		instID := s.newID("wfi")
		if err := store.InsertInstance(ctx, q, store.InstanceRow{
			ID: instID, WorkflowID: def.ID, WorkflowVersion: def.Version,
			WorkItemType: workitem.ScopeIteration, WorkItemID: dom.ID,
			DefinitionJSON: wf.Definition,
		}, inst); err != nil {
			return err
		}
		if err := store.SetIterationInstance(ctx, q, dom.ID, instID); err != nil {
			return err
		}

		// 迭代级 provider 绑定：成果物必选（inline 亦需显式选择），随迭代固化连接信息。
		if err := s.insertIterationBindings(ctx, q, actor, dom.ID, in.Providers); err != nil {
			return err
		}

		if err := audit(ctx, q, actor, "iteration.create", "iteration", dom.ID,
			map[string]any{"number": number, "workflowId": wfID, "providers": len(in.Providers)}); err != nil {
			return err
		}
		out = viewIteration(row)
		return nil
	})
	return out, err
}

// ListIterations 产品迭代列表：产品成员或 admin。
func (s *Service) ListIterations(ctx context.Context, actor Actor, productID string, status *planning.IterationStatus) ([]IterationView, error) {
	q := s.DB.Q()
	if _, err := s.mustView(ctx, q, actor, productID); err != nil {
		return nil, err
	}
	rows, err := store.ListIterationsByProduct(ctx, q, productID, status)
	if err != nil {
		return nil, err
	}
	out := make([]IterationView, 0, len(rows))
	for _, it := range rows {
		out = append(out, viewIteration(it))
	}
	return out, nil
}

// IterationDetail 是迭代详情：当前阶段 + 进度 + provider 绑定。
type IterationDetail struct {
	IterationView
	CurrentNodes []NodeStateView        `json:"currentNodes"`
	ProgressPct  int                    `json:"progressPct"` // passed / 可达节点
	OpenTasks    int64                  `json:"openTasks"`
	WorkflowName string                 `json:"workflowName"`
	Providers    IterationProvidersView `json:"providers"`
}

// NodeStateView 是节点状态视图（含乐观锁版本）。
type NodeStateView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Version     int64  `json:"version"`
	EnteredAt   string `json:"enteredAt,omitempty"`
	CompletedAt string `json:"completedAt,omitempty"`
}

// GetIteration 迭代详情：成员或 admin。
func (s *Service) GetIteration(ctx context.Context, actor Actor, id string) (IterationDetail, error) {
	q := s.DB.Q()
	it, err := store.IterationByID(ctx, q, id)
	if err != nil {
		return IterationDetail{}, wrapNotFound(err, "迭代不存在或不可见")
	}
	if _, err := s.mustView(ctx, q, actor, it.ProductID); err != nil {
		return IterationDetail{}, err
	}
	detail := IterationDetail{IterationView: viewIteration(it)}
	providers, err := s.iterationProvidersView(ctx, q, it)
	if err != nil {
		return detail, err
	}
	detail.Providers = providers
	open, err := store.CountUnfinishedTasksInIteration(ctx, q, id)
	if err != nil {
		return detail, err
	}
	detail.OpenTasks = open
	instRow, err := store.InstanceByWorkItem(ctx, q, workitem.ScopeIteration, id)
	if err != nil {
		return detail, nil // 无实例（理论不发生）时返回基础字段
	}
	detail.WorkflowName = instRow.WorkflowID
	states, err := store.ListNodeStates(ctx, q, instRow.ID)
	if err != nil {
		return detail, err
	}
	inst, err := store.RowsToInstance(instRow, states)
	if err != nil {
		return detail, err
	}
	detail.CurrentNodes = currentNodesView(inst)
	passed, total := inst.Progress()
	if total > 0 {
		detail.ProgressPct = passed * 100 / total
	}
	return detail, nil
}

// currentNodesView 把实例「当前节点」投影为视图（含版本，供并发操作回传）。
func currentNodesView(inst *workflow.Instance) []NodeStateView {
	out := []NodeStateView{}
	for _, id := range inst.CurrentNodes() {
		st, _ := inst.State(id)
		node, _ := inst.Definition.Node(id)
		v := NodeStateView{ID: id, Name: node.Name, Status: string(st.Status), Version: st.Version}
		if !st.EnteredAt.IsZero() {
			v.EnteredAt = st.EnteredAt.UTC().Format(time.RFC3339)
		}
		if !st.CompletedAt.IsZero() {
			v.CompletedAt = st.CompletedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, v)
	}
	return out
}

// TaskView 是任务视图（status 为终态投影）。
type TaskView struct {
	ID          string `json:"id"`
	ProductID   string `json:"productId"`
	IterationID string `json:"iterationId"`
	ParentID    string `json:"parentId,omitempty"`
	Number      string `json:"number"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	AssigneeID  string `json:"assigneeId,omitempty"`
	Priority    string `json:"priority"`
	Status      string `json:"status"`
	WorkflowID  string `json:"workflowId"`
}

func (s *Service) viewTask(ctx context.Context, q store.Querier, t store.TaskRow) TaskView {
	v := TaskView{
		ID: t.ID, ProductID: t.ProductID, IterationID: t.IterationID,
		Number: t.Number, Title: t.Title, Description: t.Description,
		Kind: string(t.Kind), AssigneeID: t.AssigneeID, Priority: string(t.Priority),
		Status: string(planning.TaskOpen), WorkflowID: t.WorkflowID,
	}
	if t.ParentID != nil {
		v.ParentID = *t.ParentID
	}
	// 状态投影：读实例终态（tasks 不存 status 列）。
	instRow, err := store.InstanceByWorkItem(ctx, q, workitem.ScopeTask, t.ID)
	if err != nil {
		return v // 无实例即 open
	}
	v.Status = string(taskStatusOf(instRow.TerminalNodeID, instRow.DefinitionJSON))
	return v
}

// taskStatusOf 由终态节点 + 模板快照推导任务状态（纯函数）。
func taskStatusOf(terminalNodeID string, defJSON []byte) planning.TaskStatus {
	if terminalNodeID == "" {
		return planning.TaskOpen
	}
	def, err := workflow.ParseJSON(defJSON)
	if err != nil {
		return planning.TaskOpen
	}
	node, ok := def.Node(terminalNodeID)
	if !ok {
		return planning.TaskOpen
	}
	if node.Outcome == workitem.OutcomeCancelled {
		return planning.TaskCancelled
	}
	return planning.TaskDone
}

// CreateTaskInput 建任务入参（ParentTaskID 非空即拆解）。
type CreateTaskInput struct {
	IterationID  string
	ParentTaskID string
	Title        string
	Description  string
	Kind         planning.TaskKind
	Priority     planning.Priority
	AssigneeID   string // 可选
	ModuleIDs    []string
	WorkflowID   string // 可选；子任务默认继承父任务模板
}

// CreateTask 建任务：校验当前迭代阶段允许该类型（不变量 5，allows_task_kinds 白名单）、
// 编号按父级维度原子分配、同产品同迭代（不变量 2）、实例化任务流。
func (s *Service) CreateTask(ctx context.Context, actor Actor, in CreateTaskInput) (TaskView, error) {
	var out TaskView
	err := s.DB.WithTx(ctx, func(q store.Querier) error {
		v, err := s.createTaskInTx(ctx, q, actor, in)
		if err != nil {
			return err
		}
		out = v
		return nil
	})
	if err == nil {
		// 流程记录：每个任务在绑定的 project 建票（首次动作时惰性建票）。
		s.recordProcess(taskCreateProcessAction(actor, out.ID, out.Number, out.Title))
	}
	return out, err
}

func (s *Service) createTaskInTx(ctx context.Context, q store.Querier, actor Actor, in CreateTaskInput) (TaskView, error) {
	it, err := store.IterationByID(ctx, q, in.IterationID)
	if err != nil {
		return TaskView{}, wrapNotFound(err, "迭代不存在或不可见")
	}
	mc, err := s.mustView(ctx, q, actor, it.ProductID)
	if err != nil {
		return TaskView{}, err
	}

	// 权限：顶层建任务 = 建权限；拆子任务 = 父任务负责人 / owner / pm / admin。
	var parent *store.TaskRow
	if in.ParentTaskID != "" {
		p, err := store.TaskByID(ctx, q, in.ParentTaskID)
		if err != nil {
			return TaskView{}, wrapNotFound(err, "父任务不存在或不可见")
		}
		if p.ProductID != it.ProductID || p.IterationID != it.ID {
			return TaskView{}, Invalid("父任务与迭代不一致（不变量 2：子任务须与父任务同产品同迭代）")
		}
		if err := canWorkTask(mc, actor, p.AssigneeID); err != nil {
			return TaskView{}, err
		}
		parent = &p
	} else {
		if err := canCreateInProduct(mc, actor); err != nil {
			return TaskView{}, err
		}
	}

	// 类型缺省 dev；阶段校验（不变量 5）按类型白名单判定。
	kind := in.Kind
	if kind == "" {
		kind = planning.KindDev
	}
	if err := s.ensureIterationAllowsKind(ctx, q, it, kind); err != nil {
		return TaskView{}, err
	}

	p, err := store.ProductByID(ctx, q, it.ProductID)
	if err != nil {
		return TaskView{}, err
	}

	// 编号：顶层按产品取号，子任务按父任务取号（原子，永不复用）。
	var number planning.TaskNumber
	if parent == nil {
		seq, err := store.NextCounter(ctx, q, "product:"+it.ProductID+":task")
		if err != nil {
			return TaskView{}, err
		}
		number, err = planning.FormatTaskNumber(p.Key, int(seq))
		if err != nil {
			return TaskView{}, MapError(err)
		}
	} else {
		seq, err := store.NextCounter(ctx, q, "task:"+parent.ID+":child")
		if err != nil {
			return TaskView{}, err
		}
		parentNum := planning.TaskNumber(parent.Number)
		_, seqs, err := parentNum.Parse()
		if err != nil {
			return TaskView{}, MapError(err)
		}
		number, err = planning.FormatTaskNumber(p.Key, append(seqs, int(seq))...)
		if err != nil {
			return TaskView{}, MapError(err)
		}
	}

	// 任务流模板：显式指定 > 继承父任务 > 产品按类型默认 > 产品默认。
	wfID := in.WorkflowID
	if wfID == "" && parent != nil {
		wfID = parent.WorkflowID
	}
	if wfID == "" {
		wfID = p.TaskWorkflowDefaults[string(kind)]
	}
	if wfID == "" {
		wfID = p.DefaultTaskWorkflowID
	}
	wf, err := store.WorkflowByID(ctx, q, wfID)
	if err != nil {
		return TaskView{}, wrapNotFound(err, "任务流模板不存在")
	}
	if wf.Scope != workitem.ScopeTask {
		return TaskView{}, Invalid("模板 %s 不是 task 作用域", wfID)
	}
	if wf.Status == "archived" {
		return TaskView{}, Invalid("模板 %s 已归档", wfID)
	}
	def, err := workflow.ParseJSON(wf.Definition)
	if err != nil {
		return TaskView{}, MapError(err)
	}

	// 领域构造（含拆解不变量：同产品同迭代 / 深度 ≤ 3 / 编号延伸 / bug 不可拆）。
	var parentDom *planning.Task
	if parent != nil {
		pd := parentTaskDomain(*parent)
		parentDom = &pd
	}
	priority := in.Priority
	if priority == "" {
		priority = planning.PriorityNormal
	}
	dom, err := planning.NewTask(s.newID("tsk"), it.ProductID, it.ID, parentDom,
		number, in.Title, kind, priority, wfID)
	if err != nil {
		return TaskView{}, MapError(err)
	}

	row := store.TaskRow{
		ID: dom.ID, ProductID: dom.ProductID, IterationID: dom.IterationID,
		ParentID: nil, Number: string(dom.Number), Title: dom.Title, Description: in.Description,
		Kind: dom.Kind, AssigneeID: in.AssigneeID, Priority: dom.Priority,
		WorkflowID: wfID, CreatedBy: actor.UserID,
	}
	if parent != nil {
		pid := parent.ID
		row.ParentID = &pid
	}
	if err := store.InsertTask(ctx, q, row); err != nil {
		if store.IsDuplicate(err) {
			return TaskView{}, Invalid("任务编号 %s 已存在", number)
		}
		return TaskView{}, err
	}

	// 实例化任务流。
	inst, err := workflow.NewInstance(def, s.Now())
	if err != nil {
		return TaskView{}, MapError(err)
	}
	instID := s.newID("wfi")
	if err := store.InsertInstance(ctx, q, store.InstanceRow{
		ID: instID, WorkflowID: def.ID, WorkflowVersion: def.Version,
		WorkItemType: workitem.ScopeTask, WorkItemID: dom.ID,
		DefinitionJSON: wf.Definition,
	}, inst); err != nil {
		return TaskView{}, err
	}
	if err := store.SetTaskInstance(ctx, q, dom.ID, instID); err != nil {
		return TaskView{}, err
	}

	// 模块关联：子任务默认继承父任务模块（不变量 2：同产品）。
	moduleIDs := in.ModuleIDs
	if parent != nil && len(moduleIDs) == 0 {
		parentMods, err := store.ListTaskModules(ctx, q, parent.ID)
		if err != nil {
			return TaskView{}, err
		}
		for _, m := range parentMods {
			moduleIDs = append(moduleIDs, m.ID)
		}
	}
	for _, mid := range moduleIDs {
		m, err := store.ModuleByID(ctx, q, mid)
		if err != nil {
			return TaskView{}, wrapNotFound(err, "模块不存在或不可见")
		}
		if err := planning.EnsureModuleSameProduct(it.ProductID, m.ProductID); err != nil {
			return TaskView{}, MapError(err)
		}
		if err := store.AddTaskModule(ctx, q, dom.ID, mid); err != nil {
			return TaskView{}, err
		}
	}

	// 指派人存在性。
	if in.AssigneeID != "" {
		if _, err := store.UserByID(ctx, q, in.AssigneeID); err != nil {
			return TaskView{}, wrapNotFound(err, "指派目标成员不存在")
		}
		if err := store.InsertOutbox(ctx, q, "task.assigned", "task", dom.ID,
			map[string]any{"assignee": in.AssigneeID, "number": string(number)}); err != nil {
			return TaskView{}, err
		}
	}

	if err := audit(ctx, q, actor, "task.create", "task", dom.ID,
		map[string]any{"number": string(number), "kind": string(kind), "parentId": in.ParentTaskID}); err != nil {
		return TaskView{}, err
	}
	return TaskView{
		ID: dom.ID, ProductID: dom.ProductID, IterationID: dom.IterationID,
		ParentID: in.ParentTaskID, Number: string(number), Title: dom.Title,
		Description: in.Description, Kind: string(kind), AssigneeID: in.AssigneeID,
		Priority: string(priority), Status: string(planning.TaskOpen), WorkflowID: wfID,
	}, nil
}

func parentTaskDomain(t store.TaskRow) planning.Task {
	dom := planning.Task{
		ID: t.ID, ProductID: t.ProductID, IterationID: t.IterationID,
		Number: planning.TaskNumber(t.Number), Kind: t.Kind, Priority: t.Priority,
		WorkflowID: t.WorkflowID,
	}
	if t.ParentID != nil {
		dom.ParentID = *t.ParentID
	}
	return dom
}

// ensureIterationAllowsKind 强制不变量 5：只在迭代当前节点
// allows_task_kinds 白名单（或 allows_task_creation 语法糖）包含该类型时建任务。
func (s *Service) ensureIterationAllowsKind(ctx context.Context, q store.Querier, it store.IterationRow, kind planning.TaskKind) error {
	instRow, err := store.InstanceByWorkItem(ctx, q, workitem.ScopeIteration, it.ID)
	if err != nil {
		return wrapNotFound(err, "迭代实例不存在")
	}
	states, err := store.ListNodeStates(ctx, q, instRow.ID)
	if err != nil {
		return err
	}
	inst, err := store.RowsToInstance(instRow, states)
	if err != nil {
		return err
	}
	for _, id := range inst.CurrentNodes() {
		if node, ok := inst.Definition.Node(id); ok && node.AllowsTaskKind(string(kind)) {
			return nil
		}
	}
	stage := "（无当前阶段）"
	if cur := inst.CurrentNodes(); len(cur) > 0 {
		if node, ok := inst.Definition.Node(cur[0]); ok {
			stage = node.Name
		}
	}
	return StageNotAllowedTask(stageNodeName(inst), stage, kind)
}

func stageNodeName(inst *workflow.Instance) string {
	if cur := inst.CurrentNodes(); len(cur) > 0 {
		if node, ok := inst.Definition.Node(cur[0]); ok {
			return node.ID
		}
	}
	return ""
}

// DecomposeTask 批量拆子任务（一个事务内逐个取号）。
func (s *Service) DecomposeTask(ctx context.Context, actor Actor, parentTaskID string, children []CreateTaskInput) ([]TaskView, error) {
	var out []TaskView
	err := s.DB.WithTx(ctx, func(q store.Querier) error {
		parent, err := store.TaskByID(ctx, q, parentTaskID)
		if err != nil {
			return wrapNotFound(err, "任务不存在或不可见")
		}
		if len(children) == 0 {
			return Invalid("至少提供一个子任务")
		}
		if len(children) > 50 {
			return Invalid("单次拆解上限 50 个子任务")
		}
		for i := range children {
			children[i].ParentTaskID = parentTaskID
			children[i].IterationID = parent.IterationID
			v, err := s.createTaskInTx(ctx, q, actor, children[i])
			if err != nil {
				return err
			}
			out = append(out, v)
		}
		return nil
	})
	return out, err
}

// TaskDetail 是任务详情：子树、模块、工作流状态。
type TaskDetail struct {
	TaskView
	Children []TaskView    `json:"children"`
	Modules  []ModuleView  `json:"modules"`
	Workflow *InstanceView `json:"workflow,omitempty"`
}

// InstanceView 是实例视图：模板 + 各节点状态 + 终态 + 材料/审批投影
// （console.md「任务详情：节点状态/材料/审批时间线」——M2 起随实例查询返回）。
type InstanceView struct {
	WorkflowID  string          `json:"workflowId"`
	Scope       string          `json:"scope"`
	Nodes       []NodeStateView `json:"nodes"`
	Terminal    string          `json:"terminal,omitempty"` // done / cancelled
	ProgressPct int             `json:"progressPct"`
	Artifacts   []ArtifactView  `json:"artifacts,omitempty"`
	Approvals   []ApprovalView  `json:"approvals,omitempty"`
}

// ArtifactView 材料投影：引用元数据，inline 附带内容；snapshot 为提交时点固化的快照（M3）。
type ArtifactView struct {
	Kind        string         `json:"kind"`
	Provider    string         `json:"provider"`
	URL         string         `json:"url,omitempty"`
	ExternalID  string         `json:"externalId,omitempty"`
	Title       string         `json:"title,omitempty"`
	Content     string         `json:"content,omitempty"`
	NodeID      string         `json:"nodeId"`
	SubmittedBy string         `json:"submittedBy"`
	Snapshot    map[string]any `json:"snapshot,omitempty"`
	// CreatedAt 提交时间（控制台判定「最新成果物」与历史倒序，RFC3339）。
	CreatedAt string `json:"createdAt,omitempty"`
}

// ApprovalView 审批决策投影（按时间升序即审批时间线）。
type ApprovalView struct {
	NodeID    string   `json:"nodeId"`
	Reviewer  string   `json:"reviewerId"`
	Decision  string   `json:"decision"`
	Comment   string   `json:"comment,omitempty"`
	Roles     []string `json:"roles,omitempty"`
	CreatedAt string   `json:"createdAt"`
}

// GetTask 任务详情：产品成员或 admin；含工作流状态与材料/审批（workflow 视图）。
func (s *Service) GetTask(ctx context.Context, actor Actor, id string) (TaskDetail, error) {
	q := s.DB.Q()
	t, err := store.TaskByID(ctx, q, id)
	if err != nil {
		return TaskDetail{}, wrapNotFound(err, "任务不存在或不可见")
	}
	if _, err := s.mustView(ctx, q, actor, t.ProductID); err != nil {
		return TaskDetail{}, err
	}
	detail := TaskDetail{TaskView: s.viewTask(ctx, q, t)}

	children, err := store.ListChildren(ctx, q, id)
	if err != nil {
		return detail, err
	}
	for _, c := range children {
		detail.Children = append(detail.Children, s.viewTask(ctx, q, c))
	}

	mods, err := store.ListTaskModules(ctx, q, id)
	if err != nil {
		return detail, err
	}
	for _, m := range mods {
		detail.Modules = append(detail.Modules, viewModule(m))
	}

	instRow, err := store.InstanceByWorkItem(ctx, q, workitem.ScopeTask, id)
	if err == nil {
		iv, err := s.instanceView(ctx, q, instRow)
		if err == nil {
			detail.Workflow = &iv
		}
	}
	return detail, nil
}

// instanceView 组装实例视图（全部节点状态 + 进度）。
func (s *Service) instanceView(ctx context.Context, q store.Querier, instRow store.InstanceRow) (InstanceView, error) {
	states, err := store.ListNodeStates(ctx, q, instRow.ID)
	if err != nil {
		return InstanceView{}, err
	}
	inst, err := store.RowsToInstance(instRow, states)
	if err != nil {
		return InstanceView{}, err
	}
	view := InstanceView{WorkflowID: instRow.WorkflowID, Scope: string(instRow.WorkItemType)}
	for _, n := range inst.Definition.Nodes {
		st, ok := inst.Nodes[n.ID]
		if !ok {
			continue
		}
		v := NodeStateView{ID: n.ID, Name: n.Name, Status: string(st.Status), Version: st.Version}
		if !st.EnteredAt.IsZero() {
			v.EnteredAt = st.EnteredAt.UTC().Format(time.RFC3339)
		}
		if !st.CompletedAt.IsZero() {
			v.CompletedAt = st.CompletedAt.UTC().Format(time.RFC3339)
		}
		view.Nodes = append(view.Nodes, v)
	}
	if outcome, ok := inst.Terminal(); ok {
		view.Terminal = string(outcome)
	}
	passed, total := inst.Progress()
	if total > 0 {
		view.ProgressPct = passed * 100 / total
	}

	// 材料/审批投影：控制台任务详情的审批时间线（M2 console.md 验收项）。
	artifacts, err := store.ListArtifacts(ctx, q, instRow.ID)
	if err != nil {
		return InstanceView{}, err
	}
	for _, a := range artifacts {
		av := ArtifactView{
			Kind: string(a.Ref.Kind), Provider: string(a.Ref.Provider),
			URL: a.Ref.URL, ExternalID: a.Ref.ExternalID, Title: a.Ref.Title,
			Content: a.Ref.Content, NodeID: a.Ref.NodeID, SubmittedBy: a.Ref.SubmittedBy,
		}
		if !a.CreatedAt.IsZero() {
			av.CreatedAt = a.CreatedAt.UTC().Format(time.RFC3339)
		}
		if len(a.Snapshot) > 0 {
			// 快照是提交时点固化的外部事实（M3），原样透传给控制台与 Agent。
			var snap map[string]any
			if json.Unmarshal(a.Snapshot, &snap) == nil {
				av.Snapshot = snap
			}
		}
		view.Artifacts = append(view.Artifacts, av)
	}
	approvals, err := store.ListApprovals(ctx, q, instRow.ID)
	if err != nil {
		return InstanceView{}, err
	}
	for _, d := range approvals {
		roles := make([]string, 0, len(d.Roles))
		for _, r := range d.Roles {
			roles = append(roles, string(r))
		}
		view.Approvals = append(view.Approvals, ApprovalView{
			NodeID: d.NodeID, Reviewer: d.ReviewerID, Decision: string(d.Decision),
			Comment: d.Comment, Roles: roles, CreatedAt: d.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return view, nil
}

// TaskFilter 是任务列表过滤。
type TaskFilter struct {
	ProductID   string
	IterationID string
	AssigneeID  string
	ModuleID    string
}

// ListTasks 任务列表：先过产品可见性，再按条件过滤。
func (s *Service) ListTasks(ctx context.Context, actor Actor, f TaskFilter) ([]TaskView, error) {
	q := s.DB.Q()
	productID := f.ProductID
	if productID == "" {
		// 由迭代或模块反推产品。
		switch {
		case f.IterationID != "":
			it, err := store.IterationByID(ctx, q, f.IterationID)
			if err != nil {
				return nil, wrapNotFound(err, "迭代不存在或不可见")
			}
			productID = it.ProductID
		case f.ModuleID != "":
			m, err := store.ModuleByID(ctx, q, f.ModuleID)
			if err != nil {
				return nil, wrapNotFound(err, "模块不存在或不可见")
			}
			productID = m.ProductID
		case f.AssigneeID != "":
			// 跨产品按负责人过滤：仅返回可见产品的任务。
			return s.listTasksByAssignee(ctx, actor, f.AssigneeID)
		default:
			return nil, Invalid("至少提供 productId / iterationId / moduleId / assigneeId 之一")
		}
	}
	if _, err := s.mustView(ctx, q, actor, productID); err != nil {
		return nil, err
	}

	var rows []store.TaskRow
	var err error
	switch {
	case f.IterationID != "":
		rows, err = store.ListTasksByIteration(ctx, q, f.IterationID, false)
	case f.ModuleID != "":
		rows, err = store.ListTasksByModule(ctx, q, f.ModuleID)
	default:
		// 产品全量：经迭代索引反查不优雅，此处按产品直查。
		rows, err = store.ListTasksByProduct(ctx, q, productID)
	}
	if err != nil {
		return nil, err
	}
	out := make([]TaskView, 0, len(rows))
	ids := make([]string, 0, len(rows))
	for _, t := range rows {
		ids = append(ids, t.ID)
	}
	terminals, err := store.ListTaskTerminals(ctx, q, ids)
	if err != nil {
		return nil, err
	}
	tset := map[string]planning.TaskStatus{}
	for _, tr := range terminals {
		tset[tr.WorkItemID] = taskStatusOf(tr.TerminalNodeID, tr.DefinitionJSON)
	}
	for _, t := range rows {
		v := s.viewTaskWithoutInstance(t)
		if st, ok := tset[t.ID]; ok {
			v.Status = string(st)
		}
		if f.AssigneeID != "" && t.AssigneeID != f.AssigneeID {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Service) listTasksByAssignee(ctx context.Context, actor Actor, assigneeID string) ([]TaskView, error) {
	q := s.DB.Q()
	// 可见产品集合。
	products, err := store.ListVisibleProducts(ctx, q, actor.UserID, actor.IsAdmin)
	if err != nil {
		return nil, err
	}
	if len(products) == 0 {
		return []TaskView{}, nil
	}
	var out []TaskView
	seen := map[string]bool{}
	for _, p := range products {
		rows, err := store.ListTasksByAssignee(ctx, q, p.ID, assigneeID)
		if err != nil {
			return nil, err
		}
		for _, t := range rows {
			if seen[t.ID] {
				continue
			}
			seen[t.ID] = true
			v := s.viewTask(ctx, q, t)
			out = append(out, v)
		}
	}
	return out, nil
}

func (s *Service) viewTaskWithoutInstance(t store.TaskRow) TaskView {
	v := TaskView{
		ID: t.ID, ProductID: t.ProductID, IterationID: t.IterationID,
		Number: t.Number, Title: t.Title, Description: t.Description,
		Kind: string(t.Kind), AssigneeID: t.AssigneeID, Priority: string(t.Priority),
		Status: string(planning.TaskOpen), WorkflowID: t.WorkflowID,
	}
	if t.ParentID != nil {
		v.ParentID = *t.ParentID
	}
	return v
}

// TaskUpdateInput 任务字段更新（编号/父子/产品/迭代不可变）。
type TaskUpdateInput struct {
	Title       *string
	Description *string
	AssigneeID  *string
	Priority    *planning.Priority
}

// UpdateTask 改任务字段/指派：任务负责人、产品 owner、pm、admin（不变量 4：唯一负责人）。
func (s *Service) UpdateTask(ctx context.Context, actor Actor, id string, in TaskUpdateInput) (TaskView, error) {
	var out TaskView
	err := s.DB.WithTx(ctx, func(q store.Querier) error {
		t, err := store.TaskByID(ctx, q, id)
		if err != nil {
			return wrapNotFound(err, "任务不存在或不可见")
		}
		mc, err := s.mustView(ctx, q, actor, t.ProductID)
		if err != nil {
			return err
		}
		if err := canWorkTask(mc, actor, t.AssigneeID); err != nil {
			return err
		}
		if in.AssigneeID != nil && *in.AssigneeID != "" && *in.AssigneeID != t.AssigneeID {
			if _, err := store.UserByID(ctx, q, *in.AssigneeID); err != nil {
				return wrapNotFound(err, "指派目标成员不存在")
			}
			if err := store.InsertOutbox(ctx, q, "task.assigned", "task", id,
				map[string]any{"assignee": *in.AssigneeID, "number": t.Number}); err != nil {
				return err
			}
		}
		if err := store.UpdateTask(ctx, q, id, store.TaskUpdate{
			Title: in.Title, Description: in.Description, AssigneeID: in.AssigneeID, Priority: in.Priority,
		}); err != nil {
			return err
		}
		if err := audit(ctx, q, actor, "task.update", "task", id, nil); err != nil {
			return err
		}
		updated, err := store.TaskByID(ctx, q, id)
		if err != nil {
			return err
		}
		out = s.viewTask(ctx, q, updated)
		return nil
	})
	return out, err
}

// DeleteTask 删任务：仅限未终结且无子任务的任务（已终结历史保留，呼应不变量
// 10「不允许静默丢弃」；有子任务须先删子任务）。权限同改任务字段（不变量 4）。
// 任务流实例随任务一并删除（node_states / gate_approvals / artifacts 外键级联）。
func (s *Service) DeleteTask(ctx context.Context, actor Actor, id string) error {
	return s.DB.WithTx(ctx, func(q store.Querier) error {
		t, err := store.TaskByID(ctx, q, id)
		if err != nil {
			return wrapNotFound(err, "任务不存在或不可见")
		}
		mc, err := s.mustView(ctx, q, actor, t.ProductID)
		if err != nil {
			return err
		}
		if err := canWorkTask(mc, actor, t.AssigneeID); err != nil {
			return err
		}
		if status := s.viewTask(ctx, q, t).Status; status != string(planning.TaskOpen) {
			return TaskNotDeletable("任务 %s 已终结（%s），历史保留不可删除", t.Number, status)
		}
		children, err := store.ListChildren(ctx, q, id)
		if err != nil {
			return err
		}
		if len(children) > 0 {
			return TaskNotDeletable("任务 %s 有 %d 个子任务，请先删除子任务", t.Number, len(children))
		}
		if t.WorkflowInstanceID != "" {
			if err := store.DeleteInstance(ctx, q, t.WorkflowInstanceID); err != nil {
				return err
			}
		}
		if err := store.DeleteTask(ctx, q, id); err != nil {
			return wrapNotFound(err, "任务不存在或不可见")
		}
		return audit(ctx, q, actor, "task.delete", "task", id,
			map[string]any{"number": t.Number, "title": t.Title})
	})
}

// LinkModule 增删任务与模块的关联：任务负责人/pm/owner/admin；模块同产品（不变量 2）。
func (s *Service) LinkModule(ctx context.Context, actor Actor, taskID, moduleID string, add bool) error {
	return s.DB.WithTx(ctx, func(q store.Querier) error {
		t, err := store.TaskByID(ctx, q, taskID)
		if err != nil {
			return wrapNotFound(err, "任务不存在或不可见")
		}
		mc, err := s.mustView(ctx, q, actor, t.ProductID)
		if err != nil {
			return err
		}
		if err := canWorkTask(mc, actor, t.AssigneeID); err != nil {
			return err
		}
		m, err := store.ModuleByID(ctx, q, moduleID)
		if err != nil {
			return wrapNotFound(err, "模块不存在或不可见")
		}
		if err := planning.EnsureModuleSameProduct(t.ProductID, m.ProductID); err != nil {
			return MapError(err)
		}
		if add {
			if err := store.AddTaskModule(ctx, q, taskID, moduleID); err != nil {
				return err
			}
		} else {
			if err := store.RemoveTaskModule(ctx, q, taskID, moduleID); err != nil {
				return err
			}
		}
		return audit(ctx, q, actor, "task.link_module", "task", taskID,
			map[string]any{"moduleId": moduleID, "add": add})
	})
}

// iterationFromRow 存储行 → 领域迭代。
func iterationFromRow(it store.IterationRow) planning.Iteration {
	return planning.Iteration{
		ID: it.ID, ProductID: it.ProductID, Name: it.Name, Goal: it.Goal,
		Number: planning.IterationNumber(it.Number), StartsOn: it.StartsOn, EndsOn: it.EndsOn,
		Status: it.Status, WorkflowID: it.WorkflowID,
	}
}
