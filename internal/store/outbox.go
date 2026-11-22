package store

// outbox.go：事件队列查询与状态推进（docs/modules/integration.md）。
// 本文件不做判定，只做行↔结构体转换与状态机落库（pending → sent / failed）。

import (
	"context"
	"encoding/json"
	"time"
)

// OutboxRow 是 outbox_events 行。
type OutboxRow struct {
	ID          int64
	EventType   string
	SubjectType string
	SubjectID   string
	Payload     map[string]any
	Status      string
	Attempts    int
	LastError   string
	CreatedAt   time.Time
}

const outboxColumns = `id, event_type, subject_type, subject_id, payload, status, attempts, last_error, created_at`

func scanOutbox(row rowScanner) (OutboxRow, error) {
	var r OutboxRow
	var payload []byte
	var lastErr *string
	err := row.Scan(&r.ID, &r.EventType, &r.SubjectType, &r.SubjectID, &payload,
		&r.Status, &r.Attempts, &lastErr, &r.CreatedAt)
	if err != nil {
		return r, err
	}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &r.Payload)
	}
	if lastErr != nil {
		r.LastError = *lastErr
	}
	return r, nil
}

// ListDueOutbox 取一轮 due 事件：pending 且已到 next_attempt_at。
// Queue 接口（internal/integration/notify）的生产实现。
func (db *DB) ListDueOutbox(ctx context.Context, limit int) ([]OutboxRow, error) {
	return scanAll(ctx, db.Q(),
		`SELECT `+outboxColumns+` FROM outbox_events
		 WHERE status = 'pending' AND next_attempt_at <= now()
		 ORDER BY id LIMIT $1`, scanOutbox, limit)
}

// MarkOutboxSent 投递成功：置 sent 与 sent_at。
func (db *DB) MarkOutboxSent(ctx context.Context, id int64) error {
	_, err := db.Q().Exec(ctx,
		`UPDATE outbox_events SET status = 'sent', sent_at = now(), last_error = NULL WHERE id = $1`, id)
	return err
}

// FailOutbox 记一次投递失败：attempts+1，未达上限时按 backoff 延后重试；
// backoff 为 0 时直接标 failed（调用方在达到上限时传 0）。
func (db *DB) FailOutbox(ctx context.Context, id int64, lastErr string, backoff time.Duration) error {
	if backoff > 0 {
		_, err := db.Q().Exec(ctx, `
			UPDATE outbox_events
			SET attempts = attempts + 1, last_error = $2, next_attempt_at = now() + $3::interval
			WHERE id = $1`, id, lastErr, backoff.String())
		return err
	}
	_, err := db.Q().Exec(ctx, `
		UPDATE outbox_events
		SET attempts = attempts + 1, last_error = $2, status = 'failed'
		WHERE id = $1`, id, lastErr)
	return err
}

// ---- external_links（model.md「ExternalLink」，M2 建表供后续接入）----

// ExternalLinkRow 是工作项与外部系统的稳定关联。
type ExternalLinkRow struct {
	ID          int64
	SubjectType string // task | iteration | product ...
	SubjectID   string
	Provider    string
	ExternalID  string
	URL         string
	CreatedBy   string
	CreatedAt   time.Time
}

// UpsertExternalLink 幂等登记关联（同 subject + provider + external_id 只更新 URL）。
func UpsertExternalLink(ctx context.Context, q Querier, r ExternalLinkRow) (ExternalLinkRow, error) {
	var out ExternalLinkRow
	err := q.QueryRow(ctx, `
		INSERT INTO external_links (subject_type, subject_id, provider, external_id, url, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (subject_type, subject_id, provider, external_id)
		DO UPDATE SET url = EXCLUDED.url
		RETURNING id, subject_type, subject_id, provider, external_id, url, created_by, created_at`,
		r.SubjectType, r.SubjectID, r.Provider, r.ExternalID, r.URL, r.CreatedBy,
	).Scan(&out.ID, &out.SubjectType, &out.SubjectID, &out.Provider,
		&out.ExternalID, &out.URL, &out.CreatedBy, &out.CreatedAt)
	return out, err
}

// ListExternalLinks 列某工作项的全部外部关联。
func ListExternalLinks(ctx context.Context, q Querier, subjectType, subjectID string) ([]ExternalLinkRow, error) {
	return scanAll(ctx, q, `
		SELECT id, subject_type, subject_id, provider, external_id, url, created_by, created_at
		FROM external_links WHERE subject_type = $1 AND subject_id = $2
		ORDER BY id`, func(row rowScanner) (ExternalLinkRow, error) {
		var r ExternalLinkRow
		err := row.Scan(&r.ID, &r.SubjectType, &r.SubjectID, &r.Provider,
			&r.ExternalID, &r.URL, &r.CreatedBy, &r.CreatedAt)
		return r, err
	}, subjectType, subjectID)
}
