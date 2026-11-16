export type ViewportImage = { blob: Blob; width: number; height: number }
export type CaptureViewport = (signal?: AbortSignal) => Promise<ViewportImage>

type Pending = {
  promise: Promise<ViewportImage>
  resolve(image: ViewportImage): void
  reject(error: Error): void
  timer: ReturnType<typeof setTimeout>
  encoding: boolean
  detach(): void
}

/** Read the drawing buffer in the frame that drew it, before the browser clears it. */
export class FrameCapture {
  private pending: Pending | null = null
  private redraw: () => void
  private timeoutMs: number
  constructor(redraw: () => void, timeoutMs = 10_000) {
    this.redraw = redraw
    this.timeoutMs = timeoutMs
  }

  request = (signal?: AbortSignal): Promise<ViewportImage> => {
    if (signal?.aborted) return Promise.reject(new Error('截图已取消'))
    if (this.pending) return this.pending.promise
    let resolve!: (image: ViewportImage) => void
    let reject!: (error: Error) => void
    const promise = new Promise<ViewportImage>((yes, no) => { resolve = yes; reject = no })
    const abort = () => this.cancel()
    this.pending = {
      promise, resolve, reject, encoding: false,
      detach: () => signal?.removeEventListener('abort', abort),
      timer: setTimeout(() => this.cancel('视口截图超时，请等画面就绪后重试'), this.timeoutMs),
    }
    signal?.addEventListener('abort', abort, { once: true })
    this.redraw()
    return promise
  }

  afterFrame(canvas: Pick<HTMLCanvasElement, 'width' | 'height' | 'toBlob'>) {
    const pending = this.pending
    if (!pending || pending.encoding) return
    const { width, height } = canvas
    if (!width || !height) return this.cancel('视口尚未就绪，请稍后重试')
    pending.encoding = true
    try {
      canvas.toBlob((blob) => {
        if (this.pending !== pending) return
        if (!blob) return this.cancel('截图编码失败，请重试')
        clearTimeout(pending.timer)
        pending.detach()
        this.pending = null
        pending.resolve({ blob, width, height })
      }, 'image/png')
    } catch (error) {
      this.cancel(error instanceof Error ? error.message : '无法读取视口画面，请重试')
    }
  }

  cancel(message = '截图已取消') {
    if (!this.pending) return
    clearTimeout(this.pending.timer)
    this.pending.detach()
    this.pending.reject(new Error(message))
    this.pending = null
  }
}
