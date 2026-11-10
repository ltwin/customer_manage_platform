// Spike ② (scene-skinned-humanoid): what a dragged character's trace geometry
// costs per frame. For each character: CPU skinning of every skinned
// primitive to world positions, a refit of a BVH built at the T pose, and a
// full rebuild, at a large pose; then the same rays through the refitted and
// the rebuilt tree, to see how much the refit loosens it.
import { readFileSync } from 'node:fs'
import * as THREE from './three.ts'
import { GLTFLoader } from '/Users/samson/workspace/my_project/customer_manage_platform/frontend/node_modules/three/examples/jsm/loaders/GLTFLoader.js'
import { MeshBVH, SAH, CENTER } from '/Users/samson/workspace/my_project/customer_manage_platform/frontend/node_modules/three-mesh-bvh/src/index.js'
import { apply, constraintsOf, mixamoMapping, prepare, runConstraints, turn, type Mapping } from './retarget.ts'

const dir = process.argv[2]!

/** The file without images and textures, which Node cannot decode: geometry and skins are all this needs. */
function untextured(file: string): ArrayBuffer {
  const b = readFileSync(`${dir}/${file}`)
  const jsonLen = b.readUInt32LE(12)
  const json = JSON.parse(b.subarray(20, 20 + jsonLen).toString('utf8'))
  delete json.images
  delete json.textures
  delete json.samplers
  for (const m of json.materials ?? []) {
    delete m.extensions
    for (const k of Object.keys(m)) if (k.endsWith('Texture')) delete m[k]
    if (m.pbrMetallicRoughness) for (const k of Object.keys(m.pbrMetallicRoughness)) if (k.endsWith('Texture')) delete m.pbrMetallicRoughness[k]
  }
  json.extensionsUsed = (json.extensionsUsed ?? []).filter((e: string) => !e.startsWith('VRMC_materials') && e !== 'KHR_texture_transform')
  const text = Buffer.from(JSON.stringify(json))
  const pad = Buffer.alloc((4 - (text.length % 4)) % 4, 0x20)
  const rest = b.subarray(20 + jsonLen)
  const head = Buffer.alloc(20)
  head.writeUInt32LE(0x46546c67, 0)
  head.writeUInt32LE(2, 4)
  head.writeUInt32LE(20 + text.length + pad.length + rest.length, 8)
  head.writeUInt32LE(text.length + pad.length, 12)
  head.writeUInt32LE(0x4e4f534a, 16)
  const out = Buffer.concat([head, text, pad, rest])
  return out.buffer.slice(out.byteOffset, out.byteOffset + out.byteLength)
}

const vrm0Thumb: Record<string, string> = { leftThumbProximal: 'leftThumbMetacarpal', leftThumbIntermediate: 'leftThumbProximal', rightThumbProximal: 'rightThumbMetacarpal', rightThumbIntermediate: 'rightThumbProximal' }

async function load(file: string) {
  const gltf = await new GLTFLoader().parseAsync(untextured(file), '')
  const json = gltf.parser.json
  const byIndex = new Map<number, THREE.Object3D>()
  const joints = new Set<number>((json.skins ?? []).flatMap((s: { joints: number[] }) => s.joints))
  for (const [o, a] of gltf.parser.associations) {
    const i = (a as { nodes?: number }).nodes
    if (i !== undefined && o instanceof THREE.Object3D) {
      byIndex.set(i, o)
      o.userData.gltfName = json.nodes[i].name ?? ''
      o.userData.joint = joints.has(i)
    }
  }
  let mapping: Mapping
  if (json.extensions?.VRMC_vrm) mapping = Object.fromEntries(Object.entries(json.extensions.VRMC_vrm.humanoid.humanBones as Record<string, { node: number }>).map(([b, { node }]) => [b, byIndex.get(node)!]))
  else if (json.extensions?.VRM) mapping = Object.fromEntries((json.extensions.VRM.humanoid.humanBones as { bone: string; node: number }[]).map(({ bone, node }) => [vrm0Thumb[bone] ?? bone, byIndex.get(node)!]))
  else mapping = mixamoMapping(gltf.scene)
  return { root: gltf.scene, mapping, constraints: constraintsOf(json, (i) => byIndex.get(i)!) }
}

type Skinned = { mesh: THREE.SkinnedMesh; out: Float32Array }

/** Every skinned primitive of the character, each with an array for its world positions. */
function skinnedOf(root: THREE.Object3D): Skinned[] {
  const list: Skinned[] = []
  root.traverse((o) => {
    const m = o as THREE.SkinnedMesh
    if (m.isSkinnedMesh) list.push({ mesh: m, out: new Float32Array(m.geometry.attributes.position!.count * 3) })
  })
  return list
}

const tmp = new THREE.Vector3()
const acc = new THREE.Vector3()
const bindVertex = new THREE.Vector3()

/** Linear blend skinning on the CPU, as the shader does it, to world space. */
function skin(s: Skinned) {
  const { mesh, out } = s
  const g = mesh.geometry
  const pos = g.attributes.position!
  const idx = g.attributes.skinIndex!
  const wts = g.attributes.skinWeight!
  mesh.skeleton.update()
  const bones = mesh.skeleton.boneMatrices!
  const bind = mesh.bindMatrix.elements
  const world = new THREE.Matrix4().multiplyMatrices(mesh.matrixWorld, mesh.bindMatrixInverse).elements
  for (let v = 0; v < pos.count; v++) {
    bindVertex.fromBufferAttribute(pos, v)
    // bind · v
    const x = bindVertex.x, y = bindVertex.y, z = bindVertex.z
    const bx = bind[0]! * x + bind[4]! * y + bind[8]! * z + bind[12]!
    const by = bind[1]! * x + bind[5]! * y + bind[9]! * z + bind[13]!
    const bz = bind[2]! * x + bind[6]! * y + bind[10]! * z + bind[14]!
    acc.set(0, 0, 0)
    for (let k = 0; k < 4; k++) {
      const w = wts.getComponent(v, k)
      if (w === 0) continue
      const o = idx.getComponent(v, k) * 16
      acc.x += w * (bones[o]! * bx + bones[o + 4]! * by + bones[o + 8]! * bz + bones[o + 12]!)
      acc.y += w * (bones[o + 1]! * bx + bones[o + 5]! * by + bones[o + 9]! * bz + bones[o + 13]!)
      acc.z += w * (bones[o + 2]! * bx + bones[o + 6]! * by + bones[o + 10]! * bz + bones[o + 14]!)
    }
    tmp.copy(acc)
    out[3 * v] = world[0]! * tmp.x + world[4]! * tmp.y + world[8]! * tmp.z + world[12]!
    out[3 * v + 1] = world[1]! * tmp.x + world[5]! * tmp.y + world[9]! * tmp.z + world[13]!
    out[3 * v + 2] = world[2]! * tmp.x + world[6]! * tmp.y + world[10]! * tmp.z + world[14]!
  }
}

/** One geometry of all the character's skinned triangles, in world space. */
function combined(list: Skinned[]): THREE.BufferGeometry {
  const total = list.reduce((n, s) => n + s.out.length, 0)
  const positions = new Float32Array(total)
  const indices: number[] = []
  let base = 0
  for (const s of list) {
    positions.set(s.out, base * 3)
    const index = s.mesh.geometry.index
    const count = index ? index.count : s.out.length / 3
    for (let i = 0; i < count; i++) indices.push(base + (index ? index.getX(i) : i))
    base += s.out.length / 3
  }
  const g = new THREE.BufferGeometry()
  g.setAttribute('position', new THREE.BufferAttribute(positions, 3))
  g.setIndex(indices)
  return g
}

const time = (f: () => void, runs = 5) => {
  f()
  const t: number[] = []
  for (let i = 0; i < runs; i++) {
    const s = performance.now()
    f()
    t.push(performance.now() - s)
  }
  return t.sort((a, b) => a - b)[Math.floor(runs / 2)]!
}

/** The nodes a ray visits: a stand-in for the GPU's traversal cost. */
function cost(bvh: MeshBVH, rays: THREE.Ray[]): number {
  let visited = 0
  for (const ray of rays) {
    bvh.shapecast({
      intersectsBounds: (box: THREE.Box3) => {
        visited++
        return ray.intersectsBox(box)
      },
      intersectsTriangle: () => false,
    })
  }
  return visited / rays.length
}

const big = {
  leftUpperArm: turn([0, 0, 1], 70), leftLowerArm: turn([0, 1, 0], -100), rightUpperArm: turn([0, 1, 0], 80), rightLowerArm: turn([0, 0, 1], -90),
  spine: turn([1, 0, 0], 25), chest: turn([0, 1, 0], 30), head: turn([0, 1, 0], -40), leftUpperLeg: turn([1, 0, 0], -80), leftLowerLeg: turn([1, 0, 0], 90),
  rightHand: turn([1, 0, 0], 90),
}

for (const [name, file] of [['Xbot', 'Xbot.glb'], ['Michelle', 'Michelle.glb'], ['VRM 1.0 twist sample', 'VRM1_Constraint_Twist_Sample.vrm'], ['VRM 0.x girl', 'three-vrm-girl.vrm']] as const) {
  const c = await load(file)
  const rig = prepare(c.root, c.mapping)
  const list = skinnedOf(c.root)
  apply(rig, {})
  runConstraints(c.constraints)
  c.root.updateMatrixWorld(true)
  list.forEach(skin)
  const geometry = combined(list)
  const triangles = geometry.index!.count / 3
  const vertices = geometry.attributes.position!.count
  const built = new MeshBVH(geometry, { strategy: SAH, targetLeafSize: 8 })
  // The drag's frame: the pose, constraints, skinning, the positions copied in, a refit.
  apply(rig, big)
  runConstraints(c.constraints)
  c.root.updateMatrixWorld(true)
  const poseMs = time(() => {
    apply(rig, big)
    runConstraints(c.constraints)
    c.root.updateMatrixWorld(true)
  })
  const skinMs = time(() => list.forEach(skin))
  const positions = geometry.attributes.position!.array as Float32Array
  let at = 0
  for (const s of list) {
    positions.set(s.out, at)
    at += s.out.length
  }
  geometry.attributes.position!.needsUpdate = true
  const refitMs = time(() => built.refit())
  const sahMs = time(() => new MeshBVH(geometry, { strategy: SAH, targetLeafSize: 8 }), 3)
  const centerMs = time(() => new MeshBVH(geometry, { strategy: CENTER, targetLeafSize: 8 }), 3)
  const rebuilt = new MeshBVH(geometry, { strategy: SAH, targetLeafSize: 8 })
  // Rays through the posed character's box, from every side.
  const box = new THREE.Box3().setFromBufferAttribute(geometry.attributes.position as THREE.BufferAttribute)
  const centre = box.getCenter(new THREE.Vector3())
  const radius = box.getSize(new THREE.Vector3()).length()
  let seed = 7
  const rnd = () => ((seed = (seed * 16807) % 2147483647) / 2147483647)
  const rays = Array.from({ length: 4000 }, () => {
    const from = new THREE.Vector3(rnd() - 0.5, rnd() - 0.5, rnd() - 0.5).normalize().multiplyScalar(radius).add(centre)
    const to = new THREE.Vector3(rnd() - 0.5, rnd() - 0.5, rnd() - 0.5).multiply(box.getSize(new THREE.Vector3())).add(centre)
    return new THREE.Ray(from, to.sub(from).normalize())
  })
  const refitCost = cost(built, rays)
  const rebuiltCost = cost(rebuilt, rays)
  const nodeBytes = (built as unknown as { _roots: ArrayBuffer[] })._roots.reduce((n, r) => n + r.byteLength, 0)
  const uploadKiB = (vertices * 16 + nodeBytes) / 1024
  console.log(`${name}: ${triangles.toLocaleString()} skinned triangles, ${vertices.toLocaleString()} vertices, ${c.constraints.length} constraints`)
  console.log(`    per drag frame: pose + constraints ${poseMs.toFixed(2)} ms, CPU skinning ${skinMs.toFixed(2)} ms, refit ${refitMs.toFixed(2)} ms; upload ≈ ${uploadKiB.toFixed(0)} KiB`)
  console.log(`    rebuild instead: SAH ${sahMs.toFixed(1)} ms, CENTER ${centerMs.toFixed(1)} ms`)
  console.log(`    nodes a ray visits at the big pose: refitted ${refitCost.toFixed(1)}, rebuilt ${rebuiltCost.toFixed(1)} (${((refitCost / rebuiltCost - 1) * 100).toFixed(0)}% more)`)
}
