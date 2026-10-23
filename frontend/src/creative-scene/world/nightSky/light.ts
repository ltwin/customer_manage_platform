import { blackBody, luminance, SUN_LUX, sunTint, type Vec3 } from '../atmosphere/constants.ts'

// The night sky's light (W3 design「亮度与颜色的单一来源」): every body's
// illuminance above the atmosphere in lux, per channel, from its visual
// magnitude against the sun's, so the moon and the stars sit on the same
// scale as the sun the atmosphere already lights the scene with.

/** The sun's visual magnitude: SUN_LUX is its illuminance above the atmosphere. */
const SUN_MAGNITUDE = -26.74

/** The illuminance above the atmosphere of a body of visual magnitude m, lux: about 0.3 for the full moon, 1e-5 for Sirius. */
export const magnitudeLux = (m: number) => SUN_LUX * 10 ** (-0.4 * (m - SUN_MAGNITUDE))

/** A surface brightness in magnitudes per square arcsecond as luminance, cd/m². */
export const surfaceLuminance = (magPerArcsec2: number) => 10.8e4 * 10 ** (-0.4 * magPerArcsec2)

/** A star's colour temperature from its B−V (Ballesteros 2012). */
export const bvKelvin = (bv: number) => 4600 * (1 / (0.92 * bv + 1.7) + 1 / (0.92 * bv + 0.62))

/** The sun's B−V: a star of the sun's colour is drawn white, as the sun's own light is (sunTint(6500) is (1, 1, 1)). */
const SUN_BV = 0.65
const sunColour = blackBody(bvKelvin(SUN_BV))

/** The colour of light of B−V bv beside the sun's, luminance 1. */
export function bvTint(bv: number): Vec3 {
  const c = blackBody(bvKelvin(bv)).map((v, i) => v / sunColour[i]) as Vec3
  const l = luminance(c)
  return c.map((v) => v / l) as Vec3
}

/** The moon's B−V: sunlight off its surface is redder than the sun's. */
const MOON_BV = 0.92

/** The moon's illuminance above the atmosphere per channel, lux: its magnitude, its colour, its strength and colour temperature (data: 夜空). */
export function moonAbove(magnitude: number, strength: number, kelvin: number): Vec3 {
  const lux = magnitudeLux(magnitude) * strength
  const tint = bvTint(MOON_BV)
  const t = sunTint(kelvin)
  const c = tint.map((v, i) => v * t[i]) as Vec3
  const l = luminance(c)
  return c.map((v) => (lux * v) / l) as Vec3
}

/** The stars' strength and colour temperature as one factor per channel: stars, the Milky Way and the airglow. */
export function starsScale(strength: number, kelvin: number): Vec3 {
  return sunTint(kelvin).map((t) => t * strength) as Vec3
}

/**
 * The airglow: the clear moonless sky's own light, at the zenith 21.8
 * magnitudes per square arcsecond (about 2 × 10⁻⁴ cd/m²) of a slightly warm
 * grey, brighter toward the horizon by the van Rhijn factor of a thin layer
 * 90 km up (about 6 at the horizon) before the atmosphere's extinction.
 */
export const AIRGLOW_ZENITH = surfaceLuminance(21.8)
export const AIRGLOW_TINT: Vec3 = bvTint(0.8)
const AIRGLOW_RATIO = 6371 / (6371 + 90)

/** The van Rhijn factor at the zenith cosine mu (≥ 0). */
export const vanRhijn = (mu: number) => 1 / Math.sqrt(1 - AIRGLOW_RATIO ** 2 * (1 - mu * mu))

/** The Milky Way's brightest (toward the galactic centre), cd/m²: the procedural map is 1 there (world/nightSky/milkyWay.ts). */
export const MILKY_WAY_PEAK = surfaceLuminance(20.5)

/**
 * Level ground's illuminance from the airglow, lux (before the stars'
 * strength): ∫ L(μ) μ dΩ over the upper hemisphere, the extinction taken as
 * a flat 0.8 (the airglow is lowest where the path is longest).
 */
export const AIRGLOW_LUX = (() => {
  let e = 0
  const n = 256
  for (let i = 0; i < n; i++) {
    const mu = (i + 0.5) / n
    e += AIRGLOW_ZENITH * vanRhijn(mu) * mu * (2 * Math.PI) * (1 / n)
  }
  return e * 0.8
})()

/** Level ground's illuminance from a star catalogue's stars, lux (before the stars' strength): half the sky's stars above the horizon, through a flat 0.8. */
export function starlightLux(magnitudes: ArrayLike<number>): number {
  let e = 0
  // Above the horizon a star lights level ground by its zenith cosine, on average 1/2 over the hemisphere.
  for (let i = 0; i < magnitudes.length; i++) e += magnitudeLux(magnitudes[i])
  return e * 0.5 * 0.5 * 0.8
}
