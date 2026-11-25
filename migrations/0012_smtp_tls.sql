-- SMTP TLS/STARTTLS 支持（docs/modules/integration.md「M4 增量：邮箱配置」）：
-- tls_mode：'' = 沿用环境变量 CHALKLINE_SMTP_TLS；none（明文）/ starttls（587 常用）/ tls（隐式 TLS，465 常用）。
-- tls_skip_verify：NULL = 沿用环境变量 CHALKLINE_SMTP_TLS_SKIP_VERIFY；true = 跳过证书校验（仅内网自签场景）。

-- +goose Up
ALTER TABLE smtp_configs
    ADD COLUMN tls_mode        TEXT    NOT NULL DEFAULT '',   -- '' = 沿用环境变量；none/starttls/tls
    ADD COLUMN tls_skip_verify BOOLEAN;                      -- NULL = 沿用环境变量

-- +goose Down
-- 只前进，不自动回滚（docs/tech.md）。
