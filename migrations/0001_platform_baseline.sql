-- 平台基线表：审计与 outbox（docs/domain/model.md 的 AuditLog / OutboxEvent）。
-- 所有模块的写入都走审计（AGENT.md 红线 5）；outbox 支撑 node.ready / node.passed /
-- node.rejected / gate.pending / task.assigned / iteration.closed 事件投递。

-- +goose Up
CREATE TABLE audit_logs (
    id           BIGSERIAL PRIMARY KEY,
    actor_id     TEXT        NOT NULL,
    action       TEXT        NOT NULL,           -- 如 task.start_step / gate.review
    subject_type TEXT        NOT NULL,           -- task | iteration | workflow | user ...
    subject_id   TEXT        NOT NULL,
    payload      JSONB       NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_logs_subject ON audit_logs (subject_type, subject_id);
CREATE INDEX idx_audit_logs_actor   ON audit_logs (actor_id, created_at);

CREATE TABLE outbox_events (
    id           BIGSERIAL PRIMARY KEY,
    event_type   TEXT        NOT NULL,           -- node.ready / gate.pending / ...
    subject_type TEXT        NOT NULL,
    subject_id   TEXT        NOT NULL,
    payload      JSONB       NOT NULL DEFAULT '{}',
    status       TEXT        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    attempts     INT         NOT NULL DEFAULT 0,
    last_error   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at      TIMESTAMPTZ
);
CREATE INDEX idx_outbox_events_status ON outbox_events (status, created_at);

-- +goose Down
-- 只前进，不自动回滚（docs/tech.md）。
