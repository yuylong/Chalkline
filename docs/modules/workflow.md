# 模块：workflow

> M1 范围。**引擎核心判定已于 M0 交付**（`internal/workflow`）；M1 补齐模板管理、实例持久化与应用层用例，把纯函数引擎接入数据库。模块索引见 [README.md](README.md)。

## 边界

负责：

- **工作流定义**：模板的解析、校验、版本管理（YAML→JSON 存储）、内置模板种子。
- **引擎纯函数**：节点状态机、门禁会签裁决、并行汇合、驳回重提、乐观锁、`next_action` 投影——**M0 已交付**，M1 不再改判定逻辑。
- **实例持久化**：把内存中的 `Instance` 落到数据库，操作时「加载 → 调引擎 → 写回」，用乐观锁防并发。
- **应用用例**：创建实例、`start_step` / `submit_step` / `review_step` / `attach_artifact` 的事务编排（含材料替换重置审批等 app 层语义）。

不负责：迭代/任务的业务字段（planning）、用户与权限（identity / catalog）、材料的外部校验（integration）。

## M0 已交付（不再重复开发）

| 能力 | 文件 | 状态 |
| --- | --- | --- |
| 定义解析与 Normalize | `internal/workflow/definition.go` | ✅ 已测 |
| DAG 校验（环/可达性/终态/边引用/多终态 outcome） | `validate.go` | ✅ 已测 |
| 状态机与门禁裁决（Start/Submit/Review/Evaluate） | `engine.go` | ✅ 已测 |
| 并行 fork/AND 汇合、单支驳回不连坐 | `engine.go` | ✅ 已测 |
| 驳回重提保留累计审批、同评审人最近决策为准 | `engine.go` | ✅ 已测 |
| 乐观锁版本控制、`STATE_CONFLICT` | `engine.go` | ✅ 已测 |
| `next_action` 投影（blockers 逐条） | `next_action.go` | ✅ 已测 |
| 5 个内置模板 + 种子 | `builtins.go` + `builtins/*.yaml`、`store.SeedBuiltinWorkflows` | ✅ |
| 错误定义 | `errors.go` | ✅ |
| `workflows` 表迁移 | `migrations/0002_workflows.sql` | ✅ |

## 对外接口

Go（被 app 调用，已存在或 M1 新增）：

- 引擎：`NewInstance` / `Start` / `Submit` / `Review` / `Evaluate` / `NextAction` / `State` / `Progress` / `Terminal`（M0 已有）。
- 模板：`LoadBuiltins` / `ParseYAML`（M0 已有）；M1 新增仓储 `ListWorkflows` / `GetWorkflow` / `CreateWorkflow` / `UpdateWorkflow`（升版本）/ `ArchiveWorkflow`。
- 实例：M1 新增仓储 `LoadInstance` / `SaveInstance`（含节点状态、审批、材料）。

REST（M1，部分与 planning 共用路径）：

| 端点 | 说明 |
| --- | --- |
| `GET /api/v1/workflows` | 模板列表（按 scope 过滤，含内置） |
| `POST /api/v1/workflows` | 新建模板（YAML→校验→存 JSON） |
| `PATCH /api/v1/workflows/{id}` | 改模板（升版本，运行中实例不受影响） |
| `POST /api/v1/workflows/{id}/archive` | 软删 |
| `GET /api/v1/tasks/{id}/workflow` | 本实例各节点状态 + 模板定义 |
| `GET /api/v1/reviews/pending` | 待我审批的门禁（评审人视角） |

MCP：`get_next_action` / `start_step` / `submit_step` / `attach_artifact` / `review_step` / `get_workflow` / `list_workflows` / `get_pending_reviews`（契约见 [mcp.md](../domain/mcp.md)）。

## 核心流程

**一次节点操作（以 `submit_step` 为例）**：

1. app 层开事务，`actor_id` 来自鉴权中间件。
2. 仓储 `LoadInstance`（行锁）→ 组装内存 `Instance`。
3. 权限校验（任务负责人 / 评审角色，调 catalog/identity）。
4. 调 `instance.Submit(nodeID, expectedVersion, input, now)`：引擎校验材料、门禁、版本。
5. 成功则 `SaveInstance`（节点状态 + 新增审批/材料记录）+ 写审计 + 写 outbox 事件。
6. 引擎错误映射为错误码：`ErrNotReady→NOT_READY`、`ErrGateBlocked→GATE_BLOCKED`、`ErrInvalidTransition`、`ErrStateConflict→STATE_CONFLICT`、`ErrArtifactMissing→ARTIFACT_MISSING`（见 [mcp.md](../domain/mcp.md) 错误码表）。

**材料替换重置审批（M1 app 层）**：`awaiting_review` 期间替换已进入评审的材料 → 作废该节点既有审批（保留历史），基于当前版本重新收集（[workflow.md](../domain/workflow.md)「材料变更」）。引擎提供 `Evaluate` 语义，重置动作在 app 事务内完成。

## 数据

- `workflows`（M0 已建）：模板定义 JSON、`scope`、`version`、`status`（draft/active/archived）、`(scope,name,version)` 唯一。
- M1 新建迁移：
  - `workflow_instances`：`id` / `workflow_id` / `workflow_version`（实例化时固定）/ `work_item_type` / `work_item_id` / `terminal_node_id` / `created_at`。
  - `node_states`：`instance_id` + `node_id` 联合主键 / `status` / `version`（乐观锁）/ `entered_at` / `completed_at`。
  - `gate_approvals`：`instance_id` / `node_id` / `reviewer_id` / `decision` / `comment` / `created_at`；同评审人 upsert 最近决策。
  - `artifacts`：`instance_id` / `node_id` / `kind` / `provider` / `url` / `external_id` / `external_path` / `snapshot` / `content`（inline）/ `submitted_by` / `created_at`。
  - `artifact_versions`（`0020_artifact_versions.sql`）：被替换材料的归档——`artifact_id` / 同 `artifacts` 的全部字段 + `replaced_at` / `replaced_by`；只增不删。

## 不变量（本模块保证）

- 状态只能经引擎修改，禁止直接 UPDATE 节点状态（tech.md 约束 1 / AGENT 红线 1）。
- 门禁判定只在服务端，只认库内审批与材料（AGENT 红线 2）。
- 乐观锁冲突返回 `STATE_CONFLICT`（409），客户端刷新重试（不变量：并发安全）。
- 模板修改升版本，运行中实例固定原版本（[workflow.md](../domain/workflow.md)「版本与迁移」）。
- 材料替换重置审批；驳回重提保留累计审批（不变量语义，引擎已实现）。
- 所有写操作事务内带 `actor_id` 写审计（不变量 13）。

## 验收标准

- [ ] 模板 CRUD：新建校验通过、改模板升版本不影响运行中实例、archive 软删。
- [ ] 端到端：创建实例 → start → submit（缺材料 422 / 门禁不足 409）→ review → 级联到下一节点，全程状态正确。
- [ ] 并发：两请求同版本提交，一个成功一个 `STATE_CONFLICT`。
- [ ] 驳回重提：approve 保留累计，pending reject 持续阻塞。
- [ ] 材料替换：评审中材料被替换后既有审批作废。
- [ ] 引擎纯函数测试与 M0 一致（不回退），新增持久化与集成测试全绿。

## 任务拆解

1. `migrations/0006_workflow_runtime.sql`：建 `workflow_instances` / `node_states` / `gate_approvals` / `artifacts`。
2. `migrations/0020_artifact_versions.sql`：建 `artifact_versions`（被替换材料的历史归档，支撑节点详情页「历史轮次」）。
2. `internal/store`：workflow 仓储——模板 CRUD + 实例加载/保存（节点状态、审批、材料），全部 sqlc 生成。
3. `internal/app/workflow`：实例持久化编排——`CreateInstance` / `StartStep` / `SubmitStep` / `ReviewStep` / `AttachArtifact`（事务、乐观锁、审计、outbox、错误码映射）。
4. 材料替换重置审批的 app 逻辑（任务 3 的一部分，独立验收）。
5. `internal/api`：模板管理 REST + 实例状态查询 + pending reviews 端点。
6. `internal/mcp`：上表 8 个 MCP 工具（协议转换，调 app，不写规则）。
7. 与 planning 集成：迭代/任务创建时调用 `CreateInstance`；查询当前节点 `allows_task_kinds` 白名单。
8. 集成测试：端到端状态机、并发乐观锁、驳回重提、材料替换（testcontainers 真实 PG）。
9. 回归：确认 M0 引擎单测全绿，行为未变。
10. 文档：验收后更新 `docs/modules/README.md` 状态。

## 开放问题

- 实例加载策略：每次操作全量加载节点/审批/材料，还是按需？→ M1 先全量（数据量小），性能问题在 M2 优化。
- `get_next_action` 的合格评审人列表（`eligibleReviewers`）如何计算？→ 调 catalog 取产品内持该角色的成员，M1 实现。

## M3 增量：模板导入导出与评审 SLA

> 已交付。

**模板导入导出**（跨实例迁移与备份）：

| 端点 | 说明 | 权限 |
| --- | --- | --- |
| `GET /api/v1/workflows/{id}/export` | 模板导出为 YAML（与创建入参同构，可直接再导入） | 登录 |
| `POST /api/v1/workflows/import` | 导入 YAML；id 冲突自动换新 id，(scope,name,version) 撞名自动升版本 | `pm` / `admin` |

- 导入冲突策略在服务端裁决并写审计（`workflow.import`，payload 记 `original_id`）；导出是读操作不写审计。
- YAML 往返有单测守卫（`internal/workflow/yaml_roundtrip_test.go`）：全部内置模板导出后必须能原样再解析。

**评审超期提醒与升级链**：

- 配置：`CHALKLINE_REVIEW_SLA`（默认 24h）→ `gate.overdue` 提醒评审人；`CHALKLINE_REVIEW_ESCALATION`（默认 3×SLA）→ `gate.escalated` 升级给产品 owner / admin；`CHALKLINE_REVIEW_SCAN_INTERVAL`（默认 10m）扫描间隔。
- 监控器 `internal/app/sla.go`：只写 `gate_reminders` 水位（`(instance, node, level)` 唯一，防重复）与 outbox 事件 + 审计（actor=`system`），**不改任何节点状态**（红线 1、不变量 21）；跳级时按 1→2 保序补发。
- `GET /reviews/pending` 与 MCP `chalkline_get_pending_reviews` 条目新增 `awaitingSince` 与 `overdue`，控制台队列标红。
- 事件经既有 outbox 通道投递（SMTP / Webhook / 订阅），投递失败不阻断流程（不变量 15）。

**材料快照固化**：`submit_step` / `attach_artifact` 在挂接时把快照写入 `artifacts.snapshot`——外部 provider 记 title/version/fetchedAt，inline 记内容 sha256 与长度；实例视图（`InstanceView.artifacts[].snapshot`）透传给控制台与 Agent。评审看到的是提交时点的事实（integrations.md「同步方向」）。

**成果物提交时间**：`InstanceView.artifacts[].createdAt`（RFC3339）随实例视图返回，控制台据此把节点成果物按时间倒序，判定「最新成果物」并组织历史列表。`artifacts` 表对同 `(instance, node, kind)` 只保留一份：`ReplaceArtifact` 先把旧行整条归档到 `artifact_versions`，再删后插——因此**同类型材料的历史版本可回看**（`InstanceView.artifactHistory`，按 `replaced_at` 倒序，含原提交时间与替换人）。首次挂接没有旧行，不产生历史记录。
