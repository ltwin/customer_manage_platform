import type { ObjectUpdate, SceneObject } from '../api.ts'
import type { ParamValue } from '../builtins/catalog.ts'
import type { MeshConstants, SceneModel } from '../models/cache.ts'
import { materialEntry, principledID } from './catalog.ts'
import { resolveSlot, type ResolvedSlot } from './resolve.ts'

// The edits behind the properties panel's material section and 复制材质
// (design「属性栏与复制材质」). material_slots is replaced whole by
// update_objects, so each edit writes an object's whole map with one slot
// changed; one edit of many objects is one update_objects, one undo step.

export type MaterialSlot = NonNullable<SceneObject['material_slots']>[string]
type Slots = NonNullable<SceneObject['material_slots']>

/** An object's slots: a primitive has main, a model the slots its manifest lists. */
export function slotsOf(o: SceneObject, model: SceneModel | undefined): { slot: string; hint: string | null }[] {
  if (o.kind === 'primitive') return [{ slot: 'main', hint: null }]
  if (o.kind === 'model_instance') return (model?.manifest?.slots ?? []).map((s) => ({ slot: s.id, hint: s.hint ?? null }))
  return []
}

/** What a slot shows now, assigned or not. */
export function resolvedSlot(o: SceneObject, slot: string, hint: string | null, constants: MeshConstants | null): ResolvedSlot {
  return resolveSlot(slot, o.material_slots, hint, o.kind === 'primitive' ? null : constants)
}

/**
 * The slot as an assignment: what is assigned, or what the slot resolves to
 * made explicit, so editing one parameter of a hinted or model material
 * keeps the rest of it.
 */
export function assignmentOf(r: ResolvedSlot): MaterialSlot | null {
  const m = r.material
  if (m.kind === 'unknown') return null
  return { preset: m.kind === 'pattern' ? m.id : principledID, params: { ...m.params } }
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

/** Copied materials survive across scenes in this browser. */
export const clipboardKey = 'creative-scene:material-clipboard'

export function writeClipboard(storage: Storage | undefined, m: MaterialSlot) {
  try {
    storage?.setItem(clipboardKey, JSON.stringify({ preset: m.preset, params: m.params ?? {} }))
  } catch {
    // Storage full or blocked: nothing to paste later.
  }
}

export function readClipboard(storage: Storage | undefined): MaterialSlot | null {
  try {
    const v = JSON.parse(storage?.getItem(clipboardKey) ?? 'null') as unknown
    if (!v || typeof v !== 'object') return null
    const o = v as { preset?: unknown; params?: unknown }
    if (typeof o.preset !== 'string' || !o.params || typeof o.params !== 'object') return null
    return { preset: o.preset, params: o.params as Record<string, ParamValue> }
  } catch {
    return null
  }
}
