import assert from 'node:assert/strict'
import { mock, test } from 'node:test'
import { setAnonymous } from '../src/auth/session.ts'
import {
  invalidateReads,
  readCache,
  readStamp,
} from '../src/creative-canvas/editor/readCache.ts'

test('a cached list read never survives a session change', () => {
  const cache = readCache<string[]>()
  cache.set('/projects?limit=5', ['p1'], readStamp())
  assert.deepEqual(cache.get('/projects?limit=5'), ['p1'])
  setAnonymous()
  assert.equal(cache.get('/projects?limit=5'), undefined)
})

test('the cache drops the least recently written path past its limit', () => {
  const cache = readCache<number>(2)
  cache.set('/a', 1, readStamp())
  cache.set('/b', 2, readStamp())
  cache.set('/a', 3, readStamp())
  cache.set('/c', 4, readStamp())
  assert.equal(cache.get('/b'), undefined)
  assert.equal(cache.get('/a'), 3)
  assert.equal(cache.get('/c'), 4)
})

test('an own write hides every earlier list, so new work never seems missing', () => {
  const cache = readCache<string>()
  cache.set('/assets', 'before', readStamp())
  invalidateReads()
  assert.equal(cache.get('/assets'), undefined)
})

test('a read that overlapped a write is not cached', () => {
  const cache = readCache<string>()
  const stamp = readStamp()
  invalidateReads()
  cache.set('/assets', 'read before the write landed', stamp)
  assert.equal(cache.get('/assets'), undefined)
})

test('a list older than five minutes is not shown', () => {
  mock.timers.enable({ apis: ['Date'], now: 0 })
  try {
    const cache = readCache<string>()
    cache.set('/projects', 'old', readStamp())
    mock.timers.tick(4 * 60_000)
    assert.equal(cache.get('/projects'), 'old')
    mock.timers.tick(60_000)
    assert.equal(cache.get('/projects'), undefined)
  } finally {
    mock.timers.reset()
  }
})
