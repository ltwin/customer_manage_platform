import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { luminance } from '../src/creative-scene/world/atmosphere/constants.ts'
import { loadNightAssets, nightAssetLoaders, nightNeeds, parseStars, resetNightAssets } from '../src/creative-scene/world/nightSky/assets.ts'
import { AIRGLOW_LUX, AIRGLOW_ZENITH, bvTint, magnitudeLux, moonAbove, starlightLux, starsScale, surfaceLuminance, vanRhijn } from '../src/creative-scene/world/nightSky/light.ts'
import { completeSky, type PhysicalSky } from '../src/creative-scene/world/physicalSky.ts'
import { milkyWayAt, milkyWayMean } from '../src/creative-scene/world/nightSky/milkyWay.ts'
import { refraction, spriteShare, STAR_SIGMA, starSpread } from '../src/creative-scene/world/nightSky/stars.ts'
import { Refraction } from 'astronomy-engine'
import { lightOf, lommelSeeligerMean, MILKY_WAY_LUX, nightOf, sceneLight, STARLIGHT_LUX, sunLeads } from '../src/creative-scene/world/nightSky/night.ts'
import { physicalSun, sunVector } from '../src/creative-scene/world/astro.ts'
import { CpuAtmosphere } from '../src/creative-scene/world/atmosphere/cpu.ts'
import { sunAbove } from '../src/creative-scene/world/atmosphere/constants.ts'

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

test('the Milky Way: brightest by the galactic centre, a band along the plane, the Great Rift darker than its sides, the Magellanic Clouds in the south', () => {
  const L = (l: number, b: number) => luminance(milkyWayAt(l, b))
  // The Sagittarius star cloud, just south of the centre.
  const sagittarius = L(2, -5)
  assert.ok(sagittarius > 0.6 && sagittarius < 1.4, `Sagittarius ${sagittarius}`)
  // Away from the plane it fades: at 30° it is a twentieth of the band.
  for (const l of [-120, -60, 0, 60, 120, 180]) assert.ok(L(l, 30) < L(l, -2) / 8, `l ${l}`)
  // Toward the anticentre the band is fainter than toward the centre.
  assert.ok(L(180, 0) < L(10, -3) / 2)
  // The rift: the plane at l 30°–50° is darker than 5° to either side, on average.
  let lane = 0
  let sides = 0
  for (let l = 30; l <= 50; l += 2) {
    lane += L(l, 1.5)
    sides += (L(l, 7) + L(l, -5)) / 2
  }
  assert.ok(lane < sides, `rift ${lane} against ${sides}`)
  // The Large Magellanic Cloud stands out of the dark southern sky.
  assert.ok(L(280.5, -32.9) > 5 * L(260, -32.9))
  assert.ok(milkyWayMean() > 0.02 && milkyWayMean() < 0.2, `mean ${milkyWayMean()}`)
})

test('a star sprite keeps the star’s light: its pixels sum to 1 wherever the centre falls, at 1080 px and at 8K, the bright ones too', () => {
  for (const sigma of [STAR_SIGMA, STAR_SIGMA * starSpread(-1.46), STAR_SIGMA * 4]) {
    for (const [fx, fy] of [[0, 0], [0.5, 0.5], [0.3, 0.8]]) {
      let sum = 0
      const reach = Math.ceil(4 * sigma) + 1
      for (let y = -reach; y <= reach; y++) for (let x = -reach; x <= reach; x++) sum += spriteShare(x - fx, y - fy, sigma)
      assert.ok(Math.abs(sum - 1) < 0.005, `σ ${sigma.toFixed(2)} at (${fx}, ${fy}): ${sum}`)
    }
  }
  // Brighter than magnitude 1 the sprite grows, Sirius about half as large again.
  assert.equal(starSpread(1), 1)
  assert.equal(starSpread(5), 1)
  assert.ok(Math.abs(starSpread(-1.46) - 1.49) < 0.01)
})

test('stars are refracted as the sun and the moon are (astronomy-engine’s normal refraction)', () => {
  for (const h of [-0.5, 0, 0.5, 2, 10, 30, 60, 89]) assert.ok(Math.abs(refraction(h) - Refraction('normal', h)) < 1e-9, `${h}°: ${refraction(h)} against ${Refraction('normal', h)}`)
  // By true altitude: about 0.48° at the horizon (0.57° by the apparent one), an arcminute at 45°.
  assert.ok(Math.abs(refraction(0) - 0.48) < 0.01 && Math.abs(refraction(45) - 1 / 60) < 0.002)
})

/** A sky's night as PhysicalLighting works it out: the parts, the sun, its light and E. */
function lightAt(sky: PhysicalSky) {
  const where = physicalSun(sky)
  const sun = sunVector(where.elevationDeg, where.azimuthDeg)
  const night = nightOf(sky, sun)
  return { where, night, light: sceneLight(new CpuAtmosphere(sky), sun, sunAbove(sky.sun_strength, sky.sun_temperature_k), night) }
}

test('the moon’s disc is scaled by Lommel-Seeliger’s shading over it: a half at full, less toward new', () => {
  assert.ok(Math.abs(lommelSeeligerMean(1) - 0.5) < 1e-9)
  const quarter = lommelSeeligerMean(0)
  assert.ok(quarter > 0.1 && quarter < 0.3, `quarter ${quarter}`)
  assert.ok(lommelSeeligerMean(-0.999) < 0.01)
  for (const c of [0.9, 0.5, 0, -0.5]) assert.ok(lommelSeeligerMean(c) < lommelSeeligerMean(c + 0.1))
})

test('the stars’ constant term: the catalogue’s starlight as worked out, the Milky Way’s and the airglow’s; switches and strength', () => {
  assert.ok(Math.abs(STARLIGHT_LUX / starlightLux(catalogue.v) - 1) < 0.002, `${starlightLux(catalogue.v)}`)
  // The airglow is most of a moonless night's light, the Milky Way next.
  assert.ok(AIRGLOW_LUX > MILKY_WAY_LUX && MILKY_WAY_LUX > STARLIGHT_LUX)
  const all = nightOf(beijing({ local_time: '22:00' }), sunVector(-40, 0)).lux
  assert.ok(Math.abs(all - (STARLIGHT_LUX + AIRGLOW_LUX + MILKY_WAY_LUX)) < 1e-12)
  const off = (night: Partial<PhysicalSky['night']>) => nightOf(beijing({ local_time: '22:00', night: { ...beijing({}).night, ...night } }), sunVector(-40, 0))
  assert.ok(Math.abs(off({ stars: false }).lux - MILKY_WAY_LUX) < 1e-12, 'the stars take the airglow with them')
  assert.deepEqual(off({ stars: false }).airglow, [0, 0, 0])
  assert.ok(Math.abs(off({ milky_way: false }).lux - (STARLIGHT_LUX + AIRGLOW_LUX)) < 1e-12)
  assert.ok(Math.abs(off({ stars_strength: 2 }).lux - 2 * all) < 1e-12)
  assert.equal(off({ moon: false }).moon, null)
  // Manual angles draw no night.
  const manual = nightOf(beijing({ local_time: '22:00', sun_mode: 'manual', sun_elevation_deg: -20, sun_azimuth_deg: 0 }), sunVector(-20, 0))
  assert.deepEqual([manual.moon, manual.lux, manual.stars], [null, 0, [0, 0, 0]])
})

test('a full moon high on a dark night lights the ground with 0.1 to 0.4 lux; a new moon adds nothing', () => {
  // 10-26 at 22:00 in Beijing: the full moon about 45° up, the sun far below.
  const full = lightAt(beijing({ local_time: '22:00' }))
  assert.ok(full.night.moon && full.night.moon.elevationDeg > 30, `moon at ${full.night.moon?.elevationDeg}`)
  assert.ok(full.where.elevationDeg < -18, `sun at ${full.where.elevationDeg}`)
  assert.ok(full.light.horizontal > 0.1 && full.light.horizontal < 0.4, `E ${full.light.horizontal}`)
  assert.ok(full.light.moon!.horizontal > 0.9 * (full.light.horizontal - full.night.lux), 'nearly all of it the moon’s')
  // Its strength doubles the moon's share (AC-W3-5).
  const twice = lightAt(beijing({ local_time: '22:00', night: { ...beijing({}).night, moon_strength: 2 } }))
  assert.ok(Math.abs(twice.light.moon!.horizontal / full.light.moon!.horizontal - 2) < 1e-9)
  // 11-09: new moon, below the horizon all night.
  const dark = lightAt(beijing({ date: '2026-11-09', local_time: '22:00' }))
  assert.equal(dark.light.moon, null)
  assert.ok(Math.abs(dark.light.horizontal - dark.light.sun.horizontal - dark.night.lux) < 1e-15)
})

test('the shadowing light: the sun above −6° or with no moon up, otherwise the moon', () => {
  const moon = nightOf(beijing({ local_time: '22:00' }), sunVector(-40, 0)).moon!
  assert.equal(sunLeads(-5.9, moon), true)
  assert.equal(sunLeads(-6.1, moon), false)
  assert.equal(sunLeads(-30, { ...moon, elevationDeg: -0.5 }), true, 'the moon set')
  assert.equal(sunLeads(-30, null), true, 'no moon drawn')
  // Each light's intensity is its brightest channel's lux, its colour the rest.
  const { light } = lightAt(beijing({ local_time: '22:00' }))
  const moonLight = lightOf(light.moon)
  assert.ok(moonLight.intensity > 0.1 && Math.max(...moonLight.color) === 1)
  assert.deepEqual(lightOf(null), { intensity: 0, color: [1, 1, 1] })
})
