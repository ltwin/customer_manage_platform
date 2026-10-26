// What the world-space layer may do on this device, and how finely
// (scene-world-space-gi「能力与容量」「精度分档」; S1 design「能力与设备上限」
// 「分档」). Pure functions of the device's facts and the scene's size: the
// tier depends on this machine alone and never changes the scene (I4).

/** The storage buffers the reflections' fragment binds: the BVH's four, vertex colours, the material and light table. */
export const FRAGMENT_STORAGE_BUFFERS = 6
/** The traversal's stack (three-mesh-bvh BVH_STACK_DEPTH): a deeper tree overruns it. */
export const STACK_DEPTH = 60
/** Bytes a triangle of the original layer takes, its nodes and vertices included (S0: 894 MiB for 20.11 M triangles, 36 B once normals are encoded). */
export const BYTES_PER_TRIANGLE = 40
/** The room to spare a layer is laid out with, so an edit seldom lays it out again. */
export const SPARE = 1.25
/** An estimate past the budget by this much does not build the original at all. */
export const SKIP_ORIGINAL = 1.5
/** The original layer's budget (Epic「能力与容量」): 1 GiB with its room to spare. */
export const BUDGET = 2 ** 30

/** Why there is no layer: the WebGL2 backend (compatibility mode), or a WebGPU device short of what it needs. */
export type Unsupported = 'compatibility' | 'device'

export type Capability = { supported: true; maxBinding: number } | { supported: false; reason: Unsupported }

/** The limits a device was given, as WebGPU names them; the fragment stage's own where the browser has it. */
export type StorageLimits = {
  maxStorageBufferBindingSize: number
  maxBufferSize: number
  maxStorageBuffersPerShaderStage: number
  maxStorageBuffersInFragmentStage?: number
}

export type DeviceFacts = {
  backend: 'webgpu' | 'webgl2'
  /** A WebGPU device without core features and limits. */
  compatibility: boolean
  limits: StorageLimits | null
  /** The e2e's gpuTestHooks.traceUnsupported: no device to test this on. */
  unsupported?: boolean
}

/** Whether the layer is built on this device, and the most one of its buffers may hold. */
export function capabilityOf(d: DeviceFacts): Capability {
  if (d.backend !== 'webgpu') return { supported: false, reason: 'compatibility' }
  const l = d.limits
  if (d.compatibility || d.unsupported || !l) return { supported: false, reason: 'device' }
  if ((l.maxStorageBuffersInFragmentStage ?? l.maxStorageBuffersPerShaderStage) < FRAGMENT_STORAGE_BUFFERS) return { supported: false, reason: 'device' }
  return { supported: true, maxBinding: Math.min(l.maxStorageBufferBindingSize, l.maxBufferSize) }
}

/** Whether the original layer is worth building: not when its estimate is plainly past the budget. */
export function buildsOriginal(triangles: number, budget: number): boolean {
  return triangles * BYTES_PER_TRIANGLE * SPARE <= SKIP_ORIGINAL * budget
}

/** The error a proxy may make where it is drawn (Epic ε_probe, 3 mm), in metres. */
export const EPSILON_PROBE = 0.003
/** The proxy level an instance scaled by s needs: each level √2 finer, so that ε / √2^level ≤ ε / s (design「构建、缓存与提交」). */
export const levelFor = (scale: number) => Math.ceil(2 * Math.log2(Math.max(scale, 1e-6)))
/** A level's bound in the geometry's own units. */
export const epsilonAt = (level: number) => EPSILON_PROBE / Math.SQRT2 ** level

/** A layer as built: its bytes before the room to spare, its largest buffer's, and the traversal's depth (top level and deepest tree). */
export type LayerSize = { bytes: number; largest: number; depth: number }

export type Tier = 'full' | 'proxy' | 'tooLarge'

const binds = (s: LayerSize, maxBinding: number) => s.largest * SPARE <= maxBinding && s.depth <= STACK_DEPTH

/**
 * Tier 1 when the original and the proxies, both served there (S2 design
 * 「底座的两份精度」: the probes trace the proxies), fit the budget together
 * and the device each; tier 2 when the proxies fit the device; else too
 * large. original is null when it was not built (buildsOriginal); proxy null
 * when only the original's size is known, which is judged alone.
 */
export function tierOf(original: LayerSize | null, proxy: LayerSize | null, budget: number, maxBinding: number): Tier {
  const proxies = !proxy || binds(proxy, maxBinding)
  if (original && proxies && (original.bytes + (proxy?.bytes ?? 0)) * SPARE <= budget && binds(original, maxBinding)) return 'full'
  if (proxy && binds(proxy, maxBinding)) return 'proxy'
  return 'tooLarge'
}
