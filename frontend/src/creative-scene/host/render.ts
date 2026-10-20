import { createElement } from 'react'
import { createRoot, extend } from '@react-three/fiber'
import * as THREE from 'three'
import { readScene, type Scene, type SceneDocument } from '../api.ts'
import { modelCache, type SceneModel } from '../models/cache.ts'
import { SceneRenderer } from '../render/sceneRenderer.ts'
import { LineRenderer } from '../render/linePass.ts'
import { canvasBlob } from '../sandbox/thumbnail.ts'
import type { Shot } from './executor.ts'
import { framingFor, type Framing, type RenderRequest } from './framing.ts'
import { ShotScene } from './ShotScene.tsx'

// Pictures of the confirmed scene drawn as the viewport draws it (the clay
// look: the scene's sky and sun with AO), into a canvas that is never shown. Frames are drawn
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

/** clay is the viewport's look, for render_view and the export; line is the export's line art. */
export type ShotKind = 'clay' | 'line'
export type ShotOptions = { kind: ShotKind; type: 'image/jpeg' | 'image/png'; depthOfField?: boolean }

export type Shooter = {
  shoot(framing: Framing, options: ShotOptions): Blob
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
  const shooter = await openShooter(doc.scene, doc.models)
  try {
    const image = shooter.shoot(framing, { kind: 'clay', type: 'image/jpeg', depthOfField: request.depth_of_field === true })
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

/** Mounts the scene on a hidden canvas; every shot moves one camera. */
export async function openShooter(scene: Scene, models: Readonly<Record<string, SceneModel>>): Promise<Shooter> {
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
    await new Promise<void>((resolve, reject) => {
      store = root.render(createElement(ShotScene, { scene, models, onCommit: resolve, onError: reject }))
    })
    const state = store!.getState()
    gl = state.gl
    const three = state.scene
    return {
      shoot(framing, options) {
        if (contextLost) throw new Error('WebGL 上下文丢失')
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
        if (options.kind === 'clay') {
          clay ??= new SceneRenderer(gl!, three, camera)
          clay.render(camera, { width, height, dof: options.depthOfField && framing.optics ? framing.optics : null })
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
