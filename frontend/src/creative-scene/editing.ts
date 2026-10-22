import { Matrix4, Quaternion, Vector3 } from 'three'
import type { Scene, SceneObject, SceneTransform } from './api.ts'
import { builtinSpec } from './builtins/catalog.ts'
import { newObjectID } from './ids.ts'
import { NEW_LIGHT_INTENSITY } from './render/litLook.ts'
import { sameValue, subtree, type WireOp } from './state/apply.ts'
import { PRESETS, type PresetName } from './world/atmosphere/constants.ts'
import { physicalSkyDefaults } from './world/physicalSky.ts'

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
 * An instance of a model revision placed from the asset library, last among
 * root objects, named after the asset (an object name holds 80 characters),
 * with the materials the asset was saved with.
 */
export function newModelInstance(
  revision: string,
  name: string,
  placed: Pick<SceneTransform, 'position' | 'rotation'>,
  objects: readonly SceneObject[],
  materials: NonNullable<SceneObject['material_slots']> = {},
  makeID: () => string = newObjectID,
): SceneObject {
  return {
    id: makeID(),
    kind: 'model_instance',
    name: Array.from(name).slice(0, 80).join(''),
    parent_id: null,
    order: nextOrder(objects, null),
    transform: { ...identity, ...placed },
    visible: true,
    locked: false,
    model: { content_revision_id: revision },
    material_slots: structuredClone(materials),
  }
}

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
/**
 * Where models set side by side along X go: each one's centre offset, `gap`
 * metres apart, the row centred on 0. Widths are the models' X extents.
 */
export function rowOffsets(widths: readonly number[], gap = 0.3): number[] {
  const total = widths.reduce((sum, w) => sum + w, 0) + gap * Math.max(0, widths.length - 1)
  let x = -total / 2
  return widths.map((w) => {
    const centre = x + w / 2
    x += w + gap
    return centre
  })
}

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

type LightType = NonNullable<SceneObject['light']>['type']
export const lightLabels: Record<LightType, string> = { directional: '平行光', point: '点光', spot: '聚光' }

/** The properties show a spot's full cone in degrees; the document keeps three.js's half angle in radians. */
export const coneDegrees = (angle: number) => (angle * 360) / Math.PI
export const coneAngle = (degrees: number) => (degrees * Math.PI) / 360

/** How high above the pointed ground point a new light goes. */
export const newLightHeight = 2

/**
 * A light from the add menu: above the point the pointer is on, facing the
 * way the view looks, so a spot or directional light shines into what the
 * photographer is looking at. Named 「聚光 N」 after the lights of its type.
 */
export function newLight(
  type: LightType,
  ground: [number, number, number],
  viewRotation: number[],
  objects: readonly SceneObject[],
  makeID: () => string = newObjectID,
): SceneObject {
  const n = objects.filter((o) => o.light?.type === type).length + 1
  return {
    id: makeID(),
    kind: 'light',
    name: `${lightLabels[type]} ${n}`,
    parent_id: null,
    order: nextOrder(objects, null),
    transform: { position: [ground[0], ground[1] + newLightHeight, ground[2]], rotation: type === 'point' ? [0, 0, 0, 1] : [...viewRotation], scale: [1, 1, 1] },
    visible: true,
    locked: false,
    light: { type, intensity: NEW_LIGHT_INTENSITY[type], ...(type === 'spot' ? { angle: Math.PI / 6, penumbra: 0.2 } : {}) },
  }
}

type Environment = Scene['environment']
type Sky = Environment['sky']

/** The sky a new scene starts with, and the colour a colour sky starts at. */
export const proceduralSky: Sky = { kind: 'procedural', sun_elevation_deg: 35, sun_azimuth_deg: 135, turbidity: 3 }
export const colorSky: Sky = { kind: 'color', color: '#a7b6c6' }

/** Today on this machine's calendar, YYYY-MM-DD: what a new physical sky's date starts at. */
export function localDate(now = new Date()): string {
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
}

/**
 * The environment with the sky switched to kind: each kind carries only its
 * own fields, so the other kind's values are left behind. A physical sky
 * starts from the contract's defaults (Shanghai at noon, clear) on today's
 * date (W2 design「天空模式与太阳」).
 */
export function withSkyKind(env: Environment, kind: 'procedural' | 'color' | 'physical', today = localDate()): Environment {
  if (env.sky.kind === kind) return env
  if (kind === 'physical') return { ...env, sky: { kind, ...physicalSkyDefaults, date: today } }
  return { ...env, sky: kind === 'procedural' ? { ...proceduralSky } : { ...colorSky } }
}

/**
 * A physical sky's sun mode. To manual the sun's angles as they are now
 * (sun, from the place and time) go with it in one write, so the picture
 * does not jump; below −10° (after dusk) the elevation stops at −10°. Back
 * to the place only the mode changes: both sets of fields are kept.
 */
export function withSunMode(env: Environment, mode: 'location' | 'manual', sun: { elevationDeg: number; azimuthDeg: number }): Environment {
  if (env.sky.sun_mode === mode) return env
  if (mode === 'location') return { ...env, sky: { ...env.sky, sun_mode: mode } }
  const azimuth = ((sun.azimuthDeg % 360) + 360) % 360
  return { ...env, sky: { ...env.sky, sun_mode: mode, sun_elevation_deg: Math.min(90, Math.max(-10, sun.elevationDeg)), sun_azimuth_deg: azimuth } }
}

/** A chosen city: its coordinates, time zone and name in one write. */
export function withCity(env: Environment, city: { name: string; latitude: number; longitude: number; timezone: string }): Environment {
  return { ...env, sky: { ...env.sky, latitude_deg: city.latitude, longitude_deg: city.longitude, timezone: city.timezone, place: city.name } }
}

/** An atmosphere preset: its four fields, not the sun's strength or colour. */
export function withPreset(env: Environment, preset: PresetName): Environment {
  return { ...env, sky: { ...env.sky, ...PRESETS[preset] } }
}

/** Fog on starts 10 m to 100 m in a pale grey; off removes it. */
export function withFog(env: Environment, on: boolean): Environment {
  if (on === !!env.fog) return env
  return { ...env, fog: on ? { color: '#c8ccd0', near: 10, far: 100 } : null }
}
