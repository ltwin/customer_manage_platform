import test from 'node:test'
import assert from 'node:assert/strict'
import { ApiError } from '../src/api/transport.ts'
import type { Upload } from '../src/creative-canvas/editor/media.ts'
import type { Scene, SceneObject } from '../src/creative-scene/api.ts'
import {
  aspectOf,
  blurPixels,
  cameraViewFov,
  equivalentFocal,
  fStopShortcuts,
  fStopThirds,
  maxBlurShare,
  presetOf,
  sharpRange,
  verticalFov,
} from '../src/creative-scene/cameraOptics.ts'
import { cameraFraming } from '../src/creative-scene/host/framing.ts'
import { renderShots, submitExport, uploadShots, type ExportPass, type ShotImage } from '../src/creative-scene/host/exportShots.ts'
import { ShotTimeout } from '../src/creative-scene/render/gpu.ts'
import type { Shooter } from '../src/creative-scene/host/render.ts'
import { DeviceLostError } from '../src/creative-scene/render/gpu.ts'
import { readFileSync } from 'node:fs'

const deg = (r: number) => (r * 180) / Math.PI
const close = (a: number, b: number, eps = 1e-9) => Math.abs(a - b) < eps

// --- optics -----------------------------------------------------------------

test('the field of view follows the picture’s long side: a portrait sees what the landscape sees, turned', () => {
  assert.ok(close(verticalFov(35, [36, 24], 3 / 2), deg(2 * Math.atan(12 / 35))), '3:2 landscape as before')
  const landscapeWidth = 2 * Math.atan(Math.tan((verticalFov(35, [36, 24], 3 / 2) * Math.PI) / 360) * 1.5)
  assert.ok(close(verticalFov(35, [36, 24], 2 / 3), deg(landscapeWidth)), '2:3 portrait height = 3:2 landscape width')
  assert.ok(close(verticalFov(50, [24, 36], 1), deg(2 * Math.atan(18 / 50))), 'only the sensor’s long side counts')
  assert.equal(aspectOf('16:9'), 16 / 9)
  assert.equal(aspectOf('bad'), 1.5)
})

test('the camera view fits the camera’s picture into 90% of the viewport', () => {
  const v = verticalFov(35, [36, 24], 1.5)
  const t = (d: number) => Math.tan((d * Math.PI) / 360)
  // A viewport wider than the picture: its height is what fills.
  assert.ok(close(t(cameraViewFov(v, 1.5, 2)), t(v) / 0.9))
  // Narrower: the picture's width fills 90% of the viewport's width.
  const view = cameraViewFov(v, 1.5, 1)
  assert.ok(close(t(view) * 1, (t(v) * 1.5) / 0.9))
})

test('sensor presets, the full-frame equivalent and the aperture steps', () => {
  assert.equal(presetOf([23.5, 15.6]), 'APS-C')
  assert.equal(presetOf([36, 24]), '全画幅')
  assert.equal(presetOf([30, 20]), null)
  assert.ok(close(equivalentFocal(35, [23.5, 15.6]), 53.617, 1e-3), 'APS-C 35 mm is about 53.6 mm')
  assert.ok(close(equivalentFocal(25, [17.3, 13]), 52.02, 1e-2), 'M43 by the long side: factor 2.08')
  for (const f of fStopShortcuts) assert.ok(fStopThirds.includes(f), `f/${f} is on the slider`)
  assert.deepEqual([fStopThirds[0], fStopThirds.at(-1)], [0.7, 64])
})

test('the sharp range and the blur follow the lens: 50 mm f/2.8 at 3 m is about 2.74–3.32 m', () => {
  const r = sharpRange(50, 2.8, 3, [36, 24])
  assert.ok(close(r.near, 2.739, 2e-3) && close(r.far, 3.316, 2e-3), `${r.near} – ${r.far}`)
  assert.equal(sharpRange(24, 11, 5, [36, 24]).far, Infinity, 'past the hyperfocal distance everything behind is sharp')
  const dof = { focalMM: 50, fStop: 2.8, focusM: 3, sensorLongMM: 36 }
  assert.equal(blurPixels(dof, 3, 2048), 0, 'sharp at the focus')
  assert.ok(blurPixels(dof, 2, 2048) < 0 && blurPixels(dof, 6, 2048) > 0, 'negative in front, positive behind')
  assert.ok(Math.abs(blurPixels(dof, 6, 1024) * 2 - blurPixels(dof, 6, 2048)) < 1e-9, 'scales with the picture: 1024 is half of 2048')
  assert.equal(blurPixels({ ...dof, fStop: 0.7 }, 0.2, 2048), -maxBlurShare * 2048, 'capped at 4% of the long side')
  assert.ok(blurPixels({ ...dof, fStop: 8 }, 6, 2048) < blurPixels(dof, 6, 2048), 'a smaller aperture blurs less')
})

// --- focus ------------------------------------------------------------------

const identity = () => ({ position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] })
const object = (o: Partial<SceneObject> & { id: string; kind: SceneObject['kind'] }): SceneObject => ({
  name: '',
  parent_id: null,
  order: 0,
  transform: identity(),
  visible: true,
  locked: false,
  ...o,
})
const sceneOf = (...objects: SceneObject[]): Scene =>
  ({ environment: { sky: { kind: 'color', color: '#000000' }, ambient: { color: '#ffffff', intensity: 0.4 }, fog: null }, render: { mode: 'clay', exposure_ev: 0, engine: 'workbench', resolution: '2k' }, objects }) as Scene
const camera = (extra: Record<string, unknown> = {}) =>
  object({
    id: 'cam',
    kind: 'camera',
    transform: { ...identity(), position: [0, 1, 5] },
    camera: { focal_mm: 50, sensor_mm: [36, 24], aspect: '3:2', near: 0.1, far: 100, label: '', depth_of_field: true, f_stop: 2.8, focus: 10, focus_object_id: '', ...extra },
  })

test('the camera focuses on its focus object along its view, else at focus, kept between near and far', () => {
  const empty = object({ id: 'empty', kind: 'group', transform: { ...identity(), position: [2, 1, 2] } })
  const on = cameraFraming(sceneOf(camera({ focus_object_id: 'empty' }), empty), 'cam', 2048)
  assert.ok(!('error' in on))
  assert.ok(close(on.optics!.focusM, 3, 1e-9), 'depth along −Z, not straight-line distance')
  assert.deepEqual([on.width, on.height], [2048, 1365])
  const gone = cameraFraming(sceneOf(camera({ focus_object_id: 'empty' })), 'cam')
  assert.ok(!('error' in gone) && gone.optics!.focusM === 10, 'a removed focus object falls back to focus')
  const behind = object({ id: 'behind', kind: 'primitive', transform: { ...identity(), position: [0, 1, 9] } })
  const clamped = cameraFraming(sceneOf(camera({ focus_object_id: 'behind' }), behind), 'cam')
  assert.ok(!('error' in clamped) && clamped.optics!.focusM === 0.1, 'an object behind the camera clamps to near')
  assert.ok(!('error' in clamped) && clamped.depthOfField === true)
})

// --- export flow ------------------------------------------------------------

test('the workbench GTAO is three’s GTAONode but for what is marked fork:', () => {
  const read = (p: string) => readFileSync(new URL(p, import.meta.url), 'utf8')
  const upstream = read('../node_modules/three/examples/jsm/tsl/display/GTAONode.js')
  const fork = read('../src/creative-scene/render/nodes/GTAONode.js')
  // The AO block three has where the fork has its own.
  const aoOf = (text: string) => text.slice(text.indexOf('\t\tthis._ao = Fn( () => {'), text.indexOf('\t\tthis._sharedContext = builder.getSharedContext();'))
  const restored = fork
    .slice(fork.indexOf('import '))
    .replace(/\t\t\/\/ fork: begin[^\n]*\n[\s\S]*?\t\t\/\/ fork: end\n\n/, aoOf(upstream))
    // The inserted uniform: comment, line and blank.
    .replace(/\t*\/\/ fork:[^\n]*\n[^\n]*\n\n/, '')
  assert.ok(!restored.includes('fork:'), 'every change is undone here')
  assert.equal(restored, upstream, 'three changed GTAONode: compare and port the fork')
})

test('a submitted export keeps its operation and time across retries; a refusal stops at once', async () => {
  const seen: [string, string][] = []
  let failures = 2
  const ok = await submitExport(
    {
      send: async (operation, createdAt) => {
        seen.push([operation, createdAt])
        if (failures-- > 0) throw new TypeError('Failed to fetch')
        return { change_id: 'cwch_1', scene_revision: '3', nodes: [] }
      },
      wait: async () => {},
      newID: () => 'op-1',
      clock: () => new Date('2026-10-19T12:00:00Z'),
    },
    { canvas_id: 'c', scene_node_id: 'n', scene_revision: '3', shots: [] },
  )
  assert.ok(ok.ok)
  assert.deepEqual(seen, [['op-1', '2026-10-19T12:00:00.000Z'], ['op-1', '2026-10-19T12:00:00.000Z'], ['op-1', '2026-10-19T12:00:00.000Z']])

  const refused = await submitExport({ send: async () => { throw new ApiError(409, 'creative_upload_unavailable', '上传已失效') }, wait: async () => {} }, {
    canvas_id: 'c', scene_node_id: 'n', scene_revision: '3', shots: [],
  })
  assert.deepEqual(refused, { ok: false, refused: true, message: '上传已失效', code: 'creative_upload_unavailable' })

  let calls = 0
  const unknown = await submitExport({ send: async () => { calls++; throw new ApiError(503, 'unavailable', '') }, wait: async () => {} }, {
    canvas_id: 'c', scene_node_id: 'n', scene_revision: '3', shots: [],
  })
  assert.equal(calls, 4)
  assert.deepEqual(unknown, { ok: false, refused: false, message: '没能确认导出结果，请先看画布上是否已有这组图' })
})

test('uploads go two at a time in order; a failure hands back what already went up', async () => {
  const images: ShotImage[] = ['a', 'b', 'c', 'd'].map((c) => ({ camera: c, pass: 'clay', blob: new Blob([c]) }))
  let running = 0
  let most = 0
  const sent = await uploadShots(async (file) => {
    running++
    most = Math.max(most, running)
    await new Promise((r) => setTimeout(r, 1))
    running--
    return { id: file.name } as Upload
  }, images, new AbortController().signal)
  assert.ok(sent.ok)
  assert.deepEqual(sent.uploads.map((u) => u.id), ['a-clay.png', 'b-clay.png', 'c-clay.png', 'd-clay.png'])
  assert.equal(most, 2)

  const failed = await uploadShots(async (file) => {
    if (file.name.startsWith('b')) throw new Error('网络中断')
    return { id: file.name } as Upload
  }, images, new AbortController().signal)
  assert.ok(!failed.ok)
  assert.ok(failed.uploads.some((u) => u.id === 'a-clay.png'))
})

test('render shots draws every camera and pass from the snapshot, blurring clay only with depth of field on', async () => {
  const shots: { kind: string; dof?: boolean }[] = []
  let closed = false
  const shooter: Shooter = {
    shoot: (_f, o) => {
      shots.push({ kind: o.kind, dof: o.depthOfField })
      return new Blob([o.kind])
    },
    setLook: async () => {},
    confirm: async () => {},
    close: () => { closed = true },
  }
  const doc = { scene: sceneOf(camera(), object({ id: 'cam2', kind: 'camera', camera: { aspect: '2:3', depth_of_field: false } as SceneObject['camera'] })), models: {}, revision: '5' }
  const read = async () => doc as never
  const out = await renderShots({ read, loadModels: async () => {}, open: async () => shooter }, 'doc', { cameraObjectIDs: ['cam', 'cam2'], passes: ['clay', 'line'] })
  assert.ok(out.ok && out.revision === '5' && out.images.length === 4 && closed)
  assert.deepEqual(shots, [{ kind: 'clay', dof: true }, { kind: 'line', dof: false }, { kind: 'clay', dof: false }, { kind: 'line', dof: false }])
  const missing = await renderShots({ read, loadModels: async () => {}, open: async () => shooter }, 'doc', { cameraObjectIDs: ['nope'], passes: ['clay'] })
  assert.deepEqual(missing, { ok: false, message: '摄像机 nope 已不在场景里' })
})

test('lit images come after every clay and line image, with the lit look mounted once; images keep camera × pass order', async () => {
  const doc = { scene: sceneOf(camera(), object({ id: 'cam2', kind: 'camera', camera: { aspect: '2:3', depth_of_field: false } as SceneObject['camera'] })), models: {}, revision: '5' }
  const read = async () => doc as never
  // The switch's timeout fires only when the look never comes.
  const run = async (passes: ExportPass[], setLook: Shooter['setLook'] = async () => {}, sleep = () => new Promise<void>(() => {})) => {
    const events: string[] = []
    const opened: string[] = []
    const shooter: Shooter = {
      shoot: (_f, o) => {
        events.push(`${o.kind}${o.depthOfField ? '+dof' : ''}`)
        return new Blob([o.kind])
      },
      setLook: async (look) => {
        events.push(`look ${look}`)
        return setLook(look)
      },
      confirm: async () => {},
      close: () => events.push('close'),
    }
    const out = await renderShots({ read, loadModels: async () => {}, open: async (_d, look) => (opened.push(look), shooter), sleep }, 'doc', { cameraObjectIDs: ['cam', 'cam2'], passes })
    return { out, events, opened }
  }
  const all = await run(['clay', 'line', 'lit'])
  assert.deepEqual(all.opened, ['clay'])
  assert.deepEqual(all.events, ['clay+dof', 'line', 'clay', 'line', 'look lit', 'lit+dof', 'lit', 'close'], 'lit blurs like clay')
  assert.ok(all.out.ok)
  assert.deepEqual(all.out.images.map((i) => `${i.camera} ${i.pass} ${String(i.blob.size)}`), ['cam clay 4', 'cam line 4', 'cam lit 3', 'cam2 clay 4', 'cam2 line 4', 'cam2 lit 3'])

  const only = await run(['lit'])
  assert.deepEqual(only.opened, ['lit'], 'a lit-only export opens in the lit look')
  assert.deepEqual(only.events, ['lit+dof', 'lit', 'close'])

  const stuck = await run(['clay', 'lit'], () => new Promise(() => {}), async () => {})
  assert.deepEqual(stuck.out, { ok: false, message: '切换到材质光照超过 60 秒' })
  assert.equal(stuck.events.at(-1), 'close', 'the shooter is closed all the same')
})

test('each image goes to the shooter with its deadline; one given up between frames fails the export as too slow', async () => {
  const doc = { scene: sceneOf(camera()), models: {}, revision: '5' }
  const read = async () => doc as never
  const deadlines: number[] = []
  const before = performance.now()
  const shooter: Shooter = {
    shoot: async (_f, o) => {
      deadlines.push(o.deadline ?? NaN)
      if (o.kind === 'line') throw new ShotTimeout()
      return new Blob([o.kind])
    },
    setLook: async () => {},
    confirm: async () => {},
    close: () => {},
  }
  const out = await renderShots({ read, loadModels: async () => {}, open: async () => shooter }, 'doc', { cameraObjectIDs: ['cam'], passes: ['clay', 'line'] })
  // A 2K workbench image: the 30-second floor.
  assert.deepEqual(out, { ok: false, message: '一张导出图渲染超过 30 秒' })
  assert.equal(deadlines.length, 2)
  for (const d of deadlines) assert.ok(d >= before + 30_000 && d <= performance.now() + 30_000, `deadline ${d}`)
})

test('a real-time GI lit image that draws the world-space layer gets the longer limit; clay keeps the workbench’s (S1 tasks 6.2)', async () => {
  const { drawnFrames, shotBudgetMs } = await import('../src/creative-scene/render/engines.ts')
  const doc = { scene: { ...sceneOf(camera()), render: { mode: 'lit', exposure_ev: 0, engine: 'realtime_gi', resolution: '2k' } }, models: {}, revision: '5' }
  const limits = async (world: boolean) => {
    const out: { kind: string; ms: number; width: number; height: number }[] = []
    const shooter: Shooter = {
      maxTextureSize: 16384,
      world,
      shoot: async (f, o) => {
        out.push({ kind: o.kind, ms: (o.deadline ?? NaN) - performance.now(), width: f.width, height: f.height })
        return new Blob([o.kind])
      },
      setLook: async () => {},
      confirm: async () => {},
      close: () => {},
    }
    const done = await renderShots({ read: async () => doc as never, loadModels: async () => {}, open: async () => shooter }, 'doc', { cameraObjectIDs: ['cam'], passes: ['clay', 'lit'] })
    assert.ok(done.ok)
    return out
  }
  for (const world of [false, true]) {
    const [clay, lit] = await limits(world)
    assert.ok(Math.abs(clay.ms - 30_000) < 100, `${String(world)}: clay ${String(clay.ms)}`)
    const want = shotBudgetMs('realtime_gi', lit.width, lit.height, drawnFrames('realtime_gi', 'export'), 'image/png', world)
    assert.ok(Math.abs(lit.ms - want) < 100, `${String(world)}: lit ${String(lit.ms)}, want ${String(want)}`)
  }
})

test('the output resolution sets the long side and the encoding; a device too small fails at once, a lost one at 4K is not retried', async () => {
  const { imageTypeOf } = await import('../src/creative-scene/render/engines.ts')
  assert.deepEqual(imageTypeOf('clay', '2k'), { type: 'image/png' })
  assert.deepEqual(imageTypeOf('lit', '4k'), { type: 'image/jpeg', quality: 0.95 })
  assert.deepEqual(imageTypeOf('clay', '8k'), { type: 'image/jpeg', quality: 0.95 })
  assert.deepEqual(imageTypeOf('line', '8k'), { type: 'image/png' }, 'line art stays PNG')
  const docAt = (resolution: string) => ({ scene: { ...sceneOf(camera()), render: { mode: 'clay', exposure_ev: 0, engine: 'workbench', resolution } }, models: {}, revision: '5' })
  const shots: { width: number; height: number; type: string; quality?: number }[] = []
  let closed = 0
  const shooterOf = (maxTextureSize: number, lose = false): Shooter => ({
    maxTextureSize,
    world: false,
    shoot: async (f, o) => {
      if (lose) throw new DeviceLostError()
      shots.push({ width: f.width, height: f.height, type: o.type, quality: o.quality })
      return new Blob([o.kind], { type: o.type })
    },
    setLook: async () => {},
    confirm: async () => {},
    close: () => { closed++ },
  })
  const run = (resolution: string, shooter: Shooter) => renderShots({ read: async () => docAt(resolution) as never, loadModels: async () => {}, open: async () => shooter }, 'doc', { cameraObjectIDs: ['cam'], passes: ['clay', 'line'] })
  const fourK = await run('4k', shooterOf(16384))
  assert.ok(fourK.ok)
  assert.deepEqual(shots.map((s) => [Math.max(s.width, s.height), s.type, s.quality]), [[4096, 'image/jpeg', 0.95], [4096, 'image/png', undefined]])
  shots.length = 0
  assert.ok((await run('1k', shooterOf(16384))).ok)
  assert.deepEqual(shots.map((s) => Math.max(s.width, s.height)), [1024, 1024])
  shots.length = 0
  closed = 0
  const small = await run('8k', shooterOf(4096))
  assert.deepEqual(small, { ok: false, message: '当前设备最大只能导出长边 4096 px 的图，请选更低的输出分辨率', hostError: true })
  assert.equal(shots.length, 0, 'nothing drawn smaller')
  assert.equal(closed, 1)
  const lost = await run('4k', shooterOf(16384, true))
  assert.deepEqual(lost, { ok: false, message: '渲染设备在按 4K 出图时丢失，当前设备可能画不了这个分辨率，可以换低一档再导出', hostError: true })
  await assert.rejects(run('2k', shooterOf(16384, true)), DeviceLostError, 'at 2K a lost device goes back to the queue as before')
})

test('an export image goes up as it was encoded: JPEG named .jpg', async () => {
  const names: [string, string, string][] = []
  const images: ShotImage[] = [
    { camera: 'cam', pass: 'clay', blob: new Blob(['j'], { type: 'image/jpeg' }) },
    { camera: 'cam', pass: 'line', blob: new Blob(['p'], { type: 'image/png' }) },
  ]
  const out = await uploadShots(async (file, declared) => (names.push([file.name, file.type, declared.mime]), { id: file.name } as Upload), images, new AbortController().signal)
  assert.ok(out.ok)
  assert.deepEqual(names, [['cam-clay.jpg', 'image/jpeg', 'image/jpeg'], ['cam-line.png', 'image/png', 'image/png']])
})

// --- editor -----------------------------------------------------------------

test('a new camera stands where the view looks from; an empty is a childless group named in turn', async () => {
  const { newCamera, newEmpty, localPose } = await import('../src/creative-scene/editing.ts')
  const existing = [camera(), object({ id: 'g', kind: 'group', order: 1 }), object({ id: 'child', kind: 'primitive', parent_id: 'g' })]
  const cam = newCamera({ position: [1, 2, 3], rotation: [0, 0.3826834, 0, 0.9238795] }, existing, () => 'sobj_new')
  assert.deepEqual([cam.kind, cam.name, cam.order, cam.camera], ['camera', '摄像机 2', 2, {}])
  assert.deepEqual(cam.transform, { position: [1, 2, 3], rotation: [0, 0.3826834, 0, 0.9238795], scale: [1, 1, 1] })
  const empty = newEmpty([0, 1, 0], existing, () => 'sobj_e')
  assert.deepEqual([empty.kind, empty.name, empty.parent_id, empty.transform.position], ['group', '空物体 1', null, [0, 1, 0]], 'a group with children is not an empty')

  // Ctrl+Alt+0 on a camera inside a group: the world pose in the group's space.
  const turned = object({ id: 'rig', kind: 'group', transform: { position: [2, 0, 0], rotation: [0, 0.7071068, 0, 0.7071068], scale: [2, 2, 2] } })
  const local = localPose([turned], 'rig', { position: [2, 0, -4], rotation: [0, 0.7071068, 0, 0.7071068] }, [1, 1, 1])
  assert.ok(local.position.every((v, i) => close(v, [2, 0, 0][i], 1e-6)), `position ${local.position}`)
  assert.ok(close(Math.abs(local.rotation[3]), 1, 1e-6), `rotation ${local.rotation}`)
})

test('the export dialog allows 1 to 48 images; it ticks the lit look when the view shows materials', async () => {
  const { exportCount, defaultPasses } = await import('../src/creative-scene/host/exportShots.ts')
  assert.deepEqual(defaultPasses('solid'), ['clay', 'line'])
  assert.deepEqual(defaultPasses('wireframe'), ['clay', 'line'])
  assert.deepEqual(defaultPasses('preview'), ['clay', 'line', 'lit'])
  assert.deepEqual(defaultPasses('rendered'), ['clay', 'line', 'lit'])
  assert.deepEqual(exportCount(3, 2), { count: 6, problem: null })
  assert.equal(exportCount(0, 2).problem, '至少选一台摄像机')
  assert.equal(exportCount(2, 0).problem, '至少选一种导出方式')
  assert.equal(exportCount(24, 2).problem, null)
  assert.equal(exportCount(17, 3).problem, '一次最多导出 48 张，请少选一些')
})

test('a manual export frees its uploads when it is refused or never answered', async () => {
  const { runManualExport } = await import('../src/creative-scene/host/exportShots.ts')
  const doc = { scene: sceneOf(camera()), models: {}, revision: '4' }
  const shooter: Shooter = { shoot: () => new Blob(['x']), setLook: async () => {}, confirm: async () => {}, close: () => {} }
  const run = async (send: () => Promise<never> | Promise<{ change_id: string; scene_revision: string; nodes: [] }>) => {
    const discarded: string[] = []
    let n = 0
    const out = await runManualExport(
      {
        read: async () => doc as never,
        loadModels: async () => {},
        open: async () => shooter,
        upload: async () => ({ id: `ccup_${++n}` }) as Upload,
        discard: async (u) => { discarded.push(u.id) },
        send,
        wait: async () => {},
      },
      { documentID: 'doc', canvasID: 'ccv', sceneNodeID: 'node', plan: { cameraObjectIDs: ['cam'], passes: ['clay', 'line'] } },
      new AbortController().signal,
    )
    return { out, discarded }
  }
  const ok = await run(async () => ({ change_id: 'cwch', scene_revision: '4', nodes: [] }))
  assert.ok(ok.out.ok && ok.discarded.length === 0)
  const refused = await run(async () => { throw new ApiError(409, 'archived_read_only', '已归档') })
  assert.deepEqual(refused.out, { ok: false, message: '画布已归档，恢复后再导出' })
  assert.deepEqual(refused.discarded, ['ccup_1', 'ccup_2'])
  const lost = await run(async () => { throw new TypeError('Failed to fetch') })
  assert.deepEqual(lost.out, { ok: false, message: '没能确认导出结果，请先看画布上是否已有这组图' })
  assert.equal(lost.discarded.length, 2)
})

test('a device lost while the shots were drawn hands none of them on: the shooter closes and a manual export uploads nothing', async () => {
  const { runManualExport } = await import('../src/creative-scene/host/exportShots.ts')
  const doc = { scene: sceneOf(camera()), models: {}, revision: '4' }
  let closed = 0
  const shooter: Shooter = {
    shoot: () => new Blob(['x']),
    setLook: async () => {},
    confirm: async () => { throw new DeviceLostError() },
    close: () => { closed++ },
  }
  const plan = { cameraObjectIDs: ['cam'], passes: ['clay', 'line'] as ExportPass[] }
  await assert.rejects(renderShots({ read: async () => doc as never, loadModels: async () => {}, open: async () => shooter }, 'doc', plan), DeviceLostError)
  assert.equal(closed, 1)
  const uploads: Blob[] = []
  const out = await runManualExport(
    {
      read: async () => doc as never,
      loadModels: async () => {},
      open: async () => shooter,
      upload: async (file) => (uploads.push(file), { id: 'ccup_1' }) as Upload,
      discard: async () => {},
      send: async () => ({ change_id: 'cwch', scene_revision: '4', nodes: [] }),
      wait: async () => {},
    },
    { documentID: 'doc', canvasID: 'ccv', sceneNodeID: 'node', plan },
    new AbortController().signal,
  )
  assert.deepEqual(out, { ok: false, message: '渲染设备暂时不可用，请重试' })
  assert.equal(uploads.length, 0)
  assert.equal(closed, 2)
})
