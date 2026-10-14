import { readMediaImage } from '../editor/media.ts'

type Entry = { url: string; bytes: number; readers: number }

/** 浮窗内复用已读图片；不依赖媒体接口的 HTTP 缓存，不跨浮窗持有数据。 */
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
    return this.entries.get(source)?.url
  }

  async load(source: string, controller: AbortController) {
    let entry = this.entries.get(source)
    if (!entry) {
      this.pending.add(controller)
      try {
        const blob = await readMediaImage(source, controller.signal)
        controller.signal.throwIfAborted()
        entry = { url: URL.createObjectURL(blob), bytes: blob.size, readers: 0 }
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
