import type { ParamValue } from '../builtins/catalog.ts'
import type { MeshConstants } from '../models/cache.ts'
import { completeParams, hintPreset, materialEntry, principledID } from './catalog.ts'

// Which material a mesh draws with (design「材质解析」), first match wins:
//  1. the object's material_slots names a material in the catalog;
//  2. it names one this build does not know: neutral grey, flagged;
//  3. nothing assigned, and the slot's hint maps to a preset;
//  4. nothing assigned, a model: the module's own material constants;
//  5. nothing assigned, a primitive: the clay colour.

export type SlotMaterial =
  | { kind: 'principled'; params: Record<string, ParamValue>; vertexColors?: boolean }
  | { kind: 'pattern'; id: string; params: Record<string, ParamValue> }
  | { kind: 'unknown' }

export type ResolvedSlot = {
  slot: string | null
  source: 'assigned' | 'hint' | 'model' | 'default' | 'unknown'
  material: SlotMaterial
}

type Assigned = Readonly<Record<string, { preset: string; params?: Record<string, unknown> }>> | undefined

/** A material from the catalog with its parameters complete and in range. */
export function catalogMaterial(id: string, params?: Record<string, unknown>): SlotMaterial | null {
  const entry = materialEntry(id)
  if (!entry) return null
  const complete = completeParams(entry, params)
  return entry.kind === 'principled' ? { kind: 'principled', params: complete } : { kind: 'pattern', id, params: complete }
}

/** The clay colour: what an unassigned primitive shows in the material looks. */
export const primitiveDefault: SlotMaterial = catalogMaterial(principledID, { color: '#d9d5cd', roughness: 0.92 })!

export function resolveSlot(slot: string | null, assigned: Assigned, hint: string | null | undefined, constants: MeshConstants | null): ResolvedSlot {
  const a = slot !== null ? assigned?.[slot] : undefined
  if (a) {
    const m = catalogMaterial(a.preset, a.params)
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
