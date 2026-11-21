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

## 内置模板

平台预置下列模板，**均可在控制台编辑或复制**，删掉不需要的节点即可适配团队节奏。

| 模板 | scope | 节点序列（带门禁的标 ◆） |
| --- | --- | --- |
| 全生命周期（默认） | iteration | 竞品调研◆ → 关键技术调研◆ → 产品规划◆ → 研发 → 测评◆ → 发布◆ → 总结◆ |
| 轻量交付周期 | iteration | 规划◆ → 研发 → 测试验证◆ → 发布◆ → 总结◆ |
| 标准开发任务 | task | 澄清 → 规格◆ → 实现 → 测试设计◆ → 测试执行◆ → 完成 |
| 调研任务 | task | 调研提纲 → 信息收集 → 分析 → 调研报告◆ → 完成 |
| 测试任务 | task | 测试设计◆ → 用例评审◆ → 测试执行 → 缺陷回归 → 测试报告◆ → 完成 |

「全生命周期」默认用于新迭代；若团队是常规短迭代，把产品的默认迭代模板改为「轻量交付周期」即可，前者保留备用。

对应必填材料：

| 门禁 | 必需材料 |
| --- | --- |
| 竞品调研 | `research_report` |
| 关键技术调研 | `research_report` + `tech_spike` |
| 产品规划 | `spec` |
| 测评 | `test_report` |
| 发布 | `test_report` + `pr` |
| 总结（迭代/任务） | `summary` |
| 测试设计 | `test_plan` |
| 用例评审 | `test_cases` |
| 测试执行 | `test_execution` |

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
  - { id: build, name: 研发, type: normal, allows_task_creation: true }
  - { id: assess, name: 测评, type: normal,
      gate: { approvals: 1, roles: [qa, reviewer], artifacts: [{ kind: test_report, required: true }] } }
  - { id: release, name: 发布, type: normal,
      gate: { approvals: 1, roles: [pm, admin], artifacts: [{ kind: test_report, required: true }, { kind: pr, required: true }] } }
  - { id: summary, name: 总结, type: normal,
      gate: { approvals: 1, roles: [pm], artifacts: [{ kind: summary, required: true }] } }
  - { id: closed, name: 结束, type: terminal }
edges:
  - { from: research_market, to: research_tech }
  - { from: research_tech,   to: planning }
  - { from: planning,        to: build }
  - { from: build,           to: assess }
  - { from: assess,          to: release }
  - { from: release,         to: summary }
  - { from: summary,         to: closed }
```

关键点：`build` 节点带 `allows_task_creation: true`，所以任务只能在「研发」阶段建——调研和规划没通过时，任何人都无法开工建任务。

## 迭代与任务的关系

- 迭代进入「研发」阶段后，团队才在迭代下建任务；任务跑自己的任务流（开发/调研/测试各选模板）。
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
