import * as THREE from 'three'
import { float, Fn, max, pass, renderOutput, screenUV, select, vec4 } from 'three/tsl'
import { MeshBasicNodeMaterial, NodeMaterial, QuadMesh, RenderPipeline, RenderTarget, UnsignedByteType, WebGPURenderer } from 'three/webgpu'
import { referenceScene, shotCamera } from './doc.ts'
import { boxRoom, renderGI } from './giCase.ts'

// Group 7 (R0 task 4.3): what readRenderTargetPixelsAsync gives back, on
// either backend, against Epic design「读回」(RGBA8 holding sRGB-encoded
// colour, rows flipped as needed, straight alpha):
//  - format: a four-quadrant test card (linear grey 0.5, red, green at alpha
//    0.5, blue at alpha 0) through four ways of writing the target.
//  - edges: a white sphere with 4× MSAA on a transparent background. The
//    resolve averages covered and cleared samples (premultiplied), and
//    renderOutput keeps colour premultiplied, so edges come out dark unless
//    the output divides by alpha after it.
//  - encoding: toDataURL, toBlob and OffscreenCanvas.convertToBlob timed (the
//    host uses toDataURL because toBlob waits in hidden tabs).
//  - canvasShot: the picture drawn to the canvas and taken with toDataURL in
//    the same task, the WebGL2 backend's way around its rAF-polled read-back.
// autoReport runs groups 1, 5 and 7 and posts the results (for Safari).

/* eslint-disable @typescript-eslint/no-explicit-any -- TSL nodes are loosely typed in three's typings */
type N = any

async function makeRenderer(forceWebGL: boolean, canvas?: HTMLCanvasElement) {
  const r = new WebGPURenderer({ antialias: false, forceWebGL, alpha: true, ...(canvas ? { canvas } : {}) })
  await r.init()
  r.setPixelRatio(1)
  const backend = (r.backend as { isWebGPUBackend?: boolean }).isWebGPUBackend ? 'webgpu' : 'webgl2'
  return { r, backend }
}

/** Read-back rows to tight top-down RGBA: WebGPU pads rows to 256 bytes; WebGL rows are tight and bottom-up. */
function topDown(pixels: Uint8Array, width: number, height: number, backend: string) {
  const row = width * 4
  const stride = backend === 'webgpu' ? Math.ceil(row / 256) * 256 : row
  const out = new Uint8ClampedArray(row * height)
  for (let y = 0; y < height; y++) {
    const from = (backend === 'webgpu' ? y : height - 1 - y) * stride
    out.set(pixels.subarray(from, from + row), y * row)
  }
  return out
}

// Quadrants by screen position (screenUV's origin is the top left on both backends).
const card = Fn(() => {
  const right = screenUV.x.greaterThan(0.5)
  const bottom = screenUV.y.greaterThan(0.5)
  const top = select(right, vec4(1, 0, 0, 1), vec4(0.5, 0.5, 0.5, 1))
  const low = select(right, vec4(0, 0, 1, 0), vec4(0, 1, 0, 0.5))
  return select(bottom, low, top)
})

type Way = 'renderOutput' | 'srgb-target' | 'srgb-target+renderOutput' | 'pipeline-default'

/** The test card written four ways; the read-back bytes at each quadrant's centre, top-down, and the raw first row. */
export async function format(forceWebGL: boolean, constant = false) {
  const { r, backend } = await makeRenderer(forceWebGL)
  const size = 64
  r.setSize(size, size, false)
  r.outputColorSpace = THREE.SRGBColorSpace
  r.toneMapping = THREE.NoToneMapping
  const out: Record<string, unknown> = { backend }
  for (const way of ['renderOutput', 'srgb-target', 'srgb-target+renderOutput', 'pipeline-default'] as Way[]) {
    const target = new RenderTarget(size, size, { type: UnsignedByteType, depthBuffer: false })
    if (way.startsWith('srgb-target')) target.texture.colorSpace = THREE.SRGBColorSpace
    let draw: () => void
    let dispose: () => void
    if (way === 'pipeline-default') {
      // A RenderPipeline left to apply the renderer's tone mapping and output colour space, as group 1's shooter does.
      const pipeline = new RenderPipeline(r)
      pipeline.outputNode = constant ? vec4(0.5, 0.5, 0.5, 1) : card()
      draw = () => pipeline.render()
      dispose = () => pipeline.dispose()
    } else {
      const material = new NodeMaterial()
      // constant: one grey for the whole card, no Fn (to tell a shader build failure from the target).
      const colour: N = constant ? vec4(0.5, 0.5, 0.5, 1) : card()
      material.fragmentNode = way === 'srgb-target' ? colour : renderOutput(colour, THREE.NoToneMapping, THREE.SRGBColorSpace)
      material.blending = THREE.NoBlending
      const quad = new QuadMesh(material)
      draw = () => quad.render(r)
      dispose = () => material.dispose()
    }
    r.setRenderTarget(target)
    r.setClearColor(0x000000, 0)
    r.clear()
    draw()
    const raw = (await r.readRenderTargetPixelsAsync(target, 0, 0, size, size)) as Uint8Array
    r.setRenderTarget(null)
    const px = topDown(raw, size, size, backend)
    const at = (x: number, y: number) => Array.from(px.subarray((y * size + x) * 4, (y * size + x) * 4 + 4))
    out[way] = {
      grey: at(16, 16),
      red: at(48, 16),
      green: at(16, 48),
      blue: at(48, 48),
      // Which way the raw rows run: the first raw row's left pixel (grey if top-down, green if bottom-up).
      firstRawRow: Array.from(raw.subarray(0, 4)),
      rawLength: raw.length,
    }
    dispose()
    target.dispose()
  }
  r.dispose()
  return out
}

/** A white unlit sphere, 4× MSAA, transparent background: alpha and colour on the edge pixels. */
export async function edges(forceWebGL: boolean, unpremultiply: boolean) {
  const { r, backend } = await makeRenderer(forceWebGL)
  const size = 256
  r.setSize(size, size, false)
  const scene = new THREE.Scene()
  scene.add(new THREE.Mesh(new THREE.SphereGeometry(1, 64, 32), new MeshBasicNodeMaterial({ color: '#ffffff' })))
  const camera = new THREE.PerspectiveCamera(40, 1, 0.1, 10)
  camera.position.set(0, 0, 4)
  const beauty: N = pass(scene, camera, { samples: 4 })
  // renderOutput takes premultiplied colour and gives it back premultiplied in the output colour space (r186);
  // unpremultiply: colour over alpha after it, as the canvas path's toDataURL does on encode.
  const encoded: N = renderOutput(beauty, THREE.ACESFilmicToneMapping, THREE.SRGBColorSpace)
  const pipeline = new RenderPipeline(r)
  pipeline.outputColorTransform = false
  pipeline.outputNode = unpremultiply ? vec4(encoded.rgb.div(max(encoded.a, float(1e-4))), encoded.a) : encoded
  const target = new RenderTarget(size, size, { type: UnsignedByteType, depthBuffer: false })
  r.setRenderTarget(target)
  r.setClearColor(0x000000, 0)
  pipeline.render()
  const px = topDown((await r.readRenderTargetPixelsAsync(target, 0, 0, size, size)) as Uint8Array, size, size, backend)
  r.setRenderTarget(null)
  // Edge pixels: partly covered. Straight alpha keeps the sphere's colour there; premultiplied darkens it with coverage.
  let count = 0
  let colour = 0
  let alpha = 0
  let inside = 0
  for (let i = 0; i < px.length; i += 4) {
    if (px[i + 3] === 255 && inside === 0) inside = px[i]
    if (px[i + 3] > 32 && px[i + 3] < 224) {
      count++
      colour += px[i]
      alpha += px[i + 3]
    }
  }
  const c = document.createElement('canvas')
  c.width = size
  c.height = size
  c.getContext('2d')!.putImageData(new ImageData(px as Uint8ClampedArray<ArrayBuffer>, size, size), 0, 0)
  pipeline.dispose()
  beauty.dispose()
  target.dispose()
  r.dispose()
  return { backend, unpremultiply, edgePixels: count, edgeRed: colour / count, edgeAlpha: alpha / count, insideRed: inside, png: c.toDataURL('image/png') }
}

/** A 2048 × 1365 picture encoded three ways; each time from the call to the bytes. */
export async function encoding() {
  const width = 2048
  const height = 1365
  const c = document.createElement('canvas')
  c.width = width
  c.height = height
  const g = c.getContext('2d')!
  const image = g.createImageData(width, height)
  for (let i = 0; i < image.data.length; i += 4) {
    const x = (i / 4) % width
    image.data[i] = (x * 255) / width
    image.data[i + 1] = (Math.random() * 64) | 0
    image.data[i + 2] = 128
    image.data[i + 3] = 255
  }
  g.putImageData(image, 0, 0)
  let t = performance.now()
  c.toDataURL('image/png')
  const toDataURL = performance.now() - t
  t = performance.now()
  await new Promise<Blob | null>((done) => c.toBlob(done, 'image/png'))
  const toBlob = performance.now() - t
  const off = new OffscreenCanvas(width, height)
  off.getContext('2d')!.putImageData(image, 0, 0)
  t = performance.now()
  await off.convertToBlob({ type: 'image/png' })
  const convertToBlob = performance.now() - t
  return { hidden: document.hidden, toDataURL, toBlob, convertToBlob }
}

async function sha256(bytes: Uint8Array | Uint8ClampedArray): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', bytes as Uint8Array<ArrayBuffer>)
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, '0')).join('').slice(0, 16)
}

/** The box room drawn to the canvas and taken with toDataURL in the same task; the decoded pixels' hash. */
export async function canvasShot(forceWebGL: boolean, compare = false) {
  const canvas = document.createElement('canvas')
  const { r, backend } = await makeRenderer(forceWebGL, canvas)
  const width = 960
  const height = 640
  r.setSize(width, height, false)
  r.shadowMap.enabled = true
  const doc = referenceScene('cornell')
  const { scene } = boxRoom(doc)
  const { camera } = shotCamera(doc, null, width / height)
  const pipeline = new RenderPipeline(r)
  pipeline.outputColorTransform = false
  pipeline.outputNode = renderOutput(pass(scene, camera), THREE.ACESFilmicToneMapping, THREE.SRGBColorSpace)
  const started = performance.now()
  r.setRenderTarget(null)
  pipeline.render()
  const url = canvas.toDataURL('image/png')
  const ms = performance.now() - started
  const bitmap = await createImageBitmap(await (await fetch(url)).blob())
  const c = document.createElement('canvas')
  c.width = width
  c.height = height
  const g = c.getContext('2d')!
  g.drawImage(bitmap, 0, 0)
  const pixels = g.getImageData(0, 0, width, height).data
  let lit = 0
  for (let i = 0; i < pixels.length; i += 4) if (pixels[i] + pixels[i + 1] + pixels[i + 2] > 30) lit++
  // compare: the same frame into an RGBA8 target and read back; mean absolute difference from the canvas (0-255).
  let vsTarget: number | null = null
  if (compare) {
    const target = new RenderTarget(width, height, { type: UnsignedByteType, depthBuffer: false })
    r.setRenderTarget(target)
    pipeline.render()
    const read = topDown((await r.readRenderTargetPixelsAsync(target, 0, 0, width, height)) as Uint8Array, width, height, backend)
    r.setRenderTarget(null)
    let sum = 0
    for (let i = 0; i < read.length; i++) if ((i & 3) !== 3) sum += Math.abs(read[i] - pixels[i])
    vsTarget = sum / (width * height * 3)
    target.dispose()
  }
  pipeline.dispose()
  r.dispose()
  return { backend, hidden: document.hidden, ms, hash: await sha256(pixels), litShare: lit / (width * height), vsTarget, png: url }
}

/** Groups 1, 5 and 7 in one go, posted as JSON to report (Safari, which no script here drives). */
export async function autoReport(report: string) {
  const result: Record<string, unknown> = { userAgent: navigator.userAgent, webgpu: !!navigator.gpu }
  const step = async (name: string, run: () => Promise<unknown>) => {
    const started = performance.now()
    try {
      result[name] = await run()
    } catch (e) {
      result[name] = { error: String(e).slice(0, 500) }
    }
    ;(result[name] as Record<string, unknown>).ms = performance.now() - started
    document.getElementById('root')!.textContent = `done: ${Object.keys(result).join(', ')}`
  }
  await step('adapter', async () => {
    const adapter = await navigator.gpu?.requestAdapter()
    if (!adapter) return { adapter: null }
    const info = (adapter as unknown as { info?: GPUAdapterInfo }).info
    return { vendor: info?.vendor, architecture: info?.architecture, maxTextureDimension2D: adapter.limits.maxTextureDimension2D, maxBufferSize: adapter.limits.maxBufferSize, features: [...adapter.features] }
  })
  await step('format', () => format(false))
  await step('edges', async () => {
    const { png: _png, ...rest } = await edges(false, false)
    return rest
  })
  await step('shooter', async () => {
    const run = (window as unknown as { __shooter: { run(o: object): Promise<{ backend: string; shots: { hash: string; ms: number; png: string }[] }> } }).__shooter
    const r = await run.run({ scene: 'cornell', control: true, k: 12 })
    return { backend: r.backend, hashes: r.shots.map((s) => s.hash), ms: r.shots.map((s) => s.ms), png: r.shots[0].png }
  })
  await step('gi', async () => {
    const r = await renderGI({ variant: 'full-frame', width: 960, height: 640, k: 32, accumulate: true })
    return { backend: r.backend, msPerFrame: r.msPerFrame, png: r.png }
  })
  await fetch(report, { method: 'POST', headers: { 'content-type': 'text/plain' }, body: JSON.stringify(result) })
  document.getElementById('root')!.textContent = 'reported'
}

export function exposeReadback() {
  ;(window as unknown as { __readback: unknown }).__readback = { format, edges, encoding, canvasShot, autoReport }
}
