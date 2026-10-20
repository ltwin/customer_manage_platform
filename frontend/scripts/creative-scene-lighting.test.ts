import test from 'node:test'
import assert from 'node:assert/strict'
import { DirectionalLight, Group, PointLight, Quaternion, SpotLight, Vector3 } from 'three'
import { applyLight, buildLight } from '../src/creative-scene/objects/lights.ts'
import { exposureOf, SUN_MAX, sunLight } from '../src/creative-scene/render/litLook.ts'

const close = (a: readonly number[], b: readonly number[], eps = 1e-9) =>
  assert.ok(a.every((v, i) => Math.abs(v - b[i]) < eps), `${JSON.stringify(a)} ≈ ${JSON.stringify(b)}`)

test('light fields go onto three.js lights as they are, with three.js defaults for the rest', () => {
  const spot = buildLight('spot')
  applyLight(spot, { type: 'spot', color: '#ff8800', intensity: 120, distance: 8, decay: 1.5, angle: 0.4, penumbra: 0.3, cast_shadow: true })
  const s = spot.light as SpotLight
  assert.ok(s instanceof SpotLight)
  assert.deepEqual([s.color.getHexString(), s.intensity, s.distance, s.decay, s.angle, s.penumbra, s.castShadow], ['ff8800', 120, 8, 1.5, 0.4, 0.3, true])
  assert.equal(s.shadow.mapSize.x, 1024)

  const point = buildLight('point')
  applyLight(point, { type: 'point' })
  const p = point.light as PointLight
  assert.ok(p instanceof PointLight)
  assert.deepEqual([p.color.getHexString(), p.intensity, p.distance, p.decay, p.castShadow], ['ffffff', 1, 0, 2, false])
  assert.equal(point.target, null)

  const sun = buildLight('directional')
  applyLight(sun, { type: 'directional', intensity: 3, cast_shadow: true })
  assert.ok(sun.light instanceof DirectionalLight)
  assert.ok(sun.stop, 'a shadow-casting directional light fits its shadow like the sun')
  applyLight(sun, { type: 'directional', intensity: 3, cast_shadow: false })
  assert.equal(sun.stop, null)
})

test('directional and spot lights shine along the object\'s local −Z and turn with it', () => {
  for (const type of ['directional', 'spot'] as const) {
    const built = buildLight(type)
    const object = new Group()
    object.position.set(2, 3, 4)
    object.quaternion.copy(new Quaternion().setFromAxisAngle(new Vector3(1, 0, 0), -Math.PI / 2))
    object.add(built.light, built.target!)
    object.updateMatrixWorld(true)
    const from = new Vector3().setFromMatrixPosition(built.light.matrixWorld)
    const to = new Vector3().setFromMatrixPosition(built.target!.matrixWorld)
    // Local −Z turned −90° about X points down −Y.
    close(to.sub(from).normalize().toArray(), [0, -1, 0])
    close(from.toArray(), [2, 3, 4])
  }
})

test('the lit sun dims below the horizon and warms when low; exposure is in stops', () => {
  assert.equal(sunLight(-5).intensity, 0)
  assert.equal(sunLight(60).intensity, SUN_MAX)
  const low = sunLight(3)
  const high = sunLight(60)
  assert.ok(low.intensity > 0 && low.intensity < SUN_MAX)
  assert.ok(low.color.b < high.color.b, 'warmer near the horizon')
  assert.equal(exposureOf({ exposure_ev: 2 }), 4)
  assert.equal(exposureOf({ exposure_ev: -1 }), 0.5)
  assert.equal(exposureOf(undefined), 1, 'scenes from before exposure render as before')
})
