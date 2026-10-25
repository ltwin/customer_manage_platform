import test from 'node:test'
import assert from 'node:assert/strict'
import { BoxGeometry, DirectionalLight, Mesh, Object3D, PerspectiveCamera, PlaneGeometry, Scene, Vector2, Vector3 } from 'three'
import { fitCascades, type CascadeLight } from '../src/creative-scene/render/sunShadow.ts'

// The sun's shadow in cascades (fix/scene-sun-cascaded-shadows): each map covers what in its slice of the view can be shadowed.

function sunScene(at: [number, number, number] = [30, -20, 40]) {
  const scene = new Scene()
  const sun = new DirectionalLight()
  sun.position.set(...at)
  sun.castShadow = true
  sun.shadow.mapSize.set(2048, 2048)
  scene.add(sun)
  const camera = new PerspectiveCamera(45, 16 / 9, 0.05, 5000)
  camera.up.set(0, 0, 1)
  camera.position.set(0, -12, 4)
  camera.lookAt(0, 0, 0.5)
  const lights: CascadeLight[] = Array.from({ length: 4 }, () => Object.assign(new Object3D(), { target: new Object3D(), shadow: sun.shadow.clone() }))
  const ranges = lights.map(() => new Vector2())
  return { scene, sun, camera, lights, ranges }
}

function box(scene: Scene, size: [number, number, number], at: [number, number, number]) {
  const m = new Mesh(new BoxGeometry(...size))
  m.position.set(...at)
  m.castShadow = true
  scene.add(m)
  return m
}

function fit({ scene, sun, camera, lights, ranges }: ReturnType<typeof sunScene>) {
  scene.updateMatrixWorld()
  camera.updateMatrixWorld()
  fitCascades(camera, sun, lights, ranges)
  for (const l of lights) l.shadow!.updateMatrices(l as unknown as DirectionalLight)
}

/** The cascade the shader picks for a world point: by its view depth's share of near..far. */
function cascadeOf({ camera, ranges }: ReturnType<typeof sunScene>, p: Vector3) {
  const depth = -p.clone().applyMatrix4(camera.matrixWorldInverse).z
  const share = (depth - camera.near) / (camera.far - camera.near)
  return ranges.findIndex((r) => share >= r.x && share <= r.y)
}

function onMap(light: CascadeLight, p: Vector3) {
  const q = p.clone().applyMatrix4(light.shadow!.matrix)
  return [q.x, q.y, q.z].every((v) => v >= 0 && v <= 1)
}

function corners(m: Mesh) {
  const b = m.geometry.boundingBox ?? (m.geometry.computeBoundingBox(), m.geometry.boundingBox!)
  const out: Vector3[] = []
  for (const x of [b.min.x, b.max.x]) for (const y of [b.min.y, b.max.y]) for (const z of [b.min.z, b.max.z]) out.push(new Vector3(x, y, z).applyMatrix4(m.matrixWorld))
  return out
}

const texel = (l: CascadeLight) => (l.shadow!.camera.right - l.shadow!.camera.left) / l.shadow!.mapSize.x

test('the view is split from near to far, log from 0.5 m', () => {
  const s = sunScene()
  box(s.scene, [1, 1, 1], [0, 0, 0.5])
  fit(s)
  const { near, far } = s.camera
  const at = (share: number) => near + share * (far - near)
  assert.equal(s.ranges[0].x, 0)
  assert.equal(s.ranges[3].y, 1)
  for (let i = 1; i < 4; i++) assert.equal(s.ranges[i].x, s.ranges[i - 1].y)
  assert.deepEqual(
    s.ranges.slice(0, 3).map((r) => Math.round(at(r.y))),
    [5, 50, 500],
  )
})

test('a big box far off leaves the shadows by the camera fine and on their casters', () => {
  const s = sunScene()
  const cube = box(s.scene, [1, 1, 1], [0, 0, 0.5])
  const ground = new Mesh(new PlaneGeometry(2000, 2000))
  ground.receiveShadow = true
  s.scene.add(ground)
  const far = box(s.scene, [40, 40, 30], [0, 200, 15])
  fit(s)

  const i = cascadeOf(s, new Vector3(0, 0, 0.5))
  const near = s.lights[i]
  assert.ok(texel(near) < 0.01, `by the camera a texel is ${texel(near)} m`)
  assert.ok(near.shadow!.normalBias < 0.015, `normal bias ${near.shadow!.normalBias} m`)
  for (const p of corners(cube)) assert.ok(onMap(near, p), `the cube's corner ${p.toArray()} on cascade ${i}`)
  // The big box shadows what is around it in its own cascade.
  const j = cascadeOf(s, new Vector3(0, 200, 0))
  assert.ok(j > i)
  for (const p of corners(far).filter((p) => cascadeOf(s, p) === j)) assert.ok(onMap(s.lights[j], p), `the far box's corner ${p.toArray()} on cascade ${j}`)
})

test('the ground takes a caster’s shadow however far along the light it falls', () => {
  const s = sunScene([80, 0, 7]) // a low sun: a 2 m post throws 23 m
  box(s.scene, [0.5, 0.5, 2], [0, 0, 1])
  // Looking along the shadow.
  s.camera.position.set(8, -6, 3)
  s.camera.lookAt(-25, 0, 0)
  fit(s)
  const top = new Vector3(0, 0, 2)
  const toward = s.sun.position.clone().normalize().negate()
  const tip = top.clone().addScaledVector(toward, -top.z / toward.z)
  const i = cascadeOf(s, tip)
  assert.ok(i >= 0)
  assert.ok(onMap(s.lights[i], tip), `the shadow's tip ${tip.toArray()} on cascade ${i}`)
  assert.ok(onMap(s.lights[i], top), `the post's top on cascade ${i}`)
})

test('the camera moving a little moves no texel: the maps keep their size and snap to their texels', () => {
  const s = sunScene()
  box(s.scene, [1, 1, 1], [0, 0, 0.5])
  box(s.scene, [3, 3, 3], [4, 6, 1.5])
  fit(s)
  const before = s.lights.map((l) => ({ size: l.shadow!.camera.right, frame: l.shadow!.camera.matrixWorldInverse.clone() }))
  s.camera.position.x += 0.003
  fit(s)
  s.lights.forEach((l, i) => {
    assert.equal(l.shadow!.camera.right, before[i].size, `cascade ${i} keeps its size`)
    // Across the light, it moved by whole texels.
    const moved = l.position.clone().applyMatrix4(before[i].frame)
    for (const v of [moved.x / texel(l), moved.y / texel(l)]) assert.ok(Math.abs(v - Math.round(v)) < 1e-3, `cascade ${i} moved ${v} texels`)
  })
})

// scene-performance P1 (review B1): a model's parts of one slot are one
// merged geometry; the cascades still fit to each part's box, as before.

import { Group, MeshStandardMaterial } from 'three'
import { partsOf, type ModelMeshPart } from '../src/creative-scene/models/cache.ts'

/** An L of wall in one slot, and a post in it, turned: one box over the L would cover its open corner. */
function wall(): Group {
  const root = new Group()
  const stone = new MeshStandardMaterial({ name: 'slot:stone' })
  const add = (size: [number, number, number], at: [number, number, number]) => {
    const m = new Mesh(new BoxGeometry(...size), stone)
    m.position.set(...at)
    root.add(m)
  }
  add([6, 0.3, 2.5], [0, -3, 1.25])
  add([0.3, 6, 2.5], [3, 0, 1.25])
  add([0.2, 0.2, 4], [-2, 2, 2])
  return root
}

function placed(s: ReturnType<typeof sunScene>, parts: ModelMeshPart[]) {
  const group = new Group()
  group.rotation.z = 0.6
  group.position.set(1, 2, 0)
  for (const p of parts) {
    const m = new Mesh(p.geometry)
    m.castShadow = true
    group.add(m)
  }
  s.scene.add(group)
  return group
}

const cascades = (s: ReturnType<typeof sunScene>) =>
  s.lights.map((l) => {
    const c = l.shadow!.camera
    return [...l.position.toArray(), c.left, c.right, c.top, c.bottom, c.near, c.far, l.shadow!.normalBias]
  })

test("a merged model's cascades are fitted to its parts' boxes, value for value", () => {
  const g = globalThis as { __sceneNoMerge?: boolean }
  g.__sceneNoMerge = true
  const before = partsOf(wall()).meshes
  delete g.__sceneNoMerge
  const merged = partsOf(wall()).meshes
  assert.equal(merged.length, 1, 'one slot, one geometry')

  const fitted = (parts: ModelMeshPart[]) => {
    const s = sunScene([20, -30, 25])
    s.camera.position.set(4, -9, 3)
    s.camera.lookAt(1, 1, 1)
    placed(s, parts)
    fit(s)
    return cascades(s)
  }
  const expected = fitted(before)
  assert.deepEqual(fitted(merged), expected)
  // One box over the slot (the geometry without its pieces) fits otherwise.
  assert.notDeepEqual(fitted([{ ...merged[0], geometry: merged[0].geometry.clone() }]), expected)
})
