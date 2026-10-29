import { DoubleSide, Matrix4, Mesh, MeshBasicMaterial, type BufferGeometry, type Object3D, type Raycaster } from 'three'
import { acceleratedRaycast, MeshBVH } from 'three-mesh-bvh'
import { faceTopology, type FaceTopology } from '../models/faceTopology.ts'
import { slotOfMaterial, topologyOf, type OwnedMesh } from '../models/faces.ts'
import type { MeshIdentity } from '../sandbox/gltf.ts'
import { docToView } from '../space.ts'
import { meshKey } from './state.ts'

// What face edit works on: each mesh of the object in model space, numbered
// as the assignment numbers triangles (index order), never the drawn parts
// that loading split and merged (design「模块职责」faceEdit/).

export type EditMesh = {
  key: string
  identity: MeshIdentity
  topology: string
  geometry: BufferGeometry
  /** The GLB's own slot of these triangles; a builtin's is main. */
  slot: string | null
  /** Patches, pieces and neighbours, built the first time a pick needs them. */
  shape(): FaceTopology
}

function editMesh(identity: MeshIdentity, topology: string, geometry: BufferGeometry, slot: string | null): EditMesh {
  let shape: FaceTopology | null = null
  return { key: meshKey(identity), identity, topology, geometry, slot, shape: () => (shape ??= faceTopology(geometry)) }
}

export function modelEditMeshes(owned: readonly OwnedMesh[]): EditMesh[] {
  return owned.map((m) => editMesh(m.identity, m.topology, m.geometry, slotOfMaterial(m.material)))
}

/** A builtin is one mesh at a fixed identity (data: 场景内置基础体「面级材质分配」). */
export function builtinEditMesh(geometry: BufferGeometry): EditMesh {
  return editMesh({ path: [['', 0]], primitive: 0 }, topologyOf(geometry), geometry, 'main')
}

/** Segments, in model space, where flat patches meet or the surface ends: the lines face edit draws. */
export function patchEdges(geometry: BufferGeometry, shape: FaceTopology): Float32Array {
  const position = geometry.attributes.position
  const index = geometry.index
  const vertexOf = (c: number) => (index ? index.getX(c) : c)
  // By welded corner pair: one corner pair of the edge, its first patch, how many share it, whether patches differ.
  const edges = new Map<string, { c0: number; c1: number; patch: number; count: number; mixed: boolean }>()
  for (let t = 0; t < shape.triangleCount; t++) {
    for (let e = 0; e < 3; e++) {
      const c0 = 3 * t + e
      const c1 = 3 * t + ((e + 1) % 3)
      const a = shape.corners[c0]
      const b = shape.corners[c1]
      if (a === b) continue
      const key = a < b ? `${a},${b}` : `${b},${a}`
      const seen = edges.get(key)
      if (!seen) edges.set(key, { c0, c1, patch: shape.patchOf[t], count: 1, mixed: false })
      else {
        seen.count++
        if (seen.patch !== shape.patchOf[t]) seen.mixed = true
      }
    }
  }
  const out: number[] = []
  for (const e of edges.values()) {
    if (e.count > 1 && !e.mixed) continue
    for (const c of [e.c0, e.c1]) {
      const v = vertexOf(c)
      out.push(position.getX(v), position.getY(v), position.getZ(v))
    }
  }
  return new Float32Array(out)
}

const toView = new Matrix4().makeRotationFromQuaternion(docToView)

/**
 * Where an object's meshes stand in the world: its node, then the document
 * to view turn its drawn content sits under (objects/SceneObjects.tsx ObjectNode).
 */
export function meshWorld(node: Object3D, out: Matrix4 = new Matrix4()): Matrix4 {
  node.updateWorldMatrix(true, false)
  return out.multiplyMatrices(node.matrixWorld, toView)
}

const pickMaterial = new MeshBasicMaterial({ side: DoubleSide })
const pickers = new WeakMap<BufferGeometry, Mesh>()

/**
 * The triangle a ray meets first on a mesh standing where `world` puts it:
 * its number in the geometry's index order (the BVH is indirect and leaves
 * the index alone), and how far along the ray.
 */
export function pickTriangle(raycaster: Raycaster, mesh: EditMesh, world: Mesh['matrixWorld']): { triangle: number; distance: number } | null {
  let picker = pickers.get(mesh.geometry)
  if (!picker) {
    // As placement/raycast.ts; drei brings its own copy of the library's BufferGeometry typing.
    ;(mesh.geometry as { boundsTree?: unknown }).boundsTree ??= new MeshBVH(mesh.geometry, { indirect: true })
    picker = new Mesh(mesh.geometry, pickMaterial)
    picker.raycast = acceleratedRaycast
    picker.matrixAutoUpdate = false
    pickers.set(mesh.geometry, picker)
  }
  picker.matrixWorld.copy(world)
  const bvhRay = raycaster as Raycaster & { firstHitOnly?: boolean }
  const first = bvhRay.firstHitOnly
  bvhRay.firstHitOnly = true
  try {
    const hit = raycaster.intersectObject(picker, false)[0]
    return hit && typeof hit.faceIndex === 'number' ? { triangle: hit.faceIndex, distance: hit.distance } : null
  } finally {
    bvhRay.firstHitOnly = first
  }
}
