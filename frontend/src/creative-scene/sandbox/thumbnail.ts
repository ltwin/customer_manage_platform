import * as THREE from 'three'
import { parseGLB } from './gltf.ts'
import type { Manifest } from './inspect.ts'
import { createRenderer, DeviceLostError, type SceneGPU } from '../render/gpu.ts'

// The PNG stored as the model revision's thumbnail object: the model in the
// viewport's clay, three-quarter view from the front (+Z), transparent
// background. Only GLBs that passed inspect() get here.

const size = 256
const clay = new THREE.MeshStandardMaterial({ color: '#d9d5cd', roughness: 0.92, metalness: 0 })
let shared: Promise<SceneGPU> | null = null

// One renderer for every thumbnail, the library's too, made when first
// needed and made again after its device is lost.
function sharedGPU(): Promise<SceneGPU> {
  if (!shared) {
    const made = createRenderer({ canvas: document.createElement('canvas'), alpha: true, output: 'direct', role: 'thumbnail' }).then((g) => {
      g.renderer.setPixelRatio(1)
      g.renderer.setSize(size, size, false)
      g.renderer.setClearColor(0x000000, 0)
      void g.lost.then(() => forget(made))
      return g
    })
    made.catch(() => forget(made))
    shared = made
  }
  return shared
}

function forget(made: Promise<SceneGPU>) {
  if (shared !== made) return
  shared = null
  void made.then((g) => g.dispose(), () => {})
}

/**
 * A thumbnail drawn on the shared renderer. A device lost on the way is made
 * again and the thumbnail drawn once more: a 256² picture is not worth
 * failing a bake for.
 */
export async function onThumbnailGPU<T>(draw: (gpu: SceneGPU) => Promise<T>): Promise<T> {
  const first = sharedGPU()
  try {
    return await draw(await first)
  } catch (err) {
    if (!(err instanceof DeviceLostError)) throw err
    forget(first)
    return draw(await sharedGPU())
  }
}

/** Three-quarter view from the front (+Z), the whole of the bounds in frame. */
export function frame(camera: THREE.PerspectiveCamera, manifest: Manifest): void {
  const min = new THREE.Vector3().fromArray(manifest.bounds.min)
  const max = new THREE.Vector3().fromArray(manifest.bounds.max)
  const center = min.clone().add(max).multiplyScalar(0.5)
  const radius = Math.max(min.distanceTo(max) / 2, 0.01)
  const distance = (radius / Math.sin(THREE.MathUtils.degToRad(camera.fov / 2))) * 1.05
  camera.position.copy(center).add(new THREE.Vector3(0.9, 0.7, 1.4).normalize().multiplyScalar(distance))
  camera.near = distance / 100
  camera.far = distance * 4
  camera.lookAt(center)
  camera.updateProjectionMatrix()
}

/** Draws one frame of scene on the thumbnail renderer and encodes it, the materials compiled first. */
export async function drawThumbnail(gpu: SceneGPU, scene: THREE.Scene, camera: THREE.Camera): Promise<Blob> {
  await gpu.renderer.compileAsync(scene, camera)
  gpu.resetFrames()
  return gpu.capture(
    () => {
      gpu.advanceFrame()
      gpu.renderer.render(scene, camera)
    },
    'image/png',
  )
}

export async function renderThumbnail(glb: ArrayBuffer, manifest: Manifest): Promise<Blob> {
  const model = await parseGLB(glb)
  const scene = new THREE.Scene()
  scene.add(new THREE.HemisphereLight(0xffffff, 0x8a8478, 1.6))
  const sun = new THREE.DirectionalLight(0xffffff, 1.8)
  sun.position.set(3, 5, 4)
  scene.add(sun)
  model.traverse((o) => {
    if (o instanceof THREE.Mesh) o.material = clay
  })
  scene.add(model)
  const camera = new THREE.PerspectiveCamera(30, 1)
  frame(camera, manifest)
  try {
    return await onThumbnailGPU((gpu) => drawThumbnail(gpu, scene, camera))
  } finally {
    model.traverse((o) => {
      if (o instanceof THREE.Mesh) o.geometry.dispose()
    })
  }
}
