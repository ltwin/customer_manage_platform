import type { components } from '../../api/schema'

// Messages between the editor (host) and the sandbox page, and between the
// sandbox page and its compile Worker. Everything that comes back from the
// sandbox is untrusted: the host checks each field before using it.

export const CONTRACT_VERSION = 1
export const LIB_VERSIONS: readonly number[] = [1]
export const MAX_CODE_BYTES = 64 * 1024
export const MAX_GLB_BYTES = 16 * 1024 * 1024
export const MAX_ERROR_CHARS = 2000
export const COMPILE_TIMEOUT_MS = 5000
export const SANDBOX_PAGE = '/scene-sandbox/sandbox.html'

type GeneratorInput = components['schemas']['CreativeModelGeneratorInput']
export type ParamSpec = components['schemas']['CreativeModelParamSpec']

/** What the Worker read from the module, normalized to plain JSON. */
export type ModuleInfo = Pick<
  GeneratorInput,
  'contract_version' | 'lib_version' | 'mount' | 'params_schema' | 'params'
>

export type CompileErrorKind = 'syntax' | 'runtime' | 'invalid' | 'timeout' | 'limit'
export type CompileError = {
  kind: CompileErrorKind
  message: string
  line?: number
  column?: number
}

export type CompileRequest = {
  type: 'compile'
  id: number
  code: string
  params: Record<string, unknown>
  lib_version: number
}

export type ReadyMessage = { type: 'ready' }
export type CompileResultMessage =
  | { type: 'result'; id: number; ok: true; glb: ArrayBuffer; module: ModuleInfo }
  | { type: 'result'; id: number; ok: false; error: CompileError }
export type SandboxMessage = ReadyMessage | CompileResultMessage

const errorKinds: readonly string[] = ['syntax', 'runtime', 'invalid', 'timeout', 'limit']

export function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}

export function utf8Length(s: string): number {
  return new TextEncoder().encode(s).byteLength
}

export function clip(message: string): string {
  return message.length > MAX_ERROR_CHARS ? message.slice(0, MAX_ERROR_CHARS - 1) + '…' : message
}

export function compileError(
  kind: CompileErrorKind,
  message: string,
  at?: { line: number; column: number },
): CompileError {
  const e: CompileError = { kind, message: clip(message) }
  if (at) {
    e.line = at.line
    e.column = at.column
  }
  return e
}

/** Checks a request on the sandbox side; anything else is ignored. */
export function isCompileRequest(v: unknown): v is CompileRequest {
  return (
    isRecord(v) &&
    v.type === 'compile' &&
    Number.isSafeInteger(v.id) &&
    typeof v.code === 'string' &&
    isRecord(v.params) &&
    typeof v.lib_version === 'number' &&
    LIB_VERSIONS.includes(v.lib_version)
  )
}

function position(v: unknown): boolean {
  return v === undefined || (Number.isSafeInteger(v) && (v as number) >= 0)
}

/**
 * Checks the shape of a message from the sandbox. The module and the GLB are
 * still unchecked content: the host validates them separately.
 */
export function readSandboxMessage(v: unknown): SandboxMessage | null {
  if (!isRecord(v)) return null
  if (v.type === 'ready') return { type: 'ready' }
  if (v.type !== 'result' || !Number.isSafeInteger(v.id)) return null
  const id = v.id as number
  if (v.ok === true) {
    // Size and content are the host's checks (inspect), so they get a reason.
    if (!(v.glb instanceof ArrayBuffer) || !isRecord(v.module)) return null
    return { type: 'result', id, ok: true, glb: v.glb, module: v.module as ModuleInfo }
  }
  if (v.ok !== false || !isRecord(v.error)) return null
  const { kind, message, line, column } = v.error
  if (typeof kind !== 'string' || !errorKinds.includes(kind) || typeof message !== 'string') return null
  if (message.length > MAX_ERROR_CHARS || !position(line) || !position(column)) return null
  const error: CompileError = { kind: kind as CompileErrorKind, message }
  if (line !== undefined) error.line = line as number
  if (column !== undefined) error.column = column as number
  return { type: 'result', id, ok: false, error }
}
