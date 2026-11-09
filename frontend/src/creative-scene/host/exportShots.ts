import { ApiError } from '../../api/transport.ts'
import type { components } from '../../api/schema'
import type { Generated, Upload } from '../../creative-canvas/editor/media.ts'
import type { Scene, SceneDocument } from '../api.ts'
import { DeviceLostError, ShotTimeout } from '../render/gpu.ts'
import { TraceNotReady } from '../render/trace/base.ts'
import type { DisplayMode } from '../state/sceneStore.ts'
import { cameraFraming, type Framing } from './framing.ts'
import { drawnFrames, effectiveEngine, engineOf, extensionOf, imageTypeOf, longSideOf, lookBudgetMs, shotBudgetMs, softShadowsIn } from '../render/engines.ts'
import type { Look, Shooter, ShotKind } from './render.ts'
import { evaluateAt } from '../timeline/evaluate.ts'
import { shotTitle } from '../timeline/keys.ts'

// Export (design「导出渲染」「导出接口」): every camera or shot of the plan
// drawn from the scene's confirmed snapshot (E1), a shot's at its frame
// through its camera (scene-timeline-shots「导出：每个分镜一套场景」), as clay, line art and the lit look,
// 2048 on the long side, PNG; then the images go up as staged uploads. The photographer's
// export submits them to the export endpoint; export_shots hands them to the
// job's completion. Whatever goes wrong, the uploads already made are given
// back for the caller to discard.

export type ExportPass = components['schemas']['CreativeScenePass']
export type ExportShots = components['schemas']['CreativeSceneExportShots']
export type ExportPayload = components['schemas']['CreativeSceneExportPayload']
export type ExportResult = components['schemas']['CreativeSceneExportResult']

/** By shot when markerIDs is given, else by camera at frame 0. */
export type ExportPlan = { cameraObjectIDs: readonly string[]; markerIDs?: readonly string[]; passes: readonly ExportPass[] }
/** marker: the shot it is of, in an export by shot. */
export type ShotImage = { camera: string; marker?: string; pass: ExportPass; blob: Blob }
/** hostError: the device cannot draw the chosen resolution; an assistant's job ends as host_error, not back in the queue. */
export type RenderedShots = { ok: true; revision: string; images: ShotImage[] } | { ok: false; message: string; hostError?: true }
export type Progress = { stage: 'render' | 'upload' | 'place'; done: number; total: number }

/** How many images an export makes, and why it cannot go ahead: none, more than 32 shots or cameras, or more than 48 images. */
export function exportCount(cameras: number, passCount: number, byShot = false): { count: number; problem: string | null } {
  const count = cameras * passCount
  if (count === 0) return { count, problem: cameras === 0 ? (byShot ? '至少选一个分镜' : '至少选一台摄像机') : '至少选一种导出方式' }
  if (cameras > maxRows) return { count, problem: `一次最多导出 ${maxRows} ${byShot ? '个分镜' : '台摄像机'}，请少选一些` }
  if (count > maxImages) return { count, problem: `一次最多导出 ${maxImages} 张，请少选一些` }
  return { count, problem: null }
}

// The server's limits on an export (contracts: shots 1–32, images 48).
const maxRows = 32
const maxImages = 48

/** The passes the export dialog starts with: the lit look too when the view shows materials. */
export const defaultPasses = (display: DisplayMode): ExportPass[] => (display === 'preview' || display === 'rendered' ? ['clay', 'line', 'lit'] : ['clay', 'line'])

const uploadConcurrency = 2

/**
 * What a picture of the scene depends on besides its camera (design「出图场景键」):
 * the evaluated environment, the render settings but the output resolution,
 * and every object but the cameras, whole; a camera only with its transform,
 * and only when something not a camera hangs under it. Shots with one key
 * share one mounted scene and its acceleration structures, two cameras at
 * one moment for instance.
 */
export function shotSceneKey(scene: Scene): string {
  const { resolution: _, ...render } = scene.render
  const byID = new Map(scene.objects.map((o) => [o.id, o]))
  const carrying = new Set<string>()
  for (const o of scene.objects) {
    if (o.kind === 'camera') continue
    for (let p = o.parent_id ? byID.get(o.parent_id) : undefined, n = 0; p && n < scene.objects.length; p = p.parent_id ? byID.get(p.parent_id) : undefined, n++)
      if (p.kind === 'camera') carrying.add(p.id)
  }
  const objects = scene.objects.flatMap((o) => (o.kind !== 'camera' ? [o] : carrying.has(o.id) ? [{ id: o.id, parent_id: o.parent_id, transform: o.transform }] : []))
  return JSON.stringify({ environment: scene.environment, render, objects })
}

/** One picture's subject: a camera at frame 0, or a shot at its frame through its camera. */
type Target = { id: string; camera: string; marker?: string; scene: Scene; key: string; framing: Framing }

/** The plan's targets, in its order; the reason when one cannot be drawn. */
function targetsOf(doc: SceneDocument, plan: ExportPlan, longSide: number): Target[] | { error: string } {
  const evaluated = new Map<number, { scene: Scene; key: string }>()
  const at = (frame: number) => {
    let e = evaluated.get(frame)
    if (!e) {
      const scene = evaluateAt(doc.scene, frame)
      e = { scene, key: shotSceneKey(scene) }
      evaluated.set(frame, e)
    }
    return e
  }
  const markers = doc.scene.timeline?.markers ?? []
  const subjects = plan.markerIDs?.length
    ? plan.markerIDs.map((id) => {
        const m = markers.find((x) => x.id === id)
        return m ? { id, marker: id, camera: m.camera_object_id, frame: m.frame, title: shotTitle(markers, id) } : { id, marker: id, camera: '', frame: -1, title: '' }
      })
    : plan.cameraObjectIDs.map((id) => ({ id, marker: undefined, camera: id, frame: 0, title: '' }))
  const out: Target[] = []
  for (const s of subjects) {
    if (s.frame < 0) return { error: `分镜 ${s.id} 已不存在` }
    if (!s.camera) return { error: `「${s.title}」没有绑定摄像机` }
    const { scene, key } = at(s.frame)
    const framing = cameraFraming(scene, s.camera, longSide)
    if ('error' in framing) return { error: s.marker ? `「${s.title}」：${framing.error}` : framing.error }
    out.push({ id: s.id, camera: s.camera, ...(s.marker ? { marker: s.marker } : {}), scene, key, framing })
  }
  return out
}

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
 * line art of every target first, then the lit look mounted once for every
 * target's lit image (design「lit 导出与 render_view」). Targets with the same
 * scene key are drawn one after another on one mounted scene; another key
 * mounts that scene (Shooter.setScene). Images come back in target × pass
 * order.
 */
export async function renderShots(deps: RenderDeps, documentID: string, plan: ExportPlan, onProgress?: (p: Progress) => void): Promise<RenderedShots> {
  const now = deps.now ?? (() => performance.now())
  const sleep = deps.sleep ?? ((ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms)))
  const doc = await deps.read(documentID)
  const { resolution } = doc.scene.render
  const longSide = longSideOf(resolution)
  const planned = targetsOf(doc, plan, longSide)
  if ('error' in planned) return { ok: false, message: planned.error }
  // Drawn grouped by scene, each group where its first target is.
  const keys = [...new Set(planned.map((t) => t.key))]
  const targets = keys.flatMap((k) => planned.filter((t) => t.key === k))
  await deps.loadModels(doc)
  const total = targets.length * plan.passes.length
  const clayPasses = plan.passes.filter((p) => p !== 'lit')
  const lit = plan.passes.includes('lit')
  // Each image's limit follows what it costs (R2 design「输出分辨率、编码与时限」): its pixels, the frames its
  // renderer averages, its encoding, whether it draws the world-space layer; the whole export gets the lit look's
  // switch, each other scene's mounting, and every image's limit, as if the layer were drawn.
  const budgetOf = ({ framing, scene }: Target, pass: ExportPass, world: boolean) => {
    const engine = effectiveEngine(engineOf(scene.render), pass, true)
    return shotBudgetMs(engine, framing.width, framing.height, drawnFrames(engine, 'export', pass === 'lit' && softShadowsIn(scene)), imageTypeOf(pass, resolution).type, world && pass === 'lit')
  }
  const looks = (clayPasses.length ? 1 : 0) + (lit ? 1 : 0)
  const totalMs = lookBudgetMs * (1 + (keys.length - 1) * looks) + targets.reduce((sum, t) => sum + plan.passes.reduce((s, pass) => s + budgetOf(t, pass, true), 0), 0)
  const began = now()
  const shooter = await deps.open(targets[0] ? { ...doc, scene: targets[0].scene } : doc, clayPasses.length ? 'clay' : 'lit')
  // A device that cannot draw the resolution fails at once rather than drawing smaller (R2 design R-5).
  if (longSide > shooter.maxTextureSize) {
    shooter.close()
    return { ok: false, message: `当前设备最大只能导出长边 ${shooter.maxTextureSize} px 的图，请选更低的输出分辨率`, hostError: true }
  }
  const drawn = new Map<string, Blob>()
  const key = (target: string, pass: ExportPass) => `${target}\n${pass}`
  let mounted = targets[0]?.key
  /** Mounts the target's scene unless it is the one mounted; the message when that took too long. */
  const mount = async (t: Target): Promise<string | null> => {
    if (t.key === mounted) return null
    const done = await Promise.race([shooter.setScene(t.scene).then(() => true), sleep(lookBudgetMs).then(() => false)])
    if (!done) return `换到下一个分镜的场景超过 ${lookBudgetMs / 1000} 秒`
    mounted = t.key
    return null
  }
  /** Draws one image; the message when it took too long. */
  const draw = async (shot: Target, pass: ExportPass): Promise<string | null> => {
    onProgress?.({ stage: 'render', done: drawn.size, total })
    const late = await mount(shot)
    if (late) return late
    const budget = budgetOf(shot, pass, shooter.world)
    const started = now()
    const slow = `一张导出图渲染超过 ${Math.round(budget / 1000)} 秒`
    let blob: Blob
    try {
      const encoding = imageTypeOf(pass, resolution)
      // The shooter checks the deadline against its own clock, performance.now() (host/render.ts); `now` times the export.
      blob = await shooter.shoot(shot.framing, { kind: pass as ShotKind, type: encoding.type, quality: encoding.quality, depthOfField: pass !== 'line' && shot.framing.depthOfField, deadline: performance.now() + budget })
    } catch (err) {
      if (err instanceof ShotTimeout) return slow
      throw err
    }
    if (now() - started > budget) return slow
    if (now() - began > totalMs) return `导出超过 ${Math.round(totalMs / 1000)} 秒没有完成`
    drawn.set(key(shot.id, pass), blob)
    return null
  }
  try {
    for (const shot of targets) {
      for (const pass of clayPasses) {
        const slow = await draw(shot, pass)
        if (slow) return { ok: false, message: slow }
      }
    }
    if (lit) {
      if (clayPasses.length) {
        const switched = await Promise.race([shooter.setLook('lit').then(() => true), sleep(lookBudgetMs).then(() => false)])
        if (!switched) return { ok: false, message: `切换到材质光照超过 ${lookBudgetMs / 1000} 秒` }
      }
      for (const shot of targets) {
        const slow = await draw(shot, 'lit')
        if (slow) return { ok: false, message: slow }
      }
    }
    // Nothing goes up drawn on a device lost on the way (render.ts).
    await shooter.confirm()
  } catch (err) {
    // At 4K and 8K a lost device is taken as one that cannot draw so large: no retry (R2 design R-5).
    if (err instanceof DeviceLostError && longSide >= 4096) {
      return { ok: false, message: `渲染设备在按 ${resolution.toUpperCase()} 出图时丢失，当前设备可能画不了这个分辨率，可以换低一档再导出`, hostError: true }
    }
    throw err
  } finally {
    shooter.close()
  }
  onProgress?.({ stage: 'render', done: total, total })
  const images = planned.flatMap(({ id, camera, marker }) => plan.passes.map((pass) => ({ camera, ...(marker ? { marker } : {}), pass, blob: drawn.get(key(id, pass))! })))
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
      const { camera, marker, pass, blob } = images[i]
      try {
        // PNG or JPEG as it was encoded (render/engines.ts imageTypeOf).
        const mime = blob.type === 'image/jpeg' ? 'image/jpeg' : 'image/png'
        out[i] = await upload(new File([blob], `${marker ?? camera}-${pass}.${extensionOf(mime)}`, { type: mime }), { kind: 'image', mime }, signal)
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
    let shot = img.marker ? shots.find((s) => s.marker_id === img.marker) : shots.find((s) => !s.marker_id && s.camera_object_id === img.camera)
    if (!shot) {
      shot = img.marker ? { marker_id: img.marker, images: [] } : { camera_object_id: img.camera, images: [] }
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
    // The world-space layer is still being built (D7): it goes on, and a retry usually finds it ready.
    if (err instanceof TraceNotReady) return { ok: false, message: err.message }
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
