// S0 group 2: the scene packed so an edit costs what it changes (Epic design「场景同步」), on three-mesh-bvh's own
// packing helpers. BVHComputeData.update() packs everything again and uploads every buffer whole; this subclass
// replaces update() alone:
//   - the buffers are allocated with room to spare, the node buffer with a fixed region for the top level at its
//     head and the geometries' trees after it, so a new geometry is written at the tail and uploads only itself;
//   - the top level references every instance whole (one leaf an instance), not the library's clusters of up to 64
//     triangles per leaf, which for 2000 instances of 300k-triangle meshes puts millions of leaves in the top level;
//   - moving an instance refits the top level and uploads its nodes and the moved slots, not the whole node buffer.
// Running out of room lays everything out again, larger: the one whole upload left.
import { Matrix4, type Object3D } from 'three'
import { storage } from 'three/tsl'
import { StorageBufferAttribute, StructTypeNode } from 'three/webgpu'
import { SAH, type MeshBVH } from 'three-mesh-bvh'
import { BVHComputeData, bvhNodeStruct } from 'three-mesh-bvh/webgpu'
import { ClusteredBVH } from 'three-mesh-bvh/src/webgpu/ClusteredBVH.js'
import { appendBVHData, appendBVHSubtree, appendGeometryData, appendIndexData, getMaxNodeDepth } from 'three-mesh-bvh/src/webgpu/utils/packBVHBufferUtils.js'

/** Words a node (BYTES_PER_NODE 32). */
const NODE_WORDS = 8
/** The traversal's stack (BVH_STACK_DEPTH). */
export const STACK_DEPTH = 60
/** No object: the material index a miss reads. */
export const NONE = 0xffffffff
/** The transform slot's first spare word (TransformStruct: matrixWorld 16, inverse 16, visible 1, _alignment0). */
export const MATERIAL_WORD = 33

export type Capacity = { tlasNodes: number; nodes: number; indices: number; vertices: number; transforms: number }
type Segment = { rootBases: number[]; nodes: number; indexBase: number; vertexBase: number; indices: number; vertices: number; depth: number }
type Attribute = StorageBufferAttribute & { addUpdateRange(start: number, count: number): void; needsUpdate: boolean }
type Top = { _roots: ArrayBuffer[]; primitiveBuffer: Uint32Array; primitiveBufferStride: number; objects: Object3D[]; matrixWorld: Matrix4; getObjectId(id: number): number; getBVHRootIndex(id: number): number; refit(): void }

/** The layer does not fit the device: which buffer, how large, and the limit. */
export class CapacityError extends Error {
  constructor(readonly buffer: string, readonly bytes: number, readonly limit: number) {
    super(`${buffer} needs ${bytes} bytes, the device binds at most ${limit}`)
  }
}

const inverse = new Matrix4()

export class TraceLayer extends BVHComputeData {
  readonly materialOf = new Map<Object3D, number>()
  readonly segments = new Map<MeshBVH, Segment>()
  capacity!: Capacity
  /** The tail of the geometries' region (nodes after the top level's region), of the index and of the vertices. */
  used = { nodes: 0, indices: 0, vertices: 0 }
  /** Bytes queued for upload since the last take(). */
  private queued = 0
  top!: Top
  depth = { tlas: 0, blas: 0, traversal: 0 }
  /** What the last top-level build needed, for an error. */
  lastTop: unknown = null
  private nodes!: Attribute
  private index!: Attribute
  private vertices!: Attribute
  private transforms!: Attribute
  private struct!: StructTypeNode
  private slots = new Map<Object3D, number[]>()

  /**
   * @param objects the meshes to trace
   * @param bvhOf the tree a mesh is traced against: its original geometry's or its proxy's (Epic「精度分档」)
   * @param maxBinding the device's limit on one buffer (min of maxStorageBufferBindingSize and maxBufferSize)
   */
  constructor(objects: Object3D[], readonly bvhOf: (o: Object3D) => MeshBVH, readonly maxBinding: number) {
    super(objects, { attributes: { position: 'vec4f', normal: 'vec4f' }, autogenerateBvh: false })
  }

  private get transformWords() {
    return (this.structs as unknown as { transform: { getLength(): number } }).transform.getLength()
  }

  /**
   * What each buffer would hold for these trees (one an instance), before any room to spare. A tree has a root per
   * geometry group (a material slot): each root is a top-level leaf of its own with its own transform slot.
   */
  static need(bvhs: MeshBVH[]) {
    let nodes = 0, indices = 0, vertices = 0, depth = 0
    const leaves = bvhs.reduce((n, b) => n + b._roots.length, 0)
    for (const b of new Set(bvhs)) {
      nodes += b._roots.reduce((n: number, r: ArrayBuffer) => n + r.byteLength / 32, 0)
      indices += b.geometry.index!.count
      vertices += b.geometry.attributes.position.count
      depth = Math.max(depth, ...b._roots.map((r: ArrayBuffer) => getMaxNodeDepth(r)))
    }
    // A binary tree over the leaves has 2n - 1 nodes.
    return { nodes, indices, vertices, tlasNodes: 2 * leaves - 1, transforms: leaves, depth }
  }

  /** Everything laid out again, with room for spare times what is there (the first pack, and the one whole upload after). */
  update(spare = 1.25) {
    const instances = this.objects.map((o) => this.bvhOf(o))
    const bvhs = [...new Set(instances)]
    const need = TraceLayer.need(instances)
    const room = (n: number) => Math.ceil(n * spare) + 64
    this.layout({ tlasNodes: room(need.tlasNodes), nodes: room(need.nodes), indices: room(need.indices), vertices: room(need.vertices), transforms: room(need.transforms) })
    for (const bvh of bvhs) this.writeSegment(bvh)
    if (!this.buildTop()) throw new Error(`the top level does not fit the room laid out for it (${JSON.stringify(this.lastTop)})`)
    // A new buffer uploads whole when it is first bound, so the ranges just written would only upload it again.
    for (const a of [this.nodes, this.index, this.vertices, this.transforms]) (a as unknown as { clearUpdateRanges(): void }).clearUpdateRanges()
    this.queued = this.bytes
  }

  private layout(capacity: Capacity) {
    this.dispose()
    this.struct = new StructTypeNode(this.attributes, 'bvh_GeometryStruct')
    const words = this.struct.getLength()
    const bytes = {
      nodes: (capacity.tlasNodes + capacity.nodes) * NODE_WORDS * 4,
      index: capacity.indices * 4,
      attributes: capacity.vertices * words * 4,
      transforms: capacity.transforms * this.transformWords * 4,
    }
    for (const [buffer, size] of Object.entries(bytes)) if (size > this.maxBinding) throw new CapacityError(buffer, size, this.maxBinding)
    this.capacity = capacity
    this.used = { nodes: 0, indices: 0, vertices: 0 }
    this.segments.clear()
    this.nodes = new StorageBufferAttribute(new Uint32Array(bytes.nodes / 4), 1) as Attribute
    this.index = new StorageBufferAttribute(new Uint32Array(bytes.index / 4), 1) as Attribute
    this.vertices = new StorageBufferAttribute(new Uint32Array(bytes.attributes / 4), words) as Attribute
    this.transforms = new StorageBufferAttribute(new Uint32Array(bytes.transforms / 4), 1) as Attribute
    const s = this.storage as unknown as Record<string, unknown>
    // The names and types upstream's update() gives them: the traversal's proxies resolve to these.
    s.nodes = storage(this.nodes, bvhNodeStruct).toReadOnly().setName('bvh_nodes')
    s.transforms = storage(this.transforms, (this.structs as unknown as { transform: unknown }).transform as never).toReadOnly().setName('bvh_transforms')
    s.index = storage(this.index, 'uint').toReadOnly().setName('bvh_index')
    s.attributes = storage(this.vertices, this.struct).toReadOnly().setName('bvh_attributes')
    ;(this.structs as unknown as Record<string, unknown>).attributes = this.struct
  }

  private queue(attribute: Attribute, start: number, count: number) {
    attribute.addUpdateRange(start, count)
    attribute.needsUpdate = true
    this.queued += count * 4
  }

  /** Bytes uploaded by the edits since the last take (written on the next render or compute). */
  take() {
    const bytes = this.queued
    this.queued = 0
    return bytes
  }

  /** The bytes the layer holds on the GPU. */
  get bytes() {
    return [this.nodes, this.index, this.vertices, this.transforms].reduce((n, a) => n + (a.array as Uint32Array).byteLength, 0)
  }

  /** A geometry's tree, index and vertices written at the tail; false when there is no room left. */
  writeSegment(bvh: MeshBVH): boolean {
    if (this.segments.has(bvh)) return true
    const geometry = bvh.geometry
    const indices = geometry.index!.count
    const vertices = geometry.attributes.position.count
    const nodes = bvh._roots.reduce((n: number, r: ArrayBuffer) => n + r.byteLength / 32, 0)
    const { capacity, used } = this
    if (used.nodes + nodes > capacity.nodes || used.indices + indices > capacity.indices || used.vertices + vertices > capacity.vertices) return false
    const range = { start: 0, count: indices, vertexStart: 0, vertexCount: vertices }
    const indexBase = used.indices
    const vertexBase = used.vertices
    appendIndexData(bvh, range, vertexBase, indexBase, this.index.array as Uint32Array)
    appendGeometryData(bvh, range, vertexBase, (this.vertices.array as Uint32Array).buffer, this.struct, this)
    const rootBases: number[] = []
    let write = capacity.tlasNodes + used.nodes
    for (const root of bvh._roots as ArrayBuffer[]) {
      const size = root.byteLength / 32
      appendBVHSubtree(root, 0, size, indexBase / 3, write, (this.nodes.array as Uint32Array).buffer)
      rootBases.push(write)
      write += size
    }
    const depth = Math.max(...(bvh._roots as ArrayBuffer[]).map((r) => getMaxNodeDepth(r)))
    this.segments.set(bvh, { rootBases, nodes, indexBase, vertexBase, indices, vertices, depth })
    const words = this.struct.getLength()
    this.queue(this.nodes, (capacity.tlasNodes + used.nodes) * NODE_WORDS, nodes * NODE_WORDS)
    this.queue(this.index, indexBase, indices)
    this.queue(this.vertices, vertexBase * words, vertices * words)
    used.nodes += nodes
    used.indices += indices
    used.vertices += vertices
    return true
  }

  /** The top level over the current objects, from scratch (instances added or removed), with every transform. */
  buildTop() {
    const top = new ClusteredBVH(this.getRootObject(), {
      strategy: SAH,
      getBVH: (object: Object3D) => this.bvhOf(object),
      // Whole instances: one leaf an instance (see the file's head).
      shouldCluster: () => true,
    }) as unknown as Top
    const tlasNodes = top._roots.reduce((n, r) => n + r.byteLength / 32, 0)
    const map = (this as unknown as { _getTransformMap(b: Top): Map<string, { object: Object3D; slot: number }> })._getTransformMap(top)
    this.lastTop = { tlasNodes, transforms: map.size, capacity: this.capacity }
    if (tlasNodes > this.capacity.tlasNodes || map.size > this.capacity.transforms) return false
    const { primitiveBuffer, primitiveBufferStride } = top
    const info: { transformSlot: number; nodeOffset: number }[] = []
    for (let i = 0; i < primitiveBuffer.length; i += primitiveBufferStride) {
      const compositeId = primitiveBuffer[i]
      const root = top.getBVHRootIndex(primitiveBuffer[i + 1])
      const object = top.objects[top.getObjectId(compositeId)]
      const segment = this.segments.get(this.bvhOf(object))!
      info.push({ transformSlot: map.get(`${compositeId}_${root}`)!.slot, nodeOffset: segment.rootBases[root] })
    }
    appendBVHData(top, info, 0, (this.nodes.array as Uint32Array).buffer)
    this.top = top
    this.slots.clear()
    map.forEach((entry) => this.slots.set(entry.object, [...(this.slots.get(entry.object) ?? []), entry.slot]))
    this.queue(this.nodes, 0, tlasNodes * NODE_WORDS)
    this.writeTransforms(null)
    const tlas = getMaxNodeDepth(top._roots[0])
    const blas = Math.max(0, ...[...this.segments.values()].map((s) => s.depth))
    // As upstream's check: the top level's leaf pushes one node, not two.
    this.depth = { tlas, blas, traversal: tlas + blas - 1 }
    if (this.depth.traversal > STACK_DEPTH) throw new Error(`traversal depth ${this.depth.traversal} overruns the stack of ${STACK_DEPTH}`)
    return true
  }

  /** Objects moved (or every object when null): the top level refit, its nodes and the moved slots uploaded. */
  updateTransforms(moved: Object3D[] | null = null) {
    this.top.refit()
    appendBVHData(this.top, null, 0, (this.nodes.array as Uint32Array).buffer)
    this.queue(this.nodes, 0, this.top._roots.reduce((n, r) => n + r.byteLength / 32, 0) * NODE_WORDS)
    this.writeTransforms(moved)
  }

  private writeTransforms(moved: Object3D[] | null) {
    const words = this.transformWords
    const buffer = (this.transforms.array as Uint32Array).buffer
    inverse.copy(this.top.matrixWorld).invert()
    const map = (this as unknown as { _getTransformMap(b: Top): Map<string, { object: Object3D; slot: number }> })._getTransformMap(this.top)
    if (moved === null) {
      map.forEach((entry) => this.writeTransformData(entry, inverse, entry.slot, buffer))
      this.queue(this.transforms, 0, map.size * words)
      return
    }
    const entries = new Map([...map.values()].map((e) => [e.slot, e]))
    for (const object of moved) {
      for (const slot of this.slots.get(object) ?? []) {
        this.writeTransformData(entries.get(slot)!, inverse, slot, buffer)
        this.queue(this.transforms, slot * words, words)
      }
    }
  }

  writeTransformData(info: { object: Object3D }, premultiply: Matrix4, slot: number, target: ArrayBuffer) {
    // @ts-expect-error -- private in the typings, the override upstream documents for this
    super.writeTransformData(info, premultiply, slot, target)
    new Uint32Array(target)[slot * this.transformWords + MATERIAL_WORD] = this.materialOf.get(info.object) ?? NONE
  }

  dispose() {
    // BufferAttribute.dispose() lets the renderer free the GPU buffer.
    for (const a of [this.nodes, this.index, this.vertices, this.transforms]) a?.dispose()
  }
}
