import { useFrame, useThree } from '@react-three/fiber'
import { useEffect, useMemo, useRef } from 'react'
import { MeshBasicMaterial, type Group } from 'three'
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
// The group follows the object's world matrix; nothing here is picked
// (faceEdit/meshes.ts:pickTriangle casts against the edit meshes).

const noRaycast = () => {}
const selectedColor = '#ff8a00'

export function FaceOverlay({ objectID, meshes, selected }: { objectID: string; meshes: readonly EditMesh[]; selected: FaceSelection }) {
  const scene = useThree((s) => s.scene)
  const group = useRef<Group>(null)
  useFrame(() => {
    const node = scene.getObjectByName(objectID)
    const g = group.current
    if (!node || !g) return
    meshWorld(node, g.matrix)
    g.matrixWorldNeedsUpdate = true
  })
  return (
    <group ref={group} matrixAutoUpdate={false}>
      {meshes.map((m) => (
        <MeshOverlay key={m.key} mesh={m} triangles={selected[m.key]} />
      ))}
    </group>
  )
}

function MeshOverlay({ mesh, triangles }: { mesh: EditMesh; triangles: readonly number[] | undefined }) {
  const invalidate = useThree((s) => s.invalidate)
  const lines = useMemo(() => {
    const geometry = new LineSegmentsGeometry()
    geometry.setPositions(patchEdges(mesh.geometry, mesh.shape()))
    const l = new LineSegments2(geometry, new Line2NodeMaterial({ color: 0x111111, linewidth: 1.5, transparent: true, opacity: 0.85 }))
    l.raycast = noRaycast
    l.layers.set(ASIDE_LAYER)
    l.renderOrder = 2
    return l
  }, [mesh])
  useEffect(
    () => () => {
      lines.geometry.dispose()
      lines.material.dispose()
    },
    [lines],
  )
  const tint = useMemo(() => (triangles?.length ? extract(mesh.geometry, triangles) : null), [mesh, triangles])
  useEffect(() => () => tint?.dispose(), [tint])
  const material = useMemo(
    () => new MeshBasicMaterial({ color: selectedColor, transparent: true, opacity: 0.6, depthWrite: false, polygonOffset: true, polygonOffsetFactor: -2, polygonOffsetUnits: -2 }),
    [],
  )
  useEffect(() => () => material.dispose(), [material])
  useEffect(() => invalidate(), [lines, tint, invalidate])
  return (
    <>
      <primitive object={lines} />
      {tint && <mesh geometry={tint} material={material} layers={ASIDE_LAYER} raycast={noRaycast} renderOrder={1} />}
    </>
  )
}
