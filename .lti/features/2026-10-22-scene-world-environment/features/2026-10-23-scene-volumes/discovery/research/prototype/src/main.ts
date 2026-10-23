// @ts-nocheck — spike page: window.__spike drives each case from Playwright.
import { AgXToneMapping, DepthTexture, HalfFloatType, NodeMaterial, QuadMesh, RenderTarget, SRGBColorSpace, UnsignedByteType, WebGPURenderer } from 'three/webgpu'
import { Fn, renderOutput, screenUV, texture, vec4 } from 'three/tsl'
import { fills, many, room } from './room.ts'
import { Volumes, cameraRig, froxelsB, lightRig, marchA, volumeShadows } from './vol.ts'

const params = new URLSearchParams(location.search)
const forceWebGL = params.get('backend') === 'webgl'
const canvas = document.getElementById('c') as HTMLCanvasElement
const log = (s: string) => (document.getElementById('log')!.textContent += s + '\n')

const renderer = new WebGPURenderer({ canvas, antialias: false, forceWebGL })
await renderer.init()
renderer.shadowMap.enabled = true
const backend = renderer.backend.isWebGPUBackend ? 'webgpu' : 'webgl2'
log(`backend ${backend}`)

function composite(color, depth, apply) {
  const m = new NodeMaterial()
  m.fragmentNode = Fn(() => {
    const c = texture(color, screenUV)
    return renderOutput(vec4(apply ? apply(c, depth) : c.rgb, 1), AgXToneMapping, SRGBColorSpace)
  })()
  return new QuadMesh(m)
}

async function png(target, width, height) {
  const raw = await renderer.readRenderTargetPixelsAsync(target, 0, 0, width, height)
  const c = document.createElement('canvas')
  c.width = width
  c.height = height
  const ctx = c.getContext('2d')
  const img = ctx.createImageData(width, height)
  // WebGL2 reads bottom row first.
  for (let y = 0; y < height; y++) {
    const from = backend === 'webgl2' ? height - 1 - y : y
    // WebGPU pads each row to 256 bytes.
    const stride = backend === 'webgpu' ? Math.ceil((width * 4) / 256) * 256 : width * 4
    img.data.set(raw.subarray(from * stride, from * stride + width * 4), y * width * 4)
  }
  ctx.putImageData(img, 0, 0)
  return c.toDataURL('image/png')
}

/**
 * One case: scene, volumes, a method (none | A | B) with its settings; the
 * median frame time with and without the volumes in the same build, and the
 * picture.
 */
async function run({ scene = 'room', fill = 'room', count = 0, method = 'A', scale = 0.5, steps = 48, fx = 160, fy = 90, fz = 64, selfShadow = false, blur = false, useMask = true, ambient = false, castShadow = false, width = 1920, height = 1080, repeat = 30, shot = true, sun = {}, outside = false, spot = true, point = true, view = 'window' }) {
  const built = room({ ...sun, outside, spot, point, view })
  const { camera, lights } = built
  camera.aspect = width / height
  camera.updateProjectionMatrix()
  const V = new Volumes()
  V.set(count ? many(count) : fills[fill])

  const beauty = new RenderTarget(width, height, { type: HalfFloatType, depthBuffer: true })
  beauty.depthTexture = new DepthTexture(width, height)
  const ldr = new RenderTarget(width, height, { type: UnsignedByteType, depthBuffer: false })
  const drawScene = () => {
    renderer.setRenderTarget(beauty)
    renderer.render(built.scene, camera)
  }
  const shadowRefresh = castShadow ? volumeShadows(V, lights) : () => {}
  shadowRefresh()
  drawScene() // the shadow maps exist after this
  const rig = lightRig({ ...lights, scene: built.scene, ambient }, camera)
  const cam = cameraRig(camera)
  const way = method === 'A' ? marchA(V, rig, cam, beauty.depthTexture, { width, height, scale, steps, selfShadow, blur, useMask }) : method === 'B' ? froxelsB(V, rig, cam, { fx, fy, fz, selfShadow }) : null
  const plain = composite(beauty.texture, beauty.depthTexture, null)
  const fogged = way && composite(beauty.texture, beauty.depthTexture, (c, d) => way.apply(c, d))

  const frame = (withVolume) => {
    shadowRefresh()
    cam.refresh()
    rig.refresh()
    drawScene()
    if (withVolume && way) way.render(renderer, camera)
    renderer.setRenderTarget(ldr)
    ;(withVolume && fogged ? fogged : plain).render(renderer)
  }
  const sync = async () => {
    if (backend === 'webgpu') await renderer.backend.device.queue.onSubmittedWorkDone()
    else await renderer.readRenderTargetPixelsAsync(ldr, 0, 0, 1, 1)
  }
  const errors = []
  const t0 = performance.now()
  frame(true)
  await sync()
  const firstMs = performance.now() - t0
  frame(false)
  await sync()
  const time = async (withVolume) => {
    const out = []
    for (let i = 0; i < repeat + 3; i++) {
      await sync()
      const t = performance.now()
      frame(withVolume)
      await sync()
      if (i >= 3) out.push(performance.now() - t)
    }
    out.sort((a, b) => a - b)
    return { median: +out[out.length >> 1].toFixed(2), min: +out[0].toFixed(2), max: +out[out.length - 1].toFixed(2) }
  }
  // Throughput: n frames back to back, one wait; per frame. Waiting per frame under WebGL2 lands on the compositor's ticks.
  const through = async (withVolume, n = 30) => {
    const out = []
    for (let r = 0; r < 5; r++) {
      await sync()
      const t = performance.now()
      for (let i = 0; i < n; i++) frame(withVolume)
      await sync()
      out.push((performance.now() - t) / n)
    }
    out.sort((a, b) => a - b)
    return +out[2].toFixed(2)
  }
  const result = { backend, method, ambient, castShadow, blur, scale, steps, fx, fy, fz, selfShadow, volumes: V.count.value, width, height, firstMs: +firstMs.toFixed(1) }
  if (repeat > 0) {
    // Interleaved, so drift hits both alike.
    result.base = await time(false)
    result.withVolume = await time(true)
    result.base2 = await time(false)
    result.costMs = +(result.withVolume.median - Math.min(result.base.median, result.base2.median)).toFixed(2)
    const b1 = await through(false)
    const w1 = await through(true)
    const b2 = await through(false)
    result.through = { base: Math.min(b1, b2), withVolume: w1, cost: +(w1 - Math.min(b1, b2)).toFixed(2) }
  }
  if (shot) {
    frame(true)
    await sync()
    result.png = await png(ldr, width, height)
  }
  way?.dispose()
  beauty.dispose()
  ldr.dispose()
  return result
}

window.__spike = { run }
log('ready')
