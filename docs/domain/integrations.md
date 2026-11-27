# 集成与边界

## 原则

**Chalkline 只负责流程与状态。** 成果物、代码、文档、进度视图、通知都交给既有工具，平台只保存**引用**与**事件**，不复制内容、不重建这些能力。

这样做的目的不是省事，而是避免平台变成「又一个代码托管 + 又一个文档库 + 又一个红点中心」，同时让团队继续用已经沉淀好的工具。

## 迭代 Provider（核心模型）

**Provider 不是全局配置，而是每个迭代一套。** 迭代创建时选定，连接信息随迭代固化；首次提交成果物后冻结（不变量 22）。

| 分类 | 必选 | 可选 provider | 作用 |
| --- | --- | --- | --- |
| **成果物** `artifact` | ✅ 必选且唯一 | `inline` / `outline` / `git` / `svn` | 成果物记录位置。`inline` = 直接存平台内；外部 provider 只回填摘要与链接 |
| **流程记录** `process` | 可选，至多一个 | `redmine` | 每个阶段与任务在指定 project 建票，节点动作作为回复追加到票上 |
| **代码库** `code` | 可选，可多个 | `git` / `svn` | 供 `pr` 材料关联代码引用，也可作为成果物的目录化目标 |

五条规则：

1. **连接信息随迭代固化**：`baseUrl` 与目标（collection / project / 仓库路径）存在迭代绑定上；迭代中途修改全局配置不影响历史成果物的归属。
2. **成果物必选**：没有成果物 provider 的迭代不允许创建（选 `inline` 也要显式提交）。
3. **冻结时机**：迭代（含其下任务）一旦提交过成果物，或迭代进入终态，绑定不可再修改（`409 PROVIDERS_LOCKED`）。
4. **单向分发**：MCP / REST 提交的成果物先落 Chalkline，再由分发器按绑定写外部系统；外部失败只记回执，不阻断门禁（不变量 15）。
5. **摘要而非内容**：外部 provider 的成果物在平台内只保留标题 / 链接 / 外部 ID / 提交时点快照，正文写入外部后即从平台清除（`inline` 绑定除外）。

提交约定：内容类成果物以 `provider=inline` 交给 Chalkline，由绑定决定落到 `outline` / `git` / `svn`；已带外部 URL 的引用（含 `pr`）只做关联，不重复外推。

### 写权限凭据

外部 provider 需要写权限，因此迭代绑定可携带凭据：

| 字段 | 说明 |
| --- | --- |
| `authType` | `token`（API Key / Bearer）或 `basic`（用户名 + 密码） |
| `authUsername` | basic 的用户名（非机密） |
| `authSecret` | Key / 密码：**AES-GCM 加密落库**，API 永不回显，只报「已配置 / 未配置」 |

- 未配置时沿用服务端环境变量（`CHALKLINE_OUTLINE_TOKEN` / `CHALKLINE_REDMINE_TOKEN` / `CHALKLINE_GIT_TOKEN` / `CHALKLINE_SVN_TOKEN`）。
- **个人凭据优先**：提交人可在「我的凭据」维护本人 Key（AES-GCM 密文落库，永不回显）；出站解析顺序为 提交人个人凭据 → 迭代绑定凭据 → 服务端环境变量。未配置个人 Key 时界面告警并回退公共凭据，不阻断提交。
- **凭据更新不受冻结限制**：结构配置（provider / 地址 / 目标）在首次提交成果物后冻结，但 Key 与用户名密码可随时轮换（`PUT /api/v1/iterations/{id}/providers/{providerId}/credential`）。
- 加密密钥 `CHALKLINE_SECRET_KEY`（未设置回退 `CHALKLINE_JWT_SECRET`）；密钥变更导致解密失败时按「未配置」降级并告警，不阻断流程。
- 轮换时可只提交新 Key（用户名保持不变）；留空 Secret 表示保持库内已有密文。

全局 provider 配置只有一张**服务器列表**（`provider_endpoints`）：每条记录一个服务器的类型、名称与地址，没有「默认 git / 默认 svn / 默认 outline / 默认 redmine」的概念（按 kind 的默认配置表 `integration_provider_configs` 已随迁移 0018 移除）。任何选择 provider 的地方都从列表取下拉建议，也可手填任意地址。

### 服务器列表（全局 Provider 配置）

**服务器列表**（`provider_endpoints`，迁移 0017）回答「这套部署能连哪些外部系统」：

- **admin 增删改查**（`GET/POST /api/v1/integration/endpoints`、`PATCH/DELETE /api/v1/integration/endpoints/{id}`）：`kind` + 展示名 `label` + `baseUrl` + 备注 + 启停；同一 `(kind, baseUrl)` 不重复；列表**只存地址与展示名，不存任何凭据**。
- **成员可读已启用项**（`GET /api/v1/integration/endpoints`，admin 看全部含停用）：迭代创建 / 配置弹窗、我的凭据页把列表项作为 baseURL 下拉建议（带展示名），**也可手填任意地址**——列表是建议清单，不是白名单。
- **管理台**：`/integrations`（admin）即服务器列表，列表上方是类型按钮条（「所有」+ 各类型，带数量），一键筛选。
- 删除列表项不影响已保存的迭代绑定（连接信息随迭代固化），只是不再出现在选择列表。

### 个人凭据（以员工身份出站）

外部系统里的成果物 / 流程记录若都以公共 Key 提交，就看不到「对应员工」。因此引入**个人凭据**（`user_provider_credentials`，迁移 0017）：

- **成员自助维护**（`GET/PUT /api/v1/me/credentials`、`DELETE /api/v1/me/credentials/{provider}?baseUrl=…`）：按 `(provider, baseUrl)` 配置自己的 Key（`baseUrl` 必填——凭据按具体服务器地址生效，没有「默认地址」；地址从服务器列表选择或手填）；`authType` 为 `token` / `basic`，Secret **AES-GCM 加密落库**（`platform.Seal`），API 永不回显，只报「已配置 / 未配置」；留空 Secret 表示保持库内已有密文。
- **出站优先级**：提交人个人凭据 → 迭代绑定凭据 → 服务端环境变量（`CHALKLINE_*_TOKEN`）。成果物分发与流程记录（建票 / 回复）都按此解析；个人凭据解密失败按「未配置」降级并告警，不阻断流程（不变量 15）。
- **界面告警**：迭代绑定视图（`GET /api/v1/iterations/{id}/providers` 与迭代详情）为每条外部绑定标注 `personalSecretConfigured`——当前查看人未配置个人 Key 时，迭代详情页提示「将以公共凭据出站」，并引导到控制台「我的凭据」页维护。
- **入口分层**：迭代级公共 Key 在**迭代详情页最下方** Provider 卡片（按钮为「更新公共凭据」，需产品成员 + `pm` 或 admin；视图以 `canManageCredential` / `canManageBinding` 告知前端是否显示，普通成员不会点到 403）；个人 Key 在左侧菜单**「我的凭据」**（全员可读写自己的）。材料提交 / 补挂表单顶部也提示本次将以谁的 Key 出站，缺个人 Key 时给出跳转入口。
- **不强制**：未配置个人 Key 不阻断提交（回退公共凭据）；团队若要求「必须以本人身份提交」，以页面告警 + 管理约定推动，不在门禁上强制——门禁只认流程事实，不认外部系统身份。

### 数据流

```
Agent ──MCP──> Chalkline（成果物 + 节点/任务事件）
                   │  按该迭代绑定分发（尽力而为，不阻断流程）
                   ├─ 成果物：inline 存平台内 / outline 建文档 / git 提交文件 / svn WebDAV 写入
                   ├─ 流程记录：建票 + 追加回复（只出站，不回写状态）
                   └─ 代码库：pr 材料关联代码引用
```

### 目录结构约定

`git` / `svn` 的成果物按 `dirTemplate` 落到仓库，默认 `chalkline/{iteration}/{node}`，文件名 `<kind>.md`，支持 `{iteration}` / `{node}` / `{kind}` 占位符。`outline` 没有目录概念，以「集合 + 标题约定」组织。

Node 详情页按迭代绑定展示三类连接，可直接点击跳转（`linkUrl`）。

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

- `provider` 决定校验方式与展示形式；具体由迭代绑定的 provider 决定是否分发、分发到哪里。
- `externalId` 用于去重与回查，不要求所有 provider 都支持。
- `title` / `version` 等字段是**快照**：外部内容变化不回写，避免双向同步。
- `inline` 仅在团队没有对应工具时使用，存平台内 Markdown，明确标注为轻量说明，不承载长文档。
- 外部 provider 的材料只落摘要：正文不复制进 Chalkline，分发回执写 `integration_syncs`（含外部链接与摘要）。

## 适配器接口

```go
// 引用校验与快照（门禁挂接时）
type Provider interface {
    Kind() workitem.ArtifactProvider
    Validate(ctx context.Context, ref workitem.ArtifactRef) error
    Snapshot(ctx context.Context, ref workitem.ArtifactRef) (workitem.ArtifactMeta, error)
}

// 成果物分发（app 层通过依赖倒置调用，integration.Dispatcher 实现）
type ArtifactDistributor interface {
    PushArtifact(ctx context.Context, b workitem.ProviderBinding, ref workitem.ArtifactRef,
        meta workitem.ArtifactMeta, dir string) (workitem.PushedArtifact, error)
}

// 流程记录（Redmine 建票 + 追加回复）
type ProcessRecorder interface {
    EnsureTicket(ctx context.Context, b workitem.ProviderBinding, subject workitem.ProcessSubject) (workitem.ProcessTicket, error)
    CommentTicket(ctx context.Context, b workitem.ProviderBinding, ticket workitem.ProcessTicket, note workitem.ProcessNote) error
}
```

已实现：`git`、`svn`、`outline`、`redmine`、`smtp`、`webhook`、`file`。

适配器只做协议转换与校验，**不得写入领域状态**；分发回执由 app 写入 `integration_syncs`。

## 同步方向

**单向优先，避免双主。**

| 集成 | 方向 | 说明 |
| --- | --- | --- |
| Outline | 出站写入 + 出站链接 | 绑定时把成果物建成文档（集合 + 标题约定），门禁材料指向该文档；不拉取正文 |
| Redmine | 出站写入 | 阶段/任务建票并追加动作回复；只回写 `issueId` 与链接，**不反向回写状态** |
| Git / SVN | 出站引用 + 出站写入 | `pr` 只存链接不克隆仓库；成果物按目录模板提交文件（Git 走 commits API，SVN 走 WebDAV） |
| 邮件 | 出站通知 | 门禁待办、驳回、超期提醒、节点/迭代进展 |
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

- **邮件收件人按事件解析**：投递器注入 `notify.SMTP.Recipients`（server 组装层调 `app.Service.NotifyRecipients`），按事件语义取相关成员邮箱：节点进展 → 工作项责任人；`gate.pending` / `gate.overdue` → 责任人 + 当前节点合格评审人（按实例模板 `gate.roles`）；`node.passed` / `node.rejected` → 另加产品 owner；`gate.escalated` → 另加 admin；`task.assigned` → 被指派人；`iteration.closed` → 创建人 + 产品 owner + 迭代内任务负责人。解析不出时回退 `CHALKLINE_NOTIFY_EMAILS`（兜底收件人，可留空）。
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
- **收件人**：不再群发固定列表，按事件语义解析为**相关成员**，地址取自成员设置里的邮箱（`users.email`）——节点进展通知工作项责任人（任务负责人 / 迭代创建人），`gate.pending` / `gate.overdue` 通知当前节点的合格评审人，`node.passed` / `node.rejected` / `iteration.closed` 另通知产品 owner，`gate.escalated` 升级给产品 owner 与 admin。`CHALKLINE_NOTIFY_EMAILS` 保留为**兜底收件人**（只在解析不出任何相关成员时使用），页面只读展示。
- **测试邮件**：`POST /api/v1/integration/smtp/test`（admin，控制台「发送测试邮件」按钮）用当前生效配置向**当前登录账号的邮箱**发送一封测试邮件，即时验证连通性；失败返回可展示的错误（拨号/握手/认证/TLS 各阶段），并写审计 `integration.smtp.test`。
- **生效时机**：投递器每轮投递前重新解析生效配置，保存后无需重启；密钥变更导致解密失败时按「密码未配置」降级并告警，不阻断投递。

## 安全

- 全局连接配置与迭代绑定里的 `baseUrl` 只存地址，**不得内嵌凭据**（保存时校验并拒绝）。
- 凭据来源：迭代级 `authType/authSecret`（AES-GCM 加密落库）优先，未配置时回退服务端环境变量（`CHALKLINE_OUTLINE_TOKEN` / `CHALKLINE_REDMINE_TOKEN` / `CHALKLINE_GIT_TOKEN` / `CHALKLINE_SVN_TOKEN`）。环境变量与加密密钥都不进日志；API 永不回显凭据，只报是否已配置。
- 加密密钥 `CHALKLINE_SECRET_KEY`（未设置回退 `CHALKLINE_JWT_SECRET`）。这是「凭据不落库」的两处例外之一（另一处是 SMTP 发件密码），两者共用同一实现（`platform.Seal`）。
- 出站请求做域名白名单校验（`CHALKLINE_OUTBOUND_ALLOWLIST`），禁止跟随任意重定向（防 SSRF）；白名单为空时拒绝一切出站，分发失败只记回执。
- Webhook 载荷带签名头，接收方可校验。
- 邮件只发内网 SMTP，默认关闭对外直发。

## 降级

- 集成不可用时，控制台把该 provider 标为「未连通」，流程照常推进。
- 材料若来自外部系统，门禁只校验**引用存在且格式合法**，不要求外部系统实时可访问。
- 模板未来可声明「外部校验」（如「PR 必须已合并」），当前不实现，仅在配置结构上预留。
