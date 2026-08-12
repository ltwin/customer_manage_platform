import { Temporal } from '@js-temporal/polyfill'
import { resolveLocalDateTime } from '../components/schedule/timezone.ts'

export type ExecutionWindowMode = 'automatic' | 'custom'

export interface ExecutionWindowDraft {
  startsAt: string
  endsAt: string
  timezone: string
  mode: ExecutionWindowMode
  customLiveStartsAt: string
  customLiveEndsAt: string
}

export interface ResolvedExecutionWindow {
  startsAt: string
  endsAt: string
  timezone: string
  liveWindowStartsAt: string
  liveWindowEndsAt: string
}

interface SavedExecutionWindow {
  starts_at: string
  ends_at: string
  live_window_starts_at: string
  live_window_ends_at: string
}

const LIVE_BUFFER_HOURS = 2

export function buildExecutionWindow(draft: ExecutionWindowDraft): ResolvedExecutionWindow {
  if (!draft.startsAt || !draft.endsAt) {
    throw new Error('请填写拍摄开始和拍摄结束时间。')
  }
  const timezone = draft.timezone.trim()
  if (!timezone) throw new Error('请填写拍摄地点时区。')

  const startsAt = localInputToInstant(draft.startsAt, timezone)
  const endsAt = localInputToInstant(draft.endsAt, timezone)
  const start = Temporal.Instant.from(startsAt)
  const end = Temporal.Instant.from(endsAt)
  if (Temporal.Instant.compare(end, start) <= 0) {
    throw new Error('拍摄结束时间必须晚于拍摄开始时间。')
  }

  let liveStart: Temporal.Instant
  let liveEnd: Temporal.Instant
  if (draft.mode === 'automatic') {
    liveStart = start.subtract({ hours: LIVE_BUFFER_HOURS })
    liveEnd = end.add({ hours: LIVE_BUFFER_HOURS })
  } else {
    if (!draft.customLiveStartsAt || !draft.customLiveEndsAt) {
      throw new Error('请填写现场识别范围的开始和结束时间。')
    }
    liveStart = Temporal.Instant.from(localInputToInstant(draft.customLiveStartsAt, timezone))
    liveEnd = Temporal.Instant.from(localInputToInstant(draft.customLiveEndsAt, timezone))
    if (Temporal.Instant.compare(liveStart, start) > 0 || Temporal.Instant.compare(liveEnd, end) < 0) {
      throw new Error('现场识别范围需要包含完整的拍摄时间。')
    }
  }

  return {
    startsAt,
    endsAt,
    timezone,
    liveWindowStartsAt: instantString(liveStart),
    liveWindowEndsAt: instantString(liveEnd),
  }
}

export function inferExecutionWindowMode(window: SavedExecutionWindow | null | undefined): ExecutionWindowMode {
  if (!window) return 'automatic'
  try {
    const expectedStart = Temporal.Instant.from(window.starts_at).subtract({ hours: LIVE_BUFFER_HOURS })
    const expectedEnd = Temporal.Instant.from(window.ends_at).add({ hours: LIVE_BUFFER_HOURS })
    return expectedStart.equals(Temporal.Instant.from(window.live_window_starts_at)) &&
      expectedEnd.equals(Temporal.Instant.from(window.live_window_ends_at))
      ? 'automatic'
      : 'custom'
  } catch {
    return 'custom'
  }
}

function localInputToInstant(value: string, timezone: string): string {
  const [date, time] = value.split('T')
  if (!date || !time) throw new Error('日期或时间格式无效。')
  return resolveLocalDateTime(date, time, timezone).instant
}

function instantString(instant: Temporal.Instant): string {
  return instant.toString({ smallestUnit: 'second' })
}
