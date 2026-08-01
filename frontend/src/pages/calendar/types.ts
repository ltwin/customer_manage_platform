export type CalendarView = 'week' | 'month'
export type CalendarSlotType = 'shoot' | 'hold' | 'busy'

export interface CalendarFilters {
  shoot: boolean
  hold: boolean
  busy: boolean
}

export const defaultCalendarFilters: CalendarFilters = {
  shoot: true,
  hold: true,
  busy: true,
}
