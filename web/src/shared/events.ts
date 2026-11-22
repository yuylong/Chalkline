// SSE 事件流客户端（M3 实时推送）：fetch 流式读取 /api/v1/events。
// 不用 EventSource——它无法携带 Authorization 头，而 JWT 只存内存（console.md 不变量）。
// 断线指数退避重连（1s→30s）；返回取消函数。事件丢失无害：页面仍有轮询兜底。
import { getToken } from './api'
import type { StreamEvent } from './types'

const BASE = '/api/v1'
const MAX_BACKOFF_MS = 30_000

/** 打开事件流；onEvent 收到每条领域事件。返回关闭函数。 */
export function openEventStream(onEvent: (ev: StreamEvent) => void): () => void {
  let closed = false
  let backoffMs = 1_000
  let timer: ReturnType<typeof setTimeout> | undefined

  const connect = async (): Promise<void> => {
    if (closed) return
    const token = getToken()
    if (!token) {
      // 未登录：不重连，等登录后由 Layout 重新挂订阅。
      return
    }
    try {
      const res = await fetch(`${BASE}/events`, {
        headers: { Authorization: `Bearer ${token}`, Accept: 'text/event-stream' },
      })
      if (!res.ok || !res.body) throw new Error(`HTTP ${res.status}`)
      backoffMs = 1_000 // 连接成功即重置退避

      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      for (;;) {
        const { done, value } = await reader.read()
        if (done) throw new Error('stream closed')
        buffer += decoder.decode(value, { stream: true })
        // SSE 帧：event: <type>\ndata: <json>\n\n
        let idx: number
        while ((idx = buffer.indexOf('\n\n')) >= 0) {
          const frame = buffer.slice(0, idx)
          buffer = buffer.slice(idx + 2)
          const dataLine = frame
            .split('\n')
            .find((l) => l.startsWith('data: '))
          if (!dataLine) continue // 心跳注释帧（: ping）无 data
          try {
            onEvent(JSON.parse(dataLine.slice(6)) as StreamEvent)
          } catch {
            // 单帧解析失败：跳过，不中断流
          }
        }
      }
    } catch {
      if (closed) return
      timer = setTimeout(() => {
        void connect()
      }, backoffMs)
      backoffMs = Math.min(backoffMs * 2, MAX_BACKOFF_MS)
    }
  }

  void connect()
  return () => {
    closed = true
    if (timer) clearTimeout(timer)
  }
}
