package store

// planning.go：迭代、任务、任务×模块与编号计数器（docs/modules/planning.md）。
// tasks 无 status 列：任务状态由实例终态投影，终结判定统一走 workflow_instances.terminal_node_id。

import (
	"context"
	"time"

	"github.com/yuylong/Chalkline/internal/domain/planning"
)

// IterationRow 是迭代存储形态。
type IterationRow struct {
	ID                  string
	ProductID           string
	Name                string
	Goal                string
	Number              string
	StartsOn            time.Time
	EndsOn              time.Time
	Status              planning.IterationStatus
	WorkflowID          string
	WorkflowInstanceID  string
	CreatedBy           string
	CreatedAt           time.Time
}

const iterationCols = `id, product_id, name, goal, number, starts_on, ends_on, status, workflow_id, workflow_instance_id, created_by, created_at`

func scanIteration(row rowScanner) (IterationRow, error) {
	var it IterationRow
	err := row.Scan(&it.ID, &it.ProductID, &it.Name, &it.Goal, &it.Number,
		&it.StartsOn, &it.EndsOn, &it.Status, &it.WorkflowID, &it.WorkflowInstanceID,
		&it.CreatedBy, &it.CreatedAt)
	return it, err
}

// InsertIteration 写入迭代。
func InsertIteration(ctx context.Context, q Querier, it IterationRow) error {
	_, err := q.Exec(ctx,
		`INSERT INTO iterations (id, product_id, name, goal, number, starts_on, ends_on, status, workflow_id, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		it.ID, it.ProductID, it.Name, it.Goal, it.Number, it.StartsOn, it.EndsOn,
		string(it.Status), it.WorkflowID, it.CreatedBy)
	return err
}

// IterationByID 取迭代。
func IterationByID(ctx context.Context, q Querier, id string) (IterationRow, error) {
	return scanIteration(q.QueryRow(ctx, `SELECT `+iterationCols+` FROM iterations WHERE id = $1`, id))
}

// ListIterationsByProduct 列产品迭代，可按状态过滤。
func ListIterationsByProduct(ctx context.Context, q Querier, productID string, status *planning.IterationStatus) ([]IterationRow, error) {
	sql := `SELECT ` + iterationCols + ` FROM iterations WHERE product_id = $1`
	args := []any{productID}
	if status != nil {
		sql += ` AND status = $2`
		args = append(args, string(*status))
	}
	sql += ` ORDER BY created_at DESC`
	return scanAll(ctx, q, sql, scanIteration, args...)
}

// CountOpenIterations 统计产品内未收尾迭代（planned/active）——归档守卫用（M3）。
func CountOpenIterations(ctx context.Context, q Querier, productID string) (int64, error) {
	var n int64
	err := q.QueryRow(ctx,
		`SELECT count(*) FROM iterations WHERE product_id = $1 AND status IN ('planned', 'active')`,
		productID).Scan(&n)
	return n, err
}

// ---- M3 全局概览（多产品规模化管理）----

// OverviewCounts 是可见产品集合的规模统计。
type OverviewCounts struct {
	ActiveIterations int64
	OpenTasks        int64
	PendingGates     int64
	OverdueGates     int64
}

const overviewWorkItem = `(wi.work_item_type = 'task'
   AND (SELECT t.product_id FROM tasks t WHERE t.id = wi.work_item_id) = ANY($1))
 OR (wi.work_item_type = 'iteration'
   AND (SELECT it.product_id FROM iterations it WHERE it.id = wi.work_item_id) = ANY($1))`

// CountOverview 统计给定产品集合的迭代/任务/门禁规模（SSE 可见性同源：产品集合由 app 判定）。
// overdueBefore 之前的 awaiting_review 计为超期（评审 SLA）。
func CountOverview(ctx context.Context, q Querier, productIDs []string, overdueBefore time.Time) (OverviewCounts, error) {
	var c OverviewCounts
	if len(productIDs) == 0 {
		return c, nil
	}
	err := q.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM iterations
		    WHERE product_id = ANY($1) AND status IN ('planned', 'active')),
		  (SELECT count(*) FROM tasks t
		    WHERE t.product_id = ANY($1)
		      AND NOT EXISTS (SELECT 1 FROM workflow_instances wi
		                      WHERE wi.work_item_type = 'task' AND wi.work_item_id = t.id
		                        AND wi.terminal_node_id <> '')),
		  (SELECT count(*) FROM workflow_instances wi
		    JOIN node_states ns ON ns.instance_id = wi.id AND ns.status = 'awaiting_review'
		    WHERE `+overviewWorkItem+`),
		  (SELECT count(*) FROM workflow_instances wi
		    JOIN node_states ns ON ns.instance_id = wi.id AND ns.status = 'awaiting_review'
		                           AND ns.entered_at < $2
		    WHERE `+overviewWorkItem+`)`,
		productIDs, overdueBefore).Scan(
		&c.ActiveIterations, &c.OpenTasks, &c.PendingGates, &c.OverdueGates)
	return c, err
}

// UpdateIterationStatus 更新迭代状态（仅经领域方法 Activate/Close/Cancel 之后调用）。
func UpdateIterationStatus(ctx context.Context, q Querier, id string, status planning.IterationStatus) error {
	tag, err := q.Exec(ctx, `UPDATE iterations SET status = $2, updated_at = now() WHERE id = $1`, id, string(status))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetIterationInstance 绑定实例。
func SetIterationInstance(ctx context.Context, q Querier, id, instanceID string) error {
	_, err := q.Exec(ctx, `UPDATE iterations SET workflow_instance_id = $2, updated_at = now() WHERE id = $1`, id, instanceID)
	return err
}

// CountUnfinishedTasksInIteration 迭代收口校验（不变量 10）：未终结任务数。
func CountUnfinishedTasksInIteration(ctx context.Context, q Querier, iterationID string) (int64, error) {
	var n int64
	err := q.QueryRow(ctx,
		`SELECT count(*) FROM tasks t
		 WHERE t.iteration_id = $1
		   AND NOT EXISTS (
		     SELECT 1 FROM workflow_instances wi
		     WHERE wi.work_item_type = 'task' AND wi.work_item_id = t.id AND wi.terminal_node_id <> '')`,
		iterationID).Scan(&n)
	return n, err
}

// TaskRow 是任务存储形态。
type TaskRow struct {
	ID                  string
	ProductID           string
	IterationID         string
	ParentID            *string
	Number              string
	Title               string
	Description         string
	Kind                planning.TaskKind
	AssigneeID          string
	Priority            planning.Priority
	WorkflowID          string
	WorkflowInstanceID  string
	CreatedBy           string
	CreatedAt           time.Time
}

const taskCols = `id, product_id, iteration_id, parent_id, number, title, description, kind, assignee_id, priority, workflow_id, workflow_instance_id, created_by, created_at`

func scanTask(row rowScanner) (TaskRow, error) {
	var t TaskRow
	err := row.Scan(&t.ID, &t.ProductID, &t.IterationID, &t.ParentID, &t.Number, &t.Title,
		&t.Description, &t.Kind, &t.AssigneeID, &t.Priority, &t.WorkflowID, &t.WorkflowInstanceID,
		&t.CreatedBy, &t.CreatedAt)
	return t, err
}

// InsertTask 写入任务。
func InsertTask(ctx context.Context, q Querier, t TaskRow) error {
	_, err := q.Exec(ctx,
		`INSERT INTO tasks (id, product_id, iteration_id, parent_id, number, title, description, kind, assignee_id, priority, workflow_id, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		t.ID, t.ProductID, t.IterationID, t.ParentID, t.Number, t.Title, t.Description,
		string(t.Kind), t.AssigneeID, string(t.Priority), t.WorkflowID, t.CreatedBy)
	return err
}

// TaskByID 取任务。
func TaskByID(ctx context.Context, q Querier, id string) (TaskRow, error) {
	return scanTask(q.QueryRow(ctx, `SELECT `+taskCols+` FROM tasks WHERE id = $1`, id))
}

// TaskNumberTaken 编号占用检查（计数器保证不重复，此处防御兜底）。
func TaskNumberTaken(ctx context.Context, q Querier, number string) (bool, error) {
	var exists bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tasks WHERE number = $1)`, number).Scan(&exists)
	return exists, err
}

// ListTasksByIteration 列迭代任务，可只看顶层。
func ListTasksByIteration(ctx context.Context, q Querier, iterationID string, topLevelOnly bool) ([]TaskRow, error) {
	sql := `SELECT ` + taskCols + ` FROM tasks WHERE iteration_id = $1`
	if topLevelOnly {
		sql += ` AND parent_id IS NULL`
	}
	sql += ` ORDER BY number`
	return scanAll(ctx, q, sql, scanTask, iterationID)
}

// ListChildren 列子任务（收口校验与任务树视图）。
func ListChildren(ctx context.Context, q Querier, parentID string) ([]TaskRow, error) {
	return scanAll(ctx, q,
		`SELECT `+taskCols+` FROM tasks WHERE parent_id = $1 ORDER BY number`,
		scanTask, parentID)
}

// ListTasksByAssignee 列产品内某人负责的任务。
func ListTasksByAssignee(ctx context.Context, q Querier, productID, userID string) ([]TaskRow, error) {
	return scanAll(ctx, q,
		`SELECT `+taskCols+` FROM tasks WHERE product_id = $1 AND assignee_id = $2 ORDER BY number`,
		scanTask, productID, userID)
}

// ListTasksByProduct 列产品全部任务（可见性过滤由调用方保证）。
func ListTasksByProduct(ctx context.Context, q Querier, productID string) ([]TaskRow, error) {
	return scanAll(ctx, q,
		`SELECT `+taskCols+` FROM tasks WHERE product_id = $1 ORDER BY number`,
		scanTask, productID)
}

// ListTasksByModule 列挂某模块的任务（经 task_modules）。
func ListTasksByModule(ctx context.Context, q Querier, moduleID string) ([]TaskRow, error) {
	return scanAll(ctx, q,
		`SELECT t.`+taskCols+` FROM tasks t
		 JOIN task_modules tm ON tm.task_id = t.id
		 WHERE tm.module_id = $1 ORDER BY t.number`,
		scanTask, moduleID)
}

// TaskUpdate 是任务可更新字段（编号/父子/产品/迭代不可改）。
type TaskUpdate struct {
	Title       *string
	Description *string
	AssigneeID  *string
	Priority    *planning.Priority
}

// UpdateTask 更新任务字段。
func UpdateTask(ctx context.Context, q Querier, id string, u TaskUpdate) error {
	tag, err := q.Exec(ctx,
		`UPDATE tasks SET
		   title = COALESCE($2, title),
		   description = COALESCE($3, description),
		   assignee_id = COALESCE($4, assignee_id),
		   priority = COALESCE($5, priority),
		   updated_at = now()
		 WHERE id = $1`,
		id, u.Title, u.Description, u.AssigneeID, priorityPtr(u.Priority))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func priorityPtr(p *planning.Priority) *string {
	if p == nil {
		return nil
	}
	v := string(*p)
	return &v
}

// AddTaskModule 挂模块关联。
func AddTaskModule(ctx context.Context, q Querier, taskID, moduleID string) error {
	_, err := q.Exec(ctx,
		`INSERT INTO task_modules (task_id, module_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		taskID, moduleID)
	return err
}

// RemoveTaskModule 解除关联。
func RemoveTaskModule(ctx context.Context, q Querier, taskID, moduleID string) error {
	_, err := q.Exec(ctx, `DELETE FROM task_modules WHERE task_id = $1 AND module_id = $2`, taskID, moduleID)
	return err
}

// ListTaskModules 列任务关联的模块。
func ListTaskModules(ctx context.Context, q Querier, taskID string) ([]ModuleRow, error) {
	return scanAll(ctx, q,
		`SELECT m.id, m.product_id, m.parent_id, m.name, m.path, m.sort_order
		 FROM modules m JOIN task_modules tm ON tm.module_id = m.id
		 WHERE tm.task_id = $1 ORDER BY m.path`,
		scanModule, taskID)
}

// SetTaskInstance 绑定任务实例。
func SetTaskInstance(ctx context.Context, q Querier, id, instanceID string) error {
	_, err := q.Exec(ctx, `UPDATE tasks SET workflow_instance_id = $2, updated_at = now() WHERE id = $1`, id, instanceID)
	return err
}

// TerminalInfoRow 是任务终态投影所需的最小实例信息。
type TerminalInfoRow struct {
	WorkItemID     string
	TerminalNodeID string
	DefinitionJSON []byte
}

// ListTaskTerminals 批量取任务实例终态（列表视图的状态投影）。
func ListTaskTerminals(ctx context.Context, q Querier, taskIDs []string) ([]TerminalInfoRow, error) {
	if len(taskIDs) == 0 {
		return nil, nil
	}
	return scanAll(ctx, q,
		`SELECT work_item_id, terminal_node_id, definition
		 FROM workflow_instances WHERE work_item_type = 'task' AND work_item_id = ANY($1)`,
		func(row rowScanner) (TerminalInfoRow, error) {
			var r TerminalInfoRow
			err := row.Scan(&r.WorkItemID, &r.TerminalNodeID, &r.DefinitionJSON)
			return r, err
		}, taskIDs)
}

// CountUnfinishedChildren 父任务收口（不变量 9）：未终结子任务数。
func CountUnfinishedChildren(ctx context.Context, q Querier, taskID string) (int64, error) {
	var n int64
	err := q.QueryRow(ctx,
		`SELECT count(*) FROM tasks t
		 WHERE t.parent_id = $1
		   AND NOT EXISTS (
		     SELECT 1 FROM workflow_instances wi
		     WHERE wi.work_item_type = 'task' AND wi.work_item_id = t.id AND wi.terminal_node_id <> '')`,
		taskID).Scan(&n)
	return n, err
}

// NextCounter 原子取号并自增：编号永不复用（不变量 3）。
// scopeKey 如 product:<id>:iteration / product:<id>:task / task:<id>:child。
func NextCounter(ctx context.Context, q Querier, scopeKey string) (int64, error) {
	var v int64
	err := q.QueryRow(ctx,
		`INSERT INTO counters (scope_key, value) VALUES ($1, 1)
		 ON CONFLICT (scope_key) DO UPDATE SET value = counters.value + 1
		 RETURNING value`, scopeKey).Scan(&v)
	return v, err
}
