import { DoubleSide, Matrix4, Mesh, MeshBasicMaterial, Raycaster, Vector2, Vector3, type BufferGeometry, type Camera, type Object3D } from 'three'
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

/** A box on screen, in normalised device coordinates (−1…1, y up). */
export type ScreenBox = { x0: number; y0: number; x1: number; y1: number }

const centroid = /*@__PURE__*/ new Vector3()
const projected = /*@__PURE__*/ new Vector3()
const at = /*@__PURE__*/ new Vector2()
const boxRay = /*@__PURE__*/ new Raycaster()

/**
 * The triangles a box drag catches, by mesh: the centre of each inside the
 * box and seen from the camera, nothing of the object's own nearer on the
 * way (design「框选只选可见面」; other objects never hide faces, as a click
 * passes them too). By triangle, not by patch: a patch's centre can fall in
 * a hole of it, a window in a wall.
 */
export function boxTriangles(meshes: readonly EditMesh[], world: Matrix4, camera: Camera, box: ScreenBox): Record<string, number[]> {
  const [left, right] = [Math.min(box.x0, box.x1), Math.max(box.x0, box.x1)]
  const [bottom, top] = [Math.min(box.y0, box.y1), Math.max(box.y0, box.y1)]
  camera.updateMatrixWorld()
  const out: Record<string, number[]> = {}
  for (const m of meshes) {
    const position = m.geometry.attributes.position
    const index = m.geometry.index
    const vertexOf = (c: number) => (index ? index.getX(c) : c)
    const shape = m.shape()
    const seen = new Set<number>()
    const caught: number[] = []
    for (let t = 0; t < shape.triangleCount; t++) {
      // A patch is caught once one of its triangles is: the rest need no ray.
      if (seen.has(shape.patchOf[t])) continue
      centroid.set(0, 0, 0)
      for (let k = 0; k < 3; k++) {
        const v = vertexOf(3 * t + k)
        centroid.x += position.getX(v) / 3
        centroid.y += position.getY(v) / 3
        centroid.z += position.getZ(v) / 3
      }
      centroid.applyMatrix4(world)
      projected.copy(centroid).project(camera)
      if (projected.z < -1 || projected.z > 1 || projected.x < left || projected.x > right || projected.y < bottom || projected.y > top) continue
      boxRay.setFromCamera(at.set(projected.x, projected.y), camera)
      const far = boxRay.ray.origin.distanceTo(centroid)
      // Some slack for the float maths of the ray and the BVH: 1 mm at 10 m.
      const slack = 1e-4 * far + 1e-6
      if (meshes.some((other) => (pickTriangle(boxRay, other, world)?.distance ?? Infinity) < far - slack)) continue
      seen.add(shape.patchOf[t])
      caught.push(t)
    }
    if (caught.length) out[m.key] = caught
  }
  return out
}
