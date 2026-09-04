import { Temporal } from '@js-temporal/polyfill'

import {
  WEEK_EVENT_MIN_VISUAL_MINUTES,
  calendarTimelineBlock,
  type CalendarDayModel,
  type CalendarTimeline,
} from './model.ts'
import type { CalendarFilters } from './types.ts'

/** 连续空闲不足两小时不值得折叠：44px→30px 只省一点高度，却把视觉切碎 */
export const WEEK_COLLAPSE_MIN_MINUTES = 120
export const WEEK_COLLAPSED_SEGMENT_HEIGHT = 30

export interface WeekCollapseBand {
  startMinutes: number
  endMinutes: number
}

export interface WeekTimeSegment {
  startMinutes: number
  endMinutes: number
  collapsed: boolean
}

export interface WeekTimeScale {
  height: number
  topAt(minutes: number): number
  minuteAt(px: number): number
}

export interface WeekDragRange {
  startDate: string
  startTime: string
  endDate: string
  endTime: string
}

export function dragCreateDate(
  range: WeekDragRange | null,
  selectedDate: string,
  today: string,
): string {
  return range?.startDate || selectedDate || today
}

export function weekHeadingCaption(date: string, spansMonths: boolean): string {
  const day = weekday(date)
  return spansMonths ? `${day} · ${Number(date.slice(5, 7))}月` : day
}

export function weekCollapseBandKey(band: WeekCollapseBand): string {
  return `${band.startMinutes}-${band.endMinutes}`
}

/**
 * 计算周视图默认折叠带：整点对齐、整周统一。
 * 活跃 = 任一天的可见档期（含最短视觉高度延展）或可约窗口覆盖；
 * 连续空闲 ≥ WEEK_COLLAPSE_MIN_MINUTES 才折叠；全周无活跃内容时保持全展开。
 */
export function computeWeekCollapseBands(
  days: CalendarDayModel[],
  dates: string[],
  filters: CalendarFilters,
  showCancelled: boolean,
): WeekCollapseBand[] {
  const byDate = new Map(days.map((day) => [day.date, day]))
  const busy: Array<{ start: number; end: number }> = []
  for (const date of dates) {
    const day = byDate.get(date)
    if (!day) continue
    for (const projection of day.projections) {
      if (projection.allDay) continue
      if (!filters[projection.slot.type]) continue
      if (!showCancelled && projection.cancelled) continue
      const block = calendarTimelineBlock(day.timeline, projection.startAt, projection.endAt)
      busy.push({
        start: block.topMinutes,
        end: block.topMinutes + Math.max(block.durationMinutes, WEEK_EVENT_MIN_VISUAL_MINUTES),
      })
    }
    if (day.workingWindow) {
      const block = calendarTimelineBlock(day.timeline, day.workingWindow.startAt, day.workingWindow.endAt)
      busy.push({ start: block.topMinutes, end: block.topMinutes + block.durationMinutes })
    }
  }

  const timelineMinutes = Math.max(
    24 * 60,
    ...dates.map((date) => byDate.get(date)?.timeline.durationMinutes ?? 0),
  )
  const isIdleHour = (hourStart: number) =>
    !busy.some((span) => span.start < hourStart + 60 && span.end > hourStart)

  const bands: WeekCollapseBand[] = []
  const lastHourStart = Math.floor(timelineMinutes / 60) * 60 - 60
  let runStart = -1
  for (let minute = 0; minute <= lastHourStart + 60; minute += 60) {
    const idle = minute <= lastHourStart && isIdleHour(minute)
    if (idle && runStart < 0) runStart = minute
    if (idle) continue
    if (runStart >= 0) {
      const runEnd = Math.min(minute, timelineMinutes)
      if (runEnd - runStart >= WEEK_COLLAPSE_MIN_MINUTES) {
        bands.push({ startMinutes: runStart, endMinutes: runEnd })
      }
      runStart = -1
    }
  }

  const collapsedMinutes = bands.reduce((total, band) => total + band.endMinutes - band.startMinutes, 0)
  if (collapsedMinutes >= timelineMinutes) return []
  return bands
}

export function buildWeekTimeSegments(
  bands: WeekCollapseBand[],
  expandedKeys: ReadonlySet<string>,
  timelineMinutes: number,
): WeekTimeSegment[] {
  const segments: WeekTimeSegment[] = []
  let cursor = 0
  for (const band of [...bands].sort((left, right) => left.startMinutes - right.startMinutes)) {
    if (band.startMinutes > cursor) {
      segments.push({ startMinutes: cursor, endMinutes: band.startMinutes, collapsed: false })
    }
    segments.push({ ...band, collapsed: !expandedKeys.has(weekCollapseBandKey(band)) })
    cursor = Math.max(cursor, band.endMinutes)
  }
  if (cursor < timelineMinutes) {
    segments.push({ startMinutes: cursor, endMinutes: timelineMinutes, collapsed: false })
  }
  return collapseAdjacentExpanded(segments)
}

function collapseAdjacentExpanded(segments: WeekTimeSegment[]): WeekTimeSegment[] {
  const merged: WeekTimeSegment[] = []
  for (const segment of segments) {
    const previous = merged.at(-1)
    if (previous && !previous.collapsed && !segment.collapsed) {
      previous.endMinutes = segment.endMinutes
      continue
    }
    merged.push({ ...segment })
  }
  return merged
}

export function createWeekTimeScale(
  segments: WeekTimeSegment[],
  hourHeight: number,
  collapsedHeight: number,
): WeekTimeScale {
  const layout = segments.map((segment) => {
    const height = segment.collapsed
      ? collapsedHeight
      : ((segment.endMinutes - segment.startMinutes) / 60) * hourHeight
    return { segment, height }
  })
  const tops: number[] = []
  let height = 0
  for (const entry of layout) {
    tops.push(height)
    height += entry.height
  }

  function segmentIndexAt(minutes: number): number {
    const clamped = Math.min(Math.max(minutes, 0), segments.at(-1)?.endMinutes ?? 0)
    for (let index = 0; index < segments.length; index += 1) {
      const segment = segments[index]!
      if (clamped >= segment.startMinutes && clamped < segment.endMinutes) return index
    }
    return segments.length - 1
  }

  return {
    height,
    topAt(minutes: number) {
      if (segments.length === 0) return 0
      const index = segmentIndexAt(minutes)
      const segment = segments[index]!
      const entry = layout[index]!
      const inner = segment.collapsed
        ? ((Math.min(minutes, segment.endMinutes) - segment.startMinutes) /
          (segment.endMinutes - segment.startMinutes)) * entry.height
        : ((minutes - segment.startMinutes) / 60) * hourHeight
      return tops[index]! + inner
    },
    minuteAt(px: number) {
      if (segments.length === 0) return 0
      const clamped = Math.min(Math.max(px, 0), height)
      for (let index = 0; index < layout.length; index += 1) {
        const entry = layout[index]!
        const top = tops[index]!
        if (clamped <= top + entry.height || index === layout.length - 1) {
          const segment = segments[index]!
          if (segment.collapsed) {
            return clamped - top <= entry.height / 2
              ? segment.startMinutes
              : segment.endMinutes
          }
          return segment.startMinutes + ((clamped - top) / hourHeight) * 60
        }
      }
      return segments.at(-1)!.endMinutes
    },
  }
}

export function snapDragMinute(minute: number, timelineMinutes: number): number {
  return Math.min(Math.max(Math.round(minute / 30) * 30, 0), timelineMinutes)
}

export function dragRangeFromMinutes(
  timeline: CalendarTimeline,
  startMinutes: number,
  endMinutes: number,
  timezone: string,
): WeekDragRange {
  const start = minuteToLocal(timeline, startMinutes, timezone)
  const end = minuteToLocal(timeline, endMinutes, timezone)
  return {
    startDate: start.date,
    startTime: start.time,
    endDate: end.date,
    endTime: end.time,
  }
}

function minuteToLocal(
  timeline: CalendarTimeline,
  minutes: number,
  timezone: string,
): { date: string; time: string } {
  const local = Temporal.Instant.from(timeline.startAt)
    .add({ minutes })
    .toZonedDateTimeISO(timezone)
  return {
    date: local.toPlainDate().toString(),
    time: `${String(local.hour).padStart(2, '0')}:${String(local.minute).padStart(2, '0')}`,
  }
}

function weekday(date: string): string {
  const names = ['周一', '周二', '周三', '周四', '周五', '周六', '周日']
  const value = new Date(`${date}T12:00:00Z`).getUTCDay()
  return names[(value + 6) % 7] ?? ''
}
