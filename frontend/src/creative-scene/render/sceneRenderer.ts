import { ACESFilmicToneMapping, AgXToneMapping, Color, DataTexture, Group, HalfFloatType, Layers, NoToneMapping, RepeatWrapping, Scene, SRGBColorSpace, UnsignedByteType, Vector2, Vector3, type Camera, type DirectionalLight, type Light, type LightShadow, type Mesh, type Object3D, type PerspectiveCamera, type SpotLight } from 'three'
import { float, mix, mrt, normalView, pass, renderOutput, texture, uniform, uv, vec4 } from 'three/tsl'
import { MeshBasicNodeMaterial, NodeMaterial, NodeUpdateType, PassNode, QuadMesh, RenderPipeline, RenderTarget, RendererUtils, TempNode, type Node, type NodeFrame } from 'three/webgpu'
import { denoise } from 'three/addons/tsl/display/DenoiseNode.js'
import { smaa } from 'three/addons/tsl/display/SMAANode.js'
import { SimplexNoise } from 'three/addons/math/SimplexNoise.js'
import type { DepthOfField } from '../cameraOptics.ts'
import { depthOfField, type Dof } from './dof.ts'
import { convergeFrames, type Engine, type Quality } from './engines.ts'
import { gpuTestHooks, type SceneGPU } from './gpu.ts'
import { ao } from './nodes/GTAONode.js'
import { GILightingPass, splitContext } from './giSplit.ts'
import { realtimeGI, type RealtimeGI } from './realtimeGI.ts'
import { aimStudio } from './studio.ts'
import { cascadeSunShadow } from './sunShadow.ts'
import { AMBIENT_FILL } from './probes/cascade.ts'
import { ProbeVolume } from './probes/volume.ts'
import { traceBase, type TraceBase, type TraceHandle } from './trace/base.ts'
import { hitShading } from './trace/hit.ts'
import { applyExposure, preExposure } from '../world/exposure.ts'
import { nightLookNode } from '../world/nightLook.ts'
import { aerialBehind, finishAerial, prepareAerial } from '../world/atmosphere/aerial.ts'
import { applyVolumes } from './volumes/composite.ts'
import { VolumeNode } from './volumes/march.ts'
import { shadowLike } from './volumes/shadow.ts'
import { finishVolumes, prepareVolumes } from './volumes/state.ts'

// The rendered looks (design「导出渲染」, owner 2026-10-19): the scene as
// mounted, lit as its look says (objects/Lighting.tsx), with ambient
// occlusion so contacts and corners read, then the camera's depth of field
// when it is on. The editor viewport, render_view and the exports all draw
// through it, so what the viewport shows is what the export gets. AO comes
// before the blur, so contact shadows out of focus soften with everything
// else. Clay and lit differ only in what is mounted and in the exposure.
//
// The workbench (Epic design「渲染管线组成」): a prepass of the opaque
// surfaces gives the AO its normals and depth, so glass and the grid neither
// cast AO nor hide what is behind them; the picture is drawn with 4× MSAA
// and the AO multiplied over it, as the WebGL GTAOPass did. The canvas is
// linear and tone mapping happens in the output node (render/gpu.ts,
// 'pipeline'), so the transform gizmo, drawn by a pass of its own, is laid
// over the finished picture in sRGB.
//
// The real-time GI (R2) is the other renderer for the rendered view and lit
// in a perspective view: its own passes and composite (render/realtimeGI.ts),
// antialiased by TRAA instead of MSAA, then the same depth of field, tone
// mapping and overlay. The workbench is drawn exactly as before.

/** Viewport helpers the clay look treats apart; an export has none. */
export type Overlays = {
  /** Drawn in the picture but kept out of the AO: the grid's plane would shade what stands on it. */
  aside: () => readonly (Object3D | null)[]
  /** Drawn after AO and depth of field, sharp and unshaded: the transform gizmo. */
  top: () => readonly (Object3D | null)[]
}

const noOverlays: Overlays = { aside: () => [], top: () => [] }

/**
 * The layer of the viewport's helpers: the workbench's picture draws it, the real-time GI a pass of its own
 * over its picture (realtimeGI.ts aside); no prepass does.
 */
export const ASIDE_LAYER = 30
/**
 * The layer of the sky (objects/Lighting.tsx): the picture draws it, the workbench's and the real-time GI's
 * lighting pass alike; no prepass does, as it has no surface there. Not the helpers' layer: a picture to keep
 * has no pass for those.
 */
export const SKY_LAYER = 29
/** The layer only the overlay pass draws; the gizmo's raycaster has to see it too. */
export const OVERLAY_LAYER = 31

const layers = (...ids: number[]) => {
  const l = new Layers()
  l.disableAll()
  for (const id of ids) l.enable(id)
  return l
}

export type Frame = {
  width: number
  height: number
  /** Device pixels per CSS pixel; an export draws at 1. */
  pixelRatio?: number
  /**
   * The pixel ratio the AO is worked out at, when lower than pixelRatio: the
   * viewport's at 1 (CSS pixels), a quarter of the work on a 2× screen, and
   * laid over the full-resolution picture. An export works it out in full.
   */
  aoPixelRatio?: number
  /** Blurs by this lens; null keeps the picture sharp. */
  dof: DepthOfField | null
  /** The long side of the camera's picture in device pixels, when it is not the whole frame. */
  pictureLongSide?: number
  /** Ambient occlusion; on unless turned off. */
  ao?: boolean
  /** toneMappingExposure for this frame: 2^exposure_ev for lit under any sky but the physical one, 1 otherwise. */
  exposure?: number
  /**
   * The pre-exposure k for this frame (world/exposure.ts): 1 / (1.2 · 2^EV100)
   * for lit under a physical sky, 1 otherwise. Written onto the scene's lights
   * and the global uniform just before drawing.
   */
  preExposure?: number
  /** The night look's strength for this frame (world/nightLook.ts): lit under a physical sky after dusk, 0 otherwise. */
  nightLook?: number
  /** Wireframe has no surfaces to shade: no AO, no blur. */
  wireframe?: boolean
  /**
   * The renderer (render/engines.ts effectiveEngine); the workbench unless
   * said. The real-time GI needs a perspective camera and surfaces: an
   * orthographic view and wireframe are drawn by the workbench whatever this says.
   */
  engine?: Engine
  /** Real-time GI: interactive unless said; a picture to keep is final. */
  quality?: Quality
  /**
   * Averages this renderer's frames after the first convergeFrames, after
   * the depth of field and before tone mapping: an export's or render_view's
   * frames (R2 design「累积与出图」). The viewport does not.
   */
  accumulate?: boolean
}

/**
 * One frame's load (scene-performance「每帧统计」): the draws and triangles
 * three counted in the pipeline's passes, and the CPU milliseconds of all of
 * render(), its preparations (aerial perspective, volumes) included.
 */
export type FrameStats = { readonly draws: number; readonly triangles: number; readonly cpuMs: number }

/**
 * What run draws: three's render counts after it less before. three resets
 * them only as its animation loop begins a frame (r186 Animation.js), never
 * inside a render, so autoReset is left as it is.
 */
export function measure(counts: { readonly drawCalls: number; readonly triangles: number }, run: () => void, started = performance.now()): FrameStats {
  const draws = counts.drawCalls
  const triangles = counts.triangles
  run()
  return { draws: counts.drawCalls - draws, triangles: counts.triangles - triangles, cpuMs: performance.now() - started }
}

/**
 * A colour for something drawn straight onto the 'pipeline' canvas after
 * the picture (drei's axis widget): three writes it linear and nothing
 * encodes it, so it is given encoded once more to show as asked.
 */
export function overlayColor(hex: string): string {
  // Color decodes the hex to linear; written as is, that linear value shows
  // as if it were sRGB. Encoding twice makes what is written the sRGB value.
  const c = new Color(hex)
  const encode = (x: number) => (x <= 0.0031308 ? x * 12.92 : 1.055 * x ** (1 / 2.4) - 0.055)
  return `#${[c.r, c.g, c.b].map((x) => Math.round(Math.min(1, encode(encode(x))) * 255).toString(16).padStart(2, '0')).join('')}`
}

/**
 * DenoiseNode's noise texture (generateDefaultNoise, three r186), but from a
 * fixed seed: three's seeds it with Math.random, and with a pipeline per
 * picture no two exports of the same scene would be alike (AC-R1-3).
 */
function fixedNoise(size = 64): DataTexture {
  let seed = 0x5eed
  const random = () => {
    seed = (seed + 0x6d2b79f5) | 0
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
  const simplex = new SimplexNoise({ random })
  const data = new Uint8Array(size * size * 4)
  const at = (x: number, y: number) => (simplex.noise(x, y) * 0.5 + 0.5) * 255
  for (let i = 0; i < size; i++) {
    for (let j = 0; j < size; j++) {
      const k = (i * size + j) * 4
      data[k] = at(i, j)
      data[k + 1] = at(i + size, j)
      data[k + 2] = at(i, j + size)
      data[k + 3] = at(i + size, j + size)
    }
  }
  const texture = new DataTexture(data, size, size)
  texture.wrapS = RepeatWrapping
  texture.wrapT = RepeatWrapping
  texture.needsUpdate = true
  return texture
}

type StandIn = { meshes: Group; lights: Scene; shadows: (() => void)[] }

/**
 * What SceneRenderer.compile compiles: one copy of each kind of visible mesh
 * where the mesh is (geometry and material shared, its flags kept), and the
 * visible lights copied into a scene with the same environment and fog. A
 * copied light gets a shadow node of the kind its light has (three's
 * LightShadow.copy leaves it out, and the default one builds another shader:
 * the real materials compiled again on the next frame).
 */
function standIn(scene: Scene): StandIn {
  const shadows: (() => void)[] = []
  const lights = new Scene()
  lights.environment = scene.environment
  lights.environmentIntensity = scene.environmentIntensity
  lights.environmentRotation.copy(scene.environmentRotation)
  lights.fog = scene.fog
  const meshes = new Group()
  const kinds = new Set<string>()
  scene.updateMatrixWorld()
  scene.traverseVisible((o) => {
    const copyAt = (c: Object3D) => {
      c.matrixAutoUpdate = false
      c.matrix.copy(o.matrixWorld)
      return c
    }
    if ((o as Light).isLight) {
      const copy = o.clone(false) as Light
      const undo = shadowLike(o as Light, copy, cascadeSunShadow)
      if (undo) shadows.push(undo)
      lights.add(copyAt(copy))
      return
    }
    const mesh = o as Mesh
    if (!mesh.isMesh) return
    const materials = Array.isArray(mesh.material) ? mesh.material : [mesh.material]
    // What a pipeline depends on: the vertex layout, not the geometry itself.
    const layout = [...Object.keys(mesh.geometry.attributes).sort(), mesh.geometry.index ? 'indexed' : '']
    const kind = [mesh.type, ...layout, ...materials.map((m) => m.uuid), mesh.castShadow, mesh.receiveShadow, mesh.layers.mask].join('|')
    if (kinds.has(kind)) return
    kinds.add(kind)
    meshes.add(copyAt(mesh.clone(false)))
  })
  lights.updateMatrixWorld()
  return { meshes, lights, shadows }
}

/** Lets three free what it keeps for a stand-in: its meshes' render objects (and their pipelines), its lights' shadow maps. */
function disposeStandIn({ meshes, lights, shadows }: StandIn) {
  for (const undo of shadows) undo()
  for (const m of meshes.children) m.dispose()
  for (const l of lights.children as (Light & { shadow?: LightShadow })[]) {
    // Never drawn into, so three never set up freeing its textures; the materials bound the depth.
    l.shadow?.map?.depthTexture?.dispose()
    l.dispose()
  }
}

/** A stand-in being compiled: its meshes, and the passes it compiles for (with the target each compiles into) by their MRT (null: none). */
type Compiling = { meshes: ReadonlySet<Object3D>; passes: ReadonlyMap<Node | null, { node: PassNode; target: RenderTarget }> }
type RenderContext = { mrt: Node | null }
type RenderObjects = { get(object: Object3D, material: unknown, scene: unknown, camera: unknown, lightsNode: unknown, renderContext: RenderContext, ...rest: unknown[]): unknown }
type NodeManager = { getForRenderAsync(renderObject: { object: Object3D; context: RenderContext }): Promise<unknown> }
const compiling = new Set<Compiling>()
const wrapped = new WeakSet<object>()

/**
 * compileAsync (three r186) takes the renderer's target, MRT and context as
 * each render object is made and its material built, a task or more after
 * the call, with them as the frames have left them by then. The target and
 * MRT give a material its outputs; the context matters to the
 * real-time GI: its prepass and lighting pass draw the same materials, told
 * apart by the lighting pass's context (render objects and built materials
 * are cached by material and context, not by MRT). Compiled with the frames'
 * context, the two passes' stand-ins shared one built material, and one of
 * them got a shader of the wrong number of outputs (WebGPU refuses its
 * pipeline); the lighting pass's real meshes then compiled on the next frame,
 * holding the viewport for seconds. A render object reads the context when
 * made and a material reads all three when its build starts
 * (NodeBuilder.prebuild, synchronously), so for a stand-in's mesh the pass's
 * are set around those two calls alone.
 */
function asItsPass(renderer: SceneGPU['renderer']) {
  const inPass = <T>(object: Object3D, renderContext: RenderContext, call: () => T): T => {
    for (const c of compiling) {
      const pass = c.meshes.has(object) ? c.passes.get(renderContext.mrt) : undefined
      if (!pass) continue
      const target = renderer.getRenderTarget()
      const mrt = renderer.getMRT()
      const context = renderer.contextNode
      renderer.setRenderTarget(pass.target)
      renderer.setMRT(pass.node.getMRT())
      if (pass.node instanceof GILightingPass) renderer.contextNode = splitContext(context as Parameters<typeof splitContext>[0])
      try {
        return call()
      } finally {
        renderer.setRenderTarget(target)
        renderer.setMRT(mrt)
        renderer.contextNode = context
      }
    }
    return call()
  }
  const { _objects: objects, _nodes: nodes } = renderer as unknown as { _objects: RenderObjects; _nodes: NodeManager }
  if (!wrapped.has(objects)) {
    wrapped.add(objects)
    const get = objects.get.bind(objects)
    objects.get = (object, material, scene, camera, lightsNode, renderContext, ...rest) => inPass(object, renderContext, () => get(object, material, scene, camera, lightsNode, renderContext, ...rest))
  }
  if (!wrapped.has(nodes)) {
    wrapped.add(nodes)
    const build = nodes.getForRenderAsync.bind(nodes)
    nodes.getForRenderAsync = (renderObject) => inPass(renderObject.object, renderObject.context, () => build(renderObject))
  }
}

type Shadowing = DirectionalLight | SpotLight

const lightAt = /*@__PURE__*/ new Vector3()
const targetAt = /*@__PURE__*/ new Vector3()

/**
 * The light the real-time GI traces contact shadows from (R2 design「实时 GI
 * 管线」): the sun (Lighting.tsx marks it) while it is up; else the
 * shadow-casting light that lights the scene most, a directional light by
 * its intensity, a spot by its intensity over the distance to its target
 * squared, both where the world has them. A point light has no one direction
 * to trace along.
 */
export function mainLight(scene: Object3D): Shadowing | null {
  let sun: Shadowing | null = null
  let best: Shadowing | null = null
  let most = 0
  scene.traverseVisible((o) => {
    const light = o as Shadowing
    if (!(light as DirectionalLight).isDirectionalLight && !(light as SpotLight).isSpotLight) return
    if (light.intensity <= 0) return
    if (light.userData.sun === true) {
      sun = light
      return
    }
    if (!light.castShadow) return
    const lux = (light as SpotLight).isSpotLight ? light.intensity / Math.max(light.getWorldPosition(lightAt).distanceToSquared(light.target.getWorldPosition(targetAt)), 0.01) : light.intensity
    if (lux > most) {
      most = lux
      best = light
    }
  })
  return sun ?? best
}

/**
 * How a frame is drawn, which decides the pipeline: the real-time GI or the
 * workbench; the real-time GI's world-space layer, once ready (S1); its
 * probes, once warm (S2).
 */
type Look = { wireframe: boolean; blur: boolean; gi: boolean; quality: Quality; accumulate: boolean; trace: TraceHandle | null; world: ProbeVolume | null }

const keyOf = ({ wireframe, blur, gi, quality, accumulate, trace, world }: Look) =>
  wireframe ? 'wire' : `${gi ? `gi-${quality}${trace ? `+trace-${trace.key}` : ''}${world ? `+probes-${world.serial}` : ''}` : 'shaded'}${blur ? '+dof' : ''}${accumulate ? '+acc' : ''}`

const accumulateSize = /*@__PURE__*/ new Vector2()
let accumulateState: ReturnType<typeof RendererUtils.resetRendererState> | undefined

/**
 * The running average of a picture's frames, in linear half float:
 * acc = mix(acc, frame, 1 / n), n the frames since the warm-up. The
 * warm-up frames are kept, not averaged: the temporal nodes' first frame is
 * far off in HDR (one frame of it brightened a 16-frame average by half, R2
 * tasks 5.1), and they settle within the viewport's convergeFrames. Two
 * targets in turn; the node is the latest average.
 */
class AccumulateNode extends TempNode {
  private readonly input: Node<'vec4'>
  private readonly warmup: number
  private targets: [RenderTarget, RenderTarget] = [new RenderTarget(1, 1, { type: HalfFloatType, depthBuffer: false }), new RenderTarget(1, 1, { type: HalfFloatType, depthBuffer: false })]
  private readonly weight = uniform(1)
  private readonly previous = texture(this.targets[0].texture)
  private readonly latest = texture(this.targets[1].texture)
  private readonly material = new NodeMaterial()
  private readonly quad = new QuadMesh(this.material)
  private frames = 0

  constructor(input: Node<'vec4'>, warmup: number) {
    super('vec4')
    this.input = input
    this.warmup = warmup
    this.updateBeforeType = NodeUpdateType.FRAME
  }

  setup() {
    this.material.fragmentNode = mix(this.previous.sample(uv()), this.input, this.weight)
    this.material.needsUpdate = true
    return this.latest
  }

  updateBefore(frame: NodeFrame): undefined {
    const renderer = frame.renderer!
    const { width, height } = renderer.getDrawingBufferSize(accumulateSize)
    for (const t of this.targets) t.setSize(width, height)
    const [read, write] = this.targets
    this.frames++
    this.weight.value = 1 / Math.max(1, this.frames - this.warmup)
    this.previous.value = read.texture
    // Made on the first call (three's own nodes keep theirs the same way).
    accumulateState = RendererUtils.resetRendererState(renderer, accumulateState as never)
    renderer.setRenderTarget(write)
    this.quad.render(renderer)
    RendererUtils.restoreRendererState(renderer, accumulateState)
    this.latest.value = write.texture
    this.targets = [write, read]
    return undefined
  }

  dispose() {
    super.dispose()
    for (const t of this.targets) t.dispose()
    this.material.dispose()
  }
}

/** three's typings leave most display nodes untyped; these all give a colour. */
const vec4Of = (node: Node) => node as Node<'vec4'>

/**
 * A pass drawn without the scene's background, cleared to transparent: the
 * overlay pass, where only the gizmo may cover the picture, and wireframe's
 * picture, whose plain background is laid under it after tone mapping (the
 * WebGL viewport drew wireframe straight to the canvas, where the clear
 * colour is not tone mapped).
 */
class BackgroundlessPass extends PassNode {
  updateBefore(frame: Parameters<PassNode['updateBefore']>[0]) {
    const scene = this.scene as Scene
    const renderer = frame.renderer!
    const background = scene.background
    const alpha = renderer.getClearAlpha()
    scene.background = null
    renderer.setClearAlpha(0)
    try {
      return super.updateBefore(frame)
    } finally {
      scene.background = background
      renderer.setClearAlpha(alpha)
    }
  }
}

type Built = {
  key: string
  pipeline: RenderPipeline
  /** The scene passes with the layers each draws, to compile for (SceneRenderer.compile). */
  passes: { node: PassNode; layers: Layers }[]
  /** The AO node, when the look has one: its resolution follows aoPixelRatio. */
  ao: ReturnType<typeof ao> | null
  aoOn: { value: number } | null
  /** The night look's strength, a uniform of the output node. */
  nightLook: { value: number }
  /** The blur, when the look has it. */
  dof: Dof | null
  /** Wireframe's plain background, laid under the tone-mapped picture. */
  background: { value: Color }
  /** The real-time GI's graph, when this is one. */
  gi: RealtimeGI | null
  dispose(): void
}

export class SceneRenderer {
  private readonly gpu: SceneGPU
  private readonly scene: Scene
  private readonly overlays: Overlays
  /** The viewport's renderer, the one with overlays to draw; an export's has none. */
  private readonly viewport: boolean
  private built: Built | null = null
  private width = -1
  private height = -1
  private pixelRatio = -1
  private aoPixelRatio = -1
  /**
   * The last compile's stand-in, kept until the next compile is done: three
   * frees a pipeline nothing draws with, and the meshes it stood in for take
   * its pipelines only on the frame after the light shows. Each is freed in
   * turn, so a session of light edits does not pile them up.
   */
  private kept: { standIn: StandIn; order: number } | null = null
  private compiles = 0
  private disposed = false
  private stats: FrameStats = { draws: 0, triangles: 0, cpuMs: 0 }
  /**
   * The probes (S2 design「持有者」): the viewport's kept across its pipelines
   * while the proxies they trace are the same layer; a picture's its own.
   */
  private probes: ProbeVolume | null = null
  /** The viewport's probes for more levels, warming in the background while these draw (S2 design「级数」). */
  private growing: ProbeVolume | null = null
  /** Frames this renderer has stepped its probes for: their rays' turn and random numbers come from it alone (P3). */
  private frames = 0

  /** `camera` is the one camera every frame is drawn from; the passes keep it. */
  private readonly camera: Camera
  /** The viewport's: asks for a frame once what compiles ahead of it is ready. */
  private readonly redraw: (() => void) | null

  constructor(gpu: SceneGPU, scene: Scene, camera: Camera, overlays: Overlays = noOverlays, redraw: (() => void) | null = null) {
    this.gpu = gpu
    this.camera = camera
    this.scene = scene
    this.overlays = overlays
    this.viewport = overlays !== noOverlays
    this.redraw = redraw
    // The viewport's pipeline is the one a new light is compiled for (objects/SceneLights.tsx).
    if (this.viewport) gpu.compiler = () => this.compile()
  }

  /**
   * Compiles the scene for this pipeline's passes as they draw it, with the
   * lights it has at the moment of the call (objects/SceneLights.tsx shows a
   * new light only for that moment). Two things in WebGPU (three r186) keep
   * a plain compileAsync from it, and a new light held the viewport for 5
   * seconds while every lit material compiled on the next frame:
   *  - a pipeline belongs to its target's formats: compiling for the canvas
   *    leaves the passes (MSAA, half float, the normals MRT) to compile on
   *    their first frame;
   *  - compileAsync builds the materials later, a few per task, with the
   *    scene's lights as the frames have left them by then.
   * So each pass compiles a stand-in: one copy of each kind of mesh (sharing
   * geometry and material) lit by copies of the lights in a scene of its
   * own. three keeps programs and pipelines by their code and state, so the
   * real meshes find them ready. Before the first frame, the canvas's.
   */
  compile(): Promise<void> {
    const renderer = this.gpu.renderer
    const { scene, camera } = this
    if (!this.built) return renderer.compileAsync(scene, camera)
    const next = standIn(scene)
    const { meshes, lights } = next
    const order = ++this.compiles
    // A pipeline depends on its target's formats and samples, not its size. The
    // pass itself may be resized or rebuilt (the look changing) while this compiles:
    // its targets would be freed under the compile.
    const targets = this.built.passes.map(({ node }) => {
      const t = node.renderTarget.clone()
      t.setSize(1, 1)
      return t
    })
    asItsPass(renderer)
    const entry: Compiling = { meshes: new Set(meshes.children), passes: new Map(this.built.passes.map(({ node }, i) => [node.getMRT(), { node, target: targets[i] }])) }
    compiling.add(entry)
    // compileAsync reads the target before its first await (the MRT and context again later: asItsPass).
    const compiles = this.built.passes.map(({ node, layers: drawn }, i) => {
      const target = renderer.getRenderTarget()
      const targetMRT = renderer.getMRT()
      const mask = camera.layers.mask
      renderer.setRenderTarget(targets[i])
      renderer.setMRT(node.getMRT())
      camera.layers.mask = drawn.mask
      try {
        return renderer.compileAsync(meshes, camera, lights)
      } finally {
        renderer.setRenderTarget(target)
        renderer.setMRT(targetMRT)
        camera.layers.mask = mask
      }
    })
    const freeTargets = () => {
      compiling.delete(entry)
      for (const t of targets) {
        t.dispose()
        // A cloned target's depth is not its own to three, which leaves it out of dispose().
        t.depthTexture?.dispose()
      }
    }
    return Promise.all(compiles).then(
      () => {
        freeTargets()
        // Compiles may finish out of order; the last one asked for is the scene as it is.
        if (this.disposed || (this.kept && this.kept.order > order)) return disposeStandIn(next)
        if (this.kept) disposeStandIn(this.kept.standIn)
        this.kept = { standIn: next, order }
      },
      (error: unknown) => {
        freeTargets()
        disposeStandIn(next)
        throw error
      },
    )
  }

  private build(look: Look): Built {
    const { wireframe, blur } = look
    const { scene, camera, gpu } = this
    const disposables: { dispose(): void }[] = []
    const own = <T extends { dispose(): void }>(n: T) => (disposables.push(n), n)
    const msaa = gpu.compatibility ? 0 : 4
    let passes: Built['passes']
    let picture: Node<'vec4'>
    const background = uniform(new Color())
    let aoNode: ReturnType<typeof ao> | null = null
    let aoOn: { value: number } | null = null
    let dof: Dof | null = null
    let gi: RealtimeGI | null = null
    if (look.gi) {
      // The viewport's helpers, apart from the GI and laid over it (realtimeGI.ts aside); an export has none.
      let aside: PassNode | null = null
      if (this.viewport) {
        aside = own(new BackgroundlessPass(PassNode.COLOR, scene, camera, { samples: 0 }))
        aside.setLayers(layers(ASIDE_LAYER))
        aside.getTexture('output').type = UnsignedByteType
      }
      gi = own(
        realtimeGI(scene, camera, {
          quality: look.quality,
          layers: layers(0),
          pictureLayers: layers(0, SKY_LAYER),
          aside: aside && { color: aside.getTextureNode(), depth: aside.getTextureNode('depth') },
          noise: fixedNoise,
          mainLight: () => mainLight(scene),
          redraw: this.redraw,
          // The reflections' hits lit by the probes as the screen is (S2 design「命中点的间接光」).
          trace: look.trace && hitShading(look.trace.reflect, look.trace.tables, scene, look.world && { probes: look.world.nodes, fill: AMBIENT_FILL }),
          world: look.trace && look.world ? { probes: look.world.nodes, ambient: look.trace.tables.ambient, fill: AMBIENT_FILL, ao: gpuTestHooks.probeAO } : null,
        }),
      )
      passes = aside ? [...gi.passes, { node: aside, layers: layers(ASIDE_LAYER) }] : gi.passes
      aoOn = gi.aoOn
      picture = gi.color
      if (blur) {
        dof = depthOfField(picture, gi.depth)
        disposables.push(dof)
        picture = dof.output
      }
      if (look.accumulate) picture = own(new AccumulateNode(picture, convergeFrames('realtime_gi'))) as unknown as Node<'vec4'>
    } else {
      const beauty = own(wireframe ? new BackgroundlessPass(PassNode.COLOR, scene, camera, { samples: msaa }) : pass(scene, camera, { samples: msaa }))
      beauty.setLayers(layers(0, ASIDE_LAYER, SKY_LAYER))
      passes = [{ node: beauty, layers: layers(0, ASIDE_LAYER, SKY_LAYER) }]
      picture = beauty
      if (!wireframe) {
        // Multisampled depth cannot be sampled by the AO: the prepass has none.
        const prepass = own(pass(scene, camera, { samples: 0 }))
        prepass.transparent = false
        // The surfaces' own normals, as GTAOPass drew them (MeshNormalMaterial over the scene): no
        // maps, and no lights, so a new light compiles only the picture's materials.
        prepass.overrideMaterial = own(new MeshBasicNodeMaterial())
        prepass.setLayers(layers(0))
        prepass.setMRT(mrt({ output: normalView }))
        passes.push({ node: prepass, layers: layers(0) })
        const normal = prepass.getTextureNode()
        const depth = prepass.getTextureNode('depth')
        aoNode = own(ao(depth, normal, camera))
        // GTAOPass's Poisson denoise, as it was set up there.
        const clean = own(denoise(aoNode.getTextureNode(), depth, normal, camera))
        clean.lumaPhi.value = 10
        clean.depthPhi.value = 2
        clean.normalPhi.value = 3
        clean.radius.value = 8
        ;(clean.noiseNode as unknown as { value: DataTexture }).value = own(fixedNoise())
        // Frame.ao = false multiplies by one rather than rebuilding.
        const on = uniform(1)
        aoOn = on
        picture = vec4(beauty.rgb.mul(mix(float(1), vec4Of(clean).r, on)), beauty.a)
        // The aerial perspective after the AO (design「合成顺序」), before the blur; off it is the picture as it was.
        picture = vec4(aerialBehind(picture.rgb, depth, camera), beauty.a)
        // Then the volumes, the air before and behind them apart (Epic revision v9); none drawn, the picture as it was.
        const volumes = own(new VolumeNode(depth, camera, [beauty], this.redraw))
        picture = vec4(applyVolumes(picture.rgb, depth, camera, volumes), beauty.a)
        // AO comes before the blur, so contact shadows out of focus soften with everything else.
        if (blur) {
          dof = depthOfField(picture, depth)
          disposables.push(dof)
          picture = dof.output
        }
      }
    }
    // The night look before the tone mapping (Epic revision v5): at 0 the picture is as it was. Wireframe's lines keep their colours.
    const nightLook = uniform(0)
    if (!wireframe) picture = nightLookNode(picture, nightLook)
    // AgX (W1 scene-display-look): bright saturated colours go towards white
    // without turning into another hue. Wireframe keeps ACES Filmic, so its
    // lines and colours stay exactly what they were.
    let output = vec4Of(renderOutput(picture, wireframe ? ACESFilmicToneMapping : AgXToneMapping, SRGBColorSpace))
    if (wireframe) output = vec4(output.rgb.add(vec4Of(renderOutput(vec4(background, 1), NoToneMapping, SRGBColorSpace)).rgb.mul(output.a.oneMinus())), 1)
    // The real-time GI is antialiased by its TRAA.
    if (msaa === 0 && !gi) output = vec4Of(own(smaa(output)))
    const pipeline = new RenderPipeline(gpu.renderer)
    pipeline.outputColorTransform = false
    pipeline.outputNode = vec4(output.rgb, 1)
    if (this.viewport) {
      const overlay = own(new BackgroundlessPass(PassNode.COLOR, scene, camera, { samples: msaa }))
      overlay.setLayers(layers(OVERLAY_LAYER))
      // Premultiplied sRGB over the finished picture.
      const top = vec4Of(renderOutput(overlay, NoToneMapping, SRGBColorSpace))
      pipeline.outputNode = vec4(output.rgb.mul(top.a.oneMinus()).add(top.rgb), 1)
    }
    return {
      key: keyOf(look),
      dof,
      pipeline,
      passes,
      ao: aoNode,
      aoOn,
      nightLook,
      background,
      gi,
      dispose() {
        pipeline.dispose()
        for (const d of disposables) d.dispose()
      },
    }
  }

  /** The probes as the last render() left them, and whether it drew with them (e2e, gpu.ts probeStats); null without. */
  get probeStats(): (ProbeVolume['stats'] & { drawn: boolean }) | null {
    return this.probes && { ...this.probes.stats, drawn: this.built?.key.includes(`+probes-${this.probes.serial}`) === true }
  }

  /** The probes and the layer the last render() drew with (e2e, checks.ts probeCheck); null without the probes. */
  get world(): { volume: ProbeVolume; handle: TraceHandle } | null {
    const handle = this.trace().handle()
    return this.probes && handle && this.probeStats?.drawn ? { volume: this.probes, handle } : null
  }

  /** What the last render() drew (e2e performance checks, P2's statistics overlay). */
  get lastFrame(): FrameStats {
    return this.stats
  }

  /**
   * The device's world-space layer (render/trace/base.ts): the viewport's
   * commits builds as they come and asks for frames while it does; an
   * export's packs once, in order.
   */
  trace(): TraceBase {
    const base = traceBase(this.gpu, this.scene, { incremental: this.viewport })
    base.redraw = this.redraw
    return base
  }

  /**
   * The probes for the layer the base serves: these, or new ones for another
   * layer (a cold start). Grown past their levels (the scene, or the camera
   * pulled back from it), the viewport's warm ones keep drawing while ones
   * with more levels warm in the background, and give way once those are warm.
   */
  private probesFor(handle: TraceHandle, camera: Camera): ProbeVolume {
    const p = this.probes
    if (!p || p.layer !== handle.probes || (p.outgrown && !(this.viewport && p.warm))) {
      this.dropProbes()
      return (this.probes = this.newProbes(handle, camera))
    }
    if (!p.outgrown) return p
    if (this.growing?.outgrown) this.dropGrowing()
    this.growing ??= this.newProbes(handle, camera)
    if (!this.growing.warm) return p
    p.dispose()
    this.probes = this.growing
    this.growing = null
    return this.probes
  }

  private newProbes(handle: TraceHandle, camera: Camera): ProbeVolume {
    const gpu = this.gpu
    return new ProbeVolume(gpu.renderer, handle, this.scene, this.trace(), camera, this.viewport ? 'viewport' : 'picture', (a) => gpu.release(a))
  }

  private dropGrowing() {
    this.growing?.dispose()
    this.growing = null
  }

  private dropProbes(): null {
    this.dropGrowing()
    this.probes?.dispose()
    this.probes = null
    return null
  }

  /**
   * A picture's warm-up (S2 design「导出预热与时限」): its probes from a cold
   * start, n frames before the first is drawn, each after the frame's own
   * steps (the frame advanced, the exposure, the base in step), no raster.
   * Nothing without the layer.
   */
  warm(camera: Camera, frame: Frame, n: number) {
    const perspective = (camera as PerspectiveCamera).isPerspectiveCamera === true
    if (frame.engine !== 'realtime_gi' || frame.wireframe === true || !perspective) return
    const handle = this.trace().handle()
    if (!handle) return
    const probes = this.probesFor(handle, camera)
    applyExposure(this.scene, frame.preExposure ?? 1)
    try {
      if (this.scene.matrixWorldAutoUpdate) this.scene.updateMatrixWorld()
      for (let i = 0; i < n; i++) {
        this.gpu.advanceFrame()
        this.trace().sync()
        probes.step(this.frames++, camera)
      }
    } finally {
      preExposure.value = 1
    }
  }

  /** Draws a frame into the renderer's canvas. */

  render(camera: Camera, frame: Frame) {
    const started = performance.now()
    if (camera !== this.camera) throw new Error('SceneRenderer 只画构造时的摄像机')
    const renderer = this.gpu.renderer
    const wireframe = frame.wireframe === true
    const perspective = (camera as PerspectiveCamera).isPerspectiveCamera === true
    const dof = !wireframe && perspective ? frame.dof : null
    const gi = frame.engine === 'realtime_gi' && !wireframe && perspective
    const look: Look = {
      wireframe,
      blur: dof !== null,
      gi,
      quality: frame.quality ?? 'interactive',
      accumulate: gi && frame.accumulate === true,
      // As last frame's sync left it: becoming ready asks for the frame that draws with it (base.ts).
      trace: gi ? this.trace().handle() : null,
      world: null,
    }
    // The probes once warm: the viewport warms them in the background first, drawing as before (S2 design「就绪的那一刻」).
    const probes = look.trace ? this.probesFor(look.trace, camera) : this.dropProbes()
    if (probes?.warm) look.world = probes
    const key = keyOf(look)
    if (this.built?.key !== key) {
      this.built?.dispose()
      this.built = this.build(look)
      this.width = -1
    }
    const built = this.built
    const ratio = frame.pixelRatio ?? 1
    const aoRatio = Math.min(frame.aoPixelRatio ?? ratio, ratio)
    // R3F sets the renderer's pixel ratio on resizes of its own: it is checked as well as remembered.
    if (ratio !== this.pixelRatio || renderer.getPixelRatio() !== ratio || aoRatio !== this.aoPixelRatio || frame.width !== this.width || frame.height !== this.height) {
      this.pixelRatio = ratio
      this.aoPixelRatio = aoRatio
      this.width = frame.width
      this.height = frame.height
      renderer.setPixelRatio(ratio)
      renderer.setSize(frame.width, frame.height, false)
      if (built.ao) built.ao.resolutionScale = aoRatio / ratio
      built.dof?.setSize(Math.round(frame.width * ratio), Math.round(frame.height * ratio))
    }
    if (dof && built.dof) {
      const persp = camera as PerspectiveCamera
      built.dof.setLens(dof, persp.near, persp.far, frame.pictureLongSide ?? Math.max(frame.width, frame.height) * ratio)
    }
    if (built.aoOn) built.aoOn.value = frame.ao === false ? 0 : 1
    built.nightLook.value = frame.nightLook ?? 0
    const bg = this.scene.background
    built.background.value.copy(bg instanceof Color ? bg : renderer.getClearColor(new Color()))
    // Clay's studio light comes from the view's upper left (render/studio.ts).
    aimStudio(this.scene, camera)
    for (const o of this.overlays.aside()) o?.traverse((c) => c.layers.set(ASIDE_LAYER))
    for (const o of this.overlays.top()) o?.traverse((c) => c.layers.set(OVERLAY_LAYER))
    // The output node reads the exposure from the renderer as it draws.
    const exposure = renderer.toneMappingExposure
    renderer.toneMappingExposure = frame.exposure ?? 1
    // Written and drawn in one synchronous call: no await between (world/exposure.ts preExposure).
    applyExposure(this.scene, frame.preExposure ?? 1)
    // The layer in step with the scene, its light rows as exposed for this frame (S1 design「主流程」), its meshes
    // where this frame draws them: three brings matrixWorld up to date only as it renders.
    if (gi) {
      if (this.scene.matrixWorldAutoUpdate) this.scene.updateMatrixWorld()
      this.trace().sync()
      if (probes) {
        // A cold start's levels every frame till they are warm, the viewport asking for frames meanwhile.
        const warming = !probes.warm
        const frame = this.frames++
        probes.step(frame, camera, warming)
        this.growing?.step(frame, camera, true)
        if (warming || this.growing) this.redraw?.()
      }
    }
    prepareAerial(this.scene, renderer, camera, !wireframe)
    prepareVolumes(this.scene, camera, !wireframe)
    try {
      this.stats = measure(renderer.info.render, () => built.pipeline.render(), started)
      if (look.trace) {
        // Drawn: the layer's buffers are on the GPU whole.
        this.trace().drawn()
        // The reflections' pipeline refused on this device (known a GPU round trip after it was built): upstream's
        // SSR from the next frame on (S1 design A13).
        if (built.gi?.traced && this.gpu.pipelineFailed(built.gi.traced)) this.trace().fail(true)
      }
      // The probes' kernels refused on this device: likewise, the layer off for good (S2 design「能力与显存」).
      if (probes && this.gpu.computeFailed()) this.trace().fail(true)
    } finally {
      renderer.toneMappingExposure = exposure
      preExposure.value = 1
      finishAerial()
      finishVolumes()
    }
  }

  dispose() {
    this.disposed = true
    this.dropProbes()
    if (this.kept) disposeStandIn(this.kept.standIn)
    this.kept = null
    this.built?.dispose()
    this.built = null
    if (this.viewport) this.gpu.compiler = null
  }
}
