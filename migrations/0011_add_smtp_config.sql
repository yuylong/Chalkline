-- SMTP 发件邮箱配置（docs/modules/integration.md「M4 增量：邮箱配置」）：
-- 管理台可视化维护发件服务器（host/port/账号/发件人）与启用开关。
-- 密码以 AES-GCM 密文落库（platform.Seal，密钥 CHALKLINE_SECRET_KEY，未设置回退
-- CHALKLINE_JWT_SECRET）——这是红线 8「凭据不落库」的唯一例外，API 永不回显。
-- 未写入行 = 使用环境变量默认值（CHALKLINE_SMTP_*）；行内空字段同样回退环境变量。

-- +goose Up
CREATE TABLE smtp_configs (
    config_key   TEXT        PRIMARY KEY,             -- 单行 'default'
    enabled      BOOLEAN     NOT NULL DEFAULT FALSE,  -- false = 不发邮件
    host         TEXT        NOT NULL DEFAULT '',      -- 空 = 沿用 CHALKLINE_SMTP_HOST
    port         INT         NOT NULL DEFAULT 25,      -- 0 = 默认 25
    username     TEXT        NOT NULL DEFAULT '',
    password_enc TEXT        NOT NULL DEFAULT '',      -- AES-GCM 密文；空 = 未配置（回退环境变量）
    from_addr    TEXT        NOT NULL DEFAULT '',
    updated_by   TEXT        NOT NULL DEFAULT '',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
-- 只前进，不自动回滚（docs/tech.md）。
