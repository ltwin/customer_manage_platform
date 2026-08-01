import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import {
  ApiError,
  deleteScheduleSlot,
  getSettings,
  getScheduleSlot,
  listScheduleSlots,
} from '../api/client'
import type {
  ScheduleSlotListItem,
  Settings,
} from '../api/client'
import ScheduleSlotDialog from '../components/schedule/ScheduleSlotDialog'
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
import {
  beginPageRead,
  completePageRead,
  failPageRead,
  pageReadPresentation,
  readyPageData,
  terminalPageReadError,
  type PageReadState,
} from '../components/pageReadState'
import CalendarWorkspace from './calendar/CalendarWorkspace'
import {
  buildCalendarModel,
  calculateMonthOverview,
  type CalendarProjection,
} from './calendar/model'
import {
  isCurrentCalendarRequest,
  nextCalendarRequest,
  type CalendarRequestVersion,
} from './calendar/requestState'
import type { CalendarView } from './calendar/types'
import { openingsForFirstDays } from './calendar/openings'
import { waitForCalendarSettingsIdle } from './calendar/deleteCoordination'
import {
  calendarDeleteCompletionAfterSettings,
  reconcileCalendarDeleteRefresh,
  type CalendarDeleteCompletion,
} from './calendar/urlState'

const emptyScheduleSlots: ScheduleSlotListItem[] = []

interface ActiveCalendarRequest extends CalendarRequestVersion {
  controller: AbortController | null
}

interface DeferredCalendarDeleteReconciliation {
  deletedSlotID: string
  deletedSlotStartAt: string
  expectedSearch: string
}

export default function CalendarPage() {
  const navigate = useNavigate()
  const { notify, timezone: shellTimezone, timezoneError, timezoneLoading } = useShell()
  const [searchParams, setSearchParams] = useSearchParams()
  const queryDate = searchParams.get('date') ?? ''
  const querySlot = searchParams.get('slot') ?? ''
  const queryScheduleDraft = searchParams.get('schedule_draft') ?? ''

  const [settings, setSettings] = useState<Settings | null>(null)
  const [settingsLoading, setSettingsLoading] = useState(true)
  const [settingsError, setSettingsError] = useState<string | null>(null)
  const settingsRequestRef = useRef<ActiveCalendarRequest>(emptyActiveRequest())
  const accountTimezone = settings?.timezone ?? shellTimezone
  const latestAccountTimezoneRef = useRef(accountTimezone)
  const today = accountTimezone ? accountToday(accountTimezone) : ''

  useLayoutEffect(() => {
    latestAccountTimezoneRef.current = accountTimezone
  }, [accountTimezone])

  const [month, setMonth] = useState(() => isValidDate(queryDate) ? queryDate.slice(0, 7) : '')
  const [selectedDate, setSelectedDate] = useState(() => isValidDate(queryDate) ? queryDate : '')
  const [detailOpen, setDetailOpen] = useState(Boolean(queryDate || querySlot))
  const [readState, setReadState] = useState<PageReadState<ScheduleSlotListItem[]>>({
    kind: 'loading',
    message: '正在加载档期',
  })
  const [readReloadTick, setReadReloadTick] = useState(0)
  const loadedRangeRef = useRef('')
  const mainRequestRef = useRef<ActiveCalendarRequest>(emptyActiveRequest())
  const slotLookupRequestRef = useRef<ActiveCalendarRequest>(emptyActiveRequest())
  const reloadAfterSettingsRef = useRef(false)
  const settingsIdleWaitersRef = useRef(new Set<() => void>())
  const deleteCoordinationControllerRef = useRef<AbortController | null>(null)
  const deferredDeleteReconciliationRef = useRef<DeferredCalendarDeleteReconciliation | null>(null)
  const applyDeferredDeleteReconciliationRef = useRef<(timezone: string) => void>(() => {})
  const [queryError, setQueryError] = useState<string | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingSlot, setEditingSlot] = useState<ScheduleSlotListItem | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<ScheduleSlotListItem | null>(null)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)
  const deleteInFlightRef = useRef(false)
  const deletedSlotIDRef = useRef('')
  const [lastDeletedShoot, setLastDeletedShoot] = useState<{
    slot: ScheduleSlotListItem
    date: string
  } | null>(null)
  const scheduleReturnFocusRef = useRef<HTMLElement | null>(null)

  const [openingsActive, setOpeningsActive] = useState(false)
  const [openingsSlots, setOpeningsSlots] = useState<ScheduleSlotListItem[]>([])
  const [openingsLoading, setOpeningsLoading] = useState(false)
  const [openingsError, setOpeningsError] = useState<string | null>(null)
  const openingsRequestRef = useRef<ActiveCalendarRequest>(emptyActiveRequest())

  const focusCalendarDate = useCallback((date: string) => {
    if (!date) return
    window.requestAnimationFrame(() => {
      window.requestAnimationFrame(() => {
        const target = document.querySelector<HTMLElement>(
          `.calendar-v2-week-heading[data-calendar-date="${CSS.escape(date)}"], .calendar-v2-date-button[data-calendar-date="${CSS.escape(date)}"]`,
        )
        if (target?.isConnected) target.focus({ preventScroll: true })
      })
    })
  }, [])

  const applyCalendarDeleteCompletion = useCallback((completion: CalendarDeleteCompletion) => {
    setSelectedDate(completion.date)
    setMonth(completion.date.slice(0, 7))
    const currentSlotLookup = slotLookupRequestRef.current
    currentSlotLookup.controller?.abort()
    slotLookupRequestRef.current = {
      ...nextCalendarRequest(currentSlotLookup, ''),
      controller: null,
    }
    setQueryError(null)
    setSearchParams(completion.searchParams, { replace: true })
    focusCalendarDate(completion.date)
  }, [focusCalendarDate, setSearchParams])

  const applyDeferredDeleteReconciliation = useCallback((timezone: string) => {
    const pending = deferredDeleteReconciliationRef.current
    if (!pending || window.location.pathname !== '/calendar') return
    const completion = calendarDeleteCompletionAfterSettings(
      new URLSearchParams(window.location.search),
      pending.expectedSearch,
      pending.deletedSlotID,
      pending.deletedSlotStartAt,
      timezone,
    )
    deferredDeleteReconciliationRef.current = null
    if (completion) applyCalendarDeleteCompletion(completion)
  }, [applyCalendarDeleteCompletion])

  useLayoutEffect(() => {
    applyDeferredDeleteReconciliationRef.current = applyDeferredDeleteReconciliation
  }, [applyDeferredDeleteReconciliation])

  const requestCalendarReload = useCallback(() => {
    if (settingsRequestRef.current.controller) {
      reloadAfterSettingsRef.current = true
      return
    }
    setReadReloadTick((value) => value + 1)
  }, [])

  const waitForSettingsIdle = useCallback(() => {
    return waitForCalendarSettingsIdle({
      isIdle: () => settingsRequestRef.current.controller === null,
      subscribe: (listener) => {
        settingsIdleWaitersRef.current.add(listener)
        return () => settingsIdleWaitersRef.current.delete(listener)
      },
      signal: deleteCoordinationControllerRef.current?.signal,
    })
  }, [])

  const gridDates = useMemo(() => month ? monthGrid(month) : [], [month])
  const weekDates = useMemo(
    () => isValidDate(selectedDate) ? weekDatesFor(selectedDate) : [],
    [selectedDate],
  )
  const range = useMemo(() => {
    if (!accountTimezone || gridDates.length !== 42) return null
    return {
      from: localDayRange(gridDates[0] ?? '', accountTimezone).start,
      to: localDayRange(gridDates[41] ?? '', accountTimezone).end,
    }
  }, [accountTimezone, gridDates])
  const futureDates = useMemo(
    () => isValidDate(today) ? datesAfter(today, 14) : [],
    [today],
  )
  const openingsRange = useMemo(() => {
    if (!accountTimezone || futureDates.length !== 14) return null
    return {
      from: localDayRange(futureDates[0] ?? '', accountTimezone).start,
      to: localDayRange(futureDates[13] ?? '', accountTimezone).end,
    }
  }, [accountTimezone, futureDates])

  const slots = readyPageData(readState) ?? emptyScheduleSlots
  const readPresentation = pageReadPresentation(readState)
  const loading = readState.kind === 'loading'

  const calendarModel = useMemo(
    () => accountTimezone
      ? buildCalendarModel(slots, gridDates, accountTimezone, settings?.availability ?? null)
      : { days: [], errors: [] },
    [accountTimezone, gridDates, settings, slots],
  )
  const overviewResult = useMemo(
    () => accountTimezone && settings && month
      ? calculateMonthOverview(slots, month, accountTimezone, settings.availability)
      : null,
    [accountTimezone, month, settings, slots],
  )
  const openingsModel = useMemo(
    () => accountTimezone && settings
      ? buildCalendarModel(openingsSlots, futureDates, accountTimezone, settings.availability)
      : { days: [], errors: [] },
    [accountTimezone, futureDates, openingsSlots, settings],
  )
  const openings = useMemo(
    () => openingsForFirstDays(openingsModel.days.flatMap((day) => day.openings), 8),
    [openingsModel.days],
  )

  const loadSettings = useCallback(async () => {
    const previous = settingsRequestRef.current
    previous.controller?.abort()
    const version = nextCalendarRequest(previous, 'settings')
    const controller = new AbortController()
    const request = { ...version, controller }
    settingsRequestRef.current = request
    latestAccountTimezoneRef.current = shellTimezone
    setSettings(null)
    setSettingsLoading(true)
    setSettingsError(null)
    try {
      const result = await getSettings(controller.signal)
      if (!isCurrentCalendarRequest(settingsRequestRef.current, request)) return
      latestAccountTimezoneRef.current = result.timezone
      setSettings(result)
      applyDeferredDeleteReconciliationRef.current(result.timezone)
    } catch (reason) {
      if (!isCurrentCalendarRequest(settingsRequestRef.current, request)) return
      deferredDeleteReconciliationRef.current = null
      if (reason instanceof ApiError && reason.status === 401) {
        navigate('/login', { replace: true })
        return
      }
      setSettings(null)
      setSettingsError(errorMessage(reason, '可约设置加载失败'))
    } finally {
      if (isCurrentCalendarRequest(settingsRequestRef.current, request)) {
        settingsRequestRef.current = { ...request, controller: null }
        setSettingsLoading(false)
        if (reloadAfterSettingsRef.current) {
          reloadAfterSettingsRef.current = false
          requestCalendarReload()
        }
        const waiters = [...settingsIdleWaitersRef.current]
        settingsIdleWaitersRef.current.clear()
        for (const notifyIdle of waiters) notifyIdle()
      }
    }
  }, [navigate, requestCalendarReload, shellTimezone])

  const loadSlots = useCallback(async () => {
    if (!range || !accountTimezone) {
      mainRequestRef.current.controller?.abort()
      return
    }
    const rangeKey = `${range.from}:${range.to}`
    const previous = mainRequestRef.current
    previous.controller?.abort()
    const version = nextCalendarRequest(previous, rangeKey)
    const controller = new AbortController()
    const request = { ...version, controller }
    mainRequestRef.current = request
    const preserveReady = loadedRangeRef.current === rangeKey
    loadedRangeRef.current = rangeKey
    setReadState((current) => beginPageRead(current, '正在加载档期', preserveReady))
    try {
      const result = await listScheduleSlots(range.from, range.to, controller.signal)
      if (!isCurrentCalendarRequest(mainRequestRef.current, request)) return
      setReadState(completePageRead(result, false, ''))
    } catch (reason) {
      if (!isCurrentCalendarRequest(mainRequestRef.current, request)) return
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
    } finally {
      if (isCurrentCalendarRequest(mainRequestRef.current, request)) {
        mainRequestRef.current = { ...request, controller: null }
      }
    }
  }, [accountTimezone, navigate, range])

  const loadOpenings = useCallback(async (forceNetwork = false) => {
    if (!openingsRange || !accountTimezone || !settings) return
    const rangeKey = `${openingsRange.from}:${openingsRange.to}`
    const previous = openingsRequestRef.current
    previous.controller?.abort()
    const version = nextCalendarRequest(previous, rangeKey)
    const controller = new AbortController()
    const request = { ...version, controller }
    openingsRequestRef.current = request
    setOpeningsLoading(true)
    setOpeningsError(null)
    try {
      const mainRangeCoversOpenings = range &&
        Date.parse(range.from) <= Date.parse(openingsRange.from) &&
        Date.parse(range.to) >= Date.parse(openingsRange.to) &&
        readState.kind === 'ready'
      const result = !forceNetwork && mainRangeCoversOpenings
        ? slots
        : await listScheduleSlots(openingsRange.from, openingsRange.to, controller.signal)
      if (!isCurrentCalendarRequest(openingsRequestRef.current, request)) return
      setOpeningsSlots(result)
    } catch (reason) {
      if (!isCurrentCalendarRequest(openingsRequestRef.current, request)) return
      if (reason instanceof ApiError && reason.status === 401) {
        navigate('/login', { replace: true })
        return
      }
      setOpeningsError(errorMessage(reason, '未来空档加载失败，请重试'))
    } finally {
      if (isCurrentCalendarRequest(openingsRequestRef.current, request)) {
        openingsRequestRef.current = { ...request, controller: null }
        setOpeningsLoading(false)
      }
    }
  }, [accountTimezone, navigate, openingsRange, range, readState.kind, settings, slots])

  const refreshCalendarData = useCallback(async () => {
    const requests: Array<Promise<void>> = [loadSlots()]
    if (openingsActive) requests.push(loadOpenings(true))
    await Promise.all(requests)
  }, [loadOpenings, loadSlots, openingsActive])

  const refreshCalendarAfterDelete = useCallback(async () => {
    if (settingsRequestRef.current.controller) {
      reloadAfterSettingsRef.current = true
      await waitForSettingsIdle()
      return
    }
    await refreshCalendarData()
  }, [refreshCalendarData, waitForSettingsIdle])

  useEffect(() => { void loadSettings() }, [loadSettings])
  useEffect(() => { void loadSlots() }, [loadSlots, readReloadTick])
  useEffect(() => {
    if (openingsActive) void loadOpenings()
  }, [loadOpenings, openingsActive, readReloadTick])
  useEffect(() => {
    const deleteCoordinationController = new AbortController()
    const settingsIdleWaiters = settingsIdleWaitersRef.current
    deleteCoordinationControllerRef.current = deleteCoordinationController
    return () => {
      deleteCoordinationController.abort()
      deleteCoordinationControllerRef.current = null
      deferredDeleteReconciliationRef.current = null
      settingsIdleWaiters.clear()
      deleteInFlightRef.current = false
      settingsRequestRef.current.controller?.abort()
      mainRequestRef.current.controller?.abort()
      slotLookupRequestRef.current.controller?.abort()
      openingsRequestRef.current.controller?.abort()
    }
  }, [])

  useEffect(() => {
    const pending = deferredDeleteReconciliationRef.current
    if (pending && searchParams.toString() !== pending.expectedSearch) {
      deferredDeleteReconciliationRef.current = null
    }
  }, [searchParams])

  useEffect(() => {
    if (timezoneLoading || settingsLoading || accountTimezone) return
    setReadState(terminalPageReadError(
      timezoneError ? '账号时区加载失败，修复后才能读取档期' : '账号时区不可用，暂不能读取档期',
    ))
  }, [accountTimezone, settingsLoading, timezoneError, timezoneLoading])

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
    if (!accountTimezone || !queryDate) return
    if (deletedSlotIDRef.current && querySlot === deletedSlotIDRef.current) return
    try {
      if (!isValidDate(queryDate)) throw new Error('invalid query date')
      localDayRange(queryDate, accountTimezone)
      setMonth(queryDate.slice(0, 7))
      setSelectedDate(queryDate)
      setDetailOpen(true)
    } catch {
      setQueryError('链接中的日期无效，已返回当前月份')
      setMonth(today.slice(0, 7))
      setSelectedDate(today)
    }
  }, [accountTimezone, queryDate, querySlot, today])

  useEffect(() => {
    if (!querySlot || !accountTimezone) {
      slotLookupRequestRef.current.controller?.abort()
      if (!querySlot) deletedSlotIDRef.current = ''
      return
    }
    if (querySlot === deletedSlotIDRef.current) {
      slotLookupRequestRef.current.controller?.abort()
      return
    }
    deletedSlotIDRef.current = ''
    const previous = slotLookupRequestRef.current
    previous.controller?.abort()
    const version = nextCalendarRequest(previous, querySlot)
    const controller = new AbortController()
    const request = { ...version, controller }
    slotLookupRequestRef.current = request
    setQueryError(null)
    getScheduleSlot(querySlot, controller.signal)
      .then((target) => {
        if (!isCurrentCalendarRequest(slotLookupRequestRef.current, request)) return
        const local = instantToLocalDateTime(target.start_at, accountTimezone)
        setMonth(local.date.slice(0, 7))
        setSelectedDate(local.date)
        setDetailOpen(true)
      })
      .catch((reason: unknown) => {
        if (controller.signal.aborted) return
        if (!isCurrentCalendarRequest(slotLookupRequestRef.current, request)) return
        if (reason instanceof ApiError && reason.status === 401) {
          navigate('/login', { replace: true })
          return
        }
        setQueryError(reason instanceof ApiError && reason.status === 404
          ? '链接指向的档期已不存在'
          : errorMessage(reason, '档期链接定位失败，请重试'))
      })
      .finally(() => {
        if (isCurrentCalendarRequest(slotLookupRequestRef.current, request)) {
          slotLookupRequestRef.current = { ...request, controller: null }
        }
      })
    return () => {
      controller.abort()
      if (isCurrentCalendarRequest(slotLookupRequestRef.current, request)) {
        slotLookupRequestRef.current = {
          ...nextCalendarRequest(request, ''),
          controller: null,
        }
      }
    }
  }, [accountTimezone, navigate, querySlot])

  useEffect(() => {
    setLastDeletedShoot((current) => current?.date === selectedDate ? current : null)
  }, [selectedDate])

  useEffect(() => {
    if (!querySlot || readState.kind !== 'ready') return
    const target = slots.find((slot) => slot.id === querySlot)
    if (!target) return
    const frame = window.requestAnimationFrame(() => {
      const element = document.querySelector<HTMLElement>(`[data-slot-id="${CSS.escape(querySlot)}"]`)
      element?.scrollIntoView({ block: 'center' })
      element?.focus({ preventScroll: true })
    })
    return () => window.cancelAnimationFrame(frame)
  }, [querySlot, readState.kind, slots])

  useEffect(() => {
    try {
      if (readPendingSchedule() || queryScheduleDraft) setDialogOpen(true)
    } catch (reason) {
      setQueryError(errorMessage(reason, '恢复记录读取失败'))
    }
  }, [queryScheduleDraft])

  const deleteDialogRef = useFocusTrap<HTMLElement>(Boolean(deleteTarget), closeDeleteDialog, !deleting)
  const calculationError = calendarModel.errors[0]?.message ?? overviewResult?.errors[0]?.message ?? null
  const openingsCalculationError = openingsModel.errors[0]?.message ?? null
  const hasWorkspaceNotice = Boolean(
    queryError || readPresentation.notice || settingsLoading || settingsError || calculationError,
  )
  const workspaceNotice = hasWorkspaceNotice ? (
    <>
      {queryError && <div className="form-error" role="alert">{queryError}</div>}
      {readPresentation.notice && <StateNotice {...readPresentation.notice} />}
      {settingsLoading && <StateNotice kind="loading" message="正在加载可约设置；档期查看与写操作仍可使用" />}
      {settingsError && <StateNotice kind="error" message={`${settingsError}；已隐藏可约结论，档期操作仍可使用`} retryable onRetry={() => { void loadSettings() }} />}
      {calculationError && <StateNotice kind="error" message={calculationError} retryable={false} />}
    </>
  ) : undefined

  function openDay(date: string) {
    setSelectedDate(date)
    setMonth(date.slice(0, 7))
    setDetailOpen(true)
    setQueryError(null)
    setSearchParams({ date })
  }

  function openSlot(date: string, projection: CalendarProjection) {
    setSelectedDate(date)
    setMonth(date.slice(0, 7))
    setDetailOpen(true)
    setQueryError(null)
    setSearchParams({ date, slot: projection.slot.id })
  }

  function closeDetail() {
    setLastDeletedShoot(null)
    setDetailOpen(false)
    setSearchParams(selectedDate ? { date: selectedDate } : {})
  }

  function openCreate(date = selectedDate || today) {
    if (!accountTimezone) return
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
    scheduleReturnFocusRef.current = active?.closest('.calendar-v2-detail')
      ? document.querySelector<HTMLElement>('.calendar-v2-month-cell.selected .calendar-v2-date-button, .calendar-v2-week-heading.selected')
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

  function navigatePeriod(delta: number, view: CalendarView) {
    const current = selectedDate || today || `${month}-01`
    if (!isValidDate(current)) return
    const next = view === 'week' ? shiftDate(current, delta * 7) : shiftDateByMonth(current, delta)
    setSelectedDate(next)
    setMonth(next.slice(0, 7))
    setQueryError(null)
    setSearchParams({ date: next })
  }

  function goToday() {
    if (!today) return
    openDay(today)
  }

  function jumpMonth(value: string) {
    if (!/^\d{4}-\d{2}$/.test(value)) return
    const next = dateInMonth(selectedDate || today, value)
    setMonth(value)
    setSelectedDate(next)
    setQueryError(null)
    setSearchParams({ date: next })
  }

  function closeOpenings() {
    setOpeningsActive(false)
    const current = openingsRequestRef.current
    current.controller?.abort()
    openingsRequestRef.current = {
      ...nextCalendarRequest(current, ''),
      controller: null,
    }
    setOpeningsLoading(false)
  }

  async function copyOpenings(text: string) {
    try {
      await navigator.clipboard.writeText(text)
      notify('可约文案已复制到剪贴板')
    } catch {
      notify('复制失败，请手动选择文案复制')
    }
  }

  async function confirmDelete() {
    if (deleteInFlightRef.current) return
    const target = deleteTarget
    if (!target?.id) return
    const deleteCoordinationSignal = deleteCoordinationControllerRef.current?.signal
    deleteInFlightRef.current = true
    setDeleting(true)
    setDeleteError(null)
    try {
      await deleteScheduleSlot(target.id)
      if (deleteCoordinationSignal?.aborted) return
      deletedSlotIDRef.current = target.id
      setReadState((current) => current.kind === 'ready'
        ? { ...current, data: current.data.filter((slot) => slot.id !== target.id) }
        : current)
      setOpeningsSlots((current) => current.filter((slot) => slot.id !== target.id))
      const nextCompletion = await reconcileCalendarDeleteRefresh({
        deletedSlotID: target.id,
        deletedSlotStartAt: target.start_at,
        expectedRangeKey: range ? `${range.from}:${range.to}` : '',
        readLocation: () => ({
          pathname: window.location.pathname,
          search: window.location.search,
          timezone: latestAccountTimezoneRef.current,
        }),
        refreshOriginal: refreshCalendarAfterDelete,
        requestCurrentReload: requestCalendarReload,
      })
      if (deleteCoordinationSignal?.aborted) return
      const completionSearch = new URLSearchParams(window.location.search)
      const completionUsedTimezoneDate = Boolean(
        nextCompletion
        && completionSearch.get('slot') === target.id
        && !isValidDate(completionSearch.get('date') ?? ''),
      )
      deferredDeleteReconciliationRef.current = nextCompletion
        && completionUsedTimezoneDate
        && Boolean(settingsRequestRef.current.controller)
        ? {
            deletedSlotID: target.id,
            deletedSlotStartAt: target.start_at,
            expectedSearch: nextCompletion.searchParams.toString(),
          }
        : null
      setLastDeletedShoot(nextCompletion && target.type === 'shoot'
        ? { slot: target, date: nextCompletion.date }
        : null)
      if (nextCompletion) applyCalendarDeleteCompletion(nextCompletion)
      setDeleteTarget(null)
      notify('档期已删除，订单和订单状态保持不变')
    } catch (reason) {
      if (deleteCoordinationSignal?.aborted) return
      setDeleteError(errorMessage(reason, '档期删除失败'))
    } finally {
      deleteInFlightRef.current = false
      if (!deleteCoordinationSignal?.aborted) setDeleting(false)
    }
  }

  function openDeleteDialog(slot: ScheduleSlotListItem) {
    if (deleteInFlightRef.current) return
    setLastDeletedShoot(null)
    setDeleteError(null)
    setDeleteTarget(slot)
  }

  function closeDeleteDialog() {
    if (deleteInFlightRef.current) return
    setDeleteError(null)
    setDeleteTarget(null)
  }

  return (
    <>
      <header className="topbar">
        <div>
          <h1>档期</h1>
          <div className="sub">时间按 {accountTimezone ?? '账号时区不可用'}</div>
        </div>
      </header>

      <main className="content calendar-content">
        <CalendarWorkspace
          dates={gridDates}
          weekDates={weekDates}
          days={calendarModel.days}
          month={month}
          today={today}
          selectedDate={selectedDate}
          selectedSlotID={querySlot}
          detailOpen={detailOpen}
          detailSuppressed={dialogOpen || Boolean(deleteTarget)}
          loading={loading}
          writesEnabled={Boolean(accountTimezone)}
          availabilityReady={Boolean(settings)}
          overview={overviewResult?.overview ?? null}
          notice={workspaceNotice}
          openings={openings}
          openingsLoading={openingsLoading}
          openingsError={openingsError ?? openingsCalculationError}
          lastDeletedShoot={lastDeletedShoot}
          onNavigate={navigatePeriod}
          onToday={goToday}
          onJumpMonth={jumpMonth}
          onSelectDate={openDay}
          onSelectSlot={openSlot}
          onDetailOpen={() => setDetailOpen(true)}
          onDetailClose={closeDetail}
          onCreate={openCreate}
          onEdit={openEdit}
          onDelete={openDeleteDialog}
          onOpeningsOpen={() => setOpeningsActive(true)}
          onOpeningsClose={closeOpenings}
          onOpeningsRetry={() => { void loadOpenings(true) }}
          onCopyOpenings={copyOpenings}
        />
      </main>

      <ScheduleSlotDialog
        open={dialogOpen}
        timezone={accountTimezone}
        initialDate={selectedDate || today}
        slot={editingSlot}
        scheduleDraftId={queryScheduleDraft || undefined}
        onClose={closeScheduleDialog}
        onChanged={refreshCalendarData}
        onCompleted={(date) => {
          setSelectedDate(date)
          setMonth(date.slice(0, 7))
          setDetailOpen(true)
          setSearchParams({ date })
        }}
      />

      {deleteTarget && (
        <div className="overlay open" onClick={(event) => { if (event.target === event.currentTarget) closeDeleteDialog() }}>
          <section ref={deleteDialogRef} className="dialog" role="dialog" aria-modal="true" aria-labelledby="deleteScheduleTitle" aria-busy={deleting} tabIndex={-1}>
            <h2 id="deleteScheduleTitle">删除档期</h2>
            <p className="dialog-sub">只删除日历档期，订单和订单状态都会保留。</p>
            {deleteError && <div className="form-error">{deleteError}</div>}
            <div className="dialog-actions">
              <button className="btn" type="button" disabled={deleting} onClick={closeDeleteDialog}>取消</button>
              <button className="btn btn-danger" type="button" disabled={deleting} onClick={() => { void confirmDelete() }}>
                {deleting ? '正在删除…' : '确认删除'}
              </button>
            </div>
          </section>
        </div>
      )}
    </>
  )
}

function emptyActiveRequest(): ActiveCalendarRequest {
  return { rangeKey: '', generation: 0, controller: null }
}

function weekDatesFor(date: string): string[] {
  const anchor = new Date(`${date}T12:00:00Z`)
  const mondayOffset = (anchor.getUTCDay() + 6) % 7
  return Array.from({ length: 7 }, (_, index) => shiftDate(date, index - mondayOffset))
}

function datesAfter(date: string, count: number): string[] {
  return Array.from({ length: count }, (_, index) => shiftDate(date, index + 1))
}

function shiftDate(value: string, days: number): string {
  const date = new Date(`${value}T12:00:00Z`)
  date.setUTCDate(date.getUTCDate() + days)
  return date.toISOString().slice(0, 10)
}

function shiftDateByMonth(value: string, delta: number): string {
  const [year, month, day] = value.split('-').map(Number)
  const target = new Date(Date.UTC(year ?? 1970, (month ?? 1) - 1 + delta, 1))
  const targetMonth = `${target.getUTCFullYear()}-${String(target.getUTCMonth() + 1).padStart(2, '0')}`
  return dateInMonth(`${targetMonth}-${String(day ?? 1).padStart(2, '0')}`, targetMonth)
}

function dateInMonth(preferred: string, month: string): string {
  const [year, monthValue] = month.split('-').map(Number)
  const preferredDay = Number(preferred.slice(8, 10)) || 1
  const lastDay = new Date(Date.UTC(year ?? 1970, monthValue ?? 1, 0)).getUTCDate()
  return `${month}-${String(Math.min(preferredDay, lastDay)).padStart(2, '0')}`
}

function errorMessage(reason: unknown, fallback: string): string {
  if (reason instanceof ApiError || reason instanceof Error) return reason.message
  return fallback
}
