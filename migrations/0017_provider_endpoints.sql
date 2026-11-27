-- 全局 Provider 连接目录与个人凭据（docs/domain/integrations.md「连接目录」与「个人凭据」）。
--
-- 背景：
-- 1. integration_provider_configs 每 kind 只有一行（主键即 provider），无法表达
--    「多个 git / 多个 svn / 多个 redmine / 多个 outline」。引入 provider_endpoints：
--    admin 维护的连接目录（kind + label + baseUrl + 备注），迭代绑定时可从目录选择，
--    也可自行填写额外地址。
-- 2. 成员自助维护本人在外部系统的写权限凭据（user_provider_credentials）：
--    分发成果物 / 流程记录时优先以提交人本人的 Key 出站，外部系统可见「对应员工提交」；
--    未配置时回退迭代级凭据 → 服务端环境变量（公共 Key），并在界面告警。
-- 安全：
--   1. 个人凭据只存 AES-GCM 密文（platform.Seal，密钥 CHALKLINE_SECRET_KEY 回退
--      CHALKLINE_JWT_SECRET），API 永不回显，只报「已配置 / 未配置」；
--   2. 目录只存地址与展示名，不存任何凭据（红线 8）；
--   3. 凭据按 (user_id, provider, base_url) 唯一：同一系统同一地址一人一份 Key。

-- +goose Up
CREATE TABLE provider_endpoints (
    id          TEXT        PRIMARY KEY,                          -- pep_ 前缀
    kind        TEXT        NOT NULL,                             -- git | svn | outline | redmine
    label       TEXT        NOT NULL,                             -- 展示名，如「公司 GitLab」
    base_url    TEXT        NOT NULL,                             -- http(s) 绝对地址，不得内嵌凭据
    note        TEXT        NOT NULL DEFAULT '',                  -- 备注（如「研发一部代码库」）
    enabled     BOOLEAN     NOT NULL DEFAULT TRUE,                -- false = 不再出现在选择列表
    created_by  TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (kind, base_url)
);

CREATE INDEX idx_provider_endpoints_kind ON provider_endpoints(kind);

CREATE TABLE user_provider_credentials (
    id            TEXT        PRIMARY KEY,                        -- upc_ 前缀
    user_id       TEXT        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider      TEXT        NOT NULL,                           -- outline | redmine | git | svn
    base_url      TEXT        NOT NULL DEFAULT '',                -- 空 = 适用于该 provider 的默认地址
    auth_type     TEXT        NOT NULL DEFAULT '',                -- '' | token | basic
    auth_username TEXT        NOT NULL DEFAULT '',                -- basic 的用户名（非机密）
    secret_enc    TEXT        NOT NULL DEFAULT '',                -- AES-GCM 密文；空 = 未配置
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, provider, base_url)
);

CREATE INDEX idx_user_provider_credentials_user ON user_provider_credentials(user_id);

-- +goose Down
-- 只前进，不自动回滚（docs/tech.md）。
