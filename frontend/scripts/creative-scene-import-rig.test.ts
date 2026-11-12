import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { Box3, Group, Matrix4, Object3D, PropertyBinding, Quaternion, SkinnedMesh } from 'three'
import { clone } from 'three/examples/jsm/utils/SkeletonUtils.js'
import type { DRACOLoader } from 'three/examples/jsm/loaders/DRACOLoader.js'
import { convert, toBottomCenter } from '../src/creative-scene/import/convert.ts'
import { checkMapping, fileConstraints, followClone, jointObjects, submittedRig } from '../src/creative-scene/import/mapping.ts'
import { ImportRejected, parseImport, readContainer } from '../src/creative-scene/import/parse.ts'
import { writeGLB, writtenRig } from '../src/creative-scene/import/write.ts'
import { checkRig, pruneOptional, type Rig } from '../src/creative-scene/rig/humanoid.ts'
import { staticGeometry } from '../src/creative-scene/rig/skin.ts'
import type { SkinnedPart } from '../src/creative-scene/rig/template.ts'
import { parseModelGLB } from '../src/creative-scene/sandbox/gltf.ts'
import { characterGLB, characters, glbOf, globals, inverses, quat, type NodeJSON } from './fixtures/rig/glb.ts'

// Importing a skinned model (scene-skinned-humanoid tasks 4.1–4.2): the skin
// and its influences through parse, convert and write, the rig followed to the
// written node indices, and the mapping the dialog checks and submits. The
// files are made from JSON (fixtures/rig/glb.ts).

const decoders = { draco: (): DRACOLoader => assert.fail('no Draco here') }

// GLTFExporter reads its blobs back with FileReader, which Node has not.
;(globalThis as { FileReader?: unknown }).FileReader ??= class {
  result: ArrayBuffer | string | null = null
  onloadend: (() => void) | null = null
  readAsArrayBuffer(blob: Blob) {
    void blob.arrayBuffer().then((r) => {
      this.result = r
      this.onloadend?.()
    })
  }
  readAsDataURL(blob: Blob) {
    void blob.arrayBuffer().then((r) => {
      this.result = `data:${blob.type || 'application/octet-stream'};base64,${Buffer.from(r).toString('base64')}`
      this.onloadend?.()
    })
  }
}
const parse = (glb: ArrayBuffer) => parseImport(glb, decoders)

async function rejects(glb: ArrayBuffer, reason: ImportRejected['reason'], says: RegExp) {
  await assert.rejects(parse(glb), (e) => e instanceof ImportRejected && e.reason === reason && says.test(e.message))
}

/** A chain of n joints J0 … under a root, J1 up from J0 by 0.1 m and so on. */
function chain(n: number): NodeJSON[] {
  return Array.from({ length: n }, (_, i) => ({ name: `J${i}`, translation: i === 0 ? [0, 0, 0] : [0, 0.1, 0], ...(i < n - 1 ? { children: [i + 1] } : {}) }))
}

/**
 * One triangle skinned to an 8-joint chain by two sets of influences: joints
 * 0–3 at 0.1 each, 4–7 at 0.15 each (design「导入时保留骨骼」AC-5).
 */
function twoSets(primitive: Record<string, unknown> = {}, view: Record<string, unknown> = {}): ArrayBuffer {
  const nodes: NodeJSON[] = [...chain(8), { name: 'Body', mesh: 0, skin: 0 }]
  const g = globals(nodes, [0, 8])
  const json = {
    scenes: [{ nodes: [0, 8] }],
    nodes,
    meshes: [{ primitives: [{ attributes: { POSITION: 0, JOINTS_0: 1, WEIGHTS_0: 2, JOINTS_1: 3, WEIGHTS_1: 4 }, ...primitive }] }],
    skins: [{ joints: [0, 1, 2, 3, 4, 5, 6, 7], inverseBindMatrices: 5 }],
  }
  const out = glbOf(json, [
    { array: new Float32Array([0, 0, 0, 0.1, 0, 0, 0, 0.1, 0]), type: 'VEC3' },
    { array: new Uint8Array([0, 1, 2, 3, 0, 1, 2, 3, 0, 1, 2, 3]), type: 'VEC4' },
    { array: new Float32Array(12).fill(0.1), type: 'VEC4' },
    { array: new Uint8Array([4, 5, 6, 7, 4, 5, 6, 7, 4, 5, 6, 7]), type: 'VEC4' },
    { array: new Float32Array(12).fill(0.15), type: 'VEC4' },
    { array: inverses(g.slice(0, 8)), type: 'MAT4' },
  ])
  if (Object.keys(view).length === 0) return out
  // The second set's joints in a compressed view.
  const { json: j, bin } = readContainer(new Uint8Array(out))
  Object.assign(j.bufferViews![3]!, view)
  return rewrap(j, bin!)
}

function rewrap(json: object, bin: Uint8Array): ArrayBuffer {
  const text = Buffer.from(JSON.stringify(json))
  const jsonChunk = Buffer.concat([text, Buffer.alloc((4 - (text.length % 4)) % 4, 0x20)])
  const chunk = (body: Buffer, type: number) => Buffer.concat([Buffer.from(new Uint32Array([body.length, type]).buffer), body])
  const body = Buffer.concat([chunk(jsonChunk, 0x4e4f534a), chunk(Buffer.from(bin), 0x004e4942)])
  const out = Buffer.concat([Buffer.from(new Uint32Array([0x46546c67, 2, 12 + body.length]).buffer), body])
  return out.buffer.slice(out.byteOffset, out.byteOffset + out.byteLength) as ArrayBuffer
}

const skinned = (root: Object3D) => {
  const out: SkinnedMesh[] = []
  root.traverse((o) => {
    if (o instanceof SkinnedMesh) out.push(o)
  })
  return out
}

test('AC-5: a vertex with eight influences keeps its four largest, through the whole chain', async () => {
  const parsed = await parse(twoSets())
  const c = convert(parsed, { originToBottomCenter: false })
  assert.ok(c.report.some((r) => r.text === '3 个顶点受超过 4 根骨骼影响，只保留了影响最大的 4 根'))
  assert.ok(!c.report.some((r) => /joints_1|weights_1/.test(r.text)), 'the second set is not reported as an unknown attribute')
  const written = await writeGLB(c.scene)
  assert.equal(written.manifest.skeleton?.joints.length, 8)
  const [mesh] = skinned((await parseModelGLB(written.glb)).scene)
  const index = mesh.geometry.getAttribute('skinIndex')
  const weight = mesh.geometry.getAttribute('skinWeight')
  for (let v = 0; v < 3; v++) {
    const bones = [0, 1, 2, 3].map((k) => mesh.skeleton.bones[index.getComponent(v, k)].name).sort()
    assert.deepEqual(bones, ['J4', 'J5', 'J6', 'J7'])
    for (let k = 0; k < 4; k++) assert.ok(Math.abs(weight.getComponent(v, k) - 0.25) < 1e-6)
  }
})

test('several sets of influences that are compressed cannot be read', async () => {
  await rejects(twoSets({ extensions: { KHR_draco_mesh_compression: { bufferView: 0, attributes: {} } } }), 'skin', /取消压缩/)
  await rejects(twoSets({}, { extensions: { EXT_meshopt_compression: { buffer: 0, byteLength: 12, count: 3, byteStride: 4, mode: 'ATTRIBUTES' } } }), 'skin', /取消压缩/)
})

test('too many joints are refused before the loader runs', async () => {
  const skin = (n: number, skins: number[][]) => {
    const nodes: NodeJSON[] = [...chain(n), { name: 'Body', mesh: 0 }]
    return glbOf({ scenes: [{ nodes: [0, n] }], nodes, meshes: [{ primitives: [{ attributes: { POSITION: 0 } }] }], skins: skins.map((joints) => ({ joints })) }, [
      { array: new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0]), type: 'VEC3' },
    ])
  }
  const range = (from: number, to: number) => Array.from({ length: to - from }, (_, i) => from + i)
  await rejects(skin(257, [range(0, 257)]), 'limit', /257 根骨骼，最多 256 根/)
  await rejects(skin(520, [range(0, 256), range(256, 512), range(512, 513)]), 'limit', /共有 513 根骨骼，最多 512 根/)
})

test('a vertex without weights is bound wholly to its first joint', async () => {
  const nodes: NodeJSON[] = [...chain(2), { name: 'Body', mesh: 0, skin: 0 }]
  const g = globals(nodes, [0, 2])
  const glb = glbOf({ scenes: [{ nodes: [0, 2] }], nodes, meshes: [{ primitives: [{ attributes: { POSITION: 0, JOINTS_0: 1, WEIGHTS_0: 2 } }] }], skins: [{ joints: [0, 1], inverseBindMatrices: 3 }] }, [
    { array: new Float32Array([0, 0, 0, 0.1, 0, 0, 0, 0.1, 0]), type: 'VEC3' },
    { array: new Uint8Array([1, 0, 0, 0, 0, 1, 0, 0, 1, 0, 0, 0]), type: 'VEC4' },
    // Normalised bytes: three's loader leaves these as they are.
    { array: new Uint8Array([0, 0, 0, 0, 128, 127, 0, 0, 255, 0, 0, 0]), type: 'VEC4', normalized: true },
    { array: inverses(g.slice(0, 2)), type: 'MAT4' },
  ])
  const c = convert(await parse(glb), { originToBottomCenter: false })
  assert.deepEqual(
    c.report.filter((r) => /骨骼/.test(r.text)).map((r) => r.text),
    ['1 个顶点没有骨骼权重，已绑到它的第一根骨骼'],
  )
  const [mesh] = skinned((await parseModelGLB((await writeGLB(c.scene)).glb)).scene)
  const w = mesh.geometry.getAttribute('skinWeight')
  const j = mesh.geometry.getAttribute('skinIndex')
  assert.deepEqual([w.getX(0), w.getY(0), mesh.skeleton.bones[j.getX(0)].name], [1, 0, 'J1'])
  assert.ok(Math.abs(w.getX(1) + w.getY(1) - 1) < 1e-6)
})

// --- AC-5b: written and read again, the skin is the same -----------------------

/**
 * A root turned as a Z-up export is, holding a 4-joint chain; two skinned
 * meshes with skins of their own over it (the second's bind poses 0.3 m off),
 * the first with two sets of influences.
 */
function composite(): ArrayBuffer {
  const nodes: NodeJSON[] = [
    { name: 'Root', rotation: quat([1, 0, 0], -90), translation: [0.4, 0.2, -0.3], children: [1, 5, 6] },
    { name: 'A', children: [2] },
    { name: 'B', translation: [0, 0, 0.5], rotation: quat([0, 1, 0], 20), children: [3] },
    { name: 'C', translation: [0, 0, 0.5], children: [4] },
    { name: 'D', translation: [0, 0.1, 0.4] },
    { name: 'Body', mesh: 0, skin: 0, translation: [3, 0, 0] },
    { name: 'Coat', mesh: 1, skin: 1 },
  ]
  const g = globals(nodes, [0])
  const off = (i: number) => g[i].clone().multiply(new Matrix4().makeTranslation(0.3, 0, 0))
  const ring = (z: number) => [-0.1, 0, z, 0.1, 0, z, 0, 0.1, z]
  const positions = new Float32Array([...ring(0), ...ring(0.5), ...ring(1), ...ring(1.4)])
  const tris = new Uint16Array([0, 1, 4, 0, 4, 3, 1, 2, 5, 1, 5, 4, 3, 4, 7, 3, 7, 6, 4, 5, 8, 4, 8, 7, 6, 7, 10, 6, 10, 9, 7, 8, 11, 7, 11, 10])
  const normals = new Float32Array(positions.length).map((_, i) => (i % 3 === 1 ? 1 : 0))
  // Each vertex: two joints in the first set, the other two in the second.
  const j0 = new Uint8Array(12 * 4)
  const w0 = new Float32Array(12 * 4)
  const j1 = new Uint8Array(12 * 4)
  const w1 = new Float32Array(12 * 4)
  for (let v = 0; v < 12; v++) {
    const ring = Math.floor(v / 3)
    j0.set([ring, (ring + 1) % 4, 0, 0], v * 4)
    w0.set([0.5, 0.3, 0, 0], v * 4)
    j1.set([(ring + 2) % 4, (ring + 3) % 4, 0, 0], v * 4)
    w1.set([0.15, 0.05, 0, 0], v * 4)
  }
  const single = new Uint8Array(12 * 4)
  const singleW = new Float32Array(12 * 4)
  for (let v = 0; v < 12; v++) {
    single.set([Math.floor(v / 3), 0, 0, 0], v * 4)
    singleW.set([1, 0, 0, 0], v * 4)
  }
  return glbOf(
    {
      scenes: [{ nodes: [0] }],
      nodes,
      meshes: [
        { primitives: [{ attributes: { POSITION: 0, NORMAL: 1, JOINTS_0: 2, WEIGHTS_0: 3, JOINTS_1: 4, WEIGHTS_1: 5 }, indices: 6 }] },
        { primitives: [{ attributes: { POSITION: 0, NORMAL: 1, JOINTS_0: 7, WEIGHTS_0: 8 }, indices: 6 }] },
      ],
      skins: [
        { joints: [1, 2, 3, 4], inverseBindMatrices: 9 },
        { joints: [1, 2, 3, 4], inverseBindMatrices: 10 },
      ],
    },
    [
      { array: positions, type: 'VEC3' },
      { array: normals, type: 'VEC3' },
      { array: j0, type: 'VEC4' },
      { array: w0, type: 'VEC4' },
      { array: j1, type: 'VEC4' },
      { array: w1, type: 'VEC4' },
      { array: tris, type: 'SCALAR' },
      { array: single, type: 'VEC4' },
      { array: singleW, type: 'VEC4' },
      { array: inverses([1, 2, 3, 4].map((i) => g[i])), type: 'MAT4' },
      { array: inverses([1, 2, 3, 4].map(off)), type: 'MAT4' },
    ],
  )
}

/** Positions and normals of each skinned mesh skinned on the CPU in root's space, in traversal order. */
function skins(root: Object3D): { position: number[]; normal: number[] }[] {
  root.updateMatrixWorld(true)
  return skinned(root).map((mesh) => {
    const g = staticGeometry({ mesh } as SkinnedPart, root)
    return { position: Array.from(g.getAttribute('position').array), normal: Array.from(g.getAttribute('normal').array) }
  })
}

const turns: [string, Quaternion][] = [
  ['A', new Quaternion(...quat([1, 0, 0], 25))],
  ['B', new Quaternion(...quat([0.3, 1, 0], -40))],
  ['C', new Quaternion(...quat([0, 0, 1], 60))],
]

/** Turns the named joints from where they are. */
function bend(root: Object3D) {
  for (const [name, q] of turns) root.getObjectByName(name)!.quaternion.multiply(q)
  root.updateMatrixWorld(true)
}

function same(a: number[], b: number[], what: string, offset: number[] = [0, 0, 0]) {
  assert.equal(a.length, b.length, what)
  const worst = Math.max(...a.map((x, i) => Math.abs(x - offset[i % 3] - b[i])))
  assert.ok(worst < 1e-5, `${what}: off by ${worst}`)
}

test('AC-5b: written and read again, the skin is the same at rest and in a pose', async () => {
  for (const origin of [false, true]) {
    const c = convert(await parse(composite()), { originToBottomCenter: origin })
    const joints = ['A', 'B', 'C', 'D'].map((n) => c.scene.getObjectByName(n)!)
    const written = await writeGLB(c.scene, joints)
    const read = (await parseModelGLB(written.glb)).scene
    const nodes = readContainer(new Uint8Array(written.glb)).json.nodes!
    for (const o of joints) assert.equal(nodes[written.nodes.get(o)!]!.name, o.name)
    const box = (root: Object3D) => new Box3().setFromObject((root.updateMatrixWorld(true), root), true)
    for (const posed of [false, true]) {
      if (posed) {
        bend(c.scene)
        bend(read)
      }
      const [before, after] = [skins(c.scene), skins(read)]
      assert.equal(after.length, 2)
      before.forEach((b, k) => {
        same(after[k].position, b.position, `${origin ? 'origin moved, ' : ''}${posed ? 'posed' : 'rest'} mesh ${k} positions`)
        same(after[k].normal, b.normal, `${posed ? 'posed' : 'rest'} mesh ${k} normals`)
      })
      const [x, y] = [box(c.scene), box(read)]
      same([...x.min.toArray(), ...x.max.toArray()], [...y.min.toArray(), ...y.max.toArray()], 'bounds')
    }
  }
})

test('moving the origin to the bottom centre moves the joints, not the vertices', async () => {
  const [kept, moved] = await Promise.all([false, true].map(async (origin) => convert(await parse(composite()), { originToBottomCenter: origin }).scene))
  const before = new Box3().setFromObject(kept, true)
  const offset = [(before.min.x + before.max.x) / 2, before.min.y, (before.min.z + before.max.z) / 2]
  const [a, b] = [skins(kept), skins(moved)]
  a.forEach((s, k) => same(s.position, b[k].position, `mesh ${k}`, offset))
  // The vertices and bind poses as they were.
  skinned(kept).forEach((m, k) => {
    const other = skinned(moved)[k]
    assert.deepEqual(Array.from(m.geometry.getAttribute('position').array), Array.from(other.geometry.getAttribute('position').array))
    assert.ok(m.skeleton.boneInverses.every((x, i) => x.equals(other.skeleton.boneInverses[i])))
  })
  const after = new Box3().setFromObject(moved, true)
  assert.ok(Math.abs(after.min.y) < 1e-6 && Math.abs(after.min.x + after.max.x) < 1e-6 && Math.abs(after.min.z + after.max.z) < 1e-6)
  const written = await writeGLB(moved)
  assert.ok(Math.abs(written.manifest.bounds.min[1]) < 1e-5, `stored bounds ${written.manifest.bounds.min}`)
})

// --- the rig through the export ---------------------------------------------------

/** What the dialog submits: the mapping found, cut to what the server takes, by the written GLB's indices. */
async function imported(name: string) {
  const hand = characters[name].nodes.findIndex((n) => /LeftHand$|L_Hand$/.test(n.name ?? ''))
  const parsed = await parse(characterGLB(characters[name], hand))
  const rig = parsed.rig!
  const humanoid = pruneOptional(rig.detection.humanoid, rig.skeleton).humanoid
  const fileRig = { humanoid, ...(rig.detection.constraints.length ? { constraints: rig.detection.constraints } : {}) }
  assert.equal(checkRig(fileRig, rig.skeleton), null, `${name} by the file's indices`)
  const c = convert(parsed, { originToBottomCenter: true })
  const written = await writeGLB(c.scene, jointObjects(rig))
  return { parsed, fileRig, written, stored: writtenRig(fileRig, rig.objects, written.nodes) }
}

test('the mapping and constraints follow their nodes into the written GLB, and no mark is left', async () => {
  for (const name of ['xbot', 'vrm1_twist']) {
    const { parsed, fileRig, written, stored } = await imported(name)
    assert.equal(checkRig(stored, written.manifest.skeleton), null, name)
    const out = readContainer(new Uint8Array(written.glb)).json
    // The same node, named as three names it (mixamorig:Hips is mixamorigHips).
    const same = (stored: number, file: number, what: string) => assert.equal(out.nodes![stored]!.name, PropertyBinding.sanitizeNodeName(parsed.json.nodes![file]!.name!), `${name} ${what}`)
    for (const [bone, i] of Object.entries(fileRig.humanoid)) same(stored.humanoid![bone]!, i, bone)
    for (const [k, c] of (fileRig.constraints ?? []).entries()) {
      same(stored.constraints![k]!.node, c.node, `constraint ${k}`)
      same(stored.constraints![k]!.source, c.source, `constraint ${k} source`)
    }
    assert.ok(!new TextDecoder().decode(written.glb).includes('ltiNode'), `${name}: a mark left in the file`)
    assert.ok(!out.nodes!.some((n) => n.extras), `${name}: empty extras left`)
  }
  const { fileRig } = await imported('vrm1_twist')
  assert.equal(fileRig.constraints?.length, 14)
})

test('a skinned file is no longer refused, and its skeleton is found by the file indices', async () => {
  const parsed = await parse(characterGLB(characters.xbot, 25))
  assert.equal(parsed.rig?.detection.via, 'mixamo')
  assert.equal(parsed.rig?.skeleton.joints.length, 67)
  assert.equal(parsed.rig?.objects.get(25)?.name, 'mixamorigLeftHand')
  const plain = await parse(glbOf({ scenes: [{ nodes: [0] }], nodes: [{ mesh: 0 }], meshes: [{ primitives: [{ attributes: { POSITION: 0 } }] }] }, [{ array: new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0]), type: 'VEC3' }]))
  assert.equal(plain.rig, null)
})

// --- the mapping in the dialog ----------------------------------------------------

type Vectors = { cases: { name: string; rig: Rig }[] }
const vectors = JSON.parse(readFileSync(new URL('../src/creative-scene/rig/humanoid_rig.gen.json', import.meta.url), 'utf8')) as Vectors
const vectorRig = (name: string) => vectors.cases.find((c) => c.name === name)!.rig

test('RobotExpressive mapped by hand is still refused for its feet, and imports as a non-humanoid skeleton', async () => {
  const parsed = await parse(characterGLB(characters.robot_expressive, 19))
  const rig = parsed.rig!
  const c = convert(parsed, { originToBottomCenter: false })
  // AC-4 at first sight: the found mapping says where its legs hang, before the hands it lacks.
  assert.equal(rig.detection.via, 'generic')
  assert.match(checkMapping(rig, c.scene, rig.detection.humanoid).problem ?? '', /^leftUpperLeg 上面没有映射过的骨骼，应在 hips 之下（另外人形映射缺少必需骨骼 leftHand）$/)
  for (const [name, says] of [
    ['robot_expressive: Hips 作 hips，腿挂在 Body 下', /^leftUpperLeg 上面没有映射过的骨骼/],
    ['robot_expressive: Body 作 hips，脚挂在控制骨下', /^leftFoot 上面没有映射过的骨骼/],
  ] as const) {
    assert.match(checkMapping(rig, c.scene, vectorRig(name).humanoid!).problem ?? '', says, name)
  }
  // 按非人形骨架导入：没有映射，也没有约束可带，骨架照样写进 GLB。
  assert.equal(submittedRig(null, fileConstraints(rig).constraints), undefined)
  const written = await writeGLB(c.scene, jointObjects(rig))
  assert.equal(written.manifest.skeleton?.joints.length, rig.skeleton.joints.length)
})

test('Xbot with one bone mapped wrong holds again once it is set back by hand, and its rig follows a moved origin', async () => {
  const parsed = await parse(characterGLB(characters.xbot, 25))
  const rig = parsed.rig!
  const c = convert(parsed, { originToBottomCenter: false })
  const found = rig.detection.humanoid
  assert.equal(checkMapping(rig, c.scene, found).problem, null)
  const wrong = { ...found, leftUpperLeg: found.rightUpperLeg! }
  assert.match(checkMapping(rig, c.scene, wrong).problem ?? '', /映射到同一节点/)
  const fixed = { ...wrong, leftUpperLeg: found.leftUpperLeg! }
  const check = checkMapping(rig, c.scene, fixed)
  assert.equal(check.problem, null)
  assert.deepEqual(check.nonUniform, [])

  // 勾选「原点移到底面中心」：骨架连网格一起复制，映射跟到复制出的关节上。
  const moved = clone(c.scene) as Group
  toBottomCenter(moved)
  const objects = followClone(rig.objects, c.scene, moved)
  assert.equal(objects.get(25)?.name, rig.objects.get(25)?.name)
  assert.notEqual(objects.get(25), rig.objects.get(25))
  const written = await writeGLB(moved, jointObjects(rig, objects))
  const fileRig = submittedRig(check.humanoid, fileConstraints(rig).constraints)!
  assert.equal(checkRig(writtenRig(fileRig, objects, written.nodes), written.manifest.skeleton), null)
})

test('the twist sample keeps its constraints; constraints the server would refuse are all left out, with the reason', async () => {
  const parsed = await parse(characterGLB(characters.vrm1_twist, 0))
  const rig = parsed.rig!
  assert.equal(fileConstraints(rig).constraints.length, 14)
  assert.deepEqual(fileConstraints(rig).notes, [])
  const [a, b] = rig.detection.constraints
  const cyclic = { ...rig, detection: { ...rig.detection, constraints: [{ ...a!, source: b!.node }, { ...b!, kind: 'rotation' as const, roll_axis: undefined, aim_axis: undefined, source: a!.node }] } }
  const out = fileConstraints(cyclic)
  assert.deepEqual(out.constraints, [])
  assert.match(out.notes[0] ?? '', /已全部去掉/)
})

/** twoSets' file with its JSON changed by edit. */
function editedTwoSets(edit: (json: ReturnType<typeof readContainer>['json']) => void): ArrayBuffer {
  const { json, bin } = readContainer(new Uint8Array(twoSets()))
  edit(json)
  return rewrap(json, bin!)
}

test('AC-5: the second set of weights as an uncompressed sparse accessor is read and trimmed like a plain one', async () => {
  // WEIGHTS_1 with no view of its own (zeros) and all three vertices substituted: indices 0, 1, 2 are JOINTS_0's first bytes.
  const glb = editedTwoSets((json) => {
    const weights = json.accessors![4]!
    delete weights.bufferView
    weights.sparse = { count: 3, indices: { bufferView: 1, componentType: 5121 }, values: { bufferView: 4 } }
  })
  const c = convert(await parse(glb), { originToBottomCenter: false })
  assert.ok(c.report.some((r) => r.text === '3 个顶点受超过 4 根骨骼影响，只保留了影响最大的 4 根'))
  const [mesh] = skinned((await parseModelGLB((await writeGLB(c.scene)).glb)).scene)
  const index = mesh.geometry.getAttribute('skinIndex')
  for (let v = 0; v < 3; v++) assert.deepEqual([0, 1, 2, 3].map((k) => mesh.skeleton.bones[index.getComponent(v, k)].name).sort(), ['J4', 'J5', 'J6', 'J7'])
})

test('a file of two scenes imports its default one, the skeleton only what that scene uses', async () => {
  const glb = editedTwoSets((json) => {
    // A second character in scene 1: two joints and a body on skin 1, none of it written.
    json.nodes!.push({ name: 'K0', children: [10] }, { name: 'K1' }, { name: 'Other', mesh: 0, skin: 1 })
    json.skins!.push({ joints: [9, 10] })
    json.scenes!.push({ nodes: [9, 11] })
    json.scene = 0
  })
  const parsed = await parse(glb)
  assert.deepEqual(parsed.rig!.skeleton.joints.map((j) => j.node), [0, 1, 2, 3, 4, 5, 6, 7])
  const c = convert(parsed, { originToBottomCenter: false })
  const written = await writeGLB(c.scene, jointObjects(parsed.rig!))
  assert.equal(written.manifest.skeleton?.joints.length, 8)
})

test('a node tree with a loop, even among unused nodes, is refused rather than walked forever', async () => {
  const loop = editedTwoSets((json) => void json.nodes!.push({ name: 'A', children: [10] }, { name: 'B', children: [9] }))
  await rejects(loop, 'unreadable', /节点层级不是树/)
  const shared = editedTwoSets((json) => void json.nodes!.push({ name: 'A', children: [1] }))
  await rejects(shared, 'unreadable', /节点层级不是树/)
})
