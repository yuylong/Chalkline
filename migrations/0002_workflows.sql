-- 工作流模板表：模板定义以 JSON 存储，作者用 YAML 编辑、平台校验后转换
--（docs/domain/workflow.md）。内置模板首启时由应用种子写入，与自建模板完全等价。

-- +goose Up
CREATE TABLE workflows (
    id         TEXT        PRIMARY KEY,          -- 如 wf-iteration-lifecycle
    scope      TEXT        NOT NULL CHECK (scope IN ('iteration', 'task')),
    name       TEXT        NOT NULL,
    version    INT         NOT NULL DEFAULT 1,
    status     TEXT        NOT NULL DEFAULT 'active' CHECK (status IN ('draft', 'active', 'archived')),
    definition JSONB       NOT NULL,             -- 校验通过的完整模板（含 nodes / edges / settings）
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (scope, name, version)
);

-- +goose Down
-- 只前进，不自动回滚（docs/tech.md）。
