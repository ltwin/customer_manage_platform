import type { Material, Object3D, RenderTarget } from 'three'
import { velocity } from 'three/tsl'
import { defaultBuildStages, FunctionNode, shaderStages, ViewportTextureNode, type Node, type WebGPURenderer } from 'three/webgpu'
import { pageYields, yieldToPage } from './gpu.ts'
import { buildLifetime } from './buildLifetime.ts'

// Drawing with what is built and building the rest in the background
// (scene-gi-stalls, the viewport's pipeline switches): an object whose
// material has no shader yet is not drawn, and its shader is built a stage
// at a time, the page let go between, as three's own buildAsync does; then
// its GPU pipeline is made with createRenderPipelineAsync, as compileAsync
// makes it, and it is drawn once that is ready. three builds the shader in
// the frame that first draws it, which held the page 0.3 s for the
// reflections' shader when the world-space layer or the probes came on, and
// makes the pipeline there too, which held the GPU process (no frame shown)
// some 1 s for a new light's thirty lit materials.
//
// buildAsync reads the renderer's target, MRT and context as each stage runs
// (NodeBuilder.getOutputType, MRTNode.setup), a task or more after the draw
// that asked for it, the scene's lights as the last render of that scene
// left them (LightsNode._lights: the overlay pass's camera sees none), and
// the velocity's projection as TRAA leaves it outside the pipeline's render
// (none: the velocity took the jitter, and TRAA blurred); each stage here
// runs with those the draw had. The
// result goes where three's own build puts it (NodeManager.nodeBuilderCache,
// by the render object's cache key), and the next draw takes it from there.
// three r186's internals (Renderer._renderObjectDirect, _objects, _nodes):
// to be checked on every three upgrade, as sceneRenderer.ts asItsPass is.

type LightsNode = { getLights(): unknown[]; setLights(lights: unknown[]): unknown }
type RenderObject = { material: Material; object: Object3D; lightsNode: LightsNode | null; onDispose(): void }
type BuilderState = { usedTimes: number }
type NodeBuilder = {
  context: { position?: { isNode?: boolean } }
  currentFunctionNode: unknown
  buildFunctionNode(shaderNode: unknown): unknown
  buildFunctionCode(shaderNode: unknown): string
  flowNodes: Record<string, Node[]>
  prebuild(): void
  setBuildStage(stage: string | null): void
  setShaderStage(stage: string | null): void
  flowNodeFromShaderStage(stage: string, node: unknown): void
  flowNode(node: Node): void
  buildCode(): void
  buildUpdateNodes(): void
}
type Internals = {
  backend: { get(pipeline: object): { error?: boolean } }
  _objects: { get(...args: unknown[]): RenderObject }
  _geometries: { updateForRender(renderObject: RenderObject): void }
  _bindings: { getForRender(renderObject: RenderObject): unknown }
  _pipelines: { getForRender(renderObject: RenderObject, promises: Promise<unknown>[]): object; isReady(renderObject: RenderObject): boolean }
  _nodes: {
    get(renderObject: RenderObject): { nodeBuilderState?: unknown }
    getForRender(renderObject: RenderObject): { updateBeforeNodes: Node[] }
    getNodeFrameForRender(renderObject: RenderObject): { updateBeforeNode(node: Node): void }
    getForRenderCacheKey(renderObject: RenderObject): string
    nodeBuilderCache: Map<string, BuilderState>
    _createNodeBuilder(renderObject: RenderObject, material: Material): NodeBuilder
    _createNodeBuilderState(builder: NodeBuilder): BuilderState
  }
  _currentRenderContext: unknown
  _renderObjectDirect(...args: unknown[]): void
}

/** The draws being deferred now: those left out for want of a shader, and whether the built ones are left out too (dry). */
let deferring: { renderer: WebGPURenderer; left: number; dry: boolean } | null = null
/** Cache keys and failed builds belong to one renderer, just like three's nodeBuilderCache. */
const states = new WeakMap<object, { building: Set<string>; failed: Set<string>; piping: WeakSet<object>; unpiped: WeakSet<object> }>()
function stateFor(renderer: object) {
  let state = states.get(renderer)
  if (!state) states.set(renderer, (state = { building: new Set(), failed: new Set(), piping: new WeakSet(), unpiped: new WeakSet() }))
  return state
}
const wrapped = new WeakSet<object>()

/** A disposed render object never receives a late shader, even though material.dispose does not change version. */
const owners = new WeakMap<RenderObject, { alive: boolean; unused: Map<string, BuilderState> }>()
function ownerOf(r: WebGPURenderer & Internals, object: RenderObject) {
  let owner = owners.get(object)
  if (owner) return owner
  owner = { alive: true, unused: new Map() }
  owners.set(object, owner)
  const held = owner
  const dispose = object.onDispose
  object.onDispose = () => {
    held.alive = false
    for (const [key, state] of held.unused) {
      if (state.usedTimes === 0 && r._nodes.nodeBuilderCache.get(key) === state) r._nodes.nodeBuilderCache.delete(key)
    }
    held.unused.clear()
    dispose.call(object)
  }
  return owner
}

/** What a draw had set: a build's every stage runs with it. */
type DrawState = { target: RenderTarget | null; face: number; level: number; mrt: unknown; context: WebGPURenderer['contextNode']; renderContext: unknown; lights: unknown[] | null; projection: unknown }

/** The velocity TRAA gives the unjittered projection for the length of the pipeline's render (VelocityNode.setup reads it). */
const motion = velocity as unknown as { projectionMatrix: unknown }

function stateOf(r: WebGPURenderer & Internals, lightsNode: LightsNode | null): DrawState {
  // A copy: the render list's array is filled again by the next render.
  return { target: r.getRenderTarget(), face: r.getActiveCubeFace(), level: r.getActiveMipmapLevel(), mrt: r.getMRT(), context: r.contextNode, renderContext: r._currentRenderContext, lights: lightsNode ? [...lightsNode.getLights()] : null, projection: motion.projectionMatrix }
}

function within<T>(r: WebGPURenderer & Internals, lightsNode: LightsNode | null, s: DrawState, fn: () => T): T {
  const was = stateOf(r, lightsNode)
  const lights = lightsNode?.getLights()
  r.setRenderTarget(s.target, s.face, s.level)
  r.setMRT(s.mrt as never)
  r.contextNode = s.context
  // NodeBuilder caches binding groups by this context. Building between frames without it puts unrelated passes
  // in the renderer-wide fallback cache; retiring one pipeline can then free buffers another pipeline still binds.
  r._currentRenderContext = s.renderContext
  if (lightsNode && s.lights) lightsNode.setLights(s.lights)
  motion.projectionMatrix = s.projection
  try {
    return fn()
  } finally {
    r.setRenderTarget(was.target, was.face, was.level)
    r.setMRT(was.mrt as never)
    r.contextNode = was.context
    r._currentRenderContext = was.renderContext
    if (lightsNode && lights) lightsNode.setLights(lights)
    motion.projectionMatrix = was.projection
  }
}

/**
 * NodeBuilder.build, a stage at a time (three r186's buildAsync, which yields between the same stages), each as the
 * draw had it. Not if the material has changed since the draw (a node set it up again): the next draw asks for the
 * shader it has now.
 */
async function buildInStages(r: WebGPURenderer & Internals, renderObject: RenderObject, key: string, s: DrawState, valid: () => boolean) {
  const nodes = r._nodes
  const { lightsNode } = renderObject
  if (!valid()) return
  const builder = within(r, lightsNode, s, () => {
    const b = nodes._createNodeBuilder(renderObject, renderObject.material)
    // A function with a layout three keeps per backend, the uniforms it reads named as the builder that first built
    // it named them; one a node reads its uniforms in (RecurrentDenoise's), built a second time, read uniforms its
    // shader does not declare. Built once for this builder, its uniforms are this builder's.
    const functions = new Map<unknown, unknown>()
    b.buildFunctionNode = function (this: NodeBuilder, shaderNode: unknown) {
      let fn = functions.get(shaderNode)
      if (!fn) {
        const made = new FunctionNode()
        functions.set(shaderNode, made)
        const previous = this.currentFunctionNode
        this.currentFunctionNode = made
        ;(made as unknown as { code: string }).code = this.buildFunctionCode(shaderNode)
        this.currentFunctionNode = previous
        fn = made
      }
      return fn
    }
    b.prebuild()
    return b
  })
  for (const buildStage of defaultBuildStages) {
    for (const shaderStage of shaderStages) {
      if (builder.flowNodes[shaderStage].length > 0) await yieldToPage()
      if (!valid()) return
      within(r, lightsNode, s, () => {
        builder.setBuildStage(buildStage)
        if (shaderStage === shaderStages[0] && builder.context.position?.isNode) builder.flowNodeFromShaderStage('vertex', builder.context.position)
        builder.setShaderStage(shaderStage)
        for (const node of builder.flowNodes[shaderStage]) {
          if (buildStage === 'generate') builder.flowNode(node)
          else node.build(builder as never)
        }
      })
    }
  }
  await yieldToPage()
  if (!valid()) return
  within(r, lightsNode, s, () => {
    builder.setBuildStage(null)
    builder.setShaderStage(null)
    builder.buildCode()
    builder.buildUpdateNodes()
  })
  // Used by the draws that take it (NodeManager.getForRender counts them).
  if (valid() && !nodes.nodeBuilderCache.has(key)) {
    const state = within(r, lightsNode, s, () => nodes._createNodeBuilderState(builder))
    nodes.nodeBuilderCache.set(key, state)
    ownerOf(r, renderObject).unused.set(key, state)
  }
}

/**
 * The builds, one after another: two of three's builders a stage apart from each other gave a shader that reads a
 * uniform it does not declare (nodes keep build state of their own across builders).
 */
let queue: Promise<unknown> = Promise.resolve()

/** Whether a render object's shader is built; if not, its build is started. A build that fails is left to the draw, which fails as before. */
function built(r: WebGPURenderer & Internals, renderObject: RenderObject): boolean {
  const { building, failed } = stateFor(r)
  const nodes = r._nodes
  if (nodes.get(renderObject).nodeBuilderState !== undefined) return true
  const key = nodes.getForRenderCacheKey(renderObject)
  if (nodes.nodeBuilderCache.has(key) || failed.has(key)) return true
  if (building.has(key)) return false
  building.add(key)
  const s = stateOf(r, renderObject.lightsNode)
  const version = renderObject.material.version
  const owner = ownerOf(r, renderObject)
  const lifetime = buildLifetime(r)
  let targetAlive = true
  const lostTarget = () => { targetAlive = false }
  s.target?.addEventListener('dispose', lostTarget)
  const valid = () => owner.alive && targetAlive && !lifetime.closed && renderObject.material.version === version
  const build: Promise<unknown> = queue
    .then(() => buildInStages(r, renderObject, key, s, valid))
    .catch((error: unknown) => {
      if (valid()) {
        failed.add(key)
        console.warn('Scene shader prebuild failed; the next draw will retry', error)
      }
    })
    .finally(() => {
      s.target?.removeEventListener('dispose', lostTarget)
      building.delete(key)
    })
  queue = build
  lifetime.track(build)
  return false
}

/**
 * Whether a render object's pipeline is ready; if not, it is made in the background (Renderer._createObjectPipeline's
 * steps but the node updates: the geometry, the bind groups, the pipeline with promises). One that fails is left to
 * the draw, which fails as before.
 */
function piped(r: WebGPURenderer & Internals, renderObject: RenderObject): boolean {
  const { piping, unpiped } = stateFor(r)
  if (unpiped.has(renderObject) || r._pipelines.isReady(renderObject)) return true
  if (piping.has(renderObject)) return false
  piping.add(renderObject)
  const promises: Promise<unknown>[] = []
  let pipeline: object
  try {
    // Glass must copy its current target before binding the framebuffer texture. Otherwise the bindings allocate
    // it with the canvas format (BGRA8), and its first copy from the lighting pass (RGBA16F) is invalid. Other
    // update-before nodes render passes or temporal history; those still run only when the picture is drawn.
    for (const node of r._nodes.getForRender(renderObject).updateBeforeNodes) {
      if (node instanceof ViewportTextureNode) r._nodes.getNodeFrameForRender(renderObject).updateBeforeNode(node)
    }
    r._geometries.updateForRender(renderObject)
    r._bindings.getForRender(renderObject)
    pipeline = r._pipelines.getForRender(renderObject, promises)
  } catch {
    unpiped.add(renderObject)
    return true
  }
  const made: Promise<unknown> = Promise.all(promises)
    .then(() => {
      // three resolves even a refused WebGPU pipeline; only backend data records the failure. Stop waiting so
      // the normal draw/failure checks can take over (including the world-space SSR's screen-space fallback).
      if (r.backend.get(pipeline).error) unpiped.add(renderObject)
    })
    .catch(() => void unpiped.add(renderObject))
    .finally(() => {
      piping.delete(renderObject)
    })
  buildLifetime(r).track(made)
  return r._pipelines.isReady(renderObject)
}

function install(r: WebGPURenderer & Internals) {
  if (wrapped.has(r)) return
  wrapped.add(r)
  const direct = r._renderObjectDirect
  r._renderObjectDirect = function (this: WebGPURenderer & Internals, ...args: unknown[]) {
    if (deferring?.renderer === this) {
      const [object, material, scene, camera, lightsNode, , clippingContext, passId] = args
      const renderObject = this._objects.get(object, material, scene, camera, lightsNode, this._currentRenderContext, clippingContext, passId)
      if (!built(this, renderObject) || !piped(this, renderObject)) {
        deferring.left++
        return
      }
      if (deferring.dry) return
    }
    direct.apply(this, args)
  }
}

/**
 * Runs fn with projection as the velocity's (TRAANode._originalProjectionMatrix while the pipeline renders, none
 * before TRAA has set up: three r186): what a pass drawn alone builds for the velocity is then what those frames build.
 */
export function withVelocity<T>(projection: unknown, fn: () => T): T {
  const was = motion.projectionMatrix
  motion.projectionMatrix = projection ?? null
  try {
    return fn()
  } finally {
    motion.projectionMatrix = was
  }
}

/** Once the builds started so far are done, failed or not. */
export const builds = (renderer: WebGPURenderer): Promise<void> => buildLifetime(renderer).settled()

/**
 * Draws with what is built, building the rest in the background: true once
 * a draw left nothing out (and none of its shaders is being built). Its
 * nodes that draw between frames (temporal history) draw only once what they
 * draw from is built. dry: nothing is drawn, the builds alone started (no
 * uniform, shadow map or history is touched). Where the page cannot be let
 * go (pageYields), draws as three does, building what it lacks there and
 * then.
 */
export function drawBuilt(renderer: WebGPURenderer, draw: () => void, dry = false): boolean {
  if (!pageYields) {
    if (!dry) draw()
    return true
  }
  const r = renderer as WebGPURenderer & Internals
  if (buildLifetime(r).closed) return false
  install(r)
  const outer = deferring
  const current = { renderer, left: 0, dry }
  deferring = current
  try {
    draw()
  } finally {
    deferring = outer
  }
  return current.left === 0
}
