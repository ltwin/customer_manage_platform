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

test('the lights for the volumes: a casting sun first, then by the light at the volumes; at most 6 lights and 12 maps', async () => {
  const { Sphere, Vector3 } = await import('three')
  const { selectLights, MAX_LIGHTS } = await import('../src/creative-scene/render/volumes/state.ts')
  const at = (x: number) => new Vector3(x, 0, 0)
  const bounds = [new Sphere(new Vector3(0, 0, 0), 1)]
  const sun = { kind: 'directional' as const, cast: true, sun: true, intensity: 1, position: at(50), decay: 0 }
  const near = { kind: 'point' as const, cast: false, sun: false, intensity: 10, position: at(2), decay: 2 }
  const far = { kind: 'spot' as const, cast: true, sun: false, intensity: 10, position: at(11), decay: 2 }
  // The sun leads although the point light near the volume brings more; the far spot (10 / 10²) comes last.
  assert.deepEqual(selectLights([far, near, sun], bounds), [2, 1, 0])
  // Three casting directional lights take 12 maps: a fourth casting light finds no room, a non-casting one does
  // (the point light by the volume, 10 / 1², before the directional ones at 5).
  const dir = { ...sun, sun: false, intensity: 5 }
  assert.deepEqual(selectLights([sun, dir, dir, far, near], bounds), [0, 4, 1, 2])
  assert.equal(selectLights(Array.from({ length: 9 }, () => near), bounds).length, MAX_LIGHTS)
})

test('the march rebuilds on its structure only, and the cast list holds the casting volumes in order', async () => {
  const { castList, signatureOf } = await import('../src/creative-scene/render/volumes/state.ts')
  assert.equal(signatureOf([{ kind: 'directional', cast: true }, { kind: 'spot', cast: false }], true), 'directional+,spot|env')
  assert.notEqual(signatureOf([{ kind: 'spot', cast: true }], false), signatureOf([{ kind: 'spot', cast: false }], false))
  assert.deepEqual(castList([{ cast_shadow: false }, { cast_shadow: true }, { cast_shadow: true }]), [1, 2])
})

test('the slots go by kind, casting first, then in the scene\'s order: lights swapping places at the volumes build no march', async () => {
  const { signatureOf, slotOrder } = await import('../src/creative-scene/render/volumes/state.ts')
  const lights = [
    { kind: 'point' as const, cast: true },
    { kind: 'spot' as const, cast: false },
    { kind: 'spot' as const, cast: true },
    { kind: 'directional' as const, cast: false },
  ]
  assert.deepEqual(slotOrder(lights, [0, 1, 2, 3]), [3, 2, 1, 0])
  // Whichever brings more light to the volumes, the slots are the same.
  const signature = (chosen: number[]) => signatureOf(slotOrder(lights, chosen).map((i) => lights[i]), false)
  assert.equal(signature([0, 2]), signature([2, 0]))
  assert.equal(signature([0, 2]), 'spot+,point+|')
})

test('the crossings are sorted by the network for every order of 8', async () => {
  const { SORT8 } = await import('../src/creative-scene/render/volumes/march.ts')
  // Every 0/1 input sorts, so every input does (the 0-1 principle).
  for (let bits = 0; bits < 256; bits++) {
    const v = Array.from({ length: 8 }, (_, i) => (bits >> i) & 1)
    for (const [x, y] of SORT8) if (v[y] < v[x]) [v[x], v[y]] = [v[y], v[x]]
    assert.deepEqual(v, [...v].sort((a, b) => a - b), `input ${bits.toString(2)}`)
  }
})

test('a new volume from the add menu: 2 m, standing on the point the view looks at, its fields left to the defaults', async () => {
  const { newVolume } = await import('../src/creative-scene/editing.ts')
  const v = newVolume('sphere', [1, 0.5, -2], [volume({})], () => 'sobj_000000000000000000002')
  assert.deepEqual(v.transform, { position: [1, 1.5, -2], rotation: [0, 0, 0, 1], scale: [2, 2, 2] })
  assert.equal(v.kind, 'volume')
  assert.equal(v.name, '体积 2')
  assert.deepEqual(v.volume, { shape: 'sphere' })
})
