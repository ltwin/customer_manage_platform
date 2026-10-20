import catalog from '../materials.gen.json' with { type: 'json' }
import type { ParamSpec, ParamValue } from '../builtins/catalog.ts'

// The material catalog is generated from
// backend/internal/creativescene/materials.json; the assistant's edits are
// checked against the same file. A published preset never changes.

export type MaterialEntry = {
  id: string
  kind: 'principled' | 'pattern'
  label: string
  params: Record<string, ParamSpec>
}
export type MaterialCatalog = { materials: MaterialEntry[]; hints: Record<string, string> }

export const materialCatalog = catalog as unknown as MaterialCatalog
export const principledID = 'principled@1'

const byID = new Map(materialCatalog.materials.map((m) => [m.id, m]))

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
    else if (typeof v === 'number' && Number.isFinite(v)) out[k] = Math.min(spec.max ?? v, Math.max(spec.min ?? v, v))
    else out[k] = spec.default
  }
  return out
}
