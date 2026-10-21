import type { ReactNode } from 'react'
import type { BufferGeometry } from 'three'
import { isSeeThrough } from '../materials/instances.ts'
import type { SlotMaterial } from '../materials/resolve.ts'
import { useSlotMaterial } from '../materials/useSlotMaterial.ts'

/**
 * A mesh drawn with its slot's material in the material looks. See-through
 * ones cast no shadow; three draws them with the transparent meshes, which
 * the AO's prepass leaves out (render/sceneRenderer.ts).
 */
export function SlotMesh({ geometry, material, doubleSided, castShadow, children }: { geometry: BufferGeometry; material: SlotMaterial; doubleSided: boolean; castShadow: boolean; children?: ReactNode }) {
  const instance = useSlotMaterial(material, doubleSided)
  const see = isSeeThrough(material)
  return (
    <mesh geometry={geometry} material={instance} castShadow={castShadow && !see} receiveShadow>
      {children}
    </mesh>
  )
}
