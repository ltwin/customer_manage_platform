// Exposure under a physical sky (Epic design「物理单位与曝光」). Each viewer,
// a scene camera or the default camera, is exposed automatically or by hand
// (Epic revision v5, owner 2026-10-23). By hand a camera's ISO, shutter and
// aperture, or the default camera's render.exposure_ev100, give EV100;
// automatically the scene's light is metered and the night darkened by a few
// stops only. Lights are multiplied by k = 1 / (1.2 · 2^EV100) before the
// GPU sees them (pre-exposure). The sky and its light are stored in
// environment-relative units, × s_env.

import type { Color, Light, Scene } from 'three'
import { uniform } from 'three/tsl'

/** EV100 of a camera: log₂(N² / t) − log₂(S / 100). */
export const ev100 = (fStop: number, shutterS: number, iso: number) => Math.log2((fStop * fStop) / shutterS) - Math.log2(iso / 100)

/** The pre-exposure of an EV100 (Lagarde & de Rousiers 2014): sunny 16 brings 18 % grey to about 0.12. */
export const exposureScale = (ev: number) => 1 / (1.2 * 2 ** ev)

/** The EV a level-ground illuminance E (lux) meters at by incident light (C = 250), kept within −12…20. */
export const environmentEV = (lux: number) => Math.min(20, Math.max(-12, Math.log2((Math.max(lux, 1e-9) * 100) / 250)))

/**
 * s_env: what the environment's GPU data (sky view, sky light) is stored
 * multiplied by, so noon and night both sit about 0.1–1 in half floats. It
 * depends on the environment only, never on the camera.
 */
export const environmentScale = (lux: number) => exposureScale(environmentEV(lux))

/**
 * How many stops the automatic exposure leaves a metered EV under a proper
 * exposure: none by day, a few at night, so night looks like night yet
 * needs no exposure change (Epic design「夜间压暗曲线」). Linear between
 * the points, flat beyond them.
 */
const darkening: readonly (readonly [ev: number, stops: number])[] = [
  [-11, 3.5], // a moonless night
  [-4, 2.5], // a full moon
  [0, 1.5], // the end of civil twilight
  [6, 0.5], // the golden hour
  [12, 0], // overcast or brighter
]

/** The night darkening D at a metered EV100. */
export function nightDarkening(evm: number): number {
  if (evm <= darkening[0][0]) return darkening[0][1]
  for (let i = 1; i < darkening.length; i++) {
    const [e1, d1] = darkening[i]
    if (evm <= e1) {
      const [e0, d0] = darkening[i - 1]
      return d0 + ((d1 - d0) * (evm - e0)) / (e1 - e0)
    }
  }
  return 0
}

/** The automatic exposure's EV100 under a level-ground illuminance E (lux): metered as s_env is, darkened at night, less the compensation in stops. */
export function autoEV100(lux: number, compensation: number): number {
  const metered = environmentEV(lux)
  return metered + nightDarkening(metered) - compensation
}

/** A viewer's exposure: automatic with a compensation, or by hand at an EV100. */
export type ViewerExposure = { mode: 'auto'; compensation: number } | { mode: 'manual'; ev100: number }

/** A viewer's EV100 under a level-ground illuminance E (lux). */
export const viewerEV100 = (v: ViewerExposure, lux: number) => (v.mode === 'manual' ? v.ev100 : autoEV100(lux, v.compensation))

/** Shutter speeds in thirds of a stop, 1/8000 s…30 s, as cameras mark them. */
export const shutterThirds: readonly number[] = [
  1 / 8000, 1 / 6400, 1 / 5000, 1 / 4000, 1 / 3200, 1 / 2500, 1 / 2000, 1 / 1600, 1 / 1250, 1 / 1000, 1 / 800, 1 / 640, 1 / 500, 1 / 400, 1 / 320, 1 / 250, 1 / 200, 1 / 160,
  1 / 125, 1 / 100, 1 / 80, 1 / 60, 1 / 50, 1 / 40, 1 / 30, 1 / 25, 1 / 20, 1 / 15, 1 / 13, 1 / 10, 1 / 8, 1 / 6, 1 / 5, 1 / 4, 0.3, 0.4, 0.5, 0.6, 0.8, 1, 1.3, 1.6,
  2, 2.5, 3.2, 4, 5, 6, 8, 10, 13, 15, 20, 25, 30,
]

/** ISO in thirds of a stop, 25…102400. */
export const isoThirds: readonly number[] = [
  25, 32, 40, 50, 64, 80, 100, 125, 160, 200, 250, 320, 400, 500, 640, 800, 1000, 1250, 1600, 2000, 2500, 3200, 4000, 5000, 6400, 8000, 10000, 12800, 16000, 20000, 25600,
  32000, 40000, 51200, 64000, 80000, 102400,
]

/** The step of steps nearest to v in stops. */
export function nearestStep(steps: readonly number[], v: number): number {
  let best = steps[0]
  for (const s of steps) if (Math.abs(Math.log2(s / v)) < Math.abs(Math.log2(best / v))) best = s
  return best
}

/** A shutter as cameras show it: 1/125 s, 0.3 s, 2 s. */
export const shutterLabel = (s: number) => (s < 0.3 ? `1/${Math.round(1 / s)} s` : `${s} s`)

/**
 * The exposure a camera added under a physical sky starts with, as bright
 * as the default camera at ev (W2 design「曝光」): ISO 100 and the shutter
 * nearest; where no shutter reaches, the end one and the ISO nearest. At
 * f/2.8 that covers EV100 −11.9…17.9; past it the camera is darker, up to
 * about 2 stops (a known limit: the aperture also sets the depth of field).
 */
export function newCameraExposure(ev: number, fStop: number): { iso: number; shutter_s: number } {
  const t = (fStop * fStop) / 2 ** ev
  const first = shutterThirds[0]
  const last = shutterThirds[shutterThirds.length - 1]
  if (t >= first && t <= last) return { iso: 100, shutter_s: nearestStep(shutterThirds, t) }
  const shutter = t < first ? first : last
  const iso = (100 * fStop * fStop) / (shutter * 2 ** ev)
  return { iso: nearestStep(isoThirds, Math.min(isoThirds[isoThirds.length - 1], Math.max(isoThirds[0], iso))), shutter_s: shutter }
}

/**
 * k for what is drawn now: the sky's background and sun disc and the
 * materials' emission read it. One for the viewport and the offscreen
 * shooter alike, so it is written and drawn in one synchronous call
 * (SceneRenderer.render): WebGPU reads uniforms as it encodes. Do not put an
 * `await` between writing it and drawing, nor make the drawing asynchronous.
 * Outside a draw it is 1, so renderers that do not expose (thumbnails,
 * previews) draw the materials as they are.
 */
export const preExposure = uniform(1)

/**
 * What PhysicalLighting leaves on the scene for the exposure
 * (scene.userData.physical; cleared when it unmounts). The sky light is the
 * PMREM made last, at that one's s_env; lux is E, the level ground's
 * illuminance at the origin: the fog is lit like ground of its colour under
 * it, and the automatic exposure meters it.
 */
export type PhysicalExposure = { skyLightScale: number; fogColor: Color | null; lux: number }

/** E as PhysicalLighting left it on a three scene; 0 without one. */
export const sceneLux = (scene: Scene) => (scene.userData.physical as PhysicalExposure | undefined)?.lux ?? 0

/**
 * Writes k onto the scene before a picture (design「预曝光的落点」): the
 * scene's lights (those with userData.baseIntensity, their data's
 * intensity) at base × k, and the global preExposure. Under a physical sky
 * also the sky light, k / s_env, and the fog. Under another sky k is 1 and
 * the sky light and fog are left as the look set them.
 */
export function applyExposure(scene: Scene, k: number) {
  preExposure.value = k
  scene.traverse((o) => {
    const base = o.userData.baseIntensity as number | undefined
    if (base !== undefined && (o as Light).isLight) (o as Light).intensity = base * k
  })
  const physical = scene.userData.physical as PhysicalExposure | undefined
  if (!physical) return
  scene.environmentIntensity = k / physical.skyLightScale
  if (scene.fog && physical.fogColor) scene.fog.color.copy(physical.fogColor).multiplyScalar((physical.lux / Math.PI) * k)
}
