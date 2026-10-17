import { useEffect, useRef, useState } from 'react'
import * as api from './api.ts'
import { ApiError } from '../../api/transport.ts'
import { notifyCanvasInvalidate, watchCanvasEvents } from './canvasEvents.ts'
import { newerCanvas } from './snapshot.ts'
import { errorMessage } from './queue.ts'

// Owns snapshot ordering, event subscription and retry lifetime.
export function useCanvasSnapshot(canvasID: string, tick: number) {
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
  useEffect(() => {
    const controller = new AbortController()
    let fetching = false
    let requested = false
    let retry: ReturnType<typeof setTimeout> | undefined
    let retryDelay = 1000
    const reload = async () => {
      if (controller.signal.aborted || document.hidden || !navigator.onLine || !canvasID) return
      if (fetching) {
        requested = true
        return
      }
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
        if (requested && !controller.signal.aborted) {
          requested = false
          void reload()
        }
      }
    }
    refreshCanvas.current = reload
    let stream: AbortController | undefined
    const connect = () => {
      stream?.abort()
      stream = undefined
      if (document.hidden || !navigator.onLine || !canvasID) return
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
    connect()
    window.addEventListener('online', connect)
    window.addEventListener('offline', connect)
    document.addEventListener('visibilitychange', connect)
    return () => {
      controller.abort()
      stream?.abort()
      clearTimeout(retry)
      refreshCanvas.current = () => Promise.resolve()
      window.removeEventListener('online', connect)
      window.removeEventListener('offline', connect)
      document.removeEventListener('visibilitychange', connect)
    }
  }, [canvasID])
  return { canvas, syncError, loading, refreshCanvas }
}
