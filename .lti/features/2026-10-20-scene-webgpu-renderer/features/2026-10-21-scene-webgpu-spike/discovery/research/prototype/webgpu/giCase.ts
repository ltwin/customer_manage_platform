import * as THREE from 'three'
import { diffuseColor, materialMetalness, materialRoughness, mix, mrt, normalView, output, packNormalToRGB, pass, renderOutput, sample, screenUV, texture, uniform, unpackRGBToNormal, vec2, vec4, velocity } from 'three/tsl'
import { HalfFloatType, MeshStandardNodeMaterial, NodeMaterial, QuadMesh, RenderPipeline, RenderTarget, UnsignedByteType, WebGPURenderer } from 'three/webgpu'
import { ssgi } from 'three/addons/tsl/display/SSGINode.js'
import { ssgiFrame } from './SSGINodeFrame.js'
import { ssr } from 'three/addons/tsl/display/SSRNode.js'
import { sss } from 'three/addons/tsl/display/SSSNode.js'
import { temporalReproject } from 'three/addons/tsl/display/TemporalReprojectNode.js'
import { recurrentDenoise } from 'three/addons/tsl/display/RecurrentDenoiseNode.js'
import { traa } from 'three/addons/tsl/display/TRAANode.js'
import type { Scene, SceneObject } from '../../src/creative-scene/api.ts'
import { primitiveGeometry } from '../../src/creative-scene/builtins/geometry.ts'
import { applyLight, buildLight } from '../../src/creative-scene/objects/lights.ts'
import { docToView } from '../../src/creative-scene/space.ts'
import { worldOf } from '../../src/creative-scene/state/world.ts'
import { referenceScene, shotCamera } from './doc.ts'
import { controlFrames } from './frames.ts'
import { pixelsToPNG } from './materialsCase.ts'

// Group 5 (R0 task 4.1): real-time GI on the box room (Epic AC-4). One pass
// without multisampling writes colour, base colour + metalness, packed
// normal + roughness and velocity (three's SSGI / SSR examples' layout).
//  - ssgi: SSGI (indirect light and AO), composite colour × AO + base colour × GI, TRAA.
//  - full: also stochastic SSR with temporal reprojection and recurrent
//    denoising, and contact shadows (SSS) from the ceiling spot.
//  - base: no GI, just TRAA: the box room without bounce, for comparison.
// Frames are stepped by hand (frames.ts) with the camera still.

/* eslint-disable @typescript-eslint/no-explicit-any -- TSL nodes are loosely typed in three's typings */
type N = any
export type GIVariant = 'base' | 'ssgi' | 'full' | 'full-dn' | 'full-hq' | 'full-post-dn' | 'full-frame'

function material(o: SceneObject): THREE.Material {
  const slot = o.material_slots?.main
  const p = (slot?.params ?? {}) as Record<string, unknown>
  if (slot?.preset === 'metal@1') return new MeshStandardNodeMaterial({ color: '#d8d8d8', metalness: 1, roughness: 0.12 })
  if (slot?.preset === 'principled@1') return new MeshStandardNodeMaterial({ color: (p.color as string) ?? '#ffffff', roughness: (p.roughness as number) ?? 0.5, metalness: (p.metalness as number) ?? 0 })
  if (slot?.preset === 'paint@1') return new MeshStandardNodeMaterial({ color: (p.color as string) ?? '#e8e8e4', roughness: 0.85 })
  return new MeshStandardNodeMaterial({ color: '#d9d5cd', roughness: 1 })
}

export function boxRoom(doc: Scene) {
  const scene = new THREE.Scene()
  scene.background = new THREE.Color(doc.environment.sky.kind === 'color' ? (doc.environment.sky.color ?? '#000000') : '#000000')
  const root = new THREE.Group()
  root.quaternion.copy(docToView)
  scene.add(root)
  const world = worldOf(doc.objects)
  let spot: THREE.SpotLight | null = null
  for (const o of doc.objects) {
    if (o.kind === 'primitive' && o.primitive) {
      const mesh = new THREE.Mesh(primitiveGeometry(o.primitive.builtin, o.primitive.params)!, material(o))
      mesh.matrixAutoUpdate = false
      mesh.matrix.copy(world(o.id))
      mesh.castShadow = true
      mesh.receiveShadow = true
      root.add(mesh)
    } else if (o.kind === 'light' && o.light) {
      const b = buildLight(o.light.type)
      applyLight(b, o.light)
      b.light.matrixAutoUpdate = false
      b.light.matrix.copy(world(o.id))
      root.add(b.light)
      if (b.target) b.light.add(b.target)
      if ((b.light as THREE.SpotLight).isSpotLight) spot = b.light as THREE.SpotLight
    }
  }
  const ambient = doc.environment.ambient ?? { color: '#ffffff', intensity: 0.4 }
  scene.add(new THREE.AmbientLight(ambient.color, ambient.intensity * 0.5))
  return { scene, spot }
}

export function giPipeline(renderer: WebGPURenderer, scene: THREE.Scene, camera: THREE.Camera, spot: THREE.Light | null, variant: GIVariant, linear = false, giScale = 1, withTRAA = true) {
  const scenePass = pass(scene, camera, { samples: 0 })
  scenePass.setMRT(mrt({ output, diffuseColor: vec4(diffuseColor.rgb, materialMetalness), normal: vec4(packNormalToRGB(normalView).rgb, materialRoughness), velocity }))
  scenePass.getTexture('diffuseColor').type = UnsignedByteType
  scenePass.getTexture('normal').type = UnsignedByteType
  const color: N = scenePass.getTextureNode('output')
  const diffuse: N = scenePass.getTextureNode('diffuseColor')
  const normalTex: N = scenePass.getTextureNode('normal')
  const depth: N = scenePass.getTextureNode('depth')
  const vel: N = scenePass.getTextureNode('velocity')
  const sceneNormal = sample((uv: N) => unpackRGBToNormal(normalTex.sample(uv).rgb))
  const nodes: { dispose(): void }[] = [scenePass]
  let composite: N = color
  // raw input for the post-TRAA denoiser (it samples it unconditionally).
  let giRaw: N = color
  if (variant !== 'base') {
    // full-frame: the SSGI copy whose noise changes every frame.
    const gi: N = (variant === 'full-frame' ? ssgiFrame : ssgi)(color, depth, sceneNormal, camera)
    // full-hq: three's 'high' preset with temporal filtering.
    giRaw = gi
    // giScale: SSGI at a fraction of the picture's size (the per-frame-noise copy only).
    gi.resolutionScale = giScale
    gi.sliceCount.value = variant === 'full-hq' ? 3 : 2
    gi.stepCount.value = variant === 'full-hq' ? 16 : 8
    nodes.push(gi)
    // full-dn: the GI temporally reprojected and denoised before the composite (diffuse mode).
    let giRGB: N = gi.getGINode().rgb
    if (variant === 'full-dn') {
      const giReprojected: N = temporalReproject(gi, depth, normalTex, vel, camera, { mode: 'diffuse', accumulate: false })
      const giDenoised: N = recurrentDenoise(giReprojected, camera, { depth, normal: normalTex, diffuse, raw: gi, mode: 'diffuse', accumulate: true })
      giReprojected.setHistoryTexture(giDenoised)
      nodes.push(giReprojected, giDenoised)
      giRGB = giDenoised.rgb
    }
    let lit: N = color.rgb.mul(gi.getAONode())
    if (variant !== 'ssgi' && spot) {
      const contact: N = sss(depth, camera, spot)
      nodes.push(contact)
      lit = lit.mul(contact)
    }
    composite = lit.add(diffuse.rgb.mul(giRGB))
    if (variant !== 'ssgi') {
      const reflect: N = ssr(color, depth, sceneNormal, { stochastic: true, diffuseNode: diffuse, metalnessNode: diffuse.a, roughnessNode: normalTex.a, camera })
      const reprojected: N = temporalReproject(reflect, depth, normalTex, vel, camera, { mode: 'specular', accumulate: false })
      const denoised: N = recurrentDenoise(reprojected, camera, {
        depth,
        normal: normalTex,
        raw: reflect,
        metalRoughness: sample((uv: N) => vec2(diffuse.sample(uv).a, normalTex.sample(uv).a)),
        mode: 'specular',
        accumulate: true,
      })
      denoised.alphaSource = 'raylength'
      reflect.setHistory(denoised, vel)
      reprojected.setHistoryTexture(denoised)
      nodes.push(reflect, reprojected, denoised)
      composite = composite.add(denoised.rgb.mul(reflect.a.greaterThan(0)))
    }
    composite = vec4(composite, color.a)
  }
  // withTRAA false: the composite as it is (group 6's tiles, where TRAA's first frames go wrong).
  let aa: N = withTRAA ? traa(composite, depth, vel, camera) : composite
  if (withTRAA) nodes.push(aa)
  // full-post-dn: the recurrent denoiser on the TRAA output, as its docs describe, with depth, normal and base colour edge stops.
  if (variant === 'full-post-dn') {
    const dn: N = recurrentDenoise(aa.getTextureNode(), camera, { depth, normal: normalTex, diffuse, raw: giRaw, mode: 'diffuse', accumulate: true })
    nodes.push(dn)
    aa = dn
  }
  const pipeline = new RenderPipeline(renderer)
  pipeline.outputColorTransform = false
  pipeline.outputNode = linear ? aa : renderOutput(aa, THREE.ACESFilmicToneMapping, THREE.SRGBColorSpace)
  return {
    pipeline,
    dispose() {
      pipeline.dispose()
      for (const n of nodes) n.dispose()
    },
  }
}

/**
 * Renders frames 1..k of a still view; returns the picture after k frames,
 * the mean absolute change between consecutive frames (0-255) at each
 * sampled frame, and the time per frame over the last 30 frames.
 */
export async function renderGI(o: { scene?: string; variant: GIVariant; width: number; height: number; k: number; forceWebGL?: boolean; samples?: number[]; accumulate?: boolean }) {
  const doc = referenceScene(o.scene ?? 'cornell')
  const renderer = new WebGPURenderer({ antialias: false, forceWebGL: o.forceWebGL ?? false })
  await renderer.init()
  renderer.setPixelRatio(1)
  renderer.setSize(o.width, o.height, false)
  renderer.shadowMap.enabled = true
  renderer.shadowMap.type = THREE.PCFShadowMap
  const backend = (renderer.backend as { isWebGPUBackend?: boolean }).isWebGPUBackend ? 'webgpu' : 'webgl2'
  const { scene, spot } = boxRoom(doc)
  const { camera } = shotCamera(doc, null, o.width / o.height)
  const built = giPipeline(renderer, scene, camera, spot, o.variant, o.accumulate)
  // accumulate: the linear picture of every frame averaged (acc = mix(acc, frame, 1/n)), then tone mapped.
  const frameTarget = new RenderTarget(o.width, o.height, { type: HalfFloatType, depthBuffer: false })
  let accRead = new RenderTarget(o.width, o.height, { type: HalfFloatType, depthBuffer: false })
  let accWrite = new RenderTarget(o.width, o.height, { type: HalfFloatType, depthBuffer: false })
  const weight = uniform(1)
  const previousAcc = texture(accRead.texture)
  const accQuad = new QuadMesh(Object.assign(new NodeMaterial(), { fragmentNode: mix(previousAcc.sample(screenUV), texture(frameTarget.texture).sample(screenUV), weight) }))
  const latestAcc = texture(accRead.texture)
  const outQuad = new QuadMesh(Object.assign(new NodeMaterial(), { fragmentNode: renderOutput(latestAcc.sample(screenUV), THREE.ACESFilmicToneMapping, THREE.SRGBColorSpace) }))
  let n = 0
  const frame = () => {
    if (!o.accumulate) {
      built.pipeline.render()
      return
    }
    renderer.setRenderTarget(frameTarget)
    built.pipeline.render()
    n++
    weight.value = 1 / n
    previousAcc.value = accRead.texture
    renderer.setRenderTarget(accWrite)
    accQuad.render(renderer)
    ;[accRead, accWrite] = [accWrite, accRead]
    latestAcc.value = accRead.texture
    renderer.setRenderTarget(target)
    outQuad.render(renderer)
  }
  const target = new RenderTarget(o.width, o.height, { type: UnsignedByteType, depthBuffer: false })
  const clock = controlFrames(renderer)
  clock.reset()
  renderer.setRenderTarget(target)
  const read = async () => (await renderer.readRenderTargetPixelsAsync(target, 0, 0, o.width, o.height)) as Uint8Array
  const samples = new Set(o.samples ?? [])
  const changes: { frame: number; change: number }[] = []
  let previous: Uint8Array | null = null
  for (let f = 1; f <= o.k; f++) {
    clock.step()
    frame()
    if (samples.has(f + 1)) previous = await read()
    if (samples.has(f) && previous) {
      const now = await read()
      let sum = 0
      for (let i = 0; i < now.length; i++) if ((i & 3) !== 3) sum += Math.abs(now[i] - previous[i])
      changes.push({ frame: f, change: sum / ((now.length / 4) * 3) })
    }
  }
  const pixels = await read()
  // Time 30 more frames, the GPU drained before and after.
  await renderer.readRenderTargetPixelsAsync(target, 0, 0, 1, 1)
  const started = performance.now()
  for (let i = 0; i < 30; i++) {
    clock.step()
    frame()
  }
  await renderer.readRenderTargetPixelsAsync(target, 0, 0, 1, 1)
  const msPerFrame = (performance.now() - started) / 30
  renderer.setRenderTarget(null)
  built.dispose()
  renderer.dispose()
  return { backend, png: pixelsToPNG(pixels, o.width, o.height, backend !== 'webgpu'), changes, msPerFrame }
}

export function exposeGI() {
  ;(window as unknown as { __gi: unknown }).__gi = { renderGI }
}
