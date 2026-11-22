-- 目录模块：产品、产品成员（可见性唯一依据）、模块树、轻量产品文档
--（docs/modules/catalog.md，字段定义见 docs/domain/model.md）。

-- +goose Up
CREATE TABLE products (
    id                          TEXT        PRIMARY KEY,           -- prd_ 前缀
    key                         TEXT        NOT NULL UNIQUE,       -- 编号前缀（如 CHK），不可改
    name                        TEXT        NOT NULL,
    description                 TEXT        NOT NULL DEFAULT '',
    status                      TEXT        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    default_iteration_workflow_id TEXT      NOT NULL DEFAULT '',
    default_task_workflow_id    TEXT        NOT NULL DEFAULT '',
    created_by                  TEXT        NOT NULL,             -- 创建人，自动成为 owner
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE product_members (
    product_id  TEXT        NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    user_id     TEXT        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    member_role TEXT        NOT NULL CHECK (member_role IN ('owner', 'member', 'guest')),
    created_by  TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (product_id, user_id)
);

CREATE INDEX idx_product_members_user ON product_members(user_id);

-- 模块树：物化路径存名称段（如 /catalog/task-tree），深度 ≤ 5 由应用层校验。
CREATE TABLE modules (
    id         TEXT        PRIMARY KEY,                            -- mod_ 前缀
    product_id TEXT        NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    parent_id  TEXT        REFERENCES modules(id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    path       TEXT        NOT NULL,                               -- 物化路径，子树查询用
    sort_order INT         NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (product_id, parent_id, name)                           -- 根模块 parent_id 为 NULL 时由应用层保证同级唯一
);

CREATE INDEX idx_modules_product ON modules(product_id);
CREATE INDEX idx_modules_path ON modules(product_id, path);

-- 轻量内联文档：每个 owner 一行，保存即升 version（乐观锁）。
CREATE TABLE documents (
    id         TEXT        PRIMARY KEY,                            -- doc_ 前缀
    owner_type TEXT        NOT NULL CHECK (owner_type IN ('product', 'iteration', 'task')),
    owner_id   TEXT        NOT NULL,
    content    TEXT        NOT NULL DEFAULT '',
    version    INT         NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (owner_type, owner_id)
);

-- +goose Down
-- 只前进，不自动回滚（docs/tech.md）。
