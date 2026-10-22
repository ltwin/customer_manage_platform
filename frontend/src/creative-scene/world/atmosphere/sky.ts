import { Vector3 } from 'three'
import * as tsl from 'three/tsl'
import type { TSLNode } from '../../materials/patterns.ts'
import { CAMERA_ABOVE_GROUND } from './constants.ts'
import type { AtmosphereLuts } from './luts.ts'

// The physical sky as drawn (Epic design「物理单位与曝光」): the sky-view
// LUT, stored × s_env, times r = k / s_env; and the sun disc, whose
// radiance is computed here, times k. The sky light is made from the same
// node without the disc and with r = 1, in environment-relative units. The
// material output clamp (render/gpu.ts) keeps what a camera overexposes
// within half float.

type Op = (...args: TSLNode[]) => TSLNode
const { clamp, dot, float, max, normalize, select, sqrt, uniform, vec3, vec4 } = tsl as unknown as Record<string, Op>

type Uniform<T> = TSLNode & { value: T }

export type PhysicalSkyNode = {
  /** The sky's colour toward the direction it was made for. */
  color: TSLNode
  /** The sun's direction, document space (Y up). */
  sun: Uniform<Vector3>
  /** r = k / s_env for the background; 1 for the sky light. */
  relative: Uniform<number>
  /** k, the disc's pre-exposure. */
  exposure: Uniform<number>
  /** The disc's mean radiance above the atmosphere per channel, cd/m²: the sun's illuminance there over the disc's solid angle. */
  sunRadiance: Uniform<Vector3>
  /** The cosine of the disc's angular radius. */
  sunCosRadius: Uniform<number>
  /** 1 draws the disc, 0 leaves it out. */
  disc: Uniform<number>
}

/** The disc's solid angle for an angular diameter, sr. */
export const discSolidAngle = (diameterDeg: number) => 2 * Math.PI * (1 - Math.cos(((diameterDeg / 2) * Math.PI) / 180))

/** The physical sky toward a unit direction d in document space. */
export function physicalSkyColor(luts: AtmosphereLuts, d: TSLNode): PhysicalSkyNode {
  const sun = uniform(new Vector3(0, 1, 0)) as Uniform<Vector3>
  const relative = uniform(1) as Uniform<number>
  const exposure = uniform(1) as Uniform<number>
  const sunRadiance = uniform(new Vector3()) as Uniform<Vector3>
  const sunCosRadius = uniform(Math.cos((0.2725 * Math.PI) / 180)) as Uniform<number>
  const disc = uniform(1) as Uniform<number>
  const s = normalize(sun).toVar()
  const sky = luts.sampleSkyView(d, s).mul(relative)
  const cosAngle = dot(d, s)
  // Toward the ground the disc is behind the planet: below the horizon's dip, cos β = √(r² − ground²) / r (as luts.ts, without the cancellation).
  const r = luts.viewRadius
  const towardGround = d.y.lessThan(sqrt(luts.groundRadius.mul(2).add(CAMERA_ABOVE_GROUND).mul(CAMERA_ABOVE_GROUND)).div(r).negate())
  const inDisc = cosAngle.greaterThan(sunCosRadius).and(towardGround.not()).and(disc.greaterThan(0.5))
  // Limb darkening I(μ) = 1 − 0.6 (1 − μ), its mean over the disc 1.
  const rho = sqrt(max(float(1).sub(cosAngle.mul(cosAngle)), 0)).div(sqrt(float(1).sub(sunCosRadius.mul(sunCosRadius))))
  const limb = float(0.4).add(sqrt(max(float(1).sub(rho.mul(rho)), 0)).mul(0.6)).div(0.8)
  const sunL = luts.sampleTransmittance(r, clamp(s.y, -1, 1)).mul(sunRadiance).mul(limb).mul(exposure)
  const color = vec4(sky.add(select(inDisc, sunL, vec3(0))), 1)
  return { color, sun, relative, exposure, sunRadiance, sunCosRadius, disc }
}
