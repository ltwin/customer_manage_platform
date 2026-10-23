import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { luminance } from '../src/creative-scene/world/atmosphere/constants.ts'
import { loadNightAssets, nightAssetLoaders, nightNeeds, parseStars, resetNightAssets } from '../src/creative-scene/world/nightSky/assets.ts'
import { AIRGLOW_LUX, AIRGLOW_ZENITH, bvTint, magnitudeLux, moonAbove, starlightLux, starsScale, surfaceLuminance, vanRhijn } from '../src/creative-scene/world/nightSky/light.ts'
import { completeSky, type PhysicalSky } from '../src/creative-scene/world/physicalSky.ts'

// W3: the night sky's light and assets (design「亮度与颜色的单一来源」「资源与确定性」).

const catalogue = parseStars(readFileSync(new URL('../src/creative-scene/world/nightSky/stars.bin', import.meta.url)).buffer as ArrayBuffer)

test('the star catalogue: 8404 stars to V 6.5, brightest first, Sirius at its place', () => {
  assert.equal(catalogue.count, 8404)
  assert.ok(catalogue.v.every((v, i) => v <= 6.5 + 1e-6 && (i === 0 || v >= catalogue.v[i - 1] - 1e-6)))
  // Sirius: RA 6h 45m 08.9s, Dec −16° 42′ 58″, V −1.46, B−V 0.00.
  assert.ok(Math.abs(catalogue.ra[0] - 101.287) < 0.01 && Math.abs(catalogue.dec[0] + 16.716) < 0.01)
  assert.ok(Math.abs(catalogue.v[0] + 1.46) <= 0.025 && Math.abs(catalogue.bv[0]) <= 0.01)
})

test('magnitudes to illuminance against the sun: the full moon about 0.3 lux, Sirius about 1e-5', () => {
  assert.ok(Math.abs(magnitudeLux(-26.74) - 128000) < 1)
  const full = magnitudeLux(-12.7)
  assert.ok(full > 0.2 && full < 0.4, `full moon ${full}`)
  const sirius = magnitudeLux(-1.46)
  assert.ok(sirius > 0.8e-5 && sirius < 1.3e-5, `Sirius ${sirius}`)
  // 21.8 magnitudes per square arcsecond: about 2e-4 cd/m².
  assert.ok(Math.abs(surfaceLuminance(21.8) - 2e-4) < 0.3e-4)
})

test('colours: a sun-like star is white, a red giant red, a hot star blue; the moon warmer than sunlight', () => {
  const sunLike = bvTint(0.65)
  for (const c of sunLike) assert.ok(Math.abs(c - 1) < 1e-6)
  const red = bvTint(1.6)
  assert.ok(red[0] > red[2] * 2)
  const blue = bvTint(-0.2)
  assert.ok(blue[2] > blue[0])
  for (const tint of [red, blue]) assert.ok(Math.abs(luminance(tint) - 1) < 1e-9)
  const moon = moonAbove(-12.7, 1, 6500)
  assert.ok(moon[0] > moon[2])
  assert.ok(Math.abs(luminance(moon) - magnitudeLux(-12.7)) < 1e-12)
  // The strength scales, the colour temperature tints without changing the brightness.
  assert.ok(Math.abs(luminance(moonAbove(-12.7, 2, 6500)) - 2 * luminance(moon)) < 1e-12)
  const cool = moonAbove(-12.7, 1, 9000)
  assert.ok(Math.abs(luminance(cool) - luminance(moon)) < 1e-12 && cool[2] / cool[0] > moon[2] / moon[0])
  for (const c of starsScale(1, 6500)) assert.ok(Math.abs(c - 1) < 1e-9)
})

test('the airglow brightens toward the horizon and lights level ground with about a thousandth of a lux', () => {
  assert.ok(Math.abs(vanRhijn(1) - 1) < 1e-12)
  assert.ok(vanRhijn(0) > 5 && vanRhijn(0) < 7)
  assert.ok(AIRGLOW_ZENITH > 1.5e-4 && AIRGLOW_ZENITH < 2.5e-4)
  assert.ok(AIRGLOW_LUX > 4e-4 && AIRGLOW_LUX < 2e-3, `airglow ${AIRGLOW_LUX}`)
  // All the catalogue's stars together light level ground less than the airglow.
  const stars = starlightLux(catalogue.v)
  assert.ok(stars > 1e-5 && stars < AIRGLOW_LUX, `starlight ${stars}`)
})

const beijing = (fields: Partial<PhysicalSky>) =>
  completeSky({ kind: 'physical', date: '2026-10-26', latitude_deg: 39.9042, longitude_deg: 116.4074, timezone: 'Asia/Shanghai', ...fields }) as PhysicalSky

test('assets are needed only by night, by a moon that is up, and only for the parts switched on', () => {
  // 10-26: full moon, up all night, down at noon.
  assert.deepEqual(nightNeeds(beijing({ local_time: '12:00' })), { stars: false, moon: false })
  assert.deepEqual(nightNeeds(beijing({ local_time: '22:00' })), { stars: true, moon: true })
  assert.deepEqual(nightNeeds(beijing({ local_time: '22:00', night: { moon: false, stars: false, milky_way: true, moon_strength: 1, moon_temperature_k: 6500, stars_strength: 1, stars_temperature_k: 6500 } })), { stars: false, moon: false })
  assert.deepEqual(nightNeeds(beijing({ local_time: '22:00', sun_mode: 'manual' })), { stars: false, moon: false })
})

test('an offscreen picture waits for the assets it needs and fails when one does not load', async () => {
  const loaders = { ...nightAssetLoaders }
  const calls: string[] = []
  try {
    nightAssetLoaders.stars = async () => {
      calls.push('stars')
      return catalogue
    }
    nightAssetLoaders.moon = async () => {
      calls.push('moon')
      throw new Error('offline')
    }
    resetNightAssets()
    const env = (sky: PhysicalSky) => ({ environment: { sky, ambient: { color: '#ffffff', intensity: 0.4 }, fog: null } })
    await loadNightAssets(env(beijing({ local_time: '12:00' })))
    assert.deepEqual(calls, [])
    await assert.rejects(loadNightAssets(env(beijing({ local_time: '22:00' }))), /offline/)
    // The moon is tried again next time; the stars came the first time.
    await assert.rejects(loadNightAssets(env(beijing({ local_time: '22:00' }))), /offline/)
    assert.deepEqual(calls, ['stars', 'moon', 'moon'])
  } finally {
    Object.assign(nightAssetLoaders, loaders)
    resetNightAssets()
  }
})
