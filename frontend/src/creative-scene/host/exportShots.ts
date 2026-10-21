import { ApiError } from '../../api/transport.ts'
import type { components } from '../../api/schema'
import type { Generated, Upload } from '../../creative-canvas/editor/media.ts'
import type { SceneDocument } from '../api.ts'
import { DeviceLostError } from '../render/gpu.ts'
import type { DisplayMode } from '../state/sceneStore.ts'
import { exportLongSide, cameraFraming, type Framing } from './framing.ts'
import type { Look, Shooter, ShotKind } from './render.ts'

// Export (design「导出渲染」「导出接口」): every camera of the plan drawn
// from the scene's confirmed snapshot (E1) as clay, line art and the lit look,
// 2048 on the long side, PNG; then the images go up as staged uploads. The photographer's
// export submits them to the export endpoint; export_shots hands them to the
// job's completion. Whatever goes wrong, the uploads already made are given
// back for the caller to discard.

export type ExportPass = components['schemas']['CreativeScenePass']
export type ExportShots = components['schemas']['CreativeSceneExportShots']
export type ExportPayload = components['schemas']['CreativeSceneExportPayload']
export type ExportResult = components['schemas']['CreativeSceneExportResult']

export type ExportPlan = { cameraObjectIDs: readonly string[]; passes: readonly ExportPass[] }
export type ShotImage = { camera: string; pass: ExportPass; blob: Blob }
export type RenderedShots = { ok: true; revision: string; images: ShotImage[] } | { ok: false; message: string }
export type Progress = { stage: 'render' | 'upload' | 'place'; done: number; total: number }

/** How many images an export makes, and why it cannot go ahead: none, or more than 48. */
export function exportCount(cameras: number, passCount: number): { count: number; problem: string | null } {
  const count = cameras * passCount
  if (count === 0) return { count, problem: cameras === 0 ? '至少选一台摄像机' : '至少选一种导出方式' }
  if (count > maxImages) return { count, problem: `一次最多导出 ${maxImages} 张，请少选一些` }
  return { count, problem: null }
}

const maxImages = 48

/** The passes the export dialog starts with: the lit look too when the view shows materials. */
export const defaultPasses = (display: DisplayMode): ExportPass[] => (display === 'preview' || display === 'rendered' ? ['clay', 'line', 'lit'] : ['clay', 'line'])

/** One image that takes longer than this, rendered and encoded, fails the export. */
export const shotTimeoutMs = 30_000
/** Mounting the lit look and compiling its materials longer than this fails the export. */
export const lookTimeoutMs = 60_000
const uploadConcurrency = 2

export type RenderDeps = {
  read(documentID: string): Promise<SceneDocument>
  loadModels(doc: SceneDocument): Promise<void>
  open(doc: SceneDocument, look: Look): Promise<Shooter>
  now?(): number
  /** Resolves after ms; a timer by default. */
  sleep?(ms: number): Promise<void>
}

/**
 * Draws every image of the plan from the scene's current snapshot: clay and
 * line art of every camera first, then the lit look mounted once for every
 * camera's lit image (design「lit 导出与 render_view」). Images come back in
 * camera × pass order.
 */
export async function renderShots(deps: RenderDeps, documentID: string, plan: ExportPlan, onProgress?: (p: Progress) => void): Promise<RenderedShots> {
  const now = deps.now ?? (() => performance.now())
  const sleep = deps.sleep ?? ((ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms)))
  const doc = await deps.read(documentID)
  const framings: { id: string; framing: Framing }[] = []
  for (const id of plan.cameraObjectIDs) {
    const framing = cameraFraming(doc.scene, id, exportLongSide)
    if ('error' in framing) return { ok: false, message: framing.error }
    framings.push({ id, framing })
  }
  await deps.loadModels(doc)
  const total = framings.length * plan.passes.length
  const clayPasses = plan.passes.filter((p) => p !== 'lit')
  const lit = plan.passes.includes('lit')
  const shooter = await deps.open(doc, clayPasses.length ? 'clay' : 'lit')
  const drawn = new Map<string, Blob>()
  const key = (camera: string, pass: ExportPass) => `${camera}\n${pass}`
  /** Draws one image; false when it took too long. */
  const draw = (id: string, framing: Framing, pass: ExportPass) => {
    onProgress?.({ stage: 'render', done: drawn.size, total })
    const started = now()
    const blob = shooter.shoot(framing, { kind: pass as ShotKind, type: 'image/png', depthOfField: pass !== 'line' && framing.depthOfField })
    if (now() - started > shotTimeoutMs) return false
    drawn.set(key(id, pass), blob)
    return true
  }
  const slow = { ok: false as const, message: `一张导出图渲染超过 ${shotTimeoutMs / 1000} 秒` }
  try {
    for (const { id, framing } of framings) for (const pass of clayPasses) if (!draw(id, framing, pass)) return slow
    if (lit) {
      if (clayPasses.length) {
        const switched = await Promise.race([shooter.setLook('lit').then(() => true), sleep(lookTimeoutMs).then(() => false)])
        if (!switched) return { ok: false, message: `切换到材质光照超过 ${lookTimeoutMs / 1000} 秒` }
      }
      for (const { id, framing } of framings) if (!draw(id, framing, 'lit')) return slow
    }
    // Nothing goes up drawn on a device lost on the way (render.ts).
    await shooter.confirm()
  } finally {
    shooter.close()
  }
  onProgress?.({ stage: 'render', done: total, total })
  const images = framings.flatMap(({ id }) => plan.passes.map((pass) => ({ camera: id, pass, blob: drawn.get(key(id, pass))! })))
  return { ok: true, revision: doc.revision, images }
}

export type Uploaded = { ok: true; uploads: Upload[] } | { ok: false; cause: unknown; uploads: Upload[] }
type UploadFn = (file: File, declared: Generated, signal: AbortSignal) => Promise<Upload>

/** Uploads the images two at a time; `uploads` keeps the images' order. */
export async function uploadShots(upload: UploadFn, images: readonly ShotImage[], signal: AbortSignal, onProgress?: (p: Progress) => void): Promise<Uploaded> {
  const out: (Upload | undefined)[] = new Array(images.length)
  let next = 0
  let done = 0
  let failure: { cause: unknown } | null = null
  const worker = async () => {
    while (failure === null && next < images.length) {
      const i = next++
      const { camera, pass, blob } = images[i]
      try {
        out[i] = await upload(new File([blob], `${camera}-${pass}.png`, { type: 'image/png' }), { kind: 'image', mime: 'image/png' }, signal)
        onProgress?.({ stage: 'upload', done: ++done, total: images.length })
      } catch (cause) {
        failure ??= { cause }
      }
    }
  }
  await Promise.all(Array.from({ length: Math.min(uploadConcurrency, images.length) }, worker))
  const uploads = out.filter((u): u is Upload => u !== undefined)
  return failure === null ? { ok: true, uploads } : { ok: false, cause: (failure as { cause: unknown }).cause, uploads }
}

/** The shots of a request or a completion, from images and their uploads in the same order. */
export function shotsOf(images: readonly ShotImage[], uploads: readonly Upload[]): ExportShots {
  const shots: ExportShots = []
  images.forEach((img, i) => {
    let shot = shots.find((s) => s.camera_object_id === img.camera)
    if (!shot) {
      shot = { camera_object_id: img.camera, images: [] }
      shots.push(shot)
    }
    shot.images.push({ pass: img.pass, upload_id: uploads[i].id })
  })
  return shots
}

export type SubmitOutcome =
  | { ok: true; result: ExportResult }
  /** Refused: the request was wrong or its uploads unusable; nothing was placed. */
  | { ok: false; refused: true; message: string; code?: string }
  /** No answer after the retries; the nodes may or may not be on the canvas. */
  | { ok: false; refused: false; message: string }

export type SubmitDeps = {
  send(operationID: string, createdAt: string, payload: ExportPayload): Promise<ExportResult>
  wait(ms: number): Promise<void>
  newID?(): string
  clock?(): Date
}

const submitAttempts = 4

/**
 * Submits a photographer's export. The operation ID and creation time are
 * made once, so a retry after a lost answer replays the recorded receipt
 * (its hash includes client_created_at) and never places the images twice.
 */
export async function submitExport(deps: SubmitDeps, payload: ExportPayload): Promise<SubmitOutcome> {
  const operation = deps.newID?.() ?? crypto.randomUUID()
  const createdAt = (deps.clock?.() ?? new Date()).toISOString()
  for (let attempt = 1; ; attempt++) {
    try {
      return { ok: true, result: await deps.send(operation, createdAt, payload) }
    } catch (err) {
      if (err instanceof ApiError && err.status < 500) return { ok: false, refused: true, message: err.message, code: err.code }
      if (attempt >= submitAttempts) return { ok: false, refused: false, message: '没能确认导出结果，请先看画布上是否已有这组图' }
      await deps.wait(1000 * 2 ** (attempt - 1))
    }
  }
}

export type ManualExportDeps = RenderDeps & {
  upload: UploadFn
  discard(upload: Upload): Promise<void>
  send: SubmitDeps['send']
  wait(ms: number): Promise<void>
}

export type ManualExport = { documentID: string; canvasID: string; sceneNodeID: string; plan: ExportPlan }
export type ManualOutcome = { ok: true; result: ExportResult } | { ok: false; message: string }

const refusals: Record<string, string> = {
  creative_upload_unavailable: '上传的图已失效，请重新导出',
  archived_read_only: '画布已归档，恢复后再导出',
  not_found: '场景节点已不在画布上，请从画布重新打开场景',
  creative_size_limit: '画布放不下这么多图，请少选几台摄像机或几种方式',
  creative_state_conflict: '场景在回收站，恢复后再导出',
}

/**
 * The photographer's export: draw, upload, place. Uploads that will never
 * be placed are discarded whatever stops it; one already placed refuses the
 * discard, which is harmless.
 */
export async function runManualExport(deps: ManualExportDeps, job: ManualExport, signal: AbortSignal, onProgress?: (p: Progress) => void): Promise<ManualOutcome> {
  const drop = (uploads: readonly Upload[]) => {
    for (const u of uploads) void deps.discard(u).catch(() => {})
  }
  let rendered: RenderedShots
  try {
    rendered = await renderShots(deps, job.documentID, job.plan, onProgress)
  } catch (err) {
    // The device came back or will (render/gpu.ts): nothing was handed on, a retry draws them all.
    if (err instanceof DeviceLostError) return { ok: false, message: '渲染设备暂时不可用，请重试' }
    return { ok: false, message: `导出图渲染失败：${err instanceof Error ? err.message : String(err)}` }
  }
  if (!rendered.ok) return { ok: false, message: rendered.message }
  const sent = await uploadShots(deps.upload, rendered.images, signal, onProgress)
  if (!sent.ok) {
    drop(sent.uploads)
    return { ok: false, message: `导出图上传失败：${sent.cause instanceof Error ? sent.cause.message : String(sent.cause)}` }
  }
  onProgress?.({ stage: 'place', done: 0, total: 1 })
  const out = await submitExport(
    { send: deps.send, wait: deps.wait },
    { canvas_id: job.canvasID, scene_node_id: job.sceneNodeID, scene_revision: rendered.revision, shots: shotsOf(rendered.images, sent.uploads) },
  )
  if (out.ok) return out
  drop(sent.uploads)
  return { ok: false, message: out.refused ? (refusals[out.code ?? ''] ?? out.message) : out.message }
}
