// 任务树：多级子任务展开。GET /tasks/{id} 的 children 提供直接子任务，
// 展开时懒加载下一层（后端 listTasks 暂无 parentId 过滤参数，逐节点取详情）。
import { Tag, Tree, Typography } from 'antd'
import type { DataNode } from 'antd/es/tree'
import { useState } from 'react'
import type { Key } from 'react'
import { useNavigate } from 'react-router'
import { apiGet } from '../../../shared/api'
import { memberNameOf, priorityMeta, taskKindMeta, taskStatusMeta } from '../../../shared/status'
import type { TaskDetail, TaskView } from '../../../shared/types'

interface TaskTreeProps {
  /** 当前任务的直接子任务（来自任务详情）。 */
  roots: TaskView[]
  memberNames: Record<string, string>
}

export default function TaskTree({ roots, memberNames }: TaskTreeProps) {
  const navigate = useNavigate()
  const [childMap, setChildMap] = useState<Record<string, TaskView[]>>({})

  const titleOf = (t: TaskView) => (
    <span onClick={() => navigate(`/tasks/${t.id}`)} style={{ cursor: 'pointer' }}>
      <Typography.Text strong style={{ fontSize: 12, marginInlineEnd: 8 }}>
        {t.number}
      </Typography.Text>
      <span style={{ marginInlineEnd: 8 }}>{t.title}</span>
      {t.status !== 'open' && (
        <Tag color={taskStatusMeta[t.status]?.color} style={{ marginInlineEnd: 8 }}>
          {taskStatusMeta[t.status]?.label ?? t.status}
        </Tag>
      )}
      <Tag color={taskKindMeta[t.kind]?.color} style={{ marginInlineEnd: 8 }}>
        {taskKindMeta[t.kind]?.label ?? t.kind}
      </Tag>
      <Tag color={priorityMeta[t.priority]?.color} style={{ marginInlineEnd: 8 }}>
        {priorityMeta[t.priority]?.label ?? t.priority}
      </Tag>
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        {t.assigneeId ? memberNameOf(memberNames, t.assigneeId) : '未指派'}
      </Typography.Text>
    </span>
  )

  const build = (items: TaskView[]): DataNode[] =>
    items.map((t) => {
      const kids = childMap[t.id]
      return {
        key: t.id,
        title: titleOf(t),
        isLeaf: kids ? kids.length === 0 : false,
        children: kids && kids.length > 0 ? build(kids) : undefined,
      }
    })

  const onLoadData = (node: { key: Key }) =>
    apiGet<TaskDetail>(`/tasks/${String(node.key)}`)
      .then((d) => {
        setChildMap((m) => ({ ...m, [String(node.key)]: d.children ?? [] }))
      })
      .catch(() => {
        // 加载失败保持可再次展开；错误已在请求层提示或可忽略。
      })

  if (roots.length === 0) return <Typography.Text type="secondary">暂无子任务。</Typography.Text>
  // 不用 defaultExpandAll：与 loadData 懒加载组合会在挂载时对全部根节点预取。
  return <Tree treeData={build(roots)} loadData={onLoadData} blockNode />
}
