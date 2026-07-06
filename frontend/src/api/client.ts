// API client：类型来自契约 codegen（src/api/schema.d.ts），错误统一走封套（§4.1）。
import type { paths } from './schema'
import { clearToken, getToken } from '../auth/token'

export type LoginResponse =
  paths['/auth/login']['post']['responses']['200']['content']['application/json']
export type Me = paths['/me']['get']['responses']['200']['content']['application/json']

type ErrorEnvelope = { error: { code: string; message: string } }

export class ApiError extends Error {
  readonly code: string
  readonly status: number

  constructor(status: number, code: string, message: string) {
    super(message)
    this.code = code
    this.status = status
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Content-Type', 'application/json')
  const token = getToken()
  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }
  const res = await fetch(`/api/v1${path}`, { ...init, headers })
  if (!res.ok) {
    let envelope: ErrorEnvelope | null = null
    try {
      envelope = (await res.json()) as ErrorEnvelope
    } catch {
      envelope = null
    }
    const code = envelope?.error.code ?? 'internal'
    if (res.status === 401) {
      clearToken()
    }
    throw new ApiError(res.status, code, envelope?.error.message ?? `请求失败（${res.status}）`)
  }
  return (await res.json()) as T
}

export function login(password: string): Promise<LoginResponse> {
  return request<LoginResponse>('/auth/login', {
    method: 'POST',
    body: JSON.stringify({ password }),
  })
}

export function fetchMe(): Promise<Me> {
  return request<Me>('/me')
}
