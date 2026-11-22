// 审批队列：待我确认的门禁，10 秒轮询；每条可「通过」或「驳回（必填理由）」。
// 操作后由 ReviewActions 统一失效相关缓存，队列随轮询/失效刷新。
import { useQuery } from '@tanstack/react-query'
import { Card, Progress, Table, Tag, Typography } from 'antd'
import { useNavigate } from 'react-router'
import { apiGet } from '../../shared/api'
import { fmtTime } from '../../shared/status'
import { ApiErrorBanner } from '../../shared/ui'
import type { ListEnvelope, PendingReviewView } from '../../shared/types'
import { ReviewActions } from '../gates/ReviewAction'

const REVIEW_POLL_MS = 10_000

export default function ReviewQueue() {
  const navigate = useNavigate()
  const { data, isPending, isError, error } = useQuery({
    queryKey: ['reviews', 'pending'],
    queryFn: () => apiGet<ListEnvelope<PendingReviewView>>('/reviews/pending'),
    refetchInterval: REVIEW_POLL_MS,
  })

  return (
    <Card
      title="待我审批"
      extra={<Typography.Text type="secondary">每 10 秒自动刷新</Typography.Text>}
    >
      {isError ? (
        <ApiErrorBanner error={error} />
      ) : (
        <Table<PendingReviewView>
          rowKey={(r) => `${r.workItemType}-${r.workItemId}-${r.nodeId}`}
          loading={isPending}
          dataSource={data?.items ?? []}
          pagination={false}
          locale={{ emptyText: '暂无待审批事项' }}
          columns={[
            {
              title: '类型',
              dataIndex: 'workItemType',
              width: 80,
              render: (v: string) => (
                <Tag color={v === 'task' ? 'blue' : 'purple'}>{v === 'task' ? '任务' : '迭代'}</Tag>
              ),
            },
            {
              title: '工作项',
              dataIndex: 'title',
              render: (_, r) => (
                <Typography.Link
                  onClick={() =>
                    navigate(r.workItemType === 'task' ? `/tasks/${r.workItemId}` : `/iterations/${r.workItemId}`)
                  }
                >
                  {r.title}
                </Typography.Link>
              ),
            },
            { title: '门禁节点', dataIndex: 'nodeName', width: 150 },
            {
              title: '会签进度',
              key: 'progress',
              width: 180,
              render: (_, r) => (
                <Progress
                  percent={r.approvalsNeeded > 0 ? Math.round((100 * r.currentApprovals) / r.approvalsNeeded) : 0}
                  size="small"
                  format={() => `${r.currentApprovals}/${r.approvalsNeeded}`}
                />
              ),
            },
            {
              title: '要求角色',
              dataIndex: 'roles',
              width: 150,
              render: (v: string[]) => v.join(' / '),
            },
            {
              title: '等待',
              key: 'waiting',
              width: 130,
              render: (_, r) =>
                r.overdue ? (
                  <Tag color="red">已超期</Tag>
                ) : r.awaitingSince ? (
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {fmtTime(r.awaitingSince)}
                  </Typography.Text>
                ) : (
                  '—'
                ),
            },
            {
              title: '操作',
              key: 'op',
              width: 220,
              render: (_, r) => (
                <ReviewActions
                  scope={r.workItemType === 'task' ? 'task' : 'iteration'}
                  workItemId={r.workItemId}
                  nodeId={r.nodeId}
                />
              ),
            },
          ]}
        />
      )}
    </Card>
  )
}
