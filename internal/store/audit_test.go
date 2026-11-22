package store

// audit_test.go：审计检索 WHERE 构造（纯函数）——占位符编号与参数一一对应，
// 产品过滤条件复用同一参数位（PG 允许 $n 重复引用）。

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestAuditWherePlaceholders(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := AuditFilter{
		ActorID:     "usr_1",
		Action:      "gate.review",
		SubjectType: "task",
		SubjectID:   "tsk_9",
		ProductID:   "prd_1",
		From:        &from,
	}
	where, args := auditWhere(f)

	if len(args) != 6 {
		t.Fatalf("应有 6 个参数（产品过滤复用同一位），实际 %d: %v", len(args), args)
	}
	for i := range args {
		if !strings.Contains(where, fmt.Sprintf("$%d", i+1)) {
			t.Fatalf("WHERE 缺少占位符 $%d: %s", i+1, where)
		}
	}
	// 产品条件四处引用同一参数位 $6，不得出现 MISSING。
	if strings.Contains(where, "MISSING") || strings.Contains(where, "%!") {
		t.Fatalf("占位符构造错误: %s", where)
	}
	if strings.Count(where, "$6") != 4 {
		t.Fatalf("产品过滤应复用 $6 四次，实际 %d 次: %s", strings.Count(where, "$6"), where)
	}
}

func TestAuditWhereEmpty(t *testing.T) {
	where, args := auditWhere(AuditFilter{})
	if where != "" || len(args) != 0 {
		t.Fatalf("空过滤应无 WHERE，实际 %q %v", where, args)
	}
}
