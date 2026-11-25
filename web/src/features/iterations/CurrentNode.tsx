// 节点详情页（从迭代页「阶段泳道」或「当前节点摘要」点入，路由
// /iterations/:id/node/:nodeId）：承载原迭代页就地展开的全部细节——阻塞提示、
// 节点操作（开始/录入提交/评审）、最新成果物正文、节点内全部成果物列表与
// 评审意见。
// 「返回迭代页」回到来源页（首页或迭代详情页，跳转时带上 from；直接打开
// URL 时回退到迭代详情页）。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { App, Button, Card, Result, Space, Tag, Typography } from 'antd'
import { useLocation, useNavigate, useParams } from 'react-router'
import { apiGet, apiPost } from '../../shared/api'
import { fmtTime, memberNameMap, nodeStatusMeta } from '../../shared/status'
import { DetailError, PageLoading } from '../../shared/ui'
import type {
  InstanceView,
  IterationDetail,
  ListEnvelope,
  MemberView,
  NextActionReport,
  NodeOpResult,
} from '../../shared/types'
import { CurrentNodeDetail } from '../gates/CurrentNodePanel'
import { ProviderBindingSummary } from './Providers'

export default function IterationCurrentNodePage() {
  const { id = '', nodeId = '' } = useParams()
  const navigate = useNavigate()
  const location = useLocation()
  const queryClient = useQueryClient()
  const { message } = App.useApp()

  const from = (location.state as { from?: string } | null)?.from
  const goBack = () => navigate(from ?? `/iterations/${id}`)

  // 查询键与迭代页一致，共享缓存与失效。
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
  // 评审人 / 提交人展示用：产品成员名映射（查询键与迭代页一致，共享缓存）。
  const productId = itQuery.data?.productId ?? ''
  const membersQuery = useQuery({
    queryKey: ['members', productId],
    queryFn: () => apiGet<ListEnvelope<MemberView>>(`/products/${productId}/members`),
    enabled: productId !== '',
  })
  const memberNames = memberNameMap(membersQuery.data?.items ?? [])

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['iteration', id] })
    queryClient.invalidateQueries({ queryKey: ['iteration-workflow', id] })
    queryClient.invalidateQueries({ queryKey: ['next-action', 'iteration', id] })
    queryClient.invalidateQueries({ queryKey: ['reviews'] })
  }

  const startMut = useMutation({
    mutationFn: () =>
      apiPost<NodeOpResult>(`/iterations/${id}/steps/start`, { nodeId, expectedVersion: 0 }),
    onSuccess: (res) => {
      message.success(`阶段「${res.node.name}」已开始`)
      invalidate()
    },
    onError: (err: Error) => message.error(err.message),
  })

  if (itQuery.isError) return <DetailError error={itQuery.error} />
  if (wfQuery.isError) return <DetailError error={wfQuery.error} />
  if (itQuery.isPending || wfQuery.isPending) return <PageLoading />

  const node = wfQuery.data.nodes.find((n) => n.id === nodeId)
  if (!node) {
    return (
      <Result
        status="404"
        title="节点不存在或不可见"
        subTitle="该节点不在迭代生命周期实例中。"
        extra={
          <Button type="primary" onClick={goBack}>
            返回迭代页
          </Button>
        }
      />
    )
  }

  const it = itQuery.data
  const meta = nodeStatusMeta[node.status] ?? { label: node.status, color: 'default' }
  const blockers = nextQuery.data?.nextActions.find((a) => a.nodeId === nodeId)?.blockers

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <Card>
        <Space direction="vertical" size={6} style={{ width: '100%' }}>
          <Space wrap>
            <Button onClick={goBack}>← 返回迭代页</Button>
            <Typography.Title level={4} style={{ margin: 0 }}>
              {node.name}
            </Typography.Title>
            <Tag color={meta.color}>{meta.label}</Tag>
          </Space>
          <Typography.Text type="secondary">
            所属迭代：{it.number} {it.name}
            {node.enteredAt ? ` · 进入 ${fmtTime(node.enteredAt)}` : ''}
            {node.completedAt ? ` · 完成 ${fmtTime(node.completedAt)}` : ''}
          </Typography.Text>
        </Space>
      </Card>

      <Card title="阶段连接（迭代 Provider）">
        <ProviderBindingSummary providers={itQuery.data.providers} />
      </Card>

      <Card title="节点详情">
        <CurrentNodeDetail
          scope="iteration"
          workItemId={id}
          node={node}
          artifacts={wfQuery.data.artifacts}
          approvals={wfQuery.data.approvals}
          blockers={blockers}
          memberNames={memberNames}
          onStart={node.status === 'ready' ? () => startMut.mutate() : undefined}
          starting={startMut.isPending}
          onDone={invalidate}
        />
      </Card>
    </div>
  )
}
