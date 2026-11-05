import * as tsl from 'three/tsl'
import { Node } from 'three/webgpu'
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
const { If, Loop, abs, clamp, dot, exp2, float, floor, int, ivec2, length, max, min, mix, normalize, pow, select, texture, textureLoad, uint, uniform, vec2, vec3, vec4 } = T
const Fn = tsl.Fn as unknown as (fn: (args: TSLNode[]) => TSLNode) => { setLayout(layout: object): Op }

/** Tiles along a side of a level's atlas: 4096 probes, 64 × 64. */
export const TILES = Math.sqrt(PROBES)
export const IRRADIANCE_TILE = IRRADIANCE_TEXELS + 2
export const DEPTH_TILE = DEPTH_TEXELS + 2

/** The GPU side of a probe volume, as its users read it (volume.ts makes it). */
export type ProbeNodes = {
  /** Levels allocated: the levels a lookup goes through. */
  readonly allocated: number
  /** The atlases' height in levels (float): the levels allocated of the volume drawn. */
  readonly rows: TSLNode
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
  /** The cells the coarsest level drawn fades out over at its box's low faces and high (vec3 each: cascade.ts fadeSpans). */
  readonly fadeLo: TSLNode
  readonly fadeHi: TSLNode
  /** Updates a probe's data fades in over (A0, float). */
  readonly fadeIn: TSLNode
  /** k / s_env: from what the probes store to what is drawn (S2 design P4). */
  readonly toExposed: TSLNode
}

type Valued<T> = TSLNode & { value: T }

/**
 * The probes as a pipeline reads them (scene-gi-stalls): the volume drawn
 * bound to them each frame (bind), so the volume a grown one gives way to (S2
 * design「级数」) is drawn by the same shaders, not by a pipeline built again.
 * Every level a lookup could go through; those the volume drawn has not, its
 * levels (at most its allocated) leave out.
 */
export class ProbeView implements ProbeNodes {
  readonly allocated = LEVELS_MAX
  readonly rows: TSLNode
  readonly irradiance: TSLNode
  readonly depth: TSLNode
  readonly state: TSLNode
  readonly origins: readonly TSLNode[]
  readonly levels: TSLNode
  readonly fadeLo: TSLNode
  readonly fadeHi: TSLNode
  readonly fadeIn: TSLNode
  readonly toExposed: TSLNode

  constructor(first: ProbeNodes) {
    const of = (n: TSLNode) => (n as Valued<unknown>).value
    this.rows = uniform(of(first.rows))
    this.irradiance = texture(of(first.irradiance))
    this.depth = texture(of(first.depth))
    this.state = texture(of(first.state))
    this.origins = first.origins.map((o) => uniform((of(o) as { clone(): unknown }).clone()))
    this.levels = uniform(of(first.levels), 'uint')
    this.fadeLo = uniform((of(first.fadeLo) as { clone(): unknown }).clone())
    this.fadeHi = uniform((of(first.fadeHi) as { clone(): unknown }).clone())
    this.fadeIn = uniform(of(first.fadeIn))
    this.toExposed = uniform(of(first.toExposed))
  }

  /** Reads the volume v from now on, as its last step left it. */
  bind(v: ProbeNodes) {
    const to = <T>(n: TSLNode) => n as Valued<T>
    for (const k of ['rows', 'irradiance', 'depth', 'state', 'levels', 'fadeIn', 'toExposed'] as const) to(this[k]).value = to(v[k]).value
    for (const k of ['fadeLo', 'fadeHi'] as const) to<{ copy(o: unknown): void }>(this[k]).value.copy(to(v[k]).value)
    this.origins.forEach((o, l) => to<{ copy(o: unknown): void }>(o).value.copy(to(v.origins[l]).value))
  }
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
function atlasUV(n: TSLNode, slot: TSLNode, level: TSLNode, tile: number, interior: number, rows: TSLNode): TSLNode {
  const e = octEncode(n).mul(0.5).add(0.5)
  const at = tileCorner(slot, level, tile).add(1).add(e.mul(interior))
  return at.div(vec2(TILES * tile, rows.mul(TILES * tile)))
}

/**
 * One level's irradiance at P (normal n, towards the viewer V): the eight
 * probes about it weighed by trilinear, their side of the surface, Chebyshev
 * visibility and whether they are on; where its state names another world
 * cell (moved since, not yet updated: S2 design B3), a probe weighs 0.
 * Returns the irradiance as stored and how far the level covers P: its
 * probes' maturity, faded over the box's outer cell, or over the cells
 * fadeSpans gives when it is the coarsest level drawn.
 *
 * The level k (uint) and the probe are a loop's, not unrolled in JS: the
 * shader holds the eight probes' reads once, not once a probe, a level and a
 * caller (scene-gi-stalls, root cause 2: 64 copies made the reflections'
 * shader 0.6 MB). The spacing, a power of two times spacingOf(0), is exact
 * as a constant was.
 */
function levelAt(v: ProbeNodes, k: TSLNode, P: TSLNode, n: TSLNode, V: TSLNode) {
  const s = spacingNode(k).toVar()
  const origin = ofLevel(v.origins, k).xyz.toVar()
  const biased = P.add(n.mul(0.2).add(V.mul(0.8)).mul(s.mul(0.5))).toVar()
  const local = biased.div(s).sub(origin).toVar()
  const g = local.sub(0.5)
  const base = floor(g).toVar()
  const f = g.sub(base).toVar()
  const outer = v.levels.equal(k.add(uint(1)))
  const faded = min(clamp(local.sub(0.5).div(select(outer, v.fadeLo, vec3(1))), 0, 1), clamp(vec3(GRID).sub(local).sub(0.5).div(select(outer, v.fadeHi, vec3(1))), 0, 1))
  const fade = min(faded.x, min(faded.y, faded.z))
  const sum = vec3(0).toVar()
  const weights = float(0).toVar()
  const mature = float(0).toVar()
  Loop({ start: uint(0), end: uint(8), type: 'uint', condition: '<', name: 'corner' }, ({ corner: c }: { corner: TSLNode }) => {
    const bit = (j: number) => float(c.shiftRight(uint(j)).bitAnd(uint(1)))
    const off = vec3(bit(0), bit(1), bit(2)).toVar()
    const at = base.add(off)
    const inside = at.x.greaterThanEqual(0).and(at.y.greaterThanEqual(0)).and(at.z.greaterThanEqual(0)).and(at.x.lessThan(GRID)).and(at.y.lessThan(GRID)).and(at.z.lessThan(GRID))
    const cell = origin.add(at).toVar()
    const slot = slotNode(cell).toVar()
    const texel = stateTexel(slot, k).toVar()
    const a = textureLoad(v.state, texel, int(0)).toVar()
    const b = textureLoad(v.state, texel.add(ivec2(1, 0)), int(0))
    const valid = inside.and(b.x.equal(cell.x)).and(b.y.equal(cell.y)).and(b.z.equal(cell.z)).and(a.w.greaterThanEqual(0))
    const probe = cell.add(0.5).mul(s).add(a.xyz)
    const toProbe = normalize(probe.sub(P))
    const tri = mix(vec3(1).sub(f), f, off)
    const side = dot(toProbe, n).add(1).mul(0.5)
    const fromProbe = biased.sub(probe).toVar()
    const d = length(fromProbe)
    const m = texture(v.depth, atlasUV(normalize(fromProbe), slot, k, DEPTH_TILE, DEPTH_TEXELS, v.rows)).level(0).toVar()
    const variance = abs(m.x.mul(m.x).sub(m.y))
    const cheb = select(d.greaterThan(m.x), variance.div(variance.add(d.sub(m.x).mul(d.sub(m.x)))), float(1))
    const w0 = side.mul(side).add(0.2).mul(max(pow(cheb, 3), 0)).max(1e-6)
    // Crush the small weights (DDGI): they otherwise leak through thin walls.
    const w = select(w0.lessThan(0.2), w0.mul(w0).mul(w0).div(0.04), w0).mul(tri.x.mul(tri.y).mul(tri.z)).mul(select(valid, float(1), float(0))).toVar()
    sum.addAssign(texture(v.irradiance, atlasUV(n, slot, k, IRRADIANCE_TILE, IRRADIANCE_TEXELS, v.rows)).level(0).rgb.mul(w))
    weights.addAssign(w)
    mature.addAssign(w.mul(min(a.w.div(v.fadeIn), 1)))
  })
  const covered = weights.greaterThan(0)
  return {
    stored: select(covered, sum.div(max(weights, 1e-30)), vec3(0)),
    coverage: select(covered, fade.mul(mature.div(max(weights, 1e-30))), float(0)),
  }
}

/**
 * What is left uncovered below which the coarser levels are not sampled: their
 * share of the result is at most this (a level's irradiance against its
 * finer ones', about alike), under half a level of an 8-bit picture.
 */
const UNCOVERED = 1 / 1024

/**
 * A volume's lookup called in a shader: the shader function its builder made
 * of it. three keeps the code of a function with a layout per function and
 * backend, the names of the uniforms and textures it reads with it, which are
 * another shader's names in the next shader built; a function a builder, made
 * when it first calls one, keeps them its own.
 */
class LookupCall extends Node {
  private readonly lookupOf: (builder: object) => Op
  private readonly args: TSLNode[]

  constructor(lookupOf: (builder: object) => Op, args: TSLNode[]) {
    super('vec4')
    this.lookupOf = lookupOf
    this.args = args
  }

  setup(builder: object): TSLNode {
    return this.lookupOf(builder)(...this.args)
  }
}

/** Each volume's lookups, a shader function each builder: every caller in a shader calls it (the reflections twice a hit). */
const lookups = new WeakMap<ProbeNodes, (builder: object) => Op>()

/**
 * The probes' irradiance at P, exposed and premultiplied by how far they
 * cover it, and that coverage (S2 design P5): each level laid over the
 * coarser by its own coverage, so what is drawn there is pre + fallback ×
 * (1 − coverage). Gathered from the finest to the coarsest, front to back
 * (pre += rest · c · E, rest ×= 1 − c), as the same sum: once the finer
 * ones cover P but UNCOVERED, the coarser are not sampled, nor is a level
 * whose box does not hold P.
 *
 * A shader function (WGSL), the levels a loop: P, n and V are its
 * parameters, assigned in every level's branch.
 */
export function probeIrradiance(v: ProbeNodes, at: TSLNode, normal: TSLNode, toward: TSLNode): { pre: TSLNode; coverage: TSLNode } {
  let lookupOf = lookups.get(v)
  if (!lookupOf) {
    const made = new WeakMap<object, Op>()
    const make = () =>
      Fn(([P, n, V]) => {
        const pre = vec3(0).toVar()
        const rest = float(1).toVar()
        Loop({ start: uint(0), end: uint(Math.min(v.allocated, LEVELS_MAX)), type: 'uint', condition: '<', name: 'level' }, ({ level: k }: { level: TSLNode }) => {
          const local = P.div(spacingNode(k)).sub(ofLevel(v.origins, k).xyz)
          const held = local.x.greaterThanEqual(0).and(local.y.greaterThanEqual(0)).and(local.z.greaterThanEqual(0)).and(local.x.lessThan(GRID)).and(local.y.lessThan(GRID)).and(local.z.lessThan(GRID))
          If(v.levels.greaterThan(k).and(held).and(rest.greaterThan(UNCOVERED)), () => {
            const l = levelAt(v, k, P, n, V)
            const c = l.coverage.toVar()
            pre.addAssign(l.stored.mul(v.toExposed).mul(c.mul(rest)))
            rest.mulAssign(float(1).sub(c))
          })
        })
        return vec4(pre, float(1).sub(rest))
      }).setLayout({
        name: 'probeIrradiance',
        type: 'vec4',
        inputs: [
          { name: 'P', type: 'vec3' },
          { name: 'n', type: 'vec3' },
          { name: 'V', type: 'vec3' },
        ],
      })
    lookupOf = (builder) => {
      let fn = made.get(builder)
      if (!fn) made.set(builder, (fn = make()))
      return fn
    }
    lookups.set(v, lookupOf)
  }
  const r = (tsl.nodeObject(new LookupCall(lookupOf, [at, normal, toward]) as never) as unknown as TSLNode).toVar()
  return { pre: r.xyz, coverage: r.w }
}
