import { Temporal } from '@js-temporal/polyfill'

export type LocalTimeOccurrence = 'first' | 'second'
export type ScheduleTimeErrorCode =
  | 'invalid_date_time'
  | 'invalid_timezone'
  | 'nonexistent_local_time'
  | 'ambiguous_local_time'

export class ScheduleTimeError extends Error {
	readonly code: ScheduleTimeErrorCode

	constructor(code: ScheduleTimeErrorCode, message: string) {
		super(message)
		this.name = 'ScheduleTimeError'
		this.code = code
	}
}

export interface LocalDayRange {
  start: string
  end: string
}

export interface LocalDayProjection {
  date: string
  allDay: boolean
  displayStart: string
  displayEnd: string
}

export interface LocalDateTimeInput {
  date: string
  time: string
}

export function monthGrid(month: string): string[] {
  let first: Temporal.PlainDate
  try {
    first = Temporal.PlainYearMonth.from(month).toPlainDate({ day: 1 })
  } catch {
    throw new ScheduleTimeError('invalid_date_time', '月份格式无效')
  }
  const start = first.subtract({ days: first.dayOfWeek - 1 })
  return Array.from({ length: 42 }, (_, index) => start.add({ days: index }).toString())
}

export function localDayRange(date: string, timeZone: string): LocalDayRange {
  const plainDate = parseDate(date)
  try {
    const start = plainDate.toZonedDateTime(timeZone).toInstant()
    const end = plainDate.add({ days: 1 }).toZonedDateTime(timeZone).toInstant()
    return { start: instantString(start), end: instantString(end) }
  } catch {
    throw new ScheduleTimeError('invalid_timezone', '账号时区无效')
  }
}

export function resolveLocalDateTime(
  date: string,
  time: string,
  timeZone: string,
  occurrence?: LocalTimeOccurrence,
): { instant: string; offset: string; ambiguous: boolean } {
  let plainDateTime: Temporal.PlainDateTime
  try {
    plainDateTime = Temporal.PlainDateTime.from(`${date}T${time}`)
  } catch {
    throw new ScheduleTimeError('invalid_date_time', '日期或时间格式无效')
  }

  let earlier: Temporal.ZonedDateTime
  let later: Temporal.ZonedDateTime
  try {
    earlier = plainDateTime.toZonedDateTime(timeZone, { disambiguation: 'earlier' })
    later = plainDateTime.toZonedDateTime(timeZone, { disambiguation: 'later' })
  } catch {
    throw new ScheduleTimeError('invalid_timezone', '账号时区无效')
  }

  const earlierMatches = earlier.toPlainDateTime().equals(plainDateTime)
  const laterMatches = later.toPlainDateTime().equals(plainDateTime)
  if (!earlierMatches && !laterMatches) {
    throw new ScheduleTimeError('nonexistent_local_time', '该本地时间不存在')
  }

  const ambiguous =
    earlierMatches &&
    laterMatches &&
    !earlier.toInstant().equals(later.toInstant())
  if (ambiguous && !occurrence) {
    throw new ScheduleTimeError('ambiguous_local_time', '该本地时间重复，请选择第一次或第二次')
  }

  let resolved = earlierMatches ? earlier : later
  if (ambiguous && occurrence === 'second') resolved = later
  return {
    instant: instantString(resolved.toInstant()),
    offset: resolved.offset,
    ambiguous,
  }
}

export function accountDateAtNoonToInstant(date: string, timeZone: string): string {
  return resolveLocalDateTime(date, '12:00', timeZone).instant
}

export function isValidDate(date: string): boolean {
  try {
    parseDate(date)
    return true
  } catch {
    return false
  }
}

export function isValidAccountDate(date: string, timeZone: string): boolean {
	try {
		accountDateAtNoonToInstant(date, timeZone)
		return true
	} catch {
		return false
	}
}

export function accountToday(timeZone: string, now?: string): string {
  let instant: Temporal.Instant
  try {
    instant = now ? Temporal.Instant.from(now) : Temporal.Now.instant()
    return instant.toZonedDateTimeISO(timeZone).toPlainDate().toString()
  } catch {
    throw new ScheduleTimeError('invalid_timezone', '账号时区无效')
  }
}

export function instantToLocalDateTime(instant: string, timeZone: string): LocalDateTimeInput {
  try {
    const value = Temporal.Instant.from(instant).toZonedDateTimeISO(timeZone)
    return {
      date: value.toPlainDate().toString(),
      time: `${String(value.hour).padStart(2, '0')}:${String(value.minute).padStart(2, '0')}`,
    }
  } catch {
    throw new ScheduleTimeError('invalid_date_time', '档期时间或账号时区无效')
  }
}

export function projectSlotToLocalDays(
  slot: { startAt: string; endAt: string },
  timeZone: string,
): LocalDayProjection[] {
  let start: Temporal.Instant
  let end: Temporal.Instant
  try {
    start = Temporal.Instant.from(slot.startAt)
    end = Temporal.Instant.from(slot.endAt)
  } catch {
    throw new ScheduleTimeError('invalid_date_time', '档期时间格式无效')
  }
  if (Temporal.Instant.compare(start, end) >= 0) {
    throw new ScheduleTimeError('invalid_date_time', '档期结束时间必须晚于开始时间')
  }

  let firstDate: Temporal.PlainDate
  let lastDate: Temporal.PlainDate
  try {
    firstDate = start.toZonedDateTimeISO(timeZone).toPlainDate()
    lastDate = end.subtract({ nanoseconds: 1 }).toZonedDateTimeISO(timeZone).toPlainDate()
  } catch {
    throw new ScheduleTimeError('invalid_timezone', '账号时区无效')
  }

  const result: LocalDayProjection[] = []
  for (
    let date = firstDate;
    Temporal.PlainDate.compare(date, lastDate) <= 0;
    date = date.add({ days: 1 })
  ) {
    const range = localDayRange(date.toString(), timeZone)
    const dayStart = Temporal.Instant.from(range.start)
    const dayEnd = Temporal.Instant.from(range.end)
    const segmentStart = Temporal.Instant.compare(start, dayStart) > 0 ? start : dayStart
    const segmentEnd = Temporal.Instant.compare(end, dayEnd) < 0 ? end : dayEnd
    if (Temporal.Instant.compare(segmentStart, segmentEnd) >= 0) continue

    const startsAtBoundary = segmentStart.equals(dayStart)
    const endsAtBoundary = segmentEnd.equals(dayEnd)
    result.push({
      date: date.toString(),
      allDay: startsAtBoundary && endsAtBoundary,
      displayStart: startsAtBoundary ? '00:00' : localTime(segmentStart, timeZone),
      displayEnd: endsAtBoundary ? '24:00' : localTime(segmentEnd, timeZone),
    })
  }
  return result
}

function parseDate(date: string): Temporal.PlainDate {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) {
    throw new ScheduleTimeError('invalid_date_time', '日期格式无效')
  }
  try {
    return Temporal.PlainDate.from(date)
  } catch {
    throw new ScheduleTimeError('invalid_date_time', '日期格式无效')
  }
}

function instantString(instant: Temporal.Instant): string {
  return instant.toString({ smallestUnit: 'second' })
}

function localTime(instant: Temporal.Instant, timeZone: string): string {
  const value = instant.toZonedDateTimeISO(timeZone)
  return `${String(value.hour).padStart(2, '0')}:${String(value.minute).padStart(2, '0')}`
}
