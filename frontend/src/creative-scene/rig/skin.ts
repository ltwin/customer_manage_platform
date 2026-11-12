import { BufferAttribute, BufferGeometry, Matrix4, type InterleavedBufferAttribute, type Object3D } from 'three'
import { MeshBVH } from 'three-mesh-bvh'
import { disposePieceViews } from '../render/pieces.ts'
import { sha256 } from '../models/sha256.ts'
import type { Pose } from './retarget.ts'
import type { SkinnedPart } from './template.ts'

// The static geometry of a skinned primitive in a pose (design「带骨骼的模型
// 怎样加载和绘制」): skinned on the CPU in the model root's space,
//   v' = Σ wᵢ · (Jᵢ · inverse bindᵢ) · v,
// Jᵢ being joint i's matrix in that space, so it does not depend on where an
// instance stands and instances of one model in one pose share it. What reads
// it (the world-space layer, picking, outlines) pairs it with the instance's
// root matrix. Normals take the inverse transpose of the same blend.

/** The pose's hash for cache keys: "rest" for none; otherwise its non-identity rotations by bone name, 6 decimals. */
export function poseHash(pose: Pose | null): string {
  if (!pose) return 'rest'
  const text = Object.keys(pose)
    .sort()
    .flatMap((b) => {
      const q = pose[b]
      if (!q) return []
      // q and −q are the same rotation.
      const sign = q.w < 0 ? -1 : 1
      const xyzw = [q.x, q.y, q.z, q.w].map((c) => (sign * c).toFixed(6).replace(/^-(0\.0+)$/, '$1'))
      return xyzw.join(',') === '0.000000,0.000000,0.000000,1.000000' ? [] : [`${b}:${xyzw.join(',')}`]
    })
    .join(';')
  const sum = sha256(new TextEncoder().encode(text))
  return Array.from(sum.subarray(0, 8), (x) => x.toString(16).padStart(2, '0')).join('')
}

/**
 * What a skinned mesh is traced and picked as, on its userData.posed, set by
 * the model drawing it (objects/SkinnedModel.tsx): its static geometry in the
 * current pose, in the model root's space, and that root, whose world matrix
 * places it. Unset while the pose is being worked out.
 */
export type Posed = { geometry: BufferGeometry; root: Object3D }

/** The world-space layer's key for a part in a pose: <model key>#n<node>p<primitive>#<pose hash>. */
export const traceKeyOf = (modelKey: string, part: SkinnedPart, hash: string) => `${modelKey}#${part.id}#${hash}`

type Attribute = BufferAttribute | InterleavedBufferAttribute

/** The same values in an attribute of its own, the array shared where it can be: three frees a geometry's GPU buffers per attribute. */
function shareOf(a: Attribute): BufferAttribute {
  if (a instanceof BufferAttribute) return new BufferAttribute(a.array, a.itemSize, a.normalized)
  return a.clone()
}

/** A skinned geometry's inputs as plain arrays, read once per geometry: a pose then reads arrays, not attributes. */
type Flat = { position: Float32Array; normal: Float32Array | null; tangent: Float32Array | null; joints: Uint32Array; weights: Float32Array }
const flats = new WeakMap<BufferGeometry, Flat>()

function flatOf(g: BufferGeometry): Flat {
  const hit = flats.get(g)
  if (hit) return hit
  const read = (a: Attribute, size: number) => {
    const out = new Float32Array(a.count * size)
    for (let i = 0; i < a.count; i++) for (let k = 0; k < size; k++) out[i * size + k] = a.getComponent(i, k)
    return out
  }
  const skinIndex = g.getAttribute('skinIndex')
  const joints = new Uint32Array(skinIndex.count * 4)
  for (let i = 0; i < skinIndex.count; i++) for (let k = 0; k < 4; k++) joints[i * 4 + k] = skinIndex.getComponent(i, k)
  const flat: Flat = {
    position: read(g.getAttribute('position'), 3),
    normal: g.hasAttribute('normal') ? read(g.getAttribute('normal'), 3) : null,
    tangent: g.hasAttribute('tangent') ? read(g.getAttribute('tangent'), 4) : null,
    joints,
    weights: read(g.getAttribute('skinWeight'), 4),
  }
  flats.set(g, flat)
  return flat
}

/**
 * The part's mesh skinned at its skeleton's current state, in root's space.
 * The world matrices of root and the joints must be up to date. Each vertex
 * blends the 3 × 4 it needs of its joints' matrices; normals take the inverse
 * transpose of the blend's 3 × 3, tangents the 3 × 3 itself, both normalized.
 */
export function staticGeometry(part: SkinnedPart, root: Object3D): BufferGeometry {
  const { geometry: src, skeleton } = part.mesh
  const toRoot = root.matrixWorld.clone().invert()
  const joint = new Matrix4()
  const matrices = new Float32Array(skeleton.bones.length * 16)
  skeleton.bones.forEach((b, i) => {
    joint.multiplyMatrices(toRoot, b.matrixWorld).multiply(skeleton.boneInverses[i]).multiply(part.mesh.bindMatrix)
    matrices.set(joint.elements, i * 16)
  })
  const { position, normal, tangent, joints, weights } = flatOf(src)
  const count = position.length / 3
  const outPosition = new Float32Array(count * 3)
  const outNormal = normal ? new Float32Array(count * 3) : null
  const outTangent = tangent ? new Float32Array(count * 4) : null
  for (let i = 0; i < count; i++) {
    // Column-major, the bottom row left out: e0 e1 e2 | e4 e5 e6 | e8 e9 e10 | e12 e13 e14.
    let e0 = 0, e1 = 0, e2 = 0, e4 = 0, e5 = 0, e6 = 0, e8 = 0, e9 = 0, e10 = 0, e12 = 0, e13 = 0, e14 = 0
    for (let k = 0; k < 4; k++) {
      const w = weights[i * 4 + k]
      if (w === 0) continue
      const o = joints[i * 4 + k] * 16
      e0 += w * matrices[o]; e1 += w * matrices[o + 1]; e2 += w * matrices[o + 2]
      e4 += w * matrices[o + 4]; e5 += w * matrices[o + 5]; e6 += w * matrices[o + 6]
      e8 += w * matrices[o + 8]; e9 += w * matrices[o + 9]; e10 += w * matrices[o + 10]
      e12 += w * matrices[o + 12]; e13 += w * matrices[o + 13]; e14 += w * matrices[o + 14]
    }
    const x = position[i * 3], y = position[i * 3 + 1], z = position[i * 3 + 2]
    outPosition[i * 3] = e0 * x + e4 * y + e8 * z + e12
    outPosition[i * 3 + 1] = e1 * x + e5 * y + e9 * z + e13
    outPosition[i * 3 + 2] = e2 * x + e6 * y + e10 * z + e14
    if (normal && outNormal) {
      // The inverse transpose of the blend's 3 × 3 without inverting it: its columns are c1 × c2, c2 × c0 and
      // c0 × c1 over the determinant, whose sign alone matters once normalized.
      const a = normal[i * 3], b = normal[i * 3 + 1], c = normal[i * 3 + 2]
      const x0 = e5 * e10 - e6 * e9, y0 = e6 * e8 - e4 * e10, z0 = e4 * e9 - e5 * e8
      const x1 = e9 * e2 - e10 * e1, y1 = e10 * e0 - e8 * e2, z1 = e8 * e1 - e9 * e0
      const x2 = e1 * e6 - e2 * e5, y2 = e2 * e4 - e0 * e6, z2 = e0 * e5 - e1 * e4
      const sign = e0 * x0 + e1 * y0 + e2 * z0 < 0 ? -1 : 1
      const nx = sign * (a * x0 + b * x1 + c * x2), ny = sign * (a * y0 + b * y1 + c * y2), nz = sign * (a * z0 + b * z1 + c * z2)
      const l = Math.hypot(nx, ny, nz) || 1
      outNormal[i * 3] = nx / l
      outNormal[i * 3 + 1] = ny / l
      outNormal[i * 3 + 2] = nz / l
    }
    if (tangent && outTangent) {
      const a = tangent[i * 4], b = tangent[i * 4 + 1], c = tangent[i * 4 + 2]
      const tx = e0 * a + e4 * b + e8 * c, ty = e1 * a + e5 * b + e9 * c, tz = e2 * a + e6 * b + e10 * c
      const l = Math.hypot(tx, ty, tz) || 1
      outTangent[i * 4] = tx / l
      outTangent[i * 4 + 1] = ty / l
      outTangent[i * 4 + 2] = tz / l
      outTangent[i * 4 + 3] = tangent[i * 4 + 3]
    }
  }
  const out = new BufferGeometry()
  out.setAttribute('position', new BufferAttribute(outPosition, 3))
  if (outNormal) out.setAttribute('normal', new BufferAttribute(outNormal, 3))
  if (outTangent) out.setAttribute('tangent', new BufferAttribute(outTangent, 4))
  for (const [name, a] of Object.entries(src.attributes)) {
    if (!['position', 'normal', 'tangent', 'skinIndex', 'skinWeight'].includes(name)) out.setAttribute(name, shareOf(a))
  }
  if (src.index) out.setIndex(shareOf(src.index))
  // Without NORMAL they come of the posed faces, through the index once it is set.
  if (!outNormal) out.computeVertexNormals()
  for (const g of src.groups) out.addGroup(g.start, g.count, g.materialIndex)
  out.setDrawRange(src.drawRange.start, src.drawRange.count)
  out.computeBoundingBox()
  out.computeBoundingSphere()
  return out
}

/** The bytes a static geometry holds on the CPU: positions, normals, tangents, the index and its BVH. */
export function measure(g: BufferGeometry): number {
  let bytes = 0
  for (const name of ['position', 'normal', 'tangent']) {
    const a = g.getAttribute(name) as Attribute | undefined
    if (a instanceof BufferAttribute) bytes += a.array.byteLength
  }
  bytes += g.index?.array.byteLength ?? 0
  const tree = (g as { boundsTree?: MeshBVH }).boundsTree
  if (tree) {
    const s = MeshBVH.serialize(tree, { cloneBuffers: false })
    bytes += s.roots.reduce((n, r) => n + r.byteLength, 0) + (s.indirectBuffer?.byteLength ?? 0)
  }
  return bytes
}

type Entry = { geometry: BufferGeometry; users: number; bytes: number; cached: boolean }

/**
 * Static geometries by trace key, within a byte budget (design「缓存预算」).
 * An entry some instance is drawing is pinned; the others go least recently
 * used first, their geometry and BVH freed. When the pinned ones alone pass
 * the budget, new entries are not kept: each is freed when its last user lets
 * go, and the console says so once.
 */
export class StaticGeometryCache {
  readonly budget: number
  private readonly entries = new Map<string, Entry>()
  private readonly warn: (message: string) => void
  private over = false

  constructor(budget = 256 * 1024 * 1024, warn: (message: string) => void = (m) => console.warn(m)) {
    this.budget = budget
    this.warn = warn
  }

  /** The geometry under key, made when missing; the caller draws it until release. */
  acquire(key: string, make: () => BufferGeometry): BufferGeometry {
    const hit = this.entries.get(key)
    if (hit) {
      hit.users++
      this.entries.delete(key)
      this.entries.set(key, hit)
      return hit.geometry
    }
    const geometry = make()
    geometry.userData.traceKey = key
    const entry: Entry = { geometry, users: 1, bytes: measure(geometry), cached: true }
    this.entries.set(key, entry)
    if (this.trim() > this.budget) {
      entry.cached = false
      if (!this.over) this.warn(`姿态几何缓存超出 ${Math.round(this.budget / 2 ** 20)} MiB 预算，在用的几何不再缓存`)
      this.over = true
    }
    return geometry
  }

  /** One user of key lets go. */
  release(key: string) {
    const entry = this.entries.get(key)
    if (!entry || entry.users === 0) return
    entry.users--
    if (entry.users > 0) return
    if (!entry.cached) this.drop(key, entry)
    else this.trim()
  }

  /** The bytes held now. */
  get bytes(): number {
    let n = 0
    for (const e of this.entries.values()) n += e.bytes
    return n
  }

  has(key: string): boolean {
    return this.entries.has(key)
  }

  /** Frees everything; nothing may be drawing it. */
  clear() {
    for (const [key, e] of [...this.entries]) this.drop(key, e)
    this.over = false
  }

  /** Measures again (a BVH may have been built since), frees unpinned entries oldest first while over budget; returns the bytes left. */
  private trim(): number {
    let total = 0
    for (const e of this.entries.values()) total += e.bytes = measure(e.geometry)
    for (const [key, e] of [...this.entries]) {
      if (total <= this.budget) break
      if (e.users > 0) continue
      total -= e.bytes
      this.drop(key, e)
    }
    if (total <= this.budget) this.over = false
    return total
  }

  private drop(key: string, e: Entry) {
    this.entries.delete(key)
    disposePieceViews(e.geometry)
    ;(e.geometry as { boundsTree?: MeshBVH }).boundsTree = undefined
    e.geometry.dispose()
  }
}

/** The editor's cache of static geometries. */
export const staticGeometries = new StaticGeometryCache()
