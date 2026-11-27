package store

// user_provider_credential.go：成员个人在外部系统的写权限凭据
// （docs/domain/integrations.md「个人凭据」）。
// 分发成果物 / 流程记录时优先以提交人本人的 Key 出站，外部系统可见「对应员工提交」；
// 只存 AES-GCM 密文（platform.Seal），API 永不回显（红线 8 的管理台例外，与 SMTP 同源）。

import (
	"context"
	"time"
)

// UserProviderCredentialRow 是 user_provider_credentials 行。
type UserProviderCredentialRow struct {
	ID           string
	UserID       string
	Provider     string
	BaseURL      string
	AuthType     string
	AuthUsername string
	SecretEnc    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

const userProviderCredentialCols = `id, user_id, provider, base_url, auth_type, auth_username, secret_enc, created_at, updated_at`

func scanUserProviderCredential(row rowScanner) (UserProviderCredentialRow, error) {
	var c UserProviderCredentialRow
	err := row.Scan(&c.ID, &c.UserID, &c.Provider, &c.BaseURL,
		&c.AuthType, &c.AuthUsername, &c.SecretEnc, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

// ListUserProviderCredentials 列成员本人的全部个人凭据（只报是否已配置）。
func ListUserProviderCredentials(ctx context.Context, q Querier, userID string) ([]UserProviderCredentialRow, error) {
	return scanAll(ctx, q,
		`SELECT `+userProviderCredentialCols+` FROM user_provider_credentials
		 WHERE user_id = $1 ORDER BY provider, base_url, created_at`,
		scanUserProviderCredential, userID)
}

// UserProviderCredential 取单条个人凭据（provider + baseURL 定位；baseURL 空 = 默认地址）。
func UserProviderCredential(ctx context.Context, q Querier, userID, provider, baseURL string) (UserProviderCredentialRow, error) {
	return scanUserProviderCredential(q.QueryRow(ctx,
		`SELECT `+userProviderCredentialCols+`
		 FROM user_provider_credentials WHERE user_id = $1 AND provider = $2 AND base_url = $3`,
		userID, provider, baseURL))
}

// UpsertUserProviderCredential 写入（或覆盖）个人凭据。
// secretEnc 为空表示「保留库内已有密文」（轮换时可只改用户名）。
func UpsertUserProviderCredential(ctx context.Context, q Querier, c UserProviderCredentialRow) error {
	_, err := q.Exec(ctx,
		`INSERT INTO user_provider_credentials
		   (id, user_id, provider, base_url, auth_type, auth_username, secret_enc)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (user_id, provider, base_url) DO UPDATE SET
		   auth_type = EXCLUDED.auth_type,
		   auth_username = EXCLUDED.auth_username,
		   secret_enc = CASE
		     WHEN EXCLUDED.auth_type = '' THEN ''
		     WHEN EXCLUDED.secret_enc = '' THEN user_provider_credentials.secret_enc
		     ELSE EXCLUDED.secret_enc
		   END,
		   updated_at = now()`,
		c.ID, c.UserID, c.Provider, c.BaseURL, c.AuthType, c.AuthUsername, c.SecretEnc)
	return err
}

// DeleteUserProviderCredential 删除个人凭据行。
func DeleteUserProviderCredential(ctx context.Context, q Querier, userID, provider, baseURL string) error {
	tag, err := q.Exec(ctx,
		`DELETE FROM user_provider_credentials WHERE user_id = $1 AND provider = $2 AND base_url = $3`,
		userID, provider, baseURL)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
