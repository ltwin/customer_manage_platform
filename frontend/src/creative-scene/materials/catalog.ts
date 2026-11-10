import catalog from '../materials.gen.json' with { type: 'json' }
import type { ParamSpec, ParamValue } from '../builtins/catalog.ts'

// The material catalog is generated from
// backend/internal/creativescene/materials.json; the assistant's edits are
// checked against the same file. A published preset never changes.

/** A texture of a built-in textured material (data: 内置贴图库): a file of its entry, and the channel a scalar input reads. */
export type BuiltinMap = { file: string; channel?: 'r' | 'g' | 'b' | 'a' }
export type BuiltinMaps = Partial<Record<'base_color' | 'normal' | 'roughness' | 'metalness' | 'ao', BuiltinMap>>

/**
 * A catalog entry. A textured one (C5c2) is a built-in CC0 material: its own
 * textures, its real size, and principled's parameters with its defaults,
 * which materialEntry gives it as params.
 */
export type MaterialEntry = {
  id: string
  kind: 'principled' | 'pattern' | 'textured'
  label: string
  params: Record<string, ParamSpec>
  category?: string
  size_m?: number
  maps?: BuiltinMaps
  source?: { site: string; asset: string; author: string; license: string }
}
type RawEntry = Omit<MaterialEntry, 'params'> & { params?: Record<string, ParamSpec>; defaults?: Record<string, ParamValue> }
export type MaterialCategory = { id: string; label: string }
export type MaterialCatalog = { materials: MaterialEntry[]; categories: MaterialCategory[]; hints: Record<string, string> }

export const principledID = 'principled@1'

const raw = catalog as unknown as { materials: RawEntry[]; categories: MaterialCategory[]; hints: Record<string, string> }
const principledParams = raw.materials.find((m) => m.id === principledID)!.params!

/** A textured entry's parameters: principled's, its defaults in place of theirs. */
function withDefaults(m: RawEntry): MaterialEntry {
  const { defaults, ...entry } = m
  if (m.kind !== 'textured') return entry as MaterialEntry
  const params = Object.fromEntries(Object.entries(principledParams).map(([k, spec]) => [k, defaults?.[k] !== undefined ? { ...spec, default: defaults[k] } : spec]))
  return { ...entry, params }
}

export const materialCatalog: MaterialCatalog = { materials: raw.materials.map(withDefaults), categories: raw.categories, hints: raw.hints }

const byID = new Map(materialCatalog.materials.map((m) => [m.id, m]))

/** The built-in textured materials, in catalog order. */
export const texturedEntries = materialCatalog.materials.filter((m) => m.kind === 'textured')

/**
 * Where a built-in texture's image comes from, as the image cache names a
 * source (materials/images.ts): never an image revision's ID, which is ccrv_….
 */
export const builtinSource = (file: string) => `tex:${file}`

export function materialEntry(id: string): MaterialEntry | undefined {
  return byID.get(id)
}

/** The preset a model's slot hint stands for: lowercased, trimmed, matched whole. */
export function hintPreset(hint: string | null | undefined): string | undefined {
  return hint ? materialCatalog.hints[hint.trim().toLowerCase()] : undefined
}

const colour = /^#[0-9a-fA-F]{6}$/

/**
 * Every parameter of the entry, from params where valid and in range (numbers
 * clamped into it), the default otherwise. The command interface checks only
 * the shape, so old or hand-written slots may hold anything.
 */
export function completeParams(entry: MaterialEntry, params: Record<string, unknown> | undefined): Record<string, ParamValue> {
  const out: Record<string, ParamValue> = {}
  for (const [k, spec] of Object.entries(entry.params)) {
    const v = params?.[k]
    if (spec.type === 'color') out[k] = typeof v === 'string' && colour.test(v) ? v : spec.default
    else if (spec.type === 'boolean') out[k] = typeof v === 'boolean' ? v : spec.default
    else if (typeof v === 'number' && Number.isFinite(v)) out[k] = Math.min(spec.max ?? v, Math.max(spec.min ?? v, v))
    else out[k] = spec.default
  }
  return out
}
