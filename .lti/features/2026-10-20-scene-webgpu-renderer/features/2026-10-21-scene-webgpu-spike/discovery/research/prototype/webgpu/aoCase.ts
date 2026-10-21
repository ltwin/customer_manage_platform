import * as THREE from 'three'
import { mrt, normalView, pass, vec4 } from 'three/tsl'
import { RenderPipeline, RenderTarget, UnsignedByteType, WebGPURenderer } from 'three/webgpu'
import { ao } from 'three/addons/tsl/display/GTAONode.js'
import { primitiveGeometry } from '../../src/creative-scene/builtins/geometry.ts'
import { docToView } from '../../src/creative-scene/space.ts'
import { worldOf } from '../../src/creative-scene/state/world.ts'
import { aoOrtho } from './GTAONodeOrtho.js'
import { referenceScene } from './doc.ts'
import { controlFrames } from './frames.ts'
import { pixelsToPNG } from './materialsCase.ts'

// Group 2: GTAO alone, as grey, for the room seen from front right above
// (the baseline's orthographic view: view space, Z up, 80 px a metre), by
// three's GTAONode and by the copy whose view direction is fixed for
// orthographic cameras, through an orthographic and a perspective camera.

function roomScene(): THREE.Scene {
  const doc = referenceScene('room')
  const scene = new THREE.Scene()
  const root = new THREE.Group()
  root.quaternion.copy(docToView)
  scene.add(root)
  const world = worldOf(doc.objects)
  const material = new THREE.MeshStandardMaterial({ color: '#d9d5cd' })
  for (const o of doc.objects) {
    if (o.kind !== 'primitive' || !o.primitive) continue
    const mesh = new THREE.Mesh(primitiveGeometry(o.primitive.builtin, o.primitive.params)!, material)
    mesh.matrixAutoUpdate = false
    mesh.matrix.copy(world(o.id))
    root.add(mesh)
  }
  return scene
}

function camera(kind: 'ortho' | 'persp', width: number, height: number): THREE.Camera {
  const c = kind === 'ortho' ? new THREE.OrthographicCamera(-width / 2, width / 2, height / 2, -height / 2, -5000, 5000) : new THREE.PerspectiveCamera(45, width / height, 0.05, 5000)
  if (kind === 'ortho') (c as THREE.OrthographicCamera).zoom = 80
  c.up.set(0, 0, 1)
  c.position.set(7, -9, 7)
  c.lookAt(0, 0, 0.5)
  ;(c as THREE.PerspectiveCamera).updateProjectionMatrix()
  c.updateMatrixWorld()
  return c
}

export async function renderAO(kind: 'ortho' | 'persp', node: 'three' | 'fork', width: number, height: number): Promise<string> {
  const renderer = new WebGPURenderer({ antialias: false })
  await renderer.init()
  renderer.setPixelRatio(1)
  renderer.setSize(width, height, false)
  const frames = controlFrames(renderer)
  const scene = roomScene()
  const cam = camera(kind, width, height)
  const prePass = pass(scene, cam, { samples: 0 })
  prePass.setMRT(mrt({ output: normalView }))
  const aoPass = (node === 'fork' ? aoOrtho : ao)(prePass.getTextureNode('depth'), prePass.getTextureNode(), cam)
  const pipeline = new RenderPipeline(renderer)
  pipeline.outputColorTransform = false
  pipeline.outputNode = vec4(aoPass.getTextureNode().r.toVec3(), 1)
  const target = new RenderTarget(width, height, { type: UnsignedByteType, depthBuffer: false })
  frames.reset()
  frames.step()
  renderer.setRenderTarget(target)
  pipeline.render()
  renderer.setRenderTarget(null)
  const pixels = (await renderer.readRenderTargetPixelsAsync(target, 0, 0, width, height)) as Uint8Array
  renderer.dispose()
  return pixelsToPNG(pixels, width, height, false)
}

export function exposeAO() {
  ;(window as unknown as { __ao: unknown }).__ao = { renderAO }
}
