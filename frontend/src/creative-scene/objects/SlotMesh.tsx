import type { ReactNode } from 'react'
import type { BufferGeometry, Mesh } from 'three'
import { renderPolicy } from '../materials/principled.ts'
import { onGeometry, type SlotMaterial } from '../materials/resolve.ts'
import { useSlotMaterial } from '../materials/useSlotMaterial.ts'

/**
 * A mesh drawn with its slot's material in the material looks. See-through
 * ones (materials/principled.ts renderPolicy) cast no shadow; three draws them
 * with the transparent meshes, which the AO's prepass leaves out
 * (render/sceneRenderer.ts). raycast: how it is
 * picked, three's way unless given (a model's, piece by piece). slot: the
 * slot it draws, so a material dropped on it knows where to go (Viewport.tsx).
 */
export function SlotMesh({ geometry, material, slot, doubleSided, castShadow, raycast, children }: { geometry: BufferGeometry; material: SlotMaterial; slot: string | null; doubleSided: boolean; castShadow: boolean; raycast?: Mesh['raycast']; children?: ReactNode }) {
  const instance = useSlotMaterial(onGeometry(material, 'uv' in geometry.attributes), doubleSided)
  const see = renderPolicy(material).seeThrough
  return (
    <mesh geometry={geometry} material={instance} castShadow={castShadow && !see} receiveShadow userData={{ slot }} {...(raycast ? { raycast } : {})}>
      {children}
    </mesh>
  )
}
