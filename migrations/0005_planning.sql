-- 计划模块：迭代、任务、任务×模块关联与编号计数器（docs/modules/planning.md）。
-- 注意：tasks 不存 status 列——任务状态是工作流节点终态的投影，读时联查实例推导。

-- +goose Up
CREATE TABLE iterations (
    id                   TEXT        PRIMARY KEY,                  -- itr_ 前缀
    product_id           TEXT        NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    name                 TEXT        NOT NULL,
    goal                 TEXT        NOT NULL DEFAULT '',
    number               TEXT        NOT NULL,                     -- <key>-IT<n>，产品内递增
    starts_on            DATE        NOT NULL,
    ends_on              DATE        NOT NULL,
    status               TEXT        NOT NULL DEFAULT 'planned' CHECK (status IN ('planned', 'active', 'closed', 'cancelled')),
    workflow_id          TEXT        NOT NULL,                    -- 生命周期模板（实例化时固定版本）
    workflow_instance_id TEXT        NOT NULL DEFAULT '',
    created_by           TEXT        NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (product_id, number)
);

CREATE TABLE tasks (
    id                   TEXT        PRIMARY KEY,                  -- tsk_ 前缀
    product_id           TEXT        NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    iteration_id         TEXT        NOT NULL REFERENCES iterations(id) ON DELETE CASCADE,
    parent_id            TEXT        REFERENCES tasks(id) ON DELETE CASCADE,
    number               TEXT        NOT NULL UNIQUE,              -- 多级编号，全局唯一，不可变
    title                TEXT        NOT NULL,
    description          TEXT        NOT NULL DEFAULT '',
    kind                 TEXT        NOT NULL CHECK (kind IN ('research', 'design', 'dev', 'test', 'ops', 'summary')),
    assignee_id          TEXT        NOT NULL DEFAULT '',          -- 唯一负责人，空即待认领
    priority             TEXT        NOT NULL DEFAULT 'normal' CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    workflow_id          TEXT        NOT NULL,                    -- 任务流模板（子任务默认继承父任务）
    workflow_instance_id TEXT        NOT NULL DEFAULT '',
    created_by           TEXT        NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_tasks_iteration ON tasks(iteration_id);
CREATE INDEX idx_tasks_parent ON tasks(parent_id);
CREATE INDEX idx_tasks_assignee ON tasks(assignee_id) WHERE assignee_id <> '';

CREATE TABLE task_modules (
    task_id   TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    module_id TEXT NOT NULL REFERENCES modules(id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, module_id)
);

-- 编号计数器：按「产品:迭代」「产品:顶层任务」「父任务:子任务」维度取号，
-- INSERT ... ON CONFLICT 原子自增，并发不重复；编号永不复用（不变量 3）。
CREATE TABLE counters (
    scope_key TEXT   PRIMARY KEY,
    value     BIGINT NOT NULL DEFAULT 0
);

-- +goose Down
-- 只前进，不自动回滚（docs/tech.md）。
