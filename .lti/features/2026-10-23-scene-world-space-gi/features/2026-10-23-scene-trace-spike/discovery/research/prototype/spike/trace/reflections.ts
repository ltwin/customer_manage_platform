// S0 group 4, reflections (scene-trace-spike design「判定标准」组 4): the product's real-time GI's reflection term —
// its prepass, its split lighting pass (giSplit.ts) and the stochastic SSR, composited as realtimeGI.ts composites
// it — with the SSR forked (SSRTraceNode.js) to retrace its misses and its edge band on the world-space layer:
//   ssr   as today (the fork with no retrace)
//   A     retraced in the SSR's fragment shader
//   B     retraced in a compute pass between the SSR and what reads it
// The temporal reprojection and denoising downstream are left out: each picture is the plain average of 64 frames of
// the stochastic SSR (its noise advancing every frame), so what differs is the retrace alone. Over a scene:
//   pictures  the three, still, and |A − B| (the same retrace in two places: noise-level apart)
//   pitch     (mirror-board) the camera pitched down in steps while the ball leaves the top of the screen and its
//             reflection on the floor stays: that floor point's brightness at each step, ssr and A
//   timing    GPU time a frame (render + compute timestamps), median of 30, each way
//   rays      how many pixels ask for a retrace, by roughness: the cost of each r_max
//   limits    storage buffers and textures per stage, core and compatibility (whether A can run at all)
// ?rmax= the retrace's roughness limit (1); ?layer=library packs with three-mesh-bvh's own update(); ?probe=x,y
// reads one pixel's targets in A and B; ?retrace= hitShade.ts's experiment switches; ?proxies=1 (or the bounds):
// group 5, A's retrace over the original and over each proxy (proxy.ts), the pictures apart.
import { BasicShadowMap, Box3, DirectionalLight, FloatType, HalfFloatType, HemisphereLight, Mesh, RenderTarget, UnsignedByteType, Vector3, type Object3D, type Texture } from 'three'
import { RoomEnvironment } from 'three/addons/environments/RoomEnvironment.js'
import { mrt, normalView, output as outputNode, packNormalToRGB, pass, roughness, sample, uniform, unpackRGBToNormal, vec4, velocity } from 'three/tsl'
import { MeshBasicNodeMaterial, PassNode, PMREMGenerator, QuadMesh, type WebGPURenderer } from 'three/webgpu'
import type { SlotMaterial } from '../../src/creative-scene/materials/resolve.ts'
import { giAmbient, giBase, giEnvSpec, GILightingPass, installGISplit } from '../../src/creative-scene/render/giSplit.ts'
import './computePatches.ts'
import { reflectionTrace } from './hitShade.ts'
import { MeshBVH, SAH } from 'three-mesh-bvh'
import { TracedScene } from './integration.ts'
import { TraceLayer } from './layer.ts'
import { compare, EPSILONS, proxiesAt, strip } from './proxy.ts'
import type { Built } from './scene.ts'
import type { Lights } from './shading.ts'
import { ssrTrace } from './SSRTraceNode.js'
import { traceShim } from './trace.ts'

/* eslint-disable @typescript-eslint/no-explicit-any -- three's display nodes are loosely typed */
type N = any
type Mode = 'ssr' | 'A' | 'B'
type Device = { queue: { onSubmittedWorkDone(): Promise<void> }; limits: Record<string, number> }

const W = 1536
const H = 1024
/** realtimeGI.ts MISSED: where the SSR's ray length says the environment. */
const MISSED = 1e3
const median = (v: number[]) => [...v].sort((a, b) => a - b)[Math.floor(v.length / 2)]
const round = (v: number, d = 2) => Math.round(v * 10 ** d) / 10 ** d
const halfAt = (h: number) => {
  const e = (h >> 10) & 0x1f, f = h & 0x3ff, sign = h & 0x8000 ? -1 : 1
  return e === 0 ? sign * 2 ** -14 * (f / 1024) : e === 31 ? (f ? NaN : sign * Infinity) : sign * 2 ** (e - 15) * (1 + f / 1024)
}
const srgb = (v: number) => 255 * Math.min(1, Math.max(0, v <= 0.0031308 ? 12.92 * v : 1.055 * Math.pow(v, 1 / 2.4) - 0.055))

/** A picture as a data URL: JPEG for photographs (the evidence's size, proposal), PNG for difference images. */
function image(w: number, h: number, rgba: Uint8ClampedArray, type: 'image/png' | 'image/jpeg' = 'image/jpeg') {
  const c = document.createElement('canvas')
  c.width = w
  c.height = h
  c.getContext('2d')!.putImageData(new ImageData(rgba, w, h), 0, 0)
  return c.toDataURL(type, 0.9)
}

/** The reflection term's pipeline, one way: { quad that draws the picture, the SSR node, dispose }. */
function pipeline(built: Built, mode: Mode, trace: ReturnType<typeof reflectionTrace> | null) {
  const { scene, camera } = built
  const prepass = pass(scene, camera, { samples: 0 })
  prepass.transparent = false
  prepass.setMRT(mrt({ output: vec4(packNormalToRGB(normalView), roughness), velocity }))
  prepass.getTexture('output').type = UnsignedByteType
  prepass.getTexture('velocity').type = HalfFloatType
  const normalPacked: N = prepass.getTextureNode()
  const depth: N = prepass.getTextureNode('depth')
  const normal: N = sample((uv: N) => unpackRGBToNormal(normalPacked.sample(uv).rgb))
  const lit = new GILightingPass(PassNode.COLOR, scene, camera, { samples: 0 })
  lit.setMRT(mrt({ output: outputNode, base: giBase, ambient: giAmbient, envSpec: giEnvSpec }))
  lit.getTexture('base').type = UnsignedByteType
  const output: N = lit.getTextureNode('output')
  const base: N = lit.getTextureNode('base')
  const ambient: N = lit.getTextureNode('ambient')
  const envSpec: N = lit.getTextureNode('envSpec')
  const reflect: N = ssrTrace(output, depth, normal, {
    stochastic: true,
    diffuseNode: base,
    metalnessNode: base.a,
    roughnessNode: normalPacked.a,
    camera,
    trace: mode === 'ssr' ? null : trace,
    traceMode: mode === 'ssr' ? 'A' : mode,
    normalTexture: prepass.getTexture('output'),
  })
  const refl: N = reflect.getTextureNode()
  // As realtimeGI.ts's composite, the reflection's part of it: the environment's specular where the ray hit nothing,
  // the reflection where it did, where a material was taken apart over an opaque surface.
  const cover: N = refl.a.lessThan(MISSED).select(1, 0)
  const split: N = ambient.a.mul(depth.r.lessThan(1).select(1, 0))
  const picture: N = output.rgb.sub(envSpec.rgb.mul(cover).mul(split)).add(refl.rgb.mul(split))
  const material = new MeshBasicNodeMaterial()
  material.colorNode = vec4(picture, 1)
  const quad = new QuadMesh(material)
  return {
    quad,
    reflect,
    prepass,
    dispose() {
      prepass.dispose()
      lit.dispose()
      reflect.dispose()
      material.dispose()
    },
  }
}

export async function runReflections(renderer: WebGPURenderer, built: Built, scene: string, query: URLSearchParams) {
  const device = (renderer.backend as unknown as { device: Device }).device
  const result: Record<string, unknown> = { scene }
  renderer.setSize(W, H, false)
  built.camera.aspect = W / H
  built.camera.updateProjectionMatrix()
  installGISplit()

  // The lights as group 3 sets them: the sun's map (Basic) for the raster, the environment a PMREM.
  const seeThrough = (m: Mesh) => {
    const s = m.userData.slot as SlotMaterial | undefined
    return s?.kind === 'principled' && (((s.params.transmission as number) ?? 0) > 0 || ((s.params.opacity as number) ?? 1) < 1)
  }
  built.meshes.filter(seeThrough).forEach((m) => (m.visible = false))
  const meshes = built.meshes.filter((m) => m.visible)
  const sun = built.scene.children.find((o): o is DirectionalLight => o instanceof DirectionalLight)!
  const hemi = built.scene.children.find((o): o is HemisphereLight => o instanceof HemisphereLight)!
  renderer.shadowMap.enabled = true
  renderer.shadowMap.type = BasicShadowMap
  meshes.forEach((m) => {
    m.castShadow = true
    m.receiveShadow = true
  })
  sun.castShadow = true
  Object.assign(sun.shadow.camera, { left: -8, right: 8, top: 8, bottom: -8, near: 1, far: 150 })
  sun.shadow.camera.updateProjectionMatrix()
  sun.shadow.mapSize.set(2048, 2048)
  sun.shadow.bias = -0.0005
  sun.shadow.normalBias = 0.02
  const envTarget = new PMREMGenerator(renderer).fromScene(new RoomEnvironment(), 0.04)
  built.scene.environment = envTarget.texture

  // The layer and what the retrace shades with: shadow rays for the sun (hit-shading.md).
  // The layer as S1 packs it (TraceLayer: one top-level leaf an instance), or the library's own clusters (?layer=library).
  let data: TracedScene | TraceLayer
  if (query.get('layer') === 'library') {
    data = new TracedScene(meshes, { attributes: { position: 'vec4f', normal: 'vec4f' } })
    meshes.forEach((m, i) => (data as TracedScene).materialOf.set(m, i))
    data.update()
  } else {
    // The reference scenes are small: their trees built here, SAH as the workers build them.
    const trees = new Map(meshes.map((m) => [m.geometry, (m.geometry.boundsTree as MeshBVH | undefined) ?? new MeshBVH(m.geometry, { strategy: SAH, targetLeafSize: 10, maxDepth: 40 })]))
    data = new TraceLayer(meshes, (o) => trees.get((o as Mesh).geometry)!, device.limits.maxStorageBufferBindingSize)
    meshes.forEach((m, i) => (data as TraceLayer).materialOf.set(m, i))
    data.update()
  }
  result.layer = query.get('layer') === 'library' ? 'library' : 'TraceLayer'
  const rMax = uniform(Number(query.get('rmax') ?? 1))
  const L: Lights = { sun, hemi, env: envTarget.texture as Texture, depth: null }
  const trace = reflectionTrace(traceShim(data), data.storage as never, meshes, L, rMax)

  const target = new RenderTarget(W, H, { type: FloatType })
  // A new frame for the node system: three advances it on its animation loop only, and the passes (and the SSR's
  // noise) update once a frame; drawn in a loop, frames would otherwise repeat the last one.
  const nodeFrame = (renderer as unknown as { _nodes: { nodeFrame: { update(): void } } })._nodes.nodeFrame
  const draw = async (p: ReturnType<typeof pipeline>) => {
    nodeFrame.update()
    renderer.setRenderTarget(target)
    p.quad.render(renderer)
    renderer.setRenderTarget(null)
    return new Float32Array((await renderer.readRenderTargetPixelsAsync(target, 0, 0, W, H)) as Float32Array)
  }
  const average = async (p: ReturnType<typeof pipeline>, frames: number) => {
    const sum = new Float32Array(W * H * 4)
    for (let f = 0; f < frames; f++) {
      const one = await draw(p)
      for (let k = 0; k < sum.length; k++) sum[k] += one[k] / frames
    }
    return sum
  }
  // --- Group 5 (?proxies=1, or the bounds): A's retrace over the original and over each proxy (the workers' trees,
  // proxy.ts), 64 frames each, and the pictures apart: the original's against each proxy's.
  const proxyMode = query.get('proxies')
  if (proxyMode) {
    // The default layer, unused here.
    if (data instanceof TraceLayer) (data as TraceLayer).dispose()
    const epsilons = proxyMode === '1' ? EPSILONS : proxyMode.split(',').map(Number)
    const pictures: Float32Array[] = []
    const reports: unknown[] = []
    const over = async (bvhOf: (o: Object3D) => MeshBVH) => {
      const layer = new TraceLayer(meshes, bvhOf, device.limits.maxStorageBufferBindingSize)
      meshes.forEach((m, i) => layer.materialOf.set(m, i))
      layer.update()
      const p = pipeline(built, 'A', reflectionTrace(traceShim(layer), layer.storage as never, meshes, L, rMax))
      await draw(p)
      pictures.push(await average(p, 64))
      p.dispose()
      layer.dispose()
    }
    for (const [i, epsilon] of epsilons.entries()) {
      const prepared = await proxiesAt(scene, built, epsilon, 4000)
      const tree = (side: 'original' | 'proxy') => (o: Object3D) => prepared.pairs.get((o as Mesh).geometry)![side]
      if (i === 0) await over(tree('original'))
      await over(tree('proxy'))
      reports.push(prepared.report)
    }
    const lum = (f: Float32Array, p: number) => 0.2126 * f[4 * p] + 0.7152 * f[4 * p + 1] + 0.0722 * f[4 * p + 2]
    const lums: number[] = []
    for (let p = 0; p < W * H; p += 7) lums.push(lum(pictures[0], p))
    lums.sort((a, b) => a - b)
    const exposure = 0.8 / Math.max(lums[Math.floor(lums.length * 0.99)], 1e-6)
    const images = pictures.map((f) => {
      const out = new Uint8ClampedArray(W * H * 4)
      for (let p = 0; p < W * H; p++) out.set([srgb(f[4 * p] * exposure), srgb(f[4 * p + 1] * exposure), srgb(f[4 * p + 2] * exposure), 255], 4 * p)
      return out
    })
    const regions = ((await (await fetch('/scripts/fixtures/scene-reference/gi-regions.json')).json())[scene] ?? {}) as Record<string, number[]>
    const regionMeans = (f: Float32Array) => Object.fromEntries(Object.entries(regions).map(([name, [x0, y0, x1, y1]]) => {
      let sum = 0, n = 0
      for (let y = Math.floor(y0 * H); y < Math.floor(y1 * H); y++) for (let x = Math.floor(x0 * W); x < Math.floor(x1 * W); x++) {
        sum += lum(f, y * W + x)
        n++
      }
      return [name, sum / Math.max(n, 1)]
    }))
    const base = regionMeans(pictures[0])
    const diffs: Uint8ClampedArray[] = []
    result.proxies = [
      { name: 'original' },
      ...reports.map((report, k) => {
        const { diff, ...picture } = compare(images[0], images[k + 1], W, H)
        diffs.push(diff)
        const mine = regionMeans(pictures[k + 1])
        return { name: `eps${epsilons[k]}`, ...(report as object), picture, regions: Object.fromEntries(Object.entries(mine).map(([n, v]) => [n, round(v / Math.max(base[n], 1e-6) - 1, 4)])) }
      }),
    ]
    result.exposure = round(exposure, 4)
    const all = strip(images, W, H, 'mean')
    const apart = strip(diffs, W, H, 'max')
    result.pictures = { 'proxy-strip': image(all.width, all.height, all.data), 'proxy-differences8x': image(apart.width, apart.height, apart.data) }
    target.dispose()
    return result
  }

  const modes: Mode[] = ['ssr', 'A', 'B']
  const pipes = Object.fromEntries(modes.map((m) => [m, pipeline(built, m, trace)])) as Record<Mode, ReturnType<typeof pipeline>>

  // Compile and first frame of each, then the pictures.
  const firstMs: Record<string, number> = {}
  for (const m of modes) {
    const t = performance.now()
    await draw(pipes[m])
    firstMs[m] = Math.round(performance.now() - t)
  }
  result.firstFrameMs = firstMs
  await renderer.resolveTimestampsAsync('render')
  await renderer.resolveTimestampsAsync('compute')
  const pictures: Record<string, Float32Array> = {}
  for (const m of modes) pictures[m] = await average(pipes[m], 64)
  const lums: number[] = []
  for (let p = 0; p < W * H; p += 7) lums.push(0.2126 * pictures.ssr[4 * p] + 0.7152 * pictures.ssr[4 * p + 1] + 0.0722 * pictures.ssr[4 * p + 2])
  lums.sort((a, b) => a - b)
  const exposure = 0.8 / Math.max(lums[Math.floor(lums.length * 0.99)], 1e-6)
  result.exposure = round(exposure, 4)
  const enc = (f: Float32Array) => {
    const out = new Uint8ClampedArray(W * H * 4)
    for (let p = 0; p < W * H; p++) out.set([srgb(f[4 * p] * exposure), srgb(f[4 * p + 1] * exposure), srgb(f[4 * p + 2] * exposure), 255], 4 * p)
    return out
  }
  const images = Object.fromEntries(modes.map((m) => [m, enc(pictures[m])]))
  // A against B: the same retrace in two places.
  let sumAB = 0, overAB = 0, sumSA = 0
  const diffAB = new Uint8ClampedArray(W * H * 4)
  for (let p = 0; p < W * H; p++) {
    let worst = 0
    for (let c = 0; c < 3; c++) {
      const d = Math.abs(images.A[4 * p + c] - images.B[4 * p + c])
      worst = Math.max(worst, d)
      sumAB += d / 3
      sumSA += Math.abs(images.A[4 * p + c] - images.ssr[4 * p + c]) / 3
      diffAB[4 * p + c] = Math.min(255, 8 * d)
    }
    diffAB[4 * p + 3] = 255
    if (worst > 2) overAB++
  }
  result.compareAB = { mean: round(sumAB / (W * H), 3), over2: round(overAB / (W * H), 5), ssrVsA: round(sumSA / (W * H), 3) }
  result.pictures = { ssr: image(W, H, images.ssr), A: image(W, H, images.A), differenceAB8x: image(W, H, diffAB, 'image/png'), ...(query.get('pictureB') ? { B: image(W, H, images.B) } : {}) }

  // Timing: 30 frames each, wall clock from submitting to the GPU done (three's timestamps for passes nested in a
  // pass's updateBefore were attributed inconsistently, reflections.md). Interleaved, so clocks drift alike.
  const timing: Record<string, unknown> = {}
  const walls: Record<Mode, number[]> = { ssr: [], A: [], B: [] }
  for (let f = 0; f < 30; f++) {
    for (const m of modes) {
      const t = performance.now()
      nodeFrame.update()
      renderer.setRenderTarget(target)
      pipes[m].quad.render(renderer)
      renderer.setRenderTarget(null)
      await device.queue.onSubmittedWorkDone()
      walls[m].push(performance.now() - t)
    }
  }
  for (const m of modes) timing[m] = { medianMs: round(median(walls[m])), minMs: round(Math.min(...walls[m])), maxMs: round(Math.max(...walls[m])) }
  await renderer.resolveTimestampsAsync('render')
  await renderer.resolveTimestampsAsync('compute')
  result.timing = timing

  // The rays B was asked to retrace, by roughness (rMax 1: all of them), on this frame.
  {
    const rt = pipes.B.reflect.getRenderTarget() as RenderTarget
    result.readback = { ssr: [rt.width, rt.height], prepass: [(pipes.B.prepass.renderTarget as RenderTarget).width, (pipes.B.prepass.renderTarget as RenderTarget).height] }
    // A half-float target reads back as its raw 16-bit words.
    const half = (h: number) => {
      const e = (h >> 10) & 0x1f, f = h & 0x3ff, sign = h & 0x8000 ? -1 : 1
      return e === 0 ? sign * 2 ** -14 * (f / 1024) : e === 31 ? (f ? NaN : sign * Infinity) : sign * 2 ** (e - 15) * (1 + f / 1024)
    }
    const raw = (await renderer.readRenderTargetPixelsAsync(rt, 0, 0, rt.width, rt.height, 1)) as unknown as Uint16Array | Float32Array
    const ray = raw instanceof Uint16Array ? Float32Array.from(raw, half) : raw
    const pre = pipes.B.prepass.renderTarget as RenderTarget
    const packed = (await renderer.readRenderTargetPixelsAsync(pre, 0, 0, rt.width, rt.height, 0)) as Uint8Array | Uint16Array
    const out0 = (await renderer.readRenderTargetPixelsAsync(rt, 0, 0, rt.width, rt.height, 0)) as unknown as Uint16Array
    const bins = [0.1, 0.25, 0.5, 0.75, 1.01]
    const count = bins.map(() => ({ pixels: 0, rays: 0, share: 0 }))
    for (let p = 0; p < rt.width * rt.height; p++) {
      // Only what the SSR drew (the background, discarded, keeps the cleared zero in its output).
      if (half(out0[4 * p + 3]) <= 0) continue
      // The prepass's packed normal and roughness read back as half floats too.
      const r = packed instanceof Uint16Array ? half(packed[4 * p + 3]) : packed[4 * p + 3] / 255
      const b = bins.findIndex((x) => r < x)
      if (b < 0) continue
      count[b].pixels++
      if (ray[4 * p + 3] > 0) {
        count[b].rays++
        count[b].share += ray[4 * p + 3]
      }
    }
    const total = rt.width * rt.height
    let nan = 0, zero = 0
    for (let p = 0; p < total; p++) {
      if (!(ray[4 * p + 3] > 0) || half(out0[4 * p + 3]) <= 0) continue
      const len = Math.hypot(ray[4 * p], ray[4 * p + 1], ray[4 * p + 2])
      if (Number.isNaN(len)) nan++
      else if (len < 0.5) zero++
    }
    result.rayDirections = { nan, nearZero: zero }
    result.rays = { pixels: total, byRoughnessBelow: Object.fromEntries(bins.map((b, i) => [b === 1.01 ? '1' : String(b), { pixels: count[i].pixels, rays: count[i].rays, raysShareOfPicture: round(count[i].rays / total, 4), shareSum: round(count[i].share, 0) }])) }
  }

  // Pitch: the camera turned down about its own x axis while the ball leaves the top of the screen; the floor point
  // where the ball is reflected (its centre mirrored in the floor, seen from the camera) measured at each step.
  if (scene === 'mirror-board') {
    const ball = meshes.find((m) => (m.userData.slot as SlotMaterial | undefined)?.kind === 'pattern' && (m.userData.slot as { id: string }).id === 'metal@1')!
    const floor = meshes.reduce((a, b) => (new Box3().setFromObject(b).getSize(new Vector3()).x > new Box3().setFromObject(a).getSize(new Vector3()).x && new Box3().setFromObject(b).max.y < 0.5 ? b : a))
    const top = new Box3().setFromObject(floor).max.y
    const centre = new Box3().setFromObject(ball).getCenter(new Vector3())
    const mirrored = centre.clone().setY(2 * top - centre.y)
    const eye = built.camera.position.clone()
    const s = (eye.y - top) / (eye.y - mirrored.y)
    const point = eye.clone().lerp(mirrored, s)
    const start = built.camera.quaternion.clone()
    const steps = [0, -6, -12, -18, -24, -30, -36]
    const rows: Record<string, unknown>[] = []
    const strip: Record<string, Uint8ClampedArray[]> = { ssr: [], A: [] }
    const sw = W / 4, sh = H / 4
    for (const deg of steps) {
      built.camera.quaternion.copy(start)
      built.camera.rotateX((deg * Math.PI) / 180)
      built.camera.updateMatrixWorld(true)
      const ndc = point.clone().project(built.camera)
      const px = Math.round(((ndc.x + 1) / 2) * W)
      const py = Math.round(((1 - ndc.y) / 2) * H)
      const ballNdc = new Vector3(centre.x, new Box3().setFromObject(ball).min.y, centre.z).project(built.camera)
      const row: Record<string, unknown> = { pitchDeg: deg, point: [px, py], ballBottomOnScreen: ballNdc.y < 1 }
      for (const m of ['ssr', 'A'] as Mode[]) {
        const f = await average(pipes[m], 32)
        let sum = 0, n = 0
        for (let y = py - 5; y <= py + 5; y++) for (let x = px - 5; x <= px + 5; x++) {
          if (x < 0 || y < 0 || x >= W || y >= H) continue
          const p = y * W + x
          sum += 0.2126 * f[4 * p] + 0.7152 * f[4 * p + 1] + 0.0722 * f[4 * p + 2]
          n++
        }
        row[m] = n ? round((sum / n) * exposure, 4) : null
        // A quarter-size picture for the strip.
        const small = new Uint8ClampedArray(sw * sh * 4)
        for (let y = 0; y < sh; y++) for (let x = 0; x < sw; x++) {
          const p = 4 * y * W + 4 * x
          small.set([srgb(f[4 * p] * exposure), srgb(f[4 * p + 1] * exposure), srgb(f[4 * p + 2] * exposure), 255], 4 * (y * sw + x))
        }
        // The measured point, marked.
        for (let k = -3; k <= 3; k++) for (const [x, y] of [[px / 4 + k, py / 4], [px / 4, py / 4 + k]]) if (x >= 0 && y >= 0 && x < sw && y < sh) small.set([255, 0, 255, 255], 4 * (Math.round(y) * sw + Math.round(x)))
        strip[m].push(small)
      }
      rows.push(row)
    }
    built.camera.quaternion.copy(start)
    built.camera.updateMatrixWorld(true)
    result.pitch = rows
    const stripImage = new Uint8ClampedArray(sw * steps.length * sh * 2 * 4)
    ;(['ssr', 'A'] as Mode[]).forEach((m, r) => strip[m].forEach((img, c) => {
      for (let y = 0; y < sh; y++) stripImage.set(img.subarray(4 * y * sw, 4 * (y + 1) * sw), 4 * ((r * sh + y) * sw * steps.length + c * sw))
    }))
    ;(result.pictures as Record<string, string>).pitchStrip = image(sw * steps.length, sh * 2, stripImage)
  }

  if (query.get('probe')) {
    const [qx, qy] = query.get('probe')!.split(',').map(Number)
    const read = async (rt: RenderTarget, index: number) => {
      const raw = (await renderer.readRenderTargetPixelsAsync(rt, qx, qy, 1, 1, index)) as unknown as Uint16Array | Float32Array | Uint8Array
      return Array.from(raw instanceof Uint16Array ? Array.from(raw, halfAt) : raw).map((v) => round(v as number, 4))
    }
    const out: Record<string, unknown> = {}
    for (const m of ['A', 'B'] as Mode[]) {
      const rt = pipes[m].reflect.getRenderTarget() as RenderTarget
      out[m] = { ssrOutput: await read(rt, 0), ...(m === 'B' ? { ray: await read(rt, 1), weight: await read(rt, 2) } : {}), picture: [0, 1, 2].map((c) => round(pictures[m][4 * (qy * W + qx) + c], 4)) }
    }
    out.packed = await read(pipes.B.prepass.renderTarget as RenderTarget, 0)
    // B's merged copy, read in a compute pass into a buffer.
    {
      const tsl = await import('three/tsl')
      const buf = tsl.instancedArray(1, 'vec4')
      const tex = (pipes.B.reflect as unknown as { _retraceTexture: Texture })._retraceTexture
      const k = tsl.Fn(() => {
        buf.element(0).assign(tsl.texture(tex).load(tsl.ivec2(qx, qy)))
      })().compute(1)
      renderer.compute(k)
      out.Bmerged = Array.from(new Float32Array(await renderer.getArrayBufferAsync(buf.value as never))).map((v) => (Number.isNaN(v) ? 'NaN' : round(v, 4)))
    }
    result.probe = out
  }

  // What the fragment and compute stages may bind, as this device has it and as a compatibility device would.
  result.limits = await stageLimits(device)
  Object.values(pipes).forEach((p) => p.dispose())
  target.dispose()
  data.dispose()
  return result
}

async function stageLimits(device: Device) {
  const keys = ['maxStorageBuffersPerShaderStage', 'maxStorageBuffersInFragmentStage', 'maxStorageBuffersInVertexStage', 'maxStorageTexturesPerShaderStage', 'maxStorageTexturesInFragmentStage', 'maxColorAttachments', 'maxStorageBufferBindingSize']
  const pick = (l: Record<string, number>) => Object.fromEntries(keys.map((k) => [k, l[k] ?? null]))
  const gpu = (navigator as unknown as { gpu: { requestAdapter(o?: object): Promise<{ featureLevel?: string; limits: Record<string, number>; requestDevice(o?: object): Promise<{ limits: Record<string, number>; destroy(): void }> } | null> } }).gpu
  const compat = await gpu.requestAdapter({ featureLevel: 'compatibility' })
  let compatDevice = null
  if (compat) {
    const d = await compat.requestDevice()
    compatDevice = pick(d.limits)
    d.destroy()
  }
  return { device: pick(device.limits), compatibilityAdapter: compat ? { featureLevel: compat.featureLevel ?? null, limits: pick(compat.limits) } : null, compatibilityDeviceDefaults: compatDevice }
}

