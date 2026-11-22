# 工作流与门禁

## 设计要点

- 工作流是**数据**：`workflows.definition` 存 JSON（作者用 YAML 编辑，平台校验后转换）。
- 引擎只支持三种要素：**节点、有向边、门禁**。不引入脚本、条件表达式、子流程。
- 两种作用域：`iteration`（生命周期）、`task`（具体工作）。
- **内置模板只是初始化数据**：首次启动写入数据库后与用户自建模板完全等价，可编辑、复制、另建、设默认，没有「内置不可改」的限制。
- 模板修改即升版本；运行中的实例固定使用实例化时的版本，不会被静默改写。

## 定义格式

```yaml
id: wf-task-standard
name: 标准开发任务
scope: task                 # iteration | task
version: 3
settings:
  require_children_done: true
  allow_skip_nodes: false
nodes:
  - id: clarify
    name: 需求澄清
    type: normal            # normal | auto | terminal
  - id: spec
    name: 规格编写
    type: normal
    gate:
      approvals: 1
      roles: [reviewer, pm]
      mode: all             # all = 需全部人数；any = 任一人即可
      artifacts:
        - kind: spec
          required: true
        - { kind: pre_review, required: false }   # AI 自查报告，可选
  - id: implement
    name: 实现
    type: normal
  - id: test_design
    name: 测试设计
    type: normal
    gate:
      approvals: 1
      roles: [qa]
      artifacts:
        - { kind: test_plan, required: true }
  - id: test_exec
    name: 测试执行
    type: normal
    gate:
      approvals: 1
      roles: [qa]
      artifacts:
        - { kind: test_execution, required: true }
  - id: done
    name: 完成
    type: terminal
edges:
  - { from: clarify,     to: spec }
  - { from: spec,        to: implement }
  - { from: implement,   to: test_design }
  - { from: test_design, to: test_exec }
  - { from: test_exec,   to: done }
```

## 节点类型

| 类型 | 语义 |
| --- | --- |
| `normal` | 需要有人推进：开始 → 提交材料 → 等待门禁 |
| `auto` | 进入即完成，用于占位或标记（如「合并后自动归档」由外部回调触发） |
| `terminal` | 终态，不可回退。多个终态时需指定 `outcome`（如 `done` / `cancelled`） |

节点可选属性：

| 属性 | 说明 |
| --- | --- |
| `gate` | 门禁配置，省略即不拦截，提交直接通过 |
| `allows_task_creation` | 仅 `iteration` 作用域有效；该阶段允许创建任务，默认 `false` |
| `require_iteration_tasks_done` | 仅 `iteration` 作用域有效；为 `true` 时该节点 `passed` 需迭代下全部任务终结，默认 `false` |
| `due_in_days` | 该阶段期望耗时，超期在控制台标红（不阻断） |

`allows_task_creation` 是声明式白名单，不是条件逻辑——它保证「规格没批完不能开工建任务」，同时不把规则写进引擎。

## 门禁语义

门禁是节点的**准出条件**，判定完全在服务端：

| 配置 | 含义 |
| --- | --- |
| `approvals: N` | 需 N 条有效 `approve` |
| `roles: [...]` | 审批人必须持有其中任一角色 |
| `mode: all` | 需来自 N 个不同评审人；`any` 时 N 人可重复 |
| `artifacts[].kind` | 必须存在该类型的材料，且由本节点提交 |
| `due_in_days` | 可选，超期在控制台标红提醒（不阻断） |

当前只做 `any` / `all` 两种会签语义，配置结构预留扩展位（如按材料类型指定不同评审组），后续按实际需要增加，不预先实现。

判定规则：

1. 材料齐备 **且** 有效审批数 ≥ `approvals` → `passed`
2. 任一 `reject` → 节点回到 `in_progress`，驳回理由写入审计与通知
3. 二者缺一 → 维持 `awaiting_review`，`get_next_action` 返回缺失项明细

> 关键约束：门禁通过只取决于数据库中的 `GateApproval` 与 `Artifact` 记录。Agent 在对话里声称「已评审」不产生任何效果。

### 预评审（pre_review）

`pre_review` 是材料的一种：提交人侧的编码 Agent 在提交前对照门禁要求逐项自查，把报告作为材料随提交挂上。**它是材料，不是审批**：

- 不计入 `approvals`，不参与 `passed` / `rejected` 判定；
- 不能触发驳回——驳回只来自评审人的 `review_step` 决策；
- 门禁可把它配为 `required: true`，那也只校验「报告存在且格式合法」，而不是「AI 认可」。

自查报告必须逐项对照门禁材料清单并附证据引用，不写「总体没问题」式的空话——结构由 Skill 约束（见 [skill.md](skill.md)）。评审人侧同样可以让自己的 Agent 调 `chalkline_get_task` 拉取全部材料做独立核查，意见供人参考，不产生平台状态变更。

## 状态与判定算法

节点状态：`pending` → `ready` → `in_progress` → `awaiting_review` → `passed` / `rejected`

```
进入判定（纯函数）:
  ready(node) = 所有前驱节点 passed
                AND 未被跳过
                AND 前置任务依赖已满足（跨任务 depends_on）

可开始:
  in_progress(node) 仅当 ready(node)

可提交:
  提交时校验门禁所需材料是否随提交上传，缺则 422

准出判定（每次审批或材料变动后重算）:
  passed(node) = 无未决 reject
                 AND 有效 approve 数 >= gate.approvals
                 AND 必需材料齐备
  （无 gate 的节点：提交即 passed）
```

父任务收口：`require_children_done: true` 时，父任务的终态节点在全部子任务 `done` 之前保持阻塞。

迭代收口：节点声明 `require_iteration_tasks_done: true` 时，迭代下存在未终结任务（`open`）则该节点保持阻塞，`get_next_action` 在 `blockers` 中返回缺失项。

材料变更：`awaiting_review` 期间允许补挂材料；替换已进入评审的材料会使该节点既有审批全部作废（记录保留），基于当前版本重新收集。

并发控制：节点状态变更基于乐观锁版本号，并发冲突返回 `409 STATE_CONFLICT`，客户端刷新状态后重试；同一评审人的重复决策采用 upsert 语义（最近一次为准），天然幂等。

## 并行与汇合

模板是 DAG，并行不需要新语法——分叉与汇合都是**边**的效果：

- **分叉（fork）**：一个节点 `passed` 后，它的所有后继（各自前驱齐备）同时进入 `in_progress`。实例的「当前节点」是一个**集合**，控制台按泳道并行展示。
- **汇合（join）**：多前驱节点采用 **AND 语义**——全部前驱 `passed` 才 `ready`。不做 OR-join（任一前驱通过即放行），避免「哪条路走通了」变得不可判定。
- **单支驳回不连坐**：驳回只回退该分支的提交，并行分支照常推进与评审。
- **门禁独立**：并行节点各配各的门禁，审批记录互不共享。
- **并行建任务**：`allows_task_creation` 在并行分支上各自生效——「研发」建开发任务、「测试设计」建测试任务，互不等待。
- 单个模板并行分支建议 ≤ 3 条；再多应拆成两个工作流，或下沉为子任务承载。

状态投影不变：实例存在任一 `in_progress` / `awaiting_review` 节点即视为「进行中」，串行模板同理。

## 内置模板

平台预置下列模板，**均可在控制台编辑或复制**，删掉不需要的节点即可适配团队节奏。`{ A ∥ B }` 表示并行分支。

| 模板 | scope | 节点序列（带门禁的标 ◆） |
| --- | --- | --- |
| 全生命周期（默认） | iteration | 竞品调研◆ → {产品规划◆ → 任务拆解◆ ∥ 关键技术调研◆} → {研发 ∥ 测试设计◆} → 测评◆ → 发布◆ → 总结◆ |
| 轻量交付周期 | iteration | 规划◆ → 任务拆解 → {研发 ∥ 测试设计◆} → 测试验证◆ → 发布◆ → 总结◆ |
| 标准开发任务 | task | 澄清 → 规格◆ → 实现 → 测试设计◆ → 测试执行◆ → 完成 |
| 调研任务 | task | 调研提纲 → 信息收集 → 分析 → 调研报告◆ → 完成 |
| 测试任务 | task | 测试设计◆ → 用例评审◆ → 测试执行 → 缺陷回归 → 测试报告◆ → 完成 |

「全生命周期」默认用于新迭代；若团队是常规短迭代，把产品的默认迭代模板改为「轻量交付周期」即可，前者保留备用。

对应必填材料：

| 门禁 | 必需材料 | 默认载体 |
| --- | --- | --- |
| 竞品调研 | `research_report` | Outline |
| 关键技术调研 | `research_report` + `tech_spike` | Outline + Git |
| 产品规划 | `spec` | Outline / inline |
| 任务拆解 | 无（人工确认任务树） | 控制台 |
| 测评 | `test_report` | Outline / 附件 |
| 发布 | `test_report` + `pr` | Outline + Git |
| 总结（迭代/任务） | `summary` | Outline / inline |
| 测试设计 | `test_plan` | Outline / inline |
| 用例评审 | `test_cases` | Outline / 附件 |
| 测试执行 | `test_execution` | 附件 / CI 链接 |

任一门禁均可追加可选材料 `pre_review`（AI 提交前自查报告，inline），配为 `required: true` 时仅校验报告存在，语义见「门禁语义 · 预评审」。

门禁只校验**材料引用存在且格式合法**，不要求外部系统实时可访问——Outline 挂了不应导致评审无法通过。引用模型与集成方式见 [integrations.md](integrations.md)。

## 生命周期模板示例

迭代承载完整生命周期，从调研一路到总结：

```yaml
id: wf-iteration-lifecycle
name: 全生命周期（默认）
scope: iteration
version: 1
nodes:
  - { id: research_market, name: 竞品调研, type: normal,
      gate: { approvals: 1, roles: [pm, reviewer], artifacts: [{ kind: research_report, required: true }] } }
  - { id: research_tech, name: 关键技术调研, type: normal,
      gate: { approvals: 1, roles: [reviewer], artifacts: [{ kind: research_report, required: true }, { kind: tech_spike, required: true }] } }
  - { id: planning, name: 产品规划, type: normal,
      gate: { approvals: 1, roles: [pm], artifacts: [{ kind: spec, required: true }] } }
  - { id: breakdown, name: 任务拆解, type: normal, allows_task_creation: true,
      gate: { approvals: 1, roles: [pm, reviewer] } }
  - { id: build, name: 研发, type: normal, allows_task_creation: true }
  - { id: test_design, name: 测试设计, type: normal, allows_task_creation: true,
      gate: { approvals: 1, roles: [qa], artifacts: [{ kind: test_plan, required: true }] } }
  - { id: assess, name: 测评, type: normal,
      gate: { approvals: 1, roles: [qa, reviewer], artifacts: [{ kind: test_report, required: true }] } }
  - { id: release, name: 发布, type: normal, require_iteration_tasks_done: true,
      gate: { approvals: 1, roles: [pm, admin], artifacts: [{ kind: test_report, required: true }, { kind: pr, required: true }] } }
  - { id: summary, name: 总结, type: normal,
      gate: { approvals: 1, roles: [pm], artifacts: [{ kind: summary, required: true }] } }
  - { id: closed, name: 结束, type: terminal }
edges:
  - { from: research_market, to: planning }        # 分叉 A：规划线
  - { from: research_market, to: research_tech }  # 分叉 B：技术调研并行推进
  - { from: planning,        to: breakdown }
  - { from: breakdown,       to: build }
  - { from: research_tech,   to: build }           # 汇合：拆解确认 + 技术调研通过才开工
  - { from: breakdown,       to: test_design }     # 分叉：测试设计与研发并行
  - { from: build,           to: assess }
  - { from: test_design,     to: assess }          # 汇合：研发 + 测试设计都完成才测评
  - { from: assess,          to: release }
  - { from: release,         to: summary }
  - { from: summary,         to: closed }
```

关键点：

- 任务只能在带 `allows_task_creation: true` 的节点创建（任务拆解 / 研发 / 测试设计）——调研和规划没通过时，任何人都无法建任务。
- 「任务拆解」与「关键技术调研」**并行**：市场调研通过后即可进入规划与拆解，技术验证同步进行，互不等待。
- 「研发」是 **AND 汇合**：任务拆解确认 + 关键技术调研通过后才开工——技术没验证过的东西不该直接开发。
- 「研发」与「测试设计」**并行**：测试设计不必等开发全部结束才开始，两边都完成才进入「测评」。
- 「任务拆解」门禁不要求新材料，只需一名评审人确认任务树：一级任务是否覆盖了规格、粒度是否适合分派、跨模块任务是否标注清楚。轻量模板可关掉这道门禁。
- 「发布」节点声明 `require_iteration_tasks_done: true`：全部任务终结才能通过发布，收口检查从迭代 `closed` 提前到发布，避免收尾时才强制转移一批任务。
- 拆解在「任务拆解」节点完成一级任务，二级任务可在拆解时或研发过程中继续拆，规则见 [model.md](model.md#任务拆解规则)。

## 迭代与任务的关系

- 迭代进入「任务拆解」后，团队才在迭代下建任务；一级任务在拆解节点建立，较大的任务再拆成二级、三级子任务，每个任务跑自己的任务流（开发/调研/测试各选模板）。
- 迭代的「测评」门禁看的是本轮整体测试报告，任务流里的测试节点管单个任务的验证，两者不重复但互为证据。
- 迭代「总结」门禁必交 `summary`，用于沉淀本轮的经验与下一轮输入。

## 可定制边界

| 可定制 | 不可定制 |
| --- | --- |
| 两层流程的节点数量、名称、顺序、分支 | 引入条件表达式或脚本 |
| 哪些节点挂门禁、门禁参数 | 绕过服务端判定 |
| 哪些阶段允许建任务 | 运行中实例改模板 |
| 迭代与任务使用不同流程、复制内置模板改默认 | 节点内嵌子流程 |

**自定义到什么程度**：能定义任意 DAG 的审批流（串行、并行会签、多终态），但不能写逻辑。需要「如果金额 > 100 万则加一级审批」这类条件时，做法是拆成两个模板，由人选择——保持引擎可验证。

## 版本与迁移

- 修改模板必升 `version`，旧实例不受影响。
- 内置模板与自建模板同表管理，编辑内置模板等同于新建一个版本并设为默认。
- 模板删除采用软删（`archived`），历史实例仍可回溯。
- 模板变更记入审计，含前后 diff。
