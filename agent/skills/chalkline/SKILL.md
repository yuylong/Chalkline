---
name: chalkline
description: 在 Chalkline 平台上推进工作项。用户提到 Chalkline、迭代、任务、任务拆解、门禁评审、下一步该做什么，或要求「把这个任务推到下一阶段」时使用。负责把自然语言请求翻译成 MCP 工具调用序列，并汇报状态与阻塞人。
---

# Chalkline 流程推进

Chalkline 是服务端流程引擎：状态、权限、门禁都由服务端裁决。你的职责是**把用户的自然语言变成正确的工具调用**，不是替人做决定。

## 五步循环

每次推进都走这五步，不要跳步：

1. `chalkline_whoami` —— 确认身份、角色、可见产品。第一次对话或换人时做一次即可。
2. `chalkline_get_next_action` —— 拿到当前工作项的 `step`、`status`、`blockers`。
3. **按 status 分支**：
   - `ready`：执行该节点要求的工作，然后 `chalkline_attach_artifact` 挂材料 → `chalkline_submit_step` 提交。
   - `blocked`：**不推进**。把 `blockers` 翻译成人话：缺哪份材料、需要几个什么角色的人审批。
   - `in_progress`：说明当前在哪一步，问是否需要继续。
4. 提交后重新 `chalkline_get_next_action`，确认状态确实变了。
5. 用固定模板汇报（见下）。

## 硬规矩

- **不猜下一步**：一切以 `get_next_action` 为准，不根据文件名、对话历史或常识推测流程。
- **不代替人审批**：`review_step` 只能由人发起。你要做的是说清「需谁确认」。
- **不重试越权**：`403` 是身份问题，重试不会成功，直接告诉用户需要谁来做。
- **不伪造材料**：材料是引用（URL / 外部 ID / inline 说明），不能拿「已完成」的描述充当证据。
- **一次只推进一个工作项**，用户要求批量时逐项确认。
- **不改模板**：修改工作流模板是人的决策，不在自动推进范围内。

## 输出模板

```
已完成：<做了什么>
当前状态：<节点名> / <passed|awaiting_review|in_progress|blocked>
下一步：<等谁审批 / 还需什么材料 / 可以继续做什么>
```

一行阻塞原因要能直接转述给同事，例如「卡在『规格』门禁：缺 1 份 spec 材料，需 1 名 reviewer 确认」。

## 参考

- [references/workflow-basics.md](references/workflow-basics.md) —— 节点、门禁、材料、拆解与并行的概念
- [references/errors.md](references/errors.md) —— 403 / 404 / 409 / 422 的处理策略
- [references/examples.md](references/examples.md) —— 典型对话到工具序列的对照
