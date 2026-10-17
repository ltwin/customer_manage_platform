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
  'model',
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
    // A committed model changes no unit until an operation places it.
    case 'commit_model':
      return state
    case 'set_instance_model': {
      const targets = new Set(instanceTargets(op))
      return {
        ...state,
        scene: {
          ...scene,
          objects: scene.objects.map((o) =>
            targets.has(o.id) ? { ...o, model: op.model } : o,
          ),
        },
      }
    }
  }
}

/** The instances set_instance_model changes, as the server reads it. */
export const instanceTargets = (
  op: Extract<WireOp, { op: 'set_instance_model' }>,
): string[] =>
  op.scope === 'all_instances' ? (op.object_ids ?? []) : [op.object_id]

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

/** Equal as JSON values: what the server would store is the same. */
export function sameValue(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if (typeof a !== 'object' || typeof b !== 'object' || a === null || b === null) return false
  if (Array.isArray(a) !== Array.isArray(b)) return false
  const ka = Object.keys(a).filter((k) => (a as Record<string, unknown>)[k] !== undefined)
  const kb = Object.keys(b).filter((k) => (b as Record<string, unknown>)[k] !== undefined)
  return ka.length === kb.length && ka.every((k) => sameValue((a as Record<string, unknown>)[k], (b as Record<string, unknown>)[k]))
}

const updateKeys = ['name', 'transform', 'visible', 'locked', 'material_slots', 'light', 'camera'] as const

/** The fields of u that would change o; null when none would. */
function changedPart(o: SceneObject, u: ObjectUpdate): ObjectUpdate | null {
  const out: ObjectUpdate = { object_id: u.object_id }
  let any = false
  for (const k of updateKeys)
    if (u[k] !== undefined && !sameValue(u[k], o[k])) {
      ;(out as Record<string, unknown>)[k] = u[k]
      any = true
    }
  if (u.primitive) {
    const now = o.primitive?.params ?? {}
    const params = Object.fromEntries(Object.entries(u.primitive.params).filter(([k, v]) => !o.primitive || !sameValue(v, now[k])))
    if (!o.primitive || Object.keys(params).length) {
      out.primitive = { ...u.primitive, params }
      any = true
    }
  }
  return any ? out : null
}

/**
 * Drops what would change nothing: fields already holding the value, updates
 * left empty and renames or settings to what is already there. Every saved
 * edit is a whole snapshot on the server and an undo step, so clearing an
 * object already at the origin must not be one. Other operations pass as
 * they are; an object the state does not have is left for the server.
 */
export function withoutNoops(ops: readonly WireOp[], state: SceneState): WireOp[] {
  const out: WireOp[] = []
  let probe = state
  for (const op of ops) {
    let kept: WireOp | null = op
    if (op.op === 'update_objects') {
      const byID = new Map(probe.scene.objects.map((o) => [o.id, o]))
      const updates = op.updates.flatMap((u) => {
        const o = byID.get(u.object_id)
        if (!o) return [u]
        const part = changedPart(o, u)
        return part ? [part] : []
      })
      kept = updates.length ? { ...op, updates } : null
    } else if (op.op === 'rename') {
      if (op.title === probe.title) kept = null
    } else if (op.op === 'set_environment') {
      if (sameValue(op.environment, probe.scene.environment)) kept = null
    } else if (op.op === 'set_render') {
      if (sameValue(op.render, probe.scene.render)) kept = null
    }
    if (!kept) continue
    out.push(kept)
    probe = applyOp(probe, kept)
  }
  return out
}
