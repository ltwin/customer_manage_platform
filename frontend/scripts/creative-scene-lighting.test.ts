import test from 'node:test'
import assert from 'node:assert/strict'
import { DirectionalLight, Group, PointLight, Quaternion, SpotLight, Vector3 } from 'three'
import { applyLight, buildLight } from '../src/creative-scene/objects/lights.ts'
import { exposureOf, NEW_LIGHT_INTENSITY, SKY_GAIN, SKY_SHARE, SUN_MAX, sunLight } from '../src/creative-scene/render/litLook.ts'
import { skyIrradiance } from '../src/creative-scene/render/atmosphere.ts'
import { colorSky, coneAngle, coneDegrees, newLight, newLightHeight, proceduralSky, withFog, withSkyKind } from '../src/creative-scene/editing.ts'

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
  const high = sunLight(60)
  const brightness = (l: ReturnType<typeof sunLight>) => l.intensity * (0.2126 * l.color.r + 0.7152 * l.color.g + 0.0722 * l.color.b)
  assert.ok(Math.abs(brightness(high) / SUN_MAX - 1) < 0.1, `about SUN_MAX high up: ${brightness(high)}`)
  const low = sunLight(5)
  assert.ok(brightness(low) > 0 && brightness(low) < brightness(high) * 0.6, `weaker low down: ${brightness(low)}`)
  assert.ok(low.color.b / low.color.r < (high.color.b / high.color.r) * 0.6, 'warmer near the horizon')
  assert.equal(exposureOf({ exposure_ev: 2 }), 4)
  assert.equal(exposureOf({ exposure_ev: -1 }), 0.5)
  // A scene from before exposure reads 0 from the server (creativescene completeRenderSettings): 2^0.
  assert.equal(exposureOf({ exposure_ev: 0 }), 1)
})

test('at the reference noon the sky lights level ground a quarter as much as the sun, and daylight comes out neutral', () => {
  const y = Math.sin((55 * Math.PI) / 180)
  const sun = sunLight(55, 3)
  const direct = [sun.color.r, sun.color.g, sun.color.b].map((c) => c * sun.intensity * y)
  const sky = skyIrradiance([0, y, Math.sqrt(1 - y * y)], 3).map((v, i) => v * SKY_GAIN[i])
  const total = direct.map((v, i) => v + sky[i])
  const lum = (c: number[]) => 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2]
  assert.ok(Math.abs(lum(sky) / lum(direct) - SKY_SHARE) < 0.02, `sky share ${lum(sky) / lum(direct)}`)
  for (const c of total) assert.ok(Math.abs(c / lum(total) - 1) < 0.01, `neutral: ${total}`)
  // Shadows stay bluer than the sunlit ground, as under a clear sky.
  assert.ok(sky[2] / sky[0] > 1.5, `a blue sky light: ${sky}`)
})

test('the sky integral has converged', () => {
  const sun: [number, number, number] = [0, Math.sin(0.3), Math.cos(0.3)]
  const coarse = skyIrradiance(sun, 3)
  const fine = skyIrradiance(sun, 3, 96, 192)
  for (let i = 0; i < 3; i++) assert.ok(Math.abs(coarse[i] / fine[i] - 1) < 0.02, `${coarse} vs ${fine}`)
})

test('a light from the add menu stands above the pointed ground, facing the view, numbered by type', () => {
  const view = new Quaternion().setFromAxisAngle(new Vector3(1, 0, 0), -0.4).toArray()
  let n = 0
  const make = () => `sobj_light${++n}`
  const spot = newLight('spot', [1, 0, -2], view, [], make)
  assert.equal(spot.kind, 'light')
  assert.equal(spot.name, '聚光 1')
  assert.deepEqual(spot.transform.position, [1, newLightHeight, -2])
  assert.deepEqual(spot.transform.rotation, view, 'it shines the way the view looks')
  assert.equal(spot.light?.intensity, NEW_LIGHT_INTENSITY.spot)
  assert.ok(spot.light?.angle && spot.light.angle <= Math.PI / 2, 'the server accepts the cone')
  const second = newLight('spot', [0, 0, 0], view, [spot], make)
  assert.equal(second.name, '聚光 2')
  assert.equal(second.order, spot.order + 1)
  const point = newLight('point', [0, 0, 0], view, [spot, second], make)
  assert.equal(point.name, '点光 1')
  assert.deepEqual(point.transform.rotation, [0, 0, 0, 1], 'a point light has no direction')
  assert.deepEqual(Object.keys(point.light!).sort(), ['intensity', 'type'], 'only fields a point light has')
})

test('the cone shows its full angle in degrees and stores three.js\'s half angle', () => {
  assert.ok(Math.abs(coneAngle(30) - Math.PI / 12) < 1e-12)
  assert.ok(Math.abs(coneDegrees(Math.PI / 3) - 120) < 1e-12)
  assert.ok(Math.abs(coneDegrees(coneAngle(47)) - 47) < 1e-12)
})

test('switching the sky keeps only the new kind\'s fields; fog toggles with a start', () => {
  const env = { sky: { kind: 'procedural' as const, sun_elevation_deg: 5, sun_azimuth_deg: 200, turbidity: 4 }, ambient: { color: '#ffffff', intensity: 0.4 }, fog: null }
  const colour = withSkyKind(env, 'color')
  assert.deepEqual(colour.sky, colorSky)
  assert.equal(colour.ambient, env.ambient)
  assert.equal(withSkyKind(env, 'procedural'), env, 'the same kind is no change')
  assert.deepEqual(withSkyKind(colour, 'procedural').sky, proceduralSky)
  const foggy = withFog(env, true)
  assert.ok(foggy.fog && foggy.fog.near > 0 && foggy.fog.far > foggy.fog.near, 'the server accepts it')
  assert.equal(withFog(foggy, false).fog, null)
  assert.equal(withFog(env, false), env)
})
