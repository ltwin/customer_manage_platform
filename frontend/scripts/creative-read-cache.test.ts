import assert from 'node:assert/strict'
import { test } from 'node:test'
import { setAnonymous } from '../src/auth/session.ts'
import { readCache } from '../src/creative-canvas/editor/readCache.ts'

test('a cached list read never survives a session change', () => {
  const cache = readCache<string[]>()
  cache.set('/projects?limit=5', ['p1'])
  assert.deepEqual(cache.get('/projects?limit=5'), ['p1'])
  setAnonymous()
  assert.equal(cache.get('/projects?limit=5'), undefined)
})

test('the cache drops the least recently written path past its limit', () => {
  const cache = readCache<number>(2)
  cache.set('/a', 1)
  cache.set('/b', 2)
  cache.set('/a', 3)
  cache.set('/c', 4)
  assert.equal(cache.get('/b'), undefined)
  assert.equal(cache.get('/a'), 3)
  assert.equal(cache.get('/c'), 4)
})
