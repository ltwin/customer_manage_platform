import type {
  ObjectUpdate,
  Scene,
  SceneEnvironment,
  SceneObject,
  SceneOperation,
  SceneRender,
} from '../api.ts'
import { builtinSpec, defaultParams } from '../builtins/catalog.ts'

// The client's copy of the server's operation semantics, used only to show
// edits before their receipt. Anything the server would reject is skipped
// here; the receipt or the next read decides.

export type RenameOp = { op: 'rename'; title: string }
export type RestoreUnitsOp = {
  op: 'restore_units'
  from_revision: string
  units: string[]
}
export type WireOp = SceneOperation | RenameOp | RestoreUnitsOp

export type SceneState = { title: string; scene: Scene }

/**
 * What an undo or redo shows before its receipt, taken from the step it
 * reverses: objects that come back whole, fields of objects that stay, and
 * objects that go away.
 */
export type ProjectionPatch = {
  put: SceneObject[]
  fields: Record<string, Partial<SceneObject>>
  removed: string[]
  environment?: SceneEnvironment
  render?: SceneRender
}

export const placementUnit = (id: string) => `object:${id}`
export const contentUnit = (id: string) => `object:${id}:content`

export const placementKeys = ['name', 'transform', 'visible', 'locked'] as const
export const contentKeys = [
  'primitive',
  'material_slots',
  'light',
  'camera',
] as const

/** Splits an object unit name; null for any other unit. */
export function objectOfUnit(
  unit: string,
): { id: string; content: boolean } | null {
  const m = /^object:(sobj_[A-Za-z0-9_-]{21})(:content)?$/.exec(unit)
  return m ? { id: m[1], content: !!m[2] } : null
}

/** The object and all its descendants. */
export function subtree(objects: readonly SceneObject[], ids: Iterable<string>): Set<string> {
  const children = new Map<string, string[]>()
  for (const o of objects)
    if (o.parent_id) {
      const list = children.get(o.parent_id) ?? []
      list.push(o.id)
      children.set(o.parent_id, list)
    }
  const out = new Set<string>()
  const walk = (id: string) => {
    if (out.has(id)) return
    out.add(id)
    for (const c of children.get(id) ?? []) walk(c)
  }
  for (const id of ids) walk(id)
  return out
}

/** Fills missing primitive params with catalog defaults, as the server does. */
export function completeObject(o: SceneObject): SceneObject {
  if (!o.primitive) return o
  return {
    ...o,
    primitive: {
      ...o.primitive,
      params: { ...defaultParams(o.primitive.builtin), ...o.primitive.params },
    },
  }
}

export function updateObject(o: SceneObject, u: ObjectUpdate): SceneObject {
  const next: SceneObject = { ...o }
  if (u.name !== undefined) next.name = u.name
  if (u.transform !== undefined) next.transform = u.transform
  if (u.visible !== undefined) next.visible = u.visible
  if (u.locked !== undefined) next.locked = u.locked
  if (u.primitive && o.primitive && builtinSpec(o.primitive.builtin))
    next.primitive = {
      ...o.primitive,
      params: { ...o.primitive.params, ...u.primitive.params },
    }
  if (u.material_slots !== undefined) next.material_slots = u.material_slots
  if (u.light !== undefined) next.light = u.light
  if (u.camera !== undefined) next.camera = u.camera
  return next
}

export function applyOp(state: SceneState, op: WireOp): SceneState {
  const { scene } = state
  switch (op.op) {
    case 'rename':
      return { ...state, title: op.title }
    case 'restore_units':
      return state
    case 'set_environment':
      return { ...state, scene: { ...scene, environment: op.environment } }
    case 'set_render':
      return { ...state, scene: { ...scene, render: op.render } }
    case 'add_objects': {
      const present = new Set(scene.objects.map((o) => o.id))
      const added = op.objects
        .filter((o) => !present.has(o.id))
        .map(completeObject)
      return { ...state, scene: { ...scene, objects: [...scene.objects, ...added] } }
    }
    case 'update_objects': {
      const updates = new Map(op.updates.map((u) => [u.object_id, u]))
      return {
        ...state,
        scene: {
          ...scene,
          objects: scene.objects.map((o) => {
            const u = updates.get(o.id)
            return u ? updateObject(o, u) : o
          }),
        },
      }
    }
    case 'remove_objects': {
      const gone = subtree(scene.objects, op.object_ids)
      return {
        ...state,
        scene: { ...scene, objects: scene.objects.filter((o) => !gone.has(o.id)) },
      }
    }
    case 'reparent_objects': {
      const moves = new Map(op.moves.map((m) => [m.object_id, m]))
      return {
        ...state,
        scene: {
          ...scene,
          objects: scene.objects.map((o) => {
            const m = moves.get(o.id)
            return m ? { ...o, parent_id: m.parent_id, order: m.order } : o
          }),
        },
      }
    }
  }
}

export function applyPatch(state: SceneState, patch: ProjectionPatch): SceneState {
  const gone = new Set(patch.removed)
  const put = new Map(patch.put.map((o) => [o.id, o]))
  const objects = state.scene.objects
    .filter((o) => !gone.has(o.id))
    .map((o) => {
      const whole = put.get(o.id)
      if (whole) {
        put.delete(o.id)
        return { ...whole, parent_id: o.parent_id, order: o.order }
      }
      const fields = patch.fields[o.id]
      return fields ? { ...o, ...fields } : o
    })
  // Objects coming back keep their recorded parent when it is still a group;
  // the server settles order clashes.
  const groups = new Set(
    [...objects, ...put.values()].filter((o) => o.kind === 'group').map((o) => o.id),
  )
  for (const o of put.values())
    objects.push({ ...o, parent_id: o.parent_id && groups.has(o.parent_id) ? o.parent_id : null })
  return {
    ...state,
    scene: {
      ...state.scene,
      objects,
      environment: patch.environment ?? state.scene.environment,
      render: patch.render ?? state.scene.render,
    },
  }
}

export function applyOps(state: SceneState, ops: readonly WireOp[]): SceneState {
  return ops.reduce(applyOp, state)
}
