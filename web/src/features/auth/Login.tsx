// 登录页：用户名密码换 JWT；token 只存内存，刷新页面即回此页。
import { useMutation } from '@tanstack/react-query'
import { App, Button, Card, Form, Input, Typography } from 'antd'
import { useState } from 'react'
import { useNavigate } from 'react-router'
import { apiLogin, setToken } from '../../shared/api'

export default function Login() {
  const navigate = useNavigate()
  const { message } = App.useApp()
  const [error, setError] = useState('')

  const loginMut = useMutation({
    mutationFn: (v: { username: string; password: string }) => apiLogin(v.username, v.password),
    onSuccess: (result) => {
      setToken(result.token)
      message.success(`欢迎，${result.user.displayName}`)
      navigate('/', { replace: true })
    },
    onError: (err: Error) => setError(err.message),
  })

  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: '#f6f7f9',
      }}
    >
      <Card style={{ width: 380 }}>
        <Typography.Title level={3} style={{ marginBottom: 4 }}>
          Chalk<span style={{ color: '#c2410c' }}>line</span> 墨线
        </Typography.Title>
        <Typography.Text type="secondary">规格驱动开发协作平台 · 控制台</Typography.Text>
        <Form
          layout="vertical"
          style={{ marginTop: 24 }}
          onFinish={(v: { username: string; password: string }) => {
            setError('')
            loginMut.mutate(v)
          }}
        >
          <Form.Item
            name="username"
            label="用户名"
            rules={[{ required: true, message: '请输入用户名' }]}
          >
            <Input autoFocus autoComplete="username" />
          </Form.Item>
          <Form.Item
            name="password"
            label="密码"
            rules={[{ required: true, message: '请输入密码' }]}
          >
            <Input.Password autoComplete="current-password" />
          </Form.Item>
          {error && (
            <Form.Item>
              <Typography.Text type="danger">{error}</Typography.Text>
            </Form.Item>
          )}
          <Button type="primary" htmlType="submit" block loading={loginMut.isPending}>
            登录
          </Button>
        </Form>
      </Card>
    </div>
  )
}
