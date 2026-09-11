import type { Journal, JournalStorage, Job } from './journal.ts'
import { canvasHistory, selectionRoots } from './graph.ts'
import { emptyJournal } from './journal.ts'
import type { Canvas, CanvasNode, Command } from './api.ts'
import {
  coalesce,
  intentReadSet,
  localNodeIDs,
  projectCanvas,
  type Intent,
  type Receipt,
} from './outbox.ts'

export type QueueTransport = {
  send(job: Job): Promise<unknown>
  lookup(operation: string): Promise<unknown>
}
const statusOf = (error: unknown) =>
  error && typeof error === 'object' && 'status' in error
    ? error.status
    : undefined
const codeOf = (error: unknown) =>
  error && typeof error === 'object' && 'code' in error ? error.code : undefined
export const errorMessage = (e: unknown): string =>
  e instanceof Error ? e.message : '请求结果待确认，请保留草稿后重试'

const record = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v)
const revision = (v: unknown): v is string =>
  typeof v === 'string' && /^[1-9][0-9]*$/.test(v)
// Outcomes decided on this machine: the edit never became a server error.
export const withdrawn = {
  cancelled: '已在本机撤销',
  rejected: '保存被拒绝，本机修改已撤回',
}
export const isWithdrawn = (e: unknown) =>
  e instanceof Error && Object.values(withdrawn).includes(e.message)

// One immutable request is in flight per editor session; everything else
// waits locally. Canvas edits queue in the outbox and are projected onto the
// canvas at once, so the editor never waits for the server to answer.
export class SaveQueue {
  value: Journal = emptyJournal()
  busy = false
  localPending = 0
  private closed = false
  private writes: Promise<void> = Promise.resolve()
  private storage: JournalStorage
  private transport: QueueTransport
  private changed: () => void
  private waiters: (() => void)[] = []
  private confirmations = new Map<
    string,
    { resolve: (r: Receipt) => void; reject: (e: Error) => void }
  >()
  constructor(
    storage: JournalStorage,
    transport: QueueTransport,
    changed: () => void,
  ) {
    this.storage = storage
    this.transport = transport
    this.changed = changed
  }
  async close(): Promise<void> {
    this.closed = true
    await this.writes.catch(() => {})
  }
  async load() {
    const loaded = await this.storage.load()
    // Journals written before moves joined the outbox kept drag targets in
    // `positions`; unsynced ones become ordinary move intents.
    const { positions, ...rest } = loaded as Journal & {
      positions?: Record<
        string,
        {
          canvasID: string
          nodeID: string
          x: number
          y: number
          synced: boolean
        }
      >
    }
    this.value = rest
    for (const p of Object.values(positions ?? {})) {
      if (p.synced) continue
      this.value = {
        ...this.value,
        outbox: coalesce(this.outbox, {
          id: crypto.randomUUID(),
          canvasID: p.canvasID,
          kind: 'actions',
          state: 'pending',
          actions: [{ type: 'move_node', node_id: p.nodeID, x: p.x, y: p.y }],
        }).outbox,
      }
    }
    if (positions) await this.update(this.value)
    this.changed()
  }
  update(next: Journal): Promise<void> {
    if (this.closed) return Promise.reject(new Error('编辑会话已关闭'))
    this.value = structuredClone(next)
    const copy = structuredClone(next)
    this.changed()
    this.localPending++
    const write = this.writes
      .catch(() => {})
      .then(() => this.storage.save(copy))
      .finally(() => {
        this.localPending--
        this.changed()
      })
    this.writes = write
    return write
  }
  // claim takes the single request slot once nothing is in flight. The check
  // and the claim run in one continuation, so two callers cannot both win.
  // A settled job needing recovery still refuses new work: its outcome is
  // never silently overtaken.
  private async claim() {
    while (this.busy)
      await new Promise<void>((resolve) => this.waiters.push(resolve))
    if (this.value.job) throw new Error('请先处理上一次保存，再提交新的修改')
    this.busy = true
    this.changed()
  }
  private release() {
    const waiting = this.waiters
    this.waiters = []
    for (const resolve of waiting) resolve()
  }
  get outbox(): Intent[] {
    return this.value.outbox ?? []
  }
  // stageIntent records a canvas edit locally and returns its identity plus a
  // promise for the server receipt. Layout bursts merge into the pending tail.
  stageIntent(
    canvasID: string,
    intent: Omit<Intent, 'id' | 'canvasID' | 'state'>,
  ): { id: string; done: Promise<Receipt> } {
    const next: Intent = {
      ...intent,
      id: crypto.randomUUID(),
      canvasID,
      state: 'pending',
    }
    const merged = coalesce(this.outbox, next)
    const done = new Promise<Receipt>((resolve, reject) => {
      this.confirmations.set(next.id, { resolve, reject })
    })
    done.catch(() => {})
    if (merged.id !== next.id) {
      // Merged into the tail: settle alongside it.
      const tail = this.confirmations.get(merged.id)
      this.confirmations.set(merged.id, {
        resolve: (r) => {
          tail?.resolve(r)
          this.confirmations.get(next.id)?.resolve(r)
        },
        reject: (e) => {
          tail?.reject(e)
          this.confirmations.get(next.id)?.reject(e)
        },
      })
    }
    void this.update({ ...this.value, outbox: merged.outbox }).catch(
      (e: unknown) => this.settle(next.id, errorMessage(e)),
    )
    return { id: merged.id, done }
  }
  // cancelTail drops the newest intent when it has not left this machine.
  cancelTail(canvasID: string): Intent | null {
    const outbox = this.outbox
    const tail = outbox.at(-1)
    if (!tail || tail.canvasID !== canvasID || tail.state !== 'pending')
      return null
    this.settle(tail.id, withdrawn.cancelled)
    void this.update({ ...this.value, outbox: outbox.slice(0, -1) })
    return tail
  }
  private settle(id: string, error: string | null, receipt?: Receipt) {
    const waiter = this.confirmations.get(id)
    this.confirmations.delete(id)
    if (!waiter) return
    if (receipt) waiter.resolve(receipt)
    else waiter.reject(new Error(error ?? '操作未能保存'))
  }
  // pruneOutbox forgets confirmed intents once the snapshot includes them.
  pruneOutbox(canvas: Canvas) {
    const kept = this.outbox.filter(
      (i) =>
        i.canvasID !== canvas.id ||
        !i.receipt ||
        BigInt(i.receipt.result_revision) > BigInt(canvas.revision),
    )
    return kept.length === this.outbox.length
      ? Promise.resolve()
      : this.update({ ...this.value, outbox: kept })
  }
  // sendNextIntent turns the head pending intent into one immutable request.
  // Positions of already committed nodes go first. The read set comes from
  // the snapshot plus every receipt before this intent (revisions the
  // snapshot may lack), never from the intent's own projected effect.
  async sendNextIntent(canvasID: string, canvas: Canvas) {
    if (this.closed || this.busy || this.value.job || canvas.id !== canvasID)
      return
    const local = localNodeIDs(this.outbox)
    // A 'sent' intent without a job never reached the journal as a request
    // (the page closed in between); it is still pending.
    const head = this.outbox.find(
      (i) => i.canvasID === canvasID && i.state !== 'confirmed',
    )
    if (!head) return
    // Everything before the head, including receipts newer than the
    // snapshot, is the state this intent was made against.
    const view = projectCanvas(
      canvas,
      this.outbox.slice(0, this.outbox.indexOf(head)),
    )
    await this.claim()
    let payload: Command
    let changeID: string | undefined
    if (head.kind === 'actions') {
      payload = {
        type: 'batch',
        expected_topology_revision: view.topology_revision,
        read_set: intentReadSet(view, head, undefined, local),
        actions: head.actions ?? [],
      }
    } else {
      const history = canvasHistory(view.changes)
      changeID = head.kind === 'undo' ? history.undo : history.redo
      if (!changeID) {
        this.settle(head.id, '没有可撤销的操作')
        try {
          await this.update({
            ...this.value,
            outbox: this.outbox.filter((i) => i.id !== head.id),
          })
        } finally {
          this.busy = false
          this.changed()
          this.release()
        }
        return
      }
      payload = {
        type: head.kind,
        change_id: changeID,
        read_set: intentReadSet(view, head, changeID, local),
      }
    }
    const outbox = this.outbox.map((i) =>
      i.id === head.id ? { ...i, state: 'sent' as const, changeID } : i,
    )
    return this.dispatch(
      { ...this.value, outbox },
      `/canvases/${canvasID}/commands`,
      payload,
      head.draftKey,
      head,
    )
  }
  // stageMoves turns a drop into one move intent for the selection roots that
  // actually moved. Bursts merge with a still-pending tail; a move landing
  // while a request is in flight simply becomes the next intent.
  stageMoves(
    view: Canvas,
    moves: { node: CanvasNode; x: number; y: number }[],
  ): { id: string; done: Promise<Receipt> } | null {
    const roots = new Set(
      selectionRoots(
        view.nodes,
        moves.map((m) => m.node.id),
      ),
    )
    const actions: Extract<
      NonNullable<Intent['actions']>[number],
      { type: 'move_node' }
    >[] = []
    for (const m of moves) {
      const current = view.nodes.find((n) => n.id === m.node.id)
      if (
        !roots.has(m.node.id) ||
        !current ||
        !Number.isFinite(m.x) ||
        !Number.isFinite(m.y) ||
        (current.metadata.x === m.x && current.metadata.y === m.y)
      )
        continue
      actions.push({ type: 'move_node', node_id: m.node.id, x: m.x, y: m.y })
    }
    if (!actions.length) return null
    return this.stageIntent(view.id, { kind: 'actions', actions })
  }
  async enqueue(
    path: string,
    payload: unknown,
    draftKey?: string,
  ): Promise<unknown> {
    await this.claim()
    return this.dispatch(this.value, path, payload, draftKey)
  }
  // dispatch persists the immutable request in the claimed slot, then sends.
  private async dispatch(
    base: Journal,
    path: string,
    payload: unknown,
    draftKey?: string,
    intent?: Intent,
  ): Promise<unknown> {
    if (this.closed) {
      this.busy = false
      return
    }
    const operation = crypto.randomUUID()
    // The submitted draft is the one captured when the edit was staged, so
    // typing that happened while it waited is never mistaken for saved.
    const job: Job = {
      path,
      operation,
      draftKey,
      intent: intent?.id,
      draftValue: intent
        ? intent.draftValue
        : draftKey
          ? this.value.drafts[draftKey]
          : undefined,
      state: 'ready',
      message: '',
      body: JSON.stringify({
        operation_id: operation,
        client_created_at: new Date().toISOString(),
        payload,
      }),
    }
    // Preparation is part of the active save, never an idle recovery prompt.
    try {
      await this.update({ ...base, job })
    } catch (error) {
      if (intent) {
        this.settle(intent.id, errorMessage(error))
        await this.update({
          ...this.value,
          outbox: this.outbox.filter((i) => i.id !== intent.id),
        }).catch(() => {})
      }
      throw error
    } finally {
      this.busy = false
      this.changed()
      this.release()
    }
    return this.flush(false)
  }
  async flush(recover = true): Promise<unknown> {
    if (this.closed || this.busy || !this.value.job) return
    const job = structuredClone(this.value.job)
    if (job.state === 'rejected') return
    this.busy = true
    this.changed()
    let uncertain = recover || job.state === 'unknown'
    try {
      // Durable 'unknown' is written BEFORE dispatch, covering a crash during fetch.
      await this.update({
        ...this.value,
        job: { ...job, state: 'unknown', message: '正在确认保存结果' },
      })
      if (this.closed) return
      if (recover) {
        try {
          const result = await this.transport.lookup(job.operation)
          if (this.closed) return
          await this.complete(job, result)
          return result
        } catch (e) {
          if (statusOf(e) !== 404) throw e
        }
      }
      if (this.closed) return
      const result = await this.transport.send(job)
      if (this.closed) return
      await this.complete(job, result)
      return result
    } catch (e) {
      if (this.closed) return
      const code = codeOf(e)
      const definitive =
        !uncertain &&
        (statusOf(e) === 400 ||
          statusOf(e) === 403 ||
          code === 'creative_revision_conflict' ||
          code === 'archived_read_only' ||
          code === 'creative_asset_trashed' ||
          statusOf(e) === 422 ||
          statusOf(e) === 404)
      // Version/state conflicts prove this operation never committed even after a retry:
      // an existing receipt would have replayed before these domain guards.
      if (
        code === 'creative_revision_conflict' ||
        code === 'archived_read_only' ||
        code === 'creative_asset_trashed'
      )
        uncertain = false
      const rejected =
        definitive ||
        (!uncertain &&
          (code === 'creative_revision_conflict' ||
            code === 'archived_read_only' ||
            code === 'creative_asset_trashed'))
      // A rejected intent and everything queued after it on that canvas are
      // dropped now: the projection must not keep showing a refused edit.
      const outbox =
        rejected && job.intent ? this.dropFrom(job.intent) : this.outbox
      await this.update({
        ...this.value,
        outbox,
        job: {
          ...job,
          state: rejected ? 'rejected' : 'unknown',
          message: errorMessage(e),
        },
      })
    } finally {
      this.busy = false
      this.changed()
      this.release()
    }
  }
  private dropFrom(intentID: string): Intent[] {
    const outbox = this.outbox
    const index = outbox.findIndex((i) => i.id === intentID)
    if (index < 0) return outbox
    const canvasID = outbox[index]!.canvasID
    const kept: Intent[] = []
    for (const [i, intent] of outbox.entries()) {
      if (i >= index && intent.canvasID === canvasID) {
        this.settle(intent.id, withdrawn.rejected)
        continue
      }
      kept.push(intent)
    }
    return kept
  }
  private async complete(job: Job, result: unknown) {
    const drafts = { ...this.value.drafts }
    if (
      job.draftKey &&
      JSON.stringify(drafts[job.draftKey]) === JSON.stringify(job.draftValue)
    )
      delete drafts[job.draftKey]
    let outbox = this.outbox
    if (job.intent) {
      const intent = outbox.find((i) => i.id === job.intent)
      if (
        !record(result) ||
        typeof result.change_id !== 'string' ||
        !revision(result.result_revision) ||
        !revision(result.result_topology_revision) ||
        !Array.isArray(result.object_results)
      )
        throw new Error('画布保存回执无法识别，请恢复原操作')
      const ids = (v: unknown) =>
        Array.isArray(v)
          ? v.filter((x): x is string => typeof x === 'string')
          : []
      const receipt: Receipt = {
        change_id: result.change_id,
        inverse_of:
          intent && intent.kind !== 'actions'
            ? (intent.changeID ?? null)
            : null,
        result_revision: result.result_revision,
        result_topology_revision: result.result_topology_revision,
        object_results: result.object_results as Receipt['object_results'],
        created_ids: ids(result.created_ids),
        omitted_reference_ids: ids(result.omitted_reference_ids),
      }
      outbox = outbox.map((i) =>
        i.id === job.intent
          ? { ...i, state: 'confirmed' as const, receipt }
          : i,
      )
      this.settle(job.intent, null, receipt)
    }
    await this.update({ ...this.value, job: null, drafts, outbox })
  }
  async dismissRejected() {
    if (this.busy || this.value.job?.state !== 'rejected') return
    await this.update({ ...this.value, job: null })
    this.release()
  }
}
