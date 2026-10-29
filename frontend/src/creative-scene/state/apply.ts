import type {
  ObjectUpdate,
  Scene,
  SceneEnvironment,
  SceneObject,
  SceneOperation,
  SceneRender,
} from '../api.ts'
import { builtinSpec, defaultParams } from '../builtins/catalog.ts'
import { sameTransform, worldOf } from './world.ts'
import { completeSky } from '../world/physicalSky.ts'

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
  'volume',
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

/** Fills missing primitive params and volume fields with their defaults, as the server does. */
export function completeObject(o: SceneObject): SceneObject {
  if (o.volume) return { ...o, volume: { ...volumeDefaults, ...o.volume } }
  if (!o.primitive) return o
  return {
    ...o,
    primitive: {
      ...o.primitive,
      params: { ...defaultParams(o.primitive.builtin), ...o.primitive.params },
    },
  }
}

type Light = NonNullable<SceneObject['light']>
type LightField = Exclude<keyof Light, 'type'>
type Camera = NonNullable<SceneObject['camera']>
type Volume = NonNullable<SceneObject['volume']>

// The light table of data: 场景文档 — the fields each type has besides type,
// and the three.js defaults the server stores for those left out.
const lightFields: Record<Light['type'], readonly LightField[]> = {
  directional: ['color', 'intensity', 'cast_shadow', 'angular_diameter_deg'],
  point: ['color', 'intensity', 'cast_shadow', 'distance', 'decay', 'radius_m'],
  spot: ['color', 'intensity', 'cast_shadow', 'distance', 'decay', 'angle', 'penumbra', 'radius_m'],
  area: ['color', 'intensity', 'cast_shadow', 'shape', 'width', 'height'],
}
export const lightDefaults: Required<Omit<Light, 'type'>> = {
  color: '#ffffff',
  intensity: 1,
  cast_shadow: false,
  distance: 0,
  decay: 2,
  angle: Math.PI / 3,
  penumbra: 0,
  // data: 光源尺寸: a point or spot light's radius, a directional light's angular diameter (the sun's).
  radius_m: 0.1,
  angular_diameter_deg: 0.545,
  // data: 面光源: a 0.25 m square.
  shape: 'rectangle',
  width: 0.25,
  height: 0.25,
}
/** The defaults the server stores for a light of type: an area light's intensity a luminance, 10000 cd/m² (data: 面光源). */
export const lightDefaultsOf = (type: Light['type']): Required<Omit<Light, 'type'>> => (type === 'area' ? { ...lightDefaults, intensity: 10000 } : lightDefaults)

/** The top of a light type's intensity: an area light's luminance, cd/m², the others' 1000. */
export const maxLightIntensity = (type: Light['type']) => (type === 'area' ? 100000 : 1000)

/** An area light's emitting area, m², its object's world transform scaling its face by areaScale (worldAreaScale). */
export function emittingArea(l: Light, areaScale: number): number {
  const a = (l.width ?? lightDefaults.width) * (l.height ?? lightDefaults.height) * areaScale
  return (l.shape ?? lightDefaults.shape) === 'ellipse' ? (a * Math.PI) / 4 : a
}

/**
 * How much object id's world transform, its parents' included, scales an area in its local XY plane: |X × Y| of
 * its world matrix's first two columns, a parallelogram's too (data: 面光源「等效发光强度」; the server's
 * worldAreaScale).
 */
export function worldAreaScale(objects: readonly SceneObject[], id: string): number {
  const e = worldOf(objects)(id).elements
  const [ax, ay, az, bx, by, bz] = [e[0], e[1], e[2], e[4], e[5], e[6]]
  return Math.hypot(ay * bz - az * by, az * bx - ax * bz, ax * by - ay * bx)
}

/**
 * A type change's intensity when the update writes none (data: 面光源「换类型」, Epic I6): to and from an area
 * light the equivalent luminous intensity kept, the old area at before and the new at after (worldAreaScale before
 * and after the update); then held to the new type's top. As the server's mergeLight, and the properties write it.
 */
export function convertedIntensity(cur: Light, next: Light, before: number, after: number): number {
  let v = next.intensity ?? lightDefaultsOf(next.type).intensity
  if (cur.type === 'area' && (next.type === 'point' || next.type === 'spot')) v *= emittingArea(cur, before)
  else if (next.type === 'area' && (cur.type === 'point' || cur.type === 'spot')) {
    const a = emittingArea(next, after)
    if (a > 0) v /= a
  }
  return Math.min(v, maxLightIntensity(next.type))
}

/**
 * A document light's size: the radius of a point or spot light, the angular
 * diameter of a directional one; left out, the default the server stores. An
 * area light's size is its width and height, not this: 0 (lightHasSize).
 */
export function lightSize(l: Light): number {
  if (l.type === 'area') return 0
  return l.type === 'directional' ? (l.angular_diameter_deg ?? lightDefaults.angular_diameter_deg) : (l.radius_m ?? lightDefaults.radius_m)
}

/** Whether a document light has a size: an area light always (data: 面光源), any other at lightSize above 0. */
export const lightHasSize = (l: Light): boolean => l.type === 'area' || lightSize(l) > 0
/** The camera fields the server stores for those left out. */
export const cameraDefaults: Required<Camera> = {
  focal_mm: 35,
  sensor_mm: [36, 24],
  aspect: '3:2',
  near: 0.1,
  far: 1000,
  label: '',
  depth_of_field: false,
  f_stop: 2.8,
  focus: 10,
  focus_object_id: '',
  iso: 100,
  shutter_s: 1 / 4000,
  exposure_mode: 'ambient',
  exposure_compensation: 0,
}

/** The volume fields the server stores for those left out (data: 体积物件). */
export const volumeDefaults: Required<Volume> = {
  shape: 'box',
  scatter_color: '#cccccc',
  scatter_density: 0.1,
  anisotropy: 0,
  absorption_color: '#cccccc',
  absorption_density: 0,
  edge_falloff: 0.1,
  cast_shadow: false,
}

/** As the server's mergeVolume: the update changes only what it carries. */
export const mergeVolume = (cur: Volume, patch: Volume): Required<Volume> => ({ ...volumeDefaults, ...cur, ...patch })

/**
 * As the server's mergeLight: the update changes only what it carries, a new
 * type keeps the fields it shares and fills the ones it gains. Null where the
 * server refuses, an update with a field the resulting type lacks.
 */
export function mergeLight(cur: Light, patch: NonNullable<ObjectUpdate['light']>, before = 1, after = 1): Light | null {
  const type = patch.type ?? cur.type
  const has: readonly string[] = lightFields[type]
  if (Object.keys(patch).some((k) => k !== 'type' && !has.includes(k))) return null
  const defaults = lightDefaultsOf(type)
  const next: Record<string, unknown> = { type }
  for (const k of lightFields[type]) next[k] = patch[k] ?? cur[k] ?? defaults[k]
  if (type !== cur.type && patch.intensity === undefined) next.intensity = convertedIntensity(cur, next as Light, before, after)
  return next as Light
}

/** Whether a light update changes the type to or from an area light without writing intensity: the merge needs the world scales. */
const convertsArea = (cur: Light, patch: NonNullable<ObjectUpdate['light']>) =>
  patch.type !== undefined && patch.type !== cur.type && patch.intensity === undefined && (cur.type === 'area' || patch.type === 'area')

/** u on o; a light turned to or from an area light at areaScale before and after (mergeLight). */
export function updateObject(o: SceneObject, u: ObjectUpdate, areaScale: { before: number; after: number } = { before: 1, after: 1 }): SceneObject {
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
  if (u.light && o.light) next.light = mergeLight(o.light, u.light, areaScale.before, areaScale.after) ?? o.light
  if (u.camera && o.camera) next.camera = { ...cameraDefaults, ...o.camera, ...u.camera }
  if (u.volume && o.volume) next.volume = mergeVolume(o.volume, u.volume)
  return next
}

export function applyOp(state: SceneState, op: WireOp): SceneState {
  const { scene } = state
  switch (op.op) {
    case 'rename':
      return { ...state, title: op.title }
    case 'restore_units':
      return state
    // A physical sky is stored complete, as the server's completeSky does.
    case 'set_environment':
      return { ...state, scene: { ...scene, environment: { ...op.environment, sky: completeSky(op.environment.sky) } } }
    // Merged by key, as the server does: writing the mode keeps the exposure.
    case 'set_render':
      return { ...state, scene: { ...scene, render: { ...scene.render, ...op.render } } }
    case 'add_objects': {
      const present = new Set(scene.objects.map((o) => o.id))
      const added = op.objects
        .filter((o) => !present.has(o.id))
        .map(completeObject)
      return { ...state, scene: { ...scene, objects: [...scene.objects, ...added] } }
    }
    case 'update_objects': {
      const updates = new Map(op.updates.map((u) => [u.object_id, u]))
      // As the server's: a light turned to or from an area light at the world scale before every transform here is
      // written and after, so such lights merge last.
      const converting = scene.objects.filter((o) => o.light && updates.get(o.id)?.light && convertsArea(o.light, updates.get(o.id)!.light!))
      const moved = scene.objects.map((o) => {
        const u = updates.get(o.id)
        if (!u) return o
        return converting.includes(o) ? updateObject(o, { ...u, light: undefined }) : updateObject(o, u)
      })
      return {
        ...state,
        scene: {
          ...scene,
          objects: moved.map((o, i) => {
            const was = scene.objects[i]
            if (!converting.includes(was)) return o
            const scale = { before: worldAreaScale(scene.objects, o.id), after: worldAreaScale(moved, o.id) }
            return { ...o, light: mergeLight(was.light!, updates.get(o.id)!.light!, scale.before, scale.after) ?? was.light }
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
    // As the server: create, then move (a transform replaces the local
    // one), then remove.
    case 'reparent_objects': {
      const present = new Set(scene.objects.map((o) => o.id))
      const created = (op.create_groups ?? []).filter((g) => !present.has(g.id))
      const moves = new Map(op.moves.map((m) => [m.object_id, m]))
      const gone = new Set(op.remove_groups ?? [])
      const objects = [...scene.objects, ...created]
        .map((o) => {
          const m = moves.get(o.id)
          return m ? { ...o, parent_id: m.parent_id, order: m.order, ...(m.transform ? { transform: m.transform } : {}) } : o
        })
        .filter((o) => !gone.has(o.id))
      return { ...state, scene: { ...scene, objects } }
    }
    // A committed or derived model changes no unit until an operation places it.
    case 'commit_model':
    case 'derive_model':
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

/**
 * A model committed by a local intent has no IDs until its receipt. Until
 * then the view, copies and later edits use these placeholders; the receipt
 * replaces them everywhere before the base moves on.
 */
export const localRevision = (intentID: string, ref: string) => `local:${intentID}:${ref}`
export const localContent = (intentID: string, ref: string) => `local-content:${intentID}:${ref}`

export function parsePlaceholder(id: string): { intent: string; ref: string; content: boolean } | null {
  const m = /^local(-content)?:([^:]+):([a-z0-9_]{1,32})$/.exec(id)
  return m ? { intent: m[2], ref: m[3], content: !!m[1] } : null
}

/** The ops with every {ref} model reference written as a revision ID. */
export function withModelRevisions(ops: readonly WireOp[], revisionOf: (ref: string) => string): WireOp[] {
  const resolve = (m: SceneObject['model']) =>
    m?.ref !== undefined ? { content_revision_id: revisionOf(m.ref) } : m
  return ops.map((op) => {
    if (op.op === 'add_objects' && op.objects.some((o) => o.model?.ref !== undefined))
      return { ...op, objects: op.objects.map((o) => (o.model?.ref !== undefined ? { ...o, model: resolve(o.model) } : o)) }
    if (op.op === 'set_instance_model' && op.model.ref !== undefined)
      return { ...op, model: resolve(op.model)! }
    return op
  })
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

const updateKeys = ['name', 'transform', 'visible', 'locked', 'material_slots', 'light', 'camera', 'volume'] as const

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
      if (sameValue({ ...probe.scene.render, ...op.render }, probe.scene.render)) kept = null
    } else if (op.op === 'reparent_objects') {
      kept = reparentWithoutNoops(op, probe)
    }
    if (!kept) continue
    out.push(kept)
    probe = applyOp(probe, kept)
  }
  return out
}

/**
 * A reparent without the moves that leave an object where it is: same
 * parent, same order, and a transform equal within float noise. Without
 * moves, new groups become an add and removed ones a remove (moves must have
 * one item); with neither, nothing is left.
 */
function reparentWithoutNoops(op: Extract<WireOp, { op: 'reparent_objects' }>, state: SceneState): WireOp | null {
  const byID = new Map(state.scene.objects.map((o) => [o.id, o]))
  const moves = op.moves.filter((m) => {
    const o = byID.get(m.object_id)
    if (!o) return true
    return (o.parent_id ?? null) !== m.parent_id || o.order !== m.order || (m.transform !== undefined && !sameTransform(m.transform, o.transform))
  })
  if (moves.length) return moves.length === op.moves.length ? op : { ...op, moves }
  if (op.create_groups?.length && !op.remove_groups?.length) return { op: 'add_objects', objects: op.create_groups }
  if (op.remove_groups?.length && !op.create_groups?.length) return { op: 'remove_objects', object_ids: op.remove_groups }
  return null
}
