import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { AmbientLight, BoxGeometry, DirectionalLight, Mesh, PerspectiveCamera, RenderTarget, Scene } from 'three'
import { mrt, output } from 'three/tsl'
import { MeshPhysicalNodeMaterial, MeshStandardNodeMaterial, PhysicalLightingModel, WebGPURenderer, type NodeMaterial } from 'three/webgpu'
import { giAmbient, giBase, giEnvSpec, installGISplit, splitContext } from '../src/creative-scene/render/giSplit.ts'
import { composite } from '../src/creative-scene/render/realtimeGI.ts'

// The real-time GI (R2 tasks 1.1): the SSGI fork against three's node, the
// per-pass indirect split against three's lighting model (render/giSplit.ts
// reaches into it), and the composite's algebra. Shaders are generated here,
// in node, by the WebGPU backend's node builder; nothing is drawn.

const read = (p: string) => readFileSync(new URL(p, import.meta.url), 'utf8')

/** Puts three's lines back: each "fork: <why>" block gives way to the lines after "fork: was", uncommented. */
export function unfork(text: string): string {
  return text
    .slice(text.indexOf('\n\n') + 2)
    .replace(/^(\t*)\/\/ fork: (?!was$|end$)[^\n]*\n[\s\S]*?^\1\/\/ fork: was\n([\s\S]*?)\n\1\/\/ fork: end/gm, (_, _indent: string, was: string) => was.replace(/^\/\//gm, ''))
}

test('the SSGI is three’s SSGINode but for its fork: blocks', () => {
  const upstream = read('../node_modules/three/examples/jsm/tsl/display/SSGINode.js')
  const fork = read('../src/creative-scene/render/nodes/SSGINode.js')
  // A marker sharing its line with code would comment that code out in the fork and still restore cleanly.
  for (const line of fork.split('\n').filter((l) => /^\t*\/\/ fork: (was|end)/.test(l))) assert.match(line, /^\t*\/\/ fork: (was|end)$/)
  const restored = unfork(fork)
  assert.ok(!restored.includes('fork:'), 'every change is undone here')
  assert.equal(restored, upstream, 'three changed SSGINode: compare and port the fork')
})

test('the reflections’ retrace is three’s SSRNode but for its fork: blocks (S1 design「反射补追」)', () => {
  const upstream = read('../node_modules/three/examples/jsm/tsl/display/SSRNode.js')
  const fork = read('../src/creative-scene/render/nodes/SSRTraceNode.js')
  for (const line of fork.split('\n').filter((l) => /^\t*\/\/ fork: (was|end)/.test(l))) assert.match(line, /^\t*\/\/ fork: (was|end)$/)
  const restored = unfork(fork)
  assert.ok(!restored.includes('fork:'), 'every change is undone here')
  assert.equal(restored, upstream, 'three changed SSRNode: compare and port the fork')
})

test('unfork puts back what a block replaced, an empty replacement and several lines alike', () => {
  const fork = '// header\n\na\n\t// fork: why\n\tb\n\t// fork: was\n//\tc\n//\n//\td\n\t// fork: end\ne\n\t// fork: gone\n\n\t// fork: was\n//\tf\n\t// fork: end\ng\n'
  assert.equal(unfork(fork), 'a\n\tc\n\n\td\ne\n\tf\ng\n')
})

// --- the indirect split ---------------------------------------------------------

test('three’s lighting model adds its indirect light where giSplit reads it', () => {
  const model = read('../node_modules/three/src/nodes/functions/PhysicalLightingModel.js')
  assert.match(model, /\tindirect\( builder \) \{\n\n\t\tthis\.indirectDiffuse\( builder \);\n\t\tthis\.indirectSpecular\( builder \);\n\t\tthis\.ambientOcclusion\( builder \);\n\n\t\}/)
  assert.match(model, /reflectedLight\.indirectDiffuse\.addAssign\(/)
  assert.match(model, /reflectedLight\.indirectSpecular\.addAssign\(/)
  // The pass's context reaches the material's build: the mark is read from builder.context.
  assert.match(read('../node_modules/three/src/nodes/display/PassNode.js'), /renderer\.contextNode = this\._contextNodeCache\.context;/)
  assert.match(read('../node_modules/three/src/nodes/core/NodeBuilder.js'), /this\.context = \{ \.\.\.this\.context, \.\.\.renderer\.contextNode\.getFlowContextData\(\) \};/)
  // An MRT output takes the material's blending only when asked; the rest are written as they are.
  assert.match(read('../node_modules/three/src/nodes/core/MRTNode.js'), /return this\.blendModes\[ name \] \|\| _noBlending;/)
})

test('c is what three’s indirectDiffuse multiplies the Lambert term by: the multiple scattering, the sheen and the AO (S2 design「合成」)', () => {
  const model = read('../node_modules/three/src/nodes/functions/PhysicalLightingModel.js')
  // The arguments diffuseShare calls computeMultiscattering with, and what indirectDiffuse makes of it.
  assert.match(model, /\tindirectDiffuse\( builder \) \{[^]*?this\.computeMultiscattering\( singleScattering, multiScattering, specularF90, specularColor, this\.iridescenceF0Dielectric \);\n\n\t\tconst diffuse = irradiance\.mul\( BRDF_Lambert\( \{ diffuseColor: diffuseContribution \} \) \)\.mul\( singleScattering\.add\( multiScattering \)\.oneMinus\(\) \)\.toVar\(\);/)
  assert.match(model, /const sheenAlbedo = IBLSheenBRDF\( \{ normal: normalView, viewDir: positionViewDirection, roughness: sheenRoughness \} \);[^]*?const sheenEnergyComp = sheen\.r\.max\( sheen\.g \)\.max\( sheen\.b \)\.mul\( sheenAlbedo \)\.oneMinus\(\);\n\n\t\t\tdiffuse\.mulAssign\( sheenEnergyComp \);/)
  assert.match(model, /\tambientOcclusion\( builder \) \{[^]*?reflectedLight\.indirectDiffuse\.mulAssign\( ambientOcclusion \);/)
  // giSplit's copy of IBLSheenBRDF, which three does not export: the same constants in the same places.
  const constants = (text: string) => text.match(/-?\s*\d+\.\d+/g)?.map((n) => Number(n.replace(/\s/g, '')))
  const theirs = model.slice(model.indexOf('const IBLSheenBRDF'), model.indexOf('} );', model.indexOf('const IBLSheenBRDF')))
  const giSplit = read('../src/creative-scene/render/giSplit.ts')
  const ours = giSplit.slice(giSplit.indexOf('const sheenAlbedo'), giSplit.indexOf('\n})', giSplit.indexOf('const sheenAlbedo')))
  assert.deepEqual(constants(ours), constants(theirs))
})

const renderer = new WebGPURenderer({ canvas: { style: {} } as never })
// Never initialised (no GPU here): the lighting asks about features, which a device would answer.
Object.assign(renderer, { hasFeature: () => false })
const scene = new Scene()
const lights = [new AmbientLight(0xffffff, 0.5), new DirectionalLight(0xffffff, 2)]
scene.add(...lights)
const camera = new PerspectiveCamera()
// The renderer hands the lights node the scene's lights as it draws; here, once.
const lightsNode = renderer.lighting.getNode(scene)
lightsNode.setLights(lights)

/** The fragment shader the WebGPU backend builds for a mesh of this material, as the renderer stands. */
function fragment(material: NodeMaterial): string {
  const mesh = new Mesh(new BoxGeometry(), material)
  const builder = (renderer.backend as unknown as { createNodeBuilder(o: Mesh, r: WebGPURenderer): { scene: Scene; camera: PerspectiveCamera; material: NodeMaterial; lightsNode: unknown; build(): void; fragmentShader: string } }).createNodeBuilder(mesh, renderer)
  builder.scene = scene
  builder.camera = camera
  builder.material = material
  builder.lightsNode = lightsNode
  builder.build()
  return builder.fragmentShader
}

/** Builds with the lighting pass's context and outputs, then puts the renderer back. */
function inLightingPass<T>(build: () => T): T {
  const target = new RenderTarget(1, 1, { count: 4 })
  for (const [i, name] of ['output', 'base', 'ambient', 'envSpec'].entries()) target.textures[i].name = name
  const split = mrt({ output, base: giBase, ambient: giAmbient, envSpec: giEnvSpec })
  const r = renderer as unknown as { getRenderTarget(): unknown; getMRT(): unknown }
  const { getRenderTarget, getMRT } = r
  const context = renderer.contextNode
  r.getRenderTarget = () => target
  r.getMRT = () => split
  renderer.contextNode = splitContext(context)
  try {
    return build()
  } finally {
    r.getRenderTarget = getRenderTarget
    r.getMRT = getMRT
    renderer.contextNode = context
  }
}

test('without the mark a material compiles to what it did before giSplit; with it, the indirect light is copied out', () => {
  const standard = () => new MeshStandardNodeMaterial({ color: 0xc08040, roughness: 0.4, metalness: 0.2 })
  const physical = () => new MeshPhysicalNodeMaterial({ color: 0x4080c0, clearcoat: 1 })
  const before = [fragment(standard()), fragment(physical())]
  installGISplit()
  installGISplit()
  assert.equal((PhysicalLightingModel.prototype as { giSplit?: true }).giSplit, true, 'wrapped once')
  assert.match(before[0], /indirect/i, 'the shaders compared do have indirect light')
  assert.deepEqual([fragment(standard()), fragment(physical())], before, 'the workbench, thumbnails and previews compile exactly as in R1')

  const split = inLightingPass(() => fragment(standard()))
  for (const name of ['GIAmbient', 'GIEnvSpec', 'GIBase']) {
    // Assigned once, from the lighting, never the placeholder; then written to its MRT output.
    assert.equal(split.match(new RegExp(`\\b${name} = `, 'g'))?.length, 1, name)
    assert.doesNotMatch(split, new RegExp(`\\b${name} = vec4<f32>\\( 0\\.0, 0\\.0, 0\\.0, 0\\.0 \\)`), name)
    assert.match(split, new RegExp(`output\\.m\\d = ${name};`), name)
  }
  // The indirect diffuse light's alpha is c, at least 1e-3, no longer 1 (S2 design「合成」).
  // The WGSL names every step: follow GIAmbient back to its alpha.
  const assigned = (name: string) => split.match(new RegExp(`\\b${name} = (.*);`))?.[1] ?? ''
  const ambient = assigned(assigned('GIAmbient'))
  const alpha = ambient.match(/^vec4<f32>\( indirectDiffuse, (\w+) \)$/)?.[1]
  assert.ok(alpha, ambient)
  assert.match(assigned(alpha), /^max\( \w+, 0\.001 \)$/)
  // Sheen adds its energy compensation, which a material without sheen leaves out.
  const sheen = inLightingPass(() => fragment(new MeshPhysicalNodeMaterial({ sheen: 1, sheenRoughness: 0.5 })))
  assert.ok(sheen.includes('1.9362') && !split.includes('1.9362'), 'the sheen’s albedo in c, only with sheen')
})

test('a transparent or transmissive material is not taken apart: its outputs are zero, alpha 0', () => {
  installGISplit()
  for (const material of [new MeshStandardNodeMaterial({ transparent: true, opacity: 0.4 }), new MeshPhysicalNodeMaterial({ transmission: 1 })]) {
    const shader = inLightingPass(() => fragment(material))
    for (const name of ['GIAmbient', 'GIEnvSpec', 'GIBase']) {
      assert.equal(shader.match(new RegExp(`\\b${name} = `, 'g'))?.length, 1, `${name}: only the placeholder`)
      assert.match(shader, new RegExp(`\\b${name} = vec4<f32>\\( 0\\.0, 0\\.0, 0\\.0, 0\\.0 \\)`), name)
    }
  }
})

// --- the composite ----------------------------------------------------------------

/** Plain-number stand-ins for the nodes the composite takes: a vec4 or a scalar that broadcasts. */
class V {
  readonly v: number[]
  constructor(...v: number[]) {
    this.v = v
  }
  get rgb() {
    return new V(...this.v.slice(0, 3))
  }
  get a() {
    return new V(this.v[3])
  }
  private zip(o: V | number, f: (a: number, b: number) => number) {
    const b = typeof o === 'number' ? [o] : o.v
    const n = Math.max(this.v.length, b.length)
    return new V(...Array.from({ length: n }, (_, i) => f(this.v[this.v.length === 1 ? 0 : i], b[b.length === 1 ? 0 : i])))
  }
  add(o: V | number) {
    return this.zip(o, (a, b) => a + b)
  }
  sub(o: V | number) {
    return this.zip(o, (a, b) => a - b)
  }
  mul(o: V | number) {
    return this.zip(o, (a, b) => a * b)
  }
  oneMinus() {
    return new V(...this.v.map((a) => 1 - a))
  }
}

const near = (a: V, b: number[]) => a.v.every((x, i) => Math.abs(x - b[i]) < 1e-9) || assert.fail(`${a.v} ≠ ${b}`)

test('the composite: AO on the indirect light only, reflections in place of the environment’s, GI by the diffuse colour', () => {
  const output = new V(0.9, 0.7, 0.5, 1)
  const ambient = new V(0.2, 0.2, 0.2, 1)
  const envSpec = new V(0.1, 0.1, 0.1, 1)
  const base = new V(0.8, 0.4, 0.2, 0.25)
  const neutral = { output, base, ambient, envSpec, gi: new V(0, 0, 0), ao: new V(1), contact: new V(1), reflection: new V(0, 0, 0), cover: new V(0), split: new V(1) }
  near(composite(neutral), [0.9, 0.7, 0.5])
  // AO 0.5: half the ambient and environment reflection gone, the direct light kept.
  near(composite({ ...neutral, ao: new V(0.5) }), [0.75, 0.55, 0.35])
  // GI × base colour × (1 − metalness).
  near(composite({ ...neutral, gi: new V(1, 1, 1) }), [0.9 + 0.6, 0.7 + 0.3, 0.5 + 0.15])
  near(composite({ ...neutral, base: new V(0.8, 0.4, 0.2, 1), gi: new V(1, 1, 1) }), [0.9, 0.7, 0.5])
  // A hit reflection takes the environment's place, not added to it.
  near(composite({ ...neutral, cover: new V(1), reflection: new V(0.3, 0.3, 0.3) }), [1.1, 0.9, 0.7])
  // Contact shadow darkens the direct light only: direct = output − ambient − envSpec.
  near(composite({ ...neutral, contact: new V(0) }), [0.3, 0.3, 0.3])
  // Not taken apart (glass, the background): the picture as it is, whatever the effects say.
  const zero = new V(0, 0, 0, 0)
  near(composite({ ...neutral, ambient: zero, envSpec: zero, base: zero, ao: new V(0.2), contact: new V(0), reflection: new V(0.5, 0.5, 0.5), cover: new V(1), split: new V(0) }), [0.9, 0.7, 0.5])
})

test('the lighting pass’s context is one node per renderer context, so pictures share their render objects', async () => {
  const { context } = await import('three/tsl')
  const base = context()
  const a = splitContext(base)
  assert.equal(splitContext(base), a, 'the same node for the next picture')
  assert.equal(a.getFlowContextData().giSplit, true)
  base.value = { ...base.value, other: 1 }
  base.version++
  assert.notEqual(splitContext(base), a, 'made again when the renderer’s context changes')
  assert.equal(splitContext(base).getFlowContextData().other, 1)
  assert.notEqual(splitContext(context()), a, 'one per renderer context')
})

// --- contact shadows and freeing -------------------------------------------------

test('the main light: the sun while up, else the shadow-casting light that lights most; never a point light', async () => {
  const { Group, PointLight, SpotLight } = await import('three')
  const { mainLight } = await import('../src/creative-scene/render/sceneRenderer.ts')
  const sun = Object.assign(new DirectionalLight(0xffffff, 2), { userData: { sun: true } })
  const fill = Object.assign(new DirectionalLight(0xffffff, 1), { castShadow: true })
  const spot = Object.assign(new SpotLight(0xffffff, 150), { castShadow: true })
  spot.position.set(0, 0, 3) // 150 cd at 3 m: about 16.7 on its target
  const lamp = Object.assign(new PointLight(0xffffff, 1000), { castShadow: true })
  const scene = new Group()
  scene.add(sun, fill, spot, lamp)
  assert.equal(mainLight(scene), sun)
  sun.intensity = 0
  assert.equal(mainLight(scene), spot, 'the sun below the horizon does not count; the spot lights more than the fill')
  spot.position.set(0, 0, 20)
  assert.equal(mainLight(scene), fill, 'far away the spot lights less')
  spot.position.set(0, 0, 3)
  const rig = new Group()
  rig.position.set(0, 0, 17)
  rig.add(spot)
  scene.add(rig)
  assert.equal(mainLight(scene), fill, 'in a group the spot is as far as the world has it, its target still at the origin')
  fill.castShadow = false
  spot.castShadow = false
  assert.equal(mainLight(scene), null, 'lights without shadows and point lights give none')
})

test('TRAA keeps its first previous depth where realtimeGI frees it', () => {
  const traa = read('../node_modules/three/examples/jsm/tsl/display/TRAANode.js')
  assert.match(traa, /this\._previousDepthNode = texture\( new DepthTexture\( 1, 1 \) \);/)
  assert.match(traa, /this\._previousDepthNode\.value = this\._historyRenderTarget\.depthTexture;/)
  assert.doesNotMatch(traa.slice(traa.indexOf('\tdispose() {')), /_previousDepthNode/, 'three frees it now: drop realtimeGI’s own')
  // A display node converts what is not a texture itself, and does not free it.
  assert.match(read('../node_modules/three/examples/jsm/tsl/display/TRAANode.js'), /new TRAANode\( convertToTexture\( beautyNode \)/)
  // Render state is per MRT node: one node for every pipeline.
  assert.match(read('../node_modules/three/src/renderers/common/RenderContexts.js'), /const mrtState = \( mrt !== null \) \? mrt\.id : 'default';/)
})

// --- the renderer in effect ---------------------------------------------------------

test('the renderer in effect: the scene’s for the rendered view and lit in perspective, the workbench for the rest', async () => {
  const { convergeFrames, effectiveEngine } = await import('../src/creative-scene/render/engines.ts')
  for (const look of ['rendered', 'lit'] as const) {
    assert.equal(effectiveEngine('realtime_gi', look, true), 'realtime_gi', look)
    assert.equal(effectiveEngine('realtime_gi', look, false), 'workbench', `${look}, orthographic`)
    assert.equal(effectiveEngine('workbench', look, true), 'workbench', look)
  }
  for (const look of ['solid', 'clay', 'wireframe', 'preview', 'line'] as const) {
    assert.equal(effectiveEngine('realtime_gi', look, true), 'workbench', look)
    assert.equal(effectiveEngine('realtime_gi', look, false), 'workbench', look)
  }
  assert.equal(convergeFrames('workbench'), 1)
  assert.ok(convergeFrames('realtime_gi') > 1)
})

test('time limits follow what a picture costs: pixels, frames, encoding; eight times the reference, with floors', async () => {
  const { convergeFrames, drawnFrames, estimateMs, framesFor, jobRenderCapMs, shotBudgetMs, viewBudgetMs } = await import('../src/creative-scene/render/engines.ts')
  const { exportTimeoutMs, renderTimeoutMs } = await import('../src/creative-scene/host/executor.ts')
  assert.equal(framesFor('workbench', 'export'), 1)
  assert.equal(framesFor('workbench', 'view'), 1)
  assert.ok(framesFor('realtime_gi', 'export') > framesFor('realtime_gi', 'view'))
  // The real-time GI settles for the viewport's frames before it averages; the workbench draws one.
  assert.equal(drawnFrames('workbench', 'export'), 1)
  assert.equal(drawnFrames('realtime_gi', 'export'), convergeFrames('realtime_gi') + framesFor('realtime_gi', 'export'))
  // The design's example: an 8K 3:2 real-time GI image at K = 64, about 87 s; its limit about 11.6 minutes.
  const k = framesFor('realtime_gi', 'export')
  assert.equal(k, 64)
  const eightK = estimateMs('realtime_gi', 8192, 5461, k, 'image/png', false)
  assert.ok(eightK > 80_000 && eightK < 100_000, `${eightK}`)
  assert.equal(shotBudgetMs('realtime_gi', 8192, 5461, k, 'image/png', false), 8 * eightK)
  // A 2K workbench image keeps R1's 30 seconds.
  assert.equal(shotBudgetMs('workbench', 2048, 1365, 1, 'image/png', false), 30_000)
  assert.equal(renderTimeoutMs, viewBudgetMs)
  assert.equal(exportTimeoutMs, jobRenderCapMs)
  assert.equal(jobRenderCapMs, 25 * 60_000, 'within the job’s 30 minutes')
})

test('a picture drawing the world-space layer gets its share of time; render_view as if it drew it, and waited the most for it (S1 tasks 6.2)', async () => {
  const { drawnFrames, estimateMs, shotBudgetMs, traceWaitMs, viewBudgetMs } = await import('../src/creative-scene/render/engines.ts')
  const frames = drawnFrames('realtime_gi', 'export')
  // 10 ns a pixel and frame: about twice the most the reference scenes' 2K lit exports took (3.4–4.7 ns).
  const share = (w: number, h: number) => estimateMs('realtime_gi', w, h, frames, 'image/png', true) - estimateMs('realtime_gi', w, h, frames, 'image/png', false)
  assert.ok(Math.abs(share(2048, 1365) - (2048 * 1365 * frames * 10) / 1e6) < 1e-6, `${share(2048, 1365)}`)
  // Above the floor the limit grows by eight times the share.
  const grown = shotBudgetMs('realtime_gi', 8192, 5461, frames, 'image/png', true) - shotBudgetMs('realtime_gi', 8192, 5461, frames, 'image/png', false)
  assert.ok(Math.abs(grown - 8 * share(8192, 5461)) < 1e-6, `${grown}`)
  // render_view: still the minute's floor, and the 15 s the lit look waits for the layer.
  assert.equal(traceWaitMs, 15_000)
  assert.equal(viewBudgetMs, 60_000 + traceWaitMs)
})
