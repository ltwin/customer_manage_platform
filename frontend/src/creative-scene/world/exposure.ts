// Exposure under a physical sky (Epic design「物理单位与曝光」): manual only
// (owner 2026-10-22). A scene camera's ISO, shutter and aperture, or the
// default camera's render.exposure_ev100, give EV100; lights are multiplied
// by k = 1 / (1.2 · 2^EV100) before the GPU sees them (pre-exposure). The
// sky and its light are stored in environment-relative units, × s_env.

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
