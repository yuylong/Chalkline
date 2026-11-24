// 首页：当前迭代（docs/modules/planning.md「当前迭代」）。
// 登录后直接展示自己选定的当前迭代看板，省去「产品 → 迭代」逐级点击；
// 未设置时给出引导（去产品列表挑一个迭代，点「设为当前迭代」）。
import { useQuery } from '@tanstack/react-query'
import { Button, Card, Empty, Space, Typography } from 'antd'
import { useNavigate } from 'react-router'
import { apiGet } from '../../shared/api'
import { ApiErrorBanner, PageLoading } from '../../shared/ui'
import type { CurrentIterationView, ProductListResult } from '../../shared/types'
import { IterationPane } from '../iterations/Detail'

/** 未设置当前迭代时的引导：列出可见产品里未收尾的迭代供一键选择跳转。 */
function NoCurrentIteration() {
  const navigate = useNavigate()
  const productsQuery = useQuery({
    queryKey: ['products', '', '', 1, 'current-iter-guide'],
    queryFn: () => apiGet<ProductListResult>('/products?limit=50&offset=0'),
  })
  const products = productsQuery.data?.items ?? []

  return (
    <Card>
      <Empty description="还没有设置当前迭代">
        <Space direction="vertical" size={12} style={{ width: '100%', maxWidth: 520 }}>
          <Typography.Text type="secondary">
            打开任意迭代详情，点右上角「设为当前迭代」，之后登录就会直接看到它。
          </Typography.Text>
          <Button type="primary" onClick={() => navigate('/products')}>
            去产品列表挑迭代
          </Button>
          {products.length > 0 && (
            <div style={{ textAlign: 'left' }}>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                或直接打开某个产品的迭代列表：
              </Typography.Text>
              <Space wrap style={{ marginTop: 8 }}>
                {products.map((p) => (
                  <Button key={p.id} size="small" onClick={() => navigate(`/products/${p.id}`)}>
                    {p.key} · {p.name}
                  </Button>
                ))}
              </Space>
            </div>
          )}
        </Space>
      </Empty>
    </Card>
  )
}

export default function CurrentIterationPage() {
  const { data, isPending, isError, error } = useQuery({
    queryKey: ['current-iteration'],
    queryFn: () => apiGet<CurrentIterationView>('/current-iteration'),
  })

  if (isError) return <ApiErrorBanner error={error} />
  if (isPending) return <PageLoading />
  if (!data?.iteration) return <NoCurrentIteration />

  return (
    <div>
      <IterationPane iterationId={data.iteration.id} />
    </div>
  )
}
