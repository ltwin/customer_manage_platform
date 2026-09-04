import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { createMemoryRouter } from 'react-router-dom'

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
import {
  buildCalendarDays,
  overlappingSlots,
} from '../src/components/schedule/calendarModel.ts'
import {
  resolveCurrentCustomer,
  type CustomerSelection,
} from '../src/components/customers/customerPickerModel.ts'
import {
  buildCalendarModel,
  calendarProjectionTimeLabel,
  calendarTimelineBlock,
  calendarWeekAxisTimeline,
  calculateMonthOverview,
  layoutWeekProjections,
  WEEK_EVENT_MIN_VISUAL_MINUTES,
} from '../src/pages/calendar/model.ts'
import {
  groupOpeningsByDate,
  openingsForFirstDays,
  openingsText,
} from '../src/pages/calendar/openings.ts'
import {
  SIDE_PANEL_MIN_WIDTH,
  calendarDetailShouldOpen,
  calendarLayoutModeForWidth,
} from '../src/pages/calendar/layoutMode.ts'
import {
  isCurrentCalendarRequest,
  nextCalendarRequest,
} from '../src/pages/calendar/requestState.ts'
import {
  conflictPreviewRequestKey,
  isCurrentConflictPreviewRequest,
} from '../src/components/schedule/conflictPreview.ts'
import { calendarDateFocusTarget } from '../src/pages/calendar/keyboard.ts'
import {
  calendarDeleteCompletionAfterSettings,
  calendarDeleteCompletion,
  calendarRangeKeyForSearch,
  calendarSearchParamsAfterDelete,
  reconcileCalendarDeleteRefresh,
} from '../src/pages/calendar/urlState.ts'
import { waitForCalendarSettingsIdle } from '../src/pages/calendar/deleteCoordination.ts'
import {
  pendingScheduleExpired,
  readPendingSchedule,
  readScheduleDraft,
  scheduleAttemptKey,
  writePendingSchedule,
  writeScheduleDraft,
} from '../src/components/schedule/journal.ts'
import type { PendingScheduleFlow, ScheduleDraft } from '../src/components/schedule/journal.ts'
import type {
  OrderListItem,
  ScheduleAvailability,
  ScheduleSlotListItem,
} from '../src/api/client.ts'
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
import {
  applyBusinessDurationStart,
  deriveBusinessPrefillEnd,
  parsePlanningSchedulePrefill,
} from '../src/components/schedule/businessPrefill.ts'

test('planning schedule prefill accepts only the versioned one-shot payload', () => {
  assert.deepEqual(parsePlanningSchedulePrefill(null), { present: false, value: null, error: null })
  assert.deepEqual(parsePlanningSchedulePrefill({ kind: 'another-route-state' }), { present: false, value: null, error: null })
  assert.deepEqual(parsePlanningSchedulePrefill({
    kind: 'planning-schedule-prefill-v1',
    order_id: ' ord-1 ',
    basis_minutes: 420,
  }), {
    present: true,
    value: { kind: 'planning-schedule-prefill-v1', order_id: 'ord-1', basis_minutes: 420 },
    error: null,
  })
  assert.equal(parsePlanningSchedulePrefill({
    kind: 'planning-schedule-prefill-v1', order_id: '', basis_minutes: 420,
  }).error, '经营草稿中的订单信息无效')
  assert.equal(parsePlanningSchedulePrefill({
    kind: 'planning-schedule-prefill-v1', order_id: 'ord-1', basis_minutes: 0,
  }).error, '经营草稿中的建议时长无效')
})

test('business duration prefill adds elapsed minutes in the account timezone', () => {
  assert.deepEqual(deriveBusinessPrefillEnd(
    { date: '2026-08-20', time: '10:00' }, 'Asia/Shanghai', 420,
  ), { date: '2026-08-20', time: '17:00' })
  assert.deepEqual(deriveBusinessPrefillEnd(
    { date: '2026-03-08', time: '01:30' }, 'America/New_York', 120,
  ), { date: '2026-03-08', time: '04:30' })
  assert.equal(deriveBusinessPrefillEnd(
    { date: '2026-11-01', time: '01:30' }, 'America/New_York', 60,
  ), null)
  assert.deepEqual(deriveBusinessPrefillEnd(
    { date: '2026-11-01', time: '01:30', occurrence: 'second' }, 'America/New_York', 60,
  ), { date: '2026-11-01', time: '02:30' })
})

test('business duration stays linked to the draft until the end is edited', () => {
  const linkedDraft = {
    startDate: '',
    startTime: '',
    endDate: '',
    endTime: '',
    allDay: false,
    businessDurationMinutes: 420,
  }

  assert.deepEqual(
    applyBusinessDurationStart({ ...linkedDraft, startDate: '2026-08-25' }, 'Asia/Shanghai'),
    { ...linkedDraft, startDate: '2026-08-25' },
  )
  const completed = applyBusinessDurationStart({
    ...linkedDraft,
    startDate: '2026-08-25',
    startTime: '10:00',
  }, 'Asia/Shanghai')
  assert.deepEqual(completed, {
    ...linkedDraft,
    startDate: '2026-08-25',
    startTime: '10:00',
    endDate: '2026-08-25',
    endTime: '17:00',
    endOccurrence: undefined,
  })
  assert.deepEqual(applyBusinessDurationStart({
    ...completed,
    startTime: '11:30',
  }, 'Asia/Shanghai'), {
    ...completed,
    startTime: '11:30',
    endTime: '18:30',
  })

  const manuallyUnlinked = {
    ...completed,
    endTime: '19:00',
    businessDurationMinutes: undefined,
  }
  assert.deepEqual(applyBusinessDurationStart({
    ...manuallyUnlinked,
    startTime: '12:00',
  }, 'Asia/Shanghai'), {
    ...manuallyUnlinked,
    startTime: '12:00',
  })
  assert.deepEqual(applyBusinessDurationStart({
    ...completed,
    allDay: true,
    startTime: '12:00',
  }, 'Asia/Shanghai'), {
    ...completed,
    allDay: true,
    startTime: '12:00',
  })
})

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

test('calendar v2 excludes cancelled shoots from conflicts and openings without hiding them', () => {
  const availability = availabilityFixture({ minOpeningMinutes: 60 })
  const slots = [
    shootSlot('cancelled', '2026-07-06T02:00:00Z', '2026-07-06T10:00:00Z', 'cancelled'),
    plainSlot('hold-a', 'hold', '2026-07-06T03:00:00Z', '2026-07-06T04:00:00Z'),
    plainSlot('busy-a', 'busy', '2026-07-06T03:30:00Z', '2026-07-06T04:30:00Z'),
    plainSlot('hold-touching', 'hold', '2026-07-06T04:30:00Z', '2026-07-06T05:30:00Z'),
  ]

  const model = buildCalendarModel(slots, ['2026-07-06'], 'Asia/Shanghai', availability)
  const day = model.days[0]
  assert.ok(day)
  assert.equal(model.errors.length, 0)
  assert.equal(day.cancelledCount, 1)
  assert.equal(day.conflictCount, 2)
  assert.deepEqual(
    day.projections.filter((entry) => entry.conflicting).map((entry) => entry.slot.id),
    ['hold-a', 'busy-a'],
  )
  assert.equal(day.projections.find((entry) => entry.slot.id === 'cancelled')?.cancelled, true)
  assert.deepEqual(day.openings.map((opening) => [opening.start, opening.end]), [
    ['10:00', '11:00'],
    ['13:30', '19:00'],
  ])
})

test('conflict preview excludes cancelled shoots with the shared occupancy rule', () => {
  const slots = [
    shootSlot('cancelled', '2026-07-06T02:00:00Z', '2026-07-06T04:00:00Z', 'cancelled'),
    plainSlot('active', 'hold', '2026-07-06T03:00:00Z', '2026-07-06T05:00:00Z'),
  ]

  assert.deepEqual(
    overlappingSlots(slots, '2026-07-06T02:30:00Z', '2026-07-06T03:30:00Z').map((slot) => slot.id),
    ['active'],
  )
})

test('openings limits and copy text count distinct dates rather than interval rows', () => {
  const openings = [
    ...Array.from({ length: 9 }, (_, index) => ({
      date: '2026-08-03',
      start: `${String(index).padStart(2, '0')}:00`,
      end: `${String(index).padStart(2, '0')}:30`,
      startAt: `2026-08-03T${String(index).padStart(2, '0')}:00:00Z`,
      endAt: `2026-08-03T${String(index).padStart(2, '0')}:30:00Z`,
    })),
    ...Array.from({ length: 8 }, (_, index) => ({
      date: `2026-08-${String(index + 4).padStart(2, '0')}`,
      start: '10:00',
      end: '12:00',
      startAt: `2026-08-${String(index + 4).padStart(2, '0')}T10:00:00Z`,
      endAt: `2026-08-${String(index + 4).padStart(2, '0')}T12:00:00Z`,
    })),
  ]

  const days = groupOpeningsByDate(openings)
  assert.equal(days.length, 9)
  assert.equal(days[0]?.openings.length, 9)
  assert.deepEqual(
    groupOpeningsByDate(openings, 8).map((day) => day.date),
    ['2026-08-03', '2026-08-04', '2026-08-05', '2026-08-06', '2026-08-07', '2026-08-08', '2026-08-09', '2026-08-10'],
  )
  assert.deepEqual(new Set(openingsForFirstDays(openings, 8).map((opening) => opening.date)).size, 8)
  const copy = openingsText(openings)
  assert.match(copy, /2026-08-07 10:00–12:00/)
  assert.doesNotMatch(copy, /2026-08-08/)
  assert.match(copy, /2026-08-03 00:00–00:30、01:00–01:30/)
})

test('fall-fold week timeline preserves elapsed duration and distinguishes repeated wall times', () => {
  const slot = plainSlot('fold', 'hold', '2026-11-01T05:30:00Z', '2026-11-01T06:30:00Z')
  const model = buildCalendarModel([slot], ['2026-11-01'], 'America/New_York', null)
  const day = model.days[0]
  const projection = day?.projections[0]
  assert.ok(day)
  assert.ok(projection)
  assert.equal(day.timeline.durationMinutes, 25 * 60)
  assert.deepEqual(calendarTimelineBlock(day.timeline, projection.startAt, projection.endAt), {
    topMinutes: 90,
    durationMinutes: 60,
  })
  assert.equal(calendarProjectionTimeLabel(projection, day.timeline), '01:30 UTC-04:00–01:30 UTC-05:00')
  assert.deepEqual(
    day.timeline.ticks.filter((tick) => tick.label.startsWith('01:00')).map((tick) => tick.label),
    ['01:00 UTC-04:00', '01:00 UTC-05:00'],
  )
})

test('mixed DST weeks keep the shared axis on an ordinary local day', () => {
  for (const dates of [
    ['2026-03-08', '2026-03-09'],
    ['2026-11-01', '2026-11-02'],
  ]) {
    const days = buildCalendarModel([], dates, 'America/New_York', null).days
    const axis = calendarWeekAxisTimeline(days, dates)
    assert.ok(axis)
    assert.equal(axis.durationMinutes, 24 * 60)
    assert.equal(axis.ticks.find((tick) => tick.offsetMinutes === 120)?.label, '02:00')
  }

  const spring = buildCalendarModel([], ['2026-03-08'], 'America/New_York', null).days[0]
  assert.equal(spring?.timeline.ticks.find((tick) => tick.offsetMinutes === 120)?.label, '03:00')
  assert.equal(spring?.timeline.ticks.find((tick) => tick.offsetMinutes === 120)?.utcOffset, '-04:00')

  const fall = buildCalendarModel([], ['2026-11-01'], 'America/New_York', null).days[0]
  assert.deepEqual(
    fall?.timeline.ticks.filter((tick) => tick.label.startsWith('01:00')).map((tick) => tick.label),
    ['01:00 UTC-04:00', '01:00 UTC-05:00'],
  )
})

test('non-hour DST days retain real intermediate minutes and an exact terminal tick', () => {
  const spring = buildCalendarModel([], ['2026-10-04'], 'Australia/Lord_Howe', null).days[0]
  assert.ok(spring)
  assert.equal(spring.timeline.durationMinutes, 23.5 * 60)
  assert.deepEqual(
    spring.timeline.ticks
      .filter((tick) => [120, 180, 240].includes(tick.offsetMinutes))
      .map((tick) => tick.label),
    ['02:30', '03:30', '04:30'],
  )
  assert.deepEqual(spring.timeline.ticks.at(-1), {
    offsetMinutes: 23.5 * 60,
    label: '24:00',
    utcOffset: '+11:00',
  })

  const fall = buildCalendarModel([], ['2026-04-05'], 'Australia/Lord_Howe', null).days[0]
  assert.ok(fall)
  assert.equal(fall.timeline.durationMinutes, 24.5 * 60)
  assert.deepEqual(
    fall.timeline.ticks
      .filter((tick) => [120, 180, 240].includes(tick.offsetMinutes))
      .map((tick) => tick.label),
    ['01:30', '02:30', '03:30'],
  )
})

test('short endpoint-touching week events use visual lanes without becoming conflicts', () => {
  const slots = [
    plainSlot('short-a', 'hold', '2026-07-06T02:00:00Z', '2026-07-06T02:15:00Z'),
    plainSlot('short-b', 'busy', '2026-07-06T02:15:00Z', '2026-07-06T02:30:00Z'),
  ]
  const day = buildCalendarModel(slots, ['2026-07-06'], 'Asia/Shanghai', null).days[0]
  assert.ok(day)
  assert.equal(WEEK_EVENT_MIN_VISUAL_MINUTES, 30)
  assert.deepEqual(day.layouts.map((layout) => ({
    id: layout.projection.slot.id,
    lane: layout.lane,
    laneCount: layout.laneCount,
    conflicting: layout.conflicting,
  })), [
    { id: 'short-a', lane: 0, laneCount: 2, conflicting: false },
    { id: 'short-b', lane: 1, laneCount: 2, conflicting: false },
  ])
})

test('deleting the current deep-link target replaces only the latest matching slot state', async () => {
  const next = calendarSearchParamsAfterDelete(
    new URLSearchParams('date=2026-08-01&slot=slot-1&view=week'),
    'slot-1',
  )
  assert.equal(next?.toString(), 'date=2026-08-01&view=week')
  assert.equal(calendarSearchParamsAfterDelete(
    new URLSearchParams('date=2026-08-02&slot=slot-2&view=week'),
    'slot-1',
  ), null)

  const router = createMemoryRouter([{ path: '*' }], {
    initialEntries: ['/before', '/calendar?date=2026-08-01&slot=slot-1&view=week'],
    initialIndex: 1,
  })
  try {
    assert.ok(next)
    await router.navigate({ pathname: '/calendar', search: `?${next.toString()}` }, { replace: true })
    assert.equal(router.state.location.search, '?date=2026-08-01&view=week')
    await router.navigate(-1)
    assert.equal(router.state.location.pathname, '/before')
  } finally {
    router.dispose()
  }
})

test('delete completion canonicalizes URL date and range as one latest-timezone state', () => {
  const startAt = '2026-08-05T01:00:00Z'
  const timezone = 'America/New_York'
  const slotOnly = calendarDeleteCompletion(
    new URLSearchParams('slot=slot-1'),
    'slot-1',
    startAt,
    timezone,
  )
  assert.equal(slotOnly?.date, '2026-08-04')
  assert.equal(slotOnly?.searchParams.toString(), 'date=2026-08-04')
  assert.equal(slotOnly?.rangeKey, calendarRangeKeyForSearch(
    new URLSearchParams('date=2026-08-04'),
    timezone,
    '',
  ))

  const invalidDate = calendarDeleteCompletion(
    new URLSearchParams('date=not-a-date&slot=slot-1&view=week'),
    'slot-1',
    startAt,
    timezone,
  )
  assert.equal(invalidDate?.date, '2026-08-04')
  assert.equal(invalidDate?.searchParams.toString(), 'date=2026-08-04&view=week')
  assert.equal(invalidDate?.rangeKey, slotOnly?.rangeKey)

  assert.equal(calendarDeleteCompletion(
    new URLSearchParams('date=2026-08-05&slot=slot-2'),
    'slot-1',
    startAt,
    timezone,
  ), null)
})

test('delete completion remains bounded while Settings is pending and safely accepts a late timezone', async () => {
  let idle = false
  const listeners = new Set<() => void>()
  const waitForIdle = (signal?: AbortSignal, timeoutMs = 50) => waitForCalendarSettingsIdle({
    isIdle: () => idle,
    subscribe: (listener) => {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
    signal,
    timeoutMs,
  })

  const timedOut = await waitForIdle(undefined, 5)
  assert.equal(timedOut, 'timeout')
  assert.equal(listeners.size, 0)

  const idlePromise = waitForIdle()
  idle = true
  for (const listener of [...listeners]) listener()
  assert.equal(await idlePromise, 'idle')
  assert.equal(listeners.size, 0)

  idle = false
  const controller = new AbortController()
  const cancelledPromise = waitForIdle(controller.signal)
  controller.abort()
  assert.equal(await cancelledPromise, 'cancelled')
  assert.equal(listeners.size, 0)

  const fallbackSearch = new URLSearchParams('date=2026-08-05&view=week')
  const lateCompletion = calendarDeleteCompletionAfterSettings(
    fallbackSearch,
    fallbackSearch.toString(),
    'slot-1',
    '2026-08-04T20:30:00Z',
    'America/New_York',
  )
  assert.equal(lateCompletion?.date, '2026-08-04')
  assert.equal(lateCompletion?.searchParams.toString(), 'view=week&date=2026-08-04')
  assert.equal(calendarDeleteCompletionAfterSettings(
    new URLSearchParams('date=2026-08-06&view=week'),
    fallbackSearch.toString(),
    'slot-1',
    '2026-08-04T20:30:00Z',
    'America/New_York',
  ), null)
})

test('deferred delete refresh never restarts an obsolete calendar range', async () => {
  const rangeM1 = calendarRangeKeyForSearch(
    new URLSearchParams('date=2026-08-01'),
    'Asia/Shanghai',
    '',
  )
  const rangeM2 = calendarRangeKeyForSearch(
    new URLSearchParams('date=2026-09-01'),
    'Asia/Shanghai',
    '',
  )
  assert.notEqual(rangeM1, rangeM2)
  const rangeM1NewTimezone = calendarRangeKeyForSearch(
    new URLSearchParams('date=2026-08-01'),
    'America/New_York',
    '',
  )
  assert.notEqual(rangeM1, rangeM1NewTimezone)
  assert.equal(calendarRangeKeyForSearch(
    new URLSearchParams('date=2026-08-31'),
    'Asia/Shanghai',
    '',
  ), rangeM1)

  let location = { pathname: '/calendar', search: '?date=2026-08-01&slot=slot-1', timezone: 'Asia/Shanghai' }
  let originalRefreshes = 0
  let currentReloads = 0
  const reconcile = (refreshOriginal: () => Promise<void>) => reconcileCalendarDeleteRefresh({
    deletedSlotID: 'slot-1',
    deletedSlotStartAt: '2026-08-05T01:00:00Z',
    expectedRangeKey: rangeM1,
    readLocation: () => location,
    refreshOriginal,
    requestCurrentReload: () => { currentReloads += 1 },
  })

  let next = await reconcile(async () => { originalRefreshes += 1 })
  assert.equal(next?.searchParams.toString(), 'date=2026-08-01')
  assert.equal(originalRefreshes, 1)
  assert.equal(currentReloads, 0)

  location = { pathname: '/calendar', search: '?date=2026-09-01&slot=slot-2', timezone: 'Asia/Shanghai' }
  next = await reconcile(async () => { originalRefreshes += 1 })
  assert.equal(next, null)
  assert.equal(originalRefreshes, 1)
  assert.equal(currentReloads, 1)

  location = { pathname: '/calendar', search: '?date=2026-08-01&slot=slot-1', timezone: 'Asia/Shanghai' }
  next = await reconcile(async () => {
    originalRefreshes += 1
    location = { pathname: '/calendar', search: '?date=2026-09-01&slot=slot-2', timezone: 'Asia/Shanghai' }
  })
  assert.equal(next, null)
  assert.equal(originalRefreshes, 2)
  assert.equal(currentReloads, 1)

  location = { pathname: '/calendar', search: '?date=2026-09-01&slot=slot-1', timezone: 'Asia/Shanghai' }
  next = await reconcile(async () => { originalRefreshes += 1 })
  assert.equal(next?.searchParams.toString(), 'date=2026-09-01')
  assert.equal(originalRefreshes, 2)
  assert.equal(currentReloads, 2)

  location = {
    pathname: '/calendar',
    search: '?date=2026-08-01&slot=slot-1',
    timezone: 'America/New_York',
  }
  next = await reconcile(async () => { originalRefreshes += 1 })
  assert.equal(next?.searchParams.toString(), 'date=2026-08-01')
  assert.equal(originalRefreshes, 2)
  assert.equal(currentReloads, 3)

  location = { pathname: '/calendar', search: '?slot=slot-1', timezone: 'America/New_York' }
  next = await reconcile(async () => { originalRefreshes += 1 })
  assert.equal(next?.searchParams.toString(), 'date=2026-08-04')
  assert.equal(next?.rangeKey, calendarRangeKeyForSearch(
    new URLSearchParams('date=2026-08-04'),
    'America/New_York',
    '',
  ))
  assert.equal(originalRefreshes, 2)
  assert.equal(currentReloads, 4)

  location = { pathname: '/calendar', search: '?date=invalid&slot=slot-1', timezone: 'America/New_York' }
  next = await reconcile(async () => { originalRefreshes += 1 })
  assert.equal(next?.searchParams.toString(), 'date=2026-08-04')
  assert.equal(originalRefreshes, 2)
  assert.equal(currentReloads, 5)

  location = { pathname: '/customers', search: '?slot=slot-1', timezone: 'America/New_York' }
  next = await reconcile(async () => { originalRefreshes += 1 })
  assert.equal(next, null)
  assert.equal(originalRefreshes, 2)
  assert.equal(currentReloads, 5)
})

test('cancelled events join only the final visible set visual lanes', () => {
  const day = buildCalendarModel([
    plainSlot('active', 'hold', '2026-07-06T02:00:00Z', '2026-07-06T03:00:00Z'),
    shootSlot('cancelled', '2026-07-06T02:00:00Z', '2026-07-06T03:00:00Z', 'cancelled'),
  ], ['2026-07-06'], 'Asia/Shanghai', null).days[0]
  assert.ok(day)

  const activeOnly = layoutWeekProjections(day.projections.filter((projection) => !projection.cancelled))
  assert.deepEqual(activeOnly.map((layout) => [layout.projection.slot.id, layout.lane, layout.laneCount]), [
    ['active', 0, 1],
  ])

  const withCancelled = layoutWeekProjections(day.projections)
  assert.deepEqual(withCancelled.map((layout) => [
    layout.projection.slot.id,
    layout.lane,
    layout.laneCount,
    layout.conflicting,
  ]), [
    ['active', 0, 2, false],
    ['cancelled', 1, 2, false],
  ])
  assert.equal(withCancelled.find((layout) => layout.projection.slot.id === 'cancelled')?.cancelled, true)

  for (const [activeRange, cancelledRange] of [
    [['2026-07-06T02:00:00Z', '2026-07-06T03:00:00Z'], ['2026-07-06T02:30:00Z', '2026-07-06T03:30:00Z']],
    [['2026-07-06T02:00:00Z', '2026-07-06T02:15:00Z'], ['2026-07-06T02:15:00Z', '2026-07-06T02:30:00Z']],
  ] as const) {
    const projected = buildCalendarModel([
      plainSlot('active', 'hold', ...activeRange),
      shootSlot('cancelled', ...cancelledRange, 'cancelled'),
    ], ['2026-07-06'], 'Asia/Shanghai', null).days[0]?.projections ?? []
    const layouts = layoutWeekProjections(projected)
    assert.equal(new Set(layouts.map((layout) => layout.lane)).size, 2)
    assert.ok(layouts.every((layout) => layout.laneCount === 2 && !layout.conflicting))
  }
})

test('calendar UI consumes turnaround warnings and keeps preview loading separate from mutation lock', () => {
  const dialog = readFileSync(new URL('../src/components/schedule/ScheduleSlotDialog.tsx', import.meta.url), 'utf8')
  const details = readFileSync(new URL('../src/pages/calendar/DayDetailPanel.tsx', import.meta.url), 'utf8')
  const week = readFileSync(new URL('../src/pages/calendar/WeekCalendar.tsx', import.meta.url), 'utf8')

  assert.match(dialog, /const \[previewLoading, setPreviewLoading\]/)
  assert.match(dialog, /const \[mutationSaving, setMutationSaving\]/)
  assert.match(dialog, /const previewWasLoading = current\.controller !== null/)
  assert.match(dialog, /if \(previewWasLoading\) setPreviewLoading\(false\)/)
  assert.doesNotMatch(
    dialog.match(/function invalidateConflictPreview\(\)[\s\S]*?\n  }/)?.[0] ?? '',
    /setMutationSaving/,
  )
  assert.match(dialog, /fieldset className="schedule-dialog-fields" disabled=\{mutationSaving\}/)
  assert.match(details, /calendar-v2-turnaround/)
  assert.match(details, /turnaroundThresholdMinutes/)
  assert.match(week, /onDoubleClick=\{\(event\) => event\.stopPropagation\(\)\}/)
})

test('business duration linkage is owned by the editable schedule draft', () => {
  const dialog = readFileSync(new URL('../src/components/schedule/ScheduleSlotDialog.tsx', import.meta.url), 'utf8')

  assert.match(dialog, /businessDurationMinutes\?: number/)
  assert.match(dialog, /businessDurationMinutes: prefill\.basisMinutes/)
  assert.match(dialog, /applyBusinessDurationStart\(next, timezone\)/)
  assert.match(dialog, /function changeEnd\(next: SlotDraft\) \{\s+changeDraft\(\{ \.\.\.next, businessDurationMinutes: undefined \}\)/)
  assert.doesNotMatch(dialog, /businessDurationLinked/)
})

test('calendar delete flow has a synchronous mutation lock, stable focus, and date-bound notice', () => {
  const page = readFileSync(new URL('../src/pages/CalendarPage.tsx', import.meta.url), 'utf8')
  const details = readFileSync(new URL('../src/pages/calendar/DayDetailPanel.tsx', import.meta.url), 'utf8')

  assert.match(page, /const \[deleting, setDeleting\] = useState\(false\)/)
  assert.match(page, /const deleteInFlightRef = useRef\(false\)/)
  assert.match(page, /const deletedSlotIDRef = useRef\(''\)/)
  assert.match(page, /if \(deleteInFlightRef\.current\) return/)
  assert.match(page, /deleteInFlightRef\.current = true/)
  assert.match(page, /useFocusTrap<HTMLElement>\(Boolean\(deleteTarget\), closeDeleteDialog, !deleting\)/)
  assert.match(page, /disabled=\{deleting\}/)
  assert.match(page, /deletedSlotStartAt: target\.start_at/)
  assert.match(page, /const deleteCoordinationSignal = deleteCoordinationControllerRef\.current\?\.signal[\s\S]*?await deleteScheduleSlot\(target\.id\)\s+if \(deleteCoordinationSignal\?\.aborted\) return\s+deletedSlotIDRef\.current = target\.id/)
  assert.match(page, /if \(deletedSlotIDRef\.current && querySlot === deletedSlotIDRef\.current\) return\s+try \{/)
  assert.match(page, /if \(querySlot === deletedSlotIDRef\.current\) \{\s+slotLookupRequestRef\.current\.controller\?\.abort\(\)\s+return\s+\}/)
  assert.match(page, /setSelectedDate\(completion\.date\)\s+setMonth\(completion\.date\.slice\(0, 7\)\)/)
  assert.match(page, /const currentSlotLookup = slotLookupRequestRef\.current\s+currentSlotLookup\.controller\?\.abort\(\)\s+slotLookupRequestRef\.current = \{\s+\.\.\.nextCalendarRequest\(currentSlotLookup, ''\),\s+controller: null,\s+\}\s+setQueryError\(null\)/)
  assert.match(page, /setSearchParams\(completion\.searchParams, \{ replace: true \}\)\s+focusCalendarDate\(completion\.date\)/)
  assert.match(page, /setReadState\(\(current\) => current\.kind === 'ready'[\s\S]*?current\.data\.filter\(\(slot\) => slot\.id !== target\.id\)/)
  assert.match(page, /setLastDeletedShoot\(nextCompletion && target\.type === 'shoot'/)
  assert.match(page, /reconcileCalendarDeleteRefresh\(\{/)
  assert.match(page, /expectedRangeKey: range \? `\$\{range\.from\}:\$\{range\.to\}` : ''/)
  assert.match(page, /timezone: latestAccountTimezoneRef\.current/)
  assert.match(page, /const latestAccountTimezoneRef = useRef\(accountTimezone\)/)
  assert.match(page, /const reloadAfterSettingsRef = useRef\(false\)/)
  assert.match(page, /const settingsIdleWaitersRef = useRef\(new Set<\(\) => void>\(\)\)/)
  assert.match(page, /waitForCalendarSettingsIdle\(\{\s+isIdle: \(\) => settingsRequestRef\.current\.controller === null,[\s\S]*?signal: deleteCoordinationControllerRef\.current\?\.signal/)
  assert.match(page, /if \(settingsRequestRef\.current\.controller\) \{\s+reloadAfterSettingsRef\.current = true\s+await waitForSettingsIdle\(\)\s+return\s+\}/)
  assert.match(page, /if \(reloadAfterSettingsRef\.current\) \{\s+reloadAfterSettingsRef\.current = false\s+requestCalendarReload\(\)\s+\}/)
  assert.match(page, /const waiters = \[\.\.\.settingsIdleWaitersRef\.current\]\s+settingsIdleWaitersRef\.current\.clear\(\)\s+for \(const notifyIdle of waiters\) notifyIdle\(\)/)
  assert.match(page, /latestAccountTimezoneRef\.current = result\.timezone\s+setSettings\(result\)\s+applyDeferredDeleteReconciliationRef\.current\(result\.timezone\)/)
  assert.match(page, /useLayoutEffect\(\(\) => \{\s+applyDeferredDeleteReconciliationRef\.current = applyDeferredDeleteReconciliation\s+\}, \[applyDeferredDeleteReconciliation\]\)/)
  assert.match(page, /calendarDeleteCompletionAfterSettings\(/)
  assert.match(page, /deleteCoordinationController\.abort\(\)[\s\S]*?deferredDeleteReconciliationRef\.current = null/)
  assert.match(page, /pathname: window\.location\.pathname,\s*search: window\.location\.search/)
  assert.match(page, /refreshOriginal: refreshCalendarAfterDelete/)
  assert.match(page, /requestCurrentReload: requestCalendarReload/)
  assert.match(details, /lastDeletedShoot\.date === day\?\.date/)
})

test('calendar dialogs and date controls retain safe close and keyboard fallbacks', () => {
  const scheduleDialog = readFileSync(new URL('../src/components/schedule/ScheduleSlotDialog.tsx', import.meta.url), 'utf8')
  const toolbar = readFileSync(new URL('../src/pages/calendar/CalendarToolbar.tsx', import.meta.url), 'utf8')
  const month = readFileSync(new URL('../src/pages/calendar/MonthCalendar.tsx', import.meta.url), 'utf8')
  const openings = readFileSync(new URL('../src/pages/calendar/OpeningsDialog.tsx', import.meta.url), 'utf8')

  assert.match(scheduleDialog, /useFocusTrap<HTMLElement>\(open, closeDialog, !mutationSaving\)/)
  assert.match(scheduleDialog, /function closeDialog\(\)/)
  assert.match(scheduleDialog, /invalidateConflictPreview\(\)\s+onClose\(\)/)
  assert.match(toolbar, /month \? formatCalendarMonth\(month\) : '—'/)
  assert.match(month, /const tabbableDate = dates\.includes\(selectedDate\) \? selectedDate : dates\[0\]/)
  assert.doesNotMatch(openings, /copyButtonRef/)
})

test('week lanes are cluster-local and endpoint touching is a zero-minute turnaround, not a conflict', () => {
  const availability = availabilityFixture({ turnaroundMinutes: 60 })
  const slots = [
    plainSlot('long', 'hold', '2026-07-06T02:00:00Z', '2026-07-06T04:00:00Z'),
    plainSlot('nested-a', 'busy', '2026-07-06T02:30:00Z', '2026-07-06T03:00:00Z'),
    plainSlot('nested-b', 'hold', '2026-07-06T03:00:00Z', '2026-07-06T03:30:00Z'),
    plainSlot('touching', 'busy', '2026-07-06T04:00:00Z', '2026-07-06T05:00:00Z'),
    plainSlot('after-gap', 'hold', '2026-07-06T05:30:00Z', '2026-07-06T06:00:00Z'),
  ]

  const day = buildCalendarModel(slots, ['2026-07-06'], 'Asia/Shanghai', availability).days[0]
  assert.ok(day)
  const lanes = Object.fromEntries(day.layouts.map((layout) => [
    layout.projection.slot.id,
    [layout.lane, layout.laneCount],
  ]))
  assert.deepEqual(lanes.long, [0, 2])
  assert.deepEqual(lanes['nested-a'], [1, 2])
  assert.deepEqual(lanes['nested-b'], [1, 2])
  assert.deepEqual(lanes.touching, [0, 1])
  assert.deepEqual(lanes['after-gap'], [0, 1])
  assert.equal(day.projections.find((entry) => entry.slot.id === 'touching')?.conflicting, false)
  assert.deepEqual(day.tightTurnarounds.map((item) => [item.previousSlotID, item.currentSlotID, item.gapMinutes]), [
    ['long', 'touching', 0],
    ['touching', 'after-gap', 30],
  ])
})

test('calendar v2 keeps cross-week and all-day projections deterministic', () => {
  const availability = availabilityFixture()
  const slots = [
    plainSlot('cross-week', 'hold', '2026-07-05T15:00:00Z', '2026-07-05T18:00:00Z'),
    plainSlot('all-day', 'busy', '2026-07-05T16:00:00Z', '2026-07-06T16:00:00Z'),
  ]
  const model = buildCalendarModel(slots, ['2026-07-05', '2026-07-06', '2026-07-07'], 'Asia/Shanghai', availability)
  assert.deepEqual(model.days.map((day) => day.projections.map((entry) => entry.slot.id)), [
    ['cross-week'],
    ['all-day', 'cross-week'],
    [],
  ])
  assert.equal(model.days[1]?.projections.find((entry) => entry.slot.id === 'all-day')?.allDay, true)
})

test('month overview follows shoot, hold, conflict, opening, and utilization rules', () => {
  const availability = availabilityFixture({
    weekly: {
      1: { start: '10:00', end: '14:00' },
      2: null,
      3: null,
      4: null,
      5: null,
      6: null,
      7: null,
    },
    minOpeningMinutes: 60,
  })
  const slots = [
    shootSlot('shoot-a', '2026-07-06T02:00:00Z', '2026-07-06T04:00:00Z', 'scheduled'),
    plainSlot('hold-a', 'hold', '2026-07-06T03:00:00Z', '2026-07-06T04:00:00Z'),
    plainSlot('busy-a', 'busy', '2026-07-06T04:00:00Z', '2026-07-06T05:00:00Z'),
    shootSlot('cancelled', '2026-07-13T02:00:00Z', '2026-07-13T06:00:00Z', 'cancelled'),
  ]

  const result = calculateMonthOverview(slots, '2026-07', 'Asia/Shanghai', availability)
  assert.equal(result.errors.length, 0)
  assert.deepEqual(result.overview, {
    shootCount: 1,
    holdDays: 1,
    conflictDays: 1,
    openDays: 4,
    utilization: 18.75,
  })

  const closed = calculateMonthOverview(slots, '2026-07', 'Asia/Shanghai', availabilityFixture({ allClosed: true }))
  assert.equal(closed.overview.utilization, null)
  assert.equal(closed.overview.openDays, 0)
})

test('recurring availability uses Temporal compatible DST mapping and fails closed on reversal', () => {
  const springAvailability = availabilityFixture({
    weekly: { 1: null, 2: null, 3: null, 4: null, 5: null, 6: null, 7: { start: '02:30', end: '04:00' } },
    minOpeningMinutes: 15,
  })
  const spring = buildCalendarModel([], ['2026-03-08'], 'America/New_York', springAvailability)
  assert.equal(spring.errors.length, 0)
  assert.deepEqual(spring.days[0]?.openings.map((item) => [item.start, item.end]), [['03:30', '04:00']])

  const reversedAvailability = availabilityFixture({
    weekly: { 1: null, 2: null, 3: null, 4: null, 5: null, 6: null, 7: { start: '02:30', end: '03:00' } },
    minOpeningMinutes: 15,
  })
  const reversed = buildCalendarModel([], ['2026-03-08'], 'America/New_York', reversedAvailability)
  assert.equal(reversed.errors.length, 1)
  assert.deepEqual(reversed.days[0]?.openings, [])

  const fallAvailability = availabilityFixture({
    weekly: { 1: null, 2: null, 3: null, 4: null, 5: null, 6: null, 7: { start: '01:30', end: '02:30' } },
    minOpeningMinutes: 120,
  })
  const fall = buildCalendarModel([], ['2026-11-01'], 'America/New_York', fallAvailability)
  const opening = fall.days[0]?.openings[0]
  assert.ok(opening)
  assert.equal((Date.parse(opening.endAt) - Date.parse(opening.startAt)) / 60_000, 120)
})

test('calendar date arrow navigation moves focus without selecting a date', () => {
  const dates = Array.from({ length: 14 }, (_, index) => `2026-07-${String(index + 1).padStart(2, '0')}`)

  assert.equal(calendarDateFocusTarget(dates, dates[7]!, 'ArrowLeft'), dates[6])
  assert.equal(calendarDateFocusTarget(dates, dates[7]!, 'ArrowRight'), dates[8])
  assert.equal(calendarDateFocusTarget(dates, dates[7]!, 'ArrowUp'), dates[0])
  assert.equal(calendarDateFocusTarget(dates, dates[0]!, 'ArrowDown'), dates[7])
  assert.equal(calendarDateFocusTarget(dates, dates[0]!, 'ArrowLeft'), null)
  assert.equal(calendarDateFocusTarget(dates, dates[13]!, 'ArrowDown'), null)
  assert.equal(calendarDateFocusTarget(dates, dates[7]!, 'Enter'), null)
})

test('calendar workspace layout mode has one content-width breakpoint', () => {
  assert.equal(SIDE_PANEL_MIN_WIDTH, 1080)
  assert.equal(calendarLayoutModeForWidth(1079), 'bottom-sheet')
  assert.equal(calendarLayoutModeForWidth(1080), 'side-panel')
  assert.equal(calendarLayoutModeForWidth(1600), 'side-panel')
})

test('calendar still projects slots when availability settings are unavailable', () => {
  const slot = plainSlot('settings-failed', 'busy', '2026-07-06T02:00:00Z', '2026-07-06T03:00:00Z')
  const model = buildCalendarModel([slot], ['2026-07-06'], 'Asia/Shanghai', null)

  assert.deepEqual(model.days[0]?.projections.map((entry) => entry.slot.id), ['settings-failed'])
  assert.equal(model.days[0]?.workingWindow, null)
  assert.deepEqual(model.days[0]?.openings, [])
  assert.deepEqual(model.days[0]?.tightTurnarounds, [])
  assert.deepEqual(model.errors, [])
})

test('calendar and conflict preview responses only apply to their latest range generation', () => {
  const firstCalendar = nextCalendarRequest({ rangeKey: '', generation: 0 }, 'month-a')
  const secondCalendar = nextCalendarRequest(firstCalendar, 'month-b')
  assert.equal(isCurrentCalendarRequest(secondCalendar, firstCalendar), false)
  assert.equal(isCurrentCalendarRequest(secondCalendar, secondCalendar), true)

  const firstPreview = {
    key: conflictPreviewRequestKey('2026-07-06T02:00:00Z', '2026-07-06T03:00:00Z', 'slot-1'),
    generation: 1,
  }
  const changedPreview = {
    key: conflictPreviewRequestKey('2026-07-06T04:00:00Z', '2026-07-06T05:00:00Z', 'slot-1'),
    generation: 2,
  }
  assert.equal(isCurrentConflictPreviewRequest(changedPreview, firstPreview), false)
  assert.equal(isCurrentConflictPreviewRequest(changedPreview, changedPreview), true)
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

test('calendar detail panel cannot cover an open schedule dialog', () => {
  assert.equal(calendarDetailShouldOpen('side-panel', false, false), true)
  assert.equal(calendarDetailShouldOpen('side-panel', true, true), false)
  assert.equal(calendarDetailShouldOpen('bottom-sheet', true, false), true)
  assert.equal(calendarDetailShouldOpen('bottom-sheet', false, false), false)
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

test('editing shows the slot customer immediately without opening the picker', () => {
  const flow = readFileSync(new URL('../src/components/schedule/ShootOrderFlow.tsx', import.meta.url), 'utf8')
  // 客户选择器未展开时 options 为空，显示名只能来自 selectedCustomer 回退
  assert.match(flow, /<CustomerPicker[^>]*selectedCustomer=\{selectedCustomer\}/s)

  const dialog = readFileSync(new URL('../src/components/schedule/ScheduleSlotDialog.tsx', import.meta.url), 'utf8')
  assert.match(dialog, /<ShootOrderFlow[^>]*selectedCustomer=\{slotCustomerSelection\}/s)

  // slot 派生的选择（无头像/渠道字段）在候选未加载时也能解析出显示对象
  const slotCustomer = { id: 'cus_slot', display_name: '档案客户', status: 'archived' } as CustomerSelection
  assert.equal(resolveCurrentCustomer([], 'cus_slot', slotCustomer), slotCustomer)
  assert.equal(resolveCurrentCustomer([], 'cus_other', slotCustomer), undefined)
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

function availabilityFixture(options: {
  weekly?: ScheduleAvailability['weekly']
  minOpeningMinutes?: number
  turnaroundMinutes?: number
  allClosed?: boolean
} = {}): ScheduleAvailability {
  const weekly: ScheduleAvailability['weekly'] = options.allClosed
    ? { 1: null, 2: null, 3: null, 4: null, 5: null, 6: null, 7: null }
    : {
        1: { start: '10:00', end: '19:00' },
        2: { start: '10:00', end: '19:00' },
        3: { start: '10:00', end: '19:00' },
        4: { start: '10:00', end: '19:00' },
        5: { start: '10:00', end: '19:00' },
        6: { start: '09:00', end: '20:00' },
        7: { start: '09:00', end: '20:00' },
      }
  return {
    weekly: options.weekly ?? weekly,
    min_opening_minutes: options.minOpeningMinutes ?? 120,
    turnaround_minutes: options.turnaroundMinutes ?? 60,
  }
}

function plainSlot(
  id: string,
  type: 'hold' | 'busy',
  startAt: string,
  endAt: string,
): ScheduleSlotListItem {
  return {
    id,
    account_id: 'acct-calendar-model',
    created_at: startAt,
    start_at: startAt,
    end_at: endAt,
    type,
  }
}

function shootSlot(
  id: string,
  startAt: string,
  endAt: string,
  orderStatus: Extract<ScheduleSlotListItem, { type: 'shoot' }>['order_status'],
): ScheduleSlotListItem {
  return {
    id,
    account_id: 'acct-calendar-model',
    created_at: startAt,
    start_at: startAt,
    end_at: endAt,
    type: 'shoot',
    order_id: `order-${id}`,
    customer_id: `customer-${id}`,
    customer_display_name: `客户 ${id}`,
    customer_status: 'active',
    order_status: orderStatus,
    order_deposit_paid: false,
    order_balance_paid: false,
  }
}

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
