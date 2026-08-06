import type { components, paths } from '../api/schema'
import { ApiError } from '../api/client'
import { clearToken, getToken } from '../auth/token'

export type ShootPlanStatus = components['schemas']['ShootPlanStatus']
export type ShootPlanList = components['schemas']['ShootPlanList']
export type ShootPlanListItem = components['schemas']['ShootPlanListItem']
export type ShootPlanDetail = components['schemas']['ShootPlanDetail']
export type ShootPlanShot = components['schemas']['ShootPlanShot']
export type ShootPlanReadinessItem = components['schemas']['ShootPlanReadinessItem']
export type ShotExecutionFact = components['schemas']['ShotExecutionFact']
export type PlanCommand = components['schemas']['PlanCommand']
export type PlanMutationResult = components['schemas']['PlanMutationResult']
export type PlanTransition = components['schemas']['PlanTransition']
export type PlanTransitionResult = components['schemas']['PlanTransitionResult']
export type ArchiveAcknowledgement = components['schemas']['ArchiveAcknowledgement']
export type CreateShootPlanInput = components['schemas']['CreateShootPlanInput']
export type AppendShotResultInput = components['schemas']['AppendShotResultInput']
export type AppendShotResultResponse = components['schemas']['AppendShotResultResponse']
export type OpenRunSessionResult = components['schemas']['OpenRunSessionResult']
export type RunInputSnapshot = components['schemas']['RunInputSnapshot']
export type ShootPlanSkipReason = components['schemas']['ShootPlanSkipReason']
export type VoidExecutionEventInput = components['schemas']['VoidExecutionEventInput']

export function listShootPlans(params: {
  status?: ShootPlanStatus
  archived?: boolean
  page?: number
  pageSize?: number
} = {}): Promise<ShootPlanList> {
  const search = new URLSearchParams()
  if (params.status) search.set('status', params.status)
  if (params.archived !== undefined) search.set('archived', String(params.archived))
  if (params.page) search.set('page', String(params.page))
  if (params.pageSize) search.set('page_size', String(params.pageSize))
  const query = search.toString()
  return request<ShootPlanList>(`/shoot-plans${query ? `?${query}` : ''}`)
}

export function createShootPlan(body: CreateShootPlanInput, idempotencyKey: string): Promise<ShootPlanDetail> {
  return request<ShootPlanDetail>('/shoot-plans', {
    method: 'POST',
    headers: { 'Idempotency-Key': idempotencyKey },
    body: JSON.stringify(body),
  })
}

export function getShootPlan(id: string, includeHistory = false): Promise<ShootPlanDetail> {
  const query = includeHistory ? '?include=execution_history' : ''
  return request<ShootPlanDetail>(`/shoot-plans/${encodeURIComponent(id)}${query}`)
}

export function applyShootPlanCommand(
  id: string,
  body: PlanCommand,
  idempotencyKey: string,
): Promise<PlanMutationResult> {
  return request<PlanMutationResult>(`/shoot-plans/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    headers: { 'Idempotency-Key': idempotencyKey },
    body: JSON.stringify(body),
  })
}

export function transitionShootPlan(
  id: string,
  body: PlanTransition,
  idempotencyKey: string,
): Promise<PlanTransitionResult> {
  return request<PlanTransitionResult>(`/shoot-plans/${encodeURIComponent(id)}/transitions`, {
    method: 'POST',
    headers: { 'Idempotency-Key': idempotencyKey },
    body: JSON.stringify(body),
  })
}

export function openShootPlanRunSession(
  id: string,
  expectedRevision: number,
  idempotencyKey: string,
): Promise<OpenRunSessionResult> {
  return request<OpenRunSessionResult>(`/shoot-plans/${encodeURIComponent(id)}/run-sessions`, {
    method: 'POST',
    headers: { 'Idempotency-Key': idempotencyKey },
    body: JSON.stringify({ expected_revision: expectedRevision }),
  })
}

export function appendShootPlanShotResult(
  planID: string,
  shotID: string,
  body: AppendShotResultInput,
  idempotencyKey: string,
): Promise<AppendShotResultResponse> {
  return request<AppendShotResultResponse>(`/shoot-plans/${encodeURIComponent(planID)}/shots/${encodeURIComponent(shotID)}/capture`, {
    method: 'POST',
    headers: { 'Idempotency-Key': idempotencyKey },
    body: JSON.stringify(body),
  })
}

export function voidShootPlanExecutionEvent(
  planID: string,
  eventID: string,
  body: VoidExecutionEventInput,
  idempotencyKey: string,
): Promise<paths['/shoot-plans/{id}/execution-events/{eventId}/void']['post']['responses']['201']['content']['application/json']> {
  return request(`/shoot-plans/${encodeURIComponent(planID)}/execution-events/${encodeURIComponent(eventID)}/void`, {
    method: 'POST',
    headers: { 'Idempotency-Key': idempotencyKey },
    body: JSON.stringify(body),
  })
}

export function newPlanningMutationKey(scope: string): string {
  const random = typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`
  return `shoot-plan-${scope}-${random}`
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Content-Type', 'application/json')
  const token = getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)
  const response = await fetch(`/api/v1${path}`, { ...init, headers })
  if (!response.ok) {
    let envelope: components['schemas']['ErrorEnvelope'] | null = null
    try {
      envelope = await response.json() as components['schemas']['ErrorEnvelope']
    } catch {
      envelope = null
    }
    if (response.status === 401) clearToken()
    throw new ApiError(response.status, envelope?.error.code ?? 'internal', envelope?.error.message ?? `请求失败（${response.status}）`, envelope?.error.details)
  }
  return await response.json() as T
}
