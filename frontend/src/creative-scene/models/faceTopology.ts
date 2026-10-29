import { Box3, type BufferGeometry } from 'three'
import { weld } from './weld.ts'

// How a mesh's triangles sit together, for picking faces (scene-face-materials
// design「平面面片」): flat patches, the pieces triangles join into, and each
// triangle's neighbours across shared edges. Corners are welded where their
// positions round to the same 1e-5 m, so split vertices still join. All in
// typed arrays: a 300k-triangle mesh is built once on entering face edit.

/** Normals within this of the patch's first triangle are flat with it. */
const flatAngle = (0.05 * Math.PI) / 180
const flatCos = Math.cos(flatAngle)
const weldStep = 1e-5

export type FaceTopology = {
  triangleCount: number
  /** Each corner's welded vertex, three per triangle. */
  corners: Uint32Array
  /** Unit normals, three numbers per triangle; zero for a degenerate one. */
  normals: Float32Array
  patches: number[][]
  patchOf: Int32Array
  components: number
  componentOf: Int32Array
  /** Triangles sharing an edge with this one. */
  neighbours(t: number): Int32Array
  /**
   * Each edge between welded vertices, in compressed rows: its triangles are
   * edgeTriangles[edgeStart[e] … edgeStart[e + 1]), and edgeCorner[e] is the
   * first corner of one of them on it (the other end is the next corner).
   */
  edgeStart: Int32Array
  edgeTriangles: Int32Array
  edgeCorner: Int32Array
}

/** The corner after c in its triangle. */
export const nextCorner = (c: number) => (c % 3 === 2 ? c - 2 : c + 1)

export function faceTopology(geometry: BufferGeometry): FaceTopology {
  const position = geometry.attributes.position
  const index = geometry.index
  const cornerCount = index ? index.count : position.count
  const triangleCount = cornerCount / 3

  // Each vertex welded once, the corners through it: numbers and arrays, no strings.
  const vertices = weld(position.count, (v, out) => {
    out[0] = Math.round(position.getX(v) / weldStep)
    out[1] = Math.round(position.getY(v) / weldStep)
    out[2] = Math.round(position.getZ(v) / weldStep)
  })
  const welded = vertices.size
  const weldedPosition = new Float64Array(3 * welded)
  vertices.firsts.forEach((v, w) => {
    weldedPosition[3 * w] = position.getX(v)
    weldedPosition[3 * w + 1] = position.getY(v)
    weldedPosition[3 * w + 2] = position.getZ(v)
  })
  const corners = new Uint32Array(cornerCount)

  const vertexOf = (c: number) => (index ? index.getX(c) : c)
  for (let c = 0; c < cornerCount; c++) corners[c] = vertices.ids[vertexOf(c)]
  const normals = new Float32Array(triangleCount * 3)
  for (let t = 0; t < triangleCount; t++) {
    const a = vertexOf(3 * t)
    const b = vertexOf(3 * t + 1)
    const c = vertexOf(3 * t + 2)
    const ux = position.getX(b) - position.getX(a)
    const uy = position.getY(b) - position.getY(a)
    const uz = position.getZ(b) - position.getZ(a)
    const vx = position.getX(c) - position.getX(a)
    const vy = position.getY(c) - position.getY(a)
    const vz = position.getZ(c) - position.getZ(a)
    const nx = uy * vz - uz * vy
    const ny = uz * vx - ux * vz
    const nz = ux * vy - uy * vx
    const length = Math.hypot(nx, ny, nz)
    if (length > 0) {
      normals[3 * t] = nx / length
      normals[3 * t + 1] = ny / length
      normals[3 * t + 2] = nz / length
    }
  }

  // Edges: each corner to the next, by its two welded vertices in either order.
  const edges = weld(cornerCount, (c, out) => {
    const p = corners[c]
    const q = corners[nextCorner(c)]
    // A degenerate edge (both ends one vertex) gets a key no real edge has.
    out[0] = p === q ? -1 : Math.min(p, q)
    out[1] = p === q ? c : Math.max(p, q)
    out[2] = 0
  })
  const edgeOf = edges.ids
  const edgeCount = edges.size
  const real = new Uint8Array(edgeCount)
  for (let c = 0; c < cornerCount; c++) if (corners[c] !== corners[nextCorner(c)]) real[edgeOf[c]] = 1
  const edgeStart = new Int32Array(edgeCount + 1)
  for (let c = 0; c < cornerCount; c++) if (real[edgeOf[c]]) edgeStart[edgeOf[c] + 1]++
  for (let e = 0; e < edgeCount; e++) edgeStart[e + 1] += edgeStart[e]
  const edgeTriangles = new Int32Array(edgeStart[edgeCount])
  const fill = edgeStart.slice(0, edgeCount)
  for (let c = 0; c < cornerCount; c++) if (real[edgeOf[c]]) edgeTriangles[fill[edgeOf[c]]++] = (c / 3) | 0
  const edgeCorner = new Int32Array(edgeCount).fill(-1)
  for (let c = 0; c < cornerCount; c++) if (real[edgeOf[c]] && edgeCorner[edgeOf[c]] < 0) edgeCorner[edgeOf[c]] = c

  // Neighbours in compressed rows: the other triangles on each of a triangle's edges, once each.
  const neighbourStart = new Int32Array(triangleCount + 1)
  for (let c = 0; c < cornerCount; c++) {
    const e = edgeOf[c]
    if (real[e]) neighbourStart[((c / 3) | 0) + 1] += edgeStart[e + 1] - edgeStart[e] - 1
  }
  for (let t = 0; t < triangleCount; t++) neighbourStart[t + 1] += neighbourStart[t]
  const neighbourList = new Int32Array(neighbourStart[triangleCount])
  const neighbourEnd = new Int32Array(triangleCount)
  for (let t = 0; t < triangleCount; t++) {
    let n = neighbourStart[t]
    for (let k = 0; k < 3; k++) {
      const e = edgeOf[3 * t + k]
      if (!real[e]) continue
      for (let i = edgeStart[e]; i < edgeStart[e + 1]; i++) {
        const u = edgeTriangles[i]
        if (u === t) continue
        let known = false
        for (let j = neighbourStart[t]; j < n && !known; j++) known = neighbourList[j] === u
        if (!known) neighbourList[n++] = u
      }
    }
    neighbourEnd[t] = n
  }
  const neighbours = (t: number) => neighbourList.subarray(neighbourStart[t], neighbourEnd[t])

  const box = new Box3().setFromArray(weldedPosition)
  const flatDistance = Math.max(1e-5, 1e-6 * (box.isEmpty() ? 0 : box.min.distanceTo(box.max)))
  const patchOf = new Int32Array(triangleCount).fill(-1)
  const patches: number[][] = []
  for (let seed = 0; seed < triangleCount; seed++) {
    if (patchOf[seed] >= 0) continue
    const id = patches.length
    const patch = [seed]
    patchOf[seed] = id
    patches.push(patch)
    const sx = normals[3 * seed]
    const sy = normals[3 * seed + 1]
    const sz = normals[3 * seed + 2]
    if (sx === 0 && sy === 0 && sz === 0) continue
    const s = corners[3 * seed]
    const offset = sx * weldedPosition[3 * s] + sy * weldedPosition[3 * s + 1] + sz * weldedPosition[3 * s + 2]
    const flat = (t: number) => {
      if (normals[3 * t] * sx + normals[3 * t + 1] * sy + normals[3 * t + 2] * sz < flatCos) return false
      for (let k = 0; k < 3; k++) {
        const w = corners[3 * t + k]
        const d = sx * weldedPosition[3 * w] + sy * weldedPosition[3 * w + 1] + sz * weldedPosition[3 * w + 2] - offset
        if (Math.abs(d) > flatDistance) return false
      }
      return true
    }
    for (let i = 0; i < patch.length; i++)
      for (const u of neighbours(patch[i]))
        if (patchOf[u] < 0 && flat(u)) {
          patchOf[u] = id
          patch.push(u)
        }
  }

  const componentOf = new Int32Array(triangleCount).fill(-1)
  const queue = new Int32Array(triangleCount)
  let components = 0
  for (let seed = 0; seed < triangleCount; seed++) {
    if (componentOf[seed] >= 0) continue
    let head = 0
    let tail = 0
    queue[tail++] = seed
    componentOf[seed] = components
    while (head < tail)
      for (const u of neighbours(queue[head++]))
        if (componentOf[u] < 0) {
          componentOf[u] = components
          queue[tail++] = u
        }
    components++
  }

  return { triangleCount, corners, normals, patches, patchOf, components, componentOf, neighbours, edgeStart, edgeTriangles, edgeCorner }
}
