import { useMemo, useState, type CSSProperties, type PointerEvent as ReactPointerEvent } from 'react'
import { ChevronsDownUp, ChevronsUpDown } from 'lucide-react'

import {
  WEEK_EVENT_MIN_VISUAL_MINUTES,
  calendarProjectionTimeLabel,
  calendarTimelineBlock,
  calendarWeekAxisTimeline,
  layoutWeekProjections,
  type CalendarDayModel,
  type CalendarProjection,
  type CalendarTimeline,
  type WeekSlotLayout,
} from './model'
import type { CalendarFilters } from './types'
import { calendarSlotTitle } from './format'
import { calendarDateFocusTarget } from './keyboard'
import {
  WEEK_COLLAPSED_SEGMENT_HEIGHT,
  buildWeekTimeSegments,
  computeWeekCollapseBands,
  createWeekTimeScale,
  dragRangeFromMinutes,
  snapDragMinute,
  weekCollapseBandKey,
  weekHeadingCaption,
  type WeekDragRange,
  type WeekTimeSegment,
} from './weekCollapse'

const HOUR_HEIGHT = 44
const DRAG_STEP_MINUTES = 30

interface DragState {
  date: string
  anchorMinute: number
  currentMinute: number
}

export default function WeekCalendar({
  dates,
  days,
  today,
  selectedDate,
  selectedSlotID,
  filters,
  showCancelled,
  loading,
  timezone,
  writesEnabled,
  expandedBands,
  onToggleBand,
  onCreateRange,
  onSelectDate,
  onSelectSlot,
}: {
  dates: string[]
  days: CalendarDayModel[]
  today: string
  selectedDate: string
  selectedSlotID: string
  filters: CalendarFilters
  showCancelled: boolean
  loading: boolean
  timezone: string | null
  writesEnabled: boolean
  expandedBands: ReadonlySet<string>
  onToggleBand(bandKey: string): void
  onCreateRange(range: WeekDragRange): void
  onSelectDate(date: string): void
  onSelectSlot(date: string, projection: CalendarProjection): void
}) {
  const [drag, setDrag] = useState<DragState | null>(null)
  const byDate = new Map(days.map((day) => [day.date, day]))
  const axisTimeline = calendarWeekAxisTimeline(days, dates)
  const timelineMinutes = Math.max(24 * 60, ...dates.map((date) => byDate.get(date)?.timeline.durationMinutes ?? 0))

  const bands = useMemo(
    () => computeWeekCollapseBands(days, dates, filters, showCancelled),
    [days, dates, filters, showCancelled],
  )
  const segments = useMemo(
    () => buildWeekTimeSegments(bands, expandedBands, timelineMinutes),
    [bands, expandedBands, timelineMinutes],
  )
  const scale = useMemo(
    () => createWeekTimeScale(segments, HOUR_HEIGHT, WEEK_COLLAPSED_SEGMENT_HEIGHT),
    [segments],
  )
  const collapsedBands = bands.filter((band) => !expandedBands.has(weekCollapseBandKey(band)))
  const reopenedBands = bands.filter((band) => expandedBands.has(weekCollapseBandKey(band)))
  const spansMonths = new Set(dates.map((date) => date.slice(0, 7))).size > 1

  const dragDay = drag ? byDate.get(drag.date) : undefined
  const dragStartMinute = drag ? Math.min(drag.anchorMinute, drag.currentMinute) : 0
  const dragEndMinute = drag ? Math.max(drag.anchorMinute, drag.currentMinute) : 0
  const dragRange = drag && dragDay && timezone && dragEndMinute - dragStartMinute >= DRAG_STEP_MINUTES
    ? dragRangeFromMinutes(dragDay.timeline, dragStartMinute, dragEndMinute, timezone)
    : null

  function moveDateFocus(event: React.KeyboardEvent<HTMLButtonElement>, date: string) {
    const targetDate = calendarDateFocusTarget(dates, date, event.key)
    if (!targetDate) return
    const target = event.currentTarget
      .closest('.calendar-v2-week')
      ?.querySelector<HTMLButtonElement>(`[data-calendar-date="${targetDate}"]`)
    if (!target) return
    event.preventDefault()
    target.focus()
  }

  function beginDrag(event: ReactPointerEvent<HTMLDivElement>, date: string) {
    if (!writesEnabled || !timezone || event.button !== 0) return
    if ((event.target as HTMLElement).closest('.calendar-v2-week-event')) return
    const day = byDate.get(date)
    if (!day) return
    const rect = event.currentTarget.getBoundingClientRect()
    const minute = Math.min(
      snapDragMinute(scale.minuteAt(event.clientY - rect.top), timelineMinutes),
      day.timeline.durationMinutes,
    )
    event.currentTarget.setPointerCapture(event.pointerId)
    setDrag({ date, anchorMinute: minute, currentMinute: minute })
  }

  function moveDrag(event: ReactPointerEvent<HTMLDivElement>) {
    if (!drag) return
    const day = byDate.get(drag.date)
    if (!day) return
    event.preventDefault()
    const rect = event.currentTarget.getBoundingClientRect()
    const minute = Math.min(
      snapDragMinute(scale.minuteAt(event.clientY - rect.top), timelineMinutes),
      day.timeline.durationMinutes,
    )
    if (minute === drag.currentMinute) return
    setDrag({ ...drag, currentMinute: minute })
  }

  function endDrag() {
    if (!drag) return
    const range = dragRange
    setDrag(null)
    if (range) onCreateRange(range)
  }

  function bandLabel(band: { startMinutes: number; endMinutes: number }): string {
    return `${axisMinuteLabel(axisTimeline, band.startMinutes)}–${axisMinuteLabel(axisTimeline, band.endMinutes)}`
  }

  return (
    <div className="calendar-v2-week-scroll" aria-busy={loading}>
      <div
        className="calendar-v2-week"
        role="grid"
        aria-label="周历"
        style={{
          '--calendar-hour-height': `${HOUR_HEIGHT}px`,
          '--calendar-event-min-height': `${(WEEK_EVENT_MIN_VISUAL_MINUTES / 60) * HOUR_HEIGHT}px`,
          '--calendar-timeline-height': `${scale.height}px`,
        } as CSSProperties}
      >
        <div className="calendar-v2-week-corner">全天</div>
        {dates.map((date) => {
          const day = byDate.get(date)
          return (
            <button
              type="button"
              className={`calendar-v2-week-heading${date === today ? ' today' : ''}${date === selectedDate ? ' selected' : ''}`}
              key={date}
              onClick={() => onSelectDate(date)}
              onKeyDown={(event) => moveDateFocus(event, date)}
              data-calendar-date={date}
              tabIndex={date === selectedDate || (!selectedDate && date === dates[0]) ? 0 : -1}
              aria-label={`${date}${(day?.conflictCount ?? 0) > 0 ? `，${day?.conflictCount} 条冲突档期` : ''}`}
            >
              <span>{weekHeadingCaption(date, spansMonths)}</span>
              <b>{Number(date.slice(8, 10))}</b>
              {(day?.conflictCount ?? 0) > 0 && <i>{day?.conflictCount} 冲突</i>}
            </button>
          )
        })}

        <div className="calendar-v2-all-day-label">全天</div>
        {dates.map((date) => {
          const day = byDate.get(date)
          const allDay = visibleLayouts(day, filters, showCancelled).filter((layout) => layout.projection.allDay)
          return (
            <div className="calendar-v2-all-day-cell" key={date}>
              {allDay.map((layout) => eventButton(layout, day, selectedSlotID, () => onSelectSlot(date, layout.projection), true, scale))}
            </div>
          )
        })}

        <div className="calendar-v2-time-axis">
          {axisTimeline?.ticks
            .filter((tick) => inExpandedSegment(segments, tick.offsetMinutes))
            .map((tick) => (
              <span key={`${tick.offsetMinutes}-${tick.label}`} style={{ top: scale.topAt(tick.offsetMinutes) }}>
                {tick.label}
              </span>
            ))}
          {collapsedBands.map((band) => (
            <button
              type="button"
              className="calendar-v2-collapse-band"
              key={weekCollapseBandKey(band)}
              style={{ top: scale.topAt(band.startMinutes), height: WEEK_COLLAPSED_SEGMENT_HEIGHT }}
              aria-expanded={false}
              aria-label={`展开 ${bandLabel(band)} 的空闲时段`}
              title={`${bandLabel(band)} 空闲，点击展开`}
              onClick={() => onToggleBand(weekCollapseBandKey(band))}
            >
              <ChevronsUpDown aria-hidden="true" />
              <span>{bandLabel(band)}</span>
            </button>
          ))}
          {reopenedBands.map((band) => (
            <button
              type="button"
              className="calendar-v2-collapse-handle"
              key={`handle-${weekCollapseBandKey(band)}`}
              style={{
                top: scale.topAt(band.startMinutes),
                transform: band.startMinutes === 0 ? 'none' : 'translateY(-50%)',
              }}
              aria-label={`收起 ${bandLabel(band)} 的空闲时段`}
              title={`收起 ${bandLabel(band)}`}
              onClick={() => onToggleBand(weekCollapseBandKey(band))}
            >
              <ChevronsDownUp aria-hidden="true" />
              <span>收起</span>
            </button>
          ))}
        </div>
        {dates.map((date) => {
          const day = byDate.get(date)
          const timed = visibleLayouts(day, filters, showCancelled).filter((layout) => !layout.projection.allDay)
          const ghost = drag && drag.date === date ? dragRange : null
          return (
            <div
              className="calendar-v2-day-track"
              key={date}
              data-timeline-minutes={day?.timeline.durationMinutes}
              data-writable={writesEnabled && timezone ? 'true' : 'false'}
              onDoubleClick={() => onSelectDate(date)}
              onPointerDown={(event) => beginDrag(event, date)}
              onPointerMove={moveDrag}
              onPointerUp={endDrag}
              onPointerCancel={() => setDrag(null)}
            >
              {day?.timeline.ticks
                .filter((tick) => inExpandedSegment(segments, tick.offsetMinutes))
                .map((tick) => (
                  <i
                    className="calendar-v2-hour-line"
                    key={`${tick.offsetMinutes}-${tick.label}`}
                    style={{ top: scale.topAt(tick.offsetMinutes) }}
                  />
                ))}
              {day && day.timeline.durationMinutes !== 24 * 60 && day.timeline.ticks
                .filter((tick) => inExpandedSegment(segments, tick.offsetMinutes))
                .map((tick) => (
                  <em
                    className="calendar-v2-transition-tick"
                    key={`transition-${tick.offsetMinutes}-${tick.label}`}
                    style={{ top: scale.topAt(tick.offsetMinutes) }}
                  >
                    {transitionTickLabel(tick.label, tick.utcOffset)}
                  </em>
                ))}
              {day?.workingWindow && (
                <span
                  className="calendar-v2-working-window"
                  title={`${day.workingWindow.start}–${day.workingWindow.end} 可约`}
                  style={timeBlockStyle(day.timeline, day.workingWindow.startAt, day.workingWindow.endAt, scale)}
                />
              )}
              {collapsedBands.map((band) => (
                <div
                  className="calendar-v2-collapse-strip"
                  key={weekCollapseBandKey(band)}
                  aria-hidden="true"
                  style={{ top: scale.topAt(band.startMinutes), height: WEEK_COLLAPSED_SEGMENT_HEIGHT }}
                />
              ))}
              {timed.map((layout) => eventButton(
                layout,
                day,
                selectedSlotID,
                () => onSelectSlot(date, layout.projection),
                false,
                scale,
              ))}
              {ghost && (
                <div
                  className="calendar-v2-drag-ghost"
                  style={{ top: scale.topAt(dragStartMinute), height: scale.topAt(dragEndMinute) - scale.topAt(dragStartMinute) }}
                >
                  <small>{ghost.startTime}–{ghost.endTime}</small>
                  <span>{dragDurationLabel(dragEndMinute - dragStartMinute)}</span>
                </div>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

function transitionTickLabel(label: string, utcOffset: string): string {
  if (label === '24:00' || label.includes('UTC')) return label
  return `${label} UTC${utcOffset}`
}

function visibleLayouts(
  day: CalendarDayModel | undefined,
  filters: CalendarFilters,
  showCancelled: boolean,
): WeekSlotLayout[] {
  const projections = (day?.projections ?? []).filter((projection) =>
    filters[projection.slot.type] && (showCancelled || !projection.cancelled))
  return layoutWeekProjections(projections)
}

function eventButton(
  layout: WeekSlotLayout,
  day: CalendarDayModel | undefined,
  selectedSlotID: string,
  onClick: () => void,
  allDay: boolean,
  scale: ReturnType<typeof createWeekTimeScale>,
) {
  const projection = layout.projection
  const style = allDay || !day ? undefined : {
    ...timeBlockStyle(day.timeline, projection.startAt, projection.endAt, scale),
    left: `calc(${(layout.lane / layout.laneCount) * 100}% + 2px)`,
    width: `calc(${100 / layout.laneCount}% - 4px)`,
  }
  return (
    <button
      type="button"
      className={[
        'calendar-v2-week-event',
        projection.slot.type,
        projection.conflicting ? 'conflicting' : '',
        projection.cancelled ? 'cancelled' : '',
        projection.slot.id === selectedSlotID ? 'selected' : '',
        allDay ? 'all-day' : '',
      ].filter(Boolean).join(' ')}
      style={style}
      key={projection.slot.id}
      onClick={onClick}
      onDoubleClick={(event) => event.stopPropagation()}
      title={calendarSlotTitle(projection.slot)}
    >
      {!allDay && day && <small>{calendarProjectionTimeLabel(projection, day.timeline)}</small>}
      <span>{calendarSlotTitle(projection.slot)}</span>
    </button>
  )
}

function timeBlockStyle(
  timeline: CalendarTimeline,
  startAt: string,
  endAt: string,
  scale: ReturnType<typeof createWeekTimeScale>,
): CSSProperties {
  const block = calendarTimelineBlock(timeline, startAt, endAt)
  return {
    top: scale.topAt(block.topMinutes),
    height: scale.topAt(block.topMinutes + block.durationMinutes) - scale.topAt(block.topMinutes),
  }
}

function inExpandedSegment(segments: WeekTimeSegment[], minutes: number): boolean {
  return segments.some((segment) => !segment.collapsed && minutes >= segment.startMinutes && minutes < segment.endMinutes)
}

function axisMinuteLabel(axis: CalendarTimeline | undefined, minutes: number): string {
  return axis?.ticks.find((tick) => tick.offsetMinutes === minutes)?.label ?? ''
}

function dragDurationLabel(minutes: number): string {
  if (minutes < 60) return `${minutes} 分钟`
  const hours = minutes / 60
  return `${Number.isInteger(hours) ? hours : hours.toFixed(1)} 小时`
}
