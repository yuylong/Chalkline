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

export interface IterationDetail extends IterationView {
  currentNodes: NodeStateView[]
  progressPct: number
  openTasks: number
  workflowName: string
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
