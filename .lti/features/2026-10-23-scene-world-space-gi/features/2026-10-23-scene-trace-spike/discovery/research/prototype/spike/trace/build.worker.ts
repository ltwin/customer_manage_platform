// S0 group 2: one geometry's one-off work off the main thread (Epic design「一次性开销与缓存」): the original's BVH,
// the tracing proxy (meshoptimizer, an absolute error bound, borders locked) and the proxy's BVH, each as the
// serialized form the geometry cache keeps. A plain module worker: ParallelMeshBVHWorker needs SharedArrayBuffer,
// which the product's pages do not have (no cross-origin isolation), so the pool parallelises across geometries.
import { BufferAttribute, BufferGeometry } from 'three'
import { MeshBVH, SAH } from 'three-mesh-bvh'
import { getMaxNodeDepth } from 'three-mesh-bvh/src/webgpu/utils/packBVHBufferUtils.js'
import { MeshoptSimplifier } from 'meshoptimizer'

export type BuildJob = {
  key: string
  positions: Float32Array
  normals: Float32Array
  index: Uint32Array
  /** The proxy's error bound in the geometry's own units (ε in metres over its largest instance scale). */
  epsilon: number
}
export type SerializedBVH = { roots: ArrayBuffer[]; index: Uint32Array }
export type BuiltGeometry = {
  key: string
  original: SerializedBVH & { depth: number; nodes: number }
  proxy: SerializedBVH & { positions: Float32Array; normals: Float32Array; depth: number; nodes: number; triangles: number; error: number }
  ms: { bvh: number; simplify: number; proxyBvh: number }
}

const bvhOptions = { strategy: SAH, targetLeafSize: 10, maxDepth: 40 } as const

function build(positions: Float32Array, index: Uint32Array) {
  const geometry = new BufferGeometry()
  geometry.setAttribute('position', new BufferAttribute(positions, 3))
  geometry.setIndex(new BufferAttribute(index, 1))
  // The build reorders the index in place: the cache keeps the reordered one with the nodes.
  const bvh = new MeshBVH(geometry, bvhOptions)
  const { roots, index: reordered } = MeshBVH.serialize(bvh, { cloneBuffers: false }) as unknown as SerializedBVH
  return { roots, index: reordered, depth: Math.max(...roots.map((r) => getMaxNodeDepth(r))), nodes: roots.reduce((n, r) => n + r.byteLength / 32, 0) }
}

self.onmessage = async ({ data: job }: MessageEvent<BuildJob>) => {
  await MeshoptSimplifier.ready
  let t = performance.now()
  const original = build(job.positions, job.index.slice())
  const bvhMs = performance.now() - t

  t = performance.now()
  // As far as the bound allows (target 0), the error absolute in the geometry's units; borders (open edges, a slot's
  // edge) stay where they are so neighbouring parts do not crack apart.
  const [simplified, error] = MeshoptSimplifier.simplify(job.index, job.positions, 3, 0, job.epsilon, ['ErrorAbsolute', 'LockBorder'])
  // Only the vertices the proxy keeps (simplify moves none: each kept vertex keeps its normal).
  const [remap, unique] = MeshoptSimplifier.compactMesh(simplified)
  const positions = new Float32Array(unique * 3)
  const normals = new Float32Array(unique * 3)
  for (let v = 0; v < remap.length; v++) {
    const to = remap[v]
    if (to === 0xffffffff) continue
    positions.set(job.positions.subarray(3 * v, 3 * v + 3), 3 * to)
    normals.set(job.normals.subarray(3 * v, 3 * v + 3), 3 * to)
  }
  const simplifyMs = performance.now() - t

  t = performance.now()
  const proxy = build(positions, simplified)
  const proxyBvhMs = performance.now() - t

  const result: BuiltGeometry = {
    key: job.key,
    original,
    proxy: { ...proxy, positions, normals, triangles: simplified.length / 3, error },
    ms: { bvh: bvhMs, simplify: simplifyMs, proxyBvh: proxyBvhMs },
  }
  const transfer = [...original.roots, original.index.buffer, ...proxy.roots, proxy.index.buffer, positions.buffer, normals.buffer]
  ;(self as unknown as Worker).postMessage(result, transfer as Transferable[])
}
