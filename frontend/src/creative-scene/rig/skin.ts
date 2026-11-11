import { BufferAttribute, BufferGeometry, Matrix3, Matrix4, Vector3, Vector4, type InterleavedBufferAttribute, type Object3D } from 'three'
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

/** The world-space layer's key for a part in a pose: <model key>#n<node>p<primitive>#<pose hash>. */
export const traceKeyOf = (modelKey: string, part: SkinnedPart, hash: string) => `${modelKey}#${part.id}#${hash}`

type Attribute = BufferAttribute | InterleavedBufferAttribute

/** The same values in an attribute of its own, the array shared where it can be: three frees a geometry's GPU buffers per attribute. */
function shareOf(a: Attribute): BufferAttribute {
  if (a instanceof BufferAttribute) return new BufferAttribute(a.array, a.itemSize, a.normalized)
  return a.clone()
}

/**
 * The part's mesh skinned at its skeleton's current state, in root's space.
 * The world matrices of root and the joints must be up to date.
 */
export function staticGeometry(part: SkinnedPart, root: Object3D): BufferGeometry {
  const { geometry: src, skeleton } = part.mesh
  const toRoot = root.matrixWorld.clone().invert()
  const joints = skeleton.bones.map((b, i) => new Matrix4().multiplyMatrices(toRoot, b.matrixWorld).multiply(skeleton.boneInverses[i]).multiply(part.mesh.bindMatrix))
  const position = src.getAttribute('position')
  const normal = src.hasAttribute('normal') ? src.getAttribute('normal') : null
  const tangent = src.hasAttribute('tangent') ? src.getAttribute('tangent') : null
  const skinIndex = src.getAttribute('skinIndex')
  const skinWeight = src.getAttribute('skinWeight')
  const count = position.count
  const outPosition = new Float32Array(count * 3)
  const outNormal = normal ? new Float32Array(count * 3) : null
  const outTangent = tangent ? new Float32Array(count * 4) : null
  const blend = new Matrix4()
  const e = blend.elements
  const normalMatrix = new Matrix3()
  const v = new Vector3()
  const t = new Vector4()
  for (let i = 0; i < count; i++) {
    e.fill(0)
    for (let k = 0; k < 4; k++) {
      const w = skinWeight.getComponent(i, k)
      if (w === 0) continue
      const m = joints[skinIndex.getComponent(i, k)].elements
      for (let c = 0; c < 16; c++) e[c] += w * m[c]
    }
    v.fromBufferAttribute(position, i).applyMatrix4(blend)
    v.toArray(outPosition, i * 3)
    if (normal && outNormal) {
      normalMatrix.getNormalMatrix(blend)
      v.fromBufferAttribute(normal, i).applyMatrix3(normalMatrix).normalize().toArray(outNormal, i * 3)
    }
    if (tangent && outTangent) {
      t.set(tangent.getX(i), tangent.getY(i), tangent.getZ(i), tangent.getW(i))
      v.set(t.x, t.y, t.z).transformDirection(blend).toArray(outTangent, i * 4)
      outTangent[i * 4 + 3] = t.w
    }
  }
  const out = new BufferGeometry()
  out.setAttribute('position', new BufferAttribute(outPosition, 3))
  if (outNormal) out.setAttribute('normal', new BufferAttribute(outNormal, 3))
  else out.computeVertexNormals()
  if (outTangent) out.setAttribute('tangent', new BufferAttribute(outTangent, 4))
  for (const [name, a] of Object.entries(src.attributes)) {
    if (!['position', 'normal', 'tangent', 'skinIndex', 'skinWeight'].includes(name)) out.setAttribute(name, shareOf(a))
  }
  if (src.index) out.setIndex(shareOf(src.index))
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
