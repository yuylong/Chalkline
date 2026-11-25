// 后端 DTO 的 TypeScript 形状，逐字段对齐 internal/app 各 view 结构
// （Go 结构体 JSON tag 即响应字段名，camelCase）。禁止 any：所有网络响应
// 经这些结构断言（shared/api.ts）。

// ---- identity ----

export interface UserView {
  id: string
  username: string
  displayName: string
  email: string
  roles: string[]
  status: string
}

export interface LoginResult {
  token: string
  user: UserView
  expiry: string // ISO 8601 UTC
}

export interface ProductLite {
  id: string
  key: string
  name: string
  status: string
}

export interface WhoamiView {
  user: UserView
  products: ProductLite[]
  via: string
}

/** MCP 接入：API Key（PAT）视图（不含 hash 与明文）。 */
export interface PATView {
  id: string
  name: string
  expiresAt: string // ISO 8601 UTC
  revoked: boolean
  createdAt: string // ISO 8601 UTC
}

/** 签发产物：明文 Key 只此一次返回，之后不可再查。 */
export interface PATCreated extends PATView {
  token: string // chkpat_…
}

/** 管理台：建成员入参（admin）。 */
export interface CreateUserInput {
  username: string
  displayName: string
  email: string
  password: string
  roles: string[]
}

/** 管理台：改显示名 / 邮箱 / 角色 / 启禁用（admin）。 */
export interface UpdateUserInput {
  displayName: string
  email: string
  roles: string[]
  status: string // active | disabled
}

// ---- catalog ----

export interface ProductView {
  id: string
  key: string
  name: string
  status: string
  description: string
  defaultIterationWorkflowId: string
  defaultTaskWorkflowId: string
  createdBy: string
  createdAt: string
}

/** M3：产品列表分页结果（搜索 + 状态过滤）。 */
export interface ProductListResult {
  items: ProductView[]
  total: number
}

/** M3：跨产品概览（按可见产品集合统计）。 */
export interface OverviewView {
  products: number
  activeIterations: number
  openTasks: number
  pendingGates: number
  overdueGates: number
}

export interface MemberView {
  userId: string
  username: string
  displayName: string
  role: string
  createdAt: string
}

export interface ModuleView {
  id: string
  productId: string
  parentId?: string
  name: string
  path: string
  sortOrder: number
  children?: ModuleView[]
}

export interface DocumentView {
  ownerType: string
  ownerId: string
  content: string
  version: number
}

export interface ProductDetail extends ProductView {
  members: MemberView[]
  modules: ModuleView[]
  document?: DocumentView
}

export interface ListEnvelope<T> {
  items: T[]
}

// ---- planning ----

export interface IterationView {
  id: string
  productId: string
  name: string
  goal: string
  number: string
  startsOn: string // YYYY-MM-DD
  endsOn: string // YYYY-MM-DD
  status: string
  workflowId: string
  createdBy: string
}

export interface NodeStateView {
  id: string
  name: string
  status: string
  version: number
  enteredAt?: string
  completedAt?: string
}

/** 迭代级 provider 绑定（成果物 / 流程记录 / 代码库）。 */
export interface IterationProviderView {
  category: string // artifact | process | code
  categoryLabel: string
  provider: string // inline | outline | git | svn | redmine
  baseUrl?: string
  target?: string
  dirTemplate?: string
  branch?: string
  /** 可点击跳转的外部地址；inline 为空。 */
  linkUrl?: string
}

export interface IterationProviderInput {
  category: string
  provider: string
  baseUrl?: string
  target?: string
  dirTemplate?: string
  branch?: string
}

export interface IterationProvidersView {
  iterationId: string
  items: IterationProviderView[]
  /** 已提交成果物或迭代终结：绑定冻结。 */
  locked: boolean
  editable: boolean
  lockedReason?: string
}

export interface IterationDetail extends IterationView {
  currentNodes: NodeStateView[]
  progressPct: number
  openTasks: number
  workflowName: string
  providers: IterationProvidersView
}

/** 个人「当前迭代」：登录后首页直接展示该迭代看板；未设置为 null。 */
export interface CurrentIterationView {
  iteration: IterationDetail | null
}

export interface TaskView {
  id: string
  productId: string
  iterationId: string
  parentId?: string
  number: string
  title: string
  description: string
  kind: string
  assigneeId?: string
  priority: string
  status: string // open | done | cancelled（终态投影）
  workflowId: string
}

export interface TaskDetail extends TaskView {
  children: TaskView[]
  modules: ModuleView[]
  workflow?: InstanceView
}

// ---- workflow ----

export interface WorkflowView {
  id: string
  scope: string
  name: string
  version: number
  status: string
  definition?: string // JSON 文本，仅详情返回
}

/** 模板导出产物（YAML 与创建/更新入参同构，可直接再导入）。 */
export interface WorkflowExport {
  id: string
  scope: string
  name: string
  version: number
  yaml: string
}

/** 材料引用（ArtifactRef）。M2 起随实例视图返回（InstanceView.artifacts）。 */
export interface ArtifactRef {
  kind: string
  provider: string
  url?: string
  externalId?: string
  title?: string
  content?: string
  nodeId: string
  submittedBy: string
  /** M3：提交时点固化的快照（外部 title/version 或 inline sha256）。 */
  snapshot?: Record<string, unknown>
  /** 提交时间（RFC3339）。控制台据此判定「最新成果物」与历史倒序。 */
  createdAt?: string
}

/** 审批记录（ReviewDecision 投影）。M2 起随实例视图返回（InstanceView.approvals）。 */
export interface ApprovalRecord {
  nodeId: string
  reviewerId: string
  decision: 'approve' | 'reject'
  comment: string
  roles?: string[]
  createdAt: string
}

export interface InstanceView {
  workflowId: string
  scope: string
  nodes: NodeStateView[]
  terminal?: string // done | cancelled
  progressPct: number
  /** 材料投影（M2 起返回）。 */
  artifacts?: ArtifactRef[]
  /** 审批时间线投影（M2 起返回）。 */
  approvals?: ApprovalRecord[]
}

export interface GateResultView {
  passed: boolean
  approvals: number
  approvalsNeeded: number
  pendingRejects?: string[]
  missingArtifacts?: string[]
  childrenBlocked?: boolean
  iterationBlocked?: boolean
  eligibleReviewers?: string[]
}

export interface NodeOpResult {
  node: NodeStateView
  gate?: GateResultView
  instance: InstanceView
}

export interface PendingReviewView {
  workItemType: string // task | iteration
  workItemId: string
  title: string
  nodeId: string
  nodeName: string
  approvalsNeeded: number
  currentApprovals: number
  roles: string[]
  /** M3：进入评审时间与是否超过评审 SLA。 */
  awaitingSince?: string
  overdue?: boolean
}

// ---- M3：审计留痕检索 ----

export interface AuditLogView {
  id: number
  actorId: string
  actorName: string
  action: string
  subjectType: string
  subjectId: string
  payload: Record<string, unknown>
  createdAt: string
}

export interface AuditListResult {
  items: AuditLogView[]
  total: number
}

// ---- M3：Webhook 订阅管理 ----

export interface WebhookView {
  id: string
  name: string
  url: string
  eventTypes: string[] // 空 = 订阅全部事件
  status: string // active | paused
  createdBy: string
  createdAt: string
  lastDeliveryAt?: string
  lastDeliveryOk?: boolean
  lastError?: string
}

// ---- M3：集成 provider 配置（admin；凭据只读环境变量）----

export interface ProviderConfigView {
  kind: string
  label: string
  enabled: boolean
  baseUrl: string // 生效值（库内覆盖优先）
  envBaseUrl: string // 环境变量默认值（只读提示）
  tokenRequired: boolean
  tokenConfigured: boolean
  updatedBy?: string
  updatedAt?: string
}

export interface ProviderConfigUpdate {
  enabled?: boolean
  baseUrl?: string
}

/** 全局连接默认值（成员可读）：供迭代 provider 绑定预填 baseURL，不含凭据。 */
export interface ProviderOptionView {
  kind: string
  label: string
  baseUrl: string
  enabled: boolean
}

// ---- 邮箱配置（admin；密码加密落库，永不回显）----

export interface SMTPConfigView {
  enabled: boolean
  host: string
  port: number
  username: string
  from: string
  /** TLS 模式：none（明文）/ starttls / tls（隐式 TLS）。 */
  tlsMode: string
  /** 跳过 TLS 证书校验（仅内网自签场景）。 */
  tlsSkipVerify: boolean
  /** 库内密文或环境变量任一已配置（视图永不携带密码明文/密文）。 */
  passwordConfigured: boolean
  envHost: string
  envPort: number
  envFrom: string
  envTlsMode: string
  envTlsSkipVerify: boolean
  /** 兜底收件人（CHALKLINE_NOTIFY_EMAILS，只读提示）：仅在解析不出相关成员时使用。 */
  envRecipients: string[]
  updatedBy?: string
  updatedAt?: string
}

export interface SMTPConfigUpdate {
  enabled?: boolean
  host?: string
  port?: number
  username?: string
  from?: string
  /** none / starttls / tls；空串 = 清除库内设置（回退环境变量）。 */
  tlsMode?: string
  /** 跳过 TLS 证书校验（仅内网自签场景）。 */
  tlsSkipVerify?: boolean
  /** 留空 = 不修改；提交空串 = 清除已存密码（回退环境变量）。 */
  password?: string
}

// ---- M3：SSE 事件流 ----

/** /api/v1/events 推送的事件（outbox 投影 + 产品归属）。 */
export interface StreamEvent {
  id: number
  eventType: string
  subjectType: string
  subjectId: string
  productId?: string
  payload?: Record<string, unknown>
}

// ---- next action（/tasks/{id}/next-action、/iterations/{id}/next-action）----

export interface Blocker {
  type: string // artifact_missing | approval_missing | pending_reject | children_not_done | iteration_tasks_not_done
  kind?: string
  required?: number
  current?: number
  roles?: string[]
  reviewer_id?: string
  message: string
}

export interface ActionView {
  nodeId: string
  tool: string
  blockers: Blocker[]
}

export interface NextActionReport {
  workItem: { type: string; id: string; title: string }
  status: string // ready | in_progress | awaiting_review | blocked | done
  currentNodes: { id: string; name: string; status: string }[]
  nextActions: ActionView[]
  allowedActions: string[]
}

// ---- 请求体 ----

export interface ArtifactInput {
  kind: string
  provider: string
  url?: string
  externalId?: string
  title?: string
  content?: string
}

export interface CreateTaskInput {
  title: string
  description?: string
  kind?: string
  priority?: string
  assigneeId?: string
  moduleIds?: string[]
  workflowId?: string
  parentTaskId?: string
}
