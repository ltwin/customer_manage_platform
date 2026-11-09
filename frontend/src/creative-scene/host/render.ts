import { createElement } from 'react'
import { createRoot, extend } from '@react-three/fiber'
import * as THREE from 'three'
import { readScene, type Scene, type SceneDocument } from '../api.ts'
import { modelCache, type SceneModel } from '../models/cache.ts'
import { drawnFrames, effectiveEngine, engineOf, PROBE_WARMUP, softShadowsIn, traceWaitMs, type Use } from '../render/engines.ts'
import { frameExposure } from '../render/litLook.ts'
import { applyExposure, preExposure, sceneLowSun, sceneLux } from '../world/exposure.ts'
import { SceneRenderer } from '../render/sceneRenderer.ts'
import { createRenderer, DeviceLostError, gpuOf, gpuTestHooks, ShotTimeout, type SceneGPU } from '../render/gpu.ts'
import { LineRenderer } from '../render/lineArt.ts'
import { TraceNotReady, traceBase } from '../render/trace/base.ts'
import { backgroundTicker } from './ticker.ts'
import type { Shot } from './executor.ts'
import { framingFor, type Framing, type RenderRequest } from './framing.ts'
import { ShotScene } from './ShotScene.tsx'
import { loadNightAssets } from '../world/nightSky/assets.ts'
import { loadSceneImages, type ImageHold } from '../materials/images.ts'
import { evaluateAt } from '../timeline/evaluate.ts'

// Pictures of the confirmed scene drawn as the viewport draws it (the clay
// look: the scene's sky and sun with AO; or the lit look: materials, lights,
// the sky's light and the scene's exposure), into a canvas that is never
// shown. Frames are stepped by hand (render/gpu.ts), each picture from frame
// 0 with a pipeline of its own, so a hidden tab without animation frames
// still takes them and every shot of a run is its own. One shooter mounts the
// scene once and loads every model once for all the shots of an export, then
// gives its device back at once.
//
// Images are encoded as they are drawn (SceneGPU.capture, toDataURL), not
// with toBlob: a hidden tab starts toBlob's idle-time encoding only after a
// throttled timeout, and the assistant's exports usually run in one.
//
// A device lost while drawing is told later, in a task of its own, and three
// then draws nothing while the canvas still encodes: pictures are handed on
// only after confirm(), which lets that task run first.

const jpegQuality = 0.85

// <Canvas> registers three's classes for JSX; a bare root has to.
extend(THREE as unknown as Parameters<typeof extend>[0])

/**
 * clay and lit are the viewport's solid and rendered looks, for render_view
 * and the export; line is the export's line art, drawn in either look.
 */
export type ShotKind = 'clay' | 'line' | 'lit'
export type ShotOptions = {
  kind: ShotKind
  type: 'image/jpeg' | 'image/png'
  depthOfField?: boolean
  /** An export's image or render_view's; the real-time GI averages more frames for an export. */
  use?: Use
  /** performance.now() past which the picture is given up, checked between its frames. */
  deadline?: number
  /** JPEG quality; render_view's 0.85 unless said. */
  quality?: number
}
/** What the shooter has mounted: clay and line shots need clay, lit ones lit. */
export type Look = 'clay' | 'lit'

export type Shooter = {
  /** The longest side the device can draw, in pixels. */
  readonly maxTextureSize: number
  /** Whether the lit look's pictures draw the world-space layer (its time limit counts it: engines.ts shotBudgetMs). */
  readonly world: boolean
  /**
   * Draws a picture's frames, yielding to the page between them
   * (SceneGPU.settle), then draws the last and encodes it in one go. The
   * frames are numbered by the loop alone: a hidden tab's picture is the
   * foreground's.
   */
  shoot(framing: Framing, options: ShotOptions): Promise<Blob>
  /** Throws DeviceLostError when the device was lost while the shots so far were drawn; call before handing them on. */
  confirm(): Promise<void>
  /**
   * Mounts the other look and compiles its materials, so its first shot is
   * complete; the lit look in the real-time GI waits for the world-space
   * layer too, and throws TraceNotReady when it is not ready in time.
   */
  setLook(look: Look): Promise<void>
  /**
   * Mounts another scene in the look mounted, a shot at another moment
   * (scene-timeline-shots「导出：每个分镜一套场景」): objects update by ID, the
   * world-space layer packs again what changed and the next picture waits
   * for it as the first did; each picture's probes start afresh anyway.
   */
  setScene(scene: Scene): Promise<void>
  close(): void
}

/** Loads every model the scene shows; one that fails draws as its grey block. */
export async function loadModels(scene: Scene, models: Readonly<Record<string, SceneModel>>): Promise<void> {
  const revisions = new Set(scene.objects.flatMap((o) => (o.model?.content_revision_id && models[o.model.content_revision_id]?.usable !== false ? [o.model.content_revision_id] : [])))
  await Promise.allSettled([...revisions].map((r) => modelCache.load(r, models[r]?.faces)))
}

export async function renderView(documentID: string, request: RenderRequest, read: (id: string) => Promise<SceneDocument> = readScene): Promise<Shot> {
  const doc = await read(documentID)
  // A shot at its frame through its camera; anything else at the frame asked, 0 when none (data: 场景时间线「客户端任务的分镜字段」).
  const shot = request.marker_id ? doc.scene.timeline?.markers.find((m) => m.id === request.marker_id) : undefined
  if (request.marker_id && !shot) return { ok: false, message: `分镜 ${request.marker_id} 已不存在` }
  if (shot && !shot.camera_object_id) return { ok: false, message: '这个分镜没有绑定摄像机' }
  const scene = evaluateAt(doc.scene, shot?.frame ?? request.frame ?? 0)
  const framing = framingFor(scene, doc.models, shot ? { ...request, camera_object_id: shot.camera_object_id } : request)
  if ('error' in framing) return { ok: false, message: framing.error }
  await loadModels(scene, doc.models)
  // The viewport's look; depth_of_field only adds the blur.
  const look: Look = request.mode === 'lit' ? 'lit' : 'clay'
  const shooter = await openShooter(scene, doc.models, look)
  try {
    const image = await shooter.shoot(framing, { kind: look, type: 'image/jpeg', depthOfField: request.depth_of_field === true, use: 'view' })
    await shooter.confirm()
    return { ok: true, image, revision: doc.revision }
  } finally {
    shooter.close()
  }
}

export async function shoot(scene: Scene, models: Readonly<Record<string, SceneModel>>, framing: Framing): Promise<Blob> {
  const shooter = await openShooter(scene, models)
  try {
    const image = await shooter.shoot(framing, { kind: 'clay', type: 'image/jpeg' })
    await shooter.confirm()
    return image
  } finally {
    shooter.close()
  }
}

/** Why a picture failed whose night sky's assets did not load: a deployment or network matter, said plainly. */
export const NIGHT_ASSETS_FAILED = '夜空资源加载失败，请稍后重试'

/** Why a lit picture failed whose slots' images did not load or did not fit the budget; the reason, naming the images, follows. */
export const SCENE_IMAGES_FAILED = '场景贴图不可用'

/**
 * The scene's images, held for a lit picture (C5c1 design「等图片就绪」): only
 * the lit look draws materials, so clay and line art never wait on them.
 */
async function holdImages(scene: Scene): Promise<ImageHold> {
  try {
    return await loadSceneImages(scene)
  } catch (err) {
    throw new Error(`${SCENE_IMAGES_FAILED}：${err instanceof Error ? err.message : String(err)}`)
  }
}

/** Mounts the scene on a hidden canvas in a look; every shot moves one camera. Throws TraceNotReady as setLook. */
export async function openShooter(first: Scene, models: Readonly<Record<string, SceneModel>>, look: Look = 'clay'): Promise<Shooter> {
  let scene = first
  // The night sky's stars and moon map first (W3 design「资源与确定性」): a picture without them would not be the scene.
  await loadNightAssets(scene).catch((err: unknown) => {
    console.warn('night sky: an asset did not load', err)
    throw new Error(NIGHT_ASSETS_FAILED)
  })
  // The device's own copies of the textures go with it in close(); the hold keeps their pixels until then.
  let images: ImageHold | null = look === 'lit' ? await holdImages(scene) : null
  const canvas = document.createElement('canvas')
  const camera = new THREE.PerspectiveCamera()
  camera.up.set(0, 0, 1)
  const root = createRoot(canvas)
  let line: LineRenderer | null = null
  let gpu: SceneGPU | null = null
  const close = () => {
    line?.dispose()
    root.unmount()
    // Give the device back now rather than when the canvas is collected.
    gpu?.dispose()
    images?.release()
    images = null
  }
  try {
    await root.configure({
      gl: ({ canvas }) => createRenderer({ canvas: canvas as HTMLCanvasElement, output: 'pipeline', role: 'offscreen' }).then((g) => g.renderer),
      // Tone mapping and sRGB happen in the pipeline's output node; R3F would set them back on the renderer.
      linear: true,
      flat: true,
      shadows: 'percentage',
      frameloop: 'never',
      dpr: 1,
      size: { width: 1, height: 1, top: 0, left: 0 },
      camera,
    })
    let store: ReturnType<typeof root.render> | undefined
    const mount = (next: Look) =>
      new Promise<void>((resolve, reject) => {
        store = root.render(createElement(ShotScene, { scene, models, look: next, onCommit: resolve, onError: reject }))
      })
    await mount(look)
    const state = store!.getState()
    const g = gpuOf(state.gl)
    gpu = g
    const three = state.scene
    // The real-time GI's lit pictures draw the world-space layer: the device's base packed before the first (D7).
    const traced = (on: Look) => on === 'lit' && effectiveEngine(engineOf(scene.render), 'lit', true) === 'realtime_gi'
    const traceReady = async (on: Look) => {
      if (!traced(on)) return
      const base = traceBase(g, three, { incremental: false })
      if (!(await base.ready(traceWaitMs))) throw new TraceNotReady()
      // Textures past the arrays' layers or the budget: not a picture missing them (design「显存预算」).
      const s = base.state
      if (s.state === 'ready' && s.mapsOver) throw new Error(`${SCENE_IMAGES_FAILED}：${s.mapsOver} 张贴图超出纹理数组的层数或显存预算，屏幕外的光照与反射画不出它们`)
    }
    // Materials compile now, in parallel where the browser can, not inside the first shot.
    await g.compile(three, camera)
    await traceReady(look)
    let mounted = look
    return {
      maxTextureSize: g.maxTextureSize,
      get world() {
        return traced(mounted) && traceBase(g, three, { incremental: false }).state.state === 'ready'
      },
      async setLook(next) {
        if (next === mounted) return
        if (next === 'lit') images ??= await holdImages(scene)
        await mount(next)
        mounted = next
        await g.compile(three, camera)
        await traceReady(next)
      },
      async setScene(next) {
        if (next === scene) return
        await loadNightAssets(next).catch((err: unknown) => {
          console.warn('night sky: an asset did not load', err)
          throw new Error(NIGHT_ASSETS_FAILED)
        })
        // The images the next scene's slots show held before the ones held go.
        if (mounted === 'lit') {
          const held = await holdImages(next)
          images?.release()
          images = held
        }
        scene = next
        await mount(mounted)
        await g.compile(three, camera)
        await traceReady(mounted)
      },
      async shoot(framing, options) {
        if (options.kind !== 'line' && options.kind !== mounted) throw new Error(`拍${options.kind}图前须先切换渲染方式`)
        const { width, height } = framing
        camera.fov = framing.fov
        camera.aspect = width / height
        camera.near = framing.near
        camera.far = framing.far
        camera.position.fromArray(framing.position)
        camera.quaternion.fromArray(framing.quaternion)
        camera.updateProjectionMatrix()
        camera.updateMatrixWorld()
        g.resetFrames()
        // Every picture gets a pipeline of its own: nodes that keep state between frames start afresh.
        const picture = options.kind === 'line' ? null : new SceneRenderer(g, three, camera)
        const engine = options.kind === 'line' ? 'workbench' : effectiveEngine(engineOf(scene.render), options.kind, true)
        const soft = options.kind === 'lit' && softShadowsIn(scene)
        const frames = drawnFrames(engine, options.use ?? 'export', soft)
        // The workbench's frames see one camera: after the first, the shadow maps are what they were, only the
        // kernels turn (scene-light-radius design「workbench 的出图累积」). Drawn again once the picture is done.
        const freezes = engine === 'workbench' && frames > 1
        const frame = {
          width,
          height,
          dof: options.depthOfField && framing.optics ? framing.optics : null,
          // A scene camera at its own exposure, a viewpoint or preset at the default camera's (physical sky), metering E as PhysicalLighting left it.
          ...frameExposure(scene, options.kind === 'lit', framing.exposure, sceneLux(three), sceneLowSun(three)),
          engine,
          quality: 'final' as const,
          accumulate: frames > 1,
        }
        let thaw: (() => void) | null = null
        try {
          // The probes from a cold start before the first frame (S2 design「导出预热与时限」).
          const started = performance.now()
          await picture?.warm(camera, frame, PROBE_WARMUP)
          // The e2e's: the warm-up's time, its work waited for, and what the probes hold after it.
          if (sceneTest && picture) {
            const warmed = picture.probeStats
            if (warmed) {
              await g.settle()
              gpuTestHooks.lastWarm = { ms: performance.now() - started, levels: warmed.levels }
              if (gpuTestHooks.probeDigest) gpuTestHooks.lastWarm.digests = [await digestProbes(picture)]
            } else gpuTestHooks.lastWarm = null
          }
          // Its lit materials built in the background, not in its first frame (SceneRenderer.prepare).
          await picture?.prepare(camera, frame)
          for (let i = 1; i < frames; i++) {
            g.advanceFrame()
            picture!.render(camera, frame)
            if (freezes && !thaw) thaw = freezeShadows(three)
            await g.settle()
            if (g.isLost()) throw new DeviceLostError()
            if (options.deadline !== undefined && performance.now() > options.deadline) throw new ShotTimeout()
          }
          // The last frame is drawn and encoded in one go (SceneGPU.capture).
          const shot = await g.capture(
            () => {
              g.advanceFrame()
              if (picture) {
                picture.render(camera, frame)
              } else {
                g.renderer.setPixelRatio(1)
                g.renderer.setSize(width, height, false)
                line ??= new LineRenderer()
                line.render(g, three, camera, width, height)
              }
            },
            options.type,
            options.type === 'image/jpeg' ? (options.quality ?? jpegQuality) : undefined,
          )
          if (sceneTest && picture?.probeStats?.drawn) {
            gpuTestHooks.picturesWithProbes++
            if (gpuTestHooks.lastWarm) gpuTestHooks.lastWarm.after = picture.probeStats
            if (gpuTestHooks.lastWarm?.digests) gpuTestHooks.lastWarm.digests.push(await digestProbes(picture))
          }
          // The e2e's per-pixel reference (S2 AC-S2-2) on the probes this picture drew with, at its exposure.
          const asked = gpuTestHooks.probeRegions
          const world = asked && !asked.result ? picture?.world : null
          if (asked && world) {
            const { probeCheck } = await import('../render/trace/checks.ts')
            applyExposure(three, frame.preExposure ?? 1)
            try {
              asked.result = await probeCheck(g.renderer, three, camera, world.handle, world.volume, asked)
            } finally {
              preExposure.value = 1
            }
          }
          return shot
        } finally {
          thaw?.()
          picture?.dispose()
        }
      },
      async confirm() {
        await backgroundTicker().wait(0)
        if (g.isLost()) throw new DeviceLostError()
      },
      close,
    }
  } catch (err) {
    close()
    throw err
  }
}

type Shadowed = THREE.Light & { shadow?: THREE.LightShadow & { shadowNode?: { lights?: { shadow?: THREE.LightShadow }[] } } }

/** Every casting light's shadow maps, a sun's cascades with them, kept as drawn; returns the undo. */
function freezeShadows(three: THREE.Object3D): () => void {
  const held: THREE.LightShadow[] = []
  three.traverse((o) => {
    const l = o as Shadowed
    if (!l.isLight || !l.castShadow || !l.shadow) return
    for (const s of [l.shadow, ...(l.shadow.shadowNode?.lights ?? []).map((c) => c.shadow)]) {
      if (s?.autoUpdate) {
        s.autoUpdate = false
        held.push(s)
      }
    }
  })
  return () => {
    for (const s of held) s.autoUpdate = true
  }
}

/** The e2e fixture page sets __sceneTest before the editor loads (render/gpu.ts): what shoot keeps for it alone. */
const sceneTest = (globalThis as { __sceneTest?: boolean }).__sceneTest === true

/** e2e only (gpuTestHooks.probeDigest): an FNV-1a hash of each of a picture's probe buffers as read back. */
async function digestProbes(picture: SceneRenderer): Promise<string> {
  const v = picture.heldProbes
  if (!v) return ''
  const read = await v.inspect()
  return [read.irradiance, read.offsets, read.cells, read.ages]
    .map((a) => {
      const bytes = new Uint8Array(a.buffer, a.byteOffset, a.byteLength)
      let h = 0x811c9dc5
      for (let i = 0; i < bytes.length; i++) h = Math.imul(h ^ bytes[i], 0x01000193) >>> 0
      return h.toString(16)
    })
    .join('/')
}
