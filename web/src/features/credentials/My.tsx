// 我的凭据：成员自助维护本人在外部系统（outline / redmine / git / svn）的
// 写权限 Key。凭据按 (provider, 服务器地址) 生效——地址从全局服务器列表选择，
// 也可手填。分发成果物 / 流程记录时优先以本人身份出站，外部系统可见
// 「对应员工提交」；未配置时回退公共凭据（迭代级 → 环境变量）。
// Key 加密保存、永不回显；留空提交表示保持已保存的凭据。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  App,
  AutoComplete,
  Button,
  Card,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd'
import { useState } from 'react'
import { apiDelete, apiGet, apiPut } from '../../shared/api'
import type {
  ListEnvelope,
  ProviderEndpointView,
  UserCredentialSet,
  UserCredentialView,
} from '../../shared/types'
import { ApiErrorBanner } from '../../shared/ui'
import { fmtTime } from '../../shared/status'

const PROVIDER_OPTIONS = [
  { value: 'outline', label: 'outline（知识库）' },
  { value: 'redmine', label: 'redmine（流程记录）' },
  { value: 'git', label: 'git（代码库）' },
  { value: 'svn', label: 'svn（代码库）' },
]

const AUTH_TYPE_OPTIONS = [
  { value: 'token', label: 'API Key / Token' },
  { value: 'basic', label: '用户名 + 密码' },
]

interface CredentialForm {
  provider: string
  baseUrl?: string
  authType?: string
  authUsername?: string
  authSecret?: string
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
        return Promise.reject(new Error('不得内嵌凭据'))
      }
      return Promise.resolve()
    } catch {
      return Promise.reject(new Error('不是合法 URL'))
    }
  },
}

export default function MyCredentials() {
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const [open, setOpen] = useState(false)
  const [form] = Form.useForm<CredentialForm>()

  const { data, isPending, isError, error } = useQuery({
    queryKey: ['my-credentials'],
    queryFn: () => apiGet<ListEnvelope<UserCredentialView>>('/me/credentials'),
  })

  // 全局服务器列表（成员可读已启用项）：凭据适用地址的下拉建议，也可手填。
  const { data: endpoints } = useQuery({
    queryKey: ['provider-endpoints'],
    queryFn: () => apiGet<ListEnvelope<ProviderEndpointView>>('/integration/endpoints'),
  })

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['my-credentials'] })
    // 绑定视图上的「以我的 Key 提交」标注随之刷新。
    void queryClient.invalidateQueries({ queryKey: ['iteration'] })
    void queryClient.invalidateQueries({ queryKey: ['current-iteration'] })
  }

  const saveMut = useMutation({
    mutationFn: (v: CredentialForm) =>
      apiPut<UserCredentialView>('/me/credentials', {
        provider: v.provider,
        baseUrl: (v.baseUrl ?? '').trim(),
        authType: v.authType ?? '',
        authUsername: v.authType === 'basic' ? (v.authUsername ?? '').trim() : '',
        authSecret: (v.authSecret ?? '').trim(),
      } satisfies UserCredentialSet),
    onSuccess: () => {
      message.success('个人凭据已保存')
      setOpen(false)
      void invalidate()
    },
    onError: (err: Error) => message.error(err.message),
  })

  const deleteMut = useMutation({
    mutationFn: (v: { provider: string; baseUrl: string }) =>
      apiDelete(
        `/me/credentials/${encodeURIComponent(v.provider)}?baseUrl=${encodeURIComponent(v.baseUrl)}`,
      ),
    onSuccess: () => {
      message.success('已删除（此后回退公共凭据出站）')
      void invalidate()
    },
    onError: (err: Error) => message.error(err.message),
  })

  const openCreate = () => {
    form.resetFields()
    setOpen(true)
  }

  const openEdit = (c: UserCredentialView) => {
    form.setFieldsValue({
      provider: c.provider,
      baseUrl: c.baseUrl ?? '',
      authType: c.authType || undefined,
      authUsername: c.authUsername,
      authSecret: '',
    })
    setOpen(true)
  }

  return (
    <Card
      title="我的凭据（个人 Provider Key）"
      extra={
        <Button type="primary" size="small" onClick={openCreate}>
          + 添加凭据
        </Button>
      }
    >
      <Typography.Paragraph type="secondary" style={{ marginTop: 0 }}>
        提交成果物 / 流程记录到外部系统（Outline / Redmine / Git / SVN）时，优先以你的个人 Key
        出站——外部系统里能看到是你提交的。未配置时回退公共凭据（迭代级 Key → 服务端环境变量），
        迭代详情页会给出提示。Key 加密保存、永不回显；留空提交表示保持已保存的凭据。
      </Typography.Paragraph>
      {isError ? (
        <ApiErrorBanner error={error} />
      ) : (
        <Table<UserCredentialView>
          rowKey={(r) => `${r.provider}|${r.baseUrl ?? ''}`}
          loading={isPending}
          dataSource={data?.items ?? []}
          pagination={false}
          locale={{ emptyText: '暂无个人凭据（外部提交将以公共凭据出站）' }}
          columns={[
            {
              title: 'Provider',
              dataIndex: 'provider',
              width: 150,
              render: (v: string, r) => (
                <Space direction="vertical" size={0}>
                  <Tag>{v}</Tag>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {r.providerLabel}
                  </Typography.Text>
                </Space>
              ),
            },
            {
              title: '适用地址',
              dataIndex: 'baseUrl',
              ellipsis: true,
              render: (v?: string) =>
                v ? (
                  <Typography.Text code>{v}</Typography.Text>
                ) : (
                  <Typography.Text type="secondary">（历史行：未指定地址）</Typography.Text>
                ),
            },
            {
              title: '认证方式',
              dataIndex: 'authType',
              width: 130,
              render: (v: string | undefined, r) =>
                v === 'basic' ? (
                  <Tag>basic · {r.authUsername}</Tag>
                ) : v ? (
                  <Tag>token</Tag>
                ) : (
                  <Tag color="orange">未设置</Tag>
                ),
            },
            {
              title: 'Key',
              dataIndex: 'secretConfigured',
              width: 110,
              render: (v: boolean) =>
                v ? <Tag color="green">已配置</Tag> : <Tag color="orange">未配置</Tag>,
            },
            {
              title: '更新时间',
              dataIndex: 'updatedAt',
              width: 160,
              render: (v?: string) =>
                v ? (
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {fmtTime(v)}
                  </Typography.Text>
                ) : (
                  <Typography.Text type="secondary">—</Typography.Text>
                ),
            },
            {
              title: '操作',
              key: 'op',
              width: 140,
              render: (_, r) => (
                <Space size={4}>
                  <Button size="small" onClick={() => openEdit(r)}>
                    更新
                  </Button>
                  <Popconfirm
                    title="删除该凭据？"
                    description="此后该系统的提交将以公共凭据出站。"
                    onConfirm={() =>
                      deleteMut.mutate({ provider: r.provider, baseUrl: r.baseUrl ?? '' })
                    }
                  >
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
        title="个人凭据"
        open={open}
        onCancel={() => setOpen(false)}
        confirmLoading={saveMut.isPending}
        onOk={() => form.submit()}
        destroyOnHidden
      >
        <Form<CredentialForm> form={form} layout="vertical" onFinish={(v) => saveMut.mutate(v)}>
          <Form.Item
            name="provider"
            label="Provider"
            rules={[{ required: true, message: '请选择 provider' }]}
            extra="凭据按 (provider, 服务器地址) 生效；同一 provider 可按不同地址分别配置 Key"
          >
            <Select options={PROVIDER_OPTIONS} />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(a, b) => a.provider !== b.provider}>
            {({ getFieldValue }) => {
              const kind: string | undefined = getFieldValue('provider')
              const options = (endpoints?.items ?? [])
                .filter((e) => e.kind === kind)
                .map((e) => ({ value: e.baseUrl, label: `${e.label}${e.note ? ` · ${e.note}` : ''}` }))
              return (
                <Form.Item
                  name="baseUrl"
                  label="适用地址"
                  rules={[{ required: true, message: '请填写适用地址' }, httpUrlRule]}
                  extra="从全局服务器列表选择，或手填任意地址"
                >
                  <AutoComplete options={options}>
                    <Input suffix={<span style={{ opacity: 0.45 }}>▾</span>} placeholder="https://git.example.com" />
                  </AutoComplete>
                </Form.Item>
              )
            }}
          </Form.Item>
          <Form.Item
            name="authType"
            label="认证方式"
            rules={[{ required: true, message: '请选择认证方式' }]}
          >
            <Select options={AUTH_TYPE_OPTIONS} />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(a, b) => a.authType !== b.authType}>
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
          <Form.Item
            name="authSecret"
            label="Key / 密码"
            extra="加密保存、永不回显；留空表示保持已保存的凭据"
          >
            <Input.Password placeholder="••••••" autoComplete="new-password" />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
