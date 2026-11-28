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
  /** 认证来源：local（仅本地密码）/ ldap（仅 LDAP）/ both（双认证，任一密码通过即可）。 */
  authSource: string
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

/** 管理台：建成员入参（admin）。authSource 为 ldap 时可不设初始密码。 */
export interface CreateUserInput {
  username: string
  displayName: string
  email: string
  password: string
  roles: string[]
  authSource: string // local | ldap | both
}

/** 管理台：改显示名 / 邮箱 / 角色 / 启禁用 / 认证来源（admin）。 */
export interface UpdateUserInput {
  displayName: string
  email: string
  roles: string[]
  status: string // active | disabled
  authSource: string // local | ldap | both
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
  /** 按任务类型的默认任务流模板（kind → workflowId）；未覆盖类型回退 defaultTaskWorkflowId。 */
  taskWorkflowDefaults: Record<string, string>
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
  /** 功能简介。 */
  description: string
  /** 技术栈标签（多选）。 */
  techStack: string[]
  /** 主要责任人 userId；空即未指定。 */
  ownerId: string
  /** 开发者 userId 集合。 */
  developers: string[]
  /** 测试者 userId 集合。 */
  testers: string[]
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
  id: string
  category: string // artifact | process | code
  categoryLabel: string
  provider: string // inline | outline | git | svn | redmine
  baseUrl?: string
  target?: string
  dirTemplate?: string
  branch?: string
  /** 可点击跳转的外部地址；inline 为空。 */
  linkUrl?: string
  /** 认证方式：''（未配置）/ token / basic。 */
  authType?: string
  authUsername?: string
  /** 迭代级已保存凭据（密文，永不回显）。 */
  secretConfigured: boolean
  /** 服务端环境变量已配置凭据。 */
  envSecretConfigured: boolean
  /** 当前查看人已为该 provider + baseUrl 配置个人凭据；false 时出站回退公共凭据。 */
  personalSecretConfigured: boolean
}

export interface IterationProviderInput {
  category: string
  provider: string
  baseUrl?: string
  target?: string
  dirTemplate?: string
  branch?: string
  authType?: string
  authUsername?: string
  /** 只写不读：留空 = 保持库内已有凭据。 */
  authSecret?: string
}

/** 凭据轮换入参（只写不读）。 */
export interface ProviderCredentialUpdate {
  authType: string
  authUsername?: string
  /** 留空 = 保持库内已有凭据。 */
  authSecret?: string
}

export interface IterationProvidersView {
  iterationId: string
  items: IterationProviderView[]
  /** 已提交成果物或迭代终结：绑定冻结。 */
  locked: boolean
  editable: boolean
  lockedReason?: string
  /** 查看人可维护本迭代公共 Key（产品成员 + pm，或 admin）。 */
  canManageCredential: boolean
  /** 查看人可修改绑定结构（未冻结时与 canManageCredential 同权限）。 */
  canManageBinding: boolean
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
  /** 被替换下来的历史轮次材料（按替换时间倒序）；节点详情页「历史轮次」用。 */
  artifactHistory?: ArtifactVersionView[]
}

/** 历史材料版本：与 ArtifactRef 同形，另带替换时间与替换人。 */
export interface ArtifactVersionView extends ArtifactRef {
  artifactId: string
  replacedAt?: string
  replacedBy?: string
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

// ---- 全局 Provider 服务器列表（admin 增删改查；成员可读已启用项）----
// 每条记录一个服务器的类型、名称与地址；没有「默认 provider」概念，
// 任何选择处都从列表选取或手填地址。

export interface ProviderEndpointView {
  id: string
  kind: string
  kindLabel: string
  label: string
  baseUrl: string
  note?: string
  enabled: boolean
  updatedAt?: string
}

export interface ProviderEndpointCreate {
  kind: string
  label: string
  baseUrl: string
  note?: string
}

export interface ProviderEndpointUpdate {
  label?: string
  baseUrl?: string
  note?: string
  enabled?: boolean
}

// ---- 个人 provider 凭据（成员自助维护；只报是否已配置，永不回显）----

export interface UserCredentialView {
  id: string
  provider: string
  providerLabel: string
  baseUrl?: string
  authType?: string
  authUsername?: string
  /** 本人已保存 Key（密文）。 */
  secretConfigured: boolean
  updatedAt?: string
}

export interface UserCredentialSet {
  provider: string
  /** 凭据按具体服务器地址生效；必填。 */
  baseUrl?: string
  authType?: string
  authUsername?: string
  /** 只写不读：留空 = 保持库内已有密文。 */
  authSecret?: string
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

// ---- LDAP 认证配置（admin；绑定密码加密落库，永不回显）----

export interface LDAPConfigView {
  enabled: boolean
  host: string
  port: number
  /** TLS 模式：none（明文，389 常用）/ starttls / tls（隐式，636 常用）。 */
  tlsMode: string
  /** 跳过 TLS 证书校验（仅内网自签场景）。 */
  tlsSkipVerify: boolean
  /** 服务账号 DN；空 = 匿名检索。 */
  bindDn: string
  /** 库内密文已配置（视图永不携带绑定密码明文/密文）。 */
  bindPasswordConfigured: boolean
  userSearchBase: string
  /** 含 %s 占位登录名的检索过滤器，如 (uid=%s) 或 (sAMAccountName=%s)。 */
  userSearchFilter: string
  timeoutSeconds: number
  updatedBy?: string
  updatedAt?: string
}

export interface LDAPConfigUpdate {
  enabled?: boolean
  host?: string
  port?: number
  tlsMode?: string
  tlsSkipVerify?: boolean
  bindDn?: string
  /** 留空 = 不修改；提交空串 = 清除已存密码（回退匿名绑定）。 */
  bindPassword?: string
  userSearchBase?: string
  userSearchFilter?: string
  timeoutSeconds?: number
}

/** 「测试连接」结果：connectivity（拨号+绑定+试检索）| bind（完整登录验证）。 */
export interface LDAPTestResult {
  ok: boolean
  mode: string
  username: string
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
