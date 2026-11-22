# AGENT.md

> AI 编码代理与新成员的开发入口。本文件只做路由与红线，细则以 docs/ 为准；两者冲突时，修本文件。README 面向使用者，本文件面向改造者。

## 项目是什么

Chalkline（墨线）：面向 5–50 人研发团队的规格驱动开发（SDD）协作平台。产品 → 模块 → 迭代 → 任务纳入可定制工作流，服务端门禁保证「规格先行、评审留痕、按步推进」，编码 Agent 通过 MCP over HTTP 接入同一份事实。

一句话定位：**给 AI 开发装上流程和门禁，让人只负责裁决。**

架构：Go 单体（`chalklined`）同时提供 MCP 与 REST + React SPA + PostgreSQL，nginx 终止 TLS。代码、文档、通知等成果物全部外置（Git / Outline / Redmine / SMTP），平台只存引用与事件，**单向连接，不做双向同步**。

## 当前状态

**M0（骨架）已交付**：

- 领域模型四包 `internal/domain/{identity,product,planning,workitem}`（含 model.md 不变量的纯函数校验）
- 工作流引擎 `internal/workflow`：YAML 定义解析与 DAG 校验、5 个内置模板（embed）、状态机（ready/in_progress/awaiting_review/passed/rejected）、门禁会签判定、AND 并行汇合、驳回重提保留审批、next_action 投影
- store：pgx 连接池、goose 内嵌迁移（0001 baseline / 0002 workflows）、内置模板种子；api：chi 路由 + 中间件 + 统一错误包络（`/healthz` `/readyz` `/api/v1/info`）
- 部署栈 `deploy/`：compose 四服务（postgres/api/web/nginx）、多阶段 Dockerfile、nginx TLS + 首启自签证书；`Makefile` 常用命令

**尚未开始（M1+）**：鉴权与登录、业务 CRUD、MCP 网关、Agent Skill、控制台、集成适配。模块划分见 [docs/modules/README.md](docs/modules/README.md)，里程碑见 README「路线图」。

## 按任务读文档

| 你要做的事 | 先读 |
| --- | --- |
| 写任何业务规则 / 校验 | [docs/domain/model.md](docs/domain/model.md)——不变量清单是硬边界 |
| 工作流判定、门禁语义、模板格式 | [docs/domain/workflow.md](docs/domain/workflow.md) |
| 加 / 改 MCP 工具 | [docs/domain/mcp.md](docs/domain/mcp.md)——工具名与错误码发布后不可改名 |
| 外部系统接入（Git / Outline / Redmine / SMTP / Webhook） | [docs/domain/integrations.md](docs/domain/integrations.md) |
| 不知道文件放哪 | [docs/structure.md](docs/structure.md)「放置规则」 |
| 想换技术栈 / 加依赖 | [docs/tech.md](docs/tech.md)——先看「硬性约束」，大概率答案是「不许」 |
| 不确定需求是否在范围内 | [docs/product.md](docs/product.md)「非目标」 |
| Agent Skill 话术与验收 | [docs/domain/skill.md](docs/domain/skill.md) |

## 红线

违反任何一条即为错误，评审会直接打回：

1. **状态只能经工作流引擎修改**——handler / MCP 工具 / 定时任务都不得直接 UPDATE 工作项状态。
2. **门禁判定只在服务端**——客户端与 Agent 传来的一切「已通过」一律忽略，只认数据库中的审批与材料记录。
3. **MCP 与 API 只做协议转换**——参数校验在领域层，业务规则不得写在工具或 handler 里。
4. **依赖方向固定**：`api/mcp → app → domain / workflow`；`app → store / integration`（经接口）。`domain` 与 `workflow` 不 import `database/sql`、`net/http`、store、api、mcp。
5. **所有写操作在事务内**，携带 `actor_id` 并写审计日志。
6. **不引入新的 Web 框架、ORM、消息队列**；选型以 docs/tech.md 为准，不得擅自替换。
7. **错误码语义固定**：403 权限不足 / 404 不存在或不可见 / 409 状态或门禁冲突 / 422 参数非法；非成员访问产品返回 404 而非 403，不暴露产品是否存在。
8. **外部系统只经 `internal/integration/` 访问**；凭据不落库、不进日志。

## 实现顺序

每个功能块固定从领域往外做，**禁止从 handler 倒推**：

```
domain 不变量 → workflow 判定 → migrations → store 查询 → app 用例 → api/mcp 入口 → 前端
```

## 常用命令

```bash
go run ./cmd/chalklined             # 起后端（需本地 PG）
go test ./...                       # 单元测试
go test -tags=integration ./...     # 集成测试（testcontainers 起真实 PG）
go vet ./... && golangci-lint run   # 提交前必须通过
sqlc generate                       # 由 SQL 生成仓储代码（store/sqlc 禁止手改）
goose -dir migrations up            # 应用迁移（只前进，不回滚）
cd web && pnpm dev                  # 前端开发服务器
docker compose -f deploy/docker-compose.yml up -d
```

## 命名速查

| 对象 | 约定 | 示例 |
| --- | --- | --- |
| 数据库表 / 列 | 复数 snake_case / snake_case | `gate_approvals` / `assignee_id` |
| REST 路径 | 复数小写，层级 ≤ 三层 | `/api/v1/products/{id}/iterations` |
| MCP 工具 | `chalkline_<动词>_<对象>` | `chalkline_get_next_action` |
| 迁移文件 | `NNNN_动词_对象.sql`，只增不改 | `0007_add_gate_artifacts.sql` |
| 分支 | `feat/<模块>-<简述>` | `feat/workflow-gate-sla` |
| 提交 | `type(scope): 描述` | `feat(workflow): 支持多级门禁` |

## 文档即规格

- 代码与 `docs/domain/` 冲突时**以文档为准修代码**；确需偏离，必须在同一次提交内对齐文档。
- MCP 新增 / 变更工具时，`docs/domain/mcp.md` 与 `agent/skills/chalkline/` 必须同一提交更新——Skill 中的工具名与错误码要能在 mcp.md 中找到，CI 会校验。
- 长期文档（product / tech / structure）的改动需在提交信息里说明原因。

## 动手前自检

1. 这个改动落在哪个模块？（见 [docs/modules/README.md](docs/modules/README.md) 模块清单）
2. 触碰了哪条不变量？（见 model.md「不变量」一节）
3. 是不是在正确的层？（见 structure.md「放置规则」）
4. 有没有绕过引擎直接改状态？（有就停）
