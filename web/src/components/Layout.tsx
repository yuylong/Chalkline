// 登录后布局：侧边导航（当前迭代/审批 ｜ 产品/审计/MCP 接入 ｜ 用户管理/
// Provider 管理/工作流管理/Webhook 订阅）+ 用户名下拉（登出）。
// M3：订阅 SSE 事件流，事件到达即失效相关缓存（轮询仍兜底）。
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Avatar, Badge, Dropdown, Layout as AntLayout, Menu, Typography } from 'antd'
import { useEffect } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router'
import { apiGet, setToken } from '../shared/api'
import { openEventStream } from '../shared/events'
import { userLabel } from '../shared/status'
import type { ListEnvelope, PendingReviewView, WhoamiView } from '../shared/types'

const REVIEW_POLL_MS = 10_000

export default function Layout() {
  const navigate = useNavigate()
  const location = useLocation()
  const queryClient = useQueryClient()

  const { data: whoami } = useQuery({
    queryKey: ['whoami'],
    queryFn: () => apiGet<WhoamiView>('/auth/me'),
  })
  // 与审批队列共用缓存键，徽标与队列页数据一致。
  const { data: pending } = useQuery({
    queryKey: ['reviews', 'pending'],
    queryFn: () => apiGet<ListEnvelope<PendingReviewView>>('/reviews/pending'),
    refetchInterval: REVIEW_POLL_MS,
  })
  const pendingCount = pending?.items.length ?? 0

  // M3 实时推送：任一领域事件到达 → 失效相关查询，TanStack Query 自动重取。
  useEffect(() => {
    return openEventStream((ev) => {
      switch (ev.subjectType) {
        case 'task':
          queryClient.invalidateQueries({ queryKey: ['tasks'] })
          queryClient.invalidateQueries({ queryKey: ['task'] })
          queryClient.invalidateQueries({ queryKey: ['task-workflow'] })
          queryClient.invalidateQueries({ queryKey: ['next-action', 'task'] })
          break
        case 'iteration':
          queryClient.invalidateQueries({ queryKey: ['iterations'] })
          queryClient.invalidateQueries({ queryKey: ['iteration'] })
          queryClient.invalidateQueries({ queryKey: ['iteration-workflow'] })
          queryClient.invalidateQueries({ queryKey: ['next-action', 'iteration'] })
          // 首页「当前迭代」随迭代推进刷新。
          queryClient.invalidateQueries({ queryKey: ['current-iteration'] })
          break
        case 'product':
          queryClient.invalidateQueries({ queryKey: ['products'] })
          queryClient.invalidateQueries({ queryKey: ['product'] })
          break
      }
      // 门禁类事件影响审批队列与概览；其余事件（task.assigned 等）一并刷新队列。
      queryClient.invalidateQueries({ queryKey: ['reviews'] })
      queryClient.invalidateQueries({ queryKey: ['overview'] })
    })
  }, [queryClient])

  const isAdmin = whoami?.user.roles.includes('admin') ?? false

  // 菜单顺序：当前迭代、审批 ｜ 产品、审计、MCP 接入 ｜ 用户管理、Provider 管理、工作流管理、Webhook 订阅。
  // 管理项仅 admin 可见（后端仍会拒绝非 admin 请求）；MCP 接入对全员开放（PAT 属个人凭据）。
  // 页面路由用 /mcpconfig，与 MCP 服务端点 POST /mcp 区分（nginx 把 /mcp 反代给 api）。
  const selected = location.pathname.startsWith('/reviews')
    ? '/reviews'
    : location.pathname.startsWith('/audit')
      ? '/audit'
      : location.pathname.startsWith('/mcpconfig')
        ? '/mcpconfig'
        : location.pathname.startsWith('/credentials')
          ? '/credentials'
          : location.pathname.startsWith('/smtp')
        ? '/smtp'
        : location.pathname.startsWith('/ldap')
        ? '/ldap'
        : location.pathname.startsWith('/webhooks')
        ? '/webhooks'
        : location.pathname.startsWith('/users')
          ? '/users'
          : location.pathname.startsWith('/workflows')
            ? '/workflows'
            : location.pathname.startsWith('/integrations')
              ? '/integrations'
              : location.pathname.startsWith('/products')
                ? '/products'
                : '/' // 首页（当前迭代）以及迭代/任务详情都归属「当前迭代」

  const adminItems = isAdmin
    ? [
        { key: '/users', label: '用户管理' },
        { key: '/integrations', label: 'Provider 管理' },
      ]
    : []

  return (
    <AntLayout style={{ minHeight: '100vh' }}>
      <AntLayout.Sider theme="light" width={200}>
        <div style={{ padding: '20px 16px 12px' }}>
          <Typography.Title level={4} style={{ margin: 0 }}>
            Chalk<span style={{ color: '#c2410c' }}>line</span>
          </Typography.Title>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            墨线 · 规格驱动协作
          </Typography.Text>
        </div>
        <Menu
          mode="inline"
          selectedKeys={[selected]}
          items={[
            { key: '/', label: '当前迭代' },
            {
              key: '/reviews',
              label: (
                <Badge count={pendingCount} size="small" offset={[10, 0]}>
                  审批
                </Badge>
              ),
            },
            { type: 'divider' },
            { key: '/products', label: '产品' },
            ...(isAdmin ? [{ key: '/audit', label: '审计' }] : []),
            { key: '/mcpconfig', label: 'MCP 接入' },
            { key: '/credentials', label: '我的凭据' },
            { type: 'divider' },
            ...adminItems,
            { key: '/workflows', label: '工作流管理' },
            ...(isAdmin
              ? [
                  { key: '/smtp', label: '邮箱配置' },
                  { key: '/ldap', label: 'LDAP 认证' },
                  { key: '/webhooks', label: 'Webhook 订阅' },
                ]
              : []),
          ]}
          onClick={({ key }) => navigate(key)}
        />
      </AntLayout.Sider>
      <AntLayout>
        <AntLayout.Header
          style={{
            background: '#fff',
            display: 'flex',
            justifyContent: 'flex-end',
            alignItems: 'center',
            paddingInline: 24,
            borderBottom: '1px solid #f0f0f0',
          }}
        >
          <Dropdown
            menu={{
              items: [{ key: 'logout', label: '登出' }],
              onClick: ({ key }) => {
                if (key === 'logout') {
                  setToken(null)
                  queryClient.clear()
                  navigate('/login', { replace: true })
                }
              },
            }}
          >
            <span style={{ cursor: 'pointer', display: 'inline-flex', alignItems: 'center', gap: 8 }}>
              <Avatar style={{ background: '#c2410c' }}>
                {(whoami?.user.displayName || whoami?.user.username || '?').slice(0, 1)}
              </Avatar>
              <Typography.Text>
                {whoami ? userLabel(whoami.user) : '…'}
              </Typography.Text>
            </span>
          </Dropdown>
        </AntLayout.Header>
        <AntLayout.Content style={{ padding: 16, background: '#f6f7f9' }}>
          <Outlet />
        </AntLayout.Content>
      </AntLayout>
    </AntLayout>
  )
}
