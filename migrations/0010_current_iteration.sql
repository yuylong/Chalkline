-- 个人「当前迭代」偏好（docs/modules/planning.md「当前迭代」）：
-- 控制台登录后的首个页面直接展示该迭代看板，省去逐级点击。
-- 每人至多一条；选中时必须对该迭代所属产品可见（app 层校验），
-- 迭代被删除时随外键级联清理。

-- +goose Up
CREATE TABLE user_current_iterations (
    user_id      TEXT        PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    iteration_id TEXT        NOT NULL REFERENCES iterations(id) ON DELETE CASCADE,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_user_current_iterations_iteration ON user_current_iterations(iteration_id);

-- +goose Down
-- 只前进，不自动回滚（docs/tech.md）。
