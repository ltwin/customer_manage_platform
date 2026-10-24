import { DataTexture, type Camera, type Matrix4, type Scene, type Texture } from 'three'
import * as tsl from 'three/tsl'
import type { WebGPURenderer } from 'three/webgpu'
import type { TSLNode } from '../../materials/patterns.ts'
import { preExposure } from '../exposure.ts'
import { AERIAL } from './constants.ts'
import type { AtmosphereLuts } from './luts.ts'

// The aerial perspective (W4, Epic design「合成顺序」第 4 步): between the
// camera and an opaque surface the air scatters light in and lets some of the
// surface through, colour · T + S, looked up in the froxels luts.ts makes
// for the camera before each picture. The sky's background already holds the
// whole atmosphere and is left as it is. One set of uniforms for the
// viewport and the offscreen shooter alike, written and drawn in one
// synchronous call as world/exposure.ts preExposure is.

type Op = (...args: TSLNode[]) => TSLNode
const { clamp, float, floor, getViewPosition, length, min, mix, screenUV, select, sqrt, texture, uniform, vec2, vec4 } = tsl as unknown as Record<string, Op>
type Uniform<T> = TSLNode & { value: T }

/** What PhysicalLighting leaves on the scene for the aerial perspective (scene.userData.aerial; cleared when it unmounts). */
export type SceneAerial = {
  luts: AtmosphereLuts
  enabled: boolean
  distanceScale: number
  /** s_env the froxels are stored at (the sky view's). */
  envScale: number
}

// No light, all let through, until a picture's froxels are swapped in.
const none = new DataTexture(new Uint8Array([0, 0, 0, 255]), 1, 1)
none.needsUpdate = true

const on = uniform(0) as Uniform<number>
const distanceScale = uniform(1) as Uniform<number>
/** k / s_env: the froxels' light, as the sky view's, into the picture's pre-exposed units. */
const relative = uniform(1) as Uniform<number>
const froxels = texture(none) as TSLNode & { value: Texture }

/**
 * Before a lit picture: the froxels for this camera and the uniforms from
 * the scene's aerial perspective, or off when it has none or it is off. After
 * applyExposure, whose k it reads.
 */
export function prepareAerial(scene: Scene, renderer: WebGPURenderer, camera: Camera, drawn: boolean) {
  const aerial = scene.userData.aerial as SceneAerial | undefined
  if (!drawn || !aerial?.enabled) {
    on.value = 0
    return
  }
  aerial.luts.renderAerial(renderer, camera)
  froxels.value = aerial.luts.aerial.texture
  on.value = 1
  distanceScale.value = aerial.distanceScale
  relative.value = preExposure.value / aerial.envScale
}

/** After a picture: off, so a renderer without prepareAerial draws none. */
export function finishAerial() {
  on.value = 0
}

/** A froxel slice's light and transmittance at a view direction's uv (0…1 each way). */
function slice(index: TSLNode, at: TSLNode): TSLNode {
  // Clamped within the slice: its neighbours sit beside it in the atlas.
  const half = 0.5 / AERIAL.w
  const x = clamp(at.x, half, 1 - half)
  const y = clamp(at.y, 0.5 / AERIAL.h, 1 - 0.5 / AERIAL.h)
  return froxels.sample(vec2(index.add(x).div(AERIAL.slices), y)).level(0)
}

/** The froxels at view-space position viewPosition: the light scattered in on the way there (rgb, the sky view's units) and what is let through (a). */
function lookup(viewPosition: TSLNode, projection: Uniform<Matrix4>): TSLNode {
  const clip = projection.mul(vec4(viewPosition, 1))
  const at = clip.xy.div(clip.w).mul(0.5).add(0.5)
  const km = length(viewPosition).mul(distanceScale).div(1000)
  // Slice centres at maxKm · ((i + 0.5) / n)²; before the first, towards nothing at the camera.
  const s = sqrt(min(km.div(AERIAL.maxKm), 1)).mul(AERIAL.slices).sub(0.5)
  const i0 = clamp(floor(s), 0, AERIAL.slices - 1)
  const i1 = min(i0.add(1), AERIAL.slices - 1)
  const between = mix(slice(i0, at), slice(i1, at), clamp(s.sub(i0), 0, 1))
  const near = clamp(s.add(0.5).mul(2), 0, 1)
  return select(s.lessThan(0), mix(vec4(0, 0, 0, 1), slice(float(0), at), near), between)
}

/**
 * The aerial perspective over colour (linear, pre-exposed) at an opaque
 * surface whose view-space position is viewPosition, for a camera whose
 * projection is projection: colour where it is off or on the background.
 */
export function applyAerial(color: TSLNode, viewPosition: TSLNode, projection: Uniform<Matrix4>, opaque: TSLNode): TSLNode {
  const ap = lookup(viewPosition, projection)
  const fogged = color.mul(ap.a).add(ap.rgb.mul(relative))
  return mix(color, fogged, on.mul(opaque))
}

/**
 * The air between the camera and view-space position viewPosition, in the
 * picture's units: the light it scatters in (rgb, S) and what it lets
 * through (a, T); none, vec4(0, 0, 0, 1), where the aerial perspective is
 * off. The volumes take the air in front of them and behind them apart
 * (Epic design「合成顺序」, revision v9).
 */
export function aerialAt(viewPosition: TSLNode, projection: Uniform<Matrix4>): TSLNode {
  const ap = lookup(viewPosition, projection)
  return mix(vec4(0, 0, 0, 1), vec4(ap.rgb.mul(relative), ap.a), on)
}

/**
 * applyAerial behind a pass: at the opaque surface its depth (a depth
 * texture node, sampled where the picture is) shows, for camera.
 */
export function aerialBehind(color: TSLNode, depth: TSLNode, camera: Camera): TSLNode {
  const d = depth.r
  const viewPosition = getViewPosition(screenUV, d, uniform(camera.projectionMatrixInverse))
  return applyAerial(color, viewPosition, uniform(camera.projectionMatrix) as Uniform<Matrix4>, select(d.lessThan(1), float(1), float(0)))
}
