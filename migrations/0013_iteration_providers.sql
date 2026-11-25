-- 迭代级 Provider 绑定与分发回执（docs/domain/integrations.md「迭代 Provider」）。
--
-- 设计要点：
-- 1. iteration_providers：每个迭代一套 provider——成果物（必选且唯一）、流程记录（至多一个）、
--    代码库（可多个）。连接信息（base_url）随迭代固化，不再只依赖全局默认值；
--    迭代创建/初始化阶段可改，首次提交成果物后冻结（不变量 22）。
-- 2. integration_syncs：成果物分发与流程记录的回执（只存摘要，不复制内容）。
--    唯一键提供幂等：同一 (迭代, 分类, provider, 主体, 动作) 只成功推送一次。
-- 3. 凭据仍只从环境变量读取，不落库（AGENT 红线 8）。

-- +goose Up
CREATE TABLE iteration_providers (
    id           TEXT        PRIMARY KEY,                          -- itp_ 前缀
    iteration_id TEXT        NOT NULL REFERENCES iterations(id) ON DELETE CASCADE,
    category     TEXT        NOT NULL CHECK (category IN ('artifact', 'process', 'code')),
    provider     TEXT        NOT NULL,                             -- inline | outline | git | svn | redmine
    base_url     TEXT        NOT NULL DEFAULT '',                  -- 外部系统根地址（inline 为空）
    target       TEXT        NOT NULL DEFAULT '',                  -- outline collection / redmine project / 代码库路径
    dir_template TEXT        NOT NULL DEFAULT '',                  -- 成果物目录结构约定
    branch       TEXT        NOT NULL DEFAULT '',                  -- 代码库分支（git 用，空即默认分支）
    created_by   TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (iteration_id, category, provider, target)
);

CREATE INDEX idx_iteration_providers_iteration ON iteration_providers(iteration_id);

CREATE TABLE integration_syncs (
    id           BIGSERIAL   PRIMARY KEY,
    iteration_id TEXT        NOT NULL REFERENCES iterations(id) ON DELETE CASCADE,
    category     TEXT        NOT NULL CHECK (category IN ('artifact', 'process')),
    provider     TEXT        NOT NULL,
    subject_type TEXT        NOT NULL,                             -- artifact | node | task | iteration
    subject_id   TEXT        NOT NULL,
    action       TEXT        NOT NULL,                             -- push | ticket | comment
    status       TEXT        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'ok', 'failed', 'skipped')),
    external_id  TEXT        NOT NULL DEFAULT '',
    url          TEXT        NOT NULL DEFAULT '',
    summary      TEXT        NOT NULL DEFAULT '',
    error        TEXT        NOT NULL DEFAULT '',
    attempts     INT         NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (iteration_id, category, provider, subject_type, subject_id, action)
);

CREATE INDEX idx_integration_syncs_iteration ON integration_syncs(iteration_id);

-- +goose Down
-- 只前进，不自动回滚（docs/tech.md）。
