package store

// audit.go：审计写入（不变量 13：所有写操作在事务内携带 actor_id 写审计）。

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// AuditLog 是审计记录的写入形态。
type AuditLog struct {
	ActorID     string
	Action      string
	SubjectType string
	SubjectID   string
	Payload     map[string]any
}

// InsertAudit 在当前事务（q）内写审计。
func InsertAudit(ctx context.Context, q Querier, log AuditLog) error {
	payload, err := json.Marshal(log.Payload)
	if err != nil {
		payload = []byte(`{}`)
	}
	_, err = q.Exec(ctx,
		`INSERT INTO audit_logs (actor_id, action, subject_type, subject_id, payload)
		 VALUES ($1, $2, $3, $4, $5)`,
		log.ActorID, log.Action, log.SubjectType, log.SubjectID, payload)
	return err
}

// ---- 审计留痕检索（M3，docs/modules/platform.md 开放问题收敛）----

// AuditFilter 是审计检索条件；零值字段不参与过滤。
// ProductID 非空时按产品归属过滤（task/iteration/module 联表 + product 直配），
// 保证产品内开放、跨产品隔离（不变量 16）。
type AuditFilter struct {
	ActorID     string
	Action      string
	SubjectType string
	SubjectID   string
	ProductID   string
	From        *time.Time
	To          *time.Time
	Limit       int
	Offset      int
}

// AuditRow 是审计检索结果行（联用户表补展示名，格式「显示名（用户名）」；用户已删时回退 actor_id）。
type AuditRow struct {
	ID          int64
	ActorID     string
	ActorName   string
	Action      string
	SubjectType string
	SubjectID   string
	Payload     map[string]any
	CreatedAt   time.Time
}

// auditWhere 组装检索 WHERE 片段与参数（纯函数，可单测）。
func auditWhere(f AuditFilter) (string, []any) {
	conds := []string{}
	args := []any{}
	add := func(cond string, arg any) {
		args = append(args, arg)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}
	if f.ActorID != "" {
		add("a.actor_id = $%d", f.ActorID)
	}
	if f.Action != "" {
		add("a.action = $%d", f.Action)
	}
	if f.SubjectType != "" {
		add("a.subject_type = $%d", f.SubjectType)
	}
	if f.SubjectID != "" {
		add("a.subject_id = $%d", f.SubjectID)
	}
	if f.From != nil {
		add("a.created_at >= $%d", *f.From)
	}
	if f.To != nil {
		add("a.created_at < $%d", *f.To)
	}
	if f.ProductID != "" {
		args = append(args, f.ProductID)
		n := len(args)
		conds = append(conds, fmt.Sprintf(`(a.subject_type = 'product' AND a.subject_id = $%[1]d)
		     OR (a.subject_type = 'iteration' AND EXISTS (
		           SELECT 1 FROM iterations i WHERE i.id = a.subject_id AND i.product_id = $%[1]d))
		     OR (a.subject_type = 'task' AND EXISTS (
		           SELECT 1 FROM tasks t WHERE t.id = a.subject_id AND t.product_id = $%[1]d))
		     OR (a.subject_type = 'module' AND EXISTS (
		           SELECT 1 FROM modules m WHERE m.id = a.subject_id AND m.product_id = $%[1]d))`, n))
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	return where, args
}

func auditLimitOffset(f AuditFilter) (int, int) {
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

// ListAuditLogs 检索审计（时间倒序分页）。
func ListAuditLogs(ctx context.Context, q Querier, f AuditFilter) ([]AuditRow, error) {
	where, args := auditWhere(f)
	limit, offset := auditLimitOffset(f)
	sql := `SELECT a.id, a.actor_id,
	               COALESCE(NULLIF(u.display_name, '') || '（' || u.username || '）', u.username, a.actor_id),
	               a.action, a.subject_type, a.subject_id, a.payload, a.created_at
	          FROM audit_logs a LEFT JOIN users u ON u.id = a.actor_id` +
		where + ` ORDER BY a.id DESC LIMIT ` + strconv.Itoa(limit) + ` OFFSET ` + strconv.Itoa(offset)
	return scanAll(ctx, q, sql, func(row rowScanner) (AuditRow, error) {
		var r AuditRow
		var payload []byte
		if err := row.Scan(&r.ID, &r.ActorID, &r.ActorName, &r.Action,
			&r.SubjectType, &r.SubjectID, &payload, &r.CreatedAt); err != nil {
			return r, err
		}
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &r.Payload)
		}
		return r, nil
	}, args...)
}

// CountAuditLogs 统计检索命中总数（分页用）。
func CountAuditLogs(ctx context.Context, q Querier, f AuditFilter) (int, error) {
	where, args := auditWhere(f)
	var n int
	err := q.QueryRow(ctx,
		`SELECT count(*) FROM audit_logs a`+where, args...).Scan(&n)
	return n, err
}
