import test from 'node:test'
import assert from 'node:assert/strict'
import { BoxGeometry, DirectionalLight, Mesh, PlaneGeometry, Scene, Vector3 } from 'three'
import { fitSunShadow } from '../src/creative-scene/render/sunShadow.ts'

function sunScene() {
  const scene = new Scene()
  const sun = new DirectionalLight()
  sun.position.set(30, 20, 40)
  sun.castShadow = true
  sun.shadow.mapSize.set(2048, 2048)
  scene.add(sun)
  return { scene, sun }
}

function box(scene: Scene, size: [number, number, number], at: [number, number, number]) {
  const m = new Mesh(new BoxGeometry(...size))
  m.position.set(...at)
  m.castShadow = true
  scene.add(m)
  return m
}

/** Where a world point falls on the shadow map: x, y and depth in 0..1 inside it. */
function onMap(sun: DirectionalLight, p: Vector3) {
  return p.clone().applyMatrix4(sun.shadow.matrix)
}

function corners(m: Mesh) {
  const b = m.geometry.boundingBox!
  const out: Vector3[] = []
  for (const x of [b.min.x, b.max.x]) for (const y of [b.min.y, b.max.y]) for (const z of [b.min.z, b.max.z]) out.push(new Vector3(x, y, z).applyMatrix4(m.matrixWorld))
  return out
}

test('the sun’s shadow map frames every caster, and tightly', () => {
  const { scene, sun } = sunScene()
  const house = box(scene, [6, 8, 5], [2, 1, 4])
  const lantern = box(scene, [0.4, 0.4, 0.6], [-2, 3, 2])
  const ground = new Mesh(new PlaneGeometry(500, 500))
  ground.receiveShadow = true
  scene.add(ground)
  const hidden = box(scene, [1, 1, 1], [200, 0, 0])
  hidden.visible = false
  fitSunShadow(sun)
  scene.updateMatrixWorld()
  sun.shadow.updateMatrices(sun)

  for (const p of [...corners(house), ...corners(lantern)]) {
    const q = onMap(sun, p)
    for (const v of [q.x, q.y, q.z]) assert.ok(v >= 0 && v <= 1, `caster corner ${p.toArray()} on the map at ${q.toArray()}`)
  }
  // Neither the 500 m ground nor the hidden box widens it: a fixed ±30 m box would be far wider.
  const cam = sun.shadow.camera
  const width = Math.max(cam.right - cam.left, cam.top - cam.bottom)
  assert.ok(width < 15, `map spans ${width} m`)
  assert.ok(sun.shadow.normalBias > 0 && sun.shadow.normalBias < 0.02, `normal bias ${sun.shadow.normalBias} is about a texel`)
})

test('the ground under a caster takes its shadow, however far behind along the light', () => {
  const { scene, sun } = sunScene()
  sun.position.set(80, 0, 7) // a low sun: long shadows
  box(scene, [1, 1, 6], [0, 0, 3])
  fitSunShadow(sun)
  scene.updateMatrixWorld()
  sun.shadow.updateMatrices(sun)
  // The top of the post throws its shadow along the light onto the ground.
  const top = new Vector3(0, 0, 6)
  const dir = sun.position.clone().normalize().negate()
  const onGround = top.clone().addScaledVector(dir, -top.z / dir.z)
  const q = onMap(sun, onGround)
  for (const v of [q.x, q.y, q.z]) assert.ok(v >= 0 && v <= 1, `shadow tip ${onGround.toArray()} on the map at ${q.toArray()}`)
})

test('with nothing casting, the shadow camera is left as it was; the undo restores three’s own update', () => {
  const { scene, sun } = sunScene()
  const cam = sun.shadow.camera
  const before = [cam.left, cam.right, cam.top, cam.bottom, cam.near, cam.far]
  const undo = fitSunShadow(sun)
  scene.updateMatrixWorld()
  sun.shadow.updateMatrices(sun)
  assert.deepEqual([cam.left, cam.right, cam.top, cam.bottom, cam.near, cam.far], before)
  undo()
  assert.ok(!Object.hasOwn(sun.shadow, 'updateMatrices') || sun.shadow.updateMatrices === Object.getPrototypeOf(sun.shadow).updateMatrices)
})
