import test from 'node:test'
import assert from 'node:assert/strict'
import type { Scene, SceneObject } from '../src/creative-scene/api.ts'
import { selectShadowCasters } from '../src/creative-scene/objects/shadowBudget.ts'

// objects/shadowBudget.ts: the same cases as the backend's
// creativescene/shadows_internal_test.go, which read_scene answers from.

type Light = NonNullable<SceneObject['light']>
const transform = { position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] }
const light = (id: string, type: Light['type'], cast_shadow: boolean | undefined, more: Partial<SceneObject> = {}): SceneObject =>
  ({ id, kind: 'light', name: id, parent_id: null, order: 0, transform, visible: true, locked: false, light: { type, ...(cast_shadow === undefined ? {} : { cast_shadow }) }, ...more }) as SceneObject
const group = (id: string, visible: boolean): SceneObject => ({ id, kind: 'group', name: id, parent_id: null, order: 0, transform, visible, locked: false }) as SceneObject
const sceneOf = (sky: string, objects: SceneObject[]) => ({ environment: { sky: { kind: sky } }, objects }) as unknown as Pick<Scene, 'environment' | 'objects'>
const states = (s: ReturnType<typeof sceneOf>) => Object.fromEntries(selectShadowCasters(s))

test('the sky\'s sun takes four maps, then lights in object-list order, each while it fits', () => {
  const objects = [
    light('spot', 'spot', true),
    light('key', 'directional', true),
    light('fill', 'point', false),
    light('rim', 'area', true),
    light('bulb', 'point', true),
    light('plain', 'point', undefined),
    { id: 'box', kind: 'primitive', name: 'box', parent_id: null, order: 0, transform, visible: true, locked: false } as SceneObject,
  ]
  const rest = { spot: 'on', fill: 'off', rim: 'on', bulb: 'on', plain: 'off' }
  assert.deepEqual(states(sceneOf('procedural', objects)), { ...rest, key: 'over_budget' })
  assert.deepEqual(states(sceneOf('physical', objects)), { ...rest, key: 'over_budget' })
  assert.deepEqual(states(sceneOf('color', objects)), { ...rest, key: 'on' })
  const many = [light('a', 'directional', true), light('b', 'directional', true), light('c', 'point', true)]
  assert.deepEqual(states(sceneOf('color', many)), { a: 'on', b: 'on', c: 'over_budget' }, 'two directionals fill the maps')
})

test('a hidden light, or one in a hidden group, casts nothing and leaves its maps to the next', () => {
  const objects = [
    light('hidden', 'directional', true, { visible: false }),
    light('boxed', 'directional', true, { parent_id: 'shut' }),
    group('shut', false),
    group('open', true),
    light('key', 'directional', true, { parent_id: 'open' }),
  ]
  assert.deepEqual(states(sceneOf('procedural', objects)), { hidden: 'off', boxed: 'off', key: 'on' })
})

test('moving or turning a light, or the view, picks the same lights', () => {
  const objects = [light('a', 'spot', true), light('b', 'directional', true), light('c', 'point', true)]
  const before = states(sceneOf('procedural', objects))
  const moved = objects.map((o, i) => ({ ...o, transform: { position: [i * 3, 2, -1], rotation: [0.2, 0.1, 0, 0.97], scale: [1, 1, 1] } }))
  assert.deepEqual(states(sceneOf('procedural', moved)), before)
  assert.deepEqual(before, { a: 'on', b: 'over_budget', c: 'on' })
})
