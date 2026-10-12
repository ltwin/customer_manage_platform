import assert from 'node:assert/strict'
import test from 'node:test'
import {
  clickSelection,
  boxSelection,
  halfRating,
  stepRating,
} from '../src/creative-canvas/assets/selection.ts'
import { placement } from '../src/creative-canvas/assets/placement.ts'

test('selection follows visible order, preserves range anchor and toggles modifiers', () => {
  const order = ['a', 'b', 'c', 'd']
  const one = clickSelection({ ids: [], anchor: null }, 'b', order, {})
  assert.deepEqual(one, { ids: ['b'], anchor: 'b' })
  assert.deepEqual(clickSelection(one, 'd', order, { shift: true }), {
    ids: ['b', 'c', 'd'],
    anchor: 'b',
  })
  assert.deepEqual(clickSelection(one, 'a', order, { additive: true }).ids, [
    'b',
    'a',
  ])
  assert.deepEqual(clickSelection(one, 'b', order, { additive: true }).ids, [])
  assert.deepEqual(
    clickSelection({ ids: ['gone'], anchor: 'gone' }, 'c', order, {
      shift: true,
    }).ids,
    ['c'],
  )
  const many = Array.from({ length: 110 }, (_, i) => String(i))
  assert.equal(
    clickSelection({ ids: ['0'], anchor: '0' }, '109', many, { shift: true })
      .ids.length,
    110,
  )
})
test('marquee intersects cards and additive selection retains its base', () => {
  const cards = [
    { id: 'a', left: 0, top: 0, right: 20, bottom: 20 },
    { id: 'b', left: 30, top: 0, right: 50, bottom: 20 },
  ]
  assert.deepEqual(
    boxSelection([], cards, { left: 10, top: 5, right: 25, bottom: 15 }),
    ['a'],
  )
  assert.deepEqual(
    boxSelection(['b'], cards, { left: 10, top: 5, right: 25, bottom: 15 }),
    ['b', 'a'],
  )
})
test('rating halves, keyboard bounds and clearing use nullable ratings', () => {
  assert.equal(halfRating(3, 0.49), 2.5)
  assert.equal(halfRating(3, 0.5), 3)
  assert.equal(stepRating(null, 1), 0.5)
  assert.equal(stepRating(0.5, -1), null)
  assert.equal(stepRating(5, 1), 5)
})
test('placement uses world bounds of nested nodes and origin for empty canvas', () => {
  assert.deepEqual(placement([]), { x: 0, y: 0 })
  assert.deepEqual(
    placement([
      {
        id: 'p',
        parent_id: null,
        metadata: { x: 100, y: -50, width: 300, height: 200 },
      },
      {
        id: 'c',
        parent_id: 'p',
        metadata: { x: 400, y: 30, width: 100, height: 80 },
      },
    ]),
    { x: 640, y: -50 },
  )
})
