// 审计留痕检索（M3）：admin 全局检索；产品内检索走 /products/{id}/audit-logs
//（本页提供 productId 过滤，后端按产品归属过滤行）。
import { useQuery } from '@tanstack/react-query'
import { Button, Card, DatePicker, Form, Input, Select, Space, Table, Tag, Typography } from 'antd'
import { useState } from 'react'
import { apiGet } from '../../shared/api'
import { fmtTime } from '../../shared/status'
import { ApiErrorBanner } from '../../shared/ui'
import type { AuditListResult, AuditLogView } from '../../shared/types'

const PAGE_SIZE = 20

const SUBJECT_TYPES = ['product', 'iteration', 'task', 'module', 'workflow', 'user', 'webhook']

interface AuditFilterForm {
  actorId?: string
  action?: string
  subjectType?: string
  subjectId?: string
  productId?: string
  range?: [{ toISOString: () => string }, { toISOString: () => string }]
}

export default function AuditList() {
  const [query, setQuery] = useState<Record<string, string>>({})
  const [range, setRange] = useState<[string, string] | undefined>(undefined)
  const [page, setPage] = useState(1)

  const { data, isPending, isError, error } = useQuery({
    queryKey: ['audit', query, range, page],
    queryFn: () => {
      const params = new URLSearchParams()
      for (const [k, v] of Object.entries(query)) {
        if (v) params.set(k, v)
      }
      if (range?.[0]) params.set('from', range[0])
      if (range?.[1]) params.set('to', range[1])
      params.set('limit', String(PAGE_SIZE))
      params.set('offset', String((page - 1) * PAGE_SIZE))
      return apiGet<AuditListResult>(`/audit-logs?${params.toString()}`)
    },
  })

  return (
    <Card title="审计留痕" extra={<Typography.Text type="secondary">全部写操作留痕，时间倒序</Typography.Text>}>
      <Form<AuditFilterForm>
        layout="inline"
        style={{ marginBottom: 12 }}
        onFinish={(v) => {
          const { range: r, ...rest } = v
          setQuery({
            actorId: rest.actorId ?? '',
            action: rest.action ?? '',
            subjectType: rest.subjectType ?? '',
            subjectId: rest.subjectId ?? '',
            productId: rest.productId ?? '',
          })
          setRange(
            r && r[0] && r[1] ? [r[0].toISOString(), r[1].toISOString()] : undefined,
          )
          setPage(1)
        }}
      >
        <Space wrap>
          <Form.Item name="actorId">
            <Input placeholder="操作者 ID（如 usr_x / system）" style={{ width: 200 }} allowClear />
          </Form.Item>
          <Form.Item name="action">
            <Input placeholder="动作（如 gate.review）" style={{ width: 170 }} allowClear />
          </Form.Item>
          <Form.Item name="subjectType">
            <Select
              placeholder="主体类型"
              style={{ width: 130 }}
              allowClear
              options={SUBJECT_TYPES.map((t) => ({ value: t, label: t }))}
            />
          </Form.Item>
          <Form.Item name="subjectId">
            <Input placeholder="主体 ID" style={{ width: 180 }} allowClear />
          </Form.Item>
          <Form.Item name="productId">
            <Input placeholder="产品 ID（按产品过滤）" style={{ width: 200 }} allowClear />
          </Form.Item>
          <Form.Item name="range">
            <DatePicker.RangePicker showTime style={{ width: 340 }} />
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit">
              检索
            </Button>
          </Form.Item>
        </Space>
      </Form>

      {isError ? (
        <ApiErrorBanner error={error} />
      ) : (
        <Table<AuditLogView>
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
          columns={[
            { title: '时间', dataIndex: 'createdAt', width: 170, render: (v: string) => fmtTime(v) },
            { title: '操作者', dataIndex: 'actorName', width: 140 },
            { title: '动作', dataIndex: 'action', width: 180, render: (v: string) => <Tag>{v}</Tag> },
            {
              title: '主体',
              key: 'subject',
              width: 220,
              render: (_, r) => (
                <Typography.Text style={{ fontSize: 12 }}>
                  {r.subjectType} / {r.subjectId}
                </Typography.Text>
              ),
            },
            {
              title: '载荷',
              dataIndex: 'payload',
              render: (v: Record<string, unknown>) => (
                <Typography.Text code style={{ fontSize: 12, wordBreak: 'break-all' }}>
                  {Object.keys(v).length ? JSON.stringify(v) : '—'}
                </Typography.Text>
              ),
            },
          ]}
        />
      )}
    </Card>
  )
}
