import type { SceneObject } from '../api.ts'
import type { FaceTopology } from '../models/faceTopology.ts'
import type { MeshIdentity } from '../sandbox/gltf.ts'

// Face edit (scene-face-materials design「面编辑与指定」): one object's
// triangles picked by flat patch, for assigning to a material slot. The
// selection is UI state, by mesh and triangle number; it never reaches the
// document. Pure: no React, no three.js scene.

/** Selected triangles by mesh (meshKey), ascending. */
export type FaceSelection = Readonly<Record<string, readonly number[]>>

export type FaceEdit = {
  objectID: string
  /** What the triangles were numbered on (faceSource); when it changes the selection no longer means anything. */
  source: string
  selected: FaceSelection
}

export const meshKey = (identity: MeshIdentity): string => JSON.stringify([identity.path, identity.primitive])

/** The object's triangles as they are now: a model revision, or a builtin and its parameters. */
export function faceSource(o: SceneObject): string | null {
  if (o.kind === 'model_instance') return o.model?.content_revision_id ?? null
  if (o.kind === 'primitive' && o.primitive) return JSON.stringify([o.primitive.builtin, o.primitive.params ?? {}])
  return null
}

/** Why Tab does not enter face edit on this object, or null when it does. */
export function faceEditBlock(o: SceneObject | undefined, usable: boolean): string | null {
  if (!o) return '先选中一个模型实例或基础体，再按 Tab 进入面编辑'
  if (o.kind !== 'model_instance' && o.kind !== 'primitive') return '面编辑只用于模型实例和基础体'
  if (o.locked) return '物件已锁定，解锁后才能面编辑'
  if (!o.visible) return '物件已隐藏，显示后才能面编辑'
  if (!faceSource(o) || !usable) return '模型不可用，不能面编辑'
  return null
}

export function selectedCount(sel: FaceSelection): number {
  return Object.values(sel).reduce((n, t) => n + t.length, 0)
}

/**
 * A click in face edit, Blender's way: a patch replaces the selection,
 * Shift adds it or, when it is all selected already, takes it out. A click
 * on nothing clears, with Shift keeps.
 */
export function clickPatch(sel: FaceSelection, key: string | null, patch: readonly number[], shift: boolean): FaceSelection {
  if (key === null) return shift ? sel : {}
  if (!shift) return { [key]: sorted(patch) }
  const had = new Set(sel[key] ?? [])
  const all = patch.every((t) => had.has(t))
  for (const t of patch) {
    if (all) had.delete(t)
    else had.add(t)
  }
  const { [key]: _, ...rest } = sel
  return had.size ? { ...rest, [key]: sorted(had) } : rest
}

const sorted = (triangles: Iterable<number>) => [...new Set(triangles)].sort((a, b) => a - b)

/** What the selection commands read of a mesh: its key and patches, pieces and neighbours. */
export type SelectMesh = { key: string; shape(): FaceTopology }

export type BoxMode = 'set' | 'add' | 'subtract'

/** A: every triangle of the object. */
export function selectAllFaces(meshes: readonly SelectMesh[]): FaceSelection {
  return byMesh(meshes, (m) => m.shape().patches.flat())
}

/** Ctrl+I: what was not selected. */
export function invertFaces(sel: FaceSelection, meshes: readonly SelectMesh[]): FaceSelection {
  return byMesh(meshes, (m) => {
    const had = new Set(sel[m.key] ?? [])
    return range(m.shape().triangleCount).filter((t) => !had.has(t))
  })
}

/** L: the piece under the pointer, added. Pieces join across shared edges (models/faceTopology.ts). */
export function linkedPick(sel: FaceSelection, mesh: SelectMesh, triangle: number): FaceSelection {
  const shape = mesh.shape()
  const piece = shape.componentOf[triangle]
  return withMesh(sel, mesh.key, [...(sel[mesh.key] ?? []), ...range(shape.triangleCount).filter((t) => shape.componentOf[t] === piece)])
}

/** Ctrl+L: the whole of every piece something is selected on. */
export function selectLinked(sel: FaceSelection, meshes: readonly SelectMesh[]): FaceSelection {
  return byMesh(meshes, (m) => {
    const shape = m.shape()
    const pieces = new Set((sel[m.key] ?? []).map((t) => shape.componentOf[t]))
    return range(shape.triangleCount).filter((t) => pieces.has(shape.componentOf[t]))
  })
}

/**
 * Ctrl+numpad +: the patches next to the selection, across an edge, added.
 * Patches, not triangles, as a click picks them.
 */
export function growFaces(sel: FaceSelection, meshes: readonly SelectMesh[]): FaceSelection {
  return byMesh(meshes, (m) => {
    const had = sel[m.key] ?? []
    const shape = m.shape()
    const patches = new Set<number>()
    for (const t of had) for (const u of shape.neighbours(t)) patches.add(shape.patchOf[u])
    return [...had, ...[...patches].flatMap((p) => shape.patches[p])]
  })
}

/** Ctrl+numpad −: the patches on the selection's edge, next to something unselected, taken out. */
export function shrinkFaces(sel: FaceSelection, meshes: readonly SelectMesh[]): FaceSelection {
  return byMesh(meshes, (m) => {
    const had = new Set(sel[m.key] ?? [])
    const shape = m.shape()
    const edge = new Set<number>()
    for (const t of had) if (shape.neighbours(t).some((u) => !had.has(u))) edge.add(shape.patchOf[t])
    return [...had].filter((t) => !edge.has(shape.patchOf[t]))
  })
}

/**
 * A box drag: the patches of the triangles it caught (faceEdit/meshes.ts
 * boxTriangles), replacing the selection, added (Shift) or taken out (Ctrl).
 */
export function boxFaces(sel: FaceSelection, caught: Readonly<Record<string, readonly number[]>>, meshes: readonly SelectMesh[], mode: BoxMode): FaceSelection {
  const patched = byMesh(meshes, (m) => {
    const shape = m.shape()
    return [...new Set((caught[m.key] ?? []).map((t) => shape.patchOf[t]))].flatMap((p) => shape.patches[p])
  })
  if (mode === 'set') return patched
  return byMesh(meshes, (m) => {
    const had = sel[m.key] ?? []
    if (mode === 'add') return [...had, ...(patched[m.key] ?? [])]
    const out = new Set(patched[m.key] ?? [])
    return had.filter((t) => !out.has(t))
  })
}

const range = (n: number) => Array.from({ length: n }, (_, i) => i)

function byMesh(meshes: readonly SelectMesh[], triangles: (m: SelectMesh) => readonly number[]): FaceSelection {
  const out: Record<string, readonly number[]> = {}
  for (const m of meshes) {
    const t = triangles(m)
    if (t.length) out[m.key] = sorted(t)
  }
  return out
}

function withMesh(sel: FaceSelection, key: string, triangles: readonly number[]): FaceSelection {
  const { [key]: _, ...rest } = sel
  return triangles.length ? { ...rest, [key]: sorted(triangles) } : rest
}
