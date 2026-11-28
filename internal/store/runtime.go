package store

// runtime.go：工作流模板与运行实例（docs/modules/workflow.md）。
// 实例操作遵循「加载 → 引擎纯函数判定 → 写回」，本文件不做任何判定；
// RowsToInstance / RowsToInputs 是纯转换函数，可脱离数据库单测。

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/yuylong/Chalkline/internal/domain/workitem"
	"github.com/yuylong/Chalkline/internal/workflow"
)

// ---- 模板 ----

// WorkflowRow 是模板行。
type WorkflowRow struct {
	ID         string
	Scope      workitem.Scope
	Name       string
	Version    int
	Status     string
	Definition json.RawMessage
}

func scanWorkflow(row rowScanner) (WorkflowRow, error) {
	var w WorkflowRow
	err := row.Scan(&w.ID, &w.Scope, &w.Name, &w.Version, &w.Status, &w.Definition)
	return w, err
}

// ListWorkflows 列模板，可按 scope 过滤（含内置）。
func ListWorkflows(ctx context.Context, q Querier, scope *workitem.Scope) ([]WorkflowRow, error) {
	sql := `SELECT id, scope, name, version, status, definition FROM workflows`
	args := []any{}
	if scope != nil {
		sql += ` WHERE scope = $1 AND status <> 'archived'`
		args = append(args, string(*scope))
	}
	sql += ` ORDER BY scope, name, version DESC`
	return scanAll(ctx, q, sql, scanWorkflow, args...)
}

// WorkflowByID 取模板。
func WorkflowByID(ctx context.Context, q Querier, id string) (WorkflowRow, error) {
	return scanWorkflow(q.QueryRow(ctx,
		`SELECT id, scope, name, version, status, definition FROM workflows WHERE id = $1`, id))
}

// InsertWorkflow 写入新模板。
func InsertWorkflow(ctx context.Context, q Querier, def workflow.Definition, status string) error {
	body, err := json.Marshal(def)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx,
		`INSERT INTO workflows (id, scope, name, version, status, definition)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		def.ID, string(def.Scope), def.Name, def.Version, status, body)
	return err
}

// BumpWorkflowVersion 改模板即升版本（运行中实例不受影响——实例存定义快照）。
// WHERE version = expected 提供乐观锁：并发修改一方 409。
func BumpWorkflowVersion(ctx context.Context, q Querier, id string, expectedVersion int, def workflow.Definition) (int, error) {
	var version int
	err := q.QueryRow(ctx,
		`UPDATE workflows SET version = $3, definition = $4, updated_at = now()
		 WHERE id = $1 AND version = $2 AND status <> 'archived' RETURNING version`,
		id, expectedVersion, expectedVersion+1, defJSON(def)).Scan(&version)
	if err == ErrNotFound {
		return 0, ErrNotFound
	}
	return version, err
}

// UpdateWorkflowDefinition 原地覆盖定义（仅 draft 模板可原地改，升版本走 Bump）。
func UpdateWorkflowDefinition(ctx context.Context, q Querier, id string, def workflow.Definition) error {
	tag, err := q.Exec(ctx,
		`UPDATE workflows SET definition = $2, updated_at = now() WHERE id = $1 AND status = 'draft'`,
		id, defJSON(def))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ArchiveWorkflow 软删模板。
func ArchiveWorkflow(ctx context.Context, q Querier, id string) error {
	tag, err := q.Exec(ctx, `UPDATE workflows SET status = 'archived', updated_at = now() WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func defJSON(def workflow.Definition) []byte {
	body, err := json.Marshal(def)
	if err != nil {
		return []byte(`{}`)
	}
	return body
}

// ---- 实例 ----

// InstanceRow 是实例行。
type InstanceRow struct {
	ID              string
	WorkflowID      string
	WorkflowVersion int
	WorkItemType    workitem.Scope
	WorkItemID      string
	DefinitionJSON  json.RawMessage
	TerminalNodeID  string
}

// InsertInstance 在事务内创建实例与全部初始节点状态。
func InsertInstance(ctx context.Context, q Querier, row InstanceRow, inst *workflow.Instance) error {
	_, err := q.Exec(ctx,
		`INSERT INTO workflow_instances (id, workflow_id, workflow_version, work_item_type, work_item_id, definition, terminal_node_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		row.ID, row.WorkflowID, row.WorkflowVersion, string(row.WorkItemType),
		row.WorkItemID, row.DefinitionJSON, inst.TerminalNodeID)
	if err != nil {
		return err
	}
	return SaveNodeStates(ctx, q, row.ID, inst)
}

// InstanceByWorkItem 按工作项取实例（一个工作项一个实例）。
func InstanceByWorkItem(ctx context.Context, q Querier, wtype workitem.Scope, workItemID string) (InstanceRow, error) {
	var r InstanceRow
	err := q.QueryRow(ctx,
		`SELECT id, workflow_id, workflow_version, work_item_type, work_item_id, definition, terminal_node_id
		 FROM workflow_instances WHERE work_item_type = $1 AND work_item_id = $2`,
		string(wtype), workItemID).
		Scan(&r.ID, &r.WorkflowID, &r.WorkflowVersion, &r.WorkItemType, &r.WorkItemID, &r.DefinitionJSON, &r.TerminalNodeID)
	return r, err
}

// DeleteInstance 删实例行（node_states / gate_approvals / artifacts 随外键级联清理）。
func DeleteInstance(ctx context.Context, q Querier, instanceID string) error {
	_, err := q.Exec(ctx, `DELETE FROM workflow_instances WHERE id = $1`, instanceID)
	return err
}

// ListNodeStates 取全部节点状态。
func ListNodeStates(ctx context.Context, q Querier, instanceID string) ([]workflow.NodeState, error) {
	return scanAll(ctx, q,
		`SELECT node_id, status, version, entered_at, completed_at
		 FROM node_states WHERE instance_id = $1 ORDER BY node_id`,
		func(row rowScanner) (workflow.NodeState, error) {
			var st workflow.NodeState
			var entered, completed *time.Time
			if err := row.Scan(&st.NodeID, &st.Status, &st.Version, &entered, &completed); err != nil {
				return st, err
			}
			if entered != nil {
				st.EnteredAt = *entered
			}
			if completed != nil {
				st.CompletedAt = *completed
			}
			return st, nil
		}, instanceID)
}

// ListApprovals 取全部审批记录。
func ListApprovals(ctx context.Context, q Querier, instanceID string) ([]workitem.ReviewDecision, error) {
	return scanAll(ctx, q,
		`SELECT node_id, reviewer_id, decision, comment, roles, created_at
		 FROM gate_approvals WHERE instance_id = $1 ORDER BY created_at`,
		func(row rowScanner) (workitem.ReviewDecision, error) {
			var d workitem.ReviewDecision
			var roles []string
			if err := row.Scan(&d.NodeID, &d.ReviewerID, &d.Decision, &d.Comment, &roles, &d.CreatedAt); err != nil {
				return d, err
			}
			d.Roles = ToRoles(roles)
			return d, nil
		}, instanceID)
}

// ArtifactRow 是材料行（含 DB id 便于管理）。
type ArtifactRow struct {
	ID         string
	InstanceID string
	Ref        workitem.ArtifactRef
	Snapshot   json.RawMessage
	CreatedAt  time.Time
	// ExternalPath 是材料在外部系统里的可读路径（面包屑），仅分发后非空。
	ExternalPath string
	// ArchiveID 是替换时给被覆盖旧行分配的归档 id（artv_ 前缀，由 app 层生成）；
	// 为空表示不归档（如首次挂接没有旧行）。
	ArchiveID string
}

// ArtifactVersionRow 是被替换下来的历史材料（artifact_versions 行）。
type ArtifactVersionRow struct {
	ID           string
	ArtifactID   string
	InstanceID   string
	Ref          workitem.ArtifactRef
	Snapshot     json.RawMessage
	CreatedAt    time.Time // 原提交时间
	ReplacedAt   time.Time // 被替换时间
	ReplacedBy   string    // 触发替换的提交人
	ExternalPath string
}

// ListArtifactVersions 取实例内被替换过的材料版本（按替换时间倒序）。
func ListArtifactVersions(ctx context.Context, q Querier, instanceID string) ([]ArtifactVersionRow, error) {
	return scanAll(ctx, q,
		`SELECT id, artifact_id, instance_id, node_id, kind, provider, url, external_id,
		     external_path, title, content, snapshot, submitted_by, created_at, replaced_at, replaced_by
		 FROM artifact_versions WHERE instance_id = $1 ORDER BY replaced_at DESC`,
		func(row rowScanner) (ArtifactVersionRow, error) {
			var v ArtifactVersionRow
			if err := row.Scan(&v.ID, &v.ArtifactID, &v.InstanceID, &v.Ref.NodeID, &v.Ref.Kind,
				&v.Ref.Provider, &v.Ref.URL, &v.Ref.ExternalID, &v.ExternalPath, &v.Ref.Title,
				&v.Ref.Content, &v.Snapshot, &v.Ref.SubmittedBy, &v.CreatedAt, &v.ReplacedAt,
				&v.ReplacedBy); err != nil {
				return v, err
			}
			return v, nil
		}, instanceID)
}

// ArtifactVersionCount 返回某 (节点, 类型) 已归档的历史版本数。
// 分发时用它推导版本号（第 n 版 = 历史数 + 1），让外部系统能区分轮次。
func ArtifactVersionCount(ctx context.Context, q Querier, instanceID, nodeID, kind string) int64 {
	var n int64
	_ = q.QueryRow(ctx,
		`SELECT count(*) FROM artifact_versions
		 WHERE instance_id = $1 AND node_id = $2 AND kind = $3`,
		instanceID, nodeID, kind).Scan(&n)
	return n
}

// ListArtifacts 取实例全部材料。
func ListArtifacts(ctx context.Context, q Querier, instanceID string) ([]ArtifactRow, error) {
	return scanAll(ctx, q,
		`SELECT id, instance_id, node_id, kind, provider, url, external_id, external_path, title, content, submitted_by, snapshot, created_at
		 FROM artifacts WHERE instance_id = $1 ORDER BY node_id, kind`,
		func(row rowScanner) (ArtifactRow, error) {
			var a ArtifactRow
			if err := row.Scan(&a.ID, &a.InstanceID, &a.Ref.NodeID, &a.Ref.Kind, &a.Ref.Provider,
				&a.Ref.URL, &a.Ref.ExternalID, &a.ExternalPath, &a.Ref.Title, &a.Ref.Content, &a.Ref.SubmittedBy,
				&a.Snapshot, &a.CreatedAt); err != nil {
				return a, err
			}
			return a, nil
		}, instanceID)
}

// SaveNodeStates 全量写回节点状态（引擎判定后的唯一落点；version 随引擎状态写）。
func SaveNodeStates(ctx context.Context, q Querier, instanceID string, inst *workflow.Instance) error {
	for _, st := range inst.Nodes {
		var entered, completed *time.Time
		if !st.EnteredAt.IsZero() {
			t := st.EnteredAt
			entered = &t
		}
		if !st.CompletedAt.IsZero() {
			t := st.CompletedAt
			completed = &t
		}
		_, err := q.Exec(ctx,
			`INSERT INTO node_states (instance_id, node_id, status, version, entered_at, completed_at)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT (instance_id, node_id) DO UPDATE SET
			   status = EXCLUDED.status, version = EXCLUDED.version,
			   entered_at = EXCLUDED.entered_at, completed_at = EXCLUDED.completed_at`,
			instanceID, st.NodeID, string(st.Status), st.Version, entered, completed)
		if err != nil {
			return err
		}
	}
	_, err := q.Exec(ctx,
		`UPDATE workflow_instances SET terminal_node_id = $2 WHERE id = $1`,
		instanceID, inst.TerminalNodeID)
	return err
}

// UpsertApproval upsert 评审决策（同评审人最近决策为准）。
func UpsertApproval(ctx context.Context, q Querier, instanceID string, d workitem.ReviewDecision) error {
	_, err := q.Exec(ctx,
		`INSERT INTO gate_approvals (instance_id, node_id, reviewer_id, decision, comment, roles, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (instance_id, node_id, reviewer_id) DO UPDATE SET
		   decision = EXCLUDED.decision, comment = EXCLUDED.comment,
		   roles = EXCLUDED.roles, created_at = EXCLUDED.created_at`,
		instanceID, d.NodeID, d.ReviewerID, string(d.Decision), d.Comment, FromRoles(d.Roles), d.CreatedAt)
	return err
}

// ResetNodeApprovals 材料替换后作废该节点既有审批（历史经审计保留，不变量见 workflow.md「材料变更」）。
func ResetNodeApprovals(ctx context.Context, q Querier, instanceID, nodeID string) error {
	_, err := q.Exec(ctx, `DELETE FROM gate_approvals WHERE instance_id = $1 AND node_id = $2`, instanceID, nodeID)
	return err
}

// ReplaceArtifact 挂接/替换材料：同 (instance, node, kind) 仅一份，替换即重置审批。
// 返回是否发生了替换（供 app 层决定是否作废审批）。
//
// 被替换的旧行不丢失：删除前整条归档到 artifact_versions（节点详情页「历史轮次」
// 的数据来源）。归档失败只记日志，不阻断替换——历史回看是增强能力，不能挡住流程推进。
func ReplaceArtifact(ctx context.Context, q Querier, art ArtifactRow, resetApprovals bool) error {
	// 归档键由调用方生成（store 不引入 id 生成器，与其它写入路径一致）。
	if art.ArchiveID != "" {
		if _, err := q.Exec(ctx,
			`INSERT INTO artifact_versions (id, artifact_id, instance_id, node_id, kind, provider,
			     url, external_id, external_path, title, content, snapshot, submitted_by, created_at, replaced_by)
			 SELECT $1::text, id, instance_id, node_id, kind, provider,
			     url, external_id, external_path, title, content, snapshot, submitted_by, created_at, $2::text
			 FROM artifacts WHERE instance_id = $3::text AND node_id = $4::text AND kind = $5::text`,
			art.ArchiveID, art.Ref.SubmittedBy, art.InstanceID, art.Ref.NodeID,
			string(art.Ref.Kind)); err != nil {
			slog.Warn("材料历史归档失败（不影响替换）", "instance", art.InstanceID,
				"node", art.Ref.NodeID, "kind", art.Ref.Kind, "error", err)
		}
	}

	if _, err := q.Exec(ctx,
		`DELETE FROM artifacts WHERE instance_id = $1 AND node_id = $2 AND kind = $3`,
		art.InstanceID, art.Ref.NodeID, string(art.Ref.Kind)); err != nil {
		return err
	}
	_, err := q.Exec(ctx,
		`INSERT INTO artifacts (id, instance_id, node_id, kind, provider, url, external_id, external_path, title, content, snapshot, submitted_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		art.ID, art.InstanceID, art.Ref.NodeID, string(art.Ref.Kind), string(art.Ref.Provider),
		art.Ref.URL, art.Ref.ExternalID, art.ExternalPath, art.Ref.Title, art.Ref.Content,
		art.Snapshot, art.Ref.SubmittedBy)
	if err != nil {
		return err
	}
	if resetApprovals {
		return ResetNodeApprovals(ctx, q, art.InstanceID, art.Ref.NodeID)
	}
	return nil
}

// InsertOutbox 写领域事件（通知走 outbox，失败重试不阻断流程，tech.md 决策）。
func InsertOutbox(ctx context.Context, q Querier, eventType, subjectType, subjectID string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		body = []byte(`{}`)
	}
	_, err = q.Exec(ctx,
		`INSERT INTO outbox_events (event_type, subject_type, subject_id, payload)
		 VALUES ($1, $2, $3, $4)`, eventType, subjectType, subjectID, body)
	return err
}

// AwaitingNodeRow 是待审批节点视图（get_pending_reviews 的存储侧查询）。
type AwaitingNodeRow struct {
	InstanceID   string
	WorkItemType workitem.Scope
	WorkItemID   string
	NodeID       string
	EnteredAt    time.Time // 进入评审时间（M3 超期判定）
}

// ListAwaitingNodes 列给定产品集合内全部 awaiting_review 节点；
// 角色资格过滤在 app 层依模板 gate.roles 完成。EnteredAt 供超期判定（M3）。
func ListAwaitingNodes(ctx context.Context, q Querier, productIDs []string) ([]AwaitingNodeRow, error) {
	if len(productIDs) == 0 {
		return nil, nil
	}
	return scanAll(ctx, q,
		`SELECT wi.id, wi.work_item_type, wi.work_item_id, ns.node_id, ns.entered_at
		 FROM workflow_instances wi
		 JOIN node_states ns ON ns.instance_id = wi.id AND ns.status = 'awaiting_review'
		 WHERE (wi.work_item_type = 'task'
		          AND (SELECT t.product_id FROM tasks t WHERE t.id = wi.work_item_id) = ANY($1))
		    OR (wi.work_item_type = 'iteration'
		          AND (SELECT it.product_id FROM iterations it WHERE it.id = wi.work_item_id) = ANY($1))
		 ORDER BY ns.entered_at`,
		func(row rowScanner) (AwaitingNodeRow, error) {
			var a AwaitingNodeRow
			err := row.Scan(&a.InstanceID, &a.WorkItemType, &a.WorkItemID, &a.NodeID, &a.EnteredAt)
			return a, err
		}, productIDs)
}

// ---- M3：评审超期提醒 / SSE 事件尾随 / 模板导入导出 ----

// OverdueAwaitingRow 是超期待审节点（含产品归属与进入评审时间）。
type OverdueAwaitingRow struct {
	InstanceID   string
	NodeID       string
	WorkItemType workitem.Scope
	WorkItemID   string
	ProductID    string
	EnteredAt    time.Time
}

// ListOverdueAwaiting 列 entered_at 早于 cutoff 的全部 awaiting_review 节点
// （SLA 监控器每轮调用；不改任何节点状态，红线 1）。
func ListOverdueAwaiting(ctx context.Context, q Querier, cutoff time.Time) ([]OverdueAwaitingRow, error) {
	return scanAll(ctx, q,
		`SELECT wi.id, ns.node_id, wi.work_item_type, wi.work_item_id,
		        COALESCE((SELECT t.product_id FROM tasks t WHERE t.id = wi.work_item_id),
		                 (SELECT it.product_id FROM iterations it WHERE it.id = wi.work_item_id)),
		        ns.entered_at
		 FROM workflow_instances wi
		 JOIN node_states ns ON ns.instance_id = wi.id AND ns.status = 'awaiting_review'
		 WHERE ns.entered_at < $1
		 ORDER BY ns.entered_at`,
		func(row rowScanner) (OverdueAwaitingRow, error) {
			var r OverdueAwaitingRow
			err := row.Scan(&r.InstanceID, &r.NodeID, &r.WorkItemType, &r.WorkItemID,
				&r.ProductID, &r.EnteredAt)
			return r, err
		}, cutoff)
}

// GateReminderExists 报告该 (instance, node, level) 提醒是否已发过（防重复）。
func GateReminderExists(ctx context.Context, q Querier, instanceID, nodeID string, level int) (bool, error) {
	var exists bool
	err := q.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM gate_reminders
		 WHERE instance_id = $1 AND node_id = $2 AND level = $3)`,
		instanceID, nodeID, level).Scan(&exists)
	return exists, err
}

// InsertGateReminder 记录一次提醒事实（水位只增不改）。
func InsertGateReminder(ctx context.Context, q Querier, instanceID, nodeID string, level int) error {
	_, err := q.Exec(ctx,
		`INSERT INTO gate_reminders (instance_id, node_id, level) VALUES ($1, $2, $3)
		 ON CONFLICT DO NOTHING`, instanceID, nodeID, level)
	return err
}

// ListOutboxAfter 尾随读取新增事件（SSE 推送侧；按 id 单调递增）。
func ListOutboxAfter(ctx context.Context, q Querier, afterID int64, limit int) ([]OutboxRow, error) {
	return scanAll(ctx, q,
		`SELECT `+outboxColumns+` FROM outbox_events WHERE id > $1 ORDER BY id LIMIT $2`,
		scanOutbox, afterID, limit)
}

// MaxOutboxID 取当前最大事件 id（SSE 启动时定位游标：只推增量）。
func MaxOutboxID(ctx context.Context, q Querier) (int64, error) {
	var id int64
	err := q.QueryRow(ctx, `SELECT COALESCE(max(id), 0) FROM outbox_events`).Scan(&id)
	return id, err
}

// ProductIDOfSubject 解析事件主体归属的产品（SSE 可见性过滤用）；
// 非工作项主体（workflow / user / webhook 等）返回空串。
func ProductIDOfSubject(ctx context.Context, q Querier, subjectType, subjectID string) (string, error) {
	switch subjectType {
	case "task":
		var pid string
		err := q.QueryRow(ctx, `SELECT product_id FROM tasks WHERE id = $1`, subjectID).Scan(&pid)
		if err != nil {
			return "", err
		}
		return pid, nil
	case "iteration":
		var pid string
		err := q.QueryRow(ctx, `SELECT product_id FROM iterations WHERE id = $1`, subjectID).Scan(&pid)
		if err != nil {
			return "", err
		}
		return pid, nil
	case "product":
		return subjectID, nil
	}
	return "", nil
}

// WorkflowExists 报告模板 id 是否已占用（导入时换 id 用）。
func WorkflowExists(ctx context.Context, q Querier, id string) (bool, error) {
	var exists bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workflows WHERE id = $1)`, id).Scan(&exists)
	return exists, err
}

// MaxWorkflowVersion 取同 scope+name 的最大版本号（导入撞名时升版本用）。
func MaxWorkflowVersion(ctx context.Context, q Querier, scope, name string) (int, error) {
	var v int
	err := q.QueryRow(ctx,
		`SELECT COALESCE(max(version), 0) FROM workflows WHERE scope = $1 AND name = $2`,
		scope, name).Scan(&v)
	return v, err
}

// ---- 纯转换（可脱离数据库测试）----

// RowsToInstance 把存储行还原为引擎实例。
func RowsToInstance(row InstanceRow, states []workflow.NodeState) (*workflow.Instance, error) {
	def, err := workflow.ParseJSON(row.DefinitionJSON)
	if err != nil {
		return nil, fmt.Errorf("store: 实例 %s 模板快照非法: %w", row.ID, err)
	}
	inst := &workflow.Instance{Definition: def, Nodes: map[string]*workflow.NodeState{}, TerminalNodeID: row.TerminalNodeID}
	for _, st := range states {
		s := st
		inst.Nodes[st.NodeID] = &s
	}
	for _, n := range def.Nodes {
		if _, ok := inst.Nodes[n.ID]; !ok {
			return nil, fmt.Errorf("store: 实例 %s 缺节点 %s 状态", row.ID, n.ID)
		}
	}
	return inst, nil
}

// RowsToInputs 把审批与材料组装为逐节点引擎输入（ChildrenDone / IterationTasksDone 由 app 填充）。
func RowsToInputs(approvals []workitem.ReviewDecision, artifacts []workitem.ArtifactRef) map[string]workflow.NodeInput {
	inputs := map[string]workflow.NodeInput{}
	get := func(nodeID string) *workflow.NodeInput {
		if in, ok := inputs[nodeID]; ok {
			return &in
		}
		in := workflow.NodeInput{}
		inputs[nodeID] = in
		return &in
	}
	for _, d := range approvals {
		in := get(d.NodeID)
		in.Decisions = append(in.Decisions, d)
		inputs[d.NodeID] = *in
	}
	for _, a := range artifacts {
		in := get(a.NodeID)
		in.Artifacts = append(in.Artifacts, a)
		inputs[a.NodeID] = *in
	}
	return inputs
}
