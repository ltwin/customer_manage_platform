import assert from 'node:assert/strict'
import { test } from 'node:test'
import { FrameCapture } from '../src/creative-scene/screenshot.ts'

test('截图请求合并，在新帧内编码 PNG，记录实际像素尺寸', async () => {
  let redraws = 0
  const capture = new FrameCapture(() => redraws++)
  const first = capture.request()
  assert.equal(capture.request(), first)
  assert.equal(redraws, 1)
  const blob = new Blob(['pixels'], { type: 'image/png' })
  let encoded = false
  capture.afterFrame({ width: 800, height: 600, toBlob(done, type) {
    assert.equal(type, 'image/png')
    encoded = true
    done(blob)
  } })
  assert.equal(encoded, true)
  assert.deepEqual(await first, { blob, width: 800, height: 600 })
})

test('编码失败可重试；取消后迟到的编码不影响新请求', async () => {
  const capture = new FrameCapture(() => {})
  const failed = capture.request()
  capture.afterFrame({ width: 1, height: 1, toBlob(done) { done(null) } })
  await assert.rejects(failed, /截图编码失败/)
  const cancelled = capture.request()
  let finish: BlobCallback = () => {}
  capture.afterFrame({ width: 2, height: 2, toBlob(done) { finish = done } })
  capture.cancel()
  await assert.rejects(cancelled, /已取消/)
  const next = capture.request()
  finish(new Blob(['old']))
  const blob = new Blob(['new'])
  capture.afterFrame({ width: 3, height: 4, toBlob(done) { done(blob) } })
  assert.deepEqual(await next, { blob, width: 3, height: 4 })
})

test('没有新帧会超时，空视口和浏览器拒绝读取会失败', async () => {
  const capture = new FrameCapture(() => {}, 5)
  await assert.rejects(capture.request(), /超时/)
  const empty = capture.request()
  capture.afterFrame({ width: 0, height: 0, toBlob() { assert.fail() } })
  await assert.rejects(empty, /视口/)
  const denied = capture.request()
  capture.afterFrame({ width: 1, height: 1, toBlob() { throw new Error('denied') } })
  await assert.rejects(denied, /denied/)
})

test('关闭预览取消编码请求；重开后旧回调和旧取消信号不会影响新截图', async () => {
  const capture = new FrameCapture(() => {})
  const closed = new AbortController()
  const old = capture.request(closed.signal)
  let finishOld: BlobCallback = () => {}
  capture.afterFrame({ width: 800, height: 600, toBlob(done) { finishOld = done } })
  closed.abort()
  await assert.rejects(old, /已取消/)
  const reopened = new AbortController()
  const next = capture.request(reopened.signal)
  assert.notEqual(next, old)
  finishOld(new Blob(['old view']))
  const blob = new Blob(['new view'])
  capture.afterFrame({ width: 400, height: 600, toBlob(done) { done(blob) } })
  assert.deepEqual(await next, { blob, width: 400, height: 600 })
  // A completed request detaches its signal before the next request starts.
  const third = capture.request()
  reopened.abort()
  capture.afterFrame({ width: 1, height: 1, toBlob(done) { done(blob) } })
  await third
  await assert.rejects(capture.request(closed.signal), /已取消/)
})
