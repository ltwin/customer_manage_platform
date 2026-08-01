export interface CalendarRequestVersion {
  rangeKey: string
  generation: number
}

export function nextCalendarRequest(
  current: CalendarRequestVersion,
  rangeKey: string,
): CalendarRequestVersion {
  return { rangeKey, generation: current.generation + 1 }
}

export function isCurrentCalendarRequest(
  current: CalendarRequestVersion,
  request: CalendarRequestVersion,
): boolean {
  return current.rangeKey === request.rangeKey && current.generation === request.generation
}
