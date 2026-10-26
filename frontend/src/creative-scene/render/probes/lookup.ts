import * as tsl from 'three/tsl'
import type { TSLNode } from '../../materials/patterns.ts'
import { DEPTH_TEXELS, GRID, IRRADIANCE_TEXELS, LEVELS_MAX, PROBES, spacingOf } from './cascade.ts'
import { octEncode } from './octahedral.ts'

// The irradiance the probes give a point (S2 design「采样」): in each level
// the eight probes around it, weighed as DDGI weighs them; the levels from
// the coarsest drawn to the finest, each covering what it can (P5), the
// environment's irradiance only where none does. Every texture read names
// its level: the level picked and the early exit branch by pixel (the
// patterns contract: no implicit derivatives in a branch).
//
// Where a probe's data sits (volume.ts allocates, kernels.ts writes):
//   atlases  a tile per probe with a 1-texel border, 64 × 64 tiles a level,
//            the levels one under the other: irradiance 10 × 10 texels a
//            tile (640 × 640 a level), depth moments 18 × 18 (1152 × 1152)
//   state    two RGBA32F texels a probe, 128 × 64 a level: (offset, data age,
//            −1 where it is off), (the world cell it stands for, 0)

type Op = (...args: TSLNode[]) => TSLNode
const T = tsl as unknown as Record<string, Op>
const { If, abs, clamp, dot, exp2, float, floor, int, ivec2, length, max, min, mix, normalize, pow, select, texture, textureLoad, uint, vec2, vec3 } = T

/** Tiles along a side of a level's atlas: 4096 probes, 64 × 64. */
export const TILES = Math.sqrt(PROBES)
export const IRRADIANCE_TILE = IRRADIANCE_TEXELS + 2
export const DEPTH_TILE = DEPTH_TEXELS + 2

/** The GPU side of a probe volume, as its users read it (volume.ts makes it). */
export type ProbeNodes = {
  /** Levels allocated: the atlases' height in levels. */
  readonly allocated: number
  /** The irradiance and depth-moment atlases (sampled, bilinear), and the state (loaded). */
  readonly irradiance: TSLNode
  readonly depth: TSLNode
  readonly state: TSLNode
  /**
   * Each level's origin, the world cell its box's lowest corner is in (xyz),
   * locked when it was last updated: a uniform a level, not a uniform array,
   * which three uploads once a render and a picture's warm-up dispatches
   * frames without one.
   */
  readonly origins: readonly TSLNode[]
  /** Levels drawn now (uint). */
  readonly levels: TSLNode
  /** Updates a probe's data fades in over (A0, float). */
  readonly fadeIn: TSLNode
  /** k / s_env: from what the probes store to what is drawn (S2 design P4). */
  readonly toExposed: TSLNode
}

/** a mod 16, never negative, for an int or a float of an integral value. */
export const emodGrid = (a: TSLNode) => a.sub(floor(a.div(GRID)).mul(GRID))

/** The slot a world cell (vec3 of integral floats) is stored in: each axis mod 16 (cascade.ts slotOf). */
export const slotNode = (cell: TSLNode) => {
  const m = emodGrid(cell)
  return uint(m.x.add(m.y.mul(GRID)).add(m.z.mul(GRID * GRID)))
}

/** The world cell a slot (uint) stands for in a box whose lowest corner is origin (vec3, integral): cascade.ts cellOf. */
export const cellNode = (slot: TSLNode, origin: TSLNode) => {
  const at = vec3(float(slot.mod(uint(GRID))), float(slot.div(uint(GRID)).mod(uint(GRID))), float(slot.div(uint(GRID * GRID))))
  return origin.add(emodGrid(at.sub(origin)))
}

/** One of a uniform a level (origins), for a level given as a node. */
export const ofLevel = (perLevel: readonly TSLNode[], level: TSLNode) => perLevel.slice(1).reduce((v: TSLNode, u, i) => select(level.equal(uint(i + 1)), u, v), perLevel[0])

/** A level's probe spacing in metres, for a level given as a node (uint or float). */
export const spacingNode = (level: TSLNode) => exp2(float(level)).mul(spacingOf(0))

/** The state's texels of a probe: (x, y) of its first; the second is beside it. */
export const stateTexel = (slot: TSLNode, level: TSLNode) => ivec2(int(slot.mod(uint(TILES)).mul(uint(2))), int(slot.div(uint(TILES)).add(uint(level).mul(uint(TILES)))))

/** A tile's first texel (its border's corner) in an atlas of tiles of side `tile`. */
export const tileCorner = (slot: TSLNode, level: TSLNode, tile: number) => vec2(float(slot.mod(uint(TILES))), float(slot.div(uint(TILES)).add(uint(level).mul(uint(TILES))))).mul(tile)

/** The UV in an atlas of a direction in a probe's tile: its octahedral point, within the tile's interior. */
function atlasUV(n: TSLNode, slot: TSLNode, level: TSLNode, tile: number, interior: number, allocated: number): TSLNode {
  const e = octEncode(n).mul(0.5).add(0.5)
  const at = tileCorner(slot, level, tile).add(1).add(e.mul(interior))
  return at.div(vec2(TILES * tile, TILES * tile * allocated))
}

/**
 * One level's irradiance at P (normal n, towards the viewer V): the eight
 * probes about it weighed by trilinear, their side of the surface, Chebyshev
 * visibility and whether they are on; where its state names another world
 * cell (moved since, not yet updated: S2 design B3), a probe weighs 0.
 * Returns the irradiance as stored and how far the level covers P: its
 * probes' maturity, faded over the box's outer cell.
 */
function levelAt(v: ProbeNodes, k: number, P: TSLNode, n: TSLNode, V: TSLNode) {
  const s = spacingOf(k)
  const origin = v.origins[k].xyz
  const biased = P.add(n.mul(0.2).add(V.mul(0.8)).mul(0.3 * s)).toVar()
  const local = biased.div(s).sub(origin).toVar()
  const g = local.sub(0.5)
  const base = floor(g).toVar()
  const f = g.sub(base).toVar()
  const edge = min(min(local, vec3(GRID).sub(local)).x, min(min(local, vec3(GRID).sub(local)).y, min(local, vec3(GRID).sub(local)).z))
  const fade = clamp(edge.sub(0.5), 0, 1)
  const sum = vec3(0).toVar()
  const weights = float(0).toVar()
  const mature = float(0).toVar()
  for (let c = 0; c < 8; c++) {
    const off = vec3(c & 1, (c >> 1) & 1, (c >> 2) & 1)
    const at = base.add(off)
    const inside = at.x.greaterThanEqual(0).and(at.y.greaterThanEqual(0)).and(at.z.greaterThanEqual(0)).and(at.x.lessThan(GRID)).and(at.y.lessThan(GRID)).and(at.z.lessThan(GRID))
    const cell = origin.add(at).toVar()
    const slot = slotNode(cell).toVar()
    const texel = stateTexel(slot, uint(k)).toVar()
    const a = textureLoad(v.state, texel, int(0)).toVar()
    const b = textureLoad(v.state, texel.add(ivec2(1, 0)), int(0))
    const valid = inside.and(b.x.equal(cell.x)).and(b.y.equal(cell.y)).and(b.z.equal(cell.z)).and(a.w.greaterThanEqual(0))
    const probe = cell.add(0.5).mul(s).add(a.xyz)
    const toProbe = normalize(probe.sub(P))
    const tri = mix(vec3(1).sub(f), f, off)
    const side = dot(toProbe, n).add(1).mul(0.5)
    const fromProbe = biased.sub(probe).toVar()
    const d = length(fromProbe)
    const m = texture(v.depth, atlasUV(normalize(fromProbe), slot, uint(k), DEPTH_TILE, DEPTH_TEXELS, v.allocated)).level(0).toVar()
    const variance = abs(m.x.mul(m.x).sub(m.y))
    const cheb = select(d.greaterThan(m.x), variance.div(variance.add(d.sub(m.x).mul(d.sub(m.x)))), float(1))
    const w0 = side.mul(side).add(0.2).mul(max(pow(cheb, 3), 0)).max(1e-6)
    // Crush the small weights (DDGI): they otherwise leak through thin walls.
    const w = select(w0.lessThan(0.2), w0.mul(w0).mul(w0).div(0.04), w0).mul(tri.x.mul(tri.y).mul(tri.z)).mul(select(valid, float(1), float(0))).toVar()
    sum.addAssign(texture(v.irradiance, atlasUV(n, slot, uint(k), IRRADIANCE_TILE, IRRADIANCE_TEXELS, v.allocated)).level(0).rgb.mul(w))
    weights.addAssign(w)
    mature.addAssign(w.mul(min(a.w.div(v.fadeIn), 1)))
  }
  const covered = weights.greaterThan(0)
  return {
    stored: select(covered, sum.div(max(weights, 1e-30)), vec3(0)),
    coverage: select(covered, fade.mul(mature.div(max(weights, 1e-30))), float(0)),
  }
}

/**
 * The probes' irradiance at P, exposed and premultiplied by how far they
 * cover it, and that coverage (S2 design P5): the levels from the coarsest
 * drawn to the finest, each laid over the coarser by its own coverage, so
 * what is drawn there is pre + fallback × (1 − coverage). A level whose box
 * does not hold P is not sampled.
 */
export function probeIrradiance(v: ProbeNodes, P: TSLNode, n: TSLNode, V: TSLNode): { pre: TSLNode; coverage: TSLNode } {
  const pre = vec3(0).toVar()
  const rest = float(1).toVar()
  for (let k = Math.min(v.allocated, LEVELS_MAX) - 1; k >= 0; k--) {
    const local = P.div(spacingOf(k)).sub(v.origins[k].xyz)
    const held = local.x.greaterThanEqual(0).and(local.y.greaterThanEqual(0)).and(local.z.greaterThanEqual(0)).and(local.x.lessThan(GRID)).and(local.y.lessThan(GRID)).and(local.z.lessThan(GRID))
    If(v.levels.greaterThan(uint(k)).and(held), () => {
      const l = levelAt(v, k, P, n, V)
      const c = l.coverage.toVar()
      pre.assign(pre.mul(float(1).sub(c)).add(l.stored.mul(v.toExposed).mul(c)))
      rest.mulAssign(float(1).sub(c))
    })
  }
  return { pre, coverage: float(1).sub(rest) }
}
