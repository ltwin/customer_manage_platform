import { LoadingManager, Mesh, type Group, type Object3D } from 'three'
import { GLTFLoader, type GLTF } from 'three/examples/jsm/loaders/GLTFLoader.js'

// The only way model GLBs reach GLTFLoader, for freshly baked ones and for
// those loaded from the server alike. Callers inspect the bytes first
// (inspect.ts), which already rejects every uri; the URL modifier is the second
// line: a uri left in the file would otherwise make the editor page, with the
// account's cookies, request it on the module's behalf.

const allowed = /^(blob|data):/i

/**
 * Which GLB primitive a loaded mesh draws (data: 模型内容「面级材质分配」):
 * the path of its node from the scene root, each step the node's name as the
 * GLB's JSON has it (three drops the `:` of `part:<id>`) and its place among
 * same-named siblings, and the primitive's index in the node's mesh.
 */
export type MeshIdentity = { path: [string, number][]; primitive: number }

export type ParsedModel = { scene: Group; identities: ReadonlyMap<Mesh, MeshIdentity> }

type NodeJSON = { name?: string; children?: number[] }
type Association = { nodes?: number; meshes?: number; primitives?: number }

/** Every node's path, by walking the scene's roots and children as the file lists them. */
function nodePaths(json: { scene?: number; scenes?: { nodes?: number[] }[]; nodes?: NodeJSON[] }): Map<number, [string, number][]> {
  const nodes = json.nodes ?? []
  const paths = new Map<number, [string, number][]>()
  const walk = (siblings: readonly number[], parent: [string, number][]) => {
    const seen = new Map<string, number>()
    for (const index of siblings) {
      const name = nodes[index]?.name ?? ''
      const n = seen.get(name) ?? 0
      seen.set(name, n + 1)
      const path: [string, number][] = [...parent, [name, n]]
      paths.set(index, path)
      walk(nodes[index]?.children ?? [], path)
    }
  }
  walk(json.scenes?.[json.scene ?? 0]?.nodes ?? [], [])
  return paths
}

function identitiesOf(gltf: GLTF): Map<Mesh, MeshIdentity> {
  const paths = nodePaths(gltf.parser.json as Parameters<typeof nodePaths>[0])
  const associations = gltf.parser.associations as Map<Object3D, Association>
  const out = new Map<Mesh, MeshIdentity>()
  gltf.scene.traverse((o) => {
    if (!(o instanceof Mesh)) return
    const own = associations.get(o)
    // A one-primitive mesh is its node's object; a mesh of several primitives
    // is a group holding one Mesh per primitive, and the group is the node's.
    const node = own?.nodes ?? (o.parent ? associations.get(o.parent)?.nodes : undefined)
    const path = node === undefined ? undefined : paths.get(node)
    if (!path) throw new Error('GLB 网格无法对应到节点')
    out.set(o, { path, primitive: own?.primitives ?? 0 })
  })
  return out
}

export async function parseModelGLB(glb: ArrayBuffer): Promise<ParsedModel> {
  let blocked: string | null = null
  const manager = new LoadingManager()
  manager.setURLModifier((url) => {
    if (allowed.test(url)) return url
    blocked ??= url
    return 'data:,'
  })
  const gltf = await new GLTFLoader(manager).parseAsync(glb, '')
  if (blocked !== null) throw new Error(`GLB 引用了外部地址 ${blocked}`)
  return { scene: gltf.scene, identities: identitiesOf(gltf) }
}

export async function parseGLB(glb: ArrayBuffer): Promise<Group> {
  return (await parseModelGLB(glb)).scene
}
