// Shared SSE client for every realtime stream (canvas invalidation, agent run
// log, …). It uses fetch so the existing Bearer authentication/refresh stays in
// charge. The server's log is the truth: a reconnect resumes from the caller's
// cursor, and anything the stream cannot serve is answered with a snapshot.

export interface StreamEvent {
  id: string
  event: string
  data: string
}

export type StreamErrorReason = 'auth' | 'unavailable' | 'network'

export interface StreamError {
  reason: StreamErrorReason
  // A terminal error ends the watch; anything else is retried with backoff.
  terminal: boolean
}

export interface WatchStreamOptions {
  // cursor is the resume point at this (re)connect, or undefined for none.
  connect: (cursor: string | undefined, signal: AbortSignal) => Promise<Response>
  signal: AbortSignal
  cursor?: () => string | undefined
  // Returning 'snapshot' means the event cannot be merged into what the caller
  // holds; the stream is dropped and onSnapshotRequired runs before resuming.
  onEvent: (event: StreamEvent) => void | 'snapshot'
  // The cursor is outside what the server still holds (409 on connect, or
  // control.snapshot_required mid-stream). Re-read the snapshot; the watch
  // reconnects afterwards from the cursor that snapshot set.
  onSnapshotRequired?: () => void | Promise<void>
  onError?: (error: StreamError) => void
}

// One frame may carry a whole block; this bounds a misbehaving server.
const maxBufferedBytes = 1 << 20
// The server sends a heartbeat every 15s; silence for 60s means a proxy has
// stranded the stream.
const watchdogMs = 60000
const backoffMs = [1000, 2000, 4000, 8000, 15000]

export class StreamUnavailable extends Error {}

// readEventStream decodes LF, CRLF and CR even when a delimiter spans network
// chunks. Comments (heartbeats) and frames without data are not events.
export async function readEventStream(
  body: ReadableStream<Uint8Array>,
  onEvent: (event: StreamEvent) => void,
): Promise<void> {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let id = ''
  let event = ''
  let data: string[] | null = null
  const line = (value: string) => {
    if (!value) {
      if (data) onEvent({ id, event: event || 'message', data: data.join('\n') })
      id = ''
      event = ''
      data = null
    } else if (value.startsWith('id:')) {
      id = value.slice(3).trimStart()
    } else if (value.startsWith('event:')) {
      event = value.slice(6).trimStart()
    } else if (value.startsWith('data:')) {
      ;(data ??= []).push(value.slice(5).replace(/^ /, ''))
    }
  }
  try {
    for (;;) {
      const { value, done } = await reader.read()
      if (done) return
      buffer += decoder.decode(value, { stream: true })
      if (buffer.length > maxBufferedBytes) throw new Error('实时事件超过大小限制')
      let start = 0
      for (let i = 0; i < buffer.length; i++) {
        if (buffer[i] !== '\n' && buffer[i] !== '\r') continue
        if (buffer[i] === '\r' && i === buffer.length - 1) break
        line(buffer.slice(start, i))
        if (buffer[i] === '\r' && buffer[i + 1] === '\n') i++
        start = i + 1
      }
      buffer = buffer.slice(start)
    }
  } finally {
    await reader.cancel().catch(() => undefined)
    reader.releaseLock()
  }
}

class SnapshotRequired extends Error {}

export async function watchStream(options: WatchStreamOptions): Promise<void> {
  const { signal } = options
  let attempt = 0
  while (!signal.aborted) {
    const connection = new AbortController()
    const abort = () => connection.abort()
    signal.addEventListener('abort', abort, { once: true })
    let watchdog = setTimeout(abort, watchdogMs)
    const touch = () => {
      clearTimeout(watchdog)
      watchdog = setTimeout(abort, watchdogMs)
    }
    let terminal = false
    let resync = false
    try {
      const response = await options.connect(options.cursor?.(), connection.signal)
      if (response.status === 409) {
        await response.body?.cancel()
        throw new SnapshotRequired()
      }
      if (
        !response.ok ||
        !response.body ||
        !response.headers.get('content-type')?.includes('text/event-stream')
      ) {
        terminal = [401, 403, 404].includes(response.status)
        await response.body?.cancel()
        throw new Error(terminal ? 'auth' : 'network')
      }
      const monitored = response.body.pipeThrough(
        new TransformStream<Uint8Array, Uint8Array>({
          transform(chunk, controller) {
            touch()
            controller.enqueue(chunk)
          },
        }),
      )
      await readEventStream(monitored, (event) => {
        if (event.event === 'control.snapshot_required') throw new SnapshotRequired()
        if (event.event === 'unavailable') throw new StreamUnavailable()
        attempt = 0
        if (options.onEvent(event) === 'snapshot') throw new SnapshotRequired()
      })
    } catch (error) {
      if (error instanceof SnapshotRequired) {
        resync = true
      } else if (!signal.aborted) {
        const unavailable = error instanceof StreamUnavailable
        terminal ||= unavailable
        options.onError?.({
          reason: unavailable ? 'unavailable' : terminal ? 'auth' : 'network',
          terminal,
        })
      }
    } finally {
      clearTimeout(watchdog)
      signal.removeEventListener('abort', abort)
      connection.abort()
    }
    if (terminal || signal.aborted) return
    if (resync) {
      // The snapshot read decides the next cursor; failures fall back to backoff.
      await Promise.resolve(options.onSnapshotRequired?.()).catch(() => undefined)
      if (signal.aborted) return
    }
    const delay = backoffMs[Math.min(attempt, backoffMs.length - 1)]
    attempt++
    await sleep(delay + Math.random() * delay * 0.2, signal)
  }
}

function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise<void>((resolve) => {
    const finish = () => {
      clearTimeout(timer)
      signal.removeEventListener('abort', finish)
      resolve()
    }
    const timer = setTimeout(finish, ms)
    signal.addEventListener('abort', finish, { once: true })
    if (signal.aborted) finish()
  })
}
