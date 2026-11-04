import type { Scene, SceneObject } from '../api.ts'
import { lightDefaultsOf } from '../state/apply.ts'

// Which lights cast shadows on surfaces (C5c1 design「表面阴影上限」, a
// stopgap until shadows get their own change). A material takes at most 16
// textures; surfaces keep 8 for shadow maps. A directional light takes four
// (cascades), any other light one. The sky's sun goes first, then the lights
// casting shadows in object-list order, each while it fits: the camera does
// not enter, so moving the view, or a light, never changes which cast and
// never recompiles. A light left out lights the scene without shadows. The
// backend's read_scene says the same (creativescene/shadows.go).

/** Shadow maps surfaces may use. */
export const shadowMaps = 8

export type ShadowState = 'on' | 'off' | 'over_budget'

/** Whether an object is drawn: it and every group above it visible. */
function shownIn(objects: readonly SceneObject[]): (id: string) => boolean {
  const byID = new Map(objects.map((o) => [o.id, o]))
  const memo = new Map<string, boolean>()
  const shown = (id: string, depth: number): boolean => {
    const known = memo.get(id)
    if (known !== undefined) return known
    const o = byID.get(id)
    // A parent chain longer than the list is a loop, which the server rules out; not followed.
    const v = !!o && o.visible && (o.parent_id === null || (depth < objects.length && shown(o.parent_id, depth + 1)))
    memo.set(id, v)
    return v
  }
  return (id) => shown(id, 0)
}

/** Every light's state by object ID; a hidden light, or one in a hidden group, is not drawn and takes no map. */
export function selectShadowCasters(scene: Pick<Scene, 'environment' | 'objects'>): ReadonlyMap<string, ShadowState> {
  const shown = shownIn(scene.objects)
  const sky = scene.environment.sky.kind
  let used = sky === 'procedural' || sky === 'physical' ? 4 : 0
  const out = new Map<string, ShadowState>()
  for (const o of scene.objects) {
    const l = o.light
    if (!l) continue
    if (!(l.cast_shadow ?? lightDefaultsOf(l.type).cast_shadow) || !shown(o.id)) {
      out.set(o.id, 'off')
      continue
    }
    const cost = l.type === 'directional' ? 4 : 1
    if (used + cost > shadowMaps) {
      out.set(o.id, 'over_budget')
      continue
    }
    used += cost
    out.set(o.id, 'on')
  }
  return out
}
