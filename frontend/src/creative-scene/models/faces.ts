import { BufferAttribute, BufferGeometry, Mesh, type Material, type Object3D } from 'three'
import type { components } from '../../api/schema'
import type { MeshIdentity } from '../sandbox/gltf.ts'

// Face assignments (data: 模型内容「面级材质分配」): some of a mesh's
// triangles drawn with another slot. The GLB is never rewritten; loading
// splits each mesh by its assignment, found by the mesh's identity and kept
// only while its fingerprint still matches. Pure: no React, no scene state.

export type Faces = components['schemas']['CreativeModelFaces']
export type FaceMesh = components['schemas']['CreativeModelFaceMesh']
export type Assign = Record<string, [number, number][]>

/** A mesh of a loaded model, in model space and in arrays of its own. */
export type OwnedMesh = { identity: MeshIdentity; topology: string; geometry: BufferGeometry; material: Material }

/** A slot and how many triangles an assignment gave it or took away. */
export type SlotCount = { slot: string; triangles: number }

export function slotOfMaterial(material: Material): string | null {
  return /^slot:(.+)$/.exec(material.name)?.[1] ?? null
}

const sameIdentity = (a: MeshIdentity, m: FaceMesh) => a.primitive === m.primitive && JSON.stringify(a.path) === JSON.stringify(m.path)

const triangleCount = (g: BufferGeometry) => (g.index ?? g.attributes.position).count / 3

/**
 * How the triangles connect, as `t1-<16 hex>`: the index, or without one
 * the corners welded where their float32 bits are equal, numbered in order of
 * first use. Coordinates do not count, so resizing usually keeps it.
 */
export async function topologyOf(geometry: BufferGeometry): Promise<string> {
  const position = geometry.attributes.position
  let corners: ArrayLike<number>
  if (geometry.index) corners = geometry.index.array
  else {
    const bits = new Uint32Array(new Float32Array(3).buffer)
    const floats = new Float32Array(bits.buffer)
    const welded = new Map<string, number>()
    const ids = new Uint32Array(position.count)
    for (let i = 0; i < position.count; i++) {
      floats[0] = position.getX(i)
      floats[1] = position.getY(i)
      floats[2] = position.getZ(i)
      const key = `${bits[0]},${bits[1]},${bits[2]}`
      let n = welded.get(key)
      if (n === undefined) welded.set(key, (n = welded.size))
      ids[i] = n
    }
    corners = ids
  }
  const view = new DataView(new ArrayBuffer(4 * (2 + corners.length)))
  view.setUint32(0, position.count, true)
  view.setUint32(4, triangleCount(geometry), true)
  for (let i = 0; i < corners.length; i++) view.setUint32(8 + 4 * i, corners[i], true)
  const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', view.buffer))
  return 't1-' + Array.from(digest.subarray(0, 8), (b) => b.toString(16).padStart(2, '0')).join('')
}

/**
 * Swaps the second and third corner of every triangle. A mirrored part's
 * transform turned its faces inside out; three.js would flip them back while
 * the transform was the object's, but baked into the geometry it is not.
 */
export function flipWinding(geometry: BufferGeometry) {
  const index = geometry.index
  if (index) {
    for (let i = 0; i + 2 < index.count; i += 3) {
      const b = index.getX(i + 1)
      index.setX(i + 1, index.getX(i + 2))
      index.setX(i + 2, b)
    }
    index.needsUpdate = true
    return
  }
  for (const attribute of Object.values(geometry.attributes) as BufferAttribute[]) {
    const n = attribute.itemSize
    const a = attribute.array
    for (let i = 0; i + 2 < attribute.count; i += 3)
      for (let k = 0; k < n; k++) {
        const x = a[(i + 1) * n + k]
        a[(i + 1) * n + k] = a[(i + 2) * n + k]
        a[(i + 2) * n + k] = x
      }
    attribute.needsUpdate = true
  }
}

/**
 * The model's meshes in traversal order, each fingerprinted as loaded, then
 * copied and given its transform once (flipped back if mirrored). The loaded
 * geometries are never written, so they can be read again.
 */
export async function ownedMeshes(root: Object3D, identities: ReadonlyMap<Mesh, MeshIdentity>): Promise<OwnedMesh[]> {
  root.updateMatrixWorld(true)
  const found: Mesh[] = []
  root.traverse((o) => {
    if (o instanceof Mesh) found.push(o)
  })
  return Promise.all(
    found.map(async (o): Promise<OwnedMesh> => {
      const identity = identities.get(o)
      if (!identity) throw new Error('网格缺少身份')
      const topology = await topologyOf(o.geometry)
      const geometry = (o.geometry as BufferGeometry).clone()
      geometry.applyMatrix4(o.matrixWorld)
      if (o.matrixWorld.determinant() < 0) flipWinding(geometry)
      const material = (Array.isArray(o.material) ? o.material[0] : o.material) as Material
      return { identity, topology, geometry, material }
    }),
  )
}

/** A new geometry of the given triangles only, its vertices compacted into arrays of its own. */
function extract(geometry: BufferGeometry, triangles: readonly number[]): BufferGeometry {
  const out = new BufferGeometry()
  const index = geometry.index
  const names = Object.keys(geometry.attributes)
  if (index) {
    const remap = new Map<number, number>()
    const order: number[] = []
    const indices = new Uint32Array(triangles.length * 3)
    triangles.forEach((t, i) => {
      for (let k = 0; k < 3; k++) {
        const v = index.getX(t * 3 + k)
        let n = remap.get(v)
        if (n === undefined) {
          remap.set(v, (n = order.length))
          order.push(v)
        }
        indices[i * 3 + k] = n
      }
    })
    for (const name of names) out.setAttribute(name, gather(geometry.getAttribute(name) as BufferAttribute, order))
    out.setIndex(new BufferAttribute(order.length > 65535 ? indices : new Uint16Array(indices), 1))
    return out
  }
  const order = triangles.flatMap((t) => [t * 3, t * 3 + 1, t * 3 + 2])
  for (const name of names) out.setAttribute(name, gather(geometry.getAttribute(name) as BufferAttribute, order))
  return out
}

function gather(source: BufferAttribute, vertices: readonly number[]): BufferAttribute {
  const n = source.itemSize
  const Array_ = source.array.constructor as new (length: number) => typeof source.array
  const array = new Array_(vertices.length * n)
  vertices.forEach((v, i) => {
    for (let k = 0; k < n; k++) array[i * n + k] = source.getComponent(v, k)
  })
  return new BufferAttribute(array, n, source.normalized)
}

/**
 * The geometry split by slot: the assigned ranges to theirs, the rest to
 * `base` (the GLB's own slot). A slot left with no triangle is left out.
 */
export function splitBySlot(geometry: BufferGeometry, assign: Assign, base: string | null): { slot: string | null; assigned: boolean; geometry: BufferGeometry }[] {
  const count = triangleCount(geometry)
  const owner = new Array<string | undefined>(count)
  for (const [slot, ranges] of Object.entries(assign)) for (const t of trianglesOf(ranges)) owner[t] = slot
  const rest: number[] = []
  const bySlot = new Map<string, number[]>()
  for (let t = 0; t < count; t++) {
    const slot = owner[t]
    if (slot === undefined) rest.push(t)
    else {
      const list = bySlot.get(slot)
      if (list) list.push(t)
      else bySlot.set(slot, [t])
    }
  }
  const out: { slot: string | null; assigned: boolean; geometry: BufferGeometry }[] = []
  if (rest.length) out.push({ slot: base, assigned: false, geometry: extract(geometry, rest) })
  for (const [slot, triangles] of bySlot) out.push({ slot, assigned: true, geometry: extract(geometry, triangles) })
  return out
}

function fits(assign: Assign, count: number): boolean {
  return Object.values(assign).every((ranges) => ranges.every(([start, end]) => start >= 0 && start < end && end <= count))
}

/**
 * Each mesh whole, or split by its assignment when one is found by identity
 * and its fingerprint matches. `stale` lists the assignments that did not
 * apply: the mesh changed, is gone, or the ranges overrun it.
 */
export function applyFaces(meshes: readonly OwnedMesh[], faces: Faces | null | undefined): { pieces: { mesh: OwnedMesh; slot: string | null; assigned: boolean; geometry: BufferGeometry }[]; stale: MeshIdentity[] } {
  const entries = faces?.meshes ?? []
  const used = new Set<FaceMesh>()
  const stale: MeshIdentity[] = []
  const pieces = meshes.flatMap((mesh) => {
    const base = slotOfMaterial(mesh.material)
    const entry = entries.find((m) => sameIdentity(mesh.identity, m))
    if (entry) used.add(entry)
    const assign = entry?.assign as Assign | undefined
    if (!entry || !assign) return [{ mesh, slot: base, assigned: false, geometry: mesh.geometry }]
    if (entry.topology !== mesh.topology || !fits(assign, triangleCount(mesh.geometry))) {
      stale.push(mesh.identity)
      return [{ mesh, slot: base, assigned: false, geometry: mesh.geometry }]
    }
    return splitBySlot(mesh.geometry, assign, base).map((p) => ({ mesh, ...p }))
  })
  for (const entry of entries) if (!used.has(entry)) stale.push({ path: entry.path as MeshIdentity['path'], primitive: entry.primitive })
  return { pieces, stale }
}

function countBySlot(entries: readonly FaceMesh[]): SlotCount[] {
  const totals = new Map<string, number>()
  for (const e of entries)
    for (const [slot, ranges] of Object.entries(e.assign as Assign)) totals.set(slot, (totals.get(slot) ?? 0) + ranges.reduce((n, [s, t]) => n + t - s, 0))
  return [...totals].map(([slot, triangles]) => ({ slot, triangles }))
}

/**
 * The assignment carried to a regenerated GLB (data: 模型内容「重新生成时」):
 * an entry stays when its identity is still there with the same fingerprint.
 * When a sibling of the same name, primitive and fingerprint is there too,
 * which is which cannot be proven; it stays only if the code is unchanged.
 */
export function carryFaces(old: Faces | null | undefined, next: readonly { identity: MeshIdentity; topology: string }[], codeChanged: boolean): { faces: Faces | null; kept: SlotCount[]; dropped: SlotCount[] } {
  if (!old) return { faces: null, kept: [], dropped: [] }
  const kept: FaceMesh[] = []
  const dropped: FaceMesh[] = []
  for (const entry of old.meshes) {
    const match = next.find((m) => sameIdentity(m.identity, entry))
    const parent = JSON.stringify(entry.path.slice(0, -1))
    const name = entry.path[entry.path.length - 1]?.[0]
    const lookalikes = next.filter(
      (m) =>
        m.topology === entry.topology &&
        m.identity.primitive === entry.primitive &&
        JSON.stringify(m.identity.path.slice(0, -1)) === parent &&
        m.identity.path[m.identity.path.length - 1]?.[0] === name,
    )
    if (match && match.topology === entry.topology && !(lookalikes.length > 1 && codeChanged)) kept.push(entry)
    else dropped.push(entry)
  }
  const faces = kept.length ? { ...old, meshes: kept } : null
  return { faces, kept: countBySlot(kept), dropped: countBySlot(dropped) }
}

/** Triangle numbers as ascending `[start, end)` ranges, neighbours merged. */
export function rangesOf(triangles: Iterable<number>): [number, number][] {
  const sorted = [...new Set(triangles)].sort((a, b) => a - b)
  const out: [number, number][] = []
  for (const t of sorted) {
    const last = out[out.length - 1]
    if (last && last[1] === t) last[1] = t + 1
    else out.push([t, t + 1])
  }
  return out
}

export function trianglesOf(ranges: readonly (readonly number[])[]): number[] {
  return ranges.flatMap(([start, end]) => Array.from({ length: end - start }, (_, i) => start + i))
}

/**
 * The assignment with these triangles moved to `slot`, or back to the GLB's
 * own slot when `slot` is null. Slots left empty are dropped.
 */
export function withAssigned(assign: Assign, triangles: Iterable<number>, slot: string | null): Assign {
  const moving = new Set(triangles)
  const out: Assign = {}
  for (const [s, ranges] of Object.entries(assign)) {
    const left = rangesOf(trianglesOf(ranges).filter((t) => !moving.has(t)))
    if (left.length) out[s] = left
  }
  if (slot !== null) out[slot] = rangesOf([...trianglesOf(out[slot] ?? []), ...moving])
  return out
}

/**
 * The slots an object shows (data: 模型内容「有效槽位」): the GLB's, then
 * those the assignment brings, each with the triangles assigned to it.
 */
export function effectiveSlots(glb: readonly { id: string; hint: string }[], faces: Faces | null | undefined): { id: string; hint: string; source: 'model' | 'faces'; triangles: number }[] {
  const counts = new Map(countBySlot(faces?.meshes ?? []).map((c) => [c.slot, c.triangles]))
  const out: { id: string; hint: string; source: 'model' | 'faces'; triangles: number }[] = glb.map((s) => ({ id: s.id, hint: s.hint, source: 'model', triangles: counts.get(s.id) ?? 0 }))
  for (const [slot, triangles] of counts) if (!glb.some((s) => s.id === slot)) out.push({ id: slot, hint: faces?.hints?.[slot] ?? '', source: 'faces', triangles })
  return out
}
