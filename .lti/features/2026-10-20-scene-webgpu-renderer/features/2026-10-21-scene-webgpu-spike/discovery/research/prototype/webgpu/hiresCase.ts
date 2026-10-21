import * as THREE from 'three'
import { directionToColor, dot, float, Fn, max, mix, normalView, perspectiveDepthToViewZ, renderOutput, screenUV, smoothstep, texture, uniform, vec2, vec3, vec4 } from 'three/tsl'
import { DepthTexture, HalfFloatType, MeshBasicNodeMaterial, NearestFilter, NodeMaterial, QuadMesh, RenderTarget, UnsignedByteType, WebGPURenderer } from 'three/webgpu'
import { depthThreshold, normalThreshold } from '../../src/creative-scene/render/linePass.ts'
import { referenceScene, shotCamera } from './doc.ts'
import { controlFrames, type Frames } from './frames.ts'
import { perfScene, pipelineFor } from './gbufferCase.ts'
import { boxRoom, giPipeline, type GIVariant } from './giCase.ts'
import { renderTSL } from './materialsCase.ts'

// Group 6 (R0 task 4.2): exports at 2K, 4K and 8K (long edge 2048 / 4096 /
// 8192 px). Each picture is drawn in one go or in n × n tiles (the full
// frustum cropped per tile, optionally with a margin drawn and cut off), then
// read back, encoded as the host does (canvas toDataURL) and measured:
//  - workbench: the group 4 performance scene, opaque prepass + GTAO, 4× MSAA.
//  - gi: the box room, real-time GI with per-frame SSGI noise and k frames
//    averaged; SSGI optionally at a fraction of the picture's size.
//  - line: linePass.ts in TSL, normals and depth at 1× or 2× the picture
//    (2× of 8K needs a device with maxTextureDimension2D 16384).
// Device loss and validation errors are recorded, not thrown.

/* eslint-disable @typescript-eslint/no-explicit-any -- TSL nodes are loosely typed in three's typings */
type N = any
type Kind = 'workbench' | 'gi' | 'line'
type Tile = { fullWidth: number; fullHeight: number; x: number; y: number; width: number; height: number }

/**
 * A tile of a larger picture: the full picture's frustum cropped to the tile,
 * with any view offset (TRAA's jitter, in the tile's pixels) applied inside
 * it. setViewOffset cannot carry the tile: TRAA sets and clears it each frame.
 */
export class TileCamera extends THREE.PerspectiveCamera {
  tile: Tile | null = null

  override copy(source: THREE.PerspectiveCamera & { tile?: Tile | null }, recursive?: boolean) {
    super.copy(source, recursive)
    this.tile = source.tile ? { ...source.tile } : null
    return this
  }

  /** three's setViewOffset sets the aspect to the drawn size's; a tile keeps the full picture's. */
  override setViewOffset(fullWidth: number, fullHeight: number, x: number, y: number, width: number, height: number) {
    const aspect = this.aspect
    super.setViewOffset(fullWidth, fullHeight, x, y, width, height)
    if (this.tile) {
      this.aspect = aspect
      this.updateProjectionMatrix()
    }
  }

  override updateProjectionMatrix() {
    const near = this.near
    let top = (near * Math.tan(THREE.MathUtils.DEG2RAD * 0.5 * this.fov)) / this.zoom
    let height = 2 * top
    let width = this.aspect * height
    let left = -0.5 * width
    // Unset while the base constructor runs.
    const t = this.tile
    if (t) {
      left += (t.x * width) / t.fullWidth
      top -= (t.y * height) / t.fullHeight
      width *= t.width / t.fullWidth
      height *= t.height / t.fullHeight
    }
    const v = this.view
    if (v?.enabled) {
      left += (v.offsetX * width) / v.fullWidth
      top -= (v.offsetY * height) / v.fullHeight
      width *= v.width / v.fullWidth
      height *= v.height / v.fullHeight
    }
    this.projectionMatrix.makePerspective(left, left + width, top, top - height, near, this.far, this.coordinateSystem, this.reversedDepth)
    this.projectionMatrixInverse.copy(this.projectionMatrix).invert()
  }
}

type Drawn = { pixels: Uint8ClampedArray; gpuMs: number; readMs: number; textureMB: number }
type Drawer = { camera: THREE.PerspectiveCamera; draw(camera: TileCamera, width: number, height: number): Promise<Drawn> }

/** Read-back rows (WebGPU pads them to 256 bytes, the last row tight) to tight RGBA. */
function tight(pixels: Uint8Array, width: number, height: number): Uint8ClampedArray {
  const row = width * 4
  const stride = Math.ceil(row / 256) * 256
  const out = new Uint8ClampedArray(row * height)
  for (let y = 0; y < height; y++) out.set(pixels.subarray(y * stride, y * stride + row), y * row)
  return out
}

/** Frames drawn into target, the GPU drained (timed), then the whole target read back (timed). */
async function finish(r: WebGPURenderer, target: RenderTarget, width: number, height: number, started: number): Promise<Drawn> {
  await r.readRenderTargetPixelsAsync(target, 0, 0, 1, 1)
  const gpuMs = performance.now() - started
  const textureMB = r.info.memory.texturesSize / 2 ** 20
  const t = performance.now()
  const pixels = tight((await r.readRenderTargetPixelsAsync(target, 0, 0, width, height)) as Uint8Array, width, height)
  return { pixels, gpuMs, readMs: performance.now() - t, textureMB }
}

function workbench(r: WebGPURenderer, clock: Frames): Drawer {
  const { scene, camera } = perfScene(false)
  return {
    camera,
    async draw(tileCamera, width, height) {
      r.setSize(width, height, false)
      const built = pipelineFor(r, 'prepass', 'picture', { scene, camera: tileCamera })
      const target = new RenderTarget(width, height, { type: UnsignedByteType, depthBuffer: false })
      r.setRenderTarget(target)
      const started = performance.now()
      clock.reset()
      clock.step()
      built.pipeline.render()
      const drawn = await finish(r, target, width, height, started)
      r.setRenderTarget(null)
      built.dispose()
      target.dispose()
      return drawn
    },
  }
}

function realtimeGI(r: WebGPURenderer, clock: Frames, aspect: number, k: number, giScale: number, variant: GIVariant, withTRAA: boolean): Drawer {
  const doc = referenceScene('cornell')
  const { scene, spot } = boxRoom(doc)
  const { camera } = shotCamera(doc, null, aspect)
  return {
    camera,
    async draw(tileCamera, width, height) {
      r.setSize(width, height, false)
      const built = giPipeline(r, scene, tileCamera, spot, variant, true, giScale, withTRAA)
      // The linear picture of every frame averaged (acc = mix(acc, frame, 1/n)), tone mapped once at the end.
      const frameTarget = new RenderTarget(width, height, { type: HalfFloatType, depthBuffer: false })
      let accRead = new RenderTarget(width, height, { type: HalfFloatType, depthBuffer: false })
      let accWrite = new RenderTarget(width, height, { type: HalfFloatType, depthBuffer: false })
      const target = new RenderTarget(width, height, { type: UnsignedByteType, depthBuffer: false })
      const weight = uniform(1)
      const previous = texture(accRead.texture)
      const accQuad = new QuadMesh(Object.assign(new NodeMaterial(), { fragmentNode: mix(previous.sample(screenUV), texture(frameTarget.texture).sample(screenUV), weight) }))
      const latest = texture(accRead.texture)
      const outQuad = new QuadMesh(Object.assign(new NodeMaterial(), { fragmentNode: renderOutput(latest.sample(screenUV), THREE.ACESFilmicToneMapping, THREE.SRGBColorSpace) }))
      const started = performance.now()
      clock.reset()
      for (let n = 1; n <= k; n++) {
        clock.step()
        r.setRenderTarget(frameTarget)
        built.pipeline.render()
        weight.value = 1 / n
        previous.value = accRead.texture
        r.setRenderTarget(accWrite)
        accQuad.render(r)
        ;[accRead, accWrite] = [accWrite, accRead]
      }
      latest.value = accRead.texture
      r.setRenderTarget(target)
      outQuad.render(r)
      const drawn = await finish(r, target, width, height, started)
      r.setRenderTarget(null)
      built.dispose()
      for (const t of [frameTarget, accRead, accWrite, target]) t.dispose()
      for (const q of [accQuad, outQuad]) q.material.dispose()
      return drawn
    },
  }
}

/** linePass.ts's edge shader in TSL: normal turns and depth jumps in a 3 × 3 ring, four taps per output pixel. */
function edges(normalTex: THREE.Texture, depthTex: THREE.Texture, camera: THREE.PerspectiveCamera, width: number, height: number, scale: number) {
  const texel = vec2(1 / (width * scale), 1 / (height * scale))
  // About 2 output pixels of line at any picture size, as linePass.ts.
  const radius = Math.max(1, Math.round(((Math.max(width, height) / 1024) * scale) / 2))
  const near = float(camera.near)
  const far = float(camera.far)
  const depthAt = (uv: N) => perspectiveDepthToViewZ(texture(depthTex, uv).x, near, far).negate()
  const normalAt = (uv: N) => texture(normalTex, uv).xyz.mul(2).sub(1)
  const edgeAt = (uv: N) => {
    const nc = normalAt(uv).toVar()
    const zc = depthAt(uv).toVar()
    let normalEdge: N = float(0)
    let depthEdge: N = float(0)
    for (let y = -1; y <= 1; y++) {
      for (let x = -1; x <= 1; x++) {
        const at = uv.add(vec2(x, y).mul(texel).mul(radius))
        normalEdge = max(normalEdge, float(1).sub(dot(nc, normalAt(at))))
        depthEdge = max(depthEdge, depthAt(at).sub(zc).abs().div(zc))
      }
    }
    return max(smoothstep(normalThreshold, normalThreshold * 1.5, normalEdge), smoothstep(depthThreshold, depthThreshold * 1.5, depthEdge))
  }
  return Fn(() => {
    const h = texel.mul(0.5)
    const edge = edgeAt(screenUV.add(vec2(h.x.negate(), h.y.negate())))
      .add(edgeAt(screenUV.add(vec2(h.x, h.y.negate()))))
      .add(edgeAt(screenUV.add(vec2(h.x.negate(), h.y))))
      .add(edgeAt(screenUV.add(h)))
      .mul(0.25)
    return vec4(vec3(float(1).sub(edge)), 1)
  })()
}

export function lineArt(r: WebGPURenderer, scale: number): Drawer {
  const { scene, camera } = perfScene(false)
  return {
    camera,
    async draw(tileCamera, width, height) {
      r.setSize(width, height, false)
      const w = width * scale
      const h = height * scale
      const normals = new RenderTarget(w, h, { minFilter: NearestFilter, magFilter: NearestFilter, depthTexture: new DepthTexture(w, h) })
      // Packed view normals as they are: MeshNormalNodeMaterial converts them from sRGB to linear (made for the canvas), which a linear target keeps.
      const material = new MeshBasicNodeMaterial({ colorNode: directionToColor(normalView) })
      const quad = new QuadMesh(Object.assign(new NodeMaterial(), { fragmentNode: edges(normals.texture, normals.depthTexture!, tileCamera, width, height, scale) }))
      const target = new RenderTarget(width, height, { type: UnsignedByteType, depthBuffer: false })
      const started = performance.now()
      // Background and sky are not lines: empty pixels sit at the far plane.
      const background = scene.background
      scene.background = null
      scene.overrideMaterial = material
      r.setClearColor(0x8080ff, 1)
      r.setRenderTarget(normals)
      r.clear()
      r.render(scene, tileCamera)
      scene.background = background
      scene.overrideMaterial = null
      r.setRenderTarget(target)
      quad.render(r)
      const drawn = await finish(r, target, width, height, started)
      r.setRenderTarget(null)
      normals.depthTexture?.dispose()
      normals.dispose()
      target.dispose()
      material.dispose()
      quad.material.dispose()
      return drawn
    },
  }
}

const dataBytes = (url: string) => Math.floor(((url.length - url.indexOf(',') - 1) * 3) / 4)

/** The picture encoded as the host does (toDataURL): PNG, WebP at quality 1 (lossless in Chrome) and JPEG at 0.92 and 0.95, timed; a 1600 px JPEG preview; full-size crops. */
function encode(canvas: HTMLCanvasElement, crops: [number, number, number][]) {
  let t = performance.now()
  const png = canvas.toDataURL('image/png')
  const pngMs = performance.now() - t
  t = performance.now()
  const webp = canvas.toDataURL('image/webp', 1)
  const webpMs = performance.now() - t
  const jpeg = canvas.toDataURL('image/jpeg', 0.92)
  const jpeg95 = canvas.toDataURL('image/jpeg', 0.95)
  const p = document.createElement('canvas')
  p.width = 1600
  p.height = Math.round((1600 * canvas.height) / canvas.width)
  p.getContext('2d')!.drawImage(canvas, 0, 0, p.width, p.height)
  const cropped = crops.map(([x, y, size]) => {
    const c = document.createElement('canvas')
    c.width = size
    c.height = size
    c.getContext('2d')!.drawImage(canvas, x, y, size, size, 0, 0, size, size)
    return c.toDataURL('image/png')
  })
  return {
    pngMB: dataBytes(png) / 2 ** 20,
    pngMs,
    webpMB: webp.startsWith('data:image/webp') ? dataBytes(webp) / 2 ** 20 : null,
    webpMs,
    jpegMB: dataBytes(jpeg) / 2 ** 20,
    jpeg95MB: dataBytes(jpeg95) / 2 ** 20,
    preview: p.toDataURL('image/jpeg', 0.85),
    crops: cropped,
  }
}

function toCanvas(pixels: Uint8ClampedArray, width: number, height: number) {
  const c = document.createElement('canvas')
  c.width = width
  c.height = height
  c.getContext('2d')!.putImageData(new ImageData(pixels as Uint8ClampedArray<ArrayBuffer>, width, height), 0, 0)
  return c
}

const seamsOf = (size: number, n: number) => Array.from({ length: n - 1 }, (_, i) => Math.round(((i + 1) * size) / n))

/** Mean absolute difference (0-255, per channel) between two pictures: everywhere, within 4 px of a seam, and elsewhere. */
function difference(a: Uint8ClampedArray, b: Uint8ClampedArray, width: number, height: number, n: number) {
  const xs = seamsOf(width, n)
  const ys = seamsOf(height, n)
  let band = 0
  let bandCount = 0
  let rest = 0
  let restCount = 0
  for (let y = 0; y < height; y++) {
    const nearY = ys.some((s) => Math.abs(y - s) < 4)
    for (let x = 0; x < width; x++) {
      const i = (y * width + x) * 4
      const d = Math.abs(a[i] - b[i]) + Math.abs(a[i + 1] - b[i + 1]) + Math.abs(a[i + 2] - b[i + 2])
      if (nearY || xs.some((s) => Math.abs(x - s) < 4)) {
        band += d
        bandCount += 3
      } else {
        rest += d
        restCount += 3
      }
    }
  }
  return { mean: (band + rest) / (bandCount + restCount), band: band / bandCount, rest: rest / restCount }
}

/** The jump across each seam (the pixels either side) against the same measure 20 px to either side of it. */
function seamJump(p: Uint8ClampedArray, width: number, height: number, n: number) {
  const step = (i: number, j: number) => (Math.abs(p[i] - p[j]) + Math.abs(p[i + 1] - p[j + 1]) + Math.abs(p[i + 2] - p[j + 2])) / 3
  let across = 0
  let control = 0
  let count = 0
  for (const s of seamsOf(width, n)) {
    for (let y = 0; y < height; y++) {
      const at = (x: number) => (y * width + x) * 4
      across += step(at(s), at(s - 1))
      control += (step(at(s + 20), at(s + 19)) + step(at(s - 20), at(s - 21))) / 2
      count++
    }
  }
  for (const s of seamsOf(height, n)) {
    for (let x = 0; x < width; x++) {
      const at = (y: number) => (y * width + x) * 4
      across += step(at(s), at(s - 1))
      control += (step(at(s + 20), at(s + 19)) + step(at(s - 20), at(s - 21))) / 2
      count++
    }
  }
  return count ? { across: across / count, control: control / count } : null
}

// One-shot pictures kept for comparing the tiled ones against.
const oneShots = new Map<string, Uint8ClampedArray>()

export type HiresOptions = {
  kind: Kind
  width: number
  height: number
  /** Tiles per side; 1 draws the picture in one go. */
  tiles?: number
  /** Pixels drawn beyond each tile's edges and cut off. */
  margin?: number
  /** gi: frames averaged. */
  k?: number
  /** gi: SSGI's size over the picture's. */
  giScale?: number
  /** gi: the giCase.ts variant, full-frame by default. */
  giVariant?: GIVariant
  /** gi: false leaves TRAA out (k frames are still averaged). */
  traa?: boolean
  /** line: normals and depth at this multiple of the picture. */
  lineScale?: number
  /** maxTextureDimension2D to request from the device. */
  limit?: number
  /** Keep a one-shot picture, or compare a tiled one with the one kept at the same kind and size. */
  compare?: boolean
  crops?: [number, number, number][]
  /** Debugging: draw only this tile (row-major). */
  only?: number
}

export async function renderHires(o: HiresOptions) {
  const { kind, width, height } = o
  const r = new WebGPURenderer({ antialias: false, ...(o.limit ? { requiredLimits: { maxTextureDimension2D: o.limit } } : {}) })
  await r.init()
  r.setPixelRatio(1)
  r.shadowMap.enabled = true
  r.shadowMap.type = THREE.PCFShadowMap
  let lost: string | null = null
  r.onDeviceLost = (info: { message?: string; reason?: string }) => {
    lost = info.message ?? String(info.reason)
  }
  const device = (r.backend as unknown as { device: GPUDevice }).device
  const errors: string[] = []
  device.addEventListener('uncapturederror', (e) => errors.push((e as GPUUncapturedErrorEvent).error.message.slice(0, 300)))
  const limits = { maxTextureDimension2D: device.limits.maxTextureDimension2D, maxBufferSize: device.limits.maxBufferSize }
  const clock = controlFrames(r)
  const drawer = kind === 'workbench' ? workbench(r, clock) : kind === 'gi' ? realtimeGI(r, clock, width / height, o.k ?? 16, o.giScale ?? 1, o.giVariant ?? 'full-frame', o.traa ?? true) : lineArt(r, o.lineScale ?? 1)
  const camera = new TileCamera().copy(drawer.camera)
  camera.aspect = width / height
  const n = o.tiles ?? 1
  const margin = n > 1 ? (o.margin ?? 0) : 0
  const picture = new Uint8ClampedArray(width * height * 4)
  const tiles: { gpuMs: number; readMs: number; textureMB: number }[] = []
  const started = performance.now()
  try {
    for (let ty = 0; ty < n; ty++) {
      for (let tx = 0; tx < n; tx++) {
        if (o.only !== undefined && o.only !== ty * n + tx) continue
        const x0 = Math.round((tx * width) / n)
        const y0 = Math.round((ty * height) / n)
        const w = Math.round(((tx + 1) * width) / n) - x0
        const h = Math.round(((ty + 1) * height) / n) - y0
        camera.tile = n > 1 ? { fullWidth: width, fullHeight: height, x: x0 - margin, y: y0 - margin, width: w + 2 * margin, height: h + 2 * margin } : null
        camera.updateProjectionMatrix()
        const drawn = await drawer.draw(camera, w + 2 * margin, h + 2 * margin)
        tiles.push({ gpuMs: drawn.gpuMs, readMs: drawn.readMs, textureMB: drawn.textureMB })
        const row = (w + 2 * margin) * 4
        for (let y = 0; y < h; y++) picture.set(drawn.pixels.subarray((y + margin) * row + margin * 4, (y + margin) * row + (margin + w) * 4), ((y0 + y) * width + x0) * 4)
      }
    }
  } catch (e) {
    r.dispose()
    return { ok: false, error: String(e).slice(0, 500), lost, errors, limits }
  }
  const ms = performance.now() - started
  const key = `${kind} ${width}x${height}`
  let seams = null
  if (o.compare && n === 1) oneShots.set(key, picture)
  else if (o.compare && oneShots.has(key)) seams = difference(oneShots.get(key)!, picture, width, height, n)
  const crops = o.crops ?? [[Math.round(width / 2) - 256, Math.round(height / 2) - 256, 512]]
  const encoded = encode(toCanvas(picture, width, height), crops)
  r.dispose()
  return { ok: !lost && errors.length === 0, lost, errors, limits, ms, tiles, seams, jump: n > 1 ? seamJump(picture, width, height, n) : null, ...encoded }
}

/** The group 2 materials scene (15 procedural materials) at a size: how large real textured pictures get. */
export async function materialSizes(width: number, height: number) {
  const url = await renderTSL(width, height)
  const bitmap = await createImageBitmap(await (await fetch(url)).blob())
  const c = document.createElement('canvas')
  c.width = width
  c.height = height
  c.getContext('2d')!.drawImage(bitmap, 0, 0)
  const { preview: _preview, crops: _crops, ...sizes } = encode(c, [])
  return sizes
}

/** The adapter's limits, as a device could request them. */
export async function adapterLimits() {
  const adapter = await navigator.gpu.requestAdapter()
  return { maxTextureDimension2D: adapter!.limits.maxTextureDimension2D, maxBufferSize: adapter!.limits.maxBufferSize }
}

export function exposeHires() {
  ;(window as unknown as { __hires: unknown }).__hires = { renderHires, materialSizes, adapterLimits }
}
