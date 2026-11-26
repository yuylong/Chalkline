package store

// catalog.go：产品、产品成员、模块树、文档查询（docs/modules/catalog.md）。
// 产品隔离硬性约束：可见性查询必须先过 product_members，禁止全量查再内存过滤（tech.md 约束 11）。

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yuylong/Chalkline/internal/domain/product"
)

// ProductRow 是产品存储形态。
type ProductRow struct {
	ID                         string
	Key                        string
	Name                       string
	Description                string
	Status                     product.Status
	DefaultIterationWorkflowID string
	DefaultTaskWorkflowID      string
	// TaskWorkflowDefaults 是按任务类型的默认任务流模板（kind → workflow_id）；
	// 未覆盖的类型回退 DefaultTaskWorkflowID。
	TaskWorkflowDefaults map[string]string
	CreatedBy            string
	CreatedAt            time.Time
}

const productCols = `id, key, name, description, status, default_iteration_workflow_id, default_task_workflow_id, task_workflow_defaults, created_by, created_at`

func scanProduct(row rowScanner) (ProductRow, error) {
	var p ProductRow
	var defaultsJSON []byte
	err := row.Scan(&p.ID, &p.Key, &p.Name, &p.Description, &p.Status,
		&p.DefaultIterationWorkflowID, &p.DefaultTaskWorkflowID, &defaultsJSON, &p.CreatedBy, &p.CreatedAt)
	if err != nil {
		return ProductRow{}, err
	}
	if len(defaultsJSON) > 0 {
		if err := json.Unmarshal(defaultsJSON, &p.TaskWorkflowDefaults); err != nil {
			return ProductRow{}, fmt.Errorf("store: 产品 %s task_workflow_defaults 非法: %w", p.ID, err)
		}
	}
	return p, nil
}

// InsertProduct 写入产品。
func InsertProduct(ctx context.Context, q Querier, p ProductRow) error {
	defaults := p.TaskWorkflowDefaults
	if defaults == nil {
		defaults = map[string]string{}
	}
	defaultsJSON, err := json.Marshal(defaults)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx,
		`INSERT INTO products (id, key, name, description, status, default_iteration_workflow_id, default_task_workflow_id, task_workflow_defaults, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		p.ID, p.Key, p.Name, p.Description, string(p.Status),
		p.DefaultIterationWorkflowID, p.DefaultTaskWorkflowID, defaultsJSON, p.CreatedBy)
	return err
}

// ProductByID 取产品。
func ProductByID(ctx context.Context, q Querier, id string) (ProductRow, error) {
	return scanProduct(q.QueryRow(ctx, `SELECT `+productCols+` FROM products WHERE id = $1`, id))
}

// ProductByKey 按编号前缀取产品。
func ProductByKey(ctx context.Context, q Querier, key string) (ProductRow, error) {
	return scanProduct(q.QueryRow(ctx, `SELECT `+productCols+` FROM products WHERE key = $1`, key))
}

// ListVisibleProducts 列出可见产品：非 admin 必须经成员关系过滤（不变量 16）。
func ListVisibleProducts(ctx context.Context, q Querier, userID string, isAdmin bool) ([]ProductRow, error) {
	if isAdmin {
		return scanAll(ctx, q, `SELECT `+productCols+` FROM products ORDER BY created_at DESC`, scanProduct)
	}
	return scanAll(ctx, q,
		`SELECT p.id, p.key, p.name, p.description, p.status, p.default_iteration_workflow_id, p.default_task_workflow_id, p.task_workflow_defaults, p.created_by, p.created_at
		 FROM products p
		 JOIN product_members pm ON pm.product_id = p.id AND pm.user_id = $1
		 ORDER BY p.created_at DESC`, scanProduct, userID)
}

// ---- 多产品规模化（M3）：搜索 + 状态过滤 + 分页 ----

// ProductListFilter 是产品列表过滤条件；零值字段不参与过滤。
type ProductListFilter struct {
	Q      string // 名称 / key 模糊匹配
	Status string // active | archived；空 = 全部
	Limit  int
	Offset int
}

// productWhere 组装可见性 + 过滤 WHERE（admin 全量，非 admin 过成员关系）。
func productWhere(userID string, isAdmin bool, f ProductListFilter) (string, []any) {
	conds := []string{}
	args := []any{}
	if !isAdmin {
		conds = append(conds, `EXISTS (SELECT 1 FROM product_members pm
		 WHERE pm.product_id = p.id AND pm.user_id = $1)`)
		args = append(args, userID)
	}
	if f.Q != "" {
		args = append(args, "%"+strings.ToLower(f.Q)+"%")
		conds = append(conds, fmt.Sprintf(`(LOWER(p.name) LIKE $%d OR LOWER(p.key) LIKE $%d)`,
			len(args), len(args)))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		conds = append(conds, fmt.Sprintf(`p.status = $%d`, len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	return where, args
}

func productPage(f ProductListFilter) (int, int) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// ListVisibleProductsPaged 分页列可见产品（M3 规模化：搜索 + 状态过滤 + 计数）。
func ListVisibleProductsPaged(ctx context.Context, q Querier, userID string, isAdmin bool, f ProductListFilter) ([]ProductRow, error) {
	where, args := productWhere(userID, isAdmin, f)
	limit, offset := productPage(f)
	sql := `SELECT ` + productColsRe + ` FROM products p` + where +
		` ORDER BY p.created_at DESC LIMIT ` + strconv.Itoa(limit) + ` OFFSET ` + strconv.Itoa(offset)
	return scanAll(ctx, q, sql, scanProduct, args...)
}

// CountVisibleProducts 统计过滤命中总数（分页用）。
func CountVisibleProducts(ctx context.Context, q Querier, userID string, isAdmin bool, f ProductListFilter) (int, error) {
	where, args := productWhere(userID, isAdmin, f)
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM products p`+where, args...).Scan(&n)
	return n, err
}

// productColsRe 是带表前缀的产品列（分页查询联 WHERE 用）。
const productColsRe = `p.id, p.key, p.name, p.description, p.status,
 p.default_iteration_workflow_id, p.default_task_workflow_id, p.task_workflow_defaults, p.created_by, p.created_at`

// ProductUpdate 是产品可更新字段（nil 即不改）。
type ProductUpdate struct {
	Name                       *string
	Description                *string
	DefaultIterationWorkflowID *string
	DefaultTaskWorkflowID      *string
	TaskWorkflowDefaults       *map[string]string // 非 nil 即整组替换
	Status                     *product.Status
}

// UpdateProduct 更新产品字段并归档。
func UpdateProduct(ctx context.Context, q Querier, id string, u ProductUpdate) error {
	var defaultsJSON any
	if u.TaskWorkflowDefaults != nil {
		b, err := json.Marshal(*u.TaskWorkflowDefaults)
		if err != nil {
			return err
		}
		defaultsJSON = b
	}
	tag, err := q.Exec(ctx,
		`UPDATE products SET
		   name = COALESCE($2, name),
		   description = COALESCE($3, description),
		   default_iteration_workflow_id = COALESCE($4, default_iteration_workflow_id),
		   default_task_workflow_id = COALESCE($5, default_task_workflow_id),
		   task_workflow_defaults = COALESCE($6, task_workflow_defaults),
		   status = COALESCE($7, status),
		   updated_at = now()
		 WHERE id = $1`,
		id, u.Name, u.Description, u.DefaultIterationWorkflowID, u.DefaultTaskWorkflowID,
		defaultsJSON, statusPtr(u.Status))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func statusPtr(s *product.Status) *string {
	if s == nil {
		return nil
	}
	v := string(*s)
	return &v
}

// MemberRow 是产品成员（联了用户展示名）。
type MemberRow struct {
	ProductID   string
	UserID      string
	Username    string
	DisplayName string
	MemberRole  product.MemberRole
	CreatedBy   string
	CreatedAt   time.Time
}

// MemberRoleOf 查成员角色；非成员返回 ok=false（admin 由 app 层另行放行）。
func MemberRoleOf(ctx context.Context, q Querier, productID, userID string) (product.MemberRole, bool, error) {
	var role product.MemberRole
	err := q.QueryRow(ctx,
		`SELECT member_role FROM product_members WHERE product_id = $1 AND user_id = $2`,
		productID, userID).Scan(&role)
	if err == ErrNotFound {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return role, true, nil
}

// UpsertMember 加入或调整成员角色（一个成员在一个产品只有一条记录）。
func UpsertMember(ctx context.Context, q Querier, productID, userID string, role product.MemberRole, createdBy string) error {
	_, err := q.Exec(ctx,
		`INSERT INTO product_members (product_id, user_id, member_role, created_by)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (product_id, user_id) DO UPDATE SET member_role = EXCLUDED.member_role`,
		productID, userID, string(role), createdBy)
	return err
}

// RemoveMember 移除成员。
func RemoveMember(ctx context.Context, q Querier, productID, userID string) error {
	tag, err := q.Exec(ctx, `DELETE FROM product_members WHERE product_id = $1 AND user_id = $2`, productID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListMembers 列产品成员。
func ListMembers(ctx context.Context, q Querier, productID string) ([]MemberRow, error) {
	return scanAll(ctx, q,
		`SELECT pm.product_id, pm.user_id, u.username, u.display_name, pm.member_role, pm.created_by, pm.created_at
		 FROM product_members pm JOIN users u ON u.id = pm.user_id
		 WHERE pm.product_id = $1 ORDER BY pm.created_at`,
		func(row rowScanner) (MemberRow, error) {
			var m MemberRow
			err := row.Scan(&m.ProductID, &m.UserID, &m.Username, &m.DisplayName, &m.MemberRole, &m.CreatedBy, &m.CreatedAt)
			return m, err
		}, productID)
}

// ListMembersLite 列产品成员（含角色），供 EnsureOwnerRetained。
func ListMembersLite(ctx context.Context, q Querier, productID string) ([]product.Member, error) {
	rows, err := ListMembers(ctx, q, productID)
	if err != nil {
		return nil, err
	}
	out := make([]product.Member, 0, len(rows))
	for _, m := range rows {
		out = append(out, product.Member{ProductID: m.ProductID, UserID: m.UserID, MemberRole: m.MemberRole})
	}
	return out, nil
}

// CountOpenTasksByAssigneeInProduct：成员移除前校验——其在产品内的未终结任务数（不变量 17）。
// 任务状态是实例终态投影：无终态即未终结（tasks 不存 status 列）。
func CountOpenTasksByAssigneeInProduct(ctx context.Context, q Querier, productID, userID string) (int64, error) {
	var n int64
	err := q.QueryRow(ctx,
		`SELECT count(*) FROM tasks t
		 WHERE t.product_id = $1 AND t.assignee_id = $2
		   AND NOT EXISTS (
		     SELECT 1 FROM workflow_instances wi
		     WHERE wi.work_item_type = 'task' AND wi.work_item_id = t.id AND wi.terminal_node_id <> '')`,
		productID, userID).Scan(&n)
	return n, err
}

// ModuleRow 是模块存储形态。
type ModuleRow struct {
	ID        string
	ProductID string
	ParentID  *string
	Name      string
	Path      string
	SortOrder int
}

func scanModule(row rowScanner) (ModuleRow, error) {
	var m ModuleRow
	err := row.Scan(&m.ID, &m.ProductID, &m.ParentID, &m.Name, &m.Path, &m.SortOrder)
	return m, err
}

const moduleCols = "id, product_id, parent_id, name, path, sort_order"

// InsertModule 写入模块。
func InsertModule(ctx context.Context, q Querier, m ModuleRow) error {
	_, err := q.Exec(ctx,
		`INSERT INTO modules (id, product_id, parent_id, name, path, sort_order)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		m.ID, m.ProductID, m.ParentID, m.Name, m.Path, m.SortOrder)
	return err
}

// ModuleByID 取模块。
func ModuleByID(ctx context.Context, q Querier, id string) (ModuleRow, error) {
	return scanModule(q.QueryRow(ctx, `SELECT `+moduleCols+` FROM modules WHERE id = $1`, id))
}

// ListModulesByProduct 列产品全部模块（app 层组树）。
func ListModulesByProduct(ctx context.Context, q Querier, productID string) ([]ModuleRow, error) {
	return scanAll(ctx, q,
		`SELECT `+moduleCols+` FROM modules WHERE product_id = $1 ORDER BY path`,
		scanModule, productID)
}

// SiblingNames 同级模块名集合（同级唯一，不变量 1）。
func SiblingNames(ctx context.Context, q Querier, productID string, parentID *string) ([]string, error) {
	return scanAll(ctx, q,
		`SELECT name FROM modules WHERE product_id = $1 AND parent_id IS NOT DISTINCT FROM $2`,
		func(row rowScanner) (string, error) {
			var s string
			return s, row.Scan(&s)
		}, productID, parentID)
}

// MoveModuleSubtree 把子树整体换父并重算物化路径（模块移动时整棵子树重算 path）。
// newParentID 可为 nil（提升为根）。oldPathPrefix → newPrefix 的替换只作用于本产品。
func MoveModuleSubtree(ctx context.Context, q Querier, productID, moduleID string, newParentID *string, newPath string) error {
	tag, err := q.Exec(ctx,
		`UPDATE modules SET parent_id = $3, path = $4 WHERE id = $2 AND product_id = $1`,
		productID, moduleID, newParentID, newPath)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RepathSubtree 把 oldPrefix 下的子模块路径批量替换为 newPrefix。
func RepathSubtree(ctx context.Context, q Querier, productID, oldPrefix, newPrefix string) error {
	_, err := q.Exec(ctx,
		`UPDATE modules SET path = $3 || substr(path, length($2) + 1)
		 WHERE product_id = $1 AND path LIKE $2 || '/%'`,
		productID, oldPrefix, newPrefix)
	return err
}

// ProductIDsOfMember 列成员身份（排除 guest——guest 只读不可审批）的产品 id。
func ProductIDsOfMember(ctx context.Context, q Querier, userID string) ([]string, error) {
	return scanAll(ctx, q,
		`SELECT product_id FROM product_members WHERE user_id = $1 AND member_role <> 'guest'`,
		func(row rowScanner) (string, error) {
			var s string
			return s, row.Scan(&s)
		}, userID)
}

// ListProductMemberUserIDs 列产品内持指定成员角色的启用成员 id。
// 邮件通知按「产品 owner」等受众解析时用（guest 由 roles 过滤天然排除）。
func ListProductMemberUserIDs(ctx context.Context, q Querier, productID string, roles []string) ([]string, error) {
	if len(roles) == 0 {
		return nil, nil
	}
	return scanAll(ctx, q,
		`SELECT pm.user_id FROM product_members pm JOIN users u ON u.id = pm.user_id
		 WHERE pm.product_id = $1 AND pm.member_role = ANY($2) AND u.status = 'active'
		 ORDER BY pm.user_id`,
		scanString, productID, roles)
}

// ReviewerCandidate 是合格评审人候选（产品成员且持所需全局角色）。
type ReviewerCandidate struct {
	UserID      string
	DisplayName string
}

// ListEligibleReviewers 列产品内持 roles 之一的非 guest 成员（TEXT[] 数组重叠）。
func ListEligibleReviewers(ctx context.Context, q Querier, productID string, roles []string) ([]ReviewerCandidate, error) {
	return scanAll(ctx, q,
		`SELECT u.id, u.display_name
		 FROM product_members pm JOIN users u ON u.id = pm.user_id
		 WHERE pm.product_id = $1 AND pm.member_role <> 'guest' AND u.status = 'active'
		   AND u.roles && $2::text[]`,
		func(row rowScanner) (ReviewerCandidate, error) {
			var r ReviewerCandidate
			return r, row.Scan(&r.UserID, &r.DisplayName)
		}, productID, roles)
}

// DocumentRow 是轻量文档存储形态。
type DocumentRow struct {
	ID        string
	OwnerType string
	OwnerID   string
	Content   string
	Version   int
	UpdatedAt time.Time
}

// GetDocument 取文档；无则返回 ErrNotFound。
func GetDocument(ctx context.Context, q Querier, ownerType, ownerID string) (DocumentRow, error) {
	var d DocumentRow
	err := q.QueryRow(ctx,
		`SELECT id, owner_type, owner_id, content, version, updated_at
		 FROM documents WHERE owner_type = $1 AND owner_id = $2`, ownerType, ownerID).
		Scan(&d.ID, &d.OwnerType, &d.OwnerID, &d.Content, &d.Version, &d.UpdatedAt)
	return d, err
}

// UpsertDocument 保存文档：版本号乐观校验，保存即升 version。
// expectedVersion=0 表示首次创建；不匹配返回 STATE_CONFLICT 语义错误（由 app 转换）。
func UpsertDocument(ctx context.Context, q Querier, id, ownerType, ownerID, content string, expectedVersion int) (int, error) {
	if expectedVersion == 0 {
		var version int
		err := q.QueryRow(ctx,
			`INSERT INTO documents (id, owner_type, owner_id, content) VALUES ($1, $2, $3, $4)
			 ON CONFLICT (owner_type, owner_id) DO UPDATE SET
			   content = EXCLUDED.content, version = documents.version + 1, updated_at = now()
			 RETURNING version`,
			id, ownerType, ownerID, content).Scan(&version)
		return version, err
	}
	var version int
	err := q.QueryRow(ctx,
		`UPDATE documents SET content = $3, version = version + 1, updated_at = now()
		 WHERE owner_type = $1 AND owner_id = $2 AND version = $4
		 RETURNING version`,
		ownerType, ownerID, content, expectedVersion).Scan(&version)
	if err == ErrNotFound {
		return 0, ErrVersionConflict
	}
	return version, err
}

// ErrVersionConflict 文档版本乐观锁冲突。
var ErrVersionConflict = errVersionConflict{}

type errVersionConflict struct{}

func (errVersionConflict) Error() string { return "store: 文档版本已被并发修改" }
