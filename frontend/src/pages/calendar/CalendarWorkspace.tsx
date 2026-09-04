import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Plus } from 'lucide-react'

import type { ScheduleSlotListItem } from '../../api/client'
import CalendarToolbar from './CalendarToolbar'
import DayDetailPanel from './DayDetailPanel'
import MonthCalendar from './MonthCalendar'
import OpeningsDialog from './OpeningsDialog'
import WeekCalendar from './WeekCalendar'
import { calendarDetailShouldOpen, useCalendarWorkspaceMode } from './layoutMode'
import type {
  CalendarDayModel,
  CalendarProjection,
  MonthOverview,
  Opening,
} from './model'
import {
  defaultCalendarFilters,
  type CalendarFilters,
  type CalendarSlotType,
  type CalendarView,
} from './types'
import type { WeekDragRange } from './weekCollapse'
import './calendar.css'

export interface CalendarWorkspaceProps {
  dates: string[]
  weekDates: string[]
  days: CalendarDayModel[]
  month: string
  today: string
  selectedDate: string
  selectedSlotID: string
  detailOpen: boolean
  detailSuppressed?: boolean
  loading: boolean
  writesEnabled: boolean
  availabilityReady: boolean
  overview: MonthOverview | null
  notice?: ReactNode
  openings: Opening[]
  openingsLoading: boolean
  openingsError: string | null
  lastDeletedShoot: { slot: ScheduleSlotListItem; date: string } | null
  timezone: string | null
  onNavigate(delta: number, view: CalendarView): void
  onToday(): void
  onJumpMonth(month: string): void
  onSelectDate(date: string): void
  onSelectSlot(date: string, projection: CalendarProjection): void
  onDetailOpen(): void
  onDetailClose(): void
  onCreate(date: string): void
  onCreateTimeRange(range: WeekDragRange): void
  onEdit(slot: ScheduleSlotListItem): void
  onDelete(slot: ScheduleSlotListItem): void
  onOpeningsOpen(): void
  onOpeningsClose(): void
  onOpeningsRetry(): void
  onCopyOpenings(text: string): Promise<void> | void
  onViewChange?(view: CalendarView): void
}

export default function CalendarWorkspace({
  dates,
  weekDates,
  days,
  month,
  today,
  selectedDate,
  selectedSlotID,
  detailOpen,
  detailSuppressed = false,
  loading,
  writesEnabled,
  availabilityReady,
  overview,
  notice,
  openings,
  openingsLoading,
  openingsError,
  lastDeletedShoot,
  timezone,
  onNavigate,
  onToday,
  onJumpMonth,
  onSelectDate,
  onSelectSlot,
  onDetailOpen,
  onDetailClose,
  onCreate,
  onCreateTimeRange,
  onEdit,
  onDelete,
  onOpeningsOpen,
  onOpeningsClose,
  onOpeningsRetry,
  onCopyOpenings,
  onViewChange,
}: CalendarWorkspaceProps) {
  const { containerRef, mode } = useCalendarWorkspaceMode()
  const [view, setView] = useState<CalendarView>('week')
  const [filters, setFilters] = useState<CalendarFilters>(defaultCalendarFilters)
  const [showCancelled, setShowCancelled] = useState(false)
  const [openingsOpen, setOpeningsOpen] = useState(false)
  const [expandedBands, setExpandedBands] = useState<Set<string>>(new Set())
  const detailReturnFocusRef = useRef<HTMLElement | null>(null)
  const selectedDay = days.find((day) => day.date === selectedDate)

  function toggleBand(bandKey: string) {
    setExpandedBands((current) => {
      const next = new Set(current)
      if (next.has(bandKey)) next.delete(bandKey)
      else next.add(bandKey)
      return next
    })
  }

  useEffect(() => {
    if (availabilityReady || !openingsOpen) return
    setOpeningsOpen(false)
    onOpeningsClose()
  }, [availabilityReady, onOpeningsClose, openingsOpen])

  function changeView(nextView: CalendarView) {
    setView(nextView)
    onViewChange?.(nextView)
  }

  function changeFilter(type: CalendarSlotType, checked: boolean) {
    setFilters((current) => ({ ...current, [type]: checked }))
  }

  function selectDate(date: string) {
    rememberDetailReturnFocus()
    onSelectDate(date)
    onDetailOpen()
  }

  function selectSlot(date: string, projection: CalendarProjection) {
    rememberDetailReturnFocus()
    onSelectSlot(date, projection)
    onDetailOpen()
  }

  function closeDetail() {
    onDetailClose()
    window.requestAnimationFrame(() => {
      window.requestAnimationFrame(() => {
        const target = detailReturnFocusRef.current ?? selectedDateFocusTarget(selectedDate)
        if (target?.isConnected && !target.closest('[aria-hidden="true"]')) {
          target.focus({ preventScroll: true })
        }
      })
    })
  }

  function rememberDetailReturnFocus() {
    const active = document.activeElement instanceof HTMLElement ? document.activeElement : null
    detailReturnFocusRef.current = active?.closest('.calendar-v2-detail') ? null : active
  }

  function openOpenings() {
    setOpeningsOpen(true)
    onOpeningsOpen()
  }

  function closeOpenings() {
    setOpeningsOpen(false)
    onOpeningsClose()
  }

  const createDate = selectedDate || weekDates[0] || dates[0] || today
  const panelOpen = calendarDetailShouldOpen(mode, detailOpen, detailSuppressed)

  return (
    <section
      ref={containerRef}
      className="calendar-v2-workspace"
      data-layout-mode={mode}
      data-calendar-view={view}
    >
      <CalendarToolbar
        view={view}
        month={month}
        filters={filters}
        showCancelled={showCancelled}
        overview={overview}
        availabilityReady={availabilityReady}
        writesEnabled={writesEnabled}
        onViewChange={changeView}
        onNavigate={(delta) => onNavigate(delta, view)}
        onToday={onToday}
        onJumpMonth={onJumpMonth}
        onFilterChange={changeFilter}
        onShowCancelledChange={setShowCancelled}
        onOpenings={openOpenings}
        onCreate={() => onCreate(createDate)}
      />

      {notice && <div className="calendar-v2-notice">{notice}</div>}

      <div className="calendar-v2-layout">
        <div className="calendar-v2-stage">
          {view === 'month' ? (
            <MonthCalendar
              dates={dates}
              days={days}
              month={month}
              today={today}
              selectedDate={selectedDate}
              selectedSlotID={selectedSlotID}
              filters={filters}
              showCancelled={showCancelled}
              loading={loading}
              onSelectDate={selectDate}
              onSelectSlot={selectSlot}
            />
          ) : (
            <WeekCalendar
              dates={weekDates}
              days={days}
              today={today}
              selectedDate={selectedDate}
              selectedSlotID={selectedSlotID}
              filters={filters}
              showCancelled={showCancelled}
              loading={loading}
              timezone={timezone}
              writesEnabled={writesEnabled}
              expandedBands={expandedBands}
              onToggleBand={toggleBand}
              onCreateRange={onCreateTimeRange}
              onSelectDate={selectDate}
              onSelectSlot={selectSlot}
            />
          )}
        </div>

        {mode === 'bottom-sheet' && panelOpen && (
          <button
            className="calendar-v2-detail-backdrop"
            type="button"
            aria-label="关闭当日详情"
            onClick={closeDetail}
          />
        )}
        <DayDetailPanel
          day={selectedDay}
          mode={mode}
          open={panelOpen}
          writesEnabled={writesEnabled}
          filters={filters}
          selectedSlotID={selectedSlotID}
          lastDeletedShoot={lastDeletedShoot}
          onClose={closeDetail}
          onCreate={() => onCreate(createDate)}
          onEdit={onEdit}
          onDelete={onDelete}
        />
      </div>

      <button
        className="calendar-v2-fab"
        type="button"
        disabled={!writesEnabled}
        aria-label={`在 ${createDate} 新建档期`}
        onClick={() => onCreate(createDate)}
      >
        <Plus aria-hidden="true" />
      </button>

      <OpeningsDialog
        open={openingsOpen}
        openings={openings}
        loading={openingsLoading}
        error={openingsError}
        onClose={closeOpenings}
        onRetry={onOpeningsRetry}
        onCopy={onCopyOpenings}
      />
    </section>
  )
}

function selectedDateFocusTarget(date: string): HTMLElement | null {
  if (!date) return null
  const escaped = CSS.escape(date)
  return document.querySelector<HTMLElement>(
    `.calendar-v2-week-heading[data-calendar-date="${escaped}"], .calendar-v2-date-button[data-calendar-date="${escaped}"]`,
  )
}
