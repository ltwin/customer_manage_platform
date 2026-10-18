import { createElement } from 'react'
import { createRoot, extend } from '@react-three/fiber'
import * as THREE from 'three'
import { readScene, type Scene, type SceneDocument } from '../api.ts'
import { modelCache, type SceneModel } from '../models/cache.ts'
import type { Shot } from './executor.ts'
import { framingFor, type Framing, type RenderRequest } from './framing.ts'
import { ShotScene } from './ShotScene.tsx'

// render_view on the page: the confirmed scene drawn as the viewport draws it
// (clay, the scene's sky and sun), from the requested camera, into a canvas
// that is never shown. It renders once by hand, so a hidden tab without
// animation frames still takes the picture, and gives its WebGL context back
// at once: browsers cap live contexts and the editor may hold one.

const jpegQuality = 0.85

// <Canvas> registers three's classes for JSX; a bare root has to.
extend(THREE as unknown as Parameters<typeof extend>[0])

export async function renderView(documentID: string, request: RenderRequest, read: (id: string) => Promise<SceneDocument> = readScene): Promise<Shot> {
  const doc = await read(documentID)
  const framing = framingFor(doc.scene, doc.models, request)
  if ('error' in framing) return { ok: false, message: framing.error }
  // Every model in before the one frame; one that fails draws as its grey block.
  const revisions = new Set(doc.scene.objects.flatMap((o) => (o.model?.content_revision_id && doc.models[o.model.content_revision_id]?.usable !== false ? [o.model.content_revision_id] : [])))
  await Promise.allSettled([...revisions].map((r) => modelCache.load(r)))
  const image = await shoot(doc.scene, doc.models, framing)
  return { ok: true, image, revision: doc.revision }
}

export async function shoot(scene: Scene, models: Readonly<Record<string, SceneModel>>, framing: Framing): Promise<Blob> {
  const { width, height } = framing
  const canvas = document.createElement('canvas')
  canvas.width = width
  canvas.height = height
  const camera = new THREE.PerspectiveCamera(framing.fov, width / height, framing.near, framing.far)
  camera.up.set(0, 0, 1)
  camera.position.fromArray(framing.position)
  camera.quaternion.fromArray(framing.quaternion)
  camera.updateProjectionMatrix()
  camera.updateMatrixWorld()
  const root = createRoot(canvas)
  try {
    await root.configure({
      gl: { preserveDrawingBuffer: true, antialias: true },
      shadows: true,
      frameloop: 'never',
      dpr: 1,
      size: { width, height, top: 0, left: 0 },
      camera,
    })
    let store: ReturnType<typeof root.render> | undefined
    await new Promise<void>((resolve, reject) => {
      store = root.render(createElement(ShotScene, { scene, models, onCommit: resolve, onError: reject }))
    })
    const state = store!.getState()
    state.gl.render(state.scene, state.camera)
    return await new Promise<Blob>((resolve, reject) => {
      canvas.toBlob((blob) => (blob ? resolve(blob) : reject(new Error('截图生成失败'))), 'image/jpeg', jpegQuality)
    })
  } finally {
    root.unmount()
  }
}
