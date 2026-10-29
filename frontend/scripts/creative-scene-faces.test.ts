import test from 'node:test'
import assert from 'node:assert/strict'
import * as THREE from 'three'
import {
  applyFaces,
  carryFaces,
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
