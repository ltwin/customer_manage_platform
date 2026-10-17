import {
  readEventStream,
  StreamUnavailable,
  watchStream,
} from '../../api/watchStream.ts'

// Other views on the canvas page (the scene editor) re-read on the same
// notifications instead of opening a second stream.
const invalidationListeners = new Set<() => void>()

export function onCanvasInvalidate(listener: () => void): () => void {
  invalidationListeners.add(listener)
  return () => invalidationListeners.delete(listener)
}

export function notifyCanvasInvalidate(): void {
  for (const listener of invalidationListeners) listener()
}

// Canvas notifications invalidate snapshots; there is no event log, so every
// reconnect starts with an invalidation instead of a cursor.
export async function consumeCanvasEvents(
  body: ReadableStream<Uint8Array>,
  onInvalidate: () => void,
): Promise<void> {
  await readEventStream(body, (event) => {
    if (event.event === 'invalidate') onInvalidate()
    if (event.event === 'unavailable') throw new StreamUnavailable()
  })
}

export async function watchCanvasEvents(
  connect: (signal: AbortSignal) => Promise<Response>,
  signal: AbortSignal,
  onInvalidate: () => void,
  onError: (message: string) => void,
): Promise<void> {
  await watchStream({
    connect: (_cursor, connection) => connect(connection),
    signal,
    onEvent: (event) => {
      if (event.event === 'invalidate') onInvalidate()
    },
    onError: (error) => {
      if (error.reason === 'unavailable') {
        // Re-read once so the page shows what access is left.
        onInvalidate()
        onError('画布访问权限已变化，请重新打开画布')
        return
      }
      onError(
        error.reason === 'auth'
          ? '画布实时同步不可用，请检查登录和访问权限'
          : '画布实时同步连接中断，正在重连',
      )
    },
  })
}
