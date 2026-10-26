import { Color, DoubleSide, FloatType, HalfFloatType, LinearFilter, Vector3, type AmbientLight, type Light, type Material, type Object3D, type PerspectiveCamera, type Scene } from 'three'
import * as tsl from 'three/tsl'
import { MeshBasicNodeMaterial, QuadMesh, RenderTarget, RendererUtils, StorageTexture, type MeshPhysicalNodeMaterial, type WebGPURenderer } from 'three/webgpu'
import { materialEntry } from '../../materials/catalog.ts'
import { slotOf, unknownGrey } from '../../materials/instances.ts'
import { patternFns, type TSLNode } from '../../materials/patterns.ts'
import type { SlotMaterial } from '../../materials/resolve.ts'
import { preExposure } from '../../world/exposure.ts'
import { gpuOf } from '../gpu.ts'
import type { TraceHandle } from './base.ts'
import { collectLights, collectMeshes, type Slot, type Traced } from './collect.ts'
import { hitShading, pcg, unit, type ShadeMode } from './hit.ts'
import type { TraceLayer } from './layer.ts'
import { AMBIENT_FILL, cellOf, PROBES, slotOf as probeSlot } from '../probes/cascade.ts'
import { cellNode, probeIrradiance, slotNode } from '../probes/lookup.ts'
import type { ProbeVolume } from '../probes/volume.ts'
import { decodeOct, encodeOct, octDecode, octEncode } from '../probes/octahedral.ts'
import { traceFirst } from './wgsl.ts'

// The e2e's checks of the layer against the raster, on the viewport's own
// device; loaded by its test hooks alone.
//  - depthCheck (S1 tasks 3.4, AC-S1-5; run again whenever three-mesh-bvh
//    is upgraded): a primary ray a pixel, its hit distance against the
//    distance the raster draws there.
//  - hitCheck (AC-S1-2): the same rays shaded at their hits (hit.ts), mesh
//    by mesh against the raster on the pixels inside each: the material's
//    outputs, the light without shadows (the raster without bump), and
//    shadow or not where the raster is wholly one or the other.
// Both draw the traced meshes alone, and the lights.
//  - atlasCheck (S2 design S2-U1): what the probe kernels need of three in
//    compute, an RGBA16F storage texture written, then loaded and sampled.
//  - probeGridCheck (S2 design「验证与交付」): the probes' TSL grid and
//    octahedral functions against their JS twins, negative cells included.
//  - probeCheck (AC-S2-2): the indirect irradiance a picture's probes give
//    each pixel against a per-pixel reference, region by region.

type Op = (...args: TSLNode[]) => TSLNode
const T = tsl as unknown as Record<string, Op>
const { Fn, If, float, fwidth, getViewPosition, instancedArray, ivec2, length, luminance, max, normalize, select, texture, textureLoad, textureStore, uniform, uvec2, varying, vec2, vec3, vec4 } = T
const { instanceIndex, modelScale, normalLocal, positionLocal, positionView, screenUV } = tsl as unknown as Record<string, TSLNode>

/** A layer no pass of the editor's draws: the traced meshes and the lights alone, for the raster's half. */
const CHECK_LAYER = 28

type Size = { width: number; height: number }

/** width a multiple of 16: a float row then reads back without padding. */
const sizeOf = (camera: PerspectiveCamera, width: number): Size => ({ width, height: Math.max(16, Math.round(width / camera.aspect / 16) * 16) })

const floatTarget = (s: Size, depthBuffer = true) => new RenderTarget(s.width, s.height, { type: FloatType, depthBuffer })

/**
 * Runs draw with the scene showing camera the traced meshes and the lights
 * alone, on no background and without fog; everything put back after.
 */
function isolated(renderer: WebGPURenderer, scene: Scene, camera: PerspectiveCamera, traced: readonly Traced[], draw: () => void) {
  const lights: Object3D[] = []
  scene.traverse((o) => (o as Light).isLight && lights.push(o))
  const shown = [...traced.map((t) => t.mesh), ...lights]
  const state = RendererUtils.resetRendererState(renderer, undefined as never)
  const mask = camera.layers.mask
  const { background, fog } = scene
  for (const o of shown) o.layers.enable(CHECK_LAYER)
  try {
    camera.layers.set(CHECK_LAYER)
    scene.background = null
    scene.fog = null
    renderer.setClearColor(0x000000, 0)
    draw()
  } finally {
    for (const o of shown) o.layers.disable(CHECK_LAYER)
    camera.layers.mask = mask
    scene.background = background
    scene.fog = fog
    RendererUtils.restoreRendererState(renderer, state)
  }
}

/** A pass of a ray a pixel from camera, with its neighbours' (the quad has a camera of its own: the scene camera's matrices as uniforms). */
function rayPass(camera: PerspectiveCamera, size: Size, body: (eye: TSLNode, dir: TSLNode, spread: TSLNode, neighbours: { dx: TSLNode; dy: TSLNode }) => TSLNode) {
  const inverse = uniform(camera.projectionMatrixInverse.clone())
  const world = uniform(camera.matrixWorld.clone())
  const eye = uniform(new Vector3().setFromMatrixPosition(camera.matrixWorld))
  // The angle a pixel spans: a primary ray's cone starts at the eye.
  const spread = uniform((2 * Math.tan((camera.fov * Math.PI) / 360)) / size.height)
  const material = new MeshBasicNodeMaterial({ fog: false })
  material.outputNode = Fn(() => {
    const through = (uv: TSLNode) => normalize(world.mul(vec4(normalize(getViewPosition(uv, float(0.5), inverse)), 0)).xyz)
    const dx = through(screenUV.add(vec2(1 / size.width, 0))).toVar()
    const dy = through(screenUV.add(vec2(0, 1 / size.height))).toVar()
    return body(eye, through(screenUV).toVar(), spread, { dx, dy })
  })()
  material.toneMapped = false
  return { quad: new QuadMesh(material), dispose: () => material.dispose() }
}

const readBack = async (renderer: WebGPURenderer, rt: RenderTarget, s: Size) => (await renderer.readRenderTargetPixelsAsync(rt, 0, 0, s.width, s.height)) as Float32Array

/** The distance the raster draws at: positionView's length; both sides unless said. */
function distanceMaterial(side: Material['side'] = DoubleSide) {
  const m = new MeshBasicNodeMaterial({ side, fog: false })
  m.outputNode = vec4(length(positionView), 0, 0, 1)
  m.toneMapped = false
  return m
}

/** Hit or miss alike, and where both hit, no further apart than a millimetre (a tenth of one a metre further than 10 m). */
const sameDistance = (r: number | null, t: number | null) => (r === null || t === null ? r === t : Math.abs(r - t) <= Math.max(1e-3, 1e-4 * r))

export type DepthCheck = {
  width: number
  height: number
  /** The meshes traced. */
  meshes: number
  /** Pixels the raster drew a traced mesh in. */
  hits: number
  /** Pixels a ray hit something in. */
  traced: number
  /** The raster's distances added up: what changes when anything traced moves. */
  sum: number
  /** The largest difference where both hit. */
  worst: number
  /** Pixels where sameDistance does not hold. */
  mismatched: number
  /** The first few, for the report. */
  samples: { x: number; y: number; raster: number | null; trace: number | null }[]
}

/** bias is added to every traced distance: the e2e's control that a difference is seen. */
export async function depthCheck(renderer: WebGPURenderer, scene: Scene, camera: PerspectiveCamera, layer: TraceLayer, width = 320, bias = 0): Promise<DepthCheck> {
  const size = sizeOf(camera, width)
  const raster = floatTarget(size)
  const traced = floatTarget(size, false)
  const distance = distanceMaterial()
  const ray = rayPass(camera, size, (eye, dir) => {
    const hit = traceFirst(layer, eye, dir).toVar()
    return vec4(hit.get('dist'), 0, 0, select(hit.get('didHit'), float(1), float(0)))
  })
  const meshes = collectMeshes(scene, slotOf)
  isolated(renderer, scene, camera, meshes, () => {
    const { overrideMaterial } = scene
    scene.overrideMaterial = distance
    try {
      renderer.setRenderTarget(raster)
      renderer.render(scene, camera)
    } finally {
      scene.overrideMaterial = overrideMaterial
    }
    renderer.setRenderTarget(traced)
    ray.quad.render(renderer)
  })
  try {
    const a = await readBack(renderer, raster, size)
    const b = await readBack(renderer, traced, size)
    const out: DepthCheck = { ...size, meshes: meshes.length, hits: 0, traced: 0, sum: 0, worst: 0, mismatched: 0, samples: [] }
    for (let i = 0; i < size.width * size.height; i++) {
      const r = a[i * 4 + 3] > 0.5 ? a[i * 4] : null
      const t = b[i * 4 + 3] > 0.5 ? b[i * 4] + bias : null
      if (r !== null) {
        out.hits++
        out.sum += r
      }
      if (t !== null) out.traced++
      if (r !== null && t !== null) out.worst = Math.max(out.worst, Math.abs(r - t))
      if (sameDistance(r, t)) continue
      out.mismatched++
      if (out.samples.length < 8) out.samples.push({ x: i % size.width, y: Math.floor(i / size.width), raster: r, trace: t })
    }
    return out
  } finally {
    for (const d of [raster, traced, distance, ray]) d.dispose()
  }
}

// --- hitCheck --------------------------------------------------------------------------------------------------

// The raster's material outputs, from the slot material as instances.ts builds it, unlit.
const patPos: TSLNode = varying(positionLocal.mul(modelScale), 'vCheckPatPos')
const patNormal: TSLNode = varying(normalLocal.div(modelScale), 'vCheckPatNormal')
const footprint: TSLNode = max(length(fwidth(patPos)), float(1e-6))

/** A slot material's outputs as the raster gives them, unlit: (albedo), or (roughness, metalness, emission's luminance). */
function outputsMaterial(m: SlotMaterial, what: 'albedo' | 'outputs', side: Material['side']): MeshBasicNodeMaterial {
  const material = new MeshBasicNodeMaterial({ side, fog: false })
  material.toneMapped = false
  let o: { albedo: TSLNode; rough: TSLNode; metal: TSLNode; emission: TSLNode }
  if (m.kind === 'pattern' && patternFns[m.id]) {
    const pm = Object.fromEntries(Object.entries(materialEntry(m.id)?.params ?? {}).map(([k, spec]) => [k, spec.type === 'color' ? uniform(new Color(m.params[k] as string)) : uniform(m.params[k] as number)]))
    o = patternFns[m.id]({ pos: patPos, width: fwidth, normal: patNormal, footprint, pm })
  } else {
    const p: Record<string, unknown> = m.kind === 'principled' ? m.params : { color: unknownGrey, roughness: 0.9, metalness: 0, emissive: '#000000', emissive_intensity: 0 }
    const c = new Color(p.color as string)
    const e = new Color(p.emissive as string).multiplyScalar(p.emissive_intensity as number)
    o = { albedo: vec3(c.r, c.g, c.b), rough: float(p.roughness as number), metal: float(p.metalness as number), emission: vec3(e.r, e.g, e.b) }
    material.vertexColors = what === 'albedo' && m.kind === 'principled' && !!m.vertexColors
  }
  material.colorNode = what === 'albedo' ? vec4(o.albedo, 1) : vec4(o.rough, o.metal, luminance(o.emission.mul(preExposure)), 1)
  return material
}

/** Each traced mesh drawn with what make gives for its material (or materials); returns the putting back. */
function swapMaterials(traced: readonly Traced[], make: (m: Material) => Material): () => void {
  const made = new Map<Material, Material>()
  const of = (m: Material) => {
    let x = made.get(m)
    if (!x) made.set(m, (x = make(m)))
    return x
  }
  const before = traced.map((t) => t.mesh.material)
  for (const t of traced) t.mesh.material = Array.isArray(t.mesh.material) ? t.mesh.material.map(of) : of(t.mesh.material)
  return () => {
    traced.forEach((t, i) => (t.mesh.material = before[i]))
    for (const m of made.values()) m.dispose()
  }
}

/** A material as the raster draws it, without its bump: what a hit is lit as (Epic「命中点着色」). */
function withoutBump(m: Material): Material {
  const c = (m as MeshPhysicalNodeMaterial).clone()
  c.normalNode = null
  return c
}

export type HitReport = {
  width: number
  height: number
  /** Per mesh slot (a material row), the mean difference inside it in 8-bit levels, the raster against the hits. */
  meshes: { row: number; material: string; pixels: number; albedo: number; outputs: number; lit: number }[]
  /** Pixels the raster has wholly lit or wholly in shadow by the lights, and how many of them the shadow rays agree on; by row too. */
  shadow: { compared: number; agree: number; rows: Record<number, { compared: number; agree: number; rasterLit: number; hitLit: number }> }
  /** What the light is scaled by before it is made 8-bit: the raster's 99th percentile luminance at 0.8 (S0). */
  exposure: number
  /**
   * The raster drawing each mesh's sides as its material does, against the
   * shaded rays (D8: a one-sided material's back face lets the ray past):
   * pixels a surface is drawn in, and pixels where the two see different ones.
   */
  surfaces: { drawn: number; mismatched: number }
}

const toSRGB = (x: number) => {
  const c = Math.min(Math.max(x, 0), 1)
  return 255 * (c <= 0.0031308 ? c * 12.92 : 1.055 * c ** (1 / 2.4) - 0.055)
}
const lum = (a: Float32Array, i: number) => 0.2126 * a[i * 4] + 0.7152 * a[i * 4 + 1] + 0.0722 * a[i * 4 + 2]

/**
 * slots: what each material row holds (TraceBase.slots), to name the meshes
 * in the report. turn: the environment turned so (Euler angles) for the
 * check alone, the table's header written for it and back after: the
 * product never turns it, and S1-U1 asks whether a hit samples it as the
 * raster does all the same.
 */
export async function hitCheck(renderer: WebGPURenderer, scene: Scene, camera: PerspectiveCamera, handle: TraceHandle, slots: readonly Slot[], width = 640, turn?: [number, number, number]): Promise<HitReport> {
  const turned = scene.environmentRotation.clone()
  if (turn) {
    scene.environmentRotation.set(...turn)
    handle.tables.setLights(collectLights(scene), scene)
  }
  try {
    return await check(renderer, scene, camera, handle, slots, width)
  } finally {
    if (turn) {
      scene.environmentRotation.copy(turned)
      handle.tables.setLights(collectLights(scene), scene)
    }
  }
}

async function check(renderer: WebGPURenderer, scene: Scene, camera: PerspectiveCamera, handle: TraceHandle, slots: readonly Slot[], width: number): Promise<HitReport> {
  const size = sizeOf(camera, width)
  const traced = collectMeshes(scene, slotOf)
  const shading = hitShading(handle.reflect, handle.tables, scene)
  shading.update()
  const modes: ShadeMode[] = ['id', 'albedo', 'outputs', 'unshadowed', 'direct', 'directUnshadowed']
  const hits = new Map(
    modes.map((mode) => [
      mode,
      {
        rt: floatTarget(size, false),
        pass: rayPass(camera, size, (eye, dir, spread, neighbours) => {
          const { color, dist } = shading.shade(eye, dir, float(0), spread, mode, neighbours)
          return vec4(color, dist)
        }),
      },
    ]),
  )
  const rasters = ['distance', 'albedo', 'outputs', 'lit', 'direct', 'directUnshadowed'] as const
  const raster = new Map(rasters.map((r) => [r, floatTarget(size)]))
  const lights: Light[] = []
  scene.traverse((o) => (o as Light).isLight && lights.push(o as Light))
  // A frame of its own each: three draws a shadow map once a frame, and the variants differ in their shadows.
  const draw = (into: RenderTarget) => {
    gpuOf(renderer).advanceFrame()
    renderer.setRenderTarget(into)
    renderer.render(scene, camera)
  }
  /**
   * The lights as they are, or with no shadows, or the direct ones alone (no
   * environment, no ambient light). No shadows: no mesh casts one. A light's
   * own castShadow is left alone: three frees its shadow node when it is off,
   * and the sun's cascades (sunShadow.ts) are not made again when it is back.
   */
  const lit = (into: RenderTarget, { shadows, indirect }: { shadows: boolean; indirect: boolean }) => {
    const casting = shadows ? [] : traced.filter((t) => t.mesh.castShadow)
    const ambient = indirect ? [] : lights.filter((l) => (l as AmbientLight).isAmbientLight)
    const intensities = ambient.map((l) => l.intensity)
    const { environment } = scene
    for (const t of casting) t.mesh.castShadow = false
    for (const l of ambient) l.intensity = 0
    if (!indirect) scene.environment = null
    try {
      draw(into)
    } finally {
      for (const t of casting) t.mesh.castShadow = true
      ambient.forEach((l, i) => (l.intensity = intensities[i]))
      scene.environment = environment
    }
  }
  isolated(renderer, scene, camera, traced, () => {
    const sided = swapMaterials(traced, (m) => distanceMaterial(m.side))
    try {
      draw(raster.get('distance')!)
    } finally {
      sided()
    }
    for (const what of ['albedo', 'outputs'] as const) {
      const back = swapMaterials(traced, (m) => outputsMaterial(slotOf(m)!, what, m.side))
      try {
        draw(raster.get(what)!)
      } finally {
        back()
      }
    }
    const back = swapMaterials(traced, withoutBump)
    try {
      lit(raster.get('lit')!, { shadows: false, indirect: true })
      lit(raster.get('direct')!, { shadows: true, indirect: false })
      lit(raster.get('directUnshadowed')!, { shadows: false, indirect: false })
    } finally {
      back()
    }
    for (const { rt, pass } of hits.values()) {
      renderer.setRenderTarget(rt)
      pass.quad.render(renderer)
    }
  })
  try {
    const r = Object.fromEntries(await Promise.all(rasters.map(async (k) => [k, await readBack(renderer, raster.get(k)!, size)] as const))) as Record<(typeof rasters)[number], Float32Array>
    const h = Object.fromEntries(await Promise.all(modes.map(async (k) => [k, await readBack(renderer, hits.get(k)!.rt, size)] as const))) as Record<ShadeMode, Float32Array>
    // The e2e's dump (SCENE_E2E_TRACE_DUMP): the pictures, as read back.
    const dump = globalThis as { __traceCheckImages?: unknown }
    if (dump.__traceCheckImages !== undefined) dump.__traceCheckImages = { size, raster: r, hits: h }
    const { width: w, height: ht } = size
    const n = w * ht
    // A pixel counts where the ray and the raster see the same surface; inside a mesh where its four neighbours do and are of the same row.
    const rowAt = new Int32Array(n).fill(-1)
    const surfaces = { drawn: 0, mismatched: 0 }
    for (let i = 0; i < n; i++) {
      const rd = r.distance[i * 4 + 3] > 0.5 ? r.distance[i * 4] : null
      const td = h.id[i * 4 + 3] >= 0 ? h.id[i * 4 + 3] : null
      if (rd !== null) surfaces.drawn++
      if (!sameDistance(rd, td)) surfaces.mismatched++
      if (rd !== null && td !== null && sameDistance(rd, td)) rowAt[i] = Math.round(h.id[i * 4])
    }
    const inside: number[] = []
    for (let y = 1; y < ht - 1; y++)
      for (let x = 1; x < w - 1; x++) {
        const i = y * w + x
        const row = rowAt[i]
        if (row >= 0 && rowAt[i - 1] === row && rowAt[i + 1] === row && rowAt[i - w] === row && rowAt[i + w] === row) inside.push(i)
      }
    const lums = inside.map((i) => lum(r.lit, i)).sort((a, b) => a - b)
    const exposure = 0.8 / Math.max(lums[Math.floor(0.99 * (lums.length - 1))] ?? 1, 1e-6)
    type Sum = { pixels: number; albedo: number; outputs: number; lit: number }
    const sums = new Map<number, Sum>()
    const shadow: HitReport['shadow'] = { compared: 0, agree: 0, rows: {} }
    for (const i of inside) {
      const s = sums.get(rowAt[i]) ?? { pixels: 0, albedo: 0, outputs: 0, lit: 0 }
      sums.set(rowAt[i], s)
      s.pixels++
      for (let c = 0; c < 3; c++) {
        const k = i * 4 + c
        s.albedo += Math.abs(toSRGB(r.albedo[k]) - toSRGB(h.albedo[k])) / 3
        s.outputs += (255 * Math.abs(Math.min(Math.max(r.outputs[k], 0), 1) - Math.min(Math.max(h.outputs[k], 0), 1))) / 3
        s.lit += Math.abs(toSRGB(r.lit[k] * exposure) - toSRGB(h.unshadowed[k] * exposure)) / 3
      }
      // Wholly lit or wholly in shadow by the raster (no penumbra): the shadow rays the same?
      const full = lum(r.directUnshadowed, i)
      if (full > 1e-4) {
        const rasterLit = lum(r.direct, i) / full
        if (rasterLit > 0.98 || rasterLit < 0.02) {
          const hitFull = lum(h.directUnshadowed, i)
          const hitLit = hitFull > 1e-6 ? lum(h.direct, i) / hitFull : 0
          const byRow = (shadow.rows[rowAt[i]] ??= { compared: 0, agree: 0, rasterLit: 0, hitLit: 0 })
          shadow.compared++
          byRow.compared++
          if (rasterLit > 0.5) byRow.rasterLit++
          if (hitLit > 0.5) byRow.hitLit++
          if (rasterLit > 0.5 === hitLit > 0.5) {
            shadow.agree++
            byRow.agree++
          }
        }
      }
    }
    const name = (row: number) => {
      const m = slots[row]?.material
      return !m ? '?' : m.kind === 'pattern' ? m.id : m.kind
    }
    const meshes = [...sums].map(([row, s]) => ({ row, material: name(row), pixels: s.pixels, albedo: s.albedo / s.pixels, outputs: s.outputs / s.pixels, lit: s.lit / s.pixels }))
    return { ...size, meshes: meshes.sort((a, b) => a.row - b.row), shadow, exposure, surfaces }
  } finally {
    for (const rt of raster.values()) rt.dispose()
    for (const { rt, pass } of hits.values()) {
      rt.dispose()
      pass.dispose()
    }
  }
}

export type AtlasCheck = {
  /** Texels read back exactly as written (textureLoad), of all of them. */
  stored: number
  texels: number
  /** Bilinear samples at an explicit level within tolerance of the filtered fp16 values, of all of them. */
  sampled: number
  samples: number
  /** The largest error of a sample channel against its tolerance (within it at 1 or less). */
  worst: number
}

/**
 * S2 design S2-U1: what the probe kernels need of three r186 in compute.
 * One pass writes an RGBA16F storage texture (textureStore), the next loads
 * every texel back and samples it bilinearly at an explicit level
 * (textureSampleLevel), each into a buffer read back here. The values are
 * exact in fp16, so a load returns them as they are and a sample their
 * bilinear blend.
 */
export async function atlasCheck(renderer: WebGPURenderer): Promise<AtlasCheck> {
  const N = 8
  const S = 16
  const atlas = new StorageTexture(N, N)
  atlas.type = HalfFloatType
  atlas.minFilter = atlas.magFilter = LinearFilter
  const valueAt = (x: number, y: number) => [x * 10.5, y * 100.5, 30000, -(x + y) * 0.25]
  const loaded = instancedArray(N * N, 'vec4')
  const sampled = instancedArray(S, 'vec4')
  const write = Fn(() => {
    const x = instanceIndex.mod(N)
    const y = instanceIndex.div(N)
    textureStore(atlas, uvec2(x, y), vec4(float(x).mul(10.5), float(y).mul(100.5), 30000, float(x.add(y)).mul(-0.25))).toWriteOnly()
  })().compute(N * N)
  // Sample i sits 0.2 of a texel right of texel 2(i mod 4) and 0.8 below texel 2⌊i / 4⌋.
  const at = (i: number) => [(2 * (i % 4) + 0.2 + 0.5) / N, (2 * Math.floor(i / 4) + 0.8 + 0.5) / N]
  const read = Fn(() => {
    loaded.element(instanceIndex).assign(textureLoad(atlas, ivec2(instanceIndex.mod(N), instanceIndex.div(N))))
    If(instanceIndex.lessThan(S), () => {
      const u = float(instanceIndex.mod(4)).mul(2).add(0.7).div(N)
      const v = float(instanceIndex.div(4)).mul(2).add(1.3).div(N)
      sampled.element(instanceIndex).assign(texture(atlas, vec2(u, v)).level(0))
    })
  })().compute(N * N)
  try {
    await renderer.computeAsync(write)
    await renderer.computeAsync(read)
    const texels = new Float32Array(await renderer.getArrayBufferAsync(loaded.value))
    const samples = new Float32Array(await renderer.getArrayBufferAsync(sampled.value))
    let stored = 0
    for (let t = 0; t < N * N; t++) {
      const want = valueAt(t % N, Math.floor(t / N))
      if (want.every((w, c) => texels[4 * t + c] === w)) stored++
    }
    let good = 0
    let worst = 0
    for (let i = 0; i < S; i++) {
      const [u, v] = at(i)
      const tx = u * N - 0.5
      const ty = v * N - 0.5
      const x0 = Math.floor(tx)
      const y0 = Math.floor(ty)
      const fx = tx - x0
      const fy = ty - y0
      const lerp = (a: number, b: number, f: number) => a + (b - a) * f
      const corners = [valueAt(x0, y0), valueAt(x0 + 1, y0), valueAt(x0, y0 + 1), valueAt(x0 + 1, y0 + 1)]
      let e = 0
      for (let c = 0; c < 4; c++) {
        const want = lerp(lerp(corners[0][c], corners[1][c], fx), lerp(corners[2][c], corners[3][c], fx), fy)
        // Filtering weights carry 8 bits of fraction on some GPUs (a 1/256 step of the corners' spread), and the result is fp16-rounded.
        const spread = Math.max(...corners.map((k) => k[c])) - Math.min(...corners.map((k) => k[c]))
        const slack = spread / 256 + Math.abs(want) / 1024 + 1e-6
        e = Math.max(e, Math.abs(samples[4 * i + c] - want) / slack)
      }
      worst = Math.max(worst, e)
      if (e <= 1) good++
    }
    return { stored, texels: N * N, sampled: good, samples: S, worst }
  } finally {
    atlas.dispose()
    write.dispose()
    read.dispose()
  }
}

export type GridCheck = {
  /** Slots whose world cell (cellOf) the GPU gives as the JS does, of all tried; the same for the slot of a cell (slotOf). */
  cells: number
  slots: number
  tried: number
  /** Directions the GPU encodes within 1e-5 of the JS, and decodes back within 1e-5, of all tried. */
  encoded: number
  decoded: number
  directions: number
  /** The first few that differ, for the report. */
  samples: unknown[]
}

/**
 * The probes' grid and octahedral functions in TSL against their JS twins
 * (cascade.ts, octahedral.ts), on this device: for boxes with negative and
 * positive origins, each slot's world cell and that cell's slot back; for
 * directions over the sphere (the axes and the folds among them), the
 * octahedral point and the direction it decodes to.
 */
export async function probeGridCheck(renderer: WebGPURenderer): Promise<GridCheck> {
  const origins: [number, number, number][] = [[0, 0, 0], [-8, -3, -4], [-23, 5, -17], [-1, -1, -1], [-16, -32, 48], [7, -100, 3]]
  const slots = [0, 1, 15, 16, 17, 255, 256, 257, 1234, 2047, 3000, 4095]
  const pairs = origins.flatMap((o) => slots.map((slot) => [...o, slot]))
  const directions: [number, number, number][] = [[0, 0, 1], [0, 0, -1], [1, 0, 0], [-1, 0, 0], [0, 1, 0], [0, -1, 0], [1, 1, 0], [-1, 0, -1], [0.3, -0.2, -0.9], [-0.7, 0.7, 0]]
  for (let k = 0; k < 118; k++) {
    const y = 1 - (2 * (k + 0.5)) / 118
    const r = Math.sqrt(1 - y * y)
    const a = k * Math.PI * (3 - Math.sqrt(5))
    directions.push([r * Math.cos(a), r * Math.sin(a), y])
  }
  const unitDirs = directions.map((d) => {
    const l = Math.hypot(...d)
    return d.map((x) => x / l) as [number, number, number]
  })
  const T2 = tsl as unknown as Record<string, Op>
  const { instanceIndex } = tsl as unknown as Record<string, TSLNode>
  const input = T2.instancedArray(new Float32Array(pairs.flat()), 'vec4')
  const cellsOut = T2.instancedArray(pairs.length, 'vec4')
  const dirIn = T2.instancedArray(new Float32Array(unitDirs.flatMap((d) => [...d, 0])), 'vec4')
  const octOut = T2.instancedArray(unitDirs.length, 'vec4')
  const grid = Fn(() => {
    const at = input.element(instanceIndex)
    const cell = cellNode(T2.uint(at.w), at.xyz).toVar()
    cellsOut.element(instanceIndex).assign(vec4(cell, float(slotNode(cell))))
  })().compute(pairs.length)
  const oct = Fn(() => {
    const e = octEncode(dirIn.element(instanceIndex).xyz).toVar()
    const back = octDecode(e)
    octOut.element(instanceIndex).assign(vec4(e, back.xy))
  })().compute(unitDirs.length)
  // The decoded direction's z, separately: four floats a result.
  const octZ = T2.instancedArray(unitDirs.length, 'float')
  const z = Fn(() => {
    octZ.element(instanceIndex).assign(octDecode(octEncode(dirIn.element(instanceIndex).xyz)).z)
  })().compute(unitDirs.length)
  try {
    await renderer.computeAsync([grid, oct, z])
    const c = new Float32Array(await renderer.getArrayBufferAsync(cellsOut.value))
    const o = new Float32Array(await renderer.getArrayBufferAsync(octOut.value))
    const oz = new Float32Array(await renderer.getArrayBufferAsync(octZ.value))
    const samples: unknown[] = []
    let cells = 0
    let slotsBack = 0
    pairs.forEach(([x, y, zz, slot], i) => {
      const want = cellOf(slot, [x, y, zz])
      const got = [c[4 * i], c[4 * i + 1], c[4 * i + 2]]
      if (want.every((w, k) => w === got[k])) cells++
      else if (samples.length < 8) samples.push({ origin: [x, y, zz], slot, want, got })
      if (c[4 * i + 3] === probeSlot(want) && probeSlot(want) === slot) slotsBack++
      else if (samples.length < 8) samples.push({ cell: want, slot, gpu: c[4 * i + 3] })
    })
    let encoded = 0
    let decoded = 0
    unitDirs.forEach((d, i) => {
      const e = encodeOct(d)
      const back = decodeOct(e)
      if (Math.abs(o[4 * i] - e[0]) <= 1e-5 && Math.abs(o[4 * i + 1] - e[1]) <= 1e-5) encoded++
      else if (samples.length < 8) samples.push({ dir: d, want: e, got: [o[4 * i], o[4 * i + 1]] })
      const got = [o[4 * i + 2], o[4 * i + 3], oz[i]]
      if (got.every((g, k) => Math.abs(g - back[k]) <= 1e-5 && Math.abs(g - d[k]) <= 1e-5)) decoded++
      else if (samples.length < 8) samples.push({ dir: d, decoded: got })
    })
    return { cells, slots: slotsBack, tried: pairs.length, encoded, decoded, directions: unitDirs.length, samples }
  } finally {
    grid.dispose()
    oct.dispose()
    z.dispose()
  }
}


// --- probeCheck --------------------------------------------------------------------------------------------------

/** Paths a reference pass traces from each pixel, and the hits each path goes to (S0 probes.md「参考」). */
const PATHS = 8
const BOUNCES = 3

export type ProbeRegion = {
  /** Pixels in the region the layer hits. */
  pixels: number
  /** The indirect irradiance as the composite takes it: the probes' as far as they cover, the environment's for the rest (luminance, exposed). */
  drawn: number
  /** The probes' alone, where they cover; and how far they cover, on average. */
  probes: number
  coverage: number
  /** The environment's (A_env's irradiance). */
  environment: number
  /** The reference's. */
  reference: number
  /** drawn over reference, less 1. */
  error: number
}

export type ProbeCheck = {
  width: number
  height: number
  pathsPerPixel: number
  ms: number
  probes: ProbeVolume['stats']
  /** Each level allocated: its box's lowest cell, the probes on, and their mean data age. */
  levels: { origin: number[]; on: number; dataAge: number }[]
  regions: Record<string, ProbeRegion>
  /** Over every pixel hit: the mean difference |drawn − reference| over the mean reference. */
  meanDifference: number
  /** Per pixel, row 0 at the top: the drawn and the reference irradiance's luminance, null where nothing is hit. */
  drawn: (number | null)[]
  reference: (number | null)[]
}

/**
 * The indirect irradiance at each pixel's surface, from a picture's probes
 * as the composite draws it and from a reference: the pixel's own cosine-
 * weighted paths, BOUNCES hits each shaded as the probes' rays shade theirs,
 * through the layer's original geometry; the last hit alone reads the
 * probes, so their leaks reach the reference only through two albedos (S0
 * probes.md: the first version read them at the first hit and hid half the
 * error). Neither counts the pixel's own ambient lights (A_lt, apart in the
 * composite). Exposed as the picture is: run with its preExposure set.
 * regions are fractions of the picture (x0, y0, x1, y1, from the top left).
 */
export async function probeCheck(renderer: WebGPURenderer, scene: Scene, camera: PerspectiveCamera, handle: TraceHandle, volume: ProbeVolume, { regions, width = 384, passes = 32 }: { regions: Record<string, readonly number[]>; width?: number; passes?: number }): Promise<ProbeCheck> {
  const started = performance.now()
  const size = sizeOf(camera, width)
  const { Loop, abs: absN, cos, cross, sin, sqrt, uint, uvec2: uv2 } = tsl as unknown as Record<string, Op>
  const shading = hitShading(handle.reflect, handle.tables, scene)
  shading.update()
  const fill = float(AMBIENT_FILL)
  const none = () => vec3(0)
  /** The irradiance at a point from the probes as the composite takes it, toward V: (rgb, coverage). */
  const fromProbes = (P: TSLNode, n: TSLNode, V: TSLNode) => {
    const { pre, coverage } = probeIrradiance(volume.nodes, P, n, V)
    return { E: pre.add(shading.environmentIrradiance(n).mul(float(1).sub(coverage))), pre, coverage }
  }
  const pixelKey = () => {
    const xy = uv2(screenUV.mul(vec2(size.width, size.height)))
    return xy.x.add(xy.y.mul(uint(size.width)))
  }
  /** A cosine-weighted direction about n from a hash (Duff et al. 2017's basis). */
  const cosineAbout = (n: TSLNode, h: TSLNode) => {
    const u1 = unit(pcg(h.bitXor(uint(0x68e31da4)))).toVar()
    const u2 = unit(pcg(h.bitXor(uint(0xb5297a4d)))).toVar()
    const sz = select(n.z.lessThan(0), float(-1), float(1))
    const a = float(-1).div(sz.add(n.z))
    const t = vec3(float(1).add(sz.mul(n.x).mul(n.x).mul(a)), sz.mul(n.x).mul(n.y).mul(a), sz.mul(n.x).negate())
    const b = cross(n, t)
    const r = sqrt(u1)
    const phi = u2.mul(2 * Math.PI)
    return normalize(t.mul(r.mul(cos(phi))).add(b.mul(r.mul(sin(phi)))).add(n.mul(sqrt(absN(float(1).sub(u1))))))
  }
  const lum = (v: TSLNode) => luminance(v)
  // (drawn, environment, coverage, the probes' pre-multiplied), the luminances; coverage −1 where nothing is hit.
  const surface = rayPass(camera, size, (eye, dir) => {
    const first = shading.bounce(eye, dir, uint(0), none, fill)
    const out = vec4(0, 0, -1, 0).toVar()
    If(first.front.greaterThan(0.5), () => {
      const p = fromProbes(first.P, first.n, dir.negate())
      out.assign(vec4(lum(p.E), lum(shading.environmentIrradiance(first.n)), p.coverage, lum(p.pre)))
    })
    return out
  })
  const seed = uniform(0, 'uint')
  const reference = rayPass(camera, size, (eye, dir) => {
    const first = shading.bounce(eye, dir, uint(0), none, fill)
    const sum = vec3(0).toVar()
    If(first.front.greaterThan(0.5), () => {
      const key = pcg(pixelKey().bitXor(pcg(seed.add(uint(0x9e3779b9))))).toVar()
      // Loops, not unrolled: each hit's shading inlines every preset and the probes.
      Loop(PATHS, ({ i }: { i: TSLNode }) => {
        const path = pcg(key.add(uint(i).mul(uint(0x85ebca6b)))).toVar()
        const o = first.lifted.toVar()
        const d = cosineAbout(first.n, path).toVar()
        const through = vec3(1).toVar()
        const alive = float(1).toVar()
        Loop(BOUNCES, ({ i: b }: { i: TSLNode }) => {
          If(alive.greaterThan(0.5), () => {
            const h = pcg(path.add(uint(b).add(1).mul(uint(0xc2b2ae35)))).toVar()
            const last = select(uint(b).equal(uint(BOUNCES - 1)), float(1), float(0)).toVar()
            const r = shading.bounce(o, d, h, (P, n) => fromProbes(P, n, d.negate()).E.mul(last), fill)
            sum.addAssign(through.mul(r.L))
            // On from a front face that is not the last, toward a new cosine-weighted direction.
            alive.assign(r.front.mul(float(1).sub(last)))
            through.mulAssign(r.albedo)
            o.assign(r.lifted)
            d.assign(cosineAbout(r.n, pcg(h)))
          })
        })
      })
    })
    return vec4(sum, select(first.front.greaterThan(0.5), float(PATHS), float(0)))
  })
  const targets = [floatTarget(size, false), floatTarget(size, false)]
  const state = RendererUtils.resetRendererState(renderer, undefined as never)
  const n = size.width * size.height
  const sums = new Float64Array(n * 4)
  let pixels: Float32Array
  try {
    renderer.setRenderTarget(targets[0])
    surface.quad.render(renderer)
    pixels = await readBack(renderer, targets[0], size)
    for (let k = 0; k < passes; k++) {
      seed.value = k
      renderer.setRenderTarget(targets[1])
      reference.quad.render(renderer)
      const got = await readBack(renderer, targets[1], size)
      for (let i = 0; i < n * 4; i++) sums[i] += got[i]
    }
  } finally {
    RendererUtils.restoreRendererState(renderer, state)
    for (const d of [...targets, surface, reference]) d.dispose()
  }
  const held = await volume.inspect()
  const levels = Array.from({ length: volume.allocated }, (_, l) => {
    let on = 0
    let age = 0
    for (let p = l * PROBES; p < (l + 1) * PROBES; p++) {
      if (held.offsets[4 * p + 3] > 0.5) on++
      age += held.ages[4 * p]
    }
    return { origin: held.origins[l], on, dataAge: age / PROBES }
  })
  const lumOf = (r: number, g: number, b: number) => 0.2126 * r + 0.7152 * g + 0.0722 * b
  const drawn: (number | null)[] = new Array(n).fill(null)
  const ref: (number | null)[] = new Array(n).fill(null)
  let difference = 0
  let total = 0
  for (let i = 0; i < n; i++) {
    if (pixels[4 * i + 2] < 0 || sums[4 * i + 3] <= 0) continue
    drawn[i] = pixels[4 * i]
    ref[i] = (Math.PI * lumOf(sums[4 * i], sums[4 * i + 1], sums[4 * i + 2])) / sums[4 * i + 3]
    difference += Math.abs(drawn[i]! - ref[i]!)
    total += ref[i]!
  }
  const out: Record<string, ProbeRegion> = {}
  for (const [name, [x0, y0, x1, y1]] of Object.entries(regions)) {
    const acc = { pixels: 0, drawn: 0, probes: 0, covered: 0, coverage: 0, environment: 0, reference: 0 }
    for (let y = Math.floor(y0 * size.height); y < Math.floor(y1 * size.height); y++) {
      for (let x = Math.floor(x0 * size.width); x < Math.floor(x1 * size.width); x++) {
        const i = y * size.width + x
        if (drawn[i] === null) continue
        const c = pixels[4 * i + 2]
        acc.pixels++
        acc.drawn += drawn[i]!
        acc.environment += pixels[4 * i + 1]
        acc.coverage += c
        acc.reference += ref[i]!
        if (c > 0) {
          acc.probes += pixels[4 * i + 3] / c
          acc.covered++
        }
      }
    }
    const m = Math.max(acc.pixels, 1)
    out[name] = {
      pixels: acc.pixels,
      drawn: acc.drawn / m,
      probes: acc.probes / Math.max(acc.covered, 1),
      coverage: acc.coverage / m,
      environment: acc.environment / m,
      reference: acc.reference / m,
      error: acc.drawn / Math.max(acc.reference, 1e-12) - 1,
    }
  }
  return { ...size, pathsPerPixel: passes * PATHS, ms: Math.round(performance.now() - started), probes: volume.stats, levels, regions: out, meanDifference: difference / Math.max(total, 1e-12), drawn, reference: ref }
}
