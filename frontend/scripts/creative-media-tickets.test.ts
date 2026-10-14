import assert from 'node:assert/strict'
import test from 'node:test'
import { setAuthenticated, setAnonymous } from '../src/auth/session.ts'
import {
  cachedMediaTicket,
  clearMediaTickets,
  invalidateMediaTicket,
  mediaTicket,
} from '../src/creative-canvas/editor/media.ts'

const now = Date.parse('2026-10-14T00:00:00Z')
function login() {
  setAuthenticated(
    { access_token: 'fixture', token_type: 'Bearer', expires_in: 600 },
    {
      id: 'fixture',
      email: 'fixture@example.invalid',
      created_at: new Date(now).toISOString(),
      timezone: 'UTC',
    },
  )
}
function response(url: string, lifetime = 120_000) {
  return Response.json({
    url,
    expires_at: new Date(Date.now() + lifetime).toISOString(),
    mime: 'image/png',
    byte_size: 100,
  })
}

test('tickets deduplicate in flight, become synchronously readable and expire early', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'], now })
  clearMediaTickets()
  login()
  t.after(clearMediaTickets)
  let calls = 0
  t.mock.method(globalThis, 'fetch', async () => response(`image-${++calls}`))
  const first = mediaTicket('r1', 'original', 'display')
  assert.equal(mediaTicket('r1', 'original', 'display'), first)
  assert.equal(cachedMediaTicket('r1', 'original'), undefined)
  await first
  assert.equal(cachedMediaTicket('r1', 'original')?.url, 'image-1')
  t.mock.timers.tick(90_000)
  assert.equal(cachedMediaTicket('r1', 'original'), undefined)
  await mediaTicket('r1', 'original', 'display')
  assert.equal(calls, 2)
  assert.equal(cachedMediaTicket('r1', 'original')?.url, 'image-2')
})

test('a response arriving after clear cannot refill or evict the replacement ticket', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'], now })
  clearMediaTickets()
  login()
  t.after(clearMediaTickets)
  let release!: (res: Response) => void
  let calls = 0
  t.mock.method(globalThis, 'fetch', () =>
    ++calls === 1
      ? new Promise<Response>((resolve) => {
          release = resolve
        })
      : Promise.resolve(response('new')),
  )
  const old = mediaTicket('r1', 'original', 'display')
  clearMediaTickets()
  await mediaTicket('r1', 'original', 'display')
  release(response('old', 40_000))
  await old
  assert.equal(cachedMediaTicket('r1', 'original')?.url, 'new')
  t.mock.timers.tick(10_001)
  assert.equal((await mediaTicket('r1', 'original', 'display')).url, 'new')
  assert.equal(calls, 2)
})

test('retry invalidation obtains a new ticket and an old failure cannot remove it', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'], now })
  clearMediaTickets()
  login()
  t.after(clearMediaTickets)
  let release!: (res: Response) => void
  let calls = 0
  t.mock.method(globalThis, 'fetch', () =>
    ++calls === 1
      ? new Promise<Response>((resolve) => {
          release = resolve
        })
      : Promise.resolve(response('retry')),
  )
  const old = mediaTicket('r1', 'original', 'display')
  invalidateMediaTicket('r1', 'original')
  await mediaTicket('r1', 'original', 'display')
  release(
    Response.json(
      { error: { code: 'temporary', message: 'failed' } },
      { status: 503 },
    ),
  )
  await assert.rejects(old)
  assert.equal((await mediaTicket('r1', 'original', 'display')).url, 'retry')
  assert.equal(calls, 2)
})

test('a new login session cannot reuse a prior session ticket for the same revision', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'], now })
  clearMediaTickets()
  login()
  t.after(clearMediaTickets)
  let calls = 0
  t.mock.method(globalThis, 'fetch', async () => response(`session-${++calls}`))
  await mediaTicket('r1', 'display', 'display')
  setAnonymous()
  assert.equal(cachedMediaTicket('r1', 'display'), undefined)
  login()
  assert.equal(cachedMediaTicket('r1', 'display'), undefined)
  assert.equal((await mediaTicket('r1', 'display', 'display')).url, 'session-2')
})
