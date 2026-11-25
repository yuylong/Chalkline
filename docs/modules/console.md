# 模块：console

> **M2 范围** · **已交付**。人类控制台：以进度为中心的 SPA——产品总览、迭代看板、任务树、审批队列。模块索引见 [README.md](README.md)。

## 边界

负责：

- **前端应用**：React 19 + TypeScript + Vite 构建的 SPA，由 nginx 托管静态产物（Go 进程不服务静态文件，见 [tech.md](../tech.md)）。
- **功能视图**：产品总览、迭代看板（阶段泳道）、任务树（多级拆解）、审批队列（待我确认的门禁）、进度可视化（燃尽、状态分布、成员负载）。
- **人类裁决入口**：评审人在控制台对门禁做 `approve` / `reject`，调 REST 接口（与 MCP 共用 app 层，服务端判定）。
- **前端基础设施**：API 客户端（TanStack Query 统一缓存/失效）、类型、通用组件（Ant Design 5）。

不负责：业务规则与权限判定（后端 app/domain）、Agent 接入（mcp-gateway）、外部系统（integration）。当前 `web/index.html` 是 M0 占位页，M2 替换为正式 SPA。

## 对外接口

- **REST 消费方**：调用后端 `/api/v1/...` 端点（各业务模块提供），经 nginx 反代。
- **路由**：`web/src/app/routes.tsx` 注册，SPA 路由回退由 nginx `try_files` 处理。
- **鉴权**：登录拿 JWT，后续请求带 `Authorization`；刷新与登出在前端管理。

视图清单（对应 `web/src/features/<feature>/`）：

| Feature | 页面 | 依赖的后端 |
| --- | --- | --- |
| `iterations` | **首页「当前迭代」**（登录直达个人当前迭代看板）、迭代看板（阶段泳道 + 并行分支）、迭代详情（含「设为当前迭代」） | planning + workflow |
| `products` | 产品列表、产品详情（模块树、默认工作流、成员） | catalog |
| `tasks` | 任务树（多级拆解）、任务详情（节点状态、材料、审批） | planning + workflow |
| `gates` | 门禁材料查看与提交、审批操作、成果物按步骤浏览（任务/迭代详情卡片 + 审批队列弹窗） | workflow |
| `reviews` | 审批队列、审批历史 | workflow + identity |
| `users` | 用户管理（admin）：成员列表、新建、改角色/启禁用、重置密码 | identity |
| `mcp` | MCP 接入（全员）：API Key（PAT）生成/吊销 + 只读连接信息 | identity |
| `workflows` | 工作流模板：列表、YAML 编辑（新建/更新）、导入/导出、归档 | workflow |
| `integrations` | Provider 配置（admin）：启停与 baseURL 可视化维护 | integration |

### 导航结构

左侧菜单顺序固定为：**当前迭代、审批** ｜ 分割线 ｜ **产品、审计、MCP 接入** ｜ 分割线 ｜ **用户管理、Provider 管理、工作流管理、Webhook 订阅**（后四项除工作流管理外仅 admin 可见；MCP 接入对全员开放）。

- **MCP 接入**（`/mcp`）：只读展示 MCP 连接信息（端点 `POST /mcp`、streamable HTTP、`Authorization: Bearer <API Key>`，端点按当前 origin 拼接）；API Key（PAT）可生成多个、仅本人可见（后端按 actor 过滤），明文只在签发时展示一次，支持吊销。

- **当前迭代**（`/`）是登录后默认落地页：直接渲染个人选定的当前迭代完整看板（阶段泳道、阶段操作、任务看板、进度图表），未设置时给出向导（去产品列表挑迭代 → 迭代详情右上角「设为当前迭代」）。迭代/任务详情页在菜单上仍高亮「当前迭代」。
- 迭代详情页顶部提供「设为当前迭代 / 取消当前迭代」，仅 `planned`/`active` 迭代可设为当前迭代（后端同样拒绝终态）。
- 迭代详情页任务看板卡片提供「新建任务」（`POST /iterations/{id}/tasks`）：表单含标题、描述、类型、优先级、负责人、关联模块（产品模块树多选）与任务流模板（不选则产品默认）；生命周期终态时按钮禁用，非允许建任务阶段由后端 409 拒绝并提示。
- 任务详情页顶部提供「编辑」（`PATCH /tasks/{id}`：标题、描述、优先级、负责人）与「删除」（`DELETE /tasks/{id}`，二次确认；仅未终结且无子任务可删，成功后返回所属迭代看板）。

## 核心流程

- **进度视图**：迭代看板按工作流节点渲染泳道，并行分支并列展示（[workflow.md](../domain/workflow.md)「并行与汇合」）；任务树支持展开多级子任务。
- **审批裁决**：评审人在 `reviews` 队列点开待办 → 「查看成果物」弹窗按步骤浏览该工作项全部成果物（待审步骤置顶标红）→ 点通过/驳回 → 调 REST → 服务端判定 → 状态刷新。
- **当前节点摘要 → 节点详情页**（两跳）：迭代详情页只渲染**摘要**——阶段名、状态标签、成果物/评审计数、最新成果物标题、阻塞计数；点击摘要行（或「查看详情」）进入独立节点详情页（`/iterations/:id/node/:nodeId`），自上而下为：阻塞提示 → 按节点状态给出的操作（开始 / 录入并提交成果物 / 通过·驳回）→ **最新成果物正文就地 Markdown 渲染（不弹窗）** → 节点内全部成果物列表（点选某版本查看正文与提交时点快照 + 本阶段评审意见时间线）。详情页「返回迭代页」回到来源迭代页（首页或迭代详情页）。
- **成果物浏览**：任务详情与迭代详情提供「成果物（按步骤组织）」卡片——按工作流节点分组、按模板顺序排列，逐条点开查看元数据、inline 全文（**按 Markdown 渲染**，react-markdown）、外链与提交时点快照（`snapshot`）；外部系统不可达时链接仍可点，不阻断评审。
- **Agent 协作**：同一任务的状态在控制台与 Agent 共享（同一份后端事实）；Agent 提交后控制台实时（轮询/刷新）看到 `awaiting_review`。
- **降级**：集成（Outline/Git）不可用时，材料引用仍展示为链接，不阻断评审。

## 数据

前端不直接访问数据库，全部经 REST。构建产物：

- `web/dist/` 静态文件 → nginx 托管。
- `web/src/shared/api.ts`：API 客户端，集中处理鉴权头、错误包络解析、TanStack Query 缓存键。

## 不变量（本模块保证）

- 前端不做权限判定，只做展示与请求发起；越权由后端返回 403/404，前端按错误码呈现（不暴露存在性）。
- 不在前端缓存敏感数据；JWT 存内存或 httpOnly（M2 评估），PAT 不在控制台暴露。
- 所有写操作必须经后端事务；前端不得假设写成功，以响应为准。
- 与后端共用错误码语义（403/404/409/422），错误 `message` 直接可展示。

## 验收标准

- [x] 产品列表/详情、迭代看板、任务树、审批队列页面可用，数据来自真实后端。
- [x] 审批操作：通过/驳回后状态正确刷新；并行分支正确并列展示。
- [x] 越权/不可见资源前端呈现 404 而非空白，不泄露存在性。
- [x] 集成不可用时页面不崩溃，材料链接可点。
- [x] 燃尽图/状态分布图正确渲染（Recharts）。
- [x] `pnpm build` 产出 nginx 可托管的静态文件（`vite preview` 验证 SPA 路由回退与静态资源）。
- [ ] `make up` 后 `https://localhost` 正常加载 SPA、全流程端到端冒烟：需真实 PG + docker 环境，沙箱未实测。

## 任务拆解（M2 排期）

1. 前端脚手架：Vite + React 19 + TS + Ant Design 5 + TanStack Query + Recharts，接入 `web/src/shared/api.ts` 与路由。
2. 登录页 + JWT 管理（与 identity 登录端点联调）。
3. `products` feature：列表、详情（模块树、成员）。4. `iterations` feature：看板（阶段泳道、并行分支）、详情。
5. `tasks` feature：任务树（多级展开）、详情（节点状态/材料/审批时间线）。
6. `reviews` feature：待审批队列 + 审批操作（approve/reject + 驳回理由）。
7. `gates` feature：材料查看与提交、`pre_review` 自查报告展示。
8. 进度可视化：迭代燃尽、状态分布、成员负载（Recharts）。
9. nginx 配置：SPA `try_files` 回退、静态托管（复用 M0 deploy，微调）。
10. 端到端冒烟：`make up` 全流程走通一个迭代的立项→拆解→评审→收尾。
11. 文档：更新模块索引状态。

## 开放问题

- 前端状态管理是否引入额外库（如 Zustand）？→ **已定**：TanStack Query 管服务端状态 + React 局部状态，未引入额外库。
- 实时性：轮询还是 WebSocket/SSE？→ **已定**：M2 用轮询（审批队列 10s `refetchInterval`），M3 评估服务端推送。
- 国际化是否在 M2 做？→ **已定**：先中文（antd ConfigProvider + dayjs zh-CN），i18n 框架预留。
- JWT 存储位置？→ **已定**：仅内存，刷新页面回登录页；不落 localStorage/sessionStorage。
- 材料/审批数据来源？→ **已定**：由实例视图（`InstanceView.artifacts` / `.approvals`）随任务/迭代详情返回，前端不做二次聚合。- **评审时如何看成果物？**→ **已定**：审批队列每条待办提供「查看成果物」弹窗（复用实例视图缓存键，SSE 自动刷新）；任务/迭代详情提供按步骤分组的成果物卡片，待审步骤置顶。
- inline 成果物如何渲染？→ **已定**：按 Markdown 渲染（`react-markdown` + `remark-gfm`，`web/src/shared/markdown.tsx` 统一组件）。必须启用 `remark-gfm`：`react-markdown` 默认只解析 CommonMark，**GFM 表格语法会退化成竖线纯文本**；表格横向滚动由外层包装容器承担（不能在 `<table>` 上用 `display:block`，会破坏列宽与 `border-collapse`），列对齐透传 GFM 的 `:---:`/`:---:`/`---:` 语法。列表行内显示纯文本摘要，详情弹窗显示完整渲染，附「复制原文」。
- 迭代页如何组织阶段成果物？→ **已定**：`gates/CurrentNodePanel.tsx`——摘要可折叠，展开后就地展示最新成果物正文（不弹窗）、历史版本与评审意见；「最新」由 `ArtifactRef.createdAt` 倒序判定（后端 `ArtifactView` 透传）。
## M3 增量：实时推送与规模化视图

> 已交付。

- **实时性收敛：SSE**（不引入 WebSocket）。理由：控制台只需要服务端→客户端的单向推送；SSE 走普通 HTTP，nginx 反代零改造（仅关缓冲），且与 JWT 头鉴权天然兼容（EventSource 不支持自定义头，前端用 fetch 流式读取，`web/src/shared/events.ts`）。
  - 端点 `GET /api/v1/events`（JWT 鉴权）：尾随 outbox 的只读投影，按成员关系过滤（非 admin 只收自己产品的事件，admin 全量；成员关系快照每分钟刷新）。断线由前端指数退避重连，**轮询兜底保留**——丢事件只意味着少一次提前刷新。
  - 服务端：`internal/events`（进程内 hub）+ `internal/server` 的 outbox tailer（`CHALKLINE_EVENTS_INTERVAL`，默认 3s）；nginx `location /api/v1/events` 关缓冲、读超时 1h。
  - 前端：`Layout` 订阅事件流，按 `subjectType` 失效对应 TanStack Query 缓存。
- **规模化视图**：产品列表搜索/状态过滤/分页 + 概览统计条（`/overview`）；产品详情提供归档入口（归档守卫见 catalog.md M3）。
- **审计页**（admin）：`/audit` 全局检索（操作者/动作/主体/产品/时间范围 + 分页），载荷 JSON 展示；产品内审计走 `GET /products/{id}/audit-logs`（REST，成员可用）。
- **Webhook 订阅页**（admin）：`/webhooks` 订阅 CRUD、启停开关、最近投递结果展示。
- **审批队列**：条目展示等待时长，超期（评审 SLA）标红。

### 管理能力（M3 补全）

- **产品成员**（产品详情页）：添加成员用下拉选择（候选来自 `GET /products/{id}/member-candidates`，仅列出启用且尚未加入的成员），不再要求手填 `usr_` id；角色可在成员表中直接改（复用 upsert 接口）。非产品 `owner` / `admin` 时入口禁用，避免无效请求。
- **用户管理**（admin，`/users`）：成员列表、新建（用户名不可改）、改全局角色/启禁用、重置密码；权限判定在后端（非 admin 403）。- **工作流模板**（`/workflows`）：列表（按 scope 过滤）、YAML 新建/更新（服务端 DAG 与门禁校验，更新自动升版本）、导入（冲突换 id/升版本）、导出下载、归档。
- **Provider 配置**（admin，`/integrations`）：`enabled` 与 `baseURL` 可视化维护并持久化（`integration_provider_configs`），保存后 `integration.Registry` 运行时生效；**凭据仍只读环境变量**，页面仅展示「是否已配置」，不显示/编辑 token。
