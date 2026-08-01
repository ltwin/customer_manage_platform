export function calendarDateFocusTarget(
  dates: string[],
  currentDate: string,
  key: string,
): string | null {
  const offset = {
    ArrowLeft: -1,
    ArrowRight: 1,
    ArrowUp: -7,
    ArrowDown: 7,
  }[key]
  if (offset === undefined) return null
  const currentIndex = dates.indexOf(currentDate)
  if (currentIndex < 0) return null
  return dates[currentIndex + offset] ?? null
}
