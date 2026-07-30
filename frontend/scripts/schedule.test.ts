import assert from 'node:assert/strict'
import test from 'node:test'

import {
  accountDateAtNoonToInstant,
  accountToday,
  isValidDate,
  localDayRange,
  monthGrid,
  projectSlotToLocalDays,
  resolveLocalDateTime,
  ScheduleTimeError,
} from '../src/components/schedule/timezone.ts'
import { buildCalendarDays } from '../src/components/schedule/calendarModel.ts'
import {
  pendingScheduleExpired,
  readPendingSchedule,
  readScheduleDraft,
  scheduleAttemptKey,
  writePendingSchedule,
  writeScheduleDraft,
} from '../src/components/schedule/journal.ts'
import type { PendingScheduleFlow, ScheduleDraft } from '../src/components/schedule/journal.ts'
import type { OrderListItem } from '../src/api/client.ts'
import * as scheduleFlow from '../src/components/schedule/flow.ts'
import {
  BackfillPersistenceError,
  claimPendingFlow,
  completeBackfillOrder,
  locateStatusSyncOrder,
  missingSelectedOrderOption,
  orderInUseScheduleAction,
  pendingAfterDeterministicFailure,
  pendingForAutomaticReplay,
  readScheduleBackfillContext,
  scheduleDrawerShouldOpen,
  scheduleDialogShouldOpen,
  scheduleKnownSlotAction,
  scheduleCreateFailureKind,
  scheduleRequestResultKnown,
  statusSyncSatisfied,
} from '../src/components/schedule/flow.ts'
import {
  backfillTimestampFields,
  scheduleDraftResumeInput,
} from '../src/components/schedule/backfill.ts'

test('month grid starts Monday and always contains 42 days', () => {
  const grid = monthGrid('2026-07')
  assert.equal(grid.length, 42)
  assert.equal(grid[0], '2026-06-29')
  assert.equal(grid[41], '2026-08-09')
})

test('calendar query dates require a real calendar date, not only a matching shape', () => {
  assert.equal(isValidDate('2026-07-10'), true)
  assert.equal(isValidDate('2026-13-40'), false)
  assert.equal(isValidDate('2026-02-29'), false)
  assert.equal(isValidDate('not-a-date'), false)
})

test('slot projection preserves half-open local day membership', () => {
  const untilMidnight = projectSlotToLocalDays(
    { startAt: '2026-07-31T14:00:00Z', endAt: '2026-07-31T16:00:00Z' },
    'Asia/Shanghai',
  )
  assert.deepEqual(untilMidnight.map((item) => item.date), ['2026-07-31'])
  assert.equal(untilMidnight[0]?.displayStart, '22:00')
  assert.equal(untilMidnight[0]?.displayEnd, '24:00')

  const crossMidnight = projectSlotToLocalDays(
    { startAt: '2026-07-31T14:00:00Z', endAt: '2026-07-31T18:00:00Z' },
    'Asia/Shanghai',
  )
  assert.deepEqual(crossMidnight.map((item) => item.date), ['2026-07-31', '2026-08-01'])
  assert.deepEqual(crossMidnight.map((item) => [item.displayStart, item.displayEnd]), [
    ['22:00', '24:00'],
    ['00:00', '02:00'],
  ])

  const allDay = projectSlotToLocalDays(
    { startAt: '2026-07-09T16:00:00Z', endAt: '2026-07-10T16:00:00Z' },
    'Asia/Shanghai',
  )
  assert.deepEqual(allDay, [{ date: '2026-07-10', allDay: true, displayStart: '00:00', displayEnd: '24:00' }])
})

test('account natural day follows 23 and 25 hour DST transitions', () => {
  const spring = localDayRange('2026-03-08', 'America/New_York')
  const fall = localDayRange('2026-11-01', 'America/New_York')
  assert.equal((Date.parse(spring.end) - Date.parse(spring.start)) / 3_600_000, 23)
  assert.equal((Date.parse(fall.end) - Date.parse(fall.start)) / 3_600_000, 25)
})

test('nonexistent local time is rejected and repeated time requires occurrence', () => {
  assert.throws(
    () => resolveLocalDateTime('2026-03-08', '02:30', 'America/New_York'),
    (error: unknown) => error instanceof ScheduleTimeError && error.code === 'nonexistent_local_time',
  )
  assert.throws(
    () => resolveLocalDateTime('2026-11-01', '01:30', 'America/New_York'),
    (error: unknown) => error instanceof ScheduleTimeError && error.code === 'ambiguous_local_time',
  )

  const first = resolveLocalDateTime('2026-11-01', '01:30', 'America/New_York', 'first')
  const second = resolveLocalDateTime('2026-11-01', '01:30', 'America/New_York', 'second')
  assert.equal(first.offset, '-04:00')
  assert.equal(second.offset, '-05:00')
  assert.equal(Date.parse(second.instant) - Date.parse(first.instant), 3_600_000)
})

test('account dates convert without browser timezone assumptions', () => {
  assert.equal(accountDateAtNoonToInstant('2026-07-10', 'Asia/Shanghai'), '2026-07-10T04:00:00Z')
  assert.equal(
    accountToday('Asia/Shanghai', '2026-07-09T16:30:00Z'),
    '2026-07-10',
  )
  assert.equal(
    accountToday('America/New_York', '2026-07-09T16:30:00Z'),
    '2026-07-09',
  )
})

test('calendar model sorts stably and counts unique conflicts inside each local day', () => {
  const slots = [
    { id: 'slot-b', start_at: '2026-07-10T14:00:00Z', end_at: '2026-07-10T18:00:00Z', type: 'hold' },
    { id: 'slot-a', start_at: '2026-07-10T14:00:00Z', end_at: '2026-07-10T16:00:00Z', type: 'busy' },
    { id: 'slot-c', start_at: '2026-07-10T16:00:00Z', end_at: '2026-07-10T19:00:00Z', type: 'hold' },
  ]
  const days = buildCalendarDays(slots, ['2026-07-10', '2026-07-11'], 'Asia/Shanghai')
  assert.deepEqual(days[0]?.slots.map((entry) => entry.slot.id), ['slot-a', 'slot-b'])
  assert.equal(days[0]?.conflictCount, 2)
  assert.deepEqual(days[1]?.slots.map((entry) => entry.slot.id), ['slot-b', 'slot-c'])
  assert.equal(days[1]?.conflictCount, 2)
})

test('pending journal and handoff draft use separate expiry boundaries', () => {
  const storage = new MemoryStorage()
  const createdAt = '2026-07-10T00:00:00Z'
  const pending: PendingScheduleFlow = {
    flow_id: '0123456789abcdef0123456789abcdef',
    phase: 'slot',
    normalized_body: { start_at: createdAt, end_at: '2026-07-10T01:00:00Z', type: 'hold' },
    attempt_key: scheduleAttemptKey('0123456789abcdef0123456789abcdef', 'slot', 1),
    created_at: createdAt,
  }
  writePendingSchedule(pending, storage)
  assert.deepEqual(readPendingSchedule(storage), pending)
  assert.equal(pendingScheduleExpired(pending, Date.parse(createdAt) + 24 * 60 * 60 * 1000), true)

  const draft: ScheduleDraft = {
    draft_id: 'fedcba9876543210fedcba9876543210',
    customer_id: 'cus-1',
    start_at: createdAt,
    end_at: '2026-07-10T01:00:00Z',
    source: 'calendar',
    return_to: '/calendar?date=2026-07-10',
    created_at: createdAt,
  }
  writeScheduleDraft(draft, storage)
  assert.deepEqual(readScheduleDraft(draft.draft_id, storage, Date.parse(createdAt) + 60_000), draft)
  assert.equal(readScheduleDraft(draft.draft_id, storage, Date.parse(createdAt) + 2 * 60 * 60 * 1000), null)
})

test('a sent backfill request preserves its draft between the 2 and 24 hour boundaries', () => {
  const storage = new MemoryStorage()
  const createdAt = '2026-07-10T00:00:00Z'
  const draft = scheduleDraft({ created_at: createdAt })
  const pending = pendingFlow({
    phase: 'backfill_order',
    source_draft_id: draft.draft_id,
    normalized_body: { creation_mode: 'backfill', customer_id: draft.customer_id, status: 'shot' },
    created_at: createdAt,
  })
  writeScheduleDraft(draft, storage)
  writePendingSchedule(pending, storage)

  const recovered = readScheduleBackfillContext(
    draft.draft_id,
    storage,
    Date.parse(createdAt) + 3 * 60 * 60 * 1000,
  )

  assert.deepEqual(recovered, { draft, pending })
})

test('an unrelated pending flow does not extend an expired backfill draft', () => {
  const storage = new MemoryStorage()
  const createdAt = '2026-07-10T00:00:00Z'
  const draft = scheduleDraft({ created_at: createdAt })
  writeScheduleDraft(draft, storage)
  writePendingSchedule(pendingFlow({
    phase: 'backfill_order',
    source_draft_id: 'another-draft',
    created_at: createdAt,
  }), storage)

  assert.deepEqual(readScheduleBackfillContext(
    draft.draft_id,
    storage,
    Date.parse(createdAt) + 3 * 60 * 60 * 1000,
  ), { draft: null, pending: null })
})

test('order_in_use details produce an account-timezone calendar deep link', () => {
  const action = orderInUseScheduleAction({
    code: 'order_in_use',
    details: {
      schedule_slot_id: 'slot-1',
      schedule_start_at: '2026-07-10T16:30:00Z',
    },
  }, 'Asia/Shanghai')

  assert.deepEqual(action, {
    href: '/calendar?date=2026-07-11&slot=slot-1',
    label: '查看占用档期',
  })
})

test('known slot recovery links include the account-local date for cross-month navigation', () => {
  const flow = pendingFlow({
    known_slot_id: 'slot-1',
    normalized_body: {
      start_at: '2026-08-31T16:30:00Z',
      end_at: '2026-08-31T18:00:00Z',
      type: 'hold',
    },
  })

  assert.deepEqual(scheduleKnownSlotAction(flow, 'Asia/Shanghai', null), {
    href: '/calendar?date=2026-09-01&slot=slot-1',
    label: '查看档期',
  })
})

test('status sync recovery derives the known slot date from its source draft', () => {
  const flow = pendingFlow({
    phase: 'status_sync',
    known_slot_id: 'slot-2',
    normalized_body: { status: 'scheduled' },
  })
  const draft = scheduleDraft({ start_at: '2026-08-31T16:30:00Z' })

  assert.deepEqual(scheduleKnownSlotAction(flow, 'Asia/Shanghai', draft), {
    href: '/calendar?date=2026-09-01&slot=slot-2',
    label: '查看档期',
  })
  assert.deepEqual(scheduleKnownSlotAction(flow, 'Asia/Shanghai', null), {
    href: '/calendar',
    label: '前往日历核对档期',
  })
})

test('malformed order_in_use details fall back to the calendar without guessing a date', () => {
  assert.deepEqual(
    orderInUseScheduleAction({ code: 'order_in_use', details: { schedule_slot_id: 42 } }, 'Asia/Shanghai'),
    { href: '/calendar', label: '前往日历查找关联档期' },
  )
  assert.equal(orderInUseScheduleAction({ code: 'validation_failed' }, 'Asia/Shanghai'), null)
})

test('pending storage failure prevents the create request', async () => {
  const flow = pendingFlow()
  const storage = new FailingStorage('set')
  let requestCount = 0

  await assert.rejects(async () => {
    claimPendingFlow(flow, storage)
    requestCount += 1
  })
  assert.equal(requestCount, 0)
})

test('an existing tab flow blocks a second flow without overwriting it', () => {
  const storage = new MemoryStorage()
  const existing = pendingFlow()
  const second = pendingFlow({
    flow_id: 'fedcba9876543210fedcba9876543210',
    attempt_key: 'scf:fedcba9876543210fedcba9876543210:slot:v1',
  })
  writePendingSchedule(existing, storage)

  assert.deepEqual(claimPendingFlow(second, storage), existing)
  assert.deepEqual(readPendingSchedule(storage), existing)
})

test('automatic replay keeps the original normalized body and attempt key', () => {
  const flow = pendingFlow()

  const replay = pendingForAutomaticReplay(flow, Date.parse(flow.created_at) + 1_000)

  assert.strictEqual(replay, flow)
  assert.deepEqual(replay.normalized_body, flow.normalized_body)
  assert.equal(replay.attempt_key, flow.attempt_key)
})

test('expired pending flow cannot replay automatically', async () => {
  const flow = pendingFlow()
  let requestCount = 0

  await assert.rejects(async () => {
    pendingForAutomaticReplay(flow, Date.parse(flow.created_at) + 24 * 60 * 60 * 1000)
    requestCount += 1
  }, /24/)
  assert.equal(requestCount, 0)
})

test('request result knowledge follows the persisted phase facts', () => {
  assert.equal(scheduleRequestResultKnown(pendingFlow()), false)
  assert.equal(scheduleRequestResultKnown(pendingFlow({ known_slot_id: 'slot-1' })), true)
  assert.equal(scheduleRequestResultKnown(pendingFlow({ phase: 'order', known_order_id: 'order-1' })), true)
  assert.equal(scheduleRequestResultKnown(pendingFlow({
    phase: 'status_sync',
    known_slot_id: 'slot-1',
    known_order_id: 'order-1',
    prior_order_status: 'consulting',
  })), false)
  assert.equal(scheduleRequestResultKnown(pendingFlow({
    phase: 'status_sync',
    known_slot_id: 'slot-1',
    known_order_id: 'order-1',
    prior_order_status: 'scheduled',
  })), true)
})

test('create failure classification never treats an idempotency binding conflict as a new attempt', () => {
  assert.equal(scheduleCreateFailureKind(null, false), 'unknown')
  assert.equal(scheduleCreateFailureKind({ status: 500, code: 'internal' }, false), 'unknown')
  assert.equal(scheduleCreateFailureKind({ status: 409, code: 'idempotency_conflict' }, false), 'idempotency_conflict')
  assert.equal(scheduleCreateFailureKind({ status: 409, code: 'customer_changed' }, false), 'customer_changed')
  assert.equal(scheduleCreateFailureKind({ status: 409, code: 'customer_archived' }, false), 'deterministic')
  assert.equal(scheduleCreateFailureKind(null, true), 'deterministic')
})

test('deterministic failure rotates only the changed current step attempt', () => {
  const slot = pendingFlow({
    known_order_id: 'order-1',
    normalized_body: {
      start_at: '2026-07-10T00:00:00Z',
      end_at: '2026-07-10T01:00:00Z',
      type: 'shoot',
      order_id: 'order-1',
    },
  })

  const unchanged = pendingAfterDeterministicFailure(slot, { ...slot.normalized_body })
  assert.equal(unchanged.attempt_key, slot.attempt_key)
  assert.equal(unchanged.known_order_id, 'order-1')

  const changed = pendingAfterDeterministicFailure(slot, {
    ...slot.normalized_body,
    note: '改期后备注',
  })
  assert.equal(changed.attempt_key, 'scf:0123456789abcdef0123456789abcdef:slot:v2')
  assert.equal(changed.known_order_id, 'order-1')

  const order = pendingFlow({
    phase: 'order',
    attempt_key: 'scf:0123456789abcdef0123456789abcdef:order:v3',
    normalized_body: { creation_mode: 'new', customer_id: 'customer-1', status: 'consulting' },
  })
  const changedOrder = pendingAfterDeterministicFailure(order, {
    ...order.normalized_body,
    title: '新的标题',
  })
  assert.equal(changedOrder.attempt_key, 'scf:0123456789abcdef0123456789abcdef:order:v4')
})

test('backfill completion persists pending id, then draft id, then clears pending', async () => {
  const storage = new TracingStorage()
  const flow = pendingFlow({
    phase: 'backfill_order',
    source_draft_id: 'draft-1',
    attempt_key: 'scf:0123456789abcdef0123456789abcdef:order:v1',
    normalized_body: { creation_mode: 'backfill', customer_id: 'customer-1', status: 'shot' },
  })
  const draft: ScheduleDraft = {
    draft_id: 'draft-1',
    customer_id: 'customer-1',
    start_at: '2026-07-10T00:00:00Z',
    end_at: '2026-07-10T01:00:00Z',
    source: 'calendar',
    return_to: '/calendar?date=2026-07-10',
    created_at: flow.created_at,
  }
  writePendingSchedule(flow, storage)
  storage.operations.length = 0

  const completed = await completeBackfillOrder(
    flow,
    draft,
    async () => ({ id: 'order-1' }),
    undefined,
    storage,
  )

  assert.equal(completed.orderID, 'order-1')
  assert.deepEqual(storage.operations, [
    'set:schedule:create:pending',
    'set:schedule:draft:draft-1',
    'remove:schedule:create:pending',
  ])
  assert.equal(readPendingSchedule(storage), null)
  assert.equal(readScheduleDraft('draft-1', storage)?.known_order_id, 'order-1')
})

test('backfill local persistence retries do not repeat a known order request', async () => {
  const flow = pendingFlow({
    phase: 'backfill_order',
    source_draft_id: 'draft-1',
    attempt_key: 'scf:0123456789abcdef0123456789abcdef:order:v1',
    normalized_body: { creation_mode: 'backfill', customer_id: 'customer-1', status: 'shot' },
  })
  const draft: ScheduleDraft = {
    draft_id: 'draft-1',
    customer_id: 'customer-1',
    start_at: '2026-07-10T00:00:00Z',
    end_at: '2026-07-10T01:00:00Z',
    source: 'calendar',
    return_to: '/calendar?date=2026-07-10',
    created_at: flow.created_at,
  }
  let requestCount = 0
  const createOrder = async () => {
    requestCount += 1
    return { id: 'order-1' }
  }

  const pendingFailure = new FailOnceStorage('schedule:create:pending')
  writePendingSchedule(flow, pendingFailure)
  pendingFailure.arm()
  let rememberedOrderID = ''
  await assert.rejects(
    completeBackfillOrder(flow, draft, createOrder, undefined, pendingFailure),
    (reason: unknown) => {
      assert.ok(reason instanceof BackfillPersistenceError)
      rememberedOrderID = reason.knownOrderID
      assert.equal(reason.stage, 'pending')
      return true
    },
  )
  await completeBackfillOrder(flow, draft, createOrder, rememberedOrderID, pendingFailure)
  assert.equal(requestCount, 1)

  const draftFailure = new FailOnceStorage('schedule:draft:draft-1')
  writePendingSchedule(flow, draftFailure)
  draftFailure.arm()
  await assert.rejects(
    completeBackfillOrder(flow, draft, createOrder, undefined, draftFailure),
    (reason: unknown) => reason instanceof BackfillPersistenceError && reason.stage === 'draft',
  )
  const persisted = readPendingSchedule(draftFailure)
  assert.equal(persisted?.known_order_id, 'order-1')
  await completeBackfillOrder(persisted ?? flow, draft, createOrder, undefined, draftFailure)
  assert.equal(requestCount, 2)

  const clearFailure = new FailOnceRemoveStorage('schedule:create:pending')
  writePendingSchedule(flow, clearFailure)
  clearFailure.arm()
  await assert.rejects(
    completeBackfillOrder(flow, draft, createOrder, undefined, clearFailure),
    (reason: unknown) => reason instanceof BackfillPersistenceError && reason.stage === 'clear',
  )
  const clearPending = readPendingSchedule(clearFailure)
  assert.equal(clearPending?.known_order_id, 'order-1')
  assert.equal(readScheduleDraft('draft-1', clearFailure)?.known_order_id, 'order-1')
  await completeBackfillOrder(clearPending ?? flow, draft, createOrder, undefined, clearFailure)
  assert.equal(requestCount, 3)
})

test('schedule backfill timestamps are pruned by the final order status', () => {
  const shotAt = '2026-07-10T04:00:00Z'
  const deliveredAt = '2026-07-12T04:00:00Z'

  assert.deepEqual(backfillTimestampFields('scheduled', shotAt, deliveredAt), {})
  for (const status of ['shot', 'selected', 'retouching'] as const) {
    assert.deepEqual(backfillTimestampFields(status, shotAt, deliveredAt), { shot_at: shotAt })
  }
  for (const status of ['delivered', 'closed'] as const) {
    assert.deepEqual(backfillTimestampFields(status, shotAt, deliveredAt), {
      shot_at: shotAt,
      delivered_at: deliveredAt,
    })
  }
})

test('completed handoff restores the original local range, note, customer, and order', () => {
  const draft: ScheduleDraft = {
    draft_id: 'draft-1',
    customer_id: 'customer-1',
    known_order_id: 'order-1',
    start_at: '2026-07-10T01:30:00Z',
    end_at: '2026-07-10T03:00:00Z',
    note: '保留原备注',
    source: 'calendar',
    return_to: '/calendar?date=2026-07-10',
    created_at: '2026-07-10T00:00:00Z',
  }

  assert.deepEqual(scheduleDraftResumeInput(draft, 'Asia/Shanghai'), {
    startDate: '2026-07-10',
    startTime: '09:30',
    endDate: '2026-07-10',
    endTime: '11:00',
    note: '保留原备注',
    customerID: 'customer-1',
    orderID: 'order-1',
  })
})

test('schedule dialog stays closed during backfill handoff and reopens on return', () => {
  assert.equal(scheduleDialogShouldOpen('backfill_order', 'draft-1', 'backfill'), false)
  assert.equal(scheduleDialogShouldOpen(undefined, 'draft-1', 'backfill'), false)
  assert.equal(scheduleDialogShouldOpen(undefined, 'draft-1', undefined), true)
  assert.equal(scheduleDialogShouldOpen('slot', undefined, undefined), true)
  assert.equal(scheduleDialogShouldOpen(undefined, undefined, undefined), false)
})

test('calendar drawer cannot cover an open schedule dialog', () => {
  assert.equal(scheduleDrawerShouldOpen(true, false), true)
  assert.equal(scheduleDrawerShouldOpen(true, true), false)
  assert.equal(scheduleDrawerShouldOpen(false, false), false)
})

test('recovery actions distinguish unknown, customer change, and status sync', () => {
  assert.equal(typeof scheduleFlow.scheduleRecoveryActions, 'function')
  const unknown = scheduleFlow.scheduleRecoveryActions(pendingFlow(), false, 'unknown', null)
  assert.deepEqual(unknown, {
    resume: true,
    reset: false,
    keep: false,
    compensateOrder: false,
    deleteSlot: false,
    abandon: false,
  })

  const customerChanged = scheduleFlow.scheduleRecoveryActions(
    pendingFlow({ known_order_id: 'order-1' }),
    false,
    'customer_changed',
    scheduleDraft({ known_order_id: 'order-1' }),
  )
  assert.deepEqual(customerChanged, {
    resume: false,
    reset: true,
    keep: true,
    compensateOrder: false,
    deleteSlot: false,
    abandon: false,
  })

  const statusSync = scheduleFlow.scheduleRecoveryActions(
    pendingFlow({ phase: 'status_sync', known_order_id: 'order-1', known_slot_id: 'slot-1' }),
    false,
    'deterministic',
    scheduleDraft({ known_order_id: 'order-1' }),
  )
  assert.deepEqual(statusSync, {
    resume: true,
    reset: false,
    keep: true,
    compensateOrder: false,
    deleteSlot: true,
    abandon: false,
  })

  assert.deepEqual(
    scheduleFlow.scheduleRecoveryActions(pendingFlow(), false, 'idempotency_conflict', null),
    {
      resume: false,
      reset: false,
      keep: false,
      compensateOrder: false,
      deleteSlot: false,
      abandon: false,
    },
  )
})

test('only path A can compensate its created order and expired recovery only abandons', () => {
  assert.equal(typeof scheduleFlow.scheduleRecoveryActions, 'function')
  const pathAFlow = pendingFlow({
    source_draft_id: 'draft-1',
    known_order_id: 'order-1',
  })
  const pathADraft = scheduleDraft()
  assert.equal(
    scheduleFlow.scheduleRecoveryActions(pathAFlow, false, 'deterministic', pathADraft).compensateOrder,
    true,
  )

  const pathB = scheduleFlow.scheduleRecoveryActions(
    pathAFlow,
    false,
    'deterministic',
    scheduleDraft({ known_order_id: 'order-1' }),
  )
  assert.equal(pathB.compensateOrder, false)

  assert.deepEqual(scheduleFlow.scheduleRecoveryActions(pathAFlow, true, 'unknown', pathADraft), {
    resume: false,
    reset: false,
    keep: false,
    compensateOrder: false,
    deleteSlot: false,
    abandon: true,
  })
})

test('editing keeps the current scheduled order visible when it is excluded from candidates', () => {
  assert.equal(missingSelectedOrderOption('order-1', [{ id: 'order-2' }]), true)
  assert.equal(missingSelectedOrderOption('order-1', [{ id: 'order-1' }]), false)
  assert.equal(missingSelectedOrderOption('', [{ id: 'order-2' }]), false)
})

test('status sync searches refreshed customer pages before stable unfiltered fallback', async () => {
  const target = { id: 'order-1', customer_id: 'customer-3', status: 'scheduled' } as OrderListItem
  const filler = Array.from({ length: 100 }, (_, index) => ({ id: `other-${index}` }) as OrderListItem)
  const calls: string[] = []
  const pages = new Map<string, { items: OrderListItem[]; total: number }>([
    ['customer-1:1', { items: filler, total: 101 }],
    ['customer-1:2', { items: [{ id: 'other-last' } as OrderListItem], total: 101 }],
    ['customer-2:1', { items: [], total: 0 }],
    ['all:1', { items: filler, total: 101 }],
    ['all:2', { items: [target], total: 101 }],
  ])

  const found = await locateStatusSyncOrder(
    'order-1',
    'customer-1',
    async () => 'customer-2',
    async (customerID, page) => {
      const key = `${customerID ?? 'all'}:${page}`
      calls.push(key)
      return pages.get(key) ?? { items: [], total: 0 }
    },
  )

  assert.equal(found?.id, 'order-1')
  assert.deepEqual(calls, ['customer-1:1', 'customer-1:2', 'customer-2:1', 'all:1', 'all:2'])
})

test('scheduled and later non-cancelled states satisfy status sync', () => {
  assert.equal(statusSyncSatisfied('consulting'), false)
  assert.equal(statusSyncSatisfied('cancelled'), false)
  for (const status of ['scheduled', 'shot', 'selected', 'retouching', 'delivered', 'closed']) {
    assert.equal(statusSyncSatisfied(status), true)
  }
})

function pendingFlow(overrides: Partial<PendingScheduleFlow> = {}): PendingScheduleFlow {
  return {
    flow_id: '0123456789abcdef0123456789abcdef',
    phase: 'slot',
    normalized_body: {
      start_at: '2026-07-10T00:00:00Z',
      end_at: '2026-07-10T01:00:00Z',
      type: 'hold',
    },
    attempt_key: 'scf:0123456789abcdef0123456789abcdef:slot:v1',
    created_at: '2026-07-10T00:00:00Z',
    ...overrides,
  }
}

function scheduleDraft(overrides: Partial<ScheduleDraft> = {}): ScheduleDraft {
  return {
    draft_id: 'draft-1',
    customer_id: 'customer-1',
    start_at: '2026-07-10T00:00:00Z',
    end_at: '2026-07-10T01:00:00Z',
    source: 'calendar',
    return_to: '/calendar?date=2026-07-10',
    created_at: '2026-07-10T00:00:00Z',
    ...overrides,
  }
}

class MemoryStorage implements Storage {
  #values = new Map<string, string>()
  get length() { return this.#values.size }
  clear() { this.#values.clear() }
  getItem(key: string) { return this.#values.get(key) ?? null }
  key(index: number) { return [...this.#values.keys()][index] ?? null }
  removeItem(key: string) { this.#values.delete(key) }
  setItem(key: string, value: string) { this.#values.set(key, value) }
}

class FailingStorage extends MemoryStorage {
  constructor(private readonly failure: 'set' | 'remove') {
    super()
  }

  override removeItem(key: string) {
    if (this.failure === 'remove') throw new Error('remove failed')
    super.removeItem(key)
  }

  override setItem(key: string, value: string) {
    if (this.failure === 'set') throw new Error('set failed')
    super.setItem(key, value)
  }
}

class TracingStorage extends MemoryStorage {
  readonly operations: string[] = []

  override removeItem(key: string) {
    this.operations.push(`remove:${key}`)
    super.removeItem(key)
  }

  override setItem(key: string, value: string) {
    this.operations.push(`set:${key}`)
    super.setItem(key, value)
  }
}

class FailOnceStorage extends MemoryStorage {
  #armed = false

  constructor(private readonly keyToFail: string) {
    super()
  }

  arm() {
    this.#armed = true
  }

  override setItem(key: string, value: string) {
    if (this.#armed && key === this.keyToFail) {
      this.#armed = false
      throw new Error(`set failed: ${key}`)
    }
    super.setItem(key, value)
  }
}

class FailOnceRemoveStorage extends MemoryStorage {
  #armed = false

  constructor(private readonly keyToFail: string) {
    super()
  }

  arm() {
    this.#armed = true
  }

  override removeItem(key: string) {
    if (this.#armed && key === this.keyToFail) {
      this.#armed = false
      throw new Error(`remove failed: ${key}`)
    }
    super.removeItem(key)
  }
}
