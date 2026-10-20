import { createElement } from 'react'
import { createRoot, extend } from '@react-three/fiber'
import * as THREE from 'three'
import { readScene, type Scene, type SceneDocument } from '../api.ts'
import { modelCache, type SceneModel } from '../models/cache.ts'
import { exposureOf } from '../render/litLook.ts'
import { SceneRenderer } from '../render/sceneRenderer.ts'
import { LineRenderer } from '../render/linePass.ts'
import { canvasBlob } from '../sandbox/thumbnail.ts'
import type { Shot } from './executor.ts'
import { framingFor, type Framing, type RenderRequest } from './framing.ts'
import { ShotScene } from './ShotScene.tsx'

// Pictures of the confirmed scene drawn as the viewport draws it (the clay
// look: the scene's sky and sun with AO; or the lit look: materials, lights,
// the sky's light and the scene's exposure), into a canvas that is never
// shown. Frames are drawn
// by hand, so a hidden tab without animation frames still takes them. One
// shooter mounts the scene once and loads every model once for all the shots
// of an export, then gives its WebGL context back at once: browsers cap live
// contexts and the editor may hold one.
//
// Images are encoded with canvasBlob (toDataURL), not toBlob: a hidden tab
// starts toBlob's idle-time encoding only after a throttled timeout, and the
// assistant's exports usually run in one.

const jpegQuality = 0.85

// <Canvas> registers three's classes for JSX; a bare root has to.
extend(THREE as unknown as Parameters<typeof extend>[0])

/**
 * clay and lit are the viewport's solid and rendered looks, for render_view
 * and the export; line is the export's line art, drawn in either look.
 */
export type ShotKind = 'clay' | 'line' | 'lit'
export type ShotOptions = { kind: ShotKind; type: 'image/jpeg' | 'image/png'; depthOfField?: boolean }
/** What the shooter has mounted: clay and line shots need clay, lit ones lit. */
export type Look = 'clay' | 'lit'

export type Shooter = {
  shoot(framing: Framing, options: ShotOptions): Blob
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
    const image = shooter.shoot(framing, { kind: look, type: 'image/jpeg', depthOfField: request.depth_of_field === true })
    return { ok: true, image, revision: doc.revision }
  } finally {
    shooter.close()
  }
}

export async function shoot(scene: Scene, models: Readonly<Record<string, SceneModel>>, framing: Framing): Promise<Blob> {
  const shooter = await openShooter(scene, models)
  try {
    return shooter.shoot(framing, { kind: 'clay', type: 'image/jpeg' })
  } finally {
    shooter.close()
  }
}

/** Mounts the scene on a hidden canvas in a look; every shot moves one camera. */
export async function openShooter(scene: Scene, models: Readonly<Record<string, SceneModel>>, look: Look = 'clay'): Promise<Shooter> {
  const canvas = document.createElement('canvas')
  let contextLost = false
  canvas.addEventListener('webglcontextlost', () => (contextLost = true), { once: true })
  const camera = new THREE.PerspectiveCamera()
  camera.up.set(0, 0, 1)
  const root = createRoot(canvas)
  let clay: SceneRenderer | null = null
  let line: LineRenderer | null = null
  let gl: THREE.WebGLRenderer | null = null
  const close = () => {
    clay?.dispose()
    line?.dispose()
    root.unmount()
    // Give the context back now rather than when the canvas is collected.
    gl?.forceContextLoss()
  }
  try {
    await root.configure({
      gl: { preserveDrawingBuffer: true, antialias: true },
      shadows: true,
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
    gl = state.gl
    const three = state.scene
    // Materials compile now, in parallel where the browser can, not inside the first shot.
    if (look === 'lit') await gl.compileAsync(three, camera)
    let mounted = look
    return {
      async setLook(next) {
        if (next === mounted) return
        await mount(next)
        mounted = next
        await gl!.compileAsync(three, camera)
      },
      shoot(framing, options) {
        if (contextLost) throw new Error('WebGL 上下文丢失')
        if (options.kind !== 'line' && options.kind !== mounted) throw new Error(`拍${options.kind}图前须先切换渲染方式`)
        const { width, height } = framing
        gl!.setSize(width, height, false)
        camera.fov = framing.fov
        camera.aspect = width / height
        camera.near = framing.near
        camera.far = framing.far
        camera.position.fromArray(framing.position)
        camera.quaternion.fromArray(framing.quaternion)
        camera.updateProjectionMatrix()
        camera.updateMatrixWorld()
        if (options.kind !== 'line') {
          clay ??= new SceneRenderer(gl!, three, camera)
          clay.render(camera, { width, height, dof: options.depthOfField && framing.optics ? framing.optics : null, exposure: options.kind === 'lit' ? exposureOf(scene.render) : 1 })
        } else {
          line ??= new LineRenderer()
          line.render(gl!, three, camera, width, height)
        }
        if (contextLost) throw new Error('WebGL 上下文丢失')
        return canvasBlob(canvas, options.type, options.type === 'image/jpeg' ? jpegQuality : undefined)
      },
      close,
    }
  } catch (err) {
    close()
    throw err
  }
}
