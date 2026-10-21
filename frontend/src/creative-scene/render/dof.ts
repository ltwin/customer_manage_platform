import { LinearFilter, Vector2 } from 'three'
import * as tsl from 'three/tsl'
import type { DepthOfField } from '../cameraOptics.ts'
import { maxBlurShare } from '../cameraOptics.ts'
import type { TSLNode } from '../materials/patterns.ts'

// Depth of field by the physical circle of confusion (design「景深」), the
// same formula as cameraOptics.blurPixels. Three steps:
//   1. the largest blur of each 16-pixel tile, so a pixel knows how far a
//      blurred neighbour can reach it;
//   2. at half resolution, a gather over a Vogel disk as wide as the nearby
//      tiles' largest blur, keeping a sample only where its own blur disk
//      covers the pixel. A sample behind the pixel reaches no further than
//      the pixel's own blur, so a sharp foreground never takes in the
//      blurred background behind it;
//   3. a blend of the sharp and the blurred picture by the pixel's blur and
//      how much blurred foreground spills over it.
// three's BokehPass is not used: its blur grows linearly with depth and does
// not follow the f-number. Ported step for step from the WebGL pass (R0:
// mean difference about 0.5 on the reference camera).

type Op = (...args: TSLNode[]) => TSLNode
const { abs, clamp, convertToTexture, cos, float, floor, If, max, min, mix, perspectiveDepthToViewZ, rtt, select, sin, sqrt, uniform, uv, vec2, vec4 } = tsl as unknown as Record<string, Op>
const Fn = tsl.Fn as unknown as (fn: () => TSLNode) => () => TSLNode
const Loop = tsl.Loop as unknown as (...args: unknown[]) => void

const tile = 16

export type Dof = {
  /** The picture with the blur blended in. */
  output: TSLNode
  /**
   * The lens of the frame about to be drawn; distances are along the
   * camera's axis. longSidePx is the long side of the camera's picture in
   * pixels: the whole frame for an export, the camera view's frame in the
   * viewport.
   */
  setLens(dof: DepthOfField, near: number, far: number, longSidePx: number): void
  /** The frame in device pixels. */
  setSize(width: number, height: number): void
  dispose(): void
}

/** color is the picture, depth the opaque surfaces' depth texture of the same frame. */
export function depthOfField(color: TSLNode, depth: TSLNode, samples = 128): Dof {
  const sharp = convertToTexture(color)
  const near = uniform(0.1)
  const far = uniform(1000)
  const focalMM = uniform(35)
  const fStop = uniform(2.8)
  const focusMM = uniform(10000)
  const sensorLongMM = uniform(36)
  const longSidePx = uniform(1024)
  const maxBlurPx = uniform(40)
  const resolution = uniform(new Vector2(1, 1))
  const tileStep = uniform(new Vector2(1, 1))

  const distanceAt = (p: TSLNode): TSLNode => perspectiveDepthToViewZ(depth.sample(p).x, near, far).negate().mul(1000)
  /** Signed: negative in front of the focus. */
  const blurAt = (d0: TSLNode): TSLNode => {
    const f = focalMM
    const s = focusMM
    const d = max(d0, f.mul(1.001)).toVar()
    const c = f.mul(f).div(fStop.mul(s.sub(f))).mul(abs(d.sub(s))).div(d)
    const px = min(c.div(sensorLongMM).mul(longSidePx), maxBlurPx).toVar()
    return select(d.lessThan(s), px.negate(), px)
  }

  const tiles = rtt(
    Fn(() => {
      const start = floor(uv().mul(resolution).div(tile)).mul(tile).toVar()
      const largest = float(0).toVar()
      Loop(8, 8, ({ i, j }: { i: TSLNode; j: TSLNode }) => {
        const at = start.add(vec2(float(j), float(i)).add(0.5).mul(tile / 8)).div(resolution)
        largest.assign(max(largest, abs(blurAt(distanceAt(at)))))
      })
      return vec4(largest, 0, 0, 1)
    })(),
    1,
    1,
  )
  const blurred = rtt(
    Fn(() => {
      const at = uv().toVar()
      const reach = float(1).toVar()
      Loop({ start: -1, end: 1, condition: '<=' }, { start: -1, end: 1, condition: '<=' }, ({ i, j }: { i: TSLNode; j: TSLNode }) => {
        reach.assign(max(reach, tiles.sample(at.add(vec2(float(j), float(i)).mul(tileStep))).r))
      })
      const dc = distanceAt(at).toVar()
      const bc = abs(blurAt(dc)).toVar()
      const sum = sharp.sample(at).rgb.toVar()
      const total = float(1).toVar()
      const spill = float(0).toVar()
      Loop(samples, ({ i }: { i: TSLNode }) => {
        const k = float(i).add(0.5)
        const r = sqrt(k.div(samples)).mul(reach).toVar()
        const a = k.mul(2.39996323)
        const p = at.add(vec2(cos(a), sin(a)).mul(r).div(resolution)).toVar()
        const ds = distanceAt(p).toVar()
        const bs = abs(blurAt(ds))
        const cover = select(ds.greaterThan(dc), min(bs, bc), bs)
        const w = clamp(cover.sub(r).add(1), 0, 1).toVar()
        sum.addAssign(sharp.sample(p).rgb.mul(w))
        total.addAssign(w)
        If(ds.lessThan(dc), () => {
          spill.addAssign(w)
        })
      })
      const mixAmount = clamp(max(bc.sub(1), spill.mul(2).div(total)), 0, 1)
      return vec4(sum.div(total), mixAmount)
    })(),
    1,
    1,
  )
  for (const t of [tiles, blurred]) {
    t.renderTarget.texture.minFilter = LinearFilter
    t.renderTarget.texture.magFilter = LinearFilter
  }

  const output = Fn(() => {
    const s = sharp.sample(uv())
    const b = blurred.sample(uv())
    return vec4(mix(s.rgb, b.rgb, b.a), s.a)
  })()

  return {
    output,
    setLens(dof, n, f, longSide) {
      near.value = n
      far.value = f
      focalMM.value = dof.focalMM
      fStop.value = dof.fStop
      focusMM.value = dof.focusM * 1000
      sensorLongMM.value = dof.sensorLongMM
      longSidePx.value = longSide
      maxBlurPx.value = maxBlurShare * longSide
    },
    setSize(width, height) {
      resolution.value.set(width, height)
      tileStep.value.set(tile / width, tile / height)
      tiles.setSize(Math.ceil(width / tile), Math.ceil(height / tile))
      blurred.setSize(Math.max(1, Math.round(width / 2)), Math.max(1, Math.round(height / 2)))
    },
    dispose() {
      tiles.dispose()
      blurred.dispose()
      sharp.dispose()
    },
  }
}
