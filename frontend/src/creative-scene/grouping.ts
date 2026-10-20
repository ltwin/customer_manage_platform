import { Matrix4, Vector3 } from 'three'
import type { SceneObject } from './api.ts'
import { objectsLabel } from './editing.ts'
import { newObjectID } from './ids.ts'
import { topmost, type Selection } from './input/selection.ts'
import { subtree, type WireOp } from './state/apply.ts'
import { localUnder, worldOf } from './state/world.ts'

// Grouping and parenting (Ctrl+G, Ctrl+Alt+G, Ctrl+P, Alt+P, dragging rows in
// the object list). Each keeps world positions and is one reparent_objects:
// one revision, one undo step. The inverse is worked out from the same
// snapshot and kept in the undo step.

export type ReparentOp = Extract<WireOp, { op: 'reparent_objects' }>
export type Regroup = { op: WireOp; inverse: ReparentOp | null; label: string }
/** An edit, a reason it cannot be done, or null when there is nothing to do. */
export type GroupingResult = Regroup | { error: string } | null

type Placement = { id: string; parent: string | null; order: number }

const cannotKeep = '无法在保持位置的前提下改挂'

/** Children of parent (null for the root) in list order. */
function childrenOf(objects: readonly SceneObject[], parent: string | null): SceneObject[] {
  return objects.filter((o) => (o.parent_id ?? null) === parent).sort((a, b) => a.order - b.order)
}

/** Every object's position in the object list: depth first, by order. */
function listIndex(objects: readonly SceneObject[]): Map<string, number> {
  const out = new Map<string, number>()
  const walk = (parent: string | null) => {
    for (const o of childrenOf(objects, parent)) {
      if (out.has(o.id)) continue
      out.set(o.id, out.size)
      walk(o.id)
    }
  }
  walk(null)
  return out
}

function inListOrder(objects: readonly SceneObject[], ids: Iterable<string>): string[] {
  const index = listIndex(objects)
  return [...ids].sort((a, b) => (index.get(a) ?? 0) - (index.get(b) ?? 0))
}

/**
 * Orders for a final sibling sequence: each object keeps its order while it
 * still comes after the one before, otherwise takes the next number, so the
 * siblings after an insertion move up only as far as they must. Objects
 * arriving from another parent, and the ones being placed, always take the
 * next number.
 */
function sequence(objects: readonly SceneObject[], parent: string | null, ids: readonly string[], placing: ReadonlySet<string> = new Set()): Placement[] {
  const byID = new Map(objects.map((o) => [o.id, o]))
  let prev = -1
  return ids.map((id) => {
    const o = byID.get(id)
    const stays = o !== undefined && (o.parent_id ?? null) === parent && !placing.has(id)
    const order = stays && o.order > prev ? o.order : prev + 1
    prev = order
    return { id, parent, order }
  })
}

/**
 * The forward and inverse reparent_objects for final placements. Objects
 * whose parent changes get the local transform that keeps them where they
 * are; ones that only change order carry none. Null when nothing changes,
 * false when an object cannot keep its world position under its new parent.
 */
function build(
  objects: readonly SceneObject[],
  create: readonly SceneObject[],
  placements: readonly Placement[],
  remove: readonly string[],
): { op: ReparentOp; inverse: ReparentOp } | null | false {
  const byID = new Map(objects.map((o) => [o.id, o]))
  const world = worldOf(objects, create)
  const moves: ReparentOp['moves'] = []
  const back: ReparentOp['moves'] = []
  for (const p of placements) {
    const o = byID.get(p.id)
    if (!o) continue
    const parent = o.parent_id ?? null
    if (parent === p.parent && o.order === p.order) continue
    if (parent === p.parent) {
      moves.push({ object_id: o.id, parent_id: p.parent, order: p.order })
      back.push({ object_id: o.id, parent_id: parent, order: o.order })
      continue
    }
    const transform = localUnder(world(p.parent), world(o.id))
    if (!transform) return false
    moves.push({ object_id: o.id, parent_id: p.parent, order: p.order, transform })
    back.push({ object_id: o.id, parent_id: parent, order: o.order, transform: o.transform })
  }
  if (!moves.length) return null
  const op: ReparentOp = { op: 'reparent_objects', moves }
  const inverse: ReparentOp = { op: 'reparent_objects', moves: back }
  if (create.length) {
    op.create_groups = [...create]
    inverse.remove_groups = create.map((g) => g.id)
  }
  if (remove.length) {
    op.remove_groups = [...remove]
    inverse.create_groups = remove.map((id) => byID.get(id)!)
  }
  return { op, inverse }
}

/** 「分组 N」 after the highest N already used. */
function groupName(objects: readonly SceneObject[]): string {
  let n = 0
  for (const o of objects) {
    const m = /^分组 (\d+)$/.exec(o.name)
    if (m) n = Math.max(n, Number(m[1]))
  }
  return `分组 ${n + 1}`
}

/**
 * Ctrl+G: a new group around the topmost selected objects, centred on them,
 * unrotated. It goes under their common parent where the first of them was,
 * or last at the root when their parents differ; they keep their list order
 * inside it.
 */
export function groupSelection(objects: readonly SceneObject[], sel: Selection, makeID: () => string = newObjectID): GroupingResult {
  const roots = inListOrder(objects, topmost(sel.selected, objects))
  if (!roots.length) return null
  const byID = new Map(objects.map((o) => [o.id, o]))
  const parents = new Set(roots.map((id) => byID.get(id)!.parent_id ?? null))
  const common = parents.size === 1 ? [...parents][0] : null
  const world = worldOf(objects)
  const centre = new Vector3()
  for (const id of roots) centre.add(new Vector3().setFromMatrixPosition(world(id)))
  centre.divideScalar(roots.length)
  const local = localUnder(world(common), new Matrix4().makeTranslation(centre))
  if (!local) return { error: cannotKeep }
  const group: SceneObject = {
    id: makeID(),
    kind: 'group',
    name: groupName(objects),
    parent_id: common,
    order: 0,
    transform: { position: local.position, rotation: [0, 0, 0, 1], scale: [1, 1, 1] },
    visible: true,
    locked: false,
  }
  const leaving = new Set(roots)
  const siblings = childrenOf(objects, common).map((o) => o.id)
  let ids: string[]
  if (parents.size === 1) {
    const at = siblings.findIndex((id) => leaving.has(id))
    ids = siblings.filter((id) => !leaving.has(id))
    ids.splice(siblings.slice(0, at).filter((id) => !leaving.has(id)).length, 0, group.id)
  } else {
    ids = [...siblings.filter((id) => !leaving.has(id)), group.id]
  }
  const placed = sequence(objects, common, ids)
  group.order = placed.find((p) => p.id === group.id)!.order
  const placements = [...placed.filter((p) => p.id !== group.id), ...roots.map((id, i) => ({ id, parent: group.id, order: i }))]
  const built = build(objects, [group], placements, [])
  if (built === false) return { error: cannotKeep }
  if (!built) return null
  return { ...built, label: objectsLabel('编组', roots.length, byID.get(roots[0])?.name) }
}

/**
 * Ctrl+Alt+G: the selected groups go away and their children take their
 * place under the nearest parent that stays, in their order; later siblings
 * move up. Groups without children are simply deleted.
 */
export function ungroupSelection(objects: readonly SceneObject[], sel: Selection): GroupingResult {
  const byID = new Map(objects.map((o) => [o.id, o]))
  const groups = sel.selected.filter((id) => byID.get(id)?.kind === 'group')
  if (!groups.length) return sel.selected.length ? { error: '选中的不是分组' } : null
  const removed = new Set(groups)
  const label = objectsLabel('取消编组', groups.length, byID.get(groups[0])?.name)
  if (!objects.some((o) => o.parent_id && removed.has(o.parent_id))) return { op: { op: 'remove_objects', object_ids: groups }, inverse: null, label }
  const expand = (parent: string | null): string[] =>
    childrenOf(objects, parent).flatMap((o) => (removed.has(o.id) ? expand(o.id) : [o.id]))
  const keeps = (id: string | null): string | null => {
    let p = id
    while (p && removed.has(p)) p = byID.get(p)?.parent_id ?? null
    return p
  }
  const parents = new Set(groups.map((id) => keeps(byID.get(id)!.parent_id ?? null)))
  const placements = [...parents].flatMap((parent) => sequence(objects, parent, expand(parent)))
  const built = build(objects, [], placements, inListOrder(objects, groups))
  if (built === false) return { error: cannotKeep }
  if (!built) return null
  return { ...built, label }
}

/** Ctrl+P: the other selected objects go under the active one, after its children. */
export function parentToActive(objects: readonly SceneObject[], sel: Selection): GroupingResult {
  const byID = new Map(objects.map((o) => [o.id, o]))
  const target = sel.active ? byID.get(sel.active) : undefined
  if (!target) return null
  const roots = topmost(sel.selected.filter((id) => id !== target.id), objects).filter((id) => byID.get(id)?.parent_id !== target.id)
  if (!roots.length) return null
  if (target.kind !== 'group') return { error: '只能挂到分组或空物体下' }
  if (subtree(objects, roots).has(target.id)) return { error: '不能挂到自己的子物件下' }
  const last = childrenOf(objects, target.id).at(-1)?.order ?? -1
  const placements = inListOrder(objects, roots).map((id, i) => ({ id, parent: target.id, order: last + 1 + i }))
  const built = build(objects, [], placements, [])
  if (built === false) return { error: cannotKeep }
  if (!built) return null
  return { ...built, label: objectsLabel('设置父级', roots.length, byID.get(roots[0])?.name) }
}

/** Alt+P: selected objects that have a parent go last at the root. */
export function clearParent(objects: readonly SceneObject[], sel: Selection): GroupingResult {
  const byID = new Map(objects.map((o) => [o.id, o]))
  const ids = inListOrder(objects, sel.selected.filter((id) => byID.get(id)?.parent_id))
  if (!ids.length) return null
  const last = childrenOf(objects, null).at(-1)?.order ?? -1
  const built = build(objects, [], ids.map((id, i) => ({ id, parent: null, order: last + 1 + i })), [])
  if (built === false) return { error: cannotKeep }
  if (!built) return null
  return { ...built, label: objectsLabel('清除父级', ids.length, byID.get(ids[0])?.name) }
}

export type DropPlace = 'before' | 'after' | 'inside'

/** What a drag of row `dragged` moves: the selection when the row is in it. */
export function draggedObjects(objects: readonly SceneObject[], sel: Selection, dragged: string): string[] {
  return sel.selected.includes(dragged) ? inListOrder(objects, topmost(sel.selected, objects)) : [dragged]
}

/** Whether rows can be dropped there: never onto themselves or their descendants, inside only a group. */
export function canDrop(objects: readonly SceneObject[], moving: readonly string[], target: string, place: DropPlace): boolean {
  const t = objects.find((o) => o.id === target)
  if (!t || subtree(objects, moving).has(target)) return false
  return place !== 'inside' || t.kind === 'group'
}

/** Dropping rows before, after or inside a row of the object list. */
export function dropRows(objects: readonly SceneObject[], moving: readonly string[], target: string, place: DropPlace): GroupingResult {
  if (!moving.length || !canDrop(objects, moving, target, place)) return null
  const byID = new Map(objects.map((o) => [o.id, o]))
  const t = byID.get(target)!
  const parent = place === 'inside' ? t.id : (t.parent_id ?? null)
  const going = new Set(moving)
  const ids = childrenOf(objects, parent)
    .map((o) => o.id)
    .filter((id) => !going.has(id))
  const at = place === 'inside' ? ids.length : ids.indexOf(target) + (place === 'after' ? 1 : 0)
  ids.splice(at, 0, ...moving)
  const built = build(objects, [], sequence(objects, parent, ids, going), [])
  if (built === false) return { error: cannotKeep }
  if (!built) return null
  return { ...built, label: objectsLabel('移动', moving.length, byID.get(moving[0])?.name) }
}
