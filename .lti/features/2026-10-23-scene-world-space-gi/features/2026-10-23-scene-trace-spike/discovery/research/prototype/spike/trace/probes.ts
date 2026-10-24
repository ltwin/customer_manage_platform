// S0 group 6, probes (scene-trace-spike design「判定标准」组 6): a two-level cascade of DDGI probes (Majercik et al.
// 2019) on the world-space layer, centred ahead of the camera:
//   rays     each probe traces RAYS rays a frame on a spherical Fibonacci set turned at random; a hit's radiance is
//            Lambertian: albedo × (direct light, through shadow rays, + last frame's probes at the hit: the bounces)
//            plus emission; a miss is the environment's radiance; a back face counts toward the probe's switching off
//   update   per probe, an 8 × 8 octahedral irradiance and a 16 × 16 octahedral depth (mean, mean²; ?octDepth=8 for
//            the coarser one, which leaks at thin walls) blended into the history (a hysteresis or a running mean)
//   sample   at a surface point: the 8 probes around it in each level, weighted by trilinear, the probe's side
//            ((n·dir + 1) / 2)² + 0.2, and Chebyshev visibility from the depth moments; the levels cross-faded over
//            the inner level's last cell
// and compared, at irradiance, against what the product draws today (the environment map's irradiance, A_env, times
// the SSGI's AO) and against a reference: each pixel's own cosine-weighted paths, REF_BOUNCES hits traced and shaded
// as the probes' rays are (the last also reading the probes, so their leaks reach the reference only through two
// albedos), so the difference is the probes' own error (leaks, resolution). The composite as the Epic's「合成的接入
// 点」: A_env replaced by kd · c_E · E_probe times a share of the SSGI's AO, its gi at a gain g (GI_INTENSITY's
// stand-in).
import { BasicShadowMap, BackSide, NoBlending, DirectionalLight, FloatType, HalfFloatType, HemisphereLight, Matrix3, Matrix4, Mesh, RenderTarget, Scene, SphereGeometry, SpotLight, UnsignedByteType, Vector3, type Object3D, type Texture } from 'three'
import * as tsl from 'three/tsl'
import { MeshBasicNodeMaterial, PassNode, PMREMGenerator, QuadMesh, type WebGPURenderer } from 'three/webgpu'
import { MeshBVH, SAH } from 'three-mesh-bvh'
import { giAmbient, giBase, giEnvSpec, GILightingPass, installGISplit } from '../../src/creative-scene/render/giSplit.ts'
import { ssgi } from '../../src/creative-scene/render/nodes/SSGINode.js'
import './computePatches.ts'
import { materialTable } from './hitShade.ts'
import { compare, EPSILONS, proxiesAt, strip } from './proxy.ts'
import { TraceLayer } from './layer.ts'
import type { TSLNode } from './patterns.ts'
import type { Built } from './scene.ts'
import { materialAt } from './shading.ts'
import { traceShim, traceWithinShim } from './trace.ts'

/* eslint-disable @typescript-eslint/no-explicit-any -- TSL is loosely typed */
type Op = (...args: TSLNode[]) => TSLNode
const T = tsl as unknown as Record<string, Op> & { Fn: typeof tsl.Fn; If: typeof tsl.If; Loop: (...a: any[]) => void }
const { abs, clamp, cos, dot, float, floor, fract, int, length, max, min, mix, normalize, pow, select, sin, sqrt, uint, uniform, vec2, vec3, vec4 } = T
type Device = { limits: Record<string, number>; queue: { onSubmittedWorkDone(): Promise<void> } }

const W = 1536
const H = 1024
/** Probes per level, in a grid of DIMS (?dims=16,8,16). */
const DIMS = (new URLSearchParams(location.search).get('dims') ?? '16,16,16').split(',').map(Number) as [number, number, number]
const PER = DIMS[0] * DIMS[1] * DIMS[2]
const LEVELS = 2
const TOTAL = PER * LEVELS
/** Texels per side of a probe's octahedral irradiance, and of its depth. */
const OCT = 8
const TEXELS = OCT * OCT
const OCTD = Number(new URLSearchParams(location.search).get('octDepth') ?? 16)
const DTEXELS = OCTD * OCTD
/** Hits a reference path traces. */
const REF_BOUNCES = 3
const TAU = 2 * Math.PI
const median = (v: number[]) => [...v].sort((a, b) => a - b)[Math.floor(v.length / 2)]
const round = (v: number, d = 3) => Math.round(v * 10 ** d) / 10 ** d
const lum = (r: number, g: number, b: number) => 0.2126 * r + 0.7152 * g + 0.0722 * b
const srgb = (v: number) => 255 * Math.min(1, Math.max(0, v <= 0.0031308 ? 12.92 * v : 1.055 * Math.pow(v, 1 / 2.4) - 0.055))

// ---------------------------------------------------------------------------------------------------------------
// Octahedral mapping, without If (a Switch after an If chains onto it in r186: reflections.md).

const signs = (v: TSLNode) => vec2(select(v.x.greaterThanEqual(0), float(1), float(-1)), select(v.y.greaterThanEqual(0), float(1), float(-1)))
const octEncode = (d: TSLNode) => {
  const n = d.div(abs(d.x).add(abs(d.y)).add(abs(d.z))).toVar()
  const folded = vec2(float(1).sub(abs(n.y)), float(1).sub(abs(n.x))).mul(signs(n.xy))
  return select(n.z.lessThan(0), folded, n.xy).mul(0.5).add(0.5)
}
const octDecode = (uv: TSLNode) => {
  const f = uv.mul(2).sub(1).toVar()
  const z = float(1).sub(abs(f.x)).sub(abs(f.y))
  const t = clamp(z.negate(), 0, 1)
  const xy = f.sub(signs(f).mul(t))
  return normalize(vec3(xy, z))
}

export async function runProbes(renderer: WebGPURenderer, built: Built, scene: string, query: URLSearchParams) {
  const device = (renderer.backend as unknown as { device: Device }).device
  const result: Record<string, unknown> = { scene }
  renderer.setSize(W, H, false)
  built.camera.aspect = W / H
  built.camera.updateProjectionMatrix()
  installGISplit()
  const nodeFrame = (renderer as unknown as { _nodes: { nodeFrame: { update(): void } } })._nodes.nodeFrame

  // --- Lights: the scene's sun and spots; no hemisphere light (the ambient is then A_env alone); the environment a
  // sky for the sunny scenes (a gradient, no sun disc: the sun is the light), near black for the dark one.
  const sun = built.scene.children.find((o): o is DirectionalLight => o instanceof DirectionalLight)!
  const hemi = built.scene.children.find((o): o is HemisphereLight => o instanceof HemisphereLight)
  if (hemi) hemi.intensity = 0
  const spots = built.scene.children.filter((o): o is SpotLight => o instanceof SpotLight).slice(0, 2)
  const dark = scene === 'cornell'
  if (dark) sun.intensity = 0
  const meshes = built.meshes.filter((m) => m.visible)
  renderer.shadowMap.enabled = true
  renderer.shadowMap.type = BasicShadowMap
  meshes.forEach((m) => {
    m.castShadow = true
    m.receiveShadow = true
  })
  sun.castShadow = !dark
  Object.assign(sun.shadow.camera, { left: -8, right: 8, top: 8, bottom: -8, near: 1, far: 150 })
  sun.shadow.camera.updateProjectionMatrix()
  sun.shadow.mapSize.set(2048, 2048)
  sun.shadow.bias = -0.0005
  sun.shadow.normalBias = 0.02
  spots.forEach((s) => {
    s.castShadow = true
    s.shadow.mapSize.set(1024, 1024)
    s.shadow.bias = -0.0005
  })
  const skyScene = new Scene()
  const skyMaterial = new MeshBasicNodeMaterial({ side: BackSide })
  const up = tsl.positionWorld.normalize().y
  skyMaterial.colorNode = dark ? vec3(0.004, 0.004, 0.005) : mix(vec3(0.75, 0.8, 0.85), vec3(0.25, 0.42, 0.78), clamp(up, 0, 1)).mul(select(up.lessThan(0), float(0.35), float(1))).mul(0.6)
  skyScene.add(new Mesh(new SphereGeometry(100, 32, 16), skyMaterial))
  const envTarget = new PMREMGenerator(renderer).fromScene(skyScene, 0, 0.1, 200)
  built.scene.environment = envTarget.texture
  built.scene.background = null

  // --- What the rays trace (tracing below): the original geometry's trees, built here (the reference scenes are
  // small), or for group 5 (?proxies=) the original's and each proxy's from the workers, traced in turn.
  const proxyMode = query.get('proxies')
  const maxBinding = Math.min(device.limits.maxStorageBufferBindingSize, device.limits.maxBufferSize)
  const table = materialTable(meshes)

  // --- The cascade: level 0 spacing s0, level 1 twice it, both ahead of the camera, snapped to their grids. Across
  // they are centred ?ahead= of their width in front of the camera (0.5: the near face at the camera); up and down
  // the camera sits at ?below= of their height (centred: 16 × 8 × 16 with the camera at three quarters still ends at
  // the floor, so the floor is never inside level 0).
  const s0 = Number(query.get('spacing') ?? (scene === 'backlit-room' ? 0.4 : scene === 'stress' ? 0.5 : 0.25))
  const forward = built.camera.getWorldDirection(new Vector3())
  const ahead = Number(query.get('ahead') ?? 0.5)
  const centre = built.camera.position.clone().addScaledVector(forward, DIMS[2] * s0 * ahead)
  centre.y = built.camera.position.y
  const below = Number(query.get('below') ?? 0.5)
  const levelOrigin = (s: number) => new Vector3(...DIMS.map((d, k) => Math.floor((centre.getComponent(k) - d * s * (k === 1 ? below : 0.5)) / s) * s))
  const spacing = [s0, 2 * s0]
  const origins = spacing.map(levelOrigin)
  result.cascade = { dims: DIMS, below, ahead, octDepth: OCTD, rays: Number(query.get('rays') ?? 64), spacing, origins: origins.map((o) => o.toArray().map((v) => round(v, 2))), probes: TOTAL }
  const uOrigin = origins.map((o) => uniform(o))
  const uSpacing = spacing.map((s) => uniform(s))
  const rays = Number(query.get('rays') ?? 64)
  const rotation = uniform(new Matrix3())
  const hysteresis = uniform(0)
  const bounce = uniform(1)
  const blendCells = uniform(Number(query.get('blend') ?? 1))

  const rayBuffer = tsl.instancedArray(TOTAL * rays, 'vec4')
  const irradiance = tsl.instancedArray(TOTAL * TEXELS, 'vec4')
  const depth = tsl.instancedArray(TOTAL * DTEXELS, 'vec2')
  const state = tsl.instancedArray(TOTAL, 'vec4')

  const dims = vec3(...DIMS)
  const levelOf = (lv: number) => ({ origin: uOrigin[lv], spacing: uSpacing[lv] })
  const probeOf = (p: TSLNode) => {
    const lv = p.div(PER)
    const q = p.mod(PER)
    const cell = vec3(float(q.mod(DIMS[0])), float(q.div(DIMS[0]).mod(DIMS[1])), float(q.div(DIMS[0] * DIMS[1])))
    const origin = select(lv.equal(0), uOrigin[0], uOrigin[1])
    const s = select(lv.equal(0), uSpacing[0], uSpacing[1])
    return { position: origin.add(cell.add(0.5).mul(s)), spacing: s }
  }
  const rayDir = (k: TSLNode) => {
    const phi = fract(float(k).mul(0.6180339887)).mul(TAU)
    const c = float(1).sub(float(k).mul(2).add(1).div(rays))
    const st = sqrt(max(float(1).sub(c.mul(c)), 0))
    return normalize(rotation.mul(vec3(st.mul(cos(phi)), st.mul(sin(phi)), c)))
  }
  const texelAt = (buffer: TSLNode, res: number, p: TSLNode, x: TSLNode, y: TSLNode) => buffer.element(p.mul(res * res).add(uint(clamp(y, 0, res - 1).mul(res).add(clamp(x, 0, res - 1)))))
  const bilinear = (buffer: TSLNode, res: number, p: TSLNode, dir: TSLNode, pick: (v: TSLNode) => TSLNode) => {
    const uv = octEncode(dir).mul(res).sub(0.5).toVar()
    const i0 = floor(uv)
    const fr = uv.sub(i0)
    const x0 = int(i0.x), y0 = int(i0.y)
    const at = (dx: number, dy: number) => pick(texelAt(buffer, res, p, x0.add(dx), y0.add(dy)))
    return mix(mix(at(0, 0), at(1, 0), fr.x), mix(at(0, 1), at(1, 1), fr.x), fr.y)
  }

  /** One level's irradiance at a point, with its weight sum (0 where no probe can see the point). */
  const levelSample = (lv: number, P: TSLNode, n: TSLNode, V: TSLNode) => {
    const { origin, spacing: s } = levelOf(lv)
    const biased = P.add(n.mul(0.2).add(V.mul(0.8)).mul(s.mul(0.3))).toVar()
    const g = biased.sub(origin).div(s).sub(0.5).toVar()
    const base = floor(g).toVar()
    const f = g.sub(base).toVar()
    let sum: TSLNode = vec3(0)
    let wsum: TSLNode = float(0)
    for (let c = 0; c < 8; c++) {
      const off = vec3(c & 1, (c >> 1) & 1, (c >> 2) & 1)
      const idx = clamp(base.add(off), vec3(0), dims.sub(1))
      const p = uint(int(idx.x).add(int(idx.y).mul(DIMS[0])).add(int(idx.z).mul(DIMS[0] * DIMS[1])).add(lv * PER)).toVar()
      const probe = origin.add(idx.add(0.5).mul(s))
      const toProbe = normalize(probe.sub(P))
      const tri = mix(vec3(1).sub(f), f, off)
      const side = dot(toProbe, n).add(1).mul(0.5)
      const fromProbe = biased.sub(probe).toVar()
      const d = length(fromProbe)
      const m = bilinear(depth, OCTD, p, normalize(fromProbe), (v) => v.xy)
      const variance = abs(m.x.mul(m.x).sub(m.y))
      const cheb = select(d.greaterThan(m.x), variance.div(variance.add(d.sub(m.x).mul(d.sub(m.x)))), float(1))
      let w = side.mul(side).add(0.2).mul(max(pow(cheb, 3), 0.0)).max(1e-6)
      // Crush the small weights (DDGI): they otherwise leak through thin walls.
      w = select(w.lessThan(0.2), w.mul(w).mul(w).div(0.04), w)
      w = w.mul(tri.x).mul(tri.y).mul(tri.z).mul(state.element(p).w)
      sum = sum.add(bilinear(irradiance, OCT, p, n, (v) => v.xyz).mul(w))
      wsum = wsum.add(w)
    }
    return { e: sum.div(max(wsum, 1e-6)), w: wsum }
  }
  /** The cascade at a point: level 0 inside its box, level 1 beyond, cross-faded over blendCells cells (k0, level 0's share). */
  const cascadeAt = (P: TSLNode, n: TSLNode, V: TSLNode) => {
    const inside = (lv: number) => {
      const { origin, spacing: s } = levelOf(lv)
      const local = P.sub(origin).div(s)
      const edge = min(min(local, dims.sub(local)).x, min(min(local, dims.sub(local)).y, min(local, dims.sub(local)).z))
      return clamp(edge.sub(0.5).div(max(blendCells, 1e-3)), 0, 1)
    }
    const a = levelSample(0, P, n, V)
    const b = levelSample(1, P, n, V)
    const k0 = select(blendCells.greaterThan(0), inside(0), select(inside(0).greaterThan(0), float(1), float(0))).mul(select(a.w.greaterThan(1e-5), float(1), float(0))).toVar()
    return { a, b, k0, e: mix(b.e, a.e, k0) }
  }
  const sampleProbes = (P: TSLNode, n: TSLNode, V: TSLNode) => cascadeAt(P, n, V).e

  // --- The radiance a ray brings back: Lambertian at its hit, or the environment.
  const sunDir = uniform(sun.position.clone().sub(sun.target.position).normalize())
  const sunColor = uniform(new Vector3(sun.color.r, sun.color.g, sun.color.b).multiplyScalar(sun.intensity))
  const spotU = spots.map((s) => ({
    position: uniform(s.getWorldPosition(new Vector3())),
    toLight: uniform(s.getWorldPosition(new Vector3()).sub(s.target.getWorldPosition(new Vector3())).normalize()),
    color: uniform(new Vector3(s.color.r, s.color.g, s.color.b).multiplyScalar(s.intensity)),
    coneCos: uniform(Math.cos(s.angle)),
    penumbraCos: uniform(Math.cos(s.angle * (1 - s.penumbra))),
    decay: uniform(s.decay),
    distance: uniform(s.distance),
  }))
  const env = envTarget.texture as Texture
  /** A layer over bvhOf's trees, and what traces it: the direct light at a point, a ray's radiance, the probes' rays. */
  const tracing = (bvhOf: (o: Object3D) => MeshBVH) => {
    const layer = new TraceLayer(meshes, bvhOf, maxBinding)
    meshes.forEach((m, i) => layer.materialOf.set(m, i))
    layer.update()
    const traceFirst = traceShim(layer)
    const traceWithin = traceWithinShim(layer)
    const store = layer.storage as unknown as { transforms: TSLNode; attributes: TSLNode }
    const directAt = (P: TSLNode, n: TSLNode) => {
      const lift = P.add(n.mul(1e-3))
      const sunHit = traceFirst(lift, sunDir).toVar('probeSun') as unknown as { get(k: string): TSLNode }
      let e: TSLNode = sunColor.mul(max(dot(n, sunDir), 0)).mul(select(sunHit.get('didHit'), float(0), float(1)))
      spotU.forEach((s, k) => {
        const v = s.position.sub(P).toVar()
        const dist = length(v)
        const l = v.div(dist)
        const spot = tsl.smoothstep(s.coneCos, s.penumbraCos, dot(l, s.toLight))
        const falloff = float(1).div(max(pow(dist, s.decay), 0.01))
        const cut = select(s.distance.greaterThan(0), pow(clamp(float(1).sub(pow(dist.div(s.distance), 4)), 0, 1), 2), float(1))
        const blocked = traceWithin(lift, l, dist.sub(2e-3)).toVar(`probeSpot${k}`) as unknown as { get(k: string): TSLNode }
        e = e.add(s.color.mul(spot).mul(falloff).mul(cut).mul(max(dot(n, l), 0)).mul(select(blocked.get('didHit'), float(0), float(1))))
      })
      return e
    }
    /** Radiance back along dir from origin: { radiance, dist (negative on a back face) }. */
    const radiance = (origin: TSLNode, dir: TSLNode, footprint: TSLNode) => {
      const out = vec4(0).toVar()
      const hit = traceFirst(origin, dir).toVar('probeRay') as unknown as { get(k: string): TSLNode }
      out.assign(vec4(tsl.pmremTexture(env as never, dir as never, float(0) as never) as TSLNode, 1e4))
      T.If(hit.get('didHit'), () => {
        const t = float(hit.get('dist')).toVar()
        const back = hit.get('side').lessThan(0)
        const slot = store.transforms.element(hit.get('objectIndex'))
        const inv = T.mat4(slot.get('inverseMatrixWorld')).toVar()
        const M = T.mat4(slot.get('matrixWorld')).toVar()
        const mi = slot.get('_alignment0').toVar()
        const P = origin.add(dir.mul(t)).toVar()
        const n = vec3(hit.get('normal')).toVar()
        const scale = vec3(length(M.mul(vec4(1, 0, 0, 0)).xyz), length(M.mul(vec4(0, 1, 0, 0)).xyz), length(M.mul(vec4(0, 0, 1, 0)).xyz)).toVar()
        const nObj = normalize(vec4(n, 0).mul(T.mat4(slot.get('matrixWorld'))).xyz).toVar()
        const m = materialAt(table, mi, inv.mul(vec4(P, 1)).xyz.mul(scale), nObj.div(scale), footprint, vec3(0), vec3(0), 'zero')
        const indirect = sampleProbes(P, n, dir.negate()).mul(bounce)
        const L = m.albedo.mul(float(1).sub(m.metal)).div(Math.PI).mul(directAt(P, n).add(indirect)).add(m.emission)
        out.assign(vec4(select(back, vec3(0), L), select(back, t.negate(), t)))
      })
      return out
    }
    // The probes' rays.
    const rayKernel = T.Fn(() => {
      const i = tsl.instanceIndex
      const p = i.div(rays)
      const k = i.mod(rays)
      const probe = probeOf(p)
      rayBuffer.element(i).assign(radiance(probe.position, rayDir(k), probe.spacing))
    })().compute(TOTAL * rays, [64])
    return { layer, traceFirst, store, directAt, rayKernel }
  }
  type Tracing = ReturnType<typeof tracing>
  /** The original geometry's trees, built here, SAH as the workers build them. */
  const originalHere = () => {
    const trees = new Map(meshes.map((m) => [m.geometry, (m.geometry.boundsTree as MeshBVH | undefined) ?? new MeshBVH(m.geometry, { strategy: SAH, targetLeafSize: 10, maxDepth: 40 })]))
    return (o: Object3D) => trees.get((o as Mesh).geometry)!
  }
  let current: Tracing | null = proxyMode ? null : tracing(originalHere())

  // --- The probes' kernels (their rays are the layer's, in tracing).
  const updateKernel = T.Fn(() => {
    const i = tsl.instanceIndex
    const p = i.div(TEXELS)
    const t = i.mod(TEXELS)
    const probe = probeOf(p)
    const dt = octDecode(vec2(float(t.mod(OCT)).add(0.5), float(t.div(OCT)).add(0.5)).div(OCT)).toVar()
    const irr = vec3(0).toVar()
    const wi = float(0).toVar()
    const backs = float(0).toVar()
    T.Loop(rays, ({ i: k }: { i: TSLNode }) => {
      const ray = rayBuffer.element(p.mul(rays).add(uint(k))).toVar()
      const c = max(dot(dt, rayDir(uint(k))), 0)
      irr.addAssign(ray.xyz.mul(c))
      wi.addAssign(c)
      backs.addAssign(select(ray.w.lessThan(0), float(1), float(0)))
    })
    const index = p.mul(TEXELS).add(t)
    // Σ L·cos / Σ cos over uniform directions is the cosine-weighted mean radiance, E / π: stored as E.
    irradiance.element(index).assign(vec4(mix(irr.div(max(wi, 1e-4)).mul(Math.PI), irradiance.element(index).xyz, hysteresis), 1))
    // Switched off when more than a quarter of its rays see back faces: it is inside something.
    T.If(t.equal(0), () => {
      state.element(p).assign(vec4(probe.position, select(backs.greaterThan(float(rays).mul(0.25)), float(0), float(1))))
    })
  })().compute(TOTAL * TEXELS, [64])
  const depthKernel = T.Fn(() => {
    const i = tsl.instanceIndex
    const p = i.div(DTEXELS)
    const t = i.mod(DTEXELS)
    const probe = probeOf(p)
    const dt = octDecode(vec2(float(t.mod(OCTD)).add(0.5), float(t.div(OCTD)).add(0.5)).div(OCTD)).toVar()
    const mom = vec2(0).toVar()
    const wd = float(0).toVar()
    const maxDist = probe.spacing.mul(4)
    T.Loop(rays, ({ i: k }: { i: TSLNode }) => {
      const ray = rayBuffer.element(p.mul(rays).add(uint(k))).toVar()
      // A back face's distance shortened, as DDGI does, so the probe reads as inside.
      const d = min(select(ray.w.lessThan(0), ray.w.negate().mul(0.2), ray.w), maxDist)
      const cd = pow(max(dot(dt, rayDir(uint(k))), 0), 50)
      mom.addAssign(vec2(d, d.mul(d)).mul(cd))
      wd.addAssign(cd)
    })
    const index = p.mul(DTEXELS).add(t)
    depth.element(index).assign(mix(mom.div(max(wd, 1e-4)), depth.element(index), hysteresis))
  })().compute(TOTAL * DTEXELS, [64])

  // --- The raster: prepass, split lighting, SSGI (the product's own), read back once each (SSGI averaged).
  const prepass = pass(built, 'prepass')
  const lit = new GILightingPass(PassNode.COLOR, built.scene, built.camera, { samples: 0 })
  lit.setMRT(tsl.mrt({ output: tsl.output, base: giBase, ambient: giAmbient, envSpec: giEnvSpec }))
  lit.getTexture('base').type = UnsignedByteType
  const normalPacked: TSLNode = prepass.getTextureNode()
  const depthNode: TSLNode = prepass.getTextureNode('depth')
  const normalNode: TSLNode = tsl.sample((uv: TSLNode) => tsl.unpackRGBToNormal(normalPacked.sample(uv).rgb))
  const gi: TSLNode = ssgi(lit.getTextureNode('output'), depthNode, normalNode, built.camera as never)
  gi.giIntensity.value = 1
  const rt = new RenderTarget(W, H, { type: FloatType })
  const draw = async (node: TSLNode) => {
    // NoBlending: an opaque material's alpha is otherwise forced to 1 (the base's metalness, the SSGI's AO).
    const material = new MeshBasicNodeMaterial({ blending: NoBlending })
    material.colorNode = node
    const quad = new QuadMesh(material)
    nodeFrame.update()
    renderer.setRenderTarget(rt)
    quad.render(renderer)
    renderer.setRenderTarget(null)
    const out = new Float32Array((await renderer.readRenderTargetPixelsAsync(rt, 0, 0, W, H)) as Float32Array)
    material.dispose()
    return out
  }
  const lighting = {
    output: await draw(vec4(lit.getTextureNode('output').rgb, 1)),
    base: await draw(lit.getTextureNode('base')),
    ambient: await draw(vec4(lit.getTextureNode('ambient').rgb, lit.getTextureNode('ambient').a)),
    envSpec: await draw(vec4(lit.getTextureNode('envSpec').rgb, 1)),
    depth: await draw(vec4(depthNode.r, 0, 0, 1)),
    // Packed: the quad's output loses negative values.
    normal: await draw(vec4(normalNode.rgb.mul(0.5).add(0.5), 1)),
  }
  const ssgiSum = new Float32Array(W * H * 4)
  const ssgiFrames = 32
  const giMaterial = new MeshBasicNodeMaterial({ blending: NoBlending })
  giMaterial.colorNode = vec4(gi.getTextureNode().rgb, gi.getTextureNode().a)
  const giQuad = new QuadMesh(giMaterial)
  for (let f = 0; f < ssgiFrames; f++) {
    nodeFrame.update()
    renderer.setRenderTarget(rt)
    giQuad.render(renderer)
    renderer.setRenderTarget(null)
    const one = new Float32Array((await renderer.readRenderTargetPixelsAsync(rt, 0, 0, W, H)) as Float32Array)
    for (let k = 0; k < one.length; k++) ssgiSum[k] += one[k] / ssgiFrames
  }

  // The pixels' surfaces (world position, normal toward the viewer) for the per-pixel kernels.
  const inverseView = built.camera.matrixWorld.clone()
  const inverseProjection = built.camera.projectionMatrixInverse.clone()
  const eye = built.camera.position.clone()
  const surfaces = new Float32Array(W * H * 8)
  for (let y = 0; y < H; y++) {
    for (let x = 0; x < W; x++) {
      const p = y * W + x
      const d = lighting.depth[4 * p]
      if (!(d < 1)) continue
      const ndc = new Vector3(((x + 0.5) / W) * 2 - 1, 1 - ((y + 0.5) / H) * 2, d)
      const world = ndc.applyMatrix4(inverseProjection).applyMatrix4(inverseView)
      const nv = new Vector3(lighting.normal[4 * p] * 2 - 1, lighting.normal[4 * p + 1] * 2 - 1, lighting.normal[4 * p + 2] * 2 - 1).transformDirection(inverseView)
      surfaces.set([world.x, world.y, world.z, 1, nv.x, nv.y, nv.z, 0], 8 * p)
    }
  }
  const surfaceBuffer = tsl.instancedArray(surfaces, 'vec4')
  const surfaceAt = (p: TSLNode) => ({ P: surfaceBuffer.element(p.mul(2)), n: surfaceBuffer.element(p.mul(2).add(1)).xyz })
  const eyeU = uniform(eye)

  // E_probe and E_env per pixel; the reference gathered per pixel, RREF cosine-weighted rays a pass, summed.
  const eProbe = tsl.instancedArray(W * H, 'vec4')
  const eEnv = tsl.instancedArray(W * H, 'vec4')
  const eRef = tsl.instancedArray(W * H, 'vec4')
  const seed = uniform(0, 'uint')
  const RREF = 8
  const pixelKernel = T.Fn(() => {
    const p = tsl.instanceIndex
    const s = surfaceAt(p)
    T.If(s.P.w.greaterThan(0), () => {
      const V = normalize(eyeU.sub(s.P.xyz))
      eProbe.element(p).assign(vec4(sampleProbes(s.P.xyz, s.n, V), 1))
      eEnv.element(p).assign(vec4(tsl.pmremTexture(env as never, s.n as never, float(1) as never) as TSLNode, 1).mul(vec4(Math.PI, Math.PI, Math.PI, 1)))
    })
  })().compute(W * H, [64])
  /** A cosine-weighted direction about n from two hashes. */
  const cosineAbout = (n: TSLNode, key: TSLNode) => {
    const tt = normalize(tsl.cross(select(abs(n.y).lessThan(0.999), vec3(0, 1, 0), vec3(1, 0, 0)), n)).toVar()
    const bb = tsl.cross(n, tt)
    const u1 = tsl.hash(key.add(seed.mul(0x9e3779b9)))
    const u2 = tsl.hash(key.add(0x68e31da4).add(seed.mul(0x85ebca6b)))
    const r = sqrt(u1)
    const phi = u2.mul(TAU)
    return normalize(tt.mul(r.mul(cos(phi))).add(bb.mul(r.mul(sin(phi)))).add(n.mul(sqrt(float(1).sub(u1)))))
  }
  const refKernelFor = ({ traceFirst, store, directAt }: Tracing) => T.Fn(() => {
    const p = tsl.instanceIndex
    const s = surfaceAt(p)
    T.If(s.P.w.greaterThan(0), () => {
      const sum = vec3(0).toVar()
      // Loops, not unrolled: each hit's shading inlines every preset and sixteen probes.
      T.Loop(RREF, ({ i: k }: { i: TSLNode }) => {
        const o = s.P.xyz.add(s.n.mul(2e-3)).toVar()
        const d = cosineAbout(s.n, p.mul(64).add(uint(k).mul(8))).toVar()
        const through = vec3(1).toVar()
        const alive = float(1).toVar()
        T.Loop(REF_BOUNCES, ({ i: b }: { i: TSLNode }) => {
          T.If(alive.greaterThan(0), () => {
            const hit = traceFirst(o, d).toVar('refRay') as unknown as { get(k: string): TSLNode }
            const t = float(hit.get('dist')).toVar()
            const slot = store.transforms.element(hit.get('objectIndex'))
            const inv = T.mat4(slot.get('inverseMatrixWorld')).toVar()
            const M = T.mat4(slot.get('matrixWorld')).toVar()
            const mi = slot.get('_alignment0').toVar()
            const P = o.add(d.mul(t)).toVar()
            const nRaw = vec3(hit.get('normal')).toVar()
            const nh = select(dot(nRaw, d).greaterThan(0), nRaw.negate(), nRaw).toVar()
            const scale = vec3(length(M.mul(vec4(1, 0, 0, 0)).xyz), length(M.mul(vec4(0, 1, 0, 0)).xyz), length(M.mul(vec4(0, 0, 1, 0)).xyz)).toVar()
            const nObj = normalize(vec4(nRaw, 0).mul(M).xyz).toVar()
            const m = materialAt(table, mi, inv.mul(vec4(P, 1)).xyz.mul(scale), nObj.div(scale), float(0.01), vec3(0), vec3(0), 'zero')
            const kd = m.albedo.mul(float(1).sub(m.metal)).toVar()
            const last = select(int(b).equal(REF_BOUNCES - 1), float(1), float(0))
            const front = select(hit.get('side').lessThan(0), float(0), float(1))
            const shaded = kd.div(Math.PI).mul(directAt(P, nh).add(sampleProbes(P, nh, d.negate()).mul(bounce).mul(last))).add(m.emission)
            const sky = vec3(tsl.pmremTexture(env as never, d as never, float(0) as never) as TSLNode)
            sum.addAssign(through.mul(select(hit.get('didHit'), shaded.mul(front), sky)))
            // On, from a front face that is not the last, toward a new cosine-weighted direction.
            alive.assign(select(hit.get('didHit'), front, float(0)).mul(float(1).sub(last)))
            through.mulAssign(kd)
            o.assign(P.add(nh.mul(1e-3)))
            d.assign(cosineAbout(nh, p.mul(64).add(uint(k).mul(8)).add(uint(b)).add(1)))
          })
        })
      })
      eRef.element(p).addAssign(vec4(sum, RREF))
    })
  })().compute(W * H, [64])
  const refKernel = current && refKernelFor(current)
  // The levels apart at each pixel, for the transition: (level 0, level 1, k0, level 0's weight).
  const levels = tsl.instancedArray(W * H, 'vec4')
  const levelKernel = T.Fn(() => {
    const p = tsl.instanceIndex
    const s = surfaceAt(p)
    T.If(s.P.w.greaterThan(0), () => {
      const c = cascadeAt(s.P.xyz, s.n, normalize(eyeU.sub(s.P.xyz)))
      const l = (e: TSLNode) => dot(e, vec3(0.2126, 0.7152, 0.0722))
      levels.element(p).assign(vec4(l(c.a.e), l(c.b.e), c.k0, c.a.w))
    })
  })().compute(W * H, [64])

  // --- Frames: the probes from nothing, a turn at random each frame.
  const turn = (f: number) => {
    const axis = new Vector3(Math.sin(f * 12.9898), Math.sin(f * 78.233), Math.sin(f * 37.719)).normalize()
    rotation.value.setFromMatrix4(new Matrix4().makeRotationAxis(axis, ((f * 0.618034) % 1) * TAU))
  }
  const probeFrame = async (f: number, alpha: number) => {
    turn(f)
    hysteresis.value = alpha
    renderer.compute(current!.rayKernel)
    renderer.compute(updateKernel)
    renderer.compute(depthKernel)
    await device.queue.onSubmittedWorkDone()
  }

  // Sample points per region, for the convergence curves: up to 64 a region, read every frame.
  const regions = (await (await fetch('/scripts/fixtures/scene-reference/gi-regions.json')).json())[scene] as Record<string, number[]> | undefined
  const points: number[] = []
  const pointRegion: string[] = []
  for (const [name, [x0, y0, x1, y1]] of Object.entries(regions ?? {})) {
    let n = 0
    for (let k = 0; k < 400 && n < 64; k++) {
      const x = Math.floor((x0 + ((k * 0.754877) % 1) * (x1 - x0)) * W)
      const y = Math.floor((y0 + ((k * 0.569840) % 1) * (y1 - y0)) * H)
      const p = y * W + x
      if (surfaces[8 * p + 3] > 0) {
        points.push(p)
        pointRegion.push(name)
        n++
      }
    }
  }
  const pointBuffer = tsl.instancedArray(new Uint32Array(Math.max(points.length, 1)).map((_, k) => points[k] ?? 0), 'uint')
  const pointOut = tsl.instancedArray(Math.max(points.length, 1), 'vec4')
  const pointKernel = T.Fn(() => {
    const k = tsl.instanceIndex
    const s = surfaceAt(pointBuffer.element(k))
    pointOut.element(k).assign(vec4(sampleProbes(s.P.xyz, s.n, normalize(eyeU.sub(s.P.xyz))), 1))
  })().compute(Math.max(points.length, 1), [64])
  /** Each point's luminance, and the region means. */
  const readPoints = async () => {
    renderer.compute(pointKernel)
    const v = new Float32Array(await renderer.getArrayBufferAsync(pointOut.value as never))
    const raw = pointRegion.map((_, k) => lum(v[4 * k], v[4 * k + 1], v[4 * k + 2]))
    const by: Record<string, number[]> = {}
    pointRegion.forEach((name, k) => (by[name] ??= []).push(raw[k]))
    return { raw, means: Object.fromEntries(Object.entries(by).map(([name, l]) => [name, l.reduce((a, b) => a + b, 0) / l.length])) }
  }
  const reset = () => {
    for (const b of [irradiance, depth, state]) (b.value as unknown as { array: Float32Array; needsUpdate: boolean }).array.fill(0)
    ;(state.value as unknown as { array: Float32Array }).array.fill(1)
    for (const b of [irradiance, depth, state]) (b.value as unknown as { needsUpdate: boolean }).needsUpdate = true
  }

  // --- The composites over the raster (the SSGI averaged): today's (A_env · ao + 5 · gi), the probes' (kd · c_E ·
  // E_probe times a share of the AO, plus g · gi) and the reference's, over the same direct light and specular.
  const ao = (p: number) => ssgiSum[4 * p + 3]
  const giAt = (p: number, c: number) => ssgiSum[4 * p + c]
  const shareOf = (k: number, p: number) => 1 - k * (1 - ao(p))
  const composite = (E: { EP: Float32Array; EE: Float32Array; ER?: Float32Array }, kind: 'today' | 'probes' | 'ref', g = 0, aoShare = 0) => {
    const { EP, EE, ER } = E
    const out = new Float32Array(W * H * 3)
    for (let p = 0; p < W * H; p++) {
      const o = lighting.output, b = lighting.base, a = lighting.ambient, s = lighting.envSpec
      for (let c = 0; c < 3; c++) {
        const kd = b[4 * p + c] * (1 - b[4 * p + 3])
        const direct = o[4 * p + c] - a[4 * p + c] - s[4 * p + c]
        // c_E: the material's own factor on the environment's irradiance, where it can be read; 0.96 otherwise.
        const cE = kd * EE[4 * p + c] > 1e-3 ? Math.min(1, (a[4 * p + c] * Math.PI) / (kd * EE[4 * p + c])) : 0.96
        let indirect: number
        if (kind === 'today') indirect = a[4 * p + c] * ao(p) + kd * giAt(p, c) * 5
        else if (kind === 'probes') indirect = (kd * cE * EP[4 * p + c] * shareOf(aoShare, p)) / Math.PI + kd * giAt(p, c) * g
        else indirect = (kd * cE * ER![4 * p + c]) / Math.PI
        out[3 * p + c] = surfaces[8 * p + 3] > 0 ? Math.max(0, direct) + indirect + s[4 * p + c] : o[4 * p + c]
      }
    }
    return out
  }
  /** The exposure that puts a picture's 99th percentile at 0.8. */
  const exposureOf = (f: Float32Array) => {
    const ls: number[] = []
    for (let p = 0; p < W * H; p += 7) ls.push(lum(f[3 * p], f[3 * p + 1], f[3 * p + 2]))
    ls.sort((a, b) => a - b)
    return 0.8 / Math.max(ls[Math.floor(ls.length * 0.99)], 1e-6)
  }
  const encode = (f: Float32Array, exposure: number) => {
    const out = new Uint8ClampedArray(W * H * 4)
    for (let p = 0; p < W * H; p++) out.set([srgb(f[3 * p] * exposure), srgb(f[3 * p + 1] * exposure), srgb(f[3 * p + 2] * exposure), 255], 4 * p)
    return out
  }

  // --- Group 5 (?proxies=1, or the bounds): the probes over the original and over each proxy, converged alike (the
  // export's warm-up: 32 frames taken whole, then a running mean, 96 frames in all), composited as research
  // probes.md recommends (the probes alone), and the pictures apart: the original's against each proxy's.
  if (proxyMode) {
    const epsilons = proxyMode === '1' ? EPSILONS : proxyMode.split(',').map(Number)
    const samples = Number(query.get('errorSamples') ?? (scene === 'stress' ? 1000 : 4000))
    const warm = (f: number) => (f < 32 ? 0 : (f - 32) / (f - 31))
    const converge = async (t: Tracing) => {
      current = t
      reset()
      const started = performance.now()
      for (let f = 0; f < 96; f++) await probeFrame(f, warm(f))
      const convergeMs = Math.round(performance.now() - started)
      renderer.compute(pixelKernel)
      return { EP: new Float32Array(await renderer.getArrayBufferAsync(eProbe.value as never)), convergeMs }
    }
    const runs: { name: string; EP: Float32Array; layerMiB: number; convergeMs: number; report?: unknown }[] = []
    for (const [i, epsilon] of epsilons.entries()) {
      const prepared = await proxiesAt(scene, built, epsilon, samples)
      // The original once, from the first bound's trees (the workers build it alike at every bound).
      for (const side of i === 0 ? (['original', 'proxy'] as const) : (['proxy'] as const)) {
        const t = tracing((o) => prepared.pairs.get((o as Mesh).geometry)![side])
        const { EP, convergeMs } = await converge(t)
        runs.push({ name: side === 'original' ? 'original' : `eps${epsilon}`, EP, layerMiB: round(t.layer.bytes / 2 ** 20, 1), convergeMs, ...(side === 'proxy' ? { report: prepared.report } : {}) })
        t.layer.dispose()
      }
    }
    current = null
    const EE = new Float32Array(await renderer.getArrayBufferAsync(eEnv.value as never))
    const floats = runs.map((r) => composite({ EP: r.EP, EE }, 'probes'))
    const exposure = exposureOf(floats[0])
    const images = floats.map((f) => encode(f, exposure))
    const onSurface = (p: number) => surfaces[8 * p + 3] > 0
    const regionMeans = (EP: Float32Array) => Object.fromEntries(Object.entries(regions ?? {}).map(([name, [x0, y0, x1, y1]]) => {
      let sum = 0, n = 0
      for (let y = Math.floor(y0 * H); y < Math.floor(y1 * H); y++) for (let x = Math.floor(x0 * W); x < Math.floor(x1 * W); x++) {
        const p = y * W + x
        if (!onSurface(p)) continue
        sum += lum(EP[4 * p], EP[4 * p + 1], EP[4 * p + 2])
        n++
      }
      return [name, sum / Math.max(n, 1)]
    }))
    const base = regionMeans(runs[0].EP)
    const diffs: Uint8ClampedArray[] = []
    result.proxies = runs.map((r, k) => {
      if (k === 0) return { name: r.name, layerMiB: r.layerMiB, convergeMs: r.convergeMs, E_probe: Object.fromEntries(Object.entries(base).map(([n, v]) => [n, round(v, 4)])) }
      const { diff, ...picture } = compare(images[0], images[k], W, H, onSurface)
      diffs.push(diff)
      const mine = regionMeans(r.EP)
      return { name: r.name, layerMiB: r.layerMiB, convergeMs: r.convergeMs, ...(r.report as object), picture, regions: Object.fromEntries(Object.entries(mine).map(([n, v]) => [n, round(v / Math.max(base[n], 1e-6) - 1, 4)])) }
    })
    result.exposure = round(exposure, 4)
    const pictures = strip(images, W, H, 'mean')
    const differences = strip(diffs, W, H, 'max')
    result.pictures = { 'proxy-strip': jpegOf(pictures.data, pictures.width, pictures.height), 'proxy-differences8x': jpegOf(differences.data, differences.width, differences.height) }
    rt.dispose()
    return result
  }

  // Convergence from nothing, for the export's warm-up and the viewport. Each frame's rays read last frame's probes at
  // their hits, so a frame brings one more bounce in, and a hysteresis α slows every bounce by 1 / (1 − α):
  //   mean        the running mean from the first frame (the Epic's warm-up as written)
  //   restart<R>  R frames each taken whole (hysteresis 0: the bounces in at a frame each), then the running mean
  //               afresh, so the early frames' missing bounces are not averaged in for good
  //   h<α>        hysteresis α after a first frame taken whole (the viewport, from nothing as after an edit)
  //   edit        12 frames taken whole, then hysteresis 0.93 (the viewport following an edit)
  //   edit05      as edit, the 16 frames at hysteresis 0.5 (half the flicker while following)
  // The target: 32 frames taken whole then a running mean of 160. A hysteresis's flicker is the mean frame-to-frame
  // change over its last 16 frames.
  const curves: Record<string, Record<string, number>[]> = {}
  const raws: Record<string, number[][]> = {}
  const FRAMES = Number(query.get('frames') ?? 96)
  const restart = (r: number) => (f: number) => (f < r ? 0 : (f - r) / (f - r + 1))
  const alphas: Record<string, (f: number) => number> = {
    mean: (f) => f / (f + 1),
    restart8: restart(8),
    restart16: restart(16),
    restart32: restart(32),
    'h0.97': (f) => (f === 0 ? 0 : 0.97),
    'h0.93': (f) => (f === 0 ? 0 : 0.93),
    'h0.85': (f) => (f === 0 ? 0 : 0.85),
    edit: (f) => (f < 12 ? 0 : 0.93),
    edit05: (f) => (f === 0 ? 0 : f < 16 ? 0.5 : 0.93),
  }
  for (const [mode, alpha] of Object.entries(alphas)) {
    reset()
    const curve: Record<string, number>[] = []
    const raw: number[][] = []
    for (let f = 0; f < FRAMES; f++) {
      await probeFrame(f, alpha(f))
      const r = await readPoints()
      curve.push(r.means)
      raw.push(r.raw)
    }
    curves[mode] = curve
    raws[mode] = raw
  }

  // Per-frame cost, converged: the rays and the update, apart (wall clock to the GPU done).
  const rayMs: number[] = [], updMs: number[] = [], pixMs: number[] = []
  for (let f = 0; f < 20; f++) {
    turn(FRAMES + f)
    hysteresis.value = 0.97
    let t = performance.now()
    renderer.compute(current!.rayKernel)
    await device.queue.onSubmittedWorkDone()
    rayMs.push(performance.now() - t)
    t = performance.now()
    renderer.compute(updateKernel)
    renderer.compute(depthKernel)
    await device.queue.onSubmittedWorkDone()
    updMs.push(performance.now() - t)
    t = performance.now()
    renderer.compute(pixelKernel)
    await device.queue.onSubmittedWorkDone()
    pixMs.push(performance.now() - t)
  }
  result.cost = { raysPerFrame: TOTAL * rays, rayMs: round(median(rayMs), 2), updateMs: round(median(updMs), 2), sampleFullResMs: round(median(pixMs), 2) }

  // The converged probes: the target, and what the pictures and regions below use.
  reset()
  const target = restart(32)
  for (let f = 0; f < 192; f++) await probeFrame(f, target(f))
  const converged = (await readPoints()).means
  const within = (curve: Record<string, number>[], tol: number) =>
    Object.fromEntries(Object.keys(converged).map((name) => {
      const target = converged[name]
      let last = -1
      curve.forEach((c, f) => {
        if (Math.abs(c[name] - target) > tol * Math.max(target, 1e-4)) last = f
      })
      return [name, last + 1]
    }))
  // Per point: the mean frame-to-frame change over frames [from, to), over the region's converged mean.
  const flicker = (mode: string, from: number, to: number) =>
    Object.fromEntries(Object.keys(converged).map((name) => {
      let sum = 0, n = 0
      for (let f = Math.max(from, 1); f < to; f++) pointRegion.forEach((r, k) => {
        if (r !== name) return
        sum += Math.abs(raws[mode][f][k] - raws[mode][f - 1][k])
        n++
      })
      return [name, round(sum / Math.max(n, 1) / Math.max(converged[name], 1e-4), 4)]
    }))
  result.convergence = {
    frames: FRAMES,
    final: Object.fromEntries(Object.entries(converged).map(([k, v]) => [k, round(v, 4)])),
    within2: Object.fromEntries(['mean', 'restart8', 'restart16', 'restart32'].map((m) => [m, within(curves[m], 0.02)])),
    // Where each mode ends, over the target.
    lastOverTarget: Object.fromEntries(Object.keys(alphas).map((m) => [m, Object.fromEntries(Object.keys(converged).map((name) => [name, round(curves[m][FRAMES - 1][name] / Math.max(converged[name], 1e-4), 3)]))])),
    within5: Object.fromEntries(Object.keys(alphas).map((m) => [m, within(curves[m], 0.05)])),
    within10: Object.fromEntries(Object.keys(alphas).map((m) => [m, within(curves[m], 0.1)])),
    flicker: Object.fromEntries(Object.keys(alphas).filter((m) => m.startsWith('h') || m.startsWith('edit')).map((m) => [m, flicker(m, FRAMES - 16, FRAMES)])),
    // While the edit takes frames whole: what the viewport shows as it follows.
    flickerWhileFollowing: { edit: flicker('edit', 2, 12), edit05: flicker('edit05', 2, 16) },
    curves: Object.fromEntries(Object.entries(curves).map(([m, c]) => [m, c.map((x) => Object.fromEntries(Object.entries(x).map(([k, v]) => [k, round(v, 4)])))])),
  }


  // The per-pixel irradiances and the reference, on the converged probes.
  const activeArr = new Float32Array(await renderer.getArrayBufferAsync(state.value as never))
  result.inactiveProbes = [0, 1].map((lv) => {
    let n = 0
    for (let p = lv * PER; p < (lv + 1) * PER; p++) if (activeArr[4 * p + 3] === 0) n++
    return n
  })
  renderer.compute(pixelKernel)
  const refPasses = Number(query.get('refPasses') ?? 32)
  const refStart = performance.now()
  for (let f = 0; f < refPasses; f++) {
    seed.value = f
    renderer.compute(refKernel!)
    await device.queue.onSubmittedWorkDone()
  }
  result.reference = { bounces: REF_BOUNCES, pathsPerPixel: refPasses * RREF, ms: Math.round(performance.now() - refStart) }
  const EP = new Float32Array(await renderer.getArrayBufferAsync(eProbe.value as never))
  const EE = new Float32Array(await renderer.getArrayBufferAsync(eEnv.value as never))
  const ER = new Float32Array(await renderer.getArrayBufferAsync(eRef.value as never))
  for (let p = 0; p < W * H; p++) if (ER[4 * p + 3] > 0) for (let c = 0; c < 3; c++) ER[4 * p + c] = (Math.PI * ER[4 * p + c]) / ER[4 * p + 3]
  {
    // Health: NaN in the probes and the pixels, and the first NaN pixel's surface.
    const irr = new Float32Array(await renderer.getArrayBufferAsync(irradiance.value as never))
    const dep = new Float32Array(await renderer.getArrayBufferAsync(depth.value as never))
    const nanTexels = [0, 1].map((lv) => {
      let a = 0, b = 0
      for (let t = lv * PER * TEXELS; t < (lv + 1) * PER * TEXELS; t++) if (!Number.isFinite(irr[4 * t])) a++
      for (let t = lv * PER * DTEXELS; t < (lv + 1) * PER * DTEXELS; t++) if (!Number.isFinite(dep[2 * t])) b++
      return { irradiance: a, depth: b }
    })
    let nanProbe = 0, nanRef = 0, first: number[] | null = null
    for (let p = 0; p < W * H; p++) if (surfaces[8 * p + 3] > 0) {
      if (!Number.isFinite(EP[4 * p])) {
        nanProbe++
        first ??= [p % W, Math.floor(p / W), ...Array.from(surfaces.subarray(8 * p, 8 * p + 7)).map((v) => round(v, 3))]
      }
      if (!Number.isFinite(ER[4 * p])) nanRef++
    }
    result.health = { nanTexels, nanProbePixels: nanProbe, nanRefPixels: nanRef, firstNaN: first }
  }

  // --- Irradiance per region, and the composites: E_probe times a share of the AO (1: the Epic's as written), plus g · gi.
  const composites = [
    { name: 'probesAo', aoShare: 1, gains: [0, 1, 2.5, 5] },
    { name: 'halfAo', aoShare: 0.5, gains: [0, 1, 2.5] },
    { name: 'probes', aoShare: 0, gains: [0, 1] },
  ]
  const regionStats: Record<string, unknown> = {}
  for (const [name, [x0, y0, x1, y1]] of Object.entries(regions ?? {})) {
    const acc = { env: 0, envAo: 0, probe: 0, probeAo: 0, ref: 0, ao: 0, gi: 0, n: 0, err: composites.map((c) => c.gains.map(() => 0)), errBase: 0 }
    for (let y = Math.floor(y0 * H); y < Math.floor(y1 * H); y++) {
      for (let x = Math.floor(x0 * W); x < Math.floor(x1 * W); x++) {
        const p = y * W + x
        if (!(surfaces[8 * p + 3] > 0) || !(ER[4 * p + 3] > 0)) continue
        const le = lum(EE[4 * p], EE[4 * p + 1], EE[4 * p + 2])
        const lp = lum(EP[4 * p], EP[4 * p + 1], EP[4 * p + 2])
        const lr = lum(ER[4 * p], ER[4 * p + 1], ER[4 * p + 2])
        const lg = lum(giAt(p, 0), giAt(p, 1), giAt(p, 2)) * Math.PI
        acc.env += le
        acc.envAo += le * ao(p)
        acc.probe += lp
        acc.probeAo += lp * ao(p)
        acc.ref += lr
        acc.ao += ao(p)
        acc.gi += lg
        composites.forEach((c, i) => c.gains.forEach((g, k) => (acc.err[i][k] += lp * shareOf(c.aoShare, p) + g * lg - lr)))
        acc.errBase += le * ao(p) + 5 * lg - lr
        acc.n++
      }
    }
    const n = Math.max(acc.n, 1)
    regionStats[name] = {
      pixels: acc.n,
      E_env: round(acc.env / n, 4),
      E_env_ao: round(acc.envAo / n, 4),
      E_probe: round(acc.probe / n, 4),
      E_probe_ao: round(acc.probeAo / n, 4),
      E_ref: round(acc.ref / n, 4),
      ao: round(acc.ao / n, 3),
      // The SSGI's gi at gain 1, as irradiance (π · its radiance).
      E_gi: round(acc.gi / n, 4),
      // Signed: what each composite's indirect irradiance is over the reference's, relative.
      today: round(acc.errBase / Math.max(acc.ref, 1e-6), 3),
      ...Object.fromEntries(composites.map((c, i) => [c.name, Object.fromEntries(c.gains.map((g, k) => [`g${g}`, round(acc.err[i][k] / Math.max(acc.ref, 1e-6), 3)]))])),
    }
  }
  result.regions = regionStats

  // Pictures: today, the Epic's composite (E_probe · ao + 5 · gi), the probes alone and the reference, at one exposure.
  const picture = (kind: 'today' | 'probes' | 'ref', g = 0, aoShare = 0) => composite({ EP, EE, ER }, kind, g, aoShare)
  const pics = { today: picture('today'), 'probes-ao': picture('probes', 5, 1), probes: picture('probes'), reference: picture('ref') }
  const exposure = exposureOf(pics.reference)
  result.exposure = round(exposure, 4)
  const toImage = (f: Float32Array) => encode(f, exposure)
  const images = Object.fromEntries(Object.entries(pics).map(([k, v]) => [k, toImage(v)]))
  const vsRef = (k: string) => {
    let sum = 0, n = 0
    for (let p = 0; p < W * H; p++) if (surfaces[8 * p + 3] > 0) {
      for (let c = 0; c < 3; c++) sum += Math.abs(images[k][4 * p + c] - images.reference[4 * p + c]) / 3
      n++
    }
    return round(sum / Math.max(n, 1), 2)
  }
  result.vsReference = { today: vsRef('today'), 'probes-ao': vsRef('probes-ao'), probes: vsRef('probes') }
  // The other composites, against the reference picture (not kept).
  result.vsReferenceMore = Object.fromEntries(composites.flatMap((c) => c.gains.map((g) => {
    images.extra = toImage(picture('probes', g, c.aoShare))
    return [`${c.name}-g${g}`, vsRef('extra')]
  })))
  delete images.extra
  result.pictures = Object.fromEntries(Object.entries(images).map(([k, v]) => [k, jpeg(v)]))

  // Cascade transition: where the levels are cross-faded (0 < k0 < 1), how far apart they are (what a hard switch
  // would step by), over the blend; and the same where level 0 alone is used, for scale.
  renderer.compute(levelKernel)
  const LV = new Float32Array(await renderer.getArrayBufferAsync(levels.value as never))
  const apart = (pick: (k0: number) => boolean) => {
    let diff = 0, sum = 0, n = 0
    for (let p = 0; p < W * H; p++) {
      const k0 = LV[4 * p + 2]
      if (!(surfaces[8 * p + 3] > 0) || !pick(k0) || !(LV[4 * p + 3] > 1e-5)) continue
      diff += Math.abs(LV[4 * p] - LV[4 * p + 1])
      sum += LV[4 * p] * k0 + LV[4 * p + 1] * (1 - k0)
      n++
    }
    return { pixels: n, levelsApart: round(diff / Math.max(sum, 1e-6), 4) }
  }
  result.transition = { blendZone: apart((k) => k > 0 && k < 1), level0Only: apart((k) => k >= 1), level1Only: { pixels: Array.from({ length: W * H }, (_, p) => p).filter((p) => surfaces[8 * p + 3] > 0 && !(LV[4 * p + 2] > 0)).length } }
  rt.dispose()
  current?.layer.dispose()
  return result
}

function jpeg(rgba: Uint8ClampedArray) {
  return jpegOf(rgba, W, H)
}

function jpegOf(rgba: Uint8ClampedArray, w: number, h: number) {
  const c = document.createElement('canvas')
  c.width = w
  c.height = h
  c.getContext('2d')!.putImageData(new ImageData(rgba, w, h), 0, 0)
  return c.toDataURL('image/jpeg', 0.9)
}

function pass(built: Built, _name: string) {
  const prepass = tsl.pass(built.scene, built.camera, { samples: 0 })
  prepass.transparent = false
  prepass.setMRT(tsl.mrt({ output: vec4(tsl.packNormalToRGB(tsl.normalView), tsl.roughness), velocity: tsl.velocity }))
  prepass.getTexture('output').type = HalfFloatType
  prepass.getTexture('velocity').type = HalfFloatType
  return prepass
}
