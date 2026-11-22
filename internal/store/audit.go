package store

// audit.go：审计写入（不变量 13：所有写操作在事务内携带 actor_id 写审计）。

import (
	"context"
	"encoding/json"
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
