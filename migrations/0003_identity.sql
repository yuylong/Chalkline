-- 身份模块：平台成员与个人访问令牌（docs/modules/identity.md）。
-- 密码只存 argon2id 编码串，PAT 只存 SHA-256 hash，明文一律不落库。

-- +goose Up
CREATE TABLE users (
    id            TEXT        PRIMARY KEY,                          -- usr_ 前缀
    username      TEXT        NOT NULL UNIQUE,                      -- 唯一登录名，不可改
    display_name  TEXT        NOT NULL,
    email         TEXT        NOT NULL UNIQUE,
    password_hash TEXT        NOT NULL,                             -- argon2id
    roles         TEXT[]      NOT NULL,                             -- 全局角色，可多选
    status        TEXT        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE personal_access_tokens (
    id           TEXT        PRIMARY KEY,                           -- pat_ 前缀
    user_id      TEXT        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT        NOT NULL,
    token_hash   TEXT        NOT NULL UNIQUE,                       -- SHA-256 hex，只存 hash
    last_used_at TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ NOT NULL,
    revoked_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_personal_access_tokens_user ON personal_access_tokens(user_id);

-- +goose Down
-- 只前进，不自动回滚（docs/tech.md）。
