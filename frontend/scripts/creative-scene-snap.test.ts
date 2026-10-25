import test from 'node:test'
import assert from 'node:assert/strict'
import { snap, type Hit, type Vec3 } from '../src/creative-scene/placement/snap.ts'

// C6 design「吸附」: each mount on suitable and unsuitable faces, wall facing, and the fallbacks.

const up: Vec3 = [0, 1, 0]
const down: Vec3 = [0, -1, 0]
const box = { min: [-0.5, 0, -0.5], max: [0.5, 2, 0.5] }
const centred = { min: [-0.5, -1, -0.5], max: [0.5, 1, 0.5] }
// Looking down at 45° from (0, 4, 4) towards the origin.
const ray = { origin: [0, 4, 4] as Vec3, direction: [0, -Math.SQRT1_2, -Math.SQRT1_2] as Vec3 }
const fallback: Vec3 = [3, 0, -2]
const near = (a: readonly number[], b: readonly number[]) => a.every((v, i) => Math.abs(v - b[i]!) < 1e-9)
const assertPlaced = (got: { position: number[]; rotation: number[] }, position: number[], rotation = [0, 0, 0, 1]) => {
  assert.ok(near(got.position, position), `position ${got.position} ≠ ${position}`)
  assert.ok(near(got.rotation, rotation), `rotation ${got.rotation} ≠ ${rotation}`)
}

const top: Hit = { point: [0.2, 1, 0.1], normal: up }
const wall: Hit = { point: [0, 1.5, -1], normal: [0, 0, 1] }
const ceiling: Hit = { point: [0, 3, 0], normal: down }

test('a floor model stands upright on the first face that looks up', () => {
  assertPlaced(snap({ hits: [wall, top], ray, mount: 'floor', bounds: box, fallback }), [0.2, 1, 0.1])
  // A slope of 30° still counts as up; a 60° one does not.
  const slope: Hit = { point: [1, 0.5, 0], normal: [Math.sin(Math.PI / 6), Math.cos(Math.PI / 6), 0] }
  assertPlaced(snap({ hits: [slope], ray, mount: 'floor', bounds: box, fallback }), [1, 0.5, 0])
  const steep: Hit = { point: [1, 0.5, 0], normal: [Math.sin(Math.PI / 3), Math.cos(Math.PI / 3), 0] }
  assertPlaced(snap({ hits: [steep], ray, mount: 'floor', bounds: box, fallback }), [0, 0, 0], undefined)
})

test('a wall model backs onto the first vertical face and faces out', () => {
  assertPlaced(snap({ hits: [top, wall], ray, mount: 'wall', bounds: box, fallback }), [0, 1.5, -1])
  // A wall facing +X turns the front (+Z) a quarter turn to +X.
  const east = snap({ hits: [{ point: [-2, 1, 0], normal: [1, 0, 0] }], ray, mount: 'wall', bounds: box, fallback })
  assertPlaced(east, [-2, 1, 0], [0, Math.sin(Math.PI / 4), 0, Math.cos(Math.PI / 4)])
  // Only the horizontal part of a leaning wall counts.
  const leaning = snap({ hits: [{ point: [0, 1, 2], normal: [0, 0.5, -Math.sqrt(0.75)] }], ray, mount: 'wall', bounds: box, fallback })
  assertPlaced(leaning, [0, 1, 2], [0, 1, 0, 0])
})

test('a ceiling model hangs from the first face that looks down', () => {
  assertPlaced(snap({ hits: [top, ceiling], ray, mount: 'ceiling', bounds: box, fallback }), [0, 3, 0])
})

test('a free model rests its bounds\' bottom on a face that looks up', () => {
  assertPlaced(snap({ hits: [top], ray, mount: 'free', bounds: centred, fallback }), [0.2, 2, 0.1])
})

test('without a suitable face it is placed like a free model: on a face that looks up, else on the ground', () => {
  // No ceiling in the scene: a ceiling model stands on the table instead of being buried in it.
  assertPlaced(snap({ hits: [top], ray, mount: 'ceiling', bounds: box, fallback }), [0.2, 1, 0.1])
  assertPlaced(snap({ hits: [ceiling], ray, mount: 'floor', bounds: box, fallback }), [0, 0, 0], undefined)
  assertPlaced(snap({ hits: [], ray, mount: 'free', bounds: centred, fallback }), [0, 1, 0])
  assertPlaced(snap({ hits: [ceiling], ray, mount: 'wall', bounds: box, fallback }), [0, 0, 0])
  // Off-centre bounds: their bottom centre, not the model's origin, goes on the point.
  assertPlaced(snap({ hits: [], ray, mount: 'floor', bounds: { min: [0, 0.2, 0], max: [1, 1, 2] }, fallback }), [-0.5, -0.2, -1])
})

test('dragging towards the sky lands at the view\'s ground point', () => {
  const sky = { origin: [0, 2, 5] as Vec3, direction: [0, 0.6, -0.8] as Vec3 }
  assertPlaced(snap({ hits: [], ray: sky, mount: 'floor', bounds: box, fallback }), [3, 0, -2])
  assertPlaced(snap({ hits: [], ray: null, mount: 'free', bounds: centred, fallback }), [3, 1, -2])
})

// scene-performance P1: a model's merged geometry is picked and snapped onto
// as its parts were, and the snapping BVH leaves its pieces' ranges alone.

import * as THREE from 'three'
import { partsOf, type ModelMeshPart } from '../src/creative-scene/models/cache.ts'
import { accelerateModels, raycastPieces } from '../src/creative-scene/placement/raycast.ts'
import { ASIDE_LAYER } from '../src/creative-scene/render/sceneRenderer.ts'
import { piecesOf } from '../src/creative-scene/render/pieces.ts'
import { shellOf } from '../src/creative-scene/objects/wireframe.ts'

/** Parts of one slot, turned and spread out, a lathe among them, and one of another slot. */
function model(): THREE.Group {
  const root = new THREE.Group()
  const a = new THREE.MeshStandardMaterial({ name: 'slot:a' })
  const geometries = [new THREE.BoxGeometry(1, 2, 1), new THREE.CylinderGeometry(0.3, 0.5, 1.5, 9), new THREE.TorusGeometry(0.6, 0.2, 8, 16), new THREE.LatheGeometry([new THREE.Vector2(0.2, 0), new THREE.Vector2(0.6, 0.5), new THREE.Vector2(0.3, 1.2)], 10)]
  geometries.forEach((g, i) => {
    const m = new THREE.Mesh(g, a)
    m.position.set(i * 1.6 - 2.4, 0.3 * i, (i % 2) - 0.5)
    m.rotation.set(0.2 * i, 0.7 * i, 0)
    root.add(m)
  })
  const b = new THREE.Mesh(new THREE.SphereGeometry(0.5, 10, 8), new THREE.MeshStandardMaterial({ name: 'slot:b' }))
  b.position.set(0, 2, 0)
  root.add(b)
  return root
}

function unmerged(): ModelMeshPart[] {
  const g = globalThis as { __sceneNoMerge?: boolean }
  g.__sceneNoMerge = true
  try {
    return partsOf(model()).meshes
  } finally {
    delete g.__sceneNoMerge
  }
}

const pickNothing = () => {}

/** A model instance as the editor mounts it: a group named by its object, turned and scaled, with a selection shell on each mesh. */
function instance(parts: ModelMeshPart[], id: string, pick?: THREE.Mesh['raycast']): THREE.Group {
  const group = new THREE.Group()
  group.userData.objectID = id
  group.position.set(0.5, 0, -1)
  group.quaternion.setFromEuler(new THREE.Euler(0.1, 0.8, 0))
  group.scale.setScalar(1.5)
  const material = new THREE.MeshStandardMaterial({ side: THREE.DoubleSide })
  for (const p of parts) {
    const mesh = new THREE.Mesh(p.geometry, material)
    if (pick) mesh.raycast = pick
    const shell = new THREE.Mesh(shellOf(p.geometry), new THREE.MeshBasicMaterial({ side: THREE.BackSide }))
    shell.raycast = pickNothing
    shell.layers.set(ASIDE_LAYER)
    mesh.add(shell)
    group.add(mesh)
  }
  group.updateMatrixWorld(true)
  return group
}

/** What a ray hits, by distance: the point and the face's normal. */
function hits(root: THREE.Object3D, rays: THREE.Ray[]): number[][][] {
  const raycaster = new THREE.Raycaster()
  raycaster.layers.enableAll()
  return rays.map((r) => {
    raycaster.ray.copy(r)
    return raycaster
      .intersectObject(root, true)
      .map((h) => [h.distance, ...h.point.toArray(), ...h.face!.normal.toArray()])
      .sort((x, y) => x[0] - y[0] || x[1] - y[1] || x[2] - y[2])
  })
}

const rays = Array.from({ length: 80 }, (_, i) => {
  const from = new THREE.Vector3().setFromSphericalCoords(9, 0.3 + (i % 8) * 0.3, i * 0.9)
  const at = new THREE.Vector3(((i * 7) % 11) / 2.5 - 2, ((i * 3) % 7) / 3 - 0.5, ((i * 5) % 9) / 4 - 1)
  return new THREE.Ray(from, at.sub(from).normalize())
})

test('a merged model is picked and snapped onto as its parts were; the BVH leaves its ranges alone', () => {
  const before = hits(instance(unmerged(), 'm0'), rays)
  assert.ok(before.filter((h) => h.length > 0).length > 40, 'most rays hit the model')

  const parts = partsOf(model()).meshes
  assert.equal(piecesOf(parts[0].geometry)!.length, 4)
  const root = instance(parts, 'm1', raycastPieces)
  assert.deepEqual(hits(root, rays), before, 'picked piece by piece: the same hits')

  const index = Array.from(parts[0].geometry.index!.array)
  accelerateModels(root, () => true)
  assert.deepEqual(Array.from(parts[0].geometry.index!.array), index, "the BVH sorts a buffer of its own, not the pieces' index")
  const after = hits(root, rays)
  assert.equal(after.length, before.length)
  after.forEach((h, i) => {
    assert.equal(h.length, before[i].length, `ray ${i}: as many hits through the BVH`)
    h.forEach((x, j) => x.forEach((v, k) => assert.ok(Math.abs(v - before[i][j][k]) < 1e-9, `ray ${i} hit ${j}: ${x} vs ${before[i][j]}`)))
  })

  const shells = root.children.map((m) => m.children[0] as THREE.Mesh)
  for (const s of shells) {
    assert.equal(s.raycast, pickNothing, "the outline's shell still picks nothing")
    assert.equal(s.geometry.boundsTree, undefined, 'and gets no BVH')
  }
})

test('a mesh given another geometry (glass made opaque, a new revision) is snapped onto through a BVH of it', () => {
  const parts = partsOf(model()).meshes
  const root = instance(parts, 'm2', raycastPieces)
  accelerateModels(root, () => true)
  const mesh = root.children[0] as THREE.Mesh
  mesh.geometry = partsOf(model()).meshes[0].geometry
  accelerateModels(root, () => true)
  assert.ok(mesh.geometry.boundsTree, 'the new geometry has its BVH')
})
