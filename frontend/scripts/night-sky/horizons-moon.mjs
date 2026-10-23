// Builds scripts/fixtures/astro/horizons-moon.json (W3 AC-W3-1): the moon from
// JPL Horizons (ssd.jpl.nasa.gov/api/horizons.api) at the four reference places
// on a new, a first-quarter and a full moon day of October 2026, hourly over
// the local day: apparent azimuth and elevation (standard refraction),
// illuminated fraction and distance. Run by hand: node scripts/night-sky/horizons-moon.mjs
import { writeFile } from 'node:fs/promises'

const places = [
  { location: 'Shanghai', latitude: 31.2304, longitude: 121.4737, timezone: 'Asia/Shanghai', utcOffset: 8 },
  { location: 'Beijing', latitude: 39.9042, longitude: 116.4074, timezone: 'Asia/Shanghai', utcOffset: 8 },
  { location: 'Chengdu', latitude: 30.6598, longitude: 104.0633, timezone: 'Asia/Shanghai', utcOffset: 8 },
  { location: 'Sydney', latitude: -33.8688, longitude: 151.2093, timezone: 'Australia/Sydney', utcOffset: 11 },
]
const days = [
  { phase: 'new', date: '2026-10-10' },
  { phase: 'first-quarter', date: '2026-10-18' },
  { phase: 'full', date: '2026-10-26' },
]

const pad = (n) => String(n).padStart(2, '0')
const AU_KM = 149597870.7

async function query(place, date) {
  // The local day in UTC: from local 00:00 to 23:00, hourly.
  const start = new Date(`${date}T00:00:00Z`).getTime() - place.utcOffset * 3600_000
  const fmt = (ms) => { const d = new Date(ms); return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())} ${pad(d.getUTCHours())}:00` }
  const params = new URLSearchParams({
    format: 'text', COMMAND: "'301'", OBJ_DATA: "'NO'", MAKE_EPHEM: "'YES'", EPHEM_TYPE: "'OBSERVER'", CENTER: "'coord@399'",
    COORD_TYPE: "'GEODETIC'", SITE_COORD: `'${place.longitude},${place.latitude},0'`, START_TIME: `'${fmt(start)}'`, STOP_TIME: `'${fmt(start + 23 * 3600_000)}'`,
    STEP_SIZE: "'1 h'", QUANTITIES: "'4,10,20'", APPARENT: "'REFRACTED'", TIME_DIGITS: "'MINUTES'", CAL_FORMAT: "'CAL'",
  })
  const text = await (await fetch(`https://ssd.jpl.nasa.gov/api/horizons.api?${params}`)).text()
  const body = text.split('$$SOE')[1]?.split('$$EOE')[0]
  if (!body) throw new Error(`no ephemeris for ${place.location} ${date}:\n${text.slice(0, 2000)}`)
  const months = { Jan: 1, Feb: 2, Mar: 3, Apr: 4, May: 5, Jun: 6, Jul: 7, Aug: 8, Sep: 9, Oct: 10, Nov: 11, Dec: 12 }
  return body.trim().split('\n').map((line) => {
    // 2026-Oct-26 14:00  m  116.485601  53.959853   99.61285  0.00242248132848  -0.2172594
    // The solar and lunar presence markers between the time and the numbers are left out when blank: take the last five fields.
    const m = line.trim().match(/^(\d{4})-(\w{3})-(\d{2}) (\d{2}):(\d{2})/)
    const fields = line.trim().split(/\s+/).slice(-5).map(Number)
    if (!m || fields.some((v) => !Number.isFinite(v))) throw new Error(`unparsed: ${line}`)
    const [azimuth, elevation, percent, au] = fields
    const utc = `${m[1]}-${pad(months[m[2]])}-${m[3]}T${m[4]}:${m[5]}:00Z`
    return { utc, azimuth, elevation, illuminated: percent / 100, distance_km: Math.round(au * AU_KM) }
  })
}

const cases = []
for (const place of places) {
  for (const day of days) {
    const { utcOffset, ...where } = place
    cases.push({ ...where, ...day, points: await query(place, day.date) })
    console.log(place.location, day.date)
  }
}
const out = {
  source: 'JPL Horizons API, target 301 (Moon), observer coord@399 geodetic at sea level, QUANTITIES 4 (apparent az/el, refracted), 10 (illuminated %), 20 (range)',
  fetched: '2026-10-23',
  cases,
}
await writeFile(new URL('../fixtures/astro/horizons-moon.json', import.meta.url), `${JSON.stringify(out, null, 1)}\n`)
