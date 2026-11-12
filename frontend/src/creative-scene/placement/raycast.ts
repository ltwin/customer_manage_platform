import { BufferGeometry, Matrix3, Matrix4, Mesh, SkinnedMesh, type Intersection, type Object3D, type Raycaster } from 'three'
import { acceleratedRaycast, MeshBVH } from 'three-mesh-bvh'
import { pieceViews } from '../render/pieces.ts'
import type { Posed } from '../rig/skin.ts'

// Snapping casts a ray through the scene on every frame of a drag, and one
// model may have 300 000 triangles. Model meshes get a BVH the first time it
// is needed, kept on their shared geometry, and raycast through it. The BVH
// is indirect: it sorts a buffer of its own and leaves the index alone, where
// a merged geometry keeps its pieces' ranges (render/pieces.ts). Kept by
// geometry, not by mesh: a mesh keeps its key when its geometry changes
// (glass made opaque draws the merged geometry, a new revision its parts).

export function accelerateModels(root: Object3D, isModel: (objectID: string) => boolean) {
  root.traverse((node) => {
    const id: unknown = node.userData?.objectID
    if (typeof id !== 'string' || !isModel(id)) return
    node.traverse((m) => {
      // Not the outline's shell or edge lines on the aside layer: they pick nothing.
      if (!(m instanceof Mesh) || !m.layers.isEnabled(0)) return
      if (m instanceof SkinnedMesh) {
        // Picked by its posed geometry (raycastPosed), which takes the BVH.
        const posed = m.userData.posed as Posed | undefined
        if (posed) posed.geometry.boundsTree ??= new MeshBVH(posed.geometry, { indirect: true })
        return
      }
      m.geometry.boundsTree ??= new MeshBVH(m.geometry, { indirect: true })
      m.raycast = acceleratedRaycast
    })
  })
}

/**
 * How a model's mesh is picked until snapping gives it a BVH: piece by piece,
 * as its parts were before the merge. three's own test on each piece's range
 * view (its sphere, its box, then its triangles), not every triangle of the
 * slot behind one large sphere. A geometry without pieces is picked as three
 * picks it.
 */
export function raycastPieces(this: Mesh, raycaster: Raycaster, intersects: Intersection[]) {
  const geometry = this.geometry
  try {
    for (const view of pieceViews(geometry)) {
      this.geometry = view
      Mesh.prototype.raycast.call(this, raycaster, intersects)
    }
  } finally {
    this.geometry = geometry
  }
}

const empty = new BufferGeometry()
const proxy = new Mesh(empty)
const toMesh = new Matrix4()
const normalMatrix = new Matrix3()

/**
 * How a skinned mesh is picked: its static geometry in the current pose
 * (rig/skin.ts Posed) placed by its model root, through the BVH snapping
 * gave it, not three's own test, which skins every vertex again. The hits
 * name the skinned mesh, their face normals in its space as three's are.
 */
export function raycastPosed(this: Mesh, raycaster: Raycaster, intersects: Intersection[]) {
  const posed = this.userData.posed as Posed | undefined
  if (!posed) return
  proxy.geometry = posed.geometry
  proxy.material = this.material
  proxy.matrixWorld.copy(posed.root.matrixWorld)
  const found: Intersection[] = []
  try {
    ;((posed.geometry as { boundsTree?: MeshBVH }).boundsTree ? acceleratedRaycast : Mesh.prototype.raycast).call(proxy, raycaster, found)
  } finally {
    // Not holding on to a geometry the cache may free.
    proxy.geometry = empty
  }
  if (found.length === 0) return
  normalMatrix.getNormalMatrix(toMesh.copy(this.matrixWorld).invert().multiply(posed.root.matrixWorld))
  for (const hit of found) intersects.push({ ...hit, object: this, face: hit.face && { ...hit.face, normal: hit.face.normal.clone().applyNormalMatrix(normalMatrix) } })
}

/** Part of a ghost: what will land, never a surface to land on. */
export function ghostOf(o: Object3D): boolean {
  for (let n: Object3D | null = o; n; n = n.parent) if (n.userData?.ghost) return true
  return false
}
