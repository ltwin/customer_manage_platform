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
import { builtins, builtinSpec, defaultParams, topologyChanged } from '../src/creative-scene/builtins/catalog.ts'
import { primitiveGeometry } from '../src/creative-scene/builtins/geometry.ts'
import { faceTopology } from '../src/creative-scene/models/faceTopology.ts'

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

