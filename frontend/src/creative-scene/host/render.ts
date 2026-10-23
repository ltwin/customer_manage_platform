import { createElement } from 'react'
import { createRoot, extend } from '@react-three/fiber'
import * as THREE from 'three'
import { readScene, type Scene, type SceneDocument } from '../api.ts'
import { modelCache, type SceneModel } from '../models/cache.ts'
import { drawnFrames, effectiveEngine, engineOf, type Use } from '../render/engines.ts'
import { frameExposure } from '../render/litLook.ts'
import { sceneLowSun, sceneLux } from '../world/exposure.ts'
import { SceneRenderer } from '../render/sceneRenderer.ts'
import { createRenderer, DeviceLostError, gpuOf, ShotTimeout, type SceneGPU } from '../render/gpu.ts'
import { LineRenderer } from '../render/lineArt.ts'
import { backgroundTicker } from './ticker.ts'
import type { Shot } from './executor.ts'
import { framingFor, type Framing, type RenderRequest } from './framing.ts'
import { ShotScene } from './ShotScene.tsx'
import { loadNightAssets } from '../world/nightSky/assets.ts'

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
  /**
   * Draws a picture's frames, yielding to the page between them
   * (SceneGPU.settle), then draws the last and encodes it in one go. The
   * frames are numbered by the loop alone: a hidden tab's picture is the
   * foreground's.
   */
  shoot(framing: Framing, options: ShotOptions): Promise<Blob>
  /** Throws DeviceLostError when the device was lost while the shots so far were drawn; call before handing them on. */
  confirm(): Promise<void>
  /** Mounts the other look and compiles its materials, so its first shot is complete. */
  setLook(look: Look): Promise<void>
  close(): void
}

/** Loads every model the scene shows; one that fails draws as its grey block. */
export async function loadModels(scene: Scene, models: Readonly<Record<string, SceneModel>>): Promise<void> {
  const revisions = new Set(scene.objects.flatMap((o) => (o.model?.content_revision_id && models[o.model.content_revision_id]?.usable !== false ? [o.model.content_revision_id] : [])))
  await Promise.allSettled([...revisions].map((r) => modelCache.load(r)))
}

export async function renderView(documentID: string, request: RenderRequest, read: (id: string) => Promise<SceneDocument> = readScene): Promise<Shot> {
  const doc = await read(documentID)
  const framing = framingFor(doc.scene, doc.models, request)
  if ('error' in framing) return { ok: false, message: framing.error }
  await loadModels(doc.scene, doc.models)
  // The viewport's look; depth_of_field only adds the blur.
  const look: Look = request.mode === 'lit' ? 'lit' : 'clay'
  const shooter = await openShooter(doc.scene, doc.models, look)
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

/** Mounts the scene on a hidden canvas in a look; every shot moves one camera. */
export async function openShooter(scene: Scene, models: Readonly<Record<string, SceneModel>>, look: Look = 'clay'): Promise<Shooter> {
  // The night sky's stars and moon map first (W3 design「资源与确定性」): a picture without them would not be the scene.
  await loadNightAssets(scene).catch((err: unknown) => {
    console.warn('night sky: an asset did not load', err)
    throw new Error(NIGHT_ASSETS_FAILED)
  })
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
    // Materials compile now, in parallel where the browser can, not inside the first shot.
    await g.compile(three, camera)
    let mounted = look
    return {
      maxTextureSize: g.maxTextureSize,
      async setLook(next) {
        if (next === mounted) return
        await mount(next)
        mounted = next
        await g.compile(three, camera)
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
        const frames = drawnFrames(engine, options.use ?? 'export')
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
        try {
          for (let i = 1; i < frames; i++) {
            g.advanceFrame()
            picture!.render(camera, frame)
            await g.settle()
            if (g.isLost()) throw new DeviceLostError()
            if (options.deadline !== undefined && performance.now() > options.deadline) throw new ShotTimeout()
          }
          // The last frame is drawn and encoded in one go (SceneGPU.capture).
          return g.capture(
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
        } finally {
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
