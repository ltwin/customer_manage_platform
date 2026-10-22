import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { applyOps } from '../src/creative-scene/state/apply.ts'
import { documentAzimuth, localToUtcMs, physicalSun, skyInstant, sunAt, sunTimes } from '../src/creative-scene/world/astro.ts'
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
  const partial = applyOps(state, [env({ latitude_deg: 30 })]).scene.environment.sky
  assert.equal(partial.timezone, undefined)
})
