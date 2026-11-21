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
| `task.assigned` | 任务指派 |
| `iteration.closed` | 迭代收尾 |

投递规则：

- 指数退避重试，最多 5 次，超过标记 `failed` 并在控制台告警。
- **投递失败不阻断流程**：门禁判定不依赖通知是否送达。
- 事件内容只含 id、标题、链接、操作者，不携带正文。

## 安全

- 各 provider 的 `baseUrl` 与凭据存环境变量或密钥文件，**不落库存明文**。
- 出站请求做域名白名单校验，禁止跟随任意重定向（防 SSRF）。
- Webhook 载荷带签名头，接收方可校验。
- 邮件只发内网 SMTP，默认关闭对外直发。

## 降级

- 集成不可用时，控制台把该 provider 标为「未连通」，流程照常推进。
- 材料若来自外部系统，门禁只校验**引用存在且格式合法**，不要求外部系统实时可访问。
- 模板未来可声明「外部校验」（如「PR 必须已合并」），当前不实现，仅在配置结构上预留。
