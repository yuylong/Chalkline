package store

// provider_config.go：集成 provider 的可视化配置（docs/modules/integration.md M3）。
// 只存开关与 baseURL；凭据仍只从环境变量读取，不落库（红线 8）。

import (
	"context"
	"time"
)

// ProviderConfigRow 是 integration_provider_configs 行。
type ProviderConfigRow struct {
	Provider  string
	Enabled   bool
	BaseURL   string
	UpdatedBy string
	UpdatedAt time.Time
}

const providerConfigCols = `provider, enabled, base_url, updated_by, updated_at`

func scanProviderConfig(row rowScanner) (ProviderConfigRow, error) {
	var c ProviderConfigRow
	err := row.Scan(&c.Provider, &c.Enabled, &c.BaseURL, &c.UpdatedBy, &c.UpdatedAt)
	return c, err
}

// ListProviderConfigs 全部 provider 配置行（管理台用，按 provider 排序）。
func ListProviderConfigs(ctx context.Context, q Querier) ([]ProviderConfigRow, error) {
	return scanAll(ctx, q,
		`SELECT `+providerConfigCols+` FROM integration_provider_configs ORDER BY provider`,
		scanProviderConfig)
}

// ProviderConfigByKind 取单个 provider 配置；ErrNotFound 表示使用环境变量默认值。
func ProviderConfigByKind(ctx context.Context, q Querier, provider string) (ProviderConfigRow, error) {
	return scanProviderConfig(q.QueryRow(ctx,
		`SELECT `+providerConfigCols+` FROM integration_provider_configs WHERE provider = $1`, provider))
}

// UpsertProviderConfig 写入（或覆盖）provider 配置。
func UpsertProviderConfig(ctx context.Context, q Querier, provider string, enabled bool, baseURL, updatedBy string) error {
	_, err := q.Exec(ctx,
		`INSERT INTO integration_provider_configs (provider, enabled, base_url, updated_by, updated_at)
		 VALUES ($1, $2, $3, $4, now())
		 ON CONFLICT (provider) DO UPDATE SET
		   enabled = EXCLUDED.enabled,
		   base_url = EXCLUDED.base_url,
		   updated_by = EXCLUDED.updated_by,
		   updated_at = now()`,
		provider, enabled, baseURL, updatedBy)
	return err
}
