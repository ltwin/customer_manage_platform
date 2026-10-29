import type { SceneObject } from '../api.ts'
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
