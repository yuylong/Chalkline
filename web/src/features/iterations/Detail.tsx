// 迭代详情 + 看板（合并页）：基本信息、阶段泳道（生命周期节点，并行并列
// 展示，点击进入节点详情页）、当前节点摘要、任务看板、进度图表。
// 另可作为「当前迭代」首页复用（IterationPane 显式传 iterationId）。
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { App, Button, Card, Progress, Space, Tag, Typography } from 'antd'
import { useLocation, useNavigate, useParams } from 'react-router'
import { apiDelete, apiGet, apiPut } from '../../shared/api'
import {
  ACTIONABLE_STATUSES,
  fmtDate,
  fmtTime,
  iterationStatusMeta,
  nextActionStatusMeta,
  nodeStatusMeta,
} from '../../shared/status'
import { DetailError, PageLoading } from '../../shared/ui'
import type {
  Blocker,
  CurrentIterationView,
  InstanceView,
  IterationDetail,
  ListEnvelope,
  MemberView,
  NextActionReport,
  TaskView,
} from '../../shared/types'
import { CurrentNodeSummary } from '../gates/CurrentNodePanel'
import TaskBoard from './Board'
import IterationCharts from './Charts'

export default function IterationDetailRoute() {
  const { id = '' } = useParams()
  return <IterationPane iterationId={id} />
}

/** IterationPane 渲染单个迭代的完整看板；被路由页与「当前迭代」首页共用。 */
export function IterationPane({ iterationId }: { iterationId: string }) {
  const id = iterationId
  const navigate = useNavigate()
  const location = useLocation()
  const queryClient = useQueryClient()
  const { message } = App.useApp()

  // 个人「当前迭代」：标记当前迭代并提供设置/取消入口。
  const currentQuery = useQuery({
    queryKey: ['current-iteration'],
    queryFn: () => apiGet<CurrentIterationView>('/current-iteration'),
  })
  const isCurrent = currentQuery.data?.iteration?.id === id

  const setCurrentMut = useMutation({
    mutationFn: () => apiPut<IterationDetail>('/current-iteration', { iterationId: id }),
    onSuccess: () => {
      message.success('已设为当前迭代，登录后首页直接进入')
      queryClient.invalidateQueries({ queryKey: ['current-iteration'] })
    },
    onError: (err: Error) => message.error(err.message),
  })
  const clearCurrentMut = useMutation({
    mutationFn: () => apiDelete('/current-iteration'),
    onSuccess: () => {
      message.success('已取消当前迭代')
      queryClient.invalidateQueries({ queryKey: ['current-iteration'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const itQuery = useQuery({
    queryKey: ['iteration', id],
    queryFn: () => apiGet<IterationDetail>(`/iterations/${id}`),
  })
  const wfQuery = useQuery({
    queryKey: ['iteration-workflow', id],
    queryFn: () => apiGet<InstanceView>(`/iterations/${id}/workflow`),
  })
  const nextQuery = useQuery({
    queryKey: ['next-action', 'iteration', id],
    queryFn: () => apiGet<NextActionReport>(`/iterations/${id}/next-action`),
  })
  const tasksQuery = useQuery({
    queryKey: ['tasks', id],
    queryFn: () => apiGet<ListEnvelope<TaskView>>(`/tasks?iterationId=${encodeURIComponent(id)}`),
  })

  const productId = itQuery.data?.productId ?? ''
  const membersQuery = useQuery({
    queryKey: ['members', productId],
    queryFn: () => apiGet<ListEnvelope<MemberView>>(`/products/${productId}/members`),
    enabled: productId !== '',
  })

  const tasks = tasksQuery.data?.items ?? []
  // 逐任务取实例视图（泳道归类与燃尽时间戳共用，键与任务详情页一致）。
  const wfQueries = useQueries({
    queries: tasks.map((t) => ({
      queryKey: ['task-workflow', t.id],
      queryFn: () => apiGet<InstanceView>(`/tasks/${t.id}/workflow`),
    })),
  })
  const wfLoading = wfQueries.some((q) => q.isPending)
  const wfMap = new Map<string, InstanceView | undefined>()
  tasks.forEach((t, i) => wfMap.set(t.id, wfQueries[i].data))
  const memberNames: Record<string, string> = {}
  for (const m of membersQuery.data?.items ?? []) {
    memberNames[m.userId] = m.displayName || m.username
  }

  if (itQuery.isError) return <DetailError error={itQuery.error} />
  if (itQuery.isPending) return <PageLoading />
  const it = itQuery.data

  const stages = wfQuery.data?.nodes ?? []
  const actionable = stages.filter((n) => ACTIONABLE_STATUSES.has(n.status))
  const blockersOf = (nodeId: string): Blocker[] | undefined =>
    nextQuery.data?.nextActions.find((a) => a.nodeId === nodeId)?.blockers

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <Card
        extra={
          it.status === 'planned' || it.status === 'active' ? (
            isCurrent ? (
              <Space>
                <Tag color="orange">当前迭代</Tag>
                <Button size="small" loading={clearCurrentMut.isPending} onClick={() => clearCurrentMut.mutate()}>
                  取消当前迭代
                </Button>
              </Space>
            ) : (
              <Button size="small" type="primary" loading={setCurrentMut.isPending} onClick={() => setCurrentMut.mutate()}>
                设为当前迭代
              </Button>
            )
          ) : undefined
        }
      >
        <Space direction="vertical" size={6} style={{ width: '100%' }}>
          <Space wrap>
            <Typography.Title level={4} style={{ margin: 0 }}>
              {it.number} {it.name}
            </Typography.Title>
            <Tag color={iterationStatusMeta[it.status]?.color}>
              {iterationStatusMeta[it.status]?.label ?? it.status}
            </Tag>
            {nextQuery.data && (
              <Tag color={nextActionStatusMeta[nextQuery.data.status]?.color}>
                下一步：{nextActionStatusMeta[nextQuery.data.status]?.label ?? nextQuery.data.status}
              </Tag>
            )}
          </Space>
          <Typography.Text type="secondary">{it.goal || '（未填写目标）'}</Typography.Text>
          <Space wrap size={24}>
            <span>
              周期：{fmtDate(it.startsOn)} ~ {fmtDate(it.endsOn)}
            </span>
            <span>未终结任务：{it.openTasks}</span>
            <span>生命周期模板：{it.workflowName}</span>
            <Typography.Link onClick={() => navigate(`/products/${it.productId}`)}>
              所属产品
            </Typography.Link>
          </Space>
          <Space size={8}>
            <span>生命周期进度</span>
            <Progress percent={it.progressPct} size="small" style={{ width: 200, margin: 0 }} />
          </Space>
        </Space>
      </Card>

      <Card title="阶段泳道（生命周期节点 · 并行阶段并列展示）">
        {stages.length === 0 ? (
          <Typography.Text type="secondary">未加载到生命周期实例。</Typography.Text>
        ) : (
          <div style={{ display: 'flex', gap: 8, overflowX: 'auto' }}>
            {stages.map((n) => {
              const meta = nodeStatusMeta[n.status] ?? { label: n.status, color: 'default' }
              const active = ACTIONABLE_STATUSES.has(n.status)
              return (
                <Card
                  key={n.id}
                  size="small"
                  hoverable
                  onClick={() =>
                    navigate(`/iterations/${id}/node/${n.id}`, {
                      state: { from: location.pathname + location.search },
                    })
                  }
                  style={{
                    minWidth: 140,
                    flex: '1 1 140px',
                    cursor: 'pointer',
                    borderColor: active ? '#1677ff' : undefined,
                  }}
                >
                  <div style={{ fontWeight: active ? 600 : 400, fontSize: 13 }}>{n.name}</div>
                  <Tag color={meta.color} style={{ marginTop: 6 }}>
                    {meta.label}
                  </Tag>
                  <div style={{ fontSize: 11, color: '#999', marginTop: 4 }}>
                    {n.enteredAt && <div>进入 {fmtTime(n.enteredAt)}</div>}
                    {n.completedAt && <div>完成 {fmtTime(n.completedAt)}</div>}
                  </div>
                </Card>
              )
            })}
          </div>
        )}
      </Card>

      <Card title="当前节点摘要">
        {wfQuery.data?.terminal ? (
          <Typography.Text type="secondary">
            生命周期已达终态（{wfQuery.data.terminal === 'done' ? '已收尾' : '已取消'}），不可回退。
          </Typography.Text>
        ) : actionable.length === 0 ? (
          <Typography.Text type="secondary">当前无可推进阶段。</Typography.Text>
        ) : (
          <Space direction="vertical" size={10} style={{ width: '100%' }}>
            {actionable.map((n) => (
              <CurrentNodeSummary
                key={n.id}
                node={n}
                artifacts={wfQuery.data?.artifacts}
                approvals={wfQuery.data?.approvals}
                blockers={blockersOf(n.id)}
                onClick={() =>
                  navigate(`/iterations/${id}/node/${n.id}`, {
                    state: { from: location.pathname + location.search },
                  })
                }
              />
            ))}
          </Space>
        )}
      </Card>

      <Card title="任务看板（按各任务工作流当前节点分列）">
        <TaskBoard tasks={tasks} workflows={wfMap} memberNames={memberNames} loading={wfLoading} />
      </Card>

      <Card title="进度图表">
        <IterationCharts
          tasks={tasks}
          workflows={wfMap}
          members={membersQuery.data?.items ?? []}
          startsOn={it.startsOn}
          endsOn={it.endsOn}
        />
      </Card>
    </div>
  )
}
