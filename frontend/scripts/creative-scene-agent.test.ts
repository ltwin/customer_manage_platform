import test from 'node:test'
import assert from 'node:assert/strict'
import { ApiError } from '../src/api/transport.ts'
import type { Upload } from '../src/creative-canvas/editor/media.ts'
import { bakeFailure, compileFailure, leaseEveryMs, SceneExecutor, type ExecutorDeps } from '../src/creative-scene/host/executor.ts'
import type { CompletePayload, Completion, SceneJob } from '../src/creative-scene/host/jobs.ts'
import type { Ticker } from '../src/creative-scene/host/ticker.ts'
import type { CompileResult } from '../src/creative-scene/sandbox/host.ts'
import type { Manifest } from '../src/creative-scene/sandbox/inspect.ts'

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
  after = () => () => {}
  wait = (ms: number) => { this.waits.push(ms); return Promise.resolve() }
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
  uploads: { name: string; signal: AbortSignal; gate: ReturnType<typeof deferred<Upload>> }[] = []
  autoUpload = true
  discarded: string[] = []
  leases: string[] = []
  leaseError: unknown = null
  completes: Call[] = []
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
      upload: (file, _declared, signal) => {
        const gate = deferred<Upload>()
        this.uploads.push({ name: file.name, signal, gate })
        if (this.autoUpload) gate.resolve({ id: `ccup_${++this.n}` } as Upload)
        return gate.promise
      },
      discard: (u) => { this.discarded.push(u.id); return Promise.resolve() },
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
