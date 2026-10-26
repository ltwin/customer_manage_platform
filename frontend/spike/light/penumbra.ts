// Penumbra widths on occluder-wall (design「测量与判定」, AC-1): each board's shadow on the wall, profiled across
// its edges in metres, for the reference, the raster averaged and its first frame.
//
// A profile runs through the shadow's centre along the wall's x (left and right edges) and along its height (lower
// and upper edges): wall pixels in a band either side of the line, binned by distance along it, each bin the mean
// shadow factor. From the centre outward, the edge's 10%–90% width is between the bins where the factor first
// reaches lo + 0.1·(1 − lo) and lo + 0.9·(1 − lo), lo being the profile's lowest near the centre.
import type { Comparison } from './reference.ts'

/** The wall's front face, and the boards' shadow centres on it (doc x, height), by the light's centre: occluder-wall-layout.py. */
const WALL_Z = -2.95
export const boards = [
  { name: '0.05 m', at: [-2.775, 0.98] },
  { name: '0.5 m', at: [-1.075, 2.52] },
  { name: '3 m', at: [3.496, 2.54] },
] as const

const BIN = 0.005
const BAND = 0.03
const REACH = 0.6

export type Edge = { width: number | null; lo: number }
export type BoardWidths = Record<'left' | 'right' | 'lower' | 'upper', Edge>
export type Penumbrae = Record<string, Record<'reference' | 'averaged' | 'first', BoardWidths>>

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

export function penumbrae(c: Comparison, s: { ref: Float32Array; averagedShadow: Float32Array; firstShadow: Float32Array }): Penumbrae {
  const out: Penumbrae = {}
  for (const b of boards) out[b.name] = { reference: widths(c, s.ref, b.at), averaged: widths(c, s.averagedShadow, b.at), first: widths(c, s.firstShadow, b.at) }
  return out
}

/** One line a board: the widths in cm, reference → averaged / first. */
export function penumbraLines(p: Penumbrae): string[] {
  const cm = (e: Edge) => (e.width == null ? '–' : (e.width * 100).toFixed(1))
  return Object.entries(p).map(([name, w]) => `${name}: ` + (['left', 'right', 'lower', 'upper'] as const).map((k) => `${k} ${cm(w.reference[k])}→${cm(w.averaged[k])}/${cm(w.first[k])}`).join(' · ') + ' cm')
}
