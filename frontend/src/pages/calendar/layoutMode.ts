import { useEffect, useRef, useState } from 'react'

export const SIDE_PANEL_MIN_WIDTH = 1080

export type CalendarLayoutMode = 'side-panel' | 'bottom-sheet'

export function calendarLayoutModeForWidth(width: number): CalendarLayoutMode {
  return width >= SIDE_PANEL_MIN_WIDTH ? 'side-panel' : 'bottom-sheet'
}

export function calendarDetailShouldOpen(
  mode: CalendarLayoutMode,
  detailOpen: boolean,
  suppressed: boolean,
): boolean {
  return !suppressed && (mode === 'side-panel' || detailOpen)
}

export function useCalendarWorkspaceMode() {
  const containerRef = useRef<HTMLDivElement>(null)
  const [mode, setMode] = useState<CalendarLayoutMode>('bottom-sheet')

  useEffect(() => {
    const container = containerRef.current
    if (!container) return
    const update = (width: number) => setMode(calendarLayoutModeForWidth(width))
    update(container.getBoundingClientRect().width)
    const observer = new ResizeObserver((entries) => {
      const entry = entries[0]
      if (entry) update(entry.contentRect.width)
    })
    observer.observe(container)
    return () => observer.disconnect()
  }, [])

  return { containerRef, mode }
}
