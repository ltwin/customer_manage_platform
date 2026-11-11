import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { BufferAttribute, BufferGeometry, Matrix4, Mesh, Object3D, Quaternion, SkinnedMesh, Vector3 } from 'three'
import { MeshBVH } from 'three-mesh-bvh'
import { parseModelGLB } from '../src/creative-scene/sandbox/gltf.ts'
import { instantiate, templateOf, type SkeletonTemplate } from '../src/creative-scene/rig/template.ts'
import { measure, poseHash, staticGeometry, StaticGeometryCache, traceKeyOf } from '../src/creative-scene/rig/skin.ts'
import { pose, posable, poseOf, prepareTemplate, setPose, testPoses } from '../src/creative-scene/rig/pose.ts'
import { standardOffset, turn } from '../src/creative-scene/rig/retarget.ts'
import type { Skeleton } from '../src/creative-scene/rig/humanoid.ts'

// The skeleton template, CPU skinning and the current pose
// (scene-skinned-humanoid task 3.2): skinned GLBs made here from JSON, one
// small model for the coordinate spaces and part identities, and Xbot's node
// tree (fixtures/rig/skeletons.json) with a few skinned vertices for a pose.

type Typed = Float32Array | Uint8Array | Uint16Array
const componentTypes = new Map<unknown, number>([
  [Float32Array, 5126],
  [Uint8Array, 5121],
  [Uint16Array, 5123],
])
const sizes: Record<string, number> = { SCALAR: 1, VEC3: 3, VEC4: 4, MAT4: 16 }

/** A GLB of json with each accessor's array in its own buffer view, in order. */
function glbOf(json: Record<string, unknown>, accessors: { array: Typed; type: string }[]): ArrayBuffer {
  const chunks: Buffer[] = []
  const bufferViews: { buffer: number; byteOffset: number; byteLength: number }[] = []
  let offset = 0
  const acc = accessors.map((a, i) => {
    const bytes = Buffer.from(a.array.buffer, a.array.byteOffset, a.array.byteLength)
    const pad = Buffer.alloc((4 - (bytes.length % 4)) % 4)
    bufferViews.push({ buffer: 0, byteOffset: offset, byteLength: bytes.length })
    chunks.push(bytes, pad)
    offset += bytes.length + pad.length
    const n = sizes[a.type]
    const out: Record<string, unknown> = { bufferView: i, componentType: componentTypes.get(a.array.constructor), count: a.array.length / n, type: a.type }
    if (a.type === 'VEC3') {
      const lo = [0, 1, 2].map((c) => Math.min(...a.array.filter((_, k) => k % 3 === c)))
      const hi = [0, 1, 2].map((c) => Math.max(...a.array.filter((_, k) => k % 3 === c)))
      Object.assign(out, { min: lo, max: hi })
    }
    return out
  })
  const bin = Buffer.concat(chunks)
  const text = Buffer.from(JSON.stringify({ asset: { version: '2.0' }, scene: 0, accessors: acc, bufferViews, buffers: [{ byteLength: bin.length }], ...json }))
  const jsonChunk = Buffer.concat([text, Buffer.alloc((4 - (text.length % 4)) % 4, 0x20)])
  const chunk = (body: Buffer, type: number) => {
    const h = Buffer.alloc(8)
    h.writeUInt32LE(body.length, 0)
    h.writeUInt32LE(type, 4)
    return Buffer.concat([h, body])
  }
  const body = Buffer.concat([chunk(jsonChunk, 0x4e4f534a), chunk(bin, 0x004e4942)])
  const header = Buffer.alloc(12)
  header.writeUInt32LE(0x46546c67, 0)
  header.writeUInt32LE(2, 4)
  header.writeUInt32LE(12 + body.length, 8)
  const out = Buffer.concat([header, body])
  return out.buffer.slice(out.byteOffset, out.byteOffset + out.byteLength) as ArrayBuffer
}

type NodeJSON = { name?: string; children?: number[]; translation?: number[]; rotation?: number[]; scale?: number[]; matrix?: number[]; mesh?: number; skin?: number }

/** Each node's matrix in the scene, the way glTF composes them. */
function globals(nodes: NodeJSON[], roots: number[]): Matrix4[] {
  const out: Matrix4[] = []
  const local = (n: NodeJSON) =>
    n.matrix
      ? new Matrix4().fromArray(n.matrix)
      : new Matrix4().compose(new Vector3(...(n.translation ?? [0, 0, 0])), new Quaternion(...(n.rotation ?? [0, 0, 0, 1])), new Vector3(...(n.scale ?? [1, 1, 1])))
  const walk = (i: number, parent: Matrix4) => {
    out[i] = parent.clone().multiply(local(nodes[i]))
    for (const c of nodes[i].children ?? []) walk(c, out[i])
  }
  for (const r of roots) walk(r, new Matrix4())
  return out
}

const inverses = (ms: Matrix4[]) => new Float32Array(ms.flatMap((m) => m.clone().invert().toArray()))
const quat = (axis: [number, number, number], deg: number) => new Quaternion().setFromAxisAngle(new Vector3(...axis).normalize(), (deg * Math.PI) / 180).toArray()

// A strip from y = 0 to 2 skinned to joints A (bottom) and B (top), drawn by
// node 1 with skin 0 and by node 5 with skin 1 (bound 0.5 m off), and a small
// triangle on B by node 7. The root node is turned as a Z-up export is; node 1
// has a transform of its own, which skinning leaves out. A plain triangle
// rides on B (node 4); another stands at the root (node 6).
const strip = (() => {
  const nodes: NodeJSON[] = [
    { name: 'Root', rotation: quat([1, 0, 0], -90), children: [1, 2, 5, 6, 7] },
    { name: 'Body', mesh: 0, skin: 0, translation: [5, 0, 0], rotation: quat([0, 1, 0], 30) },
    { name: 'A', children: [3] },
    { name: 'B', translation: [0, 0, 1], children: [4] },
    { name: 'Rider', mesh: 2, translation: [0, 0, 0.2] },
    { name: 'Body2', mesh: 0, skin: 1 },
    { name: 'Static', mesh: 2, translation: [2, 0, 0] },
    { name: 'Tip', mesh: 1, skin: 0 },
  ]
  const g = globals(nodes, [0])
  const shifted = (m: Matrix4) => m.clone().multiply(new Matrix4().makeTranslation(0, 0, 0.5))
  return {
    nodes,
    glb: () =>
      glbOf(
        {
          scenes: [{ nodes: [0] }],
          nodes,
          meshes: [
            { primitives: [{ attributes: { POSITION: 0, NORMAL: 1, JOINTS_0: 2, WEIGHTS_0: 3 }, indices: 4 }] },
            { primitives: [{ attributes: { POSITION: 5, JOINTS_0: 6, WEIGHTS_0: 7 } }] },
            { primitives: [{ attributes: { POSITION: 5 } }] },
          ],
          skins: [
            { joints: [2, 3], inverseBindMatrices: 8 },
            { joints: [2, 3], inverseBindMatrices: 9 },
          ],
        },
        [
          { array: new Float32Array([-0.1, 0, 0, 0.1, 0, 0, -0.1, 0, 1, 0.1, 0, 1, -0.1, 0, 2, 0.1, 0, 2]), type: 'VEC3' },
          { array: new Float32Array([0, -1, 0, 0, -1, 0, 0, -1, 0, 0, -1, 0, 0, -1, 0, 0, -1, 0]), type: 'VEC3' },
          { array: new Uint8Array([0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0]), type: 'VEC4' },
          { array: new Float32Array([1, 0, 0, 0, 1, 0, 0, 0, 0.5, 0.5, 0, 0, 0.5, 0.5, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0]), type: 'VEC4' },
          { array: new Uint16Array([0, 1, 2, 1, 3, 2, 2, 3, 4, 3, 5, 4]), type: 'SCALAR' },
          { array: new Float32Array([0, 0, 1.5, 0.1, 0, 1.5, 0, 0.1, 1.5]), type: 'VEC3' },
          { array: new Uint8Array([1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0]), type: 'VEC4' },
          { array: new Float32Array([1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0]), type: 'VEC4' },
          { array: inverses([g[2], g[3]]), type: 'MAT4' },
          { array: inverses([shifted(g[2]), shifted(g[3])]), type: 'MAT4' },
        ],
      ),
  }
})()

async function template(glb: ArrayBuffer): Promise<SkeletonTemplate> {
  const { scene, identities, nodes } = await parseModelGLB(glb)
  const t = templateOf(scene, nodes, identities)
  assert.ok(t)
  return t
}

const positions = (g: BufferGeometry) => Array.from(g.getAttribute('position').array as Float32Array)

/** The largest distance between three's skinned vertices (drawn) and the static geometry placed by the root. */
function rasterGap(part: SkeletonTemplate['skinned'][number], root: Object3D): number {
  const g = staticGeometry(part, root)
  const p = g.getAttribute('position')
  let worst = 0
  for (let i = 0; i < p.count; i++) {
    const drawn = part.mesh.getVertexPosition(i, new Vector3()).applyMatrix4(part.mesh.matrixWorld)
    const placed = new Vector3().fromBufferAttribute(p, i).applyMatrix4(root.matrixWorld)
    worst = Math.max(worst, drawn.distanceTo(placed))
  }
  return worst
}

test('static geometry is the drawn skin in the model root space, for any instance', async () => {
  const t = await template(strip.glb())
  // At rest the bind poses undo the joints: the strip is where its positions say,
  // the root node's turn and Body's own transform both left out.
  const restBody = staticGeometry(t.skinned.find((p) => p.id === 'n1p0')!, t.root)
  assert.ok(positions(restBody).every((x, i) => Math.abs(x - [-0.1, 0, 0, 0.1, 0, 0, -0.1, 0, 1, 0.1, 0, 1, -0.1, 0, 2, 0.1, 0, 2][i]) < 1e-6))

  const place = (position: Vector3, q: Quaternion, scale: number) => {
    const i = instantiate(t)
    const object = new Object3D()
    object.position.copy(position)
    object.quaternion.copy(q)
    object.scale.setScalar(scale)
    object.add(i.root)
    i.nodes.get(2)!.quaternion.setFromAxisAngle(new Vector3(1, 0, 0), 0.35)
    i.nodes.get(3)!.quaternion.setFromAxisAngle(new Vector3(0.3, 1, 0).normalize(), 0.9)
    object.updateMatrixWorld(true)
    return i
  }
  const one = place(new Vector3(3, 0, -1), new Quaternion().setFromAxisAngle(new Vector3(0, 1, 0), 1.2), 1.5)
  const two = place(new Vector3(-4, 2, 7), new Quaternion().setFromAxisAngle(new Vector3(1, 1, 0).normalize(), -0.7), 0.4)
  for (const i of [one, two]) {
    for (const part of i.skinned) {
      const gap = rasterGap(part, i.root)
      assert.ok(gap < 1e-5, `${part.id}: ${gap} m between drawn and static`)
    }
  }
  // The same pose gives the same static geometry, wherever the instance stands.
  for (const [a, b] of one.skinned.map((p, k) => [p, two.skinned[k]] as const)) {
    const pa = positions(staticGeometry(a, one.root))
    const pb = positions(staticGeometry(b, two.root))
    assert.ok(pa.every((x, k) => Math.abs(x - pb[k]) < 1e-5), a.id)
  }
  // Normals: unit length, and turned with the strip.
  const n = staticGeometry(one.skinned[0], one.root).getAttribute('normal')
  for (let k = 0; k < n.count; k++) assert.ok(Math.abs(new Vector3().fromBufferAttribute(n, k).length() - 1) < 1e-6)
})

test('each skinned primitive is named by the node that draws it', async () => {
  const t = await template(strip.glb())
  assert.deepEqual(t.skinned.map((p) => p.id).sort(), ['n1p0', 'n5p0', 'n7p0'])
  // Mesh 0 drawn by two nodes with two skins: one geometry, two parts, two static geometries.
  const [body, body2] = ['n1p0', 'n5p0'].map((id) => t.skinned.find((p) => p.id === id)!)
  assert.equal(body.mesh.geometry, body2.mesh.geometry)
  assert.notDeepEqual(positions(staticGeometry(body, t.root)), positions(staticGeometry(body2, t.root)))
  assert.notEqual(traceKeyOf('builtin:body_female@1', body, 'rest'), traceKeyOf('builtin:body_female@1', body2, 'rest'))
  assert.equal(traceKeyOf('ccrv_1', body, 'rest'), 'ccrv_1#n1p0#rest')
})

test('the template keeps what rides the joints and leaves the static meshes to the parts', async () => {
  const { scene, identities, nodes } = await parseModelGLB(strip.glb())
  const t = templateOf(scene, nodes, identities)!
  const byName = (root: Object3D, name: string) => root.getObjectByName(name)
  assert.ok(t.taken.has(byName(scene, 'Rider') as Mesh))
  assert.ok(!t.taken.has(byName(scene, 'Static') as Mesh))
  assert.equal(byName(t.root, 'Static'), undefined)
  assert.ok(byName(t.root, 'Rider') instanceof Mesh)
  assert.equal(t.nodes.has(6), false)
  // An instance's rider moves with its joint; the template stays at rest.
  const i = instantiate(t)
  const rider = () => byName(i.root, 'Rider')!.getWorldPosition(new Vector3())
  const before = rider()
  i.nodes.get(3)!.quaternion.setFromAxisAngle(new Vector3(0, 0, 1), 1)
  i.nodes.get(2)!.quaternion.setFromAxisAngle(new Vector3(1, 0, 0), 0.5)
  i.root.updateMatrixWorld(true)
  assert.ok(rider().distanceTo(before) > 0.1)
  assert.ok(t.nodes.get(3)!.quaternion.equals(new Quaternion()))
  assert.ok(i.skinned.every((p) => p.mesh instanceof SkinnedMesh && p.mesh.skeleton.bones.every((b) => b.parent && i.root.getObjectById(b.id))))
  // No skin, no template.
  const plain = await parseModelGLB(
    glbOf({ scenes: [{ nodes: [0] }], nodes: [{ mesh: 0 }], meshes: [{ primitives: [{ attributes: { POSITION: 0 } }] }] }, [{ array: new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0]), type: 'VEC3' }]),
  )
  assert.equal(templateOf(plain.scene, plain.nodes, plain.identities), null)
})

test('the pose hash names what the pose turns', () => {
  const a = { leftHand: turn([1, 0, 0], 30), spine: turn([0, 1, 0], 10) }
  assert.equal(poseHash(null), 'rest')
  assert.equal(poseHash(a), poseHash({ spine: a.spine, leftHand: a.leftHand }))
  assert.equal(poseHash(a), poseHash({ ...a, head: new Quaternion() }), 'identity rotations do not count')
  assert.equal(poseHash(a), poseHash({ ...a, leftHand: a.leftHand.clone().set(-a.leftHand.x, -a.leftHand.y, -a.leftHand.z, -a.leftHand.w) }), 'q and −q')
  const nudged = a.leftHand.clone()
  nudged.x += 4e-7
  assert.equal(poseHash(a), poseHash({ ...a, leftHand: nudged }), 'six decimals')
  assert.notEqual(poseHash(a), poseHash({ leftHand: a.leftHand }))
  assert.notEqual(poseHash({}), 'rest', 'the T pose is not the rest')
  assert.match(poseHash(a), /^[0-9a-f]{16}$/)
})

/** A geometry of n bytes of positions. */
const sized = (bytes: number) => {
  const g = new BufferGeometry()
  g.setAttribute('position', new BufferAttribute(new Float32Array(bytes / 4), 3))
  return g
}

test('the cache keeps what is drawn, drops the least recently used and stops caching past its budget', () => {
  const warnings: string[] = []
  const cache = new StaticGeometryCache(1200, (m) => warnings.push(m))
  const disposed: string[] = []
  const make = (key: string, bytes: number) => () => {
    const g = sized(bytes)
    g.addEventListener('dispose', () => disposed.push(key))
    return g
  }
  const a = cache.acquire('a', make('a', 480))
  assert.equal(a.userData.traceKey, 'a')
  assert.equal(cache.acquire('a', make('a', 480)), a, 'shared')
  cache.acquire('b', make('b', 480))
  cache.release('a')
  cache.release('a')
  cache.release('b')
  assert.equal(cache.bytes, 960)
  // b was used last: c pushes a out.
  cache.acquire('b', make('b', 480))
  cache.release('b')
  cache.acquire('c', make('c', 480))
  assert.deepEqual(disposed, ['a'])
  assert.ok(cache.has('b') && cache.has('c') && !cache.has('a'))
  // Pinned: c and d are drawn, so b goes; e does not fit beside them and is not kept.
  cache.acquire('d', make('d', 480))
  assert.deepEqual(disposed, ['a', 'b'])
  cache.acquire('e', make('e', 480))
  assert.equal(warnings.length, 1)
  assert.ok(cache.has('e'))
  cache.release('e')
  assert.deepEqual(disposed, ['a', 'b', 'e'], 'freed as its last user lets go')
  cache.acquire('f', make('f', 480))
  assert.equal(warnings.length, 1, 'said once')
  cache.release('f')
  cache.release('c')
  cache.release('d')
  // Back under: a new entry is kept again.
  cache.acquire('g', make('g', 480))
  cache.release('g')
  assert.ok(cache.has('g'))
  cache.clear()
  assert.equal(cache.bytes, 0)
})

test('the cache counts the picking BVH', () => {
  const g = new BufferGeometry()
  g.setAttribute('position', new BufferAttribute(new Float32Array(Array.from({ length: 900 }, (_, i) => Math.sin(i))), 3))
  const before = measure(g)
  ;(g as { boundsTree?: MeshBVH }).boundsTree = new MeshBVH(g, { indirect: true })
  assert.ok(measure(g) > before)
})

// --- a pose on a humanoid ---------------------------------------------------------

type Character = { nodes: NodeJSON[]; scenes: { nodes: number[] }[]; scene: number; skins: { joints: number[] }[] }
const xbotTree = (JSON.parse(readFileSync(new URL('fixtures/rig/skeletons.json', import.meta.url), 'utf8')) as Record<string, Character>).xbot
const vectors = JSON.parse(readFileSync(new URL('../src/creative-scene/rig/humanoid_rig.gen.json', import.meta.url), 'utf8')) as {
  skeletons: Record<string, Skeleton & { joints: { node: number; parent: number | null; name: string }[] }>
  cases: { name: string; rig: { humanoid: Record<string, number> } }[]
}
const xbotRig = { humanoid: vectors.cases.find((c) => c.name === 'xbot: 穿刺①/素体检查的映射')!.rig.humanoid }

/** Xbot's skeleton drawing one triangle at the left hand, skinned to it. */
function xbotGLB(): ArrayBuffer {
  const joints = xbotTree.skins[0].joints
  const roots = xbotTree.scenes[xbotTree.scene].nodes
  const nodes: NodeJSON[] = [...structuredClone(xbotTree.nodes), { name: 'Hand mark', mesh: 0, skin: 0 }]
  const g = globals(nodes, roots)
  const hand = xbotRig.humanoid.leftHand
  const at = new Vector3().setFromMatrixPosition(g[hand])
  const j = joints.indexOf(hand)
  return glbOf(
    {
      scenes: [{ nodes: [...roots, nodes.length - 1] }],
      nodes,
      meshes: [{ primitives: [{ attributes: { POSITION: 0, JOINTS_0: 1, WEIGHTS_0: 2 } }] }],
      skins: [{ joints, inverseBindMatrices: 3 }],
    },
    [
      { array: new Float32Array([at.x, at.y, at.z, at.x + 0.01, at.y, at.z, at.x, at.y + 0.01, at.z]), type: 'VEC3' },
      { array: new Uint8Array([j, 0, 0, 0, j, 0, 0, 0, j, 0, 0, 0]), type: 'VEC4' },
      { array: new Float32Array([1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0]), type: 'VEC4' },
      { array: inverses(joints.map((n) => g[n])), type: 'MAT4' },
    ],
  )
}

test('a test pose moves the skin of every instance the same way, and null is the rest', async () => {
  const t = await template(xbotGLB())
  const prepared = prepareTemplate(t, xbotRig)
  assert.ok(prepared)
  const skeleton = vectors.skeletons.xbot
  const rest = positions(staticGeometry(t.skinned[0], t.root))

  setPose('obj_a', { leftUpperArm: turn([0, 1, 0], -90).toArray() as [number, number, number, number] })
  assert.ok(poseOf('obj_a'))
  assert.equal(poseOf('obj_b'), null)
  assert.throws(() => setPose('obj_b', { leftArm: [0, 0, 0, 1] }), /leftArm/)

  const instance = (x: number, turned: number) => {
    const i = instantiate(t)
    const p = posable(i, xbotRig, skeleton, prepared)
    const holder = new Object3D()
    holder.position.set(x, 0, 2 * x)
    holder.rotation.y = turned
    holder.add(i.root)
    return { i, p }
  }
  const [one, two] = [instance(3, 1), instance(-6, 4)]
  for (const { p } of [one, two]) pose(p, poseOf('obj_a'))
  const [a, b] = [one, two].map(({ i }) => positions(staticGeometry(i.skinned[0], i.root)))
  assert.ok(a.every((x, k) => Math.abs(x - b[k]) < 1e-5))
  assert.ok(a.some((x, k) => Math.abs(x - rest[k]) > 0.1), 'the hand moved')
  // The arm points forward: the hand's mark is in front of the shoulder, in standard space.
  const mark = new Object3D()
  one.i.root.add(mark)
  mark.position.fromArray(a.slice(0, 3))
  one.i.root.updateMatrixWorld(true)
  const forward = standardOffset(prepared, one.i.root, mark, one.p.mapping.leftUpperArm!).normalize()
  assert.ok(forward.z > 0.999, `${forward.toArray()}`)
  assert.ok(rasterGap(one.i.skinned[0], one.i.root) < 1e-5)

  // Back to rest: the stored model, exactly.
  setPose('obj_a', null)
  assert.equal(poseOf('obj_a'), null)
  pose(one.p, poseOf('obj_a'))
  assert.ok(positions(staticGeometry(one.i.skinned[0], one.i.root)).every((x, k) => Math.abs(x - rest[k]) < 1e-6))
  // A model with no humanoid mapping takes no standard pose.
  const plain = posable(instantiate(t), undefined, skeleton, prepareTemplate(t, undefined))
  pose(plain, { leftUpperArm: turn([0, 1, 0], -90) })
  assert.ok(positions(staticGeometry(plain.instance.skinned[0], plain.instance.root)).every((x, k) => Math.abs(x - rest[k]) < 1e-6))
  testPoses.setState({}, true)
})
