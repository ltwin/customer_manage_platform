import { convergeFrames, type Engine } from '../engines.ts'

// The probe cascade's pure functions (S2 design「级联的放置与轮转」「更新」「收敛与重绘」):
// how many levels a scene takes, where each level's box sits, which slot a
// world cell lands in, which levels a frame updates, and how the history is
// weighed. Nothing here touches the GPU; kernels.ts writes the same rules
// in TSL and checks.ts compares the two on the GPU.

/** Probes along each side of a level's box. */
export const GRID = 16

/** Probes in a level. */
export const PROBES = GRID ** 3

/** Rays a probe traces each time it is updated. */
export const RAYS = 64

/** Texels along a side of a probe's octahedral irradiance and depth-moment tiles, without their 1-texel border. */
export const IRRADIANCE_TEXELS = 8
export const DEPTH_TEXELS = 16

export const LEVELS_MIN = 3
export const LEVELS_MAX = 5

/** Probe spacing of level k, in metres: 0.25 m, doubling each level. */
export const spacingOf = (k: number) => 0.25 * 2 ** k

/** Side of level k's box, in metres. */
export const widthOf = (k: number) => GRID * spacingOf(k)

/** How far ahead of the camera level k reaches: three quarters of its box. */
export const reachOf = (k: number) => (widthOf(k) * 3) / 4

/** The levels a distance d (metres) needs: the last one's reach ahead at least d, within 3 … 5. */
export function levelsFor(d: number): number {
  if (!(d > reachOf(LEVELS_MIN - 1))) return LEVELS_MIN
  return Math.min(LEVELS_MAX, LEVELS_MIN + Math.ceil(Math.log2(d / reachOf(LEVELS_MIN - 1))))
}

/**
 * The levels drawn this frame: as many as the camera's distance to the
 * scene's farthest corner needs, at most allocated; a level once on stays
 * until that distance is below 3/4 of where it came on, so the count does
 * not flicker on the boundary.
 */
export function activeLevels(allocated: number, farthest: number, previous: number): number {
  let n = levelsFor(farthest)
  // n + 1 levels come on beyond level n − 1's reach (levelsFor) and stay on down to 3/4 of it.
  while (n < previous && farthest > (reachOf(n - 1) * 3) / 4) n++
  return Math.min(allocated, n)
}

type Vec3 = readonly [number, number, number]

/**
 * Level k's box placed for a camera at eye looking along look (the view's
 * world space, Z up: space.ts): the cell its lowest corner is in. The box
 * reaches 3/4 of its width ahead along look's horizontal part, which is not
 * normalised: looking straight down the box falls back to centred on the
 * camera; vertically it is centred on the camera.
 */
export function originCell(k: number, eye: Vec3, look: Vec3): [number, number, number] {
  const s = spacingOf(k)
  const w = widthOf(k)
  const centre = [eye[0] + (look[0] * w) / 4, eye[1] + (look[1] * w) / 4, eye[2]]
  return centre.map((c) => Math.floor((c - w / 2) / s)) as [number, number, number]
}

/** a modulo n, never negative (WGSL's % truncates like JS's). */
export const emod = (a: number, n: number) => ((a % n) + n) % n

/** The slot a world cell is stored in: each axis modulo the grid, so a box that moves keeps the probes it still covers where they are. */
export const slotOf = (cell: Vec3) => emod(cell[0], GRID) + GRID * (emod(cell[1], GRID) + GRID * emod(cell[2], GRID))

/** The world cell slot stands for in a box whose lowest corner is origin: the one in the box with the slot's residues. */
export function cellOf(slot: number, origin: Vec3): [number, number, number] {
  const at = [slot % GRID, Math.floor(slot / GRID) % GRID, Math.floor(slot / GRID ** 2)]
  return origin.map((o, i) => o + emod(at[i] - o, GRID)) as [number, number, number]
}

/**
 * The frames between updates of each level, by the roots in the scene's
 * top level (the viewport's rotation; pictures update every level every frame).
 * The thresholds are where the performance task starts (AC-S2-6).
 */
export function periodsFor(roots: number): readonly number[] {
  if (roots <= 256) return [1, 1, 2, 2, 4]
  if (roots <= 1024) return [2, 2, 4, 4, 8]
  return [4, 4, 8, 8, 16]
}

/**
 * Which frame within its period each level is updated at: level by level,
 * the first offset whose frames carry the fewest levels so far, so no frame
 * carries more than one level above any other. The periods divide each
 * other, so the pattern repeats every longest period.
 */
export function offsetsFor(periods: readonly number[]): number[] {
  const span = Math.max(...periods)
  const load = new Array<number>(span).fill(0)
  return periods.map((p) => {
    let best = 0
    let fewest = Infinity
    for (let o = 0; o < p; o++) {
      let most = 0
      for (let f = o; f < span; f += p) most = Math.max(most, load[f])
      if (most < fewest) [best, fewest] = [o, most]
    }
    for (let f = best; f < span; f += p) load[f]++
    return best
  })
}

/** Whether level k is updated at frame: every periods[k] frames, staggered by offsetsFor. */
export const isDue = (k: number, frame: number, periods: readonly number[]) => emod(frame, periods[k]) === offsetsFor(periods)[k]

/** The history's weight against this frame's rays: none for A0 updates after an edit, then rising from 1/2 to alphaMax. */
export type Hysteresis = { readonly A0: number; readonly alphaMax: number }

/** The viewport: catches up within 12 updates, then settles at 0.93 (S0「edit」; owner 2026-10-26). */
export const VIEWPORT: Hysteresis = { A0: 12, alphaMax: 0.93 }

/** A picture's warm-up: two runs of 32 then a plain mean (S0「restart32」). */
export const PICTURE: Hysteresis = { A0: 32, alphaMax: 1 }

/**
 * r_fill (S2 design「合成」): the scene's ambient lights as the layer draws
 * them, a ratio on L_amb in the composite, at the hits and on the probes' rays
 * alike; 1 till AC-S2-2's comparison says otherwise (S2-U6, the owner's).
 */
export const AMBIENT_FILL = 1

/** The frames a picture warms its probes for before it draws (S2 design「导出预热与时限」). */
export const PROBE_WARMUP = 64

/** The history's weight at an edit age; a slot with no data yet (data age 0) takes none whatever its edit age. */
export const alphaOf = (editAge: number, h: Hysteresis) => (editAge < h.A0 ? 0 : Math.min(h.alphaMax, 1 - 1 / (editAge - h.A0 + 2)))

/** How far a probe's data is trusted when sampled: it fades in over its first A0 updates. */
export const maturityOf = (dataAge: number, h: Hysteresis) => Math.min(1, dataAge / h.A0)

/** Updates after an edit until the history weighs alphaMax again, near enough: α is 0.93 at A0 + 13. */
export const SETTLE_UPDATES = 14

/** Updates a relocated probe may still move in (RTXGI); after that its offset is frozen. */
export const RELOCATION_UPDATES = 8

/**
 * The frames the viewport draws before it stops asking for more (S2 design
 * 「收敛与重绘」). With the world-space layer, the slowest level drawn must
 * catch up and settle: A0 + 14 of its updates, at its period; 52 frames in the
 * reference scenes, 416 in the hall. Otherwise the renderer's own
 * convergeFrames, which pictures and the accumulation keep using either way.
 */
export function viewportSettleFrames(engine: Engine, world: boolean, levels: number, periods: readonly number[]): number {
  if (!world) return convergeFrames(engine)
  return Math.max(...periods.slice(0, levels)) * (VIEWPORT.A0 + SETTLE_UPDATES)
}
