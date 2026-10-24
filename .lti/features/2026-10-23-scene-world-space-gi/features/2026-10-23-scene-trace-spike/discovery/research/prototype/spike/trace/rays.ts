// S0 group 2: the ray kinds the layer will trace (scene-trace-spike design「判定标准」组 2), each a compute kernel
// over a TraceLayer that writes each ray's hit distance (-1 a miss) so nothing is pruned:
//   primary     one a pixel from the camera, coherent; it also writes the G-buffer the next two start from
//   hemisphere  one a pixel from its primary hit, cosine-weighted: the incoherent kind
//   reflection  one a pixel from its primary hit along a GGX lobe at roughness 0.2: SSR's misses
//   probe       a grid of probes × rays on a spherical Fibonacci set, turned at random each frame: DDGI's
//   shadow      one a pixel from its primary hit toward the sun: a hit's sun shadow where no cascade covers it
// Traversal only: no shading at the hit (group 3).
import { Matrix3, Matrix4, Vector3, type Box3, type PerspectiveCamera } from 'three'
import { abs, cos, cross, dot, float, Fn, hash, If, instancedArray, instanceIndex, normalize, reflect, select, sin, sqrt, uint, uniform, vec2, vec3, vec4 } from 'three/tsl'
import type { TraceLayer } from './layer.ts'
import { traceShim } from './trace.ts'

export type Kind = 'primary' | 'hemisphere' | 'reflection' | 'probe' | 'shadow'

/** The sun the shadow rays go toward: 53° up (the timings do not depend on where it stands). */
const SUN = new Vector3(0.4, 0.8, 0.45).normalize()

/** A ray's origin off its surface, in metres. */
const BIAS = 2e-3
const TAU = 2 * Math.PI

export function rayKernels(layer: TraceLayer, width: number, height: number, probes: { box: Box3; grid: [number, number, number]; rays: number }) {
  const traceFirst = traceShim(layer)
  const pixels = width * height
  const probeCount = probes.grid[0] * probes.grid[1] * probes.grid[2]
  const cameraWorld = uniform(new Matrix4())
  const projectionInverse = uniform(new Matrix4())
  const eye = uniform(new Vector3())
  const seed = uniform(0, 'uint')
  const turn = uniform(new Matrix3())
  const boxMin = uniform(probes.box.min.clone())
  const boxSize = uniform(probes.box.getSize(new Vector3()))
  // G-buffer: (hit position, 1 or 0) and the normal toward the camera.
  const position = instancedArray(pixels, 'vec4')
  const normal = instancedArray(pixels, 'vec4')
  const out = instancedArray(Math.max(pixels, probeCount * probes.rays), 'float')

  const random = (i: unknown, k: number) => hash(uint(i as never).mul(4).add(k).add(seed.mul(0x9e3779b9)))

  /** A frame around n: (t, b). */
  const frame = (n: ReturnType<typeof vec3>) => {
    const t = normalize(cross(select(abs(n.y).lessThan(0.999), vec3(0, 1, 0), vec3(1, 0, 0)), n))
    return [t, cross(n, t)] as const
  }

  const primary = Fn(() => {
    const i = instanceIndex
    const x = i.mod(width)
    const y = i.div(width)
    const ndc = vec2(float(x).add(0.5).div(width).mul(2).sub(1), float(1).sub(float(y).add(0.5).div(height).mul(2)))
    const view = projectionInverse.mul(vec4(ndc, 0.5, 1))
    const world = cameraWorld.mul(vec4(view.xyz.div(view.w), 1)).xyz
    const direction = normalize(world.sub(eye)).toVar()
    const hit = traceFirst(eye, direction).toVar('primary')
    If(hit.get('didHit'), () => {
      const n = vec3(hit.get('normal')).toVar()
      If(dot(n, direction).greaterThan(0), () => {
        n.assign(n.negate())
      })
      position.element(i).assign(vec4(eye.add(direction.mul(hit.get('dist'))), 1))
      normal.element(i).assign(vec4(n, 0))
      out.element(i).assign(hit.get('dist'))
    }).Else(() => {
      position.element(i).assign(vec4(0))
      out.element(i).assign(-1)
    })
  })().compute(pixels, [64])

  const fromHit = (direction: (p: ReturnType<typeof vec3>, n: ReturnType<typeof vec3>, i: typeof instanceIndex) => ReturnType<typeof vec3>) =>
    Fn(() => {
      const i = instanceIndex
      const g = position.element(i)
      If(g.w.greaterThan(0), () => {
        const p = vec3(g.xyz).toVar()
        const n = vec3(normal.element(i).xyz).toVar()
        const hit = traceFirst(p.add(n.mul(BIAS)), direction(p, n, i)).toVar('secondary')
        out.element(i).assign(select(hit.get('didHit'), float(hit.get('dist')), float(-1)))
      }).Else(() => {
        out.element(i).assign(-2)
      })
    })().compute(pixels, [64])

  const hemisphere = fromHit((_, n, i) => {
    const u1 = random(i, 0)
    const u2 = random(i, 1)
    const r = sqrt(u1)
    const phi = u2.mul(TAU)
    const [t, b] = frame(n)
    return normalize(t.mul(r.mul(cos(phi))).add(b.mul(r.mul(sin(phi)))).add(n.mul(sqrt(float(1).sub(u1)))))
  })

  const reflection = fromHit((p, n, i) => {
    const alpha2 = 0.2 ** 4
    const u1 = random(i, 2)
    const u2 = random(i, 3)
    const cosTheta = sqrt(float(1).sub(u1).div(float(1).add(u1.mul(alpha2 - 1))))
    const sinTheta = sqrt(float(1).sub(cosTheta.mul(cosTheta)))
    const phi = u2.mul(TAU)
    const [t, b] = frame(n)
    const h = normalize(t.mul(sinTheta.mul(cos(phi))).add(b.mul(sinTheta.mul(sin(phi)))).add(n.mul(cosTheta)))
    const v = normalize(p.sub(eye))
    const d = reflect(v, h).toVar()
    If(dot(d, n).lessThanEqual(0), () => {
      d.assign(reflect(v, n))
    })
    return d
  })

  const shadow = fromHit(() => vec3(SUN.x, SUN.y, SUN.z))

  const probe = Fn(() => {
    const i = instanceIndex
    const p = i.div(probes.rays)
    const k = i.mod(probes.rays)
    const [gx, gy] = probes.grid
    const cell = vec3(float(p.mod(gx)), float(p.div(gx).mod(gy)), float(p.div(gx * gy)))
    const origin = boxMin.add(cell.add(0.5).div(vec3(...probes.grid)).mul(boxSize))
    // Spherical Fibonacci: k of rays, turned by this frame's rotation.
    const phi = float(k).mul(0.6180339887).fract().mul(TAU)
    const cosTheta = float(1).sub(float(k).mul(2).add(1).div(probes.rays))
    const sinTheta = sqrt(float(1).sub(cosTheta.mul(cosTheta)))
    const direction = normalize(turn.mul(vec3(sinTheta.mul(cos(phi)), sinTheta.mul(sin(phi)), cosTheta)))
    const hit = traceFirst(origin, direction).toVar('probe')
    out.element(i).assign(select(hit.get('didHit'), float(hit.get('dist')), float(-1)))
  })().compute(probeCount * probes.rays, [64])

  return {
    kernels: { primary, hemisphere, reflection, probe, shadow } as Record<Kind, typeof primary>,
    rays: { primary: pixels, hemisphere: pixels, reflection: pixels, probe: probeCount * probes.rays, shadow: pixels } as Record<Kind, number>,
    out,
    aim(camera: PerspectiveCamera) {
      cameraWorld.value.copy(camera.matrixWorld)
      projectionInverse.value.copy(camera.projectionMatrixInverse)
      eye.value.setFromMatrixPosition(camera.matrixWorld)
    },
    /** A frame's random numbers: the seed and the probes' turn. */
    reseed(frame: number) {
      seed.value = frame
      const q = new Vector3(Math.sin(frame * 12.9898) * 0.5 + 0.5, Math.sin(frame * 78.233) * 0.5 + 0.5, Math.sin(frame * 37.719) * 0.5 + 0.5)
      turn.value.setFromMatrix4(new Matrix4().makeRotationAxis(new Vector3(q.x - 0.5, q.y - 0.5, q.z - 0.5).normalize(), q.x * TAU))
    },
  }
}
