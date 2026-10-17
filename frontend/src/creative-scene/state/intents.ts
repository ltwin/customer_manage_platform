import type { ObjectUpdate } from '../api.ts'
import {
  applyOps,
  applyPatch,
  contentKeys,
  contentUnit,
  instanceTargets,
  placementKeys,
  placementUnit,
  subtree,
  updateObject,
  type ProjectionPatch,
  type SceneState,
  type WireOp,
} from './apply.ts'

/** A revision, "does not exist", or "whatever this local intent produces". */
export type ExpectedRev = string | null | { after: string }
export type IntentState = 'pending' | 'sent' | 'unknown' | 'blocked'

export type Intent = {
  /** Also the operation_id and the Idempotency-Key. */
  id: string
  createdAt: string
  ops: WireOp[]
  /** Unit revisions as the edit saw them. */
  expected: Record<string, ExpectedRev>
  state: IntentState
  /** Built but not released: Shift+D holds its add until the move ends. */
  held?: boolean
  /** For a blocked intent, the rejected intent it depended on. */
  blockedBy?: string
  /** Undo and redo show the step's recorded state instead of replaying ops. */
  projection?: ProjectionPatch
  /** The undo step this intent reverses or replays. */
  step?: { kind: 'undo' | 'redo'; seq: number }
  /**
   * One continuous edit, such as typing in one properties field: its commits
   * fold together while unsent. Without a key an edit is a step of its own.
   */
  merge?: string
  /** How prompts name this edit, for example 「移动 3 个物件」. */
  label: string
}

export type UnitRevs = Record<string, string>

const hasAny = (u: ObjectUpdate, keys: readonly string[]) =>
  keys.some((k) => (u as Record<string, unknown>)[k] !== undefined)

/** The units one operation must name in expected, mirroring the server. */
export function touchedUnits(op: WireOp, state: SceneState): string[] {
  switch (op.op) {
    case 'rename':
      return ['meta']
    case 'set_environment':
      return ['environment']
    case 'set_render':
      return ['render']
    case 'add_objects':
    case 'reparent_objects':
      return ['objects']
    case 'update_objects':
      return op.updates.flatMap((u) => [
        ...(hasAny(u, placementKeys) ? [placementUnit(u.object_id)] : []),
        ...(hasAny(u, contentKeys) ? [contentUnit(u.object_id)] : []),
      ])
    case 'remove_objects': {
      const gone = subtree(state.scene.objects, op.object_ids)
      return [
        'objects',
        ...state.scene.objects
          .filter((o) => gone.has(o.id))
          .flatMap((o) => [placementUnit(o.id), contentUnit(o.id)]),
      ]
    }
    case 'restore_units':
      return op.units
    case 'commit_model':
      return []
    case 'set_instance_model':
      return instanceTargets(op).map(contentUnit)
  }
}

/** The units an intent advances: what it touches plus new objects' units. */
export function changedUnits(intent: Intent, state: SceneState): string[] {
  const out = new Set<string>()
  for (const op of intent.ops) {
    for (const u of touchedUnits(op, state)) out.add(u)
    if (op.op === 'add_objects')
      for (const o of op.objects) {
        out.add(placementUnit(o.id))
        out.add(contentUnit(o.id))
      }
  }
  if (intent.projection) {
    const p = intent.projection
    if (p.put.length || p.removed.length) out.add('objects')
    for (const o of p.put) {
      out.add(placementUnit(o.id))
      out.add(contentUnit(o.id))
    }
  }
  return [...out]
}

/** The units an intent removes, for receipts too large to list them. */
export function removedUnits(intent: Intent, before: SceneState): string[] {
  const ids = new Set<string>(intent.projection?.removed ?? [])
  for (const op of intent.ops)
    if (op.op === 'remove_objects') for (const id of subtree(before.scene.objects, op.object_ids)) ids.add(id)
  return [...ids].flatMap((id) => [placementUnit(id), contentUnit(id)])
}

export function applyIntent(state: SceneState, intent: Intent): SceneState {
  return intent.projection
    ? applyPatch(state, intent.projection)
    : applyOps(state, intent.ops)
}

/** The view: every intent that is not blocked, in queue order, over base. */
export function project(base: SceneState, queue: readonly Intent[]): SceneState {
  return queue
    .filter((i) => i.state !== 'blocked')
    .reduce(applyIntent, base)
}

/**
 * Records, for each unit an edit touches, the revision it was based on. A
 * unit last changed by a local intent that has no receipt yet waits for that
 * intent ({after}), so the check uses what the edit actually saw.
 */
export function expectedFor(
  units: readonly string[],
  unitRevs: UnitRevs,
  base: SceneState,
  queue: readonly Intent[],
): Record<string, ExpectedRev> {
  const writer = new Map<string, string>()
  let state = base
  for (const intent of queue) {
    if (intent.state === 'blocked') continue
    for (const u of changedUnits(intent, state)) writer.set(u, intent.id)
    state = applyIntent(state, intent)
  }
  const out: Record<string, ExpectedRev> = {}
  for (const u of units) {
    const id = writer.get(u)
    out[u] = id ? { after: id } : (unitRevs[u] ?? null)
  }
  return out
}

export function mergeUpdate(a: ObjectUpdate, b: ObjectUpdate): ObjectUpdate {
  const primitive =
    a.primitive && b.primitive
      ? { params: { ...a.primitive.params, ...b.primitive.params } }
      : (b.primitive ?? a.primitive)
  return { ...a, ...b, ...(primitive ? { primitive } : {}) }
}

const onlyUpdate = (i: Intent) =>
  !i.projection && i.ops.length === 1 && i.ops[0].op === 'update_objects'
    ? i.ops[0]
    : null

/**
 * Adds an edit to the queue. Each intent is one undo step, so edits fold
 * together only when they are one operation to the photographer:
 *  - an update that only touches objects a held add creates is written into
 *    that add, so duplicating and moving is one revision;
 *  - a commit of the same continuous edit (merge key) as the unsent tail, on
 *    exactly its objects, replaces the tail's fields and keeps the tail's
 *    earlier expected revisions.
 * Separate operations stay separate however slowly saving goes: folding an
 * add and the scale after it made Cmd+Z remove the new object.
 */
export function enqueue(
  queue: readonly Intent[],
  next: Intent,
): { queue: Intent[]; holder: string } {
  const update = onlyUpdate(next)
  if (update) {
    const ids = new Set(update.updates.map((u) => u.object_id))
    const owner = queue.findIndex(
      (i) =>
        !!i.held &&
        i.ops.some(
          (op) =>
            op.op === 'add_objects' &&
            [...ids].every((id) => op.objects.some((o) => o.id === id)),
        ),
    )
    if (owner >= 0) {
      const byID = new Map(update.updates.map((u) => [u.object_id, u]))
      const rewritten = queue.map((i, n) =>
        n !== owner
          ? i
          : {
              ...i,
              ops: i.ops.map((op) =>
                op.op !== 'add_objects'
                  ? op
                  : {
                      ...op,
                      objects: op.objects.map((o) => {
                        const u = byID.get(o.id)
                        return u ? updateObject(o, u) : o
                      }),
                    },
              ),
            },
      )
      return { queue: rewritten, holder: queue[owner].id }
    }
    const tail = queue.at(-1)
    const sameEdit = next.merge !== undefined && tail?.merge === next.merge
    const tailUpdate = tail && sameEdit && !tail.held && tail.state === 'pending' ? onlyUpdate(tail) : null
    if (tail && tailUpdate) {
      const tailIDs = new Set(tailUpdate.updates.map((u) => u.object_id))
      if (tailIDs.size === ids.size && [...ids].every((id) => tailIDs.has(id))) {
        const latest = new Map(update.updates.map((u) => [u.object_id, u]))
        const merged: Intent = {
          ...tail,
          label: next.label,
          expected: { ...next.expected, ...tail.expected },
          ops: [
            {
              op: 'update_objects',
              updates: tailUpdate.updates.map((u) => mergeUpdate(u, latest.get(u.object_id) ?? { object_id: u.object_id })),
            },
          ],
        }
        return { queue: [...queue.slice(0, -1), merged], holder: tail.id }
      }
    }
  }
  return { queue: [...queue, next], holder: next.id }
}

/** Intents that depend on id, directly or through another dependent. */
export function dependents(queue: readonly Intent[], id: string): Set<string> {
  const out = new Set<string>()
  let grew = true
  while (grew) {
    grew = false
    for (const i of queue) {
      if (out.has(i.id) || i.id === id) continue
      const waits = Object.values(i.expected).some(
        (e) => e !== null && typeof e === 'object' && (e.after === id || out.has(e.after)),
      )
      if (waits) {
        out.add(i.id)
        grew = true
      }
    }
  }
  return out
}

/** Swaps {after: id} for the revision its receipt produced. */
export function resolveAfter(queue: readonly Intent[], id: string, revision: string): Intent[] {
  return queue.map((i) => {
    let changed = false
    const expected: Record<string, ExpectedRev> = {}
    for (const [u, e] of Object.entries(i.expected)) {
      if (e !== null && typeof e === 'object' && e.after === id) {
        expected[u] = revision
        changed = true
      } else expected[u] = e
    }
    return changed ? { ...i, expected } : i
  })
}

/** An intent may go out once nothing it expects is still waiting. */
export function sendable(i: Intent): boolean {
  return (
    (i.state === 'pending' || i.state === 'unknown') &&
    !i.held &&
    Object.values(i.expected).every((e) => e === null || typeof e === 'string')
  )
}
