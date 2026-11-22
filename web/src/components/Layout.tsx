// 登录后布局：侧边导航（产品/待审批）+ 用户名下拉（登出）。
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Avatar, Badge, Dropdown, Layout as AntLayout, Menu, Typography } from 'antd'
import { Outlet, useLocation, useNavigate } from 'react-router'
import { apiGet, setToken } from '../shared/api'
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

  const selected = location.pathname.startsWith('/reviews')
    ? '/reviews'
    : location.pathname === '/' || location.pathname.startsWith('/products')
      ? '/'
      : location.pathname

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
            { key: '/', label: '产品' },
            {
              key: '/reviews',
              label: (
                <Badge count={pendingCount} size="small" offset={[10, 0]}>
                  待审批
                </Badge>
              ),
            },
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
                {whoami?.user.displayName || whoami?.user.username || '…'}
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
