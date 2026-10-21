import * as THREE from 'three'
import { parseGLB } from './gltf.ts'
import type { Manifest } from './inspect.ts'

// The PNG stored as the model revision's thumbnail object: the model in the
// viewport's clay, three-quarter view from the front (+Z), transparent
// background. Only GLBs that passed inspect() get here.

const size = 256
const clay = new THREE.MeshStandardMaterial({ color: '#d9d5cd', roughness: 0.92, metalness: 0 })
let renderer: THREE.WebGLRenderer | null = null

// One renderer for every thumbnail, the library's too: browsers cap the
// number of live WebGL contexts, and the viewport already holds one.
export function sharedRenderer(): THREE.WebGLRenderer {
  if (!renderer) {
    renderer = new THREE.WebGLRenderer({ canvas: document.createElement('canvas'), alpha: true, antialias: true, preserveDrawingBuffer: true })
    renderer.setPixelRatio(1)
    renderer.setSize(size, size, false)
    renderer.setClearColor(0x000000, 0)
  }
  return renderer
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

/**
 * The canvas as an image file, encoded now. toBlob encodes in an idle task,
 * which a background tab starts only after a throttled timeout: a hidden
 * page running the assistant's jobs would wait a second or more per image.
 */
export function canvasBlob(canvas: HTMLCanvasElement, type: string, quality?: number): Blob {
  const url = canvas.toDataURL(type, quality)
  if (!url.startsWith(`data:${type};base64,`)) throw new Error('图片编码失败')
  const text = atob(url.slice(url.indexOf(',') + 1))
  const bytes = new Uint8Array(text.length)
  for (let i = 0; i < text.length; i++) bytes[i] = text.charCodeAt(i)
  return new Blob([bytes], { type })
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
  const r = sharedRenderer()
  r.render(scene, camera)
  model.traverse((o) => {
    if (o instanceof THREE.Mesh) o.geometry.dispose()
  })
  return canvasBlob(r.domElement, 'image/png')
}
