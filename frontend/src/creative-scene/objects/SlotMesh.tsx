import { useLayoutEffect, useRef, type ReactNode } from 'react'
import type { BufferGeometry, Mesh } from 'three'
import { isSeeThrough } from '../materials/instances.ts'
import type { SlotMaterial } from '../materials/resolve.ts'
import { useSlotMaterial } from '../materials/useSlotMaterial.ts'
import { transparentMeshes } from '../render/transparent.ts'

/**
 * A mesh drawn with its slot's material in the material looks. See-through
 * ones stay out of the AO and cast no shadow.
 */
export function SlotMesh({ geometry, material, doubleSided, castShadow, children }: { geometry: BufferGeometry; material: SlotMaterial; doubleSided: boolean; castShadow: boolean; children?: ReactNode }) {
  const instance = useSlotMaterial(material, doubleSided)
  const ref = useRef<Mesh>(null)
  const see = isSeeThrough(material)
  useLayoutEffect(() => {
    const mesh = ref.current
    if (!see || !mesh) return
    transparentMeshes.add(mesh)
    return () => {
      transparentMeshes.delete(mesh)
    }
  }, [see])
  return (
    <mesh ref={ref} geometry={geometry} material={instance} castShadow={castShadow && !see} receiveShadow>
      {children}
    </mesh>
  )
}
