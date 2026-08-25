// API client：类型来自契约 codegen（src/api/schema.d.ts），错误统一走封套（§4.1）。
import type { components, paths } from './schema'
import { authorizedFetch } from '../auth/session.ts'
import { ApiError, publicRequest, request, requestWithoutAuthRetry } from './transport.ts'
import type { ErrorEnvelope } from './transport.ts'

export { ApiError } from './transport.ts'
export type { ApiErrorDetails } from './transport.ts'

export type LoginResponse =
  paths['/auth/login']['post']['responses']['200']['content']['application/json']
export type AuthCapabilities =
  paths['/auth/capabilities']['get']['responses']['200']['content']['application/json']
export type VerificationDispatch =
  paths['/auth/register']['post']['responses']['202']['content']['application/json']
export type ForgotPasswordResponse =
  paths['/auth/password/forgot']['post']['responses']['202']['content']['application/json']
export type Me = paths['/me']['get']['responses']['200']['content']['application/json']
export type AccountProfile = components['schemas']['AccountProfile']
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
export type ScheduleSlotByID =
  paths['/schedule/slots/{id}']['get']['responses']['200']['content']['application/json']
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

export function fetchAuthCapabilities(): Promise<AuthCapabilities> {
  return publicRequest<AuthCapabilities>('/auth/capabilities')
}

export function registerAccount(email: string, password: string): Promise<VerificationDispatch> {
  return publicRequest<VerificationDispatch>('/auth/register', {
    method: 'POST',
    body: JSON.stringify({ email, password }),
  })
}

export function resendVerification(email: string): Promise<VerificationDispatch> {
  return publicRequest<VerificationDispatch>('/auth/email/resend', {
    method: 'POST',
    body: JSON.stringify({ email }),
  })
}

export function verifyEmail(token: string): Promise<LoginResponse> {
  return publicRequest<LoginResponse>('/auth/email/verify', {
    method: 'POST',
    body: JSON.stringify({ token }),
  })
}

export function login(email: string, password: string): Promise<LoginResponse> {
  return publicRequest<LoginResponse>('/auth/login', {
    method: 'POST',
    body: JSON.stringify({ email, password }),
  })
}

export function forgotPassword(email: string): Promise<ForgotPasswordResponse> {
  return publicRequest<ForgotPasswordResponse>('/auth/password/forgot', {
    method: 'POST',
    body: JSON.stringify({ email }),
  })
}

export function resetPassword(token: string, newPassword: string): Promise<void> {
  return publicRequest<void>('/auth/password/reset', {
    method: 'POST',
    body: JSON.stringify({ token, new_password: newPassword }),
  })
}

export function changePassword(currentPassword: string, newPassword: string): Promise<void> {
  return requestWithoutAuthRetry<void>('/auth/password/change', {
    method: 'POST',
    body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
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

export function fetchAccountProfile(): Promise<AccountProfile> {
	return request<AccountProfile>('/account/profile')
}

export function patchAccountProfile(
	profileRevision: string,
	displayName: string | null,
): Promise<AccountProfile> {
	return request<AccountProfile>('/account/profile', {
		method: 'PATCH',
		headers: { 'If-Match': `"${profileRevision}"` },
		body: JSON.stringify({ display_name: displayName }),
	})
}

export async function putAccountProfileAvatar(
	file: File,
	avatarRevision: string,
): Promise<AccountProfile> {
	const form = new FormData()
	form.set('file', file)
	return mediaJSONRequest<AccountProfile>('/account/profile/avatar', {
		method: 'PUT',
		headers: { 'If-Match': `"${avatarRevision}"` },
		body: form,
	})
}

export function deleteAccountProfileAvatar(avatarRevision: string): Promise<AccountProfile> {
	return request<AccountProfile>('/account/profile/avatar', {
		method: 'DELETE',
		headers: { 'If-Match': `"${avatarRevision}"` },
	})
}

export function deleteCustomerAvatar(id: string, avatarRevision: string): Promise<Customer> {
	return request<Customer>(`/customers/${encodeURIComponent(id)}/avatar`, {
		method: 'DELETE',
		headers: { 'If-Match': `"${avatarRevision}"` },
	})
}

export async function fetchAvatarBlob(url: string, signal: AbortSignal): Promise<Blob> {
	const response = await authorizedFetch(url, { signal })
	if (!response.ok) {
		throw await apiErrorFromResponse(response)
	}
	return response.blob()
}

export type DataExportDownload = {
	blob: Blob
	filename: string
}

const dataExportFilenamePattern = /^photographer-crm-export-[0-9]{8}T[0-9]{6}Z\.json$/
const mediaTypeTokenPattern = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/

export async function fetchDataExport(): Promise<DataExportDownload> {
	const headers = new Headers({ Accept: 'application/json' })
	const response = await authorizedFetch('/api/v1/export', { headers })
	if (!response.ok) throw await apiErrorFromResponse(response)
	if (!isJSONMediaType(response.headers.get('Content-Type'))) {
		throw new ApiError(502, 'invalid_export_response', '导出响应不是有效的 application/json')
	}
	const filename = exportFilenameFromDisposition(response.headers.get('Content-Disposition'))
	const blob = await response.blob()
	return { blob, filename }
}

function isJSONMediaType(raw: string | null): boolean {
	if (raw === null) return false
	const segments = splitMediaType(raw)
	if (segments === null || segments.length === 0 || segments[0]?.trim().toLowerCase() !== 'application/json') {
		return false
	}
	for (const rawParameter of segments.slice(1)) {
		const parameter = rawParameter.trim()
		const equals = parameter.indexOf('=')
		if (equals < 1) return false
		const name = parameter.slice(0, equals).trim()
		const value = parameter.slice(equals + 1).trim()
		if (!mediaTypeTokenPattern.test(name) || !validMediaTypeParameterValue(value)) return false
	}
	return true
}

function splitMediaType(raw: string): string[] | null {
	const segments: string[] = []
	let start = 0
	let quoted = false
	let escaped = false
	for (let index = 0; index < raw.length; index += 1) {
		const char = raw[index]
		if (escaped) {
			escaped = false
			continue
		}
		if (quoted && char === '\\') {
			escaped = true
			continue
		}
		if (char === '"') {
			quoted = !quoted
			continue
		}
		if (!quoted && char === ';') {
			segments.push(raw.slice(start, index))
			start = index + 1
		}
	}
	if (quoted || escaped) return null
	segments.push(raw.slice(start))
	return segments
}

function validMediaTypeParameterValue(value: string): boolean {
	if (mediaTypeTokenPattern.test(value)) return true
	if (value.length < 2 || value[0] !== '"' || value.at(-1) !== '"') return false
	let escaped = false
	for (const char of value.slice(1, -1)) {
		const code = char.charCodeAt(0)
		if (escaped) {
			if (code !== 9 && (code < 32 || code > 126)) return false
			escaped = false
			continue
		}
		if (char === '\\') {
			escaped = true
			continue
		}
		if (char === '"' || (code !== 9 && (code < 32 || code > 126))) return false
	}
	return !escaped
}

function exportFilenameFromDisposition(disposition: string | null): string {
	const match = disposition?.match(/^\s*attachment\s*;\s*filename\s*=\s*"([^"]+)"\s*$/i)
	const candidate = match?.[1] ?? ''
	return dataExportFilenamePattern.test(candidate) ? candidate : localDataExportFilename()
}

function localDataExportFilename(now = new Date()): string {
	const iso = now.toISOString()
	return `photographer-crm-export-${iso.slice(0, 10).replaceAll('-', '')}T${iso.slice(11, 19).replaceAll(':', '')}Z.json`
}

async function mediaJSONRequest<T>(path: string, init: RequestInit): Promise<T> {
	const headers = new Headers(init.headers)
	const response = await authorizedFetch(`/api/v1${path}`, { ...init, headers })
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
  id?: string
  customerId?: string
  status?: OrderStatus
  unpaidBalance?: boolean
  schedulableAt?: string
  page?: number
  pageSize?: number
} = {}): Promise<OrderListResponse> {
  const search = new URLSearchParams()
  if (params.id) search.set('id', params.id)
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

export function listScheduleSlots(from: string, to: string, signal?: AbortSignal): Promise<ScheduleSlotList> {
  const search = new URLSearchParams({ from, to })
  return request<ScheduleSlotList>(`/schedule/slots?${search.toString()}`, { signal })
}

export function getScheduleSlot(id: string, signal?: AbortSignal): Promise<ScheduleSlotByID> {
  return request<ScheduleSlotByID>(`/schedule/slots/${encodeURIComponent(id)}`, { signal })
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

export type DashboardV2 =
  paths['/dashboard/v2']['get']['responses']['200']['content']['application/json']
export type DashboardV2DeliveryItem = DashboardV2['delivery_queue']['items'][number]
export type DashboardV2Opening = DashboardV2['today_openings']['openings'][number]
export type DashboardV2WorkingWindow = NonNullable<
  DashboardV2['today_openings']['working_window']
>
export type DashboardV2Reminder = DashboardV2['due_reminders'][number]
export type DashboardV2Waterfall = DashboardV2['revenue_waterfall']

export function fetchDashboardV2(): Promise<DashboardV2> {
  return request<DashboardV2>('/dashboard/v2')
}

export type Reminder = components['schemas']['Reminder']
export type CustomerChannel = components['schemas']['CustomerChannel']
export type ReminderStatus = components['schemas']['ReminderStatus']
export type ReminderType = components['schemas']['ReminderType']
export type Settings = components['schemas']['Settings']
export type PlanningBusinessRuleOverrides = components['schemas']['PlanningBusinessRuleOverrides']
export type ScheduleAvailability = components['schemas']['ScheduleAvailability']
export type ScheduleAvailabilityWeekly = components['schemas']['ScheduleAvailabilityWeekly']
export type ScheduleAvailabilityWindow = components['schemas']['ScheduleAvailabilityWindow']
export type ChurnThreshold = components['schemas']['ChurnThreshold']
export type HealthTiers = components['schemas']['HealthTiers']
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
export type CreateTelegramBindTokenResponse =
  paths['/settings/telegram/bind-token']['post']['responses']['200']['content']['application/json']

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

export function getSettings(signal?: AbortSignal): Promise<Settings> {
  return request<Settings>('/settings', { signal })
}

export function updateSettings(body: UpdateSettingsBody): Promise<Settings> {
  return request<Settings>('/settings', {
    method: 'PATCH',
    body: JSON.stringify(body),
  })
}

export function createTelegramBindToken(): Promise<CreateTelegramBindTokenResponse> {
  return request<CreateTelegramBindTokenResponse>('/settings/telegram/bind-token', {
    method: 'POST',
  })
}
