import { LinearSRGBColorSpace, NoToneMapping, Source, type Camera, type Object3D, type Texture } from 'three'
import * as tsl from 'three/tsl'
import { NodeMaterial, ViewportTextureNode, WebGPURenderer, type Node, type NodeBuilder } from 'three/webgpu'
import type { TSLNode } from '../materials/patterns.ts'
import { backgroundTicker } from '../host/ticker.ts'
import { adapter } from './probe.ts'
import type { FrameStats } from './sceneRenderer.ts'
import { capabilityOf, type Capability, type StorageLimits } from './trace/capacity.ts'
import type { DepthCheck, HitReport } from './trace/checks.ts'
import type { ShadeMode } from './trace/hit.ts'

// The one place a renderer is made (design「渲染器创建入口」): the viewport,
// the asset preview, the offscreen shooter and the thumbnails all get theirs
// here. It picks the backend (WebGPU, or the same renderer's WebGL2 backend
// when there is no adapter), asks the device for the adapter's texture and
// buffer limits instead of WebGPU's defaults (8192, 256 MiB), owns the frame
// counter, and turns a lost device into one error the callers know.
//
// Frames: three advances nodeFrame from its own rAF loop, and nodes that
// update once per frame (passes, denoise, lights) run once per frameId.
// A run of synchronous shots would be one frame, and a hidden tab has no
// loop at all; so the loop is stopped and whoever draws steps the frame
// (Epic design「帧推进与确定性」). three has no public way to do this: the
// private fields are touched here only, and a unit test keeps them in sight.

/**
 * Gives every viewport texture its own size, and lets it go with its render
 * target. Glass samples what is behind it through module-wide
 * ViewportTextureNodes, which keep a texture per render target, cloned from
 * one (three r186):
 *  - a clone shares its source, so the viewport's and an export's sizes
 *    overwrite each other, and each renderer then remakes the texture in the
 *    middle of a pass while its bindings still name the old one ("Destroyed
 *    texture used in a submit");
 *  - nothing frees a clone, so a pipeline built per picture leaves one behind
 *    for every picture with glass. Nor can one be freed with its target: the
 *    node (and every copy a material took, which reads the node's value)
 *    keeps the texture it was last given until its own pass runs, and is
 *    bound before that. A target's texture goes back to the node instead,
 *    for the next target to take: the count stays flat, and what a node
 *    still holds is never a destroyed texture.
 */
export function ownViewportTextures(node: typeof ViewportTextureNode.prototype) {
  const get = node.getTextureForReference
  type Reference = NonNullable<Parameters<typeof get>[0]>
  type Cached = { _cacheTextures: WeakMap<Reference, Texture>; referenceNode?: Cached }
  const spares = new WeakMap<Cached, Texture[]>()
  node.getTextureForReference = function (this: Cached, reference: Reference | null = null) {
    const base = this.referenceNode ?? this
    const cache = base._cacheTextures
    if (reference === null || cache.has(reference)) return get.call(this, reference)
    let spare = spares.get(base)
    if (!spare) spares.set(base, (spare = []))
    let texture = spare.pop()
    if (texture) {
      cache.set(reference, texture)
    } else {
      texture = get.call(this, reference) as Texture
      const { width, height } = texture.image as { width: number; height: number }
      texture.source = new Source({ width, height })
    }
    const own = texture
    const back = spare
    ;(reference as { addEventListener(type: 'dispose', listener: () => void): void }).addEventListener('dispose', () => {
      cache.delete(reference)
      back.push(own)
    })
    return texture
  }
}
ownViewportTextures(ViewportTextureNode.prototype)

/** The most a material may write: under RGBA16F's 65 504, with room for blending. */
export const MAX_OUTPUT = 60000

/**
 * Clamps every node material's colour to MAX_OUTPUT before it reaches a
 * buffer (W2, Epic design「曝光的确定」), the materials three maps from the
 * classic ones included. Under a physical sky a camera may overexpose by
 * many stops, and past half float's top SwiftShader writes Inf, which AgX
 * turns into NaN (74 % of a frame black, exposure.md section 2); what is
 * clamped was far overexposed anyway. Full-screen passes (the sky's LUTs,
 * post-processing) go through here too, far below the bound.
 * three 0.186.1 NodeMaterial.setupOutput (fog, premultiplied alpha):
 * compare on every upgrade (patterns「场景着色写法」).
 */
export function clampMaterialOutput(proto: { setupOutput(builder: NodeBuilder, output: Node): Node; outputClamped?: true }) {
  if (proto.outputClamped) return
  const setup = proto.setupOutput
  const { min, vec3, vec4 } = tsl as unknown as Record<string, (...args: TSLNode[]) => TSLNode>
  proto.setupOutput = function (this: unknown, builder: NodeBuilder, output: Node) {
    const o = output as TSLNode
    // A material writing several targets at once (MRT) is left as it is.
    return setup.call(this, builder, o.isOutputStructNode === true ? o : vec4(min(o.rgb, vec3(MAX_OUTPUT)), o.a))
  }
  proto.outputClamped = true
}
clampMaterialOutput(NodeMaterial.prototype as unknown as Parameters<typeof clampMaterialOutput>[0])


export type Output =
  /** Linear canvas, tone mapping and sRGB in the pipeline's output node, so overlays drawn after it do not wipe the picture. */
  | 'pipeline'
  /** Drawn straight to the canvas with the renderer's own tone mapping and sRGB. */
  | 'direct'

/** Who holds the renderer; the test hooks pick one by it. */
export type Role = 'viewport' | 'offscreen' | 'thumbnail' | 'preview'

export type GPUOptions = {
  canvas: HTMLCanvasElement
  alpha?: boolean
  output: Output
  role: Role
}

export class DeviceLostError extends Error {
  constructor(message = '渲染设备丢失') {
    super(message)
    this.name = 'DeviceLostError'
  }
}

/** A picture past its deadline, given up between two of its frames (host/render.ts). */
export class ShotTimeout extends Error {
  constructor() {
    super('出图超时')
    this.name = 'ShotTimeout'
  }
}

export type SceneGPU = {
  readonly renderer: WebGPURenderer
  readonly backend: 'webgpu' | 'webgl2'
  /** A WebGPU device without core-features-and-limits: three draws it with no MSAA. */
  readonly compatibility: boolean
  readonly maxTextureSize: number
  /** Whether the world-space layer is built on this device, and how much one of its buffers may hold (render/trace/capacity.ts). */
  readonly trace: Capability
  isLost(): boolean
  /** Settles when the device is lost; never when it is disposed. */
  readonly lost: Promise<DeviceLostError>
  /**
   * Draws and encodes in one go: what draw puts on the canvas, as an image
   * file. Every screenshot, export and thumbnail comes out through here, so
   * nothing can wait between the drawing and the encoding.
   */
  capture(draw: () => void, type: string, quality?: number): Blob
  /**
   * Settles once the GPU has done the work submitted so far: between the
   * frames of a picture, so the page's other work (lease renewals, progress,
   * cancelling) runs and a long picture does not starve it. Timed by the
   * background ticker, not animation frames, which a hidden tab has none of
   * (r186 dropped renderer.waitForGPU()).
   */
  settle(): Promise<void>
  /** The next frame, a fixed step after the last. */
  advanceFrame(): void
  /** Back to frame 0, every node due again: the start of a picture. */
  resetFrames(): void
  /**
   * Compiles the scene's materials ahead of the first picture, on the
   * driver's threads where it can. The WebGL2 backend waits for its parallel
   * compile with requestAnimationFrame, which a hidden tab never calls (the
   * assistant's jobs run in one), so in a hidden tab the first picture
   * compiles them.
   */
  compile(scene: Object3D, camera: Camera): Promise<void>
  /** Set by the viewport's SceneRenderer: compile then goes to its passes' targets instead of the canvas's. */
  compiler: (() => Promise<void>) | null
  /** A GPU resource that lives as long as this device. */
  cache<T extends { dispose(): void }>(key: string, create: () => T): T
  /**
   * Lets a storage buffer's GPU copy go now, not when the attribute is
   * collected (three r186's BufferAttribute.dispose() only sends an event,
   * which nothing hears for a storage buffer): for one no frame still to be
   * drawn binds. A frame already submitted keeps it till the GPU is done.
   */
  release(attribute: object): void
  /**
   * Whether the render pipeline last built for a material marked
   * userData.watchPipeline failed: three logs the failure and skips the
   * material's draws, and nothing else is told (the reflections' trace, S1
   * design A13). Known a GPU round trip after the pipeline is built.
   */
  pipelineFailed(material: object): boolean
  /**
   * Whether a compute pipeline of the probes' (a compute node named probe_…,
   * render/probes/kernels.ts) failed, as pipelineFailed tells for a
   * material (S2 design「能力与显存」: the layer then fails for good).
   */
  computeFailed(): boolean
  /** Called with each uncaptured device error's message; returns the unsubscribe. */
  onError(listener: (message: string) => void): () => void
  dispose(): void
}

const frameStep = 1 / 60
const textureCeiling = 16384

/** nodeFrame's fields this module steps; three keeps them on renderer._nodes. */
export type FrameState = {
  frameId: number
  time: number
  deltaTime: number
  lastTime?: number
  updateMap: WeakMap<object, unknown>
  updateBeforeMap: WeakMap<object, unknown>
  updateAfterMap: WeakMap<object, unknown>
}

type Internals = {
  _animation: { stop(): void } | null
  _nodes: { nodeFrame: FrameState } | null
  info: { frame: number }
  /** Frees an attribute's GPU buffer (backend.destroyAttribute); null once the renderer is disposed. */
  _attributes: { delete(attribute: object): unknown } | null
  onError(info: { message?: string }): void
}

/** The backend's side of a render pipeline (WebGPUPipelineUtils): error is set once the device has refused it. */
type PipelineBackend = {
  createRenderPipeline(renderObject: { material: { userData: Record<string, unknown> }; pipeline: object }, promises: unknown): void
  createComputePipeline(pipeline: { computeProgram: { name?: string } }, bindings: unknown, promises?: unknown): void
  get(object: object): { error?: boolean }
}

export function advance(frame: FrameState): void {
  frame.frameId++
  frame.deltaTime = frameStep
  frame.time += frameStep
}

/**
 * three tells a node it is due when the frameId it last ran in differs from
 * the current one. Counting from 0 again, the second picture's frame 1 would
 * look done to every node kept from the first; the records go with the count.
 */
export function reset(frame: FrameState): void {
  frame.frameId = 0
  frame.time = 0
  frame.deltaTime = 0
  frame.lastTime = undefined
  frame.updateMap = new WeakMap()
  frame.updateBeforeMap = new WeakMap()
  frame.updateAfterMap = new WeakMap()
}

/** Set by the e2e fixture page; nothing in the app touches them. */
export const gpuTestHooks = {
  /** Draws through the WebGL2 backend even with an adapter (the backend comparison). */
  forceWebGL: false,
  /** Treats the device as compatibility mode: the workbench drops MSAA for SMAA (no such device to test on). */
  forceCompatibility: false,
  /** Caps the texture size the renderers report: a device too small for an output resolution (R2 AC-R2-6). */
  textureLimit: null as number | null,
  /** The live renderers; programs counts the shader programs each has built (a recompile builds new ones). */
  live: new Set<{ role: Role; info: WebGPURenderer['info']; lose(afterCaptures?: number): void; programs: number }>(),
  /** Loses the device of every live renderer in that role, now or after that many more captures. */
  loseDevice(role: Role, afterCaptures = 0): number {
    let n = 0
    for (const g of gpuTestHooks.live) {
      if (g.role !== role) continue
      g.lose(afterCaptures)
      n++
    }
    return n
  },
  armed: new Map<Role, number>(),
  /** Compiles under way (SceneGPU.compile): a new light shows once its compile is done. */
  compiling: 0,
  /** Compiles started (SceneGPU.compile). */
  compiles: 0,
  /** The viewport's frames since the view last changed (Viewport.tsx LookView): where convergence stopped. */
  viewportFrames: 0,
  /** performance.now() when a physical sky's light (its PMREM) was last made (objects/Lighting.tsx PhysicalLighting). */
  skyLightAt: 0,
  /** Volume marches built (render/volumes/march.ts): none while a scene has no volume, one more per change of its lights. */
  marches: 0,
  /** What the viewport's last frame drew (SceneRenderer.lastFrame; Viewport.tsx LookView, e2e only). */
  lastFrame: null as FrameStats | null,
  /** The viewport's probes after its last frame, and whether it drew with them (SceneRenderer.probeStats; e2e only). */
  probeStats: null as unknown,
  /** The viewport's last frame drawn n times and waited for once, its milliseconds (Viewport.tsx LookView; e2e only). */
  redrawViewport: null as ((n: number) => Promise<number>) | null,
  /** Loses the device of the next renderer made in that role (an offscreen job's), after that many captures. */
  loseNext(role: Role, afterCaptures = 0): void {
    gpuTestHooks.armed.set(role, afterCaptures)
  },
  /**
   * The world-space layer's budget in bytes in place of 1 GiB, and, set before the layer is made, the most one of its
   * buffers may hold: pressed down, a scene goes to tier 2, then too large (render/trace/capacity.ts).
   */
  traceBudget: null as number | null,
  /** No world-space layer, as the developer switch (localStorage scene.trace = off) gives. */
  traceOff: false,
  /** Takes the device for one the layer does not support: no such device to test on. */
  traceUnsupported: false,
  /** Holds each build's answer this long: preparing, and an export's wait running out. */
  traceDelayMs: 0,
  /** What the viewport's layer did last (render/trace/base.ts), e2e only. */
  traceStats: null as unknown,
  /** Set to [] and an export's layer, as it turns ready, adds a digest of each of its buffers (render/trace/base.ts): two exports laid out alike or not. */
  traceDigests: null as Record<string, string>[] | null,
  /** Export layers that let their CPU arrays go after a frame (render/trace/base.ts drawn, design A5). */
  traceDropped: 0,
  /** The reflections' hits shaded another way than lit (render/trace/hit.ts ShadeMode), set before their shader is built: what a defect comes from. */
  traceShade: null as ShadeMode | null,
  /** The viewport's layer against its raster, a primary ray a pixel (render/trace/checks.ts; Viewport.tsx LookView, e2e only); null till it is ready. */
  traceDepth: null as ((width?: number, bias?: number) => Promise<DepthCheck | null>) | null,
  /**
   * Set before an export: its first lit picture drawn with the probes reads, after its last frame, the regions'
   * (fractions of the picture: x0, y0, x1, y1) irradiance from the probes and from a per-pixel reference
   * (render/trace/checks.ts probeCheck; host/render.ts shoot) into result.
   */
  probeRegions: null as { regions: Record<string, readonly number[]>; width?: number; passes?: number; result?: unknown } | null,
  /** What the probes trace (render/trace/base.ts handle): the proxies, as always; 'original' at tier 1 traces the original instead, for S2's AC-S2-8. */
  probeLayer: 'proxy' as 'proxy' | 'original',
  /** The same rays shaded at their hits against the raster, mesh by mesh (AC-S1-2; as traceDepth). */
  traceHits: null as ((width?: number, turn?: [number, number, number]) => Promise<HitReport | null>) | null,
}
// The e2e fixture page sets __sceneTest before the editor loads, and __sceneGPUHooks for switches the viewport's device is made with (?gpu=…).
const fixture = globalThis as { __sceneTest?: boolean; __sceneGPUHooks?: Partial<typeof gpuTestHooks> }
if (fixture.__sceneTest) Object.assign(globalThis, { __sceneGPU: Object.assign(gpuTestHooks, fixture.__sceneGPUHooks) })

/**
 * The viewport's answer to each lost device: draw again on a new one, until
 * the third loss within 10 seconds, when a device that keeps failing is not
 * worth another try (Epic design「设备丢失」).
 */
export function lossCounter(windowMs = 10_000, limit = 3): (now: number) => 'rebuild' | 'stop' {
  const times: number[] = []
  return (now) => {
    times.push(now)
    while (now - times[0] >= windowMs) times.shift()
    return times.length >= limit ? 'stop' : 'rebuild'
  }
}

const byRenderer = new WeakMap<object, SceneGPU>()

/** The SceneGPU a renderer was made by: R3F hands components the bare renderer. */
export function gpuOf(renderer: object): SceneGPU {
  const g = byRenderer.get(renderer)
  if (!g) throw new Error('渲染器不是由 createRenderer 创建的')
  return g
}

export async function createRenderer(options: GPUOptions): Promise<SceneGPU> {
  const g = await make(options)
  byRenderer.set(g.renderer, g)
  return g
}

async function make({ canvas, alpha = false, output, role }: GPUOptions): Promise<SceneGPU> {
  const found = gpuTestHooks.forceWebGL ? null : await adapter()
  const limits = found?.limits
  // The adapter's storage limits too: the world-space layer binds buffers of up to a GiB and more, six in one fragment.
  const requiredLimits = limits
    ? {
        maxTextureDimension2D: Math.min(limits.maxTextureDimension2D, textureCeiling),
        maxBufferSize: limits.maxBufferSize,
        maxStorageBufferBindingSize: limits.maxStorageBufferBindingSize,
        maxStorageBuffersPerShaderStage: limits.maxStorageBuffersPerShaderStage,
      }
    : undefined
  const renderer = new WebGPURenderer({ canvas, alpha, antialias: false, requiredLimits, forceWebGL: gpuTestHooks.forceWebGL || !found })
  await renderer.init()
  const internals = renderer as unknown as Internals
  internals._animation?.stop()
  // drei's GizmoViewport reads the WebGL renderer's capabilities.
  Object.assign(renderer, { capabilities: { getMaxAnisotropy: () => renderer.getMaxAnisotropy() } })
  if (output === 'pipeline') {
    renderer.toneMapping = NoToneMapping
    renderer.outputColorSpace = LinearSRGBColorSpace
  }
  const backendInfo = renderer.backend as { isWebGPUBackend?: boolean; compatibilityMode?: boolean | null; device?: { destroy(): void; limits: StorageLimits & { maxTextureDimension2D: number } } }
  const backend = backendInfo.isWebGPUBackend ? 'webgpu' : 'webgl2'
  const gl = backend === 'webgl2' ? (renderer.getContext() as WebGL2RenderingContext) : null
  const maxTextureSize = Math.min(backendInfo.device?.limits.maxTextureDimension2D ?? (gl?.getParameter(gl.MAX_TEXTURE_SIZE) as number | undefined) ?? 4096, gpuTestHooks.textureLimit ?? Infinity)
  console.info(`scene renderer (${role}): ${backend}${backendInfo.compatibilityMode ? ' compatibility' : ''}`)

  const device = deviceState(() => gl?.isContextLost() ?? false)
  // Both backends report through onDeviceLost; three's own handler marks the
  // renderer lost (render() then does nothing) and filters out dispose().
  const original = renderer.onDeviceLost.bind(renderer)
  renderer.onDeviceLost = (info) => {
    if (device.isLost() || device.disposed) return
    original(info)
    device.markLost()
  }
  const loseNow = () => {
    if (device.isLost() || device.disposed) return
    // The same path a real loss takes; destroy() alone is filtered out as a dispose.
    renderer.onDeviceLost({ api: backend === 'webgpu' ? 'WebGPU' : 'WebGL', message: 'test', reason: 'unknown', originalEvent: null })
    if (gl) gl.getExtension('WEBGL_lose_context')?.loseContext()
    else backendInfo.device?.destroy()
  }
  const hook = { role, info: renderer.info, lose: (afterCaptures = 0) => device.loseAfter(afterCaptures, loseNow), programs: 0 }
  // Info.createProgram is three's (Pipelines calls it for each program it builds), untyped in @types/three.
  const info = renderer.info as unknown as { createProgram(program: unknown): void }
  const createProgram = info.createProgram.bind(info)
  info.createProgram = (program) => {
    hook.programs++
    createProgram(program)
  }
  gpuTestHooks.live.add(hook)
  const armed = gpuTestHooks.armed.get(role)
  if (armed !== undefined) {
    gpuTestHooks.armed.delete(role)
    hook.lose(armed)
  }

  const frame = () => {
    const f = internals._nodes?.nodeFrame
    if (!f) throw new Error('three 的帧状态不可用')
    return f
  }

  // The pipeline last built for each watched material, to tell whether it failed (pipelineFailed).
  const watched = new WeakMap<object, object>()
  // The probes' compute pipelines, by kernel name: the last built of each.
  const computes = new Map<string, object>()
  const pipelines = renderer.backend as unknown as PipelineBackend
  if (backend === 'webgpu') {
    const create = pipelines.createRenderPipeline.bind(pipelines)
    pipelines.createRenderPipeline = (renderObject, promises) => {
      create(renderObject, promises)
      if (renderObject.material.userData.watchPipeline === true) watched.set(renderObject.material, renderObject.pipeline)
    }
    const createCompute = pipelines.createComputePipeline.bind(pipelines)
    pipelines.createComputePipeline = (pipeline, bindings, promises) => {
      createCompute(pipeline, bindings, promises)
      const name = pipeline.computeProgram.name ?? ''
      if (name.startsWith('probe_')) computes.set(name, pipeline)
    }
  }
  const errorListeners = new Set<(message: string) => void>()
  const onError = internals.onError.bind(renderer)
  internals.onError = (info) => {
    onError(info)
    for (const l of errorListeners) l(info.message ?? '')
  }

  const compatibility = backendInfo.compatibilityMode === true || gpuTestHooks.forceCompatibility
  const gpu: SceneGPU = {
    renderer,
    backend,
    compatibility,
    maxTextureSize,
    trace: capabilityOf({ backend, compatibility, limits: backendInfo.device?.limits ?? null, unsupported: gpuTestHooks.traceUnsupported }),
    isLost: device.isLost,
    lost: device.lost,
    capture: (draw, type, quality) => device.capture(draw, () => canvasBlob(renderer.domElement, type, quality)),
    compiler: null,
    async compile(scene, camera) {
      if (backend !== 'webgpu' && document.hidden) return
      gpuTestHooks.compiling++
      gpuTestHooks.compiles++
      try {
        await (gpu.compiler ? gpu.compiler() : renderer.compileAsync(scene, camera))
      } finally {
        gpuTestHooks.compiling--
      }
    },
    async settle() {
      if (device.isLost() || device.disposed) return
      const queue = (backendInfo.device as { queue?: { onSubmittedWorkDone(): Promise<void> } } | undefined)?.queue
      if (queue) return queue.onSubmittedWorkDone()
      if (!gl) return
      const sync = gl.fenceSync(gl.SYNC_GPU_COMMANDS_COMPLETE, 0)
      if (!sync) return
      gl.flush()
      try {
        while (!gl.isContextLost() && gl.clientWaitSync(sync, 0, 0) === gl.TIMEOUT_EXPIRED) await backgroundTicker().wait(1)
      } finally {
        gl.deleteSync(sync)
      }
    },
    advanceFrame() {
      const f = frame()
      advance(f)
      internals.info.frame = f.frameId
    },
    resetFrames() {
      reset(frame())
      internals.info.frame = 0
    },
    cache: device.cache,
    release: (attribute) => void internals._attributes?.delete(attribute),
    pipelineFailed(material) {
      const p = watched.get(material)
      return p !== undefined && pipelines.get(p).error === true
    },
    computeFailed() {
      for (const p of computes.values()) if (pipelines.get(p).error === true) return true
      return false
    },
    onError(listener) {
      errorListeners.add(listener)
      return () => errorListeners.delete(listener)
    },
    dispose() {
      if (device.disposed) return
      device.dispose()
      gpuTestHooks.live.delete(hook)
      renderer.dispose()
      // Give a WebGL context back now rather than when the canvas is collected: browsers cap live contexts.
      gl?.getExtension('WEBGL_lose_context')?.loseContext()
    },
  }
  return gpu
}

export type DeviceState = {
  isLost(): boolean
  readonly lost: Promise<DeviceLostError>
  readonly disposed: boolean
  markLost(): void
  /** Draws, then encodes unless the device is lost by then; a lost device throws DeviceLostError. */
  capture(draw: () => void, encode: () => Blob): Blob
  cache<T extends { dispose(): void }>(key: string, create: () => T): T
  /** Calls lose now, or right after that many more captures have drawn. */
  loseAfter(captures: number, lose: () => void): void
  dispose(): void
}

/**
 * What a device's holder tracks besides the renderer: whether it is lost,
 * the resources that die with it, and the test hook's countdown.
 * contextLost reads a WebGL context's own flag, which is set at once, where
 * onDeviceLost arrives in a later task.
 */
export function deviceState(contextLost: () => boolean = () => false): DeviceState {
  let lost = false
  let disposed = false
  let settle: (err: DeviceLostError) => void = () => {}
  const promise = new Promise<DeviceLostError>((resolve) => (settle = resolve))
  const cached = new Map<string, { dispose(): void }>()
  const release = () => {
    for (const c of cached.values()) c.dispose()
    cached.clear()
  }
  let countdown = 0
  let pending: (() => void) | null = null
  const isLost = () => lost || contextLost()
  return {
    isLost,
    lost: promise,
    get disposed() {
      return disposed
    },
    markLost() {
      if (lost || disposed) return
      lost = true
      release()
      settle(new DeviceLostError())
    },
    capture(draw, encode) {
      if (isLost()) throw new DeviceLostError()
      draw()
      if (pending && --countdown === 0) {
        const lose = pending
        pending = null
        lose()
      }
      if (isLost()) throw new DeviceLostError()
      return encode()
    },
    cache(key, create) {
      if (lost || disposed) throw new DeviceLostError()
      let c = cached.get(key)
      if (!c) {
        c = create()
        cached.set(key, c)
      }
      return c as ReturnType<typeof create>
    },
    loseAfter(captures, lose) {
      if (captures > 0) {
        countdown = captures
        pending = lose
      } else lose()
    },
    dispose() {
      disposed = true
      release()
    },
  }
}

/**
 * The canvas as an image file, encoded now. toBlob encodes in an idle task,
 * which a background tab starts only after a throttled timeout: a hidden
 * page running the assistant's jobs would wait a second or more per image.
 */
export function canvasBlob(canvas: HTMLCanvasElement, type: string, quality?: number): Blob {
  const url = canvas.toDataURL(type, quality)
  if (!url.startsWith(`data:${type};base64,`)) throw new Error('图片编码失败')
  const text = atob(url.slice(url.indexOf(',') + 1))
  const bytes = new Uint8Array(text.length)
  for (let i = 0; i < text.length; i++) bytes[i] = text.charCodeAt(i)
  return new Blob([bytes], { type })
}
