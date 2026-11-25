# 集成与边界

## 原则

**Chalkline 只负责流程与状态。** 成果物、代码、文档、进度视图、通知都交给既有工具，平台只保存**引用**与**事件**，不复制内容、不重建这些能力。

这样做的目的不是省事，而是避免平台变成「又一个代码托管 + 又一个文档库 + 又一个红点中心」，同时让团队继续用已经沉淀好的工具。

## 边界

| 能力 | 由谁承担 | Chalkline 保存 |
| --- | --- | --- |
| 代码 | Git / GitLab / Gitea / SVN | 仓库、分支、commit / MR 链接 |
| 文档与知识库 | Outline | 文档 URL、`docId`、标题与版本快照 |
| 进度说明 | Redmine | issue id 与 URL（单向导出） |
| 通知 | 邮件（SMTP）、Webhook | 事件内容与投递记录 |
| 附件 | 本地存储 / 对象存储 | 文件元数据（名称、大小、sha256、存储键） |
| CI 结果 | CI 系统 | 构建号与结果链接 |

平台自己只保留**门禁必需的最小说明**（一段 Markdown 备注、驳回理由、审批意见），不承担知识库职能。

## 引用模型

材料（Artifact）不是内容，而是引用：

```yaml
kind: pr                      # 材料类型
provider: git                 # git | svn | outline | redmine | file | url | inline
url: https://git.example.com/team/chalkline/-/merge_requests/42
externalId: "!42"             # 外部系统的稳定标识
title: "feat(workflow): 支持多级门禁"   # 快照，便于展示与审计
```

- `provider` 决定校验方式与展示形式。
- `externalId` 用于去重与回查，不要求所有 provider 都支持。
- `title` / `version` 等字段是**快照**：外部内容变化不回写，避免双向同步。
- `inline` 仅在团队没有对应工具时使用，存平台内 Markdown，明确标注为轻量说明，不承载长文档。

## 适配器接口

```go
type Provider interface {
    Kind() string
    Validate(ctx context.Context, ref Ref) error   // 校验可达与格式
    Snapshot(ctx context.Context, ref Ref) (Meta, error) // 取标题/版本/状态快照
}
```

已规划实现：`git`、`svn`、`outline`、`redmine`、`smtp`、`webhook`、`file`。

适配器只做协议转换与校验，**不得写入领域状态**。

## 同步方向

**单向优先，避免双主。**

| 集成 | 方向 | 说明 |
| --- | --- | --- |
| Outline | 出站链接 | 门禁材料指向 Outline 文档；不拉取正文 |
| Redmine | 出站导出 | 迭代/任务状态导出为 issue 或更新状态；只回写 `issueId` 与链接 |
| Git / SVN | 出站引用 | 只存链接，不克隆仓库 |
| 邮件 | 出站通知 | 门禁待办、驳回、超期提醒 |
| Webhook | 出站事件 | 通用订阅，供外围系统消费 |

Redmine 明确**不反向回写状态**：流程状态的唯一事实来源是 Chalkline，否则两边冲突无法裁决。若团队以 Redmine 为准，则不应把该能力接入。

## 事件与投递

所有状态流转产生领域事件，写入 **outbox 表**后由投递器异步发送：

| 事件 | 触发 |
| --- | --- |
| `node.ready` | 节点可开始 |
| `node.passed` | 门禁通过 |
| `node.rejected` | 门禁驳回 |
| `gate.pending` | 进入等待评审，通知有权限的评审人 |
| `gate.overdue` | 评审超过 SLA 未裁决，提醒评审人（M3） |
| `gate.escalated` | 评审超过升级阈值，升级给产品 owner / admin（M3） |
| `task.assigned` | 任务指派 |
| `iteration.closed` | 迭代收尾 |

投递规则：

- 静态通道（环境变量 `CHALKLINE_WEBHOOK_URL` / SMTP）：指数退避重试，最多 5 次，超过标记 `failed` 并在控制台告警。SMTP 发件服务器与账号可经管理台「邮箱配置」维护（见下节），环境变量仍是默认值与回退来源。
- **Webhook 订阅（M3）**：投递器每轮读取 `webhook_subscriptions` 中 `active` 的订阅，按 `event_types` 过滤后 POST 到各自 URL，带 `X-Chalkline-Subscription-Id` 与 `X-Chalkline-Signature` 头；**尽力而为**——单个订阅失败只记在该订阅行（`last_error`），不阻断事件流转，也不触发事件重试。
- **投递失败不阻断流程**：门禁判定不依赖通知是否送达。
- 事件内容只含 id、标题、链接、操作者，不携带正文。
- 控制台 SSE 实时推送（`/api/v1/events`）尾随同一 outbox，是只读投影，不参与投递状态机。

## Webhook 订阅管理（M3）

外部系统消费事件的正式入口：admin 在控制台或 REST（`/api/v1/webhooks`）维护订阅——`name` / `url` / `event_types`（空 = 全部）/ `status`。

- **签名**：全部订阅共用全局 `CHALKLINE_WEBHOOK_SECRET`（HMAC-SHA256，`X-Chalkline-Signature: sha256=<hex>`）。单部署服务一个团队（product.md 非目标：不做多租户），共享密钥是刻意取舍：凭据不落库（红线 8），轮换走环境变量。
- **信任模型**：订阅 URL 由 admin 显式指定，属受信出站，不走材料适配器的出站白名单；投递不跟随重定向、10s 超时。
- **接收方校验**：以密钥对 body 做 HMAC-SHA256，与 `X-Chalkline-Signature` 比对；`X-Chalkline-Subscription-Id` 区分订阅来源。

## 开放 API 策略（M3）

- **拉**：REST `/api/v1/...` 即开放 API。外部系统用 PAT（`Authorization: Bearer <PAT>`）访问，与 Agent 同一权限模型——PAT 属于某个成员，能看到的与该成员一致（产品内开放、跨产品 404）。
- **推**：Webhook 订阅（上节）。事件只含元数据，外部系统需要细节时再经 REST 回查。
- 不做匿名访问、不做独立 API Key 体系：单团队部署下，成员身份 + PAT 已覆盖全部外部消费方；新增凭据形态只会增加泄露面。

## 邮箱配置（M4）

admin 在控制台「邮箱配置」页（位于 Webhook 订阅之前）或 REST（`GET/PATCH /api/v1/integration/smtp`）维护发件服务器：`enabled` / `host` / `port` / `username` / `from` / `tlsMode` / `tlsSkipVerify` 与发件密码。

- **密码存储**：AES-GCM 加密落库（`smtp_configs.password_enc`），密钥来自 `CHALKLINE_SECRET_KEY`（未设置回退 `CHALKLINE_JWT_SECRET`）；API 永不回显，只报「已配置 / 未配置」。这是「凭据不落库」的唯一例外（AGENT 红线 8 / tech.md 约束 10）。
- **TLS/STARTTLS**：`tlsMode` ∈ `none`（明文，默认）/ `starttls`（连接后 EHLO 协商升级，587 常用）/ `tls`（隐式 TLS，465 常用）；`tlsSkipVerify` 跳过证书校验，**仅限内网自签证书场景**，公网 SMTP 必须关闭。
- **合并语义**：未保存过或字段留空 = 沿用环境变量默认值（`CHALKLINE_SMTP_*`，TLS 默认值来自 `CHALKLINE_SMTP_TLS` / `CHALKLINE_SMTP_TLS_SKIP_VERIFY`）；`enabled=false` 即整体停发；密码提交空串 = 清除库内密码（回退环境变量）；`tlsMode` 提交空串 = 清除库内设置（回退环境变量）。
- **收件人**：仍经 `CHALKLINE_NOTIFY_EMAILS` 配置（页面只读展示），不进库。
- **测试邮件**：`POST /api/v1/integration/smtp/test`（admin，控制台「发送测试邮件」按钮）用当前生效配置向收件人发送一封测试邮件，即时验证连通性；失败返回可展示的错误（拨号/握手/认证/TLS 各阶段），并写审计 `integration.smtp.test`。
- **生效时机**：投递器每轮投递前重新解析生效配置，保存后无需重启；密钥变更导致解密失败时按「密码未配置」降级并告警，不阻断投递。

## 安全

- 各 provider 的 `baseUrl` 与凭据存环境变量或密钥文件，**不落库存明文**。唯一例外：SMTP 发件密码可经管理台保存，以 AES-GCM 加密落库（密钥 `CHALKLINE_SECRET_KEY`，未设置回退 `CHALKLINE_JWT_SECRET`），API 永不回显、不进日志。
- 出站请求做域名白名单校验，禁止跟随任意重定向（防 SSRF）。
- Webhook 载荷带签名头，接收方可校验。
- 邮件只发内网 SMTP，默认关闭对外直发。

## 降级

- 集成不可用时，控制台把该 provider 标为「未连通」，流程照常推进。
- 材料若来自外部系统，门禁只校验**引用存在且格式合法**，不要求外部系统实时可访问。
- 模板未来可声明「外部校验」（如「PR 必须已合并」），当前不实现，仅在配置结构上预留。
