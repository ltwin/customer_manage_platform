import { createStore, type StoreApi } from 'zustand/vanilla'
import type { CommandReceipt, SceneDocument } from '../api.ts'
import { newIntentID } from '../ids.ts'
import { rebaseReparent, refitReparent } from '../grouping.ts'
import {
  applyOp,
  instanceTargets,
  objectOfUnit,
  parsePlaceholder,
  withoutNoops,
  type ProjectionPatch,
  type SceneState,
  type WireOp,
} from './apply.ts'
import {
  applyIntent,
  asConfirmed,
  changedUnits,
  dependents,
  enqueue,
  expectedFor,
  mapModelIDs,
  mergeUpdate,
  project,
  removedUnits,
  resolveAfter,
  resolveModelIDs,
  sendable,
  touchedUnits,
  type ExpectedRev,
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

/**
 * What a slow edit (a model bake and its uploads) saw when it started. A
 * re-read never changes it, so a write made elsewhere in the meantime comes
 * back as a conflict instead of being overwritten. This session's own edits
 * do move it, as they move queued intents: a receipt resolves {after} and
 * model placeholders, and a later local edit of a unit becomes its {after}.
 */
export type Baseline = {
  id: string
  expected: Record<string, ExpectedRev>
  /** Model IDs the edit started from, such as the source revision; may be placeholders. */
  models: Record<string, string>
  /** A local edit it builds on was rejected or dropped: it cannot be saved. */
  broken: boolean
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
  private baselines = new Map<string, Baseline>()
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
   * cannot be edited or the edit changes nothing the view shows.
   */
  edit(
    requested: WireOp[],
    label: string,
    options: { held?: boolean; projection?: ProjectionPatch; step?: Intent['step']; merge?: string; baseline?: string; id?: string; inverse?: Intent['inverse'] } = {},
  ): Intent | null {
    const { base, view, queue, trashed, missing } = this.state
    if (!base || !view || trashed || missing) return null
    // A model edit names its intent up front: its placeholders are registered before the view shows them.
    const { baseline: baselineID, id: presetID, ...rest } = options
    const frozen = baselineID === undefined ? undefined : this.baselines.get(baselineID)
    if (baselineID !== undefined && (!frozen || frozen.broken)) return null
    const ops = withoutNoops(requested, view)
    if (!ops.length) return null
    const units: string[] = []
    let probe = view
    for (const op of ops) {
      for (const u of touchedUnits(op, probe)) if (!units.includes(u)) units.push(u)
      probe = applyOp(probe, op)
    }
    // Undo or redo that adds or removes objects is checked against objects.
    const p = options.projection
    if (p && (p.put.length || p.removed.length) && !units.includes('objects')) units.push('objects')
    const expected = expectedFor(units, base.unit_revs, toState(base), queue)
    // Units the slow edit started from keep the revisions it saw then.
    if (frozen) for (const u of units) if (u in frozen.expected) expected[u] = frozen.expected[u]
    const intent: Intent = {
      id: presetID ?? newIntentID(),
      createdAt: new Date(this.clock.now()).toISOString(),
      ops,
      expected,
      state: 'pending',
      label,
      ...rest,
    }
    const { queue: next, holder } = enqueue(queue, intent)
    for (const b of this.baselines.values()) {
      if (b.id === baselineID) continue
      for (const u of changedUnits(intent, view)) if (u in b.expected) b.expected[u] = { after: holder }
    }
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
    this.breakBaselines(new Set([id]))
    this.set({ queue: this.state.queue.filter((q) => q.id !== id) })
    return true
  }

  /**
   * Freezes the expected revisions of units for an edit that takes a while
   * to prepare; pass its id to edit() as `baseline`, then drop it.
   */
  baseline(units: readonly string[], models: Record<string, string> = {}): Baseline | null {
    const { base, queue } = this.state
    if (!base) return null
    const b: Baseline = {
      id: newIntentID(),
      expected: expectedFor(units, base.unit_revs, toState(base), queue),
      models: { ...models },
      broken: false,
    }
    this.baselines.set(b.id, b)
    return b
  }

  baselineOf(id: string): Baseline | undefined {
    return this.baselines.get(id)
  }

  dropBaseline(id: string) {
    this.baselines.delete(id)
  }

  /** Baselines waiting on any of these intents can no longer be saved. */
  private breakBaselines(ids: Set<string>) {
    for (const b of this.baselines.values()) {
      const waits =
        Object.values(b.expected).some((e) => e !== null && typeof e === 'object' && ids.has(e.after)) ||
        Object.values(b.models).some((m) => ids.has(parsePlaceholder(m)?.intent ?? ''))
      if (waits) b.broken = true
    }
  }

  /** A receipt reaches the baselines as it reaches the queue. */
  private settleBaselines(id: string, revision: string, models: NonNullable<CommandReceipt['models']>) {
    for (const b of this.baselines.values()) {
      for (const [u, e] of Object.entries(b.expected))
        if (e !== null && typeof e === 'object' && e.after === id) b.expected[u] = revision
      for (const [k, m] of Object.entries(b.models)) {
        const p = parsePlaceholder(m)
        const real = p && p.intent === id ? models[p.ref] : undefined
        if (p && real) b.models[k] = p.content ? real.content_id : real.content_revision_id
      }
    }
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
    // Placeholders of this intent's models become real IDs everywhere before
    // the base moves on, so neither the base nor the undo steps keep them.
    const models = receipt.models ?? {}
    const own = asConfirmed(intent, models)
    const rest = resolveAfter(queue.filter((i) => i.id !== id), id, receipt.revision).map((i) => resolveModelIDs(i, id, models))
    this.settleBaselines(id, receipt.revision, models)
    const r = revision(receipt.revision)
    if (r === revision(base.revision) + 1) {
      // Our revision follows the base directly: it becomes the base now, so
      // the next edit expects the right revisions. A receipt too large to
      // list its units, or an undo the server may settle differently, is
      // checked by a read as well.
      const unit_revs = { ...base.unit_revs }
      const changed = receipt.reset_required ? changedUnits(own, before) : receipt.changed_units
      const removed = receipt.reset_required ? removedUnits(own, before) : receipt.removed_units
      for (const u of changed) unit_revs[u] = receipt.revision
      for (const u of removed) delete unit_revs[u]
      const after = applyIntent(before, own)
      this.set({ base: { ...base, revision: receipt.revision, unit_revs, title: after.title, scene: after.scene }, queue: rest })
      // New models' summaries come with the next read.
      if (receipt.reset_required || intent.projection || Object.keys(models).length) void this.read()
    } else {
      this.set({ queue: rest })
      // Someone else's revision came in between, or the receipt has no
      // detail: read the scene instead of guessing.
      if (r > revision(base.revision)) void this.read()
    }
    for (const o of this.observers) o.confirmed?.(own, receipt, before)
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
    // Model failures have codes of their own: the trash and missing-document
    // branches above must never see them.
    if (f.code === 'creative_upload_unavailable') {
      this.reject(intent, 'invalid', '模型上传已失效，请重新生成')
      return
    }
    if (f.code === 'creative_model_unavailable') {
      this.reject(intent, 'invalid', f.message || '模型不可用')
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
   * Only an intent that adds objects or reparents is rebased on its own, and
   * only when nothing but objects moved and every target parent is still a
   * group.
   */
  private async conflict(intent: Intent, f: FailureLike) {
    const units = this.conflictUnits(f)
    if (!intent.projection && intent.ops.length > 0 && intent.ops.every((op) => op.op === 'reparent_objects')) {
      await this.rebaseReparents(intent, units)
      return
    }
    // A new model's commit travels with its add and goes out again with it:
    // the failed command consumed no upload.
    const addsOnly =
      (intent.ops.some((op) => op.op === 'add_objects') && intent.ops.every((op) => op.op === 'add_objects' || op.op === 'commit_model')) ||
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

  /**
   * A grouping edit whose objects collection changed under it: read the
   * latest, check what it moves and where to are still there, put clashing
   * orders last and send it again. A placement conflict is someone else's
   * edit of the same object and goes to the prompt.
   */
  private async rebaseReparents(intent: Intent, units: string[]) {
    if (!(units.length && units.every((u) => u === 'objects')) || this.rebased.has(intent.id)) {
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
    const ops: WireOp[] = []
    for (const op of intent.ops) {
      const fitted = op.op === 'reparent_objects' ? rebaseReparent(op, doc.scene.objects) : null
      if (!fitted) {
        this.reject(intent, 'conflict', this.conflictMessage(intent, units))
        return
      }
      ops.push(fitted)
    }
    const rebased: Intent = { ...intent, ops, state: 'pending', expected: { ...intent.expected, objects: doc.unit_revs.objects ?? null } }
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
    this.breakBaselines(new Set([intent.id, ...deps]))
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
    this.breakBaselines(new Set(waiting.map((i) => i.id)))
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
    // Each intent goes back under a new id; later ones that name its models
    // by placeholder follow it.
    const renamed = new Map<string, string>()
    for (const original of group.intents) {
      const i = mapModelIDs(original, (m) => {
        const p = parsePlaceholder(m)
        const to = p && renamed.get(p.intent)
        return p && to ? m.replace(`:${p.intent}:`, `:${to}:`) : m
      })
      let ops = withUsedRefs(i.ops.map((op) => keepPresent(op, present)).filter((op): op is WireOp => !!op))
      if (!ops.length) continue
      // A grouping edit keeps where things are now, with its way back to match.
      let inverse = i.inverse
      if (inverse) {
        const fitted = ops.length === 1 && ops[0].op === 'reparent_objects' ? refitReparent(ops[0], this.state.view?.scene.objects ?? []) : null
        if (!fitted) continue
        ops = [fitted.op]
        inverse = fitted.inverse
      }
      const again = this.edit(ops, i.label, { projection: i.projection, step: i.step, ...(inverse ? { inverse } : {}) })
      if (again) renamed.set(i.id, again.id)
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
    if (op.op === 'commit_model') {
      ops.push(op)
      continue
    }
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
    // A grouping edit is all or nothing: half a group or half an ungroup
    // would leave things somewhere nobody chose. Its new groups count as
    // present.
    case 'reparent_objects': {
      const created = new Set((op.create_groups ?? []).map((g) => g.id))
      const there = (id: string | null) => id === null || present.has(id) || created.has(id)
      const whole = op.moves.every((m) => present.has(m.object_id) && there(m.parent_id)) && (op.remove_groups ?? []).every((id) => present.has(id))
      return whole ? op : null
    }
    case 'set_instance_model': {
      const targets = instanceTargets(op).filter((id) => present.has(id))
      if (!targets.length) return null
      return op.scope === 'all_instances' ? { ...op, object_id: targets.includes(op.object_id) ? op.object_id : targets[0], object_ids: targets } : op
    }
    default:
      return op
  }
}

/** Drops commits no remaining operation places; the server requires every ref used. */
function withUsedRefs(ops: WireOp[]): WireOp[] {
  const used = new Set<string>()
  for (const op of ops) {
    if (op.op === 'add_objects') for (const o of op.objects) if (o.model?.ref !== undefined) used.add(o.model.ref)
    if (op.op === 'set_instance_model' && op.model.ref !== undefined) used.add(op.model.ref)
  }
  const kept = ops.filter((op) => op.op !== 'commit_model' || used.has(op.ref))
  return kept.every((op) => op.op === 'commit_model') ? [] : kept
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
