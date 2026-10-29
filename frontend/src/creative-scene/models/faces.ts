import { BufferAttribute, BufferGeometry, Mesh, type Material, type Object3D } from 'three'
import type { components } from '../../api/schema'
import { parseModelGLB, type MeshIdentity } from '../sandbox/gltf.ts'
import { sha256 } from './sha256.ts'

// Face assignments (data: 模型内容「面级材质分配」): some of a mesh's
// triangles drawn with another slot. The GLB is never rewritten; loading
// splits each mesh by its assignment, found by the mesh's identity and kept
// only while its fingerprint still matches. Pure: no React, no scene state.

export type Faces = components['schemas']['CreativeModelFaces']
export type FaceMesh = components['schemas']['CreativeModelFaceMesh']
export type Assign = Record<string, [number, number][]>

/** A mesh of a loaded model, in model space and in arrays of its own; topology is '' for a mesh nobody asked about. */
export type OwnedMesh = { identity: MeshIdentity; topology: string; geometry: BufferGeometry; material: Material }

/** A slot and how many triangles an assignment gave it or took away. */
export type SlotCount = { slot: string; triangles: number }

/** data: 模型内容「面级材质分配」 and 场景文档「primitive.faces」. */
export const faceLimits = { ranges: 8192, builtinRanges: 256, bytes: 128 * 1024, slots: 32 } as const

export function slotOfMaterial(material: Material): string | null {
  return /^slot:(.+)$/.exec(material.name)?.[1] ?? null
}

/** A mesh's identity as one string, so entries are looked up rather than compared one by one. */
const keyOf = (m: { path: MeshIdentity['path'] | FaceMesh['path']; primitive: number }) => JSON.stringify([m.path, m.primitive])

const triangleCount = (g: BufferGeometry) => (g.index ?? g.attributes.position).count / 3

/**
 * How the triangles connect, as `t1-<16 hex>`: the index, or without one
 * the corners welded where their float32 bits are equal, numbered in order of
 * first use. Coordinates do not count, so resizing usually keeps it.
 */
export function topologyOf(geometry: BufferGeometry): string {
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
  const digest = sha256(new Uint8Array(view.buffer))
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
 * The model's meshes in traversal order, each copied and given its transform
 * once (flipped back if mirrored). The loaded geometries are never written,
 * so they can be read again. Only the meshes `fingerprint` picks are
 * fingerprinted, as loaded: hashing is most of the cost (a 300k-triangle
 * mesh takes some 40 ms) and an assignment usually names few meshes.
 */
export function ownedMeshes(root: Object3D, identities: ReadonlyMap<Mesh, MeshIdentity>, fingerprint: (identity: MeshIdentity) => boolean = () => true): OwnedMesh[] {
  root.updateMatrixWorld(true)
  const found: Mesh[] = []
  root.traverse((o) => {
    if (o instanceof Mesh) found.push(o)
  })
  return found.map((o): OwnedMesh => {
    const identity = identities.get(o)
    if (!identity) throw new Error('网格缺少身份')
    const topology = fingerprint(identity) ? topologyOf(o.geometry) : ''
    const geometry = (o.geometry as BufferGeometry).clone()
    geometry.applyMatrix4(o.matrixWorld)
    if (o.matrixWorld.determinant() < 0) flipWinding(geometry)
    const material = (Array.isArray(o.material) ? o.material[0] : o.material) as Material
    return { identity, topology, geometry, material }
  })
}

/** A new geometry of the given triangles only, its vertices compacted into arrays of its own. */
export function extract(geometry: BufferGeometry, triangles: readonly number[]): BufferGeometry {
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

/**
 * Through the attributes, not their arrays: an interleaved source's array is
 * the whole buffer, and a normalized one reads 0–1 and is scaled back on write.
 */
function gather(source: BufferAttribute, vertices: readonly number[]): BufferAttribute {
  const n = source.itemSize
  const Array_ = source.array.constructor as new (length: number) => typeof source.array
  const out = new BufferAttribute(new Array_(vertices.length * n), n, source.normalized)
  vertices.forEach((v, i) => {
    for (let k = 0; k < n; k++) out.setComponent(i, k, source.getComponent(v, k))
  })
  return out
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

/** Whether a mesh has an entry in the assignment, the meshes applyFaces needs fingerprinted. */
export function namedIn(faces: Faces | null | undefined): (identity: MeshIdentity) => boolean {
  const keys = new Set((faces?.meshes ?? []).map(keyOf))
  return (identity) => keys.has(keyOf(identity))
}

/**
 * Each mesh whole, or split by its assignment when one is found by identity
 * and its fingerprint matches. `stale` lists the assignments that did not
 * apply: the mesh changed, is gone, or the ranges overrun it.
 */
export function applyFaces(meshes: readonly OwnedMesh[], faces: Faces | null | undefined): { pieces: { mesh: OwnedMesh; slot: string | null; assigned: boolean; geometry: BufferGeometry }[]; stale: MeshIdentity[] } {
  const entries = faces?.meshes ?? []
  const byKey = new Map(entries.map((m) => [keyOf(m), m]))
  const used = new Set<FaceMesh>()
  const stale: MeshIdentity[] = []
  const pieces = meshes.flatMap((mesh) => {
    const base = slotOfMaterial(mesh.material)
    const entry = byKey.get(keyOf(mesh.identity))
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

/** A mesh of a new GLB as carryFaces reads it; slot is the GLB's own, topology '' where no entry could land. */
export type CarryTarget = { identity: MeshIdentity; topology: string; slot?: string | null }

/**
 * The assignment carried to a regenerated GLB (data: 模型内容「重新生成时」):
 * an entry stays when its identity is still there with the same fingerprint.
 * When a sibling of the same name, primitive and fingerprint is there too,
 * which is which cannot be proven; it stays only if the code is unchanged.
 * What is carried must still save: hints only for the slots kept, and an
 * entry that would take the slots past 32 with the new GLB's is dropped.
 */
export function carryFaces(old: Faces | null | undefined, next: readonly CarryTarget[], codeChanged: boolean): Carried {
  if (!old) return { faces: null, kept: [], dropped: [] }
  const byKey = new Map(next.map((m) => [keyOf(m.identity), m]))
  // Same parent path, node name, primitive and fingerprint: how many could pass for one another.
  const alike = (path: MeshIdentity['path'] | FaceMesh['path'], primitive: number, topology: string) => JSON.stringify([path.slice(0, -1), path[path.length - 1]?.[0], primitive, topology])
  const lookalikes = new Map<string, number>()
  for (const m of next) {
    const k = alike(m.identity.path, m.identity.primitive, m.topology)
    lookalikes.set(k, (lookalikes.get(k) ?? 0) + 1)
  }
  const slots = new Set(next.flatMap((m) => (m.slot ? [m.slot] : [])))
  const kept: FaceMesh[] = []
  const dropped: FaceMesh[] = []
  for (const entry of old.meshes) {
    const match = byKey.get(keyOf(entry))
    const twins = (lookalikes.get(alike(entry.path, entry.primitive, entry.topology)) ?? 0) > 1
    const adding = Object.keys(entry.assign).filter((s) => !slots.has(s))
    if (match && match.topology === entry.topology && !(twins && codeChanged) && slots.size + adding.length <= faceLimits.slots) {
      kept.push(entry)
      for (const s of adding) slots.add(s)
    } else dropped.push(entry)
  }
  const used = new Set(kept.flatMap((m) => Object.keys(m.assign)))
  const hints = Object.fromEntries(Object.entries(old.hints ?? {}).filter(([s]) => used.has(s)))
  const faces = kept.length ? { meshes: kept, ...(Object.keys(hints).length ? { hints } : {}) } : null
  return { faces, kept: countBySlot(kept), dropped: countBySlot(dropped) }
}

export type Carried = { faces: Faces | null; kept: SlotCount[]; dropped: SlotCount[] }

/**
 * A new GLB's meshes as carryFaces reads them, fingerprinted only where an
 * old entry could land: the same parent path, node name and primitive.
 */
export function carryTargets(identities: ReadonlyMap<Mesh, MeshIdentity>, old: Faces | null | undefined): CarryTarget[] {
  const near = (path: MeshIdentity['path'] | FaceMesh['path'], primitive: number) => JSON.stringify([path.slice(0, -1), path[path.length - 1]?.[0], primitive])
  const landing = new Set((old?.meshes ?? []).map((m) => near(m.path, m.primitive)))
  return [...identities].map(([mesh, identity]) => ({
    identity,
    topology: landing.has(near(identity.path, identity.primitive)) ? topologyOf(mesh.geometry) : '',
    slot: slotOfMaterial((Array.isArray(mesh.material) ? mesh.material[0] : mesh.material) as Material),
  }))
}

/** What changing the generated code means for carrying: a new hash or library version. */
export function codeDiffers(old: { code_sha256: string; lib_version: number } | null | undefined, next: { code_sha256: string; lib_version: number }): boolean {
  return !old || old.code_sha256 !== next.code_sha256 || old.lib_version !== next.lib_version
}

/**
 * carryFaces on a regenerated GLB. Without an old assignment nothing is
 * parsed; a GLB that does not parse (the sandbox already checked it) keeps
 * nothing.
 */
export async function carryOver(glb: ArrayBuffer, old: Faces | null | undefined, changed: boolean): Promise<Carried> {
  if (!old?.meshes.length) return { faces: null, kept: [], dropped: [] }
  try {
    const { identities } = await parseModelGLB(glb)
    return carryFaces(old, carryTargets(identities, old), changed)
  } catch {
    return { faces: null, kept: [], dropped: countBySlot(old.meshes) }
  }
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
 * those the assignment brings by name, as the server lists them (it stores
 * each mesh's slots sorted, so no order of first use survives a save), each
 * with the triangles assigned to it.
 */
export function effectiveSlots(glb: readonly { id: string; hint: string }[], faces: Faces | null | undefined): { id: string; hint: string; source: 'model' | 'faces'; triangles: number }[] {
  const counts = new Map(countBySlot(faces?.meshes ?? []).map((c) => [c.slot, c.triangles]))
  const out: { id: string; hint: string; source: 'model' | 'faces'; triangles: number }[] = glb.map((s) => ({ id: s.id, hint: s.hint, source: 'model', triangles: counts.get(s.id) ?? 0 }))
  for (const [slot, triangles] of [...counts].sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))) if (!glb.some((s) => s.id === slot)) out.push({ id: slot, hint: faces?.hints?.[slot] ?? '', source: 'faces', triangles })
  return out
}

/**
 * A builtin's geometry split by its assignment (data: 场景文档
 * 「primitive.faces」): one mesh at [["", 0]], primitive 0, its slot main.
 * pieces is null when there is nothing to apply; stale when the assignment no
 * longer fits the geometry (a check behind the topology declaration).
 */
export function applyBuiltinFaces(geometry: BufferGeometry, faces: Faces | null | undefined): { pieces: { slot: string; assigned: boolean; geometry: BufferGeometry }[] | null; stale: boolean } {
  const entry = faces?.meshes[0]
  if (!entry) return { pieces: null, stale: false }
  const assign = entry.assign as Assign
  if (JSON.stringify(entry.path) !== '[["",0]]' || entry.primitive !== 0 || entry.topology !== topologyOf(geometry) || !fits(assign, triangleCount(geometry))) return { pieces: null, stale: true }
  return { pieces: splitBySlot(geometry, assign, 'main').map((p) => ({ ...p, slot: p.slot ?? 'main' })), stale: false }
}
