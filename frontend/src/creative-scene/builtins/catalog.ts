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
/** A parameter that can change how the triangles connect; with zero_split only crossing 0 does. */
export type TopologyRule = string | { param: string; zero_split: boolean }
export type BuiltinSpec = {
  builtin: string
  label: string
  mount: 'floor' | 'wall' | 'ceiling' | 'free'
  topology: TopologyRule[]
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

/**
 * Whether going from `old` to `next` may change how the builtin's triangles
 * connect, so its face assignment no longer holds (data: 场景内置基础体
 * 「topology」). The server decides the same way (builtins.go topologyChanged).
 */
export function topologyChanged(spec: BuiltinSpec, old: Record<string, unknown>, next: Record<string, unknown>): boolean {
  return spec.topology.some((rule) => {
    const param = typeof rule === 'string' ? rule : rule.param
    const zeroSplit = typeof rule !== 'string' && rule.zero_split
    const before = old[param] ?? spec.params[param]?.default
    const after = next[param] ?? spec.params[param]?.default
    return zeroSplit ? (before === 0) !== (after === 0) : before !== after
  })
}
