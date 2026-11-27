// 全局 Provider 配置（admin）：一张「服务器列表」——每条记录一个服务器的
// 类型（git / svn / outline / redmine）、名称与地址。没有「默认 git / 默认 svn /
// 默认 outline / 默认 redmine」的概念；列表上方是类型按钮条，可快速筛选
// 「所有」或某一类型。任何选择 provider 的地方（迭代绑定、我的凭据）都从
// 这张列表取下拉建议，也可手填地址。凭据（token / API key）仍只从环境变量 /
// 个人凭据读取，本页不涉及。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  App,
  Button,
  Card,
  Form,
  Input,
  Modal,
  Popconfirm,
  Segmented,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from 'antd'
import { useMemo, useState } from 'react'
import { apiDelete, apiGet, apiPatch, apiPost } from '../../shared/api'
import type {
  ListEnvelope,
  ProviderEndpointCreate,
  ProviderEndpointUpdate,
  ProviderEndpointView,
} from '../../shared/types'
import { ApiErrorBanner } from '../../shared/ui'
import { fmtTime } from '../../shared/status'

interface EndpointForm {
  kind: string
  label: string
  baseUrl: string
  note?: string
}

const ENDPOINT_KINDS = [
  { value: 'git', label: 'git（代码库）' },
  { value: 'svn', label: 'svn（代码库）' },
  { value: 'outline', label: 'outline（知识库）' },
  { value: 'redmine', label: 'redmine（流程记录）' },
]

const httpUrlRule = {
  validator: (_: unknown, v?: string) => {
    const s = (v ?? '').trim()
    if (!s) return Promise.reject(new Error('请填写连接地址'))
    try {
      const u = new URL(s)
      if (u.protocol !== 'http:' && u.protocol !== 'https:') {
        return Promise.reject(new Error('必须是 http/https 地址'))
      }
      if (u.username || u.password) {
        return Promise.reject(new Error('不得内嵌凭据'))
      }
      return Promise.resolve()
    } catch {
      return Promise.reject(new Error('不是合法 URL'))
    }
  },
}

export default function ProviderConfigs() {
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const [kindFilter, setKindFilter] = useState<string>('all')

  const [epCreating, setEpCreating] = useState(false)
  const [epEditing, setEpEditing] = useState<ProviderEndpointView | null>(null)
  const [epForm] = Form.useForm<EndpointForm>()

  const { data: endpoints, isPending: epPending, isError: epError, error } = useQuery({
    queryKey: ['provider-endpoints'],
    queryFn: () => apiGet<ListEnvelope<ProviderEndpointView>>('/integration/endpoints'),
  })

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['provider-endpoints'] })
  }

  const epCreateMut = useMutation({
    mutationFn: (v: EndpointForm) =>
      apiPost<ProviderEndpointView>('/integration/endpoints', {
        kind: v.kind,
        label: v.label.trim(),
        baseUrl: v.baseUrl.trim(),
        note: (v.note ?? '').trim(),
      } satisfies ProviderEndpointCreate),
    onSuccess: () => {
      message.success('服务器已添加')
      setEpCreating(false)
      void invalidate()
    },
    onError: (err: Error) => message.error(err.message),
  })

  const epUpdateMut = useMutation({
    mutationFn: (v: { id: string; body: ProviderEndpointUpdate }) =>
      apiPatch<ProviderEndpointView>(`/integration/endpoints/${v.id}`, v.body),
    onSuccess: () => {
      message.success('服务器已更新')
      setEpEditing(null)
      void invalidate()
    },
    onError: (err: Error) => message.error(err.message),
  })

  const epToggleMut = useMutation({
    mutationFn: (v: { id: string; enabled: boolean }) =>
      apiPatch<ProviderEndpointView>(`/integration/endpoints/${v.id}`, { enabled: v.enabled }),
    onSuccess: () => void invalidate(),
    onError: (err: Error) => message.error(err.message),
  })

  const epDeleteMut = useMutation({
    mutationFn: (id: string) => apiDelete(`/integration/endpoints/${id}`),
    onSuccess: () => {
      message.success('服务器已删除')
      void invalidate()
    },
    onError: (err: Error) => message.error(err.message),
  })

  const openEpEdit = (e: ProviderEndpointView) => {
    setEpEditing(e)
    epForm.setFieldsValue({ kind: e.kind, label: e.label, baseUrl: e.baseUrl, note: e.note })
  }

  const items = endpoints?.items ?? []
  const filtered = useMemo(
    () => (kindFilter === 'all' ? items : items.filter((e) => e.kind === kindFilter)),
    [items, kindFilter],
  )
  // 类型按钮条：「所有」+ 每种类型（带数量），一键筛选列表。
  const filterOptions = useMemo(() => {
    const count = (kind?: string) =>
      kind ? items.filter((e) => e.kind === kind).length : items.length
    return [
      { value: 'all', label: `所有 · ${count()}` },
      ...ENDPOINT_KINDS.map((k) => ({ value: k.value, label: `${k.value} · ${count(k.value)}` })),
    ]
  }, [items])

  return (
    <Card
      title="全局 Provider（服务器列表）"
      extra={
        <Button
          type="primary"
          size="small"
          onClick={() => {
            epForm.resetFields()
            setEpCreating(true)
          }}
        >
          + 添加服务器
        </Button>
      }
    >
      <Typography.Paragraph type="secondary" style={{ marginTop: 0 }}>
        每条记录一个外部服务器的类型、名称与地址。迭代绑定 Provider、个人凭据等
        任何选择处都可从列表选取，也可手填额外地址；列表只存地址与展示名，不存任何凭据。
      </Typography.Paragraph>
      <Segmented
        value={kindFilter}
        onChange={(v) => setKindFilter(v as string)}
        options={filterOptions}
        style={{ marginBottom: 12 }}
      />
      {epError ? (
        <ApiErrorBanner error={error} />
      ) : (
        <Table<ProviderEndpointView>
          rowKey="id"
          loading={epPending}
          dataSource={filtered}
          pagination={false}
          locale={{ emptyText: '暂无服务器（添加后，选择 provider 的地方会出现下拉建议）' }}
          columns={[
            {
              title: '类型',
              dataIndex: 'kind',
              width: 130,
              render: (v: string, r) => (
                <Space direction="vertical" size={0}>
                  <Tag>{v}</Tag>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {r.kindLabel}
                  </Typography.Text>
                </Space>
              ),
            },
            {
              title: '名称',
              dataIndex: 'label',
              width: 180,
              render: (v: string, r) => (
                <Space direction="vertical" size={0}>
                  <Typography.Text strong>{v}</Typography.Text>
                  {r.note && (
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {r.note}
                    </Typography.Text>
                  )}
                </Space>
              ),
            },
            {
              title: '地址',
              dataIndex: 'baseUrl',
              ellipsis: true,
              render: (v: string) => <Typography.Text code>{v}</Typography.Text>,
            },
            {
              title: '可选',
              dataIndex: 'enabled',
              width: 90,
              render: (v: boolean, r) => (
                <Switch
                  size="small"
                  checked={v}
                  loading={epToggleMut.isPending}
                  onChange={(checked) => epToggleMut.mutate({ id: r.id, enabled: checked })}
                />
              ),
            },
            {
              title: '更新时间',
              dataIndex: 'updatedAt',
              width: 160,
              render: (v?: string) =>
                v ? (
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {fmtTime(v)}
                  </Typography.Text>
                ) : (
                  <Typography.Text type="secondary">—</Typography.Text>
                ),
            },
            {
              title: '操作',
              key: 'op',
              width: 140,
              render: (_, r) => (
                <Space size={4}>
                  <Button size="small" onClick={() => openEpEdit(r)}>
                    编辑
                  </Button>
                  <Popconfirm
                    title="删除该服务器？"
                    description="已保存的迭代绑定不受影响，只是不再出现在选择列表。"
                    onConfirm={() => epDeleteMut.mutate(r.id)}
                  >
                    <Button size="small" danger>
                      删除
                    </Button>
                  </Popconfirm>
                </Space>
              ),
            },
          ]}
        />
      )}

      <Modal
        title={epEditing ? '编辑服务器' : '添加服务器'}
        open={epCreating || epEditing !== null}
        onCancel={() => {
          setEpCreating(false)
          setEpEditing(null)
        }}
        confirmLoading={epCreateMut.isPending || epUpdateMut.isPending}
        onOk={() => epForm.submit()}
        destroyOnHidden
      >
        <Form<EndpointForm>
          form={epForm}
          layout="vertical"
          onFinish={(v) => {
            if (epEditing) {
              epUpdateMut.mutate({
                id: epEditing.id,
                body: {
                  label: v.label.trim(),
                  baseUrl: v.baseUrl.trim(),
                  note: (v.note ?? '').trim(),
                },
              })
            } else {
              epCreateMut.mutate(v)
            }
          }}
          style={{ marginTop: 16 }}
        >
          <Form.Item name="kind" label="类型" rules={[{ required: true }]}>
            <Select options={ENDPOINT_KINDS} disabled={epEditing !== null} />
          </Form.Item>
          <Form.Item name="label" label="名称" rules={[{ required: true, message: '请填写名称' }]}>
            <Input placeholder="如 公司 GitLab / 研发一部 Redmine" />
          </Form.Item>
          <Form.Item name="baseUrl" label="地址 baseURL" rules={[httpUrlRule]}>
            <Input placeholder="https://git.example.com" />
          </Form.Item>
          <Form.Item name="note" label="备注（可选）">
            <Input placeholder="如 研发一部代码库" />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
