# 领域模型

## 总览

```
User ──┬── owns ──> Task.assignee
       └── reviews ─> GateApproval
       └── ProductMember ──> Product（owner / member / guest，产品可见性依据）

Product ─┬── Iteration（一个交付周期 = 一条完整生命周期）
         │     └── Task（多级树，带类型与编号，跑自己的任务流）
         │            └── TaskModule ──> Module（M:N，可跨模块）
         ├── Module（多级树）
         └── Document（产品文档，也可挂在迭代/任务上）

Workflow（模板）──实例化──> WorkflowInstance ──> NodeState
                                                  └── GateApproval
                                                  └── Artifact

AuditLog（全量留痕）

外部系统：Git/SVN（代码）、Outline（文档）、Redmine（进度）、SMTP/Webhook（通知）
          ▲ 只存引用与事件，不复制内容
```

## 两层工作流

| 层级 | 管什么 | 默认节点 |
| --- | --- | --- |
| **iteration** | 一个交付周期的完整生命周期 | 竞品调研 → 关键技术调研 → 产品规划 → 研发 → 测评 → 发布 → 总结 |
| **task** | 一个具体工作的推进 | 澄清 → 规格 → 实现 → 测试设计 → 测试执行 → 完成 |

产品本身不跑工作流：**产品是迭代的序列，迭代才是一条生命周期**。一个产品可以有多个迭代，也可以多个迭代并行推进（如两条产品线），彼此不阻塞。

## 实体

### User（开发者）

平台成员。单部署 = 一个团队，用户角色为全局。

| 字段 | 说明 |
| --- | --- |
| `id` | UUID |
| `username` | 唯一登录名 |
| `display_name` | 展示名 |
| `email` | 唯一 |
| `password_hash` | argon2id |
| `roles` | `admin` / `pm` / `reviewer` / `dev` / `qa`，可多选 |
| `status` | `active` / `disabled` |

**角色权限**

| 角色 | 权限 |
| --- | --- |
| `admin` | 成员管理、产品管理、所有评审权 |
| `pm` | 建产品/模块/迭代/任务、指派、发起评审 |
| `reviewer` | 在门禁处确认或驳回（不可自审自己的提交） |
| `dev` | 推进被指派的任务、上传材料、发起评审 |
| `qa` | 编写测试设计、执行测试、产出测试报告，可担任测试门禁评审人 |

**对象级权限**

单部署 = 一个团队，但**产品之间相互隔离**：成员只能看到自己加入的产品。可见性由 `ProductMember` 决定，可写性由「产品内角色 + 全局角色 + 负责人」共同判定。`admin` 是唯一例外，可见并可管理所有产品。

| 操作 | 谁可以 |
| --- | --- |
| 查看产品及其迭代 / 任务 / 材料 / 审批记录 | 该产品的成员（`owner` / `member` / `guest`）、`admin` |
| 修改任务字段、推进与提交节点、挂材料 | 任务负责人、产品 `owner`、`pm`、`admin`；`guest` 不可写 |
| 拆解子任务、指派、关联模块 | 任务负责人（限本子树）、产品 `owner`、`pm` |
| 建迭代、模块、任务 | 产品 `owner` / `member` 且持有 `pm` 角色，或 `admin` |
| 管理工作流模板 | 产品 `owner`、`pm`、`admin` |
| 增删产品成员、调整产品角色 | 产品 `owner`、`admin` |
| 门禁审批 / 驳回 | 产品内持有该门禁 `roles` 之一的成员（`admin` 恒可），且非该节点提交人；`guest` 不可审批 |

判定在服务端统一实现，MCP 与控制台共用同一套，Agent 无法绕过：越权写返回 `403`，**非成员访问返回 `404`**（不暴露对象是否存在）。

### Product（产品）

顶层容器。`key` 为编号前缀（如 `CHK`），不随改名变化。

| 字段 | 说明 |
| --- | --- |
| `id` / `key` / `name` / `description` | `key` 为 2–8 位大写字母，产品内唯一且不可改 |
| `status` | `active` / `archived` |
| `default_iteration_workflow_id` | 新建迭代时的默认生命周期模板 |
| `default_task_workflow_id` | 新建任务时的默认模板（可按任务类型覆盖） |
| `created_by` | 创建人，自动成为该产品的 `owner` |

### ProductMember（产品成员）

成员与产品的多对多关系，是产品级可见性的唯一依据。全局角色回答「这个人会什么」，产品角色回答「在这条产品线上他能做什么」。

| 字段 | 说明 |
| --- | --- |
| `product_id` / `user_id` | 联合主键，每个成员在一个产品下只有一条记录 |
| `member_role` | `owner`（产品负责人，可管成员与产品设置）/ `member`（正常协作）/ `guest`（只读，适合外包与旁听） |
| `created_at` / `created_by` | 加入时间与邀请人 |

规则：

- 创建产品时创建人自动写入 `owner`，一个产品至少保留一个 `owner`。
- `admin` 无需成员记录即可访问，但要在任务上留痕时仍建议加入。
- 产品归档后成员记录保留，只读可查历史。
- 移除成员时，其负责的未终结任务必须先转派，规则同迭代收口（不变量 10）。

### Iteration（迭代）

**一条完整生命周期**，从调研走到总结。粒度由团队定义——可以是两周的短周期，也可以是数月的版本周期。

| 字段 | 说明 |
| --- | --- |
| `id` / `product_id` / `name` / `goal` | — |
| `number` | `<key>-IT<n>`，产品内递增 |
| `starts_on` / `ends_on` | 计划日期，`ends_on >= starts_on` |
| `status` | `planned` / `active` / `closed` / `cancelled` |
| `workflow_id` | 生命周期模板，默认取产品默认值 |

生命周期节点即「阶段」：迭代的当前节点就是它当前所处的阶段。控制台按迭代展示阶段进度，而不是另建一套阶段表。

### Module（模块）

产品下的多级树，是任务的归属「块」（对应 `docs/modules/` 中的一份设计文档）。

| 字段 | 说明 |
| --- | --- |
| `id` / `product_id` / `parent_id` | `parent_id` 为空即根模块 |
| `name` | 同级内唯一 |
| `path` | 物化路径，如 `/catalog/task-tree`，用于子树查询 |
| `sort_order` | 同级排序 |

约束：路径深度上限 5 层；模块移动时整棵子树重算 `path`。

### Task（任务）

工作项，可多级拆解。**调研、设计、开发、测试、总结都是任务，只是类型与模板不同。**

| 字段 | 说明 |
| --- | --- |
| `id` / `product_id` / `iteration_id` | 产品必须与迭代所属产品一致 |
| `number` | 多级编号，见下节，创建后不可变 |
| `parent_id` | 为空即顶层任务 |
| `title` / `description` | — |
| `kind` | `research` / `design` / `dev` / `test` / `ops` / `summary` |
| `assignee_id` | 唯一负责人，可为空（待认领） |
| `priority` | `low` / `normal` / `high` / `urgent` |
| `status` | `open` / `done` / `cancelled`，由工作流终态驱动 |
| `workflow_id` | 子任务默认继承父任务模板，可按 `kind` 覆盖 |
| `depth` | 由 `parent_id` 推导，上限 3 层 |

### 任务编号规则

| 场景 | 规则 | 示例 |
| --- | --- | --- |
| 顶层任务 | `<产品key>-<n>`，产品内从 1 单调递增 | `CHK-12` |
| 子任务 | `<父编号>.<m>`，父任务内递增 | `CHK-12.3` |
| 三级任务 | 继续追加 | `CHK-12.3.2` |
| 迭代 | `<产品key>-IT<n>` | `CHK-IT5` |

- 编号在创建时分配，**永不复用、永不重排**；任务取消后其编号作废。
- **不允许改变任务的父子关系**。确需重组时新建任务并关联原任务，保留历史。
- 允许在同一产品内更换所属迭代，编号不变。
- 编号的 `n` / `m` 由数据库序列按父级维度分配，并发下不重复。

### 任务拆解规则

拆解发生在迭代的「任务拆解」节点：原始需求级任务作为一级任务，较大的任务再拆成二级、必要时三级子任务。

| 规则 | 说明 |
| --- | --- |
| 拆解时机 | 只能在模板标记 `allows_task_creation: true` 的节点创建任务，默认是「任务拆解」「研发」「测试设计」 |
| 层级 | 最多 3 层：一级任务 → 二级任务 → 三级任务；超出应合并或另立迭代 |
| 归属 | 子任务必须与父任务同产品、同迭代；跨迭代工作需新建一级任务 |
| 模块 | 子任务默认继承父任务的 `TaskModule`，可增删，但仍在同产品内 |
| 模板 | 子任务默认继承父任务的任务流模板，可按 `kind` 覆盖 |
| 负责人 | 父任务与子任务可分派给不同人；父任务负责人负责收口与终态确认 |
| 状态 | 父任务终态受不变量 9 约束：全部子任务 `done` 才可进入终态节点 |
| 执行主体 | 叶子任务承担实际执行；父任务的节点用于规划与验收，不应长期停留在进行中 |

### TaskModule（任务 × 模块）

多对多。一个任务可同时挂多个模块（跨块工作），模块必须与任务同产品。

### Workflow（工作流模板）

见 [workflow.md](workflow.md)。`scope` 为 `iteration` / `task`；模板修改产生新版本，运行中的实例不受影响。

### WorkflowInstance / NodeState

工作项的运行态。实例绑定某个模板版本；`NodeState` 记录每个节点的状态与进入/完成时间，并携带乐观锁版本号——并发修改冲突时拒绝写入并返回 `409 STATE_CONFLICT`，客户端刷新后重试。**工作项的业务状态是节点状态的投影**，不单独维护。

### GateApproval（门禁审批）

| 字段 | 说明 |
| --- | --- |
| `instance_id` / `node_id` / `reviewer_id` | — |
| `decision` | `approve` / `reject` |
| `comment` | 驳回必填 |
| `created_at` | — |

同一位评审人在同一节点只计一次有效意见，以其最近一次决策为准。

### Artifact（材料引用）

门禁证据。**材料是引用而非内容**，可指向外部系统（见 [integrations.md](integrations.md)）。材料与「提交人 + 提交节点」绑定，不可跨节点复用。

提交后允许补挂材料；**替换已进入评审的材料会使该节点的既有审批全部作废**（历史记录保留），基于当前版本材料重新收集——审批必须对应提交人当前的材料。

| `kind` | 用于 | 典型来源 |
| --- | --- | --- |
| `research_report` | 竞品调研、技术调研 | Outline / inline |
| `tech_spike` | 关键技术验证 | Outline + Git |
| `spec` | 需求规格 | Outline / inline |
| `design` | 技术设计 | Outline / inline |
| `test_plan` | 测试设计 | Outline / inline |
| `test_cases` | 用例集 | Outline / 附件 |
| `test_execution` | 测试执行记录 | 附件 / CI 链接 |
| `test_report` | 测试报告 | Outline / 附件 |
| `pr` | 代码改动 | Git / SVN |
| `summary` | 迭代/任务总结 | Outline / inline |
| `pre_review` | AI 预评审自查报告（结构约束见 [skill.md](skill.md)） | inline |
| `file` | 其他证据 | 附件 |

| 字段 | 说明 |
| --- | --- |
| `kind` | 材料类型 |
| `provider` | `git` / `svn` / `outline` / `redmine` / `file` / `url` / `inline` |
| `url` / `externalId` | 外部定位；`inline` 时为空 |
| `snapshot` | 标题、版本、状态等快照，仅用于展示与审计，不回写 |

### Document（轻量内联文档）

平台内的 Markdown，`owner_type` 为 `product` / `iteration` / `task`，仅供没有对应外部工具时使用。**Chalkline 不是知识库**：成规模文档放 Outline，平台只保留门禁必需的最小说明。每次保存产生新 `version`。

### ExternalLink（对外关联）

记录工作项与外部系统的稳定关联：`subject_type` / `subject_id` / `provider` / `externalId` / `url`。用于 Redmine issue 回写、Outline 文档归属等场景。

### OutboxEvent（投递）

状态流转产生的领域事件写入 outbox，由投递器异步发送到邮件与 Webhook。`status` 为 `pending` / `sent` / `failed`，含重试次数与最后错误。投递失败不影响流程推进。

### AuditLog（审计）

`actor_id` + `action` + `subject_type` + `subject_id` + `payload` + `created_at`。所有状态流转、审批、指派、材料上传必须写入。

## 不变量

以下规则由领域层强制执行，任何入口（REST / MCP）不得绕过：

1. 模块深度 ≤ 5；任务深度 ≤ 3；同级模块名唯一。
2. 任务的 `product_id` 必须等于其迭代的 `product_id`；`TaskModule` 中的模块必须与任务同产品。
3. 任务编号唯一且不可变；父子关系不可变；编号不在删除后回收。
4. 一个任务只有一个负责人；子任务可指派给不同人，父任务负责人负责收口。
5. 任务只能创建在生命周期中显式允许的节点（模板字段 `allows_task_creation`），默认「任务拆解」「研发」「测试设计」允许。
6. 节点只有在所有前驱节点 `passed` 且自身门禁满足时才可进入 `in_progress`。
7. 节点 `passed` 的必要条件：门禁要求数量的有效 `approve` 且所需材料齐备；材料缺失或审批不足时状态不变。
8. 门禁审批人必须持有该门禁 `roles` 声明的角色之一（`admin` 恒可），且不得审批自己提交的节点（`reviewer_id != 提交人`）。
9. 任务只有在全部子任务 `done` 后才能进入终态节点（可由模板 `require_children_done` 关闭）。
10. 迭代进入终态前，其下未终结任务必须显式转移或取消，不允许静默丢弃。
11. 迭代进入终态后不可回退。需要新一轮交付时**新建迭代**，历史迭代完整保留。
12. 同一产品可同时存在多个 `active` 迭代，互不阻塞。
13. 所有写操作必须在事务中携带 `actor_id`，并写审计日志。
14. 材料的 `provider` 必须与 `kind` 匹配（如 `pr` 只能来自 `git` / `svn`），引用需通过格式校验。
15. 集成投递失败不改变门禁判定与工作项状态；外部系统不可用时流程照常推进。
16. 成员只能访问自己加入的产品；非成员访问返回 `404`，`admin` 不受此限；`guest` 可读不可写、不可审批。
17. 每个产品至少保留一个 `owner`；移除成员前，其未终结任务必须转派。
18. 迭代级节点可声明 `require_iteration_tasks_done`（默认 `false`）；为 `true` 时，该节点 `passed` 的必要条件是迭代下全部任务已终结（`done` / `cancelled`）。
19. `pre_review` 材料是 AI 自查报告而非审批：不计入门禁 `approvals`，不改变节点状态；驳回只来自评审人的 `review_step` 决策。

## 状态投影

| 工作项视图 | 来源 |
| --- | --- |
| 当前节点 / 当前阶段 | 最近进入且未完成的节点 |
| 是否阻塞 | 下一节点门禁未满足，或存在未完成前置任务依赖 |
| 进度百分比 | `passed` 节点数 / 可达节点总数 |
| 待办归属 | 节点门禁所需角色 → 该角色的成员列表 |
| 产品进展 | 各迭代所处阶段与完成度 |
