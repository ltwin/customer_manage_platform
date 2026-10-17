import type { CommandReceipt, SceneDocument, SceneObject } from '../api.ts'
import {
  contentKeys,
  contentUnit,
  objectOfUnit,
  placementKeys,
  placementUnit,
  subtree,
  type ProjectionPatch,
  type SceneState,
} from './apply.ts'
import { applyIntent, changedUnits, type Intent } from './intents.ts'
import type { SceneSession } from './sync.ts'

// Cmd+Z only reverses this session's own confirmed edits. A step stays valid
// while every revision of each of its units after it came from this session;
// one foreign revision makes it stale for good. Validity is decided when
// undoing, from every revision seen so far, so a read that arrives before
// our own receipt cannot mistake our write for someone else's.

export type Step = {
  seq: number
  label: string
  /** Units the step changed, without the objects collection. */
  units: string[]
  before: string
  after: string
  undoPatch: ProjectionPatch
  redoPatch: ProjectionPatch
}

export type UndoResult = 'sent' | 'cancelled' | 'empty' | 'busy'

/** What turning state `from` into state `to` looks like for these units. */
function patchFor(from: SceneState, to: SceneState, units: string[]): ProjectionPatch {
  const fromByID = new Map(from.scene.objects.map((o) => [o.id, o]))
  const toByID = new Map(to.scene.objects.map((o) => [o.id, o]))
  const patch: ProjectionPatch = { put: [], fields: {}, removed: [] }
  const seen = new Set<string>()
  for (const u of units) {
    if (u === 'environment') patch.environment = to.scene.environment
    if (u === 'render') patch.render = to.scene.render
    const unit = objectOfUnit(u)
    if (!unit) continue
    const target = toByID.get(unit.id)
    const source = fromByID.get(unit.id)
    if (!target) {
      if (source && !seen.has(unit.id)) patch.removed.push(unit.id)
    } else if (!source) {
      if (!seen.has(unit.id)) patch.put.push(target)
    } else {
      const keys = unit.content ? contentKeys : placementKeys
      const fields: Partial<SceneObject> = { ...patch.fields[unit.id] }
      for (const k of keys) (fields as Record<string, unknown>)[k] = target[k]
      patch.fields[unit.id] = fields
    }
    seen.add(unit.id)
  }
  return patch
}

function removedBy(intent: Intent, before: SceneState): string[] {
  const ids = new Set<string>(intent.projection?.removed ?? [])
  for (const op of intent.ops)
    if (op.op === 'remove_objects') for (const id of subtree(before.scene.objects, op.object_ids)) ids.add(id)
  return [...ids].flatMap((id) => [placementUnit(id), contentUnit(id)])
}

export class UndoStack {
  private undoSteps: Step[] = []
  private redoSteps: Step[] = []
  private seq = 0
  /** Every revision seen per unit, from reads and from expected revisions. */
  private seen = new Map<string, Set<number>>()
  /** The latest document revision at which a known unit was missing. */
  private gone = new Map<string, number>()
  private known = new Set<string>()
  private own = new Set<number>()
  /** The latest own revision that removed each unit. */
  private ownRemoved = new Map<string, number>()
  private inFlight = new Map<string, Step>()
  private readonly session: SceneSession
  private listeners = new Set<() => void>()
  notice = ''

  constructor(session: SceneSession) {
    this.session = session
    session.observe({
      confirmed: (intent, receipt, before) => this.confirmed(intent, receipt, before),
      read: (doc) => this.observe(doc),
      sending: (intent) => this.sending(intent),
      restored: () => this.clear(),
    })
    const base = session.state.base
    if (base) this.observe(base)
  }

  subscribe(listener: () => void): () => void {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }

  private changed() {
    for (const l of this.listeners) l()
  }

  get canUndo(): boolean {
    return this.undoSteps.length > 0 || this.unsentEdit() !== undefined
  }

  get canRedo(): boolean {
    return this.redoSteps.length > 0
  }

  valid(step: Step): boolean {
    const after = Number(step.after)
    return step.units.every((u) => {
      for (const r of this.seen.get(u) ?? [])
        if (r > after && !this.own.has(r)) return false
      const missing = this.gone.get(u)
      if (missing === undefined || missing <= after) return true
      // Missing after the step is fine only when this session removed it.
      const removed = this.ownRemoved.get(u)
      return removed !== undefined && removed > after && removed <= missing
    })
  }

  private see(unit: string, rev: string) {
    const revs = this.seen.get(unit) ?? new Set<number>()
    revs.add(Number(rev))
    this.seen.set(unit, revs)
  }

  private observe(doc: SceneDocument) {
    const at = Number(doc.revision)
    for (const [u, rev] of Object.entries(doc.unit_revs)) this.see(u, rev)
    for (const u of this.known)
      if (!(u in doc.unit_revs)) this.gone.set(u, Math.max(this.gone.get(u) ?? 0, at))
    this.known = new Set(Object.keys(doc.unit_revs))
  }

  private sending(intent: Intent) {
    for (const [u, e] of Object.entries(intent.expected)) if (typeof e === 'string') this.see(u, e)
  }

  private confirmed(intent: Intent, receipt: CommandReceipt, before: SceneState) {
    const r = Number(receipt.revision)
    this.own.add(r)
    // A large receipt lists nothing; the intent says what it removed.
    const removed = receipt.reset_required ? removedBy(intent, before) : receipt.removed_units
    for (const u of removed) this.ownRemoved.set(u, r)
    const pending = this.inFlight.get(intent.id)
    if (pending) {
      this.inFlight.delete(intent.id)
      if (intent.step?.kind === 'undo') this.redoSteps.push(pending)
      else this.undoSteps.push(pending)
      this.changed()
      return
    }
    if (intent.step) return
    const units = changedUnits(intent, before).filter((u) => u !== 'objects')
    // Hierarchy-only edits cannot be restored per unit; they are not steps.
    if (!units.length) return
    const after = applyIntent(before, intent)
    this.seq++
    this.undoSteps.push({
      seq: this.seq,
      label: intent.label,
      units,
      before: String(r - 1),
      after: receipt.revision,
      undoPatch: patchFor(after, before, units),
      redoPatch: patchFor(before, after, units),
    })
    this.redoSteps = []
    this.changed()
  }

  private clear() {
    this.undoSteps = []
    this.redoSteps = []
    this.changed()
  }

  private unsentEdit(): Intent | undefined {
    const queue = this.session.state.queue
    const last = queue.at(-1)
    return last && !last.step && last.state === 'pending' && !last.held ? last : undefined
  }

  undo(): UndoResult {
    this.notice = ''
    const unsent = this.unsentEdit()
    if (unsent) {
      if (this.session.cancel(unsent.id)) {
        this.changed()
        return 'cancelled'
      }
    }
    if (this.session.state.queue.some((i) => !i.step && (i.state === 'sent' || i.state === 'unknown'))) {
      this.notice = '正在保存，稍后再撤销'
      this.changed()
      return 'busy'
    }
    while (this.undoSteps.length) {
      const step = this.undoSteps.pop()!
      if (!this.valid(step)) {
        this.notice = '这一步之后物件已被修改，无法撤销'
        continue
      }
      return this.submit(step, 'undo')
    }
    this.changed()
    return 'empty'
  }

  redo(): UndoResult {
    this.notice = ''
    while (this.redoSteps.length) {
      const step = this.redoSteps.pop()!
      if (!this.valid(step)) {
        this.notice = '这一步之后物件已被修改，无法重做'
        continue
      }
      return this.submit(step, 'redo')
    }
    this.changed()
    return 'empty'
  }

  private submit(step: Step, kind: 'undo' | 'redo'): UndoResult {
    const intent = this.session.edit(
      [{ op: 'restore_units', from_revision: kind === 'undo' ? step.before : step.after, units: step.units }],
      `${kind === 'undo' ? '撤销' : '重做'}${step.label}`,
      { projection: kind === 'undo' ? step.undoPatch : step.redoPatch, step: { kind, seq: step.seq } },
    )
    if (!intent) {
      ;(kind === 'undo' ? this.undoSteps : this.redoSteps).push(step)
      this.changed()
      return 'empty'
    }
    this.inFlight.set(intent.id, step)
    this.changed()
    return 'sent'
  }
}
