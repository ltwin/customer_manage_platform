import { Color } from 'three'
import { luminance, skyIrradiance, transmittance, type Vec3 } from './atmosphere.ts'
import type { Scene, SceneRender } from '../api.ts'
import { exposureScale, viewerEV100, type ViewerExposure } from '../world/exposure.ts'
import { nightLookWeight } from '../world/nightLook.ts'
import type { Engine } from './engines.ts'

// The lit look's constants (design「最终渲染的光照」). They set the balance
// within one scene: sun, sky light and ambient fill. How bright a scene is
// overall is its own exposure compensation (render.exposure_ev). Calibrated
// on the reference scenes (design 未决事项 1, the e2e「calibration」check):
// a clear noon at EV 0, dusk at +2, a room lit by two 40 cd lamps at 0, and
// again under AgX (W1 scene-display-look tasks 5.1) to keep each scene's
// mean brightness within 10% of what it was under ACES Filmic: AgX keeps
// mid grey but lowers the lights and lifts the darks, so the sun and the
// sky went up and the ambient fill down.

/** The sun's brightness high in the sky (luminance in three.js units, on a surface facing it); 2.2 under ACES Filmic. */
export const SUN_MAX = 3.2
/**
 * The sky's light on level ground against the sun's, on the reference clear
 * noon (sun 55° up, turbidity 3). A quarter under ACES Filmic, as in a
 * clear-day photograph; a third under AgX, which took the dusk ground's light,
 * mostly the sky's, down more than the rest.
 */
export const SKY_SHARE = 0.35
/** The sky map lights already; the scene's ambient only fills shadows a little (0.5 under ACES Filmic, whose darks were darker). */
export const AMBIENT_FILL = 0.25
/**
 * The fill under the real-time GI (R2 design「合成」), whose bounced light
 * the workbench's fill stood in for. Calibrated on the room and noon
 * reference scenes, their average brightness within 10% of the workbench's
 * (R2 tasks 1.2: room +5.8%, noon −3.0%; anywhere from 0.25 to 0.5 stays
 * within). The box room, lit by a spot and almost no ambient, is brighter by
 * its bounce, which no fill takes back. 0.35 under ACES Filmic; under AgX it
 * follows the workbench's fill down, and the lit exports stay as much
 * brighter or darker than the workbench's as before (the e2e
 * SCENE_E2E_GI_FILL check: room +7.8%, noon −16.6%, box room +37.6%).
 */
export const AMBIENT_FILL_GI = 0.2

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

/** The default camera's exposure (the free view, render_view's viewpoints and presets): render's mode, compensation and EV100. */
export const defaultExposure = (render: SceneRender): ViewerExposure =>
  render.exposure_mode === 'manual' ? { mode: 'manual', ev100: render.exposure_ev100 } : { mode: 'ambient', compensation: render.exposure_compensation }

/**
 * A frame's toneMappingExposure, pre-exposure k and night look (W2 design「曝光」).
 * The night look follows E and the sky's night_look, whoever the viewer.
 * Under a physical sky the lit look is exposed by its viewer, a scene camera
 * or else the default camera, by ambient metering from E (lux, the level ground's
 * illuminance PhysicalLighting worked out: world/exposure.ts sceneLux) or by
 * hand (Epic revision v5); nothing is compensated after. Under another sky,
 * as before. Clay and wireframe are neither.
 */
export function frameExposure(scene: Pick<Scene, 'environment' | 'render'>, lit: boolean, camera: ViewerExposure | undefined, lux: number, lowSun = 0): { exposure: number; preExposure: number; nightLook: number } {
  if (!lit) return { exposure: 1, preExposure: 1, nightLook: 0 }
  const sky = scene.environment.sky
  if (sky.kind !== 'physical') return { exposure: exposureOf(scene.render), preExposure: 1, nightLook: 0 }
  return { exposure: 1, preExposure: exposureScale(viewerEV100(camera ?? defaultExposure(scene.render), lux, lowSun)), nightLook: nightLookWeight(lux, sky.night?.night_look ?? 1) }
}

/**
 * What a light added from the menu starts at: a desk lamp's point light, a
 * stage spot, a fill directional light (candela for point and spot).
 * Calibrated with the constants above.
 */
export const NEW_LIGHT_INTENSITY = { directional: 1, point: 40, spot: 150 } as const
