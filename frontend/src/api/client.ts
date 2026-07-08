// API client：类型来自契约 codegen（src/api/schema.d.ts），错误统一走封套（§4.1）。
import type { paths } from './schema'
import { clearToken, getToken } from '../auth/token'

export type LoginResponse =
  paths['/auth/login']['post']['responses']['200']['content']['application/json']
export type Me = paths['/me']['get']['responses']['200']['content']['application/json']
export type CustomerListResponse =
  paths['/customers']['get']['responses']['200']['content']['application/json']
export type CustomerDetail =
  paths['/customers/{id}']['get']['responses']['200']['content']['application/json']
export type CreateCustomerBody =
  paths['/customers']['post']['requestBody']['content']['application/json']
export type Customer =
  paths['/customers']['post']['responses']['201']['content']['application/json']
export type UpdateCustomerBody =
  paths['/customers/{id}']['patch']['requestBody']['content']['application/json']
export type AddIdentityBody =
  paths['/customers/{id}/identities']['post']['requestBody']['content']['application/json']
export type SocialIdentity =
  paths['/customers/{id}/identities']['post']['responses']['201']['content']['application/json']
export type CustomerNote =
  paths['/customers/{id}/notes']['post']['responses']['201']['content']['application/json']
export type CustomerListStatus = NonNullable<
  paths['/customers']['get']['parameters']['query']
>['status']

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
  if (res.status === 204) {
    return undefined as T
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

export function listCustomers(params: {
  q?: string
  channel?: string
  status?: CustomerListStatus
  page?: number
  pageSize?: number
} = {}): Promise<CustomerListResponse> {
  const search = new URLSearchParams()
  if (params.q) search.set('q', params.q)
  if (params.channel) search.set('channel', params.channel)
  if (params.status) search.set('status', params.status)
  if (params.page) search.set('page', String(params.page))
  if (params.pageSize) search.set('page_size', String(params.pageSize))
  const suffix = search.toString() ? `?${search.toString()}` : ''
  return request<CustomerListResponse>(`/customers${suffix}`)
}

export function createCustomer(body: CreateCustomerBody): Promise<Customer> {
  return request<Customer>('/customers', {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export function fetchCustomer(id: string): Promise<CustomerDetail> {
  return request<CustomerDetail>(`/customers/${encodeURIComponent(id)}`)
}

export function updateCustomer(id: string, body: UpdateCustomerBody): Promise<Customer> {
  return request<Customer>(`/customers/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(body),
  })
}

export function addCustomerIdentity(id: string, body: AddIdentityBody): Promise<SocialIdentity> {
  return request<SocialIdentity>(`/customers/${encodeURIComponent(id)}/identities`, {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export function deleteCustomerIdentity(id: string, identityId: string): Promise<void> {
  return request<void>(
    `/customers/${encodeURIComponent(id)}/identities/${encodeURIComponent(identityId)}`,
    { method: 'DELETE' },
  )
}

export function addCustomerNote(id: string, content: string): Promise<CustomerNote> {
  return request<CustomerNote>(`/customers/${encodeURIComponent(id)}/notes`, {
    method: 'POST',
    body: JSON.stringify({ content }),
  })
}

export function mergeCustomer(id: string, sourceCustomerId: string): Promise<Customer> {
  return request<Customer>(`/customers/${encodeURIComponent(id)}/merge`, {
    method: 'POST',
    body: JSON.stringify({ source_customer_id: sourceCustomerId }),
  })
}
