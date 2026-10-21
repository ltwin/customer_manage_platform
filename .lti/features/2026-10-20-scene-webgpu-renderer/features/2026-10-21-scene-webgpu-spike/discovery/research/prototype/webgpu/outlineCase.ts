import * as THREE from 'three'
import { cameraProjectionMatrix, float, Fn, modelViewMatrix, normalize, normalLocal, pass, positionLocal, renderOutput, screenSize, uniform, vec4 } from 'three/tsl'
import { MeshBasicNodeMaterial, RenderPipeline, RenderTarget, UnsignedByteType, WebGPURenderer } from 'three/webgpu'
import { outline } from 'three/addons/tsl/display/OutlineNode.js'
import { LineSegments2 } from 'three/addons/lines/webgpu/LineSegments2.js'
import { LineSegmentsGeometry } from 'three/addons/lines/LineSegmentsGeometry.js'
import { Line2NodeMaterial } from 'three/webgpu'
import { primitiveGeometry } from '../../src/creative-scene/builtins/geometry.ts'
import { clay, selectedEdge } from '../../src/creative-scene/objects/look.ts'
import { docToView } from '../../src/creative-scene/space.ts'
import { worldOf } from '../../src/creative-scene/state/world.ts'
import { referenceScene, shotCamera } from './doc.ts'
import { pixelsToPNG } from './materialsCase.ts'
import { controlFrames } from './frames.ts'

// Group 3: the selection outline two ways, on the noon yard plus a 2 cm rod
// and a group of two boxes, with the ball, the rod and the group selected:
//  - hull: drei's Outlines in TSL (a back-face shell pushed 3 px out along
//    the normal in clip space, per mesh), as the editor draws it now;
//  - node: three's OutlineNode (a mask of the selection, edges found in
//    screen space, the selection's union outlined, hidden edges too).
// A flat panel (a 1 mm box) gets drei Edges' replacement, LineSegments2
// with Line2NodeMaterial, 2 px. Also times 120 frames of each.

/* eslint-disable @typescript-eslint/no-explicit-any -- TSL nodes are loosely typed in three's typings */
type N = any
type Variant = 'none' | 'hull' | 'node'

function hullMaterial(color: string, thicknessPx: number): MeshBasicNodeMaterial {
  const thickness = uniform(thicknessPx)
  const m = new MeshBasicNodeMaterial({ color, side: THREE.BackSide })
  m.vertexNode = Fn(() => {
    const clipPosition = cameraProjectionMatrix.mul(modelViewMatrix).mul(vec4(positionLocal, 1)).toVar()
    const clipNormal = cameraProjectionMatrix.mul(modelViewMatrix).mul(vec4(normalLocal, 0))
    const offset = normalize(clipNormal.xy).mul(thickness).div(screenSize).mul(clipPosition.w).mul(2)
    return vec4(clipPosition.xy.add(offset), clipPosition.zw)
  })()
  return m
}

function build(variant: Variant) {
  const doc = referenceScene('noon')
  const scene = new THREE.Scene()
  scene.background = new THREE.Color('#9db8d0')
  const root = new THREE.Group()
  root.quaternion.copy(docToView)
  scene.add(root)
  const world = worldOf(doc.objects)
  const material = new THREE.MeshStandardMaterial({ color: clay, roughness: 1 })
  const selected: THREE.Object3D[] = []
  const add = (geometry: THREE.BufferGeometry, matrix: THREE.Matrix4, select: boolean, parent: THREE.Object3D = root) => {
    const mesh = new THREE.Mesh(geometry, material)
    mesh.matrixAutoUpdate = false
    mesh.matrix.copy(matrix)
    parent.add(mesh)
    if (select) selected.push(mesh)
    if (select && variant === 'hull') {
      const shell = new THREE.Mesh(geometry, hullMaterial(selectedEdge, 3))
      mesh.add(shell)
    }
    return mesh
  }
  for (const o of doc.objects) {
    if (o.kind !== 'primitive' || !o.primitive) continue
    add(primitiveGeometry(o.primitive.builtin, o.primitive.params)!, world(o.id), o.name === 'ball')
  }
  // A 2 cm rod leaning in front, and a group of two boxes on the right.
  add(new THREE.CylinderGeometry(0.01, 0.01, 1.6, 12).translate(0, 0.8, 0), new THREE.Matrix4().compose(new THREE.Vector3(-0.6, 0, 2.2), new THREE.Quaternion().setFromEuler(new THREE.Euler(0, 0, 0.35)), new THREE.Vector3(1, 1, 1)), true)
  const group = new THREE.Group()
  group.matrixAutoUpdate = false
  group.matrix.makeTranslation(0.95, 0, 3.2)
  root.add(group)
  add(new THREE.BoxGeometry(0.5, 0.5, 0.5).translate(0, 0.25, 0), new THREE.Matrix4(), true, group)
  add(new THREE.BoxGeometry(0.4, 0.4, 0.4).translate(0, 0.7, 0), new THREE.Matrix4(), true, group)
  // A flat panel with its edges, as drei Edges draws flat objects.
  const panelGeometry = new THREE.BoxGeometry(0.6, 0.001, 0.4)
  const panel = add(panelGeometry, new THREE.Matrix4().makeTranslation(-0.9, 0.01, 3.0), false)
  if (variant !== 'none') {
    const edges = new LineSegmentsGeometry().fromEdgesGeometry(new THREE.EdgesGeometry(panelGeometry, 20))
    const line = new LineSegments2(edges, new Line2NodeMaterial({ color: selectedEdge, linewidth: 2 }))
    panel.add(line)
  }
  const sun = new THREE.DirectionalLight('#ffffff', 2.2)
  sun.position.set(30, -20, 40)
  scene.add(sun, new THREE.AmbientLight('#ffffff', 0.7))
  return { scene, selected, camera: shotCamera(doc, null, 3 / 2).camera }
}

export async function renderOutline(variant: Variant, width: number, height: number, frames = 120): Promise<{ png: string; msPerFrame: number }> {
  const renderer = new WebGPURenderer({ antialias: false })
  await renderer.init()
  renderer.setPixelRatio(1)
  renderer.setSize(width, height, false)
  const { scene, selected, camera } = build(variant)
  camera.aspect = width / height
  camera.updateProjectionMatrix()
  const scenePass = pass(scene, camera, { samples: 0 })
  let color: N = scenePass
  if (variant === 'node') {
    const o = outline(scene, camera, { selectedObjects: selected, edgeThickness: float(1.5), edgeGlow: float(0), downSampleRatio: 1 })
    color = scenePass.add(o.visibleEdge.mul(new THREE.Color(selectedEdge)).add(o.hiddenEdge.mul(new THREE.Color(selectedEdge)).mul(0.35)).mul(3))
  }
  const pipeline = new RenderPipeline(renderer)
  pipeline.outputColorTransform = false
  pipeline.outputNode = renderOutput(color, THREE.ACESFilmicToneMapping, THREE.SRGBColorSpace)
  const target = new RenderTarget(width, height, { type: UnsignedByteType, depthBuffer: false })
  // Passes draw once a frame: every render here is a frame of its own.
  const clock = controlFrames(renderer)
  clock.reset()
  renderer.setRenderTarget(target)
  clock.step()
  pipeline.render()
  await renderer.readRenderTargetPixelsAsync(target, 0, 0, 1, 1)
  const started = performance.now()
  for (let i = 0; i < frames; i++) {
    clock.step()
    pipeline.render()
  }
  await renderer.readRenderTargetPixelsAsync(target, 0, 0, 1, 1)
  const msPerFrame = (performance.now() - started) / frames
  const pixels = (await renderer.readRenderTargetPixelsAsync(target, 0, 0, width, height)) as Uint8Array
  renderer.setRenderTarget(null)
  renderer.dispose()
  return { png: pixelsToPNG(pixels, width, height, false), msPerFrame }
}

export function exposeOutline() {
  ;(window as unknown as { __outline: unknown }).__outline = { renderOutline }
}
