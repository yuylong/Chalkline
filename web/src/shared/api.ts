// API 客户端：baseURL /api/v1、JWT 头、错误包络解析、401 清 token 跳登录。
// JWT 只存内存（模块不变量：不用 localStorage/sessionStorage），刷新即回登录页。
import type { LoginResult } from './types'

const BASE = '/api/v1'

let token: string | null = null

export function setToken(v: string | null): void {
  token = v
}

export function getToken(): string | null {
  return token
}

/** 统一错误：message 可直接展示（后端保证不泄露内部细节）。 */
export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly details?: Record<string, unknown>

  constructor(status: number, code: string, message: string, details?: Record<string, unknown>) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.details = details
  }
}

function fallbackMessage(status: number): string {
  if (status === 503) return '后端未就绪，请稍后重试'
  if (status === 404) return '资源不存在或不可见'
  if (status === 403) return '没有执行该操作的权限'
  return `请求失败（HTTP ${status}）`
}

interface Envelope {
  error?: { code?: string; message?: string; details?: Record<string, unknown> }
}

interface RequestOptions {
  body?: unknown
  /** 登录接口自身返回 401 时不跳转，交由登录页展示错误。 */
  noAuthRedirect?: boolean
}

async function request<T>(method: string, path: string, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (token) headers.Authorization = `Bearer ${token}`
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json'

  let res: Response
  try {
    res = await fetch(`${BASE}${path}`, {
      method,
      headers,
      body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
    })
  } catch {
    throw new ApiError(0, 'NETWORK_ERROR', '网络错误：无法连接服务器')
  }

  if (res.status === 204) return undefined as T
  let payload: unknown = null
  try {
    payload = await res.json()
  } catch {
    payload = null
  }

  if (!res.ok) {
    const env = payload as Envelope | null
    const code = env?.error?.code ?? 'UNKNOWN'
    const message = env?.error?.message?.trim() || fallbackMessage(res.status)
    if (res.status === 401 && !opts.noAuthRedirect) {
      setToken(null)
      if (!window.location.pathname.startsWith('/login')) window.location.assign('/login')
    }
    throw new ApiError(res.status, code, message, env?.error?.details)
  }
  return payload as T
}

export function apiGet<T>(path: string): Promise<T> {
  return request<T>('GET', path)
}

export function apiPost<T>(path: string, body?: unknown): Promise<T> {
  return request<T>('POST', path, { body })
}

export function apiPatch<T>(path: string, body?: unknown): Promise<T> {
  return request<T>('PATCH', path, { body })
}

export function apiDelete<T>(path: string): Promise<T> {
  return request<T>('DELETE', path)
}

/** 登录：用户名密码换 JWT；失败返回 401 且不触发跳转。 */
export function apiLogin(username: string, password: string): Promise<LoginResult> {
  return request<LoginResult>('POST', '/auth/login', {
    body: { username, password },
    noAuthRedirect: true,
  })
}
