// S0 group 3 (scene-trace-spike design「判定标准」组 3): a copy of src/creative-scene/materials/patterns.ts whose
// presets take a width function (Epic design「命中点着色」, I7) in place of calling fwidth: the raster passes fwidth
// and draws what it draws today; a hit point, where WGSL has no derivatives, passes its own. Every other line is the
// original's; the diff against it is the change S1 makes.
import * as tsl from 'three/tsl'

// The procedural presets' shading (design「材质着色」, patterns「场景着色写法」):
// each preset is a TSL function of the object-space position in true metres
// (so a scaled object does not stretch its pattern), the object-space normal
// for the projection weights, its parameters as uniforms and the metres one
// pixel covers; it gives base colour, roughness, metalness, a height in
// metres for bump, and emission. It reads nothing of the renderer, so a
// compute path tracer can call the same function later.
//
// Noise presets are solid textures evaluated in 3D: no projection, no
// seams. Flat patterns (brick, tile, planks, fabric) are projected along the
// three axes and blended by pow(|n|, 4); their joints may not meet at
// corners, which is accepted.
//
// Anti-aliasing, since multisampling does not reach inside a shader:
//  - hard edges (joints, gaps) are smoothsteps as wide as a pixel (aaStep);
//  - fbm octaves finer than about two pixels fade out (fbm), and the
//    height uses the same faded values, so bumps flatten with distance;
//  - a joint narrower than a pixel blends towards its average share.
//
// Ported line for line from the GLSL the presets were published with (R1):
// a published preset never changes, a different look is a new @2.

/* eslint-disable @typescript-eslint/no-explicit-any -- three's TSL typings do not follow swizzles and chained maths */
/** A TSL node; three's typings are too partial to say which. */
export type TSLNode = any

type Op = (...args: TSLNode[]) => TSLNode
const { abs, clamp, cross, dFdx, dFdy, dot, float, floor, fract, length, max, min, mix, mod, normalize, pow, select, sin, smoothstep, sqrt, vec2, vec3 } = tsl as unknown as Record<string, Op>
type Width = PatternInput['width']
const Fn = tsl.Fn as unknown as (fn: (args: TSLNode[]) => TSLNode) => { setLayout(layout: object): Op }

/** What a preset gives. */
export type PatternOut = { albedo: TSLNode; rough: TSLNode; metal: TSLNode; height: TSLNode; emission: TSLNode }

/** What a preset reads. */
export type PatternInput = {
  /** What fwidth gives for a quantity of the preset: |d/dx| + |d/dy| over a pixel (the raster passes fwidth). */
  width: (x: TSLNode) => TSLNode
  /** Object-space position in metres. */
  pos: TSLNode
  /** Object-space normal, for the projection weights. */
  normal: TSLNode
  /** Metres one pixel covers at pos. */
  footprint: TSLNode
  /** The preset's parameters by name: colours as vec3, the rest as floats. */
  pm: Record<string, TSLNode>
}

// Hashes without sin, so every GPU gives the same pattern.
const hash13 = Fn(([p]) => {
  const p3 = fract(p.mul(0.1031)).toVar()
  p3.addAssign(dot(p3, p3.zyx.add(31.32)))
  return fract(p3.x.add(p3.y).mul(p3.z))
}).setLayout({ name: 'patHash13', type: 'float', inputs: [{ name: 'p', type: 'vec3' }] })

const hash12 = Fn(([p]) => {
  const p3 = fract(vec3(p.x, p.y, p.x).mul(0.1031)).toVar()
  p3.addAssign(dot(p3, p3.yzx.add(33.33)))
  return fract(p3.x.add(p3.y).mul(p3.z))
}).setLayout({ name: 'patHash12', type: 'float', inputs: [{ name: 'p', type: 'vec2' }] })

const noise = Fn(([p]) => {
  const i = floor(p).toVar()
  const f = fract(p).toVar()
  const u = f.mul(f).mul(f.mul(-2).add(3)).toVar()
  const h = (x: number, y: number, z: number) => hash13(i.add(vec3(x, y, z)))
  return mix(
    mix(mix(h(0, 0, 0), h(1, 0, 0), u.x), mix(h(0, 1, 0), h(1, 1, 0), u.x), u.y),
    mix(mix(h(0, 0, 1), h(1, 0, 1), u.x), mix(h(0, 1, 1), h(1, 1, 1), u.x), u.y),
    u.z,
  )
}).setLayout({ name: 'patNoise', type: 'float', inputs: [{ name: 'p', type: 'vec3' }] })

/**
 * fbm around 0.5 starting at freq cycles per metre. An octave whose
 * wavelength is under about two pixels (fp metres each) fades out.
 * Unrolled: octaves is a constant of the preset.
 */
function fbm(p: TSLNode, freq: number | TSLNode, fp: TSLNode, octaves: number): TSLNode {
  let sum: TSLNode = float(0)
  let norm = 0
  let amp = 0.5
  let f: TSLNode = float(freq)
  for (let i = 0; i < octaves; i++) {
    const fade = float(1).sub(smoothstep(0.25, 0.5, f.mul(fp)))
    sum = sum.add(fade.mul(amp).mul(noise(p.mul(f).add(i * 17.31)).sub(0.5)))
    norm += amp
    amp *= 0.5
    f = f.mul(2)
  }
  return sum.div(norm).add(0.5)
}

/** Distances to the nearest and second nearest cell point. Unrolled over the 27 neighbours. */
const voronoi = Fn(([p]) => {
  const i = floor(p).toVar()
  const f = fract(p).toVar()
  const d1 = float(8).toVar()
  const d2 = float(8).toVar()
  for (let z = -1; z <= 1; z++)
    for (let y = -1; y <= 1; y++)
      for (let x = -1; x <= 1; x++) {
        const g = vec3(x, y, z)
        const o = vec3(hash13(i.add(g)), hash13(i.add(g).add(11.1)), hash13(i.add(g).add(23.7)))
        const r = g.add(o).sub(f)
        const d = dot(r, r).toVar()
        // if (d < d1) { d2 = d1; d1 = d } else if (d < d2) d2 = d
        d2.assign(min(d2, max(d1, d)))
        d1.assign(min(d1, d))
      }
  return vec2(sqrt(d1), sqrt(d2))
}).setLayout({ name: 'patVoronoi', type: 'vec2', inputs: [{ name: 'p', type: 'vec3' }] })

const triWeights = (n: TSLNode): TSLNode => {
  const a = pow(abs(normalize(n)), vec3(4)).toVar()
  return a.div(a.x.add(a.y).add(a.z))
}

/** 0 below edge, 1 above, over the width of a pixel. */
const aaStep = (edge: TSLNode, x: TSLNode, width: Width): TSLNode => {
  const w = max(width(x), 1e-6)
  return smoothstep(edge.sub(w), edge.add(w), x)
}

/** A joint of width jw metres at distance d from the cell edge: 1 in the joint; narrower than a pixel, its share of the cell. */
const joint = (d: TSLNode, jw: TSLNode, share: TSLNode, width: Width): TSLNode => {
  const j = float(1).sub(aaStep(jw.mul(0.5), d, width))
  return mix(j, share, smoothstep(0.5, 1.5, width(d).div(jw)))
}

/**
 * The normal bent by the height's slope along the surface (surface
 * gradient), resolution independent; P and N in view space.
 */
export const bumpNormal = (P: TSLNode, N: TSLNode, h: TSLNode): TSLNode => {
  const dpdx = dFdx(P).toVar()
  const dpdy = dFdy(P).toVar()
  const r1 = cross(dpdy, N).toVar()
  const r2 = cross(N, dpdx).toVar()
  const det = dot(dpdx, r1).toVar()
  const grad = dFdx(h).mul(r1).add(dFdy(h).mul(r2)).div(det)
  return select(abs(det).lessThan(1e-20), N, normalize(N.sub(grad)))
}

const out = (albedo: TSLNode, rough: TSLNode, metal: TSLNode, height: TSLNode, emission: TSLNode = vec3(0)): PatternOut => ({
  albedo,
  rough: clamp(rough, 0, 1),
  metal: clamp(metal, 0, 1),
  height: float(height),
  emission,
})

const mix3 = (a: PatternOut, b: PatternOut, c: PatternOut, w: TSLNode): PatternOut => {
  const m = (k: keyof PatternOut) => a[k].mul(w.x).add(b[k].mul(w.y)).add(c[k].mul(w.z))
  return { albedo: m('albedo'), rough: m('rough'), metal: m('metal'), height: m('height'), emission: m('emission') }
}

/** A flat pattern f(uv, fp) projected along the three axes. */
const planar =
  (fn: (uv: TSLNode, fp: TSLNode, pm: PatternInput['pm'], width: Width) => PatternOut) =>
  ({ pos, normal, footprint, pm, width }: PatternInput): PatternOut => {
    const w = triWeights(normal).toVar()
    return mix3(fn(pos.zy, footprint, pm, width), fn(pos.xz, footprint, pm, width), fn(pos.xy, footprint, pm, width), w)
  }

const one = float(1)

/** Every preset's function. */
export const patternFns: Record<string, (input: PatternInput) => PatternOut> = {
  'paint@1': ({ pos, footprint: fp, pm }) => {
    const n = fbm(pos, 3, fp, 4)
    return out(pm.color.mul(n.mul(0.06).add(0.97)), pm.roughness, pm.metalness, fbm(pos, 60, fp, 3).mul(0.0002))
  },
  'plaster@1': ({ pos, footprint: fp, pm }) => {
    const n = fbm(pos, 1.5, fp, 5).toVar()
    const fine = fbm(pos, 12, fp, 4).toVar()
    const albedo = pm.color.mul(mix(one, n.mul(0.56).add(0.72), pm.mottle))
    return out(albedo, pm.roughness.add(fine.sub(0.5).mul(0.08)), pm.metalness, fine.mul(0.0015).add(n.mul(0.002)))
  },
  'concrete@1': ({ pos, footprint: fp, pm }) => {
    const n = fbm(pos, 2, fp, 5)
    const fine = fbm(pos, 25, fp, 3)
    const pores = one.sub(smoothstep(0.12, 0.18, noise(pos.mul(60)))).mul(one.sub(smoothstep(0.25, 0.5, fp.mul(60)))).toVar()
    const stain = smoothstep(0.55, 0.8, fbm(pos.add(7.3), 0.35, fp, 4)).mul(pm.stains)
    const albedo = pm.color.mul(n.mul(0.3).add(0.85)).mul(one.sub(stain.mul(0.45))).mul(one.sub(pores.mul(0.3)))
    return out(albedo, pm.roughness, pm.metalness, fine.mul(0.0015).sub(pores.mul(0.0015)))
  },
  'stone_brick@1': planar((uv, fp, pm, width) => {
    const h = pm.brick_size
    const w = h.mul(2)
    const jw = clamp(h.mul(0.06), 0.004, 0.02).toVar()
    const cy = uv.y.div(h).toVar()
    const cx = uv.x.div(w).add(mod(floor(cy), 2).mul(0.5)).toVar()
    const id = floor(vec2(cx, cy))
    const fx = fract(cx).toVar()
    const fy = fract(cy).toVar()
    const d = min(min(fx, one.sub(fx)).mul(w), min(fy, one.sub(fy)).mul(h)).toVar()
    const j = joint(d, jw, jw.div(h).add(jw.div(w)), width).toVar()
    const tint = hash12(id).toVar()
    const n = fbm(vec3(uv, tint.mul(13)), 6, fp, 4).toVar()
    const brick = pm.color.mul(tint.mul(0.3).add(0.82)).mul(n.mul(0.2).add(0.9))
    const bevel = smoothstep(0, 0.015, d.sub(jw.mul(0.5)))
    return out(mix(brick, pm.mortar_color, j), mix(pm.roughness, pm.roughness.add(0.1), j), pm.metalness, one.sub(bevel).mul(-0.01).add(n.mul(0.002)))
  }),
  'stone@1': ({ pos, footprint, pm, width }) => {
    const s = pm.scale
    const q = pos.div(s).toVar()
    const fp = footprint.div(s)
    const n = fbm(q, 1, fp, 6).toVar()
    const v = voronoi(q.mul(2).add(n.sub(0.5).mul(0.5))).toVar()
    const gap = v.y.sub(v.x).toVar()
    const cw = max(width(gap), 1e-4).toVar()
    const crack = one.sub(smoothstep(0.02, cw.add(0.02), gap)).mul(one.sub(smoothstep(0.5, 1, cw.div(0.05)))).toVar()
    const albedo = pm.color.mul(n.mul(0.6).add(0.7)).mul(one.sub(crack.mul(0.5)))
    return out(albedo, pm.roughness, pm.metalness, s.mul(n.mul(0.03).sub(crack.mul(0.01))))
  },
  'marble@1': ({ pos, footprint, pm, width }) => {
    const s = pm.scale
    const q = pos.div(s).toVar()
    const fp = footprint.div(s)
    const turb = fbm(q, 1.5, fp, 6).toVar()
    const t = q.x.mul(1.3).add(q.y.mul(0.4)).add(turb.mul(4)).toVar()
    const raw = pow(one.sub(abs(sin(t.mul(3.14159265)))), 6)
    const vein = mix(raw, 0.15, smoothstep(0.3, 0.8, width(t))).toVar()
    const albedo = mix(pm.color.mul(turb.mul(0.1).add(0.95)), pm.vein_color, vein.mul(0.85))
    return out(albedo, pm.roughness.add(vein.mul(0.05)), pm.metalness, vein.mul(-0.0005))
  },
  'tile@1': planar((uv, _fp, pm, width) => {
    const s = pm.tile_size
    const jw = float(0.004)
    const c = uv.div(s).toVar()
    const id = floor(c)
    const f = fract(c).toVar()
    const d = min(min(f.x, one.sub(f.x)), min(f.y, one.sub(f.y))).mul(s).toVar()
    const j = joint(d, jw, jw.mul(2).div(s), width).toVar()
    const tint = hash12(id)
    const bevel = smoothstep(0, 0.004, d.sub(jw.mul(0.5)))
    return out(mix(pm.color.mul(tint.mul(0.08).add(0.96)), pm.grout_color, j), mix(pm.roughness, 0.85, j), pm.metalness, one.sub(bevel).mul(-0.003))
  }),
  'wood@1': ({ pos, footprint, pm, width }) => {
    const s = pm.scale
    const q = pos.div(s).toVar()
    const fp = footprint.div(s).toVar()
    const warp = fbm(q.mul(vec3(1, 0.15, 1)), 2, fp, 4)
    const r = length(q.xz.add(0.3)).mul(8).add(warp.mul(3)).toVar()
    const raw = smoothstep(0, 0.25, fract(r)).mul(one.sub(smoothstep(0.6, 1, fract(r))))
    const ring = mix(raw, 0.55, smoothstep(0.3, 0.8, width(r))).toVar()
    const grain = fbm(q.mul(vec3(8, 0.5, 8)), 4, fp.mul(8), 4).toVar()
    const albedo = pm.color.mul(mix(0.72, 1.12, ring)).mul(grain.mul(0.16).add(0.92))
    return out(albedo, pm.roughness, pm.metalness, ring.mul(0.0004).add(grain.mul(0.0003)))
  },
  'wood_plank@1': planar((uv, fp, pm, width) => {
    const wv = pm.plank_width
    const len = wv.mul(8).toVar()
    const row = floor(uv.y.div(wv)).toVar()
    const x = uv.x.div(len).add(hash12(vec2(row, 3.7))).toVar()
    const id = vec2(floor(x), row)
    const fx = fract(x).toVar()
    const fy = fract(uv.y.div(wv)).toVar()
    const d = min(min(fx, one.sub(fx)).mul(len), min(fy, one.sub(fy)).mul(wv)).toVar()
    const gap = joint(d, float(0.003), float(0.003).div(wv).add(float(0.003).div(len)), width).toVar()
    const tint = hash12(id.add(1.3)).toVar()
    const grain = fbm(vec3(uv.x.mul(0.6), fy.mul(wv).mul(25), tint.mul(9)), 4, fp.mul(25), 5).toVar()
    const plank = pm.color.mul(tint.mul(0.3).add(0.85)).mul(grain.mul(0.3).add(0.85))
    return out(mix(plank, pm.color.mul(0.25), gap), pm.roughness.add(gap.mul(0.1)), pm.metalness, gap.mul(-0.002).add(grain.mul(0.0003)))
  }),
  'metal@1': ({ pos, footprint: fp, pm }) => {
    const n = fbm(pos, 3, fp, 5).toVar()
    const r = fbm(pos.add(3.1), 1.5, fp, 6)
    const mask = smoothstep(one.sub(pm.rust), float(1.15).sub(pm.rust), r).toVar()
    const rust = vec3(0.32, 0.13, 0.05).mul(n.mul(0.6).add(0.7))
    const albedo = mix(pm.color.mul(n.mul(0.2).add(0.9)), rust, mask)
    const rough = mix(pm.roughness.add(n.sub(0.5).mul(0.1)), 0.92, mask)
    return out(albedo, rough, mix(pm.metalness, 0, mask), mask.mul(0.0008).mul(n).add(fbm(pos, 40, fp, 3).mul(0.0002)))
  },
  'fabric@1': planar((uv, fp, pm, width) => {
    const s = pm.scale
    const c = uv.div(s).toVar()
    const f = fract(c).toVar()
    const over = mod(floor(c.x).add(floor(c.y)), 2)
    const raw = select(over.greaterThan(0.5), sin(f.x.mul(3.14159265)), sin(f.y.mul(3.14159265)))
    const fw = width(c).toVar()
    const fade = one.sub(smoothstep(0.25, 0.5, max(fw.x, fw.y))).toVar()
    const h = mix(0.6, raw, fade).toVar()
    const n = fbm(vec3(uv, 0), 8, fp, 4)
    return out(pm.color.mul(h.mul(0.25).add(0.8)).mul(n.mul(0.1).add(0.95)), pm.roughness, pm.metalness, s.mul(0.3).mul(h).mul(fade))
  }),
  'dirt@1': ({ pos, footprint: fp, pm }) => {
    const n = fbm(pos, 1, fp, 6).toVar()
    const fine = fbm(pos, 15, fp, 4).toVar()
    const v = voronoi(pos.mul(25))
    const pebble = one.sub(smoothstep(0.15, 0.25, v.x)).mul(one.sub(smoothstep(0.25, 0.5, fp.mul(25)))).toVar()
    const albedo = pm.color.mul(n.mul(0.5).add(0.75)).mul(fine.mul(0.2).add(0.9)).mul(pebble.mul(0.25).add(1))
    return out(albedo, pm.roughness, pm.metalness, n.mul(0.01).add(fine.mul(0.002)).add(pebble.mul(0.003)))
  },
  'grass@1': ({ pos, footprint: fp, pm }) => {
    const n = fbm(pos, 0.8, fp, 5)
    const blades = fbm(pos, 60, fp, 3).toVar()
    const dry = smoothstep(0.55, 0.8, fbm(pos.add(5), 0.3, fp, 4))
    const tint = mix(pm.color, pm.color.mul(vec3(1.35, 1.15, 0.6)), dry)
    return out(tint.mul(n.mul(0.5).add(0.7)).mul(blades.mul(0.4).add(0.8)), pm.roughness, pm.metalness, blades.mul(0.004).add(n.mul(0.01)))
  },
  'emissive@1': ({ pm }) => out(pm.color, pm.roughness, pm.metalness, 0, pm.color.mul(pm.strength)),
}
