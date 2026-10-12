import test from 'node:test'
import assert from 'node:assert/strict'
import {
  shapeOf,
  colorValue,
} from '../src/creative-canvas/assets/appearance.ts'

test('shape shares the server 1.1 boundary, with no classification for missing dimensions', () => {
  assert.equal(shapeOf(110, 100), 'square')
  assert.equal(shapeOf(100, 110), 'square')
  assert.equal(shapeOf(111, 100), 'landscape')
  assert.equal(shapeOf(100, 111), 'portrait')
  assert.equal(shapeOf(100, null), '')
  assert.equal(shapeOf(0, 100), '')
})
test('colors accept exactly six hex digits and normalize casing', () => {
  assert.equal(colorValue('#AABBCC'), '#aabbcc')
  for (const value of ['red', '#abc', '#11223344', '#12345g', null])
    assert.equal(colorValue(value), '')
})
