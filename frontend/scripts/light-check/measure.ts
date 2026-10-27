// What the light reference scenes are measured by (scene-light-size S0's spike/light/penumbra.ts and panels.ts, moved
// here for S1's e2e): the shadow's error, the boards' penumbra widths on occluder-wall (AC-1), the gloss panel's
// highlight (AC-4) and the ball's terminator (AC-3), for the reference, the raster averaged and its first frame.
//
// A profile runs through the shadow's centre along the wall's x (left and right edges) and along its height (lower
// and upper edges): wall pixels in a band either side of the line, binned by distance along it, each bin the mean
// shadow factor. From the centre outward, the edge's 10%–90% width is between the bins where the factor first
// reaches lo + 0.1·(1 − lo) and lo + 0.9·(1 − lo), lo being the profile's lowest near the centre.
import { lum, type Comparison } from './reference.ts'

/**
 * The wall's front face, and the boards' shadow centres on it (doc x, height): from the light's centre for the
 * point and spot lights (occluder-wall-layout.py) and area-studio's softbox (at (−2.5, 1.8, 2.5)), along the sun's
 * way for the sun (the lights' place toward the wall's middle at (0, 1.8, −2.95)).
 */
const WALL_Z = -2.95
const centres = {
  near: [
    { name: '0.05 m', at: [-2.775, 0.98] },
    { name: '0.5 m', at: [-1.075, 2.52] },
    { name: '3 m', at: [3.496, 2.54] },
  ],
  area: [
    { name: '0.05 m', at: [-2.803, 0.991] },
    { name: '0.5 m', at: [-1.286, 2.683] },
    // Above the wall's top (4 m): nothing to measure.
    { name: '3 m', at: [2.191, 4.48] },
  ],
  sun: [
    { name: '0.05 m', at: [-2.76, 0.987] },
    { name: '0.5 m', at: [-0.989, 2.465] },
    { name: '3 m', at: [1.994, 2.218] },
  ],
} as const
/** Which shadow centres a light's boards throw: the sun's are along its way, the others' from the lights' place. */
export type Boards = keyof typeof centres

/** The boards' shadow centres in three's world (z up): where a narrow penumbra's texel is taken (AC-1). */
export const boardCentres = (boards: Boards) => centres[boards].map((b) => ({ name: b.name, at: [b.at[0], -WALL_Z, b.at[1]] as const }))

const BIN = 0.005
const BAND = 0.03
const REACH = 1.2

export type Edge = { width: number | null; lo: number }
export type BoardWidths = Record<'left' | 'right' | 'lower' | 'upper', Edge>
export type ProfileError = { mean: number; max: number }
export type Penumbrae = Record<string, Record<'reference' | 'averaged' | 'first', BoardWidths> & { profile: Record<'averaged' | 'first', ProfileError> }>

/** Doc coordinates from three's: the scene is z up, doc y up (x, −z, y). */
const docOf = (p: Float32Array, i: number) => [p[i * 4], p[i * 4 + 2], -p[i * 4 + 1]] as const

/** Mean shadow factor per bin, from -REACH to +REACH along one axis of the wall through centre. */
function profile(c: Comparison, shadow: Float32Array, centre: readonly [number, number], along: 0 | 1): (number | null)[] {
  const bins = Math.round((2 * REACH) / BIN)
  const sum = new Float64Array(bins)
  const count = new Uint32Array(bins)
  const n = c.size.width * c.size.height
  for (let i = 0; i < n; i++) {
    if (Number.isNaN(shadow[i])) continue
    const [x, y, z] = docOf(c.position, i)
    if (Math.abs(z - WALL_Z) > 0.004) continue
    const p = [x - centre[0], y - centre[1]]
    if (Math.abs(p[1 - along]) > BAND) continue
    const b = Math.floor((p[along] + REACH) / BIN)
    if (b < 0 || b >= bins) continue
    sum[b] += shadow[i]
    count[b]++
  }
  return Array.from(sum, (s, b) => (count[b] ? s / count[b] : null))
}

/** The 10%–90% width from the centre bin outward, in metres; null when the profile never reaches 90%. */
function edge(p: (number | null)[], step: 1 | -1): Edge {
  const mid = Math.floor(p.length / 2)
  // The lowest within the first 0.1 m out: the umbra, or the deepest a soft shadow gets.
  let lo = 1
  for (let k = 0; k <= 0.1 / BIN; k++) {
    const v = p[mid + step * k]
    if (v != null) lo = Math.min(lo, v)
  }
  const at = (frac: number) => {
    const level = lo + frac * (1 - lo)
    let passedLow = false
    for (let k = 0; mid + step * k >= 0 && mid + step * k < p.length; k++) {
      const v = p[mid + step * k]
      if (v == null) continue
      if (v <= lo + 1e-6) passedLow = true
      if (passedLow && v >= level) return k * BIN
    }
    return null
  }
  // No shadow at the centre (lo near 1): nothing to measure.
  if (lo > 0.9) return { width: null, lo }
  const a = at(0.1)
  const b = at(0.9)
  return { width: a == null || b == null ? null : b - a, lo }
}

function widths(c: Comparison, shadow: Float32Array, centre: readonly [number, number]): BoardWidths {
  const h = profile(c, shadow, centre, 0)
  const v = profile(c, shadow, centre, 1)
  return { left: edge(h, -1), right: edge(h, 1), lower: edge(v, -1), upper: edge(v, 1) }
}

/**
 * How far the raster's profiles are from the reference's, bin by bin, both axes: what still compares when a shadow
 * has no umbra and its 10%–90% width means nothing.
 */
function profileError(c: Comparison, ref: Float32Array, raster: Float32Array, centre: readonly [number, number]): ProfileError {
  const d: number[] = []
  for (const along of [0, 1] as const) {
    const a = profile(c, ref, centre, along)
    const b = profile(c, raster, centre, along)
    a.forEach((v, k) => v != null && b[k] != null && d.push(Math.abs(v - b[k]!)))
  }
  return { mean: d.length ? d.reduce((x, y) => x + y, 0) / d.length : 0, max: d.length ? Math.max(...d) : 0 }
}

export function penumbrae(c: Comparison, s: { ref: Float32Array; averagedShadow: Float32Array; firstShadow: Float32Array }, boards: Boards): Penumbrae {
  const out: Penumbrae = {}
  for (const b of centres[boards]) {
    out[b.name] = {
      reference: widths(c, s.ref, b.at),
      averaged: widths(c, s.averagedShadow, b.at),
      first: widths(c, s.firstShadow, b.at),
      profile: { averaged: profileError(c, s.ref, s.averagedShadow, b.at), first: profileError(c, s.ref, s.firstShadow, b.at) },
    }
  }
  return out
}

/** One line a board: the widths in cm, reference → averaged / first. */
export function penumbraLines(p: Penumbrae): string[] {
  const cm = (e: Edge) => (e.width == null ? '–' : (e.width * 100).toFixed(1))
  return Object.entries(p).map(([name, w]) => `${name}: ` + (['left', 'right', 'lower', 'upper'] as const).map((k) => `${k} ${cm(w.reference[k])}→${cm(w.averaged[k])}/${cm(w.first[k])}`).join(' · ') + ' cm')
}

/** Mean and p99 |Δ shadow| on the wall's face and on the ground, averaged and first frame: where a bias shows. */
export function regionErrors(c: Comparison, s: { ref: Float32Array; averagedShadow: Float32Array; firstShadow: Float32Array }) {
  const regions = { wall: (p: readonly number[]) => Math.abs(p[2] - WALL_Z) < 0.004, ground: (p: readonly number[]) => Math.abs(p[1]) < 0.004 }
  const out: Record<string, Record<'averaged' | 'first', { mean: number; p99: number }>> = {}
  for (const [name, inside] of Object.entries(regions)) {
    const e: Record<'averaged' | 'first', number[]> = { averaged: [], first: [] }
    for (let i = 0; i < c.size.width * c.size.height; i++) {
      if (Number.isNaN(s.ref[i]) || Number.isNaN(s.averagedShadow[i]) || !inside(docOf(c.position, i))) continue
      e.averaged.push(Math.abs(s.averagedShadow[i] - s.ref[i]))
      e.first.push(Math.abs(s.firstShadow[i] - s.ref[i]))
    }
    const stat = (a: number[]) => (a.sort((x, y) => x - y), { mean: a.reduce((x, y) => x + y, 0) / Math.max(1, a.length), p99: a[Math.floor(a.length * 0.99)] ?? 0 })
    out[name] = { averaged: stat(e.averaged), first: stat(e.first) }
  }
  return out
}

/**
 * The gloss panel's highlight (AC-4): on its face (doc x −3.74…−2.74, height 2.36…3.36, z −2.93), the light it
 * sends back unshadowed, the reference's and the raster's: the sum (energy), the peak, and how many pixels pass
 * half of their own peak (area).
 */
export function highlight(c: Comparison) {
  // The wall's light, unshadowed: the diffuse the light gives, raster over reference (AC-3, AC-5).
  const wall = { reference: 0, raster: 0 }
  for (let i = 0; i < c.size.width * c.size.height; i++) {
    if (Math.abs(docOf(c.position, i)[2] - WALL_Z) > 0.004) continue
    for (const [k, a] of [['reference', c.reference.unshadowed], ['raster', c.raster.unshadowed]] as const) wall[k] += 0.2126 * a[i * 4] + 0.7152 * a[i * 4 + 1] + 0.0722 * a[i * 4 + 2]
  }
  const sums = { reference: 0, raster: 0 }
  const values: Record<'reference' | 'raster', number[]> = { reference: [], raster: [] }
  for (let i = 0; i < c.size.width * c.size.height; i++) {
    const [x, y, z] = docOf(c.position, i)
    if (Math.abs(z + 2.93) > 0.004 || x < -3.74 || x > -2.74 || y < 2.36 || y > 3.36) continue
    for (const [k, a] of [['reference', c.reference.unshadowed], ['raster', c.raster.unshadowed]] as const) {
      const v = 0.2126 * a[i * 4] + 0.7152 * a[i * 4 + 1] + 0.0722 * a[i * 4 + 2]
      sums[k] += v
      values[k].push(v)
    }
  }
  const peak = (a: number[]) => a.reduce((m, v) => Math.max(m, v), 0)
  // The panel's diffuse, its dimmest 5%: the highlight is what stands above it.
  const floor = (a: number[]) => [...a].sort((x, y) => x - y)[Math.floor(a.length * 0.05)] ?? 0
  const above = (a: number[]) => a.map((v) => Math.max(0, v - floor(a)))
  const area = (a: number[]) => {
    const h = above(a)
    return h.filter((v) => v > peak(h) / 2).length
  }
  const lobe = (a: number[]) => above(a).reduce((x, y) => x + y, 0)
  return {
    pixels: values.reference.length,
    wall: wall.raster / wall.reference,
    energy: { reference: sums.reference, raster: sums.raster, ratio: sums.raster / sums.reference },
    lobe: { reference: lobe(values.reference), raster: lobe(values.raster), ratio: lobe(values.raster) / lobe(values.reference) },
    peak: { reference: peak(values.reference), raster: peak(values.raster) },
    area: { reference: area(values.reference), raster: area(values.raster), ratio: area(values.raster) / Math.max(1, area(values.reference)) },
  }
}

/** The ball's centre (doc; a sphere primitive stands on its base) and the point light's place under ?layout=near. */
const BALL = [2.6, 0.25, -1.4] as const
const NEAR_LIGHT = [0.6, 0.6, -1.4] as const

/**
 * Where the ball's terminator lies (design「测量与判定」包裹): on the ball, the angle between a point's normal and
 * the way to the light's centre, in 1° bins of the mean unshadowed light; the largest angle whose bin still has 1%
 * of the brightest bin. 90° for a point light; past it as a sphere light wraps.
 */
export function terminator(c: Comparison) {
  const bins = { reference: new Float64Array(181), raster: new Float64Array(181) }
  const count = new Uint32Array(181)
  const toLight = NEAR_LIGHT.map((v, k) => v - BALL[k])
  const len = Math.hypot(...toLight)
  for (let i = 0; i < c.size.width * c.size.height; i++) {
    const p = docOf(c.position, i)
    const n = p.map((v, k) => v - BALL[k])
    const r = Math.hypot(...n)
    // On the ball, not the ground ring as far from its centre where it touches down.
    if (Math.abs(r - 0.25) > 0.01 || p[1] < 0.005) continue
    const cos = (n[0] * toLight[0] + n[1] * toLight[1] + n[2] * toLight[2]) / (r * len)
    const b = Math.round((Math.acos(Math.max(-1, Math.min(1, cos))) * 180) / Math.PI)
    count[b]++
    for (const [k, a] of [['reference', c.reference.unshadowed], ['raster', c.raster.unshadowed]] as const) bins[k][b] += 0.2126 * a[i * 4] + 0.7152 * a[i * 4 + 1] + 0.0722 * a[i * 4 + 2]
  }
  const edge = (a: Float64Array) => {
    const mean = Array.from(a, (v, b) => (count[b] ? v / count[b] : Number.NaN))
    const peak = mean.reduce((m, v) => (Number.isNaN(v) ? m : Math.max(m, v)), 0)
    let last = -1
    mean.forEach((v, b) => !Number.isNaN(v) && v >= peak * 0.01 && (last = b))
    return last
  }
  // Every 10°, each bin's mean as a share of its brightest: the curves behind the two numbers.
  const curve = (a: Float64Array) => {
    const mean = Array.from(a, (v, b) => (count[b] ? v / count[b] : Number.NaN))
    const peak = mean.reduce((m, v) => (Number.isNaN(v) ? m : Math.max(m, v)), 0)
    return mean.filter((_, b) => b % 10 === 0).map((v) => (Number.isNaN(v) ? null : +(v / peak).toFixed(4)))
  }
  return { curves: { reference: curve(bins.reference), raster: curve(bins.raster) }, reference: edge(bins.reference), raster: edge(bins.raster), seen: [count.findIndex((n) => n > 0), 180 - [...count].reverse().findIndex((n) => n > 0)] }
}

// --- the shadow's error and the pictures ---

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
