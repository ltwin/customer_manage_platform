import test from 'node:test'
import assert from 'node:assert/strict'
import type { SceneObject } from '../src/creative-scene/api.ts'
import {
  canDrop,
  clearParent,
  draggedObjects,
  dropRows,
  groupSelection,
  parentToActive,
  ungroupSelection,
  type GroupingResult,
  type Regroup,
} from '../src/creative-scene/grouping.ts'
import { applyOp, withoutNoops, type SceneState } from '../src/creative-scene/state/apply.ts'
import { changedUnits, removedUnits, touchedUnits, type Intent } from '../src/creative-scene/state/intents.ts'
import { sameTransform, worldOf } from '../src/creative-scene/state/world.ts'

const sid = (n: number) => `sobj_${String(n).padStart(21, '0')}`
const so = (n: number, extra: Partial<SceneObject> = {}): SceneObject => ({
  id: sid(n), kind: 'primitive', name: '', parent_id: null, order: n,
  transform: { position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] }, visible: true, locked: false, ...extra,
})
const group = (n: number, extra: Partial<SceneObject> = {}) => so(n, { kind: 'group', ...extra })
const at = (x: number, y = 0, z = 0) => ({ position: [x, y, z], rotation: [0, 0, 0, 1], scale: [1, 1, 1] })
const sel = (ids: number[], active = ids.at(-1)) => ({ selected: ids.map(sid), active: active === undefined ? null : sid(active) })
const state = (objects: SceneObject[]): SceneState => ({ title: '场景', scene: { environment: {} as never, render: { mode: 'clay' }, objects } })

// A parent turned by Euler(0, 0.8, 0) and scaled 2; a child tilted and stretched.
const turned = { position: [2, 1, -3], rotation: [0, 0.3894183423086505, 0, 0.9210609940028851], scale: [2, 2, 2] }
const tilted = { position: [1, 0.5, 2], rotation: [0.06407134770607115, 0.09115754934299071, 0.15343930202422257, 0.9818561728660807], scale: [1, 1.5, 1] }

function edit(r: GroupingResult): Regroup {
  assert.ok(r && !('error' in r), `expected an edit, got ${JSON.stringify(r)}`)
  return r
}

const worlds = (objects: SceneObject[]) => {
  const world = worldOf(objects)
  return new Map(objects.map((o) => [o.id, world(o.id)]))
}

function sameWorlds(before: SceneObject[], after: SceneObject[]) {
  const a = worlds(before)
  const b = worlds(after)
  for (const [id, m] of a) {
    const n = b.get(id)
    if (!n) continue
    assert.ok(m.elements.every((v, i) => Math.abs(v - n.elements[i]) < 1e-6), `${id} moved: ${m.elements} → ${n.elements}`)
  }
}

const tree = (objects: SceneObject[]) => Object.fromEntries(objects.map((o) => [o.id, `${o.parent_id ?? '-'}#${o.order}`]))

/** Siblings never share an order and parents are groups, as the server checks. */
function valid(objects: SceneObject[]) {
  const kinds = new Map(objects.map((o) => [o.id, o.kind]))
  const taken = new Set<string>()
  for (const o of objects) {
    if (o.parent_id) assert.equal(kinds.get(o.parent_id), 'group', `${o.id} under a ${kinds.get(o.parent_id)}`)
    const key = `${o.parent_id}#${o.order}`
    assert.ok(!taken.has(key), `order clash ${key}`)
    taken.add(key)
  }
}

/** Applies an edit and its inverse: world positions hold, and the inverse comes back to the start. */
function roundTrip(objects: SceneObject[], r: Regroup): SceneObject[] {
  const after = applyOp(state(objects), r.op).scene.objects
  valid(after)
  sameWorlds(objects, after)
  assert.ok(r.inverse)
  const back = applyOp(state(after), r.inverse).scene.objects
  assert.deepEqual(tree(back), tree(objects))
  for (const o of objects) assert.ok(sameTransform(back.find((b) => b.id === o.id)!.transform, o.transform), `${o.id} transform`)
  return after
}

test('Ctrl+G groups the topmost selected objects in a new group where the first one was, keeping world positions', () => {
  const objects = [group(1, { transform: turned, order: 0 }), so(2, { parent_id: sid(1), order: 0, transform: tilted }), group(3, { parent_id: sid(1), order: 1, transform: at(-1, 0, 4) }), so(4, { parent_id: sid(1), order: 2 }), so(5, { parent_id: sid(3), order: 0 })]
  const r = edit(groupSelection(objects, sel([3, 2, 5]), () => sid(9)))
  assert.equal(r.label, '编组 2 个物件')
  const after = roundTrip(objects, r)
  const g = after.find((o) => o.id === sid(9))!
  assert.deepEqual([g.kind, g.name, g.parent_id, g.order], ['group', '分组 1', sid(1), 0])
  assert.deepEqual(g.transform.rotation, [0, 0, 0, 1])
  assert.deepEqual(tree(after)[sid(2)], `${sid(9)}#0`, 'list order inside the group')
  assert.deepEqual(tree(after)[sid(3)], `${sid(9)}#1`)
  assert.deepEqual(tree(after)[sid(5)], `${sid(3)}#0`, 'a selected child goes along with its parent')
  // The group sits at the middle of what it holds.
  const world = worldOf(after)
  const centre = [world(sid(2)), world(sid(3))].map((m) => [m.elements[12], m.elements[13], m.elements[14]])
  const gw = world(sid(9)).elements
  for (let k = 0; k < 3; k++) assert.ok(Math.abs(gw[12 + k] - (centre[0][k] + centre[1][k]) / 2) < 1e-9)
})

test('objects under different parents are grouped last at the root; names count up', () => {
  const objects = [group(1, { name: '分组 3', order: 0 }), so(2, { parent_id: sid(1), order: 0, transform: at(1) }), so(3, { order: 1, transform: at(3) })]
  const r = edit(groupSelection(objects, sel([2, 3]), () => sid(9)))
  const after = roundTrip(objects, r)
  const g = after.find((o) => o.id === sid(9))!
  assert.deepEqual([g.name, g.parent_id, g.order], ['分组 4', null, 1])
  assert.equal(groupSelection(objects, sel([])), null)
})

test('Ctrl+Alt+G puts the children where the group was and moves later siblings up', () => {
  const objects = [so(1, { order: 0 }), group(2, { order: 1, transform: turned }), so(3, { order: 2 }), so(4, { parent_id: sid(2), order: 0, transform: tilted }), so(5, { parent_id: sid(2), order: 3 })]
  const r = edit(ungroupSelection(objects, sel([2])))
  assert.equal(r.op.op, 'reparent_objects')
  const after = roundTrip(objects, r)
  assert.deepEqual(tree(after), { [sid(1)]: '-#0', [sid(4)]: '-#1', [sid(5)]: '-#2', [sid(3)]: '-#3' })
  assert.deepEqual((r.inverse?.create_groups ?? []).map((g) => g.id), [sid(2)], 'undo brings the group back under its ID')
  assert.deepEqual(ungroupSelection(objects, sel([1])), { error: '选中的不是分组' })
})

test('ungrouping nested groups removes both; groups without children are just deleted', () => {
  const objects = [group(1, { order: 0, transform: at(1) }), group(2, { parent_id: sid(1), order: 0, transform: turned }), so(3, { parent_id: sid(2), order: 0, transform: tilted })]
  const r = edit(ungroupSelection(objects, sel([1, 2])))
  assert.ok(r.op.op === 'reparent_objects')
  assert.deepEqual(r.op.remove_groups, [sid(1), sid(2)])
  const after = roundTrip(objects, r)
  assert.deepEqual(tree(after), { [sid(3)]: '-#0' })
  const empty = ungroupSelection([group(7), group(8)], sel([7, 8]))
  assert.deepEqual(empty && !('error' in empty) && empty.op, { op: 'remove_objects', object_ids: [sid(7), sid(8)] })
  assert.equal(empty && !('error' in empty) && empty.inverse, null, 'undone like any delete')
})

test('Ctrl+P parents to the active group after its children; not to a primitive, not into a loop', () => {
  const objects = [group(1, { order: 0, transform: turned }), so(2, { parent_id: sid(1), order: 4 }), so(3, { order: 1, transform: tilted }), group(4, { order: 2 }), so(5, { parent_id: sid(4), order: 0 })]
  const r = edit(parentToActive(objects, sel([3, 4, 1], 1)))
  const after = roundTrip(objects, r)
  assert.equal(tree(after)[sid(3)], `${sid(1)}#5`)
  assert.equal(tree(after)[sid(4)], `${sid(1)}#6`)
  assert.equal(tree(after)[sid(5)], `${sid(4)}#0`)
  assert.deepEqual(parentToActive(objects, sel([1, 3], 3)), { error: '只能挂到分组或空物体下' })
  assert.deepEqual(parentToActive(objects, sel([4, 5], 5)), { error: '只能挂到分组或空物体下' })
  assert.deepEqual(parentToActive([...objects, group(6, { parent_id: sid(4), order: 1 })], sel([4, 6], 6)), { error: '不能挂到自己的子物件下' })
  assert.equal(parentToActive(objects, sel([2, 1], 1)), null, 'already its child')
})

test('Alt+P clears the parent of selected children, last at the root', () => {
  const objects = [group(1, { order: 0, transform: turned }), so(2, { parent_id: sid(1), order: 0, transform: tilted }), so(3, { order: 5 })]
  const after = roundTrip(objects, edit(clearParent(objects, sel([2, 3]))))
  assert.equal(tree(after)[sid(2)], '-#6')
  assert.equal(clearParent(objects, sel([3])), null, 'already at the root')
})

test('dragging rows inserts before, after or inside, never onto themselves', () => {
  const objects = [so(1, { order: 0 }), group(2, { order: 1, transform: turned }), so(3, { order: 2 }), so(4, { parent_id: sid(2), order: 0 }), so(5, { order: 3, transform: tilted })]
  const after = roundTrip(objects, edit(dropRows(objects, [sid(5)], sid(1), 'after')))
  assert.deepEqual(tree(after), { [sid(1)]: '-#0', [sid(5)]: '-#1', [sid(2)]: '-#2', [sid(3)]: '-#3', [sid(4)]: `${sid(2)}#0` })
  const inside = roundTrip(objects, edit(dropRows(objects, [sid(5), sid(1)], sid(2), 'inside')))
  assert.equal(tree(inside)[sid(5)], `${sid(2)}#1`)
  assert.equal(tree(inside)[sid(1)], `${sid(2)}#2`)
  assert.equal(canDrop(objects, [sid(2)], sid(4), 'before'), false, 'onto its own child')
  assert.equal(canDrop(objects, [sid(1)], sid(3), 'inside'), false, 'inside a primitive')
  assert.equal(dropRows(objects, [sid(1)], sid(2), 'before'), null, 'where it already is')
  assert.deepEqual(draggedObjects(objects, sel([2, 4, 5]), sid(4)), [sid(2), sid(5)], 'a selected row drags the topmost selection')
  assert.deepEqual(draggedObjects(objects, sel([2]), sid(5)), [sid(5)])
})

test('a parent too small to keep the scale refuses instead of clamping', () => {
  const objects = [group(1, { order: 0, transform: { position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [0.001, 0.001, 0.001] } }), so(2, { order: 1, transform: { ...at(0), scale: [2, 2, 2] } })]
  assert.deepEqual(parentToActive(objects, sel([2, 1], 1)), { error: '无法在保持位置的前提下改挂' })
})

test('units match the server receipt: the contract grouping example', () => {
  const objects = [so(1, { order: 0, transform: at(1.5, 1.4, 7) }), so(2, { order: 1, transform: at(0, 0, -3) })]
  const r = edit(groupSelection(objects, sel([1, 2]), () => sid(9)))
  const s = state(objects)
  const intent: Intent = { id: 'i', createdAt: '', ops: [r.op], expected: {}, state: 'pending', label: r.label }
  assert.deepEqual(touchedUnits(r.op, s).sort(), ['object:' + sid(1), 'object:' + sid(2), 'objects'])
  assert.deepEqual(changedUnits(intent, s).sort(), ['object:' + sid(1), 'object:' + sid(2), 'object:' + sid(9), `object:${sid(9)}:content`, 'objects'])
  const undo: Intent = { ...intent, ops: [r.inverse!] }
  const grouped = applyOp(s, r.op)
  assert.deepEqual(touchedUnits(r.inverse!, grouped).sort(), ['object:' + sid(1), 'object:' + sid(2), 'object:' + sid(9), `object:${sid(9)}:content`, 'objects'])
  assert.deepEqual(removedUnits(undo, grouped), ['object:' + sid(9), `object:${sid(9)}:content`])
})

test('a reparent that leaves things where they are is not sent; float noise counts as no change', () => {
  const objects = [group(1, { order: 0 }), so(2, { parent_id: sid(1), order: 0, transform: tilted })]
  const noisy = { ...tilted, position: tilted.position.map((v) => v + 1e-9) }
  const op = { op: 'reparent_objects' as const, moves: [{ object_id: sid(2), parent_id: sid(1), order: 0, transform: noisy }] }
  assert.deepEqual(withoutNoops([op], state(objects)), [])
  const moved = { ...op, moves: [...op.moves, { object_id: sid(1), parent_id: null, order: 3 }] }
  assert.deepEqual(withoutNoops([moved], state(objects)), [{ ...op, moves: [{ object_id: sid(1), parent_id: null, order: 3 }] }])
})
