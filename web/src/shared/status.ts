// 展示元数据：状态/类型 → 中文标签与 antd 颜色。不做权限判定，仅渲染。
import dayjs from 'dayjs'

export const nodeStatusMeta: Record<string, { label: string; color: string }> = {
  pending: { label: '未开始', color: 'default' },
  ready: { label: '可开始', color: 'gold' },
  in_progress: { label: '进行中', color: 'processing' },
  awaiting_review: { label: '待评审', color: 'warning' },
  passed: { label: '已通过', color: 'success' },
  rejected: { label: '已驳回', color: 'error' },
}

/** 「当前节点」判定（workitem.NodeStatus.Actionable 的前端镜像）。 */
export const ACTIONABLE_STATUSES = new Set(['ready', 'in_progress', 'awaiting_review', 'rejected'])

export const taskStatusMeta: Record<string, { label: string; color: string }> = {
  open: { label: '未终结', color: 'processing' },
  done: { label: '已完成', color: 'success' },
  cancelled: { label: '已取消', color: 'default' },
}

export const iterationStatusMeta: Record<string, { label: string; color: string }> = {
  planned: { label: '已规划', color: 'default' },
  active: { label: '进行中', color: 'processing' },
  closed: { label: '已收尾', color: 'success' },
  cancelled: { label: '已取消', color: 'default' },
}

export const priorityMeta: Record<string, { label: string; color: string }> = {
  low: { label: '低', color: 'default' },
  normal: { label: '普通', color: 'blue' },
  high: { label: '高', color: 'orange' },
  urgent: { label: '紧急', color: 'red' },
}

export const taskKindMeta: Record<string, { label: string; color: string }> = {
  research: { label: '调研', color: 'purple' },
  design: { label: '设计', color: 'geekblue' },
  dev: { label: '开发', color: 'blue' },
  test: { label: '测试', color: 'cyan' },
  ops: { label: '运维', color: 'gold' },
  summary: { label: '总结', color: 'green' },
  bug: { label: '缺陷', color: 'red' },
}

export const memberRoleMeta: Record<string, { label: string; color: string }> = {
  owner: { label: '负责人', color: 'purple' },
  member: { label: '成员', color: 'blue' },
  guest: { label: '访客', color: 'default' },
}

export const nextActionStatusMeta: Record<string, { label: string; color: string }> = {
  ready: { label: '可推进', color: 'gold' },
  in_progress: { label: '进行中', color: 'processing' },
  awaiting_review: { label: '待评审', color: 'warning' },
  blocked: { label: '受阻', color: 'error' },
  done: { label: '已完成', color: 'success' },
}

/** RFC3339（UTC 存储）→ 本地时区展示（tech.md 约束 7）。 */
export function fmtTime(iso?: string): string {
  if (!iso) return '—'
  const d = dayjs(iso)
  return d.isValid() ? d.format('YYYY-MM-DD HH:mm') : iso
}

export function fmtDate(iso?: string): string {
  if (!iso) return '—'
  const d = dayjs(iso)
  return d.isValid() ? d.format('YYYY-MM-DD') : iso
}

/** 用户展示名：显示名（用户名）；无显示名时仅用户名。全站用户位置统一用此格式。 */
export function userLabel(u: { displayName?: string; username: string }): string {
  return u.displayName ? `${u.displayName}（${u.username}）` : u.username
}

/** userId → 展示名映射（userLabel 格式）。 */
export function memberNameMap(
  members: { userId: string; displayName?: string; username: string }[],
): Record<string, string> {
  const m: Record<string, string> = {}
  for (const mem of members) m[mem.userId] = userLabel(mem)
  return m
}

/** 按映射取用户展示名；查不到（成员已移除等）给「未知用户」，不露出内部 ID。 */
export function memberNameOf(names: Record<string, string>, userId?: string): string {
  return userId ? (names[userId] ?? '未知用户') : '—'
}

/** 材料类型（workitem.ArtifactKind）。 */
export const ARTIFACT_KINDS: { value: string; label: string }[] = [
  { value: 'research_report', label: '调研报告 research_report' },
  { value: 'tech_spike', label: '技术验证 tech_spike' },
  { value: 'spec', label: '需求规格 spec' },
  { value: 'design', label: '技术设计 design' },
  { value: 'test_plan', label: '测试设计 test_plan' },
  { value: 'test_cases', label: '用例集 test_cases' },
  { value: 'test_execution', label: '测试执行 test_execution' },
  { value: 'test_report', label: '测试报告 test_report' },
  { value: 'pr', label: '代码改动 pr' },
  { value: 'summary', label: '总结 summary' },
  { value: 'pre_review', label: 'AI 自查报告 pre_review' },
  { value: 'file', label: '附件 file' },
]

/** 材料来源（workitem.ArtifactProvider）。 */
export const ARTIFACT_PROVIDERS: { value: string; label: string }[] = [
  { value: 'git', label: 'git' },
  { value: 'svn', label: 'svn' },
  { value: 'outline', label: 'outline' },
  { value: 'redmine', label: 'redmine' },
  { value: 'file', label: 'file' },
  { value: 'url', label: 'url' },
  { value: 'inline', label: 'inline' },
]

export const TASK_KINDS = Object.keys(taskKindMeta)
export const PRIORITIES = ['low', 'normal', 'high', 'urgent']
export const MEMBER_ROLES = ['owner', 'member', 'guest']
