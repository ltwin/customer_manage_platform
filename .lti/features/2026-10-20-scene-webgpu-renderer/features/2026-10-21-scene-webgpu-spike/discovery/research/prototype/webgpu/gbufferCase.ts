import * as THREE from 'three'
import { mrt, normalView, output, pass, renderOutput, screenUV, vec4, velocity } from 'three/tsl'
import { RenderPipeline, RenderTarget, UnsignedByteType, WebGPURenderer, MeshStandardNodeMaterial } from 'three/webgpu'
import { ao } from 'three/addons/tsl/display/GTAONode.js'
import { traa } from 'three/addons/tsl/display/TRAANode.js'
import { smaa } from 'three/addons/tsl/display/SMAANode.js'
import { controlFrames } from './frames.ts'
import { pixelsToPNG } from './materialsCase.ts'

// Group 4 (R0 task 3.4): the AO's normals and depth from opaque geometry
// only, two ways, on the C5 AC-9 performance scene (about 200 objects in
// 8 materials, the sun and four lights, three casting shadows) plus a few
// see-through spheres:
//  - prepass: workbench. A normal/depth pass without multisampling and
//    without see-through meshes (camera layers), the picture with 4× MSAA,
//    the AO multiplied over it as the WebGL GTAOPass does now.
//  - mrt: real-time GI. One pass without multisampling writing colour,
//    normal and velocity; see-through materials write a zero-alpha normal
//    so the blend leaves the opaque normal behind; AO multiplied, then TRAA.
// Also: switching the two back and forth, and SMAA on a device made in
// WebGPU compatibility mode (no core-features-and-limits).

/* eslint-disable @typescript-eslint/no-explicit-any -- TSL nodes are loosely typed in three's typings */
type N = any
const glassLayer = 1

export function perfScene(glassNormalMask: boolean) {
  const scene = new THREE.Scene()
  scene.background = new THREE.Color('#9db8d0')
  const colors = ['#8b6b4a', '#a0a0a0', '#c8c2b8', '#6f7f5a', '#e4e0d8', '#b4b0a8', '#d8d4cc', '#c04040']
  const materials = colors.map((color, i) => new THREE.MeshStandardMaterial({ color, roughness: 0.3 + (i % 4) * 0.2, metalness: i === 1 ? 1 : 0 }))
  const geometries = [new THREE.BoxGeometry(0.8, 0.8, 0.8), new THREE.CylinderGeometry(0.35, 0.35, 1.2, 24), new THREE.SphereGeometry(0.45, 32, 16)]
  for (let i = 0; i < 196; i++) {
    const mesh = new THREE.Mesh(geometries[i % 3], materials[i % 8])
    mesh.position.set((i % 14) * 1.3 - 8.5, 0.6, -(Math.floor(i / 14) * 1.3 - 8.5))
    mesh.castShadow = true
    mesh.receiveShadow = true
    scene.add(mesh)
  }
  const ground = new THREE.Mesh(new THREE.PlaneGeometry(40, 40), new THREE.MeshStandardMaterial({ color: '#d9d5cd', roughness: 1 }))
  ground.rotation.x = -Math.PI / 2
  ground.receiveShadow = true
  scene.add(ground)
  // See-through spheres: in the picture, not in the AO's normals and depth.
  const glass = new MeshStandardNodeMaterial({ color: '#ffffff', roughness: 0.05, transparent: true, opacity: 0.35, depthWrite: false })
  if (glassNormalMask) glass.mrtNode = mrt({ normal: vec4(0, 0, 0, 0) })
  for (let i = 0; i < 6; i++) {
    const s = new THREE.Mesh(new THREE.SphereGeometry(0.7, 48, 24), glass)
    s.position.set(-3 + i * 1.3, 1.6, 3.5)
    s.layers.set(glassLayer)
    scene.add(s)
  }
  const sun = new THREE.DirectionalLight('#ffffff', 2.2)
  sun.position.set(20, 30, 15)
  sun.castShadow = true
  sun.shadow.mapSize.set(2048, 2048)
  Object.assign(sun.shadow.camera, { left: -15, right: 15, top: 15, bottom: -15, far: 80 })
  scene.add(sun, new THREE.AmbientLight('#ffffff', 0.4))
  for (const [x, c] of [[-3, '#ffffff'], [3, '#ffe0c0']] as const) {
    const spot = new THREE.SpotLight(c, 150, 0, Math.PI / 6, 0.2, 2)
    spot.position.set(x, 5, 0)
    spot.target.position.set(x, 0, 0)
    spot.castShadow = true
    spot.shadow.mapSize.set(1024, 1024)
    scene.add(spot, spot.target)
  }
  for (const x of [-5, 5]) {
    const p = new THREE.PointLight('#ffd9b0', 40, 0, 2)
    p.position.set(x, 2, -4)
    scene.add(p)
  }
  const camera = new THREE.PerspectiveCamera(45, 1.6, 0.1, 200)
  camera.position.set(0, 9, 14)
  camera.lookAt(0, 0, 0)
  camera.layers.enable(glassLayer)
  camera.updateMatrixWorld()
  return { scene, camera }
}

type Variant = 'prepass' | 'mrt' | 'mrt-unmasked'
type Show = 'picture' | 'normal' | 'ao'

export function pipelineFor(renderer: WebGPURenderer, variant: Variant, show: Show, shared?: ReturnType<typeof perfScene>) {
  const { scene, camera } = shared ?? perfScene(variant === 'mrt')
  let picture: N
  let normal: N
  let aoNode: N
  const disposables: { dispose(): void }[] = []
  if (variant === 'prepass') {
    // ?prepass=layers: see-through meshes left out by camera layers; otherwise by the pass's own opaque-only switch.
    const byLayers = new URLSearchParams(location.search).get('prepass') === 'layers'
    const opaque = camera.clone()
    if (byLayers) opaque.layers.disable(glassLayer)
    const prePass = pass(scene, opaque, { samples: 0 })
    if (!byLayers) prePass.transparent = false
    prePass.setMRT(mrt({ output: normalView }))
    normal = prePass.getTextureNode()
    aoNode = ao(prePass.getTextureNode('depth'), normal, opaque)
    const beauty = pass(scene, camera, { samples: 4 })
    picture = beauty.mul(aoNode.getTextureNode().sample(screenUV).r)
    disposables.push(prePass, beauty, aoNode)
  } else {
    const scenePass = pass(scene, camera, { samples: 0 })
    scenePass.setMRT(mrt({ output, normal: normalView, velocity }))
    normal = scenePass.getTextureNode('normal')
    aoNode = ao(scenePass.getTextureNode('depth'), normal, camera)
    const lit = scenePass.getTextureNode('output').mul(aoNode.getTextureNode().sample(screenUV).r)
    const aa = traa(lit, scenePass.getTextureNode('depth'), scenePass.getTextureNode('velocity'), camera)
    picture = aa
    disposables.push(scenePass, aoNode, aa)
  }
  const pipeline = new RenderPipeline(renderer)
  pipeline.outputColorTransform = false
  pipeline.outputNode =
    show === 'normal' ? vec4(normal.sample(screenUV).rgb.mul(0.5).add(0.5), 1) : show === 'ao' ? vec4(aoNode.getTextureNode().sample(screenUV).r.toVec3(), 1) : renderOutput(picture, THREE.ACESFilmicToneMapping, THREE.SRGBColorSpace)
  return {
    pipeline,
    dispose() {
      pipeline.dispose()
      for (const d of disposables) d.dispose()
    },
  }
}

async function renderer(options: { device?: GPUDevice } = {}) {
  const r = new WebGPURenderer({ antialias: false, ...options })
  await r.init()
  r.setPixelRatio(1)
  r.shadowMap.enabled = true
  r.shadowMap.type = THREE.PCFShadowMap
  return r
}

/** A picture (or the normal or AO buffer) of a variant, and its frame time over n frames stepped by hand. */
export async function renderGBuffer(variant: Variant, show: Show, width: number, height: number, n = 60) {
  const r = await renderer()
  r.setSize(width, height, false)
  const clock = controlFrames(r)
  const built = pipelineFor(r, variant, show)
  const target = new RenderTarget(width, height, { type: UnsignedByteType, depthBuffer: false })
  r.setRenderTarget(target)
  clock.reset()
  for (let i = 0; i < 4; i++) {
    clock.step()
    built.pipeline.render()
  }
  await r.readRenderTargetPixelsAsync(target, 0, 0, 1, 1)
  const started = performance.now()
  for (let i = 0; i < n; i++) {
    clock.step()
    built.pipeline.render()
  }
  await r.readRenderTargetPixelsAsync(target, 0, 0, 1, 1)
  const ms = (performance.now() - started) / n
  const pixels = (await r.readRenderTargetPixelsAsync(target, 0, 0, width, height)) as Uint8Array
  r.setRenderTarget(null)
  built.dispose()
  r.dispose()
  return { png: pixelsToPNG(pixels, width, height, false), ms }
}

/** Builds and frees the two pipelines in turn; reports the renderer's texture count after each switch. */
export async function switchPipelines(times: number, width: number, height: number) {
  const r = await renderer()
  r.setSize(width, height, false)
  const clock = controlFrames(r)
  const target = new RenderTarget(width, height, { type: UnsignedByteType, depthBuffer: false })
  r.setRenderTarget(target)
  const textures: number[] = []
  // One scene for every pipeline: only the passes are made and freed.
  const shared = perfScene(true)
  for (let i = 0; i < times; i++) {
    const built = pipelineFor(r, i % 2 === 0 ? 'prepass' : 'mrt', 'picture', shared)
    for (let k = 0; k < 3; k++) {
      clock.step()
      built.pipeline.render()
    }
    await r.readRenderTargetPixelsAsync(target, 0, 0, 1, 1)
    built.dispose()
    textures.push(r.info.memory.textures)
  }
  r.setRenderTarget(null)
  r.dispose()
  return textures
}

/** SMAA after the picture on a device without core-features-and-limits, where three turns MSAA off. */
export async function compatibilitySMAA(width: number, height: number) {
  const adapter = await navigator.gpu.requestAdapter({ featureLevel: 'compatibility' } as GPURequestAdapterOptions)
  if (!adapter) return { error: 'no compatibility adapter' }
  const device = await adapter.requestDevice({ requiredFeatures: [] })
  const r = await renderer({ device })
  const compatibilityMode = (r.backend as unknown as { compatibilityMode: boolean }).compatibilityMode
  r.setSize(width, height, false)
  const { scene, camera } = perfScene(false)
  const out: Record<string, string> = {}
  for (const withSMAA of [false, true]) {
    const scenePass = pass(scene, camera)
    const node = withSMAA ? smaa(renderOutput(scenePass, THREE.ACESFilmicToneMapping, THREE.SRGBColorSpace)) : renderOutput(scenePass, THREE.ACESFilmicToneMapping, THREE.SRGBColorSpace)
    const pipeline = new RenderPipeline(r)
    pipeline.outputColorTransform = false
    pipeline.outputNode = node
    const target = new RenderTarget(width, height, { type: UnsignedByteType, depthBuffer: false })
    r.setRenderTarget(target)
    pipeline.render()
    const pixels = (await r.readRenderTargetPixelsAsync(target, 0, 0, width, height)) as Uint8Array
    r.setRenderTarget(null)
    out[withSMAA ? 'smaa' : 'none'] = pixelsToPNG(pixels, width, height, false)
    pipeline.dispose()
    scenePass.dispose()
  }
  const samples = (r as unknown as { _samples: number })._samples
  r.dispose()
  return { compatibilityMode, samples, ...out }
}

export function exposeGBuffer() {
  ;(window as unknown as { __gbuffer: unknown }).__gbuffer = { renderGBuffer, switchPipelines, compatibilitySMAA }
}
