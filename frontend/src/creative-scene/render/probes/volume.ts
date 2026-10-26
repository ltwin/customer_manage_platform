import { Box3, FloatType, HalfFloatType, LinearFilter, Matrix3, Matrix4, NearestFilter, Quaternion, Vector3, Vector4, type Camera, type Scene } from 'three'
import * as tsl from 'three/tsl'
import { StorageTexture, type ComputeNode, type WebGPURenderer } from 'three/webgpu'
import type { TSLNode } from '../../materials/patterns.ts'
import { preExposure, type PhysicalExposure } from '../../world/exposure.ts'
import type { TraceHandle } from '../trace/base.ts'
import { hitShading, type HitShading } from '../trace/hit.ts'
import type { TraceLayer } from '../trace/layer.ts'
import { activeLevels, DEPTH_TEXELS, IRRADIANCE_TEXELS, isDue, LEVELS_MAX, LEVELS_MIN, levelsFor, originCell, periodsFor, PICTURE, PROBES, RAYS, SETTLE_UPDATES, VIEWPORT, type Hysteresis } from './cascade.ts'
import { rayKernel, rescaleKernel, updateKernel, type ProbeData } from './kernels.ts'
import { DEPTH_TILE, IRRADIANCE_TILE, TILES, type ProbeNodes } from './lookup.ts'

// A camera-centred cascade of irradiance probes (S2 design「探针体」「模块
// 职责与协作」): its buffers and atlases, allocated for the levels the scene
// takes, and step, the one entry a frame goes through, the viewport's and a
// picture's warm-up alike. The viewport's volume is its SceneRenderer's,
// kept across pipelines while the proxies it traces are the same layer; a
// picture's is made for it and freed with it (Epic I3).

type Op = (...args: TSLNode[]) => TSLNode
const T = tsl as unknown as Record<string, Op>
const { instancedArray, texture, uniform } = T

/** What the probes are told by the base (render/trace/base.ts TraceBase): what changed, where the scene is, how large its top level is. */
export type ProbeSource = {
  readonly bounds: Box3
  readonly geometryVersion: number
  readonly lightingVersion: number
  takeEdits(): Box3
  /** The material rows: a root of the top level each (the viewport's rotation, cascade.ts periodsFor). */
  readonly slots: readonly unknown[]
}

/** The viewport's volume rotates its levels and follows edits; a picture's updates every level every frame. */
export type Mode = 'viewport' | 'picture'

/** Frames the geometry stands still before the probes near it may relocate again (S2 design「编辑的分类」). */
const SETTLED = 4

/** Each frame's turn of the probes' rays: a uniform rotation from the frame number alone (Shoemake 1992; P3). */
export function frameRotation(frame: number, out = new Matrix3()): Matrix3 {
  // mulberry32: three numbers in [0, 1) from the frame.
  let a = (frame * 0x9e3779b9) >>> 0
  const next = () => {
    a = (a + 0x6d2b79f5) >>> 0
    let t = a
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
  const [u1, u2, u3] = [next(), next(), next()]
  const q = new Quaternion(Math.sqrt(1 - u1) * Math.sin(2 * Math.PI * u2), Math.sqrt(1 - u1) * Math.cos(2 * Math.PI * u2), Math.sqrt(u1) * Math.sin(2 * Math.PI * u3), Math.sqrt(u1) * Math.cos(2 * Math.PI * u3))
  return out.setFromMatrix4(new Matrix4().makeRotationFromQuaternion(q))
}

let volumes = 0

const corner = new Vector3()
const eye = new Vector3()
const look = new Vector3()

export class ProbeVolume {
  /** The layer the probes trace: the volume's identity (a new one is a cold start). */
  readonly layer: TraceLayer
  /** Which volume this is, over the page's life: a pipeline drawing another is built again (sceneRenderer.ts keyOf). */
  readonly serial = ++volumes
  readonly mode: Mode
  readonly allocated: number
  readonly nodes: ProbeNodes
  /** Levels drawn (cascade.ts activeLevels). */
  levels = LEVELS_MIN
  private readonly hysteresis: Hysteresis
  private readonly data: ProbeData
  private readonly buffers: TSLNode[]
  private readonly handle: TraceHandle
  private readonly scene: Scene
  private readonly source: ProbeSource
  private readonly renderer: WebGPURenderer
  private readonly release: (attribute: object) => void
  private kernels: { rays: ComputeNode; update: ComputeNode; rescale: ComputeNode; shading: HitShading; environment: boolean } | null = null
  /** Updates each level has had since it was cleared: the viewport's warm-up reads it (warm). */
  private readonly updates = new Array<number>(LEVELS_MAX).fill(0)
  private seen: { geometry: number; lighting: number } | null = null
  private edits = 0
  private relocations = 0
  private still = 0
  private readonly pending = new Box3()
  private sEnv: number | null = null

  constructor(renderer: WebGPURenderer, handle: TraceHandle, scene: Scene, source: ProbeSource, mode: Mode, release: (attribute: object) => void) {
    this.renderer = renderer
    this.handle = handle
    this.layer = handle.probes
    this.scene = scene
    this.source = source
    this.mode = mode
    this.release = release
    this.hysteresis = mode === 'picture' ? PICTURE : VIEWPORT
    // As many levels as the scene's diagonal needs: a camera inside it is never farther from a corner (S2 design「级数」).
    this.allocated = levelsFor(source.bounds.isEmpty() ? 0 : source.bounds.getSize(corner).length())
    const n = this.allocated
    const atlas = (width: number, height: number, type: typeof HalfFloatType | typeof FloatType, filter: typeof LinearFilter | typeof NearestFilter) => {
      const t = new StorageTexture(width, height)
      t.type = type
      t.minFilter = t.magFilter = filter
      t.generateMipmaps = false
      return t
    }
    const atlases = {
      irradiance: atlas(TILES * IRRADIANCE_TILE, TILES * IRRADIANCE_TILE * n, HalfFloatType, LinearFilter),
      depth: atlas(TILES * DEPTH_TILE, TILES * DEPTH_TILE * n, HalfFloatType, LinearFilter),
      state: atlas(TILES * 2, TILES * n, FloatType, NearestFilter),
    }
    const store = (count: number, type: string) => instancedArray(count, type)
    const probes = n * PROBES
    this.buffers = [store(probes * RAYS, 'vec4'), store(probes * IRRADIANCE_TEXELS ** 2, 'vec4'), store(probes * DEPTH_TEXELS ** 2, 'vec2'), store(probes, 'vec4'), store(probes, 'vec4'), store(probes, 'vec4')]
    const [rays, irradiance, depth, offsets, cells, ages] = this.buffers
    this.nodes = {
      allocated: n,
      irradiance: texture(atlases.irradiance),
      depth: texture(atlases.depth),
      state: texture(atlases.state),
      origins: Array.from({ length: LEVELS_MAX }, () => uniform(new Vector4())),
      levels: uniform(LEVELS_MIN, 'uint'),
      fadeIn: uniform(this.hysteresis.A0),
      toExposed: uniform(1),
    }
    this.data = {
      nodes: this.nodes,
      rays,
      irradiance,
      depth,
      offsets,
      cells,
      ages,
      atlases,
      u: {
        due: Array.from({ length: LEVELS_MAX }, () => uniform(0, 'uint')),
        rotation: uniform(new Matrix3()),
        frame: uniform(0, 'uint'),
        A0: uniform(this.hysteresis.A0),
        alphaMax: uniform(this.hysteresis.alphaMax),
        editCount: uniform(0),
        relocationCount: uniform(0),
        editedMin: uniform(new Vector3(Infinity, Infinity, Infinity)),
        editedMax: uniform(new Vector3(-Infinity, -Infinity, -Infinity)),
        toStored: uniform(1),
        rescale: uniform(1),
        fill: uniform(1),
      },
    }
  }

  /** Whether the scene has grown past the levels allocated: the owner makes a new volume (a cold start). */
  get outgrown(): boolean {
    return levelsFor(this.source.bounds.isEmpty() ? 0 : this.source.bounds.getSize(corner).length()) > this.allocated
  }

  /** Whether every level drawn has had A0 + 14 updates since it was cleared: the viewport takes the probes up then (S2 design「就绪的那一刻」). */
  get warm(): boolean {
    return this.updates.slice(0, this.levels).every((u) => u >= this.hysteresis.A0 + SETTLE_UPDATES)
  }

  /** For the e2e (gpu.ts probeStats): the levels allocated and drawn, each one's updates since it was cleared, warm. */
  get stats(): { serial: number; allocated: number; levels: number; updates: number[]; warm: boolean } {
    return { serial: this.serial, allocated: this.allocated, levels: this.levels, updates: this.updates.slice(0, this.allocated), warm: this.warm }
  }

  /** The kernels, built once, and again if the scene gained an environment since (hit.ts takes it as it builds). */
  private built() {
    const environment = this.scene.environment !== null
    if (this.kernels && (this.kernels.environment || !environment)) return this.kernels
    this.disposeKernels()
    const shading = hitShading(this.handle.probes, this.handle.tables, this.scene)
    const count = this.allocated * PROBES * RAYS
    this.kernels = { rays: rayKernel(this.data, shading, count), update: updateKernel(this.data, count), rescale: rescaleKernel(this.data, count), shading, environment }
    return this.kernels
  }

  /**
   * One frame of the probes (S2 design「ProbeVolume.step」), after the
   * frame's applyExposure, base.sync and the hit's update: the levels drawn,
   * the boxes of those due placed and locked, what changed counted, the sky's
   * scale followed, then the rays of every level due and their updates.
   * warming: every level drawn updated every frame (a cold start's warm-up).
   */
  step(frame: number, camera: Camera, warming = false) {
    const k = this.built()
    k.shading.update()
    camera.getWorldPosition(eye)
    camera.getWorldDirection(look)
    const b = this.source.bounds
    let farthest = 0
    if (!b.isEmpty()) for (let c = 0; c < 8; c++) farthest = Math.max(farthest, eye.distanceTo(corner.set(c & 1 ? b.max.x : b.min.x, c & 2 ? b.max.y : b.min.y, c & 4 ? b.max.z : b.min.z)))
    this.levels = activeLevels(this.allocated, farthest, this.levels)
    this.nodes.levels.value = this.levels
    const periods = this.mode === 'picture' || warming ? [1, 1, 1, 1, 1] : periodsFor(this.source.slots.length)
    const due = Array.from({ length: this.levels }, (_, l) => l).filter((l) => isDue(l, frame, periods))
    // The forward direction's horizontal part, not normalised (cascade.ts originCell).
    const forward = [look.x, 0, look.z] as const
    for (const l of due) (this.nodes.origins[l].value as Vector4).set(...originCell(l, [eye.x, eye.y, eye.z], forward), 0)
    this.follow()
    const u = this.data.u
    const physical = this.scene.userData.physical as PhysicalExposure | undefined
    const sEnv = physical?.skyLightScale ?? 1
    if (this.sEnv !== null && sEnv !== this.sEnv) {
      u.rescale.value = sEnv / this.sEnv
      this.renderer.compute(k.rescale, this.allocated * PROBES * RAYS)
    }
    this.sEnv = sEnv
    u.toStored.value = sEnv / preExposure.value
    this.nodes.toExposed.value = preExposure.value / sEnv
    frameRotation(frame, u.rotation.value as Matrix3)
    u.frame.value = frame >>> 0
    due.forEach((l, i) => (u.due[i].value = l))
    if (due.length === 0) return
    const threads = due.length * PROBES * RAYS
    this.renderer.compute(k.rays, threads)
    this.renderer.compute(k.update, threads)
    for (const l of due) this.updates[l]++
  }

  /** What changed since the last step (S2 design「编辑的分类」): an edit count the probes reset their edit age by; the relocation count, once geometry has stood still, for where it changed. */
  private follow() {
    const s = this.source
    const now = { geometry: s.geometryVersion, lighting: s.lightingVersion }
    const was = this.seen
    this.seen = now
    if (!was) {
      // A cold start: every probe relocates and gathers anyway.
      s.takeEdits()
      return
    }
    const u = this.data.u
    if (now.geometry !== was.geometry || now.lighting !== was.lighting) u.editCount.value = ++this.edits
    if (now.geometry !== was.geometry) {
      this.pending.union(s.takeEdits())
      this.still = 0
      return
    }
    if (this.pending.isEmpty() || ++this.still < SETTLED) return
    ;(u.editedMin.value as Vector3).copy(this.pending.min)
    ;(u.editedMax.value as Vector3).copy(this.pending.max)
    u.relocationCount.value = ++this.relocations
    this.pending.makeEmpty()
  }

  /**
   * For the e2e and checks.ts: each probe's irradiance history (64 texels of
   * rgb, as stored), offset and on (offsets), world cell and ages, read back.
   */
  async inspect(): Promise<{ irradiance: Float32Array; offsets: Float32Array; cells: Float32Array; ages: Float32Array; origins: number[][] }> {
    const read = async (b: TSLNode) => new Float32Array(await this.renderer.getArrayBufferAsync((b as { value: never }).value))
    const [, irradiance, , offsets, cells, ages] = this.buffers
    return {
      irradiance: await read(irradiance),
      offsets: await read(offsets),
      cells: await read(cells),
      ages: await read(ages),
      origins: this.nodes.origins.map((o) => (o.value as Vector4).toArray().slice(0, 3)),
    }
  }

  private disposeKernels() {
    if (!this.kernels) return
    for (const c of [this.kernels.rays, this.kernels.update, this.kernels.rescale]) c.dispose()
    this.kernels = null
  }

  dispose() {
    this.disposeKernels()
    for (const b of this.buffers) this.release((b as { value: object }).value)
    for (const t of Object.values(this.data.atlases)) t.dispose()
  }
}
