// 产品详情：基本信息、迭代列表（跳看板）、成员表、模块树（只读 + 新建）。
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
import { apiDelete, apiGet, apiPatch, apiPost, apiPut } from '../../shared/api'
import { fmtDate, fmtTime, iterationStatusMeta, memberRoleMeta, memberNameMap, memberNameOf, MEMBER_ROLES, userLabel } from '../../shared/status'
import { ApiErrorBanner, DetailError, PageLoading } from '../../shared/ui'
import { formToProviders, ProviderBindingFields, type ProviderFormValues } from '../iterations/Providers'
import type {
  CurrentIterationView,
  IterationView,
  ListEnvelope,
  MemberView,
  ModuleView,
  ProductDetail,
  ProductView,
  UserView,
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
  const [iterationForm] = Form.useForm<
    {
      name: string
      goal?: string
      range: [dayjs.Dayjs, dayjs.Dayjs]
      workflowId?: string
    } & ProviderFormValues
  >()

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
      queryClient.invalidateQueries({ queryKey: ['member-candidates', id] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const removeMemberMut = useMutation({
    mutationFn: (uid: string) => apiDelete(`/products/${id}/members/${uid}`),
    onSuccess: () => {
      message.success('成员已移除')
      queryClient.invalidateQueries({ queryKey: ['product', id] })
      queryClient.invalidateQueries({ queryKey: ['members', id] })
      queryClient.invalidateQueries({ queryKey: ['member-candidates', id] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const createIterationMut = useMutation({
    mutationFn: (
      v: { name: string; goal?: string; range: [dayjs.Dayjs, dayjs.Dayjs]; workflowId?: string } & ProviderFormValues,
    ) =>
      apiPost<IterationView>(`/products/${id}/iterations`, {
        name: v.name,
        goal: v.goal ?? '',
        startsOn: v.range[0].format('YYYY-MM-DD'),
        endsOn: v.range[1].format('YYYY-MM-DD'),
        workflowId: v.workflowId ?? '',
        // 迭代级 provider 绑定：成果物必选（inline 亦需显式选择），随迭代固化。
        providers: formToProviders(v),
      }),
    onSuccess: () => {
      message.success('迭代已创建')
      setIterationOpen(false)
      iterationForm.resetFields()
      queryClient.invalidateQueries({ queryKey: ['iterations', id] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  // 当前迭代：在迭代列表里一键设为当前迭代（首页直达），无需先进迭代详情。
  const { data: current } = useQuery({
    queryKey: ['current-iteration'],
    queryFn: () => apiGet<CurrentIterationView>('/current-iteration'),
  })
  const setCurrentMut = useMutation({
    mutationFn: (iterationId: string) =>
      apiPut<IterationView>('/current-iteration', { iterationId }),
    onSuccess: () => {
      message.success('已设为当前迭代，登录后首页直接进入')
      queryClient.invalidateQueries({ queryKey: ['current-iteration'] })
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

  // 添加成员改为下拉选择：候选 = 尚未加入本产品的启用成员（产品 owner / admin 可查）。
  const candidates = useQuery({
    queryKey: ['member-candidates', id],
    queryFn: () => apiGet<ListEnvelope<UserView>>(`/products/${id}/member-candidates`),
    enabled: memberOpen,
  })
  const candidateOptions = (candidates.data?.items ?? []).map((u) => ({
    value: u.id,
    label: userLabel(u),
  }))

  if (detail.isError) return <DetailError error={detail.error} />
  if (detail.isPending) return <PageLoading />
  const p = detail.data
  const memberNames = memberNameMap(p.members)
  const myRole = p.members.find((m) => m.userId === whoami?.user.id)?.role
  const isAdmin = whoami?.user.roles.includes('admin') === true
  const canManageMembers = isAdmin || myRole === 'owner'
  const canArchive = p.status === 'active' && canManageMembers

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
            { key: 'createdBy', label: '创建人', children: memberNameOf(memberNames, p.createdBy) },
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
              {
                title: '当前迭代',
                key: 'current',
                width: 130,
                render: (_, r) => {
                  if (r.status !== 'planned' && r.status !== 'active') return '—'
                  if (current?.iteration?.id === r.id) return <Tag color="orange">当前迭代</Tag>
                  return (
                    <Button
                      size="small"
                      type="link"
                      style={{ padding: 0 }}
                      loading={setCurrentMut.isPending}
                      onClick={(e) => {
                        e.stopPropagation()
                        setCurrentMut.mutate(r.id)
                      }}
                    >
                      设为当前
                    </Button>
                  )
                },
              },
            ]}
          />
        )}
      </Card>

      <Card
        title={`成员（${p.members.length}）`}
        extra={
          <Button
            size="small"
            onClick={() => setMemberOpen(true)}
            disabled={!canManageMembers}
            title={canManageMembers ? undefined : '仅产品负责人或管理员可添加成员'}
          >
            添加成员
          </Button>
        }
      >
        <Table<MemberView>
          rowKey="userId"
          size="small"
          pagination={false}
          dataSource={p.members}
          columns={[
            {
              title: '成员',
              key: 'user',
              render: (_, r) => userLabel(r),
            },
            {
              title: '产品角色',
              dataIndex: 'role',
              width: 150,
              render: (v: string, record) => {
                // 有管理权时可直接改角色（复用 upsert 接口）；否则只读展示。
                if (!canManageMembers) {
                  const m = memberRoleMeta[v]
                  return m ? <Tag color={m.color}>{m.label}</Tag> : v
                }
                return (
                  <Select
                    size="small"
                    value={v}
                    style={{ width: 120 }}
                    loading={upsertMemberMut.isPending}
                    onChange={(role) =>
                      upsertMemberMut.mutate({ userId: record.userId, role })
                    }
                    options={MEMBER_ROLES.map((r) => ({
                      value: r,
                      label: memberRoleMeta[r]?.label ?? r,
                    }))}
                  />
                )
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
        title="模块树"
        extra={<Button size="small" onClick={() => setModuleOpen(true)}>新建模块</Button>}
      >
        {p.modules.length ? (
          <Tree treeData={toTreeNodes(p.modules)} defaultExpandAll blockNode />
        ) : (
          <Typography.Text type="secondary">暂无模块</Typography.Text>
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
            label="选择成员"
            rules={[{ required: true, message: '请选择成员' }]}
            extra={candidates.isError ? '候选列表加载失败，请重试' : undefined}
          >
            <Select
              showSearch
              optionFilterProp="label"
              placeholder="输入姓名或用户名搜索"
              loading={candidates.isPending}
              options={candidateOptions}
              notFoundContent={candidates.isPending ? '加载中…' : '没有可加入的成员'}
            />
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
          <ProviderBindingFields />
        </Form>
      </Modal>
    </div>
  )
}
