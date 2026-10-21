import type { components } from '../api/schema'
import type { SceneDocument, SceneObject } from './api.ts'
import type { SceneModel } from './models/cache.ts'
import { parsePlaceholder } from './state/apply.ts'
import type { Intent } from './state/intents.ts'

// Saving a scene's code model into the asset library: which version, and
// whether it can be saved now. The canvas page holds the dialog; it imports
// only these types, never the editor (three.js stays out of its bundle).

/** An asset the drawer hands the editor to place. */
export type LibraryAsset = components['schemas']['CreativeLibraryAsset']

/** The model version a save dialog froze when it opened. */
export type SaveTarget = {
  objectID: string
  contentRevisionID: string
  /** The object's content unit as the editor showed it: the request's expected_object_rev. */
  unitRev: string
  /** The instance's materials in that version: the asset keeps them, the thumbnail shows them. */
  materials: NonNullable<SceneObject['material_slots']>
  /** The dialog's default title. */
  name: string
}

export type SaveModelRequest = {
  documentID: string
  target: SaveTarget
  /** Renders the target with its materials and stages the PNG; resolves to the upload's ID. */
  thumbnail(target: SaveTarget, signal: AbortSignal): Promise<string>
  /**
   * After a conflict: re-reads, waits until the editor shows the unit at
   * currentRev and checks the object again.
   */
  refresh(currentRev: string, signal: AbortSignal): Promise<SaveTarget | { error: string }>
}

export type SaveInput = {
  selected: readonly string[]
  objects: readonly SceneObject[]
  base: SceneDocument | null
  models: Readonly<Record<string, SceneModel>>
  /** Instances whose rebake after a parameter change is still running. */
  previews: Readonly<Record<string, string>>
  /** Unconfirmed edits: queued, and rejected or blocked ones still held. */
  unsaved: readonly Intent[]
}

export const contentUnit = (id: string) => `object:${id}:content`

/** What the selection can save, or the first reason it cannot (design「可存入判断」). */
export function saveTarget(input: SaveInput): SaveTarget | { error: string } {
  if (input.selected.length !== 1) return { error: '选一个模型' }
  const o = input.objects.find((x) => x.id === input.selected[0])
  if (!o) return { error: '这个物件已不在场景里' }
  const revision = o.kind === 'model_instance' ? o.model?.content_revision_id : undefined
  if (!revision) return { error: '只有代码模型能存入资产库' }
  if (parsePlaceholder(revision)) return { error: '模型还在生成' }
  if (input.previews[o.id]) return { error: '参数修改还在生成，稍后再存' }
  const summary = input.models[revision]
  if (!summary?.usable) return { error: '模型不可用' }
  const unit = contentUnit(o.id)
  const unitRev = input.base?.unit_revs[unit]
  if (!unitRev || input.unsaved.some((i) => unit in i.expected)) return { error: '正在保存，稍后再试' }
  return { objectID: o.id, contentRevisionID: revision, unitRev, materials: { ...o.material_slots }, name: summary.name_hint || o.name || '代码模型' }
}

type Watched = {
  getState(): { base: SceneDocument | null }
  subscribe(listener: () => void): () => void
}

/**
 * Resolves true once the session's snapshot shows the object's model at
 * currentRev or later, or without the object; false when aborted first.
 */
export function whenShown(store: Watched, objectID: string, currentRev: string, signal: AbortSignal): Promise<boolean> {
  return new Promise((resolve) => {
    const unit = contentUnit(objectID)
    const done = (shown: boolean) => {
      stop()
      signal.removeEventListener('abort', aborted)
      resolve(shown)
    }
    const aborted = () => done(false)
    const check = () => {
      const base = store.getState().base
      if (!base) return
      const gone = !base.scene.objects.some((o) => o.id === objectID)
      if (gone || Number(base.unit_revs[unit] ?? 0) >= Number(currentRev)) done(true)
    }
    const stop = store.subscribe(check)
    if (signal.aborted) return done(false)
    signal.addEventListener('abort', aborted)
    check()
  })
}
