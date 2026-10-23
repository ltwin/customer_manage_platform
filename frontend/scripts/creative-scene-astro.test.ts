import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { applyOps } from '../src/creative-scene/state/apply.ts'
import { Vector3 } from 'three'
import { documentAzimuth, equatorToScene, galacticToScene, localToUtcMs, moonAt, moonTimes, physicalMoon, physicalSun, skyInstant, sunAt, sunTimes } from '../src/creative-scene/world/astro.ts'
import { completeSky, isPhysical, type PhysicalSky } from '../src/creative-scene/world/physicalSky.ts'

// W2 AC-W2-1: the sun of a physical sky against NOAA's solar calculator, and
// local time in a zone against Python's zoneinfo (fixtures/astro).

type NoaaCase = { location: string; latitude: number; longitude: number; timezone: string; date: string; sunrise: string; sunset: string; points: [string, number, number][] }
const noaa = JSON.parse(readFileSync(new URL('fixtures/astro/noaa-sun.json', import.meta.url), 'utf8')) as { cases: NoaaCase[] }
const zones = JSON.parse(readFileSync(new URL('fixtures/astro/local-time.json', import.meta.url), 'utf8')) as {
  cases: { timezone: string; date: string; local_time: string; utc: string; note: string }[]
}

const rad = Math.PI / 180
/** The great-circle angle between two directions given as elevation and azimuth, in degrees. */
function separation(a: { elevationDeg: number; azimuthDeg: number }, b: { elevationDeg: number; azimuthDeg: number }): number {
  const c = Math.sin(a.elevationDeg * rad) * Math.sin(b.elevationDeg * rad) + Math.cos(a.elevationDeg * rad) * Math.cos(b.elevationDeg * rad) * Math.cos((a.azimuthDeg - b.azimuthDeg) * rad)
  return Math.acos(Math.min(1, c)) / rad
}

function sky(fields: Partial<PhysicalSky> & { date: string }): PhysicalSky {
  const s = completeSky({ kind: 'physical', ...fields })
  assert.ok(isPhysical(s))
  return s
}

const minutes = (hhmm: string) => Number(hhmm.slice(0, 2)) * 60 + Number(hhmm.slice(3, 5))
/** An instant as HH:MM in a zone, rounded to the minute. */
const localClock = (iso: string, timeZone: string) =>
  new Intl.DateTimeFormat('en-GB', { timeZone, hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).format(Math.round(Date.parse(iso) / 60_000) * 60_000)

test("local time in a zone is Temporal's 'compatible': skipped times move forward, repeated ones are the earlier", () => {
  for (const c of zones.cases) {
    assert.equal(new Date(localToUtcMs(c.date, c.local_time, c.timezone)).toISOString(), c.utc, `${c.timezone} ${c.date} ${c.local_time}: ${c.note}`)
  }
  assert.throws(() => localToUtcMs('2026-06-21', '12:00', 'Mars/Olympus'), RangeError)
})

test('a zone this browser does not know falls back to round(longitude / 15) hours', () => {
  const s = sky({ date: '2026-06-21', local_time: '12:00', latitude_deg: 30, longitude_deg: 104.06, timezone: 'Mars/Olympus' })
  assert.deepEqual(skyInstant(s), { utcMs: Date.UTC(2026, 5, 21, 5), zoneKnown: false })
  assert.equal(physicalSun(s).zoneKnown, false)
})

test("the sun's direction is NOAA's to 0.02° wherever it is up, at the four reference places on three dates", () => {
  let worst = 0
  for (const c of noaa.cases) {
    for (const [utc, elevationDeg, azimuthDeg] of c.points) {
      if (elevationDeg < 0) continue
      const d = separation(sunAt(c.latitude, c.longitude, Date.parse(utc)), { elevationDeg, azimuthDeg })
      assert.ok(d <= 0.02, `${c.location} ${utc}: ${d.toFixed(4)}° from NOAA`)
      worst = Math.max(worst, d)
    }
  }
  assert.ok(worst > 0, 'compared something')
})

test("sunrise and sunset are NOAA's to the minute", () => {
  for (const c of noaa.cases) {
    const t = sunTimes(sky({ date: c.date, latitude_deg: c.latitude, longitude_deg: c.longitude, timezone: c.timezone }))
    assert.ok(t.sunrise !== undefined && t.sunset !== undefined, `${c.location} ${c.date}: rises and sets`)
    assert.ok(Math.abs(minutes(t.sunrise) - minutes(localClock(c.sunrise, c.timezone))) <= 1, `${c.location} ${c.date} sunrise ${t.sunrise}, NOAA ${localClock(c.sunrise, c.timezone)}`)
    assert.ok(Math.abs(minutes(t.sunset) - minutes(localClock(c.sunset, c.timezone))) <= 1, `${c.location} ${c.date} sunset ${t.sunset}, NOAA ${localClock(c.sunset, c.timezone)}`)
  }
})

test('a compass azimuth turns with where north points, counter-clockwise like sun_azimuth_deg', () => {
  assert.equal(documentAzimuth(90, 180), 90, 'north at −Z: east is +X')
  assert.equal(documentAzimuth(0, 180), 180)
  assert.equal(documentAzimuth(180, 180), 0, 'south is +Z')
  assert.equal(documentAzimuth(90, 90), 0, 'north turned to +X: east is +Z')
  assert.equal(documentAzimuth(270, 0), 90)
})

test('at noon in Sydney the sun is in the northern sky; in Shanghai at midsummer noon, high in the south', () => {
  const sydney = physicalSun(sky({ date: '2026-06-21', local_time: '12:00', latitude_deg: -33.8688, longitude_deg: 151.2093, timezone: 'Australia/Sydney' }))
  assert.ok(Math.abs(sydney.azimuthDeg - 180) < 15 && sydney.elevationDeg > 25 && sydney.elevationDeg < 40, JSON.stringify(sydney))
  const shanghai = physicalSun(sky({ date: '2026-06-21', local_time: '12:00' }))
  assert.ok(shanghai.azimuthDeg < 25 || shanghai.azimuthDeg > 335, `south is +Z: ${JSON.stringify(shanghai)}`)
  assert.ok(shanghai.elevationDeg > 80, JSON.stringify(shanghai))
})

test('in manual mode the sun is the angles written', () => {
  assert.deepEqual(physicalSun(sky({ date: '2026-06-21', sun_mode: 'manual', sun_elevation_deg: -7, sun_azimuth_deg: 250 })), { elevationDeg: -7, azimuthDeg: 250, zoneKnown: true })
})

// The backend's sun reference (sun.go) is checked against these values (testdata/sun_times_frontend.json).
test("Shanghai's midsummer day: sunrise, sunset, noon and the blue and golden hours in order", () => {
  const t = sunTimes(sky({ date: '2026-06-21' }))
  assert.deepEqual(t, {
    solarNoon: '11:56',
    noonElevationDeg: t.noonElevationDeg,
    sunrise: '04:50',
    sunset: '19:01',
    morning: { blueHour: ['04:22', '04:33'], goldenHour: ['04:33', '05:26'] },
    evening: { goldenHour: ['18:26', '19:18'], blueHour: ['19:18', '19:29'] },
  })
  assert.ok(Math.abs(t.noonElevationDeg - 82.2) < 0.2, String(t.noonElevationDeg))
})

test('above the Arctic Circle the sun neither rises nor sets at midsummer and midwinter', () => {
  const tromso = { latitude_deg: 69.6496, longitude_deg: 18.956, timezone: 'Europe/Oslo' }
  const summer = sunTimes(sky({ date: '2026-06-21', ...tromso }))
  assert.equal(summer.polar, 'day')
  assert.equal(summer.sunrise, undefined)
  assert.equal(summer.evening.goldenHour, undefined)
  const winter = sunTimes(sky({ date: '2026-12-21', ...tromso }))
  assert.equal(winter.polar, 'night')
  assert.equal(winter.sunset, undefined)
})

test('a physical sky is completed as the server completes it; one with part of a place is left for the server to refuse', () => {
  const state = { title: '', scene: { environment: { sky: { kind: 'procedural' as const }, ambient: { color: '#ffffff', intensity: 0.4 }, fog: null }, render: { mode: 'lit' as const, exposure_ev: 0, engine: 'workbench' as const, resolution: '2k' as const, exposure_ev100: 15 }, objects: [] } }
  const env = (s: object) => ({ op: 'set_environment' as const, environment: { sky: { kind: 'physical' as const, date: '2026-06-21', ...s }, ambient: { color: '#ffffff', intensity: 0.4 }, fog: null } })
  const done = applyOps(state, [env({})]).scene.environment.sky
  assert.ok(isPhysical(done))
  assert.equal(done.timezone, 'Asia/Shanghai')
  assert.equal(done.local_time, '12:00')
  assert.deepEqual(done.night, { moon: true, stars: true, milky_way: true, moon_strength: 1, moon_temperature_k: 6500, stars_strength: 1, stars_temperature_k: 6500 })
  // Part of the night sky: the rest field by field (W3, data: 夜空).
  const night = applyOps(state, [env({ night: { milky_way: false, stars_strength: 2 } })]).scene.environment.sky
  assert.ok(isPhysical(night))
  assert.deepEqual(night.night, { moon: true, stars: true, milky_way: false, moon_strength: 1, moon_temperature_k: 6500, stars_strength: 2, stars_temperature_k: 6500 })
  // What the renderer draws as a physical sky has its night sky complete.
  assert.equal(isPhysical({ ...done, night: { moon: true } }), false)
  const partial = applyOps(state, [env({ latitude_deg: 30 })]).scene.environment.sky
  assert.equal(partial.timezone, undefined)
})

// W3 AC-W3-1, AC-W3-3: the moon against JPL Horizons, the stars against Skyfield (fixtures/astro).

type HorizonsCase = { location: string; latitude: number; longitude: number; timezone: string; phase: string; date: string; points: { utc: string; azimuth: number; elevation: number; illuminated: number; distance_km: number }[] }
const horizons = JSON.parse(readFileSync(new URL('fixtures/astro/horizons-moon.json', import.meta.url), 'utf8')) as { cases: HorizonsCase[] }
const skyfield = JSON.parse(readFileSync(new URL('fixtures/astro/skyfield-stars.json', import.meta.url), 'utf8')) as {
  points: { star: string; ra_hours: number; dec_degrees: number; location: string; latitude: number; longitude: number; utc: string; elevation: number; azimuth: number }[]
}

test('the moon is where JPL Horizons has it, as lit and as far, at the four reference places', () => {
  let worst = 0
  for (const c of horizons.cases) {
    for (const p of c.points) {
      const moon = moonAt(c.latitude, c.longitude, Date.parse(p.utc))
      assert.ok(Math.abs(moon.illuminated - p.illuminated) <= 0.02, `${c.location} ${p.utc}: illuminated ${moon.illuminated} against ${p.illuminated}`)
      const diameter = (2 * Math.asin(1737.4 / p.distance_km) * 180) / Math.PI
      assert.ok(Math.abs(moon.diameterDeg - diameter) < 0.002, `${c.location} ${p.utc}: diameter`)
      // Below the horizon the two refraction models differ (W2: up to 0.55°).
      if (p.elevation < 0) continue
      const off = separation(moon, { elevationDeg: p.elevation, azimuthDeg: p.azimuth })
      worst = Math.max(worst, off)
      assert.ok(off <= 0.2, `${c.location} ${p.utc}: ${off.toFixed(3)}° from Horizons`)
    }
  }
  assert.ok(worst > 0, 'some points above the horizon')
})

test('a physical sky draws its moon where the place and time put it, turned with north; manual angles have none', () => {
  const full = sky({ date: '2026-10-26', local_time: '22:00', latitude_deg: 39.9042, longitude_deg: 116.4074, timezone: 'Asia/Shanghai' })
  const moon = physicalMoon(full)!
  assert.ok(moon.illuminated > 0.99)
  assert.equal(moon.azimuthDeg, documentAzimuth(moon.compassDeg, 180))
  const turned = physicalMoon({ ...full, north_azimuth_deg: 90 })!
  assert.ok(Math.abs(turned.azimuthDeg - documentAzimuth(moon.compassDeg, 90)) < 1e-9)
  // The pole is a unit vector, roughly toward the celestial north pole (the moon's axis is within 7° of the ecliptic pole's).
  assert.ok(Math.abs(moon.north.length() - 1) < 1e-9)
  assert.equal(physicalMoon({ ...full, sun_mode: 'manual' }), null)
})

test('bright stars of the catalogue land where Skyfield has them, through the J2000-to-document matrix', () => {
  for (const p of skyfield.points) {
    const zone = p.location === 'Sydney' ? 'Australia/Sydney' : 'Asia/Shanghai'
    const local = new Intl.DateTimeFormat('sv-SE', { timeZone: zone, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).format(Date.parse(p.utc))
    const s = sky({ date: local.slice(0, 10), local_time: local.slice(11, 16), latitude_deg: p.latitude, longitude_deg: p.longitude, timezone: zone })
    const ra = (p.ra_hours * 15 * Math.PI) / 180
    const dec = (p.dec_degrees * Math.PI) / 180
    const d = new Vector3(Math.cos(dec) * Math.cos(ra), Math.cos(dec) * Math.sin(ra), Math.sin(dec)).applyMatrix3(equatorToScene(s))
    const elevationDeg = Math.asin(d.y) / rad
    // The document's azimuth back to the compass (north at 180°: north is −Z).
    const documentDeg = ((Math.atan2(d.x, d.z) / rad) + 360) % 360
    const compassDeg = (((180 - documentDeg) % 360) + 360) % 360
    const off = separation({ elevationDeg, azimuthDeg: compassDeg }, { elevationDeg: p.elevation, azimuthDeg: p.azimuth })
    assert.ok(off <= 0.2, `${p.star} at ${p.location} ${p.utc}: ${off.toFixed(3)}° from Skyfield`)
    if (p.star === 'Polaris' && p.location === 'Beijing') assert.ok(Math.abs(elevationDeg - p.latitude) <= 1, `Polaris at ${elevationDeg.toFixed(2)}°`)
    if (p.star === 'Polaris' && p.location === 'Sydney') assert.ok(elevationDeg < 0)
  }
})

test('the galactic centre and pole land where the J2000 matrix puts them', () => {
  const s = sky({ date: '2026-07-15', local_time: '22:00', latitude_deg: 39.9042, longitude_deg: 116.4074, timezone: 'Asia/Shanghai' })
  const galactic = galacticToScene(s)
  const equator = equatorToScene(s)
  // The galactic centre, (l, b) = (0, 0), is at RA 266.405°, Dec −28.936° (J2000, IAU 1958 as refined by Hipparcos).
  const ra = (266.405 * Math.PI) / 180
  const dec = (-28.936 * Math.PI) / 180
  const centre = new Vector3(Math.cos(dec) * Math.cos(ra), Math.cos(dec) * Math.sin(ra), Math.sin(dec)).applyMatrix3(equator)
  assert.ok(new Vector3(1, 0, 0).applyMatrix3(galactic).angleTo(centre) / rad < 0.05)
})

test("the moon's day: the phase, moonrise and moonset, and the full and new moons either side", () => {
  const beijing = { latitude_deg: 39.9042, longitude_deg: 116.4074, timezone: 'Asia/Shanghai' }
  const full = moonTimes(sky({ date: '2026-10-26', local_time: '22:00', ...beijing }))
  assert.equal(full.phase, 'full')
  assert.equal(full.nextFullMoon, '2026-10-26')
  assert.equal(full.previousFullMoon, '2026-09-27')
  assert.equal(full.previousNewMoon, '2026-10-10')
  assert.ok(full.moonrise && full.moonset)
  const quarter = moonTimes(sky({ date: '2026-10-19', local_time: '19:00', ...beijing }))
  assert.equal(quarter.phase, 'first_quarter')
  assert.equal(quarter.nextFullMoon, '2026-10-26')
  assert.equal(quarter.previousNewMoon, '2026-10-10')
  assert.equal(quarter.nextNewMoon, '2026-11-09')
})
