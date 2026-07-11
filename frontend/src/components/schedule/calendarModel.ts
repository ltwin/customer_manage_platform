import type { ScheduleSlotListItem } from '../../api/client'
import { localDayRange, projectSlotToLocalDays } from './timezone.ts'

export interface CalendarSlotProjection {
  slot: ScheduleSlotListItem
  date: string
  allDay: boolean
  displayStart: string
  displayEnd: string
  conflicting: boolean
}

export interface CalendarDay {
  date: string
  slots: CalendarSlotProjection[]
  conflictCount: number
}

export function buildCalendarDays(
  slots: ScheduleSlotListItem[],
  dates: string[],
  timezone: string,
): CalendarDay[] {
  const byDate = new Map<string, CalendarSlotProjection[]>(dates.map((date) => [date, []]))
  for (const slot of slots) {
    const projections = projectSlotToLocalDays(
      { startAt: slot.start_at, endAt: slot.end_at },
      timezone,
    )
    for (const projection of projections) {
      const entries = byDate.get(projection.date)
      if (!entries) continue
      entries.push({ slot, ...projection, conflicting: false })
    }
  }

  return dates.map((date) => {
    const entries = byDate.get(date) ?? []
    entries.sort((a, b) => {
      const byStart = a.displayStart.localeCompare(b.displayStart)
      if (byStart !== 0) return byStart
      return (a.slot.id ?? '').localeCompare(b.slot.id ?? '')
    })
    const conflicts = conflictingSlotIDsForDay(entries.map((entry) => entry.slot), date, timezone)
    for (const entry of entries) entry.conflicting = conflicts.has(entry.slot.id ?? '')
    return { date, slots: entries, conflictCount: conflicts.size }
  })
}

export function overlappingSlots(
  slots: ScheduleSlotListItem[],
  startAt: string,
  endAt: string,
  excludeID?: string,
): ScheduleSlotListItem[] {
  const start = Date.parse(startAt)
  const end = Date.parse(endAt)
  return slots.filter((slot) =>
    slot.id !== excludeID && Date.parse(slot.start_at) < end && start < Date.parse(slot.end_at))
}

function conflictingSlotIDsForDay(
  slots: ScheduleSlotListItem[],
  date: string,
  timezone: string,
): Set<string> {
  const range = localDayRange(date, timezone)
  const dayStart = Date.parse(range.start)
  const dayEnd = Date.parse(range.end)
  const result = new Set<string>()
  for (let left = 0; left < slots.length; left += 1) {
    for (let right = left + 1; right < slots.length; right += 1) {
      const a = slots[left]
      const b = slots[right]
      if (!a || !b) continue
      const overlapStart = Math.max(Date.parse(a.start_at), Date.parse(b.start_at), dayStart)
      const overlapEnd = Math.min(Date.parse(a.end_at), Date.parse(b.end_at), dayEnd)
      if (overlapStart >= overlapEnd) continue
      if (a.id) result.add(a.id)
      if (b.id) result.add(b.id)
    }
  }
  return result
}
