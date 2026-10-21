import { abs, clamp, cross, dFdx, dFdy, dot, float, floor, fract, Fn, fwidth, length, max, min, mix, modelScale, normalize, normalLocal, positionLocal, positionView, normalView, pow, select, sin, smoothstep, uniform, varying, vec3 } from 'three/tsl'
import { Color, MeshPhysicalNodeMaterial } from 'three/webgpu'
import { completeParams, materialEntry } from '../../src/creative-scene/materials/catalog.ts'

// Group 2 (R0 task 3.2): tile@1 (projected along three axes) and marble@1
// (solid fbm noise) written in TSL from materials/patterns.ts, line for
// line, so the two can be told apart only by the compilers. Every helper
// keeps the GLSL name without its pat prefix.

/* eslint-disable @typescript-eslint/no-explicit-any -- TSL nodes are loosely typed in three's typings */
type N = any

// vPatPos / vPatNormal: object-space position in true metres, and the normal for the weights.
const patPos: N = varying(positionLocal.mul(modelScale), 'vPatPos')
const patNormal: N = varying(normalLocal.div(modelScale), 'vPatNormal')

const hash13 = Fn(([p]: [N]) => {
  const p3 = fract(p.mul(0.1031)).toVar()
  p3.addAssign(dot(p3, p3.zyx.add(31.32)))
  return fract(p3.x.add(p3.y).mul(p3.z))
}).setLayout({ name: 'patHash13', type: 'float', inputs: [{ name: 'p', type: 'vec3' }] })

const hash12 = Fn(([p]: [N]) => {
  const p3 = fract(vec3(p.x, p.y, p.x).mul(0.1031)).toVar()
  p3.addAssign(dot(p3, p3.yzx.add(33.33)))
  return fract(p3.x.add(p3.y).mul(p3.z))
}).setLayout({ name: 'patHash12', type: 'float', inputs: [{ name: 'p', type: 'vec2' }] })

const noise = Fn(([p]: [N]) => {
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

/** Metres one pixel covers here. */
const footprint = (): N => max(length(fwidth(patPos)), 1e-6)

/** fbm around 0.5 from freq cycles per metre; octaves under about two pixels fade out (unrolled: octaves is a constant). */
function fbm(p: N, freq: number, fp: N, octaves: number): N {
  let sum: N = float(0)
  let norm = 0
  let amp = 0.5
  let f = freq
  for (let i = 0; i < octaves; i++) {
    const fade = float(1).sub(smoothstep(0.25, 0.5, fp.mul(f)))
    sum = sum.add(fade.mul(amp).mul(noise(p.mul(f).add(i * 17.31)).sub(0.5)))
    norm += amp
    amp *= 0.5
    f *= 2
  }
  return sum.div(norm).add(0.5)
}

const triWeights = (n: N): N => {
  const a = pow(abs(normalize(n)), vec3(4)).toVar()
  return a.div(a.x.add(a.y).add(a.z))
}

/** 0 below edge, 1 above, over the width of a pixel. */
const aaStep = (edge: N, x: N): N => {
  const w = max(fwidth(x), 1e-6)
  return smoothstep(edge.sub(w), edge.add(w), x)
}

/** A joint of width jw at distance d from the cell edge: 1 in the joint, its share when under a pixel. */
const joint = (d: N, jw: number, share: N): N => {
  const j = float(1).sub(aaStep(float(jw * 0.5), d))
  return mix(j, share, smoothstep(0.5, 1.5, fwidth(d).div(jw)))
}

/** The normal bent by the height's slope along the surface; P and N in view space. */
const bumpNormal = (P: N, Nn: N, h: N): N => {
  const dpdx = dFdx(P).toVar()
  const dpdy = dFdy(P).toVar()
  const r1 = cross(dpdy, Nn).toVar()
  const r2 = cross(Nn, dpdx).toVar()
  const det = dot(dpdx, r1).toVar()
  const grad = dFdx(h).mul(r1).add(dFdy(h).mul(r2)).div(det)
  return select(abs(det).lessThan(1e-20), Nn, normalize(Nn.sub(grad)))
}

type Out = { albedo: N; rough: N; metal: N; height: N }
const out = (albedo: N, rough: N, metal: N, height: N): Out => ({ albedo, rough: clamp(rough, 0, 1), metal: clamp(metal, 0, 1), height })
const mix3 = (a: Out, b: Out, c: Out, w: N): Out => ({
  albedo: a.albedo.mul(w.x).add(b.albedo.mul(w.y)).add(c.albedo.mul(w.z)),
  rough: a.rough.mul(w.x).add(b.rough.mul(w.y)).add(c.rough.mul(w.z)),
  metal: a.metal.mul(w.x).add(b.metal.mul(w.y)).add(c.metal.mul(w.z)),
  height: a.height.mul(w.x).add(b.height.mul(w.y)).add(c.height.mul(w.z)),
})

type Params = Record<string, N>

const presets: Record<string, (pm: Params) => Out> = {
  'marble@1': (pm) => {
    const s = pm.scale
    const q = patPos.div(s).toVar()
    const fp = footprint().div(s)
    const turb = fbm(q, 1.5, fp, 6).toVar()
    const t = q.x.mul(1.3).add(q.y.mul(0.4)).add(turb.mul(4)).toVar()
    let vein: N = pow(float(1).sub(abs(sin(t.mul(3.14159265)))), 6)
    vein = mix(vein, 0.15, smoothstep(0.3, 0.8, fwidth(t))).toVar()
    const albedo = mix(pm.color.mul(turb.mul(0.1).add(0.95)), pm.vein_color, vein.mul(0.85))
    return out(albedo, pm.roughness.add(vein.mul(0.05)), pm.metalness, vein.mul(-0.0005))
  },
  'tile@1': (pm) => {
    const tile = (uv: N): Out => {
      const s = pm.tile_size
      const jw = 0.004
      const c = uv.div(s).toVar()
      const id = floor(c)
      const f = fract(c).toVar()
      const d = min(min(f.x, float(1).sub(f.x)), min(f.y, float(1).sub(f.y))).mul(s).toVar()
      const j = joint(d, jw, float(2 * jw).div(s)).toVar()
      const tint = hash12(id)
      const bevel = smoothstep(0, 0.004, d.sub(jw * 0.5))
      return out(mix(pm.color.mul(tint.mul(0.08).add(0.96)), pm.grout_color, j), mix(pm.roughness, 0.85, j), pm.metalness, float(1).sub(bevel).mul(-0.003))
    }
    const w = triWeights(patNormal).toVar()
    return mix3(tile(patPos.zy), tile(patPos.xz), tile(patPos.xy), w)
  },
}

export const tslPresets = Object.keys(presets)

/** A MeshPhysicalNodeMaterial drawing a preset with its parameters, as makeMaterial does with the GLSL. */
export function tslPatternMaterial(id: string, params?: Record<string, unknown>): MeshPhysicalNodeMaterial {
  const entry = materialEntry(id)
  const build = presets[id]
  if (!entry || !build) throw new Error(`no TSL port of ${id}`)
  const complete = completeParams(entry, params)
  const pm: Params = Object.fromEntries(Object.entries(entry.params).map(([k, spec]) => [k, uniform(spec.type === 'color' ? new Color(complete[k] as string) : (complete[k] as number))]))
  const o = build(pm)
  const material = new MeshPhysicalNodeMaterial()
  material.colorNode = o.albedo
  material.roughnessNode = o.rough
  material.metalnessNode = o.metal
  material.normalNode = bumpNormal(positionView, normalView, o.height)
  material.name = id
  return material
}
