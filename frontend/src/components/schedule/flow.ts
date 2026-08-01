import {
  clearPendingSchedule,
  pendingScheduleExpired,
  readPendingSchedule,
  readScheduleDraft,
  scheduleAttemptKey,
  writeScheduleDraft,
  writePendingSchedule,
} from './journal.ts'
import type {
  PendingScheduleFlow,
  ScheduleDraft,
  ScheduleNormalizedBody,
} from './journal.ts'
import type { CreateOrderBody, OrderListItem } from '../../api/client.ts'
import { instantToLocalDateTime } from './timezone.ts'

export type ScheduleRecoveryMode = 'unknown' | 'idempotency_conflict' | 'customer_changed' | 'deterministic' | null
export type ScheduleCreateFailureKind = Exclude<ScheduleRecoveryMode, null>

export class BackfillPersistenceError extends Error {
  readonly stage: 'pending' | 'draft' | 'clear'
  readonly knownOrderID: string

  constructor(
    stage: 'pending' | 'draft' | 'clear',
    knownOrderID: string,
    options?: ErrorOptions,
  ) {
    super('补录订单本地收口失败', options)
    this.stage = stage
    this.knownOrderID = knownOrderID
  }
}

export function claimPendingFlow(
  flow: PendingScheduleFlow,
  storage: Storage = sessionStorage,
): PendingScheduleFlow | null {
  const existing = readPendingSchedule(storage)
  if (existing) return existing
  writePendingSchedule(flow, storage)
  return null
}

export function readScheduleBackfillContext(
  draftID: string,
  storage: Storage = sessionStorage,
  now = Date.now(),
): { draft: ScheduleDraft | null; pending: PendingScheduleFlow | null } {
  const stored = readPendingSchedule(storage)
  const pending = stored?.phase === 'backfill_order' && stored.source_draft_id === draftID
    ? stored
    : null
  const draft = readScheduleDraft(draftID, storage, now, Boolean(pending))
  return { draft, pending }
}

export function orderInUseScheduleAction(
  error: { code: string; details?: unknown },
  timezone: string | null,
): { href: string; label: string } | null {
  if (error.code !== 'order_in_use') return null
  if (timezone && error.details && typeof error.details === 'object') {
    const details = error.details as Record<string, unknown>
    const slotID = details.schedule_slot_id
    const startAt = details.schedule_start_at
    if (typeof slotID === 'string' && slotID && typeof startAt === 'string') {
      try {
        const date = instantToLocalDateTime(startAt, timezone).date
        return {
          href: `/calendar?date=${date}&slot=${encodeURIComponent(slotID)}`,
          label: '查看占用档期',
        }
      } catch {
        // Fall through to the safe calendar-level action below.
      }
    }
  }
  return { href: '/calendar', label: '前往日历查找关联档期' }
}

export function scheduleKnownSlotAction(
  flow: PendingScheduleFlow,
  timezone: string | null,
  sourceDraft: ScheduleDraft | null,
): { href: string; label: string } | null {
  if (!flow.known_slot_id) return null
  let startAt = sourceDraft?.start_at
  if (!startAt && flow.phase === 'slot' && flow.normalized_body && typeof flow.normalized_body === 'object') {
    const candidate = (flow.normalized_body as Record<string, unknown>).start_at
    if (typeof candidate === 'string') startAt = candidate
  }
  if (timezone && startAt) {
    try {
      const date = instantToLocalDateTime(startAt, timezone).date
      return {
        href: `/calendar?date=${date}&slot=${encodeURIComponent(flow.known_slot_id)}`,
        label: '查看档期',
      }
    } catch {
      // Use the calendar-level fallback when the stored timestamp is malformed.
    }
  }
  return { href: '/calendar', label: '前往日历核对档期' }
}

export function scheduleDialogShouldOpen(
  pendingPhase: PendingScheduleFlow['phase'] | undefined,
  scheduleDraftID: string | undefined,
  scheduleMode: string | undefined,
): boolean {
  if (pendingPhase === 'backfill_order' || scheduleMode === 'backfill') return false
  return Boolean(pendingPhase || scheduleDraftID)
}

export function scheduleRecoveryActions(
  flow: PendingScheduleFlow,
  expired: boolean,
  mode: ScheduleRecoveryMode,
  sourceDraft: ScheduleDraft | null,
) {
  if (expired) {
    return {
      resume: false,
      reset: false,
      keep: false,
      compensateOrder: false,
      deleteSlot: false,
      abandon: true,
    }
  }
  if (mode === 'idempotency_conflict') {
    return {
      resume: false,
      reset: false,
      keep: false,
      compensateOrder: false,
      deleteSlot: false,
      abandon: false,
    }
  }
  const resultKnown = mode === 'deterministic' || mode === 'customer_changed'
  const pathACreatedOrder = Boolean(
    resultKnown
    && flow.known_order_id
    && !flow.known_slot_id
    && flow.source_draft_id
    && sourceDraft?.draft_id === flow.source_draft_id
    && !sourceDraft.known_order_id,
  )
  return {
    resume: mode === 'unknown' || mode === 'deterministic',
    reset: mode === 'customer_changed'
      || (mode === 'deterministic' && flow.phase === 'slot' && !flow.known_slot_id),
    keep: resultKnown,
    compensateOrder: pathACreatedOrder,
    deleteSlot: mode === 'deterministic' && Boolean(flow.known_slot_id),
    abandon: false,
  }
}

export function missingSelectedOrderOption(
  selectedOrderID: string,
  orders: ReadonlyArray<{ id: string }>,
): boolean {
  return Boolean(selectedOrderID) && !orders.some((order) => order.id === selectedOrderID)
}

export function pendingForAutomaticReplay(
  flow: PendingScheduleFlow,
  now = Date.now(),
): PendingScheduleFlow {
  if (pendingScheduleExpired(flow, now)) {
    throw new Error('恢复记录已超过 24 小时，禁止自动重放')
  }
  return flow
}

export function scheduleRequestResultKnown(flow: PendingScheduleFlow): boolean {
  if (flow.phase === 'slot') return Boolean(flow.known_slot_id)
  if (flow.phase === 'order' || flow.phase === 'backfill_order') {
    return Boolean(flow.known_order_id)
  }
  return statusSyncSatisfied(flow.prior_order_status ?? '')
}

export function scheduleCreateFailureKind(
  error: { status: number; code: string } | null,
  resultKnown: boolean,
): ScheduleCreateFailureKind {
  if (error?.code === 'idempotency_conflict') return 'idempotency_conflict'
  if (resultKnown) return 'deterministic'
  if (!error || error.status >= 500) return 'unknown'
  if (error.code === 'customer_changed') return 'customer_changed'
  return 'deterministic'
}

export function pendingAfterDeterministicFailure(
  flow: PendingScheduleFlow,
  normalizedBody: ScheduleNormalizedBody,
): PendingScheduleFlow {
  if (canonicalJSON(flow.normalized_body) === canonicalJSON(normalizedBody)) {
    return { ...flow, normalized_body: normalizedBody }
  }
  const step = flow.phase === 'slot' ? 'slot' : 'order'
  const version = Number(flow.attempt_key.match(/:v(\d+)$/)?.[1] ?? '1') + 1
  return {
    ...flow,
    normalized_body: normalizedBody,
    attempt_key: scheduleAttemptKey(flow.flow_id, step, version),
  }
}

export async function completeBackfillOrder(
  flow: PendingScheduleFlow,
  draft: ScheduleDraft,
  createOrder: (body: CreateOrderBody, key: string) => Promise<{ id: string }>,
  knownOrderID?: string,
  storage: Storage = sessionStorage,
): Promise<{ orderID: string; pending: PendingScheduleFlow; draft: ScheduleDraft }> {
  const orderID = knownOrderID
    ?? flow.known_order_id
    ?? (await createOrder(flow.normalized_body as CreateOrderBody, flow.attempt_key)).id
  const confirmed = { ...flow, known_order_id: orderID }
  const completedDraft = { ...draft, known_order_id: orderID }
  try {
    writePendingSchedule(confirmed, storage)
  } catch (cause) {
    throw new BackfillPersistenceError('pending', orderID, { cause })
  }
  try {
    writeScheduleDraft(completedDraft, storage)
  } catch (cause) {
    throw new BackfillPersistenceError('draft', orderID, { cause })
  }
  try {
    clearPendingSchedule(storage)
  } catch (cause) {
    throw new BackfillPersistenceError('clear', orderID, { cause })
  }
  return { orderID, pending: confirmed, draft: completedDraft }
}

export async function locateStatusSyncOrder(
  orderID: string,
  customerID: string,
  refreshCustomerID: () => Promise<string>,
  loadPage: (customerID: string | undefined, page: number) => Promise<{ items: OrderListItem[]; total: number }>,
): Promise<OrderListItem | null> {
  const initial = await findOrderAcrossPages(orderID, customerID, loadPage)
  if (initial) return initial
  const refreshedCustomerID = await refreshCustomerID()
  if (refreshedCustomerID !== customerID) {
    const refreshed = await findOrderAcrossPages(orderID, refreshedCustomerID, loadPage)
    if (refreshed) return refreshed
  }
  return findOrderAcrossPages(orderID, undefined, loadPage)
}

export function statusSyncSatisfied(status: string): boolean {
  return ['scheduled', 'shot', 'selected', 'retouching', 'delivered', 'closed'].includes(status)
}

async function findOrderAcrossPages(
  orderID: string,
  customerID: string | undefined,
  loadPage: (customerID: string | undefined, page: number) => Promise<{ items: OrderListItem[]; total: number }>,
): Promise<OrderListItem | null> {
  let loaded = 0
  for (let page = 1; ; page += 1) {
    const result = await loadPage(customerID, page)
    const found = result.items.find((item) => item.id === orderID)
    if (found) return found
    loaded += result.items.length
    if (loaded >= result.total || result.items.length === 0) return null
  }
}

function canonicalJSON(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonicalJSON).join(',')}]`
  if (value && typeof value === 'object') {
    const entries = Object.entries(value as Record<string, unknown>)
      .filter(([, item]) => item !== undefined)
      .sort(([left], [right]) => left.localeCompare(right))
    return `{${entries.map(([key, item]) => `${JSON.stringify(key)}:${canonicalJSON(item)}`).join(',')}}`
  }
  return JSON.stringify(value)
}
