import { Box3, type BufferGeometry } from 'three'

// How a mesh's triangles sit together, for picking faces (scene-face-materials
// design「平面面片」): flat patches, the pieces triangles join into, and each
// triangle's neighbours across shared edges. Corners are welded where their
// positions round to the same 1e-5 m, so split vertices still join.

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
  neighbours(t: number): readonly number[]
}

export function faceTopology(geometry: BufferGeometry): FaceTopology {
  const position = geometry.attributes.position
  const index = geometry.index
  const cornerCount = index ? index.count : position.count
  const triangleCount = cornerCount / 3
  const vertexOf = (c: number) => (index ? index.getX(c) : c)

  const welded = new Map<string, number>()
  const weldedPosition: number[] = []
  const corners = new Uint32Array(cornerCount)
  for (let c = 0; c < cornerCount; c++) {
    const v = vertexOf(c)
    const x = position.getX(v)
    const y = position.getY(v)
    const z = position.getZ(v)
    const key = `${Math.round(x / weldStep)},${Math.round(y / weldStep)},${Math.round(z / weldStep)}`
    let id = welded.get(key)
    if (id === undefined) {
      welded.set(key, (id = welded.size))
      weldedPosition.push(x, y, z)
    }
    corners[c] = id
  }

  const normals = new Float32Array(triangleCount * 3)
  for (let t = 0; t < triangleCount; t++) {
    const [a, b, c] = [vertexOf(3 * t), vertexOf(3 * t + 1), vertexOf(3 * t + 2)]
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
    if (length > 0) normals.set([nx / length, ny / length, nz / length], 3 * t)
  }

  // Neighbours in compressed rows: triangles grouped by the undirected edges they share.
  const byEdge = new Map<string, number[]>()
  for (let t = 0; t < triangleCount; t++)
    for (let k = 0; k < 3; k++) {
      const p = corners[3 * t + k]
      const q = corners[3 * t + ((k + 1) % 3)]
      if (p === q) continue
      const key = p < q ? `${p},${q}` : `${q},${p}`
      const list = byEdge.get(key)
      if (list) list.push(t)
      else byEdge.set(key, [t])
    }
  const lists: number[][] = Array.from({ length: triangleCount }, () => [])
  for (const sharing of byEdge.values())
    for (const t of sharing) for (const u of sharing) if (u !== t && !lists[t].includes(u)) lists[t].push(u)

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
    const [sx, sy, sz] = normals.subarray(3 * seed, 3 * seed + 3)
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
      for (const u of lists[patch[i]])
        if (patchOf[u] < 0 && flat(u)) {
          patchOf[u] = id
          patch.push(u)
        }
  }

  const componentOf = new Int32Array(triangleCount).fill(-1)
  let components = 0
  for (let seed = 0; seed < triangleCount; seed++) {
    if (componentOf[seed] >= 0) continue
    const queue = [seed]
    componentOf[seed] = components
    for (let i = 0; i < queue.length; i++)
      for (const u of lists[queue[i]])
        if (componentOf[u] < 0) {
          componentOf[u] = components
          queue.push(u)
        }
    components++
  }

  return { triangleCount, corners, normals, patches, patchOf, components, componentOf, neighbours: (t) => lists[t] }
}
