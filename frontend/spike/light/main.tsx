// scene-light-size S0 (scene-light-size-spike): the spike page. On the dev server only (no build entry); moved into
// the change's discovery when S0 ends (design「穿刺页面」). It mounts the product's ShotScene and draws it through the
// product's SceneRenderer, as an export does (host/render.ts openShooter), with the candidate soft shadows put on the
// lights before their first build.
//
//   ?scene=frame-two-lights | occluder-wall       the reference scene (scripts/fixtures/scene-reference)
//   ?light=spot | point | sun                     occluder-wall's one light on (spot by default)
//   ?method=pcf | …                                the soft shadow (soft.ts); pcf is the product's
//   ?size=<m or °>                                 the light's radius, or a directional light's angular diameter
//   ?engine=workbench | realtime_gi                the renderer (the scene's own by default)
//   ?frames=<n>                                    frames drawn before the picture is kept (the export's by default)
//   ?side=<px>                                     the picture's long side (1200 by default)
//   ?backend=webgl                                 draws through the WebGL2 backend
//   ?cold=1                                        a constant new every load in the spike's shaders
//   ?view=compare                                  the reference against the raster's direct light (reference.ts):
//     &refFrames= &rasterFrames= &width= &shape=ellipse &flip=1
//
// window.__spike tells the measure scripts when the picture is done and what was drawn.
import { createElement } from 'react'
import { createRoot, extend } from '@react-three/fiber'
import * as THREE from 'three'
import { RectAreaLightTexturesLib } from 'three/addons/lights/RectAreaLightTexturesLib.js'
import { MeshLambertNodeMaterial, MeshPhysicalNodeMaterial, RectAreaLightNode } from 'three/webgpu'
import type { Scene } from '../../src/creative-scene/api.ts'
import { ShotScene } from '../../src/creative-scene/host/ShotScene.tsx'
import { cameraFraming } from '../../src/creative-scene/host/framing.ts'
import { modelCache, type SceneModel } from '../../src/creative-scene/models/cache.ts'
import { drawnFrames, effectiveEngine, engineOf, traceWaitMs } from '../../src/creative-scene/render/engines.ts'
import { createRenderer, gpuOf, gpuTestHooks } from '../../src/creative-scene/render/gpu.ts'
import { frameExposure } from '../../src/creative-scene/render/litLook.ts'
import { SceneRenderer } from '../../src/creative-scene/render/sceneRenderer.ts'
import { traceBase } from '../../src/creative-scene/render/trace/base.ts'
import { sceneLowSun, sceneLux } from '../../src/creative-scene/world/exposure.ts'
import { convergence, drawPanels, shadowError } from './panels.ts'
import { highlight, penumbrae, penumbraLines, regionErrors, terminator } from './penumbra.ts'
import { compare, refLights, type Comparison } from './reference.ts'
import { methods, softShadows, type Method } from './soft.ts'
import { sphereLights } from './sphere.ts'
import { contactShadows } from './contact.ts'

extend(THREE as unknown as Parameters<typeof extend>[0])

const query = new URLSearchParams(location.search)
const sceneName = query.get('scene') ?? 'occluder-wall'
const lightName = query.get('light') ?? 'spot'
const method = (query.get('method') ?? 'pcf') as Method
const size = Number(query.get('size') ?? '0')
const side = Number(query.get('side') ?? '1200')
if (query.get('backend') === 'webgl') gpuTestHooks.forceWebGL = true

/**
 * Unused: a measure of cold compiles needs no salt, since every measure launch is a fresh browser profile; the warm
 * case loads twice in one browser (measure/shot.mjs SPIKE_WARM). Kept in the report as 0.
 */
export const coldSalt = 0

type Spike = { done: boolean; error: string | null; report: Record<string, unknown>; data?: Comparison }
const spike: Spike = { done: false, error: null, report: {} }
;(window as unknown as { __spike: Spike }).__spike = spike

const info = document.getElementById('info')!
const show = (lines: string[]) => (info.textContent = lines.join('\n'))

const fixtures = '/scripts/fixtures/scene-reference'
const occluderLights: Record<string, string> = { spot: '聚光', point: '点光', sun: '平行光' }

/**
 * area-studio: occluder-wall's room with its lights off and a softbox in their place, an area light the document
 * cannot hold until S2 (design「局部设计与偏差」1). Square 1 m, 100 cd/m² (?nits=), from the left, facing the wall: lit like the other lights, about 3 lux on the wall.
 */
function addSoftbox(root: THREE.Object3D) {
  RectAreaLightNode.setLTC(RectAreaLightTexturesLib.init())
  const box = new THREE.RectAreaLight(0xffffff, Number(query.get('nits') ?? 100), 1, 1)
  box.name = 'softbox'
  // View space (Z up): the document's (−2.5, 1.8, 2.5) in Y up, aimed at the wall's middle.
  box.position.set(-2.5, -2.5, 1.8)
  box.lookAt(0, 2.95, 1.8)
  root.add(box)
  box.updateMatrixWorld()
}

/** The reference scene as the e2e's server gives it (creative-scene.e2e.mjs referenceScene), and its models. */
async function load(): Promise<{ scene: Scene; models: Record<string, SceneModel> }> {
  const file = sceneName === 'area-studio' ? 'occluder-wall' : sceneName
  const scene = (await (await fetch(`${fixtures}/${file}.json`)).json()) as Scene
  if (sceneName === 'area-studio') for (const o of scene.objects) if (o.kind === 'light') o.visible = false
  scene.render.exposure_ev100 ??= 15
  scene.render.exposure_mode ??= 'ambient'
  scene.render.exposure_compensation ??= 0
  for (const o of scene.objects) if (o.camera) Object.assign(o.camera, { exposure_mode: 'manual', exposure_compensation: 0, ...o.camera })
  if (sceneName === 'occluder-wall') for (const o of scene.objects) if (o.kind === 'light') o.visible = o.name === occluderLights[lightName]
  // ?layout=stack: the 3 m board on the way from the lights to the 0.5 m board, so their shadows lie one in the
  // other: two layers of blockers for one receiver (design「测量与判定」多层遮挡). A box stands on its base.
  // ?layout=near: the point light 2 m to the ball's left, so a sphere light's size shows on the ball's terminator
  // (design「测量与判定」包裹; penumbra.ts NEAR_LIGHT).
  if (sceneName === 'occluder-wall' && query.get('layout') === 'near') {
    const point = scene.objects.find((o) => o.name === '点光')!
    point.transform.position = [0.6, 0.6, -1.4]
  }
  if (sceneName === 'occluder-wall' && query.get('layout') === 'stack') {
    const far = scene.objects.find((o) => o.name === '遮挡板 3 m')!
    far.transform.position = [-2.988, 2.787, 0.06]
  }
  const engine = query.get('engine')
  if (engine === 'workbench' || engine === 'realtime_gi') scene.render.engine = engine
  const models: Record<string, SceneModel> = {}
  for (const rev of new Set(scene.objects.flatMap((o) => (o.model ? [o.model.content_revision_id] : [])))) {
    const meta = (await (await fetch(`${fixtures}/${sceneName}/${rev}.json`)).json()) as Omit<SceneModel, 'content_id' | 'usable'>
    const summary = { content_id: `ccnt_${rev}`, usable: true, ...meta } as SceneModel
    models[rev] = summary
    modelCache.register(rev, { glb: await (await fetch(`${fixtures}/${sceneName}/${rev}.glb`)).arrayBuffer(), summary })
  }
  await Promise.all(Object.keys(models).map((r) => modelCache.load(r)))
  return { scene, models }
}

async function boot() {
  if (!(method in methods)) throw new Error(`unknown method ${method}; one of ${Object.keys(methods).join(', ')}`)
  const { scene, models } = await load()
  const cameraID = scene.objects.find((o) => o.kind === 'camera')?.id
  if (!cameraID) throw new Error('the scene has no camera')
  const framing = cameraFraming(scene, cameraID, side)
  if ('error' in framing) throw new Error(framing.error)

  const canvas = document.getElementById('view') as HTMLCanvasElement
  const camera = new THREE.PerspectiveCamera()
  camera.up.set(0, 0, 1)
  const root = createRoot(canvas)
  await root.configure({
    gl: ({ canvas }) => createRenderer({ canvas: canvas as HTMLCanvasElement, output: 'pipeline', role: 'offscreen' }).then((g) => g.renderer),
    linear: true,
    flat: true,
    shadows: 'percentage',
    frameloop: 'never',
    dpr: 1,
    size: { width: framing.width, height: framing.height, top: 0, left: 0 },
    camera,
  })
  let store: ReturnType<typeof root.render> | undefined
  await new Promise<void>((resolve, reject) => {
    store = root.render(createElement(ShotScene, { scene, models, look: 'lit', onCommit: resolve, onError: reject }))
  })
  const state = store!.getState()
  const g = gpuOf(state.gl)
  const three = state.scene

  if (sceneName === 'area-studio') addSoftbox(three)
  // Before anything builds: three keeps the first shadow node a light makes.
  const lights = softShadows(three, method, size)
  const contact = contactShadows()
  if (contact) lights.push(contact)
  // Sphere lights (highlights and falloff, sphere.ts) unless ?sphere=0: the product's point lights.
  if (query.get('sphere') !== '0') lights.push(sphereLights(g.renderer, three, size))

  const engine = effectiveEngine(engineOf(scene.render), 'lit', true)
  const compileStarted = performance.now()
  await g.compile(three, camera)
  const compileMs = performance.now() - compileStarted
  if (engine === 'realtime_gi' && !(await traceBase(g, three, { incremental: false }).ready(traceWaitMs))) throw new Error('the world-space layer was not ready')

  camera.fov = framing.fov
  camera.aspect = framing.width / framing.height
  camera.near = framing.near
  camera.far = framing.far
  camera.position.fromArray(framing.position)
  camera.quaternion.fromArray(framing.quaternion)
  camera.updateProjectionMatrix()
  camera.updateMatrixWorld()
  g.resetFrames()
  const picture = new SceneRenderer(g, three, camera)
  const frames = Number(query.get('frames') ?? drawnFrames(engine, 'export'))
  const frame = {
    width: framing.width,
    height: framing.height,
    dof: null,
    ...frameExposure(scene, true, framing.exposure, sceneLux(three), sceneLowSun(three)),
    engine,
    quality: 'final' as const,
    accumulate: frames > 1,
  }
  const drawStarted = performance.now()
  let firstMs = 0
  for (let i = 0; i < frames; i++) {
    g.advanceFrame()
    picture.render(camera, frame)
    await g.settle()
    if (i === 0) firstMs = performance.now() - drawStarted
  }
  const drawMs = performance.now() - drawStarted
  spike.report = { scene: sceneName, light: sceneName === 'occluder-wall' ? lightName : null, method, size, engine, frames, width: framing.width, height: framing.height, lights, compileMs: Math.round(compileMs), firstMs: Math.round(firstMs), drawMs: Math.round(drawMs), coldSalt }
  const head = `${sceneName}${sceneName === 'occluder-wall' ? ` (${lightName})` : ''} · ${method} · size ${size} · ${engine} · ${frames} frames`
  show([head, `compile ${Math.round(compileMs)} ms, first frame ${Math.round(firstMs)} ms, all ${Math.round(drawMs)} ms`])

  // ?standIn=1: whether SceneRenderer.compile's stand-in compiles what the next frame draws (design「挂载点」
  // standIn): a new kind of material appears, compile() runs, and the frame after it should build no new shader.
  if (query.get('standIn') === '1') {
    const shaders = () => new Set((g.renderer as unknown as { _pipelines: { programs: { fragment: Map<string, unknown> } } })._pipelines.programs.fragment.keys())
    const programs = () => shaders().size
    const known = shaders()
    const ball = new THREE.Mesh(new THREE.SphereGeometry(0.3, 48, 24), query.get('standInMaterial') === 'lambert' ? new MeshLambertNodeMaterial({ color: 0x8899aa }) : new MeshPhysicalNodeMaterial({ color: 0x8899aa, roughness: 0.4, clearcoat: 1, sheen: 1 }))
    ball.position.set(1, -1, 0.3)
    ball.castShadow = ball.receiveShadow = true
    const before = programs()
    three.add(ball)
    const compileStarted = performance.now()
    await picture.compile()
    const compiled = programs()
    ;(window as unknown as { __atCompile?: Set<string> }).__atCompile = shaders()
    const compileMs = performance.now() - compileStarted
    const frameStarted = performance.now()
    g.advanceFrame()
    picture.render(camera, frame)
    await g.settle()
    const frameMs = performance.now() - frameStarted
    const after = programs()
    // What the frame built that the compile had not: its size, and a few marks of what it holds.
    const atCompile = (window as unknown as { __atCompile?: Set<string> }).__atCompile ?? known
    const fresh = [...shaders()].filter((c) => !atCompile.has(c)).map((c) => ({ chars: c.length, csm: (c.match(/_csm\d+/g) ?? []).slice(0, 3), loads: (c.match(/textureLoad|textureSampleLevel|texelFetch/g) ?? []).length, compares: (c.match(/textureSampleCompare|texture\( *\w*[Ss]hadow/g) ?? []).length }))
    ;(spike.report as Record<string, unknown>).fresh = fresh
    spike.report = { ...spike.report, standIn: { before, compiled, after, newAtFrame: after - compiled, compileMs: Math.round(compileMs), frameMs: Math.round(frameMs) } }
    show([head, `stand-in: ${before} → compile ${compiled} → frame ${after} fragment shaders (${after - compiled} new at the frame, ${Math.round(frameMs)} ms)`])
  }

  // ?view=compare: the reference against the raster's direct light, under this frame's exposure (design「参考解」).
  if (query.get('view') === 'compare') {
    const base = traceBase(g, three, { incremental: false })
    if (!(await base.ready(traceWaitMs))) throw new Error('the world-space layer was not ready')
    const handle = base.handle()
    if (!handle) throw new Error('the world-space layer has no handle')
    const width = Number(query.get('width') ?? 960)
    const cmpSize = { width, height: Math.max(16, Math.round(width / camera.aspect / 16) * 16) }
    const refFrames = Number(query.get('refFrames') ?? 8192)
    const rasterFrames = Number(query.get('rasterFrames') ?? 64)
    const shape = query.get('shape') === 'ellipse' ? 'ellipse' : 'rectangle'
    const c = await compare(g.renderer, three, camera, handle.layer, refLights(three, size, shape, Number(query.get('sunSize') ?? size)), cmpSize, refFrames, rasterFrames, query.get('converge') === '1')
    const error = shadowError(c)
    const panels = drawPanels(c, query.get('flip') === '1')
    document.body.append(panels)
    canvas.style.display = 'none'
    const widths = sceneName === 'occluder-wall' ? penumbrae(c, error) : null
    spike.report = { ...spike.report, view: 'compare', tier: handle.tier, compare: { size: cmpSize, refFrames, rasterFrames, referenceMs: Math.round(c.referenceMs), rasterMs: Math.round(c.rasterMs), averaged: error.averaged, first: error.first, convergence: convergence(c), penumbrae: widths, regions: sceneName === 'occluder-wall' ? regionErrors(c, error) : null, highlight: sceneName === 'occluder-wall' ? highlight(c) : null, terminator: query.get('layout') === 'near' ? terminator(c) : null } }
    show([head, `reference ${refFrames} frames in ${Math.round(c.referenceMs)} ms; |Δ shadow| averaged ${error.averaged.mean.toFixed(4)} (p95 ${error.averaged.p95.toFixed(3)}), first ${error.first.mean.toFixed(4)}`, ...(widths ? penumbraLines(widths) : [])])
    spike.data = c
  }
  spike.done = true
}

boot().catch((err: unknown) => {
  spike.error = err instanceof Error ? `${err.message}\n${err.stack ?? ''}` : String(err)
  show([`error: ${spike.error}`])
})
