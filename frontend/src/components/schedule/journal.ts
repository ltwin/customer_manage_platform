import type { CreateOrderBody, CreateScheduleSlotBody, UpdateOrderBody } from '../../api/client'

export const pendingScheduleKey = 'schedule:create:pending'
const draftPrefix = 'schedule:draft:'
const pendingLifetimeMs = 24 * 60 * 60 * 1000
const draftLifetimeMs = 2 * 60 * 60 * 1000

export type ScheduleFlowPhase = 'order' | 'slot' | 'status_sync' | 'backfill_order'
export type ScheduleAttemptStep = 'order' | 'slot'
export type ScheduleNormalizedBody = CreateOrderBody | CreateScheduleSlotBody | UpdateOrderBody

export interface PendingScheduleFlow {
  flow_id: string
  phase: ScheduleFlowPhase
  source_draft_id?: string
  normalized_body: ScheduleNormalizedBody
  attempt_key: string
  known_customer_id?: string
  known_order_id?: string
  known_slot_id?: string
  prior_order_status?: string
  created_at: string
}

export interface ScheduleDraft {
  draft_id: string
  customer_id: string
  start_at: string
  end_at: string
  note?: string
  source: 'calendar' | 'customer'
  return_to: string
  created_at: string
  known_order_id?: string
}

export class ScheduleStorageError extends Error {}

export function newScheduleFlowID(): string {
  return crypto.randomUUID().replaceAll('-', '')
}

export function scheduleAttemptKey(flowID: string, step: ScheduleAttemptStep, version: number): string {
  return `scf:${flowID}:${step}:v${version}`
}

export function readPendingSchedule(storage: Storage = sessionStorage): PendingScheduleFlow | null {
  const raw = readStorage(storage, pendingScheduleKey)
  if (!raw) return null
  const value = parseJSON(raw, '待恢复排期记录已损坏') as Partial<PendingScheduleFlow>
  if (!value.flow_id || !value.phase || !value.attempt_key || !value.created_at || !value.normalized_body) {
    throw new ScheduleStorageError('待恢复排期记录字段不完整')
  }
  return value as PendingScheduleFlow
}

export function writePendingSchedule(value: PendingScheduleFlow, storage: Storage = sessionStorage): void {
  writeStorage(storage, pendingScheduleKey, JSON.stringify(value))
}

export function clearPendingSchedule(storage: Storage = sessionStorage): void {
  removeStorage(storage, pendingScheduleKey)
}

export function pendingScheduleExpired(value: PendingScheduleFlow, now = Date.now()): boolean {
  return now - Date.parse(value.created_at) >= pendingLifetimeMs
}

export function writeScheduleDraft(value: ScheduleDraft, storage: Storage = sessionStorage): void {
  writeStorage(storage, draftPrefix + value.draft_id, JSON.stringify(value))
}

export function readScheduleDraft(
  draftID: string,
  storage: Storage = sessionStorage,
  now = Date.now(),
  preserveExpired = false,
): ScheduleDraft | null {
  const raw = readStorage(storage, draftPrefix + draftID)
  if (!raw) return null
  const value = parseJSON(raw, '排期草稿已损坏') as Partial<ScheduleDraft>
  if (!value.draft_id || !value.customer_id || !value.start_at || !value.end_at || !value.created_at) {
    throw new ScheduleStorageError('排期草稿字段不完整')
  }
  if (!preserveExpired && now - Date.parse(value.created_at) >= draftLifetimeMs && !value.known_order_id) return null
  return value as ScheduleDraft
}

export function clearScheduleDraft(draftID: string, storage: Storage = sessionStorage): void {
  removeStorage(storage, draftPrefix + draftID)
}

function readStorage(storage: Storage, key: string): string | null {
  try {
    return storage.getItem(key)
  } catch {
    throw new ScheduleStorageError('浏览器存储不可用，暂不能开始排期')
  }
}

function writeStorage(storage: Storage, key: string, value: string): void {
  try {
    storage.setItem(key, value)
  } catch {
    throw new ScheduleStorageError('浏览器存储不可用，暂不能开始排期')
  }
}

function removeStorage(storage: Storage, key: string): void {
  try {
    storage.removeItem(key)
  } catch {
    throw new ScheduleStorageError('浏览器存储不可用，请重试')
  }
}

function parseJSON(raw: string, message: string): unknown {
  try {
    return JSON.parse(raw)
  } catch {
    throw new ScheduleStorageError(message)
  }
}
