import assert from 'node:assert/strict'
import test from 'node:test'
import { ViewerImageCache } from '../src/creative-canvas/viewer/imageCache.ts'

test('viewer image bytes are reused and all object URLs are revoked on close', async (t) => {
  const created: string[] = [],
    revoked: string[] = []
  let reads = 0
  t.mock.method(URL, 'createObjectURL', () => {
    const url = `blob:${created.length}`
    created.push(url)
    return url
  })
  t.mock.method(URL, 'revokeObjectURL', (url: string) => revoked.push(url))
  t.mock.method(globalThis, 'fetch', async () => {
    reads++
    return new Response('image')
  })
  const cache = new ViewerImageCache()
  const first = await cache.load('/signed-image', new AbortController())
  first.release()
  const again = await cache.load('/signed-image', new AbortController())
  assert.equal(first.url, again.url)
  assert.equal(reads, 1)
  assert.equal(cache.peek('/signed-image'), first.url)
  cache.clear()
  assert.deepEqual(revoked, created)
  assert.equal(cache.peek('/signed-image'), undefined)
  again.release()
})

test('budget evicts oldest unused images, and oversized current images leave on release', async (t) => {
  let next = 0
  const revoked: string[] = []
  t.mock.method(URL, 'createObjectURL', () => `blob:${++next}`)
  t.mock.method(URL, 'revokeObjectURL', (url: string) => revoked.push(url))
  t.mock.method(globalThis, 'fetch', async () => new Response('1234'))
  const cache = new ViewerImageCache(8, 2)
  for (const source of ['a', 'b', 'c']) {
    const lease = await cache.load(source, new AbortController())
    lease.release()
  }
  assert.equal(cache.peek('a'), undefined)
  assert(cache.peek('b'))
  assert(cache.peek('c'))
  assert.deepEqual(revoked, ['blob:1'])
  cache.clear()
  const small = new ViewerImageCache(2)
  const large = await small.load('large', new AbortController())
  assert.equal(small.peek('large'), large.url)
  large.release()
  assert.equal(small.peek('large'), undefined)
})

test('switching away or closing cancels pending reads without retaining a late blob', async (t) => {
  let release!: (value: Response) => void
  let created = 0
  t.mock.method(
    globalThis,
    'fetch',
    () =>
      new Promise<Response>((resolve) => {
        release = resolve
      }),
  )
  t.mock.method(URL, 'createObjectURL', () => {
    created++
    return 'blob:late'
  })
  const cache = new ViewerImageCache()
  const controller = new AbortController()
  const reading = cache.load('late', controller)
  cache.clear()
  release(new Response('late'))
  await assert.rejects(reading, { name: 'AbortError' })
  assert.equal(created, 0)
  assert.equal(cache.peek('late'), undefined)
})

test('A → close → B → close → A reuses bytes until the ticket expires', async (t) => {
  t.mock.timers.enable({ apis: ['Date'], now: 1000 })
  let reads = 0
  const revoked: string[] = []
  t.mock.method(globalThis, 'fetch', async () => {
    reads++
    return new Response('image')
  })
  t.mock.method(URL, 'createObjectURL', () => `blob:${reads}`)
  t.mock.method(URL, 'revokeObjectURL', (url: string) => revoked.push(url))
  const cache = new ViewerImageCache()
  t.after(() => cache.clear())
  const a = await cache.load('a', new AbortController(), 2000)
  a.release()
  const b = await cache.load('b', new AbortController(), 3000)
  b.release()
  const again = await cache.load('a', new AbortController(), 2000)
  assert.equal(again.url, a.url)
  assert.equal(reads, 2)
  again.release()
  t.mock.timers.setTime(2001)
  assert.equal(cache.peek('a'), undefined)
  assert.equal(cache.peek('b'), b.url)
  assert.deepEqual(revoked, [a.url])
  await assert.rejects(cache.load('a', new AbortController(), 2000))
  assert.equal(reads, 2, 'an expired ticket must not start another download')
})

test('a ticket expiring during download cannot populate the cache', async (t) => {
  t.mock.timers.enable({ apis: ['Date'], now: 1000 })
  let resolve!: (value: Response) => void
  let created = 0
  t.mock.method(globalThis, 'fetch', () => new Promise<Response>((r) => { resolve = r }))
  t.mock.method(URL, 'createObjectURL', () => { created++; return 'blob:expired' })
  const cache = new ViewerImageCache()
  const pending = cache.load('a', new AbortController(), 2000)
  t.mock.timers.setTime(2001)
  resolve(new Response('image'))
  await assert.rejects(pending)
  assert.equal(created, 0)
  assert.equal(cache.peek('a'), undefined)
})
