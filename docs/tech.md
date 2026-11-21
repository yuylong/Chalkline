# 技术架构

> 本文件是 Chalkline 的技术上下文，每次 Agent 交互都会注入。AI 提出实现方案时优先使用这里的技术栈，不得擅自引入替代品。

## 语言选型：Go

结论：**Go**。这是一个长期运行、以状态机与关系数据为核心、需要单实例承载多人并发长连接的协作服务。

| 维度 | Go | Rust | Python |
| --- | --- | --- | --- |
| 部署形态 | 静态单二进制，镜像 ~20MB | 单二进制，镜像更小 | 需解释器与依赖隔离，镜像与启动都重 |
| 并发模型 | goroutine 天然适配多路 SSE/HTTP 长连接 | tokio 更强，但心智负担高 | GIL 限制，异步生态分叉 |
| 开发速度 | 快，语法克制，新人一天上手 | 慢，借用检查与生命周期成本高 | 最快，但工程约束弱 |
| 领域建模表达力 | 中（无 sum type，用枚举 + 接口弥补） | 高（enum + match 最适合状态机） | 高 |
| 性能 | 足够（瓶颈在 DB 与网络 IO） | 过剩 | 足够，但并发场景需绕 |
| 可维护性 | 强，格式与工具链统一 | 强，但团队门槛高 | 依赖团队自律 |

Rust 的表达力最贴合状态机，但此项目的瓶颈不在 CPU，付出开发速度换性能不划算。Python 起动快，但长期运行服务与打包部署的成本会在 M1 之后反噬。**Go 的短板是缺少代数数据类型**，用「显式状态枚举 + 构造函数校验」弥补，禁止用 `string` 裸传状态。

## 技术栈

| 层 | 选型 | 说明 |
| --- | --- | --- |
| 语言 | Go 1.23+ | 标准库优先 |
| HTTP 路由 | chi | 贴近 `net/http`，不引入重框架 |
| 数据库 | PostgreSQL 16 | 唯一数据存储 |
| 数据访问 | pgx/v5 + sqlc | SQL 手写、类型安全生成，不用 ORM |
| 迁移 | goose | 只前进，不自动回滚 |
| MCP | modelcontextprotocol/go-sdk | streamable HTTP transport，无需桥接 |
| 鉴权 | 平台 JWT + 个人访问令牌（PAT） | 人用 JWT，MCP 用 PAT，二者共用权限模型 |
| 密码哈希 | argon2id | 不存明文，不用 md5/sha |
| 配置 | 环境变量 + `.env` | 无配置文件格式自研 |
| 日志 | log/slog（JSON） | 结构化，含 request_id、actor_id |
| 前端 | React 19 + TypeScript + Vite | SPA，构建产物由 nginx 托管 |
| UI 组件 | Ant Design 5 | 表格、表单、树、审批流密集，管理台适配好 |
| 数据请求 | TanStack Query | 缓存与失效策略统一 |
| 图表 | Recharts | 燃尽图、状态分布 |
| 部署 | docker compose + nginx | nginx 终止 TLS，反代 API/MCP，直接托管前端静态产物 |
| 集成 | provider 适配器（net/http、net/smtp） | Git/SVN、Outline、Redmine、邮件、Webhook，只存引用 |
| 测试 | go test + testcontainers-go | 集成测试跑真实 PG |

## 架构决策记录

- **单体而非微服务**：M1 之前一个 `chalklined` 进程同时提供 MCP 与 REST，共享领域层与事务边界。MCP 与 REST 只是两个入口，不构成两个服务。
- **工作流引擎自研且极小**：只支持节点、有向边、门禁三种要素；不引入 Temporal / Camunda。流程定义存于数据库，引擎是纯函数式的状态判定。
- **规格存数据库而非文件**：多人并发编辑与门禁审计要求事务与版本，文件方案（git 里放 Markdown）在协作场景会退化为冲突源。
- **不用 ORM**：状态机需要精确的事务与行锁语义，sqlc 生成的 SQL 可读可控。
- **TLS 由 nginx 终止，应用不感知**：`chalklined` 只监听容器内 HTTP，证书、HSTS、限流、内网白名单都在 nginx 层配置；应用不引入自签证书逻辑，避免两处维护。
- **集成只存引用，不做双向同步**：内容归外部系统，Chalkline 只存 URL、外部 ID 与快照。双向同步会产生无法裁决的冲突源。详见 [docs/domain/integrations.md](domain/integrations.md)。
- **通知走 outbox**：状态流转写事件表，由投递器异步发送，失败重试且不阻断流程。
- **前端静态产物由 nginx 托管**：Go 进程不承担静态文件服务，SPA 路由回退在 nginx 用 `try_files` 处理。

## 常用命令

```bash
go run ./cmd/chalklined          # 本地起后端（需 PG）
go test ./...                    # 单元测试
go test -tags=integration ./...  # 集成测试（需 testcontainers）
go vet ./...                     # 静态检查
golangci-lint run                # 代码规范
sqlc generate                    # 由 SQL 生成仓储代码
goose -dir migrations up         # 应用迁移
cd web && pnpm dev               # 前端开发服务器
cd web && pnpm build             # 构建前端产物
docker compose -f deploy/docker-compose.yml up -d
```

## 硬性约束

1. **状态只能由工作流引擎修改**。任何 handler、MCP 工具、定时任务都不得直接 UPDATE 工作项状态字段，必须经过引擎的裁决函数。
2. **门禁判定必须在服务端完成**。客户端或 Agent 传来的「已通过」一律忽略，只认数据库中的审批与材料记录。
3. **MCP 层只做协议转换**。参数校验在领域层，业务规则不得写在 MCP 工具里。
4. **禁止在 HTTP 路由层直接访问数据库**，必须经仓储接口。
5. **所有写操作必须在事务内**，且携带 `actor_id` 写入审计日志。
6. **禁止引入新的 Web 框架、ORM、消息队列**，确需引入时先在此文件记录理由。
7. **时间统一 UTC 存储**，展示层按用户时区转换。
8. **错误码语义固定**：`403` 权限不足、`404` 不存在或不可见、`409` 状态冲突或门禁未通过、`422` 参数非法。
9. **外部系统只能经 `internal/integration/` 访问**，domain / api / mcp 不得直连 Git、Outline、Redmine、SMTP。
10. **外部凭据不落库**：token、密码只从环境变量或密钥文件读取，日志中禁止出现。

## 代码约定

- 错误包装统一用 `fmt.Errorf("...: %w", err)`；对外错误经 `apierr` 转换，不泄露内部堆栈。
- 所有对外 API 输入输出使用显式 DTO，不直接序列化领域实体。
- 数据库列名 `snake_case`，Go 字段 `CamelCase`，JSON 字段 `camelCase`。
- 领域层不 import `database/sql`、`net/http`、任何 MCP 包。
