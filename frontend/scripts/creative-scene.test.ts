import test from 'node:test'
import assert from 'node:assert/strict'
import { Box3 } from 'three'
import type { SceneObject } from '../src/creative-scene/api.ts'
import { builtins } from '../src/creative-scene/builtins/catalog.ts'
import {
  geometryBuilders,
  primitiveGeometry,
} from '../src/creative-scene/builtins/geometry.ts'
import {
  applyOps,
  applyPatch,
  type SceneState,
} from '../src/creative-scene/state/apply.ts'
import {
  dependents,
  enqueue,
  expectedFor,
  project,
  resolveAfter,
  sendable,
  touchedUnits,
  type Intent,
} from '../src/creative-scene/state/intents.ts'

const id = (n: number) => `sobj_${String(n).padStart(21, '0')}`
const identity = { position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] }
const obj = (n: number, kind: SceneObject['kind'], parent: string | null, order: number, extra: Partial<SceneObject> = {}): SceneObject => ({
  id: id(n),
  kind,
  name: '',
  parent_id: parent,
  order,
  transform: identity,
  visible: true,
  locked: false,
  ...extra,
})
const box = (n: number, parent: string | null = null, order = n) =>
  obj(n, 'primitive', parent, order, { primitive: { builtin: 'box@1', params: { width: 1, height: 1, depth: 1 } } })

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

let seq = 0
function intent(ops: Intent['ops'], extra: Partial<Intent> = {}): Intent {
  seq++
  return { id: `i${seq}`, createdAt: '2026-10-16T00:00:00Z', ops, expected: {}, state: 'pending', label: '编辑', ...extra }
}
const moveTo = (ids: string[], x: number) => ({
  op: 'update_objects' as const,
  updates: ids.map((object_id) => ({ object_id, transform: { ...identity, position: [x, 0, 0] } })),
})

test('every catalog builtin has geometry and every geometry is in the catalog', () => {
  assert.deepEqual(
    builtins.map((b) => b.builtin).sort(),
    Object.keys(geometryBuilders).sort(),
  )
  for (const b of builtins) {
    const g = primitiveGeometry(b.builtin, {})
    assert.ok(g, b.builtin)
    const bounds = new Box3().setFromBufferAttribute(g.getAttribute('position') as never)
    assert.ok(bounds.max.x > bounds.min.x, `${b.builtin} has width`)
    // Floor mounts stand on the ground; the wall plate starts at the floor too.
    if (b.mount === 'floor' || b.mount === 'wall')
      assert.ok(Math.abs(bounds.min.y) < 1e-6, `${b.builtin} min y ${bounds.min.y}`)
    g.dispose()
  }
})

test('geometry follows its parameters and shrinks an oversized arch opening', () => {
  const tall = primitiveGeometry('box@1', { height: 3 })!
  assert.equal(new Box3().setFromBufferAttribute(tall.getAttribute('position') as never).max.y, 3)
  const arch = primitiveGeometry('arch@1', { width: 1, height: 1, opening_width: 100, opening_height: 100 })!
  const bounds = new Box3().setFromBufferAttribute(arch.getAttribute('position') as never)
  assert.ok(bounds.max.y <= 1 + 1e-9 && bounds.max.x <= 0.5 + 1e-9, 'arch stays inside its frame')
  assert.equal(primitiveGeometry('torus@1', {}), null)
})

test('projection mirrors the server: defaults, param merge, subtree removal, reparent, rename', () => {
  const g = obj(10, 'group', null, 1)
  let s = state([g, box(1, id(10), 0)])
  s = applyOps(s, [
    { op: 'add_objects', objects: [obj(2, 'primitive', null, 2, { primitive: { builtin: 'cylinder@1', params: { height: 2 } } })] },
    { op: 'update_objects', updates: [{ object_id: id(1), primitive: { params: { width: 4 } } }] },
    { op: 'rename', title: '中殿' },
  ])
  assert.equal(s.title, '中殿')
  const cyl = s.scene.objects.find((o) => o.id === id(2))!
  assert.deepEqual(cyl.primitive?.params, { radius_top: 0.5, radius_bottom: 0.5, height: 2, radial_segments: 32 })
  assert.deepEqual(s.scene.objects.find((o) => o.id === id(1))!.primitive?.params, { width: 4, height: 1, depth: 1 })
  s = applyOps(s, [{ op: 'reparent_objects', moves: [{ object_id: id(2), parent_id: id(10), order: 1 }] }])
  assert.equal(s.scene.objects.find((o) => o.id === id(2))!.parent_id, id(10))
  s = applyOps(s, [{ op: 'remove_objects', object_ids: [id(10)] }])
  assert.deepEqual(s.scene.objects, [], 'removing a group takes its descendants')
})

test('an undo patch puts objects back under a returning group and replaces only listed fields', () => {
  const g = obj(10, 'group', null, 1)
  const child = box(1, id(10), 0)
  const kept = box(2)
  const s = applyPatch(state([kept]), {
    put: [child, g],
    fields: { [id(2)]: { name: '石台' } },
    removed: [],
  })
  assert.equal(s.scene.objects.find((o) => o.id === id(1))!.parent_id, id(10))
  assert.equal(s.scene.objects.find((o) => o.id === id(2))!.name, '石台')
  const orphan = applyPatch(state([]), { put: [child], fields: {}, removed: [] })
  assert.equal(orphan.scene.objects[0].parent_id, null, 'a missing parent falls back to the root')
})

test('touched units match the server, a removed group naming every descendant', () => {
  const s = state([obj(10, 'group', null, 1), box(1, id(10), 0), box(2)])
  assert.deepEqual(touchedUnits({ op: 'remove_objects', object_ids: [id(10)] }, s).sort(), [
    'objects', `object:${id(10)}`, `object:${id(10)}:content`, `object:${id(1)}`, `object:${id(1)}:content`,
  ].sort())
  assert.deepEqual(
    touchedUnits({ op: 'update_objects', updates: [{ object_id: id(2), visible: false, light: undefined }] }, s),
    [`object:${id(2)}`],
  )
})

test('expected revisions wait for the local intent that last changed a unit', () => {
  const base = state([box(1), box(2)])
  const revs = { [`object:${id(1)}`]: '4', [`object:${id(2)}`]: '3', objects: '4' }
  const move = intent([moveTo([id(1)], 1)], { state: 'sent' })
  const add = intent([{ op: 'add_objects', objects: [box(3)] }])
  const blocked = intent([moveTo([id(2)], 9)], { state: 'blocked' })
  const e = expectedFor([`object:${id(1)}`, `object:${id(2)}`, `object:${id(3)}`, 'objects'], revs, base, [move, add, blocked])
  assert.deepEqual(e, {
    [`object:${id(1)}`]: { after: move.id },
    [`object:${id(2)}`]: '3',
    [`object:${id(3)}`]: { after: add.id },
    objects: { after: add.id },
  })
  assert.equal(sendable(intent([], { expected: e })), false)
  assert.equal(sendable(intent([], { expected: { objects: '4', x: null } })), true)
})

test('an update of the same objects folds into the unsent tail and keeps the earliest expected', () => {
  const first = intent([moveTo([id(1), id(2)], 1)], { expected: { [`object:${id(1)}`]: '4', [`object:${id(2)}`]: '4' } })
  const second = intent([moveTo([id(2), id(1)], 2)], { expected: { [`object:${id(1)}`]: { after: first.id }, [`object:${id(2)}`]: { after: first.id } } })
  const queue = enqueue([first], second)
  assert.equal(queue.length, 1)
  assert.equal(queue[0].id, first.id)
  assert.deepEqual(queue[0].expected, first.expected)
  const s = project(state([box(1), box(2)]), queue)
  assert.deepEqual(s.scene.objects.map((o) => o.transform.position[0]), [2, 2])

  assert.equal(enqueue([first], intent([moveTo([id(1)], 3)])).length, 2, 'a different set of objects stays separate')
  assert.equal(enqueue([{ ...first, state: 'sent' }], second).length, 2, 'a sent tail is never rewritten')
})

test('moving a held duplicate rewrites its add, so duplicate-and-move is one intent', () => {
  const add = intent([{ op: 'add_objects', objects: [box(3), box(4)] }], { held: true })
  const queue = enqueue([add], intent([moveTo([id(3), id(4)], 5)]))
  assert.equal(queue.length, 1)
  const op = queue[0].ops[0]
  assert.equal(op.op, 'add_objects')
  if (op.op === 'add_objects') assert.deepEqual(op.objects.map((o) => o.transform.position[0]), [5, 5])
  assert.equal(sendable(queue[0]), false, 'still held until released')
  assert.equal(enqueue([add], intent([moveTo([id(3), id(1)], 5)])).length, 2, 'touching an existing object is a separate intent')
})

test('dependents follow {after} transitively and receipts resolve it', () => {
  const a = intent([moveTo([id(1)], 1)], { state: 'sent' })
  const b = intent([moveTo([id(1)], 2)], { expected: { [`object:${id(1)}`]: { after: a.id } } })
  const c = intent([moveTo([id(1)], 3)], { expected: { [`object:${id(1)}`]: { after: b.id } } })
  const d = intent([moveTo([id(2)], 3)], { expected: { [`object:${id(2)}`]: '3' } })
  assert.deepEqual([...dependents([a, b, c, d], a.id)].sort(), [b.id, c.id])
  const resolved = resolveAfter([a, b, c, d], a.id, '7')
  assert.deepEqual(resolved[1].expected, { [`object:${id(1)}`]: '7' })
  assert.equal(sendable(resolved[1]), true)
  assert.equal(sendable(resolved[2]), false)
})
