import { BufferAttribute, BufferGeometry, EdgesGeometry, MeshBasicMaterial } from 'three'
import { weld } from '../models/weld.ts'
import { perPiece } from '../render/pieces.ts'

// The wireframe shading (Blender's X-ray wireframe): meshes are not drawn but
// still picked, and each geometry shows its outline edges. Edges are worked
// out once per geometry, so instances of one model share them. Edges and the
// selection outline's shell are made per piece of a merged model geometry
// (render/pieces.ts): where two parts touch, the edges show as before.

export const wireBackground = '#3d3f43'
export const wireColor = '#c9c6bf'
/** Edges between faces bending more than this show; smooth curves stay clean. */
const thresholdDeg = 30

/** Not drawn, still hit by the pick ray (three.js checks the object, not the material). */
export const hiddenMaterial = new MeshBasicMaterial({ visible: false })

const edges = new WeakMap<BufferGeometry, BufferGeometry>()

export function edgesOf(geometry: BufferGeometry): BufferGeometry {
  let e = edges.get(geometry)
  if (!e) {
    e = perPiece(geometry, (g) => new EdgesGeometry(g, thresholdDeg))
    edges.set(geometry, e)
  }
  return e
}

/**
 * The selection outline's shell (objects/Outline.tsx): a copy with the
 * normals averaged across creases, per piece. It shares no buffer with the
 * model, so freeing the shell frees none of the model's.
 */
export function shellOf(geometry: BufferGeometry): BufferGeometry {
  return perPiece(geometry, creasedShell)
}

/**
 * Corners this close count as one (0.1 mm): seams that differ by a float's
 * error still close. three's toCreasedNormals took 1 cm, which on a dense
 * mesh put dozens of faces in each corner's group, every corner going
 * through them all: three seconds for two million triangles
 * (scene-glb-import 6.3).
 */
const cornerGrid = 1e4

/**
 * three's toCreasedNormals at a crease angle of π, non-indexed: each corner
 * the sum of the unit normals of the faces meeting there, except those
 * facing exactly away, so the two sides of a thin card part.
 */
function creasedShell(g: BufferGeometry): BufferGeometry {
  const position = g.attributes.position!
  const index = g.index
  const corners = index ? index.count : position.count
  const vertexOf = (c: number) => (index ? index.getX(c) : c)
  const { ids, size } = weld(position.count, (v, out) => {
    out[0] = Math.trunc(position.getX(v) * cornerGrid)
    out[1] = Math.trunc(position.getY(v) * cornerGrid)
    out[2] = Math.trunc(position.getZ(v) * cornerGrid)
  })
  const p = new Float32Array(corners * 3)
  for (let c = 0; c < corners; c++) {
    const v = vertexOf(c)
    p[3 * c] = position.getX(v)
    p[3 * c + 1] = position.getY(v)
    p[3 * c + 2] = position.getZ(v)
  }
  // Each face's unit normal, as three works it out.
  const faces = corners / 3
  const facing = new Float64Array(faces * 3)
  for (let f = 0; f < faces; f++) {
    const a = 9 * f
    const ux = p[a + 6]! - p[a + 3]!
    const uy = p[a + 7]! - p[a + 4]!
    const uz = p[a + 8]! - p[a + 5]!
    const wx = p[a]! - p[a + 3]!
    const wy = p[a + 1]! - p[a + 4]!
    const wz = p[a + 2]! - p[a + 5]!
    const nx = uy * wz - uz * wy
    const ny = uz * wx - ux * wz
    const nz = ux * wy - uy * wx
    const inv = 1 / (Math.sqrt(nx * nx + ny * ny + nz * nz) || 1)
    facing[3 * f] = nx * inv
    facing[3 * f + 1] = ny * inv
    facing[3 * f + 2] = nz * inv
  }
  // The faces at each corner, grouped by the corner's number.
  const cornerId = (c: number) => ids[vertexOf(c)]!
  const offsets = new Int32Array(size + 1)
  for (let c = 0; c < corners; c++) offsets[cornerId(c) + 1]!++
  for (let i = 0; i < size; i++) offsets[i + 1]! += offsets[i]!
  const cursor = offsets.slice(0, size)
  const around = new Int32Array(corners)
  for (let c = 0; c < corners; c++) around[cursor[cornerId(c)]!++] = (c / 3) | 0
  const normals = new Float32Array(corners * 3)
  for (let c = 0; c < corners; c++) {
    const f = 3 * ((c / 3) | 0)
    const id = cornerId(c)
    let x = 0
    let y = 0
    let z = 0
    for (let k = offsets[id]!, end = offsets[id + 1]!; k < end; k++) {
      const o = 3 * around[k]!
      if (facing[f]! * facing[o]! + facing[f + 1]! * facing[o + 1]! + facing[f + 2]! * facing[o + 2]! > -1) {
        x += facing[o]!
        y += facing[o + 1]!
        z += facing[o + 2]!
      }
    }
    const inv = 1 / (Math.sqrt(x * x + y * y + z * z) || 1)
    normals[3 * c] = x * inv
    normals[3 * c + 1] = y * inv
    normals[3 * c + 2] = z * inv
  }
  return new BufferGeometry().setAttribute('position', new BufferAttribute(p, 3)).setAttribute('normal', new BufferAttribute(normals, 3))
}

/** Frees a geometry's edges along with the geometry. */
export function disposeEdges(geometry: BufferGeometry) {
  edges.get(geometry)?.dispose()
  edges.delete(geometry)
}
