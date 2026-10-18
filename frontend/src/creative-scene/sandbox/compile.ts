import { parse } from 'acorn'
import {
  compileError,
  isRecord,
  type CompileError,
  type ModuleInfo,
  type ParamSpec,
} from './protocol.ts'

// The compile Worker's steps that do not need three.js: checking the module
// text, normalizing what the module declares, and locating runtime errors.

export class CompileFailure extends Error {
  readonly detail: CompileError
  constructor(detail: CompileError) {
    super(detail.message)
    this.detail = detail
  }
}

export function invalid(message: string, at?: { line: number; column: number }): CompileFailure {
  return new CompileFailure(compileError('invalid', message, at))
}

type Located = { type: string; start: number; loc?: { start: { line: number; column: number } } | null }

// Statements that would load another module. Dependencies are injected, so
// none is allowed: not even one CSP would block.
function loadsModule(n: Record<string, unknown>): boolean {
  return (
    n.type === 'ImportDeclaration' ||
    n.type === 'ImportExpression' ||
    ((n.type === 'ExportNamedDeclaration' || n.type === 'ExportAllDeclaration') && n.source != null)
  )
}

function firstImport(program: unknown): Located | null {
  let first: Located | null = null
  const stack: unknown[] = [program]
  while (stack.length > 0) {
    const n = stack.pop()
    if (Array.isArray(n)) {
      stack.push(...n)
      continue
    }
    if (!isRecord(n) || typeof n.type !== 'string') continue
    if (loadsModule(n) && (first === null || (n.start as number) < first.start)) first = n as Located
    for (const [k, v] of Object.entries(n)) if (k !== 'loc' && typeof v === 'object' && v !== null) stack.push(v)
  }
  return first
}

/** Step 1: parses the module so syntax errors carry a line and column. */
export function checkSyntax(code: string): void {
  let program: unknown
  try {
    program = parse(code, { ecmaVersion: 'latest', sourceType: 'module', locations: true })
  } catch (err) {
    const loc = isRecord(err) && isRecord(err.loc) ? err.loc : null
    const message = err instanceof Error ? err.message.replace(/ \(\d+:\d+\)$/, '') : String(err)
    const at = loc && typeof loc.line === 'number' && typeof loc.column === 'number'
      ? { line: loc.line, column: loc.column + 1 }
      : undefined
    throw new CompileFailure(compileError('syntax', message, at))
  }
  const found = firstImport(program)
  if (found) {
    const start = found.loc?.start
    throw invalid(
      '模块不能导入其他模块，THREE、lib 和 rng 由沙箱注入',
      start ? { line: start.line, column: start.column + 1 } : undefined,
    )
  }
}

const specKeys = ['type', 'default', 'min', 'max', 'step', 'unit', 'options', 'label'] as const

/**
 * Step 4a: the module's `params` as plain JSON with only the keys the contract
 * defines. Whether the definitions are valid is the host's check, by the
 * server's rules.
 */
export function normalizeSchema(raw: unknown): Record<string, ParamSpec> {
  if (raw === undefined) return {}
  if (!isRecord(raw)) throw invalid('export const params 须为对象')
  const entries: [string, ParamSpec][] = []
  for (const name of Object.keys(raw)) {
    const spec: unknown = raw[name]
    if (!isRecord(spec)) throw invalid(`参数 ${name} 的定义须为对象`)
    const picked: Record<string, unknown> = {}
    for (const k of specKeys) if (spec[k] !== undefined) picked[k] = spec[k]
    let plain: unknown
    try {
      plain = JSON.parse(JSON.stringify(picked))
    } catch {
      throw invalid(`参数 ${name} 的定义须为 JSON 值`)
    }
    entries.push([name, plain as ParamSpec])
  }
  // fromEntries keeps a parameter named __proto__ an ordinary key.
  return Object.fromEntries(entries)
}

const colorPattern = /^#[0-9a-fA-F]{6}$/

function value(spec: ParamSpec, v: unknown): unknown {
  switch (spec.type) {
    case 'number':
    case 'integer': {
      if (typeof v !== 'number' || !Number.isFinite(v)) return spec.default
      let n = spec.type === 'integer' ? Math.round(v) : v
      if (typeof spec.min === 'number') n = Math.max(n, spec.min)
      if (typeof spec.max === 'number') n = Math.min(n, spec.max)
      return n
    }
    case 'boolean':
      return typeof v === 'boolean' ? v : spec.default
    case 'enum':
      return typeof v === 'string' && Array.isArray(spec.options) && spec.options.includes(v) ? v : spec.default
    case 'color':
      return typeof v === 'string' && colorPattern.test(v) ? v : spec.default
    default:
      return spec.default
  }
}

/**
 * Step 4b: one value per definition. Given values without a definition are
 * dropped, out-of-range numbers are clamped, and anything else unusable falls
 * back to the default.
 */
export function fillParams(schema: Record<string, ParamSpec>, given: Record<string, unknown>): Record<string, unknown> {
  return Object.fromEntries(
    Object.entries(schema).map(([name, spec]) => [name, value(spec, Object.hasOwn(given, name) ? given[name] : undefined)]),
  )
}

const mounts: readonly string[] = ['floor', 'wall', 'ceiling', 'free']

/** Step 3–4: the module's exports as the module info, without the parameter values. */
export function readModule(
  exports: Record<string, unknown>,
  requestedLib: number,
  libVersions: readonly number[],
  contractVersion: number,
): Omit<ModuleInfo, 'params'> {
  const lib = exports.lib_version ?? requestedLib
  if (typeof lib !== 'number' || !libVersions.includes(lib)) {
    throw invalid(`lib_version 须为 ${libVersions.join('、')}`)
  }
  const mount = exports.mount
  if (typeof mount !== 'string' || !mounts.includes(mount)) {
    throw invalid("export const mount 须为 'floor'、'wall'、'ceiling' 或 'free'")
  }
  if (typeof exports.default !== 'function') throw invalid('须有 export default function build(...)')
  return {
    contract_version: contractVersion as ModuleInfo['contract_version'],
    lib_version: lib as ModuleInfo['lib_version'],
    mount: mount as ModuleInfo['mount'],
    params_schema: normalizeSchema(exports.params),
  }
}

/**
 * The innermost stack frame in the module, found by its Blob URL. Chromium
 * writes `at f (blob:null/…:3:9)`, WebKit `f@blob:null/…:3:9`.
 */
export function locate(stack: string, moduleURL: string): { line: number; column: number } | undefined {
  const i = stack.indexOf(moduleURL + ':')
  if (i < 0) return undefined
  const m = /^:(\d+):(\d+)/.exec(stack.slice(i + moduleURL.length))
  return m ? { line: Number(m[1]), column: Number(m[2]) } : undefined
}

/** A thrown value as a runtime error; reading it never throws. */
export function runtimeError(err: unknown, moduleURL: string): CompileError {
  if (err instanceof CompileFailure) return err.detail
  let message = '未知错误'
  let stack = ''
  try {
    message = err instanceof Error ? `${err.name}: ${err.message}` : String(err)
    stack = err instanceof Error && typeof err.stack === 'string' ? err.stack : ''
  } catch {
    // A thrown object whose accessors throw keeps the generic message.
  }
  const kind = err instanceof SyntaxError && !stack.includes(moduleURL) ? 'syntax' : 'runtime'
  return compileError(kind, message, locate(stack, moduleURL))
}

/** Seeded generator (mulberry32); the sequence is part of lib@1's determinism. */
export function seeded(seed: number): () => number {
  let s = seed >>> 0
  return () => {
    s = (s + 0x6d2b79f5) >>> 0
    let t = s
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}
