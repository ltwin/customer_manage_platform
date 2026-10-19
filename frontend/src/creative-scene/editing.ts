import { Matrix4, Quaternion, Vector3 } from 'three'
import type { SceneObject, SceneTransform } from './api.ts'
import { builtinSpec } from './builtins/catalog.ts'
import { newObjectID } from './ids.ts'
import { sameValue, subtree, type WireOp } from './state/apply.ts'

// The edits behind menu items and shortcuts, each one intent's operations.

const identity: SceneTransform = { position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] }

/** The next free sibling order under a parent (null for the root). */
export function nextOrder(objects: readonly SceneObject[], parent: string | null): number {
  let max = -1
  for (const o of objects) if ((o.parent_id ?? null) === parent) max = Math.max(max, o.order)
  return max + 1
}

export const groundBuiltin = 'ground@1'

/**
 * A new primitive at a point on the ground, last among root objects. A
 * ground goes to the origin locked, like the one a new scene starts with,
 * so clicks keep reaching what stands on it.
 */
export function newPrimitive(
  builtin: string,
  position: [number, number, number],
  objects: readonly SceneObject[],
  makeID: () => string = newObjectID,
): SceneObject {
  const ground = builtin === groundBuiltin
  return {
    id: makeID(),
    kind: 'primitive',
    name: builtinSpec(builtin)?.label ?? '',
    parent_id: null,
    order: nextOrder(objects, null),
    transform: { ...identity, position: ground ? [0, 0, 0] : position },
    visible: true,
    locked: ground,
    primitive: { builtin },
  }
}

/**
 * Copies of the given objects with everything under them, under new IDs.
 * Each copy root goes last under the original's parent; descendants keep
 * their place inside the copy.
 */
export function duplicate(
  objects: readonly SceneObject[],
  ids: readonly string[],
  makeID: () => string = newObjectID,
): SceneObject[] {
  const copied = subtree(objects, ids)
  const fresh = new Map<string, string>()
  for (const o of objects) if (copied.has(o.id)) fresh.set(o.id, makeID())
  const taken = new Map<string, number>()
  const out: SceneObject[] = []
  for (const o of objects) {
    if (!copied.has(o.id)) continue
    const parent = o.parent_id ?? null
    const inside = parent !== null && copied.has(parent)
    let order = o.order
    if (!inside) {
      const key = parent ?? ''
      order = taken.get(key) ?? nextOrder(objects, parent)
      taken.set(key, order + 1)
    }
    out.push({
      ...structuredClone(o),
      id: fresh.get(o.id)!,
      parent_id: inside ? fresh.get(parent!)! : parent,
      order,
    })
  }
  return out
}

/** H hides the selection, Shift+H everything else, Alt+H shows all. */
export function visibilityOp(
  objects: readonly SceneObject[],
  selected: readonly string[],
  mode: 'hide' | 'hide-unselected' | 'reveal',
): WireOp | null {
  const chosen = new Set(selected)
  const targets = objects.filter((o) =>
    mode === 'hide' ? chosen.has(o.id) && o.visible : mode === 'hide-unselected' ? !chosen.has(o.id) && o.visible && !o.locked : !o.visible,
  )
  if (!targets.length) return null
  return { op: 'update_objects', updates: targets.map((o) => ({ object_id: o.id, visible: mode === 'reveal' })) }
}

/**
 * Alt+G / R / S puts location, rotation or scale back to the default.
 * Objects already there are left out, so the label counts what changes.
 */
export function clearOp(
  objects: readonly SceneObject[],
  ids: readonly string[],
  what: 'location' | 'rotation' | 'scale',
): WireOp | null {
  const byID = new Map(objects.map((o) => [o.id, o]))
  const updates = ids.flatMap((id) => {
    const o = byID.get(id)
    if (!o) return []
    const t = o.transform
    const transform =
      what === 'location'
        ? { ...t, position: [0, 0, 0] }
        : what === 'rotation'
          ? { ...t, rotation: [0, 0, 0, 1] }
          : { ...t, scale: [1, 1, 1] }
    return sameValue(transform, t) ? [] : [{ object_id: o.id, transform }]
  })
  return updates.length ? { op: 'update_objects', updates } : null
}

/** One confirmed transform of any number of objects: one update. */
export function transformOp(results: readonly { id: string; position: number[]; rotation: number[]; scale: number[] }[]): WireOp {
  return {
    op: 'update_objects',
    updates: results.map((r) => ({ object_id: r.id, transform: { position: r.position, rotation: r.rotation, scale: r.scale } })),
  }
}

/** How a prompt names an edit of n objects, for example 「移动 3 个物件」. */
export function objectsLabel(verb: string, n: number, name?: string): string {
  return n === 1 && name ? `${verb}「${name}」` : `${verb} ${n} 个物件`
}

/**
 * A camera standing where the viewport looks from, facing the same way, with
 * the server's defaults for its lens. Named 「摄像机 N」 after the cameras
 * already there.
 */
export function newCamera(pose: { position: number[]; rotation: number[] }, objects: readonly SceneObject[], makeID: () => string = newObjectID): SceneObject {
  const n = objects.filter((o) => o.kind === 'camera').length + 1
  return {
    id: makeID(),
    kind: 'camera',
    name: `摄像机 ${n}`,
    parent_id: null,
    order: nextOrder(objects, null),
    transform: { position: pose.position, rotation: pose.rotation, scale: [1, 1, 1] },
    visible: true,
    locked: false,
    camera: {},
  }
}

/** An empty (空物体): a group without children, to focus on or to parent things to. */
export function newEmpty(position: [number, number, number], objects: readonly SceneObject[], makeID: () => string = newObjectID): SceneObject {
  const parents = new Set(objects.map((o) => o.parent_id).filter(Boolean))
  const n = objects.filter((o) => o.kind === 'group' && !parents.has(o.id)).length + 1
  return {
    id: makeID(),
    kind: 'group',
    name: `空物体 ${n}`,
    parent_id: null,
    order: nextOrder(objects, null),
    transform: { ...identity, position },
    visible: true,
    locked: false,
  }
}

/**
 * A world pose (document space) as the local transform of an object under
 * `parent`, keeping `scale`: Ctrl+Alt+0 on a camera inside a group.
 */
export function localPose(objects: readonly SceneObject[], parent: string | null, pose: { position: number[]; rotation: number[] }, scale: number[]): SceneTransform {
  const byID = new Map(objects.map((o) => [o.id, o]))
  const world = new Matrix4()
  const chain: SceneObject[] = []
  for (let at = parent ? byID.get(parent) : undefined; at && chain.length <= objects.length; at = at.parent_id ? byID.get(at.parent_id) : undefined) chain.unshift(at)
  for (const o of chain) {
    const t = o.transform
    world.multiply(new Matrix4().compose(new Vector3().fromArray(t.position), new Quaternion().fromArray(t.rotation), new Vector3().fromArray(t.scale)))
  }
  const local = world.invert().multiply(new Matrix4().compose(new Vector3().fromArray(pose.position), new Quaternion().fromArray(pose.rotation), new Vector3(1, 1, 1)))
  const position = new Vector3()
  const rotation = new Quaternion()
  local.decompose(position, rotation, new Vector3())
  rotation.normalize()
  return { position: position.toArray(), rotation: rotation.toArray(), scale }
}
