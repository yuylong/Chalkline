// 邮箱配置（admin）：SMTP 发件服务器可视化维护，保存后投递器下一轮即生效。
// 密码经后端 AES-GCM 加密落库（密钥 CHALKLINE_SECRET_KEY），本页只显示
// 「已配置 / 未配置」，永不回显明文；通知按事件发给相关成员（成员设置里的邮箱），
// CHALKLINE_NOTIFY_EMAILS 仅作解析不出相关成员时的兜底收件人。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { App, Button, Card, Form, Input, InputNumber, Popconfirm, Select, Space, Switch, Tag, Typography } from 'antd'
import { useEffect } from 'react'
import { apiGet, apiPatch, apiPost } from '../../shared/api'
import { fmtTime } from '../../shared/status'
import { ApiErrorBanner } from '../../shared/ui'
import type { SMTPConfigUpdate, SMTPConfigView } from '../../shared/types'

interface SmtpForm {
  enabled: boolean
  host: string
  port: number
  username: string
  from: string
  tlsMode: string
  tlsSkipVerify: boolean
  password?: string
}

export default function SmtpConfig() {
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const [form] = Form.useForm<SmtpForm>()

  const { data, isPending, isError, error } = useQuery({
    queryKey: ['smtp-config'],
    queryFn: () => apiGet<SMTPConfigView>('/integration/smtp'),
  })

  useEffect(() => {
    if (data) {
      form.setFieldsValue({
        enabled: data.enabled,
        host: data.host,
        port: data.port,
        username: data.username,
        from: data.from,
        tlsMode: data.tlsMode,
        tlsSkipVerify: data.tlsSkipVerify,
        password: '',
      })
    }
  }, [data, form])

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['smtp-config'] })

  const saveMut = useMutation({
    mutationFn: (v: SmtpForm) => {
      const body: SMTPConfigUpdate = {
        enabled: v.enabled,
        host: v.host.trim(),
        port: v.port,
        username: v.username.trim(),
        from: v.from.trim(),
        tlsMode: v.tlsMode ?? '', // 清除选择 = 清除库内设置，回退环境变量
        tlsSkipVerify: v.tlsSkipVerify,
      }
      if (v.password && v.password.trim() !== '') body.password = v.password
      return apiPatch<SMTPConfigView>('/integration/smtp', body)
    },
    onSuccess: () => {
      message.success('邮箱配置已保存（下一轮投递即生效）')
      form.setFieldValue('password', '')
      void invalidate()
    },
    onError: (err: Error) => message.error(err.message),
  })

  const clearPwMut = useMutation({
    mutationFn: () => apiPatch<SMTPConfigView>('/integration/smtp', { password: '' }),
    onSuccess: () => {
      message.success('已清除保存的密码（回退环境变量 CHALKLINE_SMTP_PASSWORD）')
      void invalidate()
    },
    onError: (err: Error) => message.error(err.message),
  })

  const testMut = useMutation({
    mutationFn: () => apiPost<{ sent: boolean }>('/integration/smtp/test', {}),
    onSuccess: () => message.success('测试邮件已发送到当前登录账号的邮箱，请查收'),
    onError: (err: Error) => message.error(err.message),
  })

  return (
    <Card
      title="邮箱配置（SMTP 发件）"
      extra={
        <Typography.Text type="secondary">
          密码加密落库（CHALKLINE_SECRET_KEY），永不回显；通知按事件发给相关成员（成员设置里的邮箱）
        </Typography.Text>
      }
    >
      {isError ? (
        <ApiErrorBanner error={error} />
      ) : (
        <Form<SmtpForm>
          form={form}
          layout="vertical"
          onFinish={(v) => saveMut.mutate(v)}
          disabled={isPending || saveMut.isPending}
          style={{ maxWidth: 560 }}
        >
          <Form.Item name="enabled" label="启用邮件通知" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item
            name="host"
            label="SMTP 服务器"
            extra={data?.envHost ? `留空沿用环境变量默认值：${data.envHost}` : '留空 = 未配置（环境变量亦未设置）'}
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
            <Input placeholder="smtp.example.com" />
          </Form.Item>
          <Form.Item name="port" label="端口" extra="0 = 默认 25">
            <InputNumber min={0} max={65535} style={{ width: 160 }} />
          </Form.Item>
          <Form.Item
            name="tlsMode"
            label="TLS 模式"
            extra={
              data?.envTlsMode
                ? `留空沿用环境变量默认值：${data.envTlsMode}（CHALKLINE_SMTP_TLS）`
                : 'CHALKLINE_SMTP_TLS 未设置时默认 none'
            }
          >
            <Select
              allowClear
              style={{ width: 200 }}
              placeholder="沿用环境变量默认值"
              options={[
                { value: 'none', label: 'none（明文）' },
                { value: 'starttls', label: 'STARTTLS（587 常用）' },
                { value: 'tls', label: 'TLS（隐式，465 常用）' },
              ]}
            />
          </Form.Item>
          <Form.Item
            name="tlsSkipVerify"
            label="跳过证书校验"
            valuePropName="checked"
            extra="仅内网自签证书场景开启；公网 SMTP 必须关闭（默认关闭）"
          >
            <Switch />
          </Form.Item>
          <Form.Item name="username" label="账号（可选）">
            <Input autoComplete="off" />
          </Form.Item>
          <Form.Item
            name="password"
            label="密码"
            extra={
              <Space size={8}>
                {data?.passwordConfigured ? <Tag color="green">已配置</Tag> : <Tag color="orange">未配置</Tag>}
                <Popconfirm
                  title="确认清除已保存的密码？"
                  description="清除后回退环境变量 CHALKLINE_SMTP_PASSWORD"
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
            name="from"
            label="发件人地址"
            extra={data?.envFrom ? `留空沿用环境变量默认值：${data.envFrom}` : '留空 = 未配置（环境变量亦未设置）'}
            rules={[
              {
                validator: (_, v: string) => {
                  const s = (v ?? '').trim()
                  if (!s) return Promise.resolve()
                  return /^[^\s@]+@[^\s@]+$/.test(s)
                    ? Promise.resolve()
                    : Promise.reject(new Error('不是合法邮箱地址'))
                },
              },
            ]}
          >
            <Input placeholder="chalk@example.com" />
          </Form.Item>

          <Form.Item
            label="兜底收件人（只读，经 CHALKLINE_NOTIFY_EMAILS 配置）"
            extra="通知默认发给相关成员（任务负责人、合格评审人、产品 owner 等）的邮箱，仅在解析不出相关成员时发往此处"
          >
            {data?.envRecipients.length ? (
              <Space size={4} wrap>
                {data.envRecipients.map((r) => (
                  <Tag key={r}>{r}</Tag>
                ))}
              </Space>
            ) : (
              <Tag>未配置兜底收件人</Tag>
            )}
          </Form.Item>

          <Form.Item style={{ marginBottom: 0 }}>
            <Space>
              <Button type="primary" htmlType="submit" loading={saveMut.isPending}>
                保存配置
              </Button>
              <Popconfirm
                title="发送测试邮件？"
                description="将用当前生效配置向当前登录账号的邮箱发送一封测试邮件"
                onConfirm={() => testMut.mutate()}
                disabled={!data?.enabled}
              >
                <Button loading={testMut.isPending} disabled={!data?.enabled}>
                  发送测试邮件
                </Button>
              </Popconfirm>
              {data?.updatedAt && (
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  最近更新：{fmtTime(data.updatedAt)}
                </Typography.Text>
              )}
            </Space>
          </Form.Item>
        </Form>
      )}
    </Card>
  )
}
