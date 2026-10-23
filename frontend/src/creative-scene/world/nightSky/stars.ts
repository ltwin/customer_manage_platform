import { CustomBlending, Float32BufferAttribute, InstancedBufferAttribute, InstancedBufferGeometry, Matrix3, Mesh, OneFactor, Vector3, ZeroFactor } from 'three'
import * as tsl from 'three/tsl'
import { NodeMaterial } from 'three/webgpu'
import type { TSLNode } from '../../materials/patterns.ts'
import { CAMERA_ABOVE_GROUND } from '../atmosphere/constants.ts'
import type { AtmosphereLuts } from '../atmosphere/luts.ts'
import type { NightSkyNode } from '../atmosphere/sky.ts'
import { preExposure } from '../exposure.ts'
import type { StarCatalogue } from './assets.ts'
import { bvTint, magnitudeLux } from './light.ts'

// The stars as sprites (W3 design「星星」): one instanced quad per star of
// the catalogue, at the far plane like the sky box (render/sky.ts skyMesh),
// added onto the sky. A sprite's size is fixed in pixels against the
// picture's height (σ at 1080 px), so the viewport and an 8K export look
// alike; its light is the star's illuminance through the atmosphere spread
// over the sprite, so the sum over its pixels is the star's (energy
// conserving, under any exposure, lens or size). Stars brighter than
// magnitude 1 are drawn larger, their light the same. Refracted as the sun
// and moon are; none below the horizon or behind the moon's disc; no
// twinkling (an export is deterministic).

type Op = (...args: TSLNode[]) => TSLNode
const { asin, attribute, clamp, cos, dot, exp, float, length, max, positionGeometry, select, sin, sqrt, tan, uniform, varying, vec2, vec3, vec4 } = tsl as unknown as Record<string, Op> & {
  positionGeometry: TSLNode
}
const { cameraProjectionMatrix, cameraViewMatrix, viewportSize } = tsl as unknown as { cameraProjectionMatrix: TSLNode; cameraViewMatrix: TSLNode; viewportSize: TSLNode }
const varyingProperty = tsl.varyingProperty as unknown as (type: string, name: string) => TSLNode
const Fn = tsl.Fn as unknown as (fn: () => TSLNode) => () => TSLNode

type Uniform<T> = TSLNode & { value: T }

/** A sprite's Gaussian σ in pixels of a picture 1080 px high. */
export const STAR_SIGMA = 0.9
/** The quad's half-width in σ: the Gaussian beyond it holds 0.2% of the light. */
const REACH = 3.5
const DEG = Math.PI / 180

/** A star's σ over STAR_SIGMA: 1 to magnitude 1, then larger by a fifth a magnitude (Sirius about 1.5). */
export const starSpread = (v: number) => 1 + 0.2 * Math.max(0, 1 - v)

/** The refraction at a true altitude, degrees (Saemundsson, as astronomy-engine's 'normal' above −1°). */
export function refraction(altitudeDeg: number): number {
  const h = Math.max(altitudeDeg, -1)
  return Math.max(0, 1.02 / Math.tan((h + 10.3 / (h + 5.11)) * DEG) / 60)
}

/** The sprite's light on the pixel at offset (dx, dy) px from its centre, of a star whose light is 1: the sum over the pixels is 1. */
export function spriteShare(dx: number, dy: number, sigmaPx: number): number {
  if (Math.max(Math.abs(dx), Math.abs(dy)) > REACH * sigmaPx) return 0
  return Math.exp(-(dx * dx + dy * dy) / (2 * sigmaPx * sigmaPx)) / (2 * Math.PI * sigmaPx * sigmaPx)
}

export type StarField = Mesh & {
  /** J2000 equatorial to the document (world/astro.ts equatorToScene). */
  equator: Uniform<Matrix3>
  /** The stars' switch, strength and colour temperature per channel (light.ts starsScale); 0 draws none. */
  brightness: Uniform<Vector3>
}

/**
 * The catalogue's stars over a physical sky. luts is the sky's (the
 * transmittance and the radii), night the background's night uniforms (the
 * moon's disc hides the stars behind it). Drawn on the sky's layer after it.
 */
export function starField(catalogue: StarCatalogue, luts: AtmosphereLuts, night: NightSkyNode): StarField {
  const n = catalogue.count
  const dirs = new Float32Array(n * 3)
  const lux = new Float32Array(n * 3)
  const spread = new Float32Array(n)
  for (let i = 0; i < n; i++) {
    const ra = catalogue.ra[i] * DEG
    const dec = catalogue.dec[i] * DEG
    dirs.set([Math.cos(dec) * Math.cos(ra), Math.cos(dec) * Math.sin(ra), Math.sin(dec)], i * 3)
    const e = magnitudeLux(catalogue.v[i])
    const tint = bvTint(catalogue.bv[i])
    lux.set([e * tint[0], e * tint[1], e * tint[2]], i * 3)
    spread[i] = starSpread(catalogue.v[i])
  }
  const geometry = new InstancedBufferGeometry()
  geometry.setIndex([0, 1, 2, 0, 2, 3])
  geometry.setAttribute('position', new Float32BufferAttribute([-1, -1, 0, 1, -1, 0, 1, 1, 0, -1, 1, 0], 3))
  geometry.setAttribute('starDirection', new InstancedBufferAttribute(dirs, 3))
  geometry.setAttribute('starLux', new InstancedBufferAttribute(lux, 3))
  geometry.setAttribute('starSpread', new InstancedBufferAttribute(spread, 1))
  geometry.instanceCount = n

  const equator = uniform(new Matrix3()) as Uniform<Matrix3>
  const brightness = uniform(new Vector3()) as Uniform<Vector3>

  const material = new NodeMaterial()
  material.transparent = true
  material.depthWrite = false
  material.fog = false
  // Added onto the sky, its alpha left as it is.
  material.blending = CustomBlending
  material.blendSrc = OneFactor
  material.blendDst = OneFactor
  material.blendSrcAlpha = ZeroFactor
  material.blendDstAlpha = OneFactor

  // The sprite's σ in pixels of this picture, and the star's light per pixel at its centre: both from the vertex stage.
  const sigma = float(STAR_SIGMA).mul(viewportSize.y).div(1080).mul(attribute('starSpread', 'float'))
  const centre = varyingProperty('vec3', 'vStarLight')
  material.vertexNode = Fn(() => {
    // Apparent: raised by the refraction at its true altitude, as the sun and the moon are.
    const d = equator.mul(attribute('starDirection', 'vec3')).toVar()
    const h = asin(clamp(d.y, -1, 1)).mul(1 / DEG)
    const hd = max(h, -1)
    const raised = max(float(1.02).div(tan(hd.add(float(10.3).div(hd.add(5.11))).mul(DEG))).div(60), 0)
    const apparent = h.add(raised).mul(DEG)
    const level = vec2(d.x, d.z).div(max(length(vec2(d.x, d.z)), 1e-6))
    const a = vec3(level.x.mul(cos(apparent)), sin(apparent), level.y.mul(cos(apparent))).toVar()

    // Document (Y up) to the world the camera sees (render/sky.ts: view (x, y, z) is document (x, z, −y)).
    const view = cameraViewMatrix.mul(vec4(a.x, a.z.negate(), a.y, 0)).xyz.toVar()
    const position = cameraProjectionMatrix.mul(vec4(view, 1)).toVar()
    position.z.assign(position.w)

    // Hidden: below the horizon's dip (as the sky's discs), behind a drawn moon, behind the camera, or in an orthographic view, whose sky is a backdrop.
    const dip = sqrt(luts.groundRadius.mul(2).add(CAMERA_ABOVE_GROUND).mul(CAMERA_ABOVE_GROUND)).div(luts.viewRadius)
    const moonDrawn = dot(night.moonRadiance, vec3(1)).greaterThan(0)
    const isOrtho = cameraProjectionMatrix.element(3).element(3).equal(1)
    const hidden = a.y
      .lessThan(dip.negate())
      .or(moonDrawn.and(dot(a, night.moon.div(max(length(night.moon), 1e-6))).greaterThan(night.moonCosRadius)))
      .or(view.z.greaterThanEqual(0))
      .or(isOrtho)

    // A pixel's solid angle there: the centre's, (2 / (P₀₀ W)) (2 / (P₁₁ H)), times cos³ of the angle off the axis.
    const offAxis = view.z.negate()
    const pixel = float(4)
      .div(cameraProjectionMatrix.element(0).element(0).mul(viewportSize.x).mul(cameraProjectionMatrix.element(1).element(1)).mul(viewportSize.y))
      .mul(offAxis.mul(offAxis).mul(offAxis))
    const through = luts.sampleTransmittance(luts.viewRadius, clamp(a.y, -1, 1))
    centre.assign(attribute('starLux', 'vec3').mul(brightness).mul(through).mul(preExposure).div(pixel.mul(sigma.mul(sigma).mul(2 * Math.PI))))

    const reach = sigma.mul(REACH)
    const offset = positionGeometry.xy.mul(reach).mul(2).div(viewportSize).mul(position.w)
    return select(hidden, vec4(0, 0, 2, 1), position.add(vec4(offset, 0, 0)))
  })()
  const atPixels = varying(positionGeometry.xy.mul(sigma.mul(REACH)))
  const s = varying(sigma)
  material.colorNode = Fn(() => {
    const r2 = dot(atPixels, atPixels)
    return vec4(centre.mul(exp(r2.div(s.mul(s).mul(-2)))), 1)
  })()

  const mesh = new Mesh(geometry, material)
  mesh.frustumCulled = false
  // Drawn first of what is transparent: glass in front blends over the stars.
  mesh.renderOrder = -1
  // Its quad sits about the origin: it must not take a click meant for what is there.
  mesh.raycast = () => {}
  return Object.assign(mesh, { equator, brightness }) as StarField
}

export function disposeStars(stars: StarField) {
  stars.geometry.dispose()
  ;(stars.material as NodeMaterial).dispose()
}
