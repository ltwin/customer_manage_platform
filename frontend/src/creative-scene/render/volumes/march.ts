import { HalfFloatType, Mesh, NearestFilter, OrthographicCamera, PlaneGeometry, Vector2, type Camera, type Texture } from 'three'
import * as tsl from 'three/tsl'
import { NodeMaterial, NodeUpdateType, QuadMesh, RenderTarget, RendererUtils, TempNode, type NodeBuilder, type NodeFrame } from 'three/webgpu'
import type { TSLNode } from '../../materials/patterns.ts'
import { gpuTestHooks } from '../gpu.ts'
import { chordThrough, hitVolume, volumeDensity } from './medium.ts'
import { volumeLights } from './lights.ts'
import { bindVolumeTextures, lighting, MAX_VOLUMES, V } from './state.ts'

// The march (scene-volumes design「步进」「模糊与合成」): per pixel at half the
// CSS resolution, a ray from the near plane to the opaque surface, stepped
// only where it crosses a volume. The crossings are sorted by entry and
// merged; 48 steps are shared out by length, at least 4 to a segment. Each
// step lights the volumes it is in by every light of the slots (shadowed,
// times the volume's own chord toward the light) and the sky light, and
// integrates as Hillaire 2015. Out come the light scattered in (rgb) and
// the view depth (a), what is let through (rgb) and the view-ray distance
// the volumes sit at, weighted by what they take away (a); then a blur
// each way that keeps to like depths.

type Op = (...args: TSLNode[]) => TSLNode
const { abs, clamp, dot, exp, float, floor, getViewPosition, int, interleavedGradientNoise, ivec2, length, max, min, normalize, outputStruct, passTexture, pow, select, struct, textureLoad, uniform, vec2, vec3, vec4 } = tsl as unknown as Record<string, Op>
const { screenCoordinate, screenUV } = tsl as unknown as Record<string, TSLNode>
const Fn = tsl.Fn as unknown as (fn: () => TSLNode) => () => TSLNode
const If = tsl.If as unknown as (cond: TSLNode, then: () => void) => { Else(otherwise: () => void): void }
const Loop = tsl.Loop as unknown as (count: TSLNode | number, body: (i: { i: TSLNode }) => void) => void
type Uniform<T> = TSLNode & { value: T }

/** Steps a ray takes through the volumes, and the least a segment gets. */
export const STEPS = 48
export const MIN_STEPS = 4
/** The blur's taps each way, and how near in view depth a tap must be (a share of the centre's). */
const BLUR_RADIUS = 3
const DEPTH_LIKENESS = 0.02
/** The view depth the sky stands at, the march's, the blur's and the composite's alike. */
export const SKY_DEPTH = 1e4
/** Kept below half float's largest: an inf would turn the composite to NaN. */
const MAX_LIGHT = 6e4

/**
 * Batcher's network for 8 (19 comparators): the crossings sorted by entry
 * with no indexing into arrays, which TSL cannot do on locals.
 */
export const SORT8: readonly (readonly [number, number])[] = [
  [0, 2], [1, 3], [4, 6], [5, 7],
  [0, 4], [1, 5], [2, 6], [3, 7],
  [0, 1], [2, 3], [4, 5], [6, 7],
  [2, 4], [3, 5],
  [1, 4], [3, 6],
  [1, 2], [3, 4], [5, 6],
]

/**
 * The ray of a pixel in view space: from the near plane through the pixel,
 * perspective or orthographic alike, to the opaque surface at depth d (a
 * depth texture's value), or to SKY_DEPTH where there is none.
 */
export function viewRay(uv: TSLNode, d: TSLNode, projectionInverse: TSLNode) {
  const start = getViewPosition(uv, float(0), projectionInverse).toVar()
  const direction = normalize(getViewPosition(uv, float(1), projectionInverse).sub(start)).toVar()
  const sky = d.greaterThanEqual(1)
  const surface = getViewPosition(uv, d, projectionInverse)
  const end = select(sky, float(SKY_DEPTH).add(start.z).div(direction.z.negate()), length(surface.sub(start)))
  const depth = select(sky, float(SKY_DEPTH), surface.z.negate())
  return { start, direction, end, depth }
}

const Pair = struct({ a: 'vec4', b: 'vec4' })
const both = (f: () => TSLNode) => {
  const r = f()
  return outputStruct(r.get('a'), r.get('b'))
}

const hg = (g: TSLNode, c: TSLNode) => {
  const g2 = g.mul(g)
  return g2.oneMinus().div(pow(g2.add(1).sub(g.mul(c).mul(2)), 1.5).mul(4 * Math.PI))
}

const luma = (c: TSLNode) => dot(c, vec3(1 / 3))

function target(): RenderTarget {
  const rt = new RenderTarget(1, 1, { type: HalfFloatType, count: 2, depthBuffer: false })
  for (const t of rt.textures) {
    t.minFilter = t.magFilter = NearestFilter
    t.generateMipmaps = false
  }
  return rt
}

let rendererState: ReturnType<typeof RendererUtils.resetRendererState> | undefined
const drawn = new Vector2()

export class VolumeNode extends TempNode {
  /** The opaque surfaces' depth, and the passes that must draw first (the scene pass that draws the shadow maps). */
  private readonly depth: TSLNode
  private readonly after: readonly TSLNode[]
  private readonly projectionInverse: TSLNode
  private readonly world: TSLNode
  private readonly marched = target()
  private readonly across = target()
  private readonly blurred = target()
  /** The march's size in pixels. */
  readonly resolution = uniform(new Vector2(1, 1)) as Uniform<Vector2>
  /** The blurred light (rgb) and view depth (a); what is let through (rgb) and where (a). */
  readonly light: TSLNode
  readonly transmittance: TSLNode
  /** 1 once the march for the lights as they are has drawn: the composite leaves the picture alone until then. */
  readonly ready = uniform(0) as Uniform<number>
  /**
   * The viewport's: compile a new march on the driver's threads and draw again once it is ready, as a new light
   * warms (objects/SceneLights.tsx). Without it (a shot) the first frame compiles it as it draws.
   */
  private readonly warm: (() => void) | null
  private march: NodeMaterial | null = null
  private compiling = false
  private signature = ''
  private lights: ReturnType<typeof volumeLights> | null = null
  private readonly blurX = new NodeMaterial()
  private readonly blurY = new NodeMaterial()
  private readonly quad = new QuadMesh(this.blurX)
  /**
   * The march's own quad, a plain mesh: compileAsync builds a QuadMesh's vertex stage unlike its draw does (the
   * draw takes the full-screen triangle from the vertex index), and the frame compiled it again. A plane in front
   * of an orthographic camera compiles ahead as it draws.
   */
  private readonly marchQuad = new Mesh(new PlaneGeometry(2, 2), this.blurX)
  private readonly marchCamera = new OrthographicCamera(-1, 1, 1, -1, 0, 1)

  constructor(depth: TSLNode, camera: Camera, after: readonly TSLNode[] = [], warm: (() => void) | null = null) {
    super('vec4')
    this.marchQuad.frustumCulled = false
    this.marchCamera.position.z = 0.5
    this.marchCamera.updateMatrixWorld()
    this.depth = depth
    this.after = after
    this.warm = warm
    this.projectionInverse = uniform(camera.projectionMatrixInverse)
    this.world = uniform(camera.matrixWorld)
    this.updateBeforeType = NodeUpdateType.FRAME
    this.light = passTexture(this, this.blurred.textures[0])
    this.transmittance = passTexture(this, this.blurred.textures[1])
    this.blurX.fragmentNode = this.blur(this.marched, 1, 0)
    this.blurY.fragmentNode = this.blur(this.across, 0, 1)
  }

  /** The march's code for the slots prepareVolumes chose; made on the first frame with volumes, again when they change. */
  private marchMaterial(): NodeMaterial {
    const lights = volumeLights(lighting.slots, lighting.environment)
    this.lights = lights
    gpuTestHooks.marches++
    const { depth, projectionInverse, world } = this
    const material = new NodeMaterial()
    material.name = 'Volumes'
    material.fragmentNode = both(
      Fn(() => {
        const uv = screenUV
        const ray = viewRay(uv, depth.sample(uv).r, projectionInverse)
        const origin = world.mul(vec4(ray.start, 1)).xyz.toVar()
        const dir = world.mul(vec4(ray.direction, 0)).xyz.toVar()

        // Each volume's crossing, cut to near plane … surface; a miss enters at 1e9.
        const enter = Array.from({ length: MAX_VOLUMES }, () => float(1e9).toVar())
        const leave = Array.from({ length: MAX_VOLUMES }, () => float(1e9).toVar())
        const crossed = int(0).toVar()
        for (let i = 0; i < MAX_VOLUMES; i++) {
          If(int(i).lessThan(V.count), () => {
            const h = hitVolume(V, int(i), origin, dir)
            const a = max(h.x, 0)
            const b = min(h.y, ray.end)
            If(b.greaterThan(a), () => {
              enter[i].assign(a)
              leave[i].assign(b)
              crossed.assign(crossed.bitOr(int(1 << i)))
            })
          })
        }
        for (const [x, y] of SORT8) {
          const swap = enter[y].lessThan(enter[x]).toVar()
          const ax = select(swap, enter[y], enter[x]).toVar()
          const bx = select(swap, leave[y], leave[x]).toVar()
          const ay = select(swap, enter[x], enter[y]).toVar()
          const by = select(swap, leave[x], leave[y]).toVar()
          enter[x].assign(ax)
          leave[x].assign(bx)
          enter[y].assign(ay)
          leave[y].assign(by)
        }
        // Overlapping crossings merged into segments, front to back.
        const from = Array.from({ length: MAX_VOLUMES }, () => float(0).toVar())
        const to = Array.from({ length: MAX_VOLUMES }, () => float(0).toVar())
        const segments = int(0).toVar()
        const total = float(0).toVar()
        const emit = (a: TSLNode, b: TSLNode) => {
          for (let j = 0; j < MAX_VOLUMES; j++)
            If(segments.equal(j), () => {
              from[j].assign(a)
              to[j].assign(b)
            })
          total.addAssign(b.sub(a))
          segments.addAssign(1)
        }
        const a0 = enter[0].toVar()
        const b0 = leave[0].toVar()
        for (let k = 1; k < MAX_VOLUMES; k++) {
          If(enter[k].lessThan(1e8), () => {
            If(enter[k].lessThanEqual(b0), () => {
              b0.assign(max(b0, leave[k]))
            }).Else(() => {
              emit(a0, b0)
              a0.assign(enter[k])
              b0.assign(leave[k])
            })
          })
        }
        If(a0.lessThan(1e8), () => emit(a0, b0))

        const L = vec3(0).toVar()
        const T = vec3(1).toVar()
        const weighted = float(0).toVar()
        const taken = float(0).toVar()
        const spare = float(STEPS).sub(float(segments).mul(MIN_STEPS))
        const jitter = interleavedGradientNoise(screenCoordinate.xy)
        // Evaluated here, once a pixel: used first inside the loop it would be sampled at every step.
        const sky = vec3(lights.sky()).toVar()
        Loop(segments, ({ i: j }) => {
          let a = from[MAX_VOLUMES - 1]
          let b = to[MAX_VOLUMES - 1]
          for (let k = MAX_VOLUMES - 2; k >= 0; k--) {
            a = select(j.equal(k), from[k], a)
            b = select(j.equal(k), to[k], b)
          }
          const start = float(a).toVar()
          const span = float(b).sub(start).toVar()
          const n = int(MIN_STEPS).add(int(floor(spare.mul(span).div(max(total, 1e-6))))).toVar()
          const dt = span.div(float(n)).toVar()
          Loop(n, ({ i }) => {
            const t = start.add(float(i).add(jitter).mul(dt)).toVar()
            const p = origin.add(dir.mul(t)).toVar()
            const viewZ = ray.start.z.add(ray.direction.z.mul(t))
            const lit = lights.at(p, viewZ).map((l) => ({ radiance: vec3(l.radiance).toVar(), toward: vec3(l.toward).toVar(), distance: float(l.distance).toVar() }))
            const S = vec3(0).toVar()
            const st = vec3(0).toVar()
            Loop(V.count, ({ i: v }) => {
              If(crossed.bitAnd(int(1).shiftLeft(v)).notEqual(0), () => {
                const rho = volumeDensity(V, v, p).toVar()
                If(rho.greaterThan(0), () => {
                  const scatter = V.sct.element(v)
                  const extinction = V.ext.element(v).xyz
                  st.addAssign(extinction.mul(rho))
                  // The sky light comes evenly from all round: the phase function sums to one over it.
                  const sum = vec3(sky).toVar()
                  for (const l of lit) {
                    // Self-shadowing: this volume alone, along its chord toward the light.
                    const own = exp(extinction.mul(chordThrough(V, v, p, l.toward, l.distance)).negate())
                    sum.addAssign(l.radiance.mul(own).mul(hg(scatter.w, dot(l.toward, dir))))
                  }
                  S.addAssign(scatter.xyz.mul(rho).mul(sum))
                })
              })
            })
            const through = exp(st.mul(dt).negate()).toVar()
            const before = luma(T)
            L.addAssign(T.mul(S.sub(S.mul(through)).div(max(st, vec3(1e-6)))))
            T.mulAssign(through)
            const lost = before.sub(luma(T))
            weighted.addAssign(t.mul(lost))
            taken.addAssign(lost)
          })
        })
        const at = select(taken.greaterThan(1e-6), weighted.div(max(taken, 1e-6)), float(0))
        return Pair(vec4(min(L, vec3(MAX_LIGHT)), ray.depth), vec4(T, at))
      }),
    )
    return material
  }

  /** One blur pass over source along (dx, dy): 7 taps, Gaussian, times how alike their view depths are. */
  private blur(source: RenderTarget, dx: number, dy: number): TSLNode {
    const res = this.resolution
    return both(
      Fn(() => {
        const c = ivec2(floor(screenUV.mul(res)))
        const centre = textureLoad(source.textures[0], c)
        const L = vec3(0).toVar()
        const T = vec3(0).toVar()
        const sum = float(0).toVar()
        for (let k = -BLUR_RADIUS; k <= BLUR_RADIUS; k++) {
          const at = ivec2(clamp(vec2(c).add(vec2(dx * k, dy * k)), vec2(0), res.sub(1)))
          const l = textureLoad(source.textures[0], at)
          const w = float(Math.exp(-(k * k) / 4.5)).div(abs(l.a.sub(centre.a)).div(centre.a.mul(DEPTH_LIKENESS)).add(1))
          L.addAssign(l.rgb.mul(w))
          T.addAssign(textureLoad(source.textures[1], at).rgb.mul(w))
          sum.addAssign(w)
        }
        return Pair(vec4(L.div(sum), centre.a), vec4(T.div(sum), textureLoad(source.textures[1], c).a))
      }),
    )
  }

  setup(builder: NodeBuilder) {
    // Built before this node, so their passes draw first: the march reads the depth and the shadow maps of this frame.
    this.depth.build(builder)
    for (const n of this.after) n.build(builder)
    return this.light
  }

  updateBefore(frame: NodeFrame): undefined {
    if (V.on.value === 0) return undefined
    const renderer = frame.renderer!
    const { width, height } = renderer.getDrawingBufferSize(drawn)
    // Half the CSS pixels: a quarter of the picture on a 2× screen, half of an export's.
    const scale = 0.5 / renderer.getPixelRatio()
    const w = Math.max(1, Math.round(width * scale))
    const h = Math.max(1, Math.round(height * scale))
    this.resolution.value.set(w, h)
    for (const t of [this.marched, this.across, this.blurred]) t.setSize(w, h)
    if (!this.march || this.signature !== lighting.signature) {
      this.march?.dispose()
      this.march = this.marchMaterial()
      this.marchQuad.material = this.march
      this.signature = lighting.signature
      this.ready.value = 0
      if (this.warm) this.compile(renderer, this.march)
    }
    // Lights the scene no longer has are not drawn while the new march compiles.
    if (this.compiling) return undefined
    bindVolumeTextures(this.lights!.maps, this.lights!.skies)
    rendererState = RendererUtils.resetRendererState(renderer, rendererState as never)
    renderer.setRenderTarget(this.marched)
    renderer.render(this.marchQuad, this.marchCamera)
    for (const [material, into] of [
      [this.blurX, this.across],
      [this.blurY, this.blurred],
    ] as const) {
      this.quad.material = material
      renderer.setRenderTarget(into)
      this.quad.render(renderer)
    }
    RendererUtils.restoreRendererState(renderer, rendererState)
    this.ready.value = 1
    return undefined
  }

  /** The march compiled for its target on the driver's threads; then drawn on the frame warm asks for. */
  private compile(renderer: NodeFrame['renderer'] & object, material: NodeMaterial) {
    this.compiling = true
    gpuTestHooks.compiling++
    const target = renderer.getRenderTarget()
    const mrt = renderer.getMRT()
    // compileAsync takes the target's formats and the MRT before its first await: the march's, not the pass the frame is in.
    renderer.setRenderTarget(this.marched)
    renderer.setMRT(null)
    let done: Promise<void>
    try {
      done = renderer.compileAsync(this.marchQuad, this.marchCamera)
    } finally {
      renderer.setRenderTarget(target)
      renderer.setMRT(mrt)
    }
    void done
      .catch((error: unknown) => console.warn('volumes: the march did not compile ahead', error))
      .finally(() => {
        gpuTestHooks.compiling--
        if (this.march !== material) return
        this.compiling = false
        this.warm?.()
      })
  }

  /** The textures the composite reads, for its upsampling. */
  get textures(): readonly Texture[] {
    return this.blurred.textures
  }

  dispose() {
    super.dispose()
    for (const t of [this.marched, this.across, this.blurred]) t.dispose()
    this.march?.dispose()
    this.march = null
    this.marchQuad.geometry.dispose()
    this.blurX.dispose()
    this.blurY.dispose()
  }
}
