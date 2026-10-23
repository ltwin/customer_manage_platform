import { Vector3 } from 'three'
import { physicalMoon, sunTimes, sunVector } from '../astro.ts'
import type { CpuAtmosphere, Daylight } from '../atmosphere/cpu.ts'
import type { Vec3 } from '../atmosphere/constants.ts'
import { discSolidAngle } from '../atmosphere/sky.ts'
import type { PhysicalSky } from '../physicalSky.ts'
import { AIRGLOW_GAIN, AIRGLOW_LUX, AIRGLOW_TINT, AIRGLOW_ZENITH, MILKY_WAY_PEAK, moonAbove, starsScale } from './light.ts'
import { milkyWayMean } from './milkyWay.ts'

// What a physical sky's night puts in the picture (W3 design「夜空何时画」
// 「光照」「星空常数项」), worked out on the CPU for PhysicalLighting: pure,
// from the scene alone, so the viewport and an export agree and the units
// can be tested without a GPU.

/** Level ground's illuminance from the catalogue's stars, lux: light.ts starlightLux over stars.bin, worked out once (a test holds it to the file). */
export const STARLIGHT_LUX = 4.87e-5

/** Level ground's illuminance from the Milky Way, lux (before the stars' strength): its mean over the sphere as the hemisphere's, × π, through a flat 0.8 as the airglow's. */
export const MILKY_WAY_LUX = milkyWayMean() * MILKY_WAY_PEAK * Math.PI * 0.8

/**
 * Lommel-Seeliger's μ₀ / (μ₀ + μ) averaged over the moon's disc at a phase
 * angle (its cosine; 1 is full): what the disc's shading (world/atmosphere/sky.ts)
 * integrates to over its solid angle, so the disc can be scaled to the
 * moon's illuminance. 0.5 at full, 0 at new.
 */
export function lommelSeeligerMean(cosPhase: number): number {
  const c = Math.min(1, Math.max(-1, cosPhase))
  const s = Math.sqrt(1 - c * c)
  const n = 64
  let sum = 0
  let count = 0
  for (let j = 0; j < n; j++) {
    for (let i = 0; i < n; i++) {
      const x = ((i + 0.5) / n) * 2 - 1
      const y = ((j + 0.5) / n) * 2 - 1
      const r2 = x * x + y * y
      if (r2 >= 1) continue
      // Seen from +z, the sun at the phase angle from the viewer in the x–z plane.
      const mu = Math.sqrt(1 - r2)
      const mu0 = Math.max(0, x * s + mu * c)
      sum += mu0 / (mu0 + mu)
      count++
    }
  }
  return sum / count
}

export type NightMoon = {
  /** Toward the moon, document space (refracted, as the sun). */
  direction: Vector3
  /** Its pole, document space. */
  north: Vector3
  elevationDeg: number
  diameterDeg: number
  /** Its illuminance above the atmosphere per channel, lux: magnitude, strength and colour temperature. */
  top: Vec3
  /** The disc's radiance per channel where the shading and the map are 1, cd/m²: top over the shading's integral. */
  radiance: Vec3
}

export type NightParts = {
  /** The moon when drawn: location mode, switched on, some of its disc above the horizon; null otherwise. */
  moon: NightMoon | null
  /** The stars' brightness per channel (strength and colour temperature), 0 when they are off. */
  stars: Vec3
  /** The Milky Way's brightest per channel, cd/m², 0 when off. */
  milkyWay: Vec3
  /** The airglow at the zenith per channel, cd/m², 0 when the stars are off. */
  airglow: Vec3
  /** Level ground's illuminance from the stars, the Milky Way and the airglow, lux: the constant term of E. */
  lux: number
}

const zero: Vec3 = [0, 0, 0]
const times = (a: Readonly<Vec3>, b: Readonly<Vec3>, k = 1): Vec3 => [a[0] * b[0] * k, a[1] * b[1] * k, a[2] * b[2] * k]

/** What a physical sky's night draws, with the sun toward sun (document space). Nothing under manual angles. */
export function nightOf(sky: PhysicalSky, sun: Vector3): NightParts {
  if (sky.sun_mode !== 'location') return { moon: null, stars: zero, milkyWay: zero, airglow: zero, lux: 0 }
  const night = sky.night
  const scale = starsScale(night.stars_strength, night.stars_temperature_k)
  const stars = night.stars ? scale : zero
  const lux = ((night.stars ? STARLIGHT_LUX + AIRGLOW_LUX * AIRGLOW_GAIN : 0) + (night.milky_way ? MILKY_WAY_LUX : 0)) * night.stars_strength
  let moon: NightMoon | null = null
  const where = night.moon ? physicalMoon(sky) : null
  if (where && where.elevationDeg > -1) {
    const direction = sunVector(where.elevationDeg, where.azimuthDeg)
    const top = moonAbove(where.magnitude, night.moon_strength, night.moon_temperature_k)
    const shading = lommelSeeligerMean(-sun.dot(direction)) * discSolidAngle(where.diameterDeg)
    const radiance = shading > 1e-9 ? (top.map((t) => t / shading) as Vec3) : zero
    moon = { direction, north: where.north, elevationDeg: where.elevationDeg, diameterDeg: where.diameterDeg, top, radiance }
  }
  return {
    moon,
    stars,
    milkyWay: night.milky_way ? times(scale, [1, 1, 1], MILKY_WAY_PEAK) : zero,
    airglow: night.stars ? times(scale, AIRGLOW_TINT, AIRGLOW_ZENITH * AIRGLOW_GAIN) : zero,
    lux,
  }
}

/** Whether the sun takes the shadowing light (design「投影」): above −6°, or with no moon up to take it. */
export function sunLeads(sunElevationDeg: number, moon: NightMoon | null): boolean {
  return moon === null || sunElevationDeg > -6 || moon.elevationDeg < 0
}

/** The sun's and the moon's light at the scene, and E: both with their skies, plus the night's constant term. */
export function sceneLight(cpu: CpuAtmosphere, sun: Vector3, sunTop: Readonly<Vec3>, night: NightParts): { sun: Daylight; moon: Daylight | null; horizontal: number } {
  const day = cpu.daylight(sun.y, sunTop)
  const moon = night.moon && cpu.daylight(night.moon.direction.y, night.moon.top)
  return { sun: day, moon, horizontal: day.horizontal + (moon?.horizontal ?? 0) + night.lux }
}

/**
 * How many stops ambient metering darkens for a low sun (owner
 * 2026-10-23). It meters level ground, which a low sun lights little while a
 * surface facing it gets nearly a noon's light: unchecked, what the setting
 * sun lights is brighter than at noon. So the picture is darkened by as many
 * stops as the most a surface facing the sun outshines level ground, between
 * the sun's height now and the day's noon, exceeds the noon's: what the sun
 * lights is never brighter than at noon, and the darkening only grows as the
 * sun goes down and stays once it has set (the night's darkening takes over
 * from it, ambientEV100).
 */
export function lowSunStops(cpu: CpuAtmosphere, elevationDeg: number, noonElevationDeg: number): number {
  const peak = cpu.sunFacingPeak()
  const noon = cpu.sunFacing(noonElevationDeg)
  if (noonElevationDeg <= peak.elevationDeg || elevationDeg >= noonElevationDeg) return 0
  const most = elevationDeg >= peak.elevationDeg ? cpu.sunFacing(elevationDeg) : peak.ratio
  return Math.max(0, Math.log2(most / noon))
}

/** The noon a lowSunStops holds a sky's sun to: its date's and place's highest sun, a manual sun's too, so taking the angles keeps the picture. */
export const noonElevation = (sky: PhysicalSky) => sunTimes(sky).noonElevationDeg

/** The light a daylight puts on a surface facing its body, as a light's intensity (lux at its brightest channel) and colour. */
export function lightOf(d: Daylight | null): { intensity: number; color: Vec3 } {
  const peak = d ? Math.max(...d.sun) : 0
  return { intensity: peak, color: peak > 0 ? (d!.sun.map((c) => c / peak) as Vec3) : [1, 1, 1] }
}
