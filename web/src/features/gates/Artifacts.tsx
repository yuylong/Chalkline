// 门禁材料：按步骤组织的成果物浏览器 + 提交表单。被任务详情、迭代详情
// 与审批队列复用。材料是「引用」不是内容：inline 显示文本，其余渲染为可
// 点击链接，外部系统不可达时不阻断评审（console.md 降级要求）。
import { useQuery } from '@tanstack/react-query'
import {
  App,
  Button,
  Card,
  Descriptions,
  Empty,
  Form,
  Input,
  Modal,
  Select,
  Space,
  Tag,
  Typography,
} from 'antd'
import { useState } from 'react'
import { apiGet, apiPost } from '../../shared/api'
import { Markdown, MarkdownInlineSummary } from '../../shared/markdown'
import { ARTIFACT_KINDS, ARTIFACT_PROVIDERS, memberNameMap, memberNameOf, nodeStatusMeta } from '../../shared/status'
import { ApiErrorBanner, PageLoading } from '../../shared/ui'
import type {
  ArtifactRef,
  InstanceView,
  IterationDetail,
  ListEnvelope,
  MemberView,
  NodeOpResult,
  NodeStateView,
  TaskDetail,
} from '../../shared/types'

// ---- 成果物浏览器（按步骤组织）----

interface ArtifactDetail {
  artifact: ArtifactRef
  nodeName?: string
}

/** 单条成果物详情弹窗：元数据 + inline 全文 / 外链 + 提交时点快照。 */
function ArtifactDetailModal({
  detail,
  memberNames,
  onClose,
}: {
  detail: ArtifactDetail | null
  memberNames?: Record<string, string>
  onClose: () => void
}) {
  const a = detail?.artifact
  return (
    <Modal
      open={a !== undefined && a !== null}
      title={a ? `成果物 · ${detail?.nodeName ?? a.nodeId}` : ''}
      onCancel={onClose}
      footer={null}
      width={680}
    >
      {a && (
        <Space direction="vertical" size={12} style={{ width: '100%' }}>
          <Descriptions
            size="small"
            column={2}
            items={[
              { key: 'kind', label: '类型', children: <Tag color="orange">{a.kind}</Tag> },
              { key: 'provider', label: '来源', children: <Tag>{a.provider}</Tag> },
              ...(a.title ? [{ key: 'title', label: '标题', children: a.title }] : []),
              ...(a.externalId ? [{ key: 'externalId', label: '外部 ID', children: a.externalId }] : []),
              { key: 'node', label: '所属步骤', children: detail?.nodeName ?? a.nodeId },
              {
                key: 'submittedBy',
                label: '提交人',
                children: a.submittedBy ? memberNameOf(memberNames ?? {}, a.submittedBy) : '—',
              },
            ]}
          />
          {a.url && (
            <a href={a.url} target="_blank" rel="noreferrer">
              {a.url}
            </a>
          )}
          {a.content && (
            <div>
              <Typography.Text type="secondary" style={{ fontSize: 12, display: 'block', marginBottom: 4 }}>
                内容（Markdown 渲染）
              </Typography.Text>
              <div style={{ border: '1px solid #f0f0f0', borderRadius: 6, padding: '8px 12px' }}>
                <Markdown text={a.content} />
              </div>
              <Button
                size="small"
                type="text"
                style={{ marginTop: 4 }}
                onClick={() => navigator.clipboard?.writeText(a.content ?? '')}
              >
                复制原文
              </Button>
            </div>
          )}
          {a.snapshot && (
            <div>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                提交时点快照（评审看到的是提交时的事实）
              </Typography.Text>
              <pre
                style={{
                  margin: '4px 0 0',
                  padding: 8,
                  background: '#fafafa',
                  borderRadius: 4,
                  fontSize: 12,
                  maxHeight: 240,
                  overflow: 'auto',
                }}
              >
                {JSON.stringify(a.snapshot, null, 2)}
              </pre>
            </div>
          )}
        </Space>
      )}
    </Modal>
  )
}

export interface ArtifactBrowserProps {
  artifacts?: ArtifactRef[]
  /** 实例节点（提供步骤名称与模板顺序）。 */
  nodes?: NodeStateView[]
  /** 审批场景：待审步骤排最前并高亮。 */
  focusNodeId?: string
  /** userId → 显示名（可选）。 */
  memberNames?: Record<string, string>
}

/** 成果物浏览器：按工作流步骤分组，逐条点开查看。 */
export function ArtifactBrowser({ artifacts, nodes, focusNodeId, memberNames }: ArtifactBrowserProps) {
  const [detail, setDetail] = useState<ArtifactDetail | null>(null)

  if (!artifacts || artifacts.length === 0) {
    return (
      <Empty
        image={Empty.PRESENTED_IMAGE_SIMPLE}
        description="暂无成果物（节点提交材料后会在此按步骤展示）"
      />
    )
  }

  const names = new Map<string, string>()
  const order = new Map<string, number>()
  for (const [i, n] of (nodes ?? []).entries()) {
    names.set(n.id, n.name)
    order.set(n.id, i)
  }
  const groups = new Map<string, ArtifactRef[]>()
  for (const a of artifacts) {
    const list = groups.get(a.nodeId) ?? []
    list.push(a)
    groups.set(a.nodeId, list)
  }
  const nodeIds = [...groups.keys()].sort((x, y) => {
    if (focusNodeId) {
      if (x === focusNodeId) return -1
      if (y === focusNodeId) return 1
    }
    return (order.get(x) ?? Number.MAX_SAFE_INTEGER) - (order.get(y) ?? Number.MAX_SAFE_INTEGER)
  })

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {nodeIds.map((nodeId) => {
        const node = (nodes ?? []).find((n) => n.id === nodeId)
        const meta = node ? nodeStatusMeta[node.status] : undefined
        const focused = nodeId === focusNodeId
        return (
          <Card
            key={nodeId}
            size="small"
            title={
              <Space size={6}>
                <span>{names.get(nodeId) ?? nodeId}</span>
                {meta && <Tag color={meta.color}>{meta.label}</Tag>}
                {focused && <Tag color="red">待评审</Tag>}
              </Space>
            }
          >
            <Space direction="vertical" size={6} style={{ width: '100%' }}>
              {(groups.get(nodeId) ?? []).map((a, i) => (
                <div
                  key={`${a.kind}-${i}`}
                  style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8 }}
                >
                  <Space wrap size={[8, 4]}>
                    <Tag color="orange">{a.kind}</Tag>
                    <Tag>{a.provider}</Tag>
                    {a.title || a.url || a.externalId ? (
                      <Typography.Text ellipsis style={{ maxWidth: 360 }}>
                        {a.title || a.url || a.externalId}
                      </Typography.Text>
                    ) : a.content ? (
                      <MarkdownInlineSummary text={a.content} />
                    ) : (
                      <Typography.Text type="secondary">—</Typography.Text>
                    )}
                    {a.submittedBy && (
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        {memberNameOf(memberNames ?? {}, a.submittedBy)}
                      </Typography.Text>
                    )}
                  </Space>
                  <Button
                    size="small"
                    type="link"
                    onClick={() => setDetail({ artifact: a, nodeName: names.get(nodeId) })}
                  >
                    查看
                  </Button>
                </div>
              ))}
            </Space>
          </Card>
        )
      })}
      <ArtifactDetailModal
        detail={detail}
        memberNames={memberNames}
        onClose={() => setDetail(null)}
      />
    </div>
  )
}

/** 审批队列入口：按工作项拉取实例视图，弹窗内按步骤浏览成果物。 */
export function WorkItemArtifactsModal({
  workItemType,
  workItemId,
  focusNodeId,
  open,
  onClose,
}: {
  workItemType: string // task | iteration
  workItemId: string
  focusNodeId?: string
  open: boolean
  onClose: () => void
}) {
  const isTask = workItemType === 'task'
  // 与详情页共用缓存键：SSE 事件失效与既有缓存直接复用。
  const { data, isPending, isError, error } = useQuery({
    queryKey: [isTask ? 'task-workflow' : 'iteration-workflow', workItemId],
    queryFn: () =>
      apiGet<InstanceView>(`${isTask ? '/tasks' : '/iterations'}/${workItemId}/workflow`),
    enabled: open,
  })
  // 提交人展示需要成员名：先取工作项拿 productId，再取产品成员（评审人必为产品成员）。
  const { data: item } = useQuery({
    queryKey: [isTask ? 'task' : 'iteration', workItemId],
    queryFn: (): Promise<TaskDetail | IterationDetail> =>
      isTask
        ? apiGet<TaskDetail>(`/tasks/${workItemId}`)
        : apiGet<IterationDetail>(`/iterations/${workItemId}`),
    enabled: open,
  })
  const productId = item?.productId ?? ''
  const { data: members } = useQuery({
    queryKey: ['members', productId],
    queryFn: () => apiGet<ListEnvelope<MemberView>>(`/products/${productId}/members`),
    enabled: open && productId !== '',
  })
  const memberNames = memberNameMap(members?.items ?? [])
  return (
    <Modal
      open={open}
      onCancel={onClose}
      footer={null}
      width={720}
      title="成果物（按步骤组织）"
    >
      {isPending ? (
        <PageLoading tip="加载成果物…" />
      ) : isError ? (
        <ApiErrorBanner error={error} />
      ) : (
        <ArtifactBrowser
          artifacts={data?.artifacts}
          nodes={data?.nodes}
          focusNodeId={focusNodeId}
          memberNames={memberNames}
        />
      )}
    </Modal>
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
