// @ts-nocheck — spike code: TSL typings are loose in three r186.
// W2 spike: Hillaire 2020's transmittance, multiple-scattering and sky-view
// LUTs as full-screen fragment passes (one TSL source for WebGPU and
// WebGL2), and a sky drawn from the sky-view LUT with the sun disc.
import { BackSide, BoxGeometry, ClampToEdgeWrapping, FloatType, HalfFloatType, LinearFilter, Mesh, Vector3 } from 'three'
import { Fn, Loop, abs, acos, cameraProjectionMatrix, cameraViewMatrix, clamp, cos, dot, exp, float, length, max, min, normalize, positionLocal, select, sqrt, texture, uniform, uv, vec2, vec3, vec4 } from 'three/tsl'
import { NodeMaterial, QuadMesh, RenderTarget } from 'three/webgpu'
import { BOTTOM, MIE_G, MULTI, PLANET_OFFSET, SKYVIEW, SUN_LUX, TOP, TRANSMITTANCE, coefficients, type Atmosphere } from './atmo.ts'

const H = Math.sqrt(TOP * TOP - BOTTOM * BOTTOM)
const PI = Math.PI

function raySphere(ro, rd, radius: number) {
  const b = dot(ro, rd)
  const c = dot(ro, ro).sub(radius * radius)
  const d = b.mul(b).sub(c)
  const s = sqrt(max(d, 0))
  const t0 = b.negate().sub(s)
  const t1 = b.negate().add(s)
  return select(d.lessThan(0), float(-1), select(t0.greaterThanEqual(0), t0, select(t1.greaterThanEqual(0), t1, float(-1))))
}

export type Uniforms = ReturnType<typeof makeUniforms>

export function makeUniforms() {
  return {
    rayleigh: uniform(new Vector3()),
    mieScattering: uniform(0),
    mieAbsorption: uniform(new Vector3()),
    ozone: uniform(new Vector3()),
    groundAlbedo: uniform(0.3),
    /** Sky-view: the camera's distance from the centre (km), the sun's zenith cosine there, and what radiance per unit sun illuminance is multiplied by. */
    viewRadius: uniform(BOTTOM + PLANET_OFFSET),
    sunMu: uniform(1),
    scale: uniform(1),
  }
}

export function setAtmosphere(u: Uniforms, a: Atmosphere) {
  const c = coefficients(a)
  u.rayleigh.value.set(...c.rayleigh)
  u.mieScattering.value = c.mieScattering
  u.mieAbsorption.value.set(...c.mieAbsorption)
  u.ozone.value.set(...c.ozone)
  u.groundAlbedo.value = a.groundAlbedo
}

function medium(u: Uniforms, h) {
  const hh = max(h, 0)
  const dR = exp(hh.div(-8))
  const dM = exp(hh.div(-1.2))
  const dO = max(float(0), float(1).sub(abs(hh.sub(25)).div(15)))
  const rayleigh = u.rayleigh.mul(dR)
  const mie = u.mieScattering.mul(dM)
  const scattering = rayleigh.add(mie)
  const extinction = scattering.add(u.mieAbsorption.mul(dM)).add(u.ozone.mul(dO))
  return { rayleigh, mie, scattering, extinction }
}

function transmittanceUv(r, mu) {
  const rho = sqrt(max(r.mul(r).sub(BOTTOM * BOTTOM), 0))
  const disc = r.mul(r).mul(mu.mul(mu).sub(1)).add(TOP * TOP)
  const d = max(r.negate().mul(mu).add(sqrt(max(disc, 0))), 0)
  const dMin = float(TOP).sub(r)
  const dMax = rho.add(H)
  return vec2(d.sub(dMin).div(dMax.sub(dMin)), rho.div(H))
}

const toSub = (x, n: number) => x.add(0.5 / n).mul((n - 1) / n)
const fromSub = (x, n: number) => x.sub(0.5 / n).mul(n / (n - 1))

export function makeTarget(w: number, h: number, half: boolean) {
  const t = new RenderTarget(w, h, { type: half ? HalfFloatType : FloatType, depthBuffer: false })
  t.texture.minFilter = LinearFilter
  t.texture.magFilter = LinearFilter
  t.texture.wrapS = ClampToEdgeWrapping
  t.texture.wrapT = ClampToEdgeWrapping
  t.texture.generateMipmaps = false
  return t
}

export type Luts = ReturnType<typeof makeLuts>

/** The three LUTs and their passes. half: RGBA16F targets, else RGBA32F. */
export function makeLuts(half: boolean) {
  const u = makeUniforms()
  const tr = makeTarget(TRANSMITTANCE.w, TRANSMITTANCE.h, half)
  const ms = makeTarget(MULTI.w, MULTI.h, half)
  const sv = makeTarget(SKYVIEW.w, SKYVIEW.h, half)
  const trTex = texture(tr.texture)
  const msTex = texture(ms.texture)
  const svTex = texture(sv.texture)
  const sampleTr = (r, mu) => trTex.sample(transmittanceUv(r, mu)).level(0).rgb
  const sampleMs = (r, mu) =>
    msTex.sample(vec2(toSub(clamp(mu.mul(0.5).add(0.5), 0, 1), MULTI.w), toSub(clamp(r.sub(BOTTOM).div(TOP - BOTTOM), 0, 1), MULTI.h))).level(0).rgb

  // Transmittance to the top of the atmosphere (Bruneton's mapping), the planet not tested.
  const trMaterial = new NodeMaterial()
  trMaterial.fragmentNode = Fn(() => {
    const p = uv()
    const rho = p.y.mul(H)
    const r = sqrt(rho.mul(rho).add(BOTTOM * BOTTOM)).toVar()
    const dMin = float(TOP).sub(r)
    const dMax = rho.add(H)
    const d = dMin.add(p.x.mul(dMax.sub(dMin)))
    const mu = select(d.equal(0), float(1), clamp(float(H * H).sub(rho.mul(rho)).sub(d.mul(d)).div(r.mul(d).mul(2)), -1, 1)).toVar()
    const ro = vec3(0, r, 0)
    const rd = vec3(sqrt(max(float(1).sub(mu.mul(mu)), 0)), mu, 0)
    const tMax = raySphere(ro, rd, TOP).toVar()
    const depth = vec3(0).toVar()
    const n = TRANSMITTANCE.steps
    Loop({ start: 0, end: n, name: 's' }, ({ s }) => {
      const t = tMax.mul(float(s).add(0.3).div(n))
      const h = length(ro.add(rd.mul(t))).sub(BOTTOM)
      depth.addAssign(medium(u, h).extinction.mul(tMax.div(n)))
    })
    return vec4(exp(depth.negate()), 1)
  })()
  const trQuad = new QuadMesh(trMaterial)

  /** IntegrateScatteredLuminance (sun illuminance 1): multiple scattering when inMs (isotropic phases, ground lit), else the sky view. */
  function integrate(ro, rd, sun, steps: number, inMs: boolean) {
    const tBottom = raySphere(ro, rd, BOTTOM).toVar()
    const tTop = raySphere(ro, rd, TOP)
    const tMax = min(select(tBottom.greaterThanEqual(0), tBottom, tTop), 9000).toVar()
    const cosTheta = dot(rd, sun)
    const phaseR = inMs ? float(1 / (4 * PI)) : float(3 / (16 * PI)).mul(cosTheta.mul(cosTheta).add(1))
    const g = MIE_G
    const denom = float(1 + g * g).sub(cosTheta.mul(2 * g))
    const phaseM = inMs ? float(1 / (4 * PI)) : float(1 - g * g).div(denom.mul(sqrt(denom)).mul(4 * PI))
    const L = vec3(0).toVar()
    const f = vec3(0).toVar()
    const throughput = vec3(1).toVar()
    const dt = tMax.div(steps).toVar()
    Loop({ start: 0, end: steps, name: 's' }, ({ s }) => {
      const p = ro.add(rd.mul(tMax.mul(float(s).add(0.3).div(steps)))).toVar()
      const r = length(p).toVar()
      const up = p.div(r)
      const m = medium(u, r.sub(BOTTOM))
      const sunMu = dot(sun, up).toVar()
      const tSun = sampleTr(r, sunMu)
      const shadow = select(raySphere(p.add(up.mul(PLANET_OFFSET)), sun, BOTTOM).greaterThanEqual(0), float(0), float(1))
      const ext = max(m.extinction, vec3(1e-12)).toVar()
      const sampleT = exp(ext.negate().mul(dt)).toVar()
      const phaseScat = m.rayleigh.mul(phaseR).add(m.mie.mul(phaseM))
      const S = inMs ? tSun.mul(phaseScat).mul(shadow) : tSun.mul(phaseScat).mul(shadow).add(sampleMs(r, sunMu).mul(m.scattering))
      L.addAssign(throughput.mul(S.sub(S.mul(sampleT)).div(ext)))
      if (inMs) f.addAssign(throughput.mul(m.scattering.sub(m.scattering.mul(sampleT)).div(ext)))
      throughput.mulAssign(sampleT)
    })
    // The planet's ground (albedo, lit by the sun through the atmosphere): in the multiple scattering
    // as the sample has it, and in the sky view too, so below the horizon there is lit ground, not black.
    {
      const hitGround = tBottom.greaterThan(0).and(tMax.equal(tBottom))
      const p = ro.add(rd.mul(tBottom))
      const up = normalize(p)
      const tSun = sampleTr(length(p), dot(sun, up))
      const lit = tSun.mul(throughput).mul(clamp(dot(up, sun), 0, 1)).mul(u.groundAlbedo.div(PI))
      L.addAssign(select(hitGround, lit, vec3(0)))
    }
    return { L, f }
  }

  // Ψms per texel: a loop over 8 × 8 directions in each pixel instead of the sample's compute reduction.
  const msMaterial = new NodeMaterial()
  msMaterial.fragmentNode = Fn(() => {
    const p = uv()
    const sunMu = fromSub(p.x, MULTI.w).mul(2).sub(1).toVar()
    const sun = vec3(sqrt(max(float(1).sub(sunMu.mul(sunMu)), 0)), sunMu, 0).toVar()
    const r = float(BOTTOM).add(clamp(fromSub(p.y, MULTI.h).add(PLANET_OFFSET), 0, 1).mul(TOP - BOTTOM - PLANET_OFFSET))
    const ro = vec3(0, r, 0).toVar()
    const n = MULTI.dirs
    const L = vec3(0).toVar()
    const f = vec3(0).toVar()
    Loop({ start: 0, end: n, name: 'a' }, ({ a }) => {
      Loop({ start: 0, end: n, name: 'b' }, ({ b }) => {
        const theta = float(a).add(0.5).mul((2 * PI) / n)
        const phi = acos(float(1).sub(float(b).add(0.5).mul(2 / n)))
        const sp = phi.sin()
        const rd = vec3(theta.cos().mul(sp), phi.cos(), theta.sin().mul(sp))
        const s = integrate(ro, rd, sun, MULTI.steps, true)
        L.addAssign(s.L.div(n * n))
        f.addAssign(s.f.div(n * n))
      })
    })
    return vec4(L.div(vec3(1).sub(f)), 1)
  })()
  const msQuad = new QuadMesh(msMaterial)

  // Sky view from the camera's height, per unit sun illuminance times scale.
  const svMaterial = new NodeMaterial()
  svMaterial.fragmentNode = Fn(() => {
    const p = uv()
    const uu = fromSub(p.x, SKYVIEW.w)
    const vv = fromSub(p.y, SKYVIEW.h)
    const r = u.viewRadius
    const vHorizon = sqrt(r.mul(r).sub(BOTTOM * BOTTOM))
    const beta = acos(vHorizon.div(r)).toVar()
    const zenithHorizon = float(PI).sub(beta).toVar()
    const lower = float(1).sub(vv.mul(2))
    const upper = vv.mul(2).sub(1)
    const viewZenith = select(vv.lessThan(0.5), zenithHorizon.mul(float(1).sub(lower.mul(lower))), zenithHorizon.add(beta.mul(upper.mul(upper))))
    const viewMu = cos(viewZenith).toVar()
    const c = uu.mul(uu)
    const lightCos = c.mul(2).sub(1).negate().toVar()
    const vs = sqrt(max(float(1).sub(viewMu.mul(viewMu)), 0))
    const rd = vec3(vs.mul(lightCos), viewMu, vs.mul(sqrt(max(float(1).sub(lightCos.mul(lightCos)), 0))))
    const sun = vec3(sqrt(max(float(1).sub(u.sunMu.mul(u.sunMu)), 0)), u.sunMu, 0).toVar()
    const s = integrate(vec3(0, r, 0), rd, sun, SKYVIEW.steps, false)
    return vec4(s.L.mul(u.scale), 1)
  })()
  const svQuad = new QuadMesh(svMaterial)

  return {
    u,
    tr,
    ms,
    sv,
    trTex,
    svTex,
    sampleTr,
    /** Transmittance and multiple scattering: on an atmosphere change. */
    renderAtmosphere(renderer) {
      const target = renderer.getRenderTarget()
      renderer.setRenderTarget(tr)
      trQuad.render(renderer)
      renderer.setRenderTarget(ms)
      msQuad.render(renderer)
      renderer.setRenderTarget(target)
    },
    /** The sky view: on an environment change. */
    renderSkyView(renderer) {
      const target = renderer.getRenderTarget()
      renderer.setRenderTarget(sv)
      svQuad.render(renderer)
      renderer.setRenderTarget(target)
    },
    dispose() {
      for (const t of [tr, ms, sv]) t.dispose()
      for (const m of [trMaterial, msMaterial, svMaterial]) m.dispose()
    },
  }
}

/**
 * The physical sky on a box at the far plane (the product's skyMesh), in
 * document space (Y up): the sky-view LUT times r, the sun disc times k.
 * Disc off for the sky light. Everything clamped to 60 000 before the buffer.
 */
export function physicalSky(luts: Luts) {
  const sunDir = uniform(new Vector3(0, 1, 0))
  const relative = uniform(1)
  const exposure = uniform(1)
  const disc = uniform(1)
  const sunCosRadius = uniform(Math.cos(((0.545 / 2) * Math.PI) / 180))
  const sunRadiance = uniform(SUN_LUX / (2 * Math.PI * (1 - Math.cos(((0.545 / 2) * Math.PI) / 180))))
  const material = new NodeMaterial()
  material.side = BackSide
  material.depthWrite = false
  material.vertexNode = Fn(() => {
    const isOrtho = cameraProjectionMatrix.element(3).element(3).equal(1)
    const orthoScale = float(1).div(cameraProjectionMatrix.element(1).element(1)).mul(3)
    const local = isOrtho.select(positionLocal.mul(orthoScale), positionLocal)
    const view = cameraViewMatrix.mul(vec4(local, 0))
    const position = cameraProjectionMatrix.mul(vec4(view.xyz, 1)).toVar()
    position.z.assign(position.w)
    return position
  })()
  material.colorNode = Fn(() => {
    // The product's viewport is Z up: view (x, y, z) → document (x, z, −y), as render/sky.ts.
    const v = normalize(positionLocal)
    const d = vec3(v.x, v.z, v.y.negate()).toVar()
    const r = luts.u.viewRadius
    const sun = normalize(sunDir).toVar()
    const ground = raySphere(vec3(0, r, 0), d, BOTTOM).greaterThanEqual(0)
    const vHorizon = sqrt(r.mul(r).sub(BOTTOM * BOTTOM))
    const beta = acos(vHorizon.div(r)).toVar()
    const zenithHorizon = float(PI).sub(beta).toVar()
    const za = acos(clamp(d.y, -1, 1)).toVar()
    const above = float(1).sub(sqrt(max(float(1).sub(za.div(zenithHorizon)), 0))).mul(0.5)
    const below = sqrt(max(za.sub(zenithHorizon).div(beta), 0)).mul(0.5).add(0.5)
    const vCoord = select(ground, below, above)
    const dh = vec2(d.x, d.z)
    const sh = vec2(sun.x, sun.z)
    const lh = length(dh).mul(length(sh))
    const lightCos = select(lh.greaterThan(1e-6), dot(dh, sh).div(max(lh, 1e-6)), float(1))
    const uu = sqrt(max(lightCos.mul(-0.5).add(0.5), 0))
    const sky = luts.svTex.sample(vec2(toSub(uu, SKYVIEW.w), toSub(vCoord, SKYVIEW.h))).level(0).rgb.mul(relative)
    const cosAngle = dot(d, sun)
    const inDisc = cosAngle.greaterThan(sunCosRadius).and(ground.not())
    // Limb darkening I(μ) = 1 − 0.6 (1 − μ), normalised to a mean of 1 over the disc.
    const rr = sqrt(max(float(1).sub(cosAngle.mul(cosAngle)), 0)).div(sqrt(float(1).sub(sunCosRadius.mul(sunCosRadius))))
    const muDisc = sqrt(max(float(1).sub(rr.mul(rr)), 0))
    const limb = float(0.4).add(muDisc.mul(0.6)).div(0.8)
    const tSun = luts.sampleTr(r, sun.y)
    const sunL = tSun.mul(sunRadiance).mul(limb).mul(exposure).mul(disc)
    const color = sky.add(select(inDisc, sunL, vec3(0)))
    return vec4(min(color, vec3(60000)), 1)
  })()
  const mesh = new Mesh(new BoxGeometry(1, 1, 1), material)
  mesh.frustumCulled = false
  return Object.assign(mesh, { sunDir, relative, exposure, disc, sunCosRadius, sunRadiance })
}
