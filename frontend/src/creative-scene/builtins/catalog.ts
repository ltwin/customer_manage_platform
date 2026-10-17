import catalog from '../builtins.gen.json' with { type: 'json' }

// The catalog is generated from backend/internal/creativescene/builtins.json;
// the server validates against the same file.
export type ParamValue = number | boolean | string
export type ParamSpec = {
  type: 'number' | 'integer' | 'boolean' | 'enum' | 'color'
  default: ParamValue
  min?: number
  max?: number
  step?: number
  unit?: string
  options?: string[]
  label?: string
}
export type BuiltinSpec = {
  builtin: string
  label: string
  mount: 'floor' | 'wall' | 'ceiling' | 'free'
  params: Record<string, ParamSpec>
}

export const builtins: readonly BuiltinSpec[] = catalog as unknown as BuiltinSpec[]

const byID = new Map(builtins.map((b) => [b.builtin, b]))

export function builtinSpec(id: string): BuiltinSpec | undefined {
  return byID.get(id)
}

export function defaultParams(id: string): Record<string, ParamValue> {
  const spec = byID.get(id)
  if (!spec) return {}
  return Object.fromEntries(
    Object.entries(spec.params).map(([k, p]) => [k, p.default]),
  )
}

/** Reads a parameter, falling back to the catalog default. */
export function paramNumber(
  params: Record<string, unknown> | undefined,
  spec: BuiltinSpec,
  key: string,
): number {
  const v = params?.[key]
  if (typeof v === 'number' && Number.isFinite(v)) return v
  const d = spec.params[key]?.default
  return typeof d === 'number' ? d : 0
}
