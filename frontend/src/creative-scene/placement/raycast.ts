import { Mesh, type Intersection, type Object3D, type Raycaster } from 'three'
import { acceleratedRaycast, MeshBVH } from 'three-mesh-bvh'
import { pieceViews } from '../render/pieces.ts'

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

/** Part of a ghost: what will land, never a surface to land on. */
export function ghostOf(o: Object3D): boolean {
  for (let n: Object3D | null = o; n; n = n.parent) if (n.userData?.ghost) return true
  return false
}
