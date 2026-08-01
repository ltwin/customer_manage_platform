import { Temporal } from '@js-temporal/polyfill'

import type {
  ScheduleAvailability,
  ScheduleSlotListItem,
} from '../../api/client.ts'
import {
  localDayRange,
  projectSlotToLocalDays,
} from '../../components/schedule/timezone.ts'
import { isCancelledShoot } from '../../components/schedule/calendarModel.ts'

export interface Opening {
  date: string
  start: string
  end: string
  startAt: string
  endAt: string
}

export interface TightTurnaround {
  previousSlotID: string
  currentSlotID: string
  gapMinutes: number
}

export interface CalendarProjection {
  slot: ScheduleSlotListItem
  date: string
  allDay: boolean
  displayStart: string
  displayEnd: string
  displayStartOffset: string
  displayEndOffset: string
  startAt: string
  endAt: string
  conflicting: boolean
  cancelled: boolean
}

export interface CalendarTimelineTick {
  offsetMinutes: number
  label: string
  utcOffset: string
}

export interface CalendarTimeline {
  startAt: string
  endAt: string
  durationMinutes: number
  ticks: CalendarTimelineTick[]
}

export interface CalendarTimelineBlock {
  topMinutes: number
  durationMinutes: number
}

export interface WeekSlotLayout {
  projection: CalendarProjection
  lane: number
  laneCount: number
  conflicting: boolean
  cancelled: boolean
}

export interface WorkingWindow {
  start: string
  end: string
  startAt: string
  endAt: string
}

export interface CalendarDayModel {
  date: string
  timeline: CalendarTimeline
  projections: CalendarProjection[]
  layouts: WeekSlotLayout[]
  tightTurnarounds: TightTurnaround[]
  openings: Opening[]
  workingWindow: WorkingWindow | null
  conflictCount: number
  cancelledCount: number
  turnaroundThresholdMinutes: number | null
}

export interface AvailabilityCalculationError {
  date: string
  message: string
}

export interface CalendarModel {
  days: CalendarDayModel[]
  errors: AvailabilityCalculationError[]
}

export interface MonthOverview {
  shootCount: number
  holdDays: number
  conflictDays: number
  openDays: number
  utilization: number | null
}

interface InternalProjection extends CalendarProjection {
  startEpochMs: number
  endEpochMs: number
}

interface InternalWorkingWindow extends WorkingWindow {
  startEpochMs: number
  endEpochMs: number
}

export function buildCalendarModel(
  slots: ScheduleSlotListItem[],
  dates: string[],
  timezone: string,
  availability: ScheduleAvailability | null,
): CalendarModel {
  const byDate = projectSlots(slots, dates, timezone)
  const errors: AvailabilityCalculationError[] = []
  const days = dates.map((date): CalendarDayModel => {
    const timeline = calendarTimeline(date, timezone)
    const projections = byDate.get(date) ?? []
    markConflicts(projections)
    let workingWindow: InternalWorkingWindow | null = null
    let openings: Opening[] = []
    if (availability) {
      try {
        workingWindow = resolveWorkingWindow(date, timezone, availability)
        if (workingWindow) {
          openings = openingsForDay(
            date,
            timezone,
            workingWindow,
            projections,
            availability.min_opening_minutes,
          )
        }
      } catch (reason) {
        errors.push({
          date,
          message: reason instanceof Error ? reason.message : '可约时段计算失败',
        })
      }
    }
    return {
      date,
      timeline,
      projections,
      layouts: layoutWeekProjections(projections),
      tightTurnarounds: availability
        ? tightTurnaroundsForDay(projections, availability.turnaround_minutes)
        : [],
      openings,
      workingWindow,
      conflictCount: projections.filter((projection) => projection.conflicting).length,
      cancelledCount: projections.filter((projection) => projection.cancelled).length,
      turnaroundThresholdMinutes: availability?.turnaround_minutes ?? null,
    }
  })
  return { days, errors }
}

export function calculateMonthOverview(
  slots: ScheduleSlotListItem[],
  month: string,
  timezone: string,
  availability: ScheduleAvailability,
): { overview: MonthOverview; errors: AvailabilityCalculationError[] } {
  const dates = datesInMonth(month)
  const model = buildCalendarModel(slots, dates, timezone, availability)
  const shootIDs = new Set<string>()
  let utilizedMs = 0
  let availableMs = 0
  let holdDays = 0

  for (const day of model.days) {
    if (day.projections.some((projection) => !projection.cancelled && projection.slot.type === 'hold')) {
      holdDays += 1
    }
    for (const projection of day.projections) {
      if (!projection.cancelled && projection.slot.type === 'shoot') {
        shootIDs.add(projection.slot.id)
      }
    }
    if (!day.workingWindow) continue
    const workStart = Date.parse(day.workingWindow.startAt)
    const workEnd = Date.parse(day.workingWindow.endAt)
    availableMs += workEnd - workStart
    for (const projection of day.projections) {
      if (projection.cancelled || (projection.slot.type !== 'shoot' && projection.slot.type !== 'hold')) {
        continue
      }
      const occupiedStart = Math.max(Date.parse(projection.startAt), workStart)
      const occupiedEnd = Math.min(Date.parse(projection.endAt), workEnd)
      if (occupiedStart < occupiedEnd) utilizedMs += occupiedEnd - occupiedStart
    }
  }

  const utilization = availableMs === 0
    ? null
    : roundToTwo(Math.min(100, (utilizedMs / availableMs) * 100))
  return {
    overview: {
      shootCount: shootIDs.size,
      holdDays,
      conflictDays: model.days.filter((day) => day.conflictCount > 0).length,
      openDays: model.days.filter((day) => day.openings.length > 0).length,
      utilization,
    },
    errors: model.errors,
  }
}

function projectSlots(
  slots: ScheduleSlotListItem[],
  dates: string[],
  timezone: string,
): Map<string, InternalProjection[]> {
  const byDate = new Map<string, InternalProjection[]>(dates.map((date) => [date, []]))
  for (const slot of slots) {
    const slotStart = Date.parse(slot.start_at)
    const slotEnd = Date.parse(slot.end_at)
    const projections = projectSlotToLocalDays(
      { startAt: slot.start_at, endAt: slot.end_at },
      timezone,
    )
    for (const projection of projections) {
      const entries = byDate.get(projection.date)
      if (!entries) continue
      const range = localDayRange(projection.date, timezone)
      const startEpochMs = Math.max(slotStart, Date.parse(range.start))
      const endEpochMs = Math.min(slotEnd, Date.parse(range.end))
      entries.push({
        slot,
        ...projection,
        startAt: new Date(startEpochMs).toISOString(),
        endAt: new Date(endEpochMs).toISOString(),
        displayStartOffset: projection.displayStart === '00:00'
          ? ''
          : Temporal.Instant.fromEpochMilliseconds(startEpochMs).toZonedDateTimeISO(timezone).offset,
        displayEndOffset: projection.displayEnd === '24:00'
          ? ''
          : Temporal.Instant.fromEpochMilliseconds(endEpochMs).toZonedDateTimeISO(timezone).offset,
        startEpochMs,
        endEpochMs,
        conflicting: false,
        cancelled: isCancelledShoot(slot),
      })
    }
  }
  for (const entries of byDate.values()) {
    entries.sort(compareProjection)
  }
  return byDate
}

function markConflicts(projections: InternalProjection[]): void {
  for (let left = 0; left < projections.length; left += 1) {
    const first = projections[left]
    if (!first || first.cancelled) continue
    for (let right = left + 1; right < projections.length; right += 1) {
      const second = projections[right]
      if (!second || second.cancelled) continue
      if (second.startEpochMs >= first.endEpochMs) break
      if (first.startEpochMs < second.endEpochMs && second.startEpochMs < first.endEpochMs) {
        first.conflicting = true
        second.conflicting = true
      }
    }
  }
}

export function layoutWeekProjections(projections: CalendarProjection[]): WeekSlotLayout[] {
  const layouts = new Map<CalendarProjection, WeekSlotLayout>()
  const timed = projections.filter((projection) => !projection.allDay)
  let cluster: CalendarProjection[] = []
  let clusterEnd = Number.NEGATIVE_INFINITY

  const flush = () => {
    if (cluster.length === 0) return
    const laneEnds: number[] = []
    const clusterLayouts: WeekSlotLayout[] = []
    for (const projection of cluster) {
      const startEpochMs = Date.parse(projection.startAt)
      let lane = laneEnds.findIndex((end) => end <= startEpochMs)
      if (lane === -1) lane = laneEnds.length
      laneEnds[lane] = weekEventVisualEndEpochMs(projection)
      const layout: WeekSlotLayout = {
        projection,
        lane,
        laneCount: 0,
        conflicting: projection.conflicting,
        cancelled: projection.cancelled,
      }
      clusterLayouts.push(layout)
      layouts.set(projection, layout)
    }
    for (const layout of clusterLayouts) layout.laneCount = laneEnds.length
    cluster = []
    clusterEnd = Number.NEGATIVE_INFINITY
  }

  for (const projection of timed) {
    const startEpochMs = Date.parse(projection.startAt)
    if (cluster.length > 0 && startEpochMs >= clusterEnd) flush()
    cluster.push(projection)
    clusterEnd = Math.max(clusterEnd, weekEventVisualEndEpochMs(projection))
  }
  flush()

  for (const projection of projections) {
    if (layouts.has(projection)) continue
    layouts.set(projection, {
      projection,
      lane: 0,
      laneCount: 1,
      conflicting: projection.conflicting,
      cancelled: projection.cancelled,
    })
  }
  return projections.map((projection) => layouts.get(projection) as WeekSlotLayout)
}

export const WEEK_EVENT_MIN_VISUAL_MINUTES = 30

function weekEventVisualEndEpochMs(projection: CalendarProjection): number {
  return Math.max(
    Date.parse(projection.endAt),
    Date.parse(projection.startAt) + WEEK_EVENT_MIN_VISUAL_MINUTES * 60_000,
  )
}

function tightTurnaroundsForDay(
  projections: InternalProjection[],
  thresholdMinutes: number,
): TightTurnaround[] {
  if (thresholdMinutes <= 0) return []
  const active = projections.filter((projection) => !projection.cancelled && !projection.allDay)
  const result: TightTurnaround[] = []
  for (let currentIndex = 0; currentIndex < active.length; currentIndex += 1) {
    const current = active[currentIndex]
    if (!current || current.conflicting) continue
    let previous: InternalProjection | null = null
    for (let index = 0; index < currentIndex; index += 1) {
      const candidate = active[index]
      if (!candidate || candidate.endEpochMs > current.startEpochMs) continue
      if (!previous || candidate.endEpochMs > previous.endEpochMs ||
        (candidate.endEpochMs === previous.endEpochMs && compareProjection(candidate, previous) < 0)) {
        previous = candidate
      }
    }
    if (!previous) continue
    const gapMinutes = (current.startEpochMs - previous.endEpochMs) / 60_000
    if (gapMinutes < thresholdMinutes) {
      result.push({
        previousSlotID: previous.slot.id,
        currentSlotID: current.slot.id,
        gapMinutes,
      })
    }
  }
  return result
}

function resolveWorkingWindow(
  date: string,
  timezone: string,
  availability: ScheduleAvailability,
): InternalWorkingWindow | null {
  const plainDate = Temporal.PlainDate.from(date)
  const weekday = plainDate.dayOfWeek as keyof ScheduleAvailability['weekly']
  const configured = availability.weekly[weekday]
  if (!configured) return null
  let start: Temporal.ZonedDateTime
  let end: Temporal.ZonedDateTime
  try {
    start = Temporal.PlainDateTime.from(`${date}T${configured.start}`)
      .toZonedDateTime(timezone, { disambiguation: 'compatible' })
    end = Temporal.PlainDateTime.from(`${date}T${configured.end}`)
      .toZonedDateTime(timezone, { disambiguation: 'compatible' })
  } catch {
    throw new Error('可约时段或账号时区无效')
  }
  const startInstant = start.toInstant()
  const endInstant = end.toInstant()
  if (Temporal.Instant.compare(startInstant, endInstant) >= 0) {
    throw new Error('DST 映射后的可约结束时间不晚于开始时间')
  }
  return {
    start: localTime(startInstant, timezone),
    end: localTime(endInstant, timezone),
    startAt: instantString(startInstant),
    endAt: instantString(endInstant),
    startEpochMs: startInstant.epochMilliseconds,
    endEpochMs: endInstant.epochMilliseconds,
  }
}

function openingsForDay(
  date: string,
  timezone: string,
  workingWindow: InternalWorkingWindow,
  projections: InternalProjection[],
  minOpeningMinutes: number,
): Opening[] {
  const occupied = projections
    .filter((projection) => !projection.cancelled)
    .map((projection) => ({
      start: Math.max(projection.startEpochMs, workingWindow.startEpochMs),
      end: Math.min(projection.endEpochMs, workingWindow.endEpochMs),
    }))
    .filter((interval) => interval.start < interval.end)
    .sort((left, right) => left.start - right.start || left.end - right.end)

  const merged: Array<{ start: number; end: number }> = []
  for (const interval of occupied) {
    const previous = merged[merged.length - 1]
    if (!previous || interval.start > previous.end) {
      merged.push({ ...interval })
    } else {
      previous.end = Math.max(previous.end, interval.end)
    }
  }

  const result: Opening[] = []
  let cursor = workingWindow.startEpochMs
  for (const interval of merged) {
    appendOpening(result, date, timezone, cursor, interval.start, minOpeningMinutes)
    cursor = Math.max(cursor, interval.end)
  }
  appendOpening(
    result,
    date,
    timezone,
    cursor,
    workingWindow.endEpochMs,
    minOpeningMinutes,
  )
  return result
}

function appendOpening(
  result: Opening[],
  date: string,
  timezone: string,
  startEpochMs: number,
  endEpochMs: number,
  minOpeningMinutes: number,
): void {
  if (endEpochMs - startEpochMs < minOpeningMinutes * 60_000) return
  const start = Temporal.Instant.fromEpochMilliseconds(startEpochMs)
  const end = Temporal.Instant.fromEpochMilliseconds(endEpochMs)
  result.push({
    date,
    start: localTime(start, timezone),
    end: localTime(end, timezone),
    startAt: instantString(start),
    endAt: instantString(end),
  })
}

function datesInMonth(month: string): string[] {
  let value: Temporal.PlainYearMonth
  try {
    value = Temporal.PlainYearMonth.from(month)
  } catch {
    throw new Error('月份格式无效')
  }
  return Array.from({ length: value.daysInMonth }, (_, index) =>
    value.toPlainDate({ day: index + 1 }).toString())
}

export function calendarTimeline(date: string, timezone: string): CalendarTimeline {
  const range = localDayRange(date, timezone)
  const start = Temporal.Instant.from(range.start)
  const end = Temporal.Instant.from(range.end)
  const durationMinutes = Number(end.epochMilliseconds - start.epochMilliseconds) / 60_000
  const rawTicks: Array<{ offsetMinutes: number; local: string; offset: string }> = []
  for (let offsetMinutes = 0; offsetMinutes <= durationMinutes; offsetMinutes += 60) {
    const instant = start.add({ minutes: offsetMinutes })
    const local = instant.toZonedDateTimeISO(timezone)
    rawTicks.push({
      offsetMinutes,
      local: offsetMinutes === durationMinutes
        ? '24:00'
        : `${String(local.hour).padStart(2, '0')}:${String(local.minute).padStart(2, '0')}`,
      offset: local.offset,
    })
  }
  if (rawTicks.at(-1)?.offsetMinutes !== durationMinutes) {
    const local = end.toZonedDateTimeISO(timezone)
    rawTicks.push({
      offsetMinutes: durationMinutes,
      local: '24:00',
      offset: local.offset,
    })
  }
  const repeatedLabels = new Set(
    rawTicks
      .filter((tick) => tick.local !== '24:00')
      .map((tick) => tick.local)
      .filter((label, index, labels) => labels.indexOf(label) !== index),
  )
  return {
    startAt: instantString(start),
    endAt: instantString(end),
    durationMinutes,
    ticks: rawTicks.map((tick) => ({
      offsetMinutes: tick.offsetMinutes,
      label: repeatedLabels.has(tick.local) ? `${tick.local} UTC${tick.offset}` : tick.local,
      utcOffset: tick.offset,
    })),
  }
}

export function calendarWeekAxisTimeline(
  days: CalendarDayModel[],
  dates: string[],
): CalendarTimeline | undefined {
  const byDate = new Map(days.map((day) => [day.date, day.timeline]))
  const timelines = dates.flatMap((date) => {
    const timeline = byDate.get(date)
    return timeline ? [timeline] : []
  })
  return timelines.find((timeline) => timeline.durationMinutes === 24 * 60) ?? timelines[0]
}

export function calendarTimelineBlock(
  timeline: CalendarTimeline,
  startAt: string,
  endAt: string,
): CalendarTimelineBlock {
  const timelineStart = Date.parse(timeline.startAt)
  const timelineEnd = Date.parse(timeline.endAt)
  const start = Math.max(Date.parse(startAt), timelineStart)
  const end = Math.min(Date.parse(endAt), timelineEnd)
  return {
    topMinutes: Math.max(0, (start - timelineStart) / 60_000),
    durationMinutes: Math.max(0, (end - start) / 60_000),
  }
}

export function calendarProjectionTimeLabel(
  projection: CalendarProjection,
  timeline: CalendarTimeline,
): string {
  if (projection.allDay) return '全天'
  if (timeline.durationMinutes === 24 * 60) {
    return `${projection.displayStart}–${projection.displayEnd}`
  }
  const start = projection.displayStartOffset
    ? `${projection.displayStart} UTC${projection.displayStartOffset}`
    : projection.displayStart
  const end = projection.displayEndOffset
    ? `${projection.displayEnd} UTC${projection.displayEndOffset}`
    : projection.displayEnd
  return `${start}–${end}`
}

function compareProjection(left: InternalProjection, right: InternalProjection): number {
  return left.startEpochMs - right.startEpochMs || left.slot.id.localeCompare(right.slot.id)
}

function instantString(instant: Temporal.Instant): string {
  return instant.toString({ smallestUnit: 'second' })
}

function localTime(instant: Temporal.Instant, timezone: string): string {
  const value = instant.toZonedDateTimeISO(timezone)
  return `${String(value.hour).padStart(2, '0')}:${String(value.minute).padStart(2, '0')}`
}

function roundToTwo(value: number): number {
  return Math.round((value + Number.EPSILON) * 100) / 100
}
