-- 集成模块（docs/modules/integration.md）：
-- 1. outbox_events 增加下次尝试时间列，支撑投递器指数退避（M0 只建表未接入）。
-- 2. external_links：工作项与外部系统的稳定关联（model.md「ExternalLink」），
--    用于 Redmine issue 回写、Outline 文档归属等场景；M2 建表，写入方按需接入。

-- +goose Up
ALTER TABLE outbox_events
    ADD COLUMN next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now();
DROP INDEX idx_outbox_events_status;
CREATE INDEX idx_outbox_events_due ON outbox_events (status, next_attempt_at);

CREATE TABLE external_links (
    id           BIGSERIAL PRIMARY KEY,
    subject_type TEXT        NOT NULL,           -- task | iteration | product ...
    subject_id   TEXT        NOT NULL,
    provider     TEXT        NOT NULL,           -- redmine | outline | git ...
    external_id  TEXT        NOT NULL DEFAULT '',
    url          TEXT        NOT NULL DEFAULT '',
    created_by   TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (subject_type, subject_id, provider, external_id)
);
CREATE INDEX idx_external_links_subject ON external_links (subject_type, subject_id);

-- +goose Down
-- 只前进，不自动回滚（docs/tech.md）。
