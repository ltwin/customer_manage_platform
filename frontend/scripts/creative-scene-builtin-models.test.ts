import test from 'node:test'
import assert from 'node:assert/strict'
import { existsSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import type { SceneObject } from '../src/creative-scene/api.ts'
import { newModelInstance } from '../src/creative-scene/editing.ts'
import { builtinModel, builtinModels, builtinSummaries, summaryOf } from '../src/creative-scene/models/catalog.ts'
import { builtinKey, modelKey } from '../src/creative-scene/models/key.ts'
import { applyOps, type SceneState } from '../src/creative-scene/state/apply.ts'
import { namesUnknownImage } from '../src/creative-scene/state/sync.ts'
import { builtinModelThumbnail, filterBuiltinModels } from '../src/creative-canvas/assets/builtin.ts'

// The built-in bodies in the editor (scene-skinned-humanoid 6.2): placing one
// writes {builtin} with the entry's materials; swapping brings the other's.

const female = builtinModel('body_female@1')!
const male = builtinModel('body_male@1')!

function state(objects: SceneObject[]): SceneState {
  return {
    title: '场景',
    scene: {
      environment: { sky: { kind: 'procedural', sun_elevation_deg: 35, sun_azimuth_deg: 135, turbidity: 3 }, ambient: { color: '#ffffff', intensity: 0.4 }, fog: null },
      render: { mode: 'clay' },
      objects,
    },
  }
}

test('the catalog ships the two bodies, both humanoid, each summarized under its key', () => {
  assert.deepEqual(builtinModels.map((m) => m.id), ['body_female@1', 'body_male@1'])
  for (const m of builtinModels) {
    assert.equal(m.category, 'humanoid')
    assert.equal(Object.keys(m.rig?.humanoid ?? {}).length, 52, m.id)
    assert.ok(m.manifest.skeleton, m.id)
    assert.deepEqual(Object.keys(m.materials).sort(), ['body', 'brows', 'eyes'], m.id)
    assert.equal(summaryOf({}, { builtin: m.id }), builtinSummaries[builtinKey(m.id)])
  }
})

test('a placed body references the entry and carries a copy of its materials', () => {
  let n = 0
  const o = newModelInstance({ builtin: female.id }, female.label, { position: [1, 0, 2], rotation: [0, 0, 0, 1] }, [], female.materials, () => `sobj_${String(++n).padStart(21, '0')}`)
  assert.deepEqual(o.model, { builtin: 'body_female@1' })
  assert.equal(modelKey(o.model), 'builtin:body_female@1')
  assert.equal(o.name, '女素体')
  assert.deepEqual(o.material_slots, female.materials)
  assert.notEqual(o.material_slots, female.materials, 'the catalog entry is never shared')
  assert.deepEqual(o.transform.position, [1, 0, 2])
  assert.equal(o.order, 0)
})

test('swapping the body replaces the slots whole; without material_slots they stay', () => {
  const placed = newModelInstance({ builtin: female.id }, female.label, { position: [0, 0, 0], rotation: [0, 0, 0, 1] }, [], female.materials, () => 'sobj_000000000000000000001')
  const tinted = { ...placed, material_slots: { ...placed.material_slots, body: { preset: 'principled@1', params: { color: '#ff0000' } } } }
  const swapped = applyOps(state([tinted]), [{ op: 'set_instance_model', object_id: tinted.id, model: { builtin: male.id }, material_slots: male.materials, scope: 'this' }])
  const o = swapped.scene.objects[0]!
  assert.deepEqual(o.model, { builtin: 'body_male@1' })
  assert.deepEqual(o.material_slots, male.materials)

  const kept = applyOps(state([tinted]), [{ op: 'set_instance_model', object_id: tinted.id, model: { builtin: male.id }, scope: 'this' }])
  assert.deepEqual(kept.scene.objects[0]!.material_slots, tinted.material_slots)
})

test('a swap whose slots name an unknown image waits for the next read, as an update does', () => {
  const slots = { body: { preset: 'principled@1', params: {}, maps: { base_color: { content_revision_id: 'crev_unknown' } } } } as unknown as SceneObject['material_slots']
  const op = { op: 'set_instance_model', object_id: 'sobj_000000000000000000001', model: { builtin: male.id }, material_slots: slots, scope: 'this' } as const
  assert.equal(namesUnknownImage({ ops: [op] }, {}), true)
  assert.equal(namesUnknownImage({ ops: [{ ...op, material_slots: male.materials }] }, {}), false)
})

test('the library lists the bodies by name or category, each with its thumbnail', () => {
  assert.deepEqual(filterBuiltinModels('').map((m) => m.id), ['body_female@1', 'body_male@1'])
  assert.deepEqual(filterBuiltinModels('男').map((m) => m.id), ['body_male@1'])
  assert.deepEqual(filterBuiltinModels('人形').length, 2)
  assert.deepEqual(filterBuiltinModels('universal base').length, 2)
  assert.deepEqual(filterBuiltinModels('robot'), [])
  for (const m of builtinModels) {
    const file = fileURLToPath(new URL(`../src/creative-canvas/assets/builtin/models/${m.id.replace('@', '_')}.webp`, import.meta.url))
    assert.ok(existsSync(file), file)
    assert.match(builtinModelThumbnail(m.id), /builtin\/models\/body_(fe)?male_1\.webp$/)
  }
})
