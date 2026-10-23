import type { Node } from 'three/webgpu'
import { dot, mix, vec3, vec4 } from 'three/tsl'
import { luminance, sunTint, type Vec3 } from './atmosphere/constants.ts'
import { environmentEV } from './exposure.ts'

// The night look (Epic design「夜空」「夜色」, revision v5): as night falls the
// picture turns blue and paler, as eyes see at night and as day-for-night is
// graded; without it ambient metering makes a moonlit night a darker
// day. A colour change after the exposure and before tone mapping that keeps
// each pixel's luminance. It follows the scene's light, not the camera, so
// every camera and both exposure modes see the same; night_look 0 keeps the
// physical colours.

/**
 * The colour the picture leans to, luminance 1, and how much of its own
 * colour goes at full strength: a starting point that leaves a warm moonlit
 * yard plainly bluish (owner to settle from the samples, Epic open question 10).
 */
export const NIGHT_TINT: Vec3 = sunTint(20000)
export const NIGHT_DESATURATION = 0.75

/** The metered EV100s between which the look comes in: none from the golden hour up, full from a full moon down. */
const FROM_EV = 4
const FULL_EV = -4

/** The look's strength under a level-ground illuminance E (lux) at a scene's night_look: smooth between the two EVs. */
export function nightLookWeight(lux: number, nightLook: number): number {
  const t = Math.min(1, Math.max(0, (FROM_EV - environmentEV(lux)) / (FROM_EV - FULL_EV)))
  return nightLook * t * t * (3 - 2 * t)
}

/** One colour under the look at a weight, as nightLookNode draws it (for tests). */
export function nightLookColor(c: Readonly<Vec3>, weight: number): Vec3 {
  const y = luminance(c)
  const s = weight * NIGHT_DESATURATION
  return c.map((v, i) => v + (y * NIGHT_TINT[i] - v) * s) as Vec3
}

/** The look on a linear picture; weight is a uniform, so night falling recompiles nothing and 0 leaves the picture as it was. */
export function nightLookNode(picture: Node<'vec4'>, weight: Node<'float'>): Node<'vec4'> {
  const y = dot(picture.rgb, vec3(0.2126, 0.7152, 0.0722))
  return vec4(mix(picture.rgb, vec3(...NIGHT_TINT).mul(y), weight.mul(NIGHT_DESATURATION)), picture.a)
}
