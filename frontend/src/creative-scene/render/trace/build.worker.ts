import { BufferAttribute, BufferGeometry } from 'three'
import { MeshBVH, SAH } from 'three-mesh-bvh'
import { getMaxNodeDepth } from 'three-mesh-bvh/src/webgpu/utils/packBVHBufferUtils.js'
import { MeshoptSimplifier } from 'meshoptimizer'
import { openCache, type BuildKind } from './cache.ts'
import { encodeNormals } from './normals.ts'

// One geometry's one-off work, off the main thread (scene-world-space-gi
// 「一次性开销与缓存」, S1 design「构建、缓存与提交」): the cache first; on a
// miss the original's BVH, or a proxy simplified within its error and its
// BVH, packed as a segment the layer copies in as it is, its offsets counted
// from 0 (D2). The segment goes back transferred; the cache keeps a copy.
// A plain module worker: ParallelMeshBVHWorker needs SharedArrayBuffer,
// which the app's pages do not have, so the pool works across geometries.

/** A geometry as the main thread hands it over: plain arrays, copies of its attributes. */
export type GeometryData = {
  /** xyz a vertex. */
  positions: Float32Array
  /** xyz a vertex, or none. */
  normals: Float32Array | null
  /** Linear RGB(A) a vertex, colorSize each, or none. */
  colors: Float32Array | null
  colorSize: number
  /** Three a triangle; none draws the vertices in order. */
  index: Uint32Array | null
  /**
   * The geometry's groups in index elements, when its mesh draws a material
   * a group (a root each, its own slot); none for one material, which draws
   * the whole geometry (one root, however many groups it has).
   */
  groups: { start: number; count: number }[]
}

export type BuildJob = {
  id: number
  /** The cache key (cache.ts cacheKey). */
  key: string
  /** Only a geometry with a traceKey is read from or written to the cache. */
  cacheable: boolean
  build: BuildKind
  /** None the first time a cacheable job is sent: the cache is asked first, the geometry sent on a miss. */
  geometry: GeometryData | null
  /** The geometry's triangles: a cached segment built from another count is not taken (a geometry changed under its key). */
  triangles: number
  /** A proxy's error bound, in the geometry's own units. */
  epsilon: number
  /** gpuTestHooks.traceDelayMs: the answer held this long. */
  delayMs: number
}

/**
 * A geometry packed as the layer takes it, with every offset counted from
 * its own start: the layer adds where it lands (layer.ts commit).
 */
export type Segment = {
  /** Each root's tree, one after another, 8 words a node; a leaf's triangle offset counts from the segment's first triangle. */
  nodes: Uint32Array
  /** Nodes in each root, in order: a root per group (material slot). */
  roots: number[]
  /** Each root's group, its place in GeometryData.groups (0 for a geometry handed over without groups): the slot it is drawn with. */
  rootGroups: number[]
  /** Three vertex numbers a triangle, in the tree's order, counted from the segment's first vertex. */
  index: Uint32Array
  /** xyz the position, w the encoded normal (normals.ts). */
  vertices: Float32Array
  /** RGBA8 a vertex, sRGB-encoded: only when the geometry has colours (D5). */
  colors: Uint32Array | null
  /** The deepest root's depth. */
  depth: number
  triangles: number
  /** The triangles of the geometry it was built from (BuildJob.triangles). */
  source: number
}

export type BuildReply = { id: number; segment: Segment; cached: boolean; ms: number } | { id: number; need: true } | { id: number; error: string }

/** The library's defaults, written into the cache key (cache.ts BVH_PARAMS). */
const bvhOptions = { strategy: SAH, targetLeafSize: 10, maxDepth: 40 } as const

const NODE_WORDS = 8

const srgb = (c: number) => {
  const v = c <= 0.0031308 ? c * 12.92 : 1.055 * Math.pow(c, 1 / 2.4) - 0.055
  return Math.round(Math.min(1, Math.max(0, v)) * 255)
}

function packColors(colors: Float32Array, size: number, vertices: number): Uint32Array {
  const out = new Uint32Array(vertices)
  for (let i = 0; i < vertices; i++) out[i] = srgb(colors[size * i]) | (srgb(colors[size * i + 1]) << 8) | (srgb(colors[size * i + 2]) << 16) | (255 << 24)
  return out
}

/** The vertices in order, three a triangle, for a geometry without an index. */
const ordered = (vertices: number) => Uint32Array.from({ length: vertices - (vertices % 3) }, (_, i) => i)

type Groups = { start: number; count: number }[]

/**
 * The groups' triangles one group after another, in the groups' order, and
 * the group each kept range is: three-mesh-bvh makes a root of each range
 * of groups by where it lies (sorted, split where groups overlap, an empty
 * one dropped), so laid out so its k-th root is the k-th group kept.
 * Overlapping groups each keep their triangles, as the raster draws both.
 */
function byGroup(index: Uint32Array, groups: Groups, kept: number[]): { index: Uint32Array; groups: Groups; kept: number[] } {
  const parts: Uint32Array[] = []
  const keep: number[] = []
  groups.forEach(({ start, count }, k) => {
    // A group's count may be Infinity, to the end.
    let n = Math.max(0, Math.min(index.length, start + count) - start)
    n -= n % 3
    if (n === 0) return
    parts.push(index.subarray(start, start + n))
    keep.push(kept[k])
  })
  const out = new Uint32Array(parts.reduce((n, p) => n + p.length, 0))
  const ranges: Groups = []
  let at = 0
  for (const p of parts) {
    out.set(p, at)
    ranges.push({ start: at, count: p.length })
    at += p.length
  }
  return { index: out, groups: ranges, kept: keep }
}

/**
 * Each group simplified on its own (meshoptimizer, the error absolute, open
 * borders locked, so neighbouring slots do not crack apart; I8: what will not
 * simplify within the bound stays as it is), then only the vertices kept.
 */
function simplify(g: GeometryData, index: Uint32Array, groups: Groups, epsilon: number): { g: GeometryData; index: Uint32Array; groups: Groups } {
  const parts = groups.map(({ start, count }) => MeshoptSimplifier.simplify(index.subarray(start, start + count), g.positions, 3, 0, epsilon, ['ErrorAbsolute', 'LockBorder'])[0])
  const kept = new Uint32Array(parts.reduce((n, p) => n + p.length, 0))
  const out: { start: number; count: number }[] = []
  let at = 0
  for (const p of parts) {
    kept.set(p, at)
    out.push({ start: at, count: p.length })
    at += p.length
  }
  // compactMesh renumbers kept in place; remap[old] is the new number, or ~0 for a vertex dropped.
  const [remap, unique] = MeshoptSimplifier.compactMesh(kept)
  const positions = new Float32Array(unique * 3)
  const normals = g.normals ? new Float32Array(unique * 3) : null
  const colors = g.colors ? new Float32Array(unique * g.colorSize) : null
  for (let v = 0; v < remap.length; v++) {
    const to = remap[v]
    if (to === 0xffffffff) continue
    positions.set(g.positions.subarray(3 * v, 3 * v + 3), 3 * to)
    normals?.set(g.normals!.subarray(3 * v, 3 * v + 3), 3 * to)
    colors?.set(g.colors!.subarray(g.colorSize * v, g.colorSize * (v + 1)), g.colorSize * to)
  }
  return { g: { ...g, positions, normals, colors }, index: kept, groups: out }
}

/** The segment of a geometry: its BVH built (the index reordered as it goes) and packed. */
export function buildSegment(job: Pick<BuildJob, 'build' | 'epsilon'> & { geometry: GeometryData }): Segment {
  let g = job.geometry
  const vertexCount = g.positions.length / 3
  const source = g.index ? g.index.slice() : ordered(vertexCount)
  const triangles = Math.floor(source.length / 3)
  const asked = g.groups.length > 0 ? g.groups : [{ start: 0, count: source.length }]
  let { index, groups, kept } = byGroup(source, asked, asked.map((_, k) => k))
  if (job.build.kind === 'proxy') {
    ;({ g, index, groups } = simplify(g, index, groups, job.epsilon))
    // A group simplified to nothing has no root.
    ;({ index, groups, kept } = byGroup(index, groups, kept))
  }
  const vertices = g.positions.length / 3
  const geometry = new BufferGeometry()
  geometry.setAttribute('position', new BufferAttribute(g.positions, 3))
  geometry.setIndex(new BufferAttribute(index, 1))
  // Only for a geometry of several slots: a root each, so each has a transform slot and a material row of its own.
  if (groups.length > 1) for (const { start, count } of groups) geometry.addGroup(start, count)
  const bvh = new MeshBVH(geometry, bvhOptions) as unknown as { _roots: ArrayBuffer[] }
  const roots = bvh._roots.map((r) => r.byteLength / 4 / NODE_WORDS)
  // A slot drawn with another's material would pass unseen: none at all.
  if (roots.length !== kept.length) throw new Error(`几何有 ${kept.length} 个分组，却建出 ${roots.length} 个根`)
  const nodes = new Uint32Array(roots.reduce((n, r) => n + r, 0) * NODE_WORDS)
  let at = 0
  for (const r of bvh._roots) {
    nodes.set(new Uint32Array(r), at)
    at += r.byteLength / 4
  }
  const packed = new Float32Array(vertices * 4)
  const normals = encodeNormals(g.normals, vertices)
  for (let v = 0; v < vertices; v++) {
    packed[4 * v] = g.positions[3 * v]
    packed[4 * v + 1] = g.positions[3 * v + 1]
    packed[4 * v + 2] = g.positions[3 * v + 2]
    packed[4 * v + 3] = normals[v]
  }
  return {
    nodes,
    roots,
    rootGroups: kept,
    // MeshBVH reordered it in place, triangles in the tree's order.
    index,
    vertices: packed,
    colors: g.colors ? packColors(g.colors, g.colorSize, vertices) : null,
    depth: Math.max(...bvh._roots.map((r) => getMaxNodeDepth(r))),
    triangles: index.length / 3,
    source: triangles,
  }
}

const transfers = (s: Segment): Transferable[] => [s.nodes.buffer, s.index.buffer, s.vertices.buffer, ...(s.colors ? [s.colors.buffer] : [])] as Transferable[]

const wait = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms))

async function run(job: BuildJob): Promise<BuildReply> {
  const started = performance.now()
  const cache = job.cacheable ? await openCache() : null
  try {
    const hit = await cache?.read(job.key)
    if (hit && hit.source === job.triangles) {
      if (job.delayMs > 0) await wait(job.delayMs)
      return { id: job.id, segment: hit, cached: true, ms: performance.now() - started }
    }
    if (!job.geometry) return { id: job.id, need: true }
    const room = cache?.room()
    if (job.build.kind === 'proxy') await MeshoptSimplifier.ready
    const segment = buildSegment({ build: job.build, geometry: job.geometry, epsilon: job.epsilon })
    // Copied into the cache now, before the arrays are transferred.
    const r = await room
    if (cache && r) void cache.write(r, job.key, segment)
    if (job.delayMs > 0) await wait(job.delayMs)
    return { id: job.id, segment, cached: false, ms: performance.now() - started }
  } finally {
    // IndexedDB closes once the write in flight is done.
    cache?.close()
  }
}

// Only as a worker: the unit tests import buildSegment from here.
const scope = globalThis as unknown as { WorkerGlobalScope?: abstract new () => unknown }
if (scope.WorkerGlobalScope && globalThis instanceof scope.WorkerGlobalScope) {
  self.onmessage = ({ data: job }: MessageEvent<BuildJob>) => {
    run(job).then(
      (reply) => (self as unknown as Worker).postMessage(reply, 'segment' in reply ? transfers(reply.segment) : []),
      (error: unknown) => (self as unknown as Worker).postMessage({ id: job.id, error: String((error as Error)?.message ?? error) } satisfies BuildReply),
    )
  }
}
