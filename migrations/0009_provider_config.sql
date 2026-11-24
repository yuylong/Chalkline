-- 集成 Provider 配置（docs/modules/integration.md「开放问题」M3：配置化的 provider 启停与
-- base_url 管理界面）：
-- 1. integration_provider_configs：管理台可视化维护的 provider 开关与 baseURL。
--    凭据（token / API key）仍只从环境变量读取，不落库（AGENT 红线 8：凭据不落库）。
-- 2. 未写入行 = 使用环境变量默认值（enabled=true，baseURL 取 CHALKLINE_*_BASE_URL）。

-- +goose Up
CREATE TABLE integration_provider_configs (
    provider   TEXT        PRIMARY KEY,              -- git | svn | outline | redmine
    enabled    BOOLEAN     NOT NULL DEFAULT TRUE,    -- false = 跳过外部快照（材料引用仍有效）
    base_url   TEXT        NOT NULL DEFAULT '',      -- 空 = 沿用环境变量默认值
    updated_by TEXT        NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
-- 只前进，不自动回滚（docs/tech.md）。
