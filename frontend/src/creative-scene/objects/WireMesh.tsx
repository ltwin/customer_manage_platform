import type { BufferGeometry, Matrix4 } from 'three'
import { edgesOf, hiddenMaterial, wireColor } from './wireframe.ts'

const noRaycast = () => {}

/** A mesh in wireframe: its edges, in the selection colour when selected; the mesh itself only for picking. */
export function WireMesh({ geometry, edge, matrix }: { geometry: BufferGeometry; edge: string | null; matrix?: Matrix4 }) {
  return (
    <mesh geometry={geometry} material={hiddenMaterial} {...(matrix ? { matrix, matrixAutoUpdate: false } : {})}>
      <lineSegments geometry={edgesOf(geometry)} raycast={noRaycast}>
        <lineBasicMaterial color={edge ?? wireColor} />
      </lineSegments>
    </mesh>
  )
}
