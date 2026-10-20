import { createStore, type StoreApi } from 'zustand/vanilla'
import { onCanvasInvalidate } from '../creative-canvas/editor/canvasEvents.ts'
import { listSceneJobs, readRevisions, type RevisionItem, type SceneJobSummary, type SceneObject, type SceneTransform } from './api.ts'
import type { After } from './sandbox/host.ts'
import { objectOfUnit } from './state/apply.ts'

// What the assistant is doing to the open scene, for the editor to show:
// objects a bake will replace or the assistant changed in the last minute
// (outlined), models it is adding (placeholders) and the status line. Read on
// opening and after canvas notifications, at most once a second: creating and
// finishing a job notifies, and so does every revision.

export const recentMs = 60_000
const throttleMs = 1_000
const firstPage = 20

/** An assistant revision as the editor saw it. */
export type Recent = { revision: string; at: number; objects: string[] }

export type ActivityState = {
  jobs: SceneJobSummary[]
  /** The assistant's revisions of the last minute, oldest first. */
  recent: Recent[]
}

/** Where a model the assistant is adding will appear. */
export type Placeholder = { id: string; name: string; parentID: string | null; transform: SceneTransform }

export type ActivityDeps = {
  listJobs(documentID: string, signal: AbortSignal): Promise<SceneJobSummary[]>
  readRevisions(documentID: string, since: string | null, signal: AbortSignal): Promise<RevisionItem[]>
  now(): number
  after: After
  subscribe(listener: () => void): () => void
}

const pageDeps: ActivityDeps = {
  listJobs: listSceneJobs,
  readRevisions: (id, since, signal) =>
    readRevisions(id, null, signal, since ? { since } : { limit: firstPage }).then((page) => page.items),
  now: () => Date.now(),
  after: (ms, fn) => {
    const t = window.setTimeout(fn, ms)
    return () => window.clearTimeout(t)
  },
  subscribe: onCanvasInvalidate,
}

type AddTarget = { op: 'add'; object?: { name?: string; parent_id?: string | null; transform?: SceneTransform | null } }
type ReplaceTarget = { op: 'replace'; object_ids?: string[] }

function bakeTarget(job: SceneJobSummary): AddTarget | ReplaceTarget | null {
  if (job.kind !== 'bake_model' || !job.target) return null
  const op = job.target.op
  return op === 'add' || op === 'replace' ? (job.target as AddTarget | ReplaceTarget) : null
}

/** The objects a revision changed or annotated; what it removed is gone. */
export function revisionObjects(item: Pick<RevisionItem, 'changed_units' | 'annotated_units'>): string[] {
  const ids = new Set<string>()
  for (const unit of [...item.changed_units, ...item.annotated_units]) {
    const o = objectOfUnit(unit)
    if (o) ids.add(o.id)
  }
  return [...ids]
}

/** Outlined as the assistant's: what a bake will replace, and what it changed in the last minute. */
export function highlightedObjects(state: ActivityState): Set<string> {
  const ids = new Set<string>()
  for (const job of state.jobs) {
    const t = bakeTarget(job)
    if (t?.op === 'replace') for (const id of t.object_ids ?? []) ids.add(id)
  }
  for (const r of state.recent) for (const id of r.objects) ids.add(id)
  return ids
}

const origin: SceneTransform = { position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] }

export function jobPlaceholders(jobs: readonly SceneJobSummary[]): Placeholder[] {
  const out: Placeholder[] = []
  for (const job of jobs) {
    const t = bakeTarget(job)
    if (t?.op !== 'add') continue
    const transform = t.object?.transform
    out.push({
      id: job.id,
      name: t.object?.name ?? '',
      parentID: t.object?.parent_id ?? null,
      transform: transform?.position && transform.rotation && transform.scale ? transform : origin,
    })
  }
  return out
}

/** The status line while the assistant bakes for this scene. */
export function activityText(jobs: readonly SceneJobSummary[], byID: ReadonlyMap<string, SceneObject>): string {
  const names: string[] = []
  for (const job of jobs) {
    const t = bakeTarget(job)
    if (!t) continue
    names.push(t.op === 'add' ? (t.object?.name ?? '') : (byID.get(t.object_ids?.[0] ?? '')?.name ?? ''))
  }
  if (!names.length) return ''
  const first = names.find(Boolean)
  if (names.length === 1) return first ? `助手正在生成「${first}」…` : '助手正在生成模型…'
  return first ? `助手正在生成「${first}」等 ${names.length} 个模型…` : `助手正在生成 ${names.length} 个模型…`
}

export class AgentActivity {
  readonly store: StoreApi<ActivityState> = createStore<ActivityState>(() => ({ jobs: [], recent: [] }))
  private readonly documentID: string
  private readonly deps: ActivityDeps
  /** The newest revision read; later reads ask only for what came after it. */
  private newest: string | null = null
  private busy = false
  private again = false
  private throttled: (() => void) | null = null
  private expiry: (() => void) | null = null
  private controller: AbortController | null = null

  constructor(documentID: string, deps: ActivityDeps = pageDeps) {
    this.documentID = documentID
    this.deps = deps
  }

  start(): () => void {
    this.controller = new AbortController()
    const unsubscribe = this.deps.subscribe(() => this.invalidated())
    void this.refresh()
    return () => {
      unsubscribe()
      this.controller?.abort()
      this.controller = null
      this.throttled?.()
      this.throttled = null
      this.expiry?.()
      this.expiry = null
    }
  }

  invalidated(): void {
    if (this.throttled || !this.controller) return
    this.throttled = this.deps.after(throttleMs, () => {
      this.throttled = null
      void this.refresh()
    })
  }

  async refresh(): Promise<void> {
    if (this.busy) {
      this.again = true
      return
    }
    this.busy = true
    try {
      do {
        this.again = false
        await this.read()
      } while (this.again && this.controller)
    } finally {
      this.busy = false
    }
  }

  private async read() {
    const signal = this.controller?.signal
    if (!signal || signal.aborted) return
    const [jobs, revisions] = await Promise.allSettled([
      this.deps.listJobs(this.documentID, signal),
      this.deps.readRevisions(this.documentID, this.newest, signal),
    ])
    if (signal.aborted) return
    const now = this.deps.now()
    const next: Partial<ActivityState> = {}
    if (jobs.status === 'fulfilled') next.jobs = jobs.value
    if (revisions.status === 'fulfilled') {
      const first = this.newest === null
      const items = revisions.value
      if (items[0]) this.newest = items[0].revision
      else if (first) this.newest = '0'
      const seen: Recent[] = []
      for (const item of items) {
        if (item.actor_kind !== 'agent') continue
        // The first page is dated by the server; later ones are new as they arrive.
        const at = first ? Math.min(Date.parse(item.created_at), now) : now
        if (now - at < recentMs) seen.push({ revision: item.revision, at, objects: revisionObjects(item) })
      }
      const known = new Set(this.store.getState().recent.map((r) => r.revision))
      next.recent = [...this.live(now), ...seen.filter((r) => !known.has(r.revision)).reverse()]
    }
    this.store.setState(next)
    this.schedule()
  }

  private live(now: number): Recent[] {
    return this.store.getState().recent.filter((r) => now - r.at < recentMs)
  }

  /** Drops each revision's outline a minute after it was seen. */
  private schedule() {
    this.expiry?.()
    this.expiry = null
    const oldest = this.store.getState().recent[0]
    if (!oldest || !this.controller) return
    const wait = Math.max(oldest.at + recentMs - this.deps.now(), 0)
    this.expiry = this.deps.after(wait, () => {
      this.expiry = null
      this.store.setState({ recent: this.live(this.deps.now()) })
      this.schedule()
    })
  }
}

/** The objects an edit touches, for the soft notice. */
export function editedObjects(ops: readonly unknown[]): string[] {
  const ids = new Set<string>()
  const add = (unit: unknown) => {
    const o = typeof unit === 'string' ? objectOfUnit(unit) : null
    if (o) ids.add(o.id)
  }
  const walk = (v: unknown) => {
    if (Array.isArray(v)) return v.forEach(walk)
    if (!v || typeof v !== 'object') return
    for (const [k, x] of Object.entries(v)) {
      if (k === 'object_id' && typeof x === 'string') ids.add(x)
      else if ((k === 'object_ids' || k === 'remove_groups') && Array.isArray(x)) x.forEach((id) => typeof id === 'string' && ids.add(id))
      else if (k === 'units' && Array.isArray(x)) x.forEach(add)
      else walk(x)
    }
  }
  walk(ops)
  return [...ids]
}
