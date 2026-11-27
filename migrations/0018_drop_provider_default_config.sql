-- 移除「默认 provider」配置表：全局 provider 配置只有「服务器列表」
-- （provider_endpoints，迁移 0017）——没有「默认 git / 默认 svn / 默认 outline /
-- 默认 redmine」的概念；任何选择 provider 的地方都从列表选取或手填地址。
-- integration_provider_configs 的启停与 baseURL 覆盖随本迁移一并废弃。

-- +goose Up
DROP TABLE IF EXISTS integration_provider_configs;

-- +goose Down
-- 只前进，不自动回滚（docs/tech.md）。
