import test from 'node:test'
import assert from 'node:assert/strict'
import { fitViewport, nodeBounds, panelInsets, validViewport, zoomLimits } from '../src/creative-canvas/editor/viewport.ts'
import type { CanvasNode } from '../src/creative-canvas/editor/api.ts'

const n = (id: string, x: number, y: number, w: number, h: number, parent: string | null = null) =>
  ({ id, parent_id: parent, metadata: { type_key: 'core.text', type_version: 1, title: id, intent: '', x, y, width: w, height: h, z_order: 0 } }) as CanvasNode

test('zoom range is 10%–400%', () => {
  assert.deepEqual(zoomLimits, { min: 0.1, max: 4 })
  assert.equal(validViewport({ x: 0, y: 0, zoom: 0.1 }), true)
  assert.equal(validViewport({ x: 0, y: 0, zoom: 4 }), true)
  assert.equal(validViewport({ x: 0, y: 0, zoom: 4.01 }), false)
  assert.equal(validViewport({ x: 0, y: 0, zoom: 0.09 }), false)
  assert.equal(validViewport({ x: Number.NaN, y: 0, zoom: 1 }), false)
  assert.equal(validViewport({ x: '1', y: 0, zoom: 1 }), false)
  assert.equal(validViewport(null), false)
})

test('bounds use roots only and include the 28px label', () => {
  assert.equal(nodeBounds([]), null)
  const b = nodeBounds([n('a', 0, 100, 300, 200), n('g', 500, 0, 400, 300), n('c', 10, 10, 9999, 9999, 'g')])
  assert.deepEqual(b, { x: 0, y: -28, width: 900, height: 328 })
})

test('fit centres in the visible area and clamps zoom to 20%–110%', () => {
  const stage = { width: 1600, height: 1000 }
  const none = { left: 80, right: 40, top: 90, bottom: 90 }
  // 小内容：不超过 110%。
  const small = fitViewport({ x: 0, y: 0, width: 100, height: 100 }, stage, none)
  assert.equal(small.zoom, 1.1)
  assert.equal(small.x + 50 * 1.1, 80 + (1600 - 120) / 2, 'horizontally centred in the visible area')
  // 超大内容：不低于 20%。
  assert.equal(fitViewport({ x: 0, y: 0, width: 100000, height: 100 }, stage, none).zoom, 0.2)
  // 打开资产库：左侧可见区起点右移，内容整体落在面板右侧。
  const library = { ...none, left: 432 }
  const v = fitViewport({ x: 0, y: 0, width: 2000, height: 800 }, stage, library)
  assert.ok(v.x >= 432 - 0.5, `content starts right of the drawer (x=${v.x})`)
  assert.ok(v.x + 2000 * v.zoom <= 1600 - 40 + 0.5, 'content ends before the right margin')
})

test('panel insets follow the open drawers', () => {
  const stage = { left: 0, right: 1600, top: 0, bottom: 1000, width: 1600, height: 1000 } as DOMRect
  assert.deepEqual(panelInsets(stage, null, null), { left: 80, right: 40, top: 90, bottom: 90 })
  const lib = { left: 12, right: 412 } as DOMRect
  const agent = { left: 1192, right: 1584 } as DOMRect
  assert.deepEqual(panelInsets(stage, lib, agent), { left: 428, right: 424, top: 90, bottom: 90 })
  const narrow = { ...stage, width: 700, right: 700 } as DOMRect
  assert.deepEqual(panelInsets(narrow, lib, agent), { left: 16, right: 16, top: 72, bottom: 80 })
})
