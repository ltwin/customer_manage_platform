import { Body, Equator, Horizon, MakeTime, Observer, SearchAltitude, SearchHourAngle } from 'astronomy-engine'
import type { PhysicalSky } from './physicalSky.ts'

// Where a physical sky's sun is (Epic design「地点与时间」). Pure: the time
// comes from the scene, never from the clock, so an export does not depend
// on when it is taken (Date.now() and new Date() without arguments are
// refused in world/ and render/).

const DAY_MS = 86_400_000
const MINUTE_MS = 60_000

/** Sunrise and sunset: the sun's centre at this geometric altitude, refraction and its radius counted in (NOAA). */
export const HORIZON_DEG = -0.833
/** The golden hour: the sun's centre from GOLDEN_HOUR_DEG[0] to [1]; the blue hour from BLUE_HOUR_DEG[0] to [1]. */
export const GOLDEN_HOUR_DEG = [-4, 6] as const
export const BLUE_HOUR_DEG = [-6, -4] as const

const formatters = new Map<string, Intl.DateTimeFormat>()

/** A formatter of the wall clock in timeZone; Intl throws a RangeError for a zone it does not know. */
function clockIn(timeZone: string): Intl.DateTimeFormat {
  let f = formatters.get(timeZone)
  if (!f) {
    f = new Intl.DateTimeFormat('en-US', {
      timeZone,
      calendar: 'gregory',
      numberingSystem: 'latn',
      hourCycle: 'h23',
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    })
    formatters.set(timeZone, f)
  }
  return f
}

/** timeZone's offset from UTC at an instant, in milliseconds east (to the second: local mean times have seconds). */
function offsetMs(utcMs: number, timeZone: string): number {
  const whole = Math.floor(utcMs / 1000) * 1000
  const f: Record<string, number> = {}
  for (const part of clockIn(timeZone).formatToParts(whole)) if (part.type !== 'literal') f[part.type] = Number(part.value)
  return Date.UTC(f.year, f.month - 1, f.day, f.hour, f.minute, f.second) - whole
}

/** A date and time as if they were UTC: the wall clock as a number. */
function wallMs(date: string, localTime: string): number {
  const [y, mo, d] = date.split('-').map(Number)
  const [h, mi] = localTime.split(':').map(Number)
  return Date.UTC(y, mo - 1, d, h, mi)
}

/**
 * The UTC instant of a local date and time in an IANA zone, as Temporal's
 * 'compatible' (owner 2026-10-22): a time the clocks skip moves forward by
 * the gap, a repeated time is the earlier one. Assumes a zone changes its
 * offset at most once in two days, as every real one does. Throws a
 * RangeError for a zone this browser does not know.
 */
export function localToUtcMs(date: string, localTime: string, timeZone: string): number {
  const wall = wallMs(date, localTime)
  const before = offsetMs(wall - DAY_MS, timeZone)
  const after = offsetMs(wall + DAY_MS, timeZone)
  const valid = [...new Set([before, after])].map((o) => wall - o).filter((t) => offsetMs(t, timeZone) === wall - t)
  // In a gap neither offset gives the wall clock back: the one before the jump moves it forward.
  return valid.length > 0 ? Math.min(...valid) : wall - before
}

/** An instant's offset from UTC for a sky: its zone's, or round(longitude / 15) hours where this browser does not know the zone. */
function skyOffset(sky: PhysicalSky): { at: (utcMs: number) => number; zoneKnown: boolean } {
  try {
    clockIn(sky.timezone)
    return { at: (t) => offsetMs(t, sky.timezone), zoneKnown: true }
  } catch {
    const fixed = Math.round(sky.longitude_deg / 15) * 3_600_000
    return { at: () => fixed, zoneKnown: false }
  }
}

/** The UTC instant a sky's local date and time name; zoneKnown is false where the fallback offset stood in (the environment tab says so). */
export function skyInstant(sky: PhysicalSky, localTime = sky.local_time): { utcMs: number; zoneKnown: boolean } {
  const offset = skyOffset(sky)
  if (offset.zoneKnown) return { utcMs: localToUtcMs(sky.date, localTime, sky.timezone), zoneKnown: true }
  return { utcMs: wallMs(sky.date, localTime) - offset.at(0), zoneKnown: false }
}

/** The sun's apparent elevation (standard refraction) and compass azimuth (clockwise from north), from sea level at a place and instant. */
export function sunAt(latitudeDeg: number, longitudeDeg: number, utcMs: number): { elevationDeg: number; azimuthDeg: number } {
  const observer = new Observer(latitudeDeg, longitudeDeg, 0)
  const time = MakeTime(new Date(utcMs))
  const eq = Equator(Body.Sun, time, observer, true, true)
  const hor = Horizon(time, observer, eq.ra, eq.dec, 'normal')
  return { elevationDeg: hor.altitude, azimuthDeg: hor.azimuth }
}

/** A compass azimuth as the document's (sun_azimuth_deg's convention, counter-clockwise from +Z), given where north points. */
export function documentAzimuth(compassDeg: number, northAzimuthDeg: number): number {
  return (((northAzimuthDeg - compassDeg) % 360) + 360) % 360
}

/** A physical sky's sun in the document's terms: its manual angles, or where its place and time put it. */
export function physicalSun(sky: PhysicalSky): { elevationDeg: number; azimuthDeg: number; zoneKnown: boolean } {
  if (sky.sun_mode === 'manual') return { elevationDeg: sky.sun_elevation_deg, azimuthDeg: sky.sun_azimuth_deg, zoneKnown: true }
  const { utcMs, zoneKnown } = skyInstant(sky)
  const sun = sunAt(sky.latitude_deg, sky.longitude_deg, utcMs)
  return { elevationDeg: sun.elevationDeg, azimuthDeg: documentAzimuth(sun.azimuthDeg, sky.north_azimuth_deg), zoneKnown }
}

/** A stretch of the day, from and to, in local HH:MM. */
export type Span = readonly [string, string]

/** A day's sun in the sky's local time (HH:MM): what the environment tab shows, and what the server tells the assistant (backend sun.go). */
export type SunTimes = {
  solarNoon: string
  noonElevationDeg: number
  sunrise?: string
  sunset?: string
  /** Set when the sun neither rises nor sets that day. */
  polar?: 'day' | 'night'
  morning: { blueHour?: Span; goldenHour?: Span }
  evening: { goldenHour?: Span; blueHour?: Span }
}

/** The day of a sky's date and place: sunrise and sunset, solar noon, and the blue and golden hours of morning and evening, those the day has. */
export function sunTimes(sky: PhysicalSky): SunTimes {
  const offset = skyOffset(sky)
  const start = skyInstant(sky, '00:00').utcMs
  const next = new Date(wallMs(sky.date, '00:00') + DAY_MS).toISOString().slice(0, 10)
  const end = skyInstant({ ...sky, date: next }, '00:00').utcMs
  const observer = new Observer(sky.latitude_deg, sky.longitude_deg, 0)
  const crossing = (direction: 1 | -1, altitudeDeg: number): number | null => {
    const t = SearchAltitude(Body.Sun, observer, direction, MakeTime(new Date(start)), (end - start) / DAY_MS, altitudeDeg)
    return t && t.date.getTime() < end ? t.date.getTime() : null
  }
  const clock = (utcMs: number): string => {
    const local = Math.round((utcMs + offset.at(utcMs)) / MINUTE_MS) * MINUTE_MS
    return new Date(local).toISOString().slice(11, 16)
  }
  const span = (from: number | null, to: number | null): Span | undefined => (from !== null && to !== null && from < to ? [clock(from), clock(to)] : undefined)
  const noon = SearchHourAngle(Body.Sun, observer, 0, MakeTime(new Date(start)))
  const rise = crossing(1, HORIZON_DEG)
  const set = crossing(-1, HORIZON_DEG)
  const out: SunTimes = {
    solarNoon: clock(noon.time.date.getTime()),
    noonElevationDeg: noon.hor.altitude,
    morning: { blueHour: span(crossing(1, BLUE_HOUR_DEG[0]), crossing(1, BLUE_HOUR_DEG[1])), goldenHour: span(crossing(1, GOLDEN_HOUR_DEG[0]), crossing(1, GOLDEN_HOUR_DEG[1])) },
    evening: { goldenHour: span(crossing(-1, GOLDEN_HOUR_DEG[1]), crossing(-1, GOLDEN_HOUR_DEG[0])), blueHour: span(crossing(-1, BLUE_HOUR_DEG[1]), crossing(-1, BLUE_HOUR_DEG[0])) },
  }
  if (rise !== null) out.sunrise = clock(rise)
  if (set !== null) out.sunset = clock(set)
  if (rise === null && set === null) out.polar = noon.hor.altitude > 0 ? 'day' : 'night'
  return out
}
