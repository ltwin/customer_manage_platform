import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import type { ScheduleAvailability, ScheduleSlotListItem } from '../src/api/client.ts'
import { buildCalendarModel } from '../src/pages/calendar/model.ts'
import type { CalendarFilters } from '../src/pages/calendar/types.ts'
import {
  buildWeekTimeSegments,
  computeWeekCollapseBands,
  createWeekTimeScale,
  dragCreateDate,
  dragRangeFromMinutes,
  snapDragMinute,
  weekCollapseBandKey,
  weekHeadingCaption,
} from '../src/pages/calendar/weekCollapse.ts'

const allFilters: CalendarFilters = { shoot: true, hold: true, busy: true }
const week = ['2026-07-06', '2026-07-07', '2026-07-08', '2026-07-09', '2026-07-10', '2026-07-11', '2026-07-12']
const timezone = 'Asia/Shanghai'

function availability(weekly: ScheduleAvailability['weekly']): ScheduleAvailability {
  return { weekly, min_opening_minutes: 120, turnaround_minutes: 60 }
}

const tenToNineteen: ScheduleAvailability['weekly'] = {
  1: { start: '10:00', end: '19:00' },
  2: { start: '10:00', end: '19:00' },
  3: { start: '10:00', end: '19:00' },
  4: { start: '10:00', end: '19:00' },
  5: { start: '10:00', end: '19:00' },
  6: { start: '10:00', end: '19:00' },
  7: { start: '10:00', end: '19:00' },
}

function holdSlot(id: string, date: string, start: string, end: string): ScheduleSlotListItem {
  return {
    id,
    account_id: 'acct-week-collapse',
    created_at: `${date}T${start}:00+08:00`,
    start_at: `${date}T${start}:00+08:00`,
    end_at: `${date}T${end}:00+08:00`,
    type: 'hold',
  }
}

function shootSlot(id: string, date: string, start: string, end: string, orderStatus: string): ScheduleSlotListItem {
  return {
    id,
    account_id: 'acct-week-collapse',
    created_at: `${date}T${start}:00+08:00`,
    start_at: `${date}T${start}:00+08:00`,
    end_at: `${date}T${end}:00+08:00`,
    type: 'shoot',
    order_id: `order-${id}`,
    order_status: orderStatus,
  } as ScheduleSlotListItem
}

test('empty week without availability stays fully expanded', () => {
  const { days } = buildCalendarModel([], week, timezone, null)
  assert.deepEqual(computeWeekCollapseBands(days, week, allFilters, false), [])
})

test('working windows count as active; idle hours around them collapse', () => {
  const { days } = buildCalendarModel([], week, timezone, availability(tenToNineteen))
  assert.deepEqual(computeWeekCollapseBands(days, week, allFilters, false), [
    { startMinutes: 0, endMinutes: 600 },
    { startMinutes: 1140, endMinutes: 1440 },
  ])
})

test('overnight slots keep their hours active before the working window', () => {
  const slots = [holdSlot('night', '2026-07-07', '00:30', '01:30')]
  const { days } = buildCalendarModel(slots, week, timezone, availability(tenToNineteen))
  assert.deepEqual(computeWeekCollapseBands(days, week, allFilters, false), [
    { startMinutes: 120, endMinutes: 600 },
    { startMinutes: 1140, endMinutes: 1440 },
  ])
})

test('short slots extend visually to the 30-minute minimum and stay active', () => {
  const slots = [holdSlot('short', '2026-07-07', '10:35', '10:40')]
  const { days } = buildCalendarModel(slots, week, timezone, null)
  // 10:35–10:40 视觉延展到 11:05，11 点整段保持展开；否则 0–11 会连成一个折叠段
  assert.deepEqual(computeWeekCollapseBands(days, week, allFilters, false), [
    { startMinutes: 0, endMinutes: 600 },
    { startMinutes: 720, endMinutes: 1440 },
  ])
})

test('filtered-out slot types do not count as active', () => {
  const slots = [
    holdSlot('hold', '2026-07-07', '10:00', '12:00'),
    shootSlot('shoot', '2026-07-08', '14:00', '16:00', 'scheduled'),
  ]
  const { days } = buildCalendarModel(slots, week, timezone, null)
  const noHold: CalendarFilters = { shoot: true, hold: false, busy: true }
  // hold 被过滤后 10–12 不再活跃；shoot 14–16 仍活跃，隔开两端折叠段
  assert.deepEqual(computeWeekCollapseBands(days, week, noHold, false), [
    { startMinutes: 0, endMinutes: 840 },
    { startMinutes: 960, endMinutes: 1440 },
  ])
})

test('cancelled shoots are ignored unless explicitly shown', () => {
  const slots = [shootSlot('gone', '2026-07-07', '10:00', '12:00', 'cancelled')]
  const { days } = buildCalendarModel(slots, week, timezone, null)
  assert.deepEqual(computeWeekCollapseBands(days, week, allFilters, false), [])
  assert.deepEqual(computeWeekCollapseBands(days, week, allFilters, true), [
    { startMinutes: 0, endMinutes: 600 },
    { startMinutes: 720, endMinutes: 1440 },
  ])
})

test('idle runs shorter than two hours do not collapse', () => {
  const slots = [holdSlot('evening', '2026-07-07', '20:00', '21:00')]
  const { days } = buildCalendarModel(slots, week, timezone, availability(tenToNineteen))
  // 19:00–20:00 只有 1 小时空闲，不折叠；21:00–24:00 共 3 小时折叠
  assert.deepEqual(computeWeekCollapseBands(days, week, allFilters, false), [
    { startMinutes: 0, endMinutes: 600 },
    { startMinutes: 1260, endMinutes: 1440 },
  ])
})

test('segments cover the timeline and honour expanded keys', () => {
  const bands = [
    { startMinutes: 0, endMinutes: 600 },
    { startMinutes: 1140, endMinutes: 1440 },
  ]
  assert.deepEqual(buildWeekTimeSegments(bands, new Set(), 1440), [
    { startMinutes: 0, endMinutes: 600, collapsed: true },
    { startMinutes: 600, endMinutes: 1140, collapsed: false },
    { startMinutes: 1140, endMinutes: 1440, collapsed: true },
  ])
  const expanded = new Set(['0-600'])
  assert.deepEqual(buildWeekTimeSegments(bands, expanded, 1440), [
    { startMinutes: 0, endMinutes: 1140, collapsed: false },
    { startMinutes: 1140, endMinutes: 1440, collapsed: true },
  ])
  assert.deepEqual(buildWeekTimeSegments([], new Set(), 1440), [
    { startMinutes: 0, endMinutes: 1440, collapsed: false },
  ])
})

test('weekCollapseBandKey is stable across weeks', () => {
  assert.equal(weekCollapseBandKey({ startMinutes: 600, endMinutes: 1140 }), '600-1140')
})

test('scale maps minutes through collapsed bands', () => {
  const scale = createWeekTimeScale([
    { startMinutes: 0, endMinutes: 600, collapsed: false },
    { startMinutes: 600, endMinutes: 1140, collapsed: true },
    { startMinutes: 1140, endMinutes: 1440, collapsed: false },
  ], 44, 30)
  assert.equal(scale.height, 10 * 44 + 30 + 5 * 44)
  assert.equal(scale.topAt(0), 0)
  assert.equal(scale.topAt(600), 440)
  assert.equal(scale.topAt(1140), 470)
  assert.equal(scale.topAt(1440), 690)
  assert.equal(scale.minuteAt(0), 0)
  assert.equal(scale.minuteAt(440), 600)
  assert.equal(scale.minuteAt(690), 1440)
  // 展开段内往返一致
  assert.equal(scale.minuteAt(scale.topAt(300)), 300)
  assert.equal(scale.minuteAt(scale.topAt(1200)), 1200)
  // 折叠带内的像素点吸附到最近的边缘分钟
  assert.equal(scale.minuteAt(448), 600)
  assert.equal(scale.minuteAt(462), 1140)
})

test('snapDragMinute rounds to half hours within the timeline', () => {
  assert.equal(snapDragMinute(37, 1440), 30)
  assert.equal(snapDragMinute(23, 1440), 30)
  assert.equal(snapDragMinute(745, 1440), 750)
  assert.equal(snapDragMinute(-5, 1440), 0)
  assert.equal(snapDragMinute(1450, 1440), 1440)
})

test('dragRangeFromMinutes renders local times and rolls to next day at 24:00', () => {
  const { days } = buildCalendarModel([], ['2026-07-06'], timezone, null)
  const timeline = days[0]!.timeline
  assert.deepEqual(dragRangeFromMinutes(timeline, 600, 690, timezone), {
    startDate: '2026-07-06',
    startTime: '10:00',
    endDate: '2026-07-06',
    endTime: '11:30',
  })
  assert.deepEqual(dragRangeFromMinutes(timeline, 1380, 1440, timezone), {
    startDate: '2026-07-06',
    startTime: '23:00',
    endDate: '2026-07-07',
    endTime: '00:00',
  })
})

test('dragRangeFromMinutes follows the local clock across DST fall back', () => {
  const { days } = buildCalendarModel([], ['2026-11-01'], 'America/New_York', null)
  const timeline = days[0]!.timeline
  // 25 小时的一天：timeline 分钟 1440 是本地 23:00，1500 才是次日 00:00
  assert.deepEqual(dragRangeFromMinutes(timeline, 0, 30, 'America/New_York'), {
    startDate: '2026-11-01',
    startTime: '00:00',
    endDate: '2026-11-01',
    endTime: '00:30',
  })
  assert.deepEqual(dragRangeFromMinutes(timeline, 1440, 1500, 'America/New_York'), {
    startDate: '2026-11-01',
    startTime: '23:00',
    endDate: '2026-11-02',
    endTime: '00:00',
  })
})

test('dragged October range is the authoritative create date over stale September selection', () => {
  const range = {
    startDate: '2026-10-01',
    startTime: '14:00',
    endDate: '2026-10-01',
    endTime: '16:00',
  }
  assert.equal(dragCreateDate(range, '2026-09-30', '2026-09-04'), '2026-10-01')
  assert.equal(dragCreateDate(null, '2026-09-30', '2026-09-04'), '2026-09-30')
})

test('cross-month week headings disclose the month of each date', () => {
  assert.equal(weekHeadingCaption('2026-09-28', true), '周一 · 9月')
  assert.equal(weekHeadingCaption('2026-10-01', true), '周四 · 10月')
  assert.equal(weekHeadingCaption('2026-10-01', false), '周四')
})

test('schedule dialog resets the draft before paint and tracks drag range changes', () => {
  const source = readFileSync(new URL('../src/components/schedule/ScheduleSlotDialog.tsx', import.meta.url), 'utf8')
  assert.match(source, /useLayoutEffect\(\(\) => \{\s+if \(!open\) return\s+invalidateConflictPreview\(\)/)
  assert.match(source, /\[open, slot, scheduleDraftId, initialDate, initialRange,/)
})
