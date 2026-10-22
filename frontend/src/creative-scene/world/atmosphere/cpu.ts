import {
  BOTTOM,
  coefficients,
  GROUND_ALBEDO,
  groundRadius,
  luminance,
  MIE_G,
  MIE_H,
  MULTI,
  OZONE_HALF_WIDTH,
  OZONE_PEAK,
  RAYLEIGH_H,
  SKYVIEW,
  TOP,
  TRANSMITTANCE,
  viewRadius,
  type AtmosphereParams,
  type Coefficients,
  type Vec3,
} from './constants.ts'

// Hillaire 2020 on the CPU, discretised as the GPU's passes (luts.ts): the
// sun's light at the scene and the sky's light on level ground, which set
// the sun light, s_env and the fog. Built once per atmosphere, asked once
// per environment; typed arrays and scalars, nothing allocated per step.

/** The CPU's tables are coarser than the GPU's LUTs: only these integrals read them. */
const CPU_TRANSMITTANCE = { w: 64, h: 32 }
const CPU_MULTI = { w: 16, h: 16 }
/** The sky's irradiance: rings equal in sin² elevation (cosine-weighted), segments in azimuth. */
const IRRADIANCE = { rings: 16, segments: 32 }

const H = Math.sqrt(TOP * TOP - BOTTOM * BOTTOM)
const clamp01 = (x: number) => Math.min(1, Math.max(0, x))
const toSub = (u: number, n: number) => (u + 0.5 / n) * ((n - 1) / n)
const fromSub = (u: number, n: number) => (u - 0.5 / n) * (n / (n - 1))

/** The nearest non-negative distance along d from o to the sphere of radius about the origin, or −1. */
function raySphere(ox: number, oy: number, oz: number, dx: number, dy: number, dz: number, radius: number): number {
  const b = ox * dx + oy * dy + oz * dz
  const c = ox * ox + oy * oy + oz * oz - radius * radius
  const d = b * b - c
  if (d < 0) return -1
  const s = Math.sqrt(d)
  if (-b - s >= 0) return -b - s
  if (-b + s >= 0) return -b + s
  return -1
}

/** The transmittance LUT's mapping (Bruneton 2017, as the sample): texel → radius and zenith cosine. */
export function transmittanceParams(u: number, v: number): { r: number; mu: number } {
  const rho = H * v
  const r = Math.sqrt(rho * rho + BOTTOM * BOTTOM)
  const dMin = TOP - r
  const d = dMin + u * (rho + H - dMin)
  const mu = d === 0 ? 1 : Math.min(1, Math.max(-1, (H * H - rho * rho - d * d) / (2 * r * d)))
  return { r, mu }
}

function transmittanceU(r: number, mu: number): number {
  const rho = Math.sqrt(Math.max(0, r * r - BOTTOM * BOTTOM))
  const d = Math.max(0, -r * mu + Math.sqrt(Math.max(0, r * r * (mu * mu - 1) + TOP * TOP)))
  const dMin = TOP - r
  return (d - dMin) / (rho + H - dMin)
}

const transmittanceV = (r: number) => Math.sqrt(Math.max(0, r * r - BOTTOM * BOTTOM)) / H

/** The sky-view LUT's mapping (the sample's UvToSkyViewLutParams) from a camera at radius r over ground at radius ground: texel → the view's zenith cosine and its cosine to the sun's azimuth. */
export function skyViewParams(u: number, v: number, r: number, ground: number): { viewMu: number; lightCos: number } {
  const uu = fromSub(u, SKYVIEW.w)
  const vv = fromSub(v, SKYVIEW.h)
  const beta = Math.acos(Math.sqrt(r * r - ground * ground) / r)
  const zenithHorizon = Math.PI - beta
  const below = vv * 2 - 1
  const above = 1 - 2 * vv
  const viewZenith = vv < 0.5 ? zenithHorizon * (1 - above * above) : zenithHorizon + beta * below * below
  return { viewMu: Math.cos(viewZenith), lightCos: -(uu * uu * 2 - 1) }
}

const rayleighPhase = (c: number) => (3 / (16 * Math.PI)) * (1 + c * c)
function miePhase(c: number): number {
  const d = 1 + MIE_G * MIE_G - 2 * MIE_G * c
  return (1 - MIE_G * MIE_G) / (4 * Math.PI * d * Math.sqrt(d))
}

/** The day's light at the scene, from the CPU. */
export type Daylight = {
  /** The sun's illuminance on a surface facing it, per channel, lux. */
  sun: Vec3
  /** The sky's illuminance on level ground, per channel, lux. */
  sky: Vec3
  /** E: level ground's illuminance from the sun and the sky, lux. s_env and the fog read it. */
  horizontal: number
}

export class CpuAtmosphere {
  readonly ground: number
  readonly view: number
  private readonly c: Coefficients
  private readonly tr = new Float64Array(CPU_TRANSMITTANCE.w * CPU_TRANSMITTANCE.h * 3)
  private readonly ms = new Float64Array(CPU_MULTI.w * CPU_MULTI.h * 3)
  // Scratch: the medium (Rayleigh RGB, Mie, extinction RGB) and lookups.
  private readonly m = new Float64Array(7)
  private readonly t = new Float64Array(3)
  private readonly s = new Float64Array(3)
  private readonly sum = new Float64Array(6)

  constructor(params: AtmosphereParams) {
    this.c = coefficients(params)
    this.ground = groundRadius(params)
    this.view = viewRadius(params)
    const out = new Float64Array(3)
    for (let y = 0; y < CPU_TRANSMITTANCE.h; y++) {
      for (let x = 0; x < CPU_TRANSMITTANCE.w; x++) {
        const { r, mu } = transmittanceParams((x + 0.5) / CPU_TRANSMITTANCE.w, (y + 0.5) / CPU_TRANSMITTANCE.h)
        this.transmittance(r, mu, out)
        this.tr.set(out, (y * CPU_TRANSMITTANCE.w + x) * 3)
      }
    }
    for (let y = 0; y < CPU_MULTI.h; y++) {
      for (let x = 0; x < CPU_MULTI.w; x++) {
        this.multiScatteringTexel((x + 0.5) / CPU_MULTI.w, (y + 0.5) / CPU_MULTI.h, out, CPU_MULTI.w, CPU_MULTI.h)
        this.ms.set(out, (y * CPU_MULTI.w + x) * 3)
      }
    }
  }

  private medium(heightKm: number): void {
    const h = Math.max(0, heightKm)
    const dR = Math.exp(-h / RAYLEIGH_H)
    const dM = Math.exp(-h / MIE_H)
    const dO = Math.max(0, 1 - Math.abs(h - OZONE_PEAK) / OZONE_HALF_WIDTH)
    const c = this.c
    const mie = c.mieScattering * dM
    this.m[3] = mie
    for (let i = 0; i < 3; i++) {
      this.m[i] = c.rayleigh[i] * dR
      this.m[4 + i] = this.m[i] + mie + c.mieAbsorption[i] * dM + c.ozone[i] * dO
    }
  }

  /** The transmittance from radius r along zenith cosine mu to the top of the atmosphere, integrated as the LUT's texels are (the planet not tested). */
  transmittance(r: number, mu: number, out: Float64Array | Vec3): void {
    const dx = Math.sqrt(Math.max(0, 1 - mu * mu))
    const tMax = raySphere(0, r, 0, dx, mu, 0, TOP)
    const dt = tMax / TRANSMITTANCE.steps
    let d0 = 0
    let d1 = 0
    let d2 = 0
    for (let s = 0; s < TRANSMITTANCE.steps; s++) {
      const t = tMax * ((s + 0.3) / TRANSMITTANCE.steps)
      const px = dx * t
      const py = r + mu * t
      this.medium(Math.sqrt(px * px + py * py) - BOTTOM)
      d0 += this.m[4] * dt
      d1 += this.m[5] * dt
      d2 += this.m[6] * dt
    }
    out[0] = Math.exp(-d0)
    out[1] = Math.exp(-d1)
    out[2] = Math.exp(-d2)
  }

  /** Bilinear lookup in a table of w × h RGB texels, clamped to its edge, into out. */
  private static sample(table: Float64Array, w: number, h: number, u: number, v: number, out: Float64Array): void {
    const fx = Math.min(w - 1, Math.max(0, u * w - 0.5))
    const fy = Math.min(h - 1, Math.max(0, v * h - 0.5))
    const x0 = Math.floor(fx)
    const y0 = Math.floor(fy)
    const x1 = Math.min(w - 1, x0 + 1)
    const y1 = Math.min(h - 1, y0 + 1)
    const tx = fx - x0
    const ty = fy - y0
    for (let i = 0; i < 3; i++) {
      const a = table[(y0 * w + x0) * 3 + i] * (1 - tx) + table[(y0 * w + x1) * 3 + i] * tx
      const b = table[(y1 * w + x0) * 3 + i] * (1 - tx) + table[(y1 * w + x1) * 3 + i] * tx
      out[i] = a * (1 - ty) + b * ty
    }
  }

  private lookupTransmittance(r: number, mu: number): void {
    CpuAtmosphere.sample(this.tr, CPU_TRANSMITTANCE.w, CPU_TRANSMITTANCE.h, transmittanceU(r, mu), transmittanceV(r), this.t)
  }

  private lookupMulti(r: number, sunMu: number): void {
    CpuAtmosphere.sample(this.ms, CPU_MULTI.w, CPU_MULTI.h, toSub(clamp01(sunMu * 0.5 + 0.5), CPU_MULTI.w), toSub(clamp01((r - BOTTOM) / (TOP - BOTTOM)), CPU_MULTI.h), this.s)
  }

  /**
   * The sample's IntegrateScatteredLuminance for a sun of illuminance 1, from
   * (0, r, 0) along d: into this.sum[0..2] the radiance, into [3..5] the
   * multiple scattering's transfer. view: the sky view, over the scene's
   * ground, with the phases and the multiple scattering; else the
   * multiple-scattering pass, over the sea-level ground, isotropic.
   */
  private integrate(r: number, dx: number, dy: number, dz: number, sx: number, sy: number, sz: number, steps: number, view: boolean): void {
    const ground = view ? this.ground : BOTTOM
    const tGround = raySphere(0, r, 0, dx, dy, dz, ground)
    const tTop = raySphere(0, r, 0, dx, dy, dz, TOP)
    const hit = tGround >= 0
    const tMax = Math.min(hit ? tGround : tTop, 9000)
    const cosTheta = dx * sx + dy * sy + dz * sz
    const phaseR = view ? rayleighPhase(cosTheta) : 1 / (4 * Math.PI)
    const phaseM = view ? miePhase(cosTheta) : 1 / (4 * Math.PI)
    const dt = tMax / steps
    let L0 = 0, L1 = 0, L2 = 0, f0 = 0, f1 = 0, f2 = 0
    let T0 = 1, T1 = 1, T2 = 1
    for (let s = 0; s < steps; s++) {
      const t = tMax * ((s + 0.3) / steps)
      const px = dx * t
      const py = r + dy * t
      const pz = dz * t
      const pr = Math.sqrt(px * px + py * py + pz * pz)
      const ux = px / pr, uy = py / pr, uz = pz / pr
      this.medium(pr - BOTTOM)
      const sunMu = sx * ux + sy * uy + sz * uz
      this.lookupTransmittance(pr, sunMu)
      const off = 0.01
      const lit = raySphere(px + ux * off, py + uy * off, pz + uz * off, sx, sy, sz, ground) >= 0 ? 0 : 1
      if (view) this.lookupMulti(pr, sunMu)
      for (let i = 0; i < 3; i++) {
        const ext = Math.max(this.m[4 + i], 1e-12)
        const scattering = this.m[i] + this.m[3]
        const sampleT = Math.exp(-ext * dt)
        const S = lit * this.t[i] * (this.m[i] * phaseR + this.m[3] * phaseM) + (view ? this.s[i] * scattering : 0)
        const T = i === 0 ? T0 : i === 1 ? T1 : T2
        const dL = T * ((S - S * sampleT) / ext)
        const dF = T * ((scattering - scattering * sampleT) / ext)
        if (i === 0) { L0 += dL; f0 += dF; T0 *= sampleT } else if (i === 1) { L1 += dL; f1 += dF; T1 *= sampleT } else { L2 += dL; f2 += dF; T2 *= sampleT }
      }
    }
    if (hit && tGround > 0) {
      // The planet's ground: lit by the sun through the atmosphere, and in
      // the sky view also by the sky, Ψms × albedo, so the horizon has no
      // dark seam at twilight (atmosphere-luts.md section 4).
      const px = dx * tGround, py = r + dy * tGround, pz = dz * tGround
      const pr = Math.sqrt(px * px + py * py + pz * pz)
      const nl = clamp01((px * sx + py * sy + pz * sz) / pr)
      const sunMu = (px * sx + py * sy + pz * sz) / pr
      this.lookupTransmittance(pr, sunMu)
      if (view) this.lookupMulti(pr, sunMu)
      const sky = view ? 1 : 0
      L0 += T0 * (this.t[0] * nl * (GROUND_ALBEDO / Math.PI) + sky * this.s[0] * GROUND_ALBEDO)
      L1 += T1 * (this.t[1] * nl * (GROUND_ALBEDO / Math.PI) + sky * this.s[1] * GROUND_ALBEDO)
      L2 += T2 * (this.t[2] * nl * (GROUND_ALBEDO / Math.PI) + sky * this.s[2] * GROUND_ALBEDO)
    }
    this.sum[0] = L0; this.sum[1] = L1; this.sum[2] = L2
    this.sum[3] = f0; this.sum[4] = f1; this.sum[5] = f2
  }

  /** Ψms at a texel of a w × h multiple-scattering table: the sample's NewMultiScattCS, a loop over directions per texel. */
  multiScatteringTexel(u: number, v: number, out: Float64Array | Vec3, w: number = MULTI.w, h: number = MULTI.h): void {
    const sunMu = fromSub(u, w) * 2 - 1
    const sx = Math.sqrt(Math.max(0, 1 - sunMu * sunMu))
    const r = BOTTOM + clamp01(fromSub(v, h) + 0.01) * (TOP - BOTTOM - 0.01)
    const n = MULTI.dirs
    let L0 = 0, L1 = 0, L2 = 0, f0 = 0, f1 = 0, f2 = 0
    for (let i = 0; i < n; i++) {
      for (let j = 0; j < n; j++) {
        const theta = (2 * Math.PI * (i + 0.5)) / n
        const phi = Math.acos(1 - (2 * (j + 0.5)) / n)
        const sp = Math.sin(phi)
        this.integrate(r, Math.cos(theta) * sp, Math.cos(phi), Math.sin(theta) * sp, sx, sunMu, 0, MULTI.steps, false)
        L0 += this.sum[0]; L1 += this.sum[1]; L2 += this.sum[2]
        f0 += this.sum[3]; f1 += this.sum[4]; f2 += this.sum[5]
      }
    }
    const k = n * n
    out[0] = L0 / k / (1 - f0 / k)
    out[1] = L1 / k / (1 - f1 / k)
    out[2] = L2 / k / (1 - f2 / k)
  }

  /** The sky's radiance per unit sun illuminance toward the camera from direction dir (Y up), the sun at sun: what the sky-view LUT holds. */
  skyRadiance(dir: Readonly<Vec3>, sun: Readonly<Vec3>, out: Float64Array | Vec3): void {
    this.integrate(this.view, dir[0], dir[1], dir[2], sun[0], sun[1], sun[2], SKYVIEW.steps, true)
    out[0] = this.sum[0]
    out[1] = this.sum[1]
    out[2] = this.sum[2]
  }

  /** The sky's illuminance on level ground per unit sun illuminance, the sun at zenith cosine sunMu. */
  skyIrradiance(sunMu: number): Vec3 {
    const sx = Math.sqrt(Math.max(0, 1 - sunMu * sunMu))
    let e0 = 0, e1 = 0, e2 = 0
    for (let i = 0; i < IRRADIANCE.rings; i++) {
      const y = Math.sqrt((i + 0.5) / IRRADIANCE.rings)
      const rr = Math.sqrt(1 - y * y)
      for (let j = 0; j < IRRADIANCE.segments; j++) {
        const az = ((j + 0.5) / IRRADIANCE.segments) * 2 * Math.PI
        this.integrate(this.view, rr * Math.cos(az), y, rr * Math.sin(az), sx, sunMu, 0, SKYVIEW.steps, true)
        e0 += this.sum[0]; e1 += this.sum[1]; e2 += this.sum[2]
      }
    }
    const k = Math.PI / (IRRADIANCE.rings * IRRADIANCE.segments)
    return [e0 * k, e1 * k, e2 * k]
  }

  /** The sun's light reaching the scene per unit illuminance above the atmosphere: 0 once it is behind the ground. */
  sunTransmittance(sunMu: number): Vec3 {
    const sx = Math.sqrt(Math.max(0, 1 - sunMu * sunMu))
    if (raySphere(0, this.view, 0, sx, sunMu, 0, this.ground) >= 0) return [0, 0, 0]
    const out: Vec3 = [0, 0, 0]
    this.transmittance(this.view, sunMu, out)
    return out
  }

  /** The day's light at the scene for a sun at zenith cosine sunMu whose illuminance above the atmosphere is top (lux per channel). */
  daylight(sunMu: number, top: Readonly<Vec3>): Daylight {
    const tSun = this.sunTransmittance(sunMu)
    const sky = this.skyIrradiance(sunMu)
    const sun = tSun.map((t, i) => t * top[i]) as Vec3
    const skyLux = sky.map((e, i) => e * top[i]) as Vec3
    return { sun, sky: skyLux, horizontal: luminance(sun) * Math.max(0, sunMu) + luminance(skyLux) }
  }
}
