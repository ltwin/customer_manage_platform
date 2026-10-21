import * as THREE from 'three'
import { OutputPass } from 'three/examples/jsm/postprocessing/OutputPass.js'
import { pass, renderOutput } from 'three/tsl'
import { RenderPipeline, RenderTarget, UnsignedByteType, WebGPURenderer, type Material } from 'three/webgpu'
import type { Scene } from '../../src/creative-scene/api.ts'
import { primitiveGeometry } from '../../src/creative-scene/builtins/geometry.ts'
import type { DepthOfField } from '../../src/creative-scene/cameraOptics.ts'
import { makeMaterial } from '../../src/creative-scene/materials/instances.ts'
import { catalogMaterial } from '../../src/creative-scene/materials/resolve.ts'
import { DofPass } from '../../src/creative-scene/render/dofPass.ts'
import { docToView } from '../../src/creative-scene/space.ts'
import { worldOf } from '../../src/creative-scene/state/world.ts'
import { depthOfField } from './dof.ts'
import { referenceScene, shotCamera } from './doc.ts'
import { pixelsToPNG } from './materialsCase.ts'
import { tslPatternMaterial } from './patterns.ts'

// Group 2: the dof reference scene through camera A (F1.4 at 8 m), its
// ground tiled so the blur shows, drawn into linear half floats and blurred
// by the editor's DofPass on WebGL and by the TSL port on WebGPU, then ACES
// and sRGB for both.

const lens: DepthOfField = { focalMM: 50, fStop: 1.4, focusM: 8, sensorLongMM: 36 }

function build(doc: Scene, materialFor: (o: { ground: boolean; color: string }) => Material) {
  const scene = new THREE.Scene()
  scene.background = new THREE.Color('#9db8d0')
  const root = new THREE.Group()
  root.quaternion.copy(docToView)
  scene.add(root)
  const world = worldOf(doc.objects)
  const colors = ['#c0504d', '#4f81bd', '#9bbb59']
  let n = 0
  for (const o of doc.objects) {
    if (o.kind !== 'primitive' || !o.primitive) continue
    const ground = o.primitive.builtin === 'ground@1'
    const mesh = new THREE.Mesh(primitiveGeometry(o.primitive.builtin, o.primitive.params)!, materialFor({ ground, color: colors[n++ % colors.length] }))
    mesh.matrixAutoUpdate = false
    mesh.matrix.copy(world(o.id))
    root.add(mesh)
  }
  const sun = new THREE.DirectionalLight('#ffffff', 2.5)
  sun.position.set(3, 4, 6)
  scene.add(sun, new THREE.AmbientLight('#ffffff', 0.5))
  return scene
}

const camA = `sobj_${'cama'.padEnd(21, '0')}`

export async function dofLegacy(width: number, height: number): Promise<string> {
  const doc = referenceScene('dof')
  const canvas = document.createElement('canvas')
  const renderer = new THREE.WebGLRenderer({ canvas, antialias: false, preserveDrawingBuffer: true })
  renderer.setPixelRatio(1)
  renderer.setSize(width, height, false)
  renderer.toneMapping = THREE.ACESFilmicToneMapping
  const scene = build(doc, (o) => (o.ground ? makeMaterial(catalogMaterial('tile@1')!, false) : new THREE.MeshStandardMaterial({ color: o.color, roughness: 0.7 })))
  const { camera } = shotCamera(doc, camA, width / height)
  const read = new THREE.WebGLRenderTarget(width, height, { type: THREE.HalfFloatType, depthTexture: new THREE.DepthTexture(width, height) })
  const write = new THREE.WebGLRenderTarget(width, height, { type: THREE.HalfFloatType })
  renderer.setRenderTarget(read)
  renderer.render(scene, camera)
  const dof = new DofPass(() => read.depthTexture!)
  dof.setSize(width, height)
  dof.setLens(lens, camera.near, camera.far, Math.max(width, height))
  dof.render(renderer, write, read)
  const output = new OutputPass()
  output.renderToScreen = true
  output.render(renderer, null as unknown as THREE.WebGLRenderTarget, write)
  const url = canvas.toDataURL('image/png')
  renderer.dispose()
  return url
}

export async function dofTSL(width: number, height: number, forceWebGL = false): Promise<string> {
  const doc = referenceScene('dof')
  const renderer = new WebGPURenderer({ antialias: false, forceWebGL })
  await renderer.init()
  renderer.setPixelRatio(1)
  renderer.setSize(width, height, false)
  const scene = build(doc, (o) => (o.ground ? tslPatternMaterial('tile@1') : new THREE.MeshStandardMaterial({ color: o.color, roughness: 0.7 })))
  const { camera } = shotCamera(doc, camA, width / height)
  const scenePass = pass(scene, camera, { samples: 0 })
  const dof = depthOfField(scenePass.getTextureNode('output'), scenePass.getTextureNode('depth'), width, height)
  dof.setLens(lens, camera.near, camera.far, Math.max(width, height))
  const pipeline = new RenderPipeline(renderer)
  pipeline.outputColorTransform = false
  pipeline.outputNode = renderOutput(dof.output, THREE.ACESFilmicToneMapping, THREE.SRGBColorSpace)
  const target = new RenderTarget(width, height, { type: UnsignedByteType, depthBuffer: false })
  renderer.setRenderTarget(target)
  pipeline.render()
  renderer.setRenderTarget(null)
  const pixels = (await renderer.readRenderTargetPixelsAsync(target, 0, 0, width, height)) as Uint8Array
  const isWebGPU = (renderer.backend as { isWebGPUBackend?: boolean }).isWebGPUBackend
  dof.dispose()
  renderer.dispose()
  return pixelsToPNG(pixels, width, height, !isWebGPU)
}

export function exposeDof() {
  ;(window as unknown as { __dof: unknown }).__dof = { dofLegacy, dofTSL }
}
