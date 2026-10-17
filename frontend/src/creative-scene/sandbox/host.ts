import { GLBError, inspect, type Manifest } from './inspect.ts'
import {
  COMPILE_TIMEOUT_MS,
  CONTRACT_VERSION,
  LIB_VERSIONS,
  MAX_CODE_BYTES,
  SANDBOX_PAGE,
  compileError,
  isRecord,
  readSandboxMessage,
  utf8Length,
  type CompileError,
  type CompileRequest,
  type ModuleInfo,
  type ParamSpec,
} from './protocol.ts'

// The editor's side of the code sandbox. It owns the sandbox iframe, runs one
// compile at a time with a hard timeout, and treats everything that comes back
// as untrusted: the module info is checked by the server's rules, the GLB is
// inspected and its manifest recomputed, and the code hash is taken from the
// code this side sent.

export type CompileResult =
  | { ok: true; glb: ArrayBuffer; manifest: Manifest; module: ModuleInfo; codeSHA256: string }
  | { ok: false; error: CompileError }

export type CompileInput = { code: string; params: Record<string, unknown>; libVersion: number }

/** One sandbox page instance; destroyed on timeout and replaced. */
export type SandboxFrame = { post(request: CompileRequest): void; destroy(): void }
export type FrameFactory = (onMessage: (data: unknown) => void) => SandboxFrame

const bootTimeoutMs = 10000

/** Hidden `<iframe sandbox="allow-scripts">` frames under `parent`. */
export function iframeFactory(parent: HTMLElement, src = SANDBOX_PAGE): FrameFactory {
  return (onMessage) => {
    const iframe = document.createElement('iframe')
    iframe.setAttribute('sandbox', 'allow-scripts')
    iframe.setAttribute('aria-hidden', 'true')
    iframe.tabIndex = -1
    iframe.style.display = 'none'
    iframe.src = src
    const listener = (e: MessageEvent) => {
      if (e.source !== null && e.source === iframe.contentWindow) onMessage(e.data)
    }
    window.addEventListener('message', listener)
    parent.appendChild(iframe)
    return {
      // The sandbox has an opaque origin, so no narrower target exists; the
      // request carries only the module text and parameter values.
      post: (request) => iframe.contentWindow?.postMessage(request, '*'),
      destroy: () => {
        window.removeEventListener('message', listener)
        iframe.remove()
      },
    }
  }
}

export async function codeSHA256(code: string): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(code))
  return 'sha256-' + Array.from(new Uint8Array(digest), (b) => b.toString(16).padStart(2, '0')).join('')
}

type Exchange = { id: number; resolve: (r: CompileResultOrTimeout) => void }
type CompileResultOrTimeout = ReturnType<typeof readSandboxMessage> | 'timeout'

export class SandboxHost {
  readonly #factory: FrameFactory
  readonly #timeoutMs: number
  #frame: SandboxFrame | null = null
  #generation = 0
  #ready: Promise<boolean> | null = null
  #markReady: (() => void) | null = null
  #pending: Exchange | null = null
  #seq = 0
  #queue: Promise<unknown> = Promise.resolve()
  #disposed = false

  constructor(factory: FrameFactory, timeoutMs = COMPILE_TIMEOUT_MS) {
    this.#factory = factory
    this.#timeoutMs = timeoutMs
  }

  /** Compiles one module; calls run one after another, never together. */
  compile(input: CompileInput): Promise<CompileResult> {
    const run = this.#queue.then(() => this.#compile(input))
    this.#queue = run.catch(() => undefined)
    return run
  }

  dispose(): void {
    this.#disposed = true
    this.#teardown()
  }

  #boot(): void {
    this.#teardown()
    const generation = ++this.#generation
    this.#ready = new Promise<boolean>((resolve) => {
      this.#markReady = () => resolve(true)
      setTimeout(() => resolve(false), bootTimeoutMs)
    })
    this.#frame = this.#factory((data) => {
      if (generation === this.#generation) this.#receive(data)
    })
  }

  #teardown(): void {
    this.#generation++
    this.#frame?.destroy()
    this.#frame = null
    this.#ready = null
    this.#markReady = null
    const pending = this.#pending
    this.#pending = null
    pending?.resolve('timeout')
  }

  #receive(data: unknown): void {
    const m = readSandboxMessage(data)
    if (!m) return
    if (m.type === 'ready') {
      this.#markReady?.()
      return
    }
    const pending = this.#pending
    if (!pending || m.id !== pending.id) return
    this.#pending = null
    pending.resolve(m)
  }

  async #compile({ code, params, libVersion }: CompileInput): Promise<CompileResult> {
    if (this.#disposed) return failure('runtime', '代码沙箱已关闭')
    if (utf8Length(code) > MAX_CODE_BYTES) return failure('limit', '代码超过 64 KiB')
    if (!LIB_VERSIONS.includes(libVersion)) return failure('invalid', `lib_version 须为 ${LIB_VERSIONS.join('、')}`)
    if (!this.#frame) this.#boot()
    if (!(await this.#ready)) {
      this.#boot()
      return failure('timeout', '代码沙箱没有启动')
    }
    const id = ++this.#seq
    const reply = await new Promise<CompileResultOrTimeout>((resolve) => {
      const timer = setTimeout(() => {
        this.#pending = null
        resolve('timeout')
      }, this.#timeoutMs)
      this.#pending = {
        id,
        resolve: (r) => {
          clearTimeout(timer)
          resolve(r)
        },
      }
      this.#frame?.post({ type: 'compile', id, code, params, lib_version: libVersion })
    })
    if (reply === 'timeout' || reply === null || reply.type !== 'result') {
      // The module may still be running: drop the whole page, not just the
      // Worker, and start a fresh one for the next compile.
      if (!this.#disposed) this.#boot()
      return failure('timeout', `编译超过 ${Math.round(this.#timeoutMs / 1000)} 秒，已中止`)
    }
    if (!reply.ok) return { ok: false, error: reply.error }
    let module: ModuleInfo
    try {
      module = checkModule(reply.module)
    } catch (err) {
      return failure('invalid', err instanceof Error ? err.message : String(err))
    }
    let manifest: Manifest
    try {
      manifest = inspect(reply.glb)
    } catch (err) {
      if (!(err instanceof GLBError)) throw err
      return err.reason === 'limit'
        ? failure('limit', `模型超出上限：${err.detail}`)
        : failure('invalid', `模型不符合要求（${err.reason}）：${err.detail}`)
    }
    return { ok: true, glb: reply.glb, manifest, module, codeSHA256: await codeSHA256(code) }
  }
}

function failure(kind: CompileError['kind'], message: string): CompileResult {
  return { ok: false, error: compileError(kind, message) }
}

// The server's rules for a generator's module fields (creativescene
// checkGenerator, paramSpec.check, fillParams), applied to the sandbox's
// untrusted module info. Messages match the server's.
const maxModelParams = 32
const maxEnumOptions = 32
const maxOptionRunes = 64
const maxLabelRunes = 40
const maxParamUnitRunes = 16
const paramNamePattern = /^[A-Za-z_][A-Za-z0-9_]{0,63}$/
const colorPattern = /^#[0-9a-fA-F]{6}$/
const mounts: readonly string[] = ['floor', 'wall', 'ceiling', 'free']
const paramTypes: readonly string[] = ['number', 'integer', 'boolean', 'enum', 'color']

const runes = (s: string) => [...s].length
// JSON null decodes to "absent" on the server.
const present = (v: unknown) => v !== undefined && v !== null

function finite(v: unknown): v is number {
  return typeof v === 'number' && Number.isFinite(v)
}

function optionalNumber(v: unknown, key: string): number | undefined {
  if (!present(v)) return undefined
  if (!finite(v)) throw new Error(`${key} 须为数值`)
  return v
}

function optionalString(v: unknown, key: string): string {
  if (!present(v)) return ''
  if (typeof v !== 'string') throw new Error(`${key} 须为字符串`)
  return v
}

function paramValue(p: ParamSpec, v: unknown): number | boolean | string {
  switch (p.type) {
    case 'number':
    case 'integer':
      if (!finite(v)) throw new Error('须为数值')
      if (p.type === 'integer' && !Number.isInteger(v)) throw new Error('须为整数')
      if (v < (p.min as number) || v > (p.max as number)) throw new Error(`须在 ${p.min} 到 ${p.max} 之间`)
      return v
    case 'boolean':
      if (typeof v !== 'boolean') throw new Error('须为布尔值')
      return v
    case 'enum':
      if (typeof v !== 'string' || !(p.options ?? []).includes(v)) throw new Error('不是可选值')
      return v
    case 'color':
      if (typeof v !== 'string' || !colorPattern.test(v)) throw new Error('须为 #rrggbb 颜色')
      return v
  }
}

/** One parameter definition in canonical form (only the contract's keys). */
function checkSpec(raw: unknown): ParamSpec {
  if (!isRecord(raw)) throw new Error('定义须为对象')
  const type = present(raw.type) ? raw.type : ''
  if (typeof type !== 'string' || !paramTypes.includes(type)) throw new Error(`未知参数类型 ${JSON.stringify(type)}`)
  const spec: ParamSpec = { type: type as ParamSpec['type'], default: undefined }
  const min = optionalNumber(raw.min, 'min')
  const max = optionalNumber(raw.max, 'max')
  const step = optionalNumber(raw.step, 'step')
  const unit = optionalString(raw.unit, 'unit')
  const label = optionalString(raw.label, 'label')
  let options: string[] | undefined
  if (present(raw.options)) {
    if (!Array.isArray(raw.options) || !raw.options.every((o) => o === null || typeof o === 'string')) {
      throw new Error('options 须为字符串数组')
    }
    options = raw.options.map((o: string | null) => o ?? '')
  }
  if (type === 'number' || type === 'integer') {
    if (min === undefined || max === undefined || min > max) throw new Error('须有 min ≤ max')
    if (step !== undefined && step <= 0) throw new Error('step 须大于 0')
    if (options !== undefined) throw new Error('数值参数不能有 options')
    spec.min = min
    spec.max = max
    if (step !== undefined) spec.step = step
  } else {
    if (type === 'enum') {
      if (!options || options.length === 0 || options.length > maxEnumOptions) {
        throw new Error(`options 须为 1 到 ${maxEnumOptions} 项`)
      }
      const seen = new Set<string>()
      for (const o of options) {
        const n = runes(o)
        if (n === 0 || n > maxOptionRunes || seen.has(o)) throw new Error('options 不能为空、过长或重复')
        seen.add(o)
      }
      spec.options = options
    }
    if (min !== undefined || max !== undefined || step !== undefined || unit !== '' || (type !== 'enum' && options !== undefined)) {
      throw new Error(`${type} 参数只能有 default 和 label`)
    }
  }
  if (runes(label) > maxLabelRunes || runes(unit) > maxParamUnitRunes) throw new Error('label 或 unit 过长')
  if (unit !== '') spec.unit = unit
  if (label !== '') spec.label = label
  try {
    spec.default = paramValue(spec, raw.default)
  } catch (err) {
    throw new Error(`默认值${(err as Error).message}`, { cause: err })
  }
  return spec
}

/**
 * Checks the sandbox's module info by the server's rules and returns it in
 * canonical form: definitions with only the contract's keys, every parameter
 * present and valid. Throws with the reason.
 */
export function checkModule(raw: unknown): ModuleInfo {
  if (!isRecord(raw)) throw new Error('模块信息格式不正确')
  if (raw.contract_version !== CONTRACT_VERSION || raw.lib_version !== 1) {
    throw new Error('contract_version 与 lib_version 均须为 1')
  }
  if (typeof raw.mount !== 'string' || !mounts.includes(raw.mount)) throw new Error('mount 须为 floor、wall、ceiling 或 free')
  const schemaRaw = present(raw.params_schema) ? raw.params_schema : {}
  if (!isRecord(schemaRaw)) throw new Error('params_schema 须为对象')
  const names = Object.keys(schemaRaw)
  if (names.length > maxModelParams) throw new Error(`参数不能超过 ${maxModelParams} 个`)
  const schema = new Map<string, ParamSpec>()
  for (const name of names) {
    if (!paramNamePattern.test(name)) throw new Error(`参数名 ${JSON.stringify(name)} 不合法`)
    try {
      schema.set(name, checkSpec(schemaRaw[name]))
    } catch (err) {
      throw new Error(`参数 ${name}：${(err as Error).message}`, { cause: err })
    }
  }
  const given = present(raw.params) ? raw.params : {}
  if (!isRecord(given)) throw new Error('params 须为对象')
  const params = new Map<string, unknown>()
  for (const k of Object.keys(given)) {
    const p = schema.get(k)
    if (!p) throw new Error(`参数 ${JSON.stringify(k)} 没有定义`)
    try {
      params.set(k, paramValue(p, given[k]))
    } catch (err) {
      throw new Error(`参数 ${k} ${(err as Error).message}`, { cause: err })
    }
  }
  for (const [k, p] of schema) if (!params.has(k)) params.set(k, p.default)
  return {
    contract_version: 1,
    lib_version: 1,
    mount: raw.mount as ModuleInfo['mount'],
    params_schema: Object.fromEntries(schema),
    params: Object.fromEntries(params),
  }
}
