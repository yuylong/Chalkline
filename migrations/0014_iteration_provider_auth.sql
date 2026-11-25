-- 迭代 provider 的写权限凭据（docs/domain/integrations.md「迭代 Provider」）。
--
-- 背景：成果物/流程记录需要对外部系统写入，凭据必须可按迭代配置，并支持定期轮换。
-- 安全：
--   1. 只存密文（AES-GCM，platform.Seal，密钥 CHALKLINE_SECRET_KEY 回退 CHALKLINE_JWT_SECRET）；
--   2. API 永不回显密文或明文，只报「已配置 / 未配置」；
--   3. 未配置时沿用服务端环境变量（CHALKLINE_OUTLINE_TOKEN 等），保持原有部署方式可用；
--   4. 凭据更新不受绑定冻结限制（公司会定期更换 Key）。

-- +goose Up
ALTER TABLE iteration_providers
    ADD COLUMN auth_type       TEXT NOT NULL DEFAULT '',  -- '' | token | basic
    ADD COLUMN auth_username   TEXT NOT NULL DEFAULT '',  -- basic 认证的用户名（非机密）
    ADD COLUMN auth_secret_enc TEXT NOT NULL DEFAULT '';  -- AES-GCM 密文；空 = 未配置（回退环境变量）

-- +goose Down
-- 只前进，不自动回滚（docs/tech.md）。
