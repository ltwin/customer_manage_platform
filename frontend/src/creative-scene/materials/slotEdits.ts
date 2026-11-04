import type { ObjectUpdate, SceneObject } from '../api.ts'
import type { ParamValue } from '../builtins/catalog.ts'
import type { MeshConstants, SceneModel } from '../models/cache.ts'
import { effectiveSlots } from '../models/faces.ts'
import { materialEntry, principledID } from './catalog.ts'
import { defaultAlphaCutoff, resolveSlot, type ResolvedSlot } from './resolve.ts'

// The edits behind the properties panel's material section and 复制材质
// (design「属性栏与复制材质」). material_slots is replaced whole by
// update_objects, so each edit writes an object's whole map with one slot
// changed; one edit of many objects is one update_objects, one undo step.

export type MaterialSlot = NonNullable<SceneObject['material_slots']>[string]
type Slots = NonNullable<SceneObject['material_slots']>
type Maps = NonNullable<MaterialSlot['maps']>
type Mapping = NonNullable<MaterialSlot['mapping']>
export type Channel = 'r' | 'g' | 'b' | 'a'
export type AlphaMode = NonNullable<MaterialSlot['alpha_mode']>

/**
 * An object's slots (data: 模型内容「有效槽位」): a primitive's main, a
 * model's the manifest lists, then those its face assignment brings (faces).
 */
export function slotsOf(o: SceneObject, model: SceneModel | undefined): { slot: string; hint: string | null; faces: boolean }[] {
  const of = (glb: { id: string; hint?: string }[], faces: Parameters<typeof effectiveSlots>[1]) =>
    effectiveSlots(
      glb.map((s) => ({ id: s.id, hint: s.hint ?? '' })),
      faces,
    ).map((s) => ({ slot: s.id, hint: s.hint || null, faces: s.source === 'faces' }))
  if (o.kind === 'primitive') return of([{ id: 'main' }], o.primitive?.faces)
  if (o.kind === 'model_instance') return of(model?.manifest?.slots ?? [], model?.faces)
  return []
}

/** What a slot shows now, assigned or not. */
export function resolvedSlot(o: SceneObject, slot: string, hint: string | null, constants: MeshConstants | null): ResolvedSlot {
  return resolveSlot(slot, o.material_slots, hint, o.kind === 'primitive' ? null : constants)
}

/**
 * A slot of o as it draws (resolve.ts resolvePart): one a face assignment
 * brought, with no material of its own, as the slot its faces came from
 * (origin). What the material panel shows, and the base a parameter edit
 * starts from, so the edit keeps the material the faces show.
 */
export function shownSlot(o: SceneObject, slot: { slot: string; hint: string | null; faces: boolean }, origin: { slot: string | null; hint: string | null } | null, constants: MeshConstants | null): ResolvedSlot {
  if (!slot.faces || !origin || o.material_slots?.[slot.slot]) return resolvedSlot(o, slot.slot, slot.hint, constants)
  return resolveSlot(origin.slot, o.material_slots, origin.hint, o.kind === 'primitive' ? null : constants)
}

/**
 * The slot as an assignment: what is assigned, or what the slot resolves to
 * made explicit, so editing one parameter of a hinted or model material
 * keeps the rest of it.
 */
export function assignmentOf(r: ResolvedSlot): MaterialSlot | null {
  const m = r.material
  if (m.kind === 'unknown') return null
  if (m.kind === 'pattern') return { preset: m.id, params: { ...m.params } }
  return { preset: principledID, params: { ...m.params }, ...structuredClone(m.texturing ?? {}) }
}

// The texture fields (C5c1 design「贴图属于材质槽」): a principled slot's five
// image inputs, the one mapping they share, and how its alpha is used. Each
// returns the slot whole, as data: 场景文档 has it: mapping only with maps,
// the cutoff only with clip, opaque written as nothing.

/** The five image inputs, in the panel's order. */
export const mapInputs = ['base_color', 'normal', 'roughness', 'metalness', 'ao'] as const
export type MapInput = (typeof mapInputs)[number]
/** The inputs as the panel names them, Blender's names. */
export const mapInputLabel: Record<MapInput, string> = { base_color: '基础色', normal: '法线', roughness: '糙度', metalness: '金属度', ao: 'AO' }
/** The inputs that read one channel of their image. */
export type ChannelInput = 'roughness' | 'metalness' | 'ao'
export const isChannelInput = (i: MapInput): i is ChannelInput => i === 'roughness' || i === 'metalness' || i === 'ao'

const defaultMapping: Mapping = { kind: 'triplanar', size_m: 1, rotation_deg: 0, normal_strength: 1 }

/** m with input's image set (rev), or cleared (null); a replaced image keeps its channel, the last one cleared takes the mapping along. */
export function withMap(m: MaterialSlot, input: MapInput, rev: string | null): MaterialSlot {
  const maps: Maps = { ...(m.maps ?? {}) }
  if (rev === null) delete maps[input]
  else maps[input] = { ...(maps[input] ?? {}), content_revision_id: rev }
  const { maps: _maps, mapping, ...rest } = m
  if (Object.keys(maps).length === 0) return rest
  return { ...rest, maps, mapping: mapping ?? { ...defaultMapping } }
}

/** m with the channel input reads; nothing to change without its image. */
export function withChannel(m: MaterialSlot, input: ChannelInput, channel: Channel): MaterialSlot {
  const map = m.maps?.[input]
  if (!map) return m
  return { ...m, maps: { ...m.maps, [input]: { ...map, channel } } }
}

/**
 * m's mapping changed: switching between box projection and UV keeps the
 * rotation and strength, and takes the other kind's own field away (size_m
 * goes with triplanar, repeat with uv), the new one at its default.
 */
export function withMapping(m: MaterialSlot, change: Partial<Mapping>): MaterialSlot {
  if (!m.maps) return m
  const was = m.mapping ?? defaultMapping
  const kind = change.kind ?? was.kind ?? 'triplanar'
  const shared = { rotation_deg: change.rotation_deg ?? was.rotation_deg ?? 0, normal_strength: change.normal_strength ?? was.normal_strength ?? 1 }
  const sameKind = kind === (was.kind ?? 'triplanar')
  const mapping: Mapping =
    kind === 'uv'
      ? { kind, repeat: change.repeat ?? (sameKind ? was.repeat : undefined) ?? [1, 1], ...shared }
      : { kind, size_m: change.size_m ?? (sameKind ? was.size_m : undefined) ?? 1, ...shared }
  return { ...m, mapping }
}

/** m with an alpha mode: clip keeps its cutoff or takes the default, the others have none; opaque is the field left out. */
export function withAlpha(m: MaterialSlot, mode: AlphaMode, cutoff?: number): MaterialSlot {
  const { alpha_mode: _mode, alpha_cutoff, ...rest } = m
  if (mode === 'opaque') return rest
  if (mode === 'blend') return { ...rest, alpha_mode: mode }
  return { ...rest, alpha_mode: mode, alpha_cutoff: cutoff ?? alpha_cutoff ?? defaultAlphaCutoff }
}

/**
 * Sets slot on every selected object that has a slot of that name, or only
 * on the active one when it alone has it. next gets each object's current
 * assignment (null when it has none).
 */
export function setSlot(
  objects: readonly SceneObject[],
  selected: readonly string[],
  active: string,
  slot: string,
  slotNames: (o: SceneObject) => string[],
  next: (o: SceneObject, current: MaterialSlot | null) => MaterialSlot | null,
): ObjectUpdate[] {
  const byID = new Map(objects.map((o) => [o.id, o]))
  const ids = selected.includes(active) ? selected : [...selected, active]
  const having = ids.filter((id) => {
    const o = byID.get(id)
    return o && slotNames(o).includes(slot)
  })
  const targets = having.length ? having : [active]
  return targets.flatMap((id) => {
    const o = byID.get(id)
    if (!o) return []
    const slots: Slots = { ...(o.material_slots ?? {}) }
    const m = next(o, slots[slot] ?? null)
    if (m) slots[slot] = m
    else delete slots[slot]
    return [{ object_id: id, material_slots: slots }]
  })
}

/**
 * 转为原理化: the preset's tint, roughness and metalness carry over; paint
 * is exactly that, emissive keeps its glow. lost is true when a pattern is
 * dropped.
 */
export function toPrincipled(m: MaterialSlot): { slot: MaterialSlot; lost: boolean } {
  if (m.preset === principledID) return { slot: m, lost: false }
  const entry = materialEntry(m.preset)
  const p = m.params ?? {}
  const pick = (k: string) => (p[k] !== undefined ? p[k] : entry?.params[k]?.default)
  const params: Record<string, unknown> = { color: pick('color'), roughness: pick('roughness'), metalness: pick('metalness') }
  if (m.preset === 'emissive@1') {
    params.emissive = pick('color')
    params.emissive_intensity = pick('strength')
  }
  return { slot: { preset: principledID, params }, lost: m.preset !== 'paint@1' && m.preset !== 'emissive@1' }
}

/**
 * 复制材质到选中物件: the active object's assignments by slot name; when it
 * has one slot, its material goes to every slot of each target. A slot the
 * active object leaves unassigned is unassigned on the target too. Targets
 * without a matching slot are left as they are.
 */
export function copyMaterials(
  objects: readonly SceneObject[],
  active: string,
  targets: readonly string[],
  slotNames: (o: SceneObject) => string[],
): ObjectUpdate[] {
  const byID = new Map(objects.map((o) => [o.id, o]))
  const source = byID.get(active)
  if (!source) return []
  const names = slotNames(source)
  if (!names.length) return []
  const from = source.material_slots ?? {}
  const updates: ObjectUpdate[] = []
  for (const id of targets) {
    const o = byID.get(id)
    if (!o || id === active) continue
    const own = slotNames(o)
    const slots: Slots = { ...(o.material_slots ?? {}) }
    let changed = false
    for (const slot of own) {
      const src = names.length === 1 ? names[0] : names.includes(slot) ? slot : null
      if (src === null) continue
      const m = from[src]
      if (m) slots[slot] = structuredClone(m)
      else delete slots[slot]
      changed = true
    }
    if (changed) updates.push({ object_id: id, material_slots: slots })
  }
  return updates
}

/** Copied materials, their textures with them, survive across scenes in this browser. */
export const clipboardKey = 'creative-scene:material-clipboard'

export function writeClipboard(storage: Storage | undefined, m: MaterialSlot) {
  try {
    storage?.setItem(clipboardKey, JSON.stringify({ ...m, params: m.params ?? {} }))
  } catch {
    // Storage full or blocked: nothing to paste later.
  }
}

export function readClipboard(storage: Storage | undefined): MaterialSlot | null {
  try {
    const v = JSON.parse(storage?.getItem(clipboardKey) ?? 'null') as unknown
    if (!v || typeof v !== 'object') return null
    const o = v as { preset?: unknown; params?: unknown } & Pick<MaterialSlot, 'maps' | 'mapping' | 'alpha_mode' | 'alpha_cutoff'>
    if (typeof o.preset !== 'string' || !o.params || typeof o.params !== 'object') return null
    // The texture fields as written; the server checks them as any write.
    const { maps, mapping, alpha_mode, alpha_cutoff } = o
    return { preset: o.preset, params: o.params as Record<string, ParamValue>, ...(maps ? { maps } : {}), ...(mapping ? { mapping } : {}), ...(alpha_mode ? { alpha_mode } : {}), ...(alpha_cutoff !== undefined ? { alpha_cutoff } : {}) }
  } catch {
    return null
  }
}
