import test from 'node:test'
import assert from 'node:assert/strict'
import {
  consumeCanvasEvents,
  watchCanvasEvents,
} from '../src/creative-canvas/editor/canvasEvents.ts'

const stream = (chunks: string[]) =>
  new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks)
        controller.enqueue(new TextEncoder().encode(chunk))
      controller.close()
    },
  })

test('SSE decodes fragmented delimiters and ignores heartbeats/unknown/incomplete events', async () => {
  let invalidations = 0
  await consumeCanvasEvents(
    stream([
      ': heartbeat\r',
      '\n\r',
      '\nevent: invali',
      'date\r\ndata: {}\r',
      '\n\r',
      '\n',
      'event: unrelated\ndata: {}\n\n',
      'event: invalidate\ndata: {}\n\n',
      'event: invalidate\ndata: {}',
    ]),
    () => {
      invalidations++
    },
  )
  assert.equal(invalidations, 2)
})

test('SSE reconnects after EOF and resyncs instead of assuming event replay', async () => {
  const abort = new AbortController()
  let calls = 0
  let invalidations = 0
  await watchCanvasEvents(
    async () => {
      calls++
      return new Response(stream(['event: invalidate\ndata: {}\n\n']), {
        headers: { 'Content-Type': 'text/event-stream' },
      })
    },
    abort.signal,
    () => {
      if (++invalidations === 2) abort.abort()
    },
    () => {},
  )
  assert.equal(calls, 2)
  assert.equal(invalidations, 2)
})

test('SSE stops retrying terminal auth errors and revocation events', async () => {
  for (const status of [401, 403, 404, 200]) {
    let calls = 0
    let invalidations = 0
    const errors: string[] = []
    await watchCanvasEvents(
      async () => {
        calls++
        return new Response(stream(['event: unavailable\ndata: {}\n\n']), {
          status,
          headers: { 'Content-Type': 'text/event-stream' },
        })
      },
      new AbortController().signal,
      () => {
        invalidations++
      },
      (error) => errors.push(error),
    )
    assert.equal(calls, 1)
    assert.equal(invalidations, status === 200 ? 1 : 0)
    assert.equal(errors.length, 1)
  }
})

test('SSE cancels an active connection when the canvas unmounts', async () => {
  const abort = new AbortController()
  let connectionSignal: AbortSignal | undefined
  const running = watchCanvasEvents(
    async (signal) => {
      connectionSignal = signal
      return new Response(
        new ReadableStream<Uint8Array>({
          start(controller) {
            signal.addEventListener(
              'abort',
              () => controller.error(new Error('aborted')),
              { once: true },
            )
            queueMicrotask(() => abort.abort())
          },
        }),
        { headers: { 'Content-Type': 'text/event-stream' } },
      )
    },
    abort.signal,
    () => {},
    () => {
      assert.fail('unmount must be silent')
    },
  )
  await running
  assert.equal(connectionSignal?.aborted, true)
})
