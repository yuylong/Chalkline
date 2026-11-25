package store

// integration_sync.go：成果物分发与流程记录的回执（documents/integrations.md「迭代 Provider」）。
// 只存摘要与链接，不复制外部内容；唯一键提供幂等，避免重复建票/重复推送。

import (
	"context"
	"errors"
	"time"
)

// IntegrationSyncRow 是 integration_syncs 行。
type IntegrationSyncRow struct {
	ID          int64
	IterationID string
	Category    string // artifact | process
	Provider    string
	SubjectType string // artifact | node | task | iteration
	SubjectID   string
	Action      string // push | ticket | comment
	Status      string // pending | ok | failed | skipped
	ExternalID  string
	URL         string
	Summary     string
	Error       string
	Attempts    int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const integrationSyncCols = `id, iteration_id, category, provider, subject_type, subject_id, action,
	status, external_id, url, summary, error, attempts, created_at, updated_at`

func scanIntegrationSync(row rowScanner) (IntegrationSyncRow, error) {
	var r IntegrationSyncRow
	err := row.Scan(&r.ID, &r.IterationID, &r.Category, &r.Provider, &r.SubjectType,
		&r.SubjectID, &r.Action, &r.Status, &r.ExternalID, &r.URL, &r.Summary, &r.Error,
		&r.Attempts, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

// ClaimIntegrationSync 抢占一次分发：已成功（ok）的记录不再重复推送；
// 其余状态递增尝试次数并重置为 pending。claimed=false 表示无需再推。
func ClaimIntegrationSync(ctx context.Context, q Querier, r IntegrationSyncRow) (id int64, claimed bool, err error) {
	err = q.QueryRow(ctx, `
		INSERT INTO integration_syncs
		  (iteration_id, category, provider, subject_type, subject_id, action, status, attempts)
		VALUES ($1, $2, $3, $4, $5, $6, 'pending', 1)
		ON CONFLICT (iteration_id, category, provider, subject_type, subject_id, action)
		DO UPDATE SET attempts = integration_syncs.attempts + 1, status = 'pending', updated_at = now()
		WHERE integration_syncs.status <> 'ok'
		RETURNING id`,
		r.IterationID, r.Category, r.Provider, r.SubjectType, r.SubjectID, r.Action).Scan(&id)
	if errors.Is(err, ErrNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

// FinishIntegrationSync 写入分发结果（ok / failed / skipped）。
func FinishIntegrationSync(ctx context.Context, q Querier, id int64,
	status, externalID, url, summary, errMsg string) error {
	_, err := q.Exec(ctx, `
		UPDATE integration_syncs
		SET status = $2, external_id = $3, url = $4, summary = $5, error = $6, updated_at = now()
		WHERE id = $1`, id, status, externalID, url, summary, errMsg)
	return err
}

// ListIntegrationSyncsByIteration 列迭代的全部分发回执（控制台展示用）。
func ListIntegrationSyncsByIteration(ctx context.Context, q Querier, iterationID string) ([]IntegrationSyncRow, error) {
	return scanAll(ctx, q, `SELECT `+integrationSyncCols+` FROM integration_syncs
		WHERE iteration_id = $1 ORDER BY id`, scanIntegrationSync, iterationID)
}

// ExternalLinkBySubject 取某个主体在某 provider 上的关联（流程记录票复用）。
func ExternalLinkBySubject(ctx context.Context, q Querier, subjectType, subjectID, provider string) (ExternalLinkRow, error) {
	var r ExternalLinkRow
	err := q.QueryRow(ctx, `
		SELECT id, subject_type, subject_id, provider, external_id, url, created_by, created_at
		FROM external_links
		WHERE subject_type = $1 AND subject_id = $2 AND provider = $3
		ORDER BY id LIMIT 1`, subjectType, subjectID, provider,
	).Scan(&r.ID, &r.SubjectType, &r.SubjectID, &r.Provider,
		&r.ExternalID, &r.URL, &r.CreatedBy, &r.CreatedAt)
	return r, err
}
