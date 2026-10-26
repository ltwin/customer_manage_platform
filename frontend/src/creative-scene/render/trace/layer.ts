import { Group, Matrix4, Object3D } from 'three'
import { storage } from 'three/tsl'
import { StorageBufferAttribute, StructTypeNode } from 'three/webgpu'
import { SAH } from 'three-mesh-bvh'
import * as bvhGPU from 'three-mesh-bvh/webgpu'
import { ClusteredBVH } from 'three-mesh-bvh/src/webgpu/ClusteredBVH.js'
import { appendBVHData, getMaxNodeDepth } from 'three-mesh-bvh/src/webgpu/utils/packBVHBufferUtils.js'
import type { Segment } from './build.worker.ts'
import { SPARE, type LayerSize } from './capacity.ts'
import type { TSLNode } from '../../materials/patterns.ts'

// One precision of the scene packed for the GPU (S1 design「层与同步」:
// layer.ts; S0 throughput.md「增量打包」): three-mesh-bvh's buffers, laid
// out so that an edit costs what it changes. BVHComputeData.update() would
// pack everything again and upload every buffer whole; this subclass keeps
// its storage and functions and replaces the packing:
//   - the buffers are allocated with room to spare: the node buffer has the
//     top level at its head and the geometries' trees after it, and a
//     segment is copied into a free range of each buffer, its offsets
//     rebased (a leaf's first triangle, an index's vertex; a tree's inner
//     offsets are relative and stay), and uploaded alone;
//   - the top level has one leaf per root of each instance's tree (every
//     instance whole), not the library's clusters of 64 triangles;
//   - moving an instance refits the top level and uploads its nodes and the
//     moved transform slots.
// Running out of room lays the buffers out again, larger: the storage nodes
// keep their identity and take the new arrays, so a built shader binds them
// without being built again (three swaps the bind group), and the old
// buffers' GPU copies go at once (S1 design A13: no frame still to be drawn
// binds them).

const NODE_WORDS = 8
/** The transform slot's first spare word (TransformStruct: matrixWorld 16, inverse 16, visible 1, then _alignment0): the instance's material row. */
export const MATERIAL_WORD = 33
/** The library leaves storage arrays of one element out of WGSL arrays (three takes them for a single struct). */
const MIN_ELEMENTS = 2

/** The layer does not fit the device: which buffer, how large, and the limit. */
export class CapacityError extends Error {
  readonly buffer: string
  readonly bytes: number
  readonly limit: number
  constructor(buffer: string, bytes: number, limit: number) {
    super(`${buffer} needs ${bytes} bytes, the device binds at most ${limit}`)
    this.name = 'CapacityError'
    this.buffer = buffer
    this.bytes = bytes
    this.limit = limit
  }
}

/** The layer let its CPU arrays go (dropArrays) and was asked to take a segment or lay its buffers out again: the base packs it anew. */
export class ArraysDropped extends Error {
  constructor() {
    super('the layer dropped its arrays')
    this.name = 'ArraysDropped'
  }
}

type Range = { start: number; count: number }

/** Ranges of one buffer, handed out first fit and given back merged with their neighbours. */
export class Space {
  tail = 0
  free: Range[] = []
  capacity: number
  constructor(capacity: number) {
    this.capacity = capacity
  }

  take(count: number): number | null {
    const i = this.free.findIndex((r) => r.count >= count)
    if (i >= 0) {
      const r = this.free[i]
      const start = r.start
      if (r.count === count) this.free.splice(i, 1)
      else this.free[i] = { start: r.start + count, count: r.count - count }
      return start
    }
    if (this.tail + count > this.capacity) return null
    const start = this.tail
    this.tail += count
    return start
  }

  give(start: number, count: number) {
    if (count === 0) return
    this.free.push({ start, count })
    this.free.sort((a, b) => a.start - b.start)
    const merged: Range[] = []
    for (const r of this.free) {
      const last = merged.at(-1)
      if (last && last.start + last.count === r.start) last.count += r.count
      else merged.push({ ...r })
    }
    // A free range at the tail goes back to it.
    const last = merged.at(-1)
    if (last && last.start + last.count === this.tail) {
      this.tail = last.start
      merged.pop()
    }
    this.free = merged
  }

  /** Free elements below the tail, the share fragmentation leaves. */
  get wasted(): number {
    return this.free.reduce((n, r) => n + r.count, 0)
  }
}

/** A segment as it lies in the layer. */
export type Placed = {
  nodes: Range
  index: Range
  vertices: Range
  /** Each root's first node in the node buffer. */
  rootBases: number[]
  /** Each root's bounds (its first node, 32 bytes), for the top level. */
  bounds: ArrayBuffer[]
  /** Each root's group, the slot it is drawn with (build.worker.ts rootGroups). */
  groups: number[]
  depth: number
  colored: boolean
}

/** An instance in the top level: where it is, which segment it draws, and the material row of each of its roots. */
export type Instance = { matrixWorld: Matrix4; segment: string; rows: readonly number[] }

/** A storage node of the layer's: its value is swapped for a larger buffer when the layer grows. */
export type StorageNode = TSLNode
/** three's storage(), its typings not following a struct's type. */
const store = storage as unknown as (value: StorageBufferAttribute, type: unknown) => StorageNode
type Attribute = StorageBufferAttribute & { addUpdateRange(start: number, count: number): void; clearUpdateRanges(): void; needsUpdate: boolean }
type TransformInfo = { object: Object3D; root: number; slot: number }
type Internals = {
  _getTransformMap(top: ClusteredBVH): Map<string, TransformInfo>
  writeTransformData(info: TransformInfo, premultiply: Matrix4, slot: number, target: ArrayBuffer): void
}

const inverse = new Matrix4()
const { BVHComputeData } = bvhGPU
/** The node struct the traversal reads (exported by the library, not in its typings). */
const bvhNodeStruct = (bvhGPU as unknown as { bvhNodeStruct: StructTypeNode }).bvhNodeStruct

export type Capacity = { tlasNodes: number; nodes: number; indices: number; vertices: number; transforms: number }

export class TraceLayer extends BVHComputeData {
  readonly placed = new Map<string, Placed>()
  /** Bytes queued for upload since the last take(). */
  private queued = 0
  private capacity: Capacity = { tlasNodes: MIN_ELEMENTS, nodes: 0, indices: 0, vertices: 0, transforms: MIN_ELEMENTS }
  private space = { nodes: new Space(0), index: new Space(0), vertices: new Space(0) }
  private top: ClusteredBVH | null = null
  private standIns: (Object3D & { rows: readonly number[] })[] = []
  private nodes: Attribute
  private index: Attribute
  private vertices: Attribute
  private transforms: Attribute
  private colors: Attribute
  /** The buffers' bytes as allocated, kept apart from the arrays: an export's layer drops them (dropArrays). */
  private allocated = 0
  /** The vertices the colour buffer has room for: none till a coloured segment lays the buffers out. */
  private colorsFor = 0
  /** The CPU holds only the top level's nodes and the transforms (dropArrays). */
  dropped = false
  private readonly struct: StructTypeNode
  /** The library's four buffers as storage nodes: the traversal reads them, and a hit its transform slot and vertices (hit.ts). */
  readonly stores: Record<'nodes' | 'transforms' | 'index' | 'attributes', StorageNode>
  /** The vertex colours' buffer (D5): a placeholder until a coloured segment comes. */
  readonly colorsNode: StorageNode
  depth = { tlas: 0, blas: 0, traversal: 0 }
  /** The times the buffers were laid out again (the e2e's statistics, the unit tests). */
  layouts = 0

  /**
   * @param maxBinding the device's limit on one buffer (render/trace/capacity.ts)
   * @param name what the storage is named in WGSL, a layer's own (L2: a layer per precision)
   * @param letGo lets a buffer's GPU copy go (SceneGPU.release)
   */
  readonly maxBinding: number
  readonly name: string
  private readonly letGo: (attribute: object) => void

  constructor(maxBinding: number, name: string, letGo: (attribute: object) => void = () => {}) {
    super([], { attributes: { position: 'vec4f' }, autogenerateBvh: false })
    this.maxBinding = maxBinding
    this.name = name
    this.letGo = letGo
    this.struct = new StructTypeNode(this.attributes, 'bvh_GeometryStruct')
    this.nodes = this.attribute(MIN_ELEMENTS * NODE_WORDS, 1, 'nodes')
    this.index = this.attribute(MIN_ELEMENTS, 1, 'index')
    this.vertices = this.attribute(MIN_ELEMENTS * this.struct.getLength(), this.struct.getLength(), 'vertices')
    this.transforms = this.attribute(MIN_ELEMENTS * this.transformWords, 1, 'transforms')
    this.colors = this.attribute(MIN_ELEMENTS, 1, 'colors')
    // Held here: the library's storage is proxies, which take a node but not a change to one.
    this.stores = {
      nodes: store(this.nodes, bvhNodeStruct).toReadOnly().setName('bvh_nodes'),
      transforms: store(this.transforms, (this.structs as unknown as { transform: StructTypeNode }).transform).toReadOnly().setName('bvh_transforms'),
      index: store(this.index, 'uint').toReadOnly().setName('bvh_index'),
      attributes: store(this.vertices, this.struct).toReadOnly().setName('bvh_attributes'),
    }
    // The names and types upstream's update() gives them: the traversal's proxies resolve to these.
    Object.assign(this.storage, this.stores)
    ;(this.structs as unknown as Record<string, unknown>).attributes = this.struct
    this.colorsNode = store(this.colors, 'uint').toReadOnly().setName(`${name}_colors`)
    this.allocated = this.arrayBytes()
    this.writeEmptyTop()
  }

  private arrayBytes(): number {
    return [this.nodes, this.index, this.vertices, this.transforms, this.colors].reduce((n, a) => n + (a.array as Uint32Array).byteLength, 0)
  }

  private get transformWords(): number {
    return (this.structs as unknown as { transform: StructTypeNode }).transform.getLength()
  }

  /** A buffer named for the device's messages: <layer>_<which>. */
  private attribute(elements: number, itemSize: number, which: string): Attribute {
    const a = new StorageBufferAttribute(new Uint32Array(Math.max(elements, MIN_ELEMENTS * itemSize)), itemSize) as Attribute
    a.name = `${this.name}_${which}`
    return a
  }

  /** The buffers as they are on the CPU (the e2e's statistics, the unit tests' traversal). */
  get arrays() {
    return { nodes: this.nodes.array as Uint32Array, index: this.index.array as Uint32Array, vertices: this.vertices.array as Uint32Array, transforms: this.transforms.array as Uint32Array, colors: this.colors.array as Uint32Array }
  }

  /** The bytes of each buffer at a capacity: what the device must bind. */
  private bytesAt(c: Capacity, colored: boolean) {
    return {
      nodes: (c.tlasNodes + c.nodes) * NODE_WORDS * 4,
      index: c.indices * 4,
      vertices: c.vertices * this.struct.getLength() * 4,
      transforms: c.transforms * this.transformWords * 4,
      colors: colored ? c.vertices * 4 : 0,
    }
  }

  /** The buffers as allocated. */
  get bytes(): number {
    return this.allocated
  }

  /** What the layer's live segments and instances need, without room to spare: the tier is judged on it (capacity.ts tierOf). */
  get size(): LayerSize {
    let nodes = 0
    let indices = 0
    let vertices = 0
    let colored = false
    for (const p of this.placed.values()) {
      nodes += p.nodes.count
      indices += p.index.count
      vertices += p.vertices.count
      colored ||= p.colored
    }
    const leaves = this.standIns.reduce((n, s) => n + s.rows.length, 0)
    const b = this.bytesAt({ tlasNodes: Math.max(1, 2 * leaves - 1), nodes, indices, vertices, transforms: leaves }, colored)
    return { bytes: b.nodes + b.index + b.vertices + b.transforms + b.colors, largest: Math.max(b.nodes, b.index, b.vertices, b.transforms, b.colors), depth: this.depth.traversal }
  }

  /** Bytes queued for upload since the last take, written on the next draw. */
  take(): number {
    const bytes = this.queued
    this.queued = 0
    return bytes
  }

  private queue(a: Attribute, start: number, count: number) {
    if (count === 0) return
    a.addUpdateRange(start, count)
    a.needsUpdate = true
    this.queued += count * 4
  }

  /**
   * Lays the buffers out again at a capacity, keeping what they hold where it
   * lies (the geometries' trees moved along if the top level's region grows).
   * Throws CapacityError when a buffer would pass the device's limit.
   */
  private relayout(next: Capacity, colored: boolean) {
    if (this.dropped) throw new ArraysDropped()
    for (const [buffer, bytes] of Object.entries(this.bytesAt(next, colored))) if (bytes > this.maxBinding) throw new CapacityError(buffer, bytes, this.maxBinding)
    const shift = next.tlasNodes - this.capacity.tlasNodes
    const grow = (old: Attribute, elements: number, itemSize: number, copy: (from: Uint32Array, to: Uint32Array) => void) => {
      const a = this.attribute(elements, itemSize, old.name.slice(this.name.length + 1))
      copy(old.array as Uint32Array, a.array as Uint32Array)
      this.letGo(old)
      // A new buffer uploads whole when it is first bound.
      this.queued += (a.array as Uint32Array).byteLength
      return a
    }
    const nodeWords = (this.capacity.tlasNodes + this.capacity.nodes) * NODE_WORDS
    this.nodes = grow(this.nodes, (next.tlasNodes + next.nodes) * NODE_WORDS, 1, (from, to) => to.set(from.subarray(this.capacity.tlasNodes * NODE_WORDS, nodeWords), next.tlasNodes * NODE_WORDS))
    this.index = grow(this.index, next.indices, 1, (from, to) => to.set(from.subarray(0, Math.min(from.length, to.length))))
    const words = this.struct.getLength()
    this.vertices = grow(this.vertices, next.vertices * words, words, (from, to) => to.set(from.subarray(0, Math.min(from.length, to.length))))
    this.transforms = grow(this.transforms, next.transforms * this.transformWords, 1, () => {})
    if (colored) {
      this.colors = grow(this.colors, next.vertices, 1, (from, to) => to.set(from.subarray(0, Math.min(from.length, to.length))))
      this.colorsFor = next.vertices
    }
    this.allocated = this.arrayBytes()
    this.layouts++
    this.stores.nodes.value = this.nodes
    this.stores.index.value = this.index
    this.stores.attributes.value = this.vertices
    this.stores.transforms.value = this.transforms
    this.colorsNode.value = this.colors
    if (shift !== 0) for (const p of this.placed.values()) p.rootBases = p.rootBases.map((b) => b + shift)
    this.capacity = next
    this.space.nodes.capacity = next.nodes
    this.space.index.capacity = next.indices
    this.space.vertices.capacity = next.vertices
    // The transforms and the top level are written again by the caller (setInstances); till then no ray enters.
    this.top = null
    this.writeEmptyTop()
  }

  /** Room for count more of each, with SPARE to spare over what is in use. */
  private reserve(more: { nodes: number; indices: number; vertices: number }, leaves: number, coloredSegment: boolean) {
    const c = this.capacity
    const colored = coloredSegment || [...this.placed.values()].some((p) => p.colored)
    const s = this.space
    const fits = (sp: Space, n: number) => sp.free.some((r) => r.count >= n) || sp.tail + n <= sp.capacity
    const room = (used: number, n: number, now: number) => Math.max(now, Math.ceil((used + n) * SPARE) + 64)
    const needNodes = !fits(s.nodes, more.nodes)
    const needIndex = !fits(s.index, more.indices)
    const needVertices = !fits(s.vertices, more.vertices)
    const tlas = Math.max(MIN_ELEMENTS, 2 * leaves - 1)
    const needTop = tlas > c.tlasNodes || leaves > c.transforms
    const needColors = colored && this.colorsFor < c.vertices
    if (!needNodes && !needIndex && !needVertices && !needTop && !needColors) return
    // The first coloured segment lays out again too, for the colour buffer at the vertices' capacity (rare).
    this.relayout(
      {
        tlasNodes: needTop ? room(0, tlas, c.tlasNodes) : c.tlasNodes,
        transforms: needTop ? room(0, leaves, c.transforms) : c.transforms,
        nodes: needNodes ? room(s.nodes.tail, more.nodes, c.nodes) : c.nodes,
        indices: needIndex ? room(s.index.tail, more.indices, c.indices) : c.indices,
        vertices: needVertices ? room(s.vertices.tail, more.vertices, c.vertices) : c.vertices,
      },
      colored,
    )
  }

  /**
   * Room at once for segments still to commit: one layout for a whole fill
   * (an export's, the viewport's first), not one each time a segment passes
   * the room left, each a copy of everything and a whole upload.
   */
  reserveFor(segments: readonly Segment[], leaves = this.standIns.length) {
    let nodes = 0
    let indices = 0
    let vertices = 0
    let colored = false
    for (const s of segments) {
      nodes += s.nodes.length / NODE_WORDS
      indices += s.index.length
      vertices += s.vertices.length / 4
      colored ||= s.colors !== null
    }
    this.reserve({ nodes, indices, vertices }, leaves, colored)
  }

  /**
   * Copies a segment in under key, rebased where it lands, and queues its
   * ranges for upload. leaves is the number of top-level leaves the next
   * setInstances will have (the top level's room is made at once).
   */
  commit(key: string, s: Segment, leaves = this.standIns.length) {
    if (this.placed.has(key)) return
    if (this.dropped) throw new ArraysDropped()
    const vertices = s.vertices.length / 4
    this.reserve({ nodes: s.nodes.length / NODE_WORDS, indices: s.index.length, vertices }, leaves, s.colors !== null)
    const nodeStart = this.space.nodes.take(s.nodes.length / NODE_WORDS)!
    const indexStart = this.space.index.take(s.index.length)!
    const vertexStart = this.space.vertices.take(vertices)!
    const n32 = this.nodes.array as Uint32Array
    const n16 = new Uint16Array(n32.buffer)
    const at = this.capacity.tlasNodes + nodeStart
    n32.set(s.nodes, at * NODE_WORDS)
    const triangleBase = indexStart / 3
    for (let i = 0, n = s.nodes.length / NODE_WORDS; i < n; i++) if (n16[(at + i) * 16 + 15] === 0xffff) n32[(at + i) * NODE_WORDS + 6] += triangleBase
    const index = this.index.array as Uint32Array
    for (let i = 0; i < s.index.length; i++) index[indexStart + i] = s.index[i] + vertexStart
    const words = this.struct.getLength()
    new Float32Array((this.vertices.array as Uint32Array).buffer).set(s.vertices, vertexStart * words)
    if (s.colors) (this.colors.array as Uint32Array).set(s.colors, vertexStart)
    const rootBases: number[] = []
    const bounds: ArrayBuffer[] = []
    let base = 0
    for (const n of s.roots) {
      rootBases.push(at + base)
      bounds.push(s.nodes.slice(base * NODE_WORDS, (base + 1) * NODE_WORDS).buffer)
      base += n
    }
    this.placed.set(key, { nodes: { start: nodeStart, count: s.nodes.length / NODE_WORDS }, index: { start: indexStart, count: s.index.length }, vertices: { start: vertexStart, count: vertices }, rootBases, bounds, groups: s.rootGroups, depth: s.depth, colored: s.colors !== null })
    this.queue(this.nodes, at * NODE_WORDS, s.nodes.length)
    this.queue(this.index, indexStart, s.index.length)
    this.queue(this.vertices, vertexStart * words, vertices * words)
    if (s.colors) this.queue(this.colors, vertexStart, vertices)
  }

  /** A segment no instance draws: its ranges go back to be used again. */
  release(key: string) {
    const p = this.placed.get(key)
    if (!p) return
    this.placed.delete(key)
    this.space.nodes.give(p.nodes.start, p.nodes.count)
    this.space.index.give(p.index.start, p.index.count)
    this.space.vertices.give(p.vertices.start, p.vertices.count)
  }

  /** The share of the buffers in use left free between segments (S1 design: tidied once past 25 %). */
  get fragmentation(): number {
    const s = this.space
    const used = s.nodes.tail + s.index.tail + s.vertices.tail
    return used === 0 ? 0 : (s.nodes.wasted + s.index.wasted + s.vertices.wasted) / used
  }

  /**
   * Moves the segments down over the free ranges between them, their offsets
   * rebased again (on an idle frame, past 25 % fragmentation). The top level
   * is laid again by setInstances.
   */
  compact() {
    if (this.dropped) throw new ArraysDropped()
    const tlas = this.capacity.tlasNodes
    const words = this.struct.getLength()
    const n32 = this.nodes.array as Uint32Array
    const n16 = new Uint16Array(n32.buffer)
    const index = this.index.array as Uint32Array
    const vertices = this.vertices.array as Uint32Array
    const colors = this.colors.array as Uint32Array
    const colored = [...this.placed.values()].some((p) => p.colored)
    // Each buffer packed in the order its ranges lie: a range only ever moves down.
    const pack = (range: 'nodes' | 'index' | 'vertices', move: (from: number, to: number, count: number) => void) => {
      const moved = new Map<Placed, number>()
      let end = 0
      for (const p of [...this.placed.values()].sort((a, b) => a[range].start - b[range].start)) {
        const r = p[range]
        moved.set(p, end - r.start)
        if (r.start !== end) move(r.start, end, r.count)
        p[range] = { start: end, count: r.count }
        end += r.count
      }
      return { moved, end }
    }
    const nodes = pack('nodes', (from, to, count) => n32.copyWithin((tlas + to) * NODE_WORDS, (tlas + from) * NODE_WORDS, (tlas + from + count) * NODE_WORDS))
    const indices = pack('index', (from, to, count) => index.copyWithin(to, from, from + count))
    const verts = pack('vertices', (from, to, count) => {
      vertices.copyWithin(to * words, from * words, (from + count) * words)
      if (colored) colors.copyWithin(to, from, from + count)
    })
    for (const p of this.placed.values()) {
      const dn = nodes.moved.get(p)!
      const dt = indices.moved.get(p)! / 3
      const dv = verts.moved.get(p)!
      p.rootBases = p.rootBases.map((b) => b + dn)
      const at = tlas + p.nodes.start
      if (dt !== 0) for (let i = 0; i < p.nodes.count; i++) if (n16[(at + i) * 16 + 15] === 0xffff) n32[(at + i) * NODE_WORDS + 6] += dt
      if (dv !== 0) for (let i = p.index.start, end = p.index.start + p.index.count; i < end; i++) index[i] += dv
    }
    const reset = (s: Space, end: number) => {
      s.tail = end
      s.free = []
    }
    reset(this.space.nodes, nodes.end)
    reset(this.space.index, indices.end)
    reset(this.space.vertices, verts.end)
    this.queue(this.nodes, tlas * NODE_WORDS, nodes.end * NODE_WORDS)
    this.queue(this.index, 0, indices.end)
    this.queue(this.vertices, 0, verts.end * words)
    if (colored) this.queue(this.colors, 0, verts.end)
    this.top = null
    this.writeEmptyTop()
  }

  /** The buffers were laid out again (relayout, compact) under instances: setInstances must lay the top level again before a ray finds them. */
  get needsTop(): boolean {
    return this.top === null && this.standIns.length > 0
  }

  /** The top level over these instances, from scratch (instances came, went or changed segment), every transform written. */
  setInstances(instances: readonly Instance[]) {
    const leaves = instances.reduce((n, i) => n + i.rows.length, 0)
    this.reserve({ nodes: 0, indices: 0, vertices: 0 }, leaves, false)
    this.standIns = instances.map((i) => {
      // A mesh to the clustered tree (it collects meshes), not one three draws.
      const o = Object.assign(new Object3D(), { isMesh: true, rows: i.rows, segment: i.segment })
      o.matrixWorld.copy(i.matrixWorld)
      o.matrixWorldAutoUpdate = false
      return o
    })
    if (this.standIns.length === 0) {
      this.top = null
      this.writeEmptyTop()
      return
    }
    const root = new Group()
    root.children = this.standIns
    const top = new ClusteredBVH(root, {
      strategy: SAH,
      getBVH: (o) => ({ _roots: this.placed.get((o as unknown as { segment: string }).segment)!.bounds }),
      // Whole instances: a leaf per root.
      shouldCluster: () => true,
    })
    const map = (this as unknown as Internals)._getTransformMap(top)
    const { primitiveBuffer, primitiveBufferStride } = top
    const info: { transformSlot: number; nodeOffset: number }[] = []
    for (let i = 0; i < primitiveBuffer.length; i += primitiveBufferStride) {
      const compositeId = primitiveBuffer[i]
      const root = top.getBVHRootIndex(primitiveBuffer[i + 1])
      const object = top.objects[top.getObjectId(compositeId)] as unknown as { segment: string }
      info.push({ transformSlot: map.get(`${compositeId}_${root}`)!.slot, nodeOffset: this.placed.get(object.segment)!.rootBases[root] })
    }
    const tlasNodes = top._roots.reduce((n, r) => n + r.byteLength / 4 / NODE_WORDS, 0)
    appendBVHData(top, info, 0, (this.nodes.array as Uint32Array<ArrayBuffer>).buffer)
    this.top = top
    this.queue(this.nodes, 0, tlasNodes * NODE_WORDS)
    this.writeTransforms(null)
    const tlas = getMaxNodeDepth(top._roots[0])
    const blas = Math.max(0, ...this.standIns.map((s) => this.placed.get((s as unknown as { segment: string }).segment)!.depth))
    // As upstream's check: the top level's leaf pushes one node, not two.
    this.depth = { tlas, blas, traversal: tlas + blas - 1 }
  }

  /** Instances moved, by their place in the list setInstances took: the top level refit, its nodes and the moved slots uploaded. */
  move(moved: readonly { at: number; matrixWorld: Matrix4 }[]) {
    if (!this.top || moved.length === 0) return
    for (const m of moved) this.standIns[m.at].matrixWorld.copy(m.matrixWorld)
    this.top.refit()
    appendBVHData(this.top, null, 0, (this.nodes.array as Uint32Array<ArrayBuffer>).buffer)
    this.queue(this.nodes, 0, this.top._roots.reduce((n, r) => n + r.byteLength / 4 / NODE_WORDS, 0) * NODE_WORDS)
    this.writeTransforms(moved.map((m) => this.standIns[m.at]))
  }

  private writeTransforms(moved: Object3D[] | null) {
    if (!this.top) return
    const words = this.transformWords
    const buffer = (this.transforms.array as Uint32Array<ArrayBuffer>).buffer
    const t = new Uint32Array(buffer)
    inverse.copy(this.top.matrixWorld).invert()
    const self = this as unknown as Internals
    const map = self._getTransformMap(this.top)
    const write = (e: TransformInfo) => {
      self.writeTransformData(e, inverse, e.slot, buffer)
      const rows = (e.object as unknown as { rows: readonly number[] }).rows
      t[e.slot * words + MATERIAL_WORD] = rows[e.root] ?? rows[0]
    }
    if (moved === null) {
      map.forEach(write)
      this.queue(this.transforms, 0, map.size * words)
      return
    }
    const touched = new Set(moved)
    map.forEach((e) => {
      if (!touched.has(e.object)) return
      write(e)
      this.queue(this.transforms, e.slot * words, words)
    })
  }

  /** No instance: a root no ray enters (bounds min 1, max −1), a top-level leaf of nothing. */
  private writeEmptyTop() {
    const n32 = this.nodes.array as Uint32Array
    const f32 = new Float32Array(n32.buffer)
    f32.set([1, 1, 1, -1, -1, -1], 0)
    n32[6] = 0
    n32[7] = 0xff000000
    this.queue(this.nodes, 0, NODE_WORDS)
    this.depth = { tlas: 1, blas: 0, traversal: 0 }
  }

  /**
   * An export's layer once a frame has drawn with it (S1 design A5): the GPU
   * has each buffer whole (a new one uploads when first bound, an update when
   * its draw binds it), so the CPU keeps only what laying the top level again
   * or moving an instance writes: the nodes up to the geometries' trees, and
   * the transforms. Taking a segment or laying the buffers out again throws
   * ArraysDropped from then on.
   */
  dropArrays() {
    if (this.dropped) return
    this.dropped = true
    this.nodes.array = (this.nodes.array as Uint32Array).slice(0, this.capacity.tlasNodes * NODE_WORDS)
    // MIN_ELEMENTS each, not none: a later picture's pipeline is built from them, and three takes a buffer of one
    // element or less for a single struct (WGSLNodeBuilder isCustomStruct reads the array's length).
    for (const a of [this.index, this.vertices, this.colors]) a.array = new Uint32Array(MIN_ELEMENTS * a.itemSize)
  }

  /** Every buffer's GPU copy goes: once no frame still to be drawn binds the layer (base.ts retire). */
  dispose() {
    for (const a of [this.nodes, this.index, this.vertices, this.transforms, this.colors]) this.letGo(a)
  }
}
