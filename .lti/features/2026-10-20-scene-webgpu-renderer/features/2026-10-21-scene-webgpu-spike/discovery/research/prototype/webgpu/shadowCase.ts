import * as THREE from 'three'
import { pass, renderOutput } from 'three/tsl'
import { RenderPipeline, RenderTarget, UnsignedByteType, WebGPURenderer } from 'three/webgpu'
import type { Scene } from '../../src/creative-scene/api.ts'
import { primitiveGeometry } from '../../src/creative-scene/builtins/geometry.ts'
import { clay } from '../../src/creative-scene/objects/look.ts'
import { fitSunShadow } from '../../src/creative-scene/render/sunShadow.ts'
import { docToView, positionToView } from '../../src/creative-scene/space.ts'
import { worldOf } from '../../src/creative-scene/state/world.ts'
import { referenceScene, shotCamera, sunDirection } from './doc.ts'
import { pixelsToPNG } from './materialsCase.ts'

// Group 3: the sun's shadow in the clay lights (ClayLights: ambient + 0.3, a
// hemisphere fill, a 2.2 sun fitted by sunShadow.ts), WebGL's
// PCFSoftShadowMap against what WebGPU has: PCF with a radius, or VSM.

function build(doc: Scene) {
  const scene = new THREE.Scene()
  scene.background = new THREE.Color('#9db8d0')
  const root = new THREE.Group()
  root.quaternion.copy(docToView)
  scene.add(root)
  const world = worldOf(doc.objects)
  const material = new THREE.MeshStandardMaterial({ color: clay, roughness: 1 })
  for (const o of doc.objects) {
    if (o.kind !== 'primitive' || !o.primitive) continue
    const mesh = new THREE.Mesh(primitiveGeometry(o.primitive.builtin, o.primitive.params)!, material)
    mesh.matrixAutoUpdate = false
    mesh.matrix.copy(world(o.id))
    mesh.castShadow = true
    mesh.receiveShadow = true
    root.add(mesh)
  }
  const ambient = doc.environment.ambient ?? { color: '#ffffff', intensity: 0.4 }
  scene.add(new THREE.AmbientLight(ambient.color, ambient.intensity + 0.3))
  const hemi = new THREE.HemisphereLight('#ffffff', '#6b6760', 0.6)
  hemi.position.set(0, 0, 1)
  scene.add(hemi)
  const s = sunDirection(doc).multiplyScalar(60)
  const sun = new THREE.DirectionalLight('#ffffff', 2.2)
  sun.position.set(...positionToView([s.x, Math.max(s.y, 2), s.z]))
  sun.castShadow = true
  sun.shadow.mapSize.set(2048, 2048)
  scene.add(sun)
  fitSunShadow(sun)
  return { scene, sun }
}

export async function shadowLegacy(sceneName: string, width: number, height: number): Promise<string> {
  const doc = referenceScene(sceneName)
  const canvas = document.createElement('canvas')
  const renderer = new THREE.WebGLRenderer({ canvas, antialias: false, preserveDrawingBuffer: true })
  renderer.setPixelRatio(1)
  renderer.setSize(width, height, false)
  renderer.toneMapping = THREE.ACESFilmicToneMapping
  renderer.shadowMap.enabled = true
  renderer.shadowMap.type = THREE.PCFSoftShadowMap
  const { scene } = build(doc)
  renderer.render(scene, shotCamera(doc, null, width / height).camera)
  const url = canvas.toDataURL('image/png')
  renderer.dispose()
  return url
}

export async function shadowTSL(sceneName: string, type: 'pcf' | 'vsm', radius: number, width: number, height: number): Promise<string> {
  const doc = referenceScene(sceneName)
  const renderer = new WebGPURenderer({ antialias: false })
  await renderer.init()
  renderer.setPixelRatio(1)
  renderer.setSize(width, height, false)
  renderer.shadowMap.enabled = true
  renderer.shadowMap.type = type === 'vsm' ? THREE.VSMShadowMap : THREE.PCFShadowMap
  const { scene, sun } = build(doc)
  sun.shadow.radius = radius
  if (type === 'vsm') sun.shadow.blurSamples = 8
  const pipeline = new RenderPipeline(renderer)
  pipeline.outputColorTransform = false
  pipeline.outputNode = renderOutput(pass(scene, shotCamera(doc, null, width / height).camera, { samples: 0 }), THREE.ACESFilmicToneMapping, THREE.SRGBColorSpace)
  const target = new RenderTarget(width, height, { type: UnsignedByteType, depthBuffer: false })
  renderer.setRenderTarget(target)
  pipeline.render()
  renderer.setRenderTarget(null)
  const pixels = (await renderer.readRenderTargetPixelsAsync(target, 0, 0, width, height)) as Uint8Array
  renderer.dispose()
  return pixelsToPNG(pixels, width, height, false)
}

export function exposeShadow() {
  ;(window as unknown as { __shadow: unknown }).__shadow = { shadowLegacy, shadowTSL }
}
