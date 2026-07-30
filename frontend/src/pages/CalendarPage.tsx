import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { CalendarRange, ChevronLeft, ChevronRight, Plus, X } from 'lucide-react'

import {
  ApiError,
  deleteScheduleSlot,
  listScheduleSlots,
} from '../api/client'
import type { ScheduleSlotListItem } from '../api/client'
import ScheduleSlotDialog from '../components/schedule/ScheduleSlotDialog'
import { buildCalendarDays } from '../components/schedule/calendarModel'
import { scheduleDrawerShouldOpen } from '../components/schedule/flow'
import { readPendingSchedule } from '../components/schedule/journal'
import {
  accountToday,
  instantToLocalDateTime,
  isValidDate,
  localDayRange,
  monthGrid,
} from '../components/schedule/timezone'
import { useShell } from '../components/shellContext'
import { useFocusTrap } from '../components/useFocusTrap'
import StateNotice from '../components/StateNotice'
import EmptyState from '../components/EmptyState'
import {
  beginPageRead,
  completePageRead,
  failPageRead,
  pageReadPresentation,
  readyPageData,
  terminalPageReadError,
  type PageReadState,
} from '../components/pageReadState'

const weekdayLabels = ['周一', '周二', '周三', '周四', '周五', '周六', '周日']
const emptyScheduleSlots: ScheduleSlotListItem[] = []

export default function CalendarPage() {
  const navigate = useNavigate()
  const { notify, timezone, timezoneError, timezoneLoading } = useShell()
  const [searchParams, setSearchParams] = useSearchParams()
  const today = timezone ? accountToday(timezone) : ''
  const queryDate = searchParams.get('date') ?? ''
  const querySlot = searchParams.get('slot') ?? ''
  const queryScheduleDraft = searchParams.get('schedule_draft') ?? ''
  const [month, setMonth] = useState(() => isValidDate(queryDate) ? queryDate.slice(0, 7) : today.slice(0, 7))
  const [selectedDate, setSelectedDate] = useState(() => isValidDate(queryDate) ? queryDate : today)
  const [readState, setReadState] = useState<PageReadState<ScheduleSlotListItem[]>>({
    kind: 'loading',
    message: '正在加载档期',
  })
  const [readReloadTick, setReadReloadTick] = useState(0)
  const loadedRangeRef = useRef('')
  const [queryError, setQueryError] = useState<string | null>(null)
  const [drawerOpen, setDrawerOpen] = useState(Boolean(queryDate || querySlot))
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingSlot, setEditingSlot] = useState<ScheduleSlotListItem | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<ScheduleSlotListItem | null>(null)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const [lastDeletedShoot, setLastDeletedShoot] = useState<ScheduleSlotListItem | null>(null)
  const scheduleReturnFocusRef = useRef<HTMLElement | null>(null)

  const gridDates = useMemo(() => month ? monthGrid(month) : [], [month])
  const range = useMemo(() => {
    if (!timezone || gridDates.length !== 42) return null
    return {
      from: localDayRange(gridDates[0] ?? '', timezone).start,
      to: localDayRange(gridDates[41] ?? '', timezone).end,
    }
  }, [gridDates, timezone])
  const slots = readyPageData(readState) ?? emptyScheduleSlots
  const readPresentation = pageReadPresentation(readState)
  const loading = readState.kind === 'loading'
  const loadError = readPresentation.notice?.kind === 'error' || readPresentation.notice?.kind === 'refresh-error'
    ? readPresentation.notice.message
    : null

  const loadSlots = useCallback(async () => {
    if (!range || !timezone) return
    const rangeKey = `${range.from}:${range.to}`
    const preserveReady = loadedRangeRef.current === rangeKey
    loadedRangeRef.current = rangeKey
    setReadState((current) => beginPageRead(current, '正在加载档期', preserveReady))
    try {
      const result = await listScheduleSlots(range.from, range.to)
      setReadState(completePageRead(result, false, ''))
    } catch (reason) {
      if (reason instanceof ApiError && reason.status === 401) {
        setReadState({ kind: 'unauthorized' })
        navigate('/login', { replace: true })
        return
      }
      setReadState((current) => failPageRead(
        current,
        errorMessage(reason, '档期加载失败'),
        () => setReadReloadTick((value) => value + 1),
      ))
    }
  }, [navigate, range, timezone])

  useEffect(() => { void loadSlots() }, [loadSlots, readReloadTick])

  useEffect(() => {
    if (timezoneLoading || timezone) return
    setReadState(terminalPageReadError(
      timezoneError ? '账号时区加载失败，修复后才能读取档期' : '账号时区不可用，暂不能读取档期',
    ))
  }, [timezone, timezoneError, timezoneLoading])

  useEffect(() => {
    if (!today || month) return
    setMonth(today.slice(0, 7))
    setSelectedDate(today)
  }, [month, today])

  useEffect(() => {
    const onFocus = () => { void loadSlots() }
    window.addEventListener('focus', onFocus)
    return () => window.removeEventListener('focus', onFocus)
  }, [loadSlots])

  useEffect(() => {
    if (!timezone) return
    if (queryDate) {
      try {
        if (!isValidDate(queryDate)) throw new Error('invalid query date')
        localDayRange(queryDate, timezone)
        setMonth(queryDate.slice(0, 7))
        setSelectedDate(queryDate)
        setDrawerOpen(true)
      } catch {
        setQueryError('链接中的日期无效，已返回当前月份')
        setMonth(today.slice(0, 7))
        setSelectedDate(today)
      }
    }
  }, [queryDate, timezone, today])

  useEffect(() => {
    if (!querySlot || !timezone || loading || loadError) return
    const target = slots.find((slot) => slot.id === querySlot)
    if (!target) {
      setQueryError('链接指向的档期已不存在')
      return
    }
    const local = instantToLocalDateTime(target.start_at, timezone)
    setQueryError(null)
    setMonth(local.date.slice(0, 7))
    setSelectedDate(local.date)
    setDrawerOpen(true)
    const frame = window.requestAnimationFrame(() => {
      const element = document.querySelector<HTMLElement>(`[data-slot-id="${CSS.escape(querySlot)}"]`)
      element?.scrollIntoView({ block: 'center' })
      element?.focus({ preventScroll: true })
    })
    return () => window.cancelAnimationFrame(frame)
  }, [loadError, loading, querySlot, slots, timezone])

  useEffect(() => {
    try {
      if (readPendingSchedule() || queryScheduleDraft) setDialogOpen(true)
    } catch (reason) {
      setQueryError(errorMessage(reason, '恢复记录读取失败'))
    }
  }, [queryScheduleDraft])

  useEffect(() => {
    if (dialogOpen) setDrawerOpen(false)
  }, [dialogOpen])

  const days = useMemo(
    () => timezone ? buildCalendarDays(slots, gridDates, timezone) : [],
    [gridDates, slots, timezone],
  )
  const selectedDay = days.find((day) => day.date === selectedDate)
  const drawerVisible = scheduleDrawerShouldOpen(drawerOpen, dialogOpen)
  const drawerRef = useFocusTrap<HTMLElement>(drawerVisible, () => setDrawerOpen(false))
  const deleteDialogRef = useFocusTrap<HTMLElement>(Boolean(deleteTarget), () => setDeleteTarget(null))

  function openDay(date: string) {
    setSelectedDate(date)
    setDrawerOpen(true)
    setSearchParams({ date })
  }

  function openCreate(date = selectedDate || today) {
    rememberScheduleReturnFocus()
    setSelectedDate(date)
    setEditingSlot(null)
    setDialogOpen(true)
  }

  function openEdit(slot: ScheduleSlotListItem) {
    rememberScheduleReturnFocus()
    setEditingSlot(slot)
    setDialogOpen(true)
  }

  function rememberScheduleReturnFocus() {
    const active = document.activeElement instanceof HTMLElement ? document.activeElement : null
    scheduleReturnFocusRef.current = drawerVisible || active?.closest('.drawer')
      ? document.querySelector<HTMLElement>('.cal-cell.selected')
      : active
  }

  function closeScheduleDialog() {
    setDialogOpen(false)
    window.requestAnimationFrame(() => {
      window.requestAnimationFrame(() => {
        const target = scheduleReturnFocusRef.current
        if (target?.isConnected) target.focus({ preventScroll: true })
      })
    })
  }

  function navigateMonth(delta: number) {
    setMonth((current) => shiftMonth(current, delta))
    setDrawerOpen(false)
    setQueryError(null)
  }

  function goToday() {
    if (!today) return
    setMonth(today.slice(0, 7))
    openDay(today)
  }

  async function confirmDelete() {
    if (!deleteTarget?.id) return
    setDeleteError(null)
    try {
      await deleteScheduleSlot(deleteTarget.id)
      if (deleteTarget.type === 'shoot') setLastDeletedShoot(deleteTarget)
      setDeleteTarget(null)
      notify('档期已删除，订单和订单状态保持不变')
      await loadSlots()
    } catch (reason) {
      setDeleteError(errorMessage(reason, '档期删除失败'))
    }
  }

  return (
    <>
      <header className="topbar">
        <div>
          <h1>档期</h1>
          <div className="sub">时间按 {timezone ?? '账号时区不可用'}</div>
        </div>
        <div className="topbar-actions desktop-schedule-actions">
          <button className="btn btn-primary" type="button" disabled={!timezone} onClick={() => openCreate()}>
            <Plus aria-hidden="true" strokeWidth={2.2} />
            新建档期
          </button>
        </div>
      </header>

      <main className="content calendar-content">
        {queryError && <div className="form-error">{queryError}</div>}
        <div className="cal-head">
          <button className="icon-btn" type="button" onClick={() => navigateMonth(-1)} aria-label="上一月"><ChevronLeft aria-hidden="true" strokeWidth={2} /></button>
          <h2>{formatMonth(month)}</h2>
          <button className="icon-btn" type="button" onClick={() => navigateMonth(1)} aria-label="下一月"><ChevronRight aria-hidden="true" strokeWidth={2} /></button>
          <button className="btn btn-sm" type="button" disabled={!today} onClick={goToday}>今天</button>
          <div className="cal-legend" aria-label="档期类型图例">
            <span><span className="dot slot-shoot" />拍摄</span>
            <span><span className="dot slot-hold" />预留</span>
            <span><span className="dot slot-busy" />个人占用</span>
          </div>
        </div>

        {readPresentation.notice && <StateNotice {...readPresentation.notice} />}

        {(loading || readPresentation.showReadyData) && <div className={`cal-grid${loading ? ' is-loading' : ''}`} aria-busy={loading}>
          {weekdayLabels.map((label) => <div className="cal-dow" key={label}>{label}</div>)}
          {gridDates.map((date, index) => {
            const day = days[index]
            const visible = day?.slots.slice(0, 3) ?? []
            const extra = Math.max(0, (day?.slots.length ?? 0) - visible.length)
            const other = date.slice(0, 7) !== month
            return (
              <button
                type="button"
                className={`cal-cell${other ? ' other' : ''}${date === today ? ' today' : ''}${date === selectedDate ? ' selected' : ''}`}
                key={date}
                onClick={() => openDay(date)}
                aria-label={`${date}，${day?.slots.length ?? 0} 条档期，${day?.conflictCount ?? 0} 条冲突档期`}
              >
                <span className="cal-date">{Number(date.slice(8, 10))}</span>
                <span className="cal-events">
                  {loading ? (
                    <><span className="cal-event-skeleton" /><span className="cal-event-skeleton short" /></>
                  ) : visible.map((entry) => (
                    <span
                      className={`cal-event ${entry.slot.type}${entry.conflicting ? ' overlap' : ''}`}
                      key={entry.slot.id}
                      title={slotSummary(entry.slot)}
                    >
                      {entry.allDay ? '全天' : entry.displayStart} {slotSummary(entry.slot)}
                    </span>
                  ))}
                  {!loading && extra > 0 && <span className="cal-more">还有 {extra} 条</span>}
                </span>
                <span className="cal-count" aria-hidden="true">
                  {day?.slots.map((entry) => <i className={`slot-${entry.slot.type}`} key={entry.slot.id} />)}
                </span>
              </button>
            )
          })}
        </div>}
      </main>

      <div className={`drawer-overlay${drawerVisible ? ' open' : ''}`} onClick={() => setDrawerOpen(false)} />
      <aside
        ref={drawerRef}
        className={`drawer${drawerVisible ? ' open' : ''}`}
        role="dialog"
        aria-modal="true"
        aria-labelledby="scheduleDrawerTitle"
        aria-hidden={!drawerVisible}
        tabIndex={-1}
      >
        <div className="drawer-head">
          <h2 id="scheduleDrawerTitle">{formatDay(selectedDate)}</h2>
          <button className="icon-btn" type="button" onClick={() => setDrawerOpen(false)} aria-label="关闭"><X aria-hidden="true" strokeWidth={2} /></button>
        </div>
        <div className="drawer-body">
          {lastDeletedShoot?.type === 'shoot' && (
            <div className="schedule-preview-clear">
              档期已删除，订单仍保留。
              <Link to={`/customers/${lastDeletedShoot.customer_id}?tab=orders&order=${lastDeletedShoot.order_id}`}>查看订单</Link>
            </div>
          )}
          {!selectedDay || selectedDay.slots.length === 0 ? (
            <EmptyState icon={CalendarRange} title="这天暂无档期" hint="点下方按钮给这天加一场拍摄" inline />
          ) : selectedDay.slots.map((entry) => (
            <div className="slot-entry" key={entry.slot.id} data-slot-id={entry.slot.id} tabIndex={-1}>
              <div className="head">
                <span className={`dot slot-${entry.slot.type}`} />
                <span className="badge badge-muted">{slotTypeLabel(entry.slot.type)}</span>
                <span className="time">{entry.allDay ? '全天' : `${entry.displayStart} - ${entry.displayEnd}`}</span>
              </div>
              <div className="body">{slotSummary(entry.slot)}</div>
              {entry.slot.type === 'shoot' && (
                <div className="meta">
                  {entry.slot.package_name ?? '未选套系'} · {orderStatusLabel(entry.slot.order_status)}
                  {entry.slot.customer_status === 'archived' && <span className="warning-text"> · 客户已归档</span>}
                  {entry.slot.order_status === 'cancelled' && <span className="danger-text"> · 订单已取消，档期仍保留</span>}
                </div>
              )}
              {entry.slot.note && <div className="meta">{entry.slot.note}</div>}
              {entry.conflicting && <div className="conflict-tip">与当天其他档期存在时间重叠</div>}
              <div className="slot-actions desktop-schedule-actions">
                {entry.slot.type === 'shoot' && (
                  <Link className="btn btn-sm" to={`/customers/${entry.slot.customer_id}?tab=orders&order=${entry.slot.order_id}`}>查看订单</Link>
                )}
                <button className="btn btn-sm" type="button" onClick={() => openEdit(entry.slot)}>编辑</button>
                <button className="btn btn-sm btn-danger-ghost" type="button" onClick={() => { setDeleteError(null); setDeleteTarget(entry.slot) }}>删除</button>
              </div>
            </div>
          ))}
        </div>
        <div className="drawer-foot">
          <button className="btn" type="button" onClick={() => setDrawerOpen(false)}>关闭</button>
          <button className="btn btn-primary desktop-schedule-actions" type="button" disabled={!timezone} onClick={() => openCreate(selectedDate)}>在这天加档期</button>
        </div>
      </aside>

      <ScheduleSlotDialog
        open={dialogOpen}
        timezone={timezone}
        initialDate={selectedDate || today}
        slot={editingSlot}
        scheduleDraftId={queryScheduleDraft || undefined}
        onClose={closeScheduleDialog}
        onChanged={loadSlots}
        onCompleted={(date) => {
          setSelectedDate(date)
          setDrawerOpen(true)
          setSearchParams({ date })
        }}
      />

      {deleteTarget && (
        <div className="overlay open" onClick={(event) => { if (event.target === event.currentTarget) setDeleteTarget(null) }}>
          <section ref={deleteDialogRef} className="dialog" role="dialog" aria-modal="true" aria-labelledby="deleteScheduleTitle" tabIndex={-1}>
            <h2 id="deleteScheduleTitle">删除档期</h2>
            <p className="dialog-sub">只删除日历档期，订单和订单状态都会保留。</p>
            {deleteError && <div className="form-error">{deleteError}</div>}
            <div className="dialog-actions">
              <button className="btn" type="button" onClick={() => setDeleteTarget(null)}>取消</button>
              <button className="btn btn-danger" type="button" onClick={() => { void confirmDelete() }}>确认删除</button>
            </div>
          </section>
        </div>
      )}
    </>
  )
}

function slotSummary(slot: ScheduleSlotListItem): string {
  if (slot.type !== 'shoot') return slot.note || slotTypeLabel(slot.type)
  return `${slot.customer_display_name} · ${slot.order_title ?? slot.package_name ?? '未命名订单'}`
}

function slotTypeLabel(type: string): string {
  return { shoot: '拍摄', hold: '预留', busy: '个人占用' }[type] ?? type
}

function orderStatusLabel(status: string): string {
  return {
    consulting: '咨询', scheduled: '定档', shot: '已拍摄', selected: '已选片',
    retouching: '精修中', delivered: '已交付', closed: '完结', cancelled: '取消',
  }[status] ?? status
}

function shiftMonth(value: string, delta: number): string {
  const [year, month] = value.split('-').map(Number)
  const date = new Date(Date.UTC(year ?? 1970, (month ?? 1) - 1 + delta, 1))
  return `${date.getUTCFullYear()}-${String(date.getUTCMonth() + 1).padStart(2, '0')}`
}

function formatMonth(value: string): string {
  const [year, month] = value.split('-')
  return `${year} 年 ${Number(month)} 月`
}

function formatDay(value: string): string {
  if (!value) return '当天档期'
  return `${Number(value.slice(5, 7))} 月 ${Number(value.slice(8, 10))} 日`
}

function errorMessage(reason: unknown, fallback: string): string {
  if (reason instanceof ApiError || reason instanceof Error) return reason.message
  return fallback
}
