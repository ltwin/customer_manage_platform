import type { components } from '../../api/schema'
import type { ParamValue } from '../builtins/catalog.ts'
import type { MeshConstants } from '../models/cache.ts'
import { completeParams, hintPreset, materialEntry, principledID } from './catalog.ts'

// Which material a mesh draws with (design「材质解析」), first match wins:
//  1. the object's material_slots names a material in the catalog;
//  2. it names one this build does not know: neutral grey, flagged;
//  3. nothing assigned, and the slot's hint maps to a preset;
//  4. nothing assigned, a model: the module's own material constants;
//  5. nothing assigned, a primitive: the clay colour.

type Slot = components['schemas']['CreativeSceneMaterialSlot']

/** A principled slot's textures and how it uses alpha, as the scene stores them; absent on a plain slot. */
export type Texturing = Pick<Slot, 'maps' | 'mapping' | 'alpha_mode' | 'alpha_cutoff'>

/** The cutoff a clip slot uses when it names none; the store fills it in, hand-written slots may not. */
export const defaultAlphaCutoff = 0.5

export type SlotMaterial =
  | { kind: 'principled'; params: Record<string, ParamValue>; vertexColors?: boolean; texturing?: Texturing }
  | { kind: 'pattern'; id: string; params: Record<string, ParamValue> }
  | { kind: 'unknown' }

export type ResolvedSlot = {
  slot: string | null
  source: 'assigned' | 'hint' | 'model' | 'default' | 'unknown'
  material: SlotMaterial
}

type Assigned = Readonly<components['schemas']['CreativeSceneMaterialSlots']> | undefined

/**
 * A material from the catalog with its parameters complete and in range. A
 * principled one keeps the slot's textures and alpha mode; presets take none.
 */
export function catalogMaterial(id: string, params?: Record<string, unknown>, slot?: Texturing): SlotMaterial | null {
  const entry = materialEntry(id)
  if (!entry) return null
  const complete = completeParams(entry, params)
  if (entry.kind !== 'principled') return { kind: 'pattern', id, params: complete }
  const texturing = slot && texturingOf(slot)
  return { kind: 'principled', params: complete, ...(texturing ? { texturing } : {}) }
}

/**
 * What of a slot's textures and alpha the material draws with, defaults left
 * out: a slot that spells out opaque shares an instance with one that does
 * not. undefined for a plain slot.
 */
function texturingOf(s: Texturing): Texturing | undefined {
  const out: Texturing = {}
  if (s.maps && Object.keys(s.maps).length > 0) {
    out.maps = s.maps
    if (s.mapping) out.mapping = s.mapping
  }
  if (s.alpha_mode === 'clip' || s.alpha_mode === 'blend') out.alpha_mode = s.alpha_mode
  if (s.alpha_mode === 'clip') out.alpha_cutoff = s.alpha_cutoff ?? defaultAlphaCutoff
  return Object.keys(out).length > 0 ? out : undefined
}

/** The clay colour: what an unassigned primitive shows in the material looks. */
export const primitiveDefault: SlotMaterial = catalogMaterial(principledID, { color: '#d9d5cd', roughness: 0.92 })!

export function resolveSlot(slot: string | null, assigned: Assigned, hint: string | null | undefined, constants: MeshConstants | null): ResolvedSlot {
  const a = slot !== null ? assigned?.[slot] : undefined
  if (a) {
    const m = catalogMaterial(a.preset, a.params, a)
    return m ? { slot, source: 'assigned', material: m } : { slot, source: 'unknown', material: { kind: 'unknown' } }
  }
  const preset = slot !== null ? hintPreset(hint) : undefined
  if (preset) return { slot, source: 'hint', material: catalogMaterial(preset)! }
  if (constants) {
    const params = completeParams(materialEntry(principledID)!, {
      color: constants.color,
      roughness: constants.roughness,
      metalness: constants.metalness,
      emissive: constants.emissive,
      emissive_intensity: constants.emissiveIntensity,
    })
    return { slot, source: 'model', material: { kind: 'principled', params, ...(constants.vertexColors ? { vertexColors: true } : {}) } }
  }
  return { slot, source: 'default', material: primitiveDefault }
}

/**
 * A model part's material in the material looks: its slot's, the hint taken
 * from the manifest first. A part a face assignment moved draws as the slot
 * it came from until its own slot has a material, so assigning faces alone
 * changes nothing on screen (scene-face-materials AC-3).
 */
export function resolvePart(
  part: { slot: string | null; hint: string | null; constants: MeshConstants | null; origin?: { slot: string | null; hint: string | null } },
  assigned: Assigned,
  hints: ReadonlyMap<string, string | undefined>,
): ResolvedSlot {
  const hintOf = (slot: string | null, own: string | null) => (slot !== null ? hints.get(slot) : undefined) ?? own
  if (!part.origin || (part.slot !== null && assigned?.[part.slot])) return resolveSlot(part.slot, assigned, hintOf(part.slot, part.hint), part.constants)
  return resolveSlot(part.origin.slot, assigned, hintOf(part.origin.slot, part.origin.hint), part.constants)
}

