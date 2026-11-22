# 项目结构

> 本文件保证生成的代码无缝落入现有仓库。新增文件之前先查「放置规则」。

## 目录职责

```
Chalkline/
├── cmd/
│   ├── chalklined/            服务进程入口（MCP + REST）
│   └── chalklinectl/          运维 CLI（二期，迁移/种子数据）
├── internal/
│   ├── domain/                实体、值对象、不变量、领域错误（无外部依赖）
│   │   ├── product/           产品、产品成员、模块树
│   │   ├── planning/          迭代、任务、子任务、指派
│   │   ├── identity/          成员、角色、令牌
│   │   └── workitem/          工作项状态与门禁的通用抽象
│   ├── workflow/              工作流定义解析、状态判定、门禁裁决（纯函数）
│   ├── app/                   用例编排：事务边界、跨聚合操作
│   ├── store/                 仓储实现（sqlc 生成 + 手写查询）
│   │   ├── sqlc/              生成代码，禁止手改
│   │   └── query/             手写 SQL 源文件（sqlc 的输入）
│   ├── api/                   REST handler、DTO、路由注册
│   ├── mcp/                   MCP 工具定义与协议转换
│   ├── auth/                  会话、PAT、权限判定
│   ├── events/                进程内事件总线（outbox → SSE 分发，M3）
│   ├── integration/           外部系统适配器（只做协议转换与引用校验）
│   │   ├── git/ svn/ outline/ redmine/
│   │   └── notify/            SMTP 与 Webhook 投递
│   └── platform/              日志、配置、时钟、ID 生成、事务工具
├── migrations/                goose SQL 迁移，只增不改
├── web/                       React 前端
│   └── src/
│       ├── features/          按功能切分（products/iterations/tasks/gates/reviews）
│       └── shared/            通用组件、API 客户端、类型
├── docs/                      产品与技术文档（见下）
├── agent/
│   └── skills/chalkline/      Agent Skill：SKILL.md + references，随服务同版本发布
├── deploy/
│   ├── docker-compose.yml      nginx / api / web / postgres
│   ├── Dockerfile              多阶段构建，产出静态二进制
│   ├── nginx/nginx.conf        TLS 终止、反代 /api 与 /mcp、托管前端
│   └── .env.example            端口、证书路径、数据库密码
├── testdata/                  测试夹具
└── Makefile                   常用命令入口
```

## 放置规则

| 你要写的东西 | 放哪 |
| --- | --- |
| 新的业务规则（如「子任务深度不超过 3 层」） | `internal/domain/<聚合>/` 的不变量校验 |
| 新的工作流判定（如「节点 ready 条件」） | `internal/workflow/` |
| 一个跨表的事务操作（如「拆解任务并继承工作流」） | `internal/app/` |
| 新接口 | `internal/api/`，同时补 DTO 与路由 |
| 新 MCP 工具 | `internal/mcp/`，内部调用 `internal/app/`，不写业务规则 |
| 新表或改表 | 新增 `migrations/` 文件，同步 `store/query/` 与 `sqlc.yaml` |
| 新前端页面 | `web/src/features/<功能>/`，路由在 `web/src/app/routes.tsx` 注册 |
| 新的外部系统（Jira、飞书等） | `internal/integration/<provider>/`，实现 Provider 接口 |
| 新的通知渠道 | `internal/integration/notify/` |
| 新的进程内事件分发（SSE 等） | `internal/events/`，只做分发不做判定，可见性过滤在 api 层 |
| Agent Skill 或话术参考 | `agent/skills/chalkline/`，工具变更须同提交更新 |
| 新的部署单元 | `deploy/` |

规则一句话：**业务规则在 domain，流程判定在 workflow，事务编排在 app，协议转换在 api/mcp，SQL 在 store。越界即为错误。**

## 分层边界

允许的依赖方向（箭头表示 import）：

```
api ──┐
      ├──> app ──> domain
mcp ──┘        └──> workflow ──> domain
app ──> store（经接口）
app ──> integration（经 Provider 接口）
api/mcp ──> auth（只做鉴权）
platform 可被以上所有层使用，但 platform 不得反向 import 业务包
```

禁止：

- `domain` 与 `workflow` 不得 import `store`、`api`、`mcp`、`integration`、`database/sql`、`net/http`
- `api` 与 `mcp` 不得直接调用 `store`
- `mcp` 与 `api` 之间不得互相调用
- 任何包不得直接 `UPDATE` 工作项状态，必须走 `workflow` 的裁决入口

## 命名约定

| 对象 | 约定 | 示例 |
| --- | --- | --- |
| Go 包目录 | 小写单词，单数 | `workitem`、`planning` |
| Go 文件 | `snake_case.go` | `task_tree.go` |
| 数据库表 | 复数 `snake_case` | `work_items`、`gate_approvals` |
| 数据库列 | `snake_case` | `assignee_id`、`created_at` |
| 迁移文件 | `NNNN_动词_对象.sql` | `0007_add_gate_artifacts.sql` |
| REST 路径 | 复数小写，层级不超过三层 | `/api/v1/products/{id}/iterations` |
| MCP 工具 | `chalkline_<动词>_<对象>` | `chalkline_get_next_action` |
| 前端组件 | `PascalCase.tsx` | `TaskTreePanel.tsx` |
| 前端功能目录 | `kebab-case` | `gate-review/` |
| 分支 | `feat/<模块>-<简述>` | `feat/workflow-gate-sla` |
| 提交 | `type(scope): 描述` | `feat(workflow): 支持多级门禁` |

## 文档分区

`docs/` 与代码模块一一对应，任务拆解出的每一块都有落点：

```
docs/
├── product.md              产品愿景（长期）
├── tech.md                 技术架构（长期）
├── structure.md            本文件（长期）
├── domain/
│   ├── model.md            领域模型与不变量
│   ├── workflow.md         工作流定义与门禁语义
│   ├── integrations.md     外部系统集成契约与边界
│   ├── skill.md            Agent Skill 设计与话术约束
│   └── mcp.md              MCP 工具契约
└── modules/
    ├── README.md           模块索引
    └── <module>.md         单模块设计（按需创建，一块一批任务）
```

- **长期文档**（product / tech / structure）随仓库演进缓慢修订，改动需在提交信息里说明原因。
- **domain 文档**描述规则契约，代码与文档冲突时以文档为准并修代码，或反之必须同一次提交内对齐。
- **modules 文档**是任务拆解的产物：一个模块一份，写清边界、接口、状态与验收标准，任务挂在该模块上。

## 开发顺序约定

每个功能块的实现顺序固定为：**domain 不变量 → workflow 判定 → migrations → store 查询 → app 用例 → api/mcp 入口 → 前端**。禁止从 handler 倒推。
