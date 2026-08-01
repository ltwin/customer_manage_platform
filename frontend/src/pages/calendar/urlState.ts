import {
  instantToLocalDateTime,
  isValidDate,
  localDayRange,
  monthGrid,
} from '../../components/schedule/timezone.ts'

export function calendarRangeKeyForSearch(
  searchParams: URLSearchParams,
  timezone: string,
  fallbackRangeKey: string,
): string {
  const date = searchParams.get('date') ?? ''
  if (!timezone || !isValidDate(date)) return fallbackRangeKey
  try {
    const dates = monthGrid(date.slice(0, 7))
    const from = localDayRange(dates[0] ?? '', timezone).start
    const to = localDayRange(dates[41] ?? '', timezone).end
    return `${from}:${to}`
  } catch {
    return fallbackRangeKey
  }
}

export function calendarSearchParamsAfterDelete(
  currentSearchParams: URLSearchParams,
  deletedSlotID: string,
): URLSearchParams | null {
  if (currentSearchParams.get('slot') !== deletedSlotID) return null
  const nextSearchParams = new URLSearchParams(currentSearchParams)
  nextSearchParams.delete('slot')
  return nextSearchParams
}

export interface CalendarDeleteCompletion {
  searchParams: URLSearchParams
  date: string
  rangeKey: string
}

export function calendarDeleteCompletion(
  currentSearchParams: URLSearchParams,
  deletedSlotID: string,
  deletedSlotStartAt: string,
  timezone: string,
): CalendarDeleteCompletion | null {
  const nextSearchParams = calendarSearchParamsAfterDelete(currentSearchParams, deletedSlotID)
  if (!nextSearchParams) return null
  const date = nextSearchParams.get('date') ?? ''
  const effectiveDate = isValidDate(date)
    ? date
    : instantToLocalDateTime(deletedSlotStartAt, timezone).date
  nextSearchParams.set('date', effectiveDate)
  return {
    searchParams: nextSearchParams,
    date: effectiveDate,
    rangeKey: calendarRangeKeyForSearch(nextSearchParams, timezone, ''),
  }
}

export function calendarDeleteCompletionAfterSettings(
  currentSearchParams: URLSearchParams,
  expectedSearch: string,
  deletedSlotID: string,
  deletedSlotStartAt: string,
  timezone: string,
): CalendarDeleteCompletion | null {
  if (currentSearchParams.toString() !== expectedSearch) return null
  const sourceSearchParams = new URLSearchParams(currentSearchParams)
  sourceSearchParams.delete('date')
  sourceSearchParams.set('slot', deletedSlotID)
  return calendarDeleteCompletion(
    sourceSearchParams,
    deletedSlotID,
    deletedSlotStartAt,
    timezone,
  )
}

export async function reconcileCalendarDeleteRefresh({
  deletedSlotID,
  deletedSlotStartAt,
  expectedRangeKey,
  readLocation,
  refreshOriginal,
  requestCurrentReload,
}: {
  deletedSlotID: string
  deletedSlotStartAt: string
  expectedRangeKey: string
  readLocation(): { pathname: string; search: string; timezone: string }
  refreshOriginal(): Promise<void>
  requestCurrentReload(): void
}): Promise<CalendarDeleteCompletion | null> {
  const initialLocation = readLocation()
  if (initialLocation.pathname !== '/calendar') return null
  const initialCompletion = calendarDeleteCompletion(
    new URLSearchParams(initialLocation.search),
    deletedSlotID,
    deletedSlotStartAt,
    initialLocation.timezone,
  )
  if (!initialCompletion) {
    requestCurrentReload()
    return null
  }
  if (initialCompletion.rangeKey !== expectedRangeKey) {
    requestCurrentReload()
    return initialCompletion
  }

  await refreshOriginal()

  const currentLocation = readLocation()
  if (currentLocation.pathname !== '/calendar') return null
  return calendarDeleteCompletion(
    new URLSearchParams(currentLocation.search),
    deletedSlotID,
    deletedSlotStartAt,
    currentLocation.timezone,
  )
}
