package store

// smtp_config.go：SMTP 发件邮箱配置（docs/modules/integration.md「M4 增量：邮箱配置」）。
// 单行配置（config_key='default'）；密码只存 AES-GCM 密文（platform.Seal），
// 明文与密钥都不经本层流转。未写入行 = 使用环境变量默认值。

import (
	"context"
	"time"
)

// SMTPConfigKey 是唯一配置行的键。
const SMTPConfigKey = "default"

// SMTPConfigRow 是 smtp_configs 行。
type SMTPConfigRow struct {
	Enabled     bool
	Host        string
	Port        int
	Username    string
	PasswordEnc string // AES-GCM 密文；空 = 未配置
	FromAddr    string
	UpdatedBy   string
	UpdatedAt   time.Time
}

const smtpConfigCols = `enabled, host, port, username, password_enc, from_addr, updated_by, updated_at`

func scanSMTPConfig(row rowScanner) (SMTPConfigRow, error) {
	var c SMTPConfigRow
	err := row.Scan(&c.Enabled, &c.Host, &c.Port, &c.Username, &c.PasswordEnc,
		&c.FromAddr, &c.UpdatedBy, &c.UpdatedAt)
	return c, err
}

// SMTPConfig 取唯一配置行；ErrNotFound 表示尚未在管理台保存过（用环境变量默认值）。
func SMTPConfig(ctx context.Context, q Querier) (SMTPConfigRow, error) {
	return scanSMTPConfig(q.QueryRow(ctx,
		`SELECT `+smtpConfigCols+` FROM smtp_configs WHERE config_key = $1`, SMTPConfigKey))
}

// UpsertSMTPConfig 写入（或覆盖）配置行。
func UpsertSMTPConfig(ctx context.Context, q Querier, row SMTPConfigRow) error {
	_, err := q.Exec(ctx,
		`INSERT INTO smtp_configs (config_key, enabled, host, port, username, password_enc, from_addr, updated_by, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
		 ON CONFLICT (config_key) DO UPDATE SET
		   enabled = EXCLUDED.enabled,
		   host = EXCLUDED.host,
		   port = EXCLUDED.port,
		   username = EXCLUDED.username,
		   password_enc = EXCLUDED.password_enc,
		   from_addr = EXCLUDED.from_addr,
		   updated_by = EXCLUDED.updated_by,
		   updated_at = now()`,
		SMTPConfigKey, row.Enabled, row.Host, row.Port, row.Username,
		row.PasswordEnc, row.FromAddr, row.UpdatedBy)
	return err
}
