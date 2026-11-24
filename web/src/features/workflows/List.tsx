// 工作流模板管理：列表、YAML 编辑（新建/更新）、导入、导出、归档。
// 校验在后端（YAML 解析 + DAG/门禁校验），失败信息直接展示；前端只做文本编辑。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { App, Button, Card, Input, Modal, Popconfirm, Select, Space, Table, Tag, Typography } from 'antd'
import { useState } from 'react'
import { apiGet, apiPatch, apiPost } from '../../shared/api'
import type { ListEnvelope, WorkflowExport, WorkflowView } from '../../shared/types'
import { ApiErrorBanner } from '../../shared/ui'

const YAML_TEMPLATE = `# 工作流模板示例：单终态任务流程。保存时服务端会做 DAG 与门禁校验。
id: wf-custom
name: 自定义任务流程
scope: task
version: 1
settings:
  require_children_done: false
  allow_skip_nodes: false
nodes:
  - { id: start, name: 开始, type: normal }
  - { id: done, name: 完成, type: terminal }
edges:
  - { from: start, to: done }
`

type EditorMode = 'create' | 'edit' | 'import' | 'export'

interface EditorState {
  mode: EditorMode
  id?: string
  title?: string
}

const MODE_TITLE: Record<EditorMode, string> = {
  create: '新建工作流模板',
  edit: '编辑工作流模板',
  import: '导入工作流模板',
  export: '导出工作流模板',
}

export default function WorkflowList() {
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const [scope, setScope] = useState<string | undefined>(undefined)
  const [editor, setEditor] = useState<EditorState | null>(null)
  const [yaml, setYaml] = useState('')
  const [loadingYaml, setLoadingYaml] = useState(false)

  const { data, isPending, isError, error } = useQuery({
    queryKey: ['workflows', scope ?? 'all'],
    queryFn: () =>
      apiGet<ListEnvelope<WorkflowView>>(`/workflows${scope ? `?scope=${encodeURIComponent(scope)}` : ''}`),
  })

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['workflows'] })

  const saveMut = useMutation({
    mutationFn: (v: { mode: EditorMode; id?: string; yaml: string }) =>
      v.mode === 'edit' && v.id
        ? apiPatch<WorkflowView>(`/workflows/${v.id}`, { yaml: v.yaml })
        : v.mode === 'import'
          ? apiPost<WorkflowView>('/workflows/import', { yaml: v.yaml })
          : apiPost<WorkflowView>('/workflows', { yaml: v.yaml }),
    onSuccess: () => {
      message.success('模板已保存')
      setEditor(null)
      void invalidate()
    },
    onError: (err: Error) => message.error(err.message),
  })

  const archiveMut = useMutation({
    mutationFn: (id: string) => apiPost<{ status: string }>(`/workflows/${id}/archive`),
    onSuccess: () => {
      message.success('模板已归档')
      void invalidate()
    },
    onError: (err: Error) => message.error(err.message),
  })

  const openEditor = async (next: EditorState) => {
    setEditor(next)
    setYaml('')
    if (next.mode === 'create') {
      setYaml(YAML_TEMPLATE)
      return
    }
    if (!next.id) {
      setYaml('') // 导入：留空由用户粘贴
      return
    }
    setLoadingYaml(true)
    try {
      const out = await apiGet<WorkflowExport>(`/workflows/${next.id}/export`)
      setYaml(out.yaml)
    } catch (err) {
      message.error(err instanceof Error ? err.message : '模板加载失败')
      setEditor(null)
    } finally {
      setLoadingYaml(false)
    }
  }

  const download = () => {
    const blob = new Blob([yaml], { type: 'text/yaml;charset=utf-8' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = `${editor?.id ?? 'workflow'}.yaml`
    a.click()
    URL.revokeObjectURL(a.href)
  }

  const readOnly = editor?.mode === 'export'

  return (
    <Card
      title="工作流模板"
      extra={
        <Space>
          <Select
            allowClear
            placeholder="全部作用域"
            style={{ width: 160 }}
            value={scope}
            onChange={(v) => setScope(v)}
            options={[
              { value: 'task', label: '任务（task）' },
              { value: 'iteration', label: '迭代（iteration）' },
            ]}
          />
          <Button onClick={() => openEditor({ mode: 'import' })}>导入 YAML</Button>
          <Button type="primary" onClick={() => openEditor({ mode: 'create' })}>
            新建模板
          </Button>
        </Space>
      }
    >
      {isError ? (
        <ApiErrorBanner error={error} />
      ) : (
        <Table<WorkflowView>
          rowKey="id"
          loading={isPending}
          dataSource={data?.items ?? []}
          pagination={false}
          columns={[
            { title: '模板 ID', dataIndex: 'id', width: 200, ellipsis: true },
            { title: '名称', dataIndex: 'name', width: 220 },
            {
              title: '作用域',
              dataIndex: 'scope',
              width: 110,
              render: (v: string) => <Tag color={v === 'task' ? 'blue' : 'purple'}>{v}</Tag>,
            },
            { title: '版本', dataIndex: 'version', width: 80, render: (v: number) => `v${v}` },
            {
              title: '状态',
              dataIndex: 'status',
              width: 100,
              render: (v: string) =>
                v === 'active' ? <Tag color="green">启用</Tag> : <Tag>已归档</Tag>,
            },
            {
              title: '操作',
              key: 'op',
              width: 240,
              render: (_, r) => (
                <Space>
                  <Button size="small" onClick={() => openEditor({ mode: 'edit', id: r.id, title: r.name })}>
                    编辑
                  </Button>
                  <Button size="small" onClick={() => openEditor({ mode: 'export', id: r.id, title: r.name })}>
                    导出
                  </Button>
                  {r.status === 'active' && (
                    <Popconfirm title="归档后不可再用于新建实例，确认？" onConfirm={() => archiveMut.mutate(r.id)}>
                      <Button size="small" danger>
                        归档
                      </Button>
                    </Popconfirm>
                  )}
                </Space>
              ),
            },
          ]}
        />
      )}

      <Modal
        title={editor ? `${MODE_TITLE[editor.mode]}${editor.title ? ` · ${editor.title}` : ''}` : ''}
        open={editor !== null}
        width={760}
        onCancel={() => setEditor(null)}
        confirmLoading={saveMut.isPending}
        okText={readOnly ? '关闭' : '保存'}
        cancelText="取消"
        onOk={() => (readOnly ? setEditor(null) : saveMut.mutate({ mode: editor!.mode, id: editor!.id, yaml }))}
        destroyOnHidden
      >
        <Typography.Paragraph type="secondary" style={{ marginTop: 8 }}>
          {editor?.mode === 'import'
            ? '粘贴导出的 YAML：id/名称冲突时服务端会换 id 或升版本。'
            : editor?.mode === 'edit'
              ? '保存即生成新版本（版本号自动递增），已运行实例不受影响。'
              : '节点类型：normal / auto / terminal；门禁字段：gate.approvals、gate.roles、gate.artifacts。'}
        </Typography.Paragraph>
        <Input.TextArea
          value={yaml}
          onChange={(e) => setYaml(e.target.value)}
          readOnly={readOnly}
          rows={20}
          spellCheck={false}
          style={{ fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace', fontSize: 13 }}
          placeholder={loadingYaml ? '加载中…' : undefined}
        />
        {readOnly && (
          <Space style={{ marginTop: 12 }}>
            <Button onClick={download}>下载 YAML</Button>
          </Space>
        )}
      </Modal>
    </Card>
  )
}
