import type { SceneObject } from '../api.ts'
import type { Fork } from '../modelEdits.ts'
import { faceLimits, trianglesOf, withAssigned, type Assign, type Faces, type FaceMesh } from '../models/faces.ts'
import type { WireOp } from '../state/apply.ts'
import { meshKey, type FaceSelection } from './state.ts'

// Giving the selected faces a slot (scene-face-materials design「面编辑与指定」):
// the next assignment, the slots' triangles for 「选择」, a new slot's name.
// Pure: what is sent is the caller's (SceneEditor.tsx assignFaces).

/** What assigning reads of a mesh: where it is, its fingerprint now, its own slot in the GLB. */
export type AssignMesh = { key: string; topology: string; slot: string | null; shape(): { triangleCount: number } }

export const tooFragmented = '面分配太碎，请在模型代码里划分槽位'

/** The assignment entry that still fits a mesh: same place, same fingerprint. */
function entryOf(faces: Faces | null | undefined, mesh: AssignMesh): FaceMesh | undefined {
  return faces?.meshes.find((m) => meshKey({ path: m.path as [string, number][], primitive: m.primitive }) === mesh.key && m.topology === mesh.topology)
}

/**
 * The triangles a slot has now, by mesh: those assigned to it, and on a
 * mesh whose own slot it is, every triangle not assigned elsewhere.
 */
export function slotFaces(faces: Faces | null | undefined, meshes: readonly AssignMesh[], slot: string): FaceSelection {
  const out: Record<string, number[]> = {}
  for (const m of meshes) {
    const assign = (entryOf(faces, m)?.assign ?? {}) as Assign
    const mine = trianglesOf(assign[slot] ?? [])
    if (m.slot === slot) {
      const elsewhere = new Set(Object.entries(assign).flatMap(([s, r]) => (s === slot ? [] : trianglesOf(r))))
      for (let t = 0; t < m.shape().triangleCount; t++) if (!elsewhere.has(t)) mine.push(t)
    }
    if (mine.length) out[m.key] = [...new Set(mine)].sort((a, b) => a - b)
  }
  return out
}

/**
 * The assignment with the selected triangles moved to `slot`; moved back
 * out of the assignment on a mesh whose own slot it is. An entry that no
 * longer fits its mesh is replaced, other entries stay as they are. Null
 * when nothing is left; an error past the limits, and nothing is sent.
 */
export function assignFaces(
  faces: Faces | null | undefined,
  meshes: readonly AssignMesh[],
  selected: FaceSelection,
  slot: string,
  context: { builtin: boolean; glbSlots: readonly string[] },
): { faces: Faces | null } | { error: string } {
  const out = new Map<string, FaceMesh>()
  for (const m of faces?.meshes ?? []) out.set(meshKey({ path: m.path as [string, number][], primitive: m.primitive }), m)
  for (const m of meshes) {
    const triangles = selected[m.key]
    if (!triangles?.length) continue
    const was = entryOf(faces, m)
    const assign = withAssigned((was?.assign ?? {}) as Assign, triangles, m.slot === slot ? null : slot)
    const at = JSON.parse(m.key) as [FaceMesh['path'], number]
    if (Object.keys(assign).length) out.set(m.key, { path: at[0], primitive: at[1], topology: m.topology, assign })
    else out.delete(m.key)
  }
  if (!out.size) return { faces: null }
  const entries = [...out.values()]
  const used = new Set(entries.flatMap((m) => Object.keys(m.assign)))
  const hints = Object.fromEntries(Object.entries(faces?.hints ?? {}).filter(([s]) => used.has(s)))
  const next: Faces = { meshes: entries, ...(Object.keys(hints).length ? { hints } : {}) }

  const ranges = entries.reduce((n, m) => n + Object.values(m.assign).reduce((k, r) => k + r.length, 0), 0)
  if (ranges > (context.builtin ? faceLimits.builtinRanges : faceLimits.ranges)) return { error: tooFragmented }
  if (new TextEncoder().encode(JSON.stringify(next)).length > faceLimits.bytes) return { error: tooFragmented }
  if (new Set([...context.glbSlots, ...used]).size > faceLimits.slots) return { error: `槽位太多：一个物件最多 ${faceLimits.slots} 个` }
  return { faces: next }
}

/** 「+」: face_<n>, the smallest n no slot has. */
export function newSlotName(taken: Iterable<string>): string {
  const names = new Set(taken)
  let n = 1
  while (names.has(`face_${n}`)) n++
  return `face_${n}`
}

/** The ref the derived model goes by within its batch. */
export const derivedRef = 'faces'

/**
 * One edit, one revision, one undo step: a builtin's faces replaced, or a
 * model revision derived over the same GLB with the new assignment and the
 * instances switched to it as the fork (modelEdits.ts forkPlan) says.
 */
export function assignOps(object: SceneObject, faces: Faces | null, fork: Fork): WireOp[] {
  if (object.kind === 'primitive') return [{ op: 'update_objects', updates: [{ object_id: object.id, primitive: { faces } }] }]
  const from = object.model?.content_revision_id
  if (!from) return []
  return [
    { op: 'derive_model', ref: derivedRef, from_revision_id: from, faces, ...(fork.baseContentID ? { base_content_id: fork.baseContentID } : {}) },
    {
      op: 'set_instance_model',
      object_id: object.id,
      model: { ref: derivedRef },
      scope: fork.scope,
      ...(fork.scope === 'all_instances' ? { object_ids: fork.objectIDs } : {}),
    },
  ]
}
