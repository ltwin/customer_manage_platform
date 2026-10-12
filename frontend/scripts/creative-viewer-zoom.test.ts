import test from 'node:test'
import assert from 'node:assert/strict'
import {
  clampPan,
  zoomAt,
  wheelZoom,
} from '../src/creative-canvas/viewer/zoom.ts'

test('viewer zoom preserves the pointer anchor and clamps scale and pan to visible bounds', () => {
  const bounds = { width: 800, height: 600, imageWidth: 800, imageHeight: 600 }
  assert.deepEqual(zoomAt({ s: 1, x: 0, y: 0 }, 2, { x: 100, y: 50 }, bounds), {
    s: 2,
    x: -100,
    y: -50,
  })
  assert.deepEqual(clampPan({ s: 2, x: 999, y: -999 }, bounds), {
    s: 2,
    x: 400,
    y: -300,
  })
  assert.deepEqual(
    zoomAt({ s: 4, x: 200, y: 100 }, 0.5, { x: 100, y: 50 }, bounds),
    { s: 1, x: 0, y: 0 },
  )
  assert.equal(zoomAt({ s: 1, x: 0, y: 0 }, 20, { x: 0, y: 0 }, bounds).s, 8)
  assert.deepEqual(
    clampPan({ s: 2, x: 999, y: -999 }, { ...bounds, imageWidth: 200 }),
    { s: 2, x: 0, y: -300 },
  )
})
test('viewer distinguishes mouse wheel / pinch from trackpad panning using the prototype rule', () => {
  assert(wheelZoom({ ctrlKey: false, deltaX: 0, deltaY: -100 })! > 1)
  assert(wheelZoom({ ctrlKey: true, deltaX: 1, deltaY: 0.5 })! < 1)
  assert.equal(wheelZoom({ ctrlKey: false, deltaX: 0, deltaY: 2.5 }), null)
  assert.equal(wheelZoom({ ctrlKey: false, deltaX: 1, deltaY: 100 }), null)
})
