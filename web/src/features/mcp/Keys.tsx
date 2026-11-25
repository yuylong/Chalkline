// MCP 接入管理：API Key（PAT）签发与吊销 + 只读连接信息。
// Key 明文只在签发时展示一次；列表仅含本人 Key（后端按 actor 过滤，用户间互不可见）。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  App,
  Button,
  Card,
  Descriptions,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd'
import { useState } from 'react'
import { apiDelete, apiGet, apiPost } from '../../shared/api'
import { fmtTime } from '../../shared/status'
import { ApiErrorBanner } from '../../shared/ui'
import type { ListEnvelope, PATCreated, PATView } from '../../shared/types'

// 控制台与 MCP 端点同源（nginx 反代 /mcp），直接用当前 origin 展示。
const MCP_ENDPOINT = `${window.location.origin}/mcp`

interface KeyForm {
  name: string
  ttlDays: number
}

function patStatus(p: PATView): { label: string; color: string } {
  if (p.revoked) return { label: '已吊销', color: 'default' }
  if (new Date(p.expiresAt).getTime() <= Date.now()) return { label: '已过期', color: 'orange' }
  return { label: '有效', color: 'green' }
}

export default function McpKeys() {
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const [open, setOpen] = useState(false)
  const [issued, setIssued] = useState<PATCreated | null>(null)
  const [form] = Form.useForm<KeyForm>()

  const { data, isPending, isError, error } = useQuery({
    queryKey: ['pats'],
    queryFn: () => apiGet<ListEnvelope<PATView>>('/auth/pats'),
  })

  const createMut = useMutation({
    mutationFn: (v: KeyForm) => apiPost<PATCreated>('/auth/pat', { name: v.name, ttlDays: v.ttlDays }),
    onSuccess: (created) => {
      setOpen(false)
      form.resetFields()
      setIssued(created)
      queryClient.invalidateQueries({ queryKey: ['pats'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const revokeMut = useMutation({
    mutationFn: (id: string) => apiDelete(`/auth/pats/${id}`),
    onSuccess: () => {
      message.success('Key 已吊销')
      queryClient.invalidateQueries({ queryKey: ['pats'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card title="MCP 连接信息（只读）">
        <Alert
          type="info"
          showIcon
          message="编码 Agent 通过下方端点接入 Chalkline；鉴权使用本页生成的 API Key。"
          style={{ marginBottom: 16 }}
        />
        <Descriptions column={1} size="small" bordered>
          <Descriptions.Item label="端点">
            <Typography.Text copyable code>
              {MCP_ENDPOINT}
            </Typography.Text>
          </Descriptions.Item>
          <Descriptions.Item label="方法 / 传输">
            <Typography.Text code>POST</Typography.Text>
            <Typography.Text type="secondary"> · streamable HTTP</Typography.Text>
          </Descriptions.Item>
          <Descriptions.Item label="鉴权头">
            <Typography.Text code>Authorization: Bearer &lt;你的 API Key&gt;</Typography.Text>
          </Descriptions.Item>
        </Descriptions>
        <Typography.Paragraph type="secondary" style={{ marginTop: 12, marginBottom: 0 }}>
          API Key 仅用于 MCP 接入，不可用于控制台登录；工具清单与错误码见平台文档（docs/domain/mcp.md）。
        </Typography.Paragraph>
      </Card>

      <Card
        title="我的 API Key"
        extra={
          <Space>
            <Typography.Text type="secondary">仅本人可见，可生成多个</Typography.Text>
            <Button type="primary" onClick={() => setOpen(true)}>
              生成 API Key
            </Button>
          </Space>
        }
      >
        {isError ? (
          <ApiErrorBanner error={error} />
        ) : (
          <Table<PATView>
            rowKey="id"
            loading={isPending}
            dataSource={data?.items ?? []}
            pagination={false}
            columns={[
              { title: '名称', dataIndex: 'name', width: 200 },
              {
                title: '状态',
                key: 'status',
                width: 100,
                render: (_, r) => {
                  const s = patStatus(r)
                  return <Tag color={s.color}>{s.label}</Tag>
                },
              },
              {
                title: '创建时间',
                dataIndex: 'createdAt',
                width: 170,
                render: (v: string) => (
                  <Typography.Text type="secondary">{fmtTime(v)}</Typography.Text>
                ),
              },
              {
                title: '过期时间',
                dataIndex: 'expiresAt',
                width: 170,
                render: (v: string) => (
                  <Typography.Text type="secondary">{fmtTime(v)}</Typography.Text>
                ),
              },
              {
                title: '操作',
                key: 'op',
                width: 110,
                render: (_, r) =>
                  r.revoked ? (
                    <Typography.Text type="secondary">—</Typography.Text>
                  ) : (
                    <Popconfirm
                      title="确认吊销该 Key？"
                      description="吊销后立即失效，使用该 Key 的 Agent 将无法访问。"
                      onConfirm={() => revokeMut.mutate(r.id)}
                    >
                      <Button size="small" danger>
                        吊销
                      </Button>
                    </Popconfirm>
                  ),
              },
            ]}
          />
        )}
      </Card>

      <Modal
        title="生成 API Key"
        open={open}
        onCancel={() => setOpen(false)}
        confirmLoading={createMut.isPending}
        onOk={() => form.submit()}
        destroyOnHidden
      >
        <Form<KeyForm>
          form={form}
          layout="vertical"
          initialValues={{ name: '', ttlDays: 90 }}
          onFinish={(v) => createMut.mutate(v)}
          style={{ marginTop: 16 }}
        >
          <Form.Item name="name" label="名称" extra="便于区分用途，如「CI Agent」；留空为 default">
            <Input placeholder="如 CI Agent" />
          </Form.Item>
          <Form.Item
            name="ttlDays"
            label="有效期（天）"
            extra="默认 90 天，最长 365 天"
            rules={[{ required: true, message: '请输入有效期' }]}
          >
            <InputNumber min={1} max={365} style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="API Key 已生成"
        open={issued !== null}
        onCancel={() => setIssued(null)}
        onOk={() => setIssued(null)}
        okText="我已保存"
        cancelButtonProps={{ style: { display: 'none' } }}
      >
        <Alert
          type="warning"
          showIcon
          message="明文 Key 只显示这一次，请立即保存到安全位置；关闭后无法再查看。"
          style={{ marginBottom: 16 }}
        />
        <Typography.Paragraph copyable={{ text: issued?.token }} code style={{ wordBreak: 'break-all' }}>
          {issued?.token}
        </Typography.Paragraph>
      </Modal>
    </Space>
  )
}
