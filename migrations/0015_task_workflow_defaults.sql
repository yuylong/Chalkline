-- 产品级「按任务类型的默认任务流模板」（docs/domain/model.md Product.task_workflow_defaults）。
--
-- 背景：任务类型分化后（bug / dev / test …），单一 default_task_workflow_id 不再够用——
-- bug 任务应默认走「缺陷修复」模板，而不是标准开发任务。
-- 设计：JSONB map（kind → workflow_id），整组替换；未覆盖的类型回退产品默认。
-- 兼容：旧行默认 '{}'，行为与升级前一致。

-- +goose Up
ALTER TABLE products
    ADD COLUMN task_workflow_defaults JSONB NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
-- 只前进，不自动回滚（docs/tech.md）。
