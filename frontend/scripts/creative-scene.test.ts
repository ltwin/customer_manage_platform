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
  const { queue, holder } = enqueue([first], second)
  assert.equal(holder, first.id)
  assert.equal(queue.length, 1)
  assert.equal(queue[0].id, first.id)
  assert.deepEqual(queue[0].expected, first.expected)
  const s = project(state([box(1), box(2)]), queue)
  assert.deepEqual(s.scene.objects.map((o) => o.transform.position[0]), [2, 2])

  assert.equal(enqueue([first], intent([moveTo([id(1)], 3)])).queue.length, 2, 'a different set of objects stays separate')
  assert.equal(enqueue([{ ...first, state: 'sent' }], second).queue.length, 2, 'a sent tail is never rewritten')
})

test('moving a held duplicate rewrites its add, so duplicate-and-move is one intent', () => {
  const add = intent([{ op: 'add_objects', objects: [box(3), box(4)] }], { held: true })
  const { queue, holder } = enqueue([add], intent([moveTo([id(3), id(4)], 5)]))
  assert.equal(holder, add.id)
  assert.equal(queue.length, 1)
  const op = queue[0].ops[0]
  assert.equal(op.op, 'add_objects')
  if (op.op === 'add_objects') assert.deepEqual(op.objects.map((o) => o.transform.position[0]), [5, 5])
  assert.equal(sendable(queue[0]), false, 'still held until released')
  assert.equal(enqueue([add], intent([moveTo([id(3), id(1)], 5)])).queue.length, 2, 'touching an existing object is a separate intent')
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

// --- the session: send loop, receipts, terminal states ----------------------

import type { CommandReceipt, SceneDocument } from '../src/creative-scene/api.ts'
import {
  SceneSession,
  anyUnsaved,
  detach,
  sessionFor,
  type Clock,
  type FailureLike,
  type Transport,
} from '../src/creative-scene/state/sync.ts'

class FakeClock implements Clock {
  t = 0
  isOnline = true
  private timers: { at: number; fn: () => void; handle: number }[] = []
  private next = 1
  now() { return this.t }
  setTimeout(fn: () => void, ms: number) {
    const handle = this.next++
    this.timers.push({ at: this.t + ms, fn, handle })
    return handle
  }
  clearTimeout(handle: unknown) { this.timers = this.timers.filter((x) => x.handle !== handle) }
  online() { return this.isOnline }
  hidden() { return false }
  advance(ms: number) {
    this.t += ms
    const due = this.timers.filter((x) => x.at <= this.t)
    this.timers = this.timers.filter((x) => x.at > this.t)
    for (const x of due) x.fn()
  }
}

type Sent = { intent: Intent; payload: { expected: Record<string, unknown>; operations: unknown[] }; resolve(r: CommandReceipt): void; reject(e: FailureLike): void }

class FakeTransport implements Transport {
  doc: SceneDocument
  reads = 0
  sent: Sent[] = []
  constructor(doc: SceneDocument) { this.doc = doc }
  read() {
    this.reads++
    return Promise.resolve(structuredClone(this.doc))
  }
  command(_id: string, intent: Intent, payload: unknown) {
    return new Promise<CommandReceipt>((resolve, reject) => {
      this.sent.push({ intent, payload: payload as Sent['payload'], resolve, reject })
    })
  }
  last() { return this.sent.at(-1)! }
}

const tick = () => new Promise((r) => setImmediate(r))

function sceneDoc(objects: SceneObject[], revision = 3): SceneDocument {
  const s = state(objects)
  const unit_revs: Record<string, string> = { meta: '1', environment: '1', render: '1', objects: String(revision) }
  for (const o of objects) {
    unit_revs[`object:${o.id}`] = String(revision)
    unit_revs[`object:${o.id}:content`] = String(revision)
  }
  return { id: 'cwdoc_1', type_key: 'scene', title: s.title, revision: String(revision), content_revision_id: 'ccrv_1', unit_revs, trashed_at: null, created_at: '', updated_at: '', scene: s.scene, models: {} }
}

const receipt = (revision: number, changed: string[], extra: Partial<CommandReceipt> = {}): CommandReceipt => ({
  revision: String(revision), changed_units: changed, annotated_units: [], removed_units: [], reset_required: false, ...extra,
})
const conflictOn = (unit: string): FailureLike => ({
  status: 409, code: 'creative_document_conflict', message: '文档已被修改',
  details: { conflicts: [{ unit, expected_rev: '3', current_rev: '4' }] },
})

async function started(objects: SceneObject[]) {
  const transport = new FakeTransport(sceneDoc(objects))
  const clock = new FakeClock()
  const session = new SceneSession('cwdoc_1', transport, clock)
  await session.start()
  return { transport, clock, session }
}

const position = (session: SceneSession, n: number) =>
  session.state.view!.scene.objects.find((o) => o.id === id(n))?.transform.position[0]

test('edits show at once and go out one request at a time', async () => {
  const { transport, session } = await started([box(1), box(2)])
  session.edit([moveTo([id(1)], 1)], '移动')
  session.edit([moveTo([id(2)], 2)], '移动')
  assert.equal(position(session, 1), 1)
  assert.equal(position(session, 2), 2)
  assert.equal(transport.sent.length, 1, 'only one request in flight')
  assert.equal(transport.last().intent.id, transport.last().intent.id)
  assert.deepEqual(transport.last().payload.expected, { [`object:${id(1)}`]: '3' })
  assert.equal(session.state.status, 'saving')

  transport.last().resolve(receipt(4, [`object:${id(1)}`]))
  await tick()
  assert.equal(session.state.base!.revision, '4')
  assert.equal(session.state.base!.unit_revs[`object:${id(1)}`], '4')
  assert.equal(transport.reads, 1, 'a contiguous receipt needs no read')
  assert.equal(transport.sent.length, 2)
  transport.last().resolve(receipt(5, [`object:${id(2)}`]))
  await tick()
  assert.equal(session.state.status, 'saved')
  assert.equal(session.hasUnsaved(), false)
})

test('a receipt after someone else\'s revision re-reads instead of guessing', async () => {
  const { transport, session } = await started([box(1)])
  session.edit([moveTo([id(1)], 1)], '移动')
  transport.doc = sceneDoc([box(1)], 6)
  transport.last().resolve(receipt(6, [`object:${id(1)}`]))
  await tick()
  await tick()
  assert.equal(transport.reads, 2)
  assert.equal(session.state.base!.revision, '6')
})

test('a rejected intent blocks its dependents, independent ones still save, and reapply merges them', async () => {
  const { transport, session } = await started([box(1), box(2)])
  session.edit([moveTo([id(1)], 1)], '移动')
  const a = transport.last()
  session.edit([moveTo([id(1)], 2)], '再次移动')
  session.edit([moveTo([id(2)], 9)], '移动 B')
  const dependent = session.state.queue[1]
  assert.deepEqual(dependent.expected, { [`object:${id(1)}`]: { after: a.intent.id } })

  transport.doc = sceneDoc([box(1), box(2)], 4)
  a.reject(conflictOn(`object:${id(1)}`))
  await tick()
  assert.equal(session.state.conflicts.length, 1)
  const group = session.state.conflicts[0]
  assert.deepEqual(group.intents.map((i) => i.id), [a.intent.id, dependent.id])
  assert.equal(group.intents[1].state, 'blocked')
  assert.match(group.message, /刚被其他人修改/)
  assert.equal(transport.sent.length, 2, 'the independent edit went out, the dependent did not')
  assert.deepEqual(transport.last().payload.operations, [moveTo([id(2)], 9)])
  assert.equal(session.state.status, 'conflict')
  assert.ok(session.hasUnsaved(), 'blocked edits keep the leave prompt')
  assert.equal(session.blockedCount(), 2)

  transport.last().resolve(receipt(5, [`object:${id(2)}`]))
  await tick()
  session.reapply(group.id)
  await tick()
  const again = transport.last()
  assert.deepEqual(again.payload.operations, [moveTo([id(1)], 2)], 'both moves merged, the latest value wins')
  assert.deepEqual(again.payload.expected, { [`object:${id(1)}`]: '4' }, 'expected is the latest revision')
})

test('an unknown outcome resends the same request with backoff while dependents wait', async () => {
  const { transport, clock, session } = await started([box(1)])
  session.edit([moveTo([id(1)], 1)], '移动')
  const first = transport.last()
  session.edit([moveTo([id(1)], 2)], '再次移动')
  first.reject({ message: 'Failed to fetch' })
  await tick()
  assert.equal(session.state.queue[0].state, 'unknown')
  assert.equal(session.state.status, 'offline')
  assert.equal(transport.sent.length, 1)
  clock.advance(999)
  assert.equal(transport.sent.length, 1)
  clock.advance(1)
  assert.equal(transport.sent.length, 2)
  assert.equal(transport.last().intent.id, first.intent.id, 'same key, so the server replays the receipt')
  transport.last().resolve(receipt(4, [`object:${id(1)}`]))
  await tick()
  assert.deepEqual(transport.last().payload.expected, { [`object:${id(1)}`]: '4' }, '{after} became the receipt revision')
})

test('an add that only collides on objects is rebased once, with its objects moved to the end', async () => {
  const { transport, session } = await started([box(1)])
  session.edit([{ op: 'add_objects', objects: [box(5, null, 2)] }], '添加')
  transport.doc = sceneDoc([box(1), box(2, null, 2)], 4)
  transport.last().reject(conflictOn('objects'))
  await tick()
  await tick()
  const resent = transport.last()
  assert.equal(transport.sent.length, 2)
  assert.deepEqual(resent.payload.expected, { objects: '4' })
  const op = resent.payload.operations[0] as { objects: SceneObject[] }
  assert.equal(op.objects[0].order, 3)
  resent.reject(conflictOn('objects'))
  await tick()
  assert.equal(session.state.conflicts.length, 1, 'a second collision is not retried')
})

test('a delete that only collides on objects asks instead of retrying', async () => {
  const { transport, session } = await started([box(1)])
  session.edit([{ op: 'remove_objects', object_ids: [id(1)] }], '删除')
  transport.last().reject(conflictOn('objects'))
  await tick()
  assert.equal(transport.sent.length, 1)
  assert.equal(session.state.conflicts[0].deletes, true)
})

test('the trash makes the scene read-only and keeps unsent edits until restored', async () => {
  const { transport, clock, session } = await started([box(1)])
  session.edit([moveTo([id(1)], 1)], '移动')
  transport.doc = { ...sceneDoc([box(1)]), trashed_at: '2026-10-16T10:00:00Z' }
  transport.last().reject({ status: 409, code: 'creative_state_conflict', message: '文档在回收站' })
  await tick()
  await tick()
  assert.equal(session.state.trashed, true)
  assert.equal(session.state.status, 'readonly')
  assert.equal(session.state.conflicts[0].kind, 'trash')
  assert.equal(session.edit([moveTo([id(1)], 3)], '移动'), null, 'no edits in the trash')

  transport.doc = sceneDoc([box(1)])
  session.invalidate()
  clock.advance(500)
  await tick()
  assert.equal(session.state.trashed, false)
  session.reapply('trash')
  await tick()
  assert.deepEqual(transport.last().payload.operations, [moveTo([id(1)], 1)])
})

test('invalidations re-read at most every 500 ms and wait for a drag to end', async () => {
  const { transport, clock, session } = await started([box(1)])
  clock.advance(1000)
  session.invalidate()
  await tick()
  assert.equal(transport.reads, 2)
  session.invalidate()
  session.invalidate()
  await tick()
  assert.equal(transport.reads, 2, 'throttled')
  clock.advance(500)
  await tick()
  assert.equal(transport.reads, 3, 'one trailing read')
  session.setDragging(true)
  clock.advance(1000)
  session.invalidate()
  await tick()
  assert.equal(transport.reads, 3, 'deferred while dragging')
  session.setDragging(false)
  await tick()
  assert.equal(transport.reads, 4)
})

test('the leave prompt follows unsaved work across sessions, and a closed editor keeps saving', async () => {
  const transport = new FakeTransport(sceneDoc([box(1)]))
  const session = sessionFor('cwdoc_leave', () => new SceneSession('cwdoc_leave', transport, new FakeClock()))
  await session.start()
  session.attached = true
  assert.equal(anyUnsaved(), false)
  session.edit([moveTo([id(1)], 1)], '移动')
  assert.equal(anyUnsaved(), true)
  detach(session)
  assert.equal(session.isDisposed, false, 'still saving after the view closed')
  assert.equal(sessionFor('cwdoc_leave', () => { throw new Error('reopened') }), session, 'reopening finds the same session')
  transport.last().resolve(receipt(4, [`object:${id(1)}`]))
  await tick()
  assert.equal(anyUnsaved(), false)
  assert.equal(session.isDisposed, true)
})
