// 「当前节点」摘要与详情（两个组件）：
// - CurrentNodeSummary：迭代页「当前节点摘要」卡中的可点行——阶段名、状态、
//   成果物/评审计数、最新成果物标题、阻塞计数；点击进入节点详情页。
// - CurrentNodeDetail：节点详情页主体——阻塞提示、按节点状态给出的操作
//   （开始 / 录入并提交成果物 / 通过·驳回）、最新成果物正文（就地 Markdown
//   渲染，不弹窗）、节点内全部成果物列表（点选后显示正文 + 评审意见）。
//
// 数据来源：实例视图（父组件已持有 wfQuery）与 next-action 阻塞项，均为服务端事实。
import { Button, Collapse, Empty, Space, Tag, Timeline, Typography } from 'antd'
import { useMemo, useState } from 'react'
import { Markdown, MarkdownInlineSummary } from '../../shared/markdown'
import { ACTIONABLE_STATUSES, fmtTime, memberNameOf, nodeStatusMeta } from '../../shared/status'
import { BlockerList } from '../../shared/ui'
import type {
  ApprovalRecord,
  ArtifactRef,
  ArtifactVersionView,
  Blocker,
  NodeStateView,
} from '../../shared/types'
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

/** 单条成果物正文：inline 走 Markdown 渲染，其余给外链与外部路径。 */
function ArtifactBody({ artifact, memberNames }: { artifact: ArtifactRef; memberNames?: Record<string, string> }) {
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
            提交人 {memberNameOf(memberNames ?? {}, artifact.submittedBy)}
          </Typography.Text>
        )}
      </Space>
      {/* 外部落点路径：让「这篇文档在哪」一眼可见，不必点开链接再找。 */}
      {artifact.externalPath && <ExternalPathBar path={artifact.externalPath} url={artifact.url} />}
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

/**
 * 外部落点路径条：以面包屑形式展示材料在外部系统里的位置。
 * outline 是「迭代 / 节点 / 文档标题」，git/svn 是仓库内文件路径。
 */
function ExternalPathBar({ path, url }: { path: string; url?: string }) {
  const segments = path.split(' / ').filter((s) => s.trim() !== '')
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 6,
        flexWrap: 'wrap',
        background: '#fafafa',
        border: '1px solid #f0f0f0',
        borderRadius: 4,
        padding: '4px 8px',
        fontSize: 12,
      }}
    >
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        外部路径
      </Typography.Text>
      {segments.map((seg, i) => (
        <span key={`${seg}-${i}`} style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
          {i > 0 && <Typography.Text type="secondary">/</Typography.Text>}
          <Typography.Text style={{ fontSize: 12 }}>{seg}</Typography.Text>
        </span>
      ))}
      {url && (
        <a href={url} target="_blank" rel="noreferrer" style={{ fontSize: 12 }}>
          打开
        </a>
      )}
    </div>
  )
}

/** 某节点的评审意见时间线（历史成果物点选后一并展示）。 */
function NodeApprovals({
  approvals,
  nodeId,
  memberNames,
}: {
  approvals?: ApprovalRecord[]
  nodeId: string
  memberNames?: Record<string, string>
}) {
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
              <Typography.Text strong>{memberNameOf(memberNames ?? {}, a.reviewerId)}</Typography.Text>
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

export interface CurrentNodeSummaryProps {
  node: NodeStateView
  /** 本实例全部成果物（组件内按 nodeId 过滤）。 */
  artifacts?: ArtifactRef[]
  approvals?: ApprovalRecord[]
  blockers?: Blocker[]
  /** 点击进入节点详情页。 */
  onClick: () => void
}

/** 摘要行（迭代页「当前节点摘要」卡内）：整行可点，细节在节点详情页展示。 */
export function CurrentNodeSummary({
  node,
  artifacts,
  approvals,
  blockers,
  onClick,
}: CurrentNodeSummaryProps) {
  const mine = sortByTimeDesc((artifacts ?? []).filter((a) => a.nodeId === node.id))
  const latest = mine[0]
  const approvalCount = (approvals ?? []).filter((a) => a.nodeId === node.id).length
  const meta = nodeStatusMeta[node.status] ?? { label: node.status, color: 'default' }

  return (
    <div
      onClick={onClick}
      style={{
        cursor: 'pointer',
        padding: '8px 12px',
        borderRadius: 6,
        border: '1px solid #f0f0f0',
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: 'center',
        gap: 8,
      }}
    >
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
      <Button
        size="small"
        type="link"
        onClick={(e) => {
          e.stopPropagation()
          onClick()
        }}
      >
        查看详情
      </Button>
    </div>
  )
}

export interface CurrentNodeDetailProps {
  scope: 'task' | 'iteration'
  workItemId: string
  node: NodeStateView
  /** 本实例全部成果物（组件内按 nodeId 过滤）。 */
  artifacts?: ArtifactRef[]
  approvals?: ApprovalRecord[]
  /** 本实例被替换下来的历史材料（组件内按 nodeId 过滤）。 */
  artifactHistory?: ArtifactVersionView[]
  blockers?: Blocker[]
  /** userId → 展示名（评审人与提交人显示用）。 */
  memberNames?: Record<string, string>
  /** 开始节点动作（ready 状态时展示「开始阶段」按钮）。 */
  onStart?: () => void
  starting?: boolean
  onDone: () => void
}

/**
 * 节点详情（节点详情页主体）：
 * 自上而下：阻塞提示 → 节点操作（开始/提交/评审）→ 最新成果物正文 →
 * 节点内全部成果物（点选看正文 + 评审意见）。
 */
export function CurrentNodeDetail({
  scope,
  workItemId,
  node,
  artifacts,
  approvals,
  artifactHistory,
  blockers,
  memberNames,
  onStart,
  starting,
  onDone,
}: CurrentNodeDetailProps) {
  const [selected, setSelected] = useState<number | null>(null)

  // 只取本节点的成果物，按时间倒序：第 0 条即「最新」。
  const mine = useMemo(
    () => sortByTimeDesc((artifacts ?? []).filter((a) => a.nodeId === node.id)),
    [artifacts, node.id],
  )
  const latest = mine[0]

  // 历史轮次：被替换下来的旧版材料（按 kind 分组后按替换时间倒序展示）。
  const history = useMemo(
    () => (artifactHistory ?? []).filter((a) => a.nodeId === node.id),
    [artifactHistory, node.id],
  )

  return (
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
                该阶段尚未开始，请先开始阶段后再操作。
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
              <ReviewActions scope={scope} workItemId={workItemId} nodeId={node.id} onDone={onDone} />
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
          {!ACTIONABLE_STATUSES.has(node.status) && node.status !== 'passed' && (
            <Typography.Text type="secondary">该阶段暂不可操作。</Typography.Text>
          )}
        </div>
      </div>

      {/* 最新成果物正文：直接就地展示，不弹窗 */}
      <div>
        <Typography.Text strong>最新成果物</Typography.Text>
        <div style={{ marginTop: 8 }}>
          {latest ? (
            <ArtifactBody artifact={latest} memberNames={memberNames} />
          ) : (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="该阶段尚无成果物" />
          )}
        </div>
      </div>

      {/* 节点内全部成果物 + 评审意见 */}
      <div>
        <Typography.Text strong>节点内成果物列表</Typography.Text>
        <div style={{ marginTop: 8 }}>
          {mine.length === 0 ? (
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              该阶段暂无成果物。
            </Typography.Text>
          ) : (
            <Space direction="vertical" size={6} style={{ width: '100%' }}>
              {mine.map((a, i) => {
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
                        ) : a.externalPath ? (
                          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                            {a.externalPath}
                          </Typography.Text>
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
                        <ArtifactBody artifact={a} memberNames={memberNames} />
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
              <NodeApprovals approvals={approvals} nodeId={node.id} memberNames={memberNames} />
            </div>
          </div>
        </div>
      </div>

      {/* 历史轮次：被替换下来的旧版材料。默认折叠，避免干扰当前轮次阅读。 */}
      <div>
        <Typography.Text strong>历史轮次</Typography.Text>
        <Typography.Text type="secondary" style={{ fontSize: 12, marginLeft: 8 }}>
          （材料被替换后自动归档，供回看上一版）
        </Typography.Text>
        <div style={{ marginTop: 8 }}>
          {history.length === 0 ? (
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              该阶段暂无历史轮次（材料首次提交或尚未被替换）。
            </Typography.Text>
          ) : (
            <Collapse
              size="small"
              items={history.map((h, i) => ({
                key: `${h.artifactId}-${i}`,
                label: (
                  <Space wrap size={[6, 2]}>
                    <Tag color="orange">{h.kind}</Tag>
                    <Typography.Text style={{ fontSize: 12 }}>
                      提交 {fmtTime(h.createdAt)}
                    </Typography.Text>
                    {h.replacedAt && (
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        · 于 {fmtTime(h.replacedAt)} 被替换
                      </Typography.Text>
                    )}
                    {h.replacedBy && (
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        · 替换人 {memberNameOf(memberNames ?? {}, h.replacedBy)}
                      </Typography.Text>
                    )}
                    {h.content ? (
                      <MarkdownInlineSummary text={h.content} max={40} />
                    ) : h.externalPath ? (
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        {h.externalPath}
                      </Typography.Text>
                    ) : null}
                  </Space>
                ),
                children: <ArtifactBody artifact={h} memberNames={memberNames} />,
              }))}
            />
          )}
        </div>
      </div>
    </Space>
  )
}
/** 判定节点是否处于「当前可操作」状态（与 ACTIONABLE_STATUSES 一致，供父组件筛选）。 */
export function isCurrentNode(node: NodeStateView): boolean {
  return ACTIONABLE_STATUSES.has(node.status)
}
