# 模块：catalog

> M1 范围。领域模型已于 M0 交付（`internal/domain/product`）；本模块补齐产品、产品成员、模块树与产品文档的持久化与接口。模块索引见 [README.md](README.md)。

## 边界

负责：

- **产品 Product**：顶层容器的 CRUD、归档、默认工作流设置。
- **产品成员 ProductMember**：成员与产品的关系（`owner` / `member` / `guest`），是产品级可见性的**唯一依据**。
- **模块树 Module**：产品下的多级树（物化路径 `path`，深度 ≤ 5），任务归属的「块」。
- **产品文档 Document**：轻量内联 Markdown（`owner_type` 为 product / iteration / task），仅门禁最小说明，非知识库。

不负责：用户全局身份（identity）、迭代/任务本身（planning）、工作流模板（workflow）、外部文档（Outline，integration）。

## 对外接口

REST（M1）：

| 端点 | 说明 | 权限 |
| --- | --- | --- |
| `GET /api/v1/products` | 产品列表（仅可见产品） | 登录 |
| `POST /api/v1/products` | 建产品，创建人自动 `owner` | `pm` / `admin` |
| `GET /api/v1/products/{id}` | 产品详情：模块树、默认工作流 | 该产品成员 / `admin` |
| `PATCH /api/v1/products/{id}` | 改名、设默认模板、归档 | 产品 `owner` / `admin` |
| `GET /api/v1/products/{id}/members` | 成员与产品角色 | 该产品成员 / `admin` |
| `POST /api/v1/products/{id}/members` | 加入成员 / 调角色 | 产品 `owner` / `admin` |
| `DELETE /api/v1/products/{id}/members/{uid}` | 移除成员（未终结任务须先转派） | 产品 `owner` / `admin` |
| `POST /api/v1/products/{id}/modules` | 建模块 | 产品 `owner` / `pm` / `admin` |
| `PATCH /api/v1/products/{id}/modules/{mid}` | 改名 / 移动子树（重算 `path`） | 同上 |
| `GET /api/v1/products/{id}/modules` | 模块树 | 该产品成员 |

MCP：`chalkline_list_products` / `get_product` / `list_product_members` / `upsert_product_member` / `remove_product_member`，权限与 REST 一致（见 [mcp.md](../domain/mcp.md)）。

Go（被其他模块调用）：`catalog.IsMember(productID, userID)` / `MemberRoleOf(...)` / `EnsureProductExists`——供 planning/workflow 的 app 层做产品隔离与不变量校验。

## 核心流程

- **建产品**：事务内写 `products` + 写一条 `owner` 成员记录（创建人）+ 审计；默认迭代/任务模板取内置模板 id。
- **产品隔离**：所有列表/详情 SQL 必须先过成员关系（`JOIN product_members` 或 `admin` 放行），禁止先查全量再内存过滤（[tech.md](../tech.md) 硬性约束 11）。非成员访问返回 `404`。
- **模块树操作**：移动模块时整棵子树重算 `path`；同级名唯一性在事务内校验（不变量 1）；深度 > 5 返回 `TREE_DEPTH_EXCEEDED`（422）。
- **移除成员**：先查该用户在本产品下是否有未终结任务，有则拒绝并要求转派（不变量 17，与 planning 协作）；保证产品至少保留一个 `owner`。

## 数据

M1 新建迁移（表名遵循复数 `snake_case`）：

- `products`：`id` / `key`（2–8 大写，唯一）/ `name` / `description` / `status` / `default_iteration_workflow_id` / `default_task_workflow_id` / `created_by` / 时间戳。`key` 不可改。
- `product_members`：`product_id` + `user_id` 联合主键 / `member_role` / `created_at` / `created_by`。
- `modules`：`id` / `product_id` / `parent_id` / `name` / `path`（物化，如 `/catalog/task-tree`）/ `sort_order` / 时间戳；`(product_id, parent_id, name)` 唯一。
- `documents`：`id` / `owner_type` / `owner_id` / `content`（Markdown）/ `version` / 时间戳。

> 字段定义来自 [model.md](../domain/model.md)；表结构在迁移时定稿，此处为建议。

## 不变量（本模块保证）

- 模块深度 ≤ 5；同级模块名唯一（不变量 1）。
- 每个产品至少保留一个 `owner`；移除成员前其未终结任务必须转派（不变量 17）。
- 非成员访问产品资源返回 `404`（不暴露存在性），`admin` 不受此限（不变量 16）。
- `guest` 只读不可写、不可审批（不变量 16）。
- 产品内查询必须带 `product_id` 过滤（tech.md 约束 11）。

## 验收标准

- [ ] 建产品后创建人成为 `owner`，可在列表中看到；未加入用户列表不可见、详情 404。
- [ ] 模块树支持移动子树并重算 `path`；超 5 层 / 同名同级被拒。
- [ ] 移除有未终结任务的成员被拒，转派后可移除。
- [ ] 产品归档后历史只读，成员记录保留。
- [ ] 所有产品内查询 SQL 均带成员过滤（集成测试用非成员账号验证 404）。
- [ ] 测试全绿（含 testcontainers 集成测试）。

## 任务拆解

1. `migrations/0004_catalog.sql`：建 `products` / `product_members` / `modules` / `documents` 表与索引。
2. `internal/store`：catalog 相关 sqlc 查询（全部带 `product_id` / 成员过滤）。
3. `internal/domain/product` 补充：模块树 path 重算、成员移除前的转派校验 hook（与 planning 接口约定）。
4. `internal/app/catalog`：产品 CRUD、成员管理（含不变量 17）、模块树 CRUD、Document 版本化的用例编排。
5. `internal/api`：上表 REST 端点 + DTO + 路由；非成员 404、越权 403。
6. `internal/mcp`：`list_products` / `get_product` / `list_product_members` / `upsert_product_member` / `remove_product_member` 工具（协议转换，调 app）。
7. 跨模块：为 planning 提供 `IsMember` / `EnsureProductExists`；为 workflow 提供产品默认模板读取。
8. 集成测试：产品隔离矩阵、模块树操作、成员管理边界用例。
9. 文档：验收后更新 `docs/modules/README.md` 状态。

## 开放问题

- 模块移动是否限制「不能移到自己的子孙下」？→ 任务 3 实现时加环检测。
- Document 的并发编辑策略（乐观锁 vs 最后写入胜）？→ 先乐观锁（`version`），与 M0 审计表配合。

## M3 增量：多产品规模化管理

> 已交付。产品数量上来后，列表、归档与跨产品视图仍可用。

- **列表检索**：`GET /api/v1/products` 支持 `q`（名称/key 模糊）、`status`（active/archived）、`limit`/`offset` 分页（默认 50、上限 200，mcp.md 约定），返回 `{items, total}`；MCP `chalkline_list_products` 同步支持可选参数。
- **归档守卫**：`PATCH /products/{id}` 归档时校验产品下无 `planned`/`active` 迭代，否则 `409 INVALID_TRANSITION`（不变量 20）。
- **跨产品概览**：`GET /api/v1/overview` 按可见产品集合统计：产品数、进行中迭代、未完结任务、待审批门禁、超期待审（评审 SLA，见 workflow.md M3）。
- 代码：`internal/app/scale.go`、`internal/store/catalog.go`（分页过滤）、`internal/store/planning.go`（`CountOpenIterations` / `CountOverview`）。
