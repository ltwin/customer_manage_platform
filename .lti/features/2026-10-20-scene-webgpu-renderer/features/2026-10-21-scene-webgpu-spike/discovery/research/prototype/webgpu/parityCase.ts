import * as THREE from 'three'
import { WebGPURenderer } from 'three/webgpu'
import { LineRenderer } from '../../src/creative-scene/render/linePass.ts'
import { SceneRenderer } from '../../src/creative-scene/render/sceneRenderer.ts'
import { perfScene, renderGBuffer } from './gbufferCase.ts'
import { lineArt, TileCamera } from './hiresCase.ts'

// R0 task 5.1: like-for-like numbers against the editor's WebGL code.
//  - workbench: the group 4 performance scene through the editor's own
//    SceneRenderer (EffectComposer, 4× MSAA, GTAOPass with its denoise,
//    OutputPass) and through the WebGPU prepass pipeline, both timed as
//    group 4 times: frames stepped back to back, the GPU drained after.
//  - line: linePass.ts on WebGLRenderer against its TSL port (hiresCase.ts).

/** The performance scene with the see-through spheres' node material swapped for a plain one WebGLRenderer can draw. */
function legacyScene(width: number, height: number) {
  const { scene, camera } = perfScene(false)
  scene.traverse((o) => {
    const mesh = o as THREE.Mesh
    if ((mesh.material as { isNodeMaterial?: boolean })?.isNodeMaterial) mesh.material = new THREE.MeshStandardMaterial({ color: '#ffffff', roughness: 0.05, transparent: true, opacity: 0.35, depthWrite: false })
  })
  camera.aspect = width / height
  camera.updateProjectionMatrix()
  return { scene, camera }
}

function legacyRenderer(width: number, height: number) {
  const canvas = document.createElement('canvas')
  const gl = new THREE.WebGLRenderer({ canvas, antialias: false, preserveDrawingBuffer: true })
  gl.setPixelRatio(1)
  gl.setSize(width, height, false)
  gl.shadowMap.enabled = true
  gl.shadowMap.type = THREE.PCFSoftShadowMap
  gl.toneMapping = THREE.ACESFilmicToneMapping
  return gl
}

/** The canvas's pixels, top-down; readPixels also waits for the GPU. */
function readCanvas(gl: THREE.WebGLRenderer, width: number, height: number) {
  const ctx = gl.getContext()
  const raw = new Uint8Array(width * height * 4)
  ctx.readPixels(0, 0, width, height, ctx.RGBA, ctx.UNSIGNED_BYTE, raw)
  const out = new Uint8ClampedArray(raw.length)
  for (let y = 0; y < height; y++) out.set(raw.subarray((height - 1 - y) * width * 4, (height - y) * width * 4), y * width * 4)
  return out
}

/** ms a frame of the editor's WebGL workbench pipeline (no depth of field) over n frames. */
export async function legacyWorkbench(width: number, height: number, n = 60) {
  const gl = legacyRenderer(width, height)
  const { scene, camera } = legacyScene(width, height)
  const renderer = new SceneRenderer(gl, scene, camera)
  const frame = { width, height, pixelRatio: 1, dof: null }
  for (let i = 0; i < 4; i++) renderer.render(camera, frame)
  const sync = new Uint8Array(4)
  const ctx = gl.getContext()
  ctx.readPixels(0, 0, 1, 1, ctx.RGBA, ctx.UNSIGNED_BYTE, sync)
  const started = performance.now()
  for (let i = 0; i < n; i++) renderer.render(camera, frame)
  ctx.readPixels(0, 0, 1, 1, ctx.RGBA, ctx.UNSIGNED_BYTE, sync)
  const ms = (performance.now() - started) / n
  renderer.dispose()
  gl.dispose()
  return { ms }
}

/** The same scene and size through group 4's WebGPU workbench pipeline. */
export async function webgpuWorkbench(width: number, height: number, n = 60) {
  const { ms } = await renderGBuffer('prepass', 'picture', width, height, n)
  return { ms }
}

/** Line art of the performance scene both ways at 2× normals; difference per channel (0-255) and both pictures. */
export async function lineCompare(width: number, height: number) {
  const gl = legacyRenderer(width, height)
  const legacy = legacyScene(width, height)
  const lines = new LineRenderer()
  lines.render(gl, legacy.scene, legacy.camera, width, height)
  const a = readCanvas(gl, width, height)
  lines.dispose()
  gl.dispose()
  const r = new WebGPURenderer({ antialias: false })
  await r.init()
  r.setPixelRatio(1)
  const drawer = lineArt(r, 2)
  const camera = new TileCamera().copy(drawer.camera)
  camera.aspect = width / height
  camera.updateProjectionMatrix()
  const { pixels: b } = await drawer.draw(camera, width, height)
  r.dispose()
  let sum = 0
  let over = 0
  let max = 0
  for (let i = 0; i < a.length; i += 4) {
    const d = Math.max(Math.abs(a[i] - b[i]), Math.abs(a[i + 1] - b[i + 1]), Math.abs(a[i + 2] - b[i + 2]))
    sum += Math.abs(a[i] - b[i]) + Math.abs(a[i + 1] - b[i + 1]) + Math.abs(a[i + 2] - b[i + 2])
    if (d > 8) over++
    max = Math.max(max, d)
  }
  const png = (px: Uint8ClampedArray) => {
    const c = document.createElement('canvas')
    c.width = width
    c.height = height
    c.getContext('2d')!.putImageData(new ImageData(px as Uint8ClampedArray<ArrayBuffer>, width, height), 0, 0)
    return c.toDataURL('image/png')
  }
  return { mean: sum / ((a.length / 4) * 3), overShare: over / (a.length / 4), max, legacy: png(a), tsl: png(b) }
}

export function exposeParity() {
  ;(window as unknown as { __parity: unknown }).__parity = { legacyWorkbench, webgpuWorkbench, lineCompare }
}
