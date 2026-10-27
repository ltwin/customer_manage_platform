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
//   ?sky=procedural | physical                   occluder-wall's sun from the sky, along the parallel light's way, which is
//                                                hidden (AC-S1-5); ?size= is the physical sky's sun_size_deg
//   ?resize=<°>                                  with ?sky=physical: drawn once, then the sun's size changed to this (AC-S1-10)
//   ?stripe=fixed                                a size-0 light's lift 0.05 m, not 1.5 texels (gpuTestHooks.stripeFixed)
//   &refFrames= (8192) &rasterFrames= (64) &width= (960)
//
// window.__lightCheck tells the e2e when it is done and what it measured.
import { createElement } from 'react'
import { createRoot, extend } from '@react-three/fiber'
import * as THREE from 'three'
import type { Scene } from '../../src/creative-scene/api.ts'
import { ShotScene } from '../../src/creative-scene/host/ShotScene.tsx'
import { cameraFraming } from '../../src/creative-scene/host/framing.ts'
import type { SceneModel } from '../../src/creative-scene/models/cache.ts'
import { traceWaitMs } from '../../src/creative-scene/render/engines.ts'
import { createRenderer, gpuOf, gpuTestHooks } from '../../src/creative-scene/render/gpu.ts'
import { traceBase } from '../../src/creative-scene/render/trace/base.ts'
import { applyExposure } from '../../src/creative-scene/world/exposure.ts'
import { nightSkyDefaults, physicalSkyDefaults } from '../../src/creative-scene/world/physicalSky.ts'
import { boardCentres, drawPanels, highlight, penumbrae, regionErrors, shadowError, terminator, type Boards } from './measure.ts'
import { compare, lum, refLights } from './reference.ts'
import { fixtures, loadModels, readFixture } from './fixture.ts'

extend(THREE as unknown as Parameters<typeof extend>[0])

const query = new URLSearchParams(location.search)
const sceneName = query.get('scene') ?? 'occluder-wall'
const lightName = query.get('light') ?? 'spot'
const size = query.has('size') ? Number(query.get('size')) : null
const skyKind = query.get('sky')
if (query.get('backend') === 'webgl') gpuTestHooks.forceWebGL = true
if (query.get('stripe') === 'fixed') gpuTestHooks.stripeFixed = true

type Check = { done: boolean; error: string | null; report: Record<string, unknown> }
const check: Check = { done: false, error: null, report: {} }
;(window as unknown as { __lightCheck: Check }).__lightCheck = check

const info = document.getElementById('info')!
const show = (lines: string[]) => (info.textContent = lines.join('\n'))

const occluderLights: Record<string, string> = { spot: '聚光', point: '点光', sun: '平行光' }

type Params = Record<string, unknown>
const paramsOf = (o: Scene['objects'][number]) => (o.material_slots as Record<string, { params: Params }> | undefined)?.main?.params

/** The reference scene as the e2e's server gives it (creative-scene.e2e.mjs referenceScene), edited by the query, and its models. */
async function load(): Promise<{ scene: Scene; models: Record<string, SceneModel> }> {
  const scene = await readFixture(sceneName)
  scene.render.engine = 'workbench'
  for (const o of scene.objects) {
    if (!o.light) continue
    if (sceneName === 'occluder-wall') o.visible = !skyKind && o.name === occluderLights[lightName]
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
    if (skyKind) {
      // The way toward the parallel light's sun, which shines along its −z: the sky's angles for it.
      const toward = new THREE.Vector3(0, 0, 1).applyQuaternion(new THREE.Quaternion().fromArray(named('平行光')!.transform.rotation))
      const angles = { sun_elevation_deg: (Math.asin(toward.y) * 180) / Math.PI, sun_azimuth_deg: (Math.atan2(toward.x, toward.z) * 180) / Math.PI }
      if (skyKind === 'procedural') scene.environment.sky = { kind: 'procedural', turbidity: 3, ...angles } as Scene['environment']['sky']
      else {
        const physical = ((await (await fetch(`${fixtures}/physical-noon.json`)).json()) as Scene).environment.sky
        // Completed as the server completes it: a physical sky short of a field is drawn as none (world/physicalSky.ts isPhysical).
        const night = { ...nightSkyDefaults, ...physical.night }
        scene.environment.sky = { ...physicalSkyDefaults, ...physical, night, sun_mode: 'manual', ...angles, sun_size_deg: size ?? 0.545 } as Scene['environment']['sky']
      }
    }
    if (query.get('layout') === 'glint') {
      const mid = new THREE.Vector3(-3.2382, 2.8598, -2.93)
      const eye = new THREE.Vector3().fromArray(scene.objects.find((o) => o.kind === 'camera')!.transform.position)
      const toEye = eye.sub(mid).normalize()
      // The panel faces +z: the mirror keeps z and turns x and y; the sun shines the other way.
      const shine = new THREE.Vector3(toEye.x, toEye.y, -toEye.z)
      named('平行光')!.transform.rotation = new THREE.Quaternion().setFromUnitVectors(new THREE.Vector3(0, 0, -1), shine).toArray() as [number, number, number, number]
    }
  }
  return { scene, models: await loadModels(sceneName, scene) }
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

type CascadeLight = THREE.Object3D & { shadow: THREE.LightShadow & { camera: THREE.OrthographicCamera } }

/**
 * A shadow map's texel in metres where each board's shadow falls on the wall, for the light that casts: a spot's
 * 2 d tan(fov/2) / mapSize, a point's 2 d / mapSize (a cube face spans 90°), d the distance to the light; the sun's
 * the width of the nearest cascade whose map holds the point (render/sunShadow.ts). A narrow penumbra may be off by
 * two of these (Epic AC-1).
 */
function texelsAt(three: THREE.Scene, boards: Boards): Record<string, number> | null {
  let light: THREE.Light | null = null
  three.traverse((o) => {
    if ((o as THREE.Light).isLight && o.castShadow && o.visible && Boolean(o.userData.sun) === Boolean(skyKind)) light ??= o as THREE.Light
  })
  if (!light) return null
  const l = light as THREE.Light & { shadow: THREE.LightShadow }
  const at = l.getWorldPosition(new THREE.Vector3())
  const cascades = (l.shadow as unknown as { shadowNode?: { lights?: CascadeLight[] } }).shadowNode?.lights ?? []
  const out: Record<string, number> = {}
  for (const b of boardCentres(boards)) {
    const p = new THREE.Vector3(...b.at)
    const d = p.distanceTo(at)
    const size = l.shadow.mapSize.x
    if ((l as THREE.SpotLight).isSpotLight) out[b.name] = (2 * d * Math.tan(((l.shadow.camera as THREE.PerspectiveCamera).fov * Math.PI) / 360)) / size
    else if ((l as THREE.PointLight).isPointLight) out[b.name] = (2 * d) / size
    else {
      const holding = cascades.map((c) => {
        const q = p.clone().applyMatrix4(c.shadow.camera.matrixWorldInverse)
        const cam = c.shadow.camera
        return q.x >= cam.left && q.x <= cam.right && q.y >= cam.bottom && q.y <= cam.top ? (cam.right - cam.left) / c.shadow.mapSize.x : Infinity
      })
      out[b.name] = Math.min(...holding)
    }
  }
  return out
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
  // A physical sky's lights are in lux, exposed as each frame is drawn (world/exposure.ts), which this page's draws
  // are not: exposed once here, the sun about 10. Shadows and highlights are measured as ratios.
  if (skyKind === 'physical') applyExposure(three, 1e-4)
  await g.compile(three, camera)
  if (skyKind === 'physical' && query.has('resize')) {
    // Drawn with the sun as loaded, then its size changed as an edit does: the shader stays, the cascades follow.
    g.renderer.render(three, camera)
    const sky = { ...scene.environment.sky, sun_size_deg: Number(query.get('resize')) } as Scene['environment']['sky']
    await new Promise<void>((resolve, reject) => {
      root.render(createElement(ShotScene, { scene: { ...scene, environment: { ...scene.environment, sky } }, models, look: 'lit', onCommit: resolve, onError: reject }))
    })
    applyExposure(three, 1e-4)
  }
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
  const sunBoards = lightName === 'sun' || skyKind != null
  check.report = {
    scene: sceneName,
    light: occluder ? (skyKind ? `${skyKind} sky` : lightName) : null,
    size,
    backend: (g.renderer.backend as { isWebGLBackend?: boolean }).isWebGLBackend ? 'webgl' : 'webgpu',
    size_px: cmpSize,
    refFrames,
    rasterFrames,
    referenceMs: Math.round(c.referenceMs),
    averaged: error.averaged,
    first: error.first,
    penumbrae: occluder && query.get('layout') == null ? penumbrae(c, error, sunBoards ? 'sun' : 'near') : null,
    texels: occluder && query.get('layout') == null ? texelsAt(three, sunBoards ? 'sun' : 'near') : null,
    regions: occluder ? regionErrors(c, error) : null,
    highlight: occluder ? highlight(c) : null,
    terminator: query.get('layout') === 'near' ? terminator(c) : null,
    lights: refLights(three).map((l) => ({ kind: l.kind, radius: 'radius' in l ? l.radius : undefined, angle: 'angle' in l ? l.angle : undefined })),
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
