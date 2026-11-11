import { Mesh, Object3D, SkinnedMesh } from 'three'
import { clone } from 'three/examples/jsm/utils/SkeletonUtils.js'
import type { MeshIdentity } from '../sandbox/gltf.ts'

// The skeleton of a skinned model (design「带骨骼的模型怎样加载和绘制」): a
// copy of the parsed scene holding the joints, the skinned meshes and the
// meshes riding on joints, its node tree kept as it is (not baked like the
// static parts). Each instance clones it, so it has a skeleton of its own; the
// geometries are shared.
//
// GLTFLoader binds every skinned mesh with the identity bind matrix, attached:
// three then draws Σ wᵢ · (jointᵢ's world matrix · inverse bindᵢ) · v, the
// skinned mesh's own node transform left out, as glTF has it.

/** A skinned primitive: its id is n<node>p<primitive>, the node that holds it in the stored GLB deciding its skin. */
export type SkinnedPart = { mesh: SkinnedMesh; node: number; primitive: number; id: string }

export type SkeletonTemplate = {
  /** The model root, at rest. */
  root: Object3D
  /** GLB node index → the template's object for it, for the nodes it keeps. */
  nodes: ReadonlyMap<number, Object3D>
  skinned: readonly SkinnedPart[]
  /** The parsed scene's meshes the template draws (skinned, or riding a joint); the static parts leave them out. */
  taken: ReadonlySet<Mesh>
}

export type SkeletonInstance = Pick<SkeletonTemplate, 'root' | 'nodes' | 'skinned'>

/** Calls f on each object of a and its counterpart in b, a copy of it. */
function pairs(a: Object3D, b: Object3D, f: (a: Object3D, b: Object3D) => void) {
  f(a, b)
  a.children.forEach((c, i) => pairs(c, b.children[i], f))
}

/** Puts a plain node in o's place, holding its children: a static mesh the joints hang under. */
function unmesh(o: Object3D) {
  const stand = new Object3D()
  stand.name = o.name
  stand.position.copy(o.position)
  stand.quaternion.copy(o.quaternion)
  stand.scale.copy(o.scale)
  for (const c of [...o.children]) stand.add(c)
  const parent = o.parent!
  parent.children[parent.children.indexOf(o)] = stand
  stand.parent = parent
  o.parent = null
  return stand
}

/**
 * The skeleton template of a parsed model, or null when it has no skinned
 * mesh. nodes and identities are parseModelGLB's.
 */
export function templateOf(scene: Object3D, nodes: ReadonlyMap<Object3D, number>, identities: ReadonlyMap<Mesh, MeshIdentity>): SkeletonTemplate | null {
  const skinned: SkinnedMesh[] = []
  scene.traverse((o) => {
    if (o instanceof SkinnedMesh) skinned.push(o)
  })
  if (skinned.length === 0) return null
  const joints = new Set<Object3D>(skinned.flatMap((m) => m.skeleton.bones))
  const taken = new Set<Mesh>(skinned)
  const rest: Mesh[] = []
  scene.traverse((o) => {
    if (!(o instanceof Mesh) || o instanceof SkinnedMesh) return
    let a = o.parent
    while (a && !joints.has(a)) a = a.parent
    if (a) taken.add(o)
    else rest.push(o)
  })

  const root = clone(scene)
  const copies = new Map<Object3D, Object3D>()
  pairs(scene, root, (a, b) => copies.set(a, b))
  const replaced = new Map<Object3D, Object3D>()
  for (const m of rest) {
    const copy = copies.get(m)!
    if (copy.children.length > 0) replaced.set(m, unmesh(copy))
    else copy.removeFromParent()
  }
  const kept = new Map<number, Object3D>()
  for (const [o, index] of nodes) {
    const copy = replaced.get(o) ?? copies.get(o)
    if (copy && (copy.parent || copy === root)) kept.set(index, copy)
  }
  const parts = skinned.map((m): SkinnedPart => {
    const node = nodes.get(m) ?? nodes.get(m.parent!)
    const primitive = identities.get(m)?.primitive ?? 0
    if (node === undefined) throw new Error('蒙皮网格无法对应到节点')
    return { mesh: copies.get(m) as SkinnedMesh, node, primitive, id: `n${node}p${primitive}` }
  })
  root.updateMatrixWorld(true)
  return { root, nodes: kept, skinned: parts, taken }
}

/** A copy of the template with a skeleton of its own, sharing the geometries. */
export function instantiate(t: SkeletonTemplate): SkeletonInstance {
  const root = clone(t.root)
  const copies = new Map<Object3D, Object3D>()
  pairs(t.root, root, (a, b) => copies.set(a, b))
  return {
    root,
    nodes: new Map([...t.nodes].map(([i, o]) => [i, copies.get(o)!])),
    skinned: t.skinned.map((p) => ({ ...p, mesh: copies.get(p.mesh) as SkinnedMesh })),
  }
}
