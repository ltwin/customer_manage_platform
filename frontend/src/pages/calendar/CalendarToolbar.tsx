import { CalendarClock, ChevronLeft, ChevronRight, Plus } from 'lucide-react'

import type { MonthOverview } from './model'
import type { CalendarFilters, CalendarSlotType, CalendarView } from './types'
import { formatCalendarMonth } from './format'

const filterLabels: Array<{ type: CalendarSlotType; label: string }> = [
  { type: 'shoot', label: '拍摄' },
  { type: 'hold', label: '预留' },
  { type: 'busy', label: '个人占用' },
]

export default function CalendarToolbar({
  view,
  month,
  filters,
  showCancelled,
  overview,
  availabilityReady,
  writesEnabled,
  onViewChange,
  onNavigate,
  onToday,
  onJumpMonth,
  onFilterChange,
  onShowCancelledChange,
  onOpenings,
  onCreate,
}: {
  view: CalendarView
  month: string
  filters: CalendarFilters
  showCancelled: boolean
  overview: MonthOverview | null
  availabilityReady: boolean
  writesEnabled: boolean
  onViewChange(view: CalendarView): void
  onNavigate(delta: number): void
  onToday(): void
  onJumpMonth(month: string): void
  onFilterChange(type: CalendarSlotType, checked: boolean): void
  onShowCancelledChange(checked: boolean): void
  onOpenings(): void
  onCreate(): void
}) {
  return (
    <div className="calendar-v2-toolbar">
      <div className="calendar-v2-toolbar-main">
        <div className="calendar-v2-period-nav" aria-label={view === 'week' ? '周导航' : '月导航'}>
          <button className="icon-btn" type="button" onClick={() => onNavigate(-1)} aria-label={view === 'week' ? '上一周' : '上一月'}>
            <ChevronLeft aria-hidden="true" strokeWidth={2} />
          </button>
          <strong>{month ? formatCalendarMonth(month) : '—'}</strong>
          <button className="icon-btn" type="button" onClick={() => onNavigate(1)} aria-label={view === 'week' ? '下一周' : '下一月'}>
            <ChevronRight aria-hidden="true" strokeWidth={2} />
          </button>
          <button className="btn btn-sm" type="button" onClick={onToday}>今天</button>
          <label className="calendar-v2-month-jump">
            <span className="sr-only">跳转年月</span>
            <input type="month" value={month} onChange={(event) => onJumpMonth(event.target.value)} />
          </label>
        </div>

        <div className="calendar-v2-toolbar-actions">
          <div className="calendar-v2-view-switch" role="group" aria-label="日历视图">
            <button type="button" className={view === 'week' ? 'active' : ''} aria-pressed={view === 'week'} onClick={() => onViewChange('week')}>周</button>
            <button type="button" className={view === 'month' ? 'active' : ''} aria-pressed={view === 'month'} onClick={() => onViewChange('month')}>月</button>
          </div>
          <button className="btn btn-sm" type="button" disabled={!availabilityReady} onClick={onOpenings}>
            <CalendarClock aria-hidden="true" strokeWidth={2} />
            未来空档
          </button>
          <button className="btn btn-primary btn-sm" type="button" disabled={!writesEnabled} onClick={onCreate}>
            <Plus aria-hidden="true" strokeWidth={2.2} />
            新建档期
          </button>
        </div>
      </div>

      <div className="calendar-v2-toolbar-secondary">
        <fieldset className="calendar-v2-filters">
          <legend className="sr-only">筛选档期类型</legend>
          {filterLabels.map(({ type, label }) => (
            <label key={type}>
              <input type="checkbox" checked={filters[type]} onChange={(event) => onFilterChange(type, event.target.checked)} />
              <span className={`calendar-v2-filter-dot slot-${type}`} aria-hidden="true" />
              {label}
            </label>
          ))}
          <label>
            <input type="checkbox" checked={showCancelled} onChange={(event) => onShowCancelledChange(event.target.checked)} />
            显示已取消
          </label>
        </fieldset>

        <div className="calendar-v2-overview" aria-label="本月概览">
          <span><b>{overview?.shootCount ?? '—'}</b> 拍摄</span>
          <span><b>{overview?.holdDays ?? '—'}</b> 预留日</span>
          <span><b>{overview?.conflictDays ?? '—'}</b> 冲突日</span>
          <span><b>{overview?.openDays ?? '—'}</b> 可约日</span>
          <span><b>{overview?.utilization === null || overview === null ? '—' : `${overview.utilization}%`}</b> 利用率</span>
        </div>
      </div>
    </div>
  )
}
