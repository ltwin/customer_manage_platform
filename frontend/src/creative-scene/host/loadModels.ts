import type { Scene } from '../api.ts'
import { modelCache, type SceneModel } from '../models/cache.ts'
import { builtinModel, summaryOf } from '../models/catalog.ts'
import { builtinOf, modelKey } from '../models/key.ts'
import { shownIn } from '../objects/shadowBudget.ts'

// Kept apart from render.ts, which mounts ShotScene.tsx: the editor, the
// export and the unit tests load models without pulling the renderer in.

/**
 * Loads every model the scene names; an account's that fails draws as its
 * grey block. A built-in model whose file cannot be read (not imported yet)
 * fails the shot, naming it, when the shot shows it (AC-12): a grey block in
 * place of the body is not the picture asked for, and a retry later draws it.
 * Hidden, or in a hidden group, it is not in the picture and stops nothing.
 */
export async function loadModels(scene: Scene, models: Readonly<Record<string, SceneModel>>): Promise<void> {
  const keys = new Map(scene.objects.flatMap((o) => {
    const key = modelKey(o.model)
    const summary = summaryOf(models, o.model)
    return key && summary?.usable !== false ? [[key, summary] as const] : []
  }))
  const shown = shownIn(scene.objects)
  const drawn = new Set(scene.objects.flatMap((o) => (shown(o.id) ? [modelKey(o.model)] : [])))
  const loaded = await Promise.allSettled([...keys].map(([k, summary]) => modelCache.load(k, summary?.faces)))
  const missing = [...keys.keys()].flatMap((k, i) => {
    const id = builtinOf(k)
    return id !== null && drawn.has(k) && loaded[i]?.status === 'rejected' ? [builtinModel(id)?.label ?? id] : []
  })
  if (missing.length) throw new Error(`内置模型「${missing.join('、')}」的文件暂时读不到，请稍后再试`)
}
