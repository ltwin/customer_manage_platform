import { forwardRef, useImperativeHandle, useMemo, useEffect } from 'react'
import { useFrame } from '@react-three/fiber'
import { BackSide, Color, Mesh, Plane, PlaneGeometry, Vector3 } from 'three'
import * as tsl from 'three/tsl'
import { MeshBasicNodeMaterial } from 'three/webgpu'
import type { TSLNode } from '../materials/patterns.ts'

// The viewport's ground grid: drei's Grid (infiniteGrid) in TSL, line for
// line. A unit plane stretched by the fade distance; cell and section lines
// a pixel wide by their screen-space derivatives, fading with the distance
// from the camera's foot on the plane. Transparent, so the AO's prepass
// leaves it out; it is also set aside from the AO as an overlay.

type Op = (...args: TSLNode[]) => TSLNode
const { abs, distance, float, fract, fwidth, min, mix, pow, uniform, varying, vec3, vec4 } = tsl as unknown as Record<string, Op>
const { cameraProjectionMatrix, cameraViewMatrix, modelWorldMatrix, positionLocal } = tsl as unknown as Record<string, TSLNode>
const Fn = tsl.Fn as unknown as (fn: () => TSLNode) => () => TSLNode
const Discard = tsl.Discard as unknown as (condition: TSLNode) => void

export type GridProps = {
  position?: [number, number, number]
  rotation?: [number, number, number]
  cellSize: number
  sectionSize: number
  cellColor: string
  sectionColor: string
  fadeDistance: number
  cellThickness?: number
  sectionThickness?: number
  fadeStrength?: number
}

export const InfiniteGrid = forwardRef<Mesh, GridProps>(function InfiniteGrid({ position, rotation, cellSize, sectionSize, cellColor, sectionColor, fadeDistance, cellThickness = 0.5, sectionThickness = 1, fadeStrength = 1 }, ref) {
  const grid = useMemo(() => {
    const u = {
      cellSize: uniform(cellSize),
      sectionSize: uniform(sectionSize),
      cellColor: uniform(new Color(cellColor)),
      sectionColor: uniform(new Color(sectionColor)),
      fadeDistance: uniform(fadeDistance),
      fadeStrength: uniform(fadeStrength),
      cellThickness: uniform(cellThickness),
      sectionThickness: uniform(sectionThickness),
      camOnPlane: uniform(new Vector3()),
    }
    const local = positionLocal.xzy.mul(u.fadeDistance.add(1))
    const world = modelWorldMatrix.mul(vec4(local, 1))
    const localPosition = varying(local, 'vGridLocal')
    const worldPosition = varying(world, 'vGridWorld')
    const material = new MeshBasicNodeMaterial({ side: BackSide, transparent: true })
    material.fog = false
    material.vertexNode = cameraProjectionMatrix.mul(cameraViewMatrix).mul(world)
    const lines = (size: TSLNode, thickness: TSLNode) => {
      const r = localPosition.xz.div(size)
      const g = abs(fract(r.sub(0.5)).sub(0.5)).div(fwidth(r))
      const line = min(g.x, g.y).add(1).sub(thickness)
      return float(1).sub(min(line, 1))
    }
    material.colorNode = Fn(() => {
      const g1 = lines(u.cellSize, u.cellThickness).toVar()
      const g2 = lines(u.sectionSize, u.sectionThickness).toVar()
      const d = float(1).sub(min(distance(u.camOnPlane, worldPosition.xyz).div(u.fadeDistance), 1))
      const color = mix(u.cellColor, u.sectionColor, min(1, u.sectionThickness.mul(g2)))
      const a0 = g1.add(g2).mul(pow(d, u.fadeStrength))
      const a = mix(a0.mul(0.75), a0, g2).toVar()
      Discard(a.lessThanEqual(0))
      return vec4(vec3(color), a)
    })()
    const mesh = new Mesh(new PlaneGeometry(), material)
    mesh.frustumCulled = false
    return Object.assign(mesh, { uniforms: u })
    // Parameters update the uniforms below; the material is made once.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  useImperativeHandle(ref, () => grid, [grid])
  useEffect(() => {
    const u = grid.uniforms
    u.cellSize.value = cellSize
    u.sectionSize.value = sectionSize
    u.cellColor.value.set(cellColor)
    u.sectionColor.value.set(sectionColor)
    u.fadeDistance.value = fadeDistance
    u.fadeStrength.value = fadeStrength
    u.cellThickness.value = cellThickness
    u.sectionThickness.value = sectionThickness
  }, [grid, cellSize, sectionSize, cellColor, sectionColor, fadeDistance, fadeStrength, cellThickness, sectionThickness])
  useEffect(
    () => () => {
      grid.geometry.dispose()
      ;(grid.material as MeshBasicNodeMaterial).dispose()
    },
    [grid],
  )
  // The camera's foot on the grid's plane, where the fade is measured from.
  const plane = useMemo(() => new Plane(), [])
  useFrame((state) => {
    plane.setFromNormalAndCoplanarPoint(new Vector3(0, 1, 0), new Vector3()).applyMatrix4(grid.matrixWorld)
    plane.projectPoint(state.camera.position, grid.uniforms.camOnPlane.value)
  })
  return <primitive object={grid} position={position} rotation={rotation} />
})
