import test from 'node:test'
import assert from 'node:assert/strict'
import { ApiError } from '../src/api/transport.ts'
import type { Upload } from '../src/creative-canvas/editor/media.ts'
import { bakeFailure, compileFailure, renderTimeoutMs, SceneExecutor, type ExecutorDeps, type Shot } from '../src/creative-scene/host/executor.ts'
import type { ExportPlan, RenderedShots } from '../src/creative-scene/host/exportShots.ts'
import { leaseEveryMs, type CompletePayload, type Completion, type SceneJob } from '../src/creative-scene/host/jobs.ts'
import type { Ticker } from '../src/creative-scene/host/ticker.ts'
import { DeviceLostError } from '../src/creative-scene/render/gpu.ts'
import type { CompileResult } from '../src/creative-scene/sandbox/host.ts'
import type { Manifest } from '../src/creative-scene/sandbox/inspect.ts'
import { activityText, AgentActivity, editedObjects, highlightedObjects, jobPlaceholders, revisionObjects, type ActivityDeps } from '../src/creative-scene/agentActivity.ts'
import type { RestoreBatch, RevisionItem, SceneJobSummary } from '../src/creative-scene/api.ts'
import { blockedText, documentOf, finishedDetail, outcomeText, restorePayload, screenshotRevision, submitBatches, type Detail } from '../src/creative-canvas/editor/agentScene.ts'
import { currentStep, type RunView } from '../src/creative-canvas/editor/agentBlocks.ts'
import { messageSteps } from '../src/creative-canvas/editor/agentState.ts'

// --- the page's scene job executor -----------------------------------------

const manifest = { bounds: { min: [0, 0, 0], max: [1, 2, 1] } } as unknown as Manifest
const module = { contract_version: 1, lib_version: 1, mount: 'floor', params_schema: { h: { type: 'number', default: 2, min: 1, max: 3 } }, params: { h: 2 } } as const

function deferred<T>() {
  let resolve!: (v: T) => void
  let reject!: (e: unknown) => void
  const promise = new Promise<T>((a, b) => { resolve = a; reject = b })
  return { promise, resolve, reject }
}

const tick = () => new Promise((r) => setTimeout(r, 0))

function bakeJob(request: Record<string, unknown> = { code: 'export default…', params: { h: 2 }, lib_version: 1, target: { op: 'add' } }): SceneJob {
  return { id: 'csj_1', document_id: 'cwdoc_1', kind: 'bake_model', request, attempt: 0, claim_token: 'tok_1', lease_until: '', deadline: '' }
}

class ManualTicker implements Ticker {
  beats: { ms: number; fn: () => void; stopped: boolean }[] = []
  waits: number[] = []
  private long: (() => void)[] = []
  after = () => () => {}
  /** Short pauses pass at once; the render timeout waits for expire(). */
  wait = (ms: number, signal?: AbortSignal) => {
    if (ms < renderTimeoutMs) { this.waits.push(ms); return Promise.resolve() }
    return new Promise<void>((resolve, reject) => {
      this.long.push(resolve)
      signal?.addEventListener('abort', () => reject(new Error('已取消')), { once: true })
    })
  }
  expire() { for (const f of this.long.splice(0)) f() }
  every = (ms: number, fn: () => void) => {
    const beat = { ms, fn, stopped: false }
    this.beats.push(beat)
    return () => { beat.stopped = true }
  }
  /** One lease interval passes. */
  beat() { for (const b of this.beats) if (!b.stopped) b.fn() }
  get running() { return this.beats.filter((b) => !b.stopped).length }
}

type Call = { operation: string; payload: CompletePayload }

class FakePage {
  ticker = new ManualTicker()
  compiles: { code: string; params: Record<string, unknown> }[] = []
  compileResult: CompileResult = { ok: true, glb: new ArrayBuffer(8), manifest, module, codeSHA256: 'sha256-x' }
  compileGate: ReturnType<typeof deferred<void>> | null = null
  thumbnailFails = false
  uploads: { name: string; signal: AbortSignal; gate: ReturnType<typeof deferred<Upload>>; mime?: string }[] = []
  autoUpload = true
  discarded: string[] = []
  leases: string[] = []
  leaseError: unknown = null
  completes: Call[] = []
  renders: { documentID: string; request: unknown }[] = []
  shot: Shot | Error | Promise<Shot> = { ok: true, image: new Blob(['jpeg']), revision: '7' }
  exports: { documentID: string; plan: ExportPlan }[] = []
  exported: RenderedShots | Error = {
    ok: true,
    revision: '9',
    images: [
      { camera: 'camA', pass: 'clay', blob: new Blob(['a']) },
      { camera: 'camA', pass: 'line', blob: new Blob(['b']) },
      { camera: 'camB', pass: 'clay', blob: new Blob(['c']) },
      { camera: 'camB', pass: 'line', blob: new Blob(['d']) },
    ],
  }
  deviceLosses = 0
  /** Answers for complete, in order; past the end, a recorded success. */
  answers: (Completion | Error)[] = []
  private n = 0

  deps(): ExecutorDeps {
    return {
      ticker: this.ticker,
      compile: async (input) => {
        this.compiles.push({ code: input.code, params: input.params })
        if (this.compileGate) await this.compileGate.promise
        return this.compileResult
      },
      thumbnail: () => (this.thumbnailFails ? Promise.reject(new Error('WebGL 上下文丢失')) : Promise.resolve(new Blob(['png']))),
      upload: (file, declared, signal) => {
        const gate = deferred<Upload>()
        this.uploads.push({ name: file.name, signal, gate, mime: declared.mime })
        if (this.autoUpload) gate.resolve({ id: `ccup_${++this.n}` } as Upload)
        return gate.promise
      },
      discard: (u) => { this.discarded.push(u.id); return Promise.resolve() },
      render: (documentID, request) => {
        this.renders.push({ documentID, request })
        return this.shot instanceof Error ? Promise.reject(this.shot) : Promise.resolve(this.shot)
      },
      exportImages: (documentID, plan) => {
        this.exports.push({ documentID, plan })
        return this.exported instanceof Error ? Promise.reject(this.exported) : Promise.resolve(this.exported)
      },
      deviceLost: () => { this.deviceLosses++ },
      api: {
        claim: () => Promise.resolve(null),
        lease: (jobID) => {
          this.leases.push(jobID)
          return this.leaseError ? Promise.reject(this.leaseError) : Promise.resolve()
        },
        complete: (_jobID, operation, _createdAt, payload) => {
          this.completes.push({ operation, payload })
          const answer = this.answers.shift() ?? { job_id: 'csj_1', state: payload.outcome, result: {} }
          return answer instanceof Error ? Promise.reject(answer) : Promise.resolve(answer)
        },
      },
    }
  }
}

const unavailable = () => new ApiError(409, 'creative_upload_unavailable', '暂存上传不可用')
const stateConflict = () => new ApiError(409, 'creative_state_conflict', '任务已结束')

test('a bake job compiles, uploads both files and reports them with the checked module', async () => {
  const page = new FakePage()
  const out = await new SceneExecutor(page.deps()).run(bakeJob())
  assert.equal(out.state, 'completed')
  assert.deepEqual(page.compiles, [{ code: 'export default…', params: { h: 2 } }])
  assert.deepEqual(page.uploads.map((u) => u.name), ['model.glb', 'thumbnail.png'])
  assert.equal(page.completes.length, 1)
  assert.deepEqual(page.completes[0].payload, {
    claim_token: 'tok_1',
    outcome: 'succeeded',
    bake: { glb_upload_id: 'ccup_1', thumbnail_upload_id: 'ccup_2', manifest, module: { mount: 'floor', params_schema: module.params_schema, params: { h: 2 } } },
  })
  assert.deepEqual(page.discarded, [], 'a recorded result keeps its files')
  assert.equal(page.ticker.running, 0, 'the lease stops being renewed')
})

test('the lease is renewed every 10 seconds while the job runs', async () => {
  const page = new FakePage()
  page.compileGate = deferred<void>()
  const running = new SceneExecutor(page.deps()).run(bakeJob())
  await tick()
  assert.deepEqual(page.ticker.beats.map((b) => b.ms), [leaseEveryMs])
  page.ticker.beat()
  page.ticker.beat()
  assert.deepEqual(page.leases, ['csj_1'], 'not again while a renewal is out')
  await tick()
  page.ticker.beat()
  assert.deepEqual(page.leases, ['csj_1', 'csj_1'])
  page.compileGate.resolve()
  await running
})

test('failures map to the codes the assistant reads: its code to fix, the page’s own trouble a host error', () => {
  const cases: [Parameters<typeof compileFailure>[0], string, string][] = [
    [{ kind: 'syntax', message: 'Unexpected token', line: 3, column: 7 }, 'compile_error', '第 3 行第 7 列：Unexpected token'],
    [{ kind: 'runtime', message: 'x is not defined', line: 12 }, 'compile_error', '第 12 行：x is not defined'],
    [{ kind: 'invalid', message: '模型不符合要求（mesh）：没有网格' }, 'compile_error', '模型不符合要求（mesh）：没有网格'],
    [{ kind: 'timeout', message: '编译超过 5 秒，已中止' }, 'compile_timeout', '编译超过 5 秒，已中止'],
    [{ kind: 'limit', message: '模型超出上限：三角形' }, 'limit_exceeded', '模型超出上限：三角形'],
    [{ kind: 'timeout', message: '代码沙箱没有启动', host: true }, 'host_error', '代码沙箱没有启动'],
  ]
  for (const [error, code, message] of cases) assert.deepEqual(compileFailure(error), [code, message], error.message)
  assert.deepEqual(bakeFailure({ ok: false, stage: 'thumbnail', cause: new Error('WebGL 上下文丢失'), uploads: [] }), ['host_error', '缩略图生成失败：WebGL 上下文丢失'])
  assert.deepEqual(bakeFailure({ ok: false, stage: 'upload', cause: new Error('媒体校验失败'), uploads: [] }), ['host_error', '模型文件上传失败：媒体校验失败'])
})

test('a failed bake reports the failure and frees what it uploaded', async () => {
  const compile = new FakePage()
  compile.compileResult = { ok: false, error: { kind: 'syntax', message: 'Unexpected end of input', line: 1 } }
  await new SceneExecutor(compile.deps()).run(bakeJob())
  assert.deepEqual(compile.completes.map((c) => c.payload), [{ claim_token: 'tok_1', outcome: 'failed', error: { code: 'compile_error', message: '第 1 行：Unexpected end of input' } }])
  assert.equal(compile.uploads.length, 0)

  const thumbnail = new FakePage()
  thumbnail.thumbnailFails = true
  await new SceneExecutor(thumbnail.deps()).run(bakeJob())
  assert.equal(thumbnail.completes[0].payload.error?.code, 'host_error')

  const upload = new FakePage()
  upload.autoUpload = false
  const running = new SceneExecutor(upload.deps()).run(bakeJob())
  await tick()
  upload.uploads[0].gate.resolve({ id: 'ccup_glb' } as Upload)
  upload.uploads[1].gate.reject(new Error('媒体校验失败'))
  await running
  assert.deepEqual(upload.completes.map((c) => c.payload.error), [{ code: 'host_error', message: '模型文件上传失败：媒体校验失败' }])
  assert.deepEqual(upload.discarded, ['ccup_glb'])

  const malformed = new FakePage()
  await new SceneExecutor(malformed.deps()).run(bakeJob({ params: {} }))
  assert.equal(malformed.compiles.length, 0)
  assert.equal(malformed.completes[0].payload.error?.code, 'host_error')
})

test('staged files that became unusable are uploaded once more, under a new operation', async () => {
  const page = new FakePage()
  page.answers = [unavailable()]
  const out = await new SceneExecutor(page.deps()).run(bakeJob())
  assert.equal(out.state, 'completed')
  assert.equal(page.compiles.length, 1, 'no second compile')
  assert.deepEqual(page.uploads.map((u) => u.name), ['model.glb', 'thumbnail.png', 'model.glb', 'thumbnail.png'])
  assert.deepEqual(page.completes.map((c) => c.payload.bake?.glb_upload_id), ['ccup_1', 'ccup_3'])
  assert.notEqual(page.completes[0].operation, page.completes[1].operation)

  const twice = new FakePage()
  twice.answers = [unavailable(), unavailable()]
  await new SceneExecutor(twice.deps()).run(bakeJob())
  assert.equal(twice.uploads.length, 4, 'only one more upload')
  assert.deepEqual(twice.completes.map((c) => c.payload.outcome), ['succeeded', 'succeeded', 'failed'])
  assert.equal(twice.completes[2].payload.error?.code, 'host_error')
})

test('a lost lease stops the job and frees its files without reporting', async () => {
  const page = new FakePage()
  page.autoUpload = false
  const running = new SceneExecutor(page.deps()).run(bakeJob())
  await tick()
  assert.equal(page.uploads.length, 2, 'uploading')
  page.leaseError = stateConflict()
  page.ticker.beat()
  await tick()
  assert.ok(page.uploads.every((u) => u.signal.aborted), 'the uploads are cancelled')
  page.uploads[0].gate.resolve({ id: 'ccup_glb' } as Upload)
  page.uploads[1].gate.reject(new Error('已取消'))
  assert.deepEqual(await running, { state: 'lost' })
  assert.equal(page.completes.length, 0, 'nothing is reported')
  assert.deepEqual(page.discarded, ['ccup_glb'])

  const compiling = new FakePage()
  compiling.compileGate = deferred<void>()
  const run = new SceneExecutor(compiling.deps()).run(bakeJob())
  await tick()
  compiling.leaseError = new ApiError(404, 'not_found', '任务不存在')
  compiling.ticker.beat()
  await tick()
  compiling.compileGate.resolve()
  assert.deepEqual(await run, { state: 'lost' })
  assert.equal(compiling.uploads.length, 0, 'a lost job is not uploaded')

  const network = new FakePage()
  network.compileGate = deferred<void>()
  const kept = new SceneExecutor(network.deps()).run(bakeJob())
  await tick()
  network.leaseError = new TypeError('Failed to fetch')
  network.ticker.beat()
  await tick()
  network.compileGate.resolve()
  assert.equal((await kept).state, 'completed', 'a failed renewal is retried, not taken as lost')
})

test('a job ended elsewhere frees the bake; a recorded failure keeps it for the retry', async () => {
  const ended = new FakePage()
  ended.answers = [stateConflict()]
  assert.deepEqual(await new SceneExecutor(ended.deps()).run(bakeJob()), { state: 'lost' })
  assert.deepEqual(ended.discarded.sort(), ['ccup_1', 'ccup_2'])

  const conflicted = new FakePage()
  conflicted.answers = [{ job_id: 'csj_1', state: 'failed', result: { code: 'conflict', bake: {} } }]
  const out = await new SceneExecutor(conflicted.deps()).run(bakeJob())
  assert.equal(out.state, 'completed')
  assert.deepEqual(conflicted.discarded, [], 'the server holds a conflicted bake for retry_job_id')
})

test('an unanswered report is retried under the same operation, then left to the lease', async () => {
  const flaky = new FakePage()
  flaky.answers = [new TypeError('Failed to fetch'), new ApiError(502, 'internal', '网关错误')]
  assert.equal((await new SceneExecutor(flaky.deps()).run(bakeJob())).state, 'completed')
  assert.equal(new Set(flaky.completes.map((c) => c.operation)).size, 1, 'one operation, replayed')
  assert.deepEqual(flaky.ticker.waits, [1000, 2000])

  const down = new FakePage()
  down.answers = [new TypeError('Failed to fetch'), new TypeError('Failed to fetch'), new TypeError('Failed to fetch')]
  const out = await new SceneExecutor(down.deps()).run(bakeJob())
  assert.equal(out.state, 'abandoned')
  assert.equal(down.completes.length, 3)
  assert.deepEqual(down.discarded.sort(), ['ccup_1', 'ccup_2'])
})

test('a refused result shape is reported as a host error instead of looping', async () => {
  const page = new FakePage()
  page.answers = [new ApiError(400, 'validation_failed', 'manifest 格式不正确')]
  await new SceneExecutor(page.deps()).run(bakeJob())
  assert.deepEqual(page.completes.map((c) => c.payload.outcome), ['succeeded', 'failed'])
  assert.equal(page.completes[1].payload.error?.message, '结果没有被接受：manifest 格式不正确')
})

test('the ticker repeats until stopped and a wait gives way to its signal', async () => {
  const { tickerFrom } = await import('../src/creative-scene/host/ticker.ts')
  const due: { ms: number; fn: () => void; cancelled: boolean }[] = []
  const ticker = tickerFrom((ms, fn) => {
    const t = { ms, fn, cancelled: false }
    due.push(t)
    return () => { t.cancelled = true }
  })
  const fire = () => { const t = due.filter((x) => !x.cancelled).at(-1)!; t.cancelled = true; t.fn() }
  let n = 0
  const stop = ticker.every(10_000, () => n++)
  fire()
  fire()
  assert.equal(n, 2)
  stop()
  assert.ok(due.every((t) => t.cancelled), 'stopping cancels the next beat')

  const done = ticker.wait(1000)
  fire()
  await done
  const abort = new AbortController()
  const waiting = ticker.wait(1000, abort.signal)
  abort.abort()
  await assert.rejects(waiting)
  assert.ok(due.at(-1)!.cancelled, 'an aborted wait cancels its timer')
})

// --- render_view framing ---------------------------------------------------

import { PerspectiveCamera, Quaternion, Vector3 } from 'three'
import type { Scene, SceneObject } from '../src/creative-scene/api.ts'
import { framingFor, sceneBounds, type Framing } from '../src/creative-scene/host/framing.ts'

const identity = () => ({ position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] })
const object = (o: Partial<SceneObject> & { id: string; kind: SceneObject['kind'] }): SceneObject =>
  ({ name: o.id, parent_id: null, order: 0, transform: identity(), visible: true, locked: false, ...o }) as SceneObject
const sceneOf = (...objects: SceneObject[]): Scene =>
  ({ environment: { sky: { kind: 'color', color: '#888888' }, ambient: { color: '#ffffff', intensity: 0.4 }, fog: null }, render: { mode: 'clay' }, objects }) as Scene
const ground = object({ id: 'ground', kind: 'primitive', locked: true, primitive: { builtin: 'ground@1', params: { size: 40 } } })
const cube = (id: string, size: number, position = [0, 0, 0]) =>
  object({ id, kind: 'primitive', transform: { ...identity(), position }, primitive: { builtin: 'box@1', params: { width: size, height: size, depth: size } } })
const lens = { focal_mm: 35, sensor_mm: [36, 24], aspect: '3:2', near: 0.1, far: 200, label: '' }
const near = (a: readonly number[], b: readonly number[], eps = 1e-6) => a.every((x, i) => Math.abs(x - b[i]) < eps)

function framed(scene: Scene, request: Parameters<typeof framingFor>[2]): Framing {
  const f = framingFor(scene, {}, request)
  assert.ok(!('error' in f), JSON.stringify(f))
  return f
}

const forwardOf = (f: Framing) => new Vector3(0, 0, -1).applyQuaternion(new Quaternion().fromArray(f.quaternion))
const upOf = (f: Framing) => new Vector3(0, 1, 0).applyQuaternion(new Quaternion().fromArray(f.quaternion))

/** How far into the picture the box reaches: the largest |NDC| over its corners. */
function reach(f: Framing, scene: Scene): number {
  const camera = new PerspectiveCamera(f.fov, f.width / f.height, f.near, f.far)
  camera.position.fromArray(f.position)
  camera.quaternion.fromArray(f.quaternion)
  camera.updateMatrixWorld()
  const { min, max } = sceneBounds(scene, {})
  let most = 0
  for (const x of [min.x, max.x]) for (const y of [min.y, max.y]) for (const z of [min.z, max.z]) {
    const p = new Vector3(x, y, z).project(camera)
    assert.ok(p.z < 1 && p.z > -1, 'in front of the camera')
    most = Math.max(most, Math.abs(p.x), Math.abs(p.y))
  }
  return most
}

test('a scene camera shoots with its own lens and picture shape, from where it stands', () => {
  const rig = object({ id: 'rig', kind: 'group', transform: { ...identity(), position: [2, 0, 0] } })
  const cam = object({ id: 'cam', kind: 'camera', parent_id: 'rig', transform: { ...identity(), position: [0, 1.6, 6] }, camera: lens })
  const f = framed(sceneOf(rig, cam), { camera_object_id: 'cam' })
  assert.ok(near(f.position, [2, -6, 1.6]), `document (2, 1.6, 6) is view (2, -6, 1.6): ${f.position}`)
  assert.ok(near(forwardOf(f).toArray(), [0, 1, 0]), 'an unturned camera looks along document -Z, view +Y')
  assert.ok(near(upOf(f).toArray(), [0, 0, 1]), 'its up is the view’s up')
  assert.ok(Math.abs(f.fov - (2 * Math.atan(12 / 35) * 180) / Math.PI) < 1e-9, 'at 3:2 the long side spans the sensor width: as the sensor height')
  assert.deepEqual([f.width, f.height, f.near, f.far], [1024, 683, 0.1, 200])
  const portrait = framed(sceneOf(object({ id: 'cam', kind: 'camera', camera: { ...lens, aspect: '4:5' } })), { camera_object_id: 'cam' })
  assert.deepEqual([portrait.width, portrait.height], [819, 1024], 'the long side is 1024 either way')
  // A portrait frame's long side is its height: the vertical angle spans the sensor's long side.
  assert.ok(Math.abs(portrait.fov - (2 * Math.atan(18 / 35) * 180) / Math.PI) < 1e-9, 'portrait: vertical angle from the sensor width')
  assert.deepEqual(framingFor(sceneOf(cube('box', 1)), {}, { camera_object_id: 'box' }), { error: '摄像机 box 已不在场景里' })
})

test('a viewpoint looks from its position at its target, 4:3', () => {
  const f = framed(sceneOf(), { viewpoint: { position: [4, 3, 4], target: [0, 1, 0], focal_mm: 50 } })
  assert.ok(near(f.position, [4, -4, 3]))
  assert.ok(near(forwardOf(f).toArray(), new Vector3(-4, 4, -2).normalize().toArray()))
  assert.ok(upOf(f).z > 0, 'level, not rolled')
  assert.deepEqual([f.width, f.height], [1024, 768])
  assert.ok(Math.abs(f.fov - (2 * Math.atan(13.5 / 50) * 180) / Math.PI) < 1e-9, 'full frame at 4:3: the 36 mm width spans the long side')
  const down = framed(sceneOf(), { viewpoint: { position: [0, 10, 0], target: [0, 0, 0], focal_mm: 35 } })
  assert.ok(near(forwardOf(down).toArray(), [0, 0, -1]), 'straight down still has an up')
})

test('presets frame every visible object with a margin, from their own side', () => {
  const box = cube('box', 2, [3, 0, -1])
  const scene = sceneOf(ground, box, object({ id: 'far', kind: 'primitive', visible: false, transform: { ...identity(), position: [90, 0, 0] }, primitive: { builtin: 'box@1', params: {} } }))
  const bounds = sceneBounds(scene, {})
  assert.ok(near(bounds.min.toArray(), [2, 0, 0]) && near(bounds.max.toArray(), [4, 2, 2]), `the ground and hidden objects do not count: ${bounds.min.toArray()} ${bounds.max.toArray()}`)
  const az = (45 * Math.PI) / 180, el = (30 * Math.PI) / 180
  const views = {
    overview: { forward: [-Math.sin(az) * Math.cos(el), Math.cos(az) * Math.cos(el), -Math.sin(el)], up: null },
    top: { forward: [0, 0, -1], up: [0, 1, 0] },
    front: { forward: [0, 1, 0], up: [0, 0, 1] },
    side: { forward: [-1, 0, 0], up: [0, 0, 1] },
  } as const
  for (const [preset, want] of Object.entries(views)) {
    const f = framed(scene, { preset: preset as keyof typeof views })
    assert.ok(near(forwardOf(f).toArray(), want.forward), `${preset}: looks ${forwardOf(f).toArray()}`)
    if (want.up) assert.ok(near(upOf(f).toArray(), want.up), `${preset}: up ${upOf(f).toArray()}`)
    assert.deepEqual([f.width, f.height], [1024, 768])
    assert.ok(Math.abs(f.fov - (2 * Math.atan(13.5 / 35) * 180) / Math.PI) < 1e-9, `${preset}: 35 mm full frame at 4:3`)
    const r = reach(f, scene)
    assert.ok(r <= 1 / 1.1 + 1e-6 && r > 0.85, `${preset}: the box fills the picture up to the margin (${r.toFixed(3)})`)
  }
  const empty = framed(sceneOf(ground), { preset: 'overview' })
  assert.ok(Number.isFinite(empty.position[0]), 'an empty scene still has a view')
})

test('a model counts with its manifest bounds turned and scaled by its instance', () => {
  const instance = object({
    id: 'm', kind: 'model_instance',
    transform: { position: [0, 0, 0], rotation: [0, Math.sin(Math.PI / 4), 0, Math.cos(Math.PI / 4)], scale: [2, 2, 2] },
    model: { content_revision_id: 'ccrv_m' },
  })
  const models = { ccrv_m: { content_id: 'ccnt_m', usable: true, manifest: { bounds: { min: [0, 0, -0.5], max: [3, 1, 0.5] } }, generator: null, name_hint: '' } } as never
  const b = sceneBounds(sceneOf(instance), models)
  // A quarter turn about document Y sends +X to -Z (view +Y); scale 2.
  assert.ok(near(b.min.toArray(), [-1, 0, 0], 1e-9) && near(b.max.toArray(), [1, 6, 2], 1e-9), `${b.min.toArray()} ${b.max.toArray()}`)
})

// --- render_view jobs -------------------------------------------------------

test('a device lost while drawing gives the job back unreported, render or export, and tells the page', async () => {
  for (const job of [renderJob(), exportJob()]) {
    const page = new FakePage()
    page.shot = new DeviceLostError()
    page.exported = new DeviceLostError()
    const out = await new SceneExecutor(page.deps()).run(job)
    assert.deepEqual(out, { state: 'abandoned', reason: '渲染设备丢失' })
    assert.equal(page.completes.length, 0, 'neither failed nor completed: the lease runs out')
    assert.equal(page.uploads.length, 0)
    assert.equal(page.deviceLosses, 1)
  }
})

const renderJob = (request: Record<string, unknown> = { preset: 'overview', mode: 'clay' }): SceneJob =>
  ({ ...bakeJob(), id: 'csj_2', kind: 'render_view', request })

test('a render job uploads the JPEG and reports the revision it shows', async () => {
  const page = new FakePage()
  const out = await new SceneExecutor(page.deps()).run(renderJob())
  assert.equal(out.state, 'completed')
  assert.deepEqual(page.renders, [{ documentID: 'cwdoc_1', request: { preset: 'overview', mode: 'clay' } }])
  assert.deepEqual(page.uploads.map((u) => [u.name, u.mime]), [['view.jpg', 'image/jpeg']])
  assert.deepEqual(page.completes.map((c) => c.payload), [{ claim_token: 'tok_1', outcome: 'succeeded', render: { image_upload_id: 'ccup_1', scene_revision: '7' } }])

  const again = new FakePage()
  again.answers = [unavailable()]
  await new SceneExecutor(again.deps()).run(renderJob())
  assert.deepEqual(again.completes.map((c) => c.payload.render?.image_upload_id), ['ccup_1', 'ccup_2'], 'an unusable screenshot goes up once more')
})

function exportJob(request: Record<string, unknown> = { camera_object_ids: ['camA', 'camB'], passes: ['clay', 'line'], canvas_id: 'ccv_1', scene_node_id: 'cwnode_1' }): SceneJob {
  return { id: 'csj_1', document_id: 'cwdoc_1', kind: 'export_shots', request, attempt: 0, claim_token: 'tok_1', lease_until: '', deadline: '' }
}

test('an export job draws the plan, uploads every image and completes with them per camera', async () => {
  const page = new FakePage()
  const out = await new SceneExecutor(page.deps()).run(exportJob())
  assert.equal(out.state, 'completed')
  assert.deepEqual(page.exports, [{ documentID: 'cwdoc_1', plan: { cameraObjectIDs: ['camA', 'camB'], passes: ['clay', 'line'] } }])
  assert.deepEqual(page.uploads.map((u) => [u.name, u.mime]), [['camA-clay.png', 'image/png'], ['camA-line.png', 'image/png'], ['camB-clay.png', 'image/png'], ['camB-line.png', 'image/png']])
  assert.deepEqual(page.completes.map((c) => c.payload), [{
    claim_token: 'tok_1',
    outcome: 'succeeded',
    export: {
      scene_revision: '9',
      shots: [
        { camera_object_id: 'camA', images: [{ pass: 'clay', upload_id: 'ccup_1' }, { pass: 'line', upload_id: 'ccup_2' }] },
        { camera_object_id: 'camB', images: [{ pass: 'clay', upload_id: 'ccup_3' }, { pass: 'line', upload_id: 'ccup_4' }] },
      ],
    },
  }])

  const again = new FakePage()
  again.answers = [unavailable()]
  await new SceneExecutor(again.deps()).run(exportJob())
  assert.equal(again.uploads.length, 8, 'unusable images go up once more')
  assert.equal(again.completes[1].payload.export?.shots[0].images[0].upload_id, 'ccup_5')
})

test('an export that cannot be drawn or uploaded fails the job and frees what went up', async () => {
  const missing = new FakePage()
  missing.exported = { ok: false, message: '摄像机 camB 已不在场景里' }
  await new SceneExecutor(missing.deps()).run(exportJob())
  assert.deepEqual(missing.completes[0].payload.error, { code: 'render_error', message: '摄像机 camB 已不在场景里' })
  assert.equal(missing.uploads.length, 0)

  // A device that cannot draw the resolution is the page's problem, not the request's (R2 AC-R2-6): failed, not requeued.
  const lost = new FakePage()
  lost.exported = { ok: false, message: '渲染设备在按 4K 出图时丢失，当前设备可能画不了这个分辨率，可以换低一档再导出', hostError: true }
  assert.equal((await new SceneExecutor(lost.deps()).run(exportJob())).state, 'completed')
  assert.equal(lost.deviceLosses, 0, 'not handed back to the queue')
  assert.deepEqual(lost.completes[0].payload.error, { code: 'host_error', message: '渲染设备在按 4K 出图时丢失，当前设备可能画不了这个分辨率，可以换低一档再导出' })

  const broken = new FakePage()
  broken.exported = new Error('WebGL 上下文丢失')
  await new SceneExecutor(broken.deps()).run(exportJob())
  assert.deepEqual(broken.completes[0].payload.error, { code: 'host_error', message: '导出图渲染失败：WebGL 上下文丢失' })

  const failing = new FakePage()
  failing.autoUpload = false
  const running = new SceneExecutor(failing.deps()).run(exportJob())
  await tick()
  failing.uploads[0].gate.resolve({ id: 'ccup_ok' } as Upload)
  failing.uploads[1].gate.reject(new Error('网络中断'))
  await tick()
  for (const u of failing.uploads.slice(2)) u.gate.resolve({ id: 'ccup_late' } as Upload)
  await running
  assert.deepEqual(failing.completes[0].payload.error, { code: 'host_error', message: '导出图上传失败：网络中断' })
  assert.ok(failing.discarded.includes('ccup_ok'), 'the finished upload is freed')

  const malformed = new FakePage()
  await new SceneExecutor(malformed.deps()).run(exportJob({ camera_object_ids: [] }))
  assert.deepEqual(malformed.completes[0].payload.error, { code: 'host_error', message: '任务内容不完整' })
})

test('a render that cannot be taken says why: the request’s problem, or the page’s', async () => {
  const missing = new FakePage()
  missing.shot = { ok: false, message: '摄像机 cam 已不在场景里' }
  await new SceneExecutor(missing.deps()).run(renderJob({ camera_object_id: 'cam' }))
  assert.deepEqual(missing.completes[0].payload.error, { code: 'render_error', message: '摄像机 cam 已不在场景里' })
  assert.equal(missing.uploads.length, 0)

  const broken = new FakePage()
  broken.shot = new Error('WebGL 上下文创建失败')
  await new SceneExecutor(broken.deps()).run(renderJob())
  assert.deepEqual(broken.completes[0].payload.error, { code: 'host_error', message: '截图失败：WebGL 上下文创建失败' })

  const stuck = new FakePage()
  stuck.shot = new Promise<Shot>(() => {})
  const running = new SceneExecutor(stuck.deps()).run(renderJob())
  await tick()
  stuck.ticker.expire()
  await running
  assert.deepEqual(stuck.completes[0].payload.error, { code: 'host_error', message: `截图超过 ${renderTimeoutMs / 1000} 秒没有完成` })

  const lost = new FakePage()
  lost.shot = new Promise<Shot>(() => {})
  const gone = new SceneExecutor(lost.deps()).run(renderJob())
  await tick()
  lost.leaseError = stateConflict()
  lost.ticker.beat()
  assert.deepEqual(await gone, { state: 'lost' })
  assert.equal(lost.completes.length, 0)
})

test('peek hands over a loaded model at once, null for a failed one, nothing before it loads', async () => {
  const { ModelCache } = await import('../src/creative-scene/models/cache.ts')
  const { readFileSync } = await import('node:fs')
  const bytes = readFileSync(new URL('../../backend/internal/creativeglb/testdata/exported-template.glb', import.meta.url))
  const glb = bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer
  const cache = new ModelCache((rev) => (rev === 'ccrv_ok' ? Promise.resolve(glb) : Promise.reject(new Error('404'))))
  assert.equal(cache.peek('ccrv_ok'), undefined)
  const parts = await cache.load('ccrv_ok')
  assert.equal(cache.peek('ccrv_ok'), parts)
  await assert.rejects(cache.load('ccrv_gone'))
  assert.equal(cache.peek('ccrv_gone'), null)
  cache.clear()
  assert.equal(cache.peek('ccrv_ok'), undefined, 'cleared with the editor')
})

// --- the page's claim loop -------------------------------------------------

import { recoverEveryMs, SceneJobLauncher, type LauncherDeps, type PageRunner } from '../src/creative-scene/host/launcher.ts'
import { LeaveGuard } from '../src/creative-scene/host/leaveGuard.ts'

class FakeHost {
  ticker = new ManualTicker()
  queue: SceneJob[] = []
  claims: (string | undefined)[] = []
  claimGate: ReturnType<typeof deferred<void>> | null = null
  leases: string[] = []
  completes: Call[] = []
  ran: string[] = []
  runGate: ReturnType<typeof deferred<void>> | null = null
  loads = 0
  loadGate: ReturnType<typeof deferred<void>> | null = null
  loadFails = false
  disposed = 0
  capable = true
  probes = 0
  recovers = 0
  recoverable = false
  clock = 0
  claimError: unknown = null
  deps(): LauncherDeps {
    return {
      ticker: this.ticker,
      probe: async () => (this.probes++, this.capable),
      recover: async () => (this.recovers++, this.recoverable),
      now: () => this.clock,
      load: async () => {
        this.loads++
        if (this.loadGate) await this.loadGate.promise
        if (this.loadFails) throw new Error('Failed to fetch dynamically imported module')
        const runner: PageRunner = {
          run: async (job) => { this.ran.push(job.id); if (this.runGate) await this.runGate.promise },
          dispose: () => { this.disposed++ },
        }
        return runner
      },
      api: {
        claim: async ({ documentID }) => {
          this.claims.push(documentID)
          if (this.claimGate) await this.claimGate.promise
          if (this.claimError) throw this.claimError
          const i = this.queue.findIndex((j) => !documentID || j.document_id === documentID)
          return i < 0 ? null : this.queue.splice(i, 1)[0]
        },
        lease: async (jobID) => { this.leases.push(jobID) },
        complete: async (_id, operation, _at, payload) => { this.completes.push({ operation, payload }); return { job_id: _id, state: 'failed', result: {} } },
      },
    }
  }
}

const queued = (id: string, documentID = 'cwdoc_1', attempt = 0): SceneJob => ({ ...bakeJob(), id, document_id: documentID, attempt })
const settle = async () => { for (let i = 0; i < 10; i++) await tick() }

test('the launcher claims until the queue is empty, one job at a time, loading the executor once', async () => {
  const host = new FakeHost()
  host.queue = [queued('csj_1'), queued('csj_2')]
  const launcher = new SceneJobLauncher(host.deps())
  launcher.start()
  await settle()
  assert.deepEqual(host.ran, ['csj_1', 'csj_2'])
  assert.equal(host.loads, 1)
  assert.equal(host.claims.length, 3, 'a third claim finds nothing')
  assert.equal(launcher.store.getState().holding, null)
})

test('nudges while busy become one more claim, not a second loop', async () => {
  const host = new FakeHost()
  host.queue = [queued('csj_1')]
  host.runGate = deferred<void>()
  const launcher = new SceneJobLauncher(host.deps())
  launcher.start()
  await settle()
  assert.deepEqual(launcher.store.getState().holding, { id: 'csj_1', documentID: 'cwdoc_1', kind: 'bake_model' })
  host.queue.push(queued('csj_2'))
  launcher.poke()
  launcher.poke()
  await settle()
  assert.equal(host.claims.length, 1, 'no claim while a job runs')
  host.runGate.resolve()
  await settle()
  assert.deepEqual(host.ran, ['csj_1', 'csj_2'])
})

test('canvas notifications are throttled to one claim per 500 ms', async () => {
  const host = new FakeHost()
  const due: (() => void)[] = []
  host.ticker.after = (_ms: number, fn: () => void) => { due.push(fn); return () => {} }
  const launcher = new SceneJobLauncher(host.deps())
  launcher.start()
  await settle()
  const before = host.claims.length
  launcher.invalidated()
  launcher.invalidated()
  launcher.invalidated()
  assert.equal(due.length, 1)
  due[0]()
  await settle()
  assert.equal(host.claims.length, before + 1)
})

test('the open editor’s scene is claimed first', async () => {
  const host = new FakeHost()
  host.queue = [queued('csj_other', 'cwdoc_other'), queued('csj_mine', 'cwdoc_mine')]
  const launcher = new SceneJobLauncher(host.deps())
  const unprefer = launcher.prefer('cwdoc_mine')
  launcher.start()
  await settle()
  assert.deepEqual(host.ran, ['csj_mine', 'csj_other'])
  assert.equal(host.claims[0], 'cwdoc_mine')
  unprefer()
  host.queue = [queued('csj_3')]
  launcher.poke()
  await settle()
  assert.equal(host.claims.at(-2), undefined, 'without the editor, the account’s oldest')
})

test('a device that fails the probe claims nothing', async () => {
  const host = new FakeHost()
  host.capable = false
  host.queue = [queued('csj_1')]
  const launcher = new SceneJobLauncher(host.deps())
  launcher.start()
  launcher.poke()
  await settle()
  assert.equal(host.claims.length, 0)
  assert.equal(launcher.store.getState().capable, false)
})

test('the probe is asked once however often the page is nudged, and claiming waits for its answer', async () => {
  const host = new FakeHost()
  host.queue = [queued('csj_1')]
  const launcher = new SceneJobLauncher(host.deps())
  launcher.start()
  launcher.poke()
  launcher.poke()
  assert.equal(host.claims.length, 0, 'not before the probe answers')
  await settle()
  launcher.poke()
  await settle()
  assert.equal(host.probes, 1)
  assert.ok(host.claims.length > 0)
})

test('the lease is renewed while the executor loads; a chunk that will not load fails the job and stops claiming', async () => {
  const host = new FakeHost()
  host.queue = [queued('csj_1'), queued('csj_2')]
  host.loadGate = deferred<void>()
  host.loadFails = true
  const launcher = new SceneJobLauncher(host.deps())
  launcher.start()
  await settle()
  host.ticker.beat()
  assert.deepEqual(host.leases, ['csj_1'], 'renewed during the load')
  host.loadGate.resolve()
  await settle()
  assert.equal(host.completes.length, 1)
  assert.equal(host.completes[0].payload.error?.code, 'host_error')
  assert.match(host.completes[0].payload.error?.message ?? '', /页面没能加载渲染模块/)
  assert.equal(launcher.store.getState().capable, false)
  assert.equal(host.queue.length, 1, 'the next job is left for another page')
})

test('taking over an interrupted job is said once; a claim error waits for the next nudge', async () => {
  const host = new FakeHost()
  host.queue = [queued('csj_1', 'cwdoc_1', 1)]
  host.runGate = deferred<void>()
  const launcher = new SceneJobLauncher(host.deps())
  launcher.start()
  await settle()
  assert.equal(launcher.store.getState().resumed, 'csj_1')
  host.runGate.resolve()
  await settle()

  const offline = new FakeHost()
  offline.claimError = new TypeError('Failed to fetch')
  const quiet = new SceneJobLauncher(offline.deps())
  quiet.start()
  await settle()
  assert.equal(offline.claims.length, 1, 'no retry loop')
  offline.claimError = null
  offline.queue = [queued('csj_1')]
  quiet.poke()
  await settle()
  assert.deepEqual(offline.ran, ['csj_1'])
})

test('leaving the page lets a running job finish, then frees the executor', async () => {
  const host = new FakeHost()
  host.queue = [queued('csj_1'), queued('csj_2')]
  host.runGate = deferred<void>()
  const launcher = new SceneJobLauncher(host.deps())
  const stop = launcher.start()
  await settle()
  stop()
  assert.equal(host.disposed, 0, 'not while csj_1 runs')
  host.runGate.resolve()
  await settle()
  assert.deepEqual(host.ran, ['csj_1'], 'nothing more is claimed')
  assert.equal(host.disposed, 1)
})

// --- closing the page while it holds a job ---------------------------------

class FakeBroadcast {
  static channels: FakeBroadcast[] = []
  onmessage: ((e: MessageEvent) => void) | null = null
  closed = false
  constructor() { FakeBroadcast.channels.push(this) }
  postMessage(m: unknown) { for (const c of FakeBroadcast.channels) if (c !== this && !c.closed) c.onmessage?.({ data: m } as MessageEvent) }
  close() { this.closed = true }
}

class FakeWindow {
  listeners = new Map<string, Set<unknown>>()
  addEventListener(type: string, fn: unknown) { if (!this.listeners.has(type)) this.listeners.set(type, new Set()); this.listeners.get(type)!.add(fn) }
  removeEventListener(type: string, fn: unknown) { this.listeners.get(type)?.delete(fn) }
  asks() { return (this.listeners.get('beforeunload')?.size ?? 0) > 0 }
}

test('closing asks first only while holding a job in the browser’s only workspace tab', () => {
  FakeBroadcast.channels = []
  const open = () => new FakeBroadcast() as never
  const a = new FakeWindow(), b = new FakeWindow()
  const first = new LeaveGuard(a as never, open)
  const stopFirst = first.start()
  first.setHolding(true)
  assert.ok(a.asks(), 'alone and holding: asks')
  const second = new LeaveGuard(b as never, open)
  const stopSecond = second.start()
  assert.ok(!first.alone && !second.alone, 'the tabs found each other')
  assert.ok(!a.asks(), 'another tab will take the job over')
  stopSecond()
  assert.ok(first.alone && a.asks(), 'the other tab said goodbye')
  first.setHolding(false)
  assert.ok(!a.asks())
  stopFirst()
})

// --- the editor's view of the assistant's work -----------------------------

const objA = 'sobj_' + 'a'.repeat(21)
const objB = 'sobj_' + 'b'.repeat(21)
const objC = 'sobj_' + 'c'.repeat(21)
const objD = 'sobj_' + 'd'.repeat(21)

function jobSummary(id: string, kind: SceneJobSummary['kind'], target: SceneJobSummary['target']): SceneJobSummary {
  return { id, kind, state: 'queued', run_id: 'car_1', created_at: '2026-10-18T00:00:00Z', target }
}

function revisionItem(revision: string, actor: RevisionItem['actor_kind'], createdAt: number, changed: string[], annotated: string[] = []): RevisionItem {
  return { revision, actor_kind: actor, run_id: actor === 'agent' ? 'car_1' : null, restored_from: null, changed_units: changed, annotated_units: annotated, removed_units: [], title: '', created_at: new Date(createdAt).toISOString() }
}

class Clock {
  now = Date.parse('2026-10-18T08:00:00Z')
  timers: { at: number; ms: number; fn: () => void; live: boolean }[] = []
  after = (ms: number, fn: () => void) => {
    const t = { at: this.now + ms, ms, fn, live: true }
    this.timers.push(t)
    return () => { t.live = false }
  }
  get pending() { return this.timers.filter((t) => t.live) }
  async advance(ms: number) {
    this.now += ms
    for (const t of this.timers) if (t.live && t.at <= this.now) { t.live = false; t.fn() }
    for (let i = 0; i < 5; i++) await tick()
  }
}

class FakeActivityServer {
  clock = new Clock()
  jobs: SceneJobSummary[] | Error = []
  revisions: RevisionItem[] = []
  reads: (string | null)[] = []
  listener: (() => void) | null = null
  deps: ActivityDeps = {
    listJobs: async () => {
      if (this.jobs instanceof Error) throw this.jobs
      return this.jobs
    },
    readRevisions: async (_id, since) => {
      this.reads.push(since)
      return this.revisions.filter((r) => since === null || Number(r.revision) > Number(since)).sort((a, b) => Number(b.revision) - Number(a.revision))
    },
    now: () => this.clock.now,
    after: this.clock.after,
    subscribe: (listener) => {
      this.listener = listener
      return () => { this.listener = null }
    },
  }
}

test('outlines, placeholders and the status line come from the jobs and the assistant’s last revisions', () => {
  const transform = { position: [2, 0, 1], rotation: [0, 0, 0, 1], scale: [1, 1, 1] }
  const jobs = [
    jobSummary('csj_1', 'bake_model', { op: 'replace', object_ids: [objA, objB] }),
    jobSummary('csj_2', 'bake_model', { op: 'add', object: { name: '椅子', parent_id: null, transform } }),
    jobSummary('csj_3', 'render_view', null),
  ]
  const state = { jobs, recent: [{ revision: '9', at: 0, objects: [objC] }] }
  assert.deepEqual([...highlightedObjects(state)].sort(), [objA, objB, objC].sort())
  assert.deepEqual(jobPlaceholders(jobs), [{ id: 'csj_2', name: '椅子', parentID: null, transform }])
  // A target written without a transform still has a place.
  assert.deepEqual(jobPlaceholders([jobSummary('csj_4', 'bake_model', { op: 'add', object: { name: '' } })])[0].transform.position, [0, 0, 0])
  const byID = new Map([[objA, { id: objA, name: '台灯' } as SceneObject]])
  assert.equal(activityText([jobs[0]], byID), '助手正在生成「台灯」…')
  assert.equal(activityText([jobs[1]], byID), '助手正在生成「椅子」…')
  assert.equal(activityText(jobs, byID), '助手正在生成「台灯」等 2 个模型…')
  assert.equal(activityText([jobs[2]], byID), '')
  assert.deepEqual(revisionObjects(revisionItem('3', 'agent', 0, ['objects', `object:${objA}`, `object:${objA}:content`, 'environment'], [`object:${objB}`])), [objA, objB])
  assert.deepEqual(
    editedObjects([
      { op: 'update_objects', updates: [{ object_id: objA, name: 'x' }] },
      { op: 'remove_objects', object_ids: [objB] },
      { op: 'restore_units', units: [`object:${objC}:content`, 'objects'] },
    ]).sort(),
    [objA, objB, objC].sort(),
  )
})

test('the editor reads on opening, then at most once a second after notifications, asking only for newer revisions', async () => {
  const s = new FakeActivityServer()
  const now = s.clock.now
  s.revisions = [
    revisionItem('4', 'agent', now - 120_000, [`object:${objB}`]),
    revisionItem('5', 'agent', now - 10_000, [`object:${objA}`]),
    revisionItem('6', 'photographer', now, [`object:${objC}`]),
  ]
  s.jobs = [jobSummary('csj_1', 'bake_model', { op: 'replace', object_ids: [objD] })]
  const activity = new AgentActivity('cwdoc_1', s.deps)
  const stop = activity.start()
  await s.clock.advance(0)
  assert.deepEqual(s.reads, [null])
  // The first page is dated by the server: two minutes ago is over, ten seconds ago is not.
  assert.deepEqual([...highlightedObjects(activity.store.getState())].sort(), [objA, objD].sort())

  s.listener?.()
  s.listener?.()
  assert.equal(s.clock.pending.filter((t) => t.ms === 1_000).length, 1)
  // A revision whose server time looks old to this machine is still new when it arrives.
  s.revisions.push(revisionItem('7', 'agent', now - 600_000, [`object:${objC}`]))
  s.jobs = []
  await s.clock.advance(1_000)
  assert.deepEqual(s.reads, [null, '6'])
  assert.deepEqual([...highlightedObjects(activity.store.getState())].sort(), [objA, objC].sort())

  // Each outline lasts a minute from when it was seen.
  await s.clock.advance(49_000)
  assert.deepEqual([...highlightedObjects(activity.store.getState())], [objC])
  await s.clock.advance(11_000)
  assert.deepEqual([...highlightedObjects(activity.store.getState())], [])

  // A failed job list keeps what was shown.
  s.jobs = [jobSummary('csj_2', 'bake_model', { op: 'replace', object_ids: [objA] })]
  s.listener?.()
  await s.clock.advance(1_000)
  s.jobs = new Error('offline')
  s.listener?.()
  await s.clock.advance(1_000)
  assert.deepEqual([...highlightedObjects(activity.store.getState())], [objA])

  stop()
  assert.equal(s.listener, null)
  assert.equal(s.clock.pending.length, 0)
})

// --- the panel: screenshots and the run's change list ----------------------

test('undoing a run sends its batches in order and stops at the first conflict', async () => {
  const batch = (from: string): RestoreBatch => ({ from_revision: from, units: [`object:${objA}`], expected: { [`object:${objA}`]: '5' } })
  const sent: string[] = []
  const conflict = new ApiError(409, 'creative_document_conflict', '文档已被修改')
  const send = (fail: string | null) => async (b: RestoreBatch) => {
    sent.push(b.from_revision)
    if (b.from_revision === fail) throw conflict
    return { revision: String(10 + sent.length) }
  }
  let out = await submitBatches([batch('1'), batch('3'), batch('4')], send('3'))
  assert.deepEqual(sent, ['1', '3'])
  assert.deepEqual(out, { written: ['11'], total: 3, conflict: true, error: null })
  assert.equal(outcomeText(out), '撤销了一部分：其余的地方刚被修改，没有覆盖，清单已重新读取')
  sent.length = 0
  out = await submitBatches([batch('1')], send(null))
  assert.equal(outcomeText(out), '已撤销本轮的改动')
  assert.equal(outcomeText(out, '椅子'), '已恢复「椅子」')
  sent.length = 0
  out = await submitBatches([batch('1')], send('1'))
  assert.equal(outcomeText(out, '椅子'), '「椅子」刚被修改，没有覆盖，清单已重新读取')
  out = await submitBatches([batch('1')], async () => { throw new Error('网络中断') })
  assert.deepEqual([out.conflict, out.error], [false, '网络中断'])
  assert.equal(outcomeText(out), '没有撤销：网络中断')
  assert.deepEqual(restorePayload(batch('1')), { expected: { [`object:${objA}`]: '5' }, operations: [{ op: 'restore_units', from_revision: '1', units: [`object:${objA}`] }] })
})

test('the panel reads a tool block once: the scene from its arguments, the screenshot from its result', async () => {
  const reads: string[] = []
  const detail = (result?: string): Detail => ({ block_id: 'b', step_id: 's', tool_key: 'render_view', arguments: { document_id: 'cwdoc_1', preset: 'overview' }, ...(result === undefined ? {} : { result }) })
  const read = async <T,>(path: string) => {
    reads.push(path)
    return detail(JSON.stringify({ image_revision_id: 'ccrv_shot', content_revision_id: 'ccrv_shot', scene_revision: 3 })) as T
  }
  const block = { id: 'ccbk_shot', run_id: 'car_1' }
  const first = await finishedDetail(block, read)
  await finishedDetail(block, read)
  assert.deepEqual(reads, ['/agent-runs/car_1/blocks/ccbk_shot/detail'])
  assert.equal(screenshotRevision(first), 'ccrv_shot')
  assert.equal(documentOf(first), 'cwdoc_1')
  assert.equal(screenshotRevision(detail('截图失败')), null)
  assert.equal(screenshotRevision(detail()), null)
  // A failed read is tried again.
  let fail = true
  const flaky = async <T,>(path: string) => {
    reads.push(path)
    if (fail) throw new Error('offline')
    return detail() as T
  }
  await assert.rejects(finishedDetail({ id: 'ccbk_x', run_id: 'car_1' }, flaky))
  fail = false
  assert.equal((await finishedDetail({ id: 'ccbk_x', run_id: 'car_1' }, flaky)).tool_key, 'render_view')
  assert.equal(blockedText({ reason: 'changed_by_others', revision: '7', actor_kind: 'photographer' }), '之后你又改过（第 7 版）')
  assert.equal(blockedText({ reason: 'changed_by_others', revision: '8', actor_kind: 'agent' }), '之后助手的其他运行又改过（第 8 版）')
  assert.equal(blockedText({ reason: 'restored_whole', revision: '9', actor_kind: 'photographer' }), '之后整份恢复过（第 9 版）')
  assert.equal(blockedText({ reason: 'hierarchy_not_restorable', revision: '4', actor_kind: 'agent' }), '层级调整不能单独恢复，可以在历史版本里整份恢复')
})

test('a run parked on a page job says what it waits for', () => {
  const view = (wait: string | undefined) =>
    ({ run: { state: 'waiting_client', wait_reason: wait }, blocks: [{ id: 'b', type: 'tool', hidden: false, finished_at: null, payload: { title: '生成模型' } }] }) as unknown as RunView
  assert.equal(currentStep(view('client_job')), '等待页面执行')
  assert.equal(currentStep(view('slot_busy')), '等待其他助手运行结束')
})

test('a run started from the scene editor carries its scene and at most 50 selected objects; elsewhere none', () => {
  const message = { model: 'vision', imageInput: true, revisionIDs: [], segments: [{ type: 'text', text: '改成木头的' }] } as Parameters<typeof messageSteps>[1]
  const runStep = (scene?: Parameters<typeof messageSteps>[1]['scene']) => messageSteps('c', { ...message, scene }).find((s) => s.path.endsWith('/runs'))!
  assert.equal('scene_context' in runStep().payload, false)
  assert.equal('scene_context' in runStep(null).payload, false)
  const many = Array.from({ length: 60 }, (_, i) => `sobj_${String(i).padStart(21, '0')}`)
  assert.deepEqual(runStep({ documentID: 'cwdoc_1', selected: many }).payload.scene_context, { document_id: 'cwdoc_1', selected_object_ids: many.slice(0, 50) })
})

test('after a device loss the page claims nothing until a wake 10 seconds on finds a device', async () => {
  const host = new FakeHost()
  host.queue = [queued('csj_1'), queued('csj_2'), queued('csj_3')]
  const launcher = new SceneJobLauncher(host.deps())
  launcher.start()
  launcher.deviceLost()
  launcher.poke()
  await settle()
  assert.equal(host.claims.length, 0)
  assert.equal(host.recovers, 0, 'not tried within 10 seconds')
  host.clock = recoverEveryMs
  launcher.poke()
  await settle()
  assert.equal(host.recovers, 1)
  assert.equal(host.claims.length, 0, 'still no device')
  host.clock += recoverEveryMs / 2
  launcher.poke()
  await settle()
  assert.equal(host.recovers, 1, 'the next try waits another 10 seconds')
  host.clock += recoverEveryMs
  host.recoverable = true
  launcher.poke()
  await settle()
  assert.equal(host.recovers, 2)
  assert.ok(host.claims.length > 0, 'claiming again')
})
