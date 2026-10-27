import * as tsl from 'three/tsl'
import type { TSLNode } from '../../materials/patterns.ts'

// The light model's formulas (Epic design「光栅与命中点一致」, I4): the
// raster's light nodes and lighting model, and the world-space layer's hits
// (render/trace/hit.ts), draw sized lights by these alone.
//
// A sized light is seen from a surface as a disc of angular radius asin(s):
// a sphere of radius r at distance d has s = r / d, a directional light of
// angular diameter θ has s = sin(θ/2) (at d = 1, its way a unit vector).

type Op = (...args: TSLNode[]) => TSLNode
const T = tsl as unknown as Record<string, Op>
const { clamp, cross, dot, float, length, max, min, normalize, select, sqrt, vec3 } = T

/** How a surface sees a sized light: the way to its centre (d long, 1 for a directional light) and s, the sine of its angular radius. */
export type Sized = { lightVector: TSLNode; sinHalf: TSLNode }

/** s held off 0 and 1: Snyder's sin² must not underflow, nor the light's disc reach the whole sky (design「灯光与阴影子类」). */
export const sinHalfOf = (r: TSLNode, d: TSLNode): TSLNode => clamp(r.div(d), 1e-3, 0.9999)

/**
 * What three's falloff at d becomes at the effective distance max(d, r)
 * (Epic design I2): 1 / max(d^decay, 0.01) made 1 / max(max(d, r)^decay,
 * 0.01), as a factor on three's. The cutoff window stays three's, at d. Not
 * three's getDistanceAttenuation run again: S0 found the ratio came out wrong
 * that way (integration.md「灯光节点」).
 */
export function effectiveFalloff(d: TSLNode, r: TSLNode, decay: TSLNode): TSLNode {
  return max(d.pow(decay), 0.01).div(max(max(d, r).pow(decay), 0.01))
}

/** Where the hybrid hands the light's size from widening to moving: s up to WIDEN_UP_TO α widens the lobe (S0 integration.md「高光模型」). */
export const WIDEN_UP_TO = 1.5
/** How much a light of angular radius s widens a lobe, in quadrature: α'² = α² + (WIDEN_BY s)² (S0, fitted at roughness 0.15). */
export const WIDEN_BY = 0.6

/**
 * The moved part's energy: (α' / α'')², α'' = α' + s_s / k, k running from
 * SHIFT_DIV_MOVED, where the moved part is all (a near-mirror coat, Karis's
 * 1/2), to SHIFT_DIV_MOVED + SHIFT_DIV_WIDE as it shrinks beside α', by the
 * widened share to SHIFT_POWER. Karis's k = 2 throughout left a moderately
 * moved highlight 15% short of the reference (r 0.3 m at roughness 0.15, the
 * sun at 5°); a k that suited it lit a near-mirror coat twice over
 * (scene-light-radius tasks 2.2, measured against the reference).
 */
export const SHIFT_DIV_MOVED = 2
export const SHIFT_DIV_WIDE = 2
export const SHIFT_POWER = 1.5

/** A specular lobe as a sized light shapes it: the roughness to draw it with, the way to light it from, and the energy kept. */
export type Lobe = { roughness: TSLNode; direction: TSLNode; energy: TSLNode }

/**
 * The hybrid of a widened lobe and Karis 2013's representative point (Epic
 * design「直接光的入口」, S0 sphere.ts hybrid): of the light's angular radius
 * s, the part within WIDEN_UP_TO α widens the lobe in quadrature, the rest
 * moves the light toward the reflected ray, its energy kept as Karis keeps it,
 * (α' / α'')², α'' widened by the moved part (SHIFT_DIV_*). A light small beside the lobe only
 * widens it; a large one shows as its mirror image. roughness is perceptual
 * (α = roughness²), as three's; normal and view are the layer's own (the
 * clearcoat has its normal), all in the same space as lightVector.
 */
export function hybridLobe({ lightVector, sinHalf }: Sized, roughness: TSLNode, normal: TSLNode, view: TSLNode): Lobe {
  const d = length(lightVector)
  const a = roughness.mul(roughness)
  const widened = min(sinHalf, a.mul(WIDEN_UP_TO))
  const moved = sinHalf.sub(widened)
  const a1 = sqrt(a.mul(a).add(widened.mul(WIDEN_BY).pow(2)))
  const k = a1.div(a1.add(moved)).pow(SHIFT_POWER).mul(SHIFT_DIV_WIDE).add(SHIFT_DIV_MOVED)
  const a2 = a1.add(moved.div(k))
  // The point on the light nearest the reflected ray, moved off the centre by at most the moved part.
  const R = view.negate().reflect(normal)
  const centreToRay = dot(lightVector, R).mul(R).sub(lightVector)
  const toward = normalize(lightVector.add(centreToRay.mul(clamp(moved.mul(d).div(max(length(centreToRay), 1e-6)), 0, 1))))
  return { roughness: sqrt(a1), direction: toward, energy: a1.div(max(a2, 1e-4)).pow(2) }
}

/**
 * The way a diffuse surface takes to a sized light that may sit partly below
 * its horizon (U6): Snyder 1996's exact irradiance from a sphere clipped by
 * the horizon, as Lagarde and de Rousiers 2014 write it, given as a cosine F,
 * the direction bent toward the normal so that N·L' = F. Above the horizon F
 * is N·L and the direction the centre's; below, the part of the light still
 * seen lights the surface (the wrap past the terminator).
 */
export function horizonDirection({ lightVector, sinHalf }: Sized, normal: TSLNode): TSLNode {
  const L = normalize(lightVector)
  const sin2 = sinHalf.mul(sinHalf)
  const cosT = clamp(dot(normal, L), -1, 1)
  const sinT = sqrt(float(1).sub(cosT.mul(cosT)))
  const x = sqrt(float(1).div(sin2).sub(1))
  const y = clamp(x.negate().mul(cosT).div(max(sinT, 1e-4)), -1, 1)
  const sinTy = sinT.mul(sqrt(float(1).sub(y.mul(y))))
  const clipped = cosT.mul(T.acos(y)).sub(x.mul(sinTy)).mul(sin2).add(T.atan(sinTy.div(x)))
  const unclipped = sin2.mul(Math.PI).mul(max(cosT, 0))
  const E = max(select(cosT.mul(cosT).greaterThan(sin2), unclipped, clipped), 0)
  const F = clamp(E.div(sin2.mul(Math.PI)), 0, 1)
  // L less its part along N, made a unit: the way to bend along (any tangent where L is the normal).
  const along = L.sub(normal.mul(cosT))
  const tangent = select(length(along).greaterThan(1e-4), normalize(along), normalize(cross(normal, vec3(0, 1, 0.1))))
  return normalize(normal.mul(F).add(tangent.mul(sqrt(float(1).sub(F.mul(F))))))
}
