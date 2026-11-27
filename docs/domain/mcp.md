# MCP 接口契约

Chalkline 通过 MCP over HTTP 暴露工具，编码 Agent 用它读流程、推进工作项。**工具只做协议转换，规则在领域层。**

端点：`POST /mcp`（streamable HTTP），鉴权：`Authorization: Bearer <PAT>`。

## 核心设计：下一步是查出来的，不是猜出来的

Agent 每轮先调 `chalkline_get_next_action`，服务端返回结构化结论。三种情况：

| `status` | 含义 | Agent 该做什么 |
| --- | --- | --- |
| `ready` | 下一步可开工 | 直接执行，完成后调 `submit_step` |
| `in_progress` | 当前节点已开始未提交 | 继续做完再提交 |
| `awaiting_review` | 已提交，等评审 | 停止推进，向人报告等谁批 |
| `blocked` | 前置条件不满足 | 按 `blockers[]` 逐条处理，不得跳过 |
| `done` | 已到终态 | 结束 |

这条契约替代了「在聊天框里推着 AI 走」：Agent 不需要被反复告知做到哪了，状态在平台里。

## 工具清单

### 读

| 工具 | 说明 |
| --- | --- |
| `chalkline_whoami` | 当前身份、角色、可见产品 |
| `chalkline_list_products` | 产品列表（含迭代数、任务统计），支持 `q` 搜索 / `status` 过滤 / `limit`+`offset` 分页（M3） |
| `chalkline_get_product` | 产品详情：模块树、产品文档、默认工作流 |
| `chalkline_list_iterations` | 迭代列表，可按产品与状态过滤 |
| `chalkline_list_tasks` | 任务列表，可按迭代、模块、负责人、状态过滤 |
| `chalkline_get_task` | 任务详情：子树、关联模块、工作流节点状态、材料、审批 |
| `chalkline_get_next_action` | **当前工作项下一步与阻塞原因** |
| `chalkline_get_workflow` | 模板定义 + 本实例各节点状态 |
| `chalkline_list_workflows` | 可用模板列表（按 scope 过滤，含内置模板） |
| `chalkline_get_pending_reviews` | 我是评审人、待我确认的门禁；条目含 `awaitingSince` 与 `overdue`（超过评审 SLA，M3） |
| `chalkline_list_product_members` | 产品成员与各自的产品角色 |
| `chalkline_get_iteration_providers` | 迭代 provider 绑定（成果物 / 流程记录 / 代码库）与冻结状态 |

### 写

| 工具 | 说明 | 权限 |
| --- | --- | --- |
| `chalkline_create_iteration` | 建迭代（一条新生命周期），指定模板与 `providers`（成果物必选且唯一，流程记录可选，代码库可选可多个） | pm |
| `chalkline_set_iteration_providers` | 整组替换迭代 provider 绑定（结构配置；已提交成果物后 `409 PROVIDERS_LOCKED`） | pm / 产品 owner |
| `chalkline_set_iteration_provider_credential` | 轮换 provider 写权限凭据（token / 用户名密码；加密落库、永不回显；**不受冻结限制**） | pm / 产品 owner |
| `chalkline_create_task` | 建任务：`kind`（`bug` 只能作为一级任务）、父任务、多个模块，返回分配的多级编号 | pm / admin |
| `chalkline_decompose_task` | 拆解为子任务（批量），返回 `CHK-12.3` 形式的编号 | 任务负责人 / pm |
| `chalkline_assign_task` | 指派负责人 | pm / 任务负责人 |
| `chalkline_link_module` | 增删任务与模块的关联 | pm / 任务负责人 |
| `chalkline_update_module` | 更新模块基本信息：功能简介、技术栈、责任人、开发者、测试者（整组替换语义，不改名/不移动） | pm 产品成员 / 产品 owner / admin |
| `chalkline_start_step` | 开始当前可执行节点 | 任务负责人 |
| `chalkline_submit_step` | 提交节点，携带材料与说明 | 任务负责人 |
| `chalkline_attach_artifact` | 挂接材料引用：`provider` + `url`/`externalId`，或 `inline` 内容 | 任务负责人 |
| `chalkline_review_step` | 门禁确认或驳回 | 门禁 `roles` 声明角色的成员（`admin` 恒可）；提交人本人为合格评审人时可自审，同样走 approve 流程 |
| `chalkline_upsert_product_member` | 加入成员或调整产品角色（`owner` / `member` / `guest`） | 产品 `owner` / `admin` |
| `chalkline_remove_product_member` | 移除成员，要求其未终结任务已转派 | 产品 `owner` / `admin` |

权限列与 [model.md](model.md) 的对象级权限表一致，在服务端强制执行：越权写返回 `403`，访问未加入的产品返回 `404`；`get_next_action` 对无权执行的成员不返回该工具。

**材料来源受迭代绑定约束**（不变量 22）：材料提交时 `provider=inline` 表示内容交给 Chalkline，由绑定的成果物 provider 决定落点（外部 provider 只回填摘要与链接）；`pr` 必须来自绑定的代码库；附件（`file` / `url`）不受限。来源与绑定冲突时 `submit_step` / `attach_artifact` 返回 `422 INVALID_ARTIFACT_REF`。

## 返回契约

### `chalkline_get_next_action`

```json
{
  "workItem": { "type": "task", "id": "tsk_7f3a", "title": "工作流引擎状态判定" },
  "currentNodes": [
    { "id": "spec", "name": "规格编写", "status": "in_progress" }
  ],
  "status": "blocked",
  "nextActions": [
    {
      "nodeId": "spec",
      "tool": "chalkline_submit_step",
      "blockers": [
        { "type": "artifact_missing", "kind": "spec", "message": "需上传规格文档" },
        { "type": "approval_missing", "required": 1, "current": 0, "roles": ["reviewer"] }
      ]
    }
  ],
  "allowedActions": ["chalkline_attach_artifact"]
}
```

`status` 为 `ready` 时各 `nextActions[].blockers` 为空数组，Agent 可直接调用对应的 `tool`。**并行分支时 `currentNodes` 与 `nextActions` 返回多个条目**：先向用户逐条报告分支状态，由人选择推进哪条，不替人排序；串行模板时数组长度恒为 1。

### 门禁未通过

```json
{
  "error": {
    "code": "GATE_BLOCKED",
    "message": "节点 review 的门禁未满足",
    "details": {
      "missingApprovals": 2,
      "missingArtifacts": ["test_report"],
      "eligibleReviewers": ["u_12", "u_31"]
    }
  }
}
```

## 错误码

| code | HTTP | 触发 |
| --- | --- | --- |
| `NOT_READY` | 409 | 前驱节点未完成时调 `start_step` |
| `STAGE_NOT_ALLOW_TASK` | 409 | 当前迭代阶段不允许创建任务 |
| `GATE_BLOCKED` | 409 | 门禁未满足时调 `submit_step` |
| `INVALID_TRANSITION` | 409 | 节点状态不允许该操作 |
| `STATE_CONFLICT` | 409 | 节点状态已被并发修改（乐观锁冲突），刷新状态后重试 |
| `ARTIFACT_MISSING` | 422 | 提交时缺必需材料 |
| `INVALID_ARTIFACT_REF` | 422 | 材料引用格式非法，或 provider 与 kind 不匹配 |
| `INTEGRATION_UNAVAILABLE` | 503 | 模板要求外部校验，但对应适配器未连通 |
| `TREE_DEPTH_EXCEEDED` | 422 | 模块 > 5 层或任务 > 3 层 |
| `ROLE_REQUIRED` | 403 | 角色不满足 |
| `NOT_FOUND` | 404 | 不存在或不可见 |

错误响应统一为 `{ "error": { "code", "message", "details" } }`，`message` 可直接展示给用户。

## 交互示例

```
Agent: chalkline_get_next_action(taskId=tsk_7f3a)
   → status=ready, tool=chalkline_start_step
Agent: chalkline_start_step(taskId=tsk_7f3a)
   → 节点进入 in_progress
Agent: （编写规格，落库到 Document）
Agent: chalkline_attach_artifact(taskId=tsk_7f3a, artifact={kind:"pre_review", provider:"inline", content:"对照门禁逐项自查：…（固定结构见 Skill）"})
Agent: chalkline_submit_step(taskId=tsk_7f3a, artifacts=[{kind:"spec", provider:"outline", url:"https://outline.example.com/doc/spec-91"}])
   → status=awaiting_review, 需 1 名 reviewer（评审人同时可见 pre_review 自查报告）
Agent: 向人报告「规格已提交，等待 <姓名> 审批」
--- 人在控制台确认 ---
Agent: chalkline_get_next_action(taskId=tsk_7f3a)
   → status=ready, node=implement
```

## 约定

- 所有 ID 带类型前缀：`prd_` / `mod_` / `itr_` / `tsk_` / `wf_` / `doc_` / `usr_`。
- 任务编号（`CHK-12.3`）由服务端分配并返回，客户端不可指定、不可修改。
- 列表接口一律分页，默认 50，上限 200。
- 写接口幂等：`submit_step` 重复提交同一节点返回当前状态而非报错。
- `awaiting_review` 期间可补挂材料；替换已进入评审的材料会重置该节点的审批收集，语义见 [workflow.md](workflow.md)「材料变更」。
- 时间字段 ISO 8601 UTC。
- 工具名与参数一旦发布不得重命名，破坏性变更通过新工具名提供。
