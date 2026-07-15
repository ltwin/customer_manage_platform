// API client：类型来自契约 codegen（src/api/schema.d.ts），错误统一走封套（§4.1）。
import type { components, paths } from './schema'
import { clearToken, getToken } from '../auth/token.ts'

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
export type PackageListResponse =
  paths['/packages']['get']['responses']['200']['content']['application/json']
export type CreatePackageBody =
  paths['/packages']['post']['requestBody']['content']['application/json']
export type Package =
  paths['/packages']['post']['responses']['201']['content']['application/json']
export type UpdatePackageBody =
  paths['/packages/{id}']['patch']['requestBody']['content']['application/json']
export type PackageListStatus = NonNullable<
  paths['/packages']['get']['parameters']['query']
>['status']
export type OrderListResponse =
  paths['/orders']['get']['responses']['200']['content']['application/json']
export type OrderListItem = OrderListResponse['items'][number]
export type CreateOrderBody =
  paths['/orders']['post']['requestBody']['content']['application/json']
export type Order =
  paths['/orders']['post']['responses']['201']['content']['application/json']
export type UpdateOrderBody =
  paths['/orders/{id}']['patch']['requestBody']['content']['application/json']
export type OrderStatus = NonNullable<
  paths['/orders']['get']['parameters']['query']
>['status']
export type ScheduleSlotList =
  paths['/schedule/slots']['get']['responses']['200']['content']['application/json']
export type ScheduleSlotListItem = ScheduleSlotList[number]
export type CreateScheduleSlotBody =
  paths['/schedule/slots']['post']['requestBody']['content']['application/json']
export type CreateScheduleSlotResponse =
  paths['/schedule/slots']['post']['responses']['201']['content']['application/json']
export type ScheduleSlot = CreateScheduleSlotResponse['slot']
export type UpdateScheduleSlotBody =
  paths['/schedule/slots/{id}']['patch']['requestBody']['content']['application/json']
export type Dashboard =
  paths['/dashboard']['get']['responses']['200']['content']['application/json']
export type DashboardSlot = Dashboard['today_slots'][number]
export type DashboardUnpaidOrder = Dashboard['unpaid_orders']['items'][number]
export type DashboardReminder = Dashboard['due_reminders'][number]

type ErrorEnvelope = components['schemas']['ErrorEnvelope']
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

export async function putCustomerAvatar(id: string, file: File, avatarRevision: string): Promise<Customer> {
	const form = new FormData()
	form.set('file', file)
	return mediaJSONRequest<Customer>(`/customers/${encodeURIComponent(id)}/avatar`, {
		method: 'PUT',
		headers: { 'If-Match': `"${avatarRevision}"` },
		body: form,
	})
}

export function deleteCustomerAvatar(id: string, avatarRevision: string): Promise<Customer> {
	return request<Customer>(`/customers/${encodeURIComponent(id)}/avatar`, {
		method: 'DELETE',
		headers: { 'If-Match': `"${avatarRevision}"` },
	})
}

export async function fetchAvatarBlob(url: string, signal: AbortSignal): Promise<Blob> {
	const headers = new Headers()
	const token = getToken()
	if (token) headers.set('Authorization', `Bearer ${token}`)
	const response = await fetch(url, { headers, signal })
	if (!response.ok) {
		throw await apiErrorFromResponse(response)
	}
	return response.blob()
}

async function mediaJSONRequest<T>(path: string, init: RequestInit): Promise<T> {
	const headers = new Headers(init.headers)
	const token = getToken()
	if (token) headers.set('Authorization', `Bearer ${token}`)
	const response = await fetch(`/api/v1${path}`, { ...init, headers })
	if (!response.ok) throw await apiErrorFromResponse(response)
	return (await response.json()) as T
}

async function apiErrorFromResponse(response: Response): Promise<ApiError> {
	let envelope: ErrorEnvelope | null = null
	try {
		envelope = (await response.json()) as ErrorEnvelope
	} catch {
		envelope = null
	}
	if (response.status === 401) clearToken()
	return new ApiError(
		response.status,
		envelope?.error.code ?? 'internal',
		envelope?.error.message ?? `请求失败（${response.status}）`,
		envelope?.error.details,
	)
}

export function listPackages(params: {
  status?: PackageListStatus
  page?: number
  pageSize?: number
} = {}): Promise<PackageListResponse> {
  const search = new URLSearchParams()
  if (params.status) search.set('status', params.status)
  if (params.page) search.set('page', String(params.page))
  if (params.pageSize) search.set('page_size', String(params.pageSize))
  const suffix = search.toString() ? `?${search.toString()}` : ''
  return request<PackageListResponse>(`/packages${suffix}`)
}

export function createPackage(body: CreatePackageBody): Promise<Package> {
  return request<Package>('/packages', {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export function updatePackage(id: string, body: UpdatePackageBody): Promise<Package> {
  return request<Package>(`/packages/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(body),
  })
}

export function deletePackage(id: string): Promise<void> {
  return request<void>(`/packages/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export function listOrders(params: {
  customerId?: string
  status?: OrderStatus
  unpaidBalance?: boolean
  schedulableAt?: string
  page?: number
  pageSize?: number
} = {}): Promise<OrderListResponse> {
  const search = new URLSearchParams()
  if (params.customerId) search.set('customer_id', params.customerId)
  if (params.status) search.set('status', params.status)
  if (params.unpaidBalance) search.set('unpaid_balance', 'true')
  if (params.schedulableAt) search.set('schedulable_at', params.schedulableAt)
  if (params.page) search.set('page', String(params.page))
  if (params.pageSize) search.set('page_size', String(params.pageSize))
  const suffix = search.toString() ? `?${search.toString()}` : ''
  return request<OrderListResponse>(`/orders${suffix}`)
}

export function createOrder(body: CreateOrderBody, idempotencyKey?: string): Promise<Order> {
  const headers = new Headers()
  if (idempotencyKey) headers.set('Idempotency-Key', idempotencyKey)
  return request<Order>('/orders', {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
  })
}

export function updateOrder(id: string, body: UpdateOrderBody): Promise<Order> {
  return request<Order>(`/orders/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(body),
  })
}

export function deleteOrder(id: string): Promise<void> {
  return request<void>(`/orders/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export function listScheduleSlots(from: string, to: string): Promise<ScheduleSlotList> {
  const search = new URLSearchParams({ from, to })
  return request<ScheduleSlotList>(`/schedule/slots?${search.toString()}`)
}

export function createScheduleSlot(
  body: CreateScheduleSlotBody,
  idempotencyKey?: string,
): Promise<CreateScheduleSlotResponse> {
  const headers = new Headers()
  if (idempotencyKey) headers.set('Idempotency-Key', idempotencyKey)
  return request<CreateScheduleSlotResponse>('/schedule/slots', {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
  })
}

export function updateScheduleSlot(id: string, body: UpdateScheduleSlotBody): Promise<ScheduleSlot> {
  return request<ScheduleSlot>(`/schedule/slots/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(body),
  })
}

export function deleteScheduleSlot(id: string): Promise<void> {
  return request<void>(`/schedule/slots/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export function fetchDashboard(): Promise<Dashboard> {
  return request<Dashboard>('/dashboard')
}

export type Reminder = components['schemas']['Reminder']
export type ReminderStatus = components['schemas']['ReminderStatus']
export type ReminderType = components['schemas']['ReminderType']
export type Settings = components['schemas']['Settings']
export type ChurnThreshold = components['schemas']['ChurnThreshold']
export type ReminderListResponse =
  paths['/reminders']['get']['responses']['200']['content']['application/json']
export type CreateReminderBody =
  paths['/reminders']['post']['requestBody']['content']['application/json']
export type ScanRemindersBody = NonNullable<
  paths['/admin/reminders/scan']['post']['requestBody']
>['content']['application/json']
export type ScanRemindersResult =
  paths['/admin/reminders/scan']['post']['responses']['200']['content']['application/json']
export type UpdateSettingsBody =
  paths['/settings']['patch']['requestBody']['content']['application/json']

export function listReminders(params: {
  status?: ReminderStatus
  customerId?: string
  dueBefore?: string
  page?: number
  pageSize?: number
} = {}): Promise<ReminderListResponse> {
  const search = new URLSearchParams()
  if (params.status) search.set('status', params.status)
  if (params.customerId) search.set('customer_id', params.customerId)
  if (params.dueBefore) search.set('due_before', params.dueBefore)
  if (params.page) search.set('page', String(params.page))
  if (params.pageSize) search.set('page_size', String(params.pageSize))
  const suffix = search.toString() ? `?${search.toString()}` : ''
  return request<ReminderListResponse>(`/reminders${suffix}`)
}

export function createReminder(body: CreateReminderBody): Promise<Reminder> {
  return request<Reminder>('/reminders', {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export function markReminderDone(id: string): Promise<Reminder> {
  return request<Reminder>(`/reminders/${encodeURIComponent(id)}/done`, { method: 'POST' })
}

export function dismissReminder(id: string): Promise<Reminder> {
  return request<Reminder>(`/reminders/${encodeURIComponent(id)}/dismiss`, { method: 'POST' })
}

export function scanReminders(body: ScanRemindersBody = {}): Promise<ScanRemindersResult> {
  return request<ScanRemindersResult>('/admin/reminders/scan', {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export function getSettings(): Promise<Settings> {
  return request<Settings>('/settings')
}

export function updateSettings(body: UpdateSettingsBody): Promise<Settings> {
  return request<Settings>('/settings', {
    method: 'PATCH',
    body: JSON.stringify(body),
  })
}
