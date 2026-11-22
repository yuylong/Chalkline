-- 工作流运行时：实例、节点状态、门禁审批、材料引用（docs/modules/workflow.md）。
-- 状态只能经引擎修改：应用层「加载 → 调引擎纯函数 → 写回」，禁止直接 UPDATE 节点状态。

-- +goose Up
CREATE TABLE workflow_instances (
    id               TEXT        PRIMARY KEY,                     -- wfi_ 前缀
    workflow_id      TEXT        NOT NULL,                        -- 模板 id
    workflow_version INT         NOT NULL,                        -- 实例化时固定版本（模板修改不影响运行中实例）
    work_item_type   TEXT        NOT NULL CHECK (work_item_type IN ('iteration', 'task')),
    work_item_id     TEXT        NOT NULL,
    definition       JSONB       NOT NULL,                       -- 模板快照，实例生命周期内不变
    terminal_node_id TEXT        NOT NULL DEFAULT '',             -- 已抵达的终态节点（空即未抵达）
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (work_item_type, work_item_id)                         -- 一个工作项一个实例
);

CREATE TABLE node_states (
    instance_id  TEXT        NOT NULL REFERENCES workflow_instances(id) ON DELETE CASCADE,
    node_id      TEXT        NOT NULL,
    status       TEXT        NOT NULL CHECK (status IN ('pending', 'ready', 'in_progress', 'awaiting_review', 'passed', 'rejected')),
    version      BIGINT      NOT NULL DEFAULT 1,                  -- 乐观锁：每次状态变更 +1
    entered_at   TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    PRIMARY KEY (instance_id, node_id)
);

CREATE TABLE gate_approvals (
    instance_id TEXT        NOT NULL REFERENCES workflow_instances(id) ON DELETE CASCADE,
    node_id     TEXT        NOT NULL,
    reviewer_id TEXT        NOT NULL,
    decision    TEXT        NOT NULL CHECK (decision IN ('approve', 'reject')),
    comment     TEXT        NOT NULL DEFAULT '',                  -- 驳回必填
    roles       TEXT[]      NOT NULL,                             -- 评审人当时的全局角色（判定资格用，写入时固化）
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (instance_id, node_id, reviewer_id)                -- 同评审人 upsert，最近决策为准
);

CREATE TABLE artifacts (
    id           TEXT        PRIMARY KEY,                         -- art_ 前缀
    instance_id  TEXT        NOT NULL REFERENCES workflow_instances(id) ON DELETE CASCADE,
    node_id      TEXT        NOT NULL,
    kind         TEXT        NOT NULL,                            -- 材料类型（workitem.ArtifactKind）
    provider     TEXT        NOT NULL,                            -- 来源（git/svn/outline/inline/...）
    url          TEXT        NOT NULL DEFAULT '',                 -- inline 时为空
    external_id  TEXT        NOT NULL DEFAULT '',
    title        TEXT        NOT NULL DEFAULT '',
    content      TEXT        NOT NULL DEFAULT '',                 -- 仅 inline 承载 Markdown
    snapshot     JSONB,                                           -- 展示快照，不回写
    submitted_by TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (instance_id, node_id, kind)                            -- 每节点每类型一份材料，重复挂接即替换（重置审批）
);

CREATE INDEX idx_artifacts_instance_node ON artifacts(instance_id, node_id);
CREATE INDEX idx_gate_approvals_instance_node ON gate_approvals(instance_id, node_id);

-- +goose Down
-- 只前进，不自动回滚（docs/tech.md）。
