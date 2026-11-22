# 模块索引

每个模块是任务拆解的一个「块」：一份设计文档 + 一组代码目录 + 一批挂在其下的任务（`TaskModule`）。

## 模块清单

> 全部 8 个模块的设计文档已就绪；M1/M2 七个模块已交付，M3 规模化能力（多产品管理、模板导入导出、审计检索、评审 SLA、材料快照固化、SSE 推送、Webhook 订阅）落在既有模块内，状态列标注当前进度。

| 模块 | 职责 | 代码位置 | 设计文档 | 状态 |
| --- | --- | --- | --- | --- |
| identity | 成员、角色、PAT、会话与鉴权 | `internal/domain/identity` `internal/auth` | [identity.md](identity.md) | M1 · 已交付 |
| catalog | 产品、产品成员、模块树、产品文档 | `internal/domain/product` | [catalog.md](catalog.md) | M1 · 已交付；M3 规模化（搜索/分页/归档守卫/概览）已交付 |
| planning | 迭代、任务、子任务、指派、跨模块关联 | `internal/domain/planning` | [planning.md](planning.md) | M1 · 已交付 |
| workflow | 工作流模板、实例、节点状态、门禁裁决、材料引用 | `internal/workflow` | [workflow.md](workflow.md) | M1 · 已交付（引擎核心 M0）；M3 模板导入导出与评审 SLA 已交付 |
| mcp-gateway | MCP 服务、工具注册、PAT 鉴权 | `internal/mcp` | [mcp-gateway.md](mcp-gateway.md) | M1 · 已交付（22 工具） |
| integration | 外部系统适配：Git/SVN、Outline、Redmine、邮件、Webhook | `internal/integration` | [integration.md](integration.md) | M2 · 已交付；M3 Webhook 订阅管理与开放 API 策略已交付 |
| console | 控制台：产品总览、迭代看板、任务树、审批队列 | `web/src/features` | [console.md](console.md) | M2 · 已交付；M3 SSE 实时刷新、审计页、订阅页、规模化列表已交付 |
| platform | 配置、日志、迁移、部署脚本 | `internal/platform` `deploy` | [platform.md](platform.md) | M0 · 已交付；M3 新增 SLA/事件间隔配置与 0008 迁移 |

## 依赖关系

```
platform  ──> 所有模块可用
identity  ──> 所有需要鉴权的模块
catalog   ──> planning（产品与模块约束）
workflow  ──> planning（任务/迭代跑工作流）
planning + workflow ──> mcp-gateway / console
integration ──> workflow（事件投递）、planning（外部关联回写）
```

实现顺序：**platform → identity → workflow（引擎纯函数）→ catalog → planning → mcp-gateway → console → integration**。

## 模块文档模板

每个模块建 `<module>.md` 时按此结构写，控制在两页内：

```markdown
# 模块：<名称>

## 边界
负责什么，明确不负责什么。

## 对外接口
REST 端点、MCP 工具、被其他模块调用的 Go 接口。

## 核心流程
关键用例的步骤与事务边界（必要时画时序）。

## 数据
涉及的表与索引，读写模式。

## 不变量
该模块自己保证的规则（与 docs/domain/model.md 不冲突）。

## 验收标准
可检查的条件，对应门禁材料。

## 开放问题
尚未决定的事项，由哪个任务收敛。
```

## 与任务的关系

- 建任务时通过 `chalkline_link_module` 关联到一个或多个模块；跨模块任务在设计文档里写清切面。
- 模块文档是门禁材料之一：任务进入「设计」或「规格」门禁时，需提交对应模块文档的版本链接。
- 模块文档与代码目录一一对应，新增代码目录必须先在本文档登记。
