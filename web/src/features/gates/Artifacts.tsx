// 门禁材料：查看列表 + 提交表单。被任务详情与迭代详情复用。
// 材料是「引用」不是内容：inline 显示文本，其余渲染为可点击链接，
// 外部系统不可达时不阻断评审（console.md 降级要求）。
import { App, Button, Card, Empty, Form, Input, Select, Space, Tag, Typography } from 'antd'
import { useState } from 'react'
import { apiPost } from '../../shared/api'
import { ARTIFACT_KINDS, ARTIFACT_PROVIDERS } from '../../shared/status'
import type { ArtifactRef, NodeOpResult } from '../../shared/types'

// ---- 查看 ----

export function ArtifactList({ artifacts }: { artifacts?: ArtifactRef[] }) {
  if (!artifacts || artifacts.length === 0) {
    return (
      <Empty
        image={Empty.PRESENTED_IMAGE_SIMPLE}
        description="暂无材料记录（若后端尚未下发材料明细，可在下方提交表单补充）"
      />
    )
  }
  return (
    <Space direction="vertical" style={{ width: '100%' }} size={8}>
      {artifacts.map((a, i) => (
        <Card key={`${a.nodeId}-${a.kind}-${i}`} size="small">
          <Space wrap size={[8, 4]}>
            <Tag color="orange">{a.kind}</Tag>
            <Tag>{a.provider}</Tag>
            {a.title && <Typography.Text strong>{a.title}</Typography.Text>}
          </Space>
          {a.url && (
            <div>
              <a href={a.url} target="_blank" rel="noreferrer">
                {a.url}
              </a>
            </div>
          )}
          {a.content && (
            <Typography.Paragraph
              style={{ whiteSpace: 'pre-wrap', marginTop: 8, marginBottom: 0 }}
              ellipsis={{ rows: 4, expandable: true, symbol: '展开全文' }}
            >
              {a.content}
            </Typography.Paragraph>
          )}
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            节点 {a.nodeId}
            {a.submittedBy ? ` · 提交人 ${a.submittedBy}` : ''}
          </Typography.Text>
        </Card>
      ))}
    </Space>
  )
}

// ---- 提交 ----

interface ArtifactFormValues {
  kind: string
  provider: string
  title?: string
  url?: string
  content?: string
}

interface SubmitValues {
  artifacts: ArtifactFormValues[]
  note?: string
}

interface SubmitFormProps {
  scope: 'task' | 'iteration'
  workItemId: string
  nodeId: string
  nodeTitle: string
  /** submit：随节点提交（含说明与材料）；attach：单独补挂材料（仅任务）。 */
  mode: 'submit' | 'attach'
  onDone?: () => void
}

function ArtifactFields({ prefix }: { prefix?: number[] }) {
  return (
    <>
      <Form.Item
        name={[...(prefix ?? []), 'kind']}
        label="材料类型"
        rules={[{ required: true, message: '请选择材料类型' }]}
      >
        <Select options={ARTIFACT_KINDS} placeholder="如 spec / pr / test_plan" />
      </Form.Item>
      <Form.Item
        name={[...(prefix ?? []), 'provider']}
        label="来源 provider"
        initialValue="inline"
        rules={[{ required: true, message: '请选择来源' }]}
      >
        <Select options={ARTIFACT_PROVIDERS} />
      </Form.Item>
      <Form.Item name={[...(prefix ?? []), 'title']} label="标题（可选）">
        <Input />
      </Form.Item>
      <Form.Item noStyle shouldUpdate={(prev, cur) => prev !== cur}>
        {({ getFieldValue }) => {
          const provider: string | undefined = prefix
            ? getFieldValue(['artifacts', ...(prefix ?? []), 'provider'])
            : getFieldValue('provider')
          if (provider === 'inline') {
            return (
              <Form.Item
                name={[...(prefix ?? []), 'content']}
                label="内容（inline Markdown）"
                rules={[{ required: true, message: 'inline 材料必须填写内容' }]}
              >
                <Input.TextArea rows={3} />
              </Form.Item>
            )
          }
          return (
            <Form.Item
              name={[...(prefix ?? []), 'url']}
              label="材料 URL（绝对地址）"
              rules={[
                { required: true, message: '非 inline 材料必须填写 URL' },
                { pattern: /^https?:\/\//, message: '需为 http/https 绝对地址' },
              ]}
            >
              <Input placeholder="https://…" />
            </Form.Item>
          )
        }}
      </Form.Item>
    </>
  )
}

export function ArtifactSubmitForm({ scope, workItemId, nodeId, nodeTitle, mode, onDone }: SubmitFormProps) {
  const { message } = App.useApp()
  const [form] = Form.useForm<SubmitValues | ArtifactFormValues>()
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    const values = await form.validateFields()
    setBusy(true)
    try {
      if (mode === 'attach') {
        await apiPost<NodeOpResult>(`/tasks/${workItemId}/artifacts`, {
          nodeId,
          artifact: values as ArtifactFormValues,
        })
        message.success('材料已补挂')
      } else {
        const v = values as SubmitValues
        await apiPost<NodeOpResult>(`/${scope}s/${workItemId}/steps/submit`, {
          nodeId,
          expectedVersion: 0,
          artifacts: v.artifacts ?? [],
          note: v.note ?? '',
        })
        message.success(`节点「${nodeTitle}」已提交`)
      }
      form.resetFields()
      onDone?.()
    } catch (err) {
      // validateFields 抛出的是表单错误对象，已由 Form.Item 展示。
      if (err instanceof Error) message.error(err.message)
    } finally {
      setBusy(false)
    }
  }

  if (mode === 'attach') {
    return (
      <Form form={form} layout="vertical" onFinish={submit}>
        <Typography.Text type="secondary" style={{ display: 'block', marginBottom: 8 }}>
          向节点「{nodeTitle}」补挂材料：替换已进入评审的材料会作废该节点既有审批。
        </Typography.Text>
        <ArtifactFields />
        <Button type="primary" htmlType="submit" loading={busy}>
          补挂材料
        </Button>
      </Form>
    )
  }

  return (
    <Form form={form} layout="vertical" onFinish={submit}>
      <Typography.Text type="secondary" style={{ display: 'block', marginBottom: 8 }}>
        提交节点「{nodeTitle}」：提交后无门禁直接通过；有门禁则进入待评审。
      </Typography.Text>
      <Form.List name="artifacts" initialValue={[{ provider: 'inline' }]}>
        {(fields, { add, remove }) => (
          <>
            {fields.map((field) => (
              <Card
                key={field.key}
                size="small"
                title={`材料 ${field.name + 1}`}
                extra={
                  fields.length > 1 ? (
                    <Button size="small" type="text" danger onClick={() => remove(field.name)}>
                      移除
                    </Button>
                  ) : undefined
                }
                style={{ marginBottom: 8 }}
              >
                <ArtifactFields prefix={[field.name]} />
              </Card>
            ))}
            <Button type="dashed" onClick={() => add({ provider: 'inline' })} block>
              + 添加材料
            </Button>
          </>
        )}
      </Form.List>
      <Form.Item name="note" label="提交说明（可选）" style={{ marginTop: 12 }}>
        <Input.TextArea rows={2} />
      </Form.Item>
      <Button type="primary" htmlType="submit" loading={busy}>
        提交节点
      </Button>
    </Form>
  )
}
