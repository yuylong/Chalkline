// 迭代级 Provider 绑定：成果物（必选）/ 流程记录（可选）/ 代码库（可选，可多个）。
// 连接信息随迭代固化，节点详情页据此展示可点击的跳转入口；首次提交成果物后冻结。
import { useMutation, useQuery } from '@tanstack/react-query'
import { App, AutoComplete, Button, Card, Form, Input, Modal, Select, Space, Tag, Typography } from 'antd'
import { useState } from 'react'
import { apiGet, apiPut } from '../../shared/api'
import type {
  IterationProviderInput,
  IterationProviderView,
  IterationProvidersView,
  ListEnvelope,
  ProviderOptionView,
} from '../../shared/types'

/** 分类展示元数据。 */
export const PROVIDER_CATEGORY_META: Record<string, { label: string; color: string; hint: string }> = {
  artifact: { label: '成果物', color: 'orange', hint: '必选：成果物记录到哪里' },
  process: { label: '流程记录', color: 'blue', hint: '可选：节点/任务建票与动作回复（Redmine）' },
  code: { label: '代码库', color: 'geekblue', hint: '可选：成果物可关联代码引用（可多个）' },
}

/** 每类可选的 provider（与后端 workitem.categoryProviders 对齐）。 */
export const CATEGORY_PROVIDER_OPTIONS: Record<string, { value: string; label: string }[]> = {
  artifact: [
    { value: 'inline', label: 'inline（直接存 Chalkline）' },
    { value: 'outline', label: 'outline（知识库）' },
    { value: 'git', label: 'git（代码库）' },
    { value: 'svn', label: 'svn（代码库）' },
  ],
  process: [{ value: 'redmine', label: 'redmine' }],
  code: [
    { value: 'git', label: 'git' },
    { value: 'svn', label: 'svn' },
  ],
}

/** 认证方式（与后端 workitem.ProviderAuthType 对齐）。 */
export const AUTH_TYPE_OPTIONS: { value: string; label: string }[] = [
  { value: 'token', label: 'API Key / Token' },
  { value: 'basic', label: '用户名 + 密码' },
]

/** 表单值形状（迭代创建与迭代详情配置页共用）。 */
export interface ProviderFormValues {
  artifactProvider?: string
  artifactBaseUrl?: string
  artifactTarget?: string
  artifactDirTemplate?: string
  artifactAuthType?: string
  artifactAuthUsername?: string
  artifactAuthSecret?: string
  processProvider?: string
  processBaseUrl?: string
  processTarget?: string
  processAuthType?: string
  processAuthUsername?: string
  processAuthSecret?: string
  codeProviders?: {
    provider?: string
    baseUrl?: string
    target?: string
    dirTemplate?: string
    branch?: string
    authType?: string
    authUsername?: string
    authSecret?: string
  }[]
}

const httpUrlRule = {
  validator: (_: unknown, v?: string) => {
    const s = (v ?? '').trim()
    if (!s) return Promise.resolve()
    try {
      const u = new URL(s)
      if (u.protocol !== 'http:' && u.protocol !== 'https:') {
        return Promise.reject(new Error('必须是 http/https 地址'))
      }
      if (u.username || u.password) {
        return Promise.reject(new Error('不得内嵌凭据（请在「认证方式」中单独配置）'))
      }
      return Promise.resolve()
    } catch {
      return Promise.reject(new Error('不是合法 URL'))
    }
  },
}

/** 绑定 → 表单值（配置弹窗打开时预填）。 */
export function providersToForm(items?: IterationProviderView[]): ProviderFormValues {
  const artifact = (items ?? []).find((i) => i.category === 'artifact')
  const processBinding = (items ?? []).find((i) => i.category === 'process')
  const code = (items ?? []).filter((i) => i.category === 'code')
  return {
    artifactProvider: artifact?.provider ?? 'inline',
    artifactBaseUrl: artifact?.baseUrl,
    artifactTarget: artifact?.target,
    artifactDirTemplate: artifact?.dirTemplate,
    artifactAuthType: artifact?.authType || undefined,
    artifactAuthUsername: artifact?.authUsername,
    processProvider: processBinding?.provider,
    processBaseUrl: processBinding?.baseUrl,
    processTarget: processBinding?.target,
    processAuthType: processBinding?.authType || undefined,
    processAuthUsername: processBinding?.authUsername,
    codeProviders: code.map((c) => ({
      provider: c.provider,
      baseUrl: c.baseUrl,
      target: c.target,
      dirTemplate: c.dirTemplate,
      branch: c.branch,
      authType: c.authType || undefined,
      authUsername: c.authUsername,
    })),
  }
}

/** 表单值 → 绑定入参（成果物必选，流程/代码按填写情况生成）。
 *  凭据字段只在选择认证方式后提交；secret 留空表示保持库内已有凭据。 */
export function formToProviders(v: ProviderFormValues): IterationProviderInput[] {
  const out: IterationProviderInput[] = []
  const artifactProvider = v.artifactProvider ?? 'inline'
  const artifactExternal = artifactProvider !== 'inline'
  out.push({
    category: 'artifact',
    provider: artifactProvider,
    baseUrl: artifactExternal ? (v.artifactBaseUrl ?? '').trim() : '',
    target: artifactExternal ? (v.artifactTarget ?? '').trim() : '',
    dirTemplate: artifactExternal ? (v.artifactDirTemplate ?? '').trim() : '',
    authType: artifactExternal ? (v.artifactAuthType ?? '') : '',
    authUsername: artifactExternal && v.artifactAuthType === 'basic' ? (v.artifactAuthUsername ?? '').trim() : '',
    authSecret: artifactExternal ? (v.artifactAuthSecret ?? '').trim() : '',
  })
  if (v.processProvider) {
    out.push({
      category: 'process',
      provider: v.processProvider,
      baseUrl: (v.processBaseUrl ?? '').trim(),
      target: (v.processTarget ?? '').trim(),
      authType: v.processAuthType ?? '',
      authUsername: v.processAuthType === 'basic' ? (v.processAuthUsername ?? '').trim() : '',
      authSecret: (v.processAuthSecret ?? '').trim(),
    })
  }
  for (const c of v.codeProviders ?? []) {
    if (!c?.provider) continue
    out.push({
      category: 'code',
      provider: c.provider,
      baseUrl: (c.baseUrl ?? '').trim(),
      target: (c.target ?? '').trim(),
      dirTemplate: (c.dirTemplate ?? '').trim(),
      branch: (c.branch ?? '').trim(),
      authType: c.authType ?? '',
      authUsername: c.authType === 'basic' ? (c.authUsername ?? '').trim() : '',
      authSecret: (c.authSecret ?? '').trim(),
    })
  }
  return out
}

/** 下拉提示箭头（不引入额外图标依赖）。 */
const caretIcon = <span style={{ opacity: 0.45 }}>▾</span>

/** 认证字段：三种 provider 共用（留空即沿用服务端环境变量）。 */
function AuthFields({
  typeField,
  usernameField,
  secretField,
}: {
  typeField: string | (string | number)[]
  usernameField: string | (string | number)[]
  secretField: string | (string | number)[]
}) {
  return (
    <>
      <Form.Item
        name={typeField}
        label="认证方式"
        extra="留空 = 沿用服务端环境变量（CHALKLINE_*_TOKEN）；填写后用于写权限"
      >
        <Select allowClear placeholder="沿用环境变量" options={AUTH_TYPE_OPTIONS} />
      </Form.Item>
      <Form.Item noStyle shouldUpdate>
        {({ getFieldValue }) => {
          const t: string | undefined = getFieldValue(typeField)
          if (!t) return null
          return (
            <>
              {t === 'basic' ? (
                <Form.Item
                  name={usernameField}
                  label="用户名"
                  rules={[{ required: true, message: '请填写用户名' }]}
                >
                  <Input autoComplete="off" />
                </Form.Item>
              ) : null}
              <Form.Item
                name={secretField}
                label={t === 'basic' ? '密码' : 'API Key / Token'}
                rules={[{ required: t === 'basic', message: '请填写密码' }]}
                extra="加密保存、永不回显；留空表示保持已保存的凭据"
              >
                <Input.Password placeholder="••••••" autoComplete="new-password" />
              </Form.Item>
            </>
          )
        }}
      </Form.Item>
    </>
  )
}

/** 配置表单字段：迭代创建弹窗与迭代详情配置弹窗共用。 */
export function ProviderBindingFields() {
  // 全局连接默认值（成员可读）：仅用于预填/提示地址，凭据仍只读环境变量。
  const { data: options } = useQuery({
    queryKey: ['provider-options'],
    queryFn: () => apiGet<ListEnvelope<ProviderOptionView>>('/integration/provider-options'),
  })
  const defaultBaseURL = (kind: string) =>
    options?.items.find((o) => o.kind === kind)?.baseUrl ?? ''
  const suggestions = (...kinds: string[]) => {
    const seen = new Set<string>()
    const out: { value: string }[] = []
    for (const k of kinds) {
      const url = defaultBaseURL(k)
      if (url && !seen.has(url)) {
        seen.add(url)
        out.push({ value: url })
      }
    }
    return out
  }

  return (
    <Space direction="vertical" size={4} style={{ width: '100%' }}>
      <Typography.Text strong>成果物 provider（必选）</Typography.Text>
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        {PROVIDER_CATEGORY_META.artifact.hint}；选 inline 表示直接存入 Chalkline，外部 provider 只回填摘要与链接。
      </Typography.Text>
      <Form.Item
        name="artifactProvider"
        label="记录位置"
        initialValue="inline"
        rules={[{ required: true, message: '请选择成果物 provider' }]}
      >
        <Select options={CATEGORY_PROVIDER_OPTIONS.artifact} />
      </Form.Item>
      <Form.Item noStyle shouldUpdate={(a, b) => a.artifactProvider !== b.artifactProvider}>
        {({ getFieldValue }) => {
          const p: string = getFieldValue('artifactProvider') ?? 'inline'
          if (p === 'inline') return null
          return (
            <>
              <Form.Item
                name="artifactBaseUrl"
                label="连接地址 baseURL"
                rules={[{ required: true, message: '请填写连接地址' }, httpUrlRule]}
                extra={
                  defaultBaseURL(p)
                    ? `全局默认：${defaultBaseURL(p)}（可直接选用，也可覆盖）`
                    : '可手动填写；认证信息在下方单独配置'
                }
              >
                <AutoComplete options={suggestions(p)}>
                  <Input
                    suffix={caretIcon}
                    placeholder={defaultBaseURL(p) || 'https://…'}
                  />
                </AutoComplete>
              </Form.Item>
              <Form.Item
                name="artifactTarget"
                label={p === 'outline' ? '集合 collectionId' : '代码库路径'}
                rules={[{ required: true, message: '请填写目标' }]}
                extra={p === 'outline' ? 'Outline 集合标识' : '如 group/repo'}
              >
                <Input />
              </Form.Item>
              {p === 'git' || p === 'svn' ? (
                <Form.Item
                  name="artifactDirTemplate"
                  label="成果物目录结构模板"
                  extra="默认 chalkline/{iteration}/{node}；支持 {iteration} {node} {kind}"
                >
                  <Input placeholder="chalkline/{iteration}/{node}" />
                </Form.Item>
              ) : null}
              <AuthFields
                typeField="artifactAuthType"
                usernameField="artifactAuthUsername"
                secretField="artifactAuthSecret"
              />
            </>
          )
        }}
      </Form.Item>

      <Typography.Text strong style={{ marginTop: 8 }}>
        流程记录 provider（可选）
      </Typography.Text>
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        {PROVIDER_CATEGORY_META.process.hint}：每个阶段与任务在该 project 建票，节点动作追加回复。
      </Typography.Text>
      <Form.Item name="processProvider" label="流程记录">
        <Select allowClear placeholder="不记录" options={CATEGORY_PROVIDER_OPTIONS.process} />
      </Form.Item>
      <Form.Item noStyle shouldUpdate={(a, b) => a.processProvider !== b.processProvider}>
        {({ getFieldValue }) =>
          getFieldValue('processProvider') ? (
            <>
              <Form.Item
                name="processBaseUrl"
                label="Redmine 地址"
                rules={[{ required: true, message: '请填写 Redmine 地址' }, httpUrlRule]}
                extra={
                  defaultBaseURL('redmine') ? `全局默认：${defaultBaseURL('redmine')}` : undefined
                }
              >
                <AutoComplete options={suggestions('redmine')}>
                  <Input
                    suffix={caretIcon}
                    placeholder={defaultBaseURL('redmine') || 'https://redmine.example.com'}
                  />
                </AutoComplete>
              </Form.Item>
              <Form.Item
                name="processTarget"
                label="project 标识"
                rules={[{ required: true, message: '请填写 project 标识' }]}
              >
                <Input placeholder="如 chalkline" />
              </Form.Item>
              <AuthFields
                typeField="processAuthType"
                usernameField="processAuthUsername"
                secretField="processAuthSecret"
              />
            </>
          ) : null
        }
      </Form.Item>

      <Typography.Text strong style={{ marginTop: 8 }}>
        代码库 provider（可选，可多个）
      </Typography.Text>
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        {PROVIDER_CATEGORY_META.code.hint}：绑定后 pr 材料可关联到代码库，成果物也可推送到仓库目录。
      </Typography.Text>
      <Form.List name="codeProviders">
        {(fields, { add, remove }) => (
          <>
            {fields.map((field) => (
              <Card
                key={field.key}
                size="small"
                title={`代码库 ${field.name + 1}`}
                extra={
                  <Button size="small" type="text" danger onClick={() => remove(field.name)}>
                    移除
                  </Button>
                }
                style={{ marginBottom: 8 }}
              >
                <Form.Item
                  name={[field.name, 'provider']}
                  label="类型"
                  initialValue="git"
                  rules={[{ required: true }]}
                >
                  <Select options={CATEGORY_PROVIDER_OPTIONS.code} />
                </Form.Item>
                <Form.Item
                  name={[field.name, 'baseUrl']}
                  label="连接地址 baseURL"
                  rules={[{ required: true, message: '请填写连接地址' }, httpUrlRule]}
                  extra={
                    suggestions('git', 'svn').length > 0
                      ? `全局默认：${suggestions('git', 'svn')
                          .map((s) => s.value)
                          .join(' / ')}`
                      : undefined
                  }
                >
                  <AutoComplete options={suggestions('git', 'svn')}>
                    <Input
                      suffix={caretIcon}
                      placeholder={defaultBaseURL('git') || 'https://git.example.com'}
                    />
                  </AutoComplete>
                </Form.Item>
                <Form.Item
                  name={[field.name, 'target']}
                  label="仓库路径"
                  rules={[{ required: true, message: '请填写仓库路径' }]}
                >
                  <Input placeholder="如 team/chalkline" />
                </Form.Item>
                <Form.Item name={[field.name, 'branch']} label="分支（git）">
                  <Input placeholder="main" />
                </Form.Item>
                <AuthFields
                  typeField={[field.name, 'authType']}
                  usernameField={[field.name, 'authUsername']}
                  secretField={[field.name, 'authSecret']}
                />
              </Card>
            ))}
            <Button type="dashed" block onClick={() => add({ provider: 'git' })}>
              + 添加代码库
            </Button>
          </>
        )}
      </Form.List>
    </Space>
  )
}

/** 单条绑定展示（provider + 连接地址 + 跳转 + 凭据状态）。 */
export function ProviderBindingLine({ item }: { item: IterationProviderView }) {
  const meta = PROVIDER_CATEGORY_META[item.category]
  const label = item.provider === 'inline' ? '平台内置（inline）' : `${item.baseUrl ?? ''}${item.target ? ` / ${item.target}` : ''}`
  const auth = (() => {
    if (item.provider === 'inline') return null
    if (item.secretConfigured) {
      return <Tag color="green">Key 已配置（本迭代）</Tag>
    }
    if (item.envSecretConfigured) {
      return <Tag>沿用环境变量</Tag>
    }
    return <Tag color="orange">未配置 Key</Tag>
  })()
  return (
    <Space wrap size={[8, 4]}>
      <Tag color={meta?.color ?? 'default'}>{item.provider}</Tag>
      <Typography.Text code>{label}</Typography.Text>
      {item.branch ? <Typography.Text type="secondary">分支 {item.branch}</Typography.Text> : null}
      {auth}
      {item.linkUrl ? (
        <a href={item.linkUrl} target="_blank" rel="noreferrer">
          打开
        </a>
      ) : null}
    </Space>
  )
}

/** 三类绑定概览：节点详情页与迭代详情页共用。 */
export function ProviderBindingSummary({ providers }: { providers?: IterationProvidersView }) {
  const items = providers?.items ?? []
  return (
    <Space direction="vertical" size={8} style={{ width: '100%' }}>
      {Object.keys(PROVIDER_CATEGORY_META).map((cat) => {
        const group = items.filter((i) => i.category === cat)
        const meta = PROVIDER_CATEGORY_META[cat]
        return (
          <div key={cat}>
            <Typography.Text type="secondary" style={{ fontSize: 12, display: 'block' }}>
              {meta.label} · {meta.hint}
            </Typography.Text>
            {group.length === 0 ? (
              <Typography.Text type="secondary">未配置</Typography.Text>
            ) : (
              <Space direction="vertical" size={2}>
                {group.map((i, idx) => (
                  <ProviderBindingLine key={`${i.category}-${i.provider}-${idx}`} item={i} />
                ))}
              </Space>
            )}
          </div>
        )
      })}
    </Space>
  )
}

/** 迭代详情页的 Provider 卡片：展示 + 初始化阶段配置。 */
export function IterationProvidersCard({
  providers,
  onChanged,
}: {
  providers: IterationProvidersView
  onChanged?: () => void
}) {
  const { message } = App.useApp()
  const [open, setOpen] = useState(false)
  const [form] = Form.useForm<ProviderFormValues>()

  // 凭据轮换：独立入口，绑定冻结后仍可用（公司会定期更换 Key）。
  const [credOpen, setCredOpen] = useState(false)
  const [credID, setCredID] = useState('')
  const [credForm] = Form.useForm<{ authType?: string; authUsername?: string; authSecret?: string }>()
  const externalItems = providers.items.filter((i) => i.provider !== 'inline')

  const credMut = useMutation({
    mutationFn: (v: { authType?: string; authUsername?: string; authSecret?: string }) =>
      apiPut<IterationProvidersView>(
        `/iterations/${providers.iterationId}/providers/${credID}/credential`,
        {
          authType: v.authType ?? '',
          authUsername: v.authType === 'basic' ? (v.authUsername ?? '') : '',
          authSecret: (v.authSecret ?? '').trim(),
        },
      ),
    onSuccess: () => {
      message.success('凭据已更新')
      setCredOpen(false)
      onChanged?.()
    },
    onError: (err: Error) => message.error(err.message),
  })

  const openCredential = () => {
    const first = externalItems[0]
    if (!first) return
    setCredID(first.id)
    credForm.setFieldsValue({
      authType: first.authType || undefined,
      authUsername: first.authUsername,
      authSecret: '',
    })
    setCredOpen(true)
  }

  const pickCredentialTarget = (id: string) => {
    setCredID(id)
    const item = externalItems.find((i) => i.id === id)
    credForm.setFieldsValue({
      authType: item?.authType || undefined,
      authUsername: item?.authUsername,
      authSecret: '',
    })
  }

  const saveMut = useMutation({
    mutationFn: (v: ProviderFormValues) =>
      apiPut<IterationProvidersView>(`/iterations/${providers.iterationId}/providers`, {
        items: formToProviders(v),
      }),
    onSuccess: () => {
      message.success('Provider 绑定已保存')
      setOpen(false)
      onChanged?.()
    },
    onError: (err: Error) => message.error(err.message),
  })

  const openEdit = () => {
    form.setFieldsValue(providersToForm(providers.items))
    setOpen(true)
  }

  return (
    <Card
      title="迭代 Provider（成果物 / 流程记录 / 代码库）"
      extra={
        <Space>
          {externalItems.length > 0 ? (
            <Button size="small" onClick={openCredential}>
              更新凭据
            </Button>
          ) : null}
          {providers.editable ? (
            <Button size="small" type="primary" onClick={openEdit}>
              配置
            </Button>
          ) : (
            <Tag>{providers.lockedReason ?? '已冻结'}</Tag>
          )}
        </Space>
      }
    >
      <ProviderBindingSummary providers={providers} />
      <Modal
        title="配置迭代 Provider"
        open={open}
        onCancel={() => setOpen(false)}
        confirmLoading={saveMut.isPending}
        onOk={() => form.submit()}
        width={680}
        destroyOnHidden
      >
        <Typography.Paragraph type="secondary" style={{ marginTop: 8 }}>
          绑定仅在迭代初始化阶段可改；一旦提交成果物即冻结，连接信息随迭代固化。
        </Typography.Paragraph>
        <Form<ProviderFormValues> form={form} layout="vertical" onFinish={(v) => saveMut.mutate(v)}>
          <ProviderBindingFields />
        </Form>
      </Modal>

      <Modal
        title="更新 Provider 凭据"
        open={credOpen}
        onCancel={() => setCredOpen(false)}
        confirmLoading={credMut.isPending}
        onOk={() => credForm.submit()}
        destroyOnHidden
      >
        <Typography.Paragraph type="secondary" style={{ marginTop: 8 }}>
          凭据加密保存、永不回显；绑定冻结后仍可更换（公司定期轮换 Key 时使用）。密码留空表示保持原值。
        </Typography.Paragraph>
        <Form
          form={credForm}
          layout="vertical"
          onFinish={(v) => credMut.mutate(v)}
        >
          <Form.Item label="目标 provider">
            <Select
              value={credID}
              onChange={pickCredentialTarget}
              options={externalItems.map((i) => ({
                value: i.id,
                label: `${i.categoryLabel} · ${i.provider}（${i.baseUrl ?? ''}${i.target ? ` / ${i.target}` : ''}）`,
              }))}
            />
          </Form.Item>
          <Form.Item
            name="authType"
            label="认证方式"
            rules={[{ required: true, message: '请选择认证方式' }]}
          >
            <Select options={AUTH_TYPE_OPTIONS} />
          </Form.Item>
          <Form.Item noStyle shouldUpdate>
            {({ getFieldValue }) =>
              getFieldValue('authType') === 'basic' ? (
                <Form.Item
                  name="authUsername"
                  label="用户名"
                  rules={[{ required: true, message: '请填写用户名' }]}
                >
                  <Input autoComplete="off" />
                </Form.Item>
              ) : null
            }
          </Form.Item>
          <Form.Item name="authSecret" label="Key / 密码" extra="留空表示保持已保存的凭据">
            <Input.Password placeholder="••••••" autoComplete="new-password" />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
