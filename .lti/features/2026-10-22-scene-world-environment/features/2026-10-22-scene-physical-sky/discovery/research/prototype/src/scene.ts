// @ts-nocheck — spike: a yard and a room lit by the physical sky with pre-exposure,
// against the product's procedural look (imported as is) for the mid-tone calibration.
import { AgXToneMapping, AmbientLight, BoxGeometry, DirectionalLight, Group, LinearSRGBColorSpace, Mesh, NoToneMapping, PerspectiveCamera, PlaneGeometry, PointLight, Scene, SphereGeometry, SRGBColorSpace, UnsignedByteType, Vector3 } from 'three'
import { MeshStandardNodeMaterial, NodeMaterial, PMREMGenerator, QuadMesh, RenderTarget } from 'three/webgpu'
import { Fn, exp2, float, log2, max, min, renderOutput, texture, uniform, uv, vec2, vec3, vec4, dot, Loop, clamp } from 'three/tsl'
import { SUN_LUX, clear, viewRadius, type Atmosphere, type Vec3 } from './atmo.ts'
import { luminance, multiScatteringTable, skyIrradiance, sunTransmittance, transmittanceTable } from './atmoCpu.ts'
import { makeLuts, makeTarget, physicalSky, setAtmosphere } from './atmoGpu.ts'
import { litSky, setSky, skyEnvironment, sunDirection } from '/@fs/Users/samson/workspace/my_project/customer_manage_platform/frontend/src/creative-scene/render/sky.ts'
import { ambientFill, sunLight } from '/@fs/Users/samson/workspace/my_project/customer_manage_platform/frontend/src/creative-scene/render/litLook.ts'

const toView = ([x, y, z]: Vec3): Vec3 => [x, -z, y]
const rad = (d: number) => (d * Math.PI) / 180

/** Document-space sun direction from elevation and azimuth (0° → +Z, 90° → +X). */
const sunDoc = (el: number, az: number): Vec3 => [Math.sin(rad(az)) * Math.cos(rad(el)), Math.sin(rad(el)), Math.cos(rad(az)) * Math.cos(rad(el))]

function mat(color: Vec3, roughness = 0.8, metalness = 0) {
  const m = new MeshStandardNodeMaterial()
  m.color.setRGB(...color)
  m.roughness = roughness
  m.metalness = metalness
  return m
}

function box(size: Vec3, at: Vec3, m) {
  const mesh = new Mesh(new BoxGeometry(...size), m)
  mesh.position.set(...at)
  mesh.castShadow = true
  mesh.receiveShadow = true
  return mesh
}

/** Scene content in view space (Z up). The ground is 0.3 albedo, the white box 0.8. */
function build(kind: string, lookAtSun: Vec3 | null = null) {
  const g = new Group()
  const camera = new PerspectiveCamera(40, 1.5, 0.1, 500)
  camera.up.set(0, 0, 1)
  if (kind === 'yard') {
    const ground = new Mesh(new PlaneGeometry(200, 200), mat([0.3, 0.3, 0.3], 0.9))
    ground.receiveShadow = true
    g.add(ground)
    g.add(box([1, 1, 1], [0, 0, 0.5], mat([0.8, 0.8, 0.8])))
    g.add(box([0.8, 0.8, 1.6], [1.8, 0.8, 0.8], mat([0.18, 0.18, 0.18])))
    g.add(box([1, 1, 1], [-1.8, -0.2, 0.5], mat([0.6, 0.08, 0.06])))
    const sphere = new Mesh(new SphereGeometry(0.5, 48, 24), mat([0.9, 0.9, 0.9], 0.15, 1))
    sphere.position.set(0.6, -1.7, 0.5)
    sphere.castShadow = true
    g.add(sphere)
    camera.position.set(5.5, -7.5, 2.4)
    camera.lookAt(0, 0, 0.6)
  } else {
    // A 6 × 6 × 3 m room, a 2 × 1.4 m window in the +X wall (document +X), the floor at z = 0.
    const wall = mat([0.7, 0.7, 0.7])
    g.add(box([6.4, 6.4, 0.2], [0, 0, -0.1], mat([0.35, 0.28, 0.2], 0.7)))
    g.add(box([6.4, 6.4, 0.2], [0, 0, 3.1], wall))
    g.add(box([0.2, 6.4, 3.2], [-3.1, 0, 1.5], wall))
    g.add(box([6.4, 0.2, 3.2], [0, 3.1, 1.5], wall))
    g.add(box([6.4, 0.2, 3.2], [0, -3.1, 1.5], wall))
    // +X wall around the window: x = 3.1, window y ∈ [−1, 1], z ∈ [0.8, 2.2].
    g.add(box([0.2, 2.2, 3.2], [3.1, -2.1, 1.5], wall))
    g.add(box([0.2, 2.2, 3.2], [3.1, 2.1, 1.5], wall))
    g.add(box([0.2, 2, 0.8], [3.1, 0, 0.4], wall))
    g.add(box([0.2, 2, 0.8], [3.1, 0, 2.6], wall))
    g.add(box([1.4, 0.8, 0.75], [0.5, 0.6, 0.375], mat([0.5, 0.35, 0.2], 0.6)))
    g.add(box([0.6, 0.6, 0.6], [-1.2, -1, 0.3], mat([0.8, 0.8, 0.8])))
    const ground = new Mesh(new PlaneGeometry(200, 200), mat([0.3, 0.3, 0.3], 0.9))
    ground.position.z = -0.21
    ground.receiveShadow = true
    g.add(ground)
    camera.position.set(-2.6, -2.6, 1.5)
    camera.lookAt(1.5, 1, 1.1)
  }
  if (lookAtSun) {
    const v = toView(lookAtSun)
    camera.lookAt(camera.position.x + v[0], camera.position.y + v[1], camera.position.z + v[2])
  }
  return { g, camera }
}

export type Shot = {
  kind?: 'yard' | 'room'
  sky?: 'physical' | 'procedural'
  atmosphere?: Atmosphere
  sunElevDeg?: number
  sunAzDeg?: number
  turbidity?: number
  /** auto: EV100 = metered − compensation − calibrationEv; manual: from iso, shutter and fStop. */
  exposure?: { mode: 'auto' | 'manual'; compensation?: number; iso?: number; shutter?: number; fStop?: number }
  calibrationEv?: number
  /** Point lights, candela, at document positions. */
  lamps?: { cd: number; at: Vec3; color?: Vec3 }[]
  /** The incident meter counts the lamps' light at the metering point too. */
  meterLamps?: boolean
  /** Metering point (document); the scene origin by default. */
  meterAt?: Vec3
  /** The sky's light through PMREM; off approximates a room's walls keeping it out. */
  skyLight?: boolean
  disc?: boolean
  /** GPU reflective metering: the picture's log average brought to the reflective meter's key, within ±limit stops. */
  reflective?: { limit: number } | null
  /** Clamp what materials write to 60 000 (NodeMaterial.setupOutput). */
  clampMaterials?: boolean
  lookAtSun?: boolean
  width?: number
  height?: number
  half?: boolean
  png?: boolean
}

export function sceneCases(renderer, backend: string, readTarget) {
  const cpu = new Map<string, { tr; ms }>()
  const cpuTables = (a: Atmosphere) => {
    const key = JSON.stringify(a)
    if (!cpu.has(key)) {
      const tr = transmittanceTable(a, 128, 32)
      cpu.set(key, { tr, ms: multiScatteringTable(a, tr, 16, 16) })
    }
    return cpu.get(key)
  }

  /** The incident meter: illuminance on level ground at the scene origin, lux, and its EV100 (C = 250). */
  function meter(a: Atmosphere, sun: Vec3, lamps: Shot['lamps'] = [], at: Vec3 = [0, 0, 0]) {
    const { tr, ms } = cpuTables(a)
    const r = viewRadius(a)
    const t0 = performance.now()
    const sky = luminance(skyIrradiance(a, tr, ms, r, sun)) * SUN_LUX
    const direct = luminance(sunTransmittance(a, r, sun)) * Math.max(0, sun[1]) * SUN_LUX
    let fromLamps = 0
    for (const l of lamps) {
      const d: Vec3 = [l.at[0] - at[0], l.at[1] - at[1], l.at[2] - at[2]]
      const d2 = d[0] * d[0] + d[1] * d[1] + d[2] * d[2]
      fromLamps += (l.cd * Math.max(0, d[1] / Math.sqrt(d2))) / d2
    }
    const E = sky + direct + fromLamps
    const ev = Math.min(20, Math.max(-12, Math.log2((E * 100) / 250)))
    return { E, sky, direct, fromLamps, ev, ms: performance.now() - t0 }
  }

  let restoreOutput: (() => void) | null = null
  function clampMaterials(on: boolean) {
    if (on && !restoreOutput) {
      const original = NodeMaterial.prototype.setupOutput
      NodeMaterial.prototype.setupOutput = function (builder, outputNode) {
        return original.call(this, builder, vec4(min(outputNode.rgb, vec3(60000)), outputNode.a))
      }
      restoreOutput = () => {
        NodeMaterial.prototype.setupOutput = original
        restoreOutput = null
      }
    } else if (!on && restoreOutput) restoreOutput()
  }

  async function shot(s: Shot) {
    const kind = s.kind ?? 'yard'
    const a = s.atmosphere ?? clear
    const el = s.sunElevDeg ?? 55
    const az = s.sunAzDeg ?? 135
    const width = s.width ?? 900
    const height = s.height ?? 600
    clampMaterials(s.clampMaterials === true)
    const { g, camera } = build(kind, s.lookAtSun ? sunDoc(el, az) : null)
    camera.aspect = width / height
    camera.updateProjectionMatrix()
    const scene = new Scene()
    scene.add(g)
    const pmrem = new PMREMGenerator(renderer)
    const disposables = [pmrem]
    const sunD = sunDoc(el, az)
    const info: Record<string, unknown> = { backend, kind, sky: s.sky ?? 'physical', sunElevDeg: el }
    const timings: Record<string, number> = {}
    const lamps = s.lamps ?? []
    let k = 1
    if ((s.sky ?? 'physical') === 'procedural') {
      const env = { sky: { kind: 'procedural', sun_elevation_deg: el, sun_azimuth_deg: az, turbidity: s.turbidity ?? 3 }, ambient: { color: '#ffffff', intensity: 0.4 }, fog: null }
      const sky = litSky()
      setSky(sky, env, s.disc !== false)
      scene.add(sky)
      const target = skyEnvironment(pmrem, env)
      disposables.push(target)
      scene.environment = target.texture
      scene.environmentIntensity = 1
      const light = sunLight(el, env.sky.turbidity)
      const sun = new DirectionalLight(light.color, light.intensity)
      const p = sunDirection(env).multiplyScalar(60)
      sun.position.set(...toView([p.x, p.y, p.z]))
      addShadow(sun, scene)
      scene.add(new AmbientLight(0xffffff, 0.4 * ambientFill('workbench')))
      for (const l of lamps) addLamp(scene, l, 1)
    } else {
      const luts = makeLuts(s.half !== false)
      disposables.push(luts)
      setAtmosphere(luts.u, a)
      const r = viewRadius(a)
      luts.u.viewRadius.value = r
      luts.u.sunMu.value = sunD[1]
      const m = meter(a, sunD, s.meterLamps ? lamps : [], s.meterAt)
      const envEv = s.meterLamps ? meter(a, sunD).ev : m.ev
      const sEnv = 1 / (1.2 * 2 ** envEv)
      const e = s.exposure ?? { mode: 'auto' }
      const ev = e.mode === 'manual' ? Math.log2((e.fStop ?? 16) ** 2 / (e.shutter ?? 1 / 125)) - Math.log2((e.iso ?? 100) / 100) : m.ev - (e.compensation ?? 0) - (s.calibrationEv ?? 0)
      k = 1 / (1.2 * 2 ** ev)
      const relative = k / sEnv
      Object.assign(info, { E: m.E, Esky: m.sky, Edirect: m.direct, Elamps: m.fromLamps, meteredEv: m.ev, envEv, ev, k, relative, meterMs: m.ms })
      luts.u.scale.value = SUN_LUX * sEnv
      let t0 = performance.now()
      luts.renderAtmosphere(renderer)
      luts.renderSkyView(renderer)
      timings.luts = performance.now() - t0
      const sky = physicalSky(luts)
      sky.sunDir.value.set(...sunD)
      sky.relative.value = relative
      sky.exposure.value = k
      sky.disc.value = s.disc === false ? 0 : 1
      scene.add(sky)
      if (s.skyLight !== false) {
        const envScene = new Scene()
        const envSky = physicalSky(luts)
        envSky.sunDir.value.set(...sunD)
        envSky.relative.value = 1
        envSky.disc.value = 0
        envScene.add(envSky)
        t0 = performance.now()
        const target = pmrem.fromScene(envScene, 0, 0.1, 100)
        timings.pmrem = performance.now() - t0
        disposables.push(target)
        scene.environment = target.texture
        scene.environmentIntensity = relative
      }
      const T = sunTransmittance(a, r, sunD)
      const lum = luminance(T)
      if (lum > 0) {
        const sun = new DirectionalLight(undefined, SUN_LUX * lum * k)
        const peak = Math.max(...T)
        sun.color.setRGB(T[0] / peak, T[1] / peak, T[2] / peak)
        sun.intensity = SUN_LUX * lum * k * (peak / lum) * 1
        // DirectionalLight colour × intensity is the irradiance per channel: T × SUN_LUX × k.
        sun.intensity = SUN_LUX * peak * k
        sun.position.set(...toView([sunD[0] * 60, sunD[1] * 60, sunD[2] * 60]))
        addShadow(sun, scene)
      }
      for (const l of lamps) addLamp(scene, l, k)
    }

    // The picture in half float, then optionally metered on the GPU, then AgX to sRGB.
    const hdr = makeTarget(width, height, true)
    hdr.depthBuffer = true
    const ldr = new RenderTarget(width, height, { type: UnsignedByteType, depthBuffer: false })
    disposables.push(hdr, ldr)
    renderer.toneMapping = NoToneMapping
    renderer.outputColorSpace = LinearSRGBColorSpace
    renderer.setRenderTarget(hdr)
    let t0 = performance.now()
    renderer.render(scene, camera)
    const gain = uniform(1)
    let meterTex = null
    if (s.reflective) {
      const m = reflectiveMeter(renderer, hdr, s.reflective.limit, s.calibrationEv ?? 0)
      disposables.push(m)
      m.render()
      meterTex = m.result
    }
    const outMaterial = new NodeMaterial()
    const residual = meterTex ? texture(meterTex.texture).sample(vec2(0.5)).level(0).r : float(0)
    outMaterial.fragmentNode = renderOutput(vec4(texture(hdr.texture).sample(uv()).rgb.mul(exp2(residual)).mul(gain), 1), AgXToneMapping, SRGBColorSpace)
    const outQuad = new QuadMesh(outMaterial)
    disposables.push(outMaterial)
    renderer.setRenderTarget(ldr)
    outQuad.render(renderer)
    timings.frame = performance.now() - t0
    const bytes = await renderer.readRenderTargetPixelsAsync(ldr, 0, 0, width, height)
    const hdrPixels = await readTarget(hdr)
    if (meterTex) info.residualEv = (await readTarget(meterTex))[0]
    let png = null
    if (s.png !== false) {
      renderer.setRenderTarget(null)
      renderer.setSize(width, height, false)
      outQuad.render(renderer)
      png = renderer.domElement.toDataURL('image/png')
    }
    clampMaterials(false)
    for (const d of disposables) d.dispose?.()
    scene.traverse((o) => {
      o.geometry?.dispose?.()
      o.material?.dispose?.()
    })
    return { ...info, timings, stats: ldrStats(bytes, width, height, backend), hdr: hdrStats(hdrPixels), png }
  }

  function addShadow(sun: DirectionalLight, scene: Scene) {
    sun.castShadow = true
    sun.shadow.mapSize.set(2048, 2048)
    const c = sun.shadow.camera
    c.left = c.bottom = -12
    c.right = c.top = 12
    c.near = 1
    c.far = 140
    sun.shadow.bias = 0
    sun.shadow.normalBias = 0.02
    scene.add(sun)
    scene.add(sun.target)
  }

  function addLamp(scene: Scene, l, k: number) {
    const p = new PointLight(undefined, l.cd * k, 0, 2)
    if (l.color) p.color.setRGB(...l.color)
    p.position.set(...toView(l.at))
    scene.add(p)
  }

  return {
    shot,
    meter: ({ atmosphere = clear, sunElevDeg = 55, sunAzDeg = 135 }) => meter(atmosphere, sunDoc(sunElevDeg, sunAzDeg)),
    /** Two shots of the same scene, compared byte for byte. */
    async twice(s: Shot) {
      const a = await shot({ ...s, png: false, raw: true })
      const b = await shot({ ...s, png: false, raw: true })
      return { a: a.stats, b: b.stats, equal: a.stats.hash === b.stats.hash }
    },
    /** PMREM of the physical sky, timed (an environment change while dragging the time). */
    async pmremTiming({ repeat = 10, half = true }) {
      const luts = makeLuts(half)
      setAtmosphere(luts.u, clear)
      luts.u.viewRadius.value = viewRadius(clear)
      luts.u.scale.value = SUN_LUX / (1.2 * 2 ** 15)
      const pmrem = new PMREMGenerator(renderer)
      const envScene = new Scene()
      const envSky = physicalSky(luts)
      envSky.disc.value = 0
      envScene.add(envSky)
      const sync = async () => {
        if (backend === 'webgpu') await renderer.backend.device.queue.onSubmittedWorkDone()
        else await readTarget(luts.tr)
      }
      luts.renderAtmosphere(renderer)
      const times = []
      for (let i = 0; i < repeat + 2; i++) {
        const el = 5 + i * 3
        luts.u.sunMu.value = Math.sin(rad(el))
        envSky.sunDir.value.set(...sunDoc(el, 135))
        await sync()
        const t0 = performance.now()
        luts.renderSkyView(renderer)
        const target = pmrem.fromScene(envScene, 0, 0.1, 100)
        await sync()
        if (i >= 2) times.push(performance.now() - t0)
        target.dispose()
      }
      pmrem.dispose()
      luts.dispose()
      times.sort((x, y) => x - y)
      return { backend, median: times[times.length >> 1], min: times[0], max: times[times.length - 1] }
    },
  }
}

/** Mean of the sRGB picture and of its lower quarter (the ground), and a hash for determinism. */
function ldrStats(bytes: Uint8Array, w: number, h: number, backend: string) {
  let sum = 0
  let ground = 0
  let groundN = 0
  let clipped = 0
  let hash = 2166136261
  for (let y = 0; y < h; y++) {
    // WebGL2 reads back bottom row first.
    const imageRow = backend === 'webgl2' ? h - 1 - y : y
    for (let x = 0; x < w; x++) {
      const i = (y * w + x) * 4
      const l = 0.2126 * bytes[i] + 0.7152 * bytes[i + 1] + 0.0722 * bytes[i + 2]
      sum += l
      if (bytes[i] >= 254 && bytes[i + 1] >= 254 && bytes[i + 2] >= 254) clipped++
      if (imageRow >= h * 0.75) {
        ground += l
        groundN++
      }
      hash = Math.imul(hash ^ bytes[i], 16777619) ^ Math.imul(bytes[i + 1], 31) ^ (bytes[i + 2] << 3)
    }
  }
  return { mean: sum / (w * h), ground: ground / groundN, clipped: clipped / (w * h), hash: hash >>> 0 }
}

function hdrStats(px: Float32Array) {
  let max = 0
  let atClamp = 0
  let nonFinite = 0
  let subnormal = 0
  let lit = 0
  let logSum = 0
  for (let i = 0; i < px.length; i += 4) {
    const r = px[i], g = px[i + 1], b = px[i + 2]
    if (!Number.isFinite(r) || !Number.isFinite(g) || !Number.isFinite(b)) {
      nonFinite++
      continue
    }
    const l = 0.2126 * r + 0.7152 * g + 0.0722 * b
    max = Math.max(max, r, g, b)
    if (Math.max(r, g, b) >= 59999) atClamp++
    if (l > 0 && l < 6.1e-5) subnormal++
    if (l > 0) {
      lit++
      logSum += Math.log2(l)
    }
  }
  const n = px.length / 4
  return { max, atClamp: atClamp / n, nonFinite: nonFinite / n, subnormal: subnormal / n, logMean: logSum / Math.max(1, lit) }
}

/**
 * Reflective metering on the GPU (no readback): log2 luminance at 64 × 64
 * (4 × 4 taps each), averaged down to 1 × 1, then the residual in stops that
 * brings the log average to the reflective meter's key (K = 12.5: 0.104 of
 * the exposure's white), within ±limit.
 */
function reflectiveMeter(renderer, hdr: RenderTarget, limit: number, calibrationEv: number) {
  const sizes = [64, 16, 4, 1]
  const targets = sizes.map((n) => makeTarget(n, n, false))
  const quads = []
  const src0 = texture(hdr.texture)
  const first = new NodeMaterial()
  first.fragmentNode = Fn(() => {
    const acc = float(0).toVar()
    const base = uv()
    Loop({ start: 0, end: 4, name: 'a' }, ({ a }) => {
      Loop({ start: 0, end: 4, name: 'b' }, ({ b }) => {
        const o = vec2(float(a).add(0.5).div(4).sub(0.5), float(b).add(0.5).div(4).sub(0.5)).div(64)
        const c = src0.sample(base.add(o)).level(0).rgb
        acc.addAssign(log2(max(dot(c, vec3(0.2126, 0.7152, 0.0722)), 1e-7)))
      })
    })
    return vec4(acc.div(16), 0, 0, 1)
  })()
  quads.push(new QuadMesh(first))
  for (let i = 1; i < sizes.length; i++) {
    const src = texture(targets[i - 1].texture)
    const n = sizes[i - 1]
    const m = new NodeMaterial()
    const last = i === sizes.length - 1
    m.fragmentNode = Fn(() => {
      const acc = float(0).toVar()
      const base = uv()
      Loop({ start: 0, end: 4, name: 'a' }, ({ a }) => {
        Loop({ start: 0, end: 4, name: 'b' }, ({ b }) => {
          const o = vec2(float(a).add(0.5).div(4).sub(0.5), float(b).add(0.5).div(4).sub(0.5)).mul(4 / n)
          acc.addAssign(src.sample(base.add(o)).level(0).r)
        })
      })
      const mean = acc.div(16)
      if (!last) return vec4(mean, 0, 0, 1)
      // Key: 0.104 × 2^calibration; residual = log2(key) − mean, within ±limit.
      const key = Math.log2(0.104) + calibrationEv
      return vec4(clamp(float(key).sub(mean), -limit, limit), mean, 0, 1)
    })()
    quads.push(new QuadMesh(m))
  }
  return {
    result: targets[targets.length - 1],
    render() {
      const target = renderer.getRenderTarget()
      quads.forEach((q, i) => {
        renderer.setRenderTarget(targets[i])
        q.render(renderer)
      })
      renderer.setRenderTarget(target)
    },
    dispose() {
      for (const t of targets) t.dispose()
      for (const q of quads) q.material.dispose()
    },
  }
}
