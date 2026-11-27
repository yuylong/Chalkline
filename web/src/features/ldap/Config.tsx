// LDAP 认证配置（admin）：LDAP 服务器可视化维护 + 连通性测试。
// 绑定密码经后端 AES-GCM 加密落库（密钥 CHALKLINE_SECRET_KEY），本页只显示
// 「已配置 / 未配置」，永不回显；成员认证来源（本地/LDAP/双认证）在用户管理页设置。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { App, Button, Card, Form, Input, InputNumber, Modal, Popconfirm, Select, Space, Switch, Tag, Typography } from 'antd'
import { useEffect, useState } from 'react'
import { apiGet, apiPatch, apiPost } from '../../shared/api'
import { fmtTime } from '../../shared/status'
import { ApiErrorBanner } from '../../shared/ui'
import type { LDAPConfigUpdate, LDAPConfigView, LDAPTestResult } from '../../shared/types'

interface LdapForm {
  enabled: boolean
  host: string
  port: number
  tlsMode: string
  tlsSkipVerify: boolean
  bindDn: string
  userSearchBase: string
  userSearchFilter: string
  timeoutSeconds: number
  bindPassword?: string
}

export default function LdapConfig() {
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const [form] = Form.useForm<LdapForm>()
  const [bindTestForm] = Form.useForm<{ username: string; password: string }>()
  const [bindTestOpen, setBindTestOpen] = useState(false)

  const { data, isPending, isError, error } = useQuery({
    queryKey: ['ldap-config'],
    queryFn: () => apiGet<LDAPConfigView>('/auth/ldap'),
  })

  useEffect(() => {
    if (data) {
      form.setFieldsValue({
        enabled: data.enabled,
        host: data.host,
        port: data.port,
        tlsMode: data.tlsMode,
        tlsSkipVerify: data.tlsSkipVerify,
        bindDn: data.bindDn,
        userSearchBase: data.userSearchBase,
        userSearchFilter: data.userSearchFilter,
        timeoutSeconds: data.timeoutSeconds,
        bindPassword: '',
      })
    }
  }, [data, form])

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['ldap-config'] })

  const saveMut = useMutation({
    mutationFn: (v: LdapForm) => {
      const body: LDAPConfigUpdate = {
        enabled: v.enabled,
        host: v.host.trim(),
        port: v.port,
        tlsMode: v.tlsMode,
        tlsSkipVerify: v.tlsSkipVerify,
        bindDn: v.bindDn.trim(),
        userSearchBase: v.userSearchBase.trim(),
        userSearchFilter: v.userSearchFilter.trim(),
        timeoutSeconds: v.timeoutSeconds,
      }
      if (v.bindPassword && v.bindPassword.trim() !== '') body.bindPassword = v.bindPassword
      return apiPatch<LDAPConfigView>('/auth/ldap', body)
    },
    onSuccess: () => {
      message.success('LDAP 配置已保存（下一次登录即生效）')
      form.setFieldValue('bindPassword', '')
      void invalidate()
    },
    onError: (err: Error) => message.error(err.message),
  })

  const clearPwMut = useMutation({
    mutationFn: () => apiPatch<LDAPConfigView>('/auth/ldap', { bindPassword: '' }),
    onSuccess: () => {
      message.success('已清除保存的绑定密码（回退匿名绑定）')
      void invalidate()
    },
    onError: (err: Error) => message.error(err.message),
  })

  const testMut = useMutation({
    mutationFn: () => apiPost<LDAPTestResult>('/auth/ldap/test', {}),
    onSuccess: () => message.success('LDAP 连接正常（拨号、绑定与试检索均通过）'),
    onError: (err: Error) => message.error(err.message),
  })

  const bindTestMut = useMutation({
    mutationFn: (v: { username: string; password: string }) =>
      apiPost<LDAPTestResult>('/auth/ldap/test', { username: v.username, password: v.password }),
    onSuccess: (r) => {
      message.success(`用户 ${r.username} 经 LDAP 登录验证通过`)
      setBindTestOpen(false)
      bindTestForm.resetFields()
    },
    onError: (err: Error) => message.error(err.message),
  })

  return (
    <Card
      title="LDAP 认证"
      extra={
        <Typography.Text type="secondary">
          绑定密码加密落库（CHALKLINE_SECRET_KEY），永不回显；成员的认证来源（本地/LDAP/双认证）在用户管理页按人设置
        </Typography.Text>
      }
    >
      {isError ? (
        <ApiErrorBanner error={error} />
      ) : (
        <Form<LdapForm>
          form={form}
          layout="vertical"
          onFinish={(v) => saveMut.mutate(v)}
          disabled={isPending || saveMut.isPending}
          style={{ maxWidth: 560 }}
        >
          <Form.Item name="enabled" label="启用 LDAP 认证" valuePropName="checked" extra="停用后 ldap/双认证成员将无法经 LDAP 登录">
            <Switch />
          </Form.Item>
          <Form.Item
            name="host"
            label="LDAP 服务器"
            rules={[
              {
                validator: (_, v: string) => {
                  const s = (v ?? '').trim()
                  if (!s) return Promise.resolve()
                  if (/[ /@:]/.test(s) || s.includes('://')) {
                    return Promise.reject(new Error('应为纯主机名（不含协议与端口）'))
                  }
                  return Promise.resolve()
                },
              },
            ]}
          >
            <Input placeholder="ldap.example.com" />
          </Form.Item>
          <Form.Item name="port" label="端口" extra="0 = 默认 389（隐式 TLS 常用 636）">
            <InputNumber min={0} max={65535} style={{ width: 160 }} />
          </Form.Item>
          <Form.Item name="tlsMode" label="TLS 模式">
            <Select
              style={{ width: 240 }}
              options={[
                { value: 'none', label: 'none（明文）' },
                { value: 'starttls', label: 'STARTTLS（连接后升级）' },
                { value: 'tls', label: 'TLS（隐式，636 常用）' },
              ]}
            />
          </Form.Item>
          <Form.Item
            name="tlsSkipVerify"
            label="跳过证书校验"
            valuePropName="checked"
            extra="仅内网自签证书场景开启；公网 LDAP 必须关闭（默认关闭）"
          >
            <Switch />
          </Form.Item>
          <Form.Item name="bindDn" label="服务账号 DN（可选）" extra="留空 = 匿名检索（目录须允许匿名查询用户条目）">
            <Input autoComplete="off" placeholder="cn=admin,dc=example,dc=com" />
          </Form.Item>
          <Form.Item
            name="bindPassword"
            label="服务账号密码"
            extra={
              <Space size={8}>
                {data?.bindPasswordConfigured ? <Tag color="green">已配置</Tag> : <Tag color="orange">未配置</Tag>}
                <Popconfirm
                  title="确认清除已保存的绑定密码？"
                  description="清除后回退匿名绑定"
                  onConfirm={() => clearPwMut.mutate()}
                >
                  <Button size="small" danger loading={clearPwMut.isPending}>
                    清除已存密码
                  </Button>
                </Popconfirm>
              </Space>
            }
          >
            <Input.Password placeholder="留空 = 不修改已保存的密码" autoComplete="new-password" />
          </Form.Item>
          <Form.Item
            name="userSearchBase"
            label="用户检索 Base DN"
            rules={[
              {
                validator: (_, v: string) => {
                  const s = (v ?? '').trim()
                  if (!s) return Promise.resolve()
                  return /=/.test(s)
                    ? Promise.resolve()
                    : Promise.reject(new Error('应为 DN 形式，如 ou=people,dc=example,dc=com'))
                },
              },
            ]}
          >
            <Input placeholder="ou=people,dc=example,dc=com" />
          </Form.Item>
          <Form.Item
            name="userSearchFilter"
            label="用户检索过滤器"
            extra="需含 %s 占位登录名：OpenLDAP 常用 (uid=%s)，Active Directory 常用 (sAMAccountName=%s)"
            rules={[
              {
                validator: (_, v: string) => {
                  const s = (v ?? '').trim()
                  if (!s) return Promise.resolve()
                  if (!s.includes('%s')) return Promise.reject(new Error('需包含 %s 占位登录名'))
                  if (!s.startsWith('(') || !s.endsWith(')')) {
                    return Promise.reject(new Error('应为括号包裹的过滤器，如 (uid=%s)'))
                  }
                  return Promise.resolve()
                },
              },
            ]}
          >
            <Input placeholder="(uid=%s)" autoComplete="off" />
          </Form.Item>
          <Form.Item name="timeoutSeconds" label="超时（秒）">
            <InputNumber min={1} max={120} style={{ width: 160 }} />
          </Form.Item>

          <Form.Item style={{ marginBottom: 0 }}>
            <Space>
              <Button type="primary" htmlType="submit" loading={saveMut.isPending}>
                保存配置
              </Button>
              <Popconfirm
                title="测试 LDAP 连通性？"
                description="将拨号、绑定服务账号并按过滤器试检索一条"
                onConfirm={() => testMut.mutate()}
                disabled={!data?.enabled}
              >
                <Button loading={testMut.isPending} disabled={!data?.enabled}>
                  测试连接
                </Button>
              </Popconfirm>
              <Button disabled={!data?.enabled} onClick={() => setBindTestOpen(true)}>
                测试用户登录
              </Button>
              {data?.updatedAt && (
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  最近更新：{fmtTime(data.updatedAt)}
                </Typography.Text>
              )}
            </Space>
          </Form.Item>
        </Form>
      )}

      <Modal
        title="测试用户登录（完整 LDAP 验证）"
        open={bindTestOpen}
        onCancel={() => setBindTestOpen(false)}
        confirmLoading={bindTestMut.isPending}
        onOk={() => bindTestForm.submit()}
        destroyOnHidden
      >
        <Typography.Paragraph type="secondary" style={{ marginTop: 12 }}>
          将按当前生效配置检索该用户并以输入的密码做绑定验证；密码只用于本次验证，不会被保存。
        </Typography.Paragraph>
        <Form<{ username: string; password: string }>
          form={bindTestForm}
          layout="vertical"
          onFinish={(v) => bindTestMut.mutate(v)}
        >
          <Form.Item name="username" label="登录名" rules={[{ required: true, message: '请输入登录名' }]}>
            <Input placeholder="如 zhangsan" autoComplete="off" />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true, message: '请输入密码' }]}>
            <Input.Password autoComplete="new-password" />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
