import { Mesh, type Object3D } from 'three'
import { acceleratedRaycast, MeshBVH } from 'three-mesh-bvh'

// Snapping casts a ray through the scene on every frame of a drag, and one
// model may have 300 000 triangles. Model meshes get a BVH the first time it
// is needed, kept on their shared geometry, and raycast through it.

const accelerated = new WeakSet<Mesh>()

export function accelerateModels(root: Object3D, isModel: (objectID: string) => boolean) {
  root.traverse((node) => {
    const id: unknown = node.userData?.objectID
    if (typeof id !== 'string' || !isModel(id)) return
    node.traverse((m) => {
      if (!(m instanceof Mesh) || accelerated.has(m)) return
      m.geometry.boundsTree ??= new MeshBVH(m.geometry)
      m.raycast = acceleratedRaycast
      accelerated.add(m)
    })
  })
}

/** Part of a ghost: what will land, never a surface to land on. */
export function ghostOf(o: Object3D): boolean {
  for (let n: Object3D | null = o; n; n = n.parent) if (n.userData?.ghost) return true
  return false
}
