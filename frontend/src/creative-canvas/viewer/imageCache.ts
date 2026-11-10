import { readMediaImage } from '../editor/media.ts'

type Entry = { url: string; bytes: number; readers: number; expiresAt: number }

/** 有界的图片字节缓存；由查看器或画布页面持有，离开所属范围时 clear。 */
export class ViewerImageCache {
  private entries = new Map<string, Entry>()
  private pending = new Set<AbortController>()
  private maxBytes: number
  private maxEntries: number
  constructor(maxBytes = 64 * 1024 * 1024, maxEntries = 20) {
    this.maxBytes = maxBytes
    this.maxEntries = maxEntries
  }

  peek(source: string) {
    const entry = this.entries.get(source)
    if (entry && entry.expiresAt <= Date.now()) this.invalidate(source)
    return this.entries.get(source)?.url
  }

  async load(source: string, controller: AbortController, expiresAt = Infinity) {
    controller.signal.throwIfAborted()
    if (!(expiresAt > Date.now())) throw new Error('图片访问已过期，请重试')
    this.peek(source) // 过期字节不能因同一个地址再次出现而复用。
    let entry = this.entries.get(source)
    if (!entry) {
      this.pending.add(controller)
      try {
        const blob = await readMediaImage(source, controller.signal)
        controller.signal.throwIfAborted()
        if (!(expiresAt > Date.now())) throw new Error('图片访问已过期，请重试')
        entry = {
          url: URL.createObjectURL(blob),
          bytes: blob.size,
          readers: 0,
          expiresAt,
        }
        this.entries.set(source, entry)
      } finally {
        this.pending.delete(controller)
      }
    }
    const current = entry
    current.readers++
    // 访问后移到队尾；正在显示的图片不淘汰，离开后再收回超预算项。
    this.entries.delete(source)
    this.entries.set(source, current)
    this.prune()
    return {
      url: current.url,
      release: () => {
        current.readers--
        this.prune()
      },
    }
  }

  invalidate(source: string) {
    const entry = this.entries.get(source)
    if (entry) URL.revokeObjectURL(entry.url)
    this.entries.delete(source)
  }

  clear() {
    for (const controller of this.pending) controller.abort()
    this.pending.clear()
    for (const entry of this.entries.values()) URL.revokeObjectURL(entry.url)
    this.entries.clear()
  }

  private prune() {
    let bytes = [...this.entries.values()].reduce(
      (sum, entry) => sum + entry.bytes,
      0,
    )
    for (const [source, entry] of this.entries) {
      if (bytes <= this.maxBytes && this.entries.size <= this.maxEntries) break
      if (entry.readers) continue
      URL.revokeObjectURL(entry.url)
      this.entries.delete(source)
      bytes -= entry.bytes
    }
  }
}
