import type { components } from '../../api/schema'
import catalog from '../builtinModels.gen.json' with { type: 'json' }
import type { SceneModel } from './cache.ts'
import { builtinKey, modelKey } from './key.ts'

// The built-in models (data: 内置模型库), generated from
// backend/internal/creativescene/models.json. A published entry never changes;
// its summary sits in the model table under builtin:<id> beside the account's
// revisions (models/key.ts).

export type BuiltinModel = {
  id: string
  label: string
  category: string
  mount: NonNullable<components['schemas']['CreativeAssetFromGLBPayload']['mount']>
  glb: { name: string; bytes: number }
  manifest: components['schemas']['CreativeModelManifest']
  rig?: components['schemas']['CreativeModelRig']
  /** What placing it writes into the instance's material_slots. */
  materials: components['schemas']['CreativeSceneMaterialSlots']
  source: { site: string; asset: string; author: string; license: string; url: string }
}

export const builtinModels = (catalog as unknown as { models: BuiltinModel[] }).models

export const builtinModel = (id: string) => builtinModels.find((m) => m.id === id)

/** Where a built-in model's file is read (api: 读取内置模型文件). */
export const builtinModelURL = (file: string) => `/api/v1/creative/scene-models/${encodeURIComponent(file)}`

/** Each entry's summary, as the server gives an account's model. */
export const builtinSummaries: Readonly<Record<string, SceneModel>> = Object.fromEntries(
  builtinModels.map((m): [string, SceneModel] => [
    builtinKey(m.id),
    { content_id: '', usable: true, manifest: m.manifest, generator: null, name_hint: m.label, faces: null, ...(m.rig ? { rig: m.rig } : {}) },
  ]),
)

/** The summary of the model a reference names: from the table, a built-in one from the catalog whatever the table holds. */
export function summaryOf(models: Readonly<Record<string, SceneModel>>, ref: Parameters<typeof modelKey>[0]): SceneModel | undefined {
  const key = modelKey(ref)
  return key === undefined ? undefined : (models[key] ?? builtinSummaries[key])
}
