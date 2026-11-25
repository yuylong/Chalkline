# 模块：integration

> **M2 范围** · **已交付**。外部系统适配器：只做协议转换与引用校验，不写领域状态、不做双向同步。模块索引见 [README.md](README.md)。

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
    Kind() workitem.ArtifactProvider
    Validate(ctx context.Context, ref workitem.ArtifactRef) error
    Snapshot(ctx context.Context, ref workitem.ArtifactRef) (workitem.ArtifactMeta, error)
}
```

`Registry` 实现 `app.ArtifactGate`（`Validate` / `Snapshot`），由 `internal/server` 注入 `app.Service.Gate`。

- `Validate`：校验引用可达与格式（如 `pr` 必须是 http(s) 绝对 URL 且 provider 为 git/svn；`inline` 必须有内容无 URL）。**只校验引用存在与格式合法，不要求外部系统实时可访问**（[workflow.md](../domain/workflow.md)）。
- `Snapshot`：取标题/版本/状态快照，仅用于展示与审计，不回写。

通知：`notify.Dispatcher` 消费 `outbox_events` 表，按事件类型路由到 SMTP / Webhook。

## 核心流程

- **材料挂接时**：`attach_artifact` app 用例调对应 provider 的 `Validate`；失败返回 `INVALID_ARTIFACT_REF`（422）。可选调 `Snapshot` 存快照（不阻断挂接）。
- **状态流转时**：app 层写领域事件到 `outbox_events`（`node.passed` / `gate.pending` 等）；投递器后台异步发送，失败指数退避重试最多 5 次，超过标 `failed` 并控制台告警。**投递失败不阻断流程**。
- **安全**：出站请求前过域名白名单；Webhook 载荷加签名头；邮件默认只发内网 SMTP。

## 数据

- `outbox_events`（M0 `0001_platform_baseline.sql` 已建，M2 `0007_integration.sql` 增加 `next_attempt_at`）：`id` / `event_type` / `payload`（id、标题、链接、操作者，不携带正文）/ `status`（pending/sent/failed）/ `attempts` / `last_error` / `next_attempt_at` / 时间戳。
- `external_links`（`0007_integration.sql` 已建）：`subject_type` / `subject_id` / `provider` / `external_id` / `url`，唯一键 `(subject_type, subject_id, provider, external_id)`，用于 Redmine issue 回写、Outline 文档归属等稳定关联（见 model.md「ExternalLink」）；store 提供 `UpsertExternalLink` / `ListExternalLinks`。
- `integration_provider_configs`（`0009_provider_config.sql`）：`provider` / `enabled` / `base_url` / `updated_by` / `updated_at`。只存启停与地址覆盖，**不存凭据**；无行 = 使用环境变量默认值。
- `smtp_configs`（`0011_add_smtp_config.sql`，单行 `config_key='default'`）：`enabled` / `host` / `port` / `username` / `password_enc`（AES-GCM 密文）/ `from_addr` / `updated_by` / `updated_at`。**唯一落库凭据**：SMTP 发件密码，加密存储（`platform.Seal`，密钥 `CHALKLINE_SECRET_KEY`，回退 `CHALKLINE_JWT_SECRET`），API 永不回显。
- 各 provider 的 `base_url` 与凭据：环境变量/密钥文件，**不入库**（base_url 可由管理台覆盖并持久化，凭据永不入库——SMTP 发件密码是唯一例外，见上）。

## 不变量（本模块保证）

- 集成投递失败不改变门禁判定与工作项状态（不变量 15）。
- 材料 `provider` 与 `kind` 必须匹配（不变量 14）——矩阵在 `internal/domain/workitem`，适配器只做可达性校验。
- 外部凭据不落库、不进日志（tech.md 约束 10）。唯一例外：SMTP 发件密码经管理台保存时 AES-GCM 加密落库，API 永不回显、日志禁止出现。
- Redmine 不反向回写状态（integrations.md 明确），状态唯一事实来源是 Chalkline。
- 出站请求防 SSRF（域名白名单 + 不跟任意重定向）。

## 验收标准

- [x] 各 provider 的 `Validate` 正确拒绝非法引用（格式/provider 不匹配，私网/回环地址拒绝）。
- [x] 通知投递器：事件入 outbox → 异步发送 → 成功标 sent；失败重试 5 次后 failed（单测覆盖 30s/60s/120s/240s 退避）。
- [x] 测试覆盖：适配器用 httptest 桩、SMTP 用内存桩、投递失败重试与降级场景。
- [ ] 投递失败时工作流状态不受影响（集成测试：断网后流程仍推进）——需真实 PG，沙箱未执行。
- [x] 凭据不出现在日志与数据库（审计验证：配置只经环境变量，未落库、未打印）。唯一例外 SMTP 发件密码为 AES-GCM 密文，明文与密钥均不落库、不进日志。
- [x] Webhook 签名头可被接收方校验；出站域名白名单生效。
- [x] `go test ./internal/integration/...` 全绿。

## 任务拆解（M2 排期）

1. [x] `internal/integration/provider.go`：定义 `Provider` 接口与注册表（按 kind 找 provider）。
2. [x] `internal/integration/notify/dispatcher.go`：outbox 投递器（后台 worker、指数退避、SMTP/Webhook 路由）。
3. [x] 适配器逐个实现：`git` / `svn` / `outline` / `redmine`（各含 Validate + Snapshot）。
4. [x] 安全：出站域名白名单、SSRF 防护（不跟随重定向 + 私网拦截）、Webhook 签名。
5. [x] app 层接入：`attach_artifact` 调 Validate/Snapshot；状态流转写 outbox（与 workflow app 协作）。
6. [x] 迁移：`external_links` 表（0007）。
7. [x] 测试：适配器 mock 测试、投递失败重试、降级场景。
8. [x] 文档：更新模块索引状态。

## 开放问题

- 各适配器的优先级：M2 先做 `outline` + `git`（门禁材料最常用），`redmine`/`svn` 按需？→ **已定**：M2 四个全部实现，Validate 均为本地校验（不出站），Snapshot 仅 outline/redmine 做，且白名单为空时零出站。
- 是否需要配置化的 provider 启停与 base_url 管理界面？→ **已定并交付**：M3 控制台 `/integrations` + `integration_provider_configs`（凭据仍只读环境变量，见下「Provider 配置可视化」）。
- `next_attempt_at` 与 `attempts` 的语义？→ **已定**：`attempts` 累计失败次数，`next_attempt_at` 控制下次可投递时间（指数退避），达到 5 次标 `failed`。

## M3 增量：Webhook 订阅管理与开放 API 策略

> 已交付。策略详见 [integrations.md](../domain/integrations.md)「Webhook 订阅管理」与「开放 API 策略」。

- **订阅 CRUD**（admin）：`GET/POST /api/v1/webhooks`、`PATCH/DELETE /api/v1/webhooks/{id}`；订阅行只存 `name` / `url` / `event_types`（空 = 全部）/ `status` 与最近投递结果，**不存任何凭据**——签名密钥沿用全局 `CHALKLINE_WEBHOOK_SECRET`。
- **投递路由**：`notify.Dispatcher` 每轮读取 active 订阅，按 `event_types` 过滤后投递，带 `X-Chalkline-Subscription-Id` 与 `X-Chalkline-Signature` 头；**尽力而为**——单个订阅失败记在订阅行（`last_error`），不触发事件重试、不阻断其他订阅。静态通道（环境变量）语义不变（失败重试）。
- **信任模型**：订阅 URL 由 admin 指定，属受信出站，不走材料白名单；投递不跟随重定向、10s 超时。
- **开放 API**：REST + PAT 即开放 API（拉），Webhook 订阅即推送（推）；不做匿名访问与独立 API Key。
- 代码：`internal/store/webhooks.go`、`internal/app/webhooks.go`、`internal/api/handlers_scale.go`、`notify/dispatcher.go`（`SubscriptionSource`）；迁移 `0008_scale.sql`。
- 测试：`notify/dispatcher_subs_test.go`（event_types 过滤、订阅失败不阻断事件、签名头）。

## M3 增量：Provider 配置可视化（已交付）

- **表**：`integration_provider_configs`（迁移 `0009_provider_config.sql`）只存 `provider` / `enabled` / `base_url` / `updated_by` / `updated_at`；**凭据不落库**。
- **REST（admin）**：`GET /api/v1/integration/providers`、`PATCH /api/v1/integration/providers/{kind}`；仅 `enabled` 与 `baseUrl` 可改，`baseUrl` 必须为 http(s) 且不得内嵌凭据。
- **运行时生效**：`integration.Registry.WithConfig` 注入解析器（server 组装层读 store）；停用 → 跳过外部快照（本地格式与私网/回环校验仍执行）；`baseURL` 非空 → 以不可变副本覆盖环境变量值。未写入行 = 使用环境变量默认值。
- **控制台**：`/integrations`（admin）展示 label、生效 baseURL、环境变量默认值、凭据是否已配置（布尔）、启停开关与最近更新人/时间。

## M4 增量：邮箱配置（已交付）

> SMTP 发件服务器管理台。策略详见 [integrations.md](../domain/integrations.md)「邮箱配置」。

- **表**：`smtp_configs`（迁移 `0011_add_smtp_config.sql`，单行 `config_key='default'`）：`enabled` / `host` / `port` / `username` / `password_enc`（AES-GCM 密文）/ `from_addr` / `updated_by` / `updated_at`。**唯一落库凭据**：SMTP 发件密码，加密存储（`internal/platform/secret.go`，密钥 `CHALKLINE_SECRET_KEY`，未设置回退 `CHALKLINE_JWT_SECRET`），API 永不回显。
- **REST（admin）**：`GET /api/v1/integration/smtp`、`PATCH /api/v1/integration/smtp`；字段留空 = 沿用环境变量默认值（`CHALKLINE_SMTP_*`），`password` 空串 = 清除库内密码；写事务 + 审计（`integration.smtp.update`，只记是否设置密码）。
- **运行时生效**：`notify.SMTP.Resolve`（server 组装层注入 `app.Service.EffectiveSMTP`）每轮投递前解析合并配置，保存即生效、无需重启；解密失败按「密码未配置」降级并告警。收件人仍来自 `CHALKLINE_NOTIFY_EMAILS`（页面只读展示）。
- **控制台**：`/smtp`「邮箱配置」（admin，位于 Webhook 订阅之前）。
- 代码：`internal/store/smtp_config.go`、`internal/app/smtpconfig.go`、`internal/api/handlers_integration.go`、`internal/platform/secret.go`。
- 测试：`platform/secret_test.go`（加解密往返、密钥不符）、`app/smtpconfig_test.go`（视图合并、入参校验）、`notify/smtp_resolve_test.go`（Resolve 生效/未启用跳过）。
