import { ApiError } from '../../api/transport.ts'
import type { Upload } from '../../creative-canvas/editor/media.ts'
import type { CompileInput } from '../sandbox/host.ts'
import { isRecord, type CompileError } from '../sandbox/protocol.ts'
import { bake, uploadBake, type Baked, type BakeDeps, type BakeOutcome } from './bake.ts'
import type { CompletePayload, Completion, JobApi, JobErrorCode, SceneJob } from './jobs.ts'
import type { Ticker } from './ticker.ts'

// Runs one claimed scene job to its end: renews the lease while working,
// does the work, reports the result. What the server answers decides what
// happens to the artifacts: a recorded result keeps them (a conflicted bake
// is retried from them), a lost lease or a job ended elsewhere frees them.
// A page that crashes or closes reports nothing; the lease runs out and the
// job goes back to the queue, at most three claims in all.

export type ExecutorDeps = BakeDeps & {
  api: JobApi
  ticker: Ticker
  /** Releases a ready staged upload that will never be committed. */
  discard(upload: Upload): Promise<void>
}

export type JobOutcome =
  /** The server recorded a result, success or failure. */
  | { state: 'completed'; completion: Completion }
  /** The lease was lost or the job ended elsewhere; the artifacts were freed. */
  | { state: 'lost' }
  /** Nothing could be reported; the lease will run out and the job go back to the queue. */
  | { state: 'abandoned'; reason: string }

export const leaseEveryMs = 10_000
const completeAttempts = 3
const maxErrorChars = 4000

type Hold = { signal: AbortSignal; lost(): boolean; release(): void }
type Reported = JobOutcome | { state: 'unavailable' }

export class SceneExecutor {
  private readonly deps: ExecutorDeps

  constructor(deps: ExecutorDeps) {
    this.deps = deps
  }

  async run(job: SceneJob): Promise<JobOutcome> {
    const hold = this.hold(job)
    try {
      if (job.kind === 'bake_model') return await this.bakeModel(job, hold)
      return { state: 'abandoned', reason: `这个页面不能执行 ${job.kind} 任务` }
    } finally {
      hold.release()
    }
  }

  /** Renews the lease until released; a refused renewal means the job is no longer this page's. */
  private hold(job: SceneJob): Hold {
    const abort = new AbortController()
    let renewing = false
    const stop = this.deps.ticker.every(leaseEveryMs, () => {
      if (renewing || abort.signal.aborted) return
      renewing = true
      this.deps.api
        .lease(job.id, job.claim_token)
        .catch((err: unknown) => {
          // A network failure is retried on the next beat; the lease has 30 seconds.
          if (err instanceof ApiError && (err.status === 409 || err.status === 404)) abort.abort()
        })
        .finally(() => {
          renewing = false
        })
    })
    return { signal: abort.signal, lost: () => abort.signal.aborted, release: stop }
  }

  private async bakeModel(job: SceneJob, hold: Hold): Promise<JobOutcome> {
    const input = bakeInput(job.request)
    if (!input) return this.fail(job, 'host_error', '任务内容不完整')
    const out = await bake(this.deps, input, hold.signal, { live: () => !hold.lost() })
    if (!out.ok) {
      this.discard(out.uploads)
      if (out.stopped) return { state: 'lost' }
      const [code, message] = bakeFailure(out)
      return this.fail(job, code, message)
    }
    let { baked, uploads } = out
    for (let resent = false; ; resent = true) {
      const reported = await this.report(job, { claim_token: job.claim_token, outcome: 'succeeded', bake: bakeResult(baked) })
      if (reported.state === 'lost' || reported.state === 'abandoned') this.discard(uploads)
      if (reported.state !== 'unavailable') return reported
      // The staged copies are gone (expired or used); the lease still holds, so send them once more.
      if (hold.lost()) return { state: 'lost' }
      if (resent) return this.fail(job, 'host_error', '模型文件上传后无法使用')
      const again = await uploadBake(this.deps, baked.glb, baked.png, hold.signal)
      if (hold.lost()) {
        this.discard(again.uploads)
        return { state: 'lost' }
      }
      if (!again.ok) {
        this.discard(again.uploads)
        return this.fail(job, 'host_error', `模型文件上传失败：${reason(again.cause)}`)
      }
      uploads = again.uploads
      baked = { ...baked, model: again.uploads[0], thumbnail: again.uploads[1] }
    }
  }

  private async fail(job: SceneJob, code: JobErrorCode, message: string): Promise<JobOutcome> {
    const reported = await this.report(job, {
      claim_token: job.claim_token,
      outcome: 'failed',
      error: { code, message: clip(message) },
    })
    return reported.state === 'unavailable' ? { state: 'abandoned', reason: '提交失败结果时上传不可用' } : reported
  }

  /**
   * Sends the result under one operation ID, so a retry after a lost
   * response replays the recorded receipt instead of completing twice.
   */
  private async report(job: SceneJob, payload: CompletePayload): Promise<Reported> {
    const operation = crypto.randomUUID()
    const createdAt = new Date().toISOString()
    for (let attempt = 1; ; attempt++) {
      try {
        return { state: 'completed', completion: await this.deps.api.complete(job.id, operation, createdAt, payload) }
      } catch (err) {
        if (err instanceof ApiError) {
          if (err.code === 'creative_upload_unavailable') return { state: 'unavailable' }
          if (err.status === 409 || err.status === 404) return { state: 'lost' }
          // The server refused this result's shape; tell the assistant rather than let the job loop.
          if (err.status < 500 && payload.outcome === 'succeeded') return this.fail(job, 'host_error', `结果没有被接受：${err.message}`)
          if (err.status < 500) return { state: 'abandoned', reason: err.message }
        }
        if (attempt >= completeAttempts) return { state: 'abandoned', reason: reason(err) }
        await this.deps.ticker.wait(1000 * 2 ** (attempt - 1)).catch(() => {})
      }
    }
  }

  private discard(uploads: Upload[]) {
    for (const u of uploads) void this.deps.discard(u).catch(() => {})
  }
}

function bakeInput(request: Record<string, unknown>): CompileInput | null {
  const { code, params, lib_version: libVersion } = request
  if (typeof code !== 'string' || typeof libVersion !== 'number') return null
  if (params !== null && params !== undefined && !isRecord(params)) return null
  return { code, params: params ?? {}, libVersion }
}

function bakeResult(baked: Baked): NonNullable<CompletePayload['bake']> {
  const { mount, params_schema, params } = baked.module
  return {
    glb_upload_id: baked.model.id,
    thumbnail_upload_id: baked.thumbnail.id,
    manifest: baked.manifest,
    module: { mount, params_schema, params },
  }
}

/**
 * What a failed bake tells the assistant. Its code is the assistant's to
 * fix; the page's own failures (sandbox not starting, WebGL, uploads) are
 * host errors.
 */
export function bakeFailure(out: Extract<BakeOutcome, { ok: false; stopped?: false }>): [JobErrorCode, string] {
  if (out.stage === 'thumbnail') return ['host_error', `缩略图生成失败：${reason(out.cause)}`]
  if (out.stage === 'upload') return ['host_error', `模型文件上传失败：${reason(out.cause)}`]
  return compileFailure(out.error)
}

export function compileFailure(error: CompileError): [JobErrorCode, string] {
  if (error.host) return ['host_error', error.message]
  const at = error.line === undefined ? '' : `第 ${error.line} 行${error.column === undefined ? '' : `第 ${error.column} 列`}：`
  const code: JobErrorCode = error.kind === 'timeout' ? 'compile_timeout' : error.kind === 'limit' ? 'limit_exceeded' : 'compile_error'
  return [code, at + error.message]
}

function reason(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

function clip(message: string): string {
  return message.length > maxErrorChars ? message.slice(0, maxErrorChars - 1) + '…' : message
}
