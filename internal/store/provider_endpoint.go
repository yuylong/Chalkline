package store

// provider_endpoint.go：全局 Provider 连接目录（docs/domain/integrations.md「连接目录」）。
// admin 维护多套外部系统地址（多个 git / svn / redmine / outline），迭代绑定时
// 从目录选择或自行填写。只存地址与展示名，不存任何凭据（红线 8）。

import (
	"context"
	"time"
)

// ProviderEndpointRow 是 provider_endpoints 行。
type ProviderEndpointRow struct {
	ID        string
	Kind      string
	Label     string
	BaseURL   string
	Note      string
	Enabled   bool
	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}

const providerEndpointCols = `id, kind, label, base_url, note, enabled, created_by, created_at, updated_at`

func scanProviderEndpoint(row rowScanner) (ProviderEndpointRow, error) {
	var e ProviderEndpointRow
	err := row.Scan(&e.ID, &e.Kind, &e.Label, &e.BaseURL, &e.Note,
		&e.Enabled, &e.CreatedBy, &e.CreatedAt, &e.UpdatedAt)
	return e, err
}

// ListProviderEndpoints 全部连接目录（管理台用，按 kind 与创建顺序）。
func ListProviderEndpoints(ctx context.Context, q Querier) ([]ProviderEndpointRow, error) {
	return scanAll(ctx, q,
		`SELECT `+providerEndpointCols+` FROM provider_endpoints
		 ORDER BY kind, created_at, id`,
		scanProviderEndpoint)
}

// ListEnabledProviderEndpoints 已启用的连接目录（成员选型用）。
func ListEnabledProviderEndpoints(ctx context.Context, q Querier) ([]ProviderEndpointRow, error) {
	return scanAll(ctx, q,
		`SELECT `+providerEndpointCols+` FROM provider_endpoints
		 WHERE enabled ORDER BY kind, created_at, id`,
		scanProviderEndpoint)
}

// ProviderEndpointByID 取单条目录。
func ProviderEndpointByID(ctx context.Context, q Querier, id string) (ProviderEndpointRow, error) {
	return scanProviderEndpoint(q.QueryRow(ctx,
		`SELECT `+providerEndpointCols+` FROM provider_endpoints WHERE id = $1`, id))
}

// InsertProviderEndpoint 新增目录行。
func InsertProviderEndpoint(ctx context.Context, q Querier, e ProviderEndpointRow) error {
	_, err := q.Exec(ctx,
		`INSERT INTO provider_endpoints (id, kind, label, base_url, note, enabled, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		e.ID, e.Kind, e.Label, e.BaseURL, e.Note, e.Enabled, e.CreatedBy)
	return err
}

// ProviderEndpointUpdate 是目录更新入参（nil 即不改）。
type ProviderEndpointUpdate struct {
	Label   *string
	BaseURL *string
	Note    *string
	Enabled *bool
}

// UpdateProviderEndpoint 改目录行；RowsAffected=0 即不存在。
func UpdateProviderEndpoint(ctx context.Context, q Querier, id string, u ProviderEndpointUpdate) error {
	tag, err := q.Exec(ctx,
		`UPDATE provider_endpoints SET
		   label = COALESCE($2, label),
		   base_url = COALESCE($3, base_url),
		   note = COALESCE($4, note),
		   enabled = COALESCE($5, enabled),
		   updated_at = now()
		 WHERE id = $1`,
		id, u.Label, u.BaseURL, u.Note, u.Enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteProviderEndpoint 删目录行。
func DeleteProviderEndpoint(ctx context.Context, q Querier, id string) error {
	tag, err := q.Exec(ctx, `DELETE FROM provider_endpoints WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
