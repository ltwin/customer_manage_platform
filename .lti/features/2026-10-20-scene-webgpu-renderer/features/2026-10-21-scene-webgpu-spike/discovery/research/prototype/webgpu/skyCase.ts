import * as THREE from 'three'
import { Sky as StdlibSky } from 'three-stdlib'
import { pass, renderOutput } from 'three/tsl'
import { PMREMGenerator, RenderPipeline, RenderTarget, UnsignedByteType, WebGPURenderer } from 'three/webgpu'
import type { Scene } from '../../src/creative-scene/api.ts'
import { setSky, skyEnvironment, viewSky } from '../../src/creative-scene/render/sky.ts'
import { positionToView } from '../../src/creative-scene/space.ts'
import { referenceScene, sunDirection } from './doc.ts'
import { pixelsToPNG } from './materialsCase.ts'
import { claySky, litSky } from './sky.ts'

// Group 3: the sky alone, the editor's (drei/three-stdlib Sky for clay,
// viewSky for lit, on WebGL) against the TSL ports (on WebGPU), for a
// scene's sun, looking towards it and away from it. View space is Z up.

type Mode = 'clay' | 'lit'

function claySun(doc: Scene): [number, number, number] {
  const sun = sunDirection(doc).multiplyScalar(60)
  return positionToView([sun.x, Math.max(sun.y, 0.5), sun.z])
}

function camera(doc: Scene, toward: boolean, width: number, height: number): THREE.PerspectiveCamera {
  const sun = sunDirection(doc)
  const [x, y] = positionToView([sun.x, sun.y, sun.z])
  const flat = new THREE.Vector2(x, y).normalize().multiplyScalar(toward ? 1 : -1)
  const c = new THREE.PerspectiveCamera(90, width / height, 0.1, 5000)
  c.up.set(0, 0, 1)
  c.position.set(0, 0, 1.6)
  c.lookAt(flat.x * 10, flat.y * 10, 1.6 + 4)
  c.updateMatrixWorld()
  return c
}

export async function skyLegacy(scene: string, mode: Mode, toward: boolean, width: number, height: number): Promise<string> {
  const doc = referenceScene(scene)
  const canvas = document.createElement('canvas')
  const renderer = new THREE.WebGLRenderer({ canvas, antialias: false, preserveDrawingBuffer: true })
  renderer.setPixelRatio(1)
  renderer.setSize(width, height, false)
  renderer.toneMapping = THREE.ACESFilmicToneMapping
  const three = new THREE.Scene()
  if (mode === 'clay') {
    const sky = new StdlibSky()
    const u = sky.material.uniforms
    u.up.value.set(0, 0, 1)
    u.sunPosition.value.set(...claySun(doc))
    u.turbidity.value = doc.environment.sky.turbidity ?? 3
    u.rayleigh.value = 0.5
    u.mieCoefficient.value = 0.005
    u.mieDirectionalG.value = 0.8
    sky.scale.setScalar(1000)
    three.add(sky)
  } else {
    const sky = viewSky()
    setSky(sky, doc.environment, true)
    sky.scale.setScalar(1000)
    three.add(sky)
  }
  renderer.render(three, camera(doc, toward, width, height))
  const url = canvas.toDataURL('image/png')
  renderer.dispose()
  return url
}

export async function skyTSL(scene: string, mode: Mode, toward: boolean, width: number, height: number): Promise<string> {
  const doc = referenceScene(scene)
  const renderer = new WebGPURenderer({ antialias: false })
  await renderer.init()
  renderer.setPixelRatio(1)
  renderer.setSize(width, height, false)
  const three = new THREE.Scene()
  if (mode === 'clay') {
    const sky = claySky()
    sky.sunPosition.value.set(...claySun(doc))
    sky.turbidity.value = doc.environment.sky.turbidity ?? 3
    sky.scale.setScalar(1000)
    three.add(sky)
  } else {
    const sky = litSky()
    sky.sunPosition.value.copy(sunDirection(doc))
    sky.turbidity.value = doc.environment.sky.turbidity ?? 3
    sky.showSunDisc.value = 1
    sky.scale.setScalar(1000)
    three.add(sky)
  }
  const pipeline = new RenderPipeline(renderer)
  pipeline.outputColorTransform = false
  pipeline.outputNode = renderOutput(pass(three, camera(doc, toward, width, height), { samples: 0 }), THREE.ACESFilmicToneMapping, THREE.SRGBColorSpace)
  const target = new RenderTarget(width, height, { type: UnsignedByteType, depthBuffer: false })
  renderer.setRenderTarget(target)
  pipeline.render()
  renderer.setRenderTarget(null)
  const pixels = (await renderer.readRenderTargetPixelsAsync(target, 0, 0, width, height)) as Uint8Array
  renderer.dispose()
  return pixelsToPNG(pixels, width, height, false)
}

export function exposeSky() {
  ;(window as unknown as { __sky: unknown }).__sky = { skyLegacy, skyTSL, envLegacy, envTSL }
}

// The lit look's sky light: an environment map from the sky without its disc
// (render/sky.ts skyEnvironment), lighting three spheres (rough dielectric,
// glossy dielectric, polished metal) and nothing else.
function spheres(): THREE.Group {
  const g = new THREE.Group()
  const looks = [
    { color: '#d9d5cd', roughness: 0.9, metalness: 0 },
    { color: '#d9d5cd', roughness: 0.2, metalness: 0 },
    { color: '#ffffff', roughness: 0.05, metalness: 1 },
  ]
  looks.forEach((m, i) => {
    const s = new THREE.Mesh(new THREE.SphereGeometry(0.8, 96, 48), new THREE.MeshStandardMaterial(m))
    s.position.set((i - 1) * 2, 6, 1.2)
    g.add(s)
  })
  return g
}

function envCamera(width: number, height: number) {
  const c = new THREE.PerspectiveCamera(35, width / height, 0.1, 100)
  c.up.set(0, 0, 1)
  c.position.set(0, -2, 2.2)
  c.lookAt(0, 6, 1.2)
  c.updateMatrixWorld()
  return c
}

export async function envLegacy(scene: string, width: number, height: number): Promise<string> {
  const doc = referenceScene(scene)
  const canvas = document.createElement('canvas')
  const renderer = new THREE.WebGLRenderer({ canvas, antialias: false, preserveDrawingBuffer: true })
  renderer.setPixelRatio(1)
  renderer.setSize(width, height, false)
  renderer.toneMapping = THREE.ACESFilmicToneMapping
  const pmrem = new THREE.PMREMGenerator(renderer)
  const env = skyEnvironment(pmrem, doc.environment)
  const three = new THREE.Scene()
  three.background = new THREE.Color('#202124')
  three.environment = env.texture
  three.add(spheres())
  renderer.render(three, envCamera(width, height))
  const url = canvas.toDataURL('image/png')
  renderer.dispose()
  return url
}

export async function envTSL(scene: string, width: number, height: number): Promise<string> {
  const doc = referenceScene(scene)
  const renderer = new WebGPURenderer({ antialias: false })
  await renderer.init()
  renderer.setPixelRatio(1)
  renderer.setSize(width, height, false)
  // As skyEnvironment: the sky without its disc, 10 units across, PMREM from it.
  const skyScene = new THREE.Scene()
  const sky = litSky()
  sky.sunPosition.value.copy(sunDirection(doc))
  sky.turbidity.value = doc.environment.sky.turbidity ?? 3
  sky.showSunDisc.value = 0
  sky.scale.setScalar(10)
  skyScene.add(sky)
  const pmrem = new PMREMGenerator(renderer)
  const env = pmrem.fromScene(skyScene, 0, 0.1, 100)
  const three = new THREE.Scene()
  three.background = new THREE.Color('#202124')
  three.environment = env.texture
  three.add(spheres())
  const pipeline = new RenderPipeline(renderer)
  pipeline.outputColorTransform = false
  pipeline.outputNode = renderOutput(pass(three, envCamera(width, height), { samples: 0 }), THREE.ACESFilmicToneMapping, THREE.SRGBColorSpace)
  const target = new RenderTarget(width, height, { type: UnsignedByteType, depthBuffer: false })
  renderer.setRenderTarget(target)
  pipeline.render()
  renderer.setRenderTarget(null)
  const pixels = (await renderer.readRenderTargetPixelsAsync(target, 0, 0, width, height)) as Uint8Array
  renderer.dispose()
  return pixelsToPNG(pixels, width, height, false)
}
