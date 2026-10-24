// S0 group 2: one large geometry's BVH, built by one plain worker (build.worker.ts, as the pool runs it) and by
// three-mesh-bvh's ParallelMeshBVHWorker, which splits one build across workers but needs SharedArrayBuffer, so a
// cross-origin isolated page (the measure script adds COOP/COEP; the product's pages have neither).
import { BufferAttribute, BufferGeometry } from 'three'
import { SAH } from 'three-mesh-bvh'
// The source path, not the package's: Vite then bundles the library's worker as it does the spike's own.
import { ParallelMeshBVHWorker } from '../../node_modules/three-mesh-bvh/src/workers/ParallelMeshBVHWorker.js'
import { generate } from '../../scripts/fixtures/scene-stress/generate.mjs'
import { BuildPool } from './pool.ts'

export async function runParallel() {
  const spec = await (await fetch('/scripts/fixtures/scene-stress/spec.json')).json()
  const ids = ['blob-57', 'knot-58', 'blob-00']
  const { geometries } = generate(spec, { only: ids })
  const result: Record<string, unknown> = { crossOriginIsolated, hardwareConcurrency: navigator.hardwareConcurrency }
  for (const g of geometries) {
    const row: Record<string, number | string> = { triangles: g.indices.length / 3 }
    const pool = new BuildPool(1)
    // ε large enough that the proxy is a small part of the time: the BVH is what is compared.
    const one = await pool.run({ key: g.id, positions: g.positions, normals: g.normals, index: g.indices, epsilon: 1 })
    pool.dispose()
    row.oneWorkerMs = Math.round(one.ms.bvh)
    if (crossOriginIsolated) {
      const worker = new ParallelMeshBVHWorker() as unknown as { generate(g: BufferGeometry, o: object): Promise<unknown>; dispose(): void; maxWorkerCount: number }
      const geometry = new BufferGeometry()
      geometry.setAttribute('position', new BufferAttribute(g.positions.slice(), 3))
      geometry.setIndex(new BufferAttribute(g.indices.slice(), 1))
      const started = performance.now()
      await worker.generate(geometry, { strategy: SAH, targetLeafSize: 10, maxDepth: 40 })
      row.parallelMs = Math.round(performance.now() - started)
      row.parallelWorkers = worker.maxWorkerCount
      worker.dispose()
    } else row.parallelMs = 'not cross-origin isolated'
    result[g.id] = row
  }
  return result
}
