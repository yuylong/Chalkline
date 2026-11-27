package store

// ldap_config.go：LDAP 认证服务器配置（docs/modules/identity.md「LDAP 认证」）。
// 单行配置（config_key='default'）；绑定密码只存 AES-GCM 密文（platform.Seal），
// 明文与密钥都不经本层流转。未写入行 = LDAP 未配置（登录只走本地密码）。

import (
	"context"
	"time"
)

// LDAPConfigKey 是唯一配置行的键。
const LDAPConfigKey = "default"

// LDAPConfigRow 是 ldap_configs 行。
type LDAPConfigRow struct {
	Enabled          bool
	Host             string
	Port             int
	TLSMode          string // none / starttls / tls
	TLSSkipVerify    bool
	BindDN           string
	BindPasswordEnc  string // AES-GCM 密文；空 = 未配置（匿名绑定）
	UserSearchBase   string
	UserSearchFilter string // 含 %s 占位登录名，如 (uid=%s)
	TimeoutSeconds   int
	UpdatedBy        string
	UpdatedAt        time.Time
}

const ldapConfigCols = `enabled, host, port, tls_mode, tls_skip_verify, bind_dn, bind_password_enc,
	user_search_base, user_search_filter, timeout_seconds, updated_by, updated_at`

func scanLDAPConfig(row rowScanner) (LDAPConfigRow, error) {
	var c LDAPConfigRow
	err := row.Scan(&c.Enabled, &c.Host, &c.Port, &c.TLSMode, &c.TLSSkipVerify, &c.BindDN,
		&c.BindPasswordEnc, &c.UserSearchBase, &c.UserSearchFilter, &c.TimeoutSeconds,
		&c.UpdatedBy, &c.UpdatedAt)
	return c, err
}

// LDAPConfig 取唯一配置行；ErrNotFound 表示尚未在管理台保存过（LDAP 未启用）。
func LDAPConfig(ctx context.Context, q Querier) (LDAPConfigRow, error) {
	return scanLDAPConfig(q.QueryRow(ctx,
		`SELECT `+ldapConfigCols+` FROM ldap_configs WHERE config_key = $1`, LDAPConfigKey))
}

// UpsertLDAPConfig 写入（或覆盖）配置行。
func UpsertLDAPConfig(ctx context.Context, q Querier, row LDAPConfigRow) error {
	_, err := q.Exec(ctx,
		`INSERT INTO ldap_configs (config_key, enabled, host, port, tls_mode, tls_skip_verify,
		   bind_dn, bind_password_enc, user_search_base, user_search_filter, timeout_seconds, updated_by, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, now())
		 ON CONFLICT (config_key) DO UPDATE SET
		   enabled = EXCLUDED.enabled,
		   host = EXCLUDED.host,
		   port = EXCLUDED.port,
		   tls_mode = EXCLUDED.tls_mode,
		   tls_skip_verify = EXCLUDED.tls_skip_verify,
		   bind_dn = EXCLUDED.bind_dn,
		   bind_password_enc = EXCLUDED.bind_password_enc,
		   user_search_base = EXCLUDED.user_search_base,
		   user_search_filter = EXCLUDED.user_search_filter,
		   timeout_seconds = EXCLUDED.timeout_seconds,
		   updated_by = EXCLUDED.updated_by,
		   updated_at = now()`,
		LDAPConfigKey, row.Enabled, row.Host, row.Port, row.TLSMode, row.TLSSkipVerify,
		row.BindDN, row.BindPasswordEnc, row.UserSearchBase, row.UserSearchFilter,
		row.TimeoutSeconds, row.UpdatedBy)
	return err
}
