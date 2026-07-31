import type { components } from './schema'
import { authorizedFetch } from '../auth/session.ts'

export type ErrorEnvelope = components['schemas']['ErrorEnvelope']
export type ApiErrorDetails = NonNullable<ErrorEnvelope['error']['details']>

export class ApiError extends Error {
  readonly code: string
  readonly status: number
  readonly details?: ApiErrorDetails

  constructor(status: number, code: string, message: string, details?: ApiErrorDetails) {
    super(message)
    this.code = code
    this.status = status
    this.details = details
  }
}

export async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Content-Type', 'application/json')
  const res = await authorizedFetch(`/api/v1${path}`, { ...init, headers })
  return responseBody<T>(res)
}

export async function publicRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Content-Type', 'application/json')
  const res = await fetch(`/api/v1${path}`, {
    ...init,
    credentials: init.credentials ?? 'same-origin',
    headers,
  })
  return responseBody<T>(res)
}

async function responseBody<T>(res: Response): Promise<T> {
  if (!res.ok) {
    let envelope: ErrorEnvelope | null = null
    try {
      envelope = (await res.json()) as ErrorEnvelope
    } catch {
      envelope = null
    }
    const code = envelope?.error.code ?? 'internal'
    throw new ApiError(
      res.status,
      code,
      envelope?.error.message ?? `请求失败（${res.status}）`,
      envelope?.error.details,
    )
  }
  if (res.status === 204) {
    return undefined as T
  }
  return (await res.json()) as T
}
