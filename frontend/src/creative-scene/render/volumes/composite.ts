import type { Camera, Matrix4 } from 'three'
import * as tsl from 'three/tsl'
import type { TSLNode } from '../../materials/patterns.ts'
import { aerialAt } from '../../world/atmosphere/aerial.ts'
import { hitVolume } from './medium.ts'
import { VolumeNode, viewRay } from './march.ts'
import { MAX_VOLUMES, V } from './state.ts'

// The volumes over the picture (Epic design「合成顺序」, revision v9;
// scene-volumes design「模糊与合成」): after the aerial perspective, with the
// air in front of the volumes and behind them taken apart at the depth d_v
// the volumes sit at:
//
//   out = S(d_v)·(1 − T_vol) + T(d_v)·L_vol + T_vol·AP(C)
//
// AP(C) the picture with its aerial perspective, (S, T) the air up to a
// point (aerialAt). Only where the pixel's own ray crosses a volume, tested
// at full resolution: everywhere else the picture is AP(C) exactly, and the
// low resolution and the blur cannot spread the volumes past their outline.

type Op = (...args: TSLNode[]) => TSLNode
const { abs, clamp, dot, float, floor, int, ivec2, max, min, uniform, vec2, vec3, vec4 } = tsl as unknown as Record<string, Op>
const { screenUV } = tsl as unknown as Record<string, TSLNode>
const Fn = tsl.Fn as unknown as (fn: () => TSLNode) => () => TSLNode
const If = tsl.If as unknown as (cond: TSLNode, then: () => void) => void
type Uniform<T> = TSLNode & { value: T }

/** How near in view depth a low-resolution texel must be to count when upsampling (a share of the pixel's). */
const DEPTH_LIKENESS = 0.02

/**
 * The volumes of node over color (rgb, linear, pre-exposed, after the
 * aerial perspective) at the surface depth shows, for camera. With no
 * volumes drawn, color as it was.
 */
export function applyVolumes(color: TSLNode, depth: TSLNode, camera: Camera, node: VolumeNode): TSLNode {
  const projectionInverse = uniform(camera.projectionMatrixInverse)
  const projection = uniform(camera.projectionMatrix) as Uniform<Matrix4>
  const world = uniform(camera.matrixWorld)
  return Fn(() => {
    const picture = vec3(color).toVar()
    const out = vec3(picture).toVar()
    If(V.on.greaterThan(0.5), () => {
      const uv = screenUV
      const ray = viewRay(uv, depth.sample(uv).r, projectionInverse)
      const origin = world.mul(vec4(ray.start, 1)).xyz
      const dir = world.mul(vec4(ray.direction, 0)).xyz
      const covered = float(0).toVar()
      for (let i = 0; i < MAX_VOLUMES; i++)
        If(int(i).lessThan(V.count), () => {
          const h = hitVolume(V, int(i), origin, dir)
          If(min(h.y, ray.end).greaterThan(max(h.x, 0)), () => covered.assign(1))
        })
      If(covered.greaterThan(0.5), () => {
        // Four texels, by bilinear share times how alike their view depth is to the pixel's.
        const res = node.resolution
        const at = uv.mul(res).sub(0.5)
        const base = floor(at)
        const f = at.sub(base)
        const L = vec3(0).toVar()
        const T = vec3(0).toVar()
        const where = float(0).toVar()
        const sum = float(0).toVar()
        const opacity = float(0).toVar()
        for (const [ox, oy] of [
          [0, 0],
          [1, 0],
          [0, 1],
          [1, 1],
        ] as const) {
          const c = ivec2(clamp(base.add(vec2(ox, oy)), vec2(0), res.sub(1)))
          const l = node.light.load(c)
          const t = node.transmittance.load(c)
          const share = (ox ? f.x : f.x.oneMinus()).mul(oy ? f.y : f.y.oneMinus())
          const w = share.add(1e-3).div(abs(l.a.sub(ray.depth)).div(ray.depth.mul(DEPTH_LIKENESS)).add(1)).toVar()
          L.addAssign(l.rgb.mul(w))
          T.addAssign(t.rgb.mul(w))
          sum.addAssign(w)
          // Where the volumes sit, weighted by what they take away.
          const o = w.mul(float(1).sub(dot(t.rgb, vec3(1 / 3)))).add(1e-6)
          where.addAssign(t.a.mul(o))
          opacity.addAssign(o)
        }
        const light = L.div(sum)
        const through = clamp(T.div(sum), 0, 1)
        const air = aerialAt(ray.start.add(ray.direction.mul(where.div(opacity))), projection)
        out.assign(air.rgb.mul(vec3(1).sub(through)).add(light.mul(air.a)).add(through.mul(picture)))
      })
    })
    return out
  })()
}
