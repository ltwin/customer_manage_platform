// SSE uses fetch so the existing Bearer authentication/refresh stays in charge.
// Notifications invalidate snapshots; reconnect always starts with an invalidation.
export async function consumeCanvasEvents(
  body: ReadableStream<Uint8Array>,
  onInvalidate: () => void,
): Promise<void> {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let event = ''
  let hasData = false
  const line = (value: string) => {
    if (!value) {
      if (hasData && event === 'invalidate') onInvalidate()
      if (hasData && event === 'unavailable')
        throw new CanvasStreamUnavailable()
      event = ''
      hasData = false
    } else if (value.startsWith('event:')) {
      event = value.slice(6).trimStart()
    } else if (value.startsWith('data:')) {
      hasData = true
    }
  }
  try {
    for (;;) {
      const { value, done } = await reader.read()
      if (done) return
      buffer += decoder.decode(value, { stream: true })
      if (buffer.length > 65536) throw new Error('画布同步事件超过大小限制')
      // Handle LF, CRLF and CR even when the delimiter spans network chunks.
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

class CanvasStreamUnavailable extends Error {}

export async function watchCanvasEvents(
  connect: (signal: AbortSignal) => Promise<Response>,
  signal: AbortSignal,
  onInvalidate: () => void,
  onError: (message: string) => void,
): Promise<void> {
  let delay = 1000
  while (!signal.aborted) {
    const attempt = new AbortController()
    const abort = () => attempt.abort()
    signal.addEventListener('abort', abort, { once: true })
    // A broken proxy can silently strand a stream: the server sends heartbeats
    // every 20s, so absence of any bytes for 60s means reconnect.
    let watchdog = setTimeout(abort, 60000)
    const touch = () => {
      clearTimeout(watchdog)
      watchdog = setTimeout(abort, 60000)
    }
    let terminal = false
    try {
      const response = await connect(attempt.signal)
      if (
        !response.ok ||
        !response.body ||
        !response.headers.get('content-type')?.includes('text/event-stream')
      ) {
        terminal = [401, 403, 404].includes(response.status)
        await response.body?.cancel()
        throw new Error(
          terminal
            ? '画布实时同步不可用，请检查登录和访问权限'
            : '画布实时同步连接中断，正在重连',
        )
      }
      const monitored = response.body.pipeThrough(
        new TransformStream<Uint8Array, Uint8Array>({
          transform(chunk, controller) {
            touch()
            controller.enqueue(chunk)
          },
        }),
      )
      await consumeCanvasEvents(monitored, () => {
        delay = 1000
        onInvalidate()
      })
    } catch (error) {
      if (error instanceof CanvasStreamUnavailable) {
        terminal = true
        onInvalidate()
      }
      if (!signal.aborted)
        onError(
          error instanceof CanvasStreamUnavailable
            ? '画布访问权限已变化，请重新打开画布'
            : error instanceof Error && !attempt.signal.aborted
              ? error.message
              : '画布实时同步连接中断，正在重连',
        )
    } finally {
      clearTimeout(watchdog)
      signal.removeEventListener('abort', abort)
      attempt.abort()
    }
    if (terminal || signal.aborted) return
    await new Promise<void>((resolve) => {
      const finish = () => {
        clearTimeout(timer)
        signal.removeEventListener('abort', finish)
        resolve()
      }
      const timer = setTimeout(finish, delay + Math.random() * delay * 0.2)
      signal.addEventListener('abort', finish, { once: true })
      if (signal.aborted) finish()
    })
    delay = Math.min(delay * 2, 30000)
  }
}
