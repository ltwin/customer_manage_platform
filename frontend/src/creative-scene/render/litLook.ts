import { Color } from 'three'
import { luminance, skyIrradiance, transmittance, type Vec3 } from './atmosphere.ts'
import type { SceneRender } from '../api.ts'
import type { Engine } from './engines.ts'

// The lit look's constants (design「最终渲染的光照」). They set the balance
// within one scene: sun, sky light and ambient fill. How bright a scene is
// overall is its own exposure compensation (render.exposure_ev). Calibrated
// on the reference scenes (design 未决事项 1, the e2e「calibration」check):
// a clear noon at EV 0, dusk at +2, a room lit by two 40 cd lamps at 0.

/** The sun's brightness high in the sky (luminance in three.js units, on a surface facing it). */
export const SUN_MAX = 2.2
/**
 * The sky's light on level ground against the sun's, on the reference clear
 * noon (sun 55° up, turbidity 3): about a quarter, as in a clear-day photograph.
 */
export const SKY_SHARE = 0.25
/** The sky map lights already; the scene's ambient only fills shadows a little. */
export const AMBIENT_FILL = 0.5
/**
 * The fill under the real-time GI (R2 design「合成」), whose bounced light
 * the workbench's fill stood in for. Calibrated on the room and noon
 * reference scenes, their average brightness within 10% of the workbench's
 * (R2 tasks 1.2: room +5.8%, noon −3.0%; anywhere from 0.25 to 0.5 stays
 * within, this is the most even). The box room, lit by a spot and almost no
 * ambient, is brighter by its bounce, which no fill takes back.
 */
export const AMBIENT_FILL_GI = 0.35

/** The ambient fill of the lit look for a renderer. */
export const ambientFill = (engine: Engine) => (engine === 'realtime_gi' ? AMBIENT_FILL_GI : AMBIENT_FILL)

const reference = { elevationDeg: 55, turbidity: 3 }
const toY = (deg: number) => Math.sin((deg * Math.PI) / 180)
const times = (a: Vec3, b: Vec3): Vec3 => [a[0] * b[0], a[1] * b[1], a[2] * b[2]]
const scaled = (a: Vec3, k: number): Vec3 => [a[0] * k, a[1] * k, a[2] * k]

function smoothstep(a: number, b: number, x: number): number {
  const t = Math.min(1, Math.max(0, (x - a) / (b - a)))
  return t * t * (3 - 2 * t)
}

/** The sun's light on a surface facing it, before white balance: what the atmosphere lets through, gone below the horizon. */
function rawSun(elevationDeg: number, turbidity: number): Vec3 {
  const fade = smoothstep(-1, 1, elevationDeg)
  return scaled(transmittance(toY(elevationDeg), turbidity), (SUN_MAX * fade) / luminance(transmittance(1, turbidity)))
}

/**
 * The reference noon fixes two things: how much the sky shader's output is
 * scaled (its light SKY_SHARE of the sun's on level ground) and a daylight
 * white balance, as a camera set to daylight would: the sky's light alone is
 * deep blue, so without it every sunny scene came out cold. Lamps are not
 * balanced; they keep the colour they are given.
 */
const { skyScale, whiteBalance } = (() => {
  const y = toY(reference.elevationDeg)
  const sunOnGround = scaled(rawSun(reference.elevationDeg, reference.turbidity), y)
  const sky = skyIrradiance([0, y, Math.sqrt(1 - y * y)], reference.turbidity)
  const scale = (SKY_SHARE * luminance(sunOnGround)) / luminance(sky)
  const total: Vec3 = [0, 1, 2].map((i) => sunOnGround[i] + sky[i] * scale) as Vec3
  const grey = luminance(total)
  return { skyScale: scale, whiteBalance: total.map((v) => grey / v) as Vec3 }
})()

/** What the sky shader's output is multiplied by, per channel: background and sky light alike. */
export const SKY_GAIN: Vec3 = scaled(whiteBalance, skyScale)

/** The sun at an elevation: weaker and more orange as it sets, gone below the horizon. */
export function sunLight(elevationDeg: number, turbidity = reference.turbidity): { intensity: number; color: Color } {
  const light = times(rawSun(elevationDeg, turbidity), whiteBalance)
  const peak = Math.max(...light)
  return peak > 0 ? { intensity: peak, color: new Color(light[0] / peak, light[1] / peak, light[2] / peak) } : { intensity: 0, color: new Color(1, 1, 1) }
}

/** toneMappingExposure for the lit look. */
export const exposureOf = (render: Pick<SceneRender, 'exposure_ev'>) => 2 ** render.exposure_ev

/**
 * What a light added from the menu starts at: a desk lamp's point light, a
 * stage spot, a fill directional light (candela for point and spot).
 * Calibrated with the constants above.
 */
export const NEW_LIGHT_INTENSITY = { directional: 1, point: 40, spot: 150 } as const
