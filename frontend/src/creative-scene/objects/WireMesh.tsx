import type { BufferGeometry } from 'three'
import { edgesOf, hiddenMaterial, wireColor } from './wireframe.ts'

const noRaycast = () => {}

/** A mesh in wireframe: its edges, in the selection colour when selected; the mesh itself only for picking. */
export function WireMesh({ geometry, edge }: { geometry: BufferGeometry; edge: string | null }) {
  return (
    <mesh geometry={geometry} material={hiddenMaterial}>
      <lineSegments geometry={edgesOf(geometry)} raycast={noRaycast}>
        <lineBasicMaterial color={edge ?? wireColor} />
      </lineSegments>
    </mesh>
  )
}
