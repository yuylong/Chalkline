# 模块：integration

> **M2 范围**。外部系统适配器：只做协议转换与引用校验，不写领域状态、不做双向同步。模块索引见 [README.md](README.md)。

## 边界

负责：

- **Provider 适配器**：`git` / `svn` / `outline` / `redmine`，实现 `Provider` 接口（`Kind` / `Validate` / `Snapshot`）。
- **通知投递**：`notify` 子包——SMTP 邮件与 Webhook，消费 outbox 事件异步发送。
- **出站安全**：域名白名单、禁止跟随任意重定向（防 SSRF）、凭据只从环境变量/密钥文件读取（不落库、不进日志）、Webhook 签名头。
- **降级**：集成不可用时控制台标「未连通」，流程照常推进（不变量 15）。

不负责：领域状态变更（domain/workflow/app）、材料内容存储（只存引用）、反向回写（Redmine 不回写状态，见 [integrations.md](../domain/integrations.md)）。

## 对外接口

Go 接口（被 app 层经依赖倒置调用）：

```go
type Provider interface {
    Kind() string
    Validate(ctx context.Context, ref workitem.ArtifactRef) error
    Snapshot(ctx context.Context, ref workitem.ArtifactRef) (workitem.Meta, error)
}
```

- `Validate`：校验引用可达与格式（如 `pr` 必须是 http(s) 绝对 URL 且 provider 为 git/svn；`inline` 必须有内容无 URL）。**只校验引用存在与格式合法，不要求外部系统实时可访问**（[workflow.md](../domain/workflow.md)）。
- `Snapshot`：取标题/版本/状态快照，仅用于展示与审计，不回写。

通知：`notify.Dispatcher` 消费 `outbox_events` 表，按事件类型路由到 SMTP / Webhook。

## 核心流程

- **材料挂接时**：`attach_artifact` app 用例调对应 provider 的 `Validate`；失败返回 `INVALID_ARTIFACT_REF`（422）。可选调 `Snapshot` 存快照（不阻断挂接）。
- **状态流转时**：app 层写领域事件到 `outbox_events`（`node.passed` / `gate.pending` 等）；投递器后台异步发送，失败指数退避重试最多 5 次，超过标 `failed` 并控制台告警。**投递失败不阻断流程**。
- **安全**：出站请求前过域名白名单；Webhook 载荷加签名头；邮件默认只发内网 SMTP。

## 数据

- `outbox_events`（M0 `0001_platform_baseline.sql` 已建）：`id` / `event_type` / `payload`（id、标题、链接、操作者，不携带正文）/ `status`（pending/sent/failed）/ `retry_count` / `last_error` / 时间戳。
- `external_links`（M2 新建迁移，可选）：`subject_type` / `subject_id` / `provider` / `external_id` / `url`，用于 Redmine issue 回写、Outline 文档归属等稳定关联（见 model.md「ExternalLink」）。
- 各 provider 的 `base_url` 与凭据：环境变量/密钥文件，**不入库**。

## 不变量（本模块保证）

- 集成投递失败不改变门禁判定与工作项状态（不变量 15）。
- 材料 `provider` 与 `kind` 必须匹配（不变量 14）——矩阵在 `internal/domain/workitem`，适配器只做可达性校验。
- 外部凭据不落库、不进日志（tech.md 约束 10）。
- Redmine 不反向回写状态（integrations.md 明确），状态唯一事实来源是 Chalkline。
- 出站请求防 SSRF（域名白名单 + 不跟任意重定向）。

## 验收标准

- [ ] 各 provider 的 `Validate` 正确拒绝非法引用（格式/provider 不匹配）。
- [ ] 通知投递器：事件入 outbox → 异步发送 → 成功标 sent；失败重试 5 次后 failed。
- [ ] 投递失败时工作流状态不受影响（集成测试：断网后流程仍推进）。
- [ ] 凭据不出现在日志与数据库（审计验证）。
- [ ] Webhook 签名头可被接收方校验；出站域名白名单生效。
- [ ] 测试全绿（适配器单测可用 mock，投递用真实 SMTP/Webhook 测试桩）。

## 任务拆解（M2 排期）

1. `internal/integration/provider.go`：定义 `Provider` 接口与注册表（按 kind 找 provider）。
2. `internal/integration/notify/dispatcher.go`：outbox 投递器（后台 worker、指数退避、SMTP/Webhook 路由）。
3. 适配器逐个实现：`git` / `svn` / `outline` / `redmine`（各含 Validate + Snapshot）。
4. 安全：出站域名白名单中间件、SSRF 防护、Webhook 签名。
5. app 层接入：`attach_artifact` 调 Validate/Snapshot；状态流转写 outbox（与 workflow app 协作）。
6. 迁移：`external_links` 表（若 M2 需要稳定关联）。
7. 测试：适配器 mock 测试、投递失败重试、降级场景。
8. 文档：更新模块索引状态。

## 开放问题

- 各适配器的优先级：M2 先做 `outline` + `git`（门禁材料最常用），`redmine`/`svn` 按需？→ M2 启动时定。
- 是否需要配置化的 provider 启停与 base_url 管理界面？→ 先环境变量，M3 评估管理 UI。
