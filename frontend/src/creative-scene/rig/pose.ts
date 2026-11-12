import { Box3, Matrix4, Mesh, Quaternion, SkinnedMesh, type BufferGeometry, type Object3D } from 'three'
import { createStore } from 'zustand/vanilla'
import type { components } from '../../api/schema'
import { isHumanBone } from './bones.ts'
import { bindConstraints, jointParents, resetConstraints, runConstraints, type BoundConstraint } from './constraints.ts'
import { apply, prepare, type Mapping, type Pose, type Prepared } from './retarget.ts'
import { poseHash, staticGeometries, staticGeometry, traceKeyOf } from './skin.ts'
import { instantiate, type SkeletonInstance, type SkeletonTemplate } from './template.ts'

// The current pose of each skinned object (design「领域模型与业务规则」): until
// bone channels (scene-pose-system 子项 4) there are two, the rest by default
// and a test pose the e2e sets in the editor's memory by object ID, neither in
// the snapshot nor the undo history. Every drawing path reads it from here,
// so the viewport, the world-space layer, bounds and exports agree.

type Rig = components['schemas']['CreativeModelRig']
type Skeleton = components['schemas']['CreativeModelSkeleton']

/** A pose as the e2e writes it: standard bone → [x, y, z, w]. */
export type PoseInput = Readonly<Record<string, readonly [number, number, number, number]>>

/** Object ID → its test pose; an object not here is at rest. */
export const testPoses = createStore<Readonly<Record<string, Pose>>>(() => ({}))

/** Sets an object's test pose, or puts it back at rest with null. */
export function setPose(objectId: string, input: PoseInput | null) {
  const rest = { ...testPoses.getState() }
  delete rest[objectId]
  if (!input) {
    testPoses.setState(rest, true)
    return
  }
  const pose: Record<string, Quaternion> = {}
  for (const [b, q] of Object.entries(input)) {
    if (!isHumanBone(b)) throw new Error(`${b} 不是标准人形骨骼名`)
    pose[b] = new Quaternion(...q).normalize()
  }
  testPoses.setState({ ...rest, [objectId]: pose }, true)
}

/** The object's current pose; null is its rest. */
export const poseOf = (objectId: string): Pose | null => testPoses.getState()[objectId] ?? null

// The e2e fixture page sets __sceneTest before the editor loads (render/gpu.ts).
if ((globalThis as { __sceneTest?: boolean }).__sceneTest === true) Object.assign(globalThis, { __scenePose: { set: setPose } })

const mappingOf = (rig: Rig | undefined, nodes: ReadonlyMap<number, Object3D>): Mapping =>
  Object.fromEntries(Object.entries(rig?.humanoid ?? {}).flatMap(([b, n]) => (nodes.has(n) ? [[b, nodes.get(n)!]] : [])))

/**
 * What a humanoid model's poses need of its template, worked out once per
 * model: null when it has no humanoid mapping, or its skeleton gives no facing
 * (patterns: 标准人形骨骼: drawn as a skeleton that is not humanoid).
 */
export function prepareTemplate(t: SkeletonTemplate, rig: Rig | undefined): Prepared | null {
  return rig?.humanoid ? prepare(t.root, mappingOf(rig, t.nodes)) : null
}

/** An instance with what it takes to pose it. */
export type Posable = {
  instance: SkeletonInstance
  mapping: Mapping
  prepared: Prepared | null
  constraints: readonly BoundConstraint[]
  /** The mapped bones' rest rotations, to go back to. */
  rest: ReadonlyMap<Object3D, Quaternion>
}

/** A fresh instance (at rest) made posable. */
export function posable(instance: SkeletonInstance, rig: Rig | undefined, skeleton: Skeleton, prepared: Prepared | null): Posable {
  const mapping = mappingOf(rig, instance.nodes)
  const constraints = bindConstraints(rig?.constraints ?? [], jointParents(skeleton), (i) => instance.nodes.get(i)!, instance.root)
  const rest = new Map(Object.values(mapping).map((o) => [o!, o!.quaternion.clone()]))
  return { instance, mapping, prepared, constraints, rest }
}

/**
 * Puts a pose on the instance (patterns: 标准人形骨骼「求值顺序」: the pose,
 * then the node constraints) and updates its world matrices. null, or a model
 * that cannot take a standard pose, is the rest: no constraint runs there, so
 * it is the model as stored.
 */
export function pose(p: Posable, q: Pose | null) {
  resetConstraints(p.constraints)
  for (const [o, r] of p.rest) o.quaternion.copy(r)
  if (q && p.prepared) {
    apply(p.prepared, p.instance.root, p.mapping, q)
    p.instance.root.updateWorldMatrix(true, true)
    runConstraints(p.constraints)
  }
  p.instance.root.updateWorldMatrix(true, true)
}

const prepared = new WeakMap<SkeletonTemplate, Map<string, Prepared | null>>()
const noSkeleton: Skeleton = { joints: [], fingerprint: '' }

/** A rig by its content: its mapping and constraints. */
export const rigKey = (rig: Rig | undefined) => JSON.stringify(rig ?? null)

/**
 * A fresh instance of a model's template, made posable; the humanoid's
 * preparation is worked out once per template and rig. A model's rig is
 * fixed, but a summary may come without it first (a model being placed) and
 * with it after.
 */
export function posableOf(t: SkeletonTemplate, rig: Rig | undefined, skeleton: Skeleton | undefined): Posable {
  const byRig = prepared.get(t) ?? new Map<string, Prepared | null>()
  prepared.set(t, byRig)
  const key = rigKey(rig)
  if (!byRig.has(key)) byRig.set(key, prepareTemplate(t, rig))
  return posable(instantiate(t), rig, skeleton ?? noSkeleton, byRig.get(key)!)
}

/** Posed geometries, held from the cache until released. */
export type Held = { geometries: ReadonlyMap<string, BufferGeometry>; release(): void }

/**
 * Puts a pose on the instance and gives each skinned part's static geometry
 * in it, by part id (rig/skin.ts): rest, the model's rest geometries as
 * loaded; any other pose, the cache's under its trace key, held till release.
 */
export function posedGeometries(p: Posable, modelKey: string, rest: ReadonlyMap<string, BufferGeometry>, q: Pose | null): Held {
  pose(p, q)
  const hash = q && p.prepared ? poseHash(q) : 'rest'
  if (hash === 'rest') return { geometries: rest, release: () => {} }
  const keys = p.instance.skinned.map((part) => traceKeyOf(modelKey, part, hash))
  const geometries = new Map(p.instance.skinned.map((part, i) => [part.id, staticGeometries.acquire(keys[i], () => staticGeometry(part, p.instance.root))]))
  let held = true
  return {
    geometries,
    release: () => {
      if (held) for (const k of keys) staticGeometries.release(k)
      held = false
    },
  }
}

/** What the posed instance covers in its root's space: its skinned parts' geometries and the meshes riding its joints. */
export function skeletonBounds(p: Posable, geometries: ReadonlyMap<string, BufferGeometry>): Box3 {
  const box = new Box3()
  for (const g of geometries.values()) {
    if (!g.boundingBox) g.computeBoundingBox()
    box.union(g.boundingBox!)
  }
  const toRoot = p.instance.root.matrixWorld.clone().invert()
  const m = new Matrix4()
  p.instance.root.traverse((o) => {
    if (!(o instanceof Mesh) || o instanceof SkinnedMesh) return
    if (!o.geometry.boundingBox) o.geometry.computeBoundingBox()
    box.union(o.geometry.boundingBox!.clone().applyMatrix4(m.multiplyMatrices(toRoot, o.matrixWorld)))
  })
  return box
}
