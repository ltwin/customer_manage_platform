import { calendarProjectionTimeLabel, type CalendarDayModel, type CalendarProjection } from './model'
import type { CalendarFilters } from './types'
import SlotHoverCard from './SlotHoverCard'
import { useSlotHoverCard } from './useSlotHoverCard'
import { calendarSlotTitle } from './format'
import { calendarDateFocusTarget } from './keyboard'

const weekdayLabels = ['周一', '周二', '周三', '周四', '周五', '周六', '周日']

export default function MonthCalendar({
  dates,
  days,
  month,
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
  month: string
  today: string
  selectedDate: string
  selectedSlotID: string
  filters: CalendarFilters
  showCancelled: boolean
  loading: boolean
  onSelectDate(date: string): void
  onSelectSlot(date: string, projection: CalendarProjection): void
}) {
  const hover = useSlotHoverCard()
  const byDate = new Map(days.map((day) => [day.date, day]))
  const tabbableDate = dates.includes(selectedDate) ? selectedDate : dates[0]

  function moveDateFocus(event: React.KeyboardEvent<HTMLButtonElement>, date: string) {
    const targetDate = calendarDateFocusTarget(dates, date, event.key)
    if (!targetDate) return
    const target = event.currentTarget
      .closest('.calendar-v2-month')
      ?.querySelector<HTMLButtonElement>(`[data-calendar-date="${targetDate}"]`)
    if (!target) return
    event.preventDefault()
    target.focus()
  }

  return (
    <div className={`calendar-v2-month${loading ? ' is-loading' : ''}`} role="grid" aria-busy={loading} aria-label="月历">
      {weekdayLabels.map((label) => <div className="calendar-v2-month-dow" role="columnheader" key={label}>{label}</div>)}
      {dates.map((date) => {
        const day = byDate.get(date)
        const filtered = (day?.projections ?? []).filter((projection) =>
          filters[projection.slot.type] && (showCancelled || !projection.cancelled))
        const visible = filtered.slice(0, 3)
        const extra = Math.max(0, filtered.length - visible.length)
        return (
          <div
            className={[
              'calendar-v2-month-cell',
              date.slice(0, 7) !== month ? 'other' : '',
              date === today ? 'today' : '',
              date === selectedDate ? 'selected' : '',
            ].filter(Boolean).join(' ')}
            role="gridcell"
            aria-selected={date === selectedDate}
            key={date}
          >
            <button
              className="calendar-v2-date-button"
              type="button"
              onClick={() => onSelectDate(date)}
              onKeyDown={(event) => moveDateFocus(event, date)}
              data-calendar-date={date}
              tabIndex={date === tabbableDate ? 0 : -1}
              aria-label={`${date}，${filtered.length} 条可见档期，${day?.conflictCount ?? 0} 条冲突档期`}
            >
              <span>{Number(date.slice(8, 10))}</span>
              {day && day.openings.length > 0 && <i title="有可约空档">可约</i>}
            </button>
            <div className="calendar-v2-month-events">
              {loading ? (
                <><span className="calendar-v2-event-skeleton" /><span className="calendar-v2-event-skeleton short" /></>
              ) : visible.map((projection) => (
                <button
                  className={[
                    'calendar-v2-month-event',
                    projection.slot.type,
                    projection.conflicting ? 'conflicting' : '',
                    projection.cancelled ? 'cancelled' : '',
                    projection.slot.id === selectedSlotID ? 'selected' : '',
                  ].filter(Boolean).join(' ')}
                  type="button"
                  key={projection.slot.id}
                  onClick={() => {
                    hover.hide()
                    onSelectSlot(date, projection)
                  }}
                  {...hover.handlers(
                    projection,
                    day ? calendarProjectionTimeLabel(projection, day.timeline) : projection.displayStart,
                  )}
                >
                  {projection.allDay ? '全天' : projection.displayStart} {calendarSlotTitle(projection.slot)}
                </button>
              ))}
              {!loading && extra > 0 && <button className="calendar-v2-more" type="button" onClick={() => onSelectDate(date)}>还有 {extra} 条</button>}
            </div>
            {!showCancelled && (day?.cancelledCount ?? 0) > 0 && <span className="calendar-v2-cancelled-trace">{day?.cancelledCount} 条已取消</span>}
          </div>
        )
      })}
      <SlotHoverCard target={hover.target} />
    </div>
  )
}
