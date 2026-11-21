# Chalkline · 墨线

> 先弹线，后施工。 Snap the line. Build to it.

Chalkline 是面向研发团队的规格驱动开发（SDD）协作平台。它把 **产品 → 模块 → 迭代 → 任务** 全部纳入可定制的工作流，用服务端门禁保证「规格先行、评审留痕、按步推进」，并让每个成员的编码 Agent 通过 MCP 远程接入同一份事实。

木匠开工前先用墨斗弹一条基准线，所有构件照线而做。Chalkline 弹的是规格线。

## 它解决什么问题

| 做法 | 卡在哪 |
| --- | --- |
| 在聊天框里不停推着 AI 前进 | 没有稳定上下文，越聊越漂移；换个会话就重来；无法多人协作 |
| 单机版 SDD 工具（Spec Kit / OpenSpec） | 规格只是本地 Markdown，没有角色、没有门禁、没有进度视图 |
| spec-workflow-mcp | 阶段序列写死，流程不可定制；MCP 为 stdio，团队协作要靠桥接 + git 双头同步 |
| **Chalkline** | 工作流是配置、门禁在服务端裁决、多用户一个实例、Agent 通过 HTTP 原生接入 |

## 核心概念

| 概念 | 说明 |
| --- | --- |
| 开发者 Developer | 平台成员，持有角色（管理员 / 产品经理 / 评审人 / 开发者 / 测试），任务分配给具体的人推进 |
| 产品 Product | 顶层容器，拥有产品文档、模块树与多个迭代 |
| 模块 Module | 产品下的多级树，是任务归属的「块」；一个任务可跨多个模块 |
| 迭代 Iteration | **一个交付周期 = 一条完整生命周期**，从调研走到总结，承载任务 |
| 任务 Task | 工作项，挂在迭代下、关联模块，支持多级子任务拆解，有唯一负责人 |
| 工作流 Workflow | 可定制的节点图，节点之间可挂门禁；迭代与任务各自选模板 |
| 门禁 Gate | 节点上的准入条件：需 N 名指定角色确认 + 指定材料上传，缺一不可 |
| 材料 Artifact | 门禁所需证据的**引用**：Outline 文档、Git/SVN 变更、测试报告、附件 |

## 生命周期

**一个迭代就是一条完整生命周期**，产品则是迭代的序列。默认模板长这样，每一环的门禁都可改：

```
迭代（生命周期）：竞品调研 ◆ → 关键技术调研 ◆ → 产品规划 ◆ → 任务拆解 ◆ → 研发 → 测评 ◆ → 发布 ◆ → 总结 ◆
                                                                      │
                                                                      ├─ 一级任务 CHK-12
                                                                      │    └─ 二级任务 CHK-12.3
                                                                      └─ 任务流：澄清 → 规格 ◆ → 实现 → 测试设计 ◆ → 测试执行 ◆ → 完成
```

调研、测试、总结不是编码任务的附属品，而是独立的任务类型（`research` / `test` / `summary`），各有自己的流程与必交材料：

| 阶段 / 节点 | 门禁要求 |
| --- | --- |
| 竞品调研 / 关键技术调研 | 调研报告（关键技术另需验证代码） |
| 产品规划 | 规格文档 |
| 任务拆解 | 人工确认任务树（一级任务是否覆盖规格、粒度是否可分派） |
| 测试设计 / 用例评审 | 测试计划、用例集 |
| 测试执行 / 测评 | 执行记录、测试报告 |
| 发布 | 测试报告 + 代码链接 |
| 总结 | 迭代或任务总结文档 |

阶段之间还有硬约束：只有「研发」阶段允许创建任务，所以调研与规划的门禁没通过时，任何人都无法开工。内置模板只是默认配置，可随时编辑或复制。详见 [docs/domain/workflow.md](docs/domain/workflow.md)。

## 与外围系统的边界

Chalkline 不做代码托管、不做文档库、不做通知中心。它只保存**引用**和**事件**，能力交给团队已在用的工具：

| 能力 | 由谁承担 | Chalkline 保存 |
| --- | --- | --- |
| 代码 | Git / GitLab / Gitea / SVN | 分支、commit / MR 链接 |
| 文档与知识库 | Outline | 文档 URL、`docId`、标题快照 |
| 进度说明 | Redmine | issue id 与链接（单向导出） |
| 通知 | 邮件（SMTP）、Webhook | 事件内容与投递记录 |
| 附件 | 本地 / 对象存储 | 文件元数据 |

好处是团队不用搬家：已有工具继续用，Chalkline 只补上流程与门禁这一层。详见 [docs/domain/integrations.md](docs/domain/integrations.md)。

## 关键机制

**工作流即配置。** 迭代与任务两层的工作流都以 YAML 定义：节点、连线、门禁条件全部可改。内置模板只是初始化数据，与自建模板完全等价。

**门禁即服务端裁决。** 门禁通过与否由服务端判定，不由对话上下文决定。Agent 在聊天里说「已通过评审」无效——没有评审人在平台上点确认、没有材料上传记录，状态就不会前进，接口直接返回 `409 GATE_BLOCKED` 并说明缺什么。

**询问下一步，而不是猜下一步。** Agent 调 `get_next_action` 得到结构化的 `{step, status, blockers[]}`：`ready` 才能开工，`blocked` 会明确告诉它卡在哪个门禁、缺哪份材料、该找谁批。

**只做流程，成果物外置。** 门禁只校验材料引用存在且格式合法，不要求 Outline 或 Git 实时在线——外围系统挂了不该阻断评审。通知走 outbox 异步投递，失败重试但不影响状态流转。

**全员一个实例。** 服务端部署（docker compose），MCP 走 HTTP 原生支持多人并发，不需要 supergateway 之类的桥接，也不靠 git 同步规格。

## 架构

```
┌───────────────┐   ┌───────────────┐   ┌───────────────┐
│ Trae / Claude │   │ Cursor / 其他 │   │ 浏览器（人类） │
│  Code（Agent）│   │   Agent       │   │  控制台        │
└───────┬───────┘   └───────┬───────┘   └───────┬───────┘
        │ MCP over HTTPS    │                   │ HTTPS
        └───────────┬───────┘                   │
                    ▼                           ▼
        ┌───────────────────────────────────────────┐
        │    nginx（TLS 终止 / 反代 / 前端静态）      │
        └───────────────────┬───────────────────────┘
                            ▼
        ┌───────────────────────────────────────────┐
        │            chalklined（Go 单体）           │
        │  MCP 网关 │ HTTP API │ 工作流引擎 │ 鉴权   │
        └───────────────────┬───────────────────────┘
                            ▼
                     ┌─────────────┐
                     │ PostgreSQL  │
                     └─────────────┘
```

同一实例同时提供 MCP 与 REST：Agent 读写工作项，人在控制台评审与看进度。技术选型见 [docs/tech.md](docs/tech.md)。

## 快速开始

```bash
git clone https://github.com/yuylong/Chalkline.git
cd Chalkline
cp .env.example .env          # 数据库密码、JWT 密钥、证书路径
docker compose -f deploy/docker-compose.yml up -d
```

启动后：

- 控制台：`https://localhost`（nginx 终止 TLS）
- MCP 端点：`https://localhost/mcp`（Agent 配置见下）

在 Agent 的 MCP 配置里加入：

```json
{
  "mcpServers": {
    "chalkline": {
      "type": "http",
      "url": "https://<服务器地址>/mcp",
      "headers": { "Authorization": "Bearer <个人访问令牌>" }
    }
  }
}
```

然后在对话里问它：「我在 Chalkline 里的下一步是什么？」

## 文档

| 文档 | 内容 |
| --- | --- |
| [docs/product.md](docs/product.md) | 产品愿景、目标用户、非目标、设计原则 |
| [docs/tech.md](docs/tech.md) | 技术选型、技术栈、开发命令、硬性约束 |
| [docs/structure.md](docs/structure.md) | 目录职责、放置规则、命名约定、分层边界 |
| [docs/domain/model.md](docs/domain/model.md) | 领域模型、实体关系、不变量 |
| [docs/domain/workflow.md](docs/domain/workflow.md) | 工作流定义格式、门禁语义、状态判定 |
| [docs/domain/integrations.md](docs/domain/integrations.md) | 外部系统集成契约、同步方向、降级策略 |
| [docs/domain/mcp.md](docs/domain/mcp.md) | MCP 工具清单、返回契约、错误码 |
| [docs/modules/README.md](docs/modules/README.md) | 功能模块索引，一块对应一批开发任务 |

## 路线图

- **M0 · 骨架**（进行中）：仓库结构、领域模型、工作流引擎核心判定、docker compose 起服务
- **M1 · 单产品跑通**：成员与角色、产品/模块/迭代/任务 CRUD、迭代生命周期 + 任务两层工作流与预置模板、门禁审批、MCP 工具全集、控制台基础视图
- **M2 · 协作深化**：任务跨模块与多级拆解看板、迭代燃尽与进度总览、审批队列、审计留痕检索、邮件通知与 outbox 投递
- **M3 · 规模化**：Outline / Redmine / Git 适配器、多产品、工作流模板市场（导入导出）、开放 API 与 Webhook

## 参与开发

```bash
go run ./cmd/chalklined        # 起后端（需本地 PG）
cd web && pnpm dev             # 起前端
go test ./...                  # 跑测试
```

提交前请确保 `go vet ./...` 与 `golangci-lint run` 通过。任务流程本身也用 Chalkline 管理。

## 许可

BSD 3-Clause，见 [LICENSE](LICENSE)。
