import { useEffect, useRef, useState } from 'react'
import { observe, type Observation } from './api'

function sessionID() {
  try {
    const old = sessionStorage.getItem('creative-session')
    if (old) return old
    const id = crypto.randomUUID()
    sessionStorage.setItem('creative-session', id)
    return id
  } catch {
    return crypto.randomUUID()
  }
}
// Only open observations are queued. Shoot results and content are never queued.
export function useObservations(
  id: string,
  view: string,
  online: boolean,
  enabled: boolean,
) {
  const session = useRef(sessionID())
  const lastView = useRef('')
  const [queue, setQueue] = useState<Observation[]>([])
  useEffect(() => {
    if (!enabled) return
    const key = `${id}:${view === 'live' ? 'live' : 'space'}`
    if (lastView.current === key) return
    lastView.current = key
    setQueue((old) => [
      ...old,
      {
        event_id: crypto.randomUUID(),
        session_id: session.current,
        kind:
          view === 'live'
            ? online
              ? 'live_open'
              : 'live_unverified'
            : 'space_open',
        client_at: new Date().toISOString(),
      },
    ])
  }, [id, view, online, enabled])
  useEffect(() => {
    if (!online || !enabled || !queue.length) return
    let active = true
    const event = queue[0]
    void observe(id, event)
      .then(() => {
        if (active)
          setQueue((q) => q.filter((e) => e.event_id !== event.event_id))
      })
      .catch(() => {
        /* Retry only after reconnect or a new observation. */
      })
    return () => {
      active = false
    }
  }, [id, online, enabled, queue])
  return queue.some((e) => e.kind === 'live_unverified')
}
