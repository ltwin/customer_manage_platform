import { useEffect, useMemo, useState } from 'react'
import type { SceneObject } from '../api.ts'
import { primitiveGeometry } from '../builtins/geometry.ts'
import type { ModelCache } from '../models/cache.ts'
import { builtinEditMesh, modelEditMeshes, type EditMesh } from './meshes.ts'
import { faceSource, type FaceEdit } from './state.ts'

export type FaceMeshes = { state: 'loading' } | { state: 'ready'; meshes: readonly EditMesh[] } | { state: 'failed'; message: string }

/**
 * The meshes of the object in face edit, for its current source: a builtin's
 * at once, a model's once its GLB is parsed again (ModelCache.owned).
 */
export function useFaceMeshes(edit: FaceEdit | null, object: SceneObject | undefined, cache: ModelCache): FaceMeshes | null {
  const source = edit && object?.id === edit.objectID ? faceSource(object) : null
  const builtin = source && object?.kind === 'primitive' ? object.primitive : undefined
  const revision = source && object?.kind === 'model_instance' ? object.model?.content_revision_id : undefined
  const fixed = useMemo<FaceMeshes | null>(() => {
    if (!builtin) return null
    const geometry = primitiveGeometry(builtin.builtin, builtin.params)
    return geometry ? { state: 'ready', meshes: [builtinEditMesh(geometry)] } : { state: 'failed', message: '这个基础体没有几何' }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- source stands for the builtin and its parameters
  }, [source])
  const [loaded, setLoaded] = useState<{ revision: string; meshes: FaceMeshes } | null>(null)
  useEffect(() => {
    if (!revision) return
    let live = true
    cache.owned(revision).then(
      (owned) => live && setLoaded({ revision, meshes: { state: 'ready', meshes: modelEditMeshes(owned) } }),
      () => live && setLoaded({ revision, meshes: { state: 'failed', message: '模型读取失败，不能面编辑' } }),
    )
    return () => {
      live = false
    }
  }, [cache, revision])
  if (!source) return null
  if (fixed) return fixed
  return loaded && loaded.revision === revision ? loaded.meshes : { state: 'loading' }
}
