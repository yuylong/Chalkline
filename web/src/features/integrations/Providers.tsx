// 集成 Provider 配置（admin）：开关与 baseURL 可视化维护，保存后运行时立即生效。
// 凭据（token / API key）仍只从环境变量读取，本页只展示「是否已配置」。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { App, Button, Card, Form, Input, Modal, Space, Switch, Table, Tag, Typography } from 'antd'
import { useState } from 'react'
import { apiGet, apiPatch } from '../../shared/api'
import type { ListEnvelope, ProviderConfigUpdate, ProviderConfigView } from '../../shared/types'
import { ApiErrorBanner } from '../../shared/ui'
import { fmtTime } from '../../shared/status'

interface EditForm {
  enabled: boolean
  baseUrl: string
}

export default function ProviderConfigs() {
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const [editing, setEditing] = useState<ProviderConfigView | null>(null)
  const [form] = Form.useForm<EditForm>()

  const { data, isPending, isError, error } = useQuery({
    queryKey: ['integration-providers'],
    queryFn: () => apiGet<ListEnvelope<ProviderConfigView>>('/integration/providers'),
  })

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['integration-providers'] })

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

  const openEdit = (p: ProviderConfigView) => {
    setEditing(p)
    form.setFieldsValue({ enabled: p.enabled, baseUrl: p.baseUrl })
  }

  return (
    <Card
      title="集成 Provider 配置"
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
  )
}
