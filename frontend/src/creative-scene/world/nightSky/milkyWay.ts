import { ClampToEdgeWrapping, HalfFloatType, LinearFilter, RepeatWrapping } from 'three'
import * as tsl from 'three/tsl'
import { NodeMaterial, QuadMesh, RenderTarget, type WebGPURenderer } from 'three/webgpu'
import type { TSLNode } from '../../materials/patterns.ts'
import { luminance, sunTint, type Vec3 } from '../atmosphere/constants.ts'

// The Milky Way, made up (W3 design「程序化银河」, owner 2026-10-23): no
// all-sky map with a clean licence was to be had. In galactic coordinates
// (l, b): the disc's band, brighter toward the centre; the bulge; clumps of
// star clouds; the dust of the Great Rift along the plane from Cygnus to
// Sagittarius, the Ophiuchus clouds and the Coalsack; and the two
// Magellanic Clouds. Where it is in the sky is real, its detail is not.
// Written twice, alike: milkyWayAt on the CPU (the stars' constant term,
// the unit tests) and the TSL pass baked once per device into an
// equirectangular map, u = (l + 180°) / 360°, v = (b + 90°) / 180°.
// e2e「W3 milky way drift」 holds the two together.

/** The map's size: 0.18° a texel, the band's structure being a few degrees across. */
export const MILKY_WAY_SIZE = { w: 2048, h: 1024 } as const

const RAD = Math.PI / 180

// --- the model, on the CPU --------------------------------------------------------

const fract = (x: number) => x - Math.floor(x)

/** Dave Hoskins's hash without sine, 3 → 1: small, exact enough in float32 that both GPU backends agree. */
function hash(x: number, y: number, z: number): number {
  let a = fract(x * 0.1031)
  let b = fract(y * 0.1031)
  let c = fract(z * 0.1031)
  const d = a * (c + 31.32) + b * (b + 31.32) + c * (a + 31.32)
  a += d
  b += d
  c += d
  return fract((a + b) * c)
}

/** Value noise, 0…1, smooth between the integer lattice's hashed corners. */
function noise(x: number, y: number, z: number): number {
  const [ix, iy, iz] = [Math.floor(x), Math.floor(y), Math.floor(z)]
  const s = (t: number) => t * t * (3 - 2 * t)
  const [fx, fy, fz] = [s(x - ix), s(y - iy), s(z - iz)]
  const lerp = (a: number, b: number, t: number) => a + (b - a) * t
  const h = (dx: number, dy: number, dz: number) => hash(ix + dx, iy + dy, iz + dz)
  return lerp(
    lerp(lerp(h(0, 0, 0), h(1, 0, 0), fx), lerp(h(0, 1, 0), h(1, 1, 0), fx), fy),
    lerp(lerp(h(0, 0, 1), h(1, 0, 1), fx), lerp(h(0, 1, 1), h(1, 1, 1), fx), fy),
    fz,
  )
}

/** Four octaves of noise, 0…1. */
function fbm(x: number, y: number, z: number): number {
  let sum = 0
  let amplitude = 0.5
  for (let i = 0; i < 4; i++) {
    sum += amplitude * noise(x, y, z)
    x = x * 2.03 + 1.7
    y = y * 2.03 + 9.2
    z = z * 2.03 + 5.3
    amplitude *= 0.5
  }
  return sum / 0.9375
}

/** A galactic direction as a unit vector: x toward the centre, y toward l = 90°, z toward the north galactic pole. */
const galacticVector = (lDeg: number, bDeg: number): Vec3 => [Math.cos(bDeg * RAD) * Math.cos(lDeg * RAD), Math.cos(bDeg * RAD) * Math.sin(lDeg * RAD), Math.sin(bDeg * RAD)]

/** The bulge's colour (about 4500 K) and the disc's (about 6000 K), beside the sun's white. */
const BULGE: Vec3 = sunTint(4500)
const DISC: Vec3 = sunTint(6000)
/** How much more the dust dims blue than red. */
const REDDENING: Vec3 = [0.85, 1, 1.2]
/** Divides the model so its brightest, the Sagittarius star cloud, is about 1. */
const PEAK = 1.05

/** The Magellanic Clouds and the dust patches off the plane: centre (l, b), radius (degrees), weight. */
const LMC = { at: galacticVector(280.5, -32.9), sigma: 3.2, weight: 0.45 }
const SMC = { at: galacticVector(302.8, -44.3), sigma: 1.4, weight: 0.3 }
const OPHIUCHUS = { at: galacticVector(4, 9), sigma: 6, weight: 1.1 }
const COALSACK = { at: galacticVector(301, -1), sigma: 2.2, weight: 1.6 }

/** The Milky Way's relative radiance toward galactic (l, b) in degrees, linear RGB, about 1 at its brightest. */
export function milkyWayAt(lDeg: number, bDeg: number): Vec3 {
  const l = ((((lDeg + 180) % 360) + 360) % 360) - 180
  const v = galacticVector(lDeg, bDeg)
  // The angle from the chord, as the GPU works it out: acos near 1 loses float32's precision, and past −1 is NaN.
  const blob = (c: { at: Vec3; sigma: number }) => {
    const chord = Math.hypot(v[0] - c.at[0], v[1] - c.at[1], v[2] - c.at[2])
    const theta = (2 * Math.asin(Math.min(1, chord / 2))) / RAD
    return Math.exp(-(theta * theta) / (2 * c.sigma * c.sigma))
  }
  // The disc: a sech profile across the plane, thicker and brighter toward the centre.
  const height = 4 + 6 * Math.exp(-((l / 45) ** 2))
  const x = Math.abs(bDeg) / height
  const disc = ((0.3 + 0.7 * Math.exp(-((l / 70) ** 2))) * 2) / (Math.exp(x) + Math.exp(-x))
  const bulge = 1.3 * Math.exp(-((l / 11) ** 2) - (bDeg / 7) ** 2)
  const n = fbm(v[0] * 7, v[1] * 7, v[2] * 7)
  const clumps = 0.25 + 1.5 * n * n
  // Dust: the Great Rift, a lane a little north of the plane from Cygnus (l ≈ 80°) to Sagittarius (l ≈ −10°);
  // mottling over the inner disc; the Ophiuchus clouds and the Coalsack.
  const d = fbm(v[0] * 14 + 3.1, v[1] * 14 + 7.7, v[2] * 14 + 1.3)
  // The lane wanders: its middle moves with the dust's own noise, so its edges are ragged.
  const lane = Math.exp(-(((bDeg - 1.2 - 3 * (d - 0.5)) / 2.6) ** 2)) * Math.exp(-(((l - 35) / 55) ** 4))
  const mottle = Math.exp(-((bDeg / 7) ** 2)) * Math.exp(-(((l - 20) / 70) ** 4))
  const patches = OPHIUCHUS.weight * blob(OPHIUCHUS) + COALSACK.weight * blob(COALSACK)
  const tau = (2.6 * lane + patches) * (0.3 + 1.4 * d) + 1.8 * mottle * d * d
  const glow = disc * clumps + bulge
  const clouds = LMC.weight * blob(LMC) + SMC.weight * blob(SMC)
  const mix = bulge / Math.max(glow, 1e-6)
  return [0, 1, 2].map((i) => ((DISC[i] * (1 - mix) + BULGE[i] * mix) * glow * Math.exp(-tau * REDDENING[i]) + DISC[i] * clouds) / PEAK) as Vec3
}

let mean: number | null = null

/** The map's mean luminance over the whole sphere, sampled every 2°, worked out once: the Milky Way's share of the stars' constant term. */
export function milkyWayMean(): number {
  if (mean === null) {
    let sum = 0
    let weight = 0
    for (let b = -89; b < 90; b += 2) {
      const w = Math.cos(b * RAD)
      for (let l = -179; l < 180; l += 2) {
        sum += luminance(milkyWayAt(l, b)) * w
        weight += w
      }
    }
    mean = sum / weight
  }
  return mean
}

// --- the same model, baked on the GPU ---------------------------------------------

type Op = (...args: TSLNode[]) => TSLNode
const { abs, acos, asin, atan, clamp, cos, dot, exp, float, floor, length, max, min, mix, sin, texture, uv, vec2, vec3, vec4 } = tsl as unknown as Record<string, Op>
const Fn = tsl.Fn as unknown as (fn: () => TSLNode) => () => TSLNode

const fractNode = (x: TSLNode) => x.sub(floor(x))
/** x², not pow(x, 2): pow of a negative base is undefined in GLSL and WGSL (SwiftShader returns garbage). */
const sq = (x: TSLNode) => x.mul(x)

function hashNode(p: TSLNode): TSLNode {
  const q = fractNode(p.mul(0.1031)).toVar()
  const d = dot(q, vec3(q.z, q.y, q.x).add(31.32))
  const r = q.add(d)
  return fractNode(r.x.add(r.y).mul(r.z))
}

function noiseNode(p: TSLNode): TSLNode {
  const i = floor(p).toVar()
  const f = p.sub(i)
  const s = f.mul(f).mul(vec3(3).sub(f.mul(2))).toVar()
  const h = (dx: number, dy: number, dz: number) => hashNode(i.add(vec3(dx, dy, dz)))
  return mix(
    mix(mix(h(0, 0, 0), h(1, 0, 0), s.x), mix(h(0, 1, 0), h(1, 1, 0), s.x), s.y),
    mix(mix(h(0, 0, 1), h(1, 0, 1), s.x), mix(h(0, 1, 1), h(1, 1, 1), s.x), s.y),
    s.z,
  )
}

function fbmNode(p: TSLNode): TSLNode {
  let x = p
  let sum: TSLNode = float(0)
  let amplitude = 0.5
  for (let i = 0; i < 4; i++) {
    sum = sum.add(noiseNode(x).mul(amplitude))
    x = x.mul(2.03).add(vec3(1.7, 9.2, 5.3))
    amplitude *= 0.5
  }
  return sum.div(0.9375)
}

const blobNode = (v: TSLNode, c: { at: Vec3; sigma: number }) => {
  const theta = asin(min(length(v.sub(vec3(...c.at))).mul(0.5), 1)).mul(2 / RAD)
  return exp(theta.mul(theta).div(-2 * c.sigma * c.sigma))
}

/** milkyWayAt in TSL: a fragment of the map, (l, b) from its uv. Inside a TSL function, as the skies are: its variables belong to the fragment. */
const bakeNode = Fn(() => {
  const p = uv()
  const l = p.x.mul(360).sub(180).toVar()
  const b = p.y.mul(180).sub(90).toVar()
  const v = vec3(cos(b.mul(RAD)).mul(cos(l.mul(RAD))), cos(b.mul(RAD)).mul(sin(l.mul(RAD))), sin(b.mul(RAD))).toVar()
  const height = float(4).add(exp(sq(l.div(45)).negate()).mul(6))
  const x = abs(b).div(height).toVar()
  const disc = float(0.3).add(exp(sq(l.div(70)).negate()).mul(0.7)).mul(2).div(exp(x).add(exp(x.negate())))
  const bulge = exp(sq(l.div(11)).negate().sub(sq(b.div(7)))).mul(1.3).toVar()
  const n = fbmNode(v.mul(7)).toVar()
  const clumps = float(0.25).add(n.mul(n).mul(1.5))
  const d = fbmNode(v.mul(14).add(vec3(3.1, 7.7, 1.3))).toVar()
  const lane = exp(sq(b.sub(1.2).sub(d.sub(0.5).mul(3)).div(2.6)).negate()).mul(exp(sq(sq(l.sub(35).div(55))).negate()))
  const mottle = exp(sq(b.div(7)).negate()).mul(exp(sq(sq(l.sub(20).div(70))).negate()))
  const patches = blobNode(v, OPHIUCHUS).mul(OPHIUCHUS.weight).add(blobNode(v, COALSACK).mul(COALSACK.weight))
  const tau = lane.mul(2.6).add(patches).mul(float(0.3).add(d.mul(1.4))).add(mottle.mul(d).mul(d).mul(1.8)).toVar()
  const glow = disc.mul(clumps).add(bulge).toVar()
  const clouds = blobNode(v, LMC).mul(LMC.weight).add(blobNode(v, SMC).mul(SMC.weight))
  const share = bulge.div(max(glow, 1e-6))
  const colour = mix(vec3(...DISC), vec3(...BULGE), share)
  const dimmed = exp(vec3(...REDDENING).mul(tau).negate())
  return vec4(colour.mul(glow).mul(dimmed).add(vec3(...DISC).mul(clouds)).div(PEAK), 1)
})

/** The baked map: made on first use for a device (gpu.cache), and with it lost. */
export function bakeMilkyWay(renderer: WebGPURenderer): RenderTarget {
  const target = new RenderTarget(MILKY_WAY_SIZE.w, MILKY_WAY_SIZE.h, { type: HalfFloatType, depthBuffer: false })
  target.texture.minFilter = LinearFilter
  target.texture.magFilter = LinearFilter
  target.texture.wrapS = RepeatWrapping
  target.texture.wrapT = ClampToEdgeWrapping
  target.texture.generateMipmaps = false
  const material = new NodeMaterial()
  material.fragmentNode = bakeNode()
  const quad = new QuadMesh(material)
  const previous = renderer.getRenderTarget()
  renderer.setRenderTarget(target)
  quad.render(renderer)
  renderer.setRenderTarget(previous)
  material.dispose()
  return target
}

/** The map toward a galactic unit vector g (x the centre, z the north pole), relative radiance. */
export function sampleMilkyWay(map: RenderTarget, g: TSLNode): TSLNode {
  const l = atan(g.y, g.x)
  const b = acos(clamp(g.z, -1, 1)).negate().add(Math.PI / 2)
  return texture(map.texture)
    .sample(vec2(l.div(2 * Math.PI).add(0.5), b.div(Math.PI).add(0.5)))
    .level(0).rgb
}
