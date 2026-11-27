# 模块：mcp-gateway

> M1 范围。把 [mcp.md](../domain/mcp.md) 定义的工具契约落到 `internal/mcp`，是编码 Agent 接入平台的唯一入口。模块索引见 [README.md](README.md)。

## 边界

负责：

- **MCP 服务端**：`POST /mcp`（streamable HTTP transport），用 `modelcontextprotocol/go-sdk` 暴露工具。
- **工具注册与协议转换**：把 26 个工具（12 读 + 14 写）的入参/出参转换为 app 层调用，**不写任何业务规则**（AGENT 红线 3）。
- **鉴权接入**：MCP 路径走 PAT（与 identity 协作），注入 `actor_id`。
- **错误包络**：把 app 层错误翻译为 mcp.md 定义的错误码与 HTTP 状态（401/403/404/409/422/503），`message` 可直接展示。
- **契约一致性**：工具名、参数、错误码与 `docs/domain/mcp.md` 严格对齐；CI 校验 Skill 中引用的工具名都存在。

不负责：业务规则（domain/workflow）、用例编排（app 各模块）、权限判定细节（app 层 + identity/catalog）、人类控制台（console）。

## 对外接口

- **传输**：`POST /mcp`，`Authorization: Bearer <PAT>`，streamable HTTP。
- **工具清单**（见 [mcp.md](../domain/mcp.md)，发布后工具名不可改名）：

  读：`whoami` / `list_products` / `get_product` / `list_iterations` / `list_tasks` / `get_task` / `get_next_action` / `get_workflow` / `list_workflows` / `get_pending_reviews` / `list_product_members` / `get_iteration_providers`（+ `list` 系分页工具）。

  写：`create_iteration` / `create_task` / `decompose_task` / `assign_task` / `link_module` / `update_module` / `start_step` / `submit_step` / `attach_artifact` / `review_step` / `set_iteration_providers` / `set_iteration_provider_credential` / `upsert_product_member` / `remove_product_member`。

- **返回契约**：`get_next_action` 的 `workItem` / `currentNodes` / `status` / `nextActions[].blockers` / `allowedActions` 结构与 mcp.md 示例一致；并行分支返回多条目。
- **M3 参数扩展**（只加可选参数，不改名，不破坏既有调用）：`list_products` 支持 `q` / `status` / `limit` / `offset`；`get_pending_reviews` 条目新增 `awaitingSince` / `overdue`。
- **错误**：统一 `{ "error": { "code", "message", "details" } }`，`code` ∈ mcp.md 错误码表。

## 核心流程

```
Agent 请求 POST /mcp
  → PAT 鉴权中间件（identity）注入 actor_id
  → go-sdk 路由到对应工具 handler
  → handler 做参数校验 + 调 internal/app/<模块> 用例
  → app 返回领域错误 → handler 映射为 mcp 错误码
  → 序列化返回
```

关键约束：

- handler 只做 DTO ↔ 领域入参转换、错误映射；**权限与不变量校验在 app/domain 层**，handler 不得自行判定通过与否。
- `get_next_action` 对无权执行的成员不返回该工具（mcp.md 约定）——由 app 层在组装 `allowedActions` 时过滤。
- 写工具幂等：`submit_step` 重复提交同一节点返回当前状态而非报错（mcp.md 约定），由 app 层把 `ErrInvalidTransition` 翻译为幂等响应。

## 数据

本模块不建表；复用各业务模块的表。仅在配置层记录：

- MCP 端点的超时、限流（可在 nginx 或 app 中间件，M1 先不做限流）。
- 工具注册表（代码内，不入库）。

## 不变量（本模块保证）

- 工具名与参数一旦发布不得重命名；破坏性变更通过新工具名提供（mcp.md 约定）。
- 所有写操作经 app 层事务，携带 `actor_id`，写审计（不变量 13，不在本层绕过）。
- 错误码语义固定：403 权限 / 404 不可见 / 409 状态或门禁冲突 / 422 参数非法 / 503 集成不可用（[tech.md](../tech.md) 约束 8）。
- 非成员访问返回 404 不暴露存在性（不变量 16，由 app 层保证，本层透传）。

## 验收标准

- [ ] 26 个工具全部注册，`tools/list` 返回与 mcp.md 一致。
- [ ] 每个工具有协议层测试：入参校验、正常出参结构、错误码映射。
- [ ] PAT 鉴权：有效 PAT 通过，无效/过期/吊销返回 401。
- [ ] `get_next_action` 并行分支返回多节点；`blocked` 返回 blockers 明细。
- [ ] 越权写返回 403，访问未加入产品返回 404。
- [ ] CI 校验：`agent/skills/chalkline/` 中出现的工具名均在 mcp.md 工具清单中。
- [ ] 测试全绿（工具 handler 单测 + 端到端集成测试）。

## 任务拆解

1. 引入 `modelcontextprotocol/go-sdk`，在 `internal/mcp` 搭 streamable HTTP server，挂载到 `POST /mcp`。
2. PAT 鉴权接入：复用 `internal/auth` 中间件，MCP 路径强制 PAT。
3. 工具注册表骨架 + 错误映射器（app 错误 → mcp 错误码 + HTTP 状态）。
4. 逐工具实现（协议转换 + 调 app），建议按依赖顺序：
   - 读工具：`whoami` / `list_products` / `get_product` / `list_iterations` / `list_tasks` / `get_task` / `get_workflow` / `list_workflows` / `get_next_action` / `get_pending_reviews` / `list_product_members` / `get_iteration_providers`。
   - 写工具：`create_iteration` / `create_task` / `decompose_task` / `assign_task` / `link_module` / `update_module` / `start_step` / `submit_step` / `attach_artifact` / `review_step` / `set_iteration_providers` / `set_iteration_provider_credential` / `upsert_product_member` / `remove_product_member`。
5. 与 app 各模块约定用例接口（每个工具对应一个 app 函数）。
6. 测试：handler 层参数与错误映射测试；端到端集成测试（真实 PG + 起 MCP server 发请求）。
7. CI：加工具名一致性校验脚本（mcp.md ↔ agent skill ↔ 注册表）。
8. 文档：同步更新 `docs/domain/mcp.md` 若有实现中发现的契约微调（必须同提交），更新模块索引状态。

## 开放问题

- 是否需要 MCP 的 `notifications` 推送（如门禁待办主动通知 Agent）？→ M1 先不做，Agent 轮询 `get_next_action` / `get_pending_reviews`；M2 评估。
- 工具限流与配额（防 Agent 循环调用）？→ M1 不做，靠 Skill 软引导 + nginx 基础限流。
