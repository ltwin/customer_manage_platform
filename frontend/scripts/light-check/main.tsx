// The light size measurement page (scene-light-radius, S1's e2e; scene-light-size S0's spike page, trimmed). On the
// dev server only: the build's one entry is index.html. It mounts the product's ShotScene with a light reference
// scene (scripts/fixtures/scene-reference) as the document has it, sizes written into its lights, and holds the
// raster's direct light against the reference (reference.ts), as creative-scene.e2e.mjs asks.
//
//   ?scene=occluder-wall | frame-two-lights      the reference scene
//   ?light=spot | point | sun                    occluder-wall's one light on (spot by default)
//   ?size=<m or °>                               the lights' radius_m, or a directional light's angular_diameter_deg
//   ?gloss=<roughness>                           the gloss panel's roughness (0.15 as the scene has it)
//   ?coat=1                                      the gloss panel a clearcoat: base roughness 0.5, coat 1 at 0.05
//   ?layout=near                                 the point light 2 m to the ball's left (the terminator, AC-3)
//   ?layout=close                                the point light 5 cm off the wall (light near a surface stays finite)
//   ?layout=glint                                the sun turned so the camera sees it in the gloss panel's middle (AC-4)
//   ?backend=webgl                               draws through the WebGL2 backend
//   &refFrames= (8192) &rasterFrames= (64) &width= (960)
//
// window.__lightCheck tells the e2e when it is done and what it measured.
import { createElement } from 'react'
import { createRoot, extend } from '@react-three/fiber'
import * as THREE from 'three'
import type { Scene } from '../../src/creative-scene/api.ts'
import { ShotScene } from '../../src/creative-scene/host/ShotScene.tsx'
import { cameraFraming } from '../../src/creative-scene/host/framing.ts'
import { modelCache, type SceneModel } from '../../src/creative-scene/models/cache.ts'
import { traceWaitMs } from '../../src/creative-scene/render/engines.ts'
import { createRenderer, gpuOf, gpuTestHooks } from '../../src/creative-scene/render/gpu.ts'
import { traceBase } from '../../src/creative-scene/render/trace/base.ts'
import { drawPanels, highlight, penumbrae, regionErrors, shadowError, terminator } from './measure.ts'
import { compare, lum, refLights } from './reference.ts'

extend(THREE as unknown as Parameters<typeof extend>[0])

const query = new URLSearchParams(location.search)
const sceneName = query.get('scene') ?? 'occluder-wall'
const lightName = query.get('light') ?? 'spot'
const size = query.has('size') ? Number(query.get('size')) : null
if (query.get('backend') === 'webgl') gpuTestHooks.forceWebGL = true

type Check = { done: boolean; error: string | null; report: Record<string, unknown> }
const check: Check = { done: false, error: null, report: {} }
;(window as unknown as { __lightCheck: Check }).__lightCheck = check

const info = document.getElementById('info')!
const show = (lines: string[]) => (info.textContent = lines.join('\n'))

const fixtures = '/scripts/fixtures/scene-reference'
const occluderLights: Record<string, string> = { spot: '聚光', point: '点光', sun: '平行光' }

type Params = Record<string, unknown>
const paramsOf = (o: Scene['objects'][number]) => (o.material_slots as Record<string, { params: Params }> | undefined)?.main?.params

/** The reference scene as the e2e's server gives it (creative-scene.e2e.mjs referenceScene), edited by the query, and its models. */
async function load(): Promise<{ scene: Scene; models: Record<string, SceneModel> }> {
  const scene = (await (await fetch(`${fixtures}/${sceneName}.json`)).json()) as Scene
  scene.render.exposure_ev100 ??= 15
  scene.render.exposure_mode ??= 'ambient'
  scene.render.exposure_compensation ??= 0
  scene.render.engine = 'workbench'
  for (const o of scene.objects) if (o.camera) Object.assign(o.camera, { exposure_mode: 'manual', exposure_compensation: 0, ...o.camera })
  for (const o of scene.objects) {
    if (!o.light) continue
    if (sceneName === 'occluder-wall') o.visible = o.name === occluderLights[lightName]
    const directional = o.light.type === 'directional'
    if (directional) o.light.angular_diameter_deg = size ?? o.light.angular_diameter_deg ?? 0.545
    else o.light.radius_m = size ?? o.light.radius_m ?? 0.1
  }
  const named = (name: string) => scene.objects.find((o) => o.name === name)
  if (sceneName === 'occluder-wall') {
    const panel = paramsOf(named('高光板')!)!
    if (query.has('gloss')) panel.roughness = Number(query.get('gloss'))
    if (query.get('coat') === '1') Object.assign(panel, { roughness: 0.5, clearcoat: 1, clearcoat_roughness: 0.05 })
    // The point light 2 m to the ball's left: a sphere light's size shows on the ball's terminator (measure.ts NEAR_LIGHT).
    if (query.get('layout') === 'near') named('点光')!.transform.position = [0.6, 0.6, -1.4]
    // The point light 5 cm off the wall's face (z −2.95), level with the gloss panel's middle but clear of it.
    if (query.get('layout') === 'close') named('点光')!.transform.position = [1.5, 1.8, -2.9]
    // The sun along the mirror of the way from the panel's middle to the camera, so its highlight is on the panel.
    if (query.get('layout') === 'glint') {
      const mid = new THREE.Vector3(-3.2382, 2.8598, -2.93)
      const eye = new THREE.Vector3().fromArray(scene.objects.find((o) => o.kind === 'camera')!.transform.position)
      const toEye = eye.sub(mid).normalize()
      // The panel faces +z: the mirror keeps z and turns x and y; the sun shines the other way.
      const shine = new THREE.Vector3(toEye.x, toEye.y, -toEye.z)
      named('平行光')!.transform.rotation = new THREE.Quaternion().setFromUnitVectors(new THREE.Vector3(0, 0, -1), shine).toArray() as [number, number, number, number]
    }
  }
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

/** The raster's unshadowed light that is not finite, and its brightest: a light near a surface must stay finite (AC-3). */
function finite(a: Float32Array) {
  let bad = 0
  let most = 0
  for (let i = 0; i < a.length / 4; i++) {
    const v = lum(a, i)
    if (!Number.isFinite(v)) bad++
    else most = Math.max(most, v)
  }
  return { notFinite: bad, brightest: most }
}

async function boot() {
  const { scene, models } = await load()
  const cameraID = scene.objects.find((o) => o.kind === 'camera')?.id
  if (!cameraID) throw new Error('the scene has no camera')
  const framing = cameraFraming(scene, cameraID, 1200)
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

  camera.fov = framing.fov
  camera.aspect = framing.width / framing.height
  camera.near = framing.near
  camera.far = framing.far
  camera.position.fromArray(framing.position)
  camera.quaternion.fromArray(framing.quaternion)
  camera.updateProjectionMatrix()
  camera.updateMatrixWorld()
  await g.compile(three, camera)
  g.resetFrames()

  const base = traceBase(g, three, { incremental: false })
  if (!(await base.ready(traceWaitMs))) throw new Error('the world-space layer was not ready')
  const handle = base.handle()
  if (!handle) throw new Error('the world-space layer has no handle')
  const width = Number(query.get('width') ?? 960)
  const cmpSize = { width, height: Math.max(16, Math.round(width / camera.aspect / 16) * 16) }
  const refFrames = Number(query.get('refFrames') ?? 8192)
  const rasterFrames = Number(query.get('rasterFrames') ?? 64)
  const c = await compare(g.renderer, three, camera, handle.reflect, refLights(three), cmpSize, refFrames, rasterFrames)
  const error = shadowError(c)
  document.body.append(drawPanels(c, false))
  canvas.style.display = 'none'
  const occluder = sceneName === 'occluder-wall'
  check.report = {
    scene: sceneName,
    light: occluder ? lightName : null,
    size,
    backend: g.renderer.backend.isWebGLBackend ? 'webgl' : 'webgpu',
    size_px: cmpSize,
    refFrames,
    rasterFrames,
    referenceMs: Math.round(c.referenceMs),
    averaged: error.averaged,
    first: error.first,
    penumbrae: occluder && query.get('layout') == null ? penumbrae(c, error, lightName === 'sun' ? 'sun' : 'near') : null,
    regions: occluder ? regionErrors(c, error) : null,
    highlight: occluder ? highlight(c) : null,
    terminator: query.get('layout') === 'near' ? terminator(c) : null,
    raster: finite(c.raster.unshadowed),
    reference: finite(c.reference.unshadowed),
  }
  show([`${sceneName}${occluder ? ` (${lightName})` : ''} · size ${size ?? 'as stored'} · reference ${refFrames} frames in ${Math.round(c.referenceMs)} ms`, `|Δ shadow| averaged ${error.averaged.mean.toFixed(4)} (p95 ${error.averaged.p95.toFixed(3)}), first ${error.first.mean.toFixed(4)}`])
  check.done = true
}

boot().catch((err: unknown) => {
  check.error = err instanceof Error ? `${err.message}\n${err.stack ?? ''}` : String(err)
  show([`error: ${check.error}`])
})
