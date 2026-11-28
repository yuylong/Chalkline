-- 外部落点路径：材料分发到 outline / git / svn 后，记录它在外部系统的可读路径
-- （面包屑），控制台直接展示「这篇文档在哪」，不要求用户记住 URL。
--
--   outline → CHK-IT1 / 竞品调研 / 报告 v2 · research_report · v2
--   git/svn → chalkline/CHK-IT1/竞品调研/research_report.md
--
-- artifact_versions 同步加列：归档时随旧行一起复制，历史轮次也能显示路径。
-- +goose Up
ALTER TABLE artifacts ADD COLUMN external_path TEXT NOT NULL DEFAULT '';
ALTER TABLE artifact_versions ADD COLUMN external_path TEXT NOT NULL DEFAULT '';

-- +goose Down
-- 只前进，不自动回滚（docs/tech.md）。
