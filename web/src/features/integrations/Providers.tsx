// 集成 Provider 配置（admin）：开关与 baseURL 可视化维护，保存后运行时立即生效。
// 连接目录（provider_endpoints）：多套外部系统地址的增删改查——多个 git / svn /
// redmine / outline，迭代绑定时从目录选择或自行填写。
// 凭据（token / API key）仍只从环境变量读取，本页只展示「是否已配置」。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  App,
  Button,
  Card,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from 'antd'
import { useState } from 'react'
import { apiDelete, apiGet, apiPatch, apiPost } from '../../shared/api'
import type {
  ListEnvelope,
  ProviderConfigUpdate,
  ProviderConfigView,
  ProviderEndpointCreate,
  ProviderEndpointUpdate,
  ProviderEndpointView,
} from '../../shared/types'
import { ApiErrorBanner } from '../../shared/ui'
import { fmtTime } from '../../shared/status'

interface EditForm {
  enabled: boolean
  baseUrl: string
}

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
  const [editing, setEditing] = useState<ProviderConfigView | null>(null)
  const [form] = Form.useForm<EditForm>()

  // 连接目录：新增 / 编辑弹窗。
  const [epCreating, setEpCreating] = useState(false)
  const [epEditing, setEpEditing] = useState<ProviderEndpointView | null>(null)
  const [epForm] = Form.useForm<EndpointForm>()

  const { data, isPending, isError, error } = useQuery({
    queryKey: ['integration-providers'],
    queryFn: () => apiGet<ListEnvelope<ProviderConfigView>>('/integration/providers'),
  })

  const { data: endpoints, isPending: epPending } = useQuery({
    queryKey: ['provider-endpoints'],
    queryFn: () => apiGet<ListEnvelope<ProviderEndpointView>>('/integration/endpoints'),
  })

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['integration-providers'] })
    void queryClient.invalidateQueries({ queryKey: ['provider-endpoints'] })
    void queryClient.invalidateQueries({ queryKey: ['provider-options'] })
  }

  const saveMut = useMutation({
    mutationFn: (v: { kind: string; body: ProviderConfigUpdate }) =>
      apiPatch<ProviderConfigView>(`/integration/providers/${v.kind}`, v.body),
    onSuccess: () => {
      message.success('配置已保存（立即生效）')
      setEditing(null)
      void invalidate()
    },
    onError: (err: Error) => message.error(err.message),
  })

  const toggleMut = useMutation({
    mutationFn: (v: { kind: string; enabled: boolean }) =>
      apiPatch<ProviderConfigView>(`/integration/providers/${v.kind}`, { enabled: v.enabled }),
    onSuccess: () => void invalidate(),
    onError: (err: Error) => message.error(err.message),
  })

  // ---- 连接目录 CRUD ----

  const epCreateMut = useMutation({
    mutationFn: (v: EndpointForm) =>
      apiPost<ProviderEndpointView>('/integration/endpoints', {
        kind: v.kind,
        label: v.label.trim(),
        baseUrl: v.baseUrl.trim(),
        note: (v.note ?? '').trim(),
      } satisfies ProviderEndpointCreate),
    onSuccess: () => {
      message.success('目录项已添加')
      setEpCreating(false)
      void invalidate()
    },
    onError: (err: Error) => message.error(err.message),
  })

  const epUpdateMut = useMutation({
    mutationFn: (v: { id: string; body: ProviderEndpointUpdate }) =>
      apiPatch<ProviderEndpointView>(`/integration/endpoints/${v.id}`, v.body),
    onSuccess: () => {
      message.success('目录项已更新')
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
      message.success('目录项已删除')
      void invalidate()
    },
    onError: (err: Error) => message.error(err.message),
  })

  const openEdit = (p: ProviderConfigView) => {
    setEditing(p)
    form.setFieldsValue({ enabled: p.enabled, baseUrl: p.baseUrl })
  }

  const openEpEdit = (e: ProviderEndpointView) => {
    setEpEditing(e)
    epForm.setFieldsValue({ kind: e.kind, label: e.label, baseUrl: e.baseUrl, note: e.note })
  }

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card
        title="连接目录（多个 git / svn / redmine / outline）"
        extra={
          <Button
            type="primary"
            size="small"
            onClick={() => {
              epForm.resetFields()
              setEpCreating(true)
            }}
          >
            + 添加连接
          </Button>
        }
      >
        <Typography.Paragraph type="secondary" style={{ marginTop: 0 }}>
          admin 维护的外部系统地址清单。迭代绑定 Provider 时可从目录选择，也可自行填写额外地址；
          目录只存地址与展示名，不存任何凭据。
        </Typography.Paragraph>
        <Table<ProviderEndpointView>
          rowKey="id"
          loading={epPending}
          dataSource={endpoints?.items ?? []}
          pagination={false}
          locale={{ emptyText: '暂无目录项（添加后，迭代绑定弹窗会出现下拉建议）' }}
          columns={[
            {
              title: 'Provider',
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
              title: '展示名',
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
              title: 'Base URL',
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
                    title="删除该目录项？"
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

        <Modal
          title={epEditing ? '编辑目录项' : '添加连接'}
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
            <Form.Item name="kind" label="Provider 类型" rules={[{ required: true }]}>
              <Select options={ENDPOINT_KINDS} disabled={epEditing !== null} />
            </Form.Item>
            <Form.Item name="label" label="展示名" rules={[{ required: true, message: '请填写展示名' }]}>
              <Input placeholder="如 公司 GitLab / 研发一部 Redmine" />
            </Form.Item>
            <Form.Item name="baseUrl" label="连接地址 baseURL" rules={[httpUrlRule]}>
              <Input placeholder="https://git.example.com" />
            </Form.Item>
            <Form.Item name="note" label="备注（可选）">
              <Input placeholder="如 研发一部代码库" />
            </Form.Item>
          </Form>
        </Modal>
      </Card>

      <Card
        title="Provider 启停与默认地址"
        extra={
          <Typography.Text type="secondary">
            凭据只经环境变量（CHALKLINE_OUTLINE_TOKEN / CHALKLINE_REDMINE_TOKEN），不落库
          </Typography.Text>
        }
      >
        {isError ? (
          <ApiErrorBanner error={error} />
        ) : (
          <Table<ProviderConfigView>
            rowKey="kind"
            loading={isPending}
            dataSource={data?.items ?? []}
            pagination={false}
            columns={[
              {
                title: 'Provider',
                dataIndex: 'label',
                width: 220,
                render: (_: string, r) => (
                  <Space direction="vertical" size={0}>
                    <Typography.Text strong>{r.label}</Typography.Text>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {r.kind}
                    </Typography.Text>
                  </Space>
                ),
              },
              {
                title: 'Base URL',
                dataIndex: 'baseUrl',
                ellipsis: true,
                render: (v: string, r) => (
                  <Space direction="vertical" size={0}>
                    <Typography.Text code>{v || '—'}</Typography.Text>
                    {r.envBaseUrl && r.envBaseUrl !== v && (
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        环境变量默认：{r.envBaseUrl}
                      </Typography.Text>
                    )}
                  </Space>
                ),
              },
              {
                title: '凭据',
                dataIndex: 'tokenConfigured',
                width: 110,
                render: (v: boolean, r) =>
                  !r.tokenRequired ? (
                    <Tag>无需凭据</Tag>
                  ) : v ? (
                    <Tag color="green">已配置</Tag>
                  ) : (
                    <Tag color="orange">未配置</Tag>
                  ),
              },
              {
                title: '启用',
                dataIndex: 'enabled',
                width: 100,
                render: (v: boolean, r) => (
                  <Switch
                    size="small"
                    checked={v}
                    loading={toggleMut.isPending}
                    onChange={(checked) => toggleMut.mutate({ kind: r.kind, enabled: checked })}
                  />
                ),
              },
              {
                title: '最近更新',
                dataIndex: 'updatedAt',
                width: 170,
                render: (v?: string) =>
                  v ? (
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {fmtTime(v)}
                    </Typography.Text>
                  ) : (
                    <Typography.Text type="secondary">默认</Typography.Text>
                  ),
              },
              {
                title: '操作',
                key: 'op',
                width: 100,
                render: (_, r) => (
                  <Button size="small" onClick={() => openEdit(r)}>
                    配置
                  </Button>
                ),
              },
            ]}
          />
        )}

        <Modal
          title={editing ? `配置 ${editing.label}` : '配置 Provider'}
          open={editing !== null}
          onCancel={() => setEditing(null)}
          confirmLoading={saveMut.isPending}
          onOk={() => form.submit()}
          destroyOnHidden
        >
          <Form<EditForm>
            form={form}
            layout="vertical"
            onFinish={(v) =>
              editing &&
              saveMut.mutate({
                kind: editing.kind,
                body: { enabled: v.enabled, baseUrl: v.baseUrl.trim() },
              })
            }
            style={{ marginTop: 16 }}
          >
            <Form.Item name="enabled" label="启用外部快照" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item
              name="baseUrl"
              label="Base URL"
              extra={
                editing?.envBaseUrl
                  ? `留空沿用环境变量默认值：${editing.envBaseUrl}`
                  : '留空表示不覆盖（该 provider 默认无 baseURL）'
              }
              rules={[
                {
                  validator: (_, v: string) => {
                    const s = (v ?? '').trim()
                    if (!s) return Promise.resolve()
                    try {
                      const u = new URL(s)
                      if (u.protocol !== 'http:' && u.protocol !== 'https:') {
                        return Promise.reject(new Error('必须是 http/https 地址'))
                      }
                      return Promise.resolve()
                    } catch {
                      return Promise.reject(new Error('不是合法 URL'))
                    }
                  },
                },
              ]}
            >
              <Input placeholder="https://outline.example.com" />
            </Form.Item>
            {editing && (
              <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
                {editing.tokenRequired
                  ? `凭据状态：${editing.tokenConfigured ? '环境变量已配置' : '环境变量未配置'}（凭据不在控制台编辑）。`
                  : '该 provider 无需凭据（只存引用，不出站取数）。'}
              </Typography.Paragraph>
            )}
          </Form>
        </Modal>
      </Card>
    </Space>
  )
}
