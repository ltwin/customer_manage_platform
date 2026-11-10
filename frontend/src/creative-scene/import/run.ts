import type { components } from '../../api/schema'
import { ApiError } from '../../api/transport.ts'
import type { Generated, Upload } from '../../creative-canvas/editor/media.ts'
import { slotPayload, type ConvertedSlot } from './convert.ts'
import type { PreparedImage } from './images.ts'

// An import from the converted file to the asset (design「导入的提交边界」):
// staged uploads, the thumbnail, then one request. Until that request is
// sent, cancelling or a failure discards every upload. Once it is, it is
// never given up on by itself: lost responses, timeouts and 5xx are retried
// with the same operation and body, which the server replays, and only a
// definite refusal (4xx) discards the uploads. The network comes in as
// parameters, as host/exportShots.ts does, so tests can lose a response.

type Slots = components['schemas']['CreativeSceneMaterialSlots']
export type ImportPayload = components['schemas']['CreativeAssetFromGLBPayload']
export type ImportResult = components['schemas']['CreativeAssetCreateResult']

export type ImportNetwork = {
  /** Stages a file and resolves once it is ready. */
  upload(file: File, declared: Generated, signal: AbortSignal): Promise<Upload>
  discard(upload: Upload): Promise<void>
  /** POST /creative/assets/from-glb with this body and Idempotency-Key. */
  submit(body: string, operationID: string): Promise<ImportResult>
  wait(ms: number): Promise<void>
  now(): number
}

export type ImportJob = {
  glb: ArrayBuffer
  fileName: string
  images: readonly PreparedImage[]
  slots: ReadonlyMap<string, ConvertedSlot>
  /** Draws the card thumbnail with these slots, each image already uploaded as revisions has it (file image → revision). */
  thumbnail(materials: Slots, revisions: ReadonlyMap<number, string>): Promise<Blob>
  fields: Pick<ImportPayload, 'import' | 'mount' | 'target'>
}

export type RunState =
  | { phase: 'uploading'; done: number; total: number }
  | { phase: 'thumbnail' }
  | { phase: 'committing' }
  /** Retried for two minutes without an answer: the asset may or may not exist. */
  | { phase: 'unknown' }
  | { phase: 'done'; result: ImportResult }
  | { phase: 'failed'; message: string }
  | { phase: 'cancelled' }

/** Backoff between attempts at the request, and how long to keep at it before saying the outcome is unknown. */
export const retryDelaysMs = [1000, 2000, 4000, 8000] as const
export const retryForMs = 2 * 60 * 1000
const uploadConcurrency = 2

/** A refusal: the server answered and will not take it. A timeout or too many requests was never taken in. */
const definite = (e: unknown) => e instanceof ApiError && e.status >= 400 && e.status < 500 && e.status !== 408 && e.status !== 429

const extension: Record<PreparedImage['mime'], string> = { 'image/png': 'png', 'image/jpeg': 'jpg', 'image/webp': 'webp' }

export class ImportRun {
  private readonly abort = new AbortController()
  /** Every upload staged so far: what a cancel or a refusal discards. */
  private readonly staged = new Set<Upload>()
  private request: { body: string; operationID: string } | null = null
  private state: RunState = { phase: 'uploading', done: 0, total: 0 }

  private readonly job: ImportJob
  private readonly net: ImportNetwork
  private readonly onState: (s: RunState) => void

  constructor(job: ImportJob, net: ImportNetwork, onState: (s: RunState) => void) {
    this.job = job
    this.net = net
    this.onState = onState
  }

  /** True until the request is sent; afterwards a cancel would leave the outcome unknown. */
  get cancellable(): boolean {
    return this.state.phase === 'uploading' || this.state.phase === 'thumbnail'
  }

  /** Cancels before the request; does nothing (false) once it is sent. */
  cancel(): boolean {
    if (!this.cancellable) return false
    this.abort.abort()
    return true
  }

  async start(): Promise<void> {
    try {
      const { glb, textures, revisions } = await this.stage()
      this.set({ phase: 'thumbnail' })
      const materials = slotPayload(this.job.slots, (image) => revisions.get(image)!)
      const png = await this.job.thumbnail(materials, revisions)
      this.abort.signal.throwIfAborted()
      const thumbnail = await this.put(new File([png], 'thumbnail.png', { type: 'image/png' }), { kind: 'image', mime: 'image/png' })
      this.abort.signal.throwIfAborted()
      const payload: ImportPayload = {
        glb_upload_id: glb.id,
        thumbnail_upload_id: thumbnail.id,
        texture_upload_ids: textures.map((u) => u.id),
        materials,
        ...this.job.fields,
      }
      const operationID = crypto.randomUUID()
      this.request = { operationID, body: JSON.stringify({ operation_id: operationID, client_created_at: new Date(this.net.now()).toISOString(), payload }) }
    } catch (e) {
      await this.discardAll()
      this.set(this.abort.signal.aborted ? { phase: 'cancelled' } : { phase: 'failed', message: messageOf(e) })
      return
    }
    await this.commit()
  }

  /** After 'unknown': the same request again. */
  async retry(): Promise<void> {
    if (this.state.phase !== 'unknown') return
    await this.commit()
  }

  /** The GLB and the images, two at a time; each file image's revision. */
  private async stage(): Promise<{ glb: Upload; textures: Upload[]; revisions: Map<number, string> }> {
    const { job } = this
    const files: [File, Generated, PreparedImage | null][] = [
      [new File([job.glb], job.fileName, { type: 'model/gltf-binary' }), { kind: 'model', mime: 'model/gltf-binary' }, null],
      ...job.images.map((image, i): [File, Generated, PreparedImage] => [
        new File([image.bytes as Uint8Array<ArrayBuffer>], `texture-${i + 1}.${extension[image.mime]}`, { type: image.mime }),
        { kind: 'image', mime: image.mime },
        image,
      ]),
    ]
    const revisions = new Map<number, string>()
    const inOrder: (Upload | undefined)[] = new Array(files.length)
    let next = 0
    let done = 0
    this.set({ phase: 'uploading', done, total: files.length })
    let stopped = false
    const worker = async () => {
      while (!stopped && next < files.length) {
        const i = next++
        const [file, declared, image] = files[i]!
        try {
          this.abort.signal.throwIfAborted()
          const upload = await this.put(file, declared)
          inOrder[i] = upload
          if (image) {
            if (!upload.content_revision_id) throw new Error('图片上传没有得到内容修订')
            for (const index of image.images) revisions.set(index, upload.content_revision_id)
          }
        } catch (e) {
          stopped = true
          throw e
        }
        this.set({ phase: 'uploading', done: ++done, total: files.length })
      }
    }
    // Both workers settle before a failure is passed on, so nothing is staged after the discard.
    const results = await Promise.allSettled(Array.from({ length: Math.min(uploadConcurrency, files.length) }, worker))
    const failed = results.find((r) => r.status === 'rejected')
    if (failed) throw failed.reason
    const [glb, ...textures] = inOrder as Upload[]
    return { glb: glb!, textures, revisions }
  }

  private async put(file: File, declared: Generated): Promise<Upload> {
    const upload = await this.net.upload(file, declared, this.abort.signal)
    this.staged.add(upload)
    return upload
  }

  private async commit(): Promise<void> {
    const { net } = this
    const { body, operationID } = this.request!
    this.set({ phase: 'committing' })
    const started = net.now()
    for (let attempt = 0; ; attempt++) {
      try {
        const result = await net.submit(body, operationID)
        this.set({ phase: 'done', result })
        return
      } catch (e) {
        if (definite(e)) {
          await this.discardAll()
          this.set({ phase: 'failed', message: messageOf(e) })
          return
        }
        const delay = retryDelaysMs[Math.min(attempt, retryDelaysMs.length - 1)]!
        if (net.now() - started + delay > retryForMs) {
          this.set({ phase: 'unknown' })
          return
        }
        await net.wait(delay)
      }
    }
  }

  /** Best effort: an upload left behind expires with its staging after 7 days. */
  private async discardAll() {
    const all = [...this.staged]
    this.staged.clear()
    await Promise.all(all.map((u) => this.net.discard(u).catch(() => {})))
  }

  private set(s: RunState) {
    this.state = s
    this.onState(s)
  }
}

function messageOf(e: unknown): string {
  return e instanceof Error && e.message ? e.message : '导入失败'
}
