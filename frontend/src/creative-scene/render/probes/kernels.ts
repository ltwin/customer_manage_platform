import * as tsl from 'three/tsl'
import type { StorageTexture } from 'three/webgpu'
import type { TSLNode } from '../../materials/patterns.ts'
import '../trace/computePatches.ts'
import { pcg, type HitShading } from '../trace/hit.ts'
import { DEPTH_TEXELS, IRRADIANCE_TEXELS, PROBES, RAYS, RELOCATION_UPDATES } from './cascade.ts'
import { cellNode, DEPTH_TILE, IRRADIANCE_TILE, ofLevel, probeIrradiance, spacingNode, stateTexel, tileCorner, type ProbeNodes } from './lookup.ts'
import { octDecode } from './octahedral.ts'

// The probes' compute kernels (S2 design「探针光线」「更新」): a frame
// dispatches the rays of every level due, then the updates of every level
// due (P2), each over the due levels one after another (due, in volume.ts's
// order). A thread a ray; a workgroup a probe for the update, its 64 rays
// read once into workgroup memory. Each texel is written by one thread, the
// sums in a fixed order, no atomics (P3).
//
// A probe's state (vec4s, as volume.ts allocates them):
//   offsets  (relocation offset, on: 1 or 0)
//   cells    (the world cell it stands for, the edit count it last saw)
//   ages     (data, edit, relocation, the relocation count it last saw)

type Op = (...args: TSLNode[]) => TSLNode
const T = tsl as unknown as Record<string, Op>
const { Fn, If, Loop, clamp, cos, dot, float, fract, int, ivec2, max, min, mix, normalize, pow, select, sin, sqrt, storageTexture, textureLoad, textureStore, uint, uvec2, vec2, vec3, vec4, workgroupArray, workgroupBarrier } = T
const { instanceIndex } = tsl as unknown as Record<string, TSLNode>

/** The GPU data of a volume the kernels read and write (volume.ts). */
export type ProbeData = {
  readonly nodes: ProbeNodes
  /** A ray's radiance as stored and hit distance, allocated × 4096 × 64. */
  readonly rays: TSLNode
  /** The irradiance history (rgb as stored), a texel each, allocated × 4096 × 64. */
  readonly irradiance: TSLNode
  /** The depth moments' history, allocated × 4096 × 256. */
  readonly depth: TSLNode
  readonly offsets: TSLNode
  readonly cells: TSLNode
  readonly ages: TSLNode
  readonly atlases: { irradiance: StorageTexture; depth: StorageTexture; state: StorageTexture }
  readonly u: {
    /** The levels this frame updates, in order: a uint uniform each, 5 (ProbeNodes.origins says why not an array). */
    due: readonly TSLNode[]
    /** This frame's turn of every probe's ray set. */
    rotation: TSLNode
    frame: TSLNode
    /** The hysteresis (cascade.ts Hysteresis). */
    A0: TSLNode
    alphaMax: TSLNode
    editCount: TSLNode
    relocationCount: TSLNode
    /** Where geometry changed, for the relocation count (world, empty as min > max). */
    editedMin: TSLNode
    editedMax: TSLNode
    /** s_env / k: from what is drawn to what the probes store (S2 design P4). */
    toStored: TSLNode
    /** s_new / s_old, for the rescale. */
    rescale: TSLNode
    /** The ambient fill (S2 design r_fill). */
    fill: TSLNode
  }
}

/** Ray k of a probe's 64: a spherical Fibonacci point, turned by this frame's rotation (S0). */
function rayDir(d: ProbeData, k: TSLNode): TSLNode {
  const phi = fract(float(k).mul(0.6180339887)).mul(2 * Math.PI)
  const z = float(1).sub(float(k).mul(2).add(1).div(RAYS))
  const r = sqrt(max(float(1).sub(z.mul(z)), 0))
  return normalize(d.u.rotation.mul(vec3(r.mul(cos(phi)), r.mul(sin(phi)), z)))
}

/** Which probe a thread works for: its level, slot, index among all allocated, spacing and world cell; lane is its place within the probe's 64. */
function probeOf(d: ProbeData) {
  const lane = instanceIndex.mod(uint(RAYS)).toVar()
  const at = instanceIndex.div(uint(RAYS)).toVar()
  const level = ofLevel(d.u.due, at.div(uint(PROBES))).toVar()
  const slot = at.mod(uint(PROBES)).toVar()
  const g = level.mul(uint(PROBES)).add(slot).toVar()
  const s = spacingNode(level).toVar()
  const cell = cellNode(slot, ofLevel(d.nodes.origins, level).xyz).toVar()
  return { lane, level, slot, g, s, cell }
}

/** Whether a probe's state names cell: written for it, not for the cell its slot stood for before the box moved. */
const names = (was: TSLNode, cell: TSLNode) => was.x.equal(cell.x).and(was.y.equal(cell.y)).and(was.z.equal(cell.z))

/**
 * The rays (S2 design「探针光线」): one thread a ray, from the probe's place,
 * shaded by the probes' layer, the bounces from the probes as they were. The
 * probe's offset from the state texture, not its buffers: with the layer's
 * four, the vertex colours, the tables and the rays, seven storage buffers,
 * within WebGPU's default eight (S2 design「能力与显存」).
 */
export function rayKernel(d: ProbeData, shading: HitShading, count: number) {
  return Fn(() => {
    const p = probeOf(d)
    const texel = stateTexel(p.slot, p.level).toVar()
    const state = textureLoad(d.nodes.state, texel, int(0)).toVar()
    const kept = names(textureLoad(d.nodes.state, texel.add(ivec2(1, 0)), int(0)), p.cell)
    const offset = select(kept, state.xyz, vec3(0))
    const origin = p.cell.add(0.5).mul(p.s).add(offset)
    const dir = rayDir(d, p.lane).toVar()
    // P3: the frame, the probe and the ray, so a probe's 64 rays draw their lights apart.
    const seed = pcg(d.u.frame.mul(uint(0x9e3779b1)).bitXor(pcg(p.g.mul(uint(RAYS)).add(p.lane))))
    const indirect = (P: TSLNode, n: TSLNode) => {
      const { pre, coverage } = probeIrradiance(d.nodes, P, n, dir.negate())
      return pre.add(shading.environmentIrradiance(n).mul(float(1).sub(coverage)))
    }
    const L = shading.radiance(origin, dir, seed, indirect, d.u.fill).toVar()
    d.rays.element(p.g.mul(uint(RAYS)).add(p.lane)).assign(vec4(L.xyz.mul(d.u.toStored), L.w))
  })()
    .compute(count, [RAYS])
    .setName('probe_rays')
}

/**
 * Writes value at interior texel (i, j) of a probe's n-texel tile, and at the
 * border texels that copy it (octahedral.ts bordersOf): across an edge the
 * row reversed, at a corner the opposite interior corner.
 */
function storeTile(atlas: TSLNode, slot: TSLNode, level: TSLNode, tile: number, n: number, i: TSLNode, j: TSLNode, value: TSLNode) {
  const corner = tileCorner(slot, level, tile).add(1).toVar()
  // A statement, returning nothing: a TSL If whose callback returns a node builds it as the branch's value.
  const put = (x: TSLNode, y: TSLNode) => {
    textureStore(atlas, uvec2(corner.add(vec2(x, y))), value)
  }
  const last = n - 1
  put(i, j)
  If(j.equal(0), () => put(float(last).sub(i), float(-1)))
  If(j.equal(last), () => put(float(last).sub(i), float(n)))
  If(i.equal(0), () => put(float(-1), float(last).sub(j)))
  If(i.equal(last), () => put(float(n), float(last).sub(j)))
  If(i.equal(last).and(j.equal(last)), () => put(float(-1), float(-1)))
  If(i.equal(0).and(j.equal(last)), () => put(float(n), float(-1)))
  If(i.equal(last).and(j.equal(0)), () => put(float(-1), float(n)))
  If(i.equal(0).and(j.equal(0)), () => put(float(n), float(n)))
}

/** The direction texel (i, j) of an n-texel tile stands for. */
const texelDir = (i: TSLNode, j: TSLNode, n: number) => octDecode(vec2(i.add(0.5).div(n).mul(2).sub(1), j.add(0.5).div(n).mul(2).sub(1)))

/**
 * The updates (S2 design「更新」): a workgroup a probe. Its rays and their
 * directions into workgroup memory; each thread an irradiance texel and four
 * depth-moment texels over all 64, blended into the history by the probe's
 * hysteresis and written to the atlases with their borders; thread 0 the
 * probe's state: the world cell (a cell other than the one its state names
 * is a ring reset: no history, every age 0), relocation while its window is
 * open, switching off as it closes, the three ages.
 */
export function updateKernel(d: ProbeData, count: number) {
  const irradianceAtlas = storageTexture(d.atlases.irradiance).toWriteOnly()
  const depthAtlas = storageTexture(d.atlases.depth).toWriteOnly()
  const stateAtlas = storageTexture(d.atlases.state).toWriteOnly()
  return Fn(() => {
    const p = probeOf(d)
    const radiance = workgroupArray('vec4', RAYS)
    const directions = workgroupArray('vec4', RAYS)
    radiance.element(p.lane).assign(d.rays.element(p.g.mul(uint(RAYS)).add(p.lane)))
    directions.element(p.lane).assign(vec4(rayDir(d, p.lane), 0))
    const was = d.cells.element(p.g).toVar()
    const ages = d.ages.element(p.g).toVar()
    const reset = names(was, p.cell).not().toVar()
    const dataAge = select(reset, float(0), ages.x).toVar()
    const editAge = select(reset.or(was.w.notEqual(float(d.u.editCount))), float(0), ages.y).toVar()
    // No history for data age 0 (a slot just cleared); none for A0 updates after an edit, then rising to alphaMax.
    const rising = min(d.u.alphaMax, float(1).sub(float(1).div(editAge.sub(d.u.A0).add(2))))
    const alpha = select(dataAge.equal(0).or(editAge.lessThan(d.u.A0)), float(0), rising).toVar()
    workgroupBarrier()

    // The irradiance texel of this lane: the cosine-weighted mean radiance, × π (S0「辐照度的归一化」).
    const ti = float(p.lane.mod(uint(IRRADIANCE_TEXELS)))
    const tj = float(p.lane.div(uint(IRRADIANCE_TEXELS)))
    const towards = texelDir(ti, tj, IRRADIANCE_TEXELS).toVar()
    const sum = vec3(0).toVar()
    const weight = float(0).toVar()
    Loop(RAYS, ({ i: k }: { i: TSLNode }) => {
      const c = max(dot(towards, directions.element(k).xyz), 0)
      sum.addAssign(radiance.element(k).xyz.mul(c))
      weight.addAssign(c)
    })
    const index = p.g.mul(uint(RAYS)).add(p.lane)
    const E = mix(sum.div(max(weight, 1e-4)).mul(Math.PI), d.irradiance.element(index).xyz, alpha).toVar()
    d.irradiance.element(index).assign(vec4(E, 1))
    storeTile(irradianceAtlas, p.slot, p.level, IRRADIANCE_TILE, IRRADIANCE_TEXELS, ti, tj, vec4(E, 1))

    // Four depth-moment texels: the mean distance and its square, weighted cos⁵⁰; a back face's distance a fifth
    // of it (DDGI: the probe reads as inside); as far as 1.5 cell diagonals at most (DDGI).
    const farthest = p.s.mul(1.5 * Math.sqrt(3)).toVar()
    for (let m = 0; m < (DEPTH_TEXELS * DEPTH_TEXELS) / RAYS; m++) {
      const t = p.lane.add(uint(m * RAYS))
      const di = float(t.mod(uint(DEPTH_TEXELS)))
      const dj = float(t.div(uint(DEPTH_TEXELS)))
      const along = texelDir(di, dj, DEPTH_TEXELS).toVar()
      const moments = vec2(0).toVar()
      const w = float(0).toVar()
      Loop(RAYS, ({ i: k }: { i: TSLNode }) => {
        const r = radiance.element(k).w
        const dist = min(select(r.lessThan(0), r.negate().mul(0.2), r), farthest)
        const c = pow(max(dot(along, directions.element(k).xyz), 0), 50)
        moments.addAssign(vec2(dist, dist.mul(dist)).mul(c))
        w.addAssign(c)
      })
      const at = p.g.mul(uint(DEPTH_TEXELS * DEPTH_TEXELS)).add(t)
      const blended = mix(moments.div(max(w, 1e-4)), d.depth.element(at), alpha).toVar()
      d.depth.element(at).assign(blended)
      storeTile(depthAtlas, p.slot, p.level, DEPTH_TILE, DEPTH_TEXELS, di, dj, vec4(blended, 0, 1))
    }

    // The state, thread 0 alone (RTXGI's relocation; S0's switching off).
    If(p.lane.equal(0), () => {
      const backs = float(0).toVar()
      const front = float(1e30).toVar()
      const frontDir = vec3(0).toVar()
      const back = float(1e30).toVar()
      const backDir = vec3(0).toVar()
      Loop(RAYS, ({ i: k }: { i: TSLNode }) => {
        const r = radiance.element(k).w.toVar()
        If(r.lessThan(0), () => {
          backs.addAssign(1)
          If(r.negate().lessThan(back), () => {
            back.assign(r.negate())
            backDir.assign(directions.element(k).xyz)
          })
        }).Else(() => {
          If(r.lessThan(front), () => {
            front.assign(r)
            frontDir.assign(directions.element(k).xyz)
          })
        })
      })
      const kept = d.offsets.element(p.g).toVar()
      const offset = select(reset, vec3(0), kept.xyz).toVar()
      const on = select(reset, float(1), kept.w).toVar()
      // The relocation window opens again where geometry changed, once the edit is over (S2 design「编辑的分类」).
      const at = p.cell.add(0.5).mul(p.s).add(offset)
      const near = at.greaterThanEqual(d.u.editedMin.sub(p.s)).all().and(at.lessThanEqual(d.u.editedMax.add(p.s)).all())
      const reopened = ages.w.notEqual(float(d.u.relocationCount)).and(near)
      const relocationAge = select(reset.or(reopened), float(0), ages.z).toVar()
      const inside = backs.greaterThan(RAYS / 4)
      If(relocationAge.lessThan(RELOCATION_UPDATES), () => {
        // Inside something: out through its nearest back face; else away from a face nearer than a fifth of a cell.
        If(inside, () => {
          offset.addAssign(backDir.mul(back.add(p.s.mul(0.1))))
        }).ElseIf(front.lessThan(p.s.mul(0.2)), () => {
          offset.subAssign(frontDir.mul(p.s.mul(0.2).sub(front)))
        })
        offset.assign(clamp(offset, p.s.mul(-0.45), p.s.mul(0.45)))
      })
      const relocated = relocationAge.add(1)
      // As the window closes, a probe still inside switches off; reset, it is on again.
      on.assign(select(relocated.equal(RELOCATION_UPDATES), select(inside, float(0), float(1)), on))
      const updates = dataAge.add(1)
      d.offsets.element(p.g).assign(vec4(offset, on))
      d.cells.element(p.g).assign(vec4(p.cell, float(d.u.editCount)))
      d.ages.element(p.g).assign(vec4(min(updates, 1e6), min(editAge.add(1), 1e6), min(relocated, 1e6), float(d.u.relocationCount)))
      const texel = stateTexel(p.slot, p.level)
      textureStore(stateAtlas, uvec2(texel), vec4(offset, select(on.greaterThan(0.5), updates, float(-1))))
      textureStore(stateAtlas, uvec2(texel.add(ivec2(1, 0))), vec4(p.cell, 0))
    })
  })()
    .compute(count, [RAYS])
    .setName('probe_update')
}

/**
 * The sky's light scale changed (S2 design P4): every level's irradiance
 * history and atlas times s_new / s_old, a thread a texel; the depth and the
 * state stay. The probes then follow the new light by lightingVersion.
 */
export function rescaleKernel(d: ProbeData, count: number) {
  const irradianceAtlas = storageTexture(d.atlases.irradiance).toWriteOnly()
  return Fn(() => {
    const lane = instanceIndex.mod(uint(RAYS))
    const g = instanceIndex.div(uint(RAYS))
    const level = g.div(uint(PROBES))
    const slot = g.mod(uint(PROBES))
    const E = d.irradiance.element(instanceIndex).xyz.mul(d.u.rescale).toVar()
    d.irradiance.element(instanceIndex).assign(vec4(E, 1))
    storeTile(irradianceAtlas, slot, level, IRRADIANCE_TILE, IRRADIANCE_TEXELS, float(lane.mod(uint(IRRADIANCE_TEXELS))), float(lane.div(uint(IRRADIANCE_TEXELS))), vec4(E, 1))
  })()
    .compute(count, [RAYS])
    .setName('probe_rescale')
}
