import { useEffect, useRef, useState } from 'react'
import * as api from './api.ts'
import { ApiError } from '../../api/transport.ts'
import { notifyCanvasInvalidate, watchCanvasEvents } from './canvasEvents.ts'
import { newerCanvas } from './snapshot.ts'
import { errorMessage } from './queue.ts'

// Owns snapshot ordering, event subscription and retry lifetime. A hidden
// tab drops its event stream, unless `keepAlive`: the page runs or awaits
// assistant work (a scene job held here, a live run) that its notifications
// drive.
export function useCanvasSnapshot(canvasID: string, tick: number, keepAlive = false) {
  const [canvas, setCanvas] = useState<api.Canvas | null>(null)
  const [syncError, setSyncError] = useState('')
  const [loading, setLoading] = useState(true)
  const readSequence = useRef(0)
  const canvasReadSequence = useRef(0)
  useEffect(() => { setCanvas(null); setSyncError('') }, [canvasID])
  useEffect(() => {
    const controller = new AbortController()
    const seq = ++readSequence.current
    setLoading(true)
    const canvasSeq = ++canvasReadSequence.current
    void (async () => {
      const c = canvasID
        ? await api.read<api.Canvas>(
            `/canvases/${encodeURIComponent(canvasID)}`,
            controller.signal,
          )
        : null
      if (controller.signal.aborted || seq !== readSequence.current) return
      if (canvasSeq === canvasReadSequence.current)
        setCanvas((old) => newerCanvas(old, c))
      if (canvasSeq === canvasReadSequence.current) setSyncError('')
    })()
      .catch((e: unknown) => {
        if (!controller.signal.aborted && seq === readSequence.current && canvasSeq === canvasReadSequence.current) setSyncError(errorMessage(e))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [canvasID, tick])
  const refreshCanvas = useRef<() => Promise<void>>(() => Promise.resolve())
  const keepAliveRef = useRef(keepAlive)
  const reconnect = useRef<() => void>(() => {})
  useEffect(() => {
    keepAliveRef.current = keepAlive
    if (document.hidden) reconnect.current()
  }, [keepAlive])
  useEffect(() => {
    const controller = new AbortController()
    let fetching = false
    let requested = false
    let retry: ReturnType<typeof setTimeout> | undefined
    let retryDelay = 1000
    const reload = async () => {
      if (controller.signal.aborted || !canvasID) return
      if (fetching || document.hidden || !navigator.onLine) {
        requested = true
        return
      }
      requested = false
      clearTimeout(retry)
      fetching = true
      const canvasSeq = ++canvasReadSequence.current
      try {
        const c = await api.read<api.Canvas>(
          `/canvases/${encodeURIComponent(canvasID)}`,
          controller.signal,
        )
        if (
          !controller.signal.aborted &&
          canvasSeq === canvasReadSequence.current
        ) {
          setCanvas((old) => newerCanvas(old, c))
          setSyncError('')
          retryDelay = 1000
        }
      } catch (e) {
        if (!controller.signal.aborted && canvasSeq === canvasReadSequence.current) {
          setSyncError(errorMessage(e))
          // Retry a failed invalidation read, never periodically read an idle canvas.
          if (!(e instanceof ApiError && [401, 403, 404].includes(e.status))) {
            retry = setTimeout(() => void reload(), retryDelay)
            retryDelay = Math.min(retryDelay * 2, 30000)
          }
        }
      } finally {
        fetching = false
        if (requested) void reload()
      }
    }
    refreshCanvas.current = reload
    let stream: AbortController | undefined
    const connect = () => {
      const wanted = (!document.hidden || keepAliveRef.current) && navigator.onLine && !!canvasID
      // Kept alive through hiding; coming back still reconnects, which re-reads.
      if (wanted && stream && document.hidden) return
      stream?.abort()
      stream = undefined
      if (!wanted) return
      stream = new AbortController()
      void watchCanvasEvents(
        (signal) => api.connectCanvasEvents(canvasID, signal),
        stream.signal,
        () => {
          void reload()
          notifyCanvasInvalidate()
        },
        setSyncError,
      )
    }
    const resume = () => {
      connect()
      if (requested) void reload()
    }
    reconnect.current = connect
    connect()
    window.addEventListener('online', resume)
    window.addEventListener('offline', connect)
    document.addEventListener('visibilitychange', resume)
    return () => {
      controller.abort()
      stream?.abort()
      clearTimeout(retry)
      refreshCanvas.current = () => Promise.resolve()
      reconnect.current = () => {}
      window.removeEventListener('online', resume)
      window.removeEventListener('offline', connect)
      document.removeEventListener('visibilitychange', resume)
    }
  }, [canvasID])
  return { canvas, syncError, loading, refreshCanvas }
}
