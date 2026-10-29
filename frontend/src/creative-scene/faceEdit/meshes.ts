import { DoubleSide, Matrix4, Mesh, MeshBasicMaterial, Ray, Raycaster, Vector3, type BufferGeometry, type Camera, type Object3D, type OrthographicCamera } from 'three'
import { acceleratedRaycast, MeshBVH } from 'three-mesh-bvh'
import { faceTopology, nextCorner, type FaceTopology } from '../models/faceTopology.ts'
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
  /** Patches, pieces and neighbours, built once when first asked: on entering face edit, for the patch lines. */
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
  const { edgeStart, edgeTriangles, edgeCorner, patchOf } = shape
  const edgeCount = edgeCorner.length
  // Drawn: an edge with one triangle, or with triangles of more than one patch.
  const drawn = new Uint8Array(edgeCount)
  let count = 0
  for (let e = 0; e < edgeCount; e++) {
    const from = edgeStart[e]
    const to = edgeStart[e + 1]
    if (to === from) continue
    let mixed = to - from === 1
    for (let i = from + 1; i < to && !mixed; i++) mixed = patchOf[edgeTriangles[i]] !== patchOf[edgeTriangles[from]]
    if (mixed) {
      drawn[e] = 1
      count++
    }
  }
  const out = new Float32Array(6 * count)
  let n = 0
  for (let e = 0; e < edgeCount; e++) {
    if (!drawn[e]) continue
    for (const c of [edgeCorner[e], nextCorner(edgeCorner[e])]) {
      const v = vertexOf(c)
      out[n++] = position.getX(v)
      out[n++] = position.getY(v)
      out[n++] = position.getZ(v)
    }
  }
  return out
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

/** The mesh's BVH, built the first time it is needed; indirect, so the index and triangle numbers stay. */
function bvhOf(mesh: EditMesh): MeshBVH {
  // As placement/raycast.ts; drei brings its own copy of the library's BufferGeometry typing.
  const g = mesh.geometry as { boundsTree?: MeshBVH }
  return (g.boundsTree ??= new MeshBVH(mesh.geometry, { indirect: true }))
}
const pickers = new WeakMap<BufferGeometry, Mesh>()

/**
 * The triangle a ray meets first on a mesh standing where `world` puts it:
 * its number in the geometry's index order (the BVH is indirect and leaves
 * the index alone), and how far along the ray.
 */
export function pickTriangle(raycaster: Raycaster, mesh: EditMesh, world: Mesh['matrixWorld']): { triangle: number; distance: number } | null {
  let picker = pickers.get(mesh.geometry)
  if (!picker) {
    bvhOf(mesh)
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
const eyeLocal = /*@__PURE__*/ new Vector3()
const lookLocal = /*@__PURE__*/ new Vector3()
const toLocal = /*@__PURE__*/ new Matrix4()
const boxRay = /*@__PURE__*/ new Ray()

/**
 * The triangles a box drag catches, by mesh: the centre of each inside the
 * box and seen from the camera, nothing of the object's own nearer on the
 * way (design「框选只选可见面」; other objects never hide faces, as a click
 * passes them too). By triangle, not by patch: a patch's centre can fall in
 * a hole of it, a window in a wall. Rays are cast in the meshes' own space
 * straight into their BVHs, only as far as the centre.
 */
export function boxTriangles(meshes: readonly EditMesh[], world: Matrix4, camera: Camera, box: ScreenBox): Record<string, number[]> {
  const [left, right] = [Math.min(box.x0, box.x1), Math.max(box.x0, box.x1)]
  const [bottom, top] = [Math.min(box.y0, box.y1), Math.max(box.y0, box.y1)]
  camera.updateMatrixWorld()
  const toClip = new Matrix4().multiplyMatrices(camera.projectionMatrix, camera.matrixWorldInverse).multiply(world)
  toLocal.copy(world).invert()
  const orthographic = (camera as OrthographicCamera).isOrthographicCamera === true
  // The eye, or for an orthographic camera the way it looks, in the meshes' space.
  if (orthographic) lookLocal.setFromMatrixColumn(camera.matrixWorld, 2).negate().transformDirection(toLocal)
  else eyeLocal.setFromMatrixPosition(camera.matrixWorld).applyMatrix4(toLocal)
  const bvhs = meshes.map(bvhOf)
  // How far behind each point an orthographic ray starts: past the meshes' bounds.
  const extent = (m: EditMesh) => {
    if (!m.geometry.boundingSphere) m.geometry.computeBoundingSphere()
    const sphere = m.geometry.boundingSphere!
    return sphere.radius + sphere.center.length()
  }
  const reach = orthographic ? 2 * Math.max(...meshes.map(extent)) + 1 : 0
  const out: Record<string, number[]> = {}
  for (const m of meshes) {
    const position = m.geometry.attributes.position
    const index = m.geometry.index
    const vertexOf = (c: number) => (index ? index.getX(c) : c)
    const shape = m.shape()
    const seen = new Uint8Array(shape.patches.length)
    const caught: number[] = []
    for (let t = 0; t < shape.triangleCount; t++) {
      // A patch is caught once one of its triangles is: the rest need no ray.
      if (seen[shape.patchOf[t]]) continue
      centroid.set(0, 0, 0)
      for (let k = 0; k < 3; k++) {
        const v = vertexOf(3 * t + k)
        centroid.x += position.getX(v) / 3
        centroid.y += position.getY(v) / 3
        centroid.z += position.getZ(v) / 3
      }
      projected.copy(centroid).applyMatrix4(toClip)
      if (projected.z < -1 || projected.z > 1 || projected.x < left || projected.x > right || projected.y < bottom || projected.y > top) continue
      if (orthographic) boxRay.direction.copy(lookLocal)
      else boxRay.direction.subVectors(centroid, eyeLocal)
      const far = orthographic ? reach : boxRay.direction.length()
      boxRay.direction.normalize()
      boxRay.origin.copy(centroid).addScaledVector(boxRay.direction, -far)
      // Some slack for the float maths of the ray and the BVH: 1 mm at 10 m.
      const near = far - (1e-4 * far + 1e-6)
      if (near > 0 && bvhs.some((bvh) => bvh.raycastFirst(boxRay, DoubleSide, 0, near))) continue
      seen[shape.patchOf[t]] = 1
      caught.push(t)
    }
    if (caught.length) out[m.key] = caught
  }
  return out
}
