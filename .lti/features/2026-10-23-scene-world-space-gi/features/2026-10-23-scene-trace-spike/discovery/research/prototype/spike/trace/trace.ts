// The spike's one call into three-mesh-bvh's traversal (integration.md「在 three r186 上从 TSL 调用库的求交函数」).
import { wgslFn } from 'three/tsl'
import type { BVHComputeData } from 'three-mesh-bvh/webgpu'

/** The traced hit, by value: the library's IntersectionResult fields read with .get(). */
export type Hit = { get(name: 'didHit' | 'dist' | 'normal' | 'objectIndex' | 'side' | 'barycoord' | 'indices'): never }
export type TraceFirst = (origin: unknown, direction: unknown) => { toVar(name?: string): Hit }

/**
 * The first hit along a ray through data's scene. Called from TSL, the library's raycastFirstHit cannot work on three
 * r186: its hit pointer must be ptr<function>, and its scratch variable must be at module scope, where three puts a
 * compute kernel's variables only when the kernel declares them all there. The spike calls it from a WGSL shim, as
 * upstream's example does: the shim's hit is its own, the scratch variable the kernel's. Spike code only: the scene's
 * shading rule leaves WGSL to the library (patterns 场景着色写法), and S0 reports the choice (option A).
 */
export function traceShim(data: BVHComputeData): TraceFirst {
  const fns = data.fns as unknown as { raycastFirstHit: unknown }
  return wgslFn(
    `fn spike_traceFirst( origin: vec3f, direction: vec3f ) -> IntersectionResult {
      var ray: Ray;
      ray.origin = origin;
      ray.direction = direction;
      ray.maxDist = 0.0;
      var hit: IntersectionResult;
      bvh_RaycastFirstHit( ray, &hit );
      return hit;
    }`,
    [fns.raycastFirstHit],
  ) as unknown as TraceFirst
}

export type TraceWithin = (origin: unknown, direction: unknown, maxDist: unknown) => { toVar(name?: string): Hit }

/** As traceShim, the first hit closer than maxDist (a shadow ray toward a light in the scene). */
export function traceWithinShim(data: BVHComputeData): TraceWithin {
  const fns = data.fns as unknown as { raycastFirstHit: unknown }
  return wgslFn(
    `fn spike_traceWithin( origin: vec3f, direction: vec3f, maxDist: f32 ) -> IntersectionResult {
      var ray: Ray;
      ray.origin = origin;
      ray.direction = direction;
      ray.maxDist = maxDist;
      var hit: IntersectionResult;
      bvh_RaycastFirstHit( ray, &hit );
      return hit;
    }`,
    [fns.raycastFirstHit],
  ) as unknown as TraceWithin
}
