// The light reference scenes (scripts/fixtures/scene-reference) as the e2e's server gives them
// (creative-scene.e2e.mjs referenceScene), for the light size pages: the scene, and its models registered.
import type { Scene } from '../../src/creative-scene/api.ts'
import { modelCache, type SceneModel } from '../../src/creative-scene/models/cache.ts'

export const fixtures = '/scripts/fixtures/scene-reference'

/** A reference scene with the exposure fields the server completes, its cameras exposed by hand. */
export async function readFixture(name: string): Promise<Scene> {
  const scene = (await (await fetch(`${fixtures}/${name}.json`)).json()) as Scene
  scene.render.exposure_ev100 ??= 15
  scene.render.exposure_mode ??= 'ambient'
  scene.render.exposure_compensation ??= 0
  for (const o of scene.objects) if (o.camera) Object.assign(o.camera, { exposure_mode: 'manual', exposure_compensation: 0, ...o.camera })
  return scene
}

/** The scene's models, fetched and registered in the model cache. */
export async function loadModels(name: string, scene: Scene): Promise<Record<string, SceneModel>> {
  const models: Record<string, SceneModel> = {}
  for (const rev of new Set(scene.objects.flatMap((o) => (o.model?.content_revision_id ? [o.model.content_revision_id] : [])))) {
    const meta = (await (await fetch(`${fixtures}/${name}/${rev}.json`)).json()) as Omit<SceneModel, 'content_id' | 'usable'>
    const summary = { content_id: `ccnt_${rev}`, usable: true, ...meta } as SceneModel
    models[rev] = summary
    modelCache.register(rev, { glb: await (await fetch(`${fixtures}/${name}/${rev}.glb`)).arrayBuffer(), summary })
  }
  await Promise.all(Object.keys(models).map((r) => modelCache.load(r)))
  return models
}
