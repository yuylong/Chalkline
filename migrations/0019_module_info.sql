-- 模块基本信息：功能简介、技术栈标签、主要责任人、开发者、测试者
--（docs/domain/model.md Module 字段表）。
--
-- 设计：人员字段存 user_id（TEXT，空即未指定），技术栈为 TEXT[] 标签集合；
-- 旧行默认空值，行为与升级前一致。删除模块时整棵子树级联（modules.parent_id
-- ON DELETE CASCADE），挂靠任务经 task_modules 级联解除关联。
-- 0016 已用于 LDAP 认证，故本迁移使用当前最新版本号 0019。

-- +goose Up
ALTER TABLE modules
    ADD COLUMN description TEXT NOT NULL DEFAULT '',   -- 功能简介
    ADD COLUMN tech_stack  TEXT[] NOT NULL DEFAULT '{}', -- 技术栈标签（多选）
    ADD COLUMN owner_id    TEXT NOT NULL DEFAULT '',    -- 主要责任人
    ADD COLUMN developers  TEXT[] NOT NULL DEFAULT '{}', -- 开发者
    ADD COLUMN testers     TEXT[] NOT NULL DEFAULT '{}'; -- 测试者

-- +goose Down
-- 只前进，不自动回滚（docs/tech.md）。
