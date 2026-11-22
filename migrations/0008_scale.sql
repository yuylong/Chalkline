-- M3 规模化（docs/modules/README.md M3）：
-- 1. webhook_subscriptions：Webhook 订阅管理（开放 API 与事件推送的出站侧）。
--    签名密钥沿用全局 CHALKLINE_WEBHOOK_SECRET（凭据不落库，AGENT 红线 8），
--    订阅行只存 URL / 事件类型过滤 / 状态与最近投递结果。
-- 2. gate_reminders：评审超期提醒与升级链的已提醒水位（instance+node+level 唯一），
--    只记录提醒事实，不改任何节点状态（红线 1：状态只能经引擎修改）。
-- 3. audit_logs 补 created_at 索引：审计留痕检索按时间范围扫描。

-- +goose Up
CREATE TABLE webhook_subscriptions (
    id               TEXT        PRIMARY KEY,                 -- whk_ 前缀
    name             TEXT        NOT NULL,
    url              TEXT        NOT NULL,
    event_types      TEXT[]      NOT NULL DEFAULT '{}',      -- 空 = 订阅全部事件
    status           TEXT        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused')),
    created_by       TEXT        NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_delivery_at TIMESTAMPTZ,
    last_delivery_ok BOOLEAN,
    last_error       TEXT
);
CREATE INDEX idx_webhook_subscriptions_status ON webhook_subscriptions (status);

CREATE TABLE gate_reminders (
    instance_id TEXT        NOT NULL REFERENCES workflow_instances(id) ON DELETE CASCADE,
    node_id     TEXT        NOT NULL,
    level       INT         NOT NULL CHECK (level IN (1, 2)), -- 1=超期提醒评审人；2=升级给产品 owner/admin
    reminded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (instance_id, node_id, level)
);

CREATE INDEX idx_audit_logs_created ON audit_logs (created_at);

-- +goose Down
-- 只前进，不自动回滚（docs/tech.md）。
