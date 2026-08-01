import type { CSSProperties } from 'react'

import {
  calendarProjectionTimeLabel,
  calendarTimelineBlock,
  calendarWeekAxisTimeline,
  layoutWeekProjections,
  type CalendarDayModel,
  type CalendarProjection,
  type CalendarTimeline,
  type WeekSlotLayout,
  WEEK_EVENT_MIN_VISUAL_MINUTES,
} from './model'
import type { CalendarFilters } from './types'
import { calendarSlotTitle } from './format'
import { calendarDateFocusTarget } from './keyboard'

const HOUR_HEIGHT = 44

export default function WeekCalendar({
  dates,
  days,
  today,
  selectedDate,
  selectedSlotID,
  filters,
  showCancelled,
  loading,
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
  onSelectDate(date: string): void
  onSelectSlot(date: string, projection: CalendarProjection): void
}) {
  const byDate = new Map(days.map((day) => [day.date, day]))
  const axisTimeline = calendarWeekAxisTimeline(days, dates)
  const timelineMinutes = Math.max(24 * 60, ...dates.map((date) => byDate.get(date)?.timeline.durationMinutes ?? 0))

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

  return (
    <div className="calendar-v2-week-scroll" aria-busy={loading}>
      <div
        className="calendar-v2-week"
        role="grid"
        aria-label="周历"
        style={{
          '--calendar-hour-height': `${HOUR_HEIGHT}px`,
          '--calendar-event-min-height': `${(WEEK_EVENT_MIN_VISUAL_MINUTES / 60) * HOUR_HEIGHT}px`,
          '--calendar-timeline-height': `${(timelineMinutes / 60) * HOUR_HEIGHT}px`,
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
              <span>{weekday(date)}</span>
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
              {allDay.map((layout) => eventButton(layout, day, selectedSlotID, () => onSelectSlot(date, layout.projection), true))}
            </div>
          )
        })}

        <div className="calendar-v2-time-axis">
          {axisTimeline?.ticks.map((tick) => (
            <span key={`${tick.offsetMinutes}-${tick.label}`} style={{ top: (tick.offsetMinutes / 60) * HOUR_HEIGHT }}>
              {tick.label}
            </span>
          ))}
        </div>
        {dates.map((date) => {
          const day = byDate.get(date)
          const timed = visibleLayouts(day, filters, showCancelled).filter((layout) => !layout.projection.allDay)
          return (
            <div
              className="calendar-v2-day-track"
              key={date}
              data-timeline-minutes={day?.timeline.durationMinutes}
              onDoubleClick={() => onSelectDate(date)}
            >
              {day?.timeline.ticks.map((tick) => (
                <i
                  className="calendar-v2-hour-line"
                  key={`${tick.offsetMinutes}-${tick.label}`}
                  style={{ top: (tick.offsetMinutes / 60) * HOUR_HEIGHT }}
                />
              ))}
              {day && day.timeline.durationMinutes !== 24 * 60 && day.timeline.ticks.map((tick) => (
                <em
                  className="calendar-v2-transition-tick"
                  key={`transition-${tick.offsetMinutes}-${tick.label}`}
                  style={{ top: (tick.offsetMinutes / 60) * HOUR_HEIGHT }}
                >
                  {transitionTickLabel(tick.label, tick.utcOffset)}
                </em>
              ))}
              {day?.workingWindow && (
                <span
                  className="calendar-v2-working-window"
                  title={`${day.workingWindow.start}–${day.workingWindow.end} 可约`}
                  style={timeBlockStyle(day.timeline, day.workingWindow.startAt, day.workingWindow.endAt)}
                />
              )}
              {timed.map((layout) => eventButton(
                layout,
                day,
                selectedSlotID,
                () => onSelectSlot(date, layout.projection),
                false,
              ))}
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
) {
  const projection = layout.projection
  const style = allDay || !day ? undefined : {
    ...timeBlockStyle(day.timeline, projection.startAt, projection.endAt),
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

function timeBlockStyle(timeline: CalendarTimeline, startAt: string, endAt: string): CSSProperties {
  const block = calendarTimelineBlock(timeline, startAt, endAt)
  return {
    top: (block.topMinutes / 60) * HOUR_HEIGHT,
    height: (block.durationMinutes / 60) * HOUR_HEIGHT,
  }
}

function weekday(date: string): string {
  const names = ['周一', '周二', '周三', '周四', '周五', '周六', '周日']
  const value = new Date(`${date}T12:00:00Z`).getUTCDay()
  return names[(value + 6) % 7] ?? ''
}
