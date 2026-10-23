import test from 'node:test'
import assert from 'node:assert/strict'
import { chordFactor, coefficients } from '../src/creative-scene/render/volumes/medium.ts'
import { applyOp, completeObject, mergeVolume, volumeDefaults } from '../src/creative-scene/state/apply.ts'
import type { SceneObject } from '../src/creative-scene/api.ts'

// W5 (scene-volumes design「验证与交付」): the JS side of volume objects.

const near = (a: number, b: number, eps = 1e-6) => Math.abs(a - b) < eps

test('coefficients are the densities times the linear colours, as Blender', () => {
  const c = coefficients({ ...volumeDefaults, scatter_color: '#ffffff', scatter_density: 2, absorption_color: '#ff0000', absorption_density: 3 })
  assert.deepEqual(c.scatter, [2, 2, 2])
  assert.deepEqual(c.absorb, [0, 3, 3])
  assert.deepEqual(c.extinction, [2, 5, 5])
  // #cccccc is 0.6038 in linear light, not 0.8.
  const grey = coefficients({ ...volumeDefaults, scatter_density: 1 })
  assert.ok(near(grey.scatter[0], 0.603827, 1e-5), `${grey.scatter[0]}`)
  assert.deepEqual(coefficients({ ...volumeDefaults, absorption_density: 0 }).absorb, [0, 0, 0])
})

test('the chord factor is the mean density from the edge to the centre', () => {
  assert.equal(chordFactor({ ...volumeDefaults, edge_falloff: 0 }), 1)
  assert.equal(chordFactor({ ...volumeDefaults, edge_falloff: 1 }), 0.5)
})

const volume = (fields: SceneObject['volume']): SceneObject => ({
  id: 'sobj_V1o2l3u4m5e6F7o8g9x0q', kind: 'volume', name: '', parent_id: null, order: 1,
  transform: { position: [0, 1, 0], rotation: [0, 0, 0, 1], scale: [2, 2, 2] }, visible: true, locked: false, volume: fields,
})

test('a volume added with some fields is completed, and an update merges by key, as the server', () => {
  assert.deepEqual(completeObject(volume({ scatter_density: 0.3 })).volume, { ...volumeDefaults, scatter_density: 0.3 })
  assert.deepEqual(mergeVolume({ ...volumeDefaults, anisotropy: 0.6 }, { cast_shadow: true }), { ...volumeDefaults, anisotropy: 0.6, cast_shadow: true })
  const state = { title: '', scene: { environment: {}, render: {}, objects: [] } } as unknown as Parameters<typeof applyOp>[0]
  const added = applyOp(state, { op: 'add_objects', objects: [volume({ anisotropy: 0.6 })] })
  const updated = applyOp(added, { op: 'update_objects', updates: [{ object_id: 'sobj_V1o2l3u4m5e6F7o8g9x0q', volume: { edge_falloff: 0 } }] })
  assert.deepEqual(updated.scene.objects[0].volume, { ...volumeDefaults, anisotropy: 0.6, edge_falloff: 0 })
})
