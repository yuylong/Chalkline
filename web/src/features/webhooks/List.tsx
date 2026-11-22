// Webhook 订阅管理（M3，admin）：事件推送到外部系统的订阅 CRUD。
// 签名密钥是全局 CHALKLINE_WEBHOOK_SECRET（凭据不落库），本页只管理 URL / 事件类型 / 状态。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { App, Button, Card, Form, Input, Modal, Popconfirm, Select, Space, Switch, Table, Tag, Typography } from 'antd'
import { useState } from 'react'
import { apiDelete, apiGet, apiPatch, apiPost } from '../../shared/api'
import { fmtTime } from '../../shared/status'
import { ApiErrorBanner } from '../../shared/ui'
import type { ListEnvelope, WebhookView } from '../../shared/types'

const EVENT_TYPES = [
  'node.ready',
  'node.passed',
  'node.rejected',
  'gate.pending',
  'gate.overdue',
  'gate.escalated',
  'task.assigned',
  'iteration.closed',
]

interface WebhookForm {
  name: string
  url: string
  eventTypes?: string[]
}

export default function WebhookList() {
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const [open, setOpen] = useState(false)
  const [form] = Form.useForm<WebhookForm>()

  const { data, isPending, isError, error } = useQuery({
    queryKey: ['webhooks'],
    queryFn: () => apiGet<ListEnvelope<WebhookView>>('/webhooks'),
  })

  const createMut = useMutation({
    mutationFn: (v: WebhookForm) =>
      apiPost<WebhookView>('/webhooks', {
        name: v.name,
        url: v.url,
        eventTypes: v.eventTypes ?? [],
      }),
    onSuccess: () => {
      message.success('订阅已创建')
      setOpen(false)
      form.resetFields()
      queryClient.invalidateQueries({ queryKey: ['webhooks'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const toggleMut = useMutation({
    mutationFn: (v: { id: string; status: string }) =>
      apiPatch<WebhookView>(`/webhooks/${v.id}`, { status: v.status }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['webhooks'] }),
    onError: (err: Error) => message.error(err.message),
  })

  const deleteMut = useMutation({
    mutationFn: (id: string) => apiDelete(`/webhooks/${id}`),
    onSuccess: () => {
      message.success('订阅已删除')
      queryClient.invalidateQueries({ queryKey: ['webhooks'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  return (
    <Card
      title="Webhook 订阅"
      extra={
        <Space>
          <Typography.Text type="secondary">
            签名头 X-Chalkline-Signature（sha256=HMAC），密钥为全局 CHALKLINE_WEBHOOK_SECRET
          </Typography.Text>
          <Button type="primary" onClick={() => setOpen(true)}>
            新建订阅
          </Button>
        </Space>
      }
    >
      {isError ? (
        <ApiErrorBanner error={error} />
      ) : (
        <Table<WebhookView>
          rowKey="id"
          loading={isPending}
          dataSource={data?.items ?? []}
          pagination={false}
          columns={[
            { title: '名称', dataIndex: 'name', width: 160 },
            {
              title: 'URL',
              dataIndex: 'url',
              ellipsis: true,
              render: (v: string) => (
                <Typography.Link href={v} target="_blank" rel="noreferrer">
                  {v}
                </Typography.Link>
              ),
            },
            {
              title: '事件类型',
              dataIndex: 'eventTypes',
              width: 220,
              render: (v: string[]) =>
                v.length ? (
                  <Space size={4} wrap>
                    {v.map((t) => (
                      <Tag key={t}>{t}</Tag>
                    ))}
                  </Space>
                ) : (
                  <Tag color="blue">全部</Tag>
                ),
            },
            {
              title: '状态',
              dataIndex: 'status',
              width: 90,
              render: (v: string) => (v === 'active' ? <Tag color="green">启用</Tag> : <Tag>暂停</Tag>),
            },
            {
              title: '最近投递',
              key: 'delivery',
              width: 200,
              render: (_, r) =>
                r.lastDeliveryAt ? (
                  <Space size={4}>
                    <Tag color={r.lastDeliveryOk ? 'green' : 'red'}>
                      {r.lastDeliveryOk ? '成功' : '失败'}
                    </Tag>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {fmtTime(r.lastDeliveryAt)}
                    </Typography.Text>
                  </Space>
                ) : (
                  <Typography.Text type="secondary">未投递</Typography.Text>
                ),
            },
            {
              title: '操作',
              key: 'op',
              width: 150,
              render: (_, r) => (
                <Space>
                  <Switch
                    size="small"
                    checked={r.status === 'active'}
                    onChange={(checked) =>
                      toggleMut.mutate({ id: r.id, status: checked ? 'active' : 'paused' })
                    }
                  />
                  <Popconfirm title="确认删除该订阅？" onConfirm={() => deleteMut.mutate(r.id)}>
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
        title="新建 Webhook 订阅"
        open={open}
        onCancel={() => setOpen(false)}
        confirmLoading={createMut.isPending}
        onOk={() => form.submit()}
        destroyOnHidden
      >
        <Form<WebhookForm>
          form={form}
          layout="vertical"
          onFinish={(v) => createMut.mutate(v)}
          style={{ marginTop: 16 }}
        >
          <Form.Item name="name" label="订阅名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如 CI 触发器" />
          </Form.Item>
          <Form.Item
            name="url"
            label="接收地址（http/https）"
            rules={[
              { required: true, message: '请输入 URL' },
              { type: 'url', message: '需为合法 URL' },
            ]}
          >
            <Input placeholder="https://ci.example.com/hooks/chalkline" />
          </Form.Item>
          <Form.Item name="eventTypes" label="订阅事件（不选 = 全部事件）">
            <Select mode="multiple" allowClear placeholder="全部事件" options={EVENT_TYPES.map((t) => ({ value: t, label: t }))} />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
