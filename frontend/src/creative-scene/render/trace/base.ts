import { Box3, type Matrix4, type Mesh, type Scene } from 'three'
import { slotOf } from '../../materials/instances.ts'
import { backgroundTicker } from '../../host/ticker.ts'
import { gpuTestHooks, type SceneGPU } from '../gpu.ts'
import type { Segment } from './build.worker.ts'
import { BUDGET, BYTES_PER_TRIANGLE, SPARE, buildsOriginal, epsilonAt, levelFor, tierOf } from './capacity.ts'
import { collectLights, collectMeshes, type Slot, type Traced } from './collect.ts'
import { ArraysDropped, CapacityError, TraceLayer, type Instance } from './layer.ts'
import { buildPool, GeometryFailed, type Builds } from './pool.ts'
import { TraceTables } from './tables.ts'

// The world-space layer's base on one device (S1 design「底座的状态」
// 「持有关系」, D1): what the scene is, packed for the GPU at the tier the
// device takes, kept in step with the mounted scene frame by frame. The
// viewport commits each geometry as its build comes back and traces what is
// in; an export waits for every build and packs once, in a fixed order
// (L3). Nothing here reads the scene document (L4).
//
// Two precisions, the original and the proxies, both served at tier 1:
// the reflections trace the original, the probes the proxies (S2 design
// 「底座的两份精度」; S2 revises S1's D3, the proxies are always built). At
// tier 2 the reflections trace the proxies too. The base is ready once, when
// all it is building is in: a pipeline is built once, not first for the
// proxies and again for the original. While the tier changes, what serves
// goes on serving till the other is in and judged (S1 A4).
//
// What changes the light the probes gather is counted (S2 design「编辑的
// 分类」): geometryVersion for what they trace, lightingVersion for the
// materials, lights and environment it is lit by; the probes take those
// against the counts they last saw.
//
// A geometry whose build fails is left out, the rest traced (S1 design
// A13); the layer as a whole fails on what touches all of it: a worker that
// did not load, a device error on its buffers, its shader not building.

export type TraceStatus =
  | { state: 'off'; reason: 'compatibility' | 'device' | 'disabled' }
  | { state: 'preparing'; done: number; total: number }
  | { state: 'ready'; tier: 'full' | 'proxy'; pending: number }
  | { state: 'tooLarge' }
  | { state: 'failed' }

/**
 * What the layer's users bind: reflect, the layer the reflections trace
 * (render/nodes/SSRTraceNode.js), the original at tier 1 and the proxies at
 * tier 2; probes, the one the probes trace, the proxies at either tier (the
 * original only for the e2e's gpuTestHooks.probeLayer); and the tables. key
 * changes with either layer: a pipeline is built again for another
 * (sceneRenderer.ts keyOf).
 */
export type TraceHandle = { readonly reflect: TraceLayer; readonly probes: TraceLayer; readonly tables: TraceTables; readonly tier: 'full' | 'proxy'; readonly key: string }

/** An export's wait for the base ran out (D7): the picture fails, the build goes on. */
export class TraceNotReady extends Error {
  constructor() {
    super('屏幕外的光照与反射还在准备，请稍后重试')
    this.name = 'TraceNotReady'
  }
}

/** Commits take at most this much of a frame (AC-S1-8: no task over 100 ms). */
const commitBudgetMs = 8
/**
 * How long the traced meshes stand still before a precision that fills
 * takes its segments: a scene whose models still come in (a large one
 * opening) is laid out once, not again for each model.
 */
const settleMs = 1000
/** Past this share of free ranges a layer is compacted, on a frame with nothing to commit. */
const tidyPast = 0.25
/** Once the original did not fit, it is tried again only below this share of the triangles it did not fit at: no back and forth at the edge. */
const retryShare = 0.75

type Kind = 'original' | 'proxy'

let layers = 0

/** The e2e fixture page sets __sceneTest before the editor loads (render/gpu.ts): the statistics are kept for it alone. */
const sceneTest = (globalThis as { __sceneTest?: boolean }).__sceneTest === true

/** The developer switch (Epic「回退」): localStorage scene.trace = off, or the e2e's traceOff. */
function switchedOff(): boolean {
  if (gpuTestHooks.traceOff) return true
  try {
    return localStorage.getItem('scene.trace') === 'off'
  } catch {
    return false
  }
}

/** One precision of the scene: its layer, its segments by state, and the meshes its top level holds. */
class Precision {
  readonly kind: Kind
  readonly layer: TraceLayer
  /** Segment keys asked for and not back. */
  readonly building = new Set<string>()
  /** Segments back and not in the layer yet, with their geometry's key. */
  readonly arrived = new Map<string, { geometry: string; segment: Segment }>()
  /** The segment in the layer each geometry is traced with. */
  readonly current = new Map<string, string>()
  /** A segment did not fit the device (CapacityError): no more commits till the scene changes. */
  blocked = false
  /** Which layer this is, over the page's life. */
  readonly serial = ++layers
  /** The top level's meshes, in setInstances' order, with the segment each drew and its matrix then. */
  order: { mesh: Mesh; segment: string; matrix: number[] }[] = []

  constructor(kind: Kind, maxBinding: number, release: (attribute: object) => void) {
    this.kind = kind
    this.layer = new TraceLayer(maxBinding, `trace_${kind}`, release)
  }

  /** The traced geometries not in the layer. */
  missing(geometries: ReadonlyMap<string, number>): number {
    let n = 0
    for (const key of geometries.keys()) if (!this.current.has(key)) n++
    return n
  }
}

/** A traced mesh as the last sync saw it: what it drew where, and its world bounds. */
type Placed = { geometry: unknown; matrix: number[]; box: Box3; ground: boolean }

/** The builtin ground's geometries (builtins/geometry.ts traceKey). */
const isGround = (t: Traced) => t.key.startsWith('builtin:ground@')

type Options = {
  /** The viewport's: commits as builds come back; an export packs once, in order. */
  incremental: boolean
  /** Where builds come from: the page's pool, or the unit tests' own. */
  builds?: Builds
}

export class TraceBase {
  readonly scene: Scene
  readonly incremental: boolean
  private readonly maxBinding: number
  private readonly builds: Builds
  private readonly release: (attribute: object) => void
  private status: TraceStatus
  private readonly listeners = new Set<(s: TraceStatus) => void>()
  readonly tables: TraceTables
  private readonly precisions: Partial<Record<Kind, Precision>> = {}
  /**
   * Layers dropped, their GPU buffers let go at the next sync: the frame
   * whose sync dropped one may still draw it (render() takes the handle
   * before it syncs).
   */
  private retired: TraceLayer[] = []
  /** Geometries whose build failed (GeometryFailed), not traced till they leave the scene and come back. */
  private readonly broken = new Set<string>()
  private readonly unsubscribe: (() => void)[]
  /** The precision the reflections trace: the original at tier 1, the proxies at tier 2. */
  private reflecting: Precision | null = null
  /** The precision the probes trace: the proxies, once ready. */
  private probing: Precision | null = null
  private tooLarge = false
  /** Whether the first sync with a scene has chosen whether to build the original. */
  private chosen = false
  /** Tier 2 → 1: below this many triangles, and with the estimate in the budget, the original is tried again. */
  private retryBelow = Infinity
  /** The original's bytes a triangle, as last measured (capacity.ts's estimate till then). */
  private perTriangle = BYTES_PER_TRIANGLE
  /** The proxy level each geometry is built at. */
  private readonly levels = new Map<string, number>()
  /** Last frame's traced meshes: a change retries a failure, unblocks commits and ends too large. */
  private traced: Traced[] = []
  /** What each material row holds: the traced meshes' slots, in turn. */
  private rows: Slot[] = []
  /** Each traced mesh's first row. */
  private firstRow: number[] = []
  /** A failure was retried (on the next scene change); a second one before the layer is ready again stays till the device goes. */
  private retried = false
  /** The layer failed for good: its shader did not build on this device. */
  private forGood = false
  private pumping = false
  /** When the traced meshes last changed (settleMs). */
  private changedAt = performance.now()
  /** The traced geometries' triangles as the last sync saw them: the pump commits against them. */
  private geometries: ReadonlyMap<string, number> = new Map()
  /** Segments come back, from the local cache or built (the e2e's warm start). */
  private readonly arrivals = { cached: 0, built: 0 }
  private disposed = false
  /**
   * Counts a change of what the probes trace: a traced mesh in or out,
   * shown or hidden, moved, or a geometry of it committed (S2 design「编辑
   * 的分类」).
   */
  geometryVersion = 0
  /** Counts a change of what the probes' hits are lit by: a material row, a light (unexposed), the ambient light, the environment. */
  lightingVersion = 0
  /** The traced meshes' bounds, the builtin ground left out (it reaches past the scene, lit by the sky where it does): how many probe levels the scene takes. */
  readonly bounds = new Box3()
  /** Where traced meshes changed since takeEdits last took them: where the probes relocate again once the edit is over. */
  private readonly edited = new Box3()
  /** The traced meshes' world bounds as the last sync saw them, by mesh: a mesh that goes or moves changes where it was too. */
  private placedAt = new Map<Mesh, Placed>()
  /** Asks the viewport for a frame (SceneRenderer's redraw); none offscreen. */
  redraw: (() => void) | null = null

  constructor(gpu: SceneGPU, scene: Scene, { incremental, builds = buildPool }: Options) {
    this.scene = scene
    this.incremental = incremental
    this.builds = builds
    this.release = (attribute) => gpu.release(attribute)
    this.tables = new TraceTables(this.release)
    this.unsubscribe = [
      // The layer's buffers are named for the device's messages (layer.ts, tables.ts: trace_<…>).
      gpu.onError((message) => {
        if (/\btrace_/.test(message)) this.fail()
      }),
    ]
    const capability = gpu.trace
    // The e2e's pressed budget holds each buffer too: a scene goes to tier 2, then too large.
    this.maxBinding = capability.supported ? Math.min(capability.maxBinding, gpuTestHooks.traceBudget ?? Infinity) : 0
    if (!capability.supported) this.status = { state: 'off', reason: capability.reason }
    else if (switchedOff()) this.status = { state: 'off', reason: 'disabled' }
    else this.status = { state: 'preparing', done: 0, total: 0 }
  }

  get state(): TraceStatus {
    return this.status
  }

  /** Called with each new status; returns the unsubscribe. */
  subscribe(listener: (s: TraceStatus) => void): () => void {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }

  private set(next: TraceStatus) {
    if (JSON.stringify(next) === JSON.stringify(this.status)) return
    const drawn = (s: TraceStatus) => (s.state === 'ready' ? `ready/${s.tier}` : s.state)
    const redraw = drawn(next) !== drawn(this.status)
    this.status = next
    if (next.state === 'ready' && !this.incremental && gpuTestHooks.traceDigests) void this.digest(gpuTestHooks.traceDigests)
    for (const l of this.listeners) l(next)
    // The viewport draws again when what it draws changes, not for the count of what is under way: a large scene's
    // frame is long.
    if (redraw) this.redraw?.()
  }

  /** What each material row holds (the e2e's report names its meshes by them). */
  get slots(): readonly Slot[] {
    return this.rows
  }

  /** What the reflections and the probes trace, once ready. */
  handle(): TraceHandle | null {
    const s = this.status
    const reflect = this.reflecting
    if (s.state !== 'ready' || !reflect || !this.probing) return null
    const probes = gpuTestHooks.probeLayer === 'original' && reflect.kind === 'original' ? reflect : this.probing
    return { reflect: reflect.layer, probes: probes.layer, tables: this.tables, tier: s.tier, key: `${s.tier}#${reflect.serial}+${probes.serial}` }
  }

  /** Where traced meshes changed since the last call (an empty box for nowhere), and none from then on. */
  takeEdits(): Box3 {
    const out = this.edited.clone()
    this.edited.makeEmpty()
    return out
  }

  /**
   * The layer failed as a whole (a worker did not load, the device refused
   * its buffers): nothing is traced, and the layers go, till the next scene
   * change retries, once. forGood: the reflections' shader did not build on
   * this device, and is not tried again.
   */
  fail(forGood = false) {
    if (this.status.state === 'off') return
    if (forGood) this.forGood = this.retried = true
    if (this.status.state === 'failed') return
    for (const p of Object.values(this.precisions)) this.drop(p)
    // Tried again as from the start: whether to build the original too.
    this.chosen = false
    this.set({ state: 'failed' })
  }

  private get budget(): number {
    return gpuTestHooks.traceBudget ?? BUDGET
  }

  private precision(kind: Kind): Precision {
    return (this.precisions[kind] ??= new Precision(kind, this.maxBinding, this.release))
  }

  private drop(p: Precision) {
    this.retired.push(p.layer)
    delete this.precisions[p.kind]
    if (this.probing === p) this.probing = null
    // The original dropped at tier 1: the reflections trace the proxies the probes do, if they serve.
    if (this.reflecting === p) this.reflecting = this.probing
  }

  /** Whether a precision serves: the reflections or the probes trace it. */
  private served(p: Precision): boolean {
    return p === this.reflecting || p === this.probing
  }

  /** The segment a traced geometry is built as in a precision. */
  private segmentOf(p: Precision, key: string): string {
    return p.kind === 'original' ? key : `${key}@${this.levels.get(key) ?? 0}`
  }

  private ask(p: Precision, t: Traced) {
    const segment = this.segmentOf(p, t.key)
    if (p.current.get(t.key) === segment || p.building.has(segment) || p.arrived.has(segment)) return
    p.building.add(segment)
    const level = this.levels.get(t.key) ?? 0
    const build = p.kind === 'original' ? ({ kind: 'original' } as const) : ({ kind: 'proxy', level } as const)
    this.builds.build(t, build, p.kind === 'proxy' ? epsilonAt(level) : 0).then(
      (built) => {
        if (this.disposed || this.precisions[p.kind] !== p) return
        p.building.delete(segment)
        p.arrived.set(segment, { geometry: t.key, segment: built.segment })
        this.arrivals[built.cached ? 'cached' : 'built']++
        // Committed between frames; the viewport draws again when what it draws changed.
        if (this.incremental) this.pump()
      },
      (e: unknown) => {
        if (this.disposed || this.precisions[p.kind] !== p) return
        p.building.delete(segment)
        if (!(e instanceof GeometryFailed)) return this.fail()
        // The rest go on: the next sync leaves this geometry out (the viewport's asked for).
        console.warn(`屏幕外的光照与反射：几何 ${t.key} 构建失败，不参与光照与反射：${e.message}`)
        this.broken.add(t.key)
        this.redraw?.()
      },
    )
  }

  /**
   * One frame's work: what the scene is now against what was traced, the
   * builds it needs asked for, those come back committed (within a frame's
   * budget), the top levels and tables brought up to date, the status moved
   * on. Called after applyExposure, before the frame is drawn.
   */
  sync() {
    if (this.disposed) return
    this.reap()
    if (this.status.state === 'off') {
      if (this.incremental && sceneTest) gpuTestHooks.traceStats = this.stats(0, 0)
      return
    }
    const started = performance.now()
    const collected = collectMeshes(this.scene, slotOf)
    // A broken geometry is tried again once it has left the scene.
    const keys = new Set(collected.map((t) => t.key))
    for (const k of this.broken) if (!keys.has(k)) this.broken.delete(k)
    const traced = this.broken.size > 0 ? collected.filter((t) => !this.broken.has(t.key)) : collected
    const changed = traced.length !== this.traced.length || traced.some((t, i) => this.traced[i].mesh !== t.mesh || this.traced[i].key !== t.key)
    this.traced = traced
    if (this.status.state === 'failed') {
      if (this.retried || !changed) {
        if (this.incremental && sceneTest) gpuTestHooks.traceStats = this.stats(0, performance.now() - started)
        return
      }
      this.retried = true
      this.set({ state: 'preparing', done: 0, total: 0 })
    }
    if (changed) {
      this.changedAt = performance.now()
      this.tooLarge = false
      for (const p of Object.values(this.precisions)) p.blocked = false
    }
    if (this.restyle(traced)) this.lightingVersion++
    const geometries = new Map(traced.map((t) => [t.key, t.triangles]))
    this.geometries = geometries
    const total = [...geometries.values()].reduce((n, x) => n + x, 0)
    // The proxies always (the probes trace them), the original when it may fit.
    const proxy = this.precision('proxy')
    const original = this.wantsOriginal(total) ? this.precision('original') : null
    this.updateLevels(traced)
    for (const p of [original, proxy]) if (p) for (const t of traced) this.ask(p, t)
    let commitMs = 0
    let committed = false
    for (const p of Object.values(this.precisions)) {
      const waiting = p.arrived.size
      commitMs += this.commit(p, geometries)
      if (this.precisions[p.kind] !== p) continue
      if (this.served(p) && p.arrived.size < waiting) {
        // An edit's segments in a layer the viewport draws: it draws again, and the probes gather again where they trace it.
        this.redraw?.()
        committed ||= p === this.probing
      }
      this.instances(p, traced, changed)
    }
    this.track(traced, committed)
    if (this.tables.setLights(collectLights(this.scene), this.scene)) this.lightingVersion++
    this.judge(geometries, total)
    this.report(geometries)
    if (this.incremental && sceneTest) gpuTestHooks.traceStats = this.stats(commitMs, performance.now() - started)
    if (this.incremental && this.committable()) this.pump()
  }

  /** The layers dropped before this sync: no frame still to be drawn binds them. */
  private reap() {
    for (const layer of this.retired.splice(0)) layer.dispose()
  }

  /** Segments back that the next sync would commit. */
  private committable(): boolean {
    return Object.values(this.precisions).some((p) => p.arrived.size > 0 && !p.blocked && !(this.filling(p) && p.building.size > 0))
  }

  /**
   * The viewport's syncs between its frames while segments wait to be
   * committed, the page yielding between them as ready() does: a slow
   * frame (a large scene's) does not hold the layer back a frame per
   * budget's worth of segments. A precision that fills is waited for till
   * the meshes have stood still.
   */
  private pump() {
    if (this.pumping) return
    this.pumping = true
    void (async () => {
      let shown = false
      while (!this.disposed && this.committable()) {
        const hold = Math.min(...Object.values(this.precisions).filter((p) => p.arrived.size > 0 && !p.blocked).map((p) => this.holdMs(p)))
        await backgroundTicker().wait(Math.max(1, hold))
        if (this.disposed) break
        // Only the commits between frames; the rest of a sync (the top level, the tier) once they are in.
        const started = performance.now()
        let commitMs = 0
        for (const p of Object.values(this.precisions)) {
          const waiting = p.arrived.size
          commitMs += this.commit(p, this.geometries)
          shown ||= this.served(p) && p.arrived.size < waiting
        }
        if (sceneTest) gpuTestHooks.traceStats = this.stats(commitMs, performance.now() - started)
      }
      this.pumping = false
      if (this.disposed) return
      this.sync()
      // An edit's segments went into the layer the viewport draws.
      if (shown) this.redraw?.()
    })()
  }

  /** The material rows, the traced meshes' slots in turn: each written when it holds something else than it did. Whether any was. */
  private restyle(traced: readonly Traced[]): boolean {
    let row = 0
    let written = false
    this.firstRow = traced.map((t) => {
      const first = row
      for (const s of t.slots) {
        const was = this.rows[row]
        if (!was || was.material !== s.material || was.doubleSided !== s.doubleSided) {
          this.rows[row] = s
          this.tables.setMaterial(row, s.material, s.doubleSided)
          written = true
        }
        row++
      }
      return first
    })
    this.rows.length = row
    return written
  }

  /**
   * The traced meshes against the last sync's: one in or out, moved, or
   * drawing another geometry counts as a change of what the probes trace,
   * where it was and where it is now as edited; so does a segment committed
   * into the layer they trace. The bounds follow.
   */
  private track(traced: readonly Traced[], committed: boolean) {
    const next = new Map<Mesh, Placed>()
    let changed = committed
    for (const t of traced) {
      const was = this.placedAt.get(t.mesh)
      const m = t.mesh.matrixWorld.elements
      if (was && was.geometry === t.geometry && was.matrix.every((v, i) => v === m[i])) {
        next.set(t.mesh, was)
        continue
      }
      if (!t.geometry.boundingBox) t.geometry.computeBoundingBox()
      const box = t.geometry.boundingBox!.clone().applyMatrix4(t.mesh.matrixWorld)
      next.set(t.mesh, { geometry: t.geometry, matrix: [...m], box, ground: isGround(t) })
      if (was) this.edited.union(was.box)
      this.edited.union(box)
      changed = true
    }
    for (const [mesh, was] of this.placedAt) {
      if (next.has(mesh)) continue
      this.edited.union(was.box)
      changed = true
    }
    this.placedAt = next
    if (!changed) return
    this.geometryVersion++
    this.bounds.makeEmpty()
    for (const p of next.values()) if (!p.ground) this.bounds.union(p.box)
  }

  /** Whether to build the original: unless it plainly cannot fit, or did not and the scene has not shrunk well below that (D9). */
  private wantsOriginal(total: number): boolean {
    if (this.precisions.original) return true
    if (!this.chosen) {
      this.chosen = true
      return buildsOriginal(total, this.budget)
    }
    return total < this.retryBelow && total * this.perTriangle * SPARE <= this.budget
  }

  /** Each geometry's proxy level, by the largest scale an instance of it has; a finer level asks for a new build. */
  private updateLevels(traced: readonly Traced[]) {
    const need = new Map<string, number>()
    for (const t of traced) need.set(t.key, Math.max(need.get(t.key) ?? -Infinity, levelFor(t.mesh.matrixWorld.getMaxScaleOnAxis())))
    for (const [key, level] of need) if ((this.levels.get(key) ?? -Infinity) < level) this.levels.set(key, level)
  }

  /** Whether p fills before it serves: an export's precisions, and the viewport's till they serve. */
  private filling(p: Precision): boolean {
    return !this.incremental || !this.served(p)
  }

  /** The top level's leaves over the traced meshes in p: a root each (a segment not back yet counted as one). */
  private leaves(p: Precision): number {
    let n = 0
    for (const t of this.traced) {
      const segment = this.segmentOf(p, t.key)
      n += p.arrived.get(segment)?.segment.roots.length ?? p.layer.placed.get(segment)?.rootBases.length ?? 1
    }
    return n
  }

  /** How long a precision that fills waits yet for the viewport's meshes to stand still (an export's never). */
  private holdMs(p: Precision): number {
    return this.incremental && this.filling(p) ? Math.max(0, settleMs - (performance.now() - this.changedAt)) : 0
  }

  /**
   * Segments come back committed. A precision that fills takes them once
   * all are back, room made for all of them in one layout (an export's in
   * key order, L3); the serving one an edit's as they come. Either within
   * a frame's budget. Segments no traced geometry uses give their room back
   * first. Returns the milliseconds spent.
   */
  private commit(p: Precision, geometries: ReadonlyMap<string, number>): number {
    for (const [geometry, segment] of p.current) {
      if (geometries.has(geometry)) continue
      p.layer.release(segment)
      p.current.delete(geometry)
    }
    for (const [segment, a] of p.arrived) if (!geometries.has(a.geometry)) p.arrived.delete(segment)
    if (p.blocked || p.arrived.size === 0) return this.tidy(p)
    if (p.layer.dropped) {
      this.repack(p)
      return 0
    }
    if (this.filling(p) && (p.building.size > 0 || this.holdMs(p) > 0)) return 0
    const started = performance.now()
    // The top level's room made with the rest: one layout for a fill, not a second one when the top level is laid.
    const leaves = this.leaves(p)
    if (this.filling(p)) {
      try {
        p.layer.reserveFor([...p.arrived.values()].map((a) => a.segment), leaves)
      } catch (e) {
        if (!(e instanceof CapacityError)) throw e
        p.blocked = true
        return performance.now() - started
      }
    }
    const queue = this.incremental ? [...p.arrived.keys()] : [...p.arrived.keys()].sort()
    for (const segment of queue) {
      const a = p.arrived.get(segment)!
      try {
        p.layer.commit(segment, a.segment, leaves)
      } catch (e) {
        if (!(e instanceof CapacityError)) throw e
        p.blocked = true
        break
      }
      p.arrived.delete(segment)
      const before = p.current.get(a.geometry)
      if (before !== undefined && before !== segment) p.layer.release(before)
      p.current.set(a.geometry, segment)
      if (performance.now() - started > commitBudgetMs) break
    }
    return performance.now() - started
  }

  /**
   * An export's precision whose layer dropped its arrays (A5) and must take
   * a segment or more instances than it has room for (a look's meshes
   * changed between pictures): packed again from the start, the next
   * picture waiting for it as for the first (ready()).
   */
  private repack(p: Precision) {
    const kind = p.kind
    this.drop(p)
    this.precision(kind)
  }

  /** On a frame with nothing to commit, a fragmented layer is compacted (S1 design「收集与比对」). */
  private tidy(p: Precision): number {
    if (!this.incremental || p.building.size > 0 || p.layer.fragmentation <= tidyPast) return 0
    const started = performance.now()
    p.layer.compact()
    return performance.now() - started
  }

  /** The top level over the traced meshes in the layer: laid again when they or their segments changed, else refit where they moved. */
  private instances(p: Precision, traced: readonly Traced[], changed: boolean) {
    // A precision that fills lays its top level once, when everything is in.
    if (this.filling(p) && (p.building.size > 0 || p.arrived.size > 0)) return
    const live: { t: Traced; row: number; segment: string }[] = []
    traced.forEach((t, i) => {
      const segment = p.current.get(t.key)
      if (segment !== undefined) live.push({ t, row: this.firstRow[i], segment })
    })
    const same = !changed && !p.layer.needsTop && live.length === p.order.length && live.every((l, i) => p.order[i].mesh === l.t.mesh && p.order[i].segment === l.segment)
    if (!same) {
      // Each root drawn with its own group's slot (one material: its one slot).
      const instances: Instance[] = live.map((l) => ({ matrixWorld: l.t.mesh.matrixWorld, segment: l.segment, rows: p.layer.placed.get(l.segment)!.groups.map((g) => l.row + Math.min(g, l.t.slots.length - 1)) }))
      try {
        p.layer.setInstances(instances)
      } catch (e) {
        if (e instanceof ArraysDropped) return this.repack(p)
        if (!(e instanceof CapacityError)) throw e
        // The top level does not fit the device: the tier is judged on it (judge).
        p.blocked = true
        return
      }
      p.order = live.map((l) => ({ mesh: l.t.mesh, segment: l.segment, matrix: l.t.mesh.matrixWorld.toArray() }))
      return
    }
    const moved: { at: number; matrixWorld: Matrix4 }[] = []
    p.order.forEach((o, at) => {
      const m = o.mesh.matrixWorld.elements
      if (o.matrix.every((v, i) => v === m[i])) return
      o.matrix = o.mesh.matrixWorld.toArray()
      moved.push({ at, matrixWorld: o.mesh.matrixWorld })
    })
    p.layer.move(moved)
  }

  /**
   * The tier, judged on what is built once all of it is in (S1 design「分档」,
   * S2 design「底座的两份精度」): the proxies first, which the probes need at
   * either tier, then the original with them. Ready once, when everything
   * built is in; what serves goes on serving while an edit or a change of
   * tier is under way.
   */
  private judge(geometries: ReadonlyMap<string, number>, total: number) {
    if (this.status.state === 'failed') return
    const complete = (p: Precision) => p.missing(geometries) === 0 && p.building.size === 0
    const proxy = this.precisions.proxy!
    let proxies: boolean | null = null
    if (proxy.blocked) proxies = false
    else if (complete(proxy)) proxies = tierOf(null, proxy.layer.size, this.budget, this.maxBinding) !== 'tooLarge'
    if (proxies === false) {
      for (const p of Object.values(this.precisions)) this.drop(p)
      this.tooLarge = true
      return
    }
    const original = this.precisions.original
    let fits: boolean | null = null
    if (original?.blocked) fits = false
    else if (original && proxies && complete(original)) {
      const size = original.layer.size
      fits = tierOf(size, proxy.layer.size, this.budget, this.maxBinding) === 'full'
      if (total > 0) this.perTriangle = size.bytes / total
    }
    if (original && fits === false) {
      // Tier 2: the reflections go to the proxies with the probes; the original is tried again well below this.
      this.retryBelow = total * retryShare
      this.drop(original)
    }
    if (!proxies) return
    this.probing = proxy
    if (original && fits) {
      this.reflecting = original
      this.retryBelow = Infinity
      return
    }
    // The original still building: the first time, ready waits for it; later the proxies or the original go on serving.
    if (original && fits === null && !this.reflecting) {
      if (this.status.state !== 'ready') this.probing = null
      else this.reflecting = proxy
      return
    }
    if (!this.precisions.original) this.reflecting = proxy
  }

  private report(geometries: ReadonlyMap<string, number>) {
    if (this.status.state === 'failed') return
    if (this.tooLarge) return this.set({ state: 'tooLarge' })
    const r = this.reflecting
    const p = this.probing
    if (r && p) {
      // Ready again: a later failure is retried once more (a shader that did not build never is).
      if (!this.forGood) this.retried = false
      return this.set({ state: 'ready', tier: r.kind === 'original' ? 'full' : 'proxy', pending: Math.max(r.missing(geometries), p.missing(geometries)) })
    }
    const missing = Math.max(0, ...Object.values(this.precisions).map((x) => x.missing(geometries)))
    this.set({ state: 'preparing', done: geometries.size - missing, total: geometries.size })
  }

  /**
   * Settles once the base has a tier (or none to have): an export draws
   * after it (Epic「收敛、帧推进与导出」). Drives the syncs itself, the
   * page yielding between them through the background ticker (a hidden
   * tab's own timers are slowed past a second). false when timeoutMs runs
   * out first.
   */
  async ready(timeoutMs: number): Promise<boolean> {
    const deadline = performance.now() + timeoutMs
    for (;;) {
      this.sync()
      if (this.status.state !== 'preparing') return true
      if (performance.now() >= deadline) return false
      await backgroundTicker().wait(4)
    }
  }

  /** A frame has drawn with the layer: an export's lets the CPU's copies go (design A5); the viewport's keeps them for its edits. */
  drawn() {
    if (this.incremental) return
    for (const p of new Set([this.reflecting, this.probing])) {
      if (!p || p.layer.dropped) continue
      p.layer.dropArrays()
      gpuTestHooks.traceDropped++
    }
  }

  /** For the e2e (gpuTestHooks.traceDigests): the serving layers' buffers and the tables, each a SHA-256. */
  private async digest(into: Record<string, string>[]) {
    const r = this.reflecting
    const p = this.probing
    if (!r || !p) return
    const hex = async (a: ArrayBufferView) => [...new Uint8Array(await crypto.subtle.digest('SHA-256', a as ArrayBufferView<ArrayBuffer>))].map((b) => b.toString(16).padStart(2, '0')).join('').slice(0, 16)
    const arrays: Record<string, ArrayBufferView> = { tables: this.tables.data }
    for (const [name, a] of Object.entries(r.layer.arrays)) arrays[name] = a
    if (p !== r) for (const [name, a] of Object.entries(p.layer.arrays)) arrays[`probes.${name}`] = a
    const out: Record<string, string> = { layer: r.kind }
    for (const [name, a] of Object.entries(arrays)) out[name] = await hex(a)
    into.push(out)
  }

  /** For the e2e: syncMs is this sync's main-thread time, commitMs its share committing segments. */
  private stats(commitMs: number, syncMs: number) {
    const s = this.reflecting
    const status = this.status
    return {
      state: status.state,
      reason: status.state === 'off' ? status.reason : null,
      tier: status.state === 'ready' ? status.tier : null,
      pending: status.state === 'ready' ? status.pending : null,
      layer: s?.kind ?? null,
      /** What the reflections are built toward: the original while it is built, else the proxies. */
      target: this.precisions.original ? 'original' : this.precisions.proxy ? 'proxy' : null,
      probes: this.probing?.kind ?? null,
      geometryVersion: this.geometryVersion,
      lightingVersion: this.lightingVersion,
      bytes: Object.values(this.precisions).reduce((n, p) => n + p.layer.bytes, 0),
      uploaded: Object.values(this.precisions).reduce((n, p) => n + p.layer.take(), 0),
      commitMs,
      syncMs,
      meshes: this.traced.length,
      ...this.arrivals,
      depth: s?.layer.depth.traversal ?? null,
      original: this.precisions.original?.layer.size ?? null,
      proxy: this.precisions.proxy?.layer.size ?? null,
    }
  }

  dispose() {
    this.disposed = true
    this.listeners.clear()
    for (const u of this.unsubscribe) u()
    this.reap()
    for (const p of Object.values(this.precisions)) p.layer.dispose()
    this.tables.dispose()
  }
}

/** The device's base (D1): made with the first renderer that asks, gone with the device. A device draws one scene, the viewport's or an export's. */
export function traceBase(gpu: SceneGPU, scene: Scene, options: Options): TraceBase {
  const base = gpu.cache('trace', () => new TraceBase(gpu, scene, options))
  if (base.scene !== scene || base.incremental !== options.incremental) throw new Error('这个设备的光追底座属于另一个场景')
  return base
}
