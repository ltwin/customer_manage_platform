import { abs, clamp, cos, float, floor, Fn, If, Loop, max, min, mix, perspectiveDepthToViewZ, rtt, select, sin, sqrt, uniform, uv, vec2, vec4 } from 'three/tsl'
import { HalfFloatType, LinearFilter } from 'three/webgpu'
import { maxBlurShare, type DepthOfField } from '../../src/creative-scene/cameraOptics.ts'

// Group 2: render/dofPass.ts in TSL, step for step: the largest blur of each
// 16-pixel tile, a half-resolution gather over a Vogel disk as wide as the
// nearby tiles' largest blur, and a blend of the sharp and blurred picture.
// The circle of confusion is cameraOptics.blurPixels's.

/* eslint-disable @typescript-eslint/no-explicit-any -- TSL nodes are loosely typed in three's typings */
type N = any

const tile = 16

export type DofNodes = { output: N; setLens(dof: DepthOfField, near: number, far: number, longSidePx: number): void; dispose(): void }

/** color and depth are the scene pass's texture nodes; width × height the frame in pixels. */
export function depthOfField(color: N, depth: N, width: number, height: number, samples = 128): DofNodes {
  const near = uniform(0.1)
  const far = uniform(1000)
  const focalMM = uniform(35)
  const fStop = uniform(2.8)
  const focusMM = uniform(10000)
  const sensorLongMM = uniform(36)
  const longSidePx = uniform(1024)
  const maxBlurPx = uniform(40)
  const resolution = vec2(width, height)

  const distanceAt = (p: N): N => perspectiveDepthToViewZ(depth.sample(p).x, near, far).negate().mul(1000)
  /** Signed: negative in front of the focus. */
  const blurAt = (d0: N): N => {
    const f = focalMM
    const s = focusMM
    const d = max(d0, f.mul(1.001)).toVar()
    const c = f.mul(f).div(fStop.mul(s.sub(f))).mul(abs(d.sub(s))).div(d)
    const px = min(c.div(sensorLongMM).mul(longSidePx), maxBlurPx).toVar()
    return select(d.lessThan(s), px.negate(), px)
  }

  const tileFn = Fn(() => {
    const start = floor(uv().mul(resolution).div(tile)).mul(tile).toVar()
    const largest = float(0).toVar()
    Loop(8, 8, ({ i, j }: { i: N; j: N }) => {
      const at = start.add(vec2(float(j), float(i)).add(0.5).mul(tile / 8)).div(resolution)
      largest.assign(max(largest, abs(blurAt(distanceAt(at)))))
    })
    return vec4(largest, 0, 0, 1)
  })
  const tiles = rtt(tileFn(), Math.ceil(width / tile), Math.ceil(height / tile), { type: HalfFloatType })
  tiles.renderTarget.texture.minFilter = LinearFilter
  tiles.renderTarget.texture.magFilter = LinearFilter
  const tileStep = vec2(tile / width, tile / height)

  const gatherFn = Fn(() => {
    const at = uv().toVar()
    const reach = float(1).toVar()
    Loop({ start: -1, end: 1, condition: '<=' }, { start: -1, end: 1, condition: '<=' }, ({ i, j }: { i: N; j: N }) => {
      reach.assign(max(reach, tiles.sample(at.add(vec2(float(j), float(i)).mul(tileStep))).r))
    })
    const dc = distanceAt(at).toVar()
    const bc = abs(blurAt(dc)).toVar()
    const sum = color.sample(at).rgb.toVar()
    const total = float(1).toVar()
    const spill = float(0).toVar()
    Loop(samples, ({ i }: { i: N }) => {
      const k = float(i).add(0.5)
      const r = sqrt(k.div(samples)).mul(reach).toVar()
      const a = k.mul(2.39996323)
      const p = at.add(vec2(cos(a), sin(a)).mul(r).div(resolution)).toVar()
      const ds = distanceAt(p).toVar()
      const bs = abs(blurAt(ds))
      const cover = select(ds.greaterThan(dc), min(bs, bc), bs)
      const w = clamp(cover.sub(r).add(1), 0, 1).toVar()
      sum.addAssign(color.sample(p).rgb.mul(w))
      total.addAssign(w)
      If(ds.lessThan(dc), () => {
        spill.addAssign(w)
      })
    })
    const mixAmount = clamp(max(bc.sub(1), spill.mul(2).div(total)), 0, 1)
    return vec4(sum.div(total), mixAmount)
  })
  const blurred = rtt(gatherFn(), Math.max(1, Math.round(width / 2)), Math.max(1, Math.round(height / 2)), { type: HalfFloatType })
  blurred.renderTarget.texture.minFilter = LinearFilter
  blurred.renderTarget.texture.magFilter = LinearFilter

  const output = Fn(() => {
    const sharp = color.sample(uv())
    const b = blurred.sample(uv())
    return vec4(mix(sharp.rgb, b.rgb, b.a), sharp.a)
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
    dispose() {
      tiles.dispose()
      blurred.dispose()
    },
  }
}
