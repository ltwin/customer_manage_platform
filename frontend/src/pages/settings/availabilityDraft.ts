import type {
  ScheduleAvailabilityWeekly,
  ScheduleAvailabilityWindow,
  Settings,
  UpdateSettingsBody,
} from '../../api/client'

export interface AvailabilitySettingsDraft {
  weekly: ScheduleAvailabilityWeekly
  minOpeningMinutes: number
  turnaroundMinutes: number
}

export type AvailabilityValidationResult =
  | { ok: true }
  | { ok: false; field: string; message: string }

export const ISO_WEEKDAYS = [1, 2, 3, 4, 5, 6, 7] as const

const weekdayLabels: Record<(typeof ISO_WEEKDAYS)[number], string> = {
  1: '周一',
  2: '周二',
  3: '周三',
  4: '周四',
  5: '周五',
  6: '周六',
  7: '周日',
}

export function fromSettings(settings: Settings): AvailabilitySettingsDraft {
  return {
    weekly: cloneWeekly(settings.availability.weekly),
    minOpeningMinutes: settings.availability.min_opening_minutes,
    turnaroundMinutes: settings.availability.turnaround_minutes,
  }
}

export function validateAvailabilityDraft(
  draft: AvailabilitySettingsDraft,
): AvailabilityValidationResult {
  for (const day of ISO_WEEKDAYS) {
    const window = draft.weekly[day]
    if (!window) continue
    const label = weekdayLabels[day]
    const start = localTimeMinutes(window.start)
    if (start === null) {
      return { ok: false, field: `weekly.${day}.start`, message: `${label}开始时间须为 HH:MM` }
    }
    const end = localTimeMinutes(window.end)
    if (end === null) {
      return { ok: false, field: `weekly.${day}.end`, message: `${label}结束时间须为 HH:MM` }
    }
    if (end <= start) {
      return { ok: false, field: `weekly.${day}`, message: `${label}结束时间须晚于开始时间` }
    }
  }
  if (!Number.isInteger(draft.minOpeningMinutes) || draft.minOpeningMinutes < 15 || draft.minOpeningMinutes > 480) {
    return {
      ok: false,
      field: 'minOpeningMinutes',
      message: '最小可报空档须为 15–480 分钟的整数',
    }
  }
  if (!Number.isInteger(draft.turnaroundMinutes) || draft.turnaroundMinutes < 0 || draft.turnaroundMinutes > 240) {
    return {
      ok: false,
      field: 'turnaroundMinutes',
      message: '转场缓冲须为 0–240 分钟的整数',
    }
  }
  return { ok: true }
}

export function applyAvailabilityDraft(
  currentBody: UpdateSettingsBody,
  draft: AvailabilitySettingsDraft,
): UpdateSettingsBody {
  return {
    ...currentBody,
    churn_thresholds: currentBody.churn_thresholds?.map((entry) => ({ ...entry })),
    availability: {
      weekly: cloneWeekly(draft.weekly),
      min_opening_minutes: draft.minOpeningMinutes,
      turnaround_minutes: draft.turnaroundMinutes,
    },
  }
}

function cloneWeekly(weekly: ScheduleAvailabilityWeekly): ScheduleAvailabilityWeekly {
  return {
    1: cloneWindow(weekly[1]),
    2: cloneWindow(weekly[2]),
    3: cloneWindow(weekly[3]),
    4: cloneWindow(weekly[4]),
    5: cloneWindow(weekly[5]),
    6: cloneWindow(weekly[6]),
    7: cloneWindow(weekly[7]),
  }
}

function cloneWindow(window: ScheduleAvailabilityWindow | null): ScheduleAvailabilityWindow | null {
  return window ? { ...window } : null
}

function localTimeMinutes(value: string): number | null {
  const match = /^(\d{2}):(\d{2})$/.exec(value)
  if (!match) return null
  const hours = Number(match[1])
  const minutes = Number(match[2])
  if (hours > 23 || minutes > 59) return null
  return hours * 60 + minutes
}
