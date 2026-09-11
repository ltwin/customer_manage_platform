import type { Journal, JournalStorage, Job } from './journal.ts'
import { graphReadSet, selectionRoots } from './graph.ts'
import { emptyJournal } from './journal.ts'
import { ConnectionOverlay } from './connectionOverlay.ts'
import type { Canvas, CanvasNode, Command } from './api.ts'

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

// One immutable operation may be pending per editor session. Further edits stay drafts.
export class SaveQueue {
  readonly connections = new ConnectionOverlay()
  value: Journal = emptyJournal()
  busy = false
  localPending = 0
  private closed = false
  private placementRevisions = new Map<string, string>()
  private writes: Promise<void> = Promise.resolve()
  private storage: JournalStorage
  private transport: QueueTransport
  private changed: () => void
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
    this.value = await this.storage.load()
    if (this.value.job) this.connections.stage(this.value.job)
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
  stagePosition(canvasID: string, node: CanvasNode, x: number, y: number) {
    const key = `${canvasID}:${node.id}`
    const previous = this.value.positions?.[key]
    const ownRevision = this.placementRevisions.get(key)
    if (!Number.isFinite(x) || !Number.isFinite(y)) return Promise.resolve()
    if (
      previous
        ? previous.x === x && previous.y === y
        : node.metadata.x === x &&
          node.metadata.y === y &&
          (!ownRevision ||
            BigInt(ownRevision) <= BigInt(node.placement_revision))
    )
      return Promise.resolve()
    let revision =
      previous &&
      (!previous.synced ||
        BigInt(previous.revision) > BigInt(node.placement_revision))
        ? previous.revision
        : node.placement_revision
    // A drag can begin before our previous receipt and end after its snapshot.
    if (ownRevision && BigInt(ownRevision) > BigInt(revision))
      revision = ownRevision
    return this.update({
      ...this.value,
      positions: {
        ...this.value.positions,
        [key]: {
          canvasID,
          nodeID: node.id,
          x,
          y,
          revision,
          token: crypto.randomUUID(),
          synced: false,
        },
      },
    })
  }
  stagePositions(
    canvas: Canvas,
    moves: { node: CanvasNode; x: number; y: number }[],
  ) {
    const roots = new Set(
      selectionRoots(
        canvas.nodes,
        moves.map((m) => m.node.id),
      ),
    )
    const batchID = crypto.randomUUID(),
      positions = { ...this.value.positions }
    const readSet = graphReadSet(canvas)
    for (const r of readSet) {
      const own = this.placementRevisions.get(`${canvas.id}:${r.id}`)
      if (
        own &&
        r.placement_revision &&
        BigInt(own) > BigInt(r.placement_revision)
      )
        r.placement_revision = own
    }
    for (const m of moves) {
      if (
        !roots.has(m.node.id) ||
        !Number.isFinite(m.x) ||
        !Number.isFinite(m.y)
      )
        continue
      const key = `${canvas.id}:${m.node.id}`,
        previous = positions[key],
        own = this.placementRevisions.get(key)
      if (
        previous
          ? previous.x === m.x && previous.y === m.y
          : m.node.metadata.x === m.x &&
            m.node.metadata.y === m.y &&
            (!own || BigInt(own) <= BigInt(m.node.placement_revision))
      )
        continue
      positions[key] = {
        canvasID: canvas.id,
        nodeID: m.node.id,
        x: m.x,
        y: m.y,
        revision:
          previous?.revision ??
          this.placementRevisions.get(key) ??
          m.node.placement_revision,
        token: crypto.randomUUID(),
        synced: false,
        batchID,
        readSet,
      }
    }
    return this.update({ ...this.value, positions })
  }
  async sendNextPosition(canvasID: string) {
    if (this.closed || this.busy || this.value.job) return
    const entry = Object.entries(this.value.positions ?? {}).find(
      ([, p]) => p.canvasID === canvasID && !p.synced,
    )
    if (!entry) return
    const [key, p] = entry
    if (p.batchID) {
      const batch = Object.entries(this.value.positions ?? {}).filter(
        ([, v]) =>
          v.canvasID === canvasID && v.batchID === p.batchID && !v.synced,
      )
      const readSet = structuredClone(p.readSet ?? [])
      for (const [, position] of batch) {
        const r = readSet.find((r) => r.id === position.nodeID)
        if (r) r.placement_revision = position.revision
      }
      return this.enqueue(
        `/canvases/${canvasID}/commands`,
        {
          type: 'batch',
          read_set: readSet,
          actions: batch.map(([, v]) => ({
            type: 'move_node',
            node_id: v.nodeID,
            x: v.x,
            y: v.y,
          })),
        } satisfies Command,
        undefined,
        undefined,
        batch.map(([key, v]) => ({ key, token: v.token })),
      )
    }
    return this.enqueue(
      `/canvases/${canvasID}/commands`,
      {
        type: 'move_node',
        node_id: p.nodeID,
        x: p.x,
        y: p.y,
        expected_placement_revision: p.revision,
      } satisfies Command,
      undefined,
      { key, token: p.token },
    )
  }
  reconcilePositions(canvas: Canvas) {
    const positions = { ...this.value.positions }
    let changed = false
    for (const [key, p] of Object.entries(positions)) {
      if (p.canvasID !== canvas.id || !p.synced) continue
      const node = canvas.nodes.find((n) => n.id === p.nodeID)
      if (!node || BigInt(node.placement_revision) >= BigInt(p.revision)) {
        delete positions[key]
        changed = true
      }
    }
    return changed
      ? this.update({ ...this.value, positions })
      : Promise.resolve()
  }
  async enqueue(
    path: string,
    payload: unknown,
    draftKey?: string,
    position?: Job['position'],
    positionBatch?: Job['positions'],
  ): Promise<unknown> {
    if (this.busy || this.value.job)
      throw new Error('请先处理上一次保存，再提交新的修改')
    const operation = crypto.randomUUID()
    const job: Job = {
      path,
      operation,
      draftKey,
      position,
      positions: positionBatch,
      draftValue: draftKey ? this.value.drafts[draftKey] : undefined,
      state: 'ready',
      message: '',
      body: JSON.stringify({
        operation_id: operation,
        client_created_at: new Date().toISOString(),
        payload,
      }),
    }
    this.connections.stage(job)
    // Preparation is part of the active save, never an idle recovery prompt.
    this.busy = true
    this.changed()
    try {
      await this.update({ ...this.value, job })
    } catch (error) {
      this.connections.remove(job.operation)
      throw error
    } finally {
      this.busy = false
      this.changed()
    }
    return this.flush(false)
  }
  async flush(recover = true): Promise<unknown> {
    if (this.closed || this.busy || !this.value.job) return
    const job = structuredClone(this.value.job)
    if (job.state === 'rejected') return
    this.connections.stage(job)
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
      if (
        definitive ||
        (!uncertain &&
          (code === 'creative_revision_conflict' ||
            code === 'archived_read_only' ||
            code === 'creative_asset_trashed'))
      )
        this.connections.remove(job.operation)
      await this.update({
        ...this.value,
        job: {
          ...job,
          state:
            definitive ||
            (!uncertain &&
              (code === 'creative_revision_conflict' ||
                code === 'archived_read_only' ||
                code === 'creative_asset_trashed'))
              ? 'rejected'
              : 'unknown',
          message: errorMessage(e),
        },
      })
    } finally {
      this.busy = false
      this.changed()
    }
  }
  private async complete(job: Job, result: unknown) {
    const drafts = { ...this.value.drafts }
    if (
      job.draftKey &&
      JSON.stringify(drafts[job.draftKey]) === JSON.stringify(job.draftValue)
    )
      delete drafts[job.draftKey]
    const positions = { ...this.value.positions }
    if (job.positions) {
      if (
        !result ||
        typeof result !== 'object' ||
        !('object_results' in result) ||
        !Array.isArray(result.object_results)
      )
        throw new Error('画布保存结果无法识别，请恢复原操作')
      for (const submitted of job.positions) {
        const nodeID = submitted.key.slice(submitted.key.indexOf(':') + 1)
        const matches = result.object_results.filter(
          (value: unknown) =>
            value &&
            typeof value === 'object' &&
            'id' in value &&
            value.id === nodeID &&
            'kind' in value &&
            value.kind === 'node',
        )
        if (
          matches.length !== 1 ||
          !matches[0].is_live ||
          typeof matches[0].placement_revision !== 'string' ||
          !/^[1-9][0-9]*$/.test(matches[0].placement_revision)
        )
          throw new Error('位置保存回执不完整，请恢复原操作')
      }
      for (const item of result.object_results) {
        if (
          !item ||
          typeof item !== 'object' ||
          typeof item.id !== 'string' ||
          item.kind !== 'node'
        )
          continue
        const rev: unknown = item.placement_revision
        if (typeof rev !== 'string' || !/^[1-9][0-9]*$/.test(rev))
          throw new Error('布局版本无法识别')
        const pathCanvas = job.path.split('/')[2],
          key = `${pathCanvas}:${item.id}`
        this.placementRevisions.set(key, rev)
        for (const p of Object.values(positions)) {
          if (p.canvasID !== pathCanvas) continue
          const observed = p.readSet?.find((r) => r.id === item.id)
          const submitted = JSON.parse(job.body).payload.read_set.find(
            (r: { id: string }) => r.id === item.id,
          )
          if (
            observed &&
            submitted &&
            observed.placement_revision === submitted.placement_revision
          )
            observed.placement_revision = rev
        }
        const current = positions[key],
          submitted = job.positions.find((p) => p.key === key)
        if (current && submitted)
          positions[key] = {
            ...current,
            revision: rev,
            synced: current.token === submitted.token,
          }
      }
    }
    if (job.position) {
      if (
        !result ||
        typeof result !== 'object' ||
        !('placement_revision' in result) ||
        typeof result.placement_revision !== 'string' ||
        !/^[1-9][0-9]*$/.test(result.placement_revision)
      )
        throw new Error('位置保存结果无法识别，请恢复原操作')
      this.placementRevisions.set(job.position.key, result.placement_revision)
      const current = positions[job.position.key]
      if (current)
        positions[job.position.key] = {
          ...current,
          revision: result.placement_revision,
          synced: current.token === job.position.token,
        }
    }
    this.connections.confirm(job, result)
    await this.update({ ...this.value, job: null, drafts, positions })
  }
  async dismissRejected() {
    if (this.busy || this.value.job?.state !== 'rejected') return
    const positions = { ...this.value.positions }
    if (this.value.job.position) delete positions[this.value.job.position.key]
    for (const p of this.value.job.positions ?? []) delete positions[p.key]
    await this.update({ ...this.value, job: null, positions })
  }
}
