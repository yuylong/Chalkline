// 产品详情：基本信息、模块树（只读 + 新建）、成员表、迭代列表（跳看板）。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  App,
  Button,
  Card,
  DatePicker,
  Descriptions,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Table,
  Tag,
  Tree,
  TreeSelect,
  Typography,
} from 'antd'
import dayjs from 'dayjs'
import { useState } from 'react'
import { useNavigate, useParams } from 'react-router'
import { apiDelete, apiGet, apiPatch, apiPost } from '../../shared/api'
import { fmtDate, fmtTime, iterationStatusMeta, memberRoleMeta, MEMBER_ROLES } from '../../shared/status'
import { ApiErrorBanner, DetailError, PageLoading } from '../../shared/ui'
import type {
  IterationView,
  ListEnvelope,
  MemberView,
  ModuleView,
  ProductDetail,
  ProductView,
  WhoamiView,
  WorkflowView,
} from '../../shared/types'

// Tree 用 key、TreeSelect 用 value，同一形状两处复用。
type ModuleNode = { title: string; key: string; value: string; children?: ModuleNode[] }

function toTreeNodes(mods: ModuleView[]): ModuleNode[] {
  return mods.map((m) => ({
    title: m.name,
    key: m.id,
    value: m.id,
    children: m.children?.length ? toTreeNodes(m.children) : undefined,
  }))
}

export default function ProductDetail() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { message } = App.useApp()

  const [moduleOpen, setModuleOpen] = useState(false)
  const [memberOpen, setMemberOpen] = useState(false)
  const [iterationOpen, setIterationOpen] = useState(false)
  const [moduleForm] = Form.useForm<{ parentId?: string; name: string; sortOrder?: number }>()
  const [memberForm] = Form.useForm<{ userId: string; role: string }>()
  const [iterationForm] = Form.useForm<{
    name: string
    goal?: string
    range: [dayjs.Dayjs, dayjs.Dayjs]
    workflowId?: string
  }>()

  const detail = useQuery({
    queryKey: ['product', id],
    queryFn: () => apiGet<ProductDetail>(`/products/${id}`),
  })
  const iterations = useQuery({
    queryKey: ['iterations', id],
    queryFn: () => apiGet<ListEnvelope<IterationView>>(`/products/${id}/iterations`),
  })
  const { data: workflows } = useQuery({
    queryKey: ['workflows'],
    queryFn: () => apiGet<ListEnvelope<WorkflowView>>('/workflows'),
  })

  const wfName = (wfId: string) =>
    (workflows?.items ?? []).find((w) => w.id === wfId)?.name ?? wfId

  const createModuleMut = useMutation({
    mutationFn: (v: { parentId?: string; name: string; sortOrder?: number }) =>
      apiPost<ModuleView>(`/products/${id}/modules`, {
        parentId: v.parentId ?? '',
        name: v.name,
        sortOrder: v.sortOrder ?? 0,
      }),
    onSuccess: () => {
      message.success('模块已创建')
      setModuleOpen(false)
      moduleForm.resetFields()
      queryClient.invalidateQueries({ queryKey: ['product', id] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const upsertMemberMut = useMutation({
    mutationFn: (v: { userId: string; role: string }) =>
      apiPost(`/products/${id}/members`, v),
    onSuccess: () => {
      message.success('成员已保存')
      setMemberOpen(false)
      memberForm.resetFields()
      queryClient.invalidateQueries({ queryKey: ['product', id] })
      queryClient.invalidateQueries({ queryKey: ['members', id] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const removeMemberMut = useMutation({
    mutationFn: (uid: string) => apiDelete(`/products/${id}/members/${uid}`),
    onSuccess: () => {
      message.success('成员已移除')
      queryClient.invalidateQueries({ queryKey: ['product', id] })
      queryClient.invalidateQueries({ queryKey: ['members', id] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const createIterationMut = useMutation({
    mutationFn: (v: { name: string; goal?: string; range: [dayjs.Dayjs, dayjs.Dayjs]; workflowId?: string }) =>
      apiPost<IterationView>(`/products/${id}/iterations`, {
        name: v.name,
        goal: v.goal ?? '',
        startsOn: v.range[0].format('YYYY-MM-DD'),
        endsOn: v.range[1].format('YYYY-MM-DD'),
        workflowId: v.workflowId ?? '',
      }),
    onSuccess: () => {
      message.success('迭代已创建')
      setIterationOpen(false)
      iterationForm.resetFields()
      queryClient.invalidateQueries({ queryKey: ['iterations', id] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  // M3 归档：产品 owner / admin；仍有未收尾迭代时后端 409 并提示原因。
  const { data: whoami } = useQuery({
    queryKey: ['whoami'],
    queryFn: () => apiGet<WhoamiView>('/auth/me'),
  })
  const archiveMut = useMutation({
    mutationFn: () => apiPatch<ProductView>(`/products/${id}`, { archive: true }),
    onSuccess: () => {
      message.success('产品已归档')
      queryClient.invalidateQueries({ queryKey: ['product', id] })
      queryClient.invalidateQueries({ queryKey: ['products'] })
      queryClient.invalidateQueries({ queryKey: ['overview'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  if (detail.isError) return <DetailError error={detail.error} />
  if (detail.isPending) return <PageLoading />
  const p = detail.data
  const myRole = p.members.find((m) => m.userId === whoami?.user.id)?.role
  const canArchive =
    p.status === 'active' && (whoami?.user.roles.includes('admin') || myRole === 'owner')

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <Card
        extra={
          canArchive ? (
            <Popconfirm
              title="确认归档该产品？"
              description="归档后只读保留历史；仍有未收尾迭代时会拒绝"
              onConfirm={() => archiveMut.mutate()}
            >
              <Button size="small" danger loading={archiveMut.isPending}>
                归档产品
              </Button>
            </Popconfirm>
          ) : undefined
        }
      >
        <Typography.Title level={4} style={{ marginTop: 0 }}>
          <Tag>{p.key}</Tag>
          {p.name}
        </Typography.Title>
        <Descriptions
          size="small"
          column={2}
          items={[
            { key: 'status', label: '状态', children: p.status === 'active' ? '活跃' : '已归档' },
            { key: 'desc', label: '描述', children: p.description || '—' },
            { key: 'itwf', label: '默认迭代工作流', children: wfName(p.defaultIterationWorkflowId) },
            { key: 'tswf', label: '默认任务工作流', children: wfName(p.defaultTaskWorkflowId) },
            { key: 'created', label: '创建时间', children: fmtTime(p.createdAt) },
            { key: 'createdBy', label: '创建人', children: p.createdBy },
          ]}
        />
      </Card>

      <Card
        title="模块树"
        extra={<Button size="small" onClick={() => setModuleOpen(true)}>新建模块</Button>}
      >
        {p.modules.length ? (
          <Tree treeData={toTreeNodes(p.modules)} defaultExpandAll blockNode />
        ) : (
          <Typography.Text type="secondary">暂无模块</Typography.Text>
        )}
      </Card>

      <Card
        title={`成员（${p.members.length}）`}
        extra={<Button size="small" onClick={() => setMemberOpen(true)}>添加成员</Button>}
      >
        <Table<MemberView>
          rowKey="userId"
          size="small"
          pagination={false}
          dataSource={p.members}
          columns={[
            { title: '用户', dataIndex: 'username', width: 140 },
            { title: '显示名', dataIndex: 'displayName' },
            {
              title: '产品角色',
              dataIndex: 'role',
              width: 110,
              render: (v: string) => {
                const m = memberRoleMeta[v]
                return m ? <Tag color={m.color}>{m.label}</Tag> : v
              },
            },
            { title: '加入时间', dataIndex: 'createdAt', width: 160, render: (v: string) => fmtTime(v) },
            {
              title: '操作',
              key: 'op',
              width: 90,
              render: (_, record) => (
                <Popconfirm
                  title="确认移除该成员？"
                  description="其未终结任务需先转派，移除失败会提示原因"
                  onConfirm={() => removeMemberMut.mutate(record.userId)}
                >
                  <Button size="small" danger>
                    移除
                  </Button>
                </Popconfirm>
              ),
            },
          ]}
        />
      </Card>

      <Card
        title="迭代"
        extra={<Button size="small" type="primary" onClick={() => setIterationOpen(true)}>新建迭代</Button>}
      >
        {iterations.isError ? (
          <ApiErrorBanner error={iterations.error} />
        ) : (
          <Table<IterationView>
            rowKey="id"
            size="small"
            loading={iterations.isPending}
            pagination={false}
            dataSource={iterations.data?.items ?? []}
            onRow={(record) => ({ onClick: () => navigate(`/iterations/${record.id}`), style: { cursor: 'pointer' } })}
            columns={[
              { title: '编号', dataIndex: 'number', width: 110 },
              { title: '名称', dataIndex: 'name' },
              { title: '目标', dataIndex: 'goal', ellipsis: true, render: (v: string) => v || '—' },
              {
                title: '状态',
                dataIndex: 'status',
                width: 90,
                render: (v: string) => {
                  const m = iterationStatusMeta[v]
                  return m ? <Tag color={m.color}>{m.label}</Tag> : v
                },
              },
              {
                title: '起止',
                key: 'range',
                width: 200,
                render: (_, r) => `${fmtDate(r.startsOn)} ~ ${fmtDate(r.endsOn)}`,
              },
            ]}
          />
        )}
      </Card>

      <Modal
        title="新建模块"
        open={moduleOpen}
        onCancel={() => setModuleOpen(false)}
        confirmLoading={createModuleMut.isPending}
        onOk={() => moduleForm.submit()}
        destroyOnHidden
      >
        <Form form={moduleForm} layout="vertical" onFinish={(v) => createModuleMut.mutate(v)} style={{ marginTop: 16 }}>
          <Form.Item name="parentId" label="父模块（不选则建在根级）" initialValue="">
            <TreeSelect
              allowClear
              treeDefaultExpandAll
              placeholder="根级"
              treeData={toTreeNodes(p.modules)}
            />
          </Form.Item>
          <Form.Item name="name" label="模块名" rules={[{ required: true, message: '请输入模块名' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="sortOrder" label="排序号" initialValue={0}>
            <InputNumber min={0} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="添加成员"
        open={memberOpen}
        onCancel={() => setMemberOpen(false)}
        confirmLoading={upsertMemberMut.isPending}
        onOk={() => memberForm.submit()}
        destroyOnHidden
      >
        <Form form={memberForm} layout="vertical" onFinish={(v) => upsertMemberMut.mutate(v)} style={{ marginTop: 16 }}>
          <Form.Item
            name="userId"
            label="用户 ID（usr_ 前缀）"
            rules={[{ required: true, message: '请输入用户 ID' }]}
          >
            <Input placeholder="usr_xxxxxxxx" />
          </Form.Item>
          <Form.Item name="role" label="产品角色" initialValue="member" rules={[{ required: true }]}>
            <Select
              options={MEMBER_ROLES.map((r) => ({
                value: r,
                label: memberRoleMeta[r]?.label ?? r,
              }))}
            />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="新建迭代"
        open={iterationOpen}
        onCancel={() => setIterationOpen(false)}
        confirmLoading={createIterationMut.isPending}
        onOk={() => iterationForm.submit()}
        destroyOnHidden
      >
        <Form form={iterationForm} layout="vertical" onFinish={(v) => createIterationMut.mutate(v)} style={{ marginTop: 16 }}>
          <Form.Item name="name" label="迭代名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="goal" label="目标">
            <Input.TextArea rows={2} />
          </Form.Item>
          <Form.Item
            name="range"
            label="起止日期"
            rules={[{ required: true, message: '请选择起止日期' }]}
          >
            <DatePicker.RangePicker style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="workflowId" label="生命周期工作流（不选则用产品默认）">
            <Select
              allowClear
              placeholder="产品默认"
              options={(workflows?.items ?? [])
                .filter((w) => w.scope === 'iteration' && w.status === 'active')
                .map((w) => ({ value: w.id, label: `${w.name}（v${w.version}）` }))}
            />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
