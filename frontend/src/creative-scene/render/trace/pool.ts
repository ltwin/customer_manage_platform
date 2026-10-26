import type { BufferAttribute, BufferGeometry, InterleavedBufferAttribute } from 'three'
import { gpuTestHooks } from '../gpu.ts'
import type { BuildJob, BuildReply, GeometryData, Segment } from './build.worker.ts'
import { cacheKey, type BuildKind } from './cache.ts'

// The page's build workers (S1 design「持有关系」「构建、缓存与提交」): one
// pool for the viewport's layer and every export's, so a geometry asked for
// twice at once is built once, and an export often reads what the viewport
// has just built from the cache. Workers start as builds come, one per core
// but one, and go after a while with nothing to do: each holds three, the
// BVH library and meshoptimizer.

export type Built = { segment: Segment; cached: boolean; ms: number }

/** What a build is of (collect.ts Traced): the geometry, its key, and whether it is drawn a material a group. */
export type Source = { geometry: BufferGeometry; key: string; cacheable: boolean; grouped: boolean; triangles: number }

/**
 * A geometry's own build failed: its error, or it ran past its time (the
 * worker stuck on it is let go). That geometry is not traced (base.ts); a
 * worker that did not load fails its builds with a plain Error.
 */
export class GeometryFailed extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'GeometryFailed'
  }
}

/** Workers with nothing to do go after this long. */
const idleMs = 30_000
/** A build's time, from when a worker takes it: far past what one takes (S1 task 8.2: the hall's 20 million triangles all come back in some 6 s, cold). */
const timeoutMs = (triangles: number) => 120_000 + triangles * 0.06

/** An attribute's values as a plain array of size per item (interleaved, normalized and integer ones read through getComponent). */
function plain(a: BufferAttribute | InterleavedBufferAttribute, size: number): Float32Array {
  const count = a.count
  const array = (a as BufferAttribute).array
  if (!(a as InterleavedBufferAttribute).isInterleavedBufferAttribute && !a.normalized && array instanceof Float32Array && a.itemSize === size) return array.slice(0, count * size)
  const out = new Float32Array(count * size)
  for (let i = 0; i < count; i++) for (let c = 0; c < size; c++) out[i * size + c] = a.getComponent(i, c)
  return out
}

/** Copies of what a build needs from a geometry: the raster keeps drawing the originals. Its groups only when its mesh draws a material a group. */
export function geometryData(g: BufferGeometry, grouped: boolean): GeometryData {
  const color = g.attributes.color
  return {
    positions: plain(g.attributes.position, 3),
    normals: g.attributes.normal ? plain(g.attributes.normal, 3) : null,
    colors: color ? plain(color, color.itemSize) : null,
    colorSize: color?.itemSize ?? 0,
    index: g.index ? new Uint32Array(g.index.array) : null,
    groups: grouped ? g.groups.map(({ start, count }) => ({ start, count })) : [],
  }
}

/** A job waiting for a worker: its geometry copied only when one takes it, so a large scene's arrival copies a few at a time. */
type Queued = { job: Omit<BuildJob, 'geometry'>; source: Source; resolve(b: Built): void; reject(e: unknown): void }

/** What the base asks builds of: the page's pool, or the unit tests' own. */
export type Builds = { build(source: Source, build: BuildKind, epsilon?: number): Promise<Built> }

class BuildPool implements Builds {
  private readonly size = Math.max(1, (navigator.hardwareConcurrency || 2) - 1)
  private readonly idle: Worker[] = []
  private readonly busy = new Map<Worker, Queued>()
  private readonly timers = new Map<Worker, ReturnType<typeof setTimeout>>()
  private readonly queue: Queued[] = []
  private readonly asked = new Map<string, Promise<Built>>()
  private next = 1
  private timer: ReturnType<typeof setTimeout> | null = null

  /**
   * The segment of a geometry built as asked: the cache's, or built now.
   * Asked again while under way, the same answer; the segment is shared, to
   * be read and never written.
   */
  build(source: Source, build: BuildKind, epsilon = 0): Promise<Built> {
    const key = cacheKey(build, source.key)
    const asked = this.asked.get(key)
    if (asked) return asked
    const job = { id: this.next++, key, cacheable: source.cacheable, triangles: source.triangles, build, epsilon, delayMs: gpuTestHooks.traceDelayMs }
    const promise = new Promise<Built>((resolve, reject) => {
      this.queue.push({ job, source, resolve, reject })
      this.dispatch()
    }).finally(() => this.asked.delete(key))
    this.asked.set(key, promise)
    return promise
  }

  private dispatch() {
    if (this.timer !== null) clearTimeout(this.timer)
    this.timer = null
    while (this.queue.length > 0 && (this.idle.length > 0 || this.busy.size < this.size)) {
      const worker = this.idle.pop() ?? this.spawn()
      const queued = this.queue.shift()!
      this.busy.set(worker, queued)
      this.timers.set(worker, setTimeout(() => this.expire(worker), timeoutMs(queued.source.triangles) + queued.job.delayMs))
      // A cacheable job asks the cache first; its geometry goes on a miss (send below).
      if (queued.job.cacheable) worker.postMessage({ ...queued.job, geometry: null } satisfies BuildJob)
      else this.send(worker, queued)
    }
    if (this.queue.length === 0 && this.busy.size === 0) this.timer = setTimeout(() => this.retire(), idleMs)
  }

  /** The job with its geometry copied for it, to the worker that has it. */
  private send(worker: Worker, queued: Queued) {
    const g = geometryData(queued.source.geometry, queued.source.grouped)
    // The copies are the job's own: moved, not copied again.
    const transfer = [g.positions.buffer, g.normals?.buffer, g.colors?.buffer, g.index?.buffer].filter((b) => b !== undefined) as Transferable[]
    worker.postMessage({ ...queued.job, geometry: g } satisfies BuildJob, transfer)
  }

  private spawn(): Worker {
    const worker = new Worker(new URL('./build.worker.ts', import.meta.url), { type: 'module' })
    worker.onmessage = ({ data }: MessageEvent<BuildReply>) => {
      const queued = this.busy.get(worker)
      // Not in the cache: the same worker builds it.
      if (queued && 'need' in data) return this.send(worker, queued)
      this.free(worker)
      this.idle.push(worker)
      if (queued) {
        if ('segment' in data) queued.resolve({ segment: data.segment, cached: data.cached, ms: data.ms })
        else queued.reject(new GeometryFailed('error' in data ? data.error : '构建线程没有收到几何'))
      }
      this.dispatch()
    }
    worker.onerror = (e) => {
      // A worker that failed outside a job's own errors (it did not load) goes; the job fails with it.
      const queued = this.free(worker)
      worker.terminate()
      queued?.reject(new Error(e.message || '构建线程出错'))
      this.dispatch()
    }
    return worker
  }

  /** The worker's job, no longer its. */
  private free(worker: Worker): Queued | undefined {
    const queued = this.busy.get(worker)
    this.busy.delete(worker)
    clearTimeout(this.timers.get(worker))
    this.timers.delete(worker)
    return queued
  }

  /** A build past its time: the worker on it goes, and the next build gets another. */
  private expire(worker: Worker) {
    const queued = this.free(worker)
    worker.terminate()
    queued?.reject(new GeometryFailed('构建超时'))
    this.dispatch()
  }

  private retire() {
    this.timer = null
    for (const w of this.idle.splice(0)) w.terminate()
  }
}

/** The page's one pool. */
export const buildPool = new BuildPool()
