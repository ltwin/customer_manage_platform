import { ApiError } from '../../api/transport.ts'
import type { Upload } from '../../creative-canvas/editor/media.ts'
import type { CompileInput } from '../sandbox/host.ts'
import { isRecord, type CompileError } from '../sandbox/protocol.ts'
import { bake, uploadBake, type Baked, type BakeDeps, type BakeOutcome } from './bake.ts'
import type { RenderRequest } from './framing.ts'
import { leaseEveryMs, type CompletePayload, type Completion, type JobApi, type JobErrorCode, type SceneJob } from './jobs.ts'
import type { Ticker } from './ticker.ts'

// Runs one claimed scene job to its end: renews the lease while working,
// does the work, reports the result. What the server answers decides what
// happens to the artifacts: a recorded result keeps them (a conflicted bake
// is retried from them), a lost lease or a job ended elsewhere frees them.
// A page that crashes or closes reports nothing; the lease runs out and the
// job goes back to the queue, at most three claims in all.

/** A screenshot of the confirmed scene and the revision it shows, or why there is none. */
export type Shot = { ok: true; image: Blob; revision: string } | { ok: false; message: string }

export type ExecutorDeps = BakeDeps & {
  api: JobApi
  ticker: Ticker
  /** Releases a ready staged upload that will never be committed. */
  discard(upload: Upload): Promise<void>
  render(documentID: string, request: RenderRequest): Promise<Shot>
}

type Sent = { ok: true; uploads: Upload[] } | { ok: false; cause: unknown; uploads: Upload[] }

export type JobOutcome =
  /** The server recorded a result, success or failure. */
  | { state: 'completed'; completion: Completion }
  /** The lease was lost or the job ended elsewhere; the artifacts were freed. */
  | { state: 'lost' }
  /** Nothing could be reported; the lease will run out and the job go back to the queue. */
  | { state: 'abandoned'; reason: string }

/** A screenshot that has not come back by then will not; the job is the page's failure. */
export const renderTimeoutMs = 60_000
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
      if (job.kind === 'render_view') return await this.renderView(job, hold)
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
    const { baked } = out
    return this.deliver(job, hold, '模型文件', out.uploads, ([model, thumbnail]) => ({ bake: bakeResult({ ...baked, model, thumbnail }) }), () =>
      uploadBake(this.deps, baked.glb, baked.png, hold.signal),
    )
  }

  private async renderView(job: SceneJob, hold: Hold): Promise<JobOutcome> {
    const rendering = this.deps.render(job.document_id, job.request as RenderRequest)
    rendering.catch(() => {}) // still awaited below unless the timer wins
    const timer = new AbortController()
    const stopTimer = () => timer.abort()
    hold.signal.addEventListener('abort', stopTimer, { once: true })
    let shot: Shot | 'timeout' | 'stopped'
    try {
      shot = await Promise.race([rendering, this.deps.ticker.wait(renderTimeoutMs, timer.signal).then(() => 'timeout' as const, () => 'stopped' as const)])
    } catch (err) {
      if (hold.lost()) return { state: 'lost' }
      return this.fail(job, 'host_error', `截图失败：${reason(err)}`)
    } finally {
      timer.abort()
      hold.signal.removeEventListener('abort', stopTimer)
    }
    if (hold.lost() || shot === 'stopped') return { state: 'lost' }
    if (shot === 'timeout') return this.fail(job, 'host_error', '截图超过 60 秒没有完成')
    if (!shot.ok) return this.fail(job, 'render_error', shot.message)
    const { image, revision } = shot
    const send = (): Promise<Sent> => uploadOne(this.deps, new File([image], 'view.jpg', { type: 'image/jpeg' }), hold.signal)
    const sent = await send()
    if (hold.lost()) {
      this.discard(sent.uploads)
      return { state: 'lost' }
    }
    if (!sent.ok) return this.fail(job, 'host_error', `截图上传失败：${reason(sent.cause)}`)
    return this.deliver(job, hold, '截图', sent.uploads, ([upload]) => ({ render: { image_upload_id: upload.id, scene_revision: revision } }), send)
  }

  /**
   * Reports a success built from staged uploads. When the server finds them
   * unusable (expired or used) and the lease still holds, they go up once
   * more; anything that ends the job here frees them.
   */
  private async deliver(
    job: SceneJob,
    hold: Hold,
    what: string,
    uploads: Upload[],
    result: (uploads: Upload[]) => Pick<CompletePayload, 'bake' | 'render'>,
    resend: () => Promise<Sent>,
  ): Promise<JobOutcome> {
    for (let resent = false; ; resent = true) {
      const reported = await this.report(job, { claim_token: job.claim_token, outcome: 'succeeded', ...result(uploads) })
      if (reported.state === 'lost' || reported.state === 'abandoned') this.discard(uploads)
      if (reported.state !== 'unavailable') return reported
      if (hold.lost()) return { state: 'lost' }
      if (resent) return this.fail(job, 'host_error', `${what}上传后无法使用`)
      const again = await resend()
      if (hold.lost()) {
        this.discard(again.uploads)
        return { state: 'lost' }
      }
      if (!again.ok) {
        this.discard(again.uploads)
        return this.fail(job, 'host_error', `${what}上传失败：${reason(again.cause)}`)
      }
      uploads = again.uploads
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

async function uploadOne(deps: Pick<BakeDeps, 'upload'>, file: File, signal: AbortSignal): Promise<Sent> {
  try {
    return { ok: true, uploads: [await deps.upload(file, { kind: 'image', mime: 'image/jpeg' }, signal)] }
  } catch (cause) {
    return { ok: false, cause, uploads: [] }
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
