package store

// iteration_provider.go：迭代级 provider 绑定（docs/domain/integrations.md「迭代 Provider」）。
// 连接信息随迭代固化；凭据只存 AES-GCM 密文（红线 8 的唯一例外，与 SMTP 密码同源）。

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
	// AuthType：'' | token | basic；AuthSecretEnc 为空表示未配置（回退环境变量）。
	AuthType      string
	AuthUsername  string
	AuthSecretEnc string
	CreatedBy     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

const iterationProviderCols = `id, iteration_id, category, provider, base_url, target, dir_template, branch,
	auth_type, auth_username, auth_secret_enc, created_by, created_at, updated_at`

func scanIterationProvider(row rowScanner) (IterationProviderRow, error) {
	var r IterationProviderRow
	err := row.Scan(&r.ID, &r.IterationID, &r.Category, &r.Provider, &r.BaseURL,
		&r.Target, &r.DirTemplate, &r.Branch,
		&r.AuthType, &r.AuthUsername, &r.AuthSecretEnc,
		&r.CreatedBy, &r.CreatedAt, &r.UpdatedAt)
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

// IterationProviderByID 取单条绑定（凭据更新用，必须校验 iteration_id 归属）。
func IterationProviderByID(ctx context.Context, q Querier, iterationID, id string) (IterationProviderRow, error) {
	return scanIterationProvider(q.QueryRow(ctx,
		`SELECT `+iterationProviderCols+` FROM iteration_providers WHERE iteration_id = $1 AND id = $2`,
		iterationID, id))
}

// providerKey 是绑定唯一键（与表上的 UNIQUE 约束一致）。
func providerKey(category, provider, target string) string {
	return category + "\x00" + provider + "\x00" + target
}

// SyncIterationProviders 按唯一键同步迭代绑定：
//   - 新增/更新连接信息；
//   - 不再存在的绑定删除（其凭据随行删除）；
//   - 已存在绑定在未提交新密文时**保留原密文**（避免每次保存结构都把 Key 抹掉）。
func SyncIterationProviders(ctx context.Context, q Querier, iterationID string, rows []IterationProviderRow) error {
	existing, err := ListIterationProviders(ctx, q, iterationID)
	if err != nil {
		return err
	}
	keep := make(map[string]bool, len(rows))
	for _, r := range rows {
		keep[providerKey(r.Category, r.Provider, r.Target)] = true
		if _, err := q.Exec(ctx, `
			INSERT INTO iteration_providers
			  (id, iteration_id, category, provider, base_url, target, dir_template, branch,
			   auth_type, auth_username, auth_secret_enc, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT (iteration_id, category, provider, target) DO UPDATE SET
			  base_url = EXCLUDED.base_url,
			  dir_template = EXCLUDED.dir_template,
			  branch = EXCLUDED.branch,
			  auth_type = EXCLUDED.auth_type,
			  auth_username = EXCLUDED.auth_username,
			  auth_secret_enc = CASE
			    WHEN EXCLUDED.auth_type = '' THEN ''
			    WHEN EXCLUDED.auth_secret_enc = '' THEN iteration_providers.auth_secret_enc
			    ELSE EXCLUDED.auth_secret_enc
			  END,
			  updated_at = now()`,
			r.ID, iterationID, r.Category, r.Provider, r.BaseURL, r.Target, r.DirTemplate, r.Branch,
			r.AuthType, r.AuthUsername, r.AuthSecretEnc, r.CreatedBy); err != nil {
			return err
		}
	}
	for _, old := range existing {
		if keep[providerKey(old.Category, old.Provider, old.Target)] {
			continue
		}
		if _, err := q.Exec(ctx, `DELETE FROM iteration_providers WHERE id = $1`, old.ID); err != nil {
			return err
		}
	}
	return nil
}

// UpdateIterationProviderCredential 只更新认证方式与凭据（不受绑定冻结限制）。
// secretEnc 为空表示「保留库内已有密文」（轮换时可只改用户名）。
func UpdateIterationProviderCredential(ctx context.Context, q Querier, id, authType, username, secretEnc string) error {
	_, err := q.Exec(ctx, `
		UPDATE iteration_providers
		SET auth_type = $2,
		    auth_username = $3,
		    auth_secret_enc = CASE
		      WHEN $2 = '' THEN ''
		      WHEN $4 = '' THEN auth_secret_enc
		      ELSE $4
		    END,
		    updated_at = now()
		WHERE id = $1`, id, authType, username, secretEnc)
	return err
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
