// 迭代任务看板：按各任务工作流的「当前节点」分列泳道；不同模板的节点名
// 依首次出现顺序并列追加；done 任务归入其模板终态节点列（终态约定在模板
// 节点序最后），cancelled 单独成列。并行分支节点天然并列展示。
import { Card, Empty, Skeleton, Space, Tag, Typography } from 'antd'
import { useNavigate } from 'react-router'
import { ACTIONABLE_STATUSES, priorityMeta, taskKindMeta } from '../../shared/status'
import type { InstanceView, TaskView } from '../../shared/types'

const CANCELLED_LANE = '已取消'

interface BoardProps {
  tasks: TaskView[]
  /** taskId → 该任务的实例视图（可能未加载完成）。 */
  workflows: Map<string, InstanceView | undefined>
  memberNames: Record<string, string>
  loading: boolean
}

function laneOf(task: TaskView, wf?: InstanceView): string {
  if (task.status === 'cancelled') return CANCELLED_LANE
  if (task.status === 'done') {
    // 终态节点在模板节点序最后（docs/domain/workflow.md 内置模板均如此）。
    return wf?.nodes.length ? wf.nodes[wf.nodes.length - 1].name : '已完成'
  }
  if (!wf) return ''
  const cur = wf.nodes.find((n) => ACTIONABLE_STATUSES.has(n.status))
  return cur ? cur.name : ''
}

export default function TaskBoard({ tasks, workflows, memberNames, loading }: BoardProps) {
  const navigate = useNavigate()

  if (loading) return <Skeleton active paragraph={{ rows: 6 }} />
  if (tasks.length === 0) {
    return <Empty description="迭代下暂无任务（在允许建任务的阶段创建）" />
  }

  // 泳道顺序：首个可用模板的节点序优先，其余模板追加未见过的节点名。
  const laneTitles: string[] = []
  for (const wf of workflows.values()) {
    for (const n of wf?.nodes ?? []) {
      if (!laneTitles.includes(n.name)) laneTitles.push(n.name)
    }
  }
  if (!laneTitles.includes(CANCELLED_LANE)) laneTitles.push(CANCELLED_LANE)

  const groups = new Map<string, TaskView[]>()
  for (const t of tasks) {
    const key = laneOf(t, workflows.get(t.id))
    if (!key) continue // 实例信息未同步的任务暂不归列
    const arr = groups.get(key)
    if (arr) arr.push(t)
    else groups.set(key, [t])
  }

  return (
    <div style={{ display: 'flex', gap: 8, overflowX: 'auto', alignItems: 'flex-start' }}>
      {laneTitles.map((title) => {
        const items = groups.get(title) ?? []
        return (
          <Card
            key={title}
            size="small"
            title={
              <span style={{ fontSize: 13 }}>
                {title} <Typography.Text type="secondary">· {items.length}</Typography.Text>
              </span>
            }
            style={{ minWidth: 230, flex: '1 1 230px', background: '#fafafa' }}
          >
            {items.length === 0 ? (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                暂无任务
              </Typography.Text>
            ) : (
              <Space direction="vertical" size={8} style={{ width: '100%' }}>
                {items.map((t) => (
                  <Card
                    key={t.id}
                    size="small"
                    hoverable
                    onClick={() => navigate(`/tasks/${t.id}`)}
                    styles={{ body: { padding: '8px 12px' } }}
                  >
                    <Space size={6} wrap style={{ marginBottom: 2 }}>
                      <Typography.Text strong style={{ fontSize: 12 }}>
                        {t.number}
                      </Typography.Text>
                      <Tag
                        color={taskKindMeta[t.kind]?.color ?? 'default'}
                        style={{ marginInlineEnd: 0 }}
                      >
                        {taskKindMeta[t.kind]?.label ?? t.kind}
                      </Tag>
                      <Tag
                        color={priorityMeta[t.priority]?.color ?? 'default'}
                        style={{ marginInlineEnd: 0 }}
                      >
                        {priorityMeta[t.priority]?.label ?? t.priority}
                      </Tag>
                    </Space>
                    <div style={{ fontSize: 13 }}>{t.title}</div>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {t.assigneeId ? (memberNames[t.assigneeId] ?? t.assigneeId) : '未指派'}
                    </Typography.Text>
                  </Card>
                ))}
              </Space>
            )}
          </Card>
        )
      })}
    </div>
  )
}
