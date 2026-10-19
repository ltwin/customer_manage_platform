// Camera optics shared by the frustum, the camera view, the properties panel,
// export and render_view: one formula each, so what the editor shows is what
// the export draws (design「视场角」「景深」「传感器预设」).

export type Sensor = readonly [number, number]

export const fullFrame: Sensor = [36, 24]

export const sensorPresets: readonly { label: string; size: Sensor }[] = [
  { label: '全画幅', size: [36, 24] },
  { label: 'APS-C', size: [23.5, 15.6] },
  { label: 'APS-C（佳能）', size: [22.3, 14.9] },
  { label: 'M43', size: [17.3, 13] },
  { label: '中画幅', size: [43.8, 32.9] },
]

/** The preset with exactly this size, or null for a custom sensor. */
export function presetOf(sensor: readonly number[]): string | null {
  return sensorPresets.find((p) => p.size[0] === sensor[0] && p.size[1] === sensor[1])?.label ?? null
}

/** Width over height of an aspect like "3:2"; 3:2 when it cannot be read. */
export function aspectOf(aspect: string): number {
  const [w, h] = aspect.split(':').map(Number)
  return w > 0 && h > 0 ? w / h : 3 / 2
}

const longEdge = (sensor: readonly number[]) => Math.max(sensor[0], sensor[1])

/**
 * three.js's vertical field of view in degrees. The picture's long side
 * spans the sensor's long side, as Blender's Auto sensor fit: a portrait and
 * a landscape frame of one lens see the same width, turned.
 */
export function verticalFov(focalMM: number, sensor: readonly number[], aspect: number): number {
  const tanHalf = longEdge(sensor) / (2 * focalMM) / Math.max(aspect, 1)
  return (2 * Math.atan(tanHalf) * 180) / Math.PI
}

/** The full-frame focal length with the same field of view. */
export function equivalentFocal(focalMM: number, sensor: readonly number[]): number {
  return (focalMM * fullFrame[0]) / longEdge(sensor)
}

/**
 * The viewport's vertical field of view that puts the camera's picture in
 * `fill` of the viewport (the camera view's frame).
 */
export function cameraViewFov(cameraFov: number, cameraAspect: number, viewAspect: number, fill = 0.9): number {
  const rad = (d: number) => (d * Math.PI) / 180
  const deg = (r: number) => (r * 180) / Math.PI
  const tanV = Math.tan(rad(cameraFov) / 2)
  if (viewAspect >= cameraAspect) return deg(2 * Math.atan(tanV / fill))
  const tanH = (tanV * cameraAspect) / fill
  return deg(2 * Math.atan(tanH / viewAspect))
}

/** The focus distance in metres kept between near and far and beyond the lens. */
export function clampFocus(focusM: number, near: number, far: number, focalMM: number): number {
  return Math.min(Math.max(focusM, near, (focalMM / 1000) * 1.01), far)
}

/** At most this share of the picture's long side is blur, whatever the lens. */
export const maxBlurShare = 0.04

export type DepthOfField = { focalMM: number; fStop: number; focusM: number; sensorLongMM: number }

/**
 * The circle of confusion of a point `depthM` metres in front of the camera,
 * in pixels of a picture whose long side is `longSidePx`, signed: negative in
 * front of the focus, positive behind. The shader computes the same.
 */
export function blurPixels(dof: DepthOfField, depthM: number, longSidePx: number): number {
  const f = dof.focalMM
  const s = dof.focusM * 1000
  const d = depthM * 1000
  const c = ((f * f) / (dof.fStop * (s - f))) * (Math.abs(d - s) / d)
  const px = Math.min((c / dof.sensorLongMM) * longSidePx, maxBlurShare * longSidePx)
  return d < s ? -px : px
}

/**
 * The range that looks sharp, in metres: the circle of confusion stays under
 * the sensor diagonal over 1500. `far` is Infinity beyond the hyperfocal
 * distance.
 */
export function sharpRange(focalMM: number, fStop: number, focusM: number, sensor: readonly number[]): { near: number; far: number } {
  const c0 = Math.hypot(sensor[0], sensor[1]) / 1500
  const f = focalMM
  const s = focusM * 1000
  const h = (f * f) / (fStop * c0) + f
  const near = (s * (h - f)) / (h + s - 2 * f)
  const far = s < h ? (s * (h - f)) / (h - s) : Infinity
  return { near: near / 1000, far: far / 1000 }
}

/** The marked third stops from f/0.7 to f/64, for the aperture slider. */
export const fStopThirds: readonly number[] = [
  0.7, 0.8, 0.9, 1, 1.1, 1.2, 1.4, 1.6, 1.8, 2, 2.2, 2.5, 2.8, 3.2, 3.5, 4, 4.5, 5, 5.6, 6.3, 7.1, 8, 9, 10, 11, 13, 14, 16, 18, 20, 22, 25, 29, 32, 36,
  40, 45, 51, 57, 64,
]

export const fStopShortcuts: readonly number[] = [1.4, 2, 2.8, 4, 5.6, 8, 11, 16]
