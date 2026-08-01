export const CALENDAR_SETTINGS_DELETE_WAIT_MS = 1_000

export type CalendarSettingsWaitResult = 'idle' | 'timeout' | 'cancelled'

export function waitForCalendarSettingsIdle({
  isIdle,
  subscribe,
  signal,
  timeoutMs = CALENDAR_SETTINGS_DELETE_WAIT_MS,
}: {
  isIdle(): boolean
  subscribe(listener: () => void): () => void
  signal?: AbortSignal
  timeoutMs?: number
}): Promise<CalendarSettingsWaitResult> {
  if (isIdle()) return Promise.resolve('idle')
  if (signal?.aborted) return Promise.resolve('cancelled')

  return new Promise((resolve) => {
    let settled = false
    let timeoutID: ReturnType<typeof setTimeout> | null = null
    let unsubscribe = () => {}

    const finish = (result: CalendarSettingsWaitResult) => {
      if (settled) return
      settled = true
      if (timeoutID !== null) clearTimeout(timeoutID)
      signal?.removeEventListener('abort', onAbort)
      unsubscribe()
      resolve(result)
    }
    const checkIdle = () => {
      if (isIdle()) finish('idle')
    }
    const onAbort = () => finish('cancelled')

    unsubscribe = subscribe(checkIdle)
    timeoutID = setTimeout(() => finish('timeout'), timeoutMs)
    signal?.addEventListener('abort', onAbort, { once: true })
    checkIdle()
  })
}
