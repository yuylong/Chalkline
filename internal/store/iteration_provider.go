package store

// iteration_provider.go：迭代级 provider 绑定（docs/domain/integrations.md「迭代 Provider」）。
// 连接信息随迭代固化；凭据永不进库（红线 8）。

import (
	"context"
	"time"
)

// IterationProviderRow 是 iteration_providers 行。
type IterationProviderRow struct {
	ID          string
	IterationID string
	Category    string
	Provider    string
	BaseURL     string
	Target      string
	DirTemplate string
	Branch      string
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const iterationProviderCols = `id, iteration_id, category, provider, base_url, target, dir_template, branch, created_by, created_at, updated_at`

func scanIterationProvider(row rowScanner) (IterationProviderRow, error) {
	var r IterationProviderRow
	err := row.Scan(&r.ID, &r.IterationID, &r.Category, &r.Provider, &r.BaseURL,
		&r.Target, &r.DirTemplate, &r.Branch, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

// ListIterationProviders 列迭代的全部 provider 绑定（成果物在前，按分类与创建顺序）。
func ListIterationProviders(ctx context.Context, q Querier, iterationID string) ([]IterationProviderRow, error) {
	return scanAll(ctx, q,
		`SELECT `+iterationProviderCols+` FROM iteration_providers
		 WHERE iteration_id = $1
		 ORDER BY CASE category WHEN 'artifact' THEN 0 WHEN 'process' THEN 1 ELSE 2 END, provider, target, id`,
		scanIterationProvider, iterationID)
}

// ReplaceIterationProviders 整组替换迭代绑定（调用方负责在同一事务内做冻结校验）。
func ReplaceIterationProviders(ctx context.Context, q Querier, iterationID string, rows []IterationProviderRow) error {
	if _, err := q.Exec(ctx, `DELETE FROM iteration_providers WHERE iteration_id = $1`, iterationID); err != nil {
		return err
	}
	for _, r := range rows {
		if _, err := q.Exec(ctx,
			`INSERT INTO iteration_providers
			   (id, iteration_id, category, provider, base_url, target, dir_template, branch, created_by)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			r.ID, iterationID, r.Category, r.Provider, r.BaseURL, r.Target, r.DirTemplate, r.Branch, r.CreatedBy); err != nil {
			return err
		}
	}
	return nil
}

// IterationArtifactsSubmitted 报告迭代（含其下任务）是否已提交过成果物——绑定冻结判据。
func IterationArtifactsSubmitted(ctx context.Context, q Querier, iterationID string) (bool, error) {
	var exists bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM artifacts a
		  JOIN workflow_instances wi ON wi.id = a.instance_id
		  WHERE (wi.work_item_type = 'iteration' AND wi.work_item_id = $1)
		     OR (wi.work_item_type = 'task'
		         AND wi.work_item_id IN (SELECT id FROM tasks WHERE iteration_id = $1))
		)`, iterationID).Scan(&exists)
	return exists, err
}

// SetArtifactDispatch 记录成果物分发结果：外部 provider 只保留摘要与链接，正文不复制进平台。
func SetArtifactDispatch(ctx context.Context, q Querier, artifactID, provider, url, externalID string) error {
	_, err := q.Exec(ctx, `
		UPDATE artifacts SET provider = $2, url = $3, external_id = $4, content = ''
		WHERE id = $1`, artifactID, provider, url, externalID)
	return err
}
