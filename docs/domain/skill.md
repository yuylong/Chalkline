# Agent Skill 设计

## 定位

MCP 决定「能做什么」，Skill 决定「怎么配合人做」。两者是同一发布物的两半：

| 层 | 职责 | 失效后果 |
| --- | --- | --- |
| MCP（服务端） | 权限、门禁、状态机——硬约束 | 流程可被绕过，属严重缺陷 |
| Skill（Agent 侧） | 话术映射、推进顺序、汇报格式——软引导 | Agent 变笨、多问几轮，流程仍安全 |

**设计底线：任何安全相关的判断都不能只写在 Skill 里。** Skill 被模型忽略时，最坏结果只能是「Agent 做错动作被 403/409 拦下」，而不能是「状态被错误推进」。

## 为什么需要

20 个 MCP 工具对人来说是负担。开发者的心智模型是「把这个任务推到评审」，而不是「先 get_next_action 再 attach_artifact 再 submit_step」。Skill 承担翻译工作：

```
「帮我把 CHK-12 推到规格评审」
  → whoami → get_next_action → attach_artifact(spec) → attach_artifact(pre_review) → submit_step
  → 「已提交，附自查报告，等 1 名 reviewer 确认」
```

## 目录与分发

```
agent/skills/chalkline/
├── SKILL.md                     触发条件、六步循环、硬规矩、输出模板
└── references/
    ├── workflow-basics.md       概念速查（给 Agent 读，不给人背）
    ├── errors.md                错误码到动作的映射
    └── examples.md              典型对话 → 调用序列
```

- 跟随仓库发布，**版本与 MCP 服务端同号**；`SKILL.md` 里声明的工具名与 `docs/domain/mcp.md` 必须一致。
- 安装方式：拷贝到 Agent 的 skills 目录，或由 `chalklinectl skill install` 写入（二期）。
- 升级策略：MCP 增加工具或改错误码时，同一次提交里更新 `references/`，CI 校验示例中的工具名是否都存在。

## 六步循环

1. `whoami` 确认身份与可见产品；
2. `get_next_action` 拿 `step` / `status` / `blockers`；
3. `ready` 则执行该节点要求的工作，`blocked` 则停下来说明缺什么、找谁；
4. **提交前自检**：对照门禁要求逐项核查材料，生成 `pre_review` 自查报告（每项给结论与证据引用）；
5. 提交后重新读取状态，确认变化；
6. 按固定模板汇报。

## 四条硬规矩

1. **不猜下一步**——只信 `get_next_action`；
2. **不代替人审批**——审批必须由人发起；
3. **不重试越权**——403 立即停手，说明需要谁；
4. **一次一个工作项**——避免并发提交乱序。

## 重点场景的话术约束

| 场景 | 约束 |
| --- | --- |
| 任务拆解 | 批量调 `decompose_task`；粒度不明先问；拆完必须问指派，不自动认领；超 3 层要解释限制 |
| 并行工作流 | 分别汇报每条分支状态；不合并成「进行中」；一条驳回不影响另一条；汇合点单独说明阻塞在谁 |
| 门禁驳回 | 先确认材料已改，再重提；不盲目重复提交 |
| 预评审自查 | 报告必须逐项对照门禁材料清单并附证据引用，不写「总体没问题」式空话；自查帮评审人聚焦，不是替材料背书 |
| 评审人侧核查 | 评审人可让 Agent 调 `get_task` 拉全部材料独立核查并给出意见草稿；是否 `review_step` 由评审人自己决定 |
| 权限不足 | 不重试、不换工具绕行，把所需角色告诉用户 |

完整对话示例见 [agent/skills/chalkline/references/examples.md](../../agent/skills/chalkline/references/examples.md)。

## 效果如何验证

- **回放**：把 `examples.md` 的五个场景做成固定输入，检查 Agent 是否产生预期工具序列与汇报；换模型后重跑一次。
- **负向用例**：故意让 Agent 在门禁未满足时尝试提交，确认它停在 `blocked` 而不是编造结果。
- **不一致告警**：CI 校验 Skill 中出现的工具名、错误码是否都存在于 `mcp.md`，防止文档漂移。

## 不做什么

- 不写状态判断逻辑（例如「如果材料齐了就算通过」）——那是服务端的事。
- 不内置业务模板（拆解的默认粒度、审批人选择规则）——由团队模板与人的判断决定。
- 不做多 Agent 编排——Chalkline 只服务单个 Agent 会话的推进。
