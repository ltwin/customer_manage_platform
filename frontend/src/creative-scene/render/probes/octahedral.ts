import * as tsl from 'three/tsl'
import type { TSLNode } from '../../materials/patterns.ts'

// Octahedral maps of the sphere (S2 design「探针体」): a probe's irradiance
// and depth moments are stored as square tiles, a direction to a point in
// [-1, 1]², the lower hemisphere folded out to the corners (Cigolle et al.
// 2014, the layout DDGI uses). The same mapping twice, in JS for the unit
// tests and the CPU, in TSL for the kernels; checks.ts compares the two on
// the GPU.
//
// A tile of n × n texels carries a 1-texel border, so bilinear filtering at
// its edge reads what lies across the fold: the texel at (i, j) of the
// interior, j growing with v, sits at ((i + ½) / n · 2 − 1, (j + ½) / n · 2 − 1).

type Op = (...args: TSLNode[]) => TSLNode
const T = tsl as unknown as Record<string, Op>
const { Fn, abs, float, normalize, select, vec2, vec3 } = T

type Vec3 = readonly [number, number, number]

/** 1 for 0 too: a direction on a fold maps to one side of it, not to the middle. */
const signOf = (x: number) => (x < 0 ? -1 : 1)

/** The point in [-1, 1]² a unit direction maps to. */
export function encodeOct([x, y, z]: Vec3): [number, number] {
  const l1 = Math.abs(x) + Math.abs(y) + Math.abs(z)
  const u = x / l1
  const v = y / l1
  if (z >= 0) return [u, v]
  return [(1 - Math.abs(v)) * signOf(u), (1 - Math.abs(u)) * signOf(v)]
}

/** The unit direction a point in [-1, 1]² stands for. */
export function decodeOct([u, v]: readonly [number, number]): [number, number, number] {
  let x = u
  let y = v
  const z = 1 - Math.abs(u) - Math.abs(v)
  if (z < 0) {
    x = (1 - Math.abs(v)) * signOf(u)
    y = (1 - Math.abs(u)) * signOf(v)
  }
  const l = Math.hypot(x, y, z)
  return [x / l, y / l, z / l]
}

/** The centre of interior texel (i, j) of an n-texel tile, in [-1, 1]². */
export const texelCentre = (i: number, j: number, n: number): [number, number] => [((i + 0.5) / n) * 2 - 1, ((j + 0.5) / n) * 2 - 1]

/**
 * The border texels (in the tile's interior coordinates, −1 and n on the
 * border) that copy interior texel (i, j): across an edge the map folds over
 * (u, −1 − ε) ↔ (−u, −1 + ε), so a border row copies its neighbouring row
 * reversed, and each corner the interior corner diagonally opposite. The
 * thread that writes (i, j) writes these too: every border texel has one source.
 */
export function bordersOf(i: number, j: number, n: number): [number, number][] {
  const out: [number, number][] = []
  const last = n - 1
  if (j === 0) out.push([last - i, -1])
  if (j === last) out.push([last - i, n])
  if (i === 0) out.push([-1, last - j])
  if (i === last) out.push([n, last - j])
  if (i === last && j === last) out.push([-1, -1])
  if (i === 0 && j === last) out.push([n, -1])
  if (i === last && j === 0) out.push([-1, n])
  if (i === 0 && j === 0) out.push([n, n])
  return out
}

/** signOf in TSL: 1 for 0 too. */
const signNode = (x: TSLNode) => select(x.lessThan(0), float(-1), float(1))

/** encodeOct in TSL: a unit direction to its point in [-1, 1]². */
export const octEncode = Fn(([dir]: TSLNode[]) => {
  const p = dir.xy.div(abs(dir.x).add(abs(dir.y)).add(abs(dir.z))).toVar()
  const folded = vec2(float(1).sub(abs(p.y)).mul(signNode(p.x)), float(1).sub(abs(p.x)).mul(signNode(p.y)))
  return select(dir.z.lessThan(0), folded, p)
})

/** decodeOct in TSL: a point in [-1, 1]² to its unit direction. */
export const octDecode = Fn(([e]: TSLNode[]) => {
  const z = float(1).sub(abs(e.x)).sub(abs(e.y)).toVar()
  const folded = vec2(float(1).sub(abs(e.y)).mul(signNode(e.x)), float(1).sub(abs(e.x)).mul(signNode(e.y)))
  const xy = select(z.lessThan(0), folded, e)
  return normalize(vec3(xy, z))
})
