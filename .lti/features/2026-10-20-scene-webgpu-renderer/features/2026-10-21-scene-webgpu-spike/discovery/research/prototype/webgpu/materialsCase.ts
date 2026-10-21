import * as THREE from 'three'
import { pass, renderOutput } from 'three/tsl'
import { RenderPipeline, RenderTarget, UnsignedByteType, WebGPURenderer, type Material } from 'three/webgpu'
import { catalogMaterial } from '../../src/creative-scene/materials/resolve.ts'
import { makeMaterial } from '../../src/creative-scene/materials/instances.ts'
import { tslPatternMaterial } from './patterns.ts'

// Group 2: the same small set, lights and camera drawn twice: by the
// editor's GLSL presets on a WebGLRenderer (makeMaterial, onBeforeCompile)
// and by their TSL ports on a WebGPURenderer, both without multisampling,
// ACES and sRGB, read back as PNG for a difference image.

const layout: { id: string; shape: 'box' | 'sphere'; x: number }[] = [
  { id: 'tile@1', shape: 'box', x: -2.1 },
  { id: 'tile@1', shape: 'sphere', x: -0.7 },
  { id: 'marble@1', shape: 'box', x: 0.7 },
  { id: 'marble@1', shape: 'sphere', x: 2.1 },
]

function build(materialFor: (id: string) => Material): { scene: THREE.Scene; camera: THREE.PerspectiveCamera } {
  const scene = new THREE.Scene()
  scene.background = new THREE.Color('#5b5d61')
  const ground = new THREE.Mesh(new THREE.PlaneGeometry(14, 14), materialFor('tile@1'))
  ground.rotation.x = -Math.PI / 2
  scene.add(ground)
  for (const o of layout) {
    const geometry = o.shape === 'box' ? new THREE.BoxGeometry(1.2, 1.2, 1.2) : new THREE.SphereGeometry(0.6, 64, 32)
    const mesh = new THREE.Mesh(geometry, materialFor(o.id))
    mesh.position.set(o.x, 0.6, 0)
    mesh.rotation.y = 0.5
    scene.add(mesh)
  }
  const sun = new THREE.DirectionalLight('#ffffff', 2.5)
  sun.position.set(3, 5, 4)
  scene.add(sun, new THREE.AmbientLight('#ffffff', 0.4))
  const camera = new THREE.PerspectiveCamera(40, 3 / 2, 0.1, 100)
  camera.position.set(0, 3.2, 6.5)
  camera.lookAt(0, 0.4, 0)
  return { scene, camera }
}

export async function renderLegacy(width: number, height: number): Promise<string> {
  const canvas = document.createElement('canvas')
  const renderer = new THREE.WebGLRenderer({ canvas, antialias: false, preserveDrawingBuffer: true })
  renderer.setPixelRatio(1)
  renderer.setSize(width, height, false)
  renderer.toneMapping = THREE.ACESFilmicToneMapping
  const { scene, camera } = build((id) => makeMaterial(catalogMaterial(id)!, false))
  camera.aspect = width / height
  camera.updateProjectionMatrix()
  renderer.render(scene, camera)
  const url = canvas.toDataURL('image/png')
  renderer.dispose()
  return url
}

export async function renderTSL(width: number, height: number, forceWebGL = false): Promise<string> {
  const renderer = new WebGPURenderer({ antialias: false, forceWebGL })
  await renderer.init()
  renderer.setPixelRatio(1)
  renderer.setSize(width, height, false)
  const { scene, camera } = build((id) => tslPatternMaterial(id))
  camera.aspect = width / height
  camera.updateProjectionMatrix()
  const pipeline = new RenderPipeline(renderer)
  pipeline.outputColorTransform = false
  pipeline.outputNode = renderOutput(pass(scene, camera, { samples: 0 }), THREE.ACESFilmicToneMapping, THREE.SRGBColorSpace)
  const target = new RenderTarget(width, height, { type: UnsignedByteType, depthBuffer: false })
  renderer.setRenderTarget(target)
  pipeline.render()
  renderer.setRenderTarget(null)
  const pixels = (await renderer.readRenderTargetPixelsAsync(target, 0, 0, width, height)) as Uint8Array
  const isWebGPU = (renderer.backend as { isWebGPUBackend?: boolean }).isWebGPUBackend
  renderer.dispose()
  return pixelsToPNG(pixels, width, height, !isWebGPU)
}

/**
 * Read-back bytes to a PNG. WebGPU rows come padded to 256 bytes (texture to
 * buffer copies require it; three returns the buffer as copied, the last row
 * unpadded); WebGL rows are tight and bottom-up.
 */
export function pixelsToPNG(pixels: Uint8Array, width: number, height: number, flip: boolean): string {
  const c = document.createElement('canvas')
  c.width = width
  c.height = height
  const g = c.getContext('2d')!
  const image = g.createImageData(width, height)
  const stride = flip ? width * 4 : Math.ceil((width * 4) / 256) * 256
  for (let y = 0; y < height; y++) {
    const from = (flip ? height - 1 - y : y) * stride
    image.data.set(pixels.subarray(from, from + width * 4), y * width * 4)
  }
  g.putImageData(image, 0, 0)
  return c.toDataURL('image/png')
}

export function exposeMaterials() {
  ;(window as unknown as { __materials: unknown }).__materials = { renderLegacy, renderTSL }
}
