// 用户管理（admin）：成员列表、创建、改角色/启禁用、重置密码。
// 权限判定在后端（admin 之外的请求返回 403），前端只做展示与请求发起。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { App, Button, Card, Form, Input, Modal, Select, Space, Table, Tag, Typography } from 'antd'
import { useState } from 'react'
import { apiGet, apiPatch, apiPost } from '../../shared/api'
import type { CreateUserInput, ListEnvelope, UpdateUserInput, UserView } from '../../shared/types'
import { ApiErrorBanner } from '../../shared/ui'

const ROLE_OPTIONS = [
  { value: 'admin', label: 'admin · 管理员' },
  { value: 'pm', label: 'pm · 产品/项目' },
  { value: 'reviewer', label: 'reviewer · 评审' },
  { value: 'dev', label: 'dev · 开发' },
  { value: 'qa', label: 'qa · 测试' },
]

const ROLE_COLORS: Record<string, string> = {
  admin: 'red',
  pm: 'blue',
  reviewer: 'purple',
  dev: 'green',
  qa: 'orange',
}

interface EditForm {
  roles: string[]
  status: string
}

export default function UserList() {
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const [createOpen, setCreateOpen] = useState(false)
  const [editing, setEditing] = useState<UserView | null>(null)
  const [resetTarget, setResetTarget] = useState<UserView | null>(null)
  const [createForm] = Form.useForm<CreateUserInput>()
  const [editForm] = Form.useForm<EditForm>()
  const [resetForm] = Form.useForm<{ password: string }>()

  const { data, isPending, isError, error } = useQuery({
    queryKey: ['users'],
    queryFn: () => apiGet<ListEnvelope<UserView>>('/users'),
  })

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['users'] })

  const createMut = useMutation({
    mutationFn: (v: CreateUserInput) => apiPost<UserView>('/users', v),
    onSuccess: () => {
      message.success('成员已创建')
      setCreateOpen(false)
      createForm.resetFields()
      void invalidate()
    },
    onError: (err: Error) => message.error(err.message),
  })

  const updateMut = useMutation({
    mutationFn: (v: { id: string; body: UpdateUserInput }) =>
      apiPatch<UserView>(`/users/${v.id}`, v.body),
    onSuccess: () => {
      message.success('成员已更新')
      setEditing(null)
      void invalidate()
    },
    onError: (err: Error) => message.error(err.message),
  })

  const resetMut = useMutation({
    mutationFn: (v: { id: string; password: string }) =>
      apiPost<{ status: string }>(`/users/${v.id}/password`, { password: v.password }),
    onSuccess: () => {
      message.success('密码已重置')
      setResetTarget(null)
      resetForm.resetFields()
    },
    onError: (err: Error) => message.error(err.message),
  })

  const openEdit = (u: UserView) => {
    setEditing(u)
    editForm.setFieldsValue({ roles: u.roles, status: u.status })
  }

  return (
    <Card
      title="用户管理"
      extra={
        <Space>
          <Typography.Text type="secondary">
            用户名为登录标识且不可修改；停用后 JWT 与 PAT 立即失效
          </Typography.Text>
          <Button type="primary" onClick={() => setCreateOpen(true)}>
            新建成员
          </Button>
        </Space>
      }
    >
      {isError ? (
        <ApiErrorBanner error={error} />
      ) : (
        <Table<UserView>
          rowKey="id"
          loading={isPending}
          dataSource={data?.items ?? []}
          pagination={false}
          columns={[
            { title: '用户名', dataIndex: 'username', width: 160 },
            {
              title: '姓名',
              dataIndex: 'displayName',
              width: 160,
              render: (v: string) => v || <Typography.Text type="secondary">—</Typography.Text>,
            },
            { title: '邮箱', dataIndex: 'email', ellipsis: true },
            {
              title: '角色',
              dataIndex: 'roles',
              width: 280,
              render: (v: string[]) => (
                <Space size={4} wrap>
                  {v.map((r) => (
                    <Tag key={r} color={ROLE_COLORS[r] ?? 'default'}>
                      {r}
                    </Tag>
                  ))}
                </Space>
              ),
            },
            {
              title: '状态',
              dataIndex: 'status',
              width: 90,
              render: (v: string) =>
                v === 'active' ? <Tag color="green">启用</Tag> : <Tag color="red">停用</Tag>,
            },
            {
              title: '操作',
              key: 'op',
              width: 200,
              render: (_, r) => (
                <Space>
                  <Button size="small" onClick={() => openEdit(r)}>
                    编辑
                  </Button>
                  <Button size="small" onClick={() => setResetTarget(r)}>
                    重置密码
                  </Button>
                </Space>
              ),
            },
          ]}
        />
      )}

      <Modal
        title="新建成员"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        confirmLoading={createMut.isPending}
        onOk={() => createForm.submit()}
        destroyOnHidden
      >
        <Form<CreateUserInput>
          form={createForm}
          layout="vertical"
          initialValues={{ roles: ['dev'] }}
          onFinish={(v) => createMut.mutate(v)}
          style={{ marginTop: 16 }}
        >
          <Form.Item
            name="username"
            label="用户名"
            rules={[
              { required: true, message: '请输入用户名' },
              { pattern: /^[a-z0-9][a-z0-9_-]{1,31}$/, message: '2–32 位小写字母/数字/下划线/短横线' },
            ]}
          >
            <Input placeholder="如 zhangsan" autoComplete="off" />
          </Form.Item>
          <Form.Item name="displayName" label="姓名" rules={[{ required: true, message: '请输入姓名' }]}>
            <Input placeholder="如 张三" />
          </Form.Item>
          <Form.Item
            name="email"
            label="邮箱"
            rules={[
              { required: true, message: '请输入邮箱' },
              { type: 'email', message: '需为合法邮箱' },
            ]}
          >
            <Input placeholder="name@example.com" />
          </Form.Item>
          <Form.Item
            name="password"
            label="初始密码"
            rules={[{ required: true, min: 8, message: '至少 8 位' }]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Form.Item name="roles" label="角色" rules={[{ required: true, message: '至少选择一个角色' }]}>
            <Select mode="multiple" options={ROLE_OPTIONS} placeholder="选择全局角色" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={editing ? `编辑成员 · ${editing.username}` : '编辑成员'}
        open={editing !== null}
        onCancel={() => setEditing(null)}
        confirmLoading={updateMut.isPending}
        onOk={() => editForm.submit()}
        destroyOnHidden
      >
        <Form<EditForm>
          form={editForm}
          layout="vertical"
          onFinish={(v) =>
            editing && updateMut.mutate({ id: editing.id, body: { roles: v.roles, status: v.status } })
          }
          style={{ marginTop: 16 }}
        >
          <Form.Item name="roles" label="角色" rules={[{ required: true, message: '至少选择一个角色' }]}>
            <Select mode="multiple" options={ROLE_OPTIONS} />
          </Form.Item>
          <Form.Item name="status" label="状态" rules={[{ required: true }]}>
            <Select
              options={[
                { value: 'active', label: '启用' },
                { value: 'disabled', label: '停用' },
              ]}
            />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={resetTarget ? `重置密码 · ${resetTarget.username}` : '重置密码'}
        open={resetTarget !== null}
        onCancel={() => setResetTarget(null)}
        confirmLoading={resetMut.isPending}
        onOk={() => resetForm.submit()}
        destroyOnHidden
      >
        <Form<{ password: string }>
          form={resetForm}
          layout="vertical"
          onFinish={(v) => resetTarget && resetMut.mutate({ id: resetTarget.id, password: v.password })}
          style={{ marginTop: 16 }}
        >
          <Form.Item
            name="password"
            label="新密码"
            rules={[{ required: true, min: 8, message: '至少 8 位' }]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
