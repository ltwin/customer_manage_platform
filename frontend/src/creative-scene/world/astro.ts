import {
  Body,
  CombineRotation,
  Equator,
  Horizon,
  Illumination,
  MakeTime,
  MoonPhase,
  Observer,
  RotateVector,
  RotationAxis,
  Rotation_EQJ_HOR,
  Rotation_GAL_EQJ,
  SearchAltitude,
  SearchHourAngle,
  SearchMoonPhase,
  SearchRiseSet,
  Vector,
  type RotationMatrix,
} from 'astronomy-engine'
import { Matrix3, Vector3 } from 'three'
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

/** The unit vector toward the sun in document space (Y up) at an elevation and a document azimuth (counter-clockwise from +Z). */
export function sunVector(elevationDeg: number, azimuthDeg: number): Vector3 {
  const el = (elevationDeg * Math.PI) / 180
  const az = (azimuthDeg * Math.PI) / 180
  return new Vector3(Math.sin(az) * Math.cos(el), Math.sin(el), Math.cos(az) * Math.cos(el))
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

// --- The night sky (W3, Epic design「夜空」) ---------------------------------------

/** The moon's mean radius, km. */
const MOON_RADIUS_KM = 1737.4
const AU_KM = 149_597_870.7

/** The moon as seen from a place at an instant: apparent elevation (standard refraction), compass azimuth, and what lights it. */
export type MoonSight = {
  elevationDeg: number
  /** Clockwise from north. */
  azimuthDeg: number
  /** Visual magnitude (astronomy-engine: the phase law, the opposition surge and the distance counted in). */
  magnitude: number
  /** The illuminated fraction of the disc, 0…1. */
  illuminated: number
  /** Its angular diameter, degrees. */
  diameterDeg: number
}

/** The moon from sea level at a place and instant. */
export function moonAt(latitudeDeg: number, longitudeDeg: number, utcMs: number): MoonSight {
  const observer = new Observer(latitudeDeg, longitudeDeg, 0)
  const time = MakeTime(new Date(utcMs))
  const eq = Equator(Body.Moon, time, observer, true, true)
  const hor = Horizon(time, observer, eq.ra, eq.dec, 'normal')
  const light = Illumination(Body.Moon, time)
  const km = eq.dist * AU_KM
  return { elevationDeg: hor.altitude, azimuthDeg: hor.azimuth, magnitude: light.mag, illuminated: light.phase_fraction, diameterDeg: (2 * Math.asin(MOON_RADIUS_KM / km) * 180) / Math.PI }
}

/** A physical sky's moon in the document's terms (azimuth as sun_azimuth_deg, north the direction of its pole); null under manual angles, which have no date to draw it at. */
export function physicalMoon(sky: PhysicalSky): (MoonSight & { compassDeg: number; north: Vector3 }) | null {
  if (sky.sun_mode === 'manual') return null
  const { utcMs } = skyInstant(sky)
  const moon = moonAt(sky.latitude_deg, sky.longitude_deg, utcMs)
  const pole = RotationAxis(Body.Moon, MakeTime(new Date(utcMs))).north
  const north = new Vector3(pole.x, pole.y, pole.z).applyMatrix3(equatorToScene(sky)).normalize()
  return { ...moon, compassDeg: moon.azimuthDeg, azimuthDeg: documentAzimuth(moon.azimuthDeg, sky.north_azimuth_deg), north }
}

/** A rotation matrix of astronomy-engine as three's, columns the images of the basis vectors. */
function matrixOf(r: RotationMatrix): Matrix3 {
  const col = (x: number, y: number, z: number) => RotateVector(r, new Vector(x, y, z, MakeTime(0)))
  const [a, b, c] = [col(1, 0, 0), col(0, 1, 0), col(0, 0, 1)]
  return new Matrix3().set(a.x, b.x, c.x, a.y, b.y, c.y, a.z, b.z, c.z)
}

/**
 * Horizontal (x north, y west, z up) to the document (Y up), north at
 * north_azimuth_deg: a compass azimuth A goes to the document azimuth
 * N − A (documentAzimuth), counter-clockwise from +Z.
 */
function horizonToScene(northAzimuthDeg: number): Matrix3 {
  const n = (northAzimuthDeg * Math.PI) / 180
  const [s, c] = [Math.sin(n), Math.cos(n)]
  return new Matrix3().set(s, c, 0, 0, 0, 1, c, -s, 0)
}

/** J2000 equatorial (EQJ) to the document at the sky's place and time: precession, nutation, sidereal time and latitude, then north. No refraction. */
export function equatorToScene(sky: PhysicalSky): Matrix3 {
  const { utcMs } = skyInstant(sky)
  return horizonToScene(sky.north_azimuth_deg).multiply(matrixOf(Rotation_EQJ_HOR(MakeTime(new Date(utcMs)), new Observer(sky.latitude_deg, sky.longitude_deg, 0))))
}

/** Galactic (l, b) to the document at the sky's place and time. */
export function galacticToScene(sky: PhysicalSky): Matrix3 {
  const { utcMs } = skyInstant(sky)
  const toHor = CombineRotation(Rotation_GAL_EQJ(), Rotation_EQJ_HOR(MakeTime(new Date(utcMs)), new Observer(sky.latitude_deg, sky.longitude_deg, 0)))
  return horizonToScene(sky.north_azimuth_deg).multiply(matrixOf(toHor))
}

/** The moon's phase by the moon's ecliptic longitude east of the sun, in eighths centred on new, first quarter, full and last quarter. */
export type MoonPhaseName = 'new' | 'waxing_crescent' | 'first_quarter' | 'waxing_gibbous' | 'full' | 'waning_gibbous' | 'last_quarter' | 'waning_crescent'

const phaseNames: MoonPhaseName[] = ['new', 'waxing_crescent', 'first_quarter', 'waxing_gibbous', 'full', 'waning_gibbous', 'last_quarter', 'waning_crescent']

/** The phase at an instant: 0° new, 90° first quarter, 180° full, 270° last quarter, each ±22.5°. */
export function moonPhaseName(utcMs: number): MoonPhaseName {
  return phaseNames[Math.floor(((MoonPhase(new Date(utcMs)) + 22.5) % 360) / 45)]
}

/** The moon over a sky's local day: what the environment tab shows, and what the server tells the assistant (backend moon.go). Times HH:MM, dates YYYY-MM-DD, local. */
export type MoonTimes = {
  phase: MoonPhaseName
  /** At the sky's local time. */
  illuminated: number
  moonrise?: string
  moonset?: string
  previousFullMoon: string
  nextFullMoon: string
  previousNewMoon: string
  nextNewMoon: string
}

const SYNODIC_DAYS = 29.53

/** The day of a sky's date and place: the phase and illuminated fraction at its time, moonrise and moonset (upper limb, refraction), and the full and new moons either side of the day (the next one may be that day). */
export function moonTimes(sky: PhysicalSky): MoonTimes {
  const offset = skyOffset(sky)
  const start = skyInstant(sky, '00:00').utcMs
  const next = new Date(wallMs(sky.date, '00:00') + DAY_MS).toISOString().slice(0, 10)
  const end = skyInstant({ ...sky, date: next }, '00:00').utcMs
  const observer = new Observer(sky.latitude_deg, sky.longitude_deg, 0)
  const local = (utcMs: number) => new Date(Math.round((utcMs + offset.at(utcMs)) / MINUTE_MS) * MINUTE_MS).toISOString()
  const crossing = (direction: 1 | -1): string | undefined => {
    const t = SearchRiseSet(Body.Moon, observer, direction, MakeTime(new Date(start)), (end - start) / DAY_MS)
    return t && t.date.getTime() < end ? local(t.date.getTime()).slice(11, 16) : undefined
  }
  // Either side of the day's start: the last before it and the first at or after it.
  const around = (longitude: number): [string, string] => {
    let t = SearchMoonPhase(longitude, new Date(start - (SYNODIC_DAYS + 2) * DAY_MS), 40)!
    let before = t
    while (t.date.getTime() < start) {
      before = t
      t = SearchMoonPhase(longitude, new Date(t.date.getTime() + DAY_MS), 40)!
    }
    return [local(before.date.getTime()).slice(0, 10), local(t.date.getTime()).slice(0, 10)]
  }
  const { utcMs } = skyInstant(sky)
  const [previousFullMoon, nextFullMoon] = around(180)
  const [previousNewMoon, nextNewMoon] = around(0)
  const out: MoonTimes = { phase: moonPhaseName(utcMs), illuminated: Illumination(Body.Moon, MakeTime(new Date(utcMs))).phase_fraction, previousFullMoon, nextFullMoon, previousNewMoon, nextNewMoon }
  const rise = crossing(1)
  const set = crossing(-1)
  if (rise) out.moonrise = rise
  if (set) out.moonset = set
  return out
}
