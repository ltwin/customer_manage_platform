import test from 'node:test'
import assert from 'node:assert/strict'
import { saveStatus } from '../src/creative-canvas/editor/saveStatus.ts'
import { SaveQueue } from '../src/creative-canvas/editor/queue.ts'
import { emptyJournal } from '../src/creative-canvas/editor/journal.ts'
import type { Job, Journal } from '../src/creative-canvas/editor/journal.ts'
import type { Intent } from '../src/creative-canvas/editor/outbox.ts'

const intent = (state: Intent['state'], canvasID = 'c1'): Intent => ({
  id: `${canvasID}-${state}`,
  canvasID,
  kind: 'actions',
  actions: [],
  state,
})
const job = (over: Partial<Job> = {}): Job => ({
  path: '/canvases/c1/commands',
  body: '{}',
  operation: 'op',
  state: 'ready',
  message: '',
  ...over,
})
const base = {
  canvasID: 'c1',
  outbox: [] as Intent[],
  job: null as Job | null,
  busy: false,
  online: true,
  comparing: false,
  confirmedOnce: false,
}

test('no edits yet shows nothing; never claims saved before a receipt', () => {
  assert.equal(saveStatus(base), 'idle')
  assert.equal(saveStatus({ ...base, outbox: [intent('pending')] }), 'saving')
  assert.equal(saveStatus({ ...base, outbox: [intent('sent')] }), 'saving')
  assert.equal(
    saveStatus({ ...base, outbox: [intent('confirmed'), intent('pending')], confirmedOnce: true }),
    'saving',
    'an older receipt does not cover a newer pending edit',
  )
  assert.equal(saveStatus({ ...base, job: job(), busy: true }), 'saving')
  assert.equal(saveStatus({ ...base, confirmedOnce: true }), 'saved')
})

test('other canvases and non-canvas requests do not affect this canvas', () => {
  assert.equal(saveStatus({ ...base, outbox: [intent('pending', 'c2')] }), 'idle')
  assert.equal(
    saveStatus({ ...base, job: job({ path: '/library/assets' }), busy: true }),
    'idle',
  )
})

test('offline keeps local edits and says so; idle stays quiet', () => {
  assert.equal(saveStatus({ ...base, online: false }), 'idle')
  assert.equal(
    saveStatus({ ...base, online: false, outbox: [intent('pending')] }),
    'offline',
  )
  assert.equal(
    saveStatus({ ...base, online: false, job: job({ state: 'unknown' }) }),
    'offline',
  )
})

test('conflicts and refusals never read as saved', () => {
  assert.equal(
    saveStatus({ ...base, comparing: true, confirmedOnce: true }),
    'conflict',
  )
  assert.equal(
    saveStatus({ ...base, job: job({ state: 'rejected', conflict: true }), confirmedOnce: true }),
    'conflict',
  )
  assert.equal(
    saveStatus({ ...base, online: false, job: job({ state: 'rejected', conflict: true }) }),
    'conflict',
  )
  assert.equal(
    saveStatus({ ...base, job: job({ state: 'rejected' }), confirmedOnce: true }),
    'rejected',
  )
  assert.equal(
    saveStatus({ ...base, job: job({ state: 'unknown' }), confirmedOnce: true }),
    'unconfirmed',
  )
  assert.equal(
    saveStatus({ ...base, job: job({ state: 'unknown' }), busy: true }),
    'saving',
    'a resend in flight is still saving',
  )
})

test('queue marks revision conflicts on the rejected job only', async () => {
  const run = async (code: string) => {
    let saved: Journal = emptyJournal()
    const queue = new SaveQueue(
      { load: async () => structuredClone(saved), save: async (v) => { saved = structuredClone(v) } },
      {
        lookup: async () => { throw Object.assign(new Error('404'), { status: 404 }) },
        send: async () => { throw Object.assign(new Error(code), { status: 409, code }) },
      },
      () => {},
    )
    await queue.load()
    await queue.enqueue('/canvases/c1/commands', {})
    return saved.job
  }
  const conflict = await run('creative_revision_conflict')
  assert.equal(conflict?.state, 'rejected')
  assert.equal(conflict?.conflict, true)
  const other = await run('archived_read_only')
  assert.equal(other?.state, 'rejected')
  assert.equal(other?.conflict, undefined)
})
