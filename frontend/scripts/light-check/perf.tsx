// The light size performance page (scene-light-radius AC-S1-7, tasks 7.1). On the dev server only. AC-S1-7's scene:
// frame-two-lights (its parallel and point light) with a spot added and the procedural sky, so a spot, a point, a
// parallel light and the sun, four that cast; with their sizes (the defaults) or all of size 0, at 1200 × 800.
//
//   ?size=0                     every size 0, the sky's sun too (gpuTestHooks.skySunDeg)
//   ?sized=spot,point,parallel,sun   only these sized, the rest 0: which costs what
//   ?volume=1                   a casting volume in the middle (the volumes' march with the lights)
//   ?accumulate=1               the frames averaged, as an export draws them (render/sceneRenderer.ts AccumulateNode)
//   ?engine=realtime_gi         the viewport's engine (workbench by default)
//   ?cold=1                     every material compiles anew (gpuTestHooks.cold)
//   ?frames=240                 frames a timed run draws; three runs, the fastest kept
//   ?backend=webgl
//
// window.__lightCheck: the build (node build and compile), the first frame, a viewport frame, and the toggle: the
// document's lights from their sizes to 0 or back, the stall until the next frame is drawn.
import { createElement } from 'react'
import { createRoot, extend } from '@react-three/fiber'
import * as THREE from 'three'
import type { Scene } from '../../src/creative-scene/api.ts'
import { ShotScene } from '../../src/creative-scene/host/ShotScene.tsx'
import { cameraFraming } from '../../src/creative-scene/host/framing.ts'
import type { Engine } from '../../src/creative-scene/render/engines.ts'
import { createRenderer, gpuOf, gpuTestHooks } from '../../src/creative-scene/render/gpu.ts'
import { frameExposure } from '../../src/creative-scene/render/litLook.ts'
import { SceneRenderer } from '../../src/creative-scene/render/sceneRenderer.ts'
import { sceneLowSun, sceneLux } from '../../src/creative-scene/world/exposure.ts'
import { loadModels, readFixture } from './fixture.ts'

extend(THREE as unknown as Parameters<typeof extend>[0])

const query = new URLSearchParams(location.search)
const sized = query.get('size') !== '0'
/** Which lights have their size; all of them unless ?sized= names some. */
const which = new Set(query.get('sized')?.split(',') ?? ['spot', 'point', 'parallel', 'sun'])
const engine = (query.get('engine') ?? 'workbench') as Engine
const frames = Number(query.get('frames') ?? 240)
if (query.get('backend') === 'webgl') gpuTestHooks.forceWebGL = true
if (query.get('cold') === '1') gpuTestHooks.cold = true
if (!sized || !which.has('sun')) gpuTestHooks.skySunDeg = 0

type Check = { done: boolean; error: string | null; report: Record<string, unknown> }
const check: Check = { done: false, error: null, report: {} }
;(window as unknown as { __lightCheck: Check }).__lightCheck = check
const info = document.getElementById('info')!

const SCENE = 'frame-two-lights'
const down = [-Math.SQRT1_2, 0, 0, Math.SQRT1_2] as [number, number, number, number]

/** The document's lights at their default sizes, or 0. */
function sizeLights(scene: Scene, on: boolean): Scene {
  const objects = scene.objects.map((o) => {
    if (!o.light) return o
    const lit = on && which.has(o.light.type === 'directional' ? 'parallel' : o.light.type)
    const light = o.light.type === 'directional' ? { ...o.light, angular_diameter_deg: lit ? 0.545 : 0 } : { ...o.light, radius_m: lit ? 0.1 : 0 }
    return { ...o, light }
  })
  return { ...scene, objects }
}

/** AC-S1-7's scene: frame-two-lights, a spot above its middle, the procedural sky. */
async function load() {
  const scene = await readFixture(SCENE)
  scene.render.engine = engine
  scene.environment.sky = { kind: 'procedural', sun_elevation_deg: 35, sun_azimuth_deg: 135, turbidity: 3 } as Scene['environment']['sky']
  scene.objects.push({
    id: 'sobj_PerfSpot0000000000000',
    kind: 'light',
    name: '顶部聚光',
    parent_id: null,
    order: 99,
    transform: { position: [0, 6, 2], rotation: down, scale: [1, 1, 1] },
    visible: true,
    locked: false,
    light: { type: 'spot', color: '#ffffff', intensity: 120, cast_shadow: true, distance: 0, decay: 2, angle: 0.7, penumbra: 0.3 },
  } as Scene['objects'][number])
  if (query.get('volume') === '1')
    scene.objects.push({
      id: 'sobj_PerfFog00000000000000',
      kind: 'volume',
      name: '雾',
      parent_id: null,
      order: 98,
      transform: { position: [0, 1.5, 0], rotation: [0, 0, 0, 1], scale: [6, 3, 6] },
      visible: true,
      locked: false,
      volume: { scatter_density: 0.1, cast_shadow: true },
    } as Scene['objects'][number])
  return { scene: sizeLights(scene, sized), models: await loadModels(SCENE, scene) }
}

async function boot() {
  const { scene, models } = await load()
  const cameraID = scene.objects.find((o) => o.kind === 'camera')!.id
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
  // The commit may come before render returns the store.
  let store: ReturnType<typeof root.render> | undefined
  const mount = (s: Scene) =>
    new Promise<void>((resolve, reject) => {
      store = root.render(createElement(ShotScene, { scene: s, models, look: 'lit', onCommit: resolve, onError: reject }))
    })
  await mount(scene)
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

  const picture = new SceneRenderer(g, three, camera)
  const frame = {
    width: framing.width,
    height: framing.height,
    dof: null,
    ...frameExposure(scene, true, framing.exposure, sceneLux(three), sceneLowSun(three)),
    engine,
    quality: 'interactive' as const,
    accumulate: query.get('accumulate') === '1',
  }
  const draw = async () => {
    g.advanceFrame()
    picture.render(camera, frame)
    await g.settle()
  }
  // The build: the materials' nodes and shaders as the viewport compiles them, then the first frame.
  const started = performance.now()
  await g.compile(three, camera)
  const buildMs = performance.now() - started
  const firstStarted = performance.now()
  await draw()
  const firstMs = performance.now() - firstStarted
  // A viewport frame: the fastest of three runs, each frames long, waited for at its end (S0's way).
  const runs: number[] = []
  for (let r = 0; r < 3; r++) {
    const s = performance.now()
    for (let i = 0; i < frames; i++) {
      g.advanceFrame()
      picture.render(camera, frame)
    }
    await g.settle()
    runs.push((performance.now() - s) / frames)
  }
  // The toggle: the document's lights to the other side of 0, as an edit does, until the next frame is drawn.
  const toggleStarted = performance.now()
  await mount(sizeLights(scene, !sized))
  await g.compile(three, camera)
  await draw()
  const toggleMs = performance.now() - toggleStarted
  const programs = (g.renderer as unknown as { _pipelines: { programs: Record<'vertex' | 'fragment', Map<string, unknown>> } })._pipelines.programs
  const shaderKB = Math.round([...programs.fragment.keys(), ...programs.vertex.keys()].reduce((n, c) => n + c.length, 0) / 1024)
  check.report = {
    sized: sized ? [...which] : [],
    engine,
    cold: gpuTestHooks.cold,
    backend: (g.renderer.backend as { isWebGLBackend?: boolean }).isWebGLBackend ? 'webgl' : 'webgpu',
    size_px: { width: framing.width, height: framing.height },
    buildMs: Math.round(buildMs),
    firstMs: Math.round(firstMs),
    frameMs: Number(Math.min(...runs).toFixed(2)),
    runs: runs.map((r) => Number(r.toFixed(2))),
    toggleMs: Math.round(toggleMs),
    shaderKB,
  }
  picture.dispose()
  info.textContent = JSON.stringify(check.report, null, 1)
  check.done = true
}

boot().catch((err: unknown) => {
  check.error = err instanceof Error ? `${err.message}\n${err.stack ?? ''}` : String(err)
  info.textContent = `error: ${check.error}`
})
