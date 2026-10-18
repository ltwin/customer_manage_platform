import type { Upload } from '../../creative-canvas/editor/media.ts'
import type { CompileInput, CompileResult } from '../sandbox/host.ts'
import type { Manifest } from '../sandbox/inspect.ts'
import { measureStage, type CompileError } from '../sandbox/protocol.ts'

// The bake pipeline shared by the editor's own model edits and the
// assistant's jobs: compile in the sandbox, thumbnail the checked GLB, upload
// both as staged files. Between stages it asks whether the bake is still
// wanted; a stopped or failed bake hands back the uploads that finished, for
// the caller to free.

export type Declared = { kind: 'model'; mime: 'model/gltf-binary' } | { kind: 'image'; mime: 'image/png' }

export type BakeDeps = {
  compile(input: CompileInput): Promise<CompileResult>
  thumbnail(glb: ArrayBuffer, manifest: Manifest): Promise<Blob>
  upload(file: File, declared: Declared, signal: AbortSignal): Promise<Upload>
}

export type Compiled = Extract<CompileResult, { ok: true }>
export type Baked = Compiled & { png: Blob; model: Upload; thumbnail: Upload }

export type BakeHooks = {
  /** Asked after every stage; false stops the bake. */
  live(): boolean
  compiled?(out: Compiled): void
  uploading?(): void
}

/** Every outcome lists the ready uploads it leaves: the bake's two, or what a failed one got to. */
export type BakeOutcome =
  | { ok: true; baked: Baked; uploads: Upload[] }
  | { ok: false; stopped: true; uploads: Upload[] }
  | { ok: false; stopped?: false; stage: 'compile'; error: CompileError; uploads: Upload[] }
  | { ok: false; stopped?: false; stage: 'thumbnail'; cause: unknown; uploads: Upload[] }
  | { ok: false; stopped?: false; stage: 'upload'; cause: unknown; uploads: Upload[] }

export async function bake(deps: BakeDeps, input: CompileInput, signal: AbortSignal, hooks: BakeHooks): Promise<BakeOutcome> {
  const stopped = (uploads: Upload[]): BakeOutcome => ({ ok: false, stopped: true, uploads })
  let started = performance.now()
  const compiled = await deps.compile(input)
  if (!hooks.live()) return stopped([])
  if (!compiled.ok) return { ok: false, stage: 'compile', error: compiled.error, uploads: [] }
  hooks.compiled?.(compiled)
  measureStage('compile', started)

  started = performance.now()
  let png: Blob
  try {
    png = await deps.thumbnail(compiled.glb, compiled.manifest)
  } catch (cause) {
    return hooks.live() ? { ok: false, stage: 'thumbnail', cause, uploads: [] } : stopped([])
  }
  if (!hooks.live()) return stopped([])
  measureStage('thumbnail', started)

  hooks.uploading?.()
  started = performance.now()
  const sent = await uploadBake(deps, compiled.glb, png, signal)
  if (!hooks.live()) return stopped(sent.uploads)
  if (!sent.ok) return { ok: false, stage: 'upload', cause: sent.cause, uploads: sent.uploads }
  measureStage('upload', started)
  const [model, thumbnail] = sent.uploads
  return { ok: true, baked: { ...compiled, png, model, thumbnail }, uploads: sent.uploads }
}

/** Uploads a bake's GLB and thumbnail together; also used to send them again. */
export async function uploadBake(
  deps: Pick<BakeDeps, 'upload'>,
  glb: ArrayBuffer,
  png: Blob,
  signal: AbortSignal,
): Promise<{ ok: true; uploads: [Upload, Upload] } | { ok: false; cause: unknown; uploads: Upload[] }> {
  const results = await Promise.allSettled([
    deps.upload(new File([glb], 'model.glb', { type: 'model/gltf-binary' }), { kind: 'model', mime: 'model/gltf-binary' }, signal),
    deps.upload(new File([png], 'thumbnail.png', { type: 'image/png' }), { kind: 'image', mime: 'image/png' }, signal),
  ])
  const uploads = results.flatMap((r) => (r.status === 'fulfilled' ? [r.value] : []))
  const rejected = results.find((r) => r.status === 'rejected')
  if (rejected) return { ok: false, cause: rejected.reason, uploads }
  return { ok: true, uploads: uploads as [Upload, Upload] }
}
