// The compare view's pictures and numbers: the reference, the raster averaged and its first frame, side by side
// with the same tone, and the shadow's error between them (design「测量与判定」).
import { lum, type Comparison } from './reference.ts'

export type Stats = { mean: number; p95: number; max: number; pixels: number }

/** The shadow a light leaves, shadowed ÷ unshadowed, by luminance; NaN where nothing lights the pixel. */
function shadowOf(shadowed: Float32Array, unshadowed: Float32Array, n: number): Float32Array {
  const s = new Float32Array(n)
  for (let i = 0; i < n; i++) {
    const u = lum(unshadowed, i)
    s[i] = u > 1e-5 ? Math.min(1, lum(shadowed, i) / u) : Number.NaN
  }
  return s
}

function stats(errors: number[]): Stats {
  if (!errors.length) return { mean: 0, p95: 0, max: 0, pixels: 0 }
  const sorted = [...errors].sort((a, b) => a - b)
  return { mean: errors.reduce((a, b) => a + b, 0) / errors.length, p95: sorted[Math.floor(sorted.length * 0.95)], max: sorted[sorted.length - 1], pixels: errors.length }
}

/** How far the raster's shadow is from the reference's, over the pixels some light reaches. */
export function shadowError(c: Comparison): { averaged: Stats; first: Stats; ref: Float32Array; averagedShadow: Float32Array; firstShadow: Float32Array } {
  const n = c.size.width * c.size.height
  const ref = shadowOf(c.reference.shadowed, c.reference.unshadowed, n)
  const averagedShadow = shadowOf(c.raster.shadowed, c.raster.unshadowed, n)
  const firstShadow = shadowOf(c.raster.first, c.raster.unshadowed, n)
  const a: number[] = []
  const f: number[] = []
  for (let i = 0; i < n; i++) {
    if (Number.isNaN(ref[i]) || Number.isNaN(averagedShadow[i])) continue
    a.push(Math.abs(averagedShadow[i] - ref[i]))
    f.push(Math.abs(firstShadow[i] - ref[i]))
  }
  return { averaged: stats(a), first: stats(f), ref, averagedShadow, firstShadow }
}

const srgb = (x: number) => (x <= 0.0031308 ? 12.92 * x : 1.055 * x ** (1 / 2.4) - 0.055)

/** A picture of linear RGBA under a simple tone (1 − e^(−k·x)), k from the reference's lit pixels. */
function toImage(a: Float32Array, w: number, h: number, k: number): ImageData {
  const img = new ImageData(w, h)
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      // Read back bottom up: draw top down.
      const i = (h - 1 - y) * w + x
      const o = (y * w + x) * 4
      for (let c = 0; c < 3; c++) img.data[o + c] = Math.round(255 * srgb(1 - Math.exp(-k * Math.max(0, a[i * 4 + c]))))
      img.data[o + 3] = 255
    }
  }
  return img
}

/** |Δ shadow| in red, from 0 (black) to 0.5 and over (full); unlit pixels grey. */
function errorImage(ref: Float32Array, raster: Float32Array, w: number, h: number): ImageData {
  const img = new ImageData(w, h)
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      const i = (h - 1 - y) * w + x
      const o = (y * w + x) * 4
      const e = Number.isNaN(ref[i]) || Number.isNaN(raster[i]) ? -1 : Math.abs(raster[i] - ref[i])
      if (e < 0) img.data.set([40, 40, 40, 255], o)
      else img.data.set([Math.round(255 * Math.min(1, e * 2)), Math.round(255 * Math.min(1, e * 0.5)), 0, 255], o)
    }
  }
  return img
}

/** Draws the four panels into a canvas of their own; flipY when the read back runs top down. */
export function drawPanels(c: Comparison, flip: boolean): HTMLCanvasElement {
  const { width: w, height: h } = c.size
  const n = w * h
  const lit: number[] = []
  for (let i = 0; i < n; i++) {
    const v = lum(c.reference.unshadowed, i)
    if (v > 1e-5) lit.push(v)
  }
  lit.sort((a, b) => a - b)
  const k = lit.length ? 1.2 / lit[Math.floor(lit.length * 0.5)] : 1
  const e = shadowError(c)
  const flipRows = (a: Float32Array, stride: number) => {
    if (!flip) return a
    const out = new Float32Array(a.length)
    for (let y = 0; y < h; y++) out.set(a.subarray((h - 1 - y) * w * stride, (h - y) * w * stride), y * w * stride)
    return out
  }
  const canvas = document.createElement('canvas')
  canvas.id = 'panels'
  canvas.width = w * 2
  canvas.height = h * 2 + 48
  const ctx = canvas.getContext('2d')!
  ctx.fillStyle = '#111'
  ctx.fillRect(0, 0, canvas.width, canvas.height)
  const put = (img: ImageData, x: number, y: number, label: string) => {
    ctx.putImageData(img, x, y + 24)
    ctx.fillStyle = '#ddd'
    ctx.font = '14px ui-monospace, Menlo, monospace'
    ctx.fillText(label, x + 6, y + 17)
  }
  put(toImage(flipRows(c.reference.shadowed, 4), w, h, k), 0, 0, `参考解（${c.frames} 帧）`)
  put(toImage(flipRows(c.raster.shadowed, 4), w, h, k), w, 0, `光栅多帧平均  |Δ阴影| 均值 ${e.averaged.mean.toFixed(3)} · p95 ${e.averaged.p95.toFixed(3)}`)
  put(toImage(flipRows(c.raster.first, 4), w, h, k), 0, h + 24, `光栅单帧（workbench）  均值 ${e.first.mean.toFixed(3)} · p95 ${e.first.p95.toFixed(3)}`)
  put(errorImage(flipRows(e.ref, 1), flipRows(e.averagedShadow, 1), w, h), w, h + 24, '|Δ阴影|，多帧平均（红 = 0.5 以上）')
  return canvas
}

/**
 * How far the reference moved between refFrames and twice as many: the largest change of a channel in 8-bit levels
 * under the panels' tone (AC-S0-3 asks for at most 1), and the share of pixels that changed by more than one level.
 */
export function convergence(c: Comparison): { maxLevels: number; overOne: number } | null {
  if (!c.converge) return null
  const { at, twice } = c.converge
  const n = c.size.width * c.size.height
  const lit: number[] = []
  for (let i = 0; i < n; i++) {
    const v = lum(c.reference.unshadowed, i)
    if (v > 1e-5) lit.push(v)
  }
  lit.sort((a, b) => a - b)
  const k = lit.length ? 1.2 / lit[Math.floor(lit.length * 0.5)] : 1
  const level = (x: number) => Math.round(255 * srgb(1 - Math.exp(-k * Math.max(0, x))))
  let maxLevels = 0
  let over = 0
  for (let i = 0; i < n; i++) {
    let worst = 0
    for (let ch = 0; ch < 3; ch++) worst = Math.max(worst, Math.abs(level(at[i * 4 + ch]) - level(twice[i * 4 + ch])))
    maxLevels = Math.max(maxLevels, worst)
    if (worst > 1) over++
  }
  return { maxLevels, overOne: over / n }
}
