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

test('commits of one field fold into the unsent tail and keep the earliest expected; separate operations never do', () => {
  const first = intent([moveTo([id(1), id(2)], 1)], { merge: 'field', expected: { [`object:${id(1)}`]: '4', [`object:${id(2)}`]: '4' } })
  const second = intent([moveTo([id(2), id(1)], 2)], { merge: 'field', expected: { [`object:${id(1)}`]: { after: first.id }, [`object:${id(2)}`]: { after: first.id } } })
  const { queue, holder } = enqueue([first], second)
  assert.equal(holder, first.id)
  assert.equal(queue.length, 1)
  assert.equal(queue[0].id, first.id)
  assert.deepEqual(queue[0].expected, first.expected)
  const s = project(state([box(1), box(2)]), queue)
  assert.deepEqual(s.scene.objects.map((o) => o.transform.position[0]), [2, 2])

  // Each G / R / S or drag is its own undo step, however slow saving is.
  assert.equal(enqueue([first], { ...second, merge: undefined }).queue.length, 2, 'an operation without a key stays separate')
  assert.equal(enqueue([{ ...first, merge: undefined }], { ...second, merge: undefined }).queue.length, 2, 'two operations never fold')
  assert.equal(enqueue([first], { ...second, merge: 'other field' }).queue.length, 2, 'another field stays separate')
  assert.equal(enqueue([first], intent([moveTo([id(1)], 3)], { merge: 'field' })).queue.length, 2, 'a different set of objects stays separate')
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
  // A plain add that is only waiting to go out is a step of its own.
  assert.equal(enqueue([{ ...add, held: false }], intent([moveTo([id(3), id(4)], 5)])).queue.length, 2, 'an unsent add does not absorb a later edit')
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

test('an edit that changes nothing is not saved; one that changes a part keeps only that part', async () => {
  const { transport, session } = await started([box(1)])
  const origin = { position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] }
  // Each would be a whole snapshot on the server and an undo step that does nothing.
  assert.equal(session.edit([{ op: 'update_objects', updates: [{ object_id: id(1), transform: origin }] }], '清除位置'), null)
  assert.equal(session.edit([{ op: 'update_objects', updates: [{ object_id: id(1), primitive: { params: { width: 1 } } }] }], '调整参数'), null)
  assert.equal(session.edit([{ op: 'rename', title: '场景' }], '重命名'), null)
  assert.equal(session.edit([{ op: 'set_render', render: { mode: 'clay' } }], '渲染'), null)
  await tick()
  assert.equal(transport.sent.length, 0)
  assert.equal(session.state.queue.length, 0)

  const kept = session.edit(
    [{ op: 'update_objects', updates: [{ object_id: id(1), name: '', transform: origin, visible: false, primitive: { params: { width: 1, height: 2 } } }] }],
    '修改',
  )
  assert.deepEqual(kept?.ops, [{ op: 'update_objects', updates: [{ object_id: id(1), visible: false, primitive: { params: { height: 2 } } }] }])
  const half = session.edit([{ op: 'update_objects', updates: [{ object_id: id(1), name: '', visible: true }] }], '显示')
  assert.deepEqual(half?.ops, [{ op: 'update_objects', updates: [{ object_id: id(1), visible: true }] }], 'compared with what the view shows, unsent edits included')
  assert.deepEqual(Object.keys(half!.expected), [`object:${id(1)}`], 'an unchanged name does not drag in other units')
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
  const clock = new FakeClock()
  const session = sessionFor('cwdoc_leave', () => new SceneSession('cwdoc_leave', transport, clock))
  await session.start()
  session.attach()
  assert.equal(anyUnsaved(), false)
  session.edit([moveTo([id(1)], 1)], '移动')
  assert.equal(anyUnsaved(), true)
  session.detach()
  clock.advance(0)
  assert.equal(session.isDisposed, false, 'still saving after the view closed')
  assert.equal(sessionFor('cwdoc_leave', () => { throw new Error('reopened') }), session, 'reopening finds the same session')
  transport.last().resolve(receipt(4, [`object:${id(1)}`]))
  await tick()
  assert.equal(anyUnsaved(), false)
  assert.equal(session.isDisposed, true)
})

test('StrictMode detaches and attaches in one commit: the session stays and keeps sending', async () => {
  const { transport, clock, session } = await started([box(1)])
  session.attach()
  session.detach()
  session.attach()
  clock.advance(0)
  assert.equal(session.isDisposed, false)
  session.edit([moveTo([id(1)], 1)], '移动')
  assert.equal(transport.sent.length, 1, 'edits still go out')
  transport.last().resolve(receipt(4, [`object:${id(1)}`]))
  await tick()
  assert.equal(session.isDisposed, false, 'an open editor keeps its session after saving')
  session.detach()
  assert.equal(session.isDisposed, false, 'decided on the next tick')
  clock.advance(0)
  assert.equal(session.isDisposed, true, 'the last view closed with nothing unsaved')
})

test('the undo stack reconnects after StrictMode disconnects it, keeping its steps', async () => {
  const { transport, session } = await started([box(1)])
  const undo = new UndoStack(session)
  const disconnect = undo.connect()
  disconnect()
  undo.connect()
  session.edit([moveTo([id(1)], 1)], '移动')
  transport.last().resolve(receipt(4, [`object:${id(1)}`]))
  await tick()
  assert.equal(undo.canUndo, true, 'the confirmed edit became a step')
  assert.equal(undo.undo(), 'sent')
})

// --- own undo and history ---------------------------------------------------

import { UndoStack } from '../src/creative-scene/state/undo.ts'
import { groupRevisions, type RevisionItem } from '../src/creative-scene/history.ts'

async function confirmLast(transport: FakeTransport, revision: number, changed: string[], extra: Partial<CommandReceipt> = {}) {
  transport.last().resolve(receipt(revision, changed, extra))
  await tick()
}

/** The server's document after this session's writes, for later reads. */
function docAt(objects: SceneObject[], revision: number, revs: Record<string, number>): SceneDocument {
  const doc = sceneDoc(objects, 3)
  doc.revision = String(revision)
  for (const [u, r] of Object.entries(revs)) doc.unit_revs[u] = String(r)
  return doc
}

test('undo and redo walk this session\'s steps, each a restore_units of their own revisions', async () => {
  const { transport, session } = await started([box(1), box(2)])
  const undo = new UndoStack(session)
  session.edit([moveTo([id(1)], 1)], '移动 A')
  await confirmLast(transport, 4, [`object:${id(1)}`])
  session.edit([moveTo([id(2)], 2)], '移动 B')
  await confirmLast(transport, 5, [`object:${id(2)}`])

  assert.equal(undo.undo(), 'sent')
  assert.deepEqual(transport.last().payload.operations, [{ op: 'restore_units', from_revision: '4', units: [`object:${id(2)}`] }])
  assert.equal(position(session, 2), 0, 'the undo shows at once')
  await confirmLast(transport, 6, [`object:${id(2)}`])
  assert.equal(undo.undo(), 'sent')
  assert.deepEqual(transport.last().payload.operations, [{ op: 'restore_units', from_revision: '3', units: [`object:${id(1)}`] }])
  await confirmLast(transport, 7, [`object:${id(1)}`])
  assert.equal(undo.redo(), 'sent')
  assert.deepEqual(transport.last().payload.operations, [{ op: 'restore_units', from_revision: '4', units: [`object:${id(1)}`] }])
  assert.equal(position(session, 1), 1)
})

test('a foreign revision makes a step stale for good; a hierarchy change does not', async () => {
  const { transport, clock, session } = await started([box(1), box(2)])
  const undo = new UndoStack(session)
  session.edit([moveTo([id(1)], 1)], '移动 A')
  await confirmLast(transport, 4, [`object:${id(1)}`])
  session.edit([moveTo([id(2)], 1)], '移动 B')
  await confirmLast(transport, 5, [`object:${id(2)}`])
  // Someone else moves A and regroups things.
  transport.doc = docAt([box(1), box(2)], 7, { [`object:${id(1)}`]: 6, [`object:${id(2)}`]: 5, objects: 7 })
  clock.advance(1000)
  session.invalidate()
  await tick()
  assert.equal(undo.undo(), 'sent', 'B survives a hierarchy change')
  await confirmLast(transport, 8, [`object:${id(2)}`])
  assert.equal(undo.undo(), 'empty')
  assert.equal(undo.notice, '这一步之后物件已被修改，无法撤销')

  session.edit([moveTo([id(1)], 3)], '再次移动 A')
  await confirmLast(transport, 9, [`object:${id(1)}`])
  assert.equal(undo.undo(), 'sent')
  await confirmLast(transport, 10, [`object:${id(1)}`])
  assert.equal(undo.undo(), 'empty', 'moving A again never revives the stale step')
})

test('a read that shows our own write before its receipt does not stale earlier steps', async () => {
  const { transport, clock, session } = await started([box(1)])
  const undo = new UndoStack(session)
  session.edit([moveTo([id(1)], 1)], '移动')
  await confirmLast(transport, 4, [`object:${id(1)}`])
  session.edit([moveTo([id(1)], 2)], '再次移动')
  transport.doc = docAt([box(1)], 5, { [`object:${id(1)}`]: 5 })
  clock.advance(1000)
  session.invalidate()
  await tick()
  await confirmLast(transport, 5, [`object:${id(1)}`])
  assert.equal(undo.undo(), 'sent')
  await confirmLast(transport, 6, [`object:${id(1)}`])
  assert.equal(undo.undo(), 'sent', 'the first move is still ours to undo')
})

test('undoing a delete brings the object back and is checked against objects', async () => {
  const { transport, session } = await started([box(1)])
  const undo = new UndoStack(session)
  session.edit([{ op: 'remove_objects', object_ids: [id(1)] }], '删除')
  await confirmLast(transport, 4, ['objects'], { removed_units: [`object:${id(1)}`, `object:${id(1)}:content`] })
  assert.equal(session.state.view!.scene.objects.length, 0)
  assert.equal(undo.undo(), 'sent')
  const sent = transport.last().payload
  assert.deepEqual(sent.operations, [{ op: 'restore_units', from_revision: '3', units: [`object:${id(1)}`, `object:${id(1)}:content`] }])
  assert.deepEqual(sent.expected, { [`object:${id(1)}`]: null, [`object:${id(1)}:content`]: null, objects: '4' })
  assert.equal(session.state.view!.scene.objects[0].id, id(1))
})

test('an undo receipt advances the base at once; the next edit does not wait for the re-read', async () => {
  const { transport, session } = await started([box(1)])
  const undo = new UndoStack(session)
  session.edit([{ op: 'remove_objects', object_ids: [id(1)] }], '删除')
  await confirmLast(transport, 4, ['objects'], { removed_units: [`object:${id(1)}`, `object:${id(1)}:content`] })
  assert.equal(undo.undo(), 'sent')
  const reads = transport.reads
  await confirmLast(transport, 5, [`object:${id(1)}`, `object:${id(1)}:content`, 'objects'])
  assert.equal(transport.reads, reads + 1, 'the server may settle an undo differently: read it')
  assert.equal(session.state.base!.revision, '5')
  assert.equal(session.state.view!.scene.objects[0]?.id, id(1), 'the restored object stays while the read is out')
  session.edit([moveTo([id(1)], 2)], '移动')
  assert.deepEqual(transport.last().payload.expected, { [`object:${id(1)}`]: '5' })
})

test('a receipt too large to list its units still advances what the intent changed', async () => {
  const { transport, session } = await started([box(1), box(2)])
  session.edit([{ op: 'remove_objects', object_ids: [id(2)] }, moveTo([id(1)], 1)], '整理')
  await confirmLast(transport, 4, [], { reset_required: true })
  const revs = session.state.base!.unit_revs
  assert.equal(revs[`object:${id(1)}`], '4')
  assert.equal(revs.objects, '4')
  assert.equal(revs[`object:${id(2)}`], undefined)
})

test('undoing an edit that has not been sent drops it without a request', async () => {
  const { transport, session } = await started([box(1), box(2)])
  const undo = new UndoStack(session)
  session.edit([moveTo([id(1)], 1)], '移动 A')
  session.edit([moveTo([id(2)], 1)], '移动 B')
  assert.equal(undo.undo(), 'cancelled')
  assert.equal(position(session, 2), 0)
  assert.equal(transport.sent.length, 1)
  assert.equal(undo.undo(), 'busy', 'the move in flight has to land first')
  await confirmLast(transport, 4, [`object:${id(1)}`])
  assert.deepEqual(transport.last().payload.operations, [{ op: 'restore_units', from_revision: '3', units: [`object:${id(1)}`] }], 'then it is undone')
})

const scaleTo = (n: number, k: number) => ({
  op: 'update_objects' as const,
  updates: [{ object_id: id(n), transform: { ...identity, scale: [k, k, k] } }],
})

test('adding while a save is slow, then scaling: undo drops the scale and keeps the box', async () => {
  const { transport, session } = await started([box(1)])
  const undo = new UndoStack(session)
  session.edit([moveTo([id(1)], 1)], '移动')
  session.edit([{ op: 'add_objects', objects: [box(3)] }], '添加「方块」')
  session.edit([scaleTo(3, 2)], '缩放「方块」')
  assert.equal(transport.sent.length, 1, 'the add waits behind the move')
  assert.equal(session.state.queue.length, 3, 'the scale is not folded into the add')
  assert.equal(undo.undo(), 'cancelled')
  const added = session.state.view!.scene.objects.find((o) => o.id === id(3))
  assert.ok(added, 'the box stays')
  assert.deepEqual(added.transform.scale, [1, 1, 1])
})

test('undo while the last edit is saving runs once it lands; a newer edit or a rejection calls it off', async () => {
  const { transport, session } = await started([box(1), box(2)])
  const undo = new UndoStack(session)
  const notices: string[] = []
  const stop = undo.onNotice((n) => notices.push(n))
  session.edit([moveTo([id(1)], 1)], '移动 A')
  assert.equal(undo.undo(), 'busy')
  assert.deepEqual(notices, ['正在保存，保存后撤销'])
  assert.equal(transport.sent.length, 1)
  await confirmLast(transport, 4, [`object:${id(1)}`])
  assert.deepEqual(transport.last().payload.operations, [{ op: 'restore_units', from_revision: '3', units: [`object:${id(1)}`] }], 'the undo goes out after the save')
  assert.equal(position(session, 1), 0)
  await confirmLast(transport, 5, [`object:${id(1)}`])

  // A newer edit replaces the request: the next receipt undoes nothing.
  session.edit([moveTo([id(2)], 2)], '移动 B')
  assert.equal(undo.undo(), 'busy')
  session.edit([moveTo([id(1)], 3)], '再次移动 A')
  await confirmLast(transport, 6, [`object:${id(2)}`])
  assert.deepEqual(transport.last().payload.operations, [moveTo([id(1)], 3)])
  await confirmLast(transport, 7, [`object:${id(1)}`])

  // A rejected edit is already gone from the view; nothing older is undone for it.
  session.edit([moveTo([id(2)], 4)], '移动 B')
  assert.equal(undo.undo(), 'busy')
  transport.last().reject(conflictOn(`object:${id(2)}`))
  await tick()
  const sent = transport.sent.length
  session.edit([moveTo([id(1)], 5)], '移动 A')
  await confirmLast(transport, 9, [`object:${id(1)}`])
  assert.equal(transport.sent.length, sent + 1, 'only the new edit went out')
  assert.equal(transport.sent.filter((x) => (x.payload.operations[0] as { op: string }).op === 'restore_units').length, 1)
  stop()
})

test('a whole restore clears both stacks', async () => {
  const { transport, session } = await started([box(1)])
  const undo = new UndoStack(session)
  session.edit([moveTo([id(1)], 1)], '移动')
  await confirmLast(transport, 4, [`object:${id(1)}`])
  assert.equal(undo.canUndo, true)
  const restoring = session.restoreWhole('3')
  transport.last().resolve(receipt(5, [], { reset_required: true }))
  assert.equal(await restoring, 'restored')
  assert.equal(undo.canUndo, false)
  assert.deepEqual(transport.last().payload, { expected_revision: '4', operations: [{ op: 'restore', revision: '3' }] })
})

const rev = (n: number, actor: RevisionItem['actor_kind'], minute: number, extra: Partial<RevisionItem> = {}): RevisionItem => ({
  revision: String(n), actor_kind: actor, run_id: null, restored_from: null, changed_units: [`object:${id(n)}`], annotated_units: [], removed_units: [], title: '场景',
  created_at: new Date(Date.UTC(2026, 9, 16, 10, minute)).toISOString(), ...extra,
})

test('history groups a photographer\'s nearby edits and one assistant run; restores and the creation stand alone', () => {
  const groups = groupRevisions([
    rev(9, 'photographer', 30, { restored_from: '2', changed_units: ['*'] }),
    rev(8, 'agent', 20, { run_id: 'run_b' }),
    rev(7, 'agent', 19, { run_id: 'run_a' }),
    rev(6, 'agent', 18, { run_id: 'run_a', changed_units: [`object:${id(5)}:content`] }),
    rev(5, 'photographer', 10),
    rev(4, 'photographer', 9),
    rev(3, 'photographer', 6),
    rev(2, 'photographer', 5, { restored_from: '1', changed_units: [`object:${id(2)}`], removed_units: [`object:${id(3)}`] }),
    rev(1, 'photographer', 5, { changed_units: ['*'] }),
  ])
  assert.deepEqual(groups.map((g) => g.revisions), [['9'], ['8'], ['7', '6'], ['5', '4'], ['3'], ['2'], ['1']])
  assert.deepEqual(groups.map((g) => g.label), ['恢复到第 2 版', '助手的修改', '助手的修改', '你的修改', '你的修改', '恢复了 2 处', '新建场景'])
  assert.equal(groups[2].objectCount, 2)
  assert.equal(groups[3].latest, '5')
})

// --- model edits: placeholders across intents, frozen baselines ------------

import type { WireOp } from '../src/creative-scene/state/apply.ts'

const generator = {
  language: 'three-module' as const, contract_version: 1 as const, lib_version: 1 as const, code: 'export default 1',
  code_sha256: 'sha256-x', params_schema: {}, params: {}, mount: 'floor' as const,
}
const manifest = { bounds: { min: [0, 0, 0], max: [1, 1, 1] }, triangles: 12, slots: [], parts: [], anchors: [] }
const commit = (ref: string, extra: Record<string, unknown> = {}): WireOp =>
  ({ op: 'commit_model', ref, glb_upload_id: `ccup_${ref}`, thumbnail_upload_id: `ccup_${ref}_t`, generator, manifest, ...extra }) as WireOp
const instance = (n: number, model: SceneObject['model'], order = n) =>
  obj(n, 'model_instance', null, order, { model, material_slots: {} })
const content = (n: number) => `object:${id(n)}:content`
const viewModel = (session: SceneSession, n: number) =>
  session.state.view!.scene.objects.find((o) => o.id === id(n))?.model?.content_revision_id
const noPlaceholders = (v: unknown) => !JSON.stringify(v).includes('local')

test('a copy and a retune made before the new model\'s receipt wait for it, then go out with real IDs', async () => {
  const { transport, session } = await started([box(1)])
  const undo = new UndoStack(session)
  const a = session.edit([commit('m'), { op: 'add_objects', objects: [instance(10, { ref: 'm' })] }], '新建代码模型')!
  assert.equal(transport.sent.length, 1, 'its own ref goes out as is')
  assert.equal(viewModel(session, 10), `local:${a.id}:m`, 'the view names the model by placeholder')

  const copy = session.state.view!.scene.objects.find((o) => o.id === id(10))!
  session.edit([{ op: 'add_objects', objects: [{ ...copy, id: id(11), order: 11 }] }], '复制')
  session.edit([
    commit('n', { base_content_id: `local-content:${a.id}:m` }),
    { op: 'set_instance_model', object_id: id(10), model: { ref: 'n' }, scope: 'this' },
  ], '调整参数')
  assert.equal(transport.sent.length, 1, 'placeholders keep both edits waiting')
  assert.equal(viewModel(session, 11), `local:${a.id}:m`)
  const retune = session.state.queue[2]
  assert.equal(viewModel(session, 10), `local:${retune.id}:n`)

  const reads = transport.reads
  await confirmLast(transport, 4, ['objects', `object:${id(10)}`, content(10)], {
    models: { m: { content_id: 'ccnt_m', content_revision_id: 'ccrv_m1' } },
  })
  assert.equal(session.state.base!.scene.objects.find((o) => o.id === id(10))?.model?.content_revision_id, 'ccrv_m1')
  assert.ok(noPlaceholders(session.state.base), 'the base never holds a placeholder')
  assert.ok(transport.reads > reads, 'the new model\'s summary comes with a read')
  const sentCopy = transport.last().payload.operations[0] as { objects: SceneObject[] }
  assert.equal(sentCopy.objects[0].model?.content_revision_id, 'ccrv_m1', 'the copy went out with the real revision')

  await confirmLast(transport, 5, ['objects', `object:${id(11)}`, content(11)])
  const sentRetune = transport.last().payload
  assert.equal((sentRetune.operations[0] as { base_content_id: string }).base_content_id, 'ccnt_m')
  assert.deepEqual(sentRetune.expected, { [content(10)]: '4' })
  await confirmLast(transport, 6, [content(10)], { models: { n: { content_id: 'ccnt_m', content_revision_id: 'ccrv_m2' } } })
  assert.ok(noPlaceholders(session.state), 'nothing in the session keeps a placeholder')

  assert.equal(undo.undo(), 'sent')
  assert.deepEqual(transport.last().payload.operations, [{ op: 'restore_units', from_revision: '5', units: [content(10)] }])
  assert.equal(viewModel(session, 10), 'ccrv_m1', 'undoing the retune shows the earlier revision')
})

test('a rejected model commit blocks what names it, as an invalid edit and never the trash', async () => {
  for (const [code, status, message] of [
    ['creative_upload_unavailable', 409, '模型上传已失效，请重新生成'],
    ['creative_model_unavailable', 400, '模型 ccrv_x 不可用'],
  ] as const) {
    const { transport, session } = await started([box(1)])
    const a = session.edit([commit('m'), { op: 'add_objects', objects: [instance(10, { ref: 'm' })] }], '新建代码模型')!
    session.edit([{ op: 'add_objects', objects: [{ ...instance(11, { content_revision_id: `local:${a.id}:m` }) }] }], '复制')
    session.edit([moveTo([id(1)], 3)], '移动')
    transport.last().reject({ status, code, message: '模型 ccrv_x 不可用' })
    await tick()
    const [group] = session.state.conflicts
    assert.equal(group.kind, 'invalid', code)
    assert.equal(group.message, message, code)
    assert.equal(group.intents.length, 2, `${code}: the copy waits with it`)
    assert.equal(group.intents[1].state, 'blocked')
    assert.equal(session.state.trashed, false)
    assert.equal(session.state.missing, false)
    assert.deepEqual(transport.last().payload.operations, [moveTo([id(1)], 3)], 'unrelated edits still save')
  }
})

test('a baseline keeps the revisions an edit saw; a change made elsewhere meanwhile is a conflict', async () => {
  const { transport, clock, session } = await started([instance(1, { content_revision_id: 'ccrv_a' })])
  const b = session.baseline([content(1)], { source: 'ccrv_a' })!
  transport.doc = docAt([instance(1, { content_revision_id: 'ccrv_other' })], 4, { [content(1)]: 4 })
  clock.advance(1000)
  session.invalidate()
  await tick()
  assert.equal(session.state.base!.unit_revs[content(1)], '4', 'the re-read moved the base')

  const ops: WireOp[] = [commit('m', { base_content_id: 'ccnt_a' }), { op: 'set_instance_model', object_id: id(1), model: { ref: 'm' }, scope: 'this' }]
  session.edit(ops, '调整参数', { baseline: b.id })
  assert.deepEqual(transport.last().payload.expected, { [content(1)]: '3' }, 'the frozen revision, not the re-read one')
  transport.last().reject(conflictOn(content(1)))
  await tick()
  assert.equal(session.state.conflicts[0].kind, 'conflict')
  assert.equal(session.state.view!.scene.objects[0].model?.content_revision_id, 'ccrv_other', 'the other write stays')
})

test('a baseline follows this session\'s own edits and breaks when one it waits for is rejected', async () => {
  const { transport, session } = await started([instance(1, { content_revision_id: 'ccrv_a' })])
  const slots = (name: string): WireOp => ({ op: 'update_objects', updates: [{ object_id: id(1), material_slots: { main: { preset: name } } }] } as WireOp)
  const x = session.edit([slots('stone')], '材质')!
  const b = session.baseline([content(1)], { source: 'ccrv_a' })!
  assert.deepEqual(b.expected, { [content(1)]: { after: x.id } })
  await confirmLast(transport, 4, [content(1)])
  assert.deepEqual(session.baselineOf(b.id)!.expected, { [content(1)]: '4' }, 'our receipt resolves it')

  const y = session.edit([slots('wood')], '材质')!
  assert.deepEqual(session.baselineOf(b.id)!.expected, { [content(1)]: { after: y.id } }, 'a later own edit is waited for, not conflicted with')
  transport.last().reject({ status: 400, code: 'validation_failed', message: '材质不存在' })
  await tick()
  assert.equal(session.baselineOf(b.id)!.broken, true)
  const ops: WireOp[] = [commit('m'), { op: 'set_instance_model', object_id: id(1), model: { ref: 'm' }, scope: 'this' }]
  assert.equal(session.edit(ops, '调整参数', { baseline: b.id }), null, 'a broken baseline cannot be saved')
  session.dropBaseline(b.id)
  assert.equal(session.baselineOf(b.id), undefined)
})

test('a new model that only collides on objects is rebased with its commit', async () => {
  const { transport, session } = await started([box(1)])
  session.edit([commit('m'), { op: 'add_objects', objects: [instance(10, { ref: 'm' }, 2)] }], '新建代码模型')
  transport.doc = sceneDoc([box(1), box(2, null, 2)], 4)
  transport.last().reject(conflictOn('objects'))
  await tick()
  await tick()
  assert.equal(transport.sent.length, 2)
  const [sentCommit, add] = transport.last().payload.operations as [{ op: string }, { objects: SceneObject[] }]
  assert.equal(sentCommit.op, 'commit_model')
  assert.equal(add.objects[0].order, 3)
})

test('copying an instance retuned moments ago waits for the model even though no unit is shared', async () => {
  const { transport, session } = await started([instance(1, { content_revision_id: 'ccrv_a' })])
  const a = session.edit([
    commit('m', { base_content_id: 'ccnt_a' }),
    { op: 'set_instance_model', object_id: id(1), model: { ref: 'm' }, scope: 'this' },
  ], '调整参数')!
  const copy = { ...session.state.view!.scene.objects[0], id: id(2), order: 2 }
  assert.equal(copy.model?.content_revision_id, `local:${a.id}:m`)
  const b = session.edit([{ op: 'add_objects', objects: [copy] }], '复制')!
  assert.deepEqual(b.expected, { objects: '3' }, 'nothing to wait for by revision')
  assert.equal(transport.sent.length, 1, 'the placeholder alone holds it back')
  await confirmLast(transport, 4, [content(1)], { models: { m: { content_id: 'ccnt_a', content_revision_id: 'ccrv_a2' } } })
  const sent = transport.last().payload.operations[0] as { objects: SceneObject[] }
  assert.equal(sent.objects[0].model?.content_revision_id, 'ccrv_a2')
})

test('when that retune is rejected, the copy naming its model is blocked with it', async () => {
  const { transport, session } = await started([instance(1, { content_revision_id: 'ccrv_a' })])
  session.edit([
    commit('m', { base_content_id: 'ccnt_a' }),
    { op: 'set_instance_model', object_id: id(1), model: { ref: 'm' }, scope: 'this' },
  ], '调整参数')
  const copy = { ...session.state.view!.scene.objects[0], id: id(2), order: 2 }
  session.edit([{ op: 'add_objects', objects: [copy] }], '复制')
  transport.last().reject({ status: 409, code: 'creative_upload_unavailable', message: 'x' })
  await tick()
  assert.equal(transport.sent.length, 1, 'the copy never went out with a placeholder')
  assert.deepEqual(session.state.conflicts[0].intents.map((i) => i.label), ['调整参数', '复制'])
  assert.equal(session.state.queue.length, 0)
})

test('reapplying a conflicted retune renames the placeholders of what it blocked', async () => {
  const { transport, session } = await started([instance(1, { content_revision_id: 'ccrv_a' })])
  session.edit([
    commit('m', { base_content_id: 'ccnt_a' }),
    { op: 'set_instance_model', object_id: id(1), model: { ref: 'm' }, scope: 'this' },
  ], '调整参数')
  session.edit([{ op: 'add_objects', objects: [{ ...session.state.view!.scene.objects[0], id: id(2), order: 2 }] }], '复制')
  transport.doc = docAt([instance(1, { content_revision_id: 'ccrv_other' })], 4, { [content(1)]: 4 })
  transport.last().reject(conflictOn(content(1)))
  await tick()
  session.reapply(session.state.conflicts[0].id)
  await tick()
  const [again, copy] = session.state.queue
  assert.equal(viewModel(session, 2), `local:${again.id}:m`, 'the copy follows the reapplied commit')
  assert.deepEqual(again.expected, { [content(1)]: '4' })
  await confirmLast(transport, 5, [content(1)], { models: { m: { content_id: 'ccnt_a', content_revision_id: 'ccrv_a3' } } })
  assert.equal(transport.last().intent.id, copy.id)
  assert.equal((transport.last().payload.operations[0] as { objects: SceneObject[] }).objects[0].model?.content_revision_id, 'ccrv_a3')
})

// --- model edits: bake, upload, one command --------------------------------

import { readFileSync } from 'node:fs'
import { ModelEdits, forkPlan, sharingInstances, type ModelEditDeps } from '../src/creative-scene/modelEdits.ts'
import { ModelCache } from '../src/creative-scene/models/cache.ts'
import type { CompileResult } from '../src/creative-scene/sandbox/host.ts'
import type { Upload } from '../src/creative-canvas/editor/media.ts'

const testdata = new URL('../../backend/internal/creativeglb/testdata/', import.meta.url)
const templateBytes = () => { const b = readFileSync(new URL('exported-template.glb', testdata)); return b.buffer.slice(b.byteOffset, b.byteOffset + b.byteLength) as ArrayBuffer }
const templateManifest = (JSON.parse(readFileSync(new URL('cases.json', testdata), 'utf8')) as { name: string; manifest: never }[]).find((c) => c.name === 'exported-template')!.manifest
const compiled = (code: string): CompileResult => ({
  ok: true, glb: templateBytes(), manifest: templateManifest, codeSHA256: `sha256-${code.length}`,
  module: { contract_version: 1, lib_version: 1, mount: 'floor', params_schema: { height: { type: 'number', default: 3, min: 1, max: 12 } }, params: { height: 5 } },
})

function deferred<T>() {
  let resolve!: (v: T) => void
  let reject!: (e: unknown) => void
  const promise = new Promise<T>((a, b) => { resolve = a; reject = b })
  return { promise, resolve, reject }
}

class FakeBakery {
  compiles: { code: string; gate: ReturnType<typeof deferred<CompileResult>> }[] = []
  uploads: { name: string; gate: ReturnType<typeof deferred<Upload>>; signal: AbortSignal }[] = []
  discarded: string[] = []
  autoCompile = true
  autoUpload = true
  thumbnailFails = false
  private n = 0
  deps(session: SceneSession, cache = new ModelCache(() => Promise.reject(new Error('no download')))): ModelEditDeps {
    return {
      session, cache,
      compile: ({ code }) => {
        const gate = deferred<CompileResult>()
        this.compiles.push({ code, gate })
        if (this.autoCompile) gate.resolve(compiled(code))
        return gate.promise
      },
      thumbnail: () => (this.thumbnailFails ? Promise.reject(new Error('no webgl')) : Promise.resolve(new Blob(['png']))),
      upload: (file, _declared, signal) => {
        const gate = deferred<Upload>()
        this.uploads.push({ name: file.name, gate, signal })
        const id = `ccup_${++this.n}`
        if (this.autoUpload) gate.resolve({ id, publication: { kind: 'candidate', id: `ccuc_${this.n}`, revision: '1' } } as Upload)
        return gate.promise
      },
      discard: (u) => { this.discarded.push(u.id); return Promise.resolve() },
      makeObjectID: () => id(90),
    }
  }
}

async function startedWithModels(objects: SceneObject[]) {
  const doc = sceneDoc(objects)
  doc.models = { ccrv_a: { content_id: 'ccnt_a', usable: true, manifest: templateManifest, generator: null, name_hint: '柱' } }
  const transport = new FakeTransport(doc)
  const session = new SceneSession('cwdoc_1', transport, new FakeClock())
  await session.start()
  return { transport, session }
}

const pillarAt = (n: number, rev = 'ccrv_a') => instance(n, { content_revision_id: rev })

test('R4: unshared appends to the model, shared forks unless applied to every instance', () => {
  const objects = [pillarAt(1), pillarAt(2), pillarAt(3, 'ccrv_b'), box(4)]
  assert.deepEqual(sharingInstances(objects, id(1)), [id(2)])
  assert.deepEqual(forkPlan(objects, id(3), 'ccnt_b', false), { baseContentID: 'ccnt_b', scope: 'this', objectIDs: [id(3)] })
  assert.deepEqual(forkPlan(objects, id(1), 'ccnt_a', false), { scope: 'this', objectIDs: [id(1)] })
  assert.deepEqual(forkPlan(objects, id(1), 'ccnt_a', true), { baseContentID: 'ccnt_a', scope: 'all_instances', objectIDs: [id(1), id(2)] })
})

test('retuning a model instance bakes, uploads both files and queues one command per R4', async () => {
  for (const [objects, applyToAll, base, scope, units] of [
    [[pillarAt(1)], false, 'ccnt_a', 'this', [content(1)]],
    [[pillarAt(1), pillarAt(2)], false, undefined, 'this', [content(1)]],
    [[pillarAt(1), pillarAt(2)], true, 'ccnt_a', 'all_instances', [content(1), content(2)]],
  ] as const) {
    const { transport, session } = await startedWithModels([...objects])
    const bakery = new FakeBakery()
    const cache = new ModelCache(() => Promise.reject(new Error('no download')))
    const edits = new ModelEdits(bakery.deps(session, cache))
    const out = await edits.update({ objectID: id(1), code: 'code v2', params: { height: 5 }, libVersion: 1, applyToAll, label: '调整「柱」参数' })
    assert.ok(out.ok, JSON.stringify(out))
    assert.deepEqual(bakery.uploads.map((u) => u.name), ['model.glb', 'thumbnail.png'], 'both files go up together')
    const sent = transport.last().payload
    const [commitOp, set] = sent.operations as [Record<string, unknown>, Record<string, unknown>]
    assert.equal(commitOp.base_content_id, base, `${scope}/${applyToAll}: base_content_id`)
    assert.equal(commitOp.glb_upload_id, 'ccup_1')
    assert.equal(commitOp.thumbnail_upload_id, 'ccup_2')
    assert.deepEqual(commitOp.generator, { language: 'three-module', contract_version: 1, lib_version: 1, code: 'code v2', code_sha256: 'sha256-7', params_schema: { height: { type: 'number', default: 3, min: 1, max: 12 } }, params: { height: 5 }, mount: 'floor' })
    assert.equal(commitOp.name_hint, '柱')
    assert.equal(set.scope, scope)
    assert.deepEqual(set.object_ids, scope === 'all_instances' ? [id(1), id(2)] : undefined)
    assert.deepEqual(Object.keys(sent.expected).sort(), [...units].sort())
    const placeholder = `local:${out.intent.id}:model`
    assert.equal(viewModel(session, 1), placeholder)
    assert.equal(cache.local.getState()[placeholder].summary.content_id, base ?? `local-content:${out.intent.id}:model`)
    assert.deepEqual(edits.store.getState().previews, {}, 'the preview gives way to the queued edit')
    assert.deepEqual(Object.keys(cache.local.getState()), [placeholder], 'the preview bytes are released')
    assert.deepEqual(edits.store.getState().status[id(1)], { state: 'queued', intent: out.intent.id })
  }
})

test('a new code model shows as a ghost while baking and lands last among root objects', async () => {
  const { transport, session } = await startedWithModels([box(1), box(2)])
  const bakery = new FakeBakery()
  bakery.autoUpload = false
  const edits = new ModelEdits(bakery.deps(session))
  const pending = edits.create({ code: 'pillar', position: [1, 0, 2] })
  await tick()
  assert.equal(edits.store.getState().ghost?.position.join(), '1,0,2', 'the ghost stands at the drop point')
  assert.equal(transport.sent.length, 0)
  for (const u of bakery.uploads) u.gate.resolve({ id: `ccup_${u.name}`, publication: null } as Upload)
  const out = await pending
  assert.ok(out.ok)
  const [commitOp, add] = transport.last().payload.operations as [Record<string, unknown>, { objects: SceneObject[] }]
  assert.equal(commitOp.op, 'commit_model')
  assert.equal(commitOp.base_content_id, undefined, 'a new model is new content')
  assert.deepEqual(add.objects[0].model, { ref: 'model' })
  assert.equal(add.objects[0].order, 3)
  assert.deepEqual(add.objects[0].transform.position, [1, 0, 2])
  assert.deepEqual(transport.last().payload.expected, { objects: '3' })
  assert.equal(edits.store.getState().ghost, null)
})

test('a newer input replaces an edit not yet queued and frees its uploads', async () => {
  const { transport, session } = await startedWithModels([pillarAt(1)])
  const bakery = new FakeBakery()
  bakery.autoUpload = false
  const edits = new ModelEdits(bakery.deps(session))
  const first = edits.update({ objectID: id(1), code: 'v1', params: {}, libVersion: 1, applyToAll: false, label: '调整' })
  await tick()
  assert.equal(bakery.uploads.length, 2, 'the first edit is uploading')
  const second = edits.update({ objectID: id(1), code: 'v2', params: {}, libVersion: 1, applyToAll: false, label: '调整' })
  assert.ok(bakery.uploads[0].signal.aborted, 'its uploads are cancelled')
  bakery.uploads[0].gate.resolve({ id: 'ccup_old_glb' } as Upload)
  bakery.uploads[1].gate.resolve({ id: 'ccup_old_png' } as Upload)
  assert.deepEqual(await first, { ok: false, superseded: true })
  assert.deepEqual(bakery.discarded.sort(), ['ccup_old_glb', 'ccup_old_png'], 'uploads that finished anyway are discarded')
  await tick()
  for (const u of bakery.uploads.slice(2)) u.gate.resolve({ id: `ccup_new_${u.name}` } as Upload)
  const out = await second
  assert.ok(out.ok)
  assert.equal(transport.sent.length, 1, 'only the latest edit is saved')
  assert.equal((transport.last().payload.operations[0] as { generator: { code: string } }).generator.code, 'v2')
})

test('an edit that fails before the queue leaves no intent behind', async () => {
  const cases: [string, (b: FakeBakery, s: SceneSession, t: FakeTransport) => void, string, string][] = [
    ['compile', (b) => { b.autoCompile = false }, 'compile', 'Unexpected token'],
    ['thumbnail', (b) => { b.thumbnailFails = true }, 'thumbnail', '缩略图生成失败，模型没有保存'],
    ['upload', (b) => { b.autoUpload = false }, 'upload', '模型保存失败，可以重试'],
    ['deleted', (b) => { b.autoUpload = false }, 'commit', '物件已被删除，修改未保存'],
    ['rejected before', (b) => { b.autoUpload = false }, 'commit', '之前的修改没有保存，这次修改已放弃'],
  ]
  for (const [name, arrange, stage, message] of cases) {
    const { transport, session } = await startedWithModels([pillarAt(1), box(2)])
    const bakery = new FakeBakery()
    arrange(bakery, session, transport)
    if (name === 'rejected before') session.edit([{ op: 'update_objects', updates: [{ object_id: id(1), material_slots: { main: { preset: 'stone' } } }] } as WireOp], '材质')
    const sentBefore = transport.sent.length
    const edits = new ModelEdits(bakery.deps(session))
    const pending = edits.update({ objectID: id(1), code: 'x', params: {}, libVersion: 1, applyToAll: false, label: '调整' })
    await tick()
    if (name === 'compile') bakery.compiles[0].gate.resolve({ ok: false, error: { kind: 'syntax', message: 'Unexpected token', line: 3, column: 1 } })
    if (name === 'upload') {
      bakery.uploads[0].gate.resolve({ id: 'ccup_glb' } as Upload)
      bakery.uploads[1].gate.reject(new Error('network'))
    }
    if (name === 'deleted' || name === 'rejected before') {
      if (name === 'deleted') session.edit([{ op: 'remove_objects', object_ids: [id(1)] }], '删除')
      else {
        transport.last().reject({ status: 400, code: 'validation_failed', message: '材质不存在' })
        await tick()
      }
      for (const u of bakery.uploads) u.gate.resolve({ id: `ccup_${u.name}` } as Upload)
    }
    const out = await pending
    assert.ok(!out.ok && !out.superseded && out.stage === stage, `${name}: ${JSON.stringify(out)}`)
    assert.equal(out.message, message, name)
    if (name === 'compile') assert.equal(out.error?.line, 3)
    const status = edits.store.getState().status[id(1)]
    assert.equal(status.state, 'failed', name)
    const commits = transport.sent.slice(sentBefore).filter((x) => x.payload.operations.some((op) => (op as { op: string }).op === 'commit_model'))
    assert.equal(commits.length, 0, `${name}: nothing was committed`)
    assert.ok(!session.state.queue.some((i) => i.ops.some((op) => op.op === 'commit_model')), `${name}: nothing waits in the queue`)
    assert.deepEqual(edits.store.getState().previews, {}, `${name}: the preview is gone`)
    if (['upload', 'deleted', 'rejected before'].includes(name)) assert.ok(bakery.discarded.length >= 1, `${name}: ready uploads are discarded`)
  }
})

test('previews change only what the viewport draws', async () => {
  const { withPreviews } = await import('../src/creative-scene/modelEdits.ts')
  const scene = state([pillarAt(1), pillarAt(2), box(3)]).scene
  assert.equal(withPreviews(scene, {}), scene)
  const shown = withPreviews(scene, { [id(1)]: 'preview:4', [id(3)]: 'preview:4' })
  assert.deepEqual(shown.objects.map((o) => o.model?.content_revision_id), ['preview:4', 'ccrv_a', undefined], 'only model instances take a preview')
  assert.equal(scene.objects[0].model?.content_revision_id, 'ccrv_a', 'the scene itself is untouched')
})
