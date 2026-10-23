import { DataTexture, Matrix3, SRGBColorSpace, Vector3, type Texture } from 'three'
import * as tsl from 'three/tsl'
import type { TSLNode } from '../../materials/patterns.ts'
import { CAMERA_ABOVE_GROUND } from './constants.ts'
import type { AtmosphereLuts } from './luts.ts'

// The physical sky as drawn (Epic design「物理单位与曝光」「夜空」): the
// sky-view LUTs (the sun's and, by night, the moon's), stored × s_env, times
// relative; the sun and moon discs, whose radiance is computed here, times
// exposure; the Milky Way and the airglow in cd/m², times night. The background passes relative = k / s_env
// and exposure = k (world/exposure.ts preExposure); the sky light is made
// from the same node without the disc and with relative = 1, in
// environment-relative units. The material output clamp (render/gpu.ts)
// keeps what a camera overexposes within half float.

type Op = (...args: TSLNode[]) => TSLNode
const { asin, atan, clamp, cross, dot, float, length, max, normalize, select, sqrt, texture, uniform, vec2, vec3, vec4 } = tsl as unknown as Record<string, Op>
const Fn = tsl.Fn as unknown as (fn: () => TSLNode) => () => TSLNode

type Uniform<T> = TSLNode & { value: T }

/** What a physical sky draws by night (W3, Epic design「夜空」): set by PhysicalLighting, zero where nothing is drawn. */
export type NightSkyNode = {
  /** The moon's direction, document space. */
  moon: Uniform<Vector3>
  /** The moon's pole, document space: its maps' north (world/astro.ts physicalMoon). */
  moonNorth: Uniform<Vector3>
  /** The cosine of the moon disc's angular radius. */
  moonCosRadius: Uniform<number>
  /** The disc's radiance above the atmosphere per channel where Lommel-Seeliger's shading and the map are 1, cd/m² (0 when the moon is not drawn). */
  moonRadiance: Uniform<Vector3>
  /** 1 adds the moon's sky view (W3: by night, the moon on), 0 leaves it out. */
  moonSky: Uniform<number>
  /** The moon's albedo map (world/nightSky/assets.ts), mean 0.5; a grey stands in until it arrives. */
  moonMap: TSLNode & { value: Texture }
  /** Document to galactic coordinates (the transpose of world/astro.ts galacticToScene). */
  galactic: Uniform<Matrix3>
  /** The baked Milky Way (world/nightSky/milkyWay.ts); black until baked. */
  milkyWayMap: TSLNode & { value: Texture }
  /** The Milky Way's brightest per channel, cd/m², its switch and the stars' strength and tint in it (0 when not drawn). */
  milkyWay: Uniform<Vector3>
  /** The airglow at the zenith per channel, cd/m², likewise. */
  airglow: Uniform<Vector3>
}

export type PhysicalSkyNode = {
  /** The sky's colour toward the direction it was made for. */
  color: TSLNode
  /** The sun's direction, document space (Y up). */
  sun: Uniform<Vector3>
  /** The disc's mean radiance above the atmosphere per channel, cd/m²: the sun's illuminance there over the disc's solid angle. */
  sunRadiance: Uniform<Vector3>
  /** The cosine of the disc's angular radius. */
  sunCosRadius: Uniform<number>
  /** 1 draws the disc, 0 leaves it out. */
  disc: Uniform<number>
  night: NightSkyNode
}

/**
 * normalize that stays finite at zero: Metal's fast math may compute both
 * sides of a select, so one NaN off the moon's disc (a pole along the moon,
 * say) would spread over the whole sky and its light.
 */
const safeNormalize = (v: TSLNode) => v.div(max(length(v), 1e-6))

/** The disc's solid angle for an angular diameter, sr. */
export const discSolidAngle = (diameterDeg: number) => 2 * Math.PI * (1 - Math.cos(((diameterDeg / 2) * Math.PI) / 180))

/** The airglow layer's radius over the planet's: the van Rhijn factor (world/nightSky/light.ts). */
const AIRGLOW_RATIO = 6371 / (6371 + 90)

const grey = new DataTexture(new Uint8Array([128, 128, 128, 255]), 1, 1)
grey.colorSpace = SRGBColorSpace
grey.needsUpdate = true
const black = new DataTexture(new Uint8Array([0, 0, 0, 255]), 1, 1)
black.needsUpdate = true

/**
 * The physical sky toward a unit direction d in document space: the sky
 * view (and the moon's) × relative, the sun disc and the moon disc ×
 * exposure, the Milky Way and the airglow × night. The background passes
 * relative = k / s_env, exposure = night = k; the sky light's source passes
 * relative = 1, exposure 0 (no discs: the sun and moon lights count them),
 * night = s_env, so it stays in environment-relative units.
 */
export function physicalSkyColor(luts: AtmosphereLuts, d: TSLNode, relative: TSLNode, exposure: TSLNode, night: TSLNode): PhysicalSkyNode {
  const sun = uniform(new Vector3(0, 1, 0)) as Uniform<Vector3>
  const sunRadiance = uniform(new Vector3()) as Uniform<Vector3>
  const sunCosRadius = uniform(Math.cos((0.2725 * Math.PI) / 180)) as Uniform<number>
  const disc = uniform(1) as Uniform<number>
  const n: NightSkyNode = {
    moon: uniform(new Vector3(0, -1, 0)) as Uniform<Vector3>,
    moonNorth: uniform(new Vector3(0, 0, 1)) as Uniform<Vector3>,
    moonCosRadius: uniform(Math.cos((0.26 * Math.PI) / 180)) as Uniform<number>,
    moonRadiance: uniform(new Vector3()) as Uniform<Vector3>,
    moonSky: uniform(0) as Uniform<number>,
    moonMap: texture(grey) as TSLNode & { value: Texture },
    galactic: uniform(new Matrix3()) as Uniform<Matrix3>,
    milkyWayMap: texture(black) as TSLNode & { value: Texture },
    milkyWay: uniform(new Vector3()) as Uniform<Vector3>,
    airglow: uniform(new Vector3()) as Uniform<Vector3>,
  }
  // Inside a TSL function, as the other skies are (render/sky.ts): its variables belong to the fragment's body.
  const color = Fn(() => {
    const s = normalize(sun).toVar()
    const m = normalize(n.moon).toVar()
    const sky = luts.sampleSkyView(d, s).add(luts.sampleMoonSkyView(d, m).mul(n.moonSky)).mul(relative)
    const cosAngle = dot(d, s)
    // Toward the ground the discs are behind the planet: below the horizon's dip, cos β = √(r² − ground²) / r (as luts.ts, without the cancellation).
    const r = luts.viewRadius
    const towardGround = d.y.lessThan(sqrt(luts.groundRadius.mul(2).add(CAMERA_ABOVE_GROUND).mul(CAMERA_ABOVE_GROUND)).div(r).negate()).toVar()
    const inDisc = cosAngle.greaterThan(sunCosRadius).and(towardGround.not()).and(disc.greaterThan(0.5))
    // Limb darkening I(μ) = 1 − 0.6 (1 − μ), its mean over the disc 1.
    const rho = sqrt(max(float(1).sub(cosAngle.mul(cosAngle)), 0)).div(sqrt(float(1).sub(sunCosRadius.mul(sunCosRadius))))
    const limb = float(0.4).add(sqrt(max(float(1).sub(rho.mul(rho)), 0)).mul(0.6)).div(0.8)
    const sunL = luts.sampleTransmittance(r, clamp(s.y, -1, 1)).mul(sunRadiance).mul(limb).mul(exposure)

    // The moon: a sphere lit by the sun (Lommel-Seeliger), its albedo map turned by its pole; the phase comes out of the geometry.
    const sinMoon = sqrt(max(float(1).sub(n.moonCosRadius.mul(n.moonCosRadius)), 1e-12))
    const offset = d.sub(m.mul(dot(d, m))).div(sinMoon).toVar()
    const o2 = dot(offset, offset)
    const inMoon = dot(d, m).greaterThan(n.moonCosRadius).and(towardGround.not())
    const toViewer = m.negate()
    const normal = safeNormalize(offset.add(toViewer.mul(sqrt(max(float(1).sub(o2), 0))))).toVar()
    const mu0 = max(dot(normal, s), 0)
    const mu = max(dot(normal, toViewer), 1e-4)
    const pole = safeNormalize(n.moonNorth.sub(m.mul(dot(n.moonNorth, m)))).toVar()
    const front = safeNormalize(toViewer.sub(pole.mul(dot(toViewer, pole)))).toVar()
    const east = cross(pole, front)
    const lon = atan(dot(normal, east), dot(normal, front))
    const lat = asin(clamp(dot(normal, pole), -1, 1))
    const albedo = n.moonMap.sample(vec2(lon.div(2 * Math.PI).add(0.5), lat.div(Math.PI).add(0.5))).level(0).r.mul(2)
    const moonL = luts.sampleTransmittance(r, clamp(m.y, -1, 1)).mul(n.moonRadiance).mul(albedo).mul(mu0.div(mu0.add(mu))).mul(exposure)

    // The Milky Way and the airglow, through the atmosphere above the horizon.
    const g = n.galactic.mul(d)
    const above = luts.sampleTransmittance(r, clamp(d.y, -1, 1))
    const glow = float(1).div(sqrt(max(float(1).sub(float(AIRGLOW_RATIO * AIRGLOW_RATIO).mul(float(1).sub(d.y.mul(d.y)))), 1e-4)))
    const starry = sampleMilkyWayNode(n.milkyWayMap, g).mul(n.milkyWay).add(n.airglow.mul(glow)).mul(above).mul(night)

    const discs = select(inDisc, sunL, select(inMoon, moonL, vec3(0)))
    return vec4(sky.add(discs).add(select(towardGround, vec3(0), starry)), 1)
  })()
  return { color, sun, sunRadiance, sunCosRadius, disc, night: n }
}

/** world/nightSky/milkyWay.ts sampleMilkyWay on a texture node whose texture is swapped in when baked. */
function sampleMilkyWayNode(map: TSLNode, g: TSLNode): TSLNode {
  const l = atan(g.y, g.x)
  const b = asin(clamp(g.z, -1, 1))
  return map.sample(vec2(l.div(2 * Math.PI).add(0.5), b.div(Math.PI).add(0.5))).level(0).rgb
}
