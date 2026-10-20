// The sky model of three's Sky shader (Preetham, three r186
// examples/jsm/objects/Sky.js) on the CPU, without clouds. The lit look takes
// the sun's colour and strength from the same atmosphere the sky is drawn
// with, so a low sun is as orange as the sky around it; and the sky's light is
// scaled against the sun from what it adds up to (design「最终渲染的光照」).

export type Vec3 = [number, number, number]

/** The Sky uniforms the lit look sets besides the sun: one look for every scene. */
export const RAYLEIGH = 1
export const MIE_COEFFICIENT = 0.005
export const MIE_DIRECTIONAL_G = 0.8

const totalRayleigh: Vec3 = [5.804542996261093e-6, 1.3562911419845635e-5, 3.0265902468824876e-5]
const mieConst: Vec3 = [1.8399918514433978e14, 2.7798023919660528e14, 4.0790479543861094e14]
const cutoffAngle = 1.6110731556870734
const steepness = 1.5
const EE = 1000
const rayleighZenithLength = 8.4e3
const mieZenithLength = 1.25e3

const clamp = (x: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, x))
const map = (v: Vec3, f: (x: number, i: number) => number): Vec3 => [f(v[0], 0), f(v[1], 1), f(v[2], 2)]
export const luminance = (c: Vec3) => 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2]

/** The sun's light above the atmosphere as the shader has it, fading as the sun nears the horizon. */
function sunIntensity(zenithAngleCos: number): number {
  return EE * Math.max(0, 1 - Math.exp(-(cutoffAngle - Math.acos(clamp(zenithAngleCos, -1, 1))) / steepness))
}

function coefficients(turbidity: number): { betaR: Vec3; betaM: Vec3 } {
  // The shader's sun fade is 1 for a unit sun direction, leaving rayleigh as set.
  // The shader's 0.2 * T * 10E-18, as one literal: oxlint takes 10E-18 for zero.
  const c = turbidity / 5e17
  return { betaR: map(totalRayleigh, (b) => b * RAYLEIGH), betaM: map(mieConst, (m) => 0.434 * c * m * MIE_COEFFICIENT) }
}

/** What reaches the ground along a direction (y up) through the whole atmosphere. */
export function transmittance(directionY: number, turbidity: number): Vec3 {
  const { betaR, betaM } = coefficients(turbidity)
  const zenith = Math.acos(Math.max(0, directionY))
  const inverse = 1 / (Math.cos(zenith) + 0.15 * Math.pow(93.885 - (zenith * 180) / Math.PI, -1.253))
  return map(betaR, (r, i) => Math.exp(-(r * rayleighZenithLength * inverse + betaM[i] * mieZenithLength * inverse)))
}

/** The sky's radiance in a direction (unit, y up) with the sun towards sun (unit), as drawn without the disc. */
export function skyRadiance(direction: Vec3, sun: Vec3, turbidity: number): Vec3 {
  const { betaR, betaM } = coefficients(turbidity)
  const fex = transmittance(direction[1], turbidity)
  const cosTheta = direction[0] * sun[0] + direction[1] * sun[1] + direction[2] * sun[2]
  const rPhase = (3 / (16 * Math.PI)) * (1 + (cosTheta * 0.5 + 0.5) ** 2)
  const g2 = MIE_DIRECTIONAL_G ** 2
  const mPhase = (1 / (4 * Math.PI)) * ((1 - g2) / Math.pow(1 - 2 * MIE_DIRECTIONAL_G * cosTheta + g2, 1.5))
  const sunE = sunIntensity(sun[1])
  const horizon = clamp((1 - sun[1]) ** 5, 0, 1)
  return map(fex, (f, i) => {
    const ratio = (betaR[i] * rPhase + betaM[i] * mPhase) / (betaR[i] + betaM[i])
    let lin = Math.pow(sunE * ratio * (1 - f), 1.5)
    lin *= 1 + (Math.sqrt(sunE * ratio * f) - 1) * horizon
    return (lin + 0.1 * f) * 0.04 + [0, 0.0003, 0.00075][i]
  })
}

/** Irradiance on level ground from the sky alone: the radiance over the upper hemisphere, cosine weighted. */
export function skyIrradiance(sun: Vec3, turbidity: number, rings = 24, segments = 48): Vec3 {
  const out: Vec3 = [0, 0, 0]
  for (let i = 0; i < rings; i++) {
    // Equal steps in sin²(elevation) weight each ring by its cosine-weighted solid angle.
    const y = Math.sqrt((i + 0.5) / rings)
    const r = Math.sqrt(1 - y * y)
    for (let j = 0; j < segments; j++) {
      const a = ((j + 0.5) / segments) * 2 * Math.PI
      const l = skyRadiance([r * Math.cos(a), y, r * Math.sin(a)], sun, turbidity)
      for (let k = 0; k < 3; k++) out[k] += l[k]
    }
  }
  // ∫L cosθ dω = π × the mean over cosine-distributed directions.
  return map(out, (v) => (v * Math.PI) / (rings * segments))
}
