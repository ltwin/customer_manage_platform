import test from 'node:test'
import { createHash } from 'node:crypto'
import assert from 'node:assert/strict'
import * as THREE from 'three'
import {
  applyBuiltinFaces,
  applyFaces,
  carryFaces,
  carryOver,
  carryTargets,
  codeDiffers,
  effectiveSlots,
  ownedMeshes,
  rangesOf,
  splitBySlot,
  topologyOf,
  trianglesOf,
  withAssigned,
  type Faces,
  type OwnedMesh,
} from '../src/creative-scene/models/faces.ts'
import type { MeshIdentity } from '../src/creative-scene/sandbox/gltf.ts'
import { builtins, builtinSpec, defaultParams, topologyChanged } from '../src/creative-scene/builtins/catalog.ts'
import { primitiveGeometry } from '../src/creative-scene/builtins/geometry.ts'
import { faceTopology } from '../src/creative-scene/models/faceTopology.ts'
import { sha256 } from '../src/creative-scene/models/sha256.ts'

// scene-face-materials 2.1: face assignments (data: 模型内容「面级材质分配」).

const slot = (id: string) => new THREE.MeshStandardMaterial({ name: `slot:${id}` })
const id = (...path: [string, number][]): MeshIdentity => ({ path, primitive: 0 })

/** Two quads side by side in z = 0, four triangles, indexed. */
function strip(): THREE.BufferGeometry {
  const g = new THREE.BufferGeometry()
  g.setAttribute('position', new THREE.Float32BufferAttribute([0, 0, 0, 1, 0, 0, 2, 0, 0, 0, 1, 0, 1, 1, 0, 2, 1, 0], 3))
  g.setIndex([0, 1, 4, 0, 4, 3, 1, 2, 5, 1, 5, 4])
  g.computeVertexNormals()
  return g
}

test('sha256 matches node:crypto, across block boundaries', () => {
  for (const n of [0, 1, 55, 56, 63, 64, 65, 119, 120, 1000, 1 << 20]) {
    const data = new Uint8Array(n).map((_, i) => (i * 31 + n) & 255)
    assert.equal(Buffer.from(sha256(data)).toString('hex'), createHash('sha256').update(data).digest('hex'), `length ${n}`)
  }
})

test('the fingerprint follows how triangles connect, not where they are', async () => {
  const box = new THREE.BoxGeometry(1, 1, 1)
  const wide = new THREE.BoxGeometry(5, 2, 1)
  assert.equal(await topologyOf(box), await topologyOf(wide))
  assert.match(await topologyOf(box), /^t1-[0-9a-f]{16}$/)
  assert.notEqual(await topologyOf(box), await topologyOf(new THREE.BoxGeometry(1, 1, 1, 2)))

  // Without an index the corners are welded where they coincide exactly.
  const flat = box.toNonIndexed()
  const flatWide = wide.toNonIndexed()
  assert.equal(await topologyOf(flat), await topologyOf(flatWide))
  // Same vertex count, triangles reordered: a different fingerprint.
  const reordered = flat.clone()
  const p = reordered.attributes.position.array as Float32Array
  const first = p.slice(0, 9)
  p.copyWithin(0, 9, 18)
  p.set(first, 9)
  assert.equal(reordered.attributes.position.count, flat.attributes.position.count)
  assert.notEqual(await topologyOf(reordered), await topologyOf(flat))
})

test('a transform is baked into each mesh once and the loaded geometry is left alone', async () => {
  // The review's case: split into two slots under a +10 parent, every vertex
  // moves by 10 once (x in 10…12), not once per piece, and the source stays put.
  const g = strip()
  const before = new Float32Array(g.attributes.position.array)
  const mesh = new THREE.Mesh(g, slot('stone'))
  mesh.position.x = 10
  const root = new THREE.Group()
  root.add(mesh)
  const [owned] = await ownedMeshes(root, new Map([[mesh, id(['', 0])]]))
  const pieces = splitBySlot(owned.geometry, { trim: [[2, 4]] }, 'stone')
  assert.equal(pieces.length, 2)
  for (const piece of pieces) {
    const xs = Array.from({ length: piece.geometry.attributes.position.count }, (_, i) => piece.geometry.attributes.position.getX(i))
    assert.ok(Math.min(...xs) >= 10 && Math.max(...xs) <= 12, `${piece.slot}: ${xs.join(',')}`)
  }
  assert.deepEqual(g.attributes.position.array, before, 'GLTFLoader’s geometry is unchanged')

  // A geometry two meshes share is baked separately for each.
  const shared = strip()
  const a = new THREE.Mesh(shared, slot('stone'))
  const b = new THREE.Mesh(shared, slot('stone'))
  b.position.y = 5
  const two = new THREE.Group()
  two.add(a, b)
  const [ma, mb] = await ownedMeshes(two, new Map([[a, id(['', 0])], [b, id(['', 1])]]))
  assert.equal(ma.geometry.attributes.position.getY(3), 1)
  assert.equal(mb.geometry.attributes.position.getY(3), 6)
  assert.equal(ma.topology, mb.topology)
})

test('a mirrored mesh is turned back right side out', async () => {
  const mesh = new THREE.Mesh(strip(), slot('stone'))
  mesh.scale.x = -1
  const root = new THREE.Group()
  root.add(mesh)
  const [owned] = await ownedMeshes(root, new Map([[mesh, id(['', 0])]]))
  const index = owned.geometry.index!
  const n = new THREE.Triangle()
    .setFromAttributeAndIndices(owned.geometry.attributes.position as THREE.BufferAttribute, index.getX(0), index.getX(1), index.getX(2))
    .getNormal(new THREE.Vector3())
  assert.ok(n.z > 0.99, `normal ${n.toArray()}`)
  assert.equal(owned.topology, await topologyOf(strip()), 'fingerprinted before the flip')
})

test('pieces hold only the vertices they use, in arrays of their own', () => {
  const g = strip()
  const [base, trim] = splitBySlot(g, { trim: [[2, 4]] }, 'stone')
  assert.equal(base.slot, 'stone')
  assert.equal(trim.slot, 'trim')
  assert.equal(base.geometry.index!.count, 6)
  assert.equal(base.geometry.attributes.position.count, 4)
  assert.equal(trim.geometry.attributes.position.count, 4)
  assert.notEqual(base.geometry.attributes.position.array, g.attributes.position.array)
  assert.notEqual(trim.geometry.attributes.position.array, base.geometry.attributes.position.array)
  assert.equal(base.geometry.attributes.normal.count, 4)

  const flat = g.toNonIndexed()
  const [fb, ft] = splitBySlot(flat, { trim: [[0, 1]] }, null)
  assert.equal(fb.slot, null)
  assert.equal(fb.geometry.attributes.position.count, 9)
  assert.equal(ft.geometry.attributes.position.count, 3)
  assert.equal(ft.geometry.index, null)

  // Everything assigned away: the base slot draws nothing and is left out.
  const all = splitBySlot(g, { trim: [[0, 4]] }, 'stone')
  assert.deepEqual(
    all.map((p) => p.slot),
    ['trim'],
  )
})

function owned(identity: MeshIdentity, topology: string, geometry = strip()): OwnedMesh {
  return { identity, topology, geometry, material: slot('stone') }
}

test('an assignment applies only where identity and fingerprint both match', async () => {
  const t = await topologyOf(strip())
  const meshes = [owned(id(['a', 0]), t), owned(id(['b', 0]), t), owned(id(['c', 0]), t)]
  const faces: Faces = {
    meshes: [
      { path: [['a', 0]], primitive: 0, topology: t, assign: { trim: [[0, 2]] } },
      { path: [['b', 0]], primitive: 0, topology: 't1-0000000000000000', assign: { trim: [[0, 2]] } },
      { path: [['c', 0]], primitive: 0, topology: t, assign: { trim: [[3, 9]] } },
      { path: [['gone', 0]], primitive: 0, topology: t, assign: { trim: [[0, 1]] } },
    ],
  }
  const { pieces, stale } = applyFaces(meshes, faces)
  assert.deepEqual(
    pieces.map((p) => [JSON.stringify(p.mesh.identity.path), p.slot, p.assigned]),
    [
      ['[["a",0]]', 'stone', false],
      ['[["a",0]]', 'trim', true],
      ['[["b",0]]', 'stone', false],
      ['[["c",0]]', 'stone', false],
    ],
  )
  assert.deepEqual(
    stale.map((s) => s.path),
    [[['b', 0]], [['c', 0]], [['gone', 0]]],
  )
  // No assignment: every mesh whole, nothing stale.
  const plain = applyFaces(meshes, null)
  assert.equal(plain.pieces.length, 3)
  assert.equal(plain.stale.length, 0)
})

test('regenerating keeps what can be told apart and drops the rest', async () => {
  const t = await topologyOf(strip())
  const t2 = await topologyOf(new THREE.BoxGeometry())
  const entry = (path: [string, number][], topology = t) => ({ path, primitive: 0, topology, assign: { trim: [[0, 2]] as [number, number][] } })
  const old: Faces = {
    meshes: [
      entry([['part:cap', 0]]),
      entry([['part:base', 0]]),
      entry([['row', 0], ['', 0]]),
      entry([['gone', 0]]),
    ],
    hints: { trim: 'stone' },
  }
  const next = [
    { identity: id(['part:cap', 0]), topology: t },
    { identity: id(['part:base', 0]), topology: t2 },
    // Two unnamed boxes alike in one row: which is which cannot be proven.
    { identity: id(['row', 0], ['', 0]), topology: t },
    { identity: id(['row', 0], ['', 1]), topology: t },
  ]
  const params = carryFaces(old, next, false)
  assert.deepEqual(
    params.faces?.meshes.map((m) => m.path),
    [[['part:cap', 0]], [['row', 0], ['', 0]]],
  )
  assert.deepEqual(params.faces?.hints, { trim: 'stone' })
  assert.deepEqual(params.dropped, [{ slot: 'trim', triangles: 4 }])
  assert.deepEqual(params.kept, [{ slot: 'trim', triangles: 4 }])

  const code = carryFaces(old, next, true)
  assert.deepEqual(
    code.faces?.meshes.map((m) => m.path),
    [[['part:cap', 0]]],
  )
  assert.deepEqual(code.dropped, [{ slot: 'trim', triangles: 6 }])

  // Nothing survives: no faces at all, not an empty list.
  assert.equal(carryFaces(old, [], false).faces, null)
  assert.deepEqual(carryFaces(null, next, true), { faces: null, kept: [], dropped: [] })
})

test('ranges encode triangle sets and assignments move triangles between slots', () => {
  assert.deepEqual(rangesOf([5, 1, 2, 3, 9, 10]), [
    [1, 4],
    [5, 6],
    [9, 11],
  ])
  assert.deepEqual(trianglesOf([[1, 4], [9, 11]]), [1, 2, 3, 9, 10])

  const assign = { brick: [[0, 4]] as [number, number][], trim: [[4, 6]] as [number, number][] }
  // Two brick triangles and one trim triangle go to marble.
  assert.deepEqual(withAssigned(assign, [2, 3, 4], 'marble'), {
    brick: [[0, 2]],
    trim: [[5, 6]],
    marble: [[2, 5]],
  })
  // Back to the GLB's own slot: out of the assignment, empty slots dropped.
  assert.deepEqual(withAssigned(assign, [4, 5], null), { brick: [[0, 4]] })
})

test('effective slots are the GLB’s then the assignment’s, each once', () => {
  const faces: Faces = {
    meshes: [
      { path: [['a', 0]], primitive: 0, topology: 't1-0000000000000000', assign: { brick: [[0, 1]], stone: [[1, 2]] } },
      { path: [['b', 0]], primitive: 0, topology: 't1-0000000000000000', assign: { face_1: [[0, 3]] } },
    ],
    hints: { brick: 'brick' },
  }
  assert.deepEqual(effectiveSlots([{ id: 'stone', hint: 'stone' }], faces), [
    { id: 'stone', hint: 'stone', source: 'model', triangles: 1 },
    { id: 'brick', hint: 'brick', source: 'faces', triangles: 1 },
    { id: 'face_1', hint: '', source: 'faces', triangles: 3 },
  ])
  assert.deepEqual(effectiveSlots([{ id: 'stone', hint: '' }], null), [{ id: 'stone', hint: '', source: 'model', triangles: 0 }])
})

// scene-face-materials 2.2: a builtin's topology declaration may be
// conservative but must not miss a parameter. Every parameter it leaves out
// keeps the fingerprint at its bounds, default and a few fixed random values.
test('every builtin parameter outside its topology declaration keeps the fingerprint', async () => {
  let seed = 7
  const random = () => (seed = (seed * 1103515245 + 12345) % 2147483648) / 2147483648
  for (const spec of builtins) {
    const declared = new Map(spec.topology.map((r) => (typeof r === 'string' ? [r, false] : [r.param, r.zero_split])))
    const base = await topologyOf(primitiveGeometry(spec.builtin, {})!)
    for (const [name, p] of Object.entries(spec.params)) {
      if (declared.get(name) === false || p.min === undefined || p.max === undefined) continue
      // A zero_split parameter only changes topology crossing 0: try values on the default's side.
      const zeroSplit = declared.get(name) === true
      const low = zeroSplit && p.min === 0 ? Math.min(0.001, p.max) : p.min
      const values = [low, p.default as number, p.max, ...Array.from({ length: 5 }, () => low + random() * (p.max! - low))].map((v) => (p.type === 'integer' ? Math.round(v) : v))
      for (const v of values) {
        const fingerprint = await topologyOf(primitiveGeometry(spec.builtin, { [name]: v })!)
        assert.equal(fingerprint, base, `${spec.builtin}.${name} = ${v} changes the topology: declare it`)
      }
    }
  }
})

test('the editor decides builtin topology changes as the server does', () => {
  // The same table as builtins_topology_internal_test.go.
  const cases: [string, Record<string, number>, boolean][] = [
    ['box@1', { width: 5, height: 2 }, false],
    ['ground@1', { size: 100 }, false],
    ['arch@1', { depth: 1 }, false],
    ['arch@1', { opening_width: 0.4 }, true],
    ['arch@1', { width: 3 }, true],
    ['cylinder@1', { height: 4, radius_bottom: 2 }, false],
    ['cylinder@1', { radius_top: 0.3 }, false],
    ['cylinder@1', { radius_top: 0 }, true],
    ['cylinder@1', { radial_segments: 12 }, true],
    ['sphere@1', { radius: 3 }, false],
    ['sphere@1', { segments: 16 }, true],
    ['stairs@1', { step_height: 0.3, width: 3 }, false],
    ['stairs@1', { step_count: 4 }, true],
  ]
  for (const [id, patch, changed] of cases) {
    const spec = builtinSpec(id)!
    const old = defaultParams(id)
    assert.equal(topologyChanged(spec, old, { ...old, ...patch }), changed, `${id} ${JSON.stringify(patch)}`)
  }
  // Parameters left out read as their defaults.
  assert.equal(topologyChanged(builtinSpec('cylinder@1')!, {}, { radial_segments: 32 }), false)
})

// scene-face-materials 2.3: flat patches grow against their first triangle's
// plane, never neighbour to neighbour, so a curve cannot creep into one patch.
test('flat patches: one per flat face, curves stay apart', () => {
  const box = faceTopology(new THREE.BoxGeometry(1, 1, 1))
  assert.equal(box.patches.length, 6)
  assert.ok(box.patches.every((p) => p.length === 2))
  assert.equal(box.components, 1, 'welded by position, the six faces are one piece')

  assert.equal(faceTopology(primitiveGeometry('ground@1', {})!).patches.length, 1)

  // 512 sides about 0.7° apart: every side its own patch (two triangles), each cap one.
  const cylinder = faceTopology(new THREE.CylinderGeometry(0.5, 0.5, 1, 512))
  assert.equal(cylinder.patches.length, 514)
  assert.equal(cylinder.patches.filter((p) => p.length === 2).length, 512)

  // A strip bending 0.01° per segment over 200 segments: patches never drift past 0.05°.
  const positions: number[] = []
  const index: number[] = []
  const step = THREE.MathUtils.degToRad(0.01)
  for (let i = 0; i <= 200; i++) {
    const a = i * step
    const y = Math.sin(a) * 50
    const z = 50 - Math.cos(a) * 50
    positions.push(0, y, z, 1, y, z)
    if (i < 200) index.push(2 * i, 2 * i + 1, 2 * i + 3, 2 * i, 2 * i + 3, 2 * i + 2)
  }
  const band = new THREE.BufferGeometry()
  band.setAttribute('position', new THREE.Float32BufferAttribute(positions, 3))
  band.setIndex(index)
  const strip = faceTopology(band)
  assert.ok(strip.patches.length > 20, `${strip.patches.length} patches`)
  const normal = (t: number) => new THREE.Vector3().fromArray(strip.normals, t * 3)
  for (const patch of strip.patches) for (const t of patch) assert.ok(normal(t).angleTo(normal(patch[0])) <= THREE.MathUtils.degToRad(0.05) + 1e-9)

  // Stairs are eight overlapping boxes: the bottoms meet edge to edge in one
  // plane and join (8 × 2 triangles); every other face stays its own.
  const stairs = faceTopology(primitiveGeometry('stairs@1', {})!)
  assert.equal(stairs.patches.length, 8 * 6 - 7)
  assert.equal(Math.max(...stairs.patches.map((p) => p.length)), 16)
  // The arch: front and back one patch each, every side segment of the outline its own.
  const arch = faceTopology(primitiveGeometry('arch@1', {})!)
  const sizes = arch.patches.map((p) => p.length).sort((a, b) => b - a)
  assert.equal(sizes[0] + sizes[1], arch.triangleCount - 2 * (arch.patches.length - 2))
  assert.equal(sizes[0], sizes[1])
  assert.ok(sizes.slice(2).every((n) => n === 2))
})

test('neighbours and pieces follow shared edges after welding', () => {
  // Two boxes apart: two pieces; within one, every triangle has three neighbours.
  const a = new THREE.BoxGeometry(1, 1, 1)
  const b = new THREE.BoxGeometry(1, 1, 1).translate(3, 0, 0)
  const both = new THREE.BufferGeometry()
  both.setAttribute('position', new THREE.Float32BufferAttribute([...a.attributes.position.array, ...b.attributes.position.array], 3))
  both.setIndex([...a.index!.array, ...Array.from(b.index!.array, (i) => i + 24)])
  const topo = faceTopology(both)
  assert.equal(topo.components, 2)
  assert.notEqual(topo.componentOf[0], topo.componentOf[12])
  for (let t = 0; t < topo.triangleCount; t++) assert.equal(topo.neighbours(t).length, 3)
  // Corners welded across the split face vertices: a box has 8 corners.
  assert.equal(new Set(Array.from(topo.corners.subarray(0, 36))).size, 8)
})


// scene-face-materials 3.3: the editor's own copy of the server's rule, so
// what it shows before the receipt is what the server stores.
test('a builtin update keeps or clears faces as the server does', async () => {
  const { updateObject } = await import('../src/creative-scene/state/apply.ts')
  const faces: Faces = { meshes: [{ path: [['', 0]], primitive: 0, topology: 't1-9f86d081884c7d65', assign: { brick: [[0, 2]] } }] }
  const arch = {
    id: 'sobj_000000000000000000001',
    kind: 'primitive',
    name: '',
    parent_id: null,
    order: 0,
    transform: { position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] },
    visible: true,
    locked: false,
    primitive: { builtin: 'arch@1', params: defaultParams('arch@1'), faces },
  } as Parameters<typeof updateObject>[0]
  const update = (primitive: NonNullable<Parameters<typeof updateObject>[1]['primitive']>) => updateObject(arch, { object_id: arch.id, primitive }).primitive?.faces
  assert.deepEqual(update({ params: { depth: 1 } }), faces)
  assert.equal(update({ params: { opening_width: 0.4 } }), undefined)
  assert.deepEqual(update({ params: { opening_width: 0.4 }, faces }), faces)
  assert.equal(update({ faces: null }), undefined)
})

// scene-face-materials 4.1: loading applies the assignment before parts merge.
test('a model loads split by its assignment, merged by slot and origin', async () => {
  const { readFileSync } = await import('node:fs')
  const path = await import('node:path')
  const { partsOf } = await import('../src/creative-scene/models/cache.ts')
  const { parseModelGLB } = await import('../src/creative-scene/sandbox/gltf.ts')
  const b = readFileSync(path.resolve(import.meta.dirname, '../../backend/internal/creativeglb/testdata/identity-samples.glb'))
  const load = () => parseModelGLB(b.buffer.slice(b.byteOffset, b.byteOffset + b.byteLength) as ArrayBuffer)

  const plain = await load()
  const before = partsOf(plain.scene)
  const parsed = await load()
  const two = [...parsed.identities].find(([, id]) => JSON.stringify(id.path) === '[["",0],["part:two",0]]' && id.primitive === 0)![0]
  const fingerprint = topologyOf(two.geometry)
  const faces: Faces = {
    meshes: [
      { path: [['', 0], ['part:two', 0]], primitive: 0, topology: fingerprint, assign: { brick: [[0, 2]] } },
      { path: [['', 0], ['part:mirror', 0]], primitive: 0, topology: 't1-0000000000000000', assign: { brick: [[0, 1]] } },
    ],
    hints: { brick: 'brick' },
  }
  const parts = partsOf(parsed.scene, parsed.identities, faces)
  assert.equal(parts.stale, 1, 'the mirror entry no longer fits')
  const brick = parts.meshes.filter((m) => m.slot === 'brick')
  assert.equal(brick.length, 1)
  assert.deepEqual(brick[0].origin, { slot: 'stone', hint: 'stone' })
  assert.equal(brick[0].hint, 'brick')
  assert.equal((brick[0].geometry.index ?? brick[0].geometry.attributes.position).count, 6)
  // The rest of the model draws as before: one more part, the brick one.
  assert.equal(parts.meshes.length, before.meshes.length + 1)
  const triangles = (ms: typeof parts.meshes) => ms.reduce((n, m) => n + (m.geometry.index ?? m.geometry.attributes.position).count / 3, 0)
  assert.equal(triangles(parts.meshes), triangles(before.meshes))
})

test('a moved part draws as its origin until its own slot has a material', async () => {
  const { resolvePart } = await import('../src/creative-scene/materials/resolve.ts')
  const marble = { preset: 'marble@1', params: {} }
  const part = { slot: 'face_1', hint: null, constants: null, origin: { slot: 'stone', hint: 'stone' } }
  const hints = new Map<string, string | undefined>()
  assert.deepEqual(resolvePart(part, { stone: marble }, hints).material, resolvePart({ slot: 'stone', hint: 'stone', constants: null }, { stone: marble }, hints).material)
  assert.equal(resolvePart(part, { stone: marble, face_1: { preset: 'wood@1', params: {} } }, hints).source, 'assigned')
  assert.equal(resolvePart(part, { face_1: { preset: 'wood@1', params: {} } }, hints).slot, 'face_1')
})

test('a builtin splits by its assignment while it fits', () => {
  const box = primitiveGeometry('box@1', {})!
  const faces: Faces = { meshes: [{ path: [['', 0]], primitive: 0, topology: topologyOf(box), assign: { brick: [[0, 2]] } }] }
  const { pieces, stale } = applyBuiltinFaces(box, faces)
  assert.equal(stale, false)
  assert.deepEqual(
    pieces?.map((p) => [p.slot, p.assigned, (p.geometry.index ?? p.geometry.attributes.position).count / 3]),
    [
      ['main', false, 10],
      ['brick', true, 2],
    ],
  )
  assert.deepEqual(applyBuiltinFaces(primitiveGeometry('sphere@1', {})!, faces), { pieces: null, stale: true })
  assert.deepEqual(applyBuiltinFaces(box, null), { pieces: null, stale: false })
})

// scene-face-materials 4.3: regenerating carries the assignment to the new GLB.
test('carryOver reads the new GLB: same mesh kept, changed mesh dropped, twins only while the code is the same', async () => {
  const { readFileSync } = await import('node:fs')
  const path = await import('node:path')
  const { parseModelGLB } = await import('../src/creative-scene/sandbox/gltf.ts')
  const b = readFileSync(path.resolve(import.meta.dirname, '../../backend/internal/creativeglb/testdata/identity-samples.glb'))
  const glb = () => b.buffer.slice(b.byteOffset, b.byteOffset + b.byteLength) as ArrayBuffer
  const { identities } = await parseModelGLB(glb())
  const fingerprint = (p: [string, number][]) => topologyOf([...identities].find(([, i]) => JSON.stringify(i.path) === JSON.stringify(p) && i.primitive === 0)![0].geometry)
  const leaf: [string, number][] = [['', 0], ['row', 0], ['leaf', 0]]
  const twin: [string, number][] = [['', 0], ['twin', 1]]
  const faces: Faces = {
    meshes: [
      // Resized: the same triangles.
      { path: leaf, primitive: 0, topology: fingerprint(leaf), assign: { face_1: [[0, 10]] } },
      // More segments: the old fingerprint no longer matches.
      { path: [['', 0], ['part:two', 0]], primitive: 0, topology: 't1-0000000000000000', assign: { face_1: [[0, 2]] } },
      // A sibling of the same name and triangles: which is which is unknown once the code changes.
      { path: twin, primitive: 0, topology: fingerprint(twin), assign: { face_2: [[0, 1]] } },
    ],
    hints: { face_1: 'brick' },
  }
  const params = await carryOver(glb(), faces, false)
  assert.deepEqual(params.faces?.meshes.map((m) => m.path), [leaf, twin])
  assert.deepEqual(params.faces?.hints, { face_1: 'brick' })
  assert.deepEqual(params.dropped, [{ slot: 'face_1', triangles: 2 }])
  const code = await carryOver(glb(), faces, true)
  assert.deepEqual(code.faces?.meshes.map((m) => m.path), [leaf])
  assert.deepEqual(code.dropped, [{ slot: 'face_1', triangles: 2 }, { slot: 'face_2', triangles: 1 }])

  assert.deepEqual(await carryOver(new ArrayBuffer(8), null, true), { faces: null, kept: [], dropped: [] }, 'nothing to carry, nothing parsed')
  assert.deepEqual(await carryOver(new ArrayBuffer(8), faces, false), { faces: null, kept: [], dropped: [{ slot: 'face_1', triangles: 12 }, { slot: 'face_2', triangles: 1 }] }, 'an unreadable GLB keeps nothing')

  const targets = carryTargets(identities, { meshes: [faces.meshes[0]] })
  assert.deepEqual(targets.filter((t) => t.topology).map((t) => t.identity.path), [leaf], 'only meshes an entry could land on are fingerprinted')

  assert.equal(codeDiffers({ code_sha256: 'a', lib_version: 1 }, { code_sha256: 'a', lib_version: 1 }), false)
  assert.equal(codeDiffers({ code_sha256: 'a', lib_version: 1 }, { code_sha256: 'b', lib_version: 1 }), true)
  assert.equal(codeDiffers({ code_sha256: 'a', lib_version: 1 }, { code_sha256: 'a', lib_version: 2 }), true)
  assert.equal(codeDiffers(null, { code_sha256: 'a', lib_version: 1 }), true)
})

// scene-face-materials 5.1: entering face edit and picking patches.
test('face edit enters only an unlocked, visible model instance or builtin', async () => {
  const { faceEditBlock, faceSource } = await import('../src/creative-scene/faceEdit/state.ts')
  const base = { id: 'sobj_1', name: '物件', parent_id: null, order: 0, transform: { position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] }, visible: true, locked: false, material_slots: {} }
  const box = { ...base, kind: 'primitive', primitive: { builtin: 'box@1', params: { width: 1 } } } as never
  const model = { ...base, kind: 'model_instance', model: { content_revision_id: 'ccrv_a' } } as never
  assert.equal(faceEditBlock(box, true), null)
  assert.equal(faceEditBlock(model, true), null)
  assert.equal(faceEditBlock(undefined, true), '先选中一个模型实例或基础体，再按 Tab 进入面编辑')
  assert.equal(faceEditBlock({ ...base, kind: 'light', light: { type: 'point' } } as never, true), '面编辑只用于模型实例和基础体')
  assert.equal(faceEditBlock({ ...(box as object), locked: true } as never, true), '物件已锁定，解锁后才能面编辑')
  assert.equal(faceEditBlock({ ...(box as object), visible: false } as never, true), '物件已隐藏，显示后才能面编辑')
  assert.equal(faceEditBlock(model, false), '模型不可用，不能面编辑')
  assert.equal(faceSource(model), 'ccrv_a')
  assert.notEqual(faceSource(box), faceSource({ ...(box as object), primitive: { builtin: 'box@1', params: { width: 2 } } } as never), 'new parameters, new triangles')
})

test('a click picks a patch; Shift adds it or takes it out; a click on nothing clears', async () => {
  const { clickPatch, selectedCount } = await import('../src/creative-scene/faceEdit/state.ts')
  let sel = clickPatch({}, 'a', [3, 2], false)
  assert.deepEqual(sel, { a: [2, 3] })
  sel = clickPatch(sel, 'a', [0, 1], true)
  assert.deepEqual(sel, { a: [0, 1, 2, 3] })
  sel = clickPatch(sel, 'b', [5], true)
  assert.equal(selectedCount(sel), 5)
  sel = clickPatch(sel, 'a', [2, 3], true)
  assert.deepEqual(sel, { a: [0, 1], b: [5] }, 'a patch all selected comes out')
  assert.deepEqual(clickPatch(sel, 'b', [5], true), { a: [0, 1] }, 'an emptied mesh is dropped')
  assert.deepEqual(clickPatch(sel, 'a', [2, 3, 4], true).a, [0, 1, 2, 3, 4], 'a patch partly selected is added whole')
  assert.equal(clickPatch(sel, null, [], true), sel)
  assert.deepEqual(clickPatch(sel, null, [], false), {})
  assert.deepEqual(clickPatch(sel, 'b', [7, 8], false), { b: [7, 8] }, 'a plain click replaces')
})

test('on a box a click picks one side, two triangles, numbered as the assignment numbers them', async () => {
  const { builtinEditMesh, patchEdges, pickTriangle } = await import('../src/creative-scene/faceEdit/meshes.ts')
  const geometry = primitiveGeometry('box@1', defaultParams('box@1'))!
  const mesh = builtinEditMesh(geometry)
  assert.equal(mesh.key, '[[["",0]],0]')
  assert.equal(mesh.topology, topologyOf(geometry))
  // The object stands 10 m along x; the ray comes in along -x towards its middle.
  geometry.computeBoundingBox()
  const middle = geometry.boundingBox!.getCenter(new THREE.Vector3())
  const world = new THREE.Matrix4().makeTranslation(10, 0, 0)
  const raycaster = new THREE.Raycaster(new THREE.Vector3(20, middle.y, middle.z), new THREE.Vector3(-1, 0, 0))
  const hit = pickTriangle(raycaster, mesh, world)
  assert.ok(hit, 'the ray meets the box')
  const shape = mesh.shape()
  assert.ok(shape.normals[3 * hit.triangle] > 0.99, 'the triangle faces the ray')
  const patch = shape.patches[shape.patchOf[hit.triangle]]
  assert.equal(patch.length, 2)
  assert.ok(patch.every((t) => shape.normals[3 * t] > 0.99))
  assert.equal(pickTriangle(new THREE.Raycaster(new THREE.Vector3(20, 50, 0), new THREE.Vector3(-1, 0, 0)), mesh, world), null)
  // Lines where sides meet: the box's 12 edges, not the diagonals across its sides.
  assert.equal(patchEdges(geometry, shape).length, 12 * 6)
})

// scene-face-materials 5.2: the other ways to select faces.
test('A, Ctrl+I, Ctrl+numpad + and − on a box, a cylinder and an arch, by whole patches', async () => {
  const { builtinEditMesh } = await import('../src/creative-scene/faceEdit/meshes.ts')
  const { growFaces, invertFaces, selectAllFaces, selectedCount, shrinkFaces } = await import('../src/creative-scene/faceEdit/state.ts')
  const meshOf = (builtin: string) => builtinEditMesh(primitiveGeometry(builtin, defaultParams(builtin))!)
  const patchWhole = (shape: ReturnType<typeof faceTopology>, triangles: readonly number[]) => {
    const had = new Set(triangles)
    return triangles.every((t) => shape.patches[shape.patchOf[t]].every((u) => had.has(u)))
  }

  const box = meshOf('box@1')
  const boxShape = box.shape()
  const top = boxShape.patches.find((p) => boxShape.normals[3 * p[0] + 1] > 0.99)!
  assert.equal(selectedCount(selectAllFaces([box])), 12)
  assert.equal(selectedCount(invertFaces({ [box.key]: top }, [box])), 10)
  assert.deepEqual(invertFaces(selectAllFaces([box]), [box]), {}, 'nothing left')
  const grown = growFaces({ [box.key]: top }, [box])
  assert.equal(selectedCount(grown), 10, 'the top and the four sides around it, not the bottom')
  assert.ok(grown[box.key].every((t) => boxShape.normals[3 * t + 1] > -0.99))
  assert.deepEqual(shrinkFaces({ [box.key]: top }, [box]), {}, 'the top alone is all edge')
  assert.deepEqual(shrinkFaces(selectAllFaces([box]), [box]), selectAllFaces([box]), 'a closed selection has no edge')
  assert.deepEqual(shrinkFaces(grown, [box]), { [box.key]: top }, 'the sides touch the bottom; the top does not')

  const cylinder = meshOf('cylinder@1')
  const cylShape = cylinder.shape()
  assert.equal(selectedCount(selectAllFaces([cylinder])), cylShape.triangleCount)
  const strip = cylShape.patches.find((p) => p.length === 2)!
  const around = growFaces({ [cylinder.key]: strip }, [cylinder])[cylinder.key]
  // 32 segments: the strip, the strips either side, both caps of 32 triangles.
  assert.equal(around.length, 2 + 4 + 32 + 32)
  assert.ok(patchWhole(cylShape, around))
  const side = cylShape.patches.filter((p) => p.length === 2).flat()
  assert.deepEqual(shrinkFaces({ [cylinder.key]: side }, [cylinder]), {}, 'every strip touches a cap')
  assert.equal(selectedCount(invertFaces({ [cylinder.key]: side }, [cylinder])), 64, 'the caps')

  const arch = meshOf('arch@1')
  const archShape = arch.shape()
  const front = archShape.patches.find((p) => archShape.normals[3 * p[0] + 2] > 0.99)!
  const archGrown = growFaces({ [arch.key]: front }, [arch])[arch.key]
  assert.ok(archGrown.length > front.length && patchWhole(archShape, archGrown))
  assert.ok(!archGrown.some((t) => archShape.normals[3 * t + 2] < -0.99), 'the back is not next to the front')
  assert.deepEqual(shrinkFaces({ [arch.key]: archGrown }, [arch]), { [arch.key]: [...front].sort((a, b) => a - b) })
  assert.equal(selectedCount(invertFaces({ [arch.key]: front }, [arch])), archShape.triangleCount - front.length)
})

test('L adds the piece under the pointer; Ctrl+L every piece something is selected on', async () => {
  const { builtinEditMesh } = await import('../src/creative-scene/faceEdit/meshes.ts')
  const { linkedPick, selectLinked, selectedCount } = await import('../src/creative-scene/faceEdit/state.ts')
  const { mergeGeometries } = await import('three/addons/utils/BufferGeometryUtils.js')
  const two = builtinEditMesh(mergeGeometries([new THREE.BoxGeometry(), new THREE.BoxGeometry().translate(3, 0, 0)])!)
  assert.equal(two.shape().components, 2)
  assert.deepEqual(linkedPick({}, two, 0)[two.key], Array.from({ length: 12 }, (_, i) => i))
  assert.equal(selectedCount(linkedPick({ [two.key]: [12] }, two, 0)), 13, 'added to what was selected')
  assert.equal(selectedCount(selectLinked({ [two.key]: [12] }, [two])), 12)
  assert.equal(selectedCount(selectLinked({ [two.key]: [0, 12] }, [two])), 24)
  assert.deepEqual(selectLinked({}, [two]), {})
  // A builtin cylinder is one piece: its caps share the side's edges.
  const cylinder = builtinEditMesh(primitiveGeometry('cylinder@1', defaultParams('cylinder@1'))!)
  assert.equal(selectedCount(linkedPick({}, cylinder, 0)), cylinder.shape().triangleCount)
})

test('a box drag replaces, adds (Shift) or takes out (Ctrl) whole patches', async () => {
  const { builtinEditMesh } = await import('../src/creative-scene/faceEdit/meshes.ts')
  const { boxFaces } = await import('../src/creative-scene/faceEdit/state.ts')
  const box = builtinEditMesh(primitiveGeometry('box@1', defaultParams('box@1'))!)
  const shape = box.shape()
  const [a, b] = shape.patches
  const caughtA = { [box.key]: [a[0]] }
  assert.deepEqual(boxFaces({ [box.key]: b }, caughtA, [box], 'set'), { [box.key]: [...a].sort((x, y) => x - y) })
  assert.deepEqual(boxFaces({ [box.key]: b }, caughtA, [box], 'add')[box.key], [...a, ...b].sort((x, y) => x - y))
  assert.deepEqual(boxFaces({ [box.key]: [...a, ...b] }, caughtA, [box], 'subtract')[box.key], [...b].sort((x, y) => x - y))
  assert.deepEqual(boxFaces({ [box.key]: b }, {}, [box], 'set'), {}, 'an empty box clears')
  assert.deepEqual(boxFaces({ [box.key]: b }, {}, [box], 'add'), { [box.key]: [...b].sort((x, y) => x - y) })
})

test('a box drag catches only the faces the camera sees, perspective or orthographic', async () => {
  const { boxTriangles, builtinEditMesh } = await import('../src/creative-scene/faceEdit/meshes.ts')
  const { boxFaces } = await import('../src/creative-scene/faceEdit/state.ts')
  const whole = { x0: -1, y0: -1, x1: 1, y1: 1 }
  const world = new THREE.Matrix4().makeTranslation(10, 0, 0)
  const centre = new THREE.Vector3(10, 0.5, 0)
  const look = <C extends THREE.Camera>(camera: C, from: THREE.Vector3) => {
    camera.position.copy(from)
    camera.lookAt(centre)
    camera.updateMatrixWorld()
    return camera
  }
  const facing = (mesh: ReturnType<typeof builtinEditMesh>, triangles: readonly number[]) =>
    new Set(triangles.map((t) => [0, 1, 2].map((i) => Math.round(mesh.shape().normals[3 * t + i])).join(',')))

  // A box seen from front, right and above: those three sides, two triangles each.
  const box = builtinEditMesh(primitiveGeometry('box@1', defaultParams('box@1'))!)
  for (const camera of [
    look(new THREE.PerspectiveCamera(50, 1, 0.05, 5000), centre.clone().add(new THREE.Vector3(2, 2, 3))),
    look(new THREE.OrthographicCamera(-2, 2, 2, -2, 0.05, 5000), centre.clone().add(new THREE.Vector3(5, 5, 5))),
  ]) {
    const sel = boxFaces({}, boxTriangles([box], world, camera, whole), [box], 'set')
    assert.equal(sel[box.key].length, 6, (camera as THREE.OrthographicCamera).isOrthographicCamera ? 'orthographic' : 'perspective')
    assert.deepEqual(facing(box, sel[box.key]), new Set(['1,0,0', '0,1,0', '0,0,1']))
  }
  // Half the screen, right of the box's front corner: the right side only.
  const front = look(new THREE.PerspectiveCamera(50, 1, 0.05, 5000), centre.clone().add(new THREE.Vector3(2, 0, 3)))
  const corner = new THREE.Vector3(10.5, 0.5, 0.5).project(front)
  const right = boxFaces({}, boxTriangles([box], world, front, { x0: corner.x + 0.01, y0: -1, x1: 1, y1: 1 }), [box], 'set')
  assert.deepEqual(facing(box, right[box.key]), new Set(['1,0,0']))

  // A cylinder from the front at half height: the strips turned to the camera, not the caps or the back.
  const cylinder = builtinEditMesh(primitiveGeometry('cylinder@1', defaultParams('cylinder@1'))!)
  const level = look(new THREE.PerspectiveCamera(50, 1, 0.05, 5000), centre.clone().add(new THREE.Vector3(0, 0, 5)))
  const seen = boxTriangles([cylinder], world, level, whole)[cylinder.key]
  const cyl = cylinder.shape()
  const toward = (t: number) => cyl.normals[3 * t + 2]
  assert.ok(seen.length >= 12 && seen.length <= 17, `about half the 32 strips, got ${seen.length}`)
  assert.ok(seen.every((t) => toward(t) > 0 && Math.abs(cyl.normals[3 * t + 1]) < 0.5), 'turned to the camera, no cap')
  const nearest = cyl.patches.filter((p) => p.length === 2).reduce((a, b) => (toward(a[0]) > toward(b[0]) ? a : b))
  assert.ok(seen.includes(nearest[0]) || seen.includes(nearest[1]))

  // An arch from the front: the front, and the jambs' inner sides; never the back.
  const arch = builtinEditMesh(primitiveGeometry('arch@1', defaultParams('arch@1'))!)
  const archCam = look(new THREE.PerspectiveCamera(50, 1, 0.05, 5000), new THREE.Vector3(10, 1.5, 6))
  const archSel = boxFaces({}, boxTriangles([arch], world, archCam, whole), [arch], 'set')[arch.key]
  const shape = arch.shape()
  const archFront = shape.patches.find((p) => shape.normals[3 * p[0] + 2] > 0.99)!
  assert.ok(archFront.every((t) => archSel.includes(t)))
  assert.ok(archSel.every((t) => shape.normals[3 * t + 2] > -0.01), 'nothing turned away')
  assert.ok(archSel.some((t) => Math.abs(shape.normals[3 * t]) > 0.99), 'the inner side of a jamb')
})
