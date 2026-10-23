import { ClampToEdgeWrapping, HalfFloatType, LinearFilter, Vector3 } from 'three'
import * as tsl from 'three/tsl'
import { NodeMaterial, QuadMesh, RenderTarget, type WebGPURenderer } from 'three/webgpu'
import type { TSLNode } from '../../materials/patterns.ts'
import { BOTTOM, CAMERA_ABOVE_GROUND, coefficients, GROUND_ALBEDO, groundRadius, MIE_G, MIE_H, MULTI, OZONE_HALF_WIDTH, OZONE_PEAK, RAYLEIGH_H, SKYVIEW, TOP, TRANSMITTANCE, viewRadius, type AtmosphereParams, type Vec3 } from './constants.ts'

// Hillaire 2020's transmittance, multiple-scattering and sky-view LUTs as
// full-screen fragment passes: one TSL source for WebGPU and WebGL2, whose
// backend cannot write storage textures (Epic design「物理大气与 LUT」). All
// RGBA16F; the sky view holds environment-relative radiance (× s_env).
// Discretised as cpu.ts is, which e2e「W2 atmosphere drift」 holds them to.

type Op = (...args: TSLNode[]) => TSLNode
const { abs, acos, clamp, cos, dot, exp, float, length, max, min, normalize, select, sqrt, texture, uniform, uv, vec2, vec3, vec4 } = tsl as unknown as Record<string, Op>
const Fn = tsl.Fn as unknown as (fn: () => TSLNode) => () => TSLNode
const Loop = tsl.Loop as unknown as (range: { start: number; end: number; name: string }, body: (i: Record<string, TSLNode>) => void) => void

const H = Math.sqrt(TOP * TOP - BOTTOM * BOTTOM)
const PI = Math.PI

/** The nearest non-negative distance along rd from ro to the sphere of radius about the origin, or −1. */
function raySphere(ro: TSLNode, rd: TSLNode, radius: TSLNode): TSLNode {
  const b = dot(ro, rd)
  const c = dot(ro, ro).sub(float(radius).mul(radius))
  const d = b.mul(b).sub(c)
  const s = sqrt(max(d, 0))
  const t0 = b.negate().sub(s)
  const t1 = b.negate().add(s)
  return select(d.lessThan(0), float(-1), select(t0.greaterThanEqual(0), t0, select(t1.greaterThanEqual(0), t1, float(-1))))
}

/**
 * raySphere from the camera, (0, r, 0), to the scene's ground: in float32
 * |ro|² − ground² cancels (two numbers near 4e7 a few hundred apart, a few
 * percent off), which moves the horizon of a dense atmosphere; the camera is
 * CAMERA_ABOVE_GROUND above the ground, so the difference is h (2 ground + h).
 */
function rayGroundFromCamera(r: TSLNode, rd: TSLNode, ground: TSLNode): TSLNode {
  const b = r.mul(rd.y)
  const d = b.mul(b).sub(ground.mul(2).add(CAMERA_ABOVE_GROUND).mul(CAMERA_ABOVE_GROUND))
  const s = sqrt(max(d, 0))
  const t0 = b.negate().sub(s)
  return select(d.lessThan(0).or(t0.lessThan(0)), float(-1), t0)
}

const toSub = (x: TSLNode, n: number) => x.add(0.5 / n).mul((n - 1) / n)
const fromSub = (x: TSLNode, n: number) => x.sub(0.5 / n).mul(n / (n - 1))

function lutTarget(w: number, h: number): RenderTarget {
  const t = new RenderTarget(w, h, { type: HalfFloatType, depthBuffer: false })
  t.texture.minFilter = LinearFilter
  t.texture.magFilter = LinearFilter
  t.texture.wrapS = ClampToEdgeWrapping
  t.texture.wrapT = ClampToEdgeWrapping
  t.texture.generateMipmaps = false
  return t
}

type Uniform<T> = TSLNode & { value: T }

export type AtmosphereLuts = {
  readonly transmittance: RenderTarget
  readonly multiScattering: RenderTarget
  readonly skyView: RenderTarget
  /** The camera's and the ground's distance from the planet's centre, km. */
  readonly viewRadius: Uniform<number>
  readonly groundRadius: Uniform<number>
  /** The atmosphere's coefficients and radii: before renderAtmosphere. */
  setAtmosphere(params: AtmosphereParams): void
  /** The moon's sky view (W3): the same sky lit by the moon, parametrised about the moon's azimuth, so it cannot share the sun's. */
  readonly moonSkyView: RenderTarget
  /** The sun's zenith cosine and what the sky view's radiance per unit sun illuminance is multiplied by (the sun's illuminance per channel × s_env): before renderSkyView. */
  setSun(sunMu: number, scale: Readonly<Vec3>): void
  /** As setSun for the moon's sky view: the moon's zenith cosine and illuminance per channel × s_env. */
  setMoon(moonMu: number, scale: Readonly<Vec3>): void
  /** Transmittance and multiple scattering: on an atmosphere change. */
  renderAtmosphere(renderer: WebGPURenderer): void
  /** The sky view: on an environment change. */
  renderSkyView(renderer: WebGPURenderer): void
  /** The moon's sky view: on an environment change by night. */
  renderMoonSkyView(renderer: WebGPURenderer): void
  /** The transmittance LUT at a radius and zenith cosine. */
  sampleTransmittance(r: TSLNode, mu: TSLNode): TSLNode
  /** The sky view LUT toward a unit direction d (document space, Y up), the sun at the unit vector sun. */
  sampleSkyView(d: TSLNode, sun: TSLNode): TSLNode
  /** The moon's sky view toward d, the moon at the unit vector moon. */
  sampleMoonSkyView(d: TSLNode, moon: TSLNode): TSLNode
  dispose(): void
}

export function atmosphereLuts(): AtmosphereLuts {
  const u = {
    rayleigh: uniform(new Vector3()) as Uniform<Vector3>,
    mieScattering: uniform(0) as Uniform<number>,
    mieAbsorption: uniform(new Vector3()) as Uniform<Vector3>,
    ozone: uniform(new Vector3()) as Uniform<Vector3>,
    viewRadius: uniform(BOTTOM + 0.01) as Uniform<number>,
    groundRadius: uniform(BOTTOM) as Uniform<number>,
    sunMu: uniform(1) as Uniform<number>,
    scale: uniform(new Vector3(1, 1, 1)) as Uniform<Vector3>,
    moonMu: uniform(-1) as Uniform<number>,
    moonScale: uniform(new Vector3()) as Uniform<Vector3>,
  }
  const tr = lutTarget(TRANSMITTANCE.w, TRANSMITTANCE.h)
  const ms = lutTarget(MULTI.w, MULTI.h)
  const sv = lutTarget(SKYVIEW.w, SKYVIEW.h)
  const mv = lutTarget(SKYVIEW.w, SKYVIEW.h)
  const trTex = texture(tr.texture)
  const msTex = texture(ms.texture)
  const svTex = texture(sv.texture)
  const mvTex = texture(mv.texture)

  function medium(h: TSLNode) {
    const hh = max(h, 0)
    const dR = exp(hh.div(-RAYLEIGH_H))
    const dM = exp(hh.div(-MIE_H))
    const dO = max(float(0), float(1).sub(abs(hh.sub(OZONE_PEAK)).div(OZONE_HALF_WIDTH)))
    const rayleigh = u.rayleigh.mul(dR)
    const mie = u.mieScattering.mul(dM)
    const scattering = rayleigh.add(mie)
    const extinction = scattering.add(u.mieAbsorption.mul(dM)).add(u.ozone.mul(dO))
    return { rayleigh, mie, scattering, extinction }
  }

  function transmittanceUv(r: TSLNode, mu: TSLNode) {
    const rho = sqrt(max(r.mul(r).sub(BOTTOM * BOTTOM), 0))
    const d = max(r.negate().mul(mu).add(sqrt(max(r.mul(r).mul(mu.mul(mu).sub(1)).add(TOP * TOP), 0))), 0)
    const dMin = float(TOP).sub(r)
    return vec2(d.sub(dMin).div(rho.add(H).sub(dMin)), rho.div(H))
  }
  const sampleTransmittance = (r: TSLNode, mu: TSLNode) => trTex.sample(transmittanceUv(r, mu)).level(0).rgb
  const sampleMulti = (r: TSLNode, mu: TSLNode) =>
    msTex.sample(vec2(toSub(clamp(mu.mul(0.5).add(0.5), 0, 1), MULTI.w), toSub(clamp(r.sub(BOTTOM).div(TOP - BOTTOM), 0, 1), MULTI.h))).level(0).rgb

  // Transmittance to the top of the atmosphere (Bruneton's mapping), the planet not tested.
  const trMaterial = new NodeMaterial()
  trMaterial.fragmentNode = Fn(() => {
    const p = uv()
    const rho = p.y.mul(H)
    const r = sqrt(rho.mul(rho).add(BOTTOM * BOTTOM)).toVar()
    const dMin = float(TOP).sub(r)
    const d = dMin.add(p.x.mul(rho.add(H).sub(dMin)))
    const mu = select(d.equal(0), float(1), clamp(float(H * H).sub(rho.mul(rho)).sub(d.mul(d)).div(r.mul(d).mul(2)), -1, 1)).toVar()
    const ro = vec3(0, r, 0)
    const rd = vec3(sqrt(max(float(1).sub(mu.mul(mu)), 0)), mu, 0)
    const tMax = raySphere(ro, rd, TOP).toVar()
    const depth = vec3(0).toVar()
    const n = TRANSMITTANCE.steps
    Loop({ start: 0, end: n, name: 's' }, ({ s }) => {
      const h = length(ro.add(rd.mul(tMax.mul(float(s).add(0.3).div(n))))).sub(BOTTOM)
      depth.addAssign(medium(h).extinction.mul(tMax.div(n)))
    })
    return vec4(exp(depth.negate()), 1)
  })()
  const trQuad = new QuadMesh(trMaterial)

  /**
   * IntegrateScatteredLuminance for a sun of illuminance 1: the sky view over
   * the scene's ground (view), or the multiple-scattering pass over sea
   * level, isotropic.
   */
  function integrate(ro: TSLNode, rd: TSLNode, sun: TSLNode, steps: number, view: boolean) {
    const ground = view ? u.groundRadius : float(BOTTOM)
    const tGround = (view ? rayGroundFromCamera(u.viewRadius, rd, ground) : raySphere(ro, rd, ground)).toVar()
    const tTop = raySphere(ro, rd, TOP)
    const tMax = min(select(tGround.greaterThanEqual(0), tGround, tTop), 9000).toVar()
    const cosTheta = dot(rd, sun)
    const phaseR = view ? float(3 / (16 * PI)).mul(cosTheta.mul(cosTheta).add(1)) : float(1 / (4 * PI))
    const denom = float(1 + MIE_G * MIE_G).sub(cosTheta.mul(2 * MIE_G))
    const phaseM = view ? float(1 - MIE_G * MIE_G).div(denom.mul(sqrt(denom)).mul(4 * PI)) : float(1 / (4 * PI))
    const L = vec3(0).toVar()
    const f = vec3(0).toVar()
    const throughput = vec3(1).toVar()
    const dt = tMax.div(steps).toVar()
    Loop({ start: 0, end: steps, name: 's' }, ({ s }) => {
      const p = ro.add(rd.mul(tMax.mul(float(s).add(0.3).div(steps)))).toVar()
      const r = length(p).toVar()
      const up = p.div(r)
      const m = medium(r.sub(BOTTOM))
      const sunMu = dot(sun, up).toVar()
      const lit = select(raySphere(p.add(up.mul(0.01)), sun, ground).greaterThanEqual(0), float(0), float(1))
      const ext = max(m.extinction, vec3(1e-12)).toVar()
      const sampleT = exp(ext.negate().mul(dt)).toVar()
      const direct = sampleTransmittance(r, sunMu).mul(m.rayleigh.mul(phaseR).add(m.mie.mul(phaseM))).mul(lit)
      const S = view ? direct.add(sampleMulti(r, sunMu).mul(m.scattering)) : direct
      L.addAssign(throughput.mul(S.sub(S.mul(sampleT)).div(ext)))
      if (!view) f.addAssign(throughput.mul(m.scattering.sub(m.scattering.mul(sampleT)).div(ext)))
      throughput.mulAssign(sampleT)
    })
    // The planet's ground: lit by the sun through the atmosphere, and in the
    // sky view also by the sky, Ψms × albedo, so the horizon has no dark seam
    // at twilight (atmosphere-luts.md section 4).
    const hit = tGround.greaterThan(0).and(tMax.equal(tGround))
    const p = ro.add(rd.mul(tGround))
    const pr = length(p)
    const up = normalize(p)
    const sunMu = dot(up, sun)
    const sunLit = sampleTransmittance(pr, sunMu).mul(clamp(sunMu, 0, 1)).mul(GROUND_ALBEDO / PI)
    const groundL = view ? sunLit.add(sampleMulti(pr, sunMu).mul(GROUND_ALBEDO)) : sunLit
    L.addAssign(select(hit, throughput.mul(groundL), vec3(0)))
    return { L, f }
  }

  // Ψms per texel: a loop over 8 × 8 directions in each pixel instead of the sample's compute reduction.
  const msMaterial = new NodeMaterial()
  msMaterial.fragmentNode = Fn(() => {
    const p = uv()
    const sunMu = fromSub(p.x, MULTI.w).mul(2).sub(1).toVar()
    const sun = vec3(sqrt(max(float(1).sub(sunMu.mul(sunMu)), 0)), sunMu, 0).toVar()
    const r = float(BOTTOM).add(clamp(fromSub(p.y, MULTI.h).add(0.01), 0, 1).mul(TOP - BOTTOM - 0.01))
    const ro = vec3(0, r, 0).toVar()
    const n = MULTI.dirs
    const L = vec3(0).toVar()
    const f = vec3(0).toVar()
    Loop({ start: 0, end: n, name: 'a' }, ({ a }) => {
      Loop({ start: 0, end: n, name: 'b' }, ({ b }) => {
        const theta = float(a).add(0.5).mul((2 * PI) / n)
        const phi = acos(float(1).sub(float(b).add(0.5).mul(2 / n)))
        const sp = phi.sin()
        const s = integrate(ro, vec3(theta.cos().mul(sp), phi.cos(), theta.sin().mul(sp)), sun, MULTI.steps, false)
        L.addAssign(s.L.div(n * n))
        f.addAssign(s.f.div(n * n))
      })
    })
    return vec4(L.div(vec3(1).sub(f)), 1)
  })()
  const msQuad = new QuadMesh(msMaterial)

  /** The sky view's horizon from the camera: the angle below horizontal at which a view meets the scene's ground. */
  const horizon = () => {
    const r = u.viewRadius
    // √(r² − ground²) without the cancellation, as rayGroundFromCamera.
    const beta = acos(sqrt(u.groundRadius.mul(2).add(CAMERA_ABOVE_GROUND).mul(CAMERA_ABOVE_GROUND)).div(r)).toVar()
    return { r, beta, zenithHorizon: float(PI).sub(beta).toVar() }
  }

  // The sky view from the camera's height (the sample's UvToSkyViewLutParams): radiance per unit light illuminance × scale,
  // for the sun and, the same pass with its own uniforms, for the moon (W3).
  const skyViewMaterial = (lightMu: Uniform<number>, scale: Uniform<Vector3>) => {
    const material = new NodeMaterial()
    material.fragmentNode = Fn(() => {
      const p = uv()
      const uu = fromSub(p.x, SKYVIEW.w)
      const vv = fromSub(p.y, SKYVIEW.h)
      const { r, beta, zenithHorizon } = horizon()
      const above = float(1).sub(vv.mul(2))
      const below = vv.mul(2).sub(1)
      const viewZenith = select(vv.lessThan(0.5), zenithHorizon.mul(float(1).sub(above.mul(above))), zenithHorizon.add(beta.mul(below.mul(below))))
      const viewMu = cos(viewZenith).toVar()
      const lightCos = uu.mul(uu).mul(2).sub(1).negate().toVar()
      const vs = sqrt(max(float(1).sub(viewMu.mul(viewMu)), 0))
      const rd = vec3(vs.mul(lightCos), viewMu, vs.mul(sqrt(max(float(1).sub(lightCos.mul(lightCos)), 0))))
      const light = vec3(sqrt(max(float(1).sub(lightMu.mul(lightMu)), 0)), lightMu, 0).toVar()
      return vec4(integrate(vec3(0, r, 0), rd, light, SKYVIEW.steps, true).L.mul(scale), 1)
    })()
    return material
  }
  const svMaterial = skyViewMaterial(u.sunMu, u.scale)
  const svQuad = new QuadMesh(svMaterial)
  const mvMaterial = skyViewMaterial(u.moonMu, u.moonScale)
  const mvQuad = new QuadMesh(mvMaterial)

  const sampleSkyView = (d: TSLNode, sun: TSLNode) => sampleView(svTex, d, sun)
  const sampleMoonSkyView = (d: TSLNode, moon: TSLNode) => sampleView(mvTex, d, moon)

  function sampleView(view: TSLNode, d: TSLNode, sun: TSLNode): TSLNode {
    const { r, beta, zenithHorizon } = horizon()
    const ground = rayGroundFromCamera(r, d, u.groundRadius).greaterThanEqual(0)
    const za = acos(clamp(d.y, -1, 1)).toVar()
    const above = float(1).sub(sqrt(max(float(1).sub(za.div(zenithHorizon)), 0))).mul(0.5)
    const below = sqrt(max(za.sub(zenithHorizon).div(beta), 0)).mul(0.5).add(0.5)
    const lh = length(vec2(d.x, d.z)).mul(length(vec2(sun.x, sun.z)))
    const lightCos = select(lh.greaterThan(1e-6), dot(vec2(d.x, d.z), vec2(sun.x, sun.z)).div(max(lh, 1e-6)), float(1))
    const uu = sqrt(max(lightCos.mul(-0.5).add(0.5), 0))
    return view.sample(vec2(toSub(uu, SKYVIEW.w), toSub(select(ground, below, above), SKYVIEW.h))).level(0).rgb
  }

  const pass = (renderer: WebGPURenderer, target: RenderTarget, quad: QuadMesh) => {
    const previous = renderer.getRenderTarget()
    renderer.setRenderTarget(target)
    quad.render(renderer)
    renderer.setRenderTarget(previous)
  }

  return {
    transmittance: tr,
    multiScattering: ms,
    skyView: sv,
    moonSkyView: mv,
    viewRadius: u.viewRadius,
    groundRadius: u.groundRadius,
    setAtmosphere(params) {
      const c = coefficients(params)
      u.rayleigh.value.set(...c.rayleigh)
      u.mieScattering.value = c.mieScattering
      u.mieAbsorption.value.set(...c.mieAbsorption)
      u.ozone.value.set(...c.ozone)
      u.viewRadius.value = viewRadius(params)
      u.groundRadius.value = groundRadius(params)
    },
    setSun(sunMu, scale) {
      u.sunMu.value = sunMu
      u.scale.value.set(...scale)
    },
    setMoon(moonMu, scale) {
      u.moonMu.value = moonMu
      u.moonScale.value.set(...scale)
    },
    renderAtmosphere(renderer) {
      pass(renderer, tr, trQuad)
      pass(renderer, ms, msQuad)
    },
    renderSkyView(renderer) {
      pass(renderer, sv, svQuad)
    },
    renderMoonSkyView(renderer) {
      pass(renderer, mv, mvQuad)
    },
    sampleTransmittance,
    sampleSkyView,
    sampleMoonSkyView,
    dispose() {
      for (const t of [tr, ms, sv, mv]) t.dispose()
      for (const m of [trMaterial, msMaterial, svMaterial, mvMaterial]) m.dispose()
    },
  }
}
