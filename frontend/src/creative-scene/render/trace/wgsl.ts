import { wgslFn } from 'three/tsl'
import type { CodeNodeInclude } from 'three/webgpu'
import type { TSLNode } from '../../materials/patterns.ts'
import type { TraceLayer } from './layer.ts'

// The scene's one WGSL (S1 design「模块职责与协作」; the static check lets
// wgslFn through here alone): the calls into three-mesh-bvh's traversal.
// From TSL the library's bvh_RaycastFirstHit cannot be called on three r186:
// its hit must be a ptr<function>, and its scratch variable must be at
// module scope, where three puts a kernel's variables only when it declares
// them all there. A WGSL function of our own holds the hit and calls it, as
// upstream's example does (S0 integration.md). No shading in here: hit.ts
// reads the result in TSL.

/** The library's IntersectionResult held in a variable, read by field: dist along the ray, normal in world space facing the ray, side 1 for a front face and −1 for a back one, indices the triangle's vertices then the triangle, objectIndex its transform slot. */
export type Hit = TSLNode & { get(name: 'didHit' | 'dist' | 'normal' | 'objectIndex' | 'side' | 'barycoord' | 'indices'): TSLNode }
/** A call of one of the functions: made a variable before it is read (each read of the call would trace again). */
export type HitCall = { toVar(name?: string): Hit }
type Call = (...args: unknown[]) => HitCall

type Fns = { first: Call; within: Call }
/** A layer's functions, made once: a shader that called a second copy would declare one name twice. */
const made = new WeakMap<TraceLayer, Fns>()

function fnsOf(layer: TraceLayer): Fns {
  const known = made.get(layer)
  if (known) return known
  const raycast = (layer.fns as unknown as { raycastFirstHit: CodeNodeInclude }).raycastFirstHit
  const fns: Fns = {
    first: wgslFn(
      `fn ${layer.name}_first( origin: vec3f, direction: vec3f ) -> IntersectionResult {
        var ray: Ray;
        ray.origin = origin;
        ray.direction = direction;
        ray.maxDist = 0.0;
        var hit: IntersectionResult;
        bvh_RaycastFirstHit( ray, &hit );
        return hit;
      }`,
      [raycast],
    ) as unknown as Call,
    within: wgslFn(
      `fn ${layer.name}_within( origin: vec3f, direction: vec3f, maxDist: f32 ) -> IntersectionResult {
        var ray: Ray;
        ray.origin = origin;
        ray.direction = direction;
        ray.maxDist = maxDist;
        var hit: IntersectionResult;
        bvh_RaycastFirstHit( ray, &hit );
        return hit;
      }`,
      [raycast],
    ) as unknown as Call,
  }
  made.set(layer, fns)
  return fns
}

/** The first hit along a ray through the layer (direction normalised: dist is in world units). */
export function traceFirst(layer: TraceLayer, origin: TSLNode, direction: TSLNode): HitCall {
  return fnsOf(layer).first({ origin, direction })
}

/** The first hit closer than maxDist (0: any): a shadow ray towards a light. */
export function traceWithin(layer: TraceLayer, origin: TSLNode, direction: TSLNode, maxDist: TSLNode): HitCall {
  return fnsOf(layer).within({ origin, direction, maxDist })
}
