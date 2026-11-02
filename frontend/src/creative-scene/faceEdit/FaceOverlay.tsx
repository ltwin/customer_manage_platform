import { useFrame, useThree } from '@react-three/fiber'
import { useEffect, useRef, type RefObject } from 'react'
import { DoubleSide, Matrix4, Mesh, MeshBasicMaterial, Vector3, type Camera, type Group, type OrthographicCamera } from 'three'
import { Line2NodeMaterial } from 'three/webgpu'
import { LineSegments2 } from 'three/addons/lines/webgpu/LineSegments2.js'
import { LineSegmentsGeometry } from 'three/addons/lines/LineSegmentsGeometry.js'
import { extract } from '../models/faces.ts'
import { ASIDE_LAYER } from '../render/sceneRenderer.ts'
import { meshWorld, patchEdges, type EditMesh } from './meshes.ts'
import type { FaceSelection } from './state.ts'

// What face edit draws over its object: the lines between flat patches and
// the selected triangles tinted, as Blender's face select mode does. On the
// aside layer, like selection edges: in the viewport, out of AO and shots.
// The groups follow the object's world matrix; nothing here is picked
// (faceEdit/meshes.ts:pickTriangle casts against the edit meshes).
//
// Each piece is made in the effect that frees it: StrictMode's second mount
// then makes new ones. Made in render and freed by an effect, the second
// mount drew freed buffers, and the lit workbench's frames failed to submit
// ("used in submit while destroyed"): the viewport flickered, no tint showed.

const noRaycast = () => {}
const selectedColor = '#ff8a00'

// The real-time GI lays the aside layer over its picture where the layer's
// depth is no farther than the scene's (realtimeGI.ts aside): on the surface
// itself, the tint lost that test and the lines flickered in it as TRAA
// jittered the view. Both are drawn a little toward the eye, along the rays
// they lie on, so on the same pixels: by this share of their distance (1 cm
// at 10 m, some 80 depth steps of a 32-bit buffer with the viewport's 0.05 m
// near plane), or for an orthographic camera by this many times 10 m. The
// lines twice as far, over the tint.
const tintPull = 1e-3
const linePull = 2e-3

export function FaceOverlay({ objectID, meshes, selected }: { objectID: string; meshes: readonly EditMesh[]; selected: FaceSelection }) {
  const scene = useThree((s) => s.scene)
  const tints = useRef<Group>(null)
  const lines = useRef<Group>(null)
  useFrame(({ camera }) => {
    const node = scene.getObjectByName(objectID)
    if (!node || !tints.current || !lines.current) return
    const world = meshWorld(node, objectWorld)
    for (const [g, pull] of [[tints.current, tintPull], [lines.current, linePull]] as const) {
      towardEye(camera, pull, g.matrix).multiply(world)
      g.matrixWorldNeedsUpdate = true
    }
  })
  return (
    <>
      <group ref={tints} matrixAutoUpdate={false} />
      <group ref={lines} matrixAutoUpdate={false} />
      {meshes.map((m) => (
        <MeshOverlay key={m.key} mesh={m} triangles={selected[m.key]} tints={tints} lines={lines} />
      ))}
    </>
  )
}

const objectWorld = /*@__PURE__*/ new Matrix4()
const eye = /*@__PURE__*/ new Vector3()
const step = /*@__PURE__*/ new Matrix4()

/** Scales toward the eye (perspective) or moves along the view (orthographic): every point stays on its ray. */
function towardEye(camera: Camera, pull: number, out: Matrix4): Matrix4 {
  eye.setFromMatrixPosition(camera.matrixWorld)
  if ((camera as OrthographicCamera).isOrthographicCamera) {
    // The camera looks down its -Z.
    eye.setFromMatrixColumn(camera.matrixWorld, 2).normalize().multiplyScalar(pull * 10)
    return out.makeTranslation(eye)
  }
  out.makeTranslation(eye)
  out.multiply(step.makeScale(1 - pull, 1 - pull, 1 - pull))
  return out.multiply(step.makeTranslation(eye.negate()))
}

function MeshOverlay({ mesh, triangles, tints, lines }: { mesh: EditMesh; triangles: readonly number[] | undefined; tints: RefObject<Group | null>; lines: RefObject<Group | null> }) {
  const invalidate = useThree((s) => s.invalidate)
  useEffect(() => {
    const g = lines.current
    if (!g) return
    const geometry = new LineSegmentsGeometry()
    geometry.setPositions(patchEdges(mesh.geometry, mesh.shape()))
    const edges = new LineSegments2(geometry, new Line2NodeMaterial({ color: 0x111111, linewidth: 1.5, transparent: true, opacity: 0.85 }))
    edges.raycast = noRaycast
    edges.layers.set(ASIDE_LAYER)
    edges.renderOrder = 2
    g.add(edges)
    invalidate()
    return () => {
      g.remove(edges)
      geometry.dispose()
      edges.material.dispose()
      invalidate()
    }
  }, [mesh, lines, invalidate])
  useEffect(() => {
    const g = tints.current
    if (!g || !triangles?.length) return
    // Its depth written: the real-time GI's aside pass has no other. Both
    // sides, as picking and the object are: an inner wall picked shows.
    const material = new MeshBasicMaterial({ color: selectedColor, transparent: true, opacity: 0.6, side: DoubleSide })
    const tint = new Mesh(extract(mesh.geometry, triangles), material)
    tint.raycast = noRaycast
    tint.layers.set(ASIDE_LAYER)
    tint.renderOrder = 1
    g.add(tint)
    invalidate()
    return () => {
      g.remove(tint)
      tint.geometry.dispose()
      material.dispose()
      invalidate()
    }
  }, [mesh, triangles, tints, invalidate])
  return null
}
