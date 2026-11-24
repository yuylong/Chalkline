// 迭代页「当前节点」面板：默认只显示摘要（阶段名 + 状态 + 阻塞/成果物计数），
// 点开后才展开操作细节——录入成果物、最新成果物正文（就地 Markdown 渲染，
// 不弹窗）、历史成果物列表（点选后显示正文 + 该版本的评审意见）。
//
// 数据来源：实例视图（父组件已持有 wfQuery）与 next-action 阻塞项，均为服务端事实。
import { Button, Card, Empty, Space, Tag, Timeline, Typography } from 'antd'
import { useMemo, useState } from 'react'
import { Markdown, MarkdownInlineSummary } from '../../shared/markdown'
import { ACTIONABLE_STATUSES, fmtTime, nodeStatusMeta } from '../../shared/status'
import { BlockerList } from '../../shared/ui'
import type { ApprovalRecord, ArtifactRef, Blocker, NodeStateView } from '../../shared/types'
import { ArtifactSubmitForm } from './Artifacts'
import { ReviewActions } from './ReviewAction'

/** 按提交时间倒序（缺 createdAt 的视为最旧，保持稳定）。 */
function sortByTimeDesc(list: ArtifactRef[]): ArtifactRef[] {
  return [...list].sort((a, b) => {
    const ta = a.createdAt ? Date.parse(a.createdAt) : 0
    const tb = b.createdAt ? Date.parse(b.createdAt) : 0
    return tb - ta
  })
}

/** 单条成果物正文：inline 走 Markdown 渲染，其余给外链。 */
function ArtifactBody({ artifact }: { artifact: ArtifactRef }) {
  return (
    <Space direction="vertical" size={8} style={{ width: '100%' }}>
      <Space wrap size={[8, 4]}>
        <Tag color="orange">{artifact.kind}</Tag>
        <Tag>{artifact.provider}</Tag>
        {artifact.createdAt && (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {fmtTime(artifact.createdAt)}
          </Typography.Text>
        )}
        {artifact.submittedBy && (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            提交人 {artifact.submittedBy}
          </Typography.Text>
        )}
      </Space>
      {artifact.url && (
        <a href={artifact.url} target="_blank" rel="noreferrer">
          {artifact.url}
        </a>
      )}
      {artifact.content ? (
        <div style={{ border: '1px solid #f0f0f0', borderRadius: 6, padding: '10px 14px' }}>
          <Markdown text={artifact.content} />
        </div>
      ) : (
        !artifact.url && <Typography.Text type="secondary">（无正文：外部引用，请点上方链接查看）</Typography.Text>
      )}
      {artifact.snapshot && Object.keys(artifact.snapshot).length > 0 && (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          提交时点快照：{JSON.stringify(artifact.snapshot)}
        </Typography.Text>
      )}
    </Space>
  )
}

/** 某节点的评审意见时间线（历史成果物点选后一并展示）。 */
function NodeApprovals({ approvals, nodeId }: { approvals?: ApprovalRecord[]; nodeId: string }) {
  const list = (approvals ?? []).filter((a) => a.nodeId === nodeId)
  if (list.length === 0) {
    return (
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        该阶段暂无评审意见。
      </Typography.Text>
    )
  }
  return (
    <Timeline
      items={list.map((a, i) => ({
        key: `${a.reviewerId}-${i}`,
        color: a.decision === 'approve' ? 'green' : 'red',
        children: (
          <div>
            <Space size={6}>
              <Typography.Text strong>{a.reviewerId}</Typography.Text>
              <Tag color={a.decision === 'approve' ? 'green' : 'red'}>
                {a.decision === 'approve' ? '通过' : '驳回'}
              </Tag>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                {fmtTime(a.createdAt)}
              </Typography.Text>
            </Space>
            {a.comment && <div style={{ whiteSpace: 'pre-wrap' }}>{a.comment}</div>}
          </div>
        ),
      }))}
    />
  )
}

export interface CurrentNodePanelProps {
  scope: 'task' | 'iteration'
  workItemId: string
  node: NodeStateView
  /** 本实例全部成果物（面板内按 nodeId 过滤）。 */
  artifacts?: ArtifactRef[]
  approvals?: ApprovalRecord[]
  blockers?: Blocker[]
  /** 审批场景：默认展开并高亮（打开即看到待审内容）。 */
  defaultExpanded?: boolean
  /** 开始节点动作（ready 状态时展示「开始阶段」按钮）。 */
  onStart?: () => void
  starting?: boolean
  onDone: () => void
}

/**
 * 当前节点面板：
 * 摘要行（阶段名/状态/计数）→ 展开后是操作区。
 * 展开区自上而下：阻塞提示 → 节点操作（开始/提交/评审）→ 最新成果物正文 →
 * 历史成果物（点选看正文 + 评审意见）。
 */
export function CurrentNodePanel({
  scope,
  workItemId,
  node,
  artifacts,
  approvals,
  blockers,
  defaultExpanded,
  onStart,
  starting,
  onDone,
}: CurrentNodePanelProps) {
  const [open, setOpen] = useState(defaultExpanded ?? false)
  const [selected, setSelected] = useState<number | null>(null)

  // 只取本节点的成果物，按时间倒序：第 0 条即「最新」。
  const mine = useMemo(
    () => sortByTimeDesc((artifacts ?? []).filter((a) => a.nodeId === node.id)),
    [artifacts, node.id],
  )
  const latest = mine[0]
  const history = mine.slice(1)
  const approvalCount = (approvals ?? []).filter((a) => a.nodeId === node.id).length
  const meta = nodeStatusMeta[node.status] ?? { label: node.status, color: 'default' }

  const summary = (
    <Space wrap size={[8, 4]}>
      <Typography.Text strong>{node.name}</Typography.Text>
      <Tag color={meta.color}>{meta.label}</Tag>
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        成果物 {mine.length}
        {approvalCount > 0 ? ` · 评审 ${approvalCount}` : ''}
        {node.enteredAt ? ` · 进入 ${fmtTime(node.enteredAt)}` : ''}
      </Typography.Text>
      {latest && (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          最新：{latest.title || latest.kind}
        </Typography.Text>
      )}
      {blockers && blockers.length > 0 && <Tag color="error">受阻 {blockers.length}</Tag>}
    </Space>
  )

  return (
    <Card
      size="small"
      style={{ borderColor: open ? '#1677ff' : undefined }}
      title={
        <div onClick={() => setOpen((v) => !v)} style={{ cursor: 'pointer' }}>
          {summary}
        </div>
      }
      extra={
        <Button size="small" type="link" onClick={() => setOpen((v) => !v)}>
          {open ? '收起' : '展开详情'}
        </Button>
      }
    >
      {open && (
        <Space direction="vertical" size={14} style={{ width: '100%' }}>
          <BlockerList blockers={blockers} />

          {/* 操作区：按节点状态给出可执行动作 */}
          <div>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              操作
            </Typography.Text>
            <div style={{ marginTop: 6 }}>
              {node.status === 'ready' && (
                onStart ? (
                  <Button type="primary" size="small" loading={starting} onClick={onStart}>
                    开始阶段
                  </Button>
                ) : (
                  <Typography.Text type="secondary">
                    该阶段尚未开始，请先在泳道或阶段操作中点击「开始」。
                  </Typography.Text>
                )
              )}
              {(node.status === 'in_progress' || node.status === 'rejected') && (
                <ArtifactSubmitForm
                  scope={scope}
                  workItemId={workItemId}
                  nodeId={node.id}
                  nodeTitle={node.name}
                  mode="submit"
                  onDone={onDone}
                />
              )}
              {node.status === 'awaiting_review' && (
                <Space direction="vertical" size={12} style={{ width: '100%' }}>
                  <ReviewActions
                    scope={scope}
                    workItemId={workItemId}
                    nodeId={node.id}
                    onDone={onDone}
                  />
                  {scope === 'task' && (
                    <ArtifactSubmitForm
                      scope={scope}
                      workItemId={workItemId}
                      nodeId={node.id}
                      mode="attach"
                      nodeTitle={node.name}
                      onDone={onDone}
                    />
                  )}
                </Space>
              )}
              {node.status === 'passed' && (
                <Typography.Text type="secondary">该阶段已通过，不可再提交。</Typography.Text>
              )}
            </div>
          </div>

          {/* 最新成果物正文：直接就地展示，不弹窗 */}
          <div>
            <Typography.Text strong>最新成果物</Typography.Text>
            <div style={{ marginTop: 8 }}>
              {latest ? (
                <ArtifactBody artifact={latest} />
              ) : (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="该阶段尚无成果物" />
              )}
            </div>
          </div>

          {/* 历史成果物 + 评审意见 */}
          <div>
            <Typography.Text strong>历史记录</Typography.Text>
            <div style={{ marginTop: 8 }}>
              {history.length === 0 ? (
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  暂无更早的成果物版本。
                </Typography.Text>
              ) : (
                <Space direction="vertical" size={6} style={{ width: '100%' }}>
                  {history.map((a, i) => {
                    const active = selected === i
                    return (
                      <div key={`${a.kind}-${i}`}>
                        <div
                          onClick={() => setSelected(active ? null : i)}
                          style={{
                            cursor: 'pointer',
                            padding: '4px 8px',
                            borderRadius: 4,
                            background: active ? '#e6f4ff' : undefined,
                            display: 'flex',
                            justifyContent: 'space-between',
                            gap: 8,
                          }}
                        >
                          <Space wrap size={[6, 2]}>
                            <Tag color="orange">{a.kind}</Tag>
                            <Typography.Text style={{ fontSize: 12 }}>
                              {fmtTime(a.createdAt)}
                            </Typography.Text>
                            {a.content ? (
                              <MarkdownInlineSummary text={a.content} max={60} />
                            ) : (
                              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                                {a.title || a.url || '外部引用'}
                              </Typography.Text>
                            )}
                          </Space>
                          <Button size="small" type="link">
                            {active ? '收起' : '查看'}
                          </Button>
                        </div>
                        {active && (
                          <div style={{ padding: '8px 8px 0' }}>
                            <ArtifactBody artifact={a} />
                          </div>
                        )}
                      </div>
                    )
                  })}
                </Space>
              )}
              <div style={{ marginTop: 12 }}>
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  评审意见（本阶段）
                </Typography.Text>
                <div style={{ marginTop: 6 }}>
                  <NodeApprovals approvals={approvals} nodeId={node.id} />
                </div>
              </div>
            </div>
          </div>
        </Space>
      )}
    </Card>
  )
}

/** 判定节点是否处于「当前可操作」状态（与 ACTIONABLE_STATUSES 一致，供父组件筛选）。 */
export function isCurrentNode(node: NodeStateView): boolean {
  return ACTIONABLE_STATUSES.has(node.status)
}
