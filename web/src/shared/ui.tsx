// 通用错误展示：后端 message 直接可展示；404 语义统一为「不存在或不可见」。
import { Alert, Result, Spin } from 'antd'
import { ApiError } from './api'
import type { Blocker } from './types'

function asApiError(err: unknown): ApiError {
  return err instanceof ApiError
    ? err
    : new ApiError(0, 'UNKNOWN', err instanceof Error ? err.message : '未知错误')
}

/** 列表/页面级错误横幅。 */
export function ApiErrorBanner({ error }: { error: unknown }) {
  const e = asApiError(error)
  return <Alert type="error" showIcon message={e.message} style={{ margin: 16 }} />
}

/** 详情页错误：按响应呈现（404/403/503），不泄露存在性。 */
export function DetailError({ error }: { error: unknown }) {
  const e = asApiError(error)
  if (e.status === 404 || e.code === 'NOT_FOUND') {
    return <Result status="404" title="不存在或不可见" subTitle={e.message} />
  }
  return <Result status="warning" title="加载失败" subTitle={e.message} />
}

export function PageLoading({ tip = '加载中…' }: { tip?: string }) {
  return (
    <div style={{ padding: 48, textAlign: 'center' }}>
      <Spin tip={tip} size="large" />
    </div>
  )
}

/** next-action 返回的阻塞项明细（服务端事实，逐条处理）。 */
export function BlockerList({ blockers }: { blockers?: Blocker[] }) {
  if (!blockers || blockers.length === 0) return null
  return (
    <ul style={{ margin: '4px 0 8px', paddingLeft: 18, color: '#874d00' }}>
      {blockers.map((b, i) => (
        <li key={`${b.type}-${i}`}>{b.message}</li>
      ))}
    </ul>
  )
}
