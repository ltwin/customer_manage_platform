// W2 spike: Hillaire 2020 on the CPU in double precision, discretised as
// the GPU passes are, so the two can be told apart by precision alone; and
// the CPU parts the product needs anyway (the sun's transmittance, the
// incident meter's sky irradiance).
import { BOTTOM, MIE_G, MULTI, PLANET_OFFSET, SKYVIEW, TOP, TRANSMITTANCE, coefficients, type Atmosphere, type Vec3 } from './atmo.ts'

const dot = (a: Vec3, b: Vec3) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2]
const len = (a: Vec3) => Math.sqrt(dot(a, a))
const add = (a: Vec3, b: Vec3, s = 1): Vec3 => [a[0] + b[0] * s, a[1] + b[1] * s, a[2] + b[2] * s]
const clamp01 = (x: number) => Math.min(1, Math.max(0, x))

/** Nearest non-negative hit of a ray with the sphere about the origin, or −1. */
export function raySphere(ro: Vec3, rd: Vec3, radius: number): number {
  const b = dot(ro, rd)
  const c = dot(ro, ro) - radius * radius
  const d = b * b - c
  if (d < 0) return -1
  const s = Math.sqrt(d)
  const t0 = -b - s
  const t1 = -b + s
  if (t0 >= 0) return t0
  if (t1 >= 0) return t1
  return -1
}

export type Medium = { scattering: Vec3; extinction: Vec3; rayleigh: Vec3; mie: number }

export function medium(a: Atmosphere, heightKm: number): Medium {
  const c = coefficients(a)
  const h = Math.max(0, heightKm)
  const dR = Math.exp(-h / 8)
  const dM = Math.exp(-h / 1.2)
  const dO = Math.max(0, 1 - Math.abs(h - 25) / 15)
  const rayleigh = c.rayleigh.map((x) => x * dR) as Vec3
  const mie = c.mieScattering * dM
  const mieAbs = c.mieAbsorption.map((x) => x * dM)
  const scattering = rayleigh.map((x) => x + mie) as Vec3
  const extinction = scattering.map((x, i) => x + mieAbs[i] + c.ozone[i] * dO) as Vec3
  return { scattering, extinction, rayleigh, mie }
}

/** Transmittance LUT mapping (Bruneton 2017, as in the sample): uv → (r, mu). */
export function transmittanceParams(u: number, v: number): { r: number; mu: number } {
  const H = Math.sqrt(TOP * TOP - BOTTOM * BOTTOM)
  const rho = H * v
  const r = Math.sqrt(rho * rho + BOTTOM * BOTTOM)
  const dMin = TOP - r
  const dMax = rho + H
  const d = dMin + u * (dMax - dMin)
  const mu = d === 0 ? 1 : Math.min(1, Math.max(-1, (H * H - rho * rho - d * d) / (2 * r * d)))
  return { r, mu }
}

export function transmittanceUv(r: number, mu: number): [number, number] {
  const H = Math.sqrt(TOP * TOP - BOTTOM * BOTTOM)
  const rho = Math.sqrt(Math.max(0, r * r - BOTTOM * BOTTOM))
  const disc = r * r * (mu * mu - 1) + TOP * TOP
  const d = Math.max(0, -r * mu + Math.sqrt(Math.max(0, disc)))
  const dMin = TOP - r
  const dMax = rho + H
  return [(d - dMin) / (dMax - dMin), rho / H]
}

/** Transmittance from radius r along zenith cosine mu to the top of the atmosphere (the planet not tested). */
export function transmittance(a: Atmosphere, r: number, mu: number, steps = TRANSMITTANCE.steps, offset = 0.3): Vec3 {
  const ro: Vec3 = [0, r, 0]
  const rd: Vec3 = [Math.sqrt(Math.max(0, 1 - mu * mu)), mu, 0]
  const tMax = raySphere(ro, rd, TOP)
  const dt = tMax / steps
  const depth: Vec3 = [0, 0, 0]
  for (let s = 0; s < steps; s++) {
    const p = add(ro, rd, tMax * ((s + offset) / steps))
    const m = medium(a, len(p) - BOTTOM)
    for (let i = 0; i < 3; i++) depth[i] += m.extinction[i] * dt
  }
  return depth.map((x) => Math.exp(-x)) as Vec3
}

/** A table sampled bilinearly as a GPU would (clamp to edge, texel centres at (i + 0.5) / n). */
export class Table {
  readonly w: number
  readonly h: number
  readonly data: Float64Array
  constructor(w: number, h: number, data: Float64Array) {
    this.w = w
    this.h = h
    this.data = data
  }
  static make(w: number, h: number, f: (u: number, v: number) => Vec3): Table {
    const data = new Float64Array(w * h * 3)
    for (let y = 0; y < h; y++) for (let x = 0; x < w; x++) data.set(f((x + 0.5) / w, (y + 0.5) / h), (y * w + x) * 3)
    return new Table(w, h, data)
  }
  sample(u: number, v: number): Vec3 {
    const fx = Math.min(this.w - 1, Math.max(0, u * this.w - 0.5))
    const fy = Math.min(this.h - 1, Math.max(0, v * this.h - 0.5))
    const x0 = Math.floor(fx), y0 = Math.floor(fy)
    const x1 = Math.min(this.w - 1, x0 + 1), y1 = Math.min(this.h - 1, y0 + 1)
    const tx = fx - x0, ty = fy - y0
    const at = (x: number, y: number, i: number) => this.data[(y * this.w + x) * 3 + i]
    return [0, 1, 2].map((i) => (at(x0, y0, i) * (1 - tx) + at(x1, y0, i) * tx) * (1 - ty) + (at(x0, y1, i) * (1 - tx) + at(x1, y1, i) * tx) * ty) as Vec3
  }
}

export const transmittanceTable = (a: Atmosphere, w = TRANSMITTANCE.w, h = TRANSMITTANCE.h) =>
  Table.make(w, h, (u, v) => {
    const { r, mu } = transmittanceParams(u, v)
    return transmittance(a, r, mu)
  })

export const lookupTransmittance = (t: Table, r: number, mu: number): Vec3 => {
  const [u, v] = transmittanceUv(r, mu)
  return t.sample(u, v)
}

const fromSubUvs = (u: number, n: number) => (u - 0.5 / n) * (n / (n - 1))
const toSubUvs = (u: number, n: number) => (u + 0.5 / n) * ((n - 1) / n)

const rayleighPhase = (c: number) => (3 / (16 * Math.PI)) * (1 + c * c)
const hgPhase = (g: number, c: number) => {
  const denom = 1 + g * g - 2 * g * c
  return (1 - g * g) / (4 * Math.PI * denom * Math.sqrt(denom))
}

type Integration = { L: Vec3; multiAs1: Vec3 }

/**
 * IntegrateScatteredLuminance of the sample (sun illuminance 1), with the
 * transmittance and multiple scattering from tables: ms null in the
 * multiple-scattering pass, where both phases are isotropic.
 */
function integrate(a: Atmosphere, ro: Vec3, rd: Vec3, sun: Vec3, steps: number, ground: boolean, tr: Table, ms: Table | null): Integration {
  const tBottom = raySphere(ro, rd, BOTTOM)
  const tTop = raySphere(ro, rd, TOP)
  const tMax = Math.min(tBottom >= 0 ? tBottom : tTop, 9000)
  const cosTheta = dot(rd, sun)
  const phaseR = ms ? rayleighPhase(cosTheta) : 1 / (4 * Math.PI)
  const phaseM = ms ? hgPhase(MIE_G, cosTheta) : 1 / (4 * Math.PI)
  const L: Vec3 = [0, 0, 0]
  const f: Vec3 = [0, 0, 0]
  const throughput: Vec3 = [1, 1, 1]
  const dt = tMax / steps
  for (let s = 0; s < steps; s++) {
    const p = add(ro, rd, tMax * ((s + 0.3) / steps))
    const r = len(p)
    const up: Vec3 = [p[0] / r, p[1] / r, p[2] / r]
    const m = medium(a, r - BOTTOM)
    const sunMu = dot(sun, up)
    const tSun = lookupTransmittance(tr, r, sunMu)
    const shadow = raySphere(add(p, up, PLANET_OFFSET), sun, BOTTOM) >= 0 ? 0 : 1
    let msL: Vec3 = [0, 0, 0]
    if (ms) msL = ms.sample(toSubUvs(clamp01(sunMu * 0.5 + 0.5), MULTI.w), toSubUvs(clamp01((r - BOTTOM) / (TOP - BOTTOM)), MULTI.h))
    for (let i = 0; i < 3; i++) {
      const ext = m.extinction[i]
      const sampleT = Math.exp(-ext * dt)
      const phaseScat = m.rayleigh[i] * phaseR + m.mie * phaseM
      const S = shadow * tSun[i] * phaseScat + msL[i] * m.scattering[i]
      L[i] += throughput[i] * ((S - S * sampleT) / ext)
      f[i] += throughput[i] * ((m.scattering[i] - m.scattering[i] * sampleT) / ext)
      throughput[i] *= sampleT
    }
  }
  if (ground && tMax === tBottom && tBottom > 0) {
    const p = add(ro, rd, tBottom)
    const r = len(p)
    const up: Vec3 = [p[0] / r, p[1] / r, p[2] / r]
    const tSun = lookupTransmittance(tr, r, dot(sun, up))
    const nl = clamp01(dot(up, sun))
    for (let i = 0; i < 3; i++) L[i] += tSun[i] * throughput[i] * nl * (a.groundAlbedo / Math.PI)
  }
  return { L, multiAs1: f }
}

/** Ψms of the multiple-scattering LUT at a texel (the sample's NewMultiScattCS, a loop per pixel). */
export function multiScattering(a: Atmosphere, tr: Table, u: number, v: number): Vec3 {
  const uu = fromSubUvs(u, MULTI.w)
  const vv = fromSubUvs(v, MULTI.h)
  const sunMu = uu * 2 - 1
  const sun: Vec3 = [Math.sqrt(Math.max(0, 1 - sunMu * sunMu)), sunMu, 0]
  const r = BOTTOM + clamp01(vv + PLANET_OFFSET) * (TOP - BOTTOM - PLANET_OFFSET)
  const ro: Vec3 = [0, r, 0]
  const n = MULTI.dirs
  const L: Vec3 = [0, 0, 0]
  const f: Vec3 = [0, 0, 0]
  for (let i = 0; i < n; i++) {
    for (let j = 0; j < n; j++) {
      const theta = (2 * Math.PI * (i + 0.5)) / n
      const phi = Math.acos(1 - (2 * (j + 0.5)) / n)
      const rd: Vec3 = [Math.cos(theta) * Math.sin(phi), Math.cos(phi), Math.sin(theta) * Math.sin(phi)]
      const s = integrate(a, ro, rd, sun, MULTI.steps, true, tr, null)
      for (let k = 0; k < 3; k++) {
        L[k] += s.L[k] / (n * n)
        f[k] += s.multiAs1[k] / (n * n)
      }
    }
  }
  return L.map((x, k) => x / (1 - f[k])) as Vec3
}

export const multiScatteringTable = (a: Atmosphere, tr: Table, w = MULTI.w, h = MULTI.h) => Table.make(w, h, (u, v) => multiScattering(a, tr, u, v))

/** Sky-view LUT mapping (the sample's UvToSkyViewLutParams): uv → zenith cosine and the cosine to the sun's azimuth. */
export function skyViewParams(u: number, v: number, r: number): { viewMu: number; lightCos: number } {
  const uu = fromSubUvs(u, SKYVIEW.w)
  const vv = fromSubUvs(v, SKYVIEW.h)
  const vHorizon = Math.sqrt(r * r - BOTTOM * BOTTOM)
  const beta = Math.acos(vHorizon / r)
  const zenithHorizon = Math.PI - beta
  let viewZenith: number
  if (vv < 0.5) {
    let c = 1 - 2 * vv
    c *= c
    viewZenith = zenithHorizon * (1 - c)
  } else {
    let c = vv * 2 - 1
    c *= c
    viewZenith = zenithHorizon + beta * c
  }
  const c = uu * uu
  return { viewMu: Math.cos(viewZenith), lightCos: -(c * 2 - 1) }
}

export function skyViewUv(r: number, viewMu: number, lightCos: number, groundHit: boolean): [number, number] {
  const vHorizon = Math.sqrt(r * r - BOTTOM * BOTTOM)
  const beta = Math.acos(vHorizon / r)
  const zenithHorizon = Math.PI - beta
  let v: number
  if (!groundHit) {
    let c = Math.acos(Math.min(1, Math.max(-1, viewMu))) / zenithHorizon
    c = 1 - Math.sqrt(Math.max(0, 1 - c))
    v = c * 0.5
  } else {
    const c = (Math.acos(Math.min(1, Math.max(-1, viewMu))) - zenithHorizon) / beta
    v = Math.sqrt(Math.max(0, c)) * 0.5 + 0.5
  }
  const u = Math.sqrt(Math.max(0, -lightCos * 0.5 + 0.5))
  return [toSubUvs(u, SKYVIEW.w), toSubUvs(v, SKYVIEW.h)]
}

/** Sky radiance per unit sun illuminance along a direction from radius r (Y up), sun a unit vector. */
export function skyRadiance(a: Atmosphere, tr: Table, ms: Table, r: number, dir: Vec3, sun: Vec3, steps = SKYVIEW.steps): Vec3 {
  return integrate(a, [0, r, 0], dir, sun, steps, true, tr, ms).L
}

/** A texel of the sky-view LUT, per unit sun illuminance. */
export function skyViewTexel(a: Atmosphere, tr: Table, ms: Table, r: number, sunMu: number, u: number, v: number): Vec3 {
  const { viewMu, lightCos } = skyViewParams(u, v, r)
  const s = Math.sqrt(Math.max(0, 1 - viewMu * viewMu))
  const dir: Vec3 = [s * lightCos, viewMu, s * Math.sqrt(Math.max(0, 1 - lightCos * lightCos))]
  const sun: Vec3 = [Math.sqrt(Math.max(0, 1 - sunMu * sunMu)), sunMu, 0]
  return skyRadiance(a, tr, ms, r, dir, sun)
}

/**
 * Irradiance on level ground from the sky alone, per unit sun illuminance:
 * the radiance over the upper hemisphere, cosine weighted (equal steps in
 * sin² elevation weight each ring by its cosine-weighted solid angle).
 */
export function skyIrradiance(a: Atmosphere, tr: Table, ms: Table, r: number, sun: Vec3, rings = 16, segments = 32, steps = SKYVIEW.steps): Vec3 {
  const out: Vec3 = [0, 0, 0]
  for (let i = 0; i < rings; i++) {
    const y = Math.sqrt((i + 0.5) / rings)
    const rr = Math.sqrt(1 - y * y)
    for (let j = 0; j < segments; j++) {
      const az = ((j + 0.5) / segments) * 2 * Math.PI
      const l = skyRadiance(a, tr, ms, r, [rr * Math.cos(az), y, rr * Math.sin(az)], sun, steps)
      for (let k = 0; k < 3; k++) out[k] += l[k]
    }
  }
  return out.map((v) => (v * Math.PI) / (rings * segments)) as Vec3
}

/** The sun's light reaching radius r, per unit illuminance above the atmosphere: 0 behind the planet. */
export function sunTransmittance(a: Atmosphere, r: number, sun: Vec3, steps = 40): Vec3 {
  const ro: Vec3 = [0, r, 0]
  if (raySphere(ro, sun, BOTTOM) >= 0) return [0, 0, 0]
  return transmittance(a, r, sun[1], steps)
}

export const luminance = (c: Vec3) => 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2]
