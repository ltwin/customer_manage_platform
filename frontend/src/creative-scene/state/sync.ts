import { createStore, type StoreApi } from 'zustand/vanilla'
import type { CommandReceipt, SceneDocument } from '../api.ts'
import { newIntentID } from '../ids.ts'
import {
  applyOp,
  objectOfUnit,
  type ProjectionPatch,
  type SceneState,
  type WireOp,
} from './apply.ts'
import {
  applyIntent,
  changedUnits,
  dependents,
  enqueue,
  expectedFor,
  mergeUpdate,
  project,
  removedUnits,
  resolveAfter,
  sendable,
  touchedUnits,
  type Intent,
} from './intents.ts'

// One session per open scene document: the authoritative snapshot, the
// queue of local intents, and the loop that sends them one at a time. The
// session outlives the editor view until its queue is empty.

export type FailureLike = {
  status?: number
  code?: string
  message: string
  details?: unknown
}

export type Transport = {
  read(documentID: string): Promise<SceneDocument>
  command(
    documentID: string,
    intent: Intent,
    payload: unknown,
    keepalive: boolean,
  ): Promise<CommandReceipt>
}

export type Clock = {
  now(): number
  setTimeout(fn: () => void, ms: number): unknown
  clearTimeout(handle: unknown): void
  online(): boolean
  hidden(): boolean
}

/** A rejected intent and the intents it blocked, decided together. */
export type ConflictGroup = {
  id: string
  kind: 'conflict' | 'invalid' | 'trash'
  intents: Intent[]
  message: string
  /** The rejected edit deletes objects: its button says 「仍然删除」. */
  deletes: boolean
}

export type SaveStatus =
  | 'loading'
  | 'saved'
  | 'saving'
  | 'offline'
  | 'conflict'
  | 'readonly'
  | 'missing'

export type SessionState = {
  base: SceneDocument | null
  queue: Intent[]
  view: SceneState | null
  conflicts: ConflictGroup[]
  trashed: boolean
  missing: boolean
  readError: string
  status: SaveStatus
}

/** Hooks the undo stack uses to learn what this session wrote and saw. */
export type SessionObserver = {
  confirmed?(intent: Intent, receipt: CommandReceipt, before: SceneState): void
  read?(doc: SceneDocument): void
  sending?(intent: Intent): void
  /** A new edit entered the queue (folded or not). */
  queued?(intent: Intent): void
  /** An edit left the queue unsaved: rejected, or stopped by the trash. */
  rejected?(intent: Intent): void
  /** A whole-document restore succeeded. */
  restored?(): void
}

const toState = (doc: SceneDocument): SceneState => ({
  title: doc.title,
  scene: doc.scene,
})

const revision = (r: string) => Number(r)

export class SceneSession {
  readonly store: StoreApi<SessionState>
  private inflight: string | null = null
  private retryTimer: unknown = null
  private retryDelay = 1000
  private readTimer: unknown = null
  private lastRead = -Infinity
  private reading = false
  private readAgain = false
  private dragging = false
  private deferredRead = false
  private rebased = new Set<string>()
  private observers = new Set<SessionObserver>()
  private disposed = false
  /** Editors showing this session. */
  private views = 0
  /** No editor shows it; the session goes away once everything is saved. */
  private closing = false
  private closeTimer: unknown = null

  readonly documentID: string
  private readonly transport: Transport
  private readonly clock: Clock

  constructor(documentID: string, transport: Transport, clock: Clock) {
    this.documentID = documentID
    this.transport = transport
    this.clock = clock
    this.store = createStore<SessionState>(() => ({
      base: null,
      queue: [],
      view: null,
      conflicts: [],
      trashed: false,
      missing: false,
      readError: '',
      status: 'loading',
    }))
  }

  get state(): SessionState {
    return this.store.getState()
  }

  observe(o: SessionObserver): () => void {
    this.observers.add(o)
    return () => this.observers.delete(o)
  }

  /** Anything not yet saved: pending, in flight, unknown or blocked. */
  hasUnsaved(): boolean {
    return this.state.queue.length > 0 || this.state.conflicts.length > 0
  }

  blockedCount(): number {
    return this.state.conflicts.reduce((n, g) => n + g.intents.length, 0)
  }

  start(): Promise<void> {
    return this.read()
  }

  private set(patch: Partial<SessionState>) {
    const next = { ...this.state, ...patch }
    const base = next.base
    next.view = base ? project(toState(base), next.queue) : null
    next.status = this.statusOf(next)
    this.store.setState(next, true)
  }

  private statusOf(s: SessionState): SaveStatus {
    if (s.missing) return 'missing'
    if (!s.base) return 'loading'
    if (s.trashed) return 'readonly'
    if (s.conflicts.length) return 'conflict'
    if (s.queue.length && !this.clock.online()) return 'offline'
    if (s.queue.some((i) => i.state === 'unknown')) return 'offline'
    return s.queue.length ? 'saving' : 'saved'
  }

  /**
   * Records an edit and shows it at once. Returns the intent now holding it,
   * which may be an earlier one it was folded into, or null when the scene
   * cannot be edited.
   */
  edit(
    ops: WireOp[],
    label: string,
    options: { held?: boolean; projection?: ProjectionPatch; step?: Intent['step']; merge?: string } = {},
  ): Intent | null {
    const { base, view, queue, trashed, missing } = this.state
    if (!base || !view || trashed || missing || !ops.length) return null
    const units: string[] = []
    let probe = view
    for (const op of ops) {
      for (const u of touchedUnits(op, probe)) if (!units.includes(u)) units.push(u)
      probe = applyOp(probe, op)
    }
    // Undo or redo that adds or removes objects is checked against objects.
    const p = options.projection
    if (p && (p.put.length || p.removed.length) && !units.includes('objects')) units.push('objects')
    const intent: Intent = {
      id: newIntentID(),
      createdAt: new Date(this.clock.now()).toISOString(),
      ops,
      expected: expectedFor(units, base.unit_revs, toState(base), queue),
      state: 'pending',
      label,
      ...options,
    }
    const { queue: next, holder } = enqueue(queue, intent)
    this.set({ queue: next })
    for (const o of this.observers) o.queued?.(intent)
    this.pump()
    return this.state.queue.find((i) => i.id === holder) ?? null
  }

  /** Lets a held intent (a duplicate being moved) go out. */
  release(id: string) {
    this.set({ queue: this.state.queue.map((i) => (i.id === id ? { ...i, held: false } : i)) })
    this.pump()
  }

  /** Drops an intent that has not been sent; false once it is on its way. */
  cancel(id: string): boolean {
    const i = this.state.queue.find((q) => q.id === id)
    if (!i || i.state !== 'pending' || this.inflight === id) return false
    const deps = dependents(this.state.queue, id)
    if (deps.size) return false
    this.set({ queue: this.state.queue.filter((q) => q.id !== id) })
    return true
  }

  setDragging(dragging: boolean) {
    this.dragging = dragging
    if (!dragging && this.deferredRead) {
      this.deferredRead = false
      this.invalidate()
    }
  }

  /** A canvas notification: re-read at most every 500 ms, never mid-drag. */
  invalidate() {
    if (this.disposed) return
    if (this.dragging) {
      this.deferredRead = true
      return
    }
    const wait = this.lastRead + 500 - this.clock.now()
    if (wait > 0) {
      if (!this.readTimer)
        this.readTimer = this.clock.setTimeout(() => {
          this.readTimer = null
          void this.read()
        }, wait)
      return
    }
    void this.read()
  }

  private async read(): Promise<void> {
    if (this.reading) {
      this.readAgain = true
      return
    }
    this.reading = true
    this.lastRead = this.clock.now()
    try {
      const doc = await this.transport.read(this.documentID)
      this.rebase(doc)
    } catch (e) {
      const f = e as FailureLike
      if (f.status === 404) this.set({ missing: true })
      else this.set({ readError: f.message })
    } finally {
      this.reading = false
      if (this.readAgain) {
        this.readAgain = false
        this.invalidate()
      }
    }
  }

  /** A newer snapshot becomes the base; pending intents replay on top. */
  private rebase(doc: SceneDocument) {
    const base = this.state.base
    const newer = !base || revision(doc.revision) >= revision(base.revision)
    const next = newer ? doc : { ...base, trashed_at: doc.trashed_at, updated_at: doc.updated_at }
    this.set({ base: next, trashed: !!doc.trashed_at, readError: '' })
    if (newer) for (const o of this.observers) o.read?.(doc)
    this.pump()
  }

  /** Sends the first intent that may go; one request at a time. */
  pump() {
    if (this.disposed || this.inflight || this.retryTimer) return
    const { queue, trashed, missing, base } = this.state
    if (!base || trashed || missing) return
    const next = queue.find(sendable)
    if (!next) {
      this.set({})
      return
    }
    if (!this.clock.online()) {
      this.set({})
      return
    }
    this.inflight = next.id
    this.set({ queue: queue.map((i) => (i.id === next.id ? { ...i, state: 'sent' } : i)) })
    for (const o of this.observers) o.sending?.(next)
    const payload = { expected: next.expected, operations: next.ops }
    this.transport
      .command(this.documentID, next, payload, this.clock.hidden())
      .then(
        (receipt) => this.confirmed(next.id, receipt),
        (e: unknown) => this.failed(next.id, e as FailureLike),
      )
  }

  private confirmed(id: string, receipt: CommandReceipt) {
    this.inflight = null
    this.retryDelay = 1000
    const { queue, base } = this.state
    const intent = queue.find((i) => i.id === id)
    if (!intent || !base) return this.pump()
    const before = toState(base)
    const rest = resolveAfter(queue.filter((i) => i.id !== id), id, receipt.revision)
    const r = revision(receipt.revision)
    if (r === revision(base.revision) + 1) {
      // Our revision follows the base directly: it becomes the base now, so
      // the next edit expects the right revisions. A receipt too large to
      // list its units, or an undo the server may settle differently, is
      // checked by a read as well.
      const unit_revs = { ...base.unit_revs }
      const changed = receipt.reset_required ? changedUnits(intent, before) : receipt.changed_units
      const removed = receipt.reset_required ? removedUnits(intent, before) : receipt.removed_units
      for (const u of changed) unit_revs[u] = receipt.revision
      for (const u of removed) delete unit_revs[u]
      const after = applyIntent(before, intent)
      this.set({ base: { ...base, revision: receipt.revision, unit_revs, title: after.title, scene: after.scene }, queue: rest })
      if (receipt.reset_required || intent.projection) void this.read()
    } else {
      this.set({ queue: rest })
      // Someone else's revision came in between, or the receipt has no
      // detail: read the scene instead of guessing.
      if (r > revision(base.revision)) void this.read()
    }
    for (const o of this.observers) o.confirmed?.(intent, receipt, before)
    this.pump()
    this.maybeDispose()
  }

  private failed(id: string, f: FailureLike) {
    this.inflight = null
    const intent = this.state.queue.find((i) => i.id === id)
    if (!intent) return this.pump()
    if (f.status === 409 && f.code === 'creative_document_conflict') {
      void this.conflict(intent, f)
      return
    }
    if (f.status === 409 && f.code === 'creative_state_conflict') {
      this.blockForTrash()
      void this.read()
      return
    }
    if (f.status === 404) {
      this.set({ missing: true })
      return
    }
    if (f.status !== undefined && f.status >= 400 && f.status < 500 && f.status !== 408 && f.status !== 429) {
      this.reject(intent, 'invalid', f.message || '修改没有保存')
      return
    }
    // Network trouble, a timeout or a 5xx: the outcome is unknown, so the
    // same request goes out again under the same key.
    this.set({ queue: this.state.queue.map((i) => (i.id === id ? { ...i, state: 'unknown' } : i)) })
    const delay = this.retryDelay
    this.retryDelay = Math.min(this.retryDelay * 2, 30000)
    this.retryTimer = this.clock.setTimeout(() => {
      this.retryTimer = null
      this.pump()
    }, delay)
  }

  private conflictUnits(f: FailureLike): string[] {
    const d = f.details as { conflicts?: { unit: string }[] } | undefined
    return (d?.conflicts ?? []).map((c) => c.unit)
  }

  /**
   * Only an intent that adds objects is rebased on its own, and only when
   * nothing but objects moved and every target parent is still a group.
   */
  private async conflict(intent: Intent, f: FailureLike) {
    const units = this.conflictUnits(f)
    const addsOnly =
      intent.ops.every((op) => op.op === 'add_objects') ||
      (!!intent.projection && intent.projection.removed.length === 0 && Object.keys(intent.projection.fields).length === 0 && intent.projection.put.length > 0)
    if (!(units.length && units.every((u) => u === 'objects') && addsOnly) || this.rebased.has(intent.id)) {
      this.reject(intent, 'conflict', this.conflictMessage(intent, units))
      return
    }
    this.rebased.add(intent.id)
    let doc: SceneDocument
    try {
      doc = await this.transport.read(this.documentID)
    } catch {
      this.reject(intent, 'conflict', this.conflictMessage(intent, units))
      return
    }
    this.rebase(doc)
    const objects = doc.scene.objects
    const groups = new Set(objects.filter((o) => o.kind === 'group').map((o) => o.id))
    const rebased = rebaseAdds(intent, objects, groups)
    if (!rebased) {
      this.reject(intent, 'conflict', this.conflictMessage(intent, units))
      return
    }
    rebased.expected = { ...intent.expected, objects: doc.unit_revs.objects ?? null }
    this.set({ queue: this.state.queue.map((i) => (i.id === intent.id ? rebased : i)) })
    this.pump()
  }

  private conflictMessage(intent: Intent, units: string[]): string {
    const view = this.state.view
    const ids = units.map(objectOfUnit).filter((u) => !!u).map((u) => u.id)
    const named = ids.map((id) => view?.scene.objects.find((o) => o.id === id)).find((o) => o)
    const who = named ? `物件「${named.name || '未命名'}」` : '场景'
    return `${who}刚被其他人修改，「${intent.label}」没有保存`
  }

  /** The intent leaves the queue; whatever depended on it waits as blocked. */
  private reject(intent: Intent, kind: ConflictGroup['kind'], message: string) {
    const deps = dependents(this.state.queue, intent.id)
    const blocked = this.state.queue.filter((i) => deps.has(i.id)).map((i) => ({ ...i, state: 'blocked' as const, blockedBy: intent.id }))
    const queue = this.state.queue.filter((i) => i.id !== intent.id && !deps.has(i.id))
    const deletes = intent.ops.some((op) => op.op === 'remove_objects') || (intent.projection?.removed.length ?? 0) > 0
    this.set({ queue, conflicts: [...this.state.conflicts, { id: intent.id, kind, intents: [intent, ...blocked], message, deletes }] })
    for (const o of this.observers) o.rejected?.(intent)
    void this.read()
    this.pump()
  }

  /** The scene went to the trash: everything unsent waits for a restore. */
  private blockForTrash() {
    const waiting = this.state.queue.map((i) => ({ ...i, state: 'blocked' as const, blockedBy: 'trash' }))
    const others = this.state.conflicts.filter((g) => g.kind !== 'trash')
    const trash = this.state.conflicts.find((g) => g.kind === 'trash')
    this.set({
      trashed: true,
      queue: [],
      conflicts: [...others, { id: 'trash', kind: 'trash', intents: [...(trash?.intents ?? []), ...waiting], message: '场景在回收站，请先恢复', deletes: false }],
    })
    for (const i of waiting) for (const o of this.observers) o.rejected?.(i)
  }

  /**
   * 「重新应用我的修改」/「仍然删除」: the group goes back into the queue
   * against the latest snapshot, minus parts whose objects are gone. A group
   * of plain updates becomes a single intent.
   */
  reapply(groupID: string) {
    const group = this.state.conflicts.find((g) => g.id === groupID)
    if (!group || this.state.trashed) return
    this.set({ conflicts: this.state.conflicts.filter((g) => g.id !== groupID) })
    const present = new Set(this.state.view?.scene.objects.map((o) => o.id) ?? [])
    const updatesOnly = group.intents.every((i) => !i.projection && i.ops.every((op) => op.op === 'update_objects'))
    if (updatesOnly) {
      const merged = new Map<string, Parameters<typeof mergeUpdate>[0]>()
      for (const i of group.intents)
        for (const op of i.ops)
          if (op.op === 'update_objects')
            for (const u of op.updates)
              if (present.has(u.object_id)) merged.set(u.object_id, mergeUpdate(merged.get(u.object_id) ?? { object_id: u.object_id }, u))
      if (merged.size) this.edit([{ op: 'update_objects', updates: [...merged.values()] }], group.intents[0].label)
      return
    }
    for (const i of group.intents) {
      const ops = i.ops.map((op) => keepPresent(op, present)).filter((op): op is WireOp => !!op)
      if (ops.length) this.edit(ops, i.label, { projection: i.projection, step: i.step })
    }
  }

  /**
   * Restores the whole document to a revision from the history list. Only
   * while nothing is waiting to be saved; checked against the base revision.
   */
  async restoreWhole(revisionToRestore: string): Promise<'restored' | 'conflict' | 'busy' | 'failed'> {
    const base = this.state.base
    if (!base || this.state.queue.length || this.state.trashed) return 'busy'
    const intent: Intent = {
      id: newIntentID(),
      createdAt: new Date(this.clock.now()).toISOString(),
      ops: [],
      expected: {},
      state: 'sent',
      label: `恢复到第 ${revisionToRestore} 版`,
    }
    try {
      await this.transport.command(this.documentID, intent, { expected_revision: base.revision, operations: [{ op: 'restore', revision: revisionToRestore }] }, false)
    } catch (e) {
      const f = e as FailureLike
      return f.status === 409 && f.code === 'creative_document_conflict' ? 'conflict' : 'failed'
    }
    for (const o of this.observers) o.restored?.()
    await this.read()
    return 'restored'
  }

  /** 「放弃」: the group and what it blocked are dropped. */
  discard(groupID: string) {
    this.set({ conflicts: this.state.conflicts.filter((g) => g.id !== groupID) })
    this.maybeDispose()
  }

  discardConflicts() {
    this.set({ conflicts: [] })
    this.maybeDispose()
  }

  /** An editor shows this session; call from an effect, paired with detach. */
  attach() {
    this.views++
    this.closing = false
    if (this.closeTimer !== null) this.clock.clearTimeout(this.closeTimer)
    this.closeTimer = null
  }

  /**
   * An editor stopped showing it; saving continues, then the session goes
   * away. React detaches and attaches again within one commit (StrictMode in
   * development, remounts), so whether anyone still shows it is decided on
   * the next tick; deciding at once disposed the session under an open
   * editor, whose edits then never went out.
   */
  detach() {
    this.views = Math.max(0, this.views - 1)
    if (this.views > 0 || this.closeTimer !== null) return
    this.closeTimer = this.clock.setTimeout(() => {
      this.closeTimer = null
      if (this.views > 0) return
      this.closing = true
      this.maybeDispose()
    }, 0)
  }

  private maybeDispose() {
    if (this.closing && !this.hasUnsaved()) this.dispose()
  }

  dispose() {
    this.disposed = true
    if (this.closeTimer !== null) this.clock.clearTimeout(this.closeTimer)
    if (this.readTimer) this.clock.clearTimeout(this.readTimer)
    if (this.retryTimer) this.clock.clearTimeout(this.retryTimer)
    for (const cb of this.onDispose) cb()
    this.onDispose = []
  }

  get isDisposed(): boolean {
    return this.disposed
  }

  onDispose: (() => void)[] = []

  /** Changed units of an intent, against the current view. */
  unitsOf(intent: Intent): string[] {
    return this.state.view ? changedUnits(intent, this.state.view) : []
  }
}

/**
 * Puts the new top-level objects of an add at the end of their parent in
 * the latest snapshot; null when a parent is gone or no longer a group.
 */
function rebaseAdds(
  intent: Intent,
  objects: readonly { id: string; parent_id?: string | null; order: number }[],
  groups: Set<string>,
): Intent | null {
  if (intent.projection) {
    const ok = intent.projection.put.every((o) => !o.parent_id || groups.has(o.parent_id) || intent.projection?.put.some((p) => p.id === o.parent_id))
    return ok ? { ...intent, state: 'pending' } : null
  }
  const last = new Map<string, number>()
  for (const o of objects) {
    const key = o.parent_id ?? ''
    last.set(key, Math.max(last.get(key) ?? -1, o.order))
  }
  const ops: WireOp[] = []
  for (const op of intent.ops) {
    if (op.op !== 'add_objects') return null
    const added = new Set(op.objects.map((o) => o.id))
    const placed = []
    for (const o of op.objects) {
      if (o.parent_id && added.has(o.parent_id)) {
        placed.push(o)
        continue
      }
      if (o.parent_id && !groups.has(o.parent_id)) return null
      const key = o.parent_id ?? ''
      const order = (last.get(key) ?? -1) + 1
      last.set(key, order)
      placed.push({ ...o, order })
    }
    ops.push({ ...op, objects: placed })
  }
  return { ...intent, ops, state: 'pending' }
}

/** An operation limited to objects that still exist; null if nothing is left. */
function keepPresent(op: WireOp, present: Set<string>): WireOp | null {
  switch (op.op) {
    case 'update_objects': {
      const updates = op.updates.filter((u) => present.has(u.object_id))
      return updates.length ? { ...op, updates } : null
    }
    case 'remove_objects': {
      const ids = op.object_ids.filter((id) => present.has(id))
      return ids.length ? { ...op, object_ids: ids } : null
    }
    case 'reparent_objects': {
      const moves = op.moves.filter((m) => present.has(m.object_id) && (!m.parent_id || present.has(m.parent_id)))
      return moves.length ? { ...op, moves } : null
    }
    default:
      return op
  }
}

// --- the registry and the page-level guards --------------------------------

const sessions = new Map<string, SceneSession>()

export function sessionFor(
  documentID: string,
  make: () => SceneSession,
): SceneSession {
  const existing = sessions.get(documentID)
  if (existing && !existing.isDisposed) return existing
  const session = make()
  sessions.set(documentID, session)
  session.onDispose.push(() => {
    if (sessions.get(documentID) === session) sessions.delete(documentID)
  })
  return session
}


export function anyUnsaved(): boolean {
  for (const s of sessions.values()) if (s.hasUnsaved()) return true
  return false
}

export const unsavedMessage = '场景还有未保存的修改，离开会丢失'

if (typeof window !== 'undefined') {
  window.addEventListener('beforeunload', (e) => {
    if (!anyUnsaved()) return
    e.preventDefault()
    e.returnValue = unsavedMessage
  })
  const flush = () => {
    for (const s of sessions.values()) s.pump()
  }
  window.addEventListener('pagehide', flush)
  window.addEventListener('online', flush)
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) flush()
  })
}
