import type { BufferGeometry, Mesh } from 'three'
import { edgesOf, hiddenMaterial, wireColor } from './wireframe.ts'

const noRaycast = () => {}

/** A mesh in wireframe: its edges, in the selection colour when selected; the mesh itself only for picking (raycast as SlotMesh's). */
export function WireMesh({ geometry, edge, raycast }: { geometry: BufferGeometry; edge: string | null; raycast?: Mesh['raycast'] }) {
  return (
    <mesh geometry={geometry} material={hiddenMaterial} {...(raycast ? { raycast } : {})}>
      <lineSegments geometry={edgesOf(geometry)} raycast={noRaycast}>
        <lineBasicMaterial color={edge ?? wireColor} />
      </lineSegments>
    </mesh>
  )
}
