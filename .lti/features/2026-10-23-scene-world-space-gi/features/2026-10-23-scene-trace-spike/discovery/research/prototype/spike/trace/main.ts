// scene-world-space-gi S0 (scene-trace-spike): the spike page. On the dev server only, as creative-lab.html (no
// build entry); moved into the change's discovery when S0 ends (design「穿刺页面」).
//
//   ?scene=<reference scene | stress>   what to draw (cornell by default)
//   ?limits=default                     ask for no storage limits beyond WebGPU's defaults: a smaller device
//   ?cold=1                             a constant new every load in the shaders the spike builds (coldSalt)
//   ?case=<group>                       the spike group to run over the scene: integration, throughput, parallel, shading,
//                                       reflections (?rmax= the retrace's roughness limit, 1 by default), probes
//   ?part=rays,edit&cache=clear|use     throughput's parts, and whether its geometry cache starts empty
//
// window.__spike holds the renderer, the scene and what was asked for and given, for the measure scripts.
import { WebGPURenderer } from 'three/webgpu'
import { runIntegration } from './integration.ts'
import { runParallel } from './parallel.ts'
import { runProbes } from './probes.ts'
import { runReflections } from './reflections.ts'
import { runShading } from './shading.ts'
import { runThroughput } from './throughput.ts'
import { buildReference, buildStress, type Built } from './scene.ts'

const query = new URLSearchParams(location.search)
const sceneName = query.get('scene') ?? 'cornell'
const defaultLimits = query.get('limits') === 'default'
const spikeCase = query.get('case')

/** A constant that differs every load when ?cold=1: built into a shader, it misses every cached pipeline (lesson webgpu-perf-warm-shader-cache). */
export const coldSalt = query.get('cold') === '1' ? Math.random() : 0

/** The limits the world-space layer may need (Epic design「能力与容量」), asked for up to what the adapter has. */
const wanted = ['maxStorageBufferBindingSize', 'maxBufferSize', 'maxStorageBuffersPerShaderStage', 'maxStorageTexturesPerShaderStage', 'maxComputeWorkgroupStorageSize', 'maxComputeInvocationsPerWorkgroup'] as const

type GPUAdapterLike = { limits: Record<string, number>; info?: Record<string, string>; featureLevel?: string; features: Set<string> }
type Spike = {
  ready: boolean
  error: string | null
  renderer?: WebGPURenderer
  built?: Built
  /** The case's outcome (?case=…), images as PNG data URLs. */
  result?: Record<string, unknown>
  report(): Record<string, unknown>
  render(): number
}

const spike: Spike = {
  ready: false,
  error: null,
  report: () => ({}),
  render: () => 0,
}
;(window as unknown as { __spike: Spike }).__spike = spike

const info = document.getElementById('info')!
const show = (lines: string[]) => (info.textContent = lines.join('\n'))

async function boot() {
  const gpu = (navigator as unknown as { gpu?: { requestAdapter(o?: object): Promise<GPUAdapterLike | null> } }).gpu
  const adapter = gpu && (await gpu.requestAdapter())
  if (!adapter) throw new Error('no WebGPU adapter')
  const requiredLimits: Record<string, number> = {}
  if (!defaultLimits) for (const k of wanted) if (adapter.limits[k] !== undefined) requiredLimits[k] = adapter.limits[k]
  const canvas = document.getElementById('view') as HTMLCanvasElement
  const renderer = new WebGPURenderer({ canvas, antialias: false, requiredLimits, trackTimestamp: true })
  await renderer.init()
  const backend = renderer.backend as unknown as { isWebGPUBackend?: boolean; device?: { limits: Record<string, number>; features: Set<string> } }
  if (!backend.isWebGPUBackend || !backend.device) throw new Error('the renderer fell back to WebGL2')
  const device = backend.device
  spike.renderer = renderer

  const built = sceneName === 'stress' ? await buildStress() : await buildReference(sceneName)
  spike.built = built
  const render = () => {
    const started = performance.now()
    renderer.render(built.scene, built.camera)
    return performance.now() - started
  }
  spike.render = render
  const firstMs = render()
  const limit = (o: Record<string, number>) => Object.fromEntries(wanted.map((k) => [k, o[k]]))
  spike.report = () => ({
    scene: sceneName,
    limitsAsked: defaultLimits ? 'default' : 'adapter',
    // GPUAdapterInfo's fields are getters on its prototype: spread, it is empty.
    adapter: { info: adapter.info ? Object.fromEntries(['vendor', 'architecture', 'device', 'description'].map((k) => [k, adapter.info?.[k] ?? ''])) : null, featureLevel: adapter.featureLevel ?? null, limits: limit(adapter.limits) },
    device: { limits: limit(device.limits), features: [...device.features].sort() },
    scene_stats: { meshes: built.meshes.length, triangles: built.triangles, buildMs: Math.round(built.buildMs), ...('generateMs' in built ? { generateMs: Math.round(built.generateMs as number) } : {}) },
    firstRenderMs: Math.round(firstMs),
    coldSalt,
  })
  const mib = (v: number) => `${(v / 2 ** 20).toFixed(0)} MiB`
  show([
    `scene ${sceneName}: ${built.meshes.length} meshes, ${built.triangles.toLocaleString()} triangles, built in ${Math.round(built.buildMs)} ms`,
    `adapter ${adapter.info?.vendor ?? '?'} ${adapter.info?.architecture ?? ''} ${adapter.featureLevel ?? ''}`,
    `limits ${defaultLimits ? 'default' : 'as the adapter has'}:`,
    `  storage binding ${mib(device.limits.maxStorageBufferBindingSize)} (adapter ${mib(adapter.limits.maxStorageBufferBindingSize)})`,
    `  buffer ${mib(device.limits.maxBufferSize)} (adapter ${mib(adapter.limits.maxBufferSize)})`,
    `  storage buffers per stage ${device.limits.maxStorageBuffersPerShaderStage} (adapter ${adapter.limits.maxStorageBuffersPerShaderStage})`,
    `first frame ${Math.round(firstMs)} ms${coldSalt ? ', cold' : ''}`,
  ])
  if (spikeCase === 'integration') {
    // 1536 × 1024: a float row of a width that is a multiple of 16 is 256-byte aligned, as WebGPU reads a texture back.
    const r = await runIntegration(renderer, built, 1536, 1024)
    const { mask, normals, ...rest } = r
    spike.result = { ...rest, maskPNG: png(mask.width, mask.height, mask.rgba), normalsPNG: png(mask.width, mask.height, normalImage(normals)) }
  } else if (spikeCase === 'throughput') {
    spike.result = await runThroughput(renderer, built, sceneName, query)
  } else if (spikeCase === 'parallel') {
    spike.result = await runParallel()
  } else if (spikeCase === 'shading') {
    spike.result = await runShading(renderer, built, coldSalt)
  } else if (spikeCase === 'reflections') {
    spike.result = await runReflections(renderer, built, sceneName, query)
  } else if (spikeCase === 'probes') {
    spike.result = await runProbes(renderer, built, sceneName, query)
  }
  spike.ready = true
}

/** RGBA bytes as a PNG data URL. */
function png(width: number, height: number, rgba: Uint8ClampedArray) {
  const c = document.createElement('canvas')
  c.width = width
  c.height = height
  c.getContext('2d')!.putImageData(new ImageData(rgba, width, height), 0, 0)
  return c.toDataURL('image/png')
}

/** Traced (distance, normal) per pixel as an image: the normal as colour, misses black. */
function normalImage(hits: Float32Array) {
  const out = new Uint8ClampedArray((hits.length / 4) * 4)
  for (let p = 0; p < hits.length / 4; p++) {
    if (hits[4 * p] < 0) out.set([0, 0, 0, 255], 4 * p)
    else out.set([0, 1, 2].map((k) => 127.5 * (hits[4 * p + 1 + k] + 1)).concat(255), 4 * p)
  }
  return out
}

boot().catch((e: unknown) => {
  spike.error = String((e as Error)?.stack ?? e)
  show([`error: ${spike.error}`])
})
