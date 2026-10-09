import test from 'node:test'
import assert from 'node:assert/strict'
import { readEventStream, watchStream } from '../src/api/watchStream.ts'

const sse = (chunks: string[], status = 200) =>
  new Response(
    new ReadableStream<Uint8Array>({
      start(controller) {
        for (const chunk of chunks)
          controller.enqueue(new TextEncoder().encode(chunk))
        controller.close()
      },
    }),
    { status, headers: { 'Content-Type': 'text/event-stream' } },
  )

test('frames keep their id, type and multi-line data', async () => {
  const seen: string[] = []
  await readEventStream(
    sse([
      'id: 7\nevent: block.updated\ndata: {"a":\ndata: 1}\n\n',
      ': heartbeat\n\n',
      'id: 8\nevent: run.finished\ndata: {}\n\n',
    ]).body!,
    (e) => seen.push(`${e.id}|${e.event}|${e.data}`),
  )
  assert.deepEqual(seen, ['7|block.updated|{"a":\n1}', '8|run.finished|{}'])
})

test('reconnects resume from the caller cursor', async () => {
  const abort = new AbortController()
  const cursors: (string | undefined)[] = []
  let cursor = '3'
  await watchStream({
    signal: abort.signal,
    cursor: () => cursor,
    connect: async (at) => {
      cursors.push(at)
      const next = String(Number(at) + 1)
      return sse([`id: ${next}\nevent: e\ndata: {}\n\n`])
    },
    onEvent: (e) => {
      cursor = e.id
      if (cursors.length === 2) abort.abort()
    },
  })
  assert.deepEqual(cursors, ['3', '4'])
})

test('a cursor the server cannot serve asks for a snapshot, then resumes from it', async () => {
  for (const first of [
    () => new Response('{}', { status: 409 }),
    () => sse(['event: control.snapshot_required\ndata: {}\n\n']),
  ]) {
    const abort = new AbortController()
    const cursors: (string | undefined)[] = []
    let cursor = '1'
    let snapshots = 0
    const errors: string[] = []
    await watchStream({
      signal: abort.signal,
      cursor: () => cursor,
      connect: async (at) => {
        cursors.push(at)
        if (cursors.length === 1) return first()
        abort.abort()
        return sse([])
      },
      onEvent: () => {},
      onSnapshotRequired: () => {
        snapshots++
        cursor = '42'
      },
      onError: (e) => errors.push(e.reason),
    })
    assert.equal(snapshots, 1)
    assert.deepEqual(cursors, ['1', '42'])
    assert.deepEqual(errors, [], 'a resync is not an error')
  }
})
