# 模块：planning

> M1 范围。领域模型已于 M0 交付（`internal/domain/planning`）；本模块补齐迭代、任务、子任务拆解、指派与模块关联的持久化和接口，并与 workflow 集成。模块索引见 [README.md](README.md)。

## 边界

负责：

- **迭代 Iteration**：一条完整生命周期的业务实体（编号 `CHK-IT<n>`、计划日期、状态 `planned`/`active`/`closed`/`cancelled`），创建时实例化生命周期工作流。
- **任务 Task**：工作项（多级编号 `CHK-12` / `CHK-12.3` / `CHK-12.3.2`、`kind`、负责人、优先级、模块关联），创建时实例化任务工作流；**任务状态是工作流节点状态的投影，不单独维护**。
- **任务拆解**：在允许建任务的迭代节点下创建一级任务、批量拆子任务（深度 ≤ 3）。
- **指派与模块关联**：`assignee_id` 唯一负责人；`TaskModule` 多对多，跨模块但同产品。

不负责：工作流引擎与状态判定（workflow）、产品与成员（catalog）、材料与审批（workflow + workitem 领域）。

## 对外接口

REST（M1）：

| 端点 | 说明 | 权限 |
| --- | --- | --- |
| `GET /api/v1/products/{pid}/iterations` | 迭代列表，可按状态过滤 | 该产品成员 |
| `POST /api/v1/products/{pid}/iterations` | 建迭代（实例化工作流 + 写入 provider 绑定） | `pm` / `admin` |
| `GET /api/v1/iterations/{id}` | 迭代详情 + 当前阶段进度 + provider 绑定 | 该产品成员 |
| `GET /api/v1/iterations/{id}/providers` | 迭代 provider 绑定与冻结状态 | 该产品成员 |
| `PUT /api/v1/iterations/{id}/providers` | 整组替换绑定（结构配置；已提交成果物后 `409 PROVIDERS_LOCKED`） | 产品成员持 `pm` / owner / `admin` |
| `PUT /api/v1/iterations/{id}/providers/{providerId}/credential` | 轮换写权限凭据（加密落库、永不回显；不受冻结限制） | 产品成员持 `pm` / owner / `admin` |
| `GET /api/v1/current-iteration` | 我的当前迭代（未设置返回 `null`） | 登录用户 |
| `PUT /api/v1/current-iteration` | 设为当前迭代（须对该产品可见、非终态） | 该产品成员 |
| `DELETE /api/v1/current-iteration` | 取消当前迭代（幂等） | 登录用户 |
| `POST /api/v1/iterations/{id}/tasks` | 建一级任务（校验当前阶段允许建任务） | `pm` / `admin` / 产品成员 |
| `GET /api/v1/tasks/{id}` | 任务详情：子树、模块、工作流状态、材料、审批 | 该产品成员 |
| `POST /api/v1/tasks/{id}/children` | 批量拆子任务 | 任务负责人 / `pm` |
| `PATCH /api/v1/tasks/{id}` | 指派、改优先级、标题 | 任务负责人 / `pm` |
| `DELETE /api/v1/tasks/{id}` | 删任务（仅未终结且无子任务；实例随任务级联清理，409 `TASK_NOT_DELETABLE`） | 任务负责人 / `pm` |
| `POST /api/v1/tasks/{id}/modules` | 增删模块关联 | 任务负责人 / `pm` |

MCP：`create_iteration`（含 `providers`）/ `create_task` / `decompose_task` / `assign_task` / `link_module` / `list_iterations` / `list_tasks` / `get_task` / `get_iteration_providers` / `set_iteration_providers`（见 [mcp.md](../domain/mcp.md)，权限列与对象级权限表一致）。

## 核心流程

- **建迭代**：事务内校验调用者是产品成员 + `pm`/`admin` → 分配 `CHK-IT<n>` 编号 → 写 `iterations` → 调 workflow 实例化生命周期模板（写 `workflow_instances` + 起始节点 `ready`）→ 写迭代 provider 绑定（成果物必选，见 [integrations.md](../domain/integrations.md)「迭代 Provider」）→ 审计。
- **建任务/拆子任务**：校验当前迭代所处节点的 `allows_task_kinds` 白名单包含该类型（不变量 5，向 workflow 查实例当前节点）→ 校验子任务与父同产品同迭代（不变量 2）→ `bug` 只能作为一级任务、不可拆解 → 按父级维度分配编号（并发不重复）→ 写 `tasks` → 继承父任务模板/模块（无继承时回退产品按类型默认 `task_workflow_defaults`，再回退产品默认）→ 实例化任务工作流 → 审计。
- **任务状态**：不存独立 `status` 字段做真相；读时由 workflow 实例的终态节点投影（`done` / `cancelled`），未到终态为 `open`（model.md「状态投影」）。
- **删任务**：仅限 `open` 且无子任务的任务（已终结历史保留，呼应不变量 10「不允许静默丢弃」；有子任务须先删子任务）。权限同改任务字段；事务内先删任务流实例（`node_states` / `gate_approvals` / `artifacts` 外键级联）再删 `tasks` 行，最后审计 `task.delete`。
- **移除迭代（收口）**：迭代进入终态前，其下未终结任务必须显式转移或取消（不变量 10，由 workflow 迭代收口节点 `require_iteration_tasks_done` 拦截）。
- **当前迭代（个人偏好）**：每人至多一条「当前迭代」，登录后控制台首页直接渲染该迭代看板，省去「产品 → 迭代」逐级点击。设置时校验对该迭代所属产品可见（非成员 404）且迭代非终态；取消幂等。迭代被删除时随外键级联清理；若已失去可见性（成员被移除），读接口按「未设置」返回而不报错。

## 数据

M1 新建迁移：

- `iterations`：`id` / `product_id` / `name` / `goal` / `number`（`CHK-IT<n>`）/ `starts_on` / `ends_on` / `status` / `workflow_instance_id` / 时间戳；`(product_id, number)` 唯一。
- `tasks`：`id` / `product_id` / `iteration_id` / `number`（多级）/ `parent_id` / `title` / `description` / `kind` / `assignee_id` / `priority` / `workflow_instance_id` / 时间戳；编号不可变。
- `task_modules`：`task_id` + `module_id` 联合主键；模块必须与任务同产品（不变量 2）。
- `user_current_iterations`（0010）：`user_id` 主键（每人一条）/ `iteration_id`（外键级联删除）/ `updated_at`。
- 编号分配：用 Postgres sequence 或计数器表按「产品（顶层任务）/ 父任务（子任务）」维度单调递增，**永不复用、永不重排**（不变量 3）。
- `iteration_providers`（0013，归 integration 模块）：迭代级 provider 绑定，`iteration_id` 外键级联；详情见 [integration.md](integration.md)「M5 增量」。

> 工作流实例与节点状态表归 workflow 模块，本模块只存外键 `workflow_instance_id`。

## 不变量（本模块保证）

- 任务 `product_id` = 迭代 `product_id`；`TaskModule` 的模块与任务同产品（不变量 2）。
- 编号唯一、不可变、不复用；父子关系不可变（不变量 3）。
- 任务只创建在 `allows_task_kinds` 白名单包含该类型的迭代节点（不变量 5；`allows_task_creation: true` 是「全部类型」的语法糖）；`bug` 只能作为一级任务、不可拆解。
- 一个任务一个负责人；父任务负责人收口（不变量 4）。
- 任务深度 ≤ 3（不变量 1）；模块深度 ≤ 5 归 catalog。
- 任务进入终态前全部子任务 `done`（不变量 9，由 workflow `require_children_done` 拦截，本模块提供子任务终结状态查询）。
- 迭代收口前未终结任务须转移/取消；终态后不可回退（不变量 10 / 11）。
- 同产品可多 `active` 迭代并行（不变量 12）。

## 验收标准

- [ ] 建迭代返回 `CHK-IT<n>`，并可见起始阶段 `ready`；并发建任务编号不重复。
- [ ] 子任务编号 `CHK-12.3` 正确延伸，深度 > 3 被拒（422 `TREE_DEPTH_EXCEEDED`）。
- [ ] 非允许建任务的阶段（如调研未过）建任务返回 409 `STAGE_NOT_ALLOW_TASK`。
- [ ] 跨产品/跨迭代挂子任务、模块被拒。
- [ ] 任务状态与工作流终态一致：终态 `done`，未到终态 `open`。
- [ ] 测试全绿（含编号并发、收口拦截的集成测试）。

## 任务拆解

1. `migrations/0005_planning.sql`：建 `iterations` / `tasks` / `task_modules` + 编号序列/计数器表。
2. `internal/store`：planning 相关 sqlc 查询（全部带产品成员过滤），编号分配函数（事务内取号）。
3. `internal/domain/planning` 补充：编号分配与延伸的领域服务、子任务模板/模块继承校验。
4. `internal/app/planning`：建迭代（+ workflow 实例化）、建任务/拆子任务、指派、模块关联的用例编排（事务 + 审计 + 不变量校验）。
5. 跨模块：与 workflow 约定「创建实例」「查当前节点 allows_task_kinds 白名单」「查子任务终结状态」的接口。
6. `internal/api`：上表 REST 端点 + DTO；`STAGE_NOT_ALLOW_TASK`/`TREE_DEPTH_EXCEEDED` 等错误码映射（见 [mcp.md](../domain/mcp.md)）。
7. `internal/mcp`：`create_iteration` / `create_task` / `decompose_task` / `assign_task` / `link_module` / 列表与详情读工具。
8. 集成测试：编号并发安全、拆解边界、跨产品拒绝、收口约束。
9. 文档：验收后更新 `docs/modules/README.md` 状态。

## 开放问题

- 编号分配用 Postgres sequence 还是显式计数器表？→ 任务 1/2 评估（sequence 简单但「父任务内递增」需每行一个序列或计数器行）。
- 任务允许更换所属迭代（编号不变，model.md 允许）的 UI/MCP 入口是否在 M1 做？→ M1 先支持，作为独立用例。
