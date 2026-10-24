// S0 group 5, proxy quality (scene-trace-spike design「判定标准」组 5): what the probes' and the reflections'
// comparisons share. The proxies at each ε come from group 2's pool and cache (meshoptimizer, an absolute bound,
// borders locked; each geometry here is one slot, so a slot apart is the geometry whole). The error they actually
// make is measured both ways, surface to surface: points on the original to the proxy, and on the proxy to the
// original (the bound is meshoptimizer's estimate, not a distance).
import { Vector3, type BufferAttribute, type BufferGeometry, type Mesh } from 'three'
import type { MeshBVH } from 'three-mesh-bvh'
import type { Built } from './scene.ts'
import { prepare, type Pair } from './throughput.ts'

/** The candidate bounds, metres (?eps= picks some). */
export const EPSILONS = [0.001, 0.003, 0.01, 0.03]

const round = (v: number, d = 3) => Math.round(v * 10 ** d) / 10 ** d

/** Points on a tree's surface, by area, from a fixed sequence. */
function samplePoints(bvh: MeshBVH, count: number) {
  const g = bvh.geometry as BufferGeometry
  const pos = g.attributes.position as BufferAttribute
  const index = g.index!.array
  const tris = index.length / 3
  const a = new Vector3(), b = new Vector3(), c = new Vector3()
  const cdf = new Float64Array(tris)
  let total = 0
  for (let t = 0; t < tris; t++) {
    a.fromBufferAttribute(pos, index[3 * t])
    b.fromBufferAttribute(pos, index[3 * t + 1])
    c.fromBufferAttribute(pos, index[3 * t + 2])
    total += b.sub(a).cross(c.sub(a)).length() / 2
    cdf[t] = total
  }
  const points: Vector3[] = []
  for (let k = 0; k < count; k++) {
    const u = ((k + 0.5) * 0.6180339887) % 1
    let lo = 0, hi = tris - 1
    while (lo < hi) {
      const mid = (lo + hi) >> 1
      if (cdf[mid] < u * total) lo = mid + 1
      else hi = mid
    }
    let r1 = ((k + 0.5) * 0.7548776662) % 1, r2 = ((k + 0.5) * 0.5698402910) % 1
    if (r1 + r2 > 1) [r1, r2] = [1 - r1, 1 - r2]
    a.fromBufferAttribute(pos, index[3 * lo])
    b.fromBufferAttribute(pos, index[3 * lo + 1])
    c.fromBufferAttribute(pos, index[3 * lo + 2])
    points.push(a.clone().add(b.sub(a).multiplyScalar(r1)).add(c.sub(a).multiplyScalar(r2)))
  }
  return points
}

/** Distances from points on one surface to the other, scaled to metres by the largest instance scale. */
function distances(from: MeshBVH, to: MeshBVH, count: number, scale: number) {
  const target = { point: new Vector3(), distance: 0, faceIndex: 0 }
  return samplePoints(from, count).map((p) => (to.closestPointToPoint(p, target as never), target.distance * scale))
}

const percentile = (sorted: number[], q: number) => sorted[Math.min(sorted.length - 1, Math.floor(sorted.length * q))]

/** The scene's proxies at ε: the trees (original and proxy, both from the workers) and what the proxies cost and miss by. */
export async function proxiesAt(scene: string, built: Built, epsilon: number, samples: number) {
  const prepared = await prepare(scene, built, 'use', epsilon)
  const all: number[] = []
  const started = performance.now()
  for (const [geometry, pair] of prepared.pairs as Map<BufferGeometry, Pair>) {
    const meshes = built.meshes.filter((m: Mesh) => m.geometry === geometry)
    const scale = Math.max(...meshes.map((m) => m.matrixWorld.getMaxScaleOnAxis()))
    all.push(...distances(pair.original, pair.proxy, samples, scale), ...distances(pair.proxy, pair.original, samples, scale))
  }
  const measureMs = performance.now() - started
  all.sort((x, y) => x - y)
  const mm = (v: number) => round(v * 1000, 2)
  return {
    pairs: prepared.pairs as Map<BufferGeometry, Pair>,
    report: {
      epsilon,
      triangles: prepared.report.triangles,
      boundReported: prepared.report.proxyError,
      depth: prepared.report.depth,
      workerMs: prepared.report.workerMs,
      wallMs: prepared.report.wallMs,
      // Surface to surface both ways, millimetres: what the proxy actually misses by.
      errorMm: { samples: all.length, p50: mm(percentile(all, 0.5)), p95: mm(percentile(all, 0.95)), p99: mm(percentile(all, 0.99)), max: mm(all[all.length - 1]), overBound: round(all.filter((d) => d > epsilon).length / all.length, 5) },
      measureMs: Math.round(measureMs),
    },
  }
}

/** Two encoded pictures (RGBA bytes) apart: the mean channel difference, the share of pixels off by more than 2, and the difference × 8. */
export function compare(a: Uint8ClampedArray, b: Uint8ClampedArray, w: number, h: number, mask?: (p: number) => boolean) {
  let sum = 0, over = 0, n = 0, worst = 0
  const diff = new Uint8ClampedArray(w * h * 4)
  for (let p = 0; p < w * h; p++) {
    diff[4 * p + 3] = 255
    if (mask && !mask(p)) continue
    let most = 0
    for (let c = 0; c < 3; c++) {
      const d = Math.abs(a[4 * p + c] - b[4 * p + c])
      most = Math.max(most, d)
      sum += d / 3
      diff[4 * p + c] = Math.min(255, 8 * d)
    }
    if (most > 2) over++
    worst = Math.max(worst, most)
    n++
  }
  return { mean: round(sum / Math.max(n, 1), 3), over2: round(over / Math.max(n, 1), 5), max: worst, diff }
}

/** Pictures side by side at half size: averaged (pictures) or the larger of each 2 × 2 (differences, so a one-pixel difference still shows). */
export function strip(images: Uint8ClampedArray[], w: number, h: number, pool: 'mean' | 'max') {
  const hw = w >> 1, hh = h >> 1
  const out = new Uint8ClampedArray(hw * images.length * hh * 4)
  images.forEach((img, i) => {
    for (let y = 0; y < hh; y++) {
      for (let x = 0; x < hw; x++) {
        const o = 4 * (y * hw * images.length + i * hw + x)
        for (let c = 0; c < 3; c++) {
          const v = [0, 1, w, w + 1].map((d) => img[4 * (2 * y * w + 2 * x + d) + c])
          out[o + c] = pool === 'max' ? Math.max(...v) : (v[0] + v[1] + v[2] + v[3]) / 4
        }
        out[o + 3] = 255
      }
    }
  })
  return { data: out, width: hw * images.length, height: hh }
}
