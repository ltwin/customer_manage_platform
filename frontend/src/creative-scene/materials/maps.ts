import * as tsl from 'three/tsl'
import type { Texturing } from './resolve.ts'

// How a principled slot's textures land on a surface (C5c1 design「光栅材质的
// 构建」): the mapping, the sampling, and the normal map's decoding, written
// once for the raster and for traced hit points (render/trace/hit.ts), which
// pass their own arithmetic (a Kit) and their own way of sampling. A unit
// test runs the same functions on numbers.
//
// Coordinates follow Blender: a projection's plane coordinates are (u, w)
// with w up, and the Mapping node's point type rotates them after scaling;
// textures keep their rows from the top (images.ts), so the texture
// coordinate is (u', 1 − w'). Box projection maps each axis as Cycles does
// (svm_image.h), u mirrored on the faces where a texture would read
// backwards. Normal maps are tangent space, OpenGL's way (+Y up, as glTF and
// Blender): their slopes are taken along the texture's right and up.

/** Arithmetic on scalars and vectors, broadcasting a scalar: TSL nodes for a shader, numbers for a test. */
export type Kit<N> = {
  num(v: number): N
  vec2(x: N, y: N): N
  vec3(x: N, y: N, z: N): N
  vec4(xyz: N, w: N): N
  add(a: N, b: N): N
  sub(a: N, b: N): N
  mul(a: N, b: N): N
  div(a: N, b: N): N
  max(a: N, b: N): N
  abs(a: N): N
  dot(a: N, b: N): N
  cross(a: N, b: N): N
  normalize(a: N): N
  inverseSqrt(a: N): N
  /** −1 where a < 0, 1 elsewhere. */
  side(a: N): N
  x(a: N): N
  y(a: N): N
  z(a: N): N
  w(a: N): N
  xyz(a: N): N
}

/** What a slot samples: its base colour (sRGB, alpha), normal map and ORM (ambient occlusion, roughness, metalness in r g b). */
export type MapKind = 'color' | 'normal' | 'orm'

/**
 * Samples one of the slot's textures at a texture coordinate; perMetre is how
 * many texture units a metre of surface spans, for a sampler that sets its
 * own level of detail (a traced hit's ray cone).
 */
export type Sample<N> = (which: MapKind, coord: N, perMetre: N) => N

/** A slot's mapping with its defaults in (C5c1 contract「材质贴图」). */
export type Mapping = { kind: 'triplanar' | 'uv'; size: number; repeat: [number, number]; rotation: number; strength: number }

export function mappingOf(t: Texturing | undefined): Mapping {
  const m = t?.mapping
  return {
    kind: m?.kind ?? 'triplanar',
    size: m?.size_m ?? 1,
    repeat: [m?.repeat?.[0] ?? 1, m?.repeat?.[1] ?? 1],
    rotation: ((m?.rotation_deg ?? 0) * Math.PI) / 180,
    strength: m?.normal_strength ?? 1,
  }
}

/** A mapping's numbers as the shader reads them: scale (texture units per plane unit, u and w), the rotation's cosine and sine, the normal strength. */
export type MappingIn<N> = { scale: N; cos: N; sin: N; strength: N }

/** Plane coordinates (u, w) → the rotated, scaled ones (u', w'): Blender's Mapping node, point type. */
function mapped<N>(k: Kit<N>, b: N, m: MappingIn<N>): N {
  const s = k.mul(b, m.scale)
  return k.vec2(k.sub(k.mul(k.x(s), m.cos), k.mul(k.y(s), m.sin)), k.add(k.mul(k.x(s), m.sin), k.mul(k.y(s), m.cos)))
}

/** Where (u', w') samples a texture whose rows run from the top. */
const texel = <N>(k: Kit<N>, b: N): N => k.vec2(k.x(b), k.sub(k.num(1), k.y(b)))

/** The normal map's texel as a tangent-space normal (x right, y up), its bend scaled by strength. */
export function tangentNormal<N>(k: Kit<N>, rgba: N, strength: N): N {
  const n = k.sub(k.mul(k.xyz(rgba), k.num(2)), k.num(1))
  return k.vec3(k.mul(k.x(n), strength), k.mul(k.y(n), strength), k.max(k.z(n), k.num(1e-3)))
}

/** The slope a tangent-space normal stands for along the texture's right and up: what the height rises per unit along each. */
const slopes = <N>(k: Kit<N>, n: N): N => k.vec2(k.div(k.mul(k.x(n), k.num(-1)), k.z(n)), k.div(k.mul(k.y(n), k.num(-1)), k.z(n)))

/**
 * Turns slopes along (u', w') back onto (u, w): the inverse of the
 * rotation. The scale does not enter, as a normal map's angles do not grow
 * when it is tiled smaller.
 */
function unrotated<N>(k: Kit<N>, s: N, m: MappingIn<N>): N {
  return k.vec2(k.add(k.mul(k.x(s), m.cos), k.mul(k.y(s), m.sin)), k.sub(k.mul(k.y(s), m.cos), k.mul(k.x(s), m.sin)))
}

/** The triplanar blend: pow(|n|, 4), normalised, as the procedural presets project (patterns.ts). */
export function triWeights<N>(k: Kit<N>, normal: N): N {
  const a = k.abs(k.normalize(normal))
  const a2 = k.mul(a, a)
  const a4 = k.mul(a2, a2)
  return k.div(a4, k.add(k.add(k.x(a4), k.y(a4)), k.z(a4)))
}

/** What the textures give at a point: base colour with alpha (1 without a colour map), ORM (ones without), and the normal's bend. */
export type Surface<N> = { color: N; orm: N; bend: N | null }

export type Has = { color: boolean; normal: boolean; orm: boolean }

/**
 * Box projection at an object-space point in true metres (so a scaled object
 * does not stretch its textures) with its object-space normal. The bend is
 * the height's gradient along the surface in the same space, the three
 * planes' blended (surface gradient): perturbed with bendNormal.
 */
export function triplanar<N>(k: Kit<N>, pos: N, normal: N, m: MappingIn<N>, has: Has, sample: Sample<N>): Surface<N> {
  const w = triWeights(k, normal)
  const zero = k.num(0)
  const one = k.num(1)
  // Cycles' axes: X takes (y, z), Y (x, z), Z (y, x), u mirrored where it would read backwards.
  const fx = k.side(k.x(normal))
  const fy = k.mul(k.side(k.y(normal)), k.num(-1))
  const fz = k.mul(k.side(k.z(normal)), k.num(-1))
  const planes = [
    { weight: k.x(w), b: k.vec2(k.mul(k.y(pos), fx), k.z(pos)), u: k.vec3(zero, fx, zero), up: k.vec3(zero, zero, one) },
    { weight: k.y(w), b: k.vec2(k.mul(k.x(pos), fy), k.z(pos)), u: k.vec3(fy, zero, zero), up: k.vec3(zero, zero, one) },
    { weight: k.z(w), b: k.vec2(k.mul(k.y(pos), fz), k.x(pos)), u: k.vec3(zero, fz, zero), up: k.vec3(one, zero, zero) },
  ]
  // Every plane sampled, unbranched: implicit derivatives are not taken inside a branch (patterns「场景着色写法」).
  const blend = (which: MapKind, f: (rgba: N, plane: (typeof planes)[number]) => N, none: N): N => {
    let sum: N | null = null
    for (const p of planes) {
      const v = k.mul(f(sample(which, texel(k, mapped(k, p.b, m)), k.x(m.scale)), p), p.weight)
      sum = sum === null ? v : k.add(sum, v)
    }
    return sum ?? none
  }
  const color = has.color ? blend('color', (c) => c, zero) : k.vec4(k.vec3(one, one, one), one)
  const orm = has.orm ? blend('orm', (c) => k.xyz(c), zero) : k.vec3(one, one, one)
  const bend = has.normal
    ? blend(
        'normal',
        (c, p) => {
          const s = unrotated(k, slopes(k, tangentNormal(k, c, m.strength)), m)
          return k.add(k.mul(p.u, k.x(s)), k.mul(p.up, k.y(s)))
        },
        zero,
      )
    : null
  return { color, orm, bend }
}

/** N bent by a height gradient g given in N's space: g's part along the surface taken off it. */
export function bendNormal<N>(k: Kit<N>, normal: N, g: N): N {
  const along = k.sub(g, k.mul(normal, k.dot(g, normal)))
  return k.normalize(k.sub(normal, along))
}

/** UV mapping: the mapped coordinate (u', w') of a mesh's TEXCOORD_0, glTF's rows from the top. */
export function uvMapped<N>(k: Kit<N>, uv: N, m: MappingIn<N>): N {
  return mapped(k, k.vec2(k.x(uv), k.sub(k.num(1), k.y(uv))), m)
}

/** The textures at a UV-mapped point; its normal needs the frame (uvNormal). */
export function uvSurface<N>(k: Kit<N>, b: N, m: MappingIn<N>, has: Has, sample: Sample<N>): { color: N; orm: N; normal: N | null } {
  const at = texel(k, b)
  const one = k.num(1)
  return {
    color: has.color ? sample('color', at, k.x(m.scale)) : k.vec4(k.vec3(one, one, one), one),
    orm: has.orm ? k.xyz(sample('orm', at, k.x(m.scale))) : k.vec3(one, one, one),
    normal: has.normal ? tangentNormal(k, sample('normal', at, k.x(m.scale)), m.strength) : null,
  }
}

/**
 * A tangent-space normal n on a surface of normal N, whose position moves by
 * dp1, dp2 while (u', w') moves by db1, db2: across a pixel for the raster,
 * along a triangle's edges for a hit. The frame is (u', w')'s own, so a
 * rotated texture's bumps turn with it; three's normalMap takes the raw
 * uv's (C5c1 design). Christian Schüler's cotangent frame, scaled by its
 * longer axis.
 */
export function uvNormal<N>(k: Kit<N>, normal: N, dp1: N, dp2: N, db1: N, db2: N, n: N): N {
  const dp2perp = k.cross(dp2, normal)
  const dp1perp = k.cross(normal, dp1)
  const t = k.add(k.mul(dp2perp, k.x(db1)), k.mul(dp1perp, k.x(db2)))
  const b = k.add(k.mul(dp2perp, k.y(db1)), k.mul(dp1perp, k.y(db2)))
  const scale = k.inverseSqrt(k.max(k.max(k.dot(t, t), k.dot(b, b)), k.num(1e-30)))
  return k.normalize(k.add(k.add(k.mul(t, k.mul(k.x(n), scale)), k.mul(b, k.mul(k.y(n), scale))), k.mul(normal, k.z(n))))
}

/* eslint-disable @typescript-eslint/no-explicit-any -- three's TSL typings do not follow swizzles and chained maths */
type Node = any
const { float, vec2, vec3, vec4, max, abs, dot, cross, normalize, inverseSqrt, select } = tsl as unknown as Record<string, (...args: Node[]) => Node>

/** The kit for shaders. */
export const shaderKit: Kit<Node> = {
  num: (v) => float(v),
  vec2: (x, y) => vec2(x, y),
  vec3: (x, y, z) => vec3(x, y, z),
  vec4: (xyz, w) => vec4(xyz, w),
  add: (a, b) => a.add(b),
  sub: (a, b) => a.sub(b),
  mul: (a, b) => a.mul(b),
  div: (a, b) => a.div(b),
  max: (a, b) => max(a, b),
  abs: (a) => abs(a),
  dot: (a, b) => dot(a, b),
  cross: (a, b) => cross(a, b),
  normalize: (a) => normalize(a),
  inverseSqrt: (a) => inverseSqrt(a),
  side: (a) => select(a.lessThan(0), float(-1), float(1)),
  x: (a) => a.x,
  y: (a) => a.y,
  z: (a) => a.z,
  w: (a) => a.w,
  xyz: (a) => a.xyz,
}
