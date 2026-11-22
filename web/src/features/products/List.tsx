// 产品列表：我的产品 + 创建产品（默认工作流经 PATCH /products/{id} 落地）。
// M3 规模化：搜索 / 状态过滤 / 分页（服务端检索）+ 跨产品概览统计。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { App, Button, Form, Input, Modal, Select, Space, Statistic, Table, Tag, Typography } from 'antd'
import { useState } from 'react'
import { useNavigate } from 'react-router'
import { apiGet, apiPatch, apiPost } from '../../shared/api'
import { fmtTime } from '../../shared/status'
import { ApiErrorBanner } from '../../shared/ui'
import type { ListEnvelope, OverviewView, ProductListResult, ProductView, WorkflowView } from '../../shared/types'

interface CreateForm {
  key: string
  name: string
  description?: string
  defaultIterationWorkflowId?: string
  defaultTaskWorkflowId?: string
}

const PAGE_SIZE = 20

export default function ProductList() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const [open, setOpen] = useState(false)
  const [form] = Form.useForm<CreateForm>()
  const [q, setQ] = useState('')
  const [status, setStatus] = useState<string | undefined>(undefined)
  const [page, setPage] = useState(1)

  const { data, isPending, isError, error } = useQuery({
    queryKey: ['products', q, status, page],
    queryFn: () => {
      const params = new URLSearchParams()
      if (q) params.set('q', q)
      if (status) params.set('status', status)
      params.set('limit', String(PAGE_SIZE))
      params.set('offset', String((page - 1) * PAGE_SIZE))
      return apiGet<ProductListResult>(`/products?${params.toString()}`)
    },
  })
  const { data: overview } = useQuery({
    queryKey: ['overview'],
    queryFn: () => apiGet<OverviewView>('/overview'),
  })
  const { data: workflows } = useQuery({
    queryKey: ['workflows'],
    queryFn: () => apiGet<ListEnvelope<WorkflowView>>('/workflows'),
  })

  const activeByScope = (scope: string) =>
    (workflows?.items ?? []).filter((w) => w.scope === scope && w.status === 'active')

  const createMut = useMutation({
    mutationFn: async (v: CreateForm) => {
      // 创建接口只收 key/name/description；默认工作流建后补设（PATCH）。
      const created = await apiPost<ProductView>('/products', {
        key: v.key,
        name: v.name,
        description: v.description ?? '',
      })
      if (v.defaultIterationWorkflowId || v.defaultTaskWorkflowId) {
        await apiPatch<ProductView>(`/products/${created.id}`, {
          defaultIterationWorkflowId: v.defaultIterationWorkflowId,
          defaultTaskWorkflowId: v.defaultTaskWorkflowId,
        })
      }
      return created
    },
    onSuccess: (created) => {
      message.success(`产品 ${created.name} 已创建`)
      setOpen(false)
      form.resetFields()
      queryClient.invalidateQueries({ queryKey: ['products'] })
      queryClient.invalidateQueries({ queryKey: ['overview'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  if (isError) return <ApiErrorBanner error={error} />

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 12 }}>
        <Typography.Title level={4} style={{ margin: 0 }}>
          我的产品
        </Typography.Title>
        <Button type="primary" onClick={() => setOpen(true)}>
          创建产品
        </Button>
      </div>

      {overview && (
        <Space size="large" style={{ marginBottom: 16, background: '#fff', padding: '12px 24px', borderRadius: 8 }}>
          <Statistic title="可见产品" value={overview.products} />
          <Statistic title="进行中迭代" value={overview.activeIterations} />
          <Statistic title="未完结任务" value={overview.openTasks} />
          <Statistic title="待审批门禁" value={overview.pendingGates} />
          <Statistic
            title="超期待审"
            value={overview.overdueGates}
            valueStyle={overview.overdueGates > 0 ? { color: '#cf1322' } : undefined}
          />
        </Space>
      )}

      <Space style={{ marginBottom: 12 }}>
        <Input.Search
          allowClear
          placeholder="搜索名称 / 标识"
          style={{ width: 240 }}
          onSearch={(v) => {
            setQ(v)
            setPage(1)
          }}
        />
        <Select
          allowClear
          placeholder="全部状态"
          style={{ width: 140 }}
          value={status}
          onChange={(v) => {
            setStatus(v)
            setPage(1)
          }}
          options={[
            { value: 'active', label: '活跃' },
            { value: 'archived', label: '已归档' },
          ]}
        />
      </Space>

      <Table<ProductView>
        rowKey="id"
        loading={isPending}
        dataSource={data?.items ?? []}
        pagination={{
          current: page,
          pageSize: PAGE_SIZE,
          total: data?.total ?? 0,
          showSizeChanger: false,
          onChange: setPage,
        }}
        onRow={(record) => ({ onClick: () => navigate(`/products/${record.id}`), style: { cursor: 'pointer' } })}
        columns={[
          { title: '标识', dataIndex: 'key', width: 90, render: (v: string) => <Tag>{v}</Tag> },
          { title: '名称', dataIndex: 'name' },
          {
            title: '状态',
            dataIndex: 'status',
            width: 90,
            render: (v: string) => (v === 'active' ? <Tag color="green">活跃</Tag> : <Tag>已归档</Tag>),
          },
          {
            title: '描述',
            dataIndex: 'description',
            ellipsis: true,
            render: (v: string) => v || '—',
          },
          { title: '创建时间', dataIndex: 'createdAt', width: 160, render: (v: string) => fmtTime(v) },
        ]}
      />

      <Modal
        title="创建产品"
        open={open}
        onCancel={() => setOpen(false)}
        confirmLoading={createMut.isPending}
        onOk={() => form.submit()}
        destroyOnHidden
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={(v: CreateForm) => createMut.mutate(v)}
          style={{ marginTop: 16 }}
        >
          <Form.Item
            name="key"
            label="产品标识（2-8 位大写字母，创建后不可改）"
            rules={[
              { required: true, message: '请输入产品标识' },
              { pattern: /^[A-Z]{2,8}$/, message: '需为 2-8 位大写字母' },
            ]}
          >
            <Input placeholder="如 CHK" />
          </Form.Item>
          <Form.Item name="name" label="产品名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} />
          </Form.Item>
          <Form.Item
            name="defaultIterationWorkflowId"
            label="默认迭代工作流（不选则用内置全生命周期）"
          >
            <Select
              allowClear
              placeholder="内置：全生命周期（默认）"
              options={activeByScope('iteration').map((w) => ({
                value: w.id,
                label: `${w.name}（v${w.version}）`,
              }))}
            />
          </Form.Item>
          <Form.Item name="defaultTaskWorkflowId" label="默认任务工作流（不选则用内置标准开发任务）">
            <Select
              allowClear
              placeholder="内置：标准开发任务"
              options={activeByScope('task').map((w) => ({
                value: w.id,
                label: `${w.name}（v${w.version}）`,
              }))}
            />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
