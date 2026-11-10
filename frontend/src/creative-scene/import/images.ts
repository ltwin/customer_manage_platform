import type { ImageUse, ReportItem } from './convert.ts'
import { ImportRejected, type ImageMIME, type SourceImage } from './parse.ts'

// The file's images as they go up (Epic「导入的材质换算」图片): each distinct
// one once, by its content's hash; as stored when within the upload limits,
// otherwise scaled down and encoded by what it is used for and whether it has
// any transparency: data maps and anything see-through as PNG, the rest as
// JPEG. Still too large, its sides are halved until it fits.

export type ImageLimits = { maxBytes: number; maxPixels: number; maxEdge: number }

export const defaultImageLimits: Readonly<ImageLimits> = Object.freeze({ maxBytes: 25 * 1024 * 1024, maxPixels: 8192 * 8192, maxEdge: 12000 })

export const maxImages = 64

/** One image to upload, standing for every file image of the same content. */
export type PreparedImage = { images: number[]; bytes: Uint8Array; mime: ImageMIME; width: number; height: number }

/** A decoded picture at a given size, as the browser's canvas has it. */
export type Drawn = { hasAlpha: boolean; encode(type: 'image/png' | 'image/jpeg'): Promise<Uint8Array>; close(): void }

/** Decoding and encoding: the browser's in the app, a stand-in in tests. */
export type Codec = { draw(source: SourceImage, width: number, height: number): Promise<Drawn> }

export const jpegQuality = 0.92

/** The size an image says it is in its header, or null if it says nothing we read. */
export function dimensions(bytes: Uint8Array, mime: ImageMIME): { width: number; height: number } | null {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
  const u16le = (at: number) => view.getUint16(at, true)
  try {
    if (mime === 'image/png') return { width: view.getUint32(16), height: view.getUint32(20) }
    if (mime === 'image/jpeg') {
      for (let at = 2; at + 9 < bytes.length; ) {
        if (bytes[at] !== 0xff) return null
        const marker = bytes[at + 1]!
        if (marker === 0xff) {
          at++
          continue
        }
        const length = view.getUint16(at + 2)
        // SOF0–SOF15 but DHT (C4), JPG (C8) and DAC (CC).
        if (marker >= 0xc0 && marker <= 0xcf && marker !== 0xc4 && marker !== 0xc8 && marker !== 0xcc) return { width: view.getUint16(at + 7), height: view.getUint16(at + 5) }
        at += 2 + length
      }
      return null
    }
    const chunk = String.fromCharCode(...bytes.subarray(12, 16))
    if (chunk === 'VP8X') return { width: 1 + (u16le(24) | (bytes[26]! << 16)), height: 1 + (u16le(27) | (bytes[29]! << 16)) }
    if (chunk === 'VP8 ') return { width: u16le(26) & 0x3fff, height: u16le(28) & 0x3fff }
    if (chunk === 'VP8L') {
      const bits = view.getUint32(21, true)
      return { width: 1 + (bits & 0x3fff), height: 1 + ((bits >> 14) & 0x3fff) }
    }
    return null
  } catch {
    return null
  }
}

async function hashOf(bytes: Uint8Array): Promise<string> {
  const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', bytes as Uint8Array<ArrayBuffer>))
  return Array.from(digest, (b) => b.toString(16).padStart(2, '0')).join('')
}

const fits = (w: number, h: number, l: ImageLimits) => w * h <= l.maxPixels && Math.max(w, h) <= l.maxEdge

/**
 * The uploads for the images the slots use (convert.ts Conversion.images),
 * with a report line for each one re-encoded.
 */
export async function prepareImages(uses: ReadonlyMap<number, ImageUse>, sources: ReadonlyMap<number, SourceImage>, codec: Codec, limits: ImageLimits = defaultImageLimits): Promise<{ images: PreparedImage[]; report: ReportItem[] }> {
  const groups = new Map<string, { images: number[]; source: SourceImage; use: ImageUse }>()
  for (const [index, use] of [...uses].sort((a, b) => a[0] - b[0])) {
    const source = sources.get(index)
    if (!source) continue
    const hash = await hashOf(source.bytes)
    const g = groups.get(hash)
    if (g) {
      g.images.push(index)
      if (use === 'data') g.use = 'data'
    } else {
      groups.set(hash, { images: [index], source, use })
    }
  }
  if (groups.size > maxImages) throw new ImportRejected('limit', `模型用了 ${groups.size} 张不同的图片，最多 ${maxImages} 张。请在 Blender 里合并或删减贴图后再导出`)

  const images: PreparedImage[] = []
  const report: ReportItem[] = []
  for (const { images: indices, source, use } of groups.values()) {
    const size = dimensions(source.bytes, source.mime)
    if (!size) throw new ImportRejected('image_format', `图片 ${indices[0]} 无法读取尺寸`)
    if (source.bytes.byteLength <= limits.maxBytes && fits(size.width, size.height, limits)) {
      images.push({ images: indices, bytes: source.bytes, mime: source.mime, ...size })
      continue
    }
    const shrink = Math.min(1, Math.sqrt(limits.maxPixels / (size.width * size.height)), limits.maxEdge / Math.max(size.width, size.height))
    let width = Math.max(1, Math.floor(size.width * shrink))
    let height = Math.max(1, Math.floor(size.height * shrink))
    let done: PreparedImage | null = null
    for (;;) {
      const drawn = await codec.draw(source, width, height)
      try {
        const mime = use === 'data' || drawn.hasAlpha ? 'image/png' : 'image/jpeg'
        const bytes = await drawn.encode(mime)
        if (bytes.byteLength <= limits.maxBytes) done = { images: indices, bytes, mime, width, height }
      } finally {
        drawn.close()
      }
      if (done || (width === 1 && height === 1)) break
      width = Math.max(1, Math.floor(width / 2))
      height = Math.max(1, Math.floor(height / 2))
    }
    if (!done) throw new ImportRejected('limit', `图片 ${indices[0]} 缩到最小仍超过上传上限`)
    images.push(done)
    const format = done.mime === 'image/png' ? 'PNG' : 'JPEG'
    const how = width === size.width && height === size.height ? `已重新编码为 ${format}` : `已缩小到 ${width} × ${height} 并存为 ${format}`
    report.push({ slot: null, level: 'changed', text: `图片 ${indices[0]}（${size.width} × ${size.height}）超过上传上限，${how}` })
  }
  return { images, report }
}

/** The browser's codec: decoded without colour management, so data maps keep their values. */
export const browserCodec: Codec = {
  async draw(source, width, height) {
    const bitmap = await createImageBitmap(new Blob([source.bytes as Uint8Array<ArrayBuffer>], { type: source.mime }), {
      resizeWidth: width,
      resizeHeight: height,
      resizeQuality: 'high',
      colorSpaceConversion: 'none',
      premultiplyAlpha: 'none',
    })
    const canvas = new OffscreenCanvas(width, height)
    const ctx = canvas.getContext('2d')!
    ctx.drawImage(bitmap, 0, 0)
    bitmap.close()
    const pixels = ctx.getImageData(0, 0, width, height).data
    let hasAlpha = false
    for (let i = 3; i < pixels.length; i += 4) {
      if (pixels[i]! < 255) {
        hasAlpha = true
        break
      }
    }
    return {
      hasAlpha,
      async encode(type) {
        const blob = await canvas.convertToBlob(type === 'image/jpeg' ? { type, quality: jpegQuality } : { type })
        return new Uint8Array(await blob.arrayBuffer())
      },
      close() {
        canvas.width = 0
        canvas.height = 0
      },
    }
  },
}
