-- LDAP 认证（docs/modules/identity.md「LDAP 认证」）：
-- 管理台可视化维护 LDAP 服务器（host/port/TLS/绑定账号/用户检索）与启用开关。
-- 绑定密码以 AES-GCM 密文落库（platform.Seal，密钥 CHALKLINE_SECRET_KEY），
-- 与 SMTP 密码同属红线 8「凭据不落库」的管理台例外，API 永不回显。
-- users.auth_source：local（仅本地 argon2id）/ ldap（仅 LDAP）/ both（双认证，
-- 本地或 LDAP 任一密码通过即可登录）。

-- +goose Up
CREATE TABLE ldap_configs (
    config_key       TEXT        PRIMARY KEY,             -- 单行 'default'
    enabled          BOOLEAN     NOT NULL DEFAULT FALSE,  -- false = 登录不走 LDAP
    host             TEXT        NOT NULL DEFAULT '',
    port             INT         NOT NULL DEFAULT 389,
    tls_mode         TEXT        NOT NULL DEFAULT 'none',  -- none / starttls / tls
    tls_skip_verify  BOOLEAN     NOT NULL DEFAULT FALSE,   -- 跳过证书校验（内网自签场景）
    bind_dn          TEXT        NOT NULL DEFAULT '',      -- 服务账号 DN（匿名检索可留空）
    bind_password_enc TEXT       NOT NULL DEFAULT '',      -- AES-GCM 密文；空 = 未配置
    user_search_base TEXT        NOT NULL DEFAULT '',      -- 如 ou=people,dc=example,dc=com
    user_search_filter TEXT      NOT NULL DEFAULT '(uid=%s)', -- %s 占位登录名
    timeout_seconds  INT         NOT NULL DEFAULT 10,     -- 拨号/检索超时
    updated_by       TEXT        NOT NULL DEFAULT '',
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE users
    ADD COLUMN auth_source TEXT NOT NULL DEFAULT 'local'; -- local / ldap / both

-- +goose Down
-- 只前进，不自动回滚（docs/tech.md）。
