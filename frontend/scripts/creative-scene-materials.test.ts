import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { completeParams, hintPreset, materialCatalog, materialEntry, principledID } from '../src/creative-scene/materials/catalog.ts'

test('the frontend catalog is the backend one, copied by make generate', () => {
  const backend = readFileSync(new URL('../../backend/internal/creativescene/materials.json', import.meta.url), 'utf8')
  const frontend = readFileSync(new URL('../src/creative-scene/materials.gen.json', import.meta.url), 'utf8')
  assert.equal(frontend, backend)
  assert.equal(materialCatalog.materials.length, 15, 'principled and 14 presets')
  assert.equal(Object.keys(materialEntry(principledID)!.params).length, 20)
})

test('slots read complete: defaults fill in, numbers clamp into range, bad colours fall back', () => {
  const brick = materialEntry('stone_brick@1')!
  assert.deepEqual(completeParams(brick, { brick_size: 9, mortar_color: 'grey', roughness: 0.4 }), {
    color: '#8f8676',
    brick_size: 2,
    mortar_color: '#bdb5a6',
    roughness: 0.4,
    metalness: 0,
  })
  assert.equal(completeParams(materialEntry(principledID)!, { ior: 0.5 }).ior, 1)
})

test('slot hints map to presets whole, ignoring case and spaces', () => {
  assert.equal(hintPreset(' Stone '), 'stone@1')
  assert.equal(hintPreset('brick'), 'stone_brick@1')
  assert.equal(hintPreset('old stone'), undefined)
  assert.equal(hintPreset(null), undefined)
})
