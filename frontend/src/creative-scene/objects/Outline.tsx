import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { BackSide, Color, Mesh, type BufferGeometry } from 'three'
import * as tsl from 'three/tsl'
import { MeshBasicNodeMaterial } from 'three/webgpu'
import { toCreasedNormals } from 'three/addons/utils/BufferGeometryUtils.js'
import type { TSLNode } from '../materials/patterns.ts'
import { ASIDE_LAYER } from '../render/sceneRenderer.ts'

// The selection outline (Epic design「编辑器辅助显示」): drei's Outlines in
// TSL. A copy of its parent's geometry with the normals averaged across
// creases, drawn back faces only and pushed out along the normal in clip
// space by a fixed number of device pixels, so thin rods and whole groups
// read alike. R0: no measurable cost.

type Op = (...args: TSLNode[]) => TSLNode
const { normalize, uniform, vec4 } = tsl as unknown as Record<string, Op>
const { cameraProjectionMatrix, modelViewMatrix, positionLocal, normalLocal, screenSize } = tsl as unknown as Record<string, TSLNode>
const Fn = tsl.Fn as unknown as (fn: () => TSLNode) => () => TSLNode

function hullMaterial(thickness: { value: number }): MeshBasicNodeMaterial {
  const m = new MeshBasicNodeMaterial({ side: BackSide })
  m.vertexNode = Fn(() => {
    const clipPosition = cameraProjectionMatrix.mul(modelViewMatrix).mul(vec4(positionLocal, 1)).toVar()
    const clipNormal = cameraProjectionMatrix.mul(modelViewMatrix).mul(vec4(normalLocal, 0))
    const offset = normalize(clipNormal.xy).mul(thickness).div(screenSize).mul(clipPosition.w).mul(2)
    return vec4(clipPosition.xy.add(offset), clipPosition.zw)
  })()
  return m
}

const noRaycast = () => {}

/** Outlines the mesh it is a child of; thickness in device pixels, as drei's. */
export function Outline({ color, thickness = 3 }: { color: string; thickness?: number }) {
  const [shell] = useState(() => {
    const t = uniform(thickness)
    const mesh = new Mesh(undefined, hullMaterial(t))
    mesh.raycast = noRaycast
    // In the picture, out of the AO: the WebGL AO pass culled the back-face shell.
    mesh.layers.set(ASIDE_LAYER)
    return Object.assign(mesh, { thickness: t as { value: number } })
  })
  const from = useRef<BufferGeometry | null>(null)
  // The parent's geometry can change under the same shell (a new primitive size).
  useLayoutEffect(() => {
    const parent = shell.parent as Mesh | null
    const geometry = parent?.geometry ?? null
    if (geometry && geometry !== from.current) {
      const previous = from.current && shell.geometry
      shell.geometry = toCreasedNormals(geometry, Math.PI)
      previous?.dispose()
      from.current = geometry
    }
  })
  useLayoutEffect(() => {
    ;(shell.material as MeshBasicNodeMaterial).color = new Color(color)
    shell.thickness.value = thickness
  }, [shell, color, thickness])
  useEffect(
    () => () => {
      shell.geometry.dispose()
      ;(shell.material as MeshBasicNodeMaterial).dispose()
    },
    [shell],
  )
  return <primitive object={shell} />
}
