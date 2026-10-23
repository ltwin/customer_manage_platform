import { Color } from 'three'
import * as tsl from 'three/tsl'
import type { components } from '../../../api/schema'
import type { TSLNode } from '../../materials/patterns.ts'

// A volume object's medium (W5, data: 体积物件; scene-volumes design「领域模型
// 与业务规则」): a unit box or sphere in its object's local space, scattering
// and absorbing as Blender's Volume Scatter and Volume Absorption do. Free
// of raster state, so a path tracer can read the same medium later.

export type Volume = Required<components['schemas']['CreativeSceneVolume']>
export type Rgb = [number, number, number]

/** Per metre, linear: σs, σa and σt = σs + σa. */
export type Coefficients = { scatter: Rgb; absorb: Rgb; extinction: Rgb }

const linear = (hex: string): Rgb => {
  const c = new Color(hex)
  return [c.r, c.g, c.b]
}

/** σs = scatter_density × scatter_color, σa = absorption_density × (1 − absorption_color), on linear colours. */
export function coefficients(v: Volume): Coefficients {
  const s = linear(v.scatter_color)
  const a = linear(v.absorption_color)
  const scatter = s.map((c) => c * v.scatter_density) as Rgb
  const absorb = a.map((c) => (1 - c) * v.absorption_density) as Rgb
  return { scatter, absorb, extinction: [scatter[0] + absorb[0], scatter[1] + absorb[1], scatter[2] + absorb[2]] }
}

/**
 * The analytic chord's density factor: along a chord from the edge to the
 * centre the falloff ramps over edge_falloff of it, so the mean is
 * 1 − edge_falloff / 2. Self-shadowing and cast shadows use it; a wide
 * falloff no longer reads too dark.
 */
export const chordFactor = (v: Volume) => 1 - v.edge_falloff / 2

// The same medium in TSL, over the uniforms of state.ts: V.inv (world to the
// unit shape), V.ext.w the edge falloff, V.shp.x the shape, V.shp.y the chord factor.

type Op = (...args: TSLNode[]) => TSLNode
const { abs, dot, float, length, max, min, select, smoothstep, sqrt, vec2, vec3, vec4 } = tsl as unknown as Record<string, Op>
const If = tsl.If as unknown as (cond: TSLNode, then: () => void) => { Else(otherwise: () => void): void }

type Medium = { inv: TSLNode; ext: TSLNode; shp: TSLNode }

const safe = (x: TSLNode) => select(abs(x).lessThan(1e-7), float(1e-7), x)

/** Where the world ray o + t·d crosses volume i: vec2(t in, t out); in ≥ out for a miss. */
export function hitVolume(V: Medium, i: TSLNode, o: TSLNode, d: TSLNode): TSLNode {
  const m = V.inv.element(i)
  const ol = m.mul(vec4(o, 1)).xyz.toVar()
  const dl = m.mul(vec4(d, 0)).xyz.toVar()
  const r = vec2(1e9, -1e9).toVar()
  If(V.shp.element(i).x.lessThan(0.5), () => {
    // The slab test against the unit box.
    const id = vec3(1).div(vec3(safe(dl.x), safe(dl.y), safe(dl.z)))
    const a = vec3(-0.5).sub(ol).mul(id)
    const b = vec3(0.5).sub(ol).mul(id)
    const lo = min(a, b)
    const hi = max(a, b)
    r.assign(vec2(max(max(lo.x, lo.y), lo.z), min(min(hi.x, hi.y), hi.z)))
  }).Else(() => {
    // The unit sphere, radius 0.5.
    const A = dot(dl, dl)
    const B = dot(ol, dl)
    const disc = B.mul(B).sub(A.mul(dot(ol, ol).sub(0.25)))
    If(disc.greaterThan(0), () => {
      const s = sqrt(disc)
      r.assign(vec2(B.negate().sub(s), B.negate().add(s)).div(A))
    })
  })
  return r
}

/** Volume i's density share at world p: 1 inside, from 0 at the boundary over edge_falloff of the way to the centre. */
export function volumeDensity(V: Medium, i: TSLNode, p: TSLNode): TSLNode {
  const l = V.inv.element(i).mul(vec4(p, 1)).xyz
  const q = abs(l)
  const e = select(V.shp.element(i).x.lessThan(0.5), float(1).sub(max(max(q.x, q.y), q.z).mul(2)), float(1).sub(length(l).mul(2)))
  const edge = V.ext.element(i).w
  return select(e.lessThanEqual(0), float(0), select(edge.lessThan(1e-4), float(1), smoothstep(0, edge, e)))
}

/** How much of volume i lies along p + t·toward for t in 0…maxT, times its mean density along a chord (chordFactor): the analytic depth for its σt. */
export function chordThrough(V: Medium, i: TSLNode, p: TSLNode, toward: TSLNode, maxT: TSLNode): TSLNode {
  const h = hitVolume(V, i, p, toward)
  return max(min(h.y, maxT).sub(max(h.x, 0)), 0).mul(V.shp.element(i).y)
}
