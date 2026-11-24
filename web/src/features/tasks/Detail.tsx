// 任务详情：基本信息、节点状态与操作（start/submit/review/attach）、材料、
// 审批时间线、子任务树 + 拆解表单。所有写操作以响应为准，失败即提示。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  App,
  Button,
  Card,
  Descriptions,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Tag,
  Timeline,
  Typography,
} from 'antd'
import { useState } from 'react'
import { useNavigate, useParams } from 'react-router'
import { apiDelete, apiGet, apiPatch, apiPost } from '../../shared/api'
import {
  ACTIONABLE_STATUSES,
  fmtTime,
  memberNameMap,
  memberNameOf,
  nextActionStatusMeta,
  nodeStatusMeta,
  priorityMeta,
  taskKindMeta,
  taskStatusMeta,
  TASK_KINDS,
  PRIORITIES,
  userLabel,
} from '../../shared/status'
import { BlockerList, DetailError, PageLoading } from '../../shared/ui'
import type {
  Blocker,
  CreateTaskInput,
  ListEnvelope,
  MemberView,
  NextActionReport,
  NodeOpResult,
  TaskDetail,
  TaskView,
} from '../../shared/types'
import { ArtifactBrowser, ArtifactSubmitForm } from '../gates/Artifacts'
import { ReviewActions } from '../gates/ReviewAction'
import TaskTree from './components/TaskTree'

export default function TaskDetailPage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { message } = App.useApp()

  const taskQuery = useQuery({
    queryKey: ['task', id],
    queryFn: () => apiGet<TaskDetail>(`/tasks/${id}`),
  })
  const nextQuery = useQuery({
    queryKey: ['next-action', 'task', id],
    queryFn: () => apiGet<NextActionReport>(`/tasks/${id}/next-action`),
  })

  const productId = taskQuery.data?.productId ?? ''
  const membersQuery = useQuery({
    queryKey: ['members', productId],
    queryFn: () => apiGet<ListEnvelope<MemberView>>(`/products/${productId}/members`),
    enabled: productId !== '',
  })
  const memberNames = memberNameMap(membersQuery.data?.items ?? [])
  const memberOptions = (membersQuery.data?.items ?? []).map((m) => ({
    value: m.userId,
    label: userLabel(m),
  }))

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['task', id] })
    queryClient.invalidateQueries({ queryKey: ['task-workflow', id] })
    queryClient.invalidateQueries({ queryKey: ['next-action', 'task', id] })
    queryClient.invalidateQueries({ queryKey: ['reviews'] })
    queryClient.invalidateQueries({ queryKey: ['tasks'] })
    const itId = taskQuery.data?.iterationId
    if (itId) queryClient.invalidateQueries({ queryKey: ['iteration', itId] })
  }

  const startMut = useMutation({
    mutationFn: (nodeId: string) =>
      apiPost<NodeOpResult>(`/tasks/${id}/steps/start`, { nodeId, expectedVersion: 0 }),
    onSuccess: (res) => {
      message.success(`节点「${res.node.name}」已开始`)
      invalidate()
    },
    onError: (err: Error) => message.error(err.message),
  })

  // ---- 拆解 ----
  const [decomposeOpen, setDecomposeOpen] = useState(false)
  const [decomposeForm] = Form.useForm<{ children: CreateTaskInput[] }>()
  const decomposeMut = useMutation({
    mutationFn: (v: { children: CreateTaskInput[] }) =>
      apiPost<ListEnvelope<TaskView>>(`/tasks/${id}/children`, {
        children: v.children.map((c) => ({
          title: c.title,
          description: c.description ?? '',
          kind: c.kind ?? 'dev',
          priority: c.priority ?? 'normal',
          assigneeId: c.assigneeId ?? '',
        })),
      }),
    onSuccess: (res) => {
      message.success(`已拆解出 ${res.items.length} 个子任务`)
      setDecomposeOpen(false)
      decomposeForm.resetFields()
      invalidate()
    },
    onError: (err: Error) => message.error(err.message),
  })

  // ---- 编辑 / 删除 ----
  const [editOpen, setEditOpen] = useState(false)
  const [editForm] = Form.useForm<{
    title: string
    description: string
    priority: string
    assigneeId?: string
  }>()
  const updateMut = useMutation({
    mutationFn: (v: { title: string; description: string; priority: string; assigneeId?: string }) =>
      apiPatch<TaskView>(`/tasks/${id}`, {
        title: v.title,
        description: v.description,
        priority: v.priority,
        assigneeId: v.assigneeId ?? '',
      }),
    onSuccess: () => {
      message.success('任务已更新')
      setEditOpen(false)
      invalidate()
    },
    onError: (err: Error) => message.error(err.message),
  })
  const deleteMut = useMutation({
    mutationFn: () => apiDelete(`/tasks/${id}`),
    onSuccess: () => {
      message.success('任务已删除')
      const itId = taskQuery.data?.iterationId
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
      if (itId) queryClient.invalidateQueries({ queryKey: ['iteration', itId] })
      navigate(itId ? `/iterations/${itId}` : '/')
    },
    onError: (err: Error) => message.error(err.message),
  })

  if (taskQuery.isError) return <DetailError error={taskQuery.error} />
  if (taskQuery.isPending) return <PageLoading />
  const t = taskQuery.data

  const nodes = t.workflow?.nodes ?? []
  const blockersOf = (nodeId: string): Blocker[] | undefined =>
    nextQuery.data?.nextActions.find((a) => a.nodeId === nodeId)?.blockers
  const approvals = t.workflow?.approvals
  const assignee = t.assigneeId ? memberNameOf(memberNames, t.assigneeId) : '未指派'

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <Card
        extra={
          <Space>
            <Button
              size="small"
              onClick={() => {
                editForm.setFieldsValue({
                  title: t.title,
                  description: t.description,
                  priority: t.priority,
                  assigneeId: t.assigneeId || undefined,
                })
                setEditOpen(true)
              }}
            >
              编辑
            </Button>
            <Popconfirm
              title="确认删除该任务？"
              description="仅未终结且无子任务的任务可删除"
              onConfirm={() => deleteMut.mutate()}
            >
              <Button size="small" danger loading={deleteMut.isPending}>
                删除
              </Button>
            </Popconfirm>
          </Space>
        }
      >
        <Space direction="vertical" size={6} style={{ width: '100%' }}>
          <Space wrap>
            <Typography.Title level={4} style={{ margin: 0 }}>
              {t.number} {t.title}
            </Typography.Title>
            <Tag color={taskStatusMeta[t.status]?.color}>
              {taskStatusMeta[t.status]?.label ?? t.status}
            </Tag>
            {nextQuery.data && (
              <Tag color={nextActionStatusMeta[nextQuery.data.status]?.color}>
                下一步：{nextActionStatusMeta[nextQuery.data.status]?.label ?? nextQuery.data.status}
              </Tag>
            )}
          </Space>
          <Descriptions
            size="small"
            column={3}
            items={[
              {
                key: 'kind',
                label: '类型',
                children: taskKindMeta[t.kind]?.label ?? t.kind,
              },
              {
                key: 'priority',
                label: '优先级',
                children: priorityMeta[t.priority]?.label ?? t.priority,
              },
              { key: 'assignee', label: '负责人', children: assignee },
              {
                key: 'iteration',
                label: '所属迭代',
                children: (
                  <Typography.Link onClick={() => navigate(`/iterations/${t.iterationId}`)}>
                    打开看板
                  </Typography.Link>
                ),
              },
              {
                key: 'workflow',
                label: '任务流模板',
                children: t.workflowId,
              },
              {
                key: 'modules',
                label: '关联模块',
                children: t.modules.length
                  ? t.modules.map((m) => (
                      <Tag key={m.id} style={{ marginInlineEnd: 4 }}>
                        {m.name}
                      </Tag>
                    ))
                  : '—',
              },
            ]}
          />
          {t.description && (
            <Typography.Paragraph style={{ marginBottom: 0, whiteSpace: 'pre-wrap' }}>
              {t.description}
            </Typography.Paragraph>
          )}
        </Space>
      </Card>

      <Card title="节点状态与操作">
        {nodes.length === 0 ? (
          <Typography.Text type="secondary">未加载到任务流实例。</Typography.Text>
        ) : (
          nodes.map((n) => {
            const meta = nodeStatusMeta[n.status] ?? { label: n.status, color: 'default' }
            const active = ACTIONABLE_STATUSES.has(n.status)
            return (
              <div
                key={n.id}
                style={{
                  borderTop: n === nodes[0] ? undefined : '1px dashed #f0f0f0',
                  paddingTop: 12,
                  marginTop: 12,
                }}
              >
                <Space wrap>
                  <Typography.Text strong={active}>{n.name}</Typography.Text>
                  <Tag color={meta.color}>{meta.label}</Tag>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {n.enteredAt ? `进入 ${fmtTime(n.enteredAt)}` : ''}
                    {n.completedAt ? ` · 完成 ${fmtTime(n.completedAt)}` : ''}
                  </Typography.Text>
                </Space>
                {active && <BlockerList blockers={blockersOf(n.id)} />}
                {n.status === 'ready' && (
                  <div style={{ marginTop: 8 }}>
                    <Button
                      type="primary"
                      size="small"
                      loading={startMut.isPending}
                      onClick={() => startMut.mutate(n.id)}
                    >
                      开始节点
                    </Button>
                  </div>
                )}
                {(n.status === 'in_progress' || n.status === 'rejected') && (
                  <div style={{ marginTop: 8 }}>
                    <ArtifactSubmitForm
                      scope="task"
                      workItemId={id}
                      nodeId={n.id}
                      nodeTitle={n.name}
                      mode="submit"
                      onDone={invalidate}
                    />
                  </div>
                )}
                {n.status === 'awaiting_review' && (
                  <div style={{ marginTop: 8 }}>
                    <Space direction="vertical" size={12} style={{ width: '100%' }}>
                      <ReviewActions scope="task" workItemId={id} nodeId={n.id} onDone={invalidate} />
                      <ArtifactSubmitForm
                        scope="task"
                        workItemId={id}
                        nodeId={n.id}
                        nodeTitle={n.name}
                        mode="attach"
                        onDone={invalidate}
                      />
                    </Space>
                  </div>
                )}
              </div>
            )
          })
        )}
      </Card>

      <Card title="成果物（按步骤组织）">
        <ArtifactBrowser
          artifacts={t.workflow?.artifacts}
          nodes={t.workflow?.nodes}
          memberNames={memberNames}
        />
      </Card>

      <Card title="审批时间线">
        {approvals && approvals.length > 0 ? (
          <Timeline
            items={approvals.map((a, i) => ({
              key: `${a.reviewerId}-${i}`,
              color: a.decision === 'approve' ? 'green' : 'red',
              children: (
                <div>
                  <Space>
                    <Typography.Text strong>
                      {memberNameOf(memberNames, a.reviewerId)}
                    </Typography.Text>
                    <Tag color={a.decision === 'approve' ? 'green' : 'red'}>
                      {a.decision === 'approve' ? '通过' : '驳回'}
                    </Tag>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {fmtTime(a.createdAt)}
                    </Typography.Text>
                  </Space>
                  {a.comment && <div>{a.comment}</div>}
                </div>
              ),
            }))}
          />
        ) : (
          <Typography.Text type="secondary">暂无审批记录。</Typography.Text>
        )}
      </Card>

      <Card
        title="子任务"
        extra={
          <Button size="small" type="primary" onClick={() => setDecomposeOpen(true)}>
            拆解子任务
          </Button>
        }
      >
        <TaskTree key={taskQuery.dataUpdatedAt} roots={t.children} memberNames={memberNames} />
      </Card>

      <Modal
        title="拆解子任务"
        open={decomposeOpen}
        width={640}
        onCancel={() => setDecomposeOpen(false)}
        confirmLoading={decomposeMut.isPending}
        onOk={() => decomposeForm.submit()}
        destroyOnHidden
      >
        <Form
          form={decomposeForm}
          layout="vertical"
          onFinish={(v: { children: CreateTaskInput[] }) => decomposeMut.mutate(v)}
          style={{ marginTop: 16 }}
        >
          <Form.List name="children" initialValue={[{}]}>
            {(fields, { add, remove }) => (
              <>
                {fields.map((field) => (
                  <Card
                    key={field.key}
                    size="small"
                    title={`子任务 ${field.name + 1}`}
                    extra={
                      fields.length > 1 ? (
                        <Button size="small" type="text" danger onClick={() => remove(field.name)}>
                          移除
                        </Button>
                      ) : undefined
                    }
                    style={{ marginBottom: 8 }}
                  >
                    <Form.Item
                      name={[field.name, 'title']}
                      label="标题"
                      rules={[{ required: true, message: '请输入标题' }]}
                    >
                      <Input />
                    </Form.Item>
                    <Form.Item name={[field.name, 'description']} label="描述（可选）">
                      <Input.TextArea rows={2} />
                    </Form.Item>
                    <Space size={12}>
                      <Form.Item name={[field.name, 'kind']} label="类型" initialValue="dev">
                        <Select
                          style={{ width: 120 }}
                          options={TASK_KINDS.map((k) => ({
                            value: k,
                            label: taskKindMeta[k]?.label ?? k,
                          }))}
                        />
                      </Form.Item>
                      <Form.Item name={[field.name, 'priority']} label="优先级" initialValue="normal">
                        <Select
                          style={{ width: 120 }}
                          options={PRIORITIES.map((p) => ({
                            value: p,
                            label: priorityMeta[p]?.label ?? p,
                          }))}
                        />
                      </Form.Item>
                      <Form.Item name={[field.name, 'assigneeId']} label="负责人（可选）">
                        <Select
                          style={{ width: 160 }}
                          allowClear
                          showSearch
                          optionFilterProp="label"
                          placeholder="待认领"
                          options={memberOptions}
                        />
                      </Form.Item>
                    </Space>
                  </Card>
                ))}
                <Button type="dashed" onClick={() => add({})} block>
                  + 添加子任务
                </Button>
              </>
            )}
          </Form.List>
        </Form>
      </Modal>

      <Modal
        title="编辑任务"
        open={editOpen}
        onCancel={() => setEditOpen(false)}
        confirmLoading={updateMut.isPending}
        onOk={() => editForm.submit()}
        destroyOnHidden
      >
        <Form
          form={editForm}
          layout="vertical"
          onFinish={(v: { title: string; description: string; priority: string; assigneeId?: string }) =>
            updateMut.mutate(v)
          }
          style={{ marginTop: 16 }}
        >
          <Form.Item name="title" label="标题" rules={[{ required: true, message: '请输入标题' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={3} />
          </Form.Item>
          <Form.Item name="priority" label="优先级">
            <Select
              style={{ width: 160 }}
              options={PRIORITIES.map((p) => ({ value: p, label: priorityMeta[p]?.label ?? p }))}
            />
          </Form.Item>
          <Form.Item name="assigneeId" label="负责人">
            <Select
              style={{ width: 220 }}
              allowClear
              showSearch
              optionFilterProp="label"
              placeholder="待认领"
              options={memberOptions}
            />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
