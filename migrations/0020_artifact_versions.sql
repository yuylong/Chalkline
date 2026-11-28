-- 材料版本历史：artifacts 只保留「当前生效」一份（UNIQUE(instance,node,kind)），
-- 替换即覆盖。本表保存被替换的旧版正文，使节点详情页可以回看历史轮次。
--
-- 写入时机：store.ReplaceArtifact 删除同 kind 旧行之前，把旧行整条归档到这里。
-- 只增不删（审计语义）；查询按 node_id + kind + replaced_at 倒序。
-- +goose Up
CREATE TABLE artifact_versions (
    id          TEXT        PRIMARY KEY,                          -- artv_ 前缀
    artifact_id TEXT        NOT NULL,                             -- 原 artifacts.id（已删除）
    instance_id TEXT        NOT NULL REFERENCES workflow_instances(id) ON DELETE CASCADE,
    node_id     TEXT        NOT NULL,
    kind        TEXT        NOT NULL,
    provider    TEXT        NOT NULL,
    url         TEXT        NOT NULL DEFAULT '',
    external_id TEXT        NOT NULL DEFAULT '',
    title       TEXT        NOT NULL DEFAULT '',
    content     TEXT        NOT NULL DEFAULT '',
    snapshot    JSONB,
    submitted_by TEXT       NOT NULL,
    -- 该版本存在的起止：created_at 为原提交时间，replaced_at 为被替换时间。
    created_at   TIMESTAMPTZ NOT NULL,
    replaced_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    replaced_by  TEXT        NOT NULL DEFAULT ''                  -- 触发替换的提交人
);

CREATE INDEX idx_artifact_versions_node ON artifact_versions(instance_id, node_id, kind, replaced_at DESC);

-- +goose Down
-- 只前进，不自动回滚（docs/tech.md）。
