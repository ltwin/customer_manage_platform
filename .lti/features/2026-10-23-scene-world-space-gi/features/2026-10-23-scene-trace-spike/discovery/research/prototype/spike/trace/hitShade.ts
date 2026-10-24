// S0 group 4: a ray's hit, shaded as group 3 shades a hit (hit-shading.md), for the reflections' retrace. Callable
// from a fragment shader (option A) and a compute kernel (option B) alike:
//   - the width: group 3's ray differentials, synthesised from a ray cone (the cone's width where it lands, stretched
//     by 1/cos along the ray's direction across the surface);
//   - the sun's shadow: a shadow ray toward it (hit-shading.md: the product's cascades cover only the view).
import { Color, type Mesh } from 'three'
import * as tsl from 'three/tsl'
import { StorageBufferAttribute } from 'three/webgpu'
import type { SlotMaterial } from '../../src/creative-scene/materials/resolve.ts'
import type { TSLNode } from './patterns.ts'
import { lightUniforms, lit, materialAt, ROW, tableRow, type Lights } from './shading.ts'
import type { TraceFirst } from './trace.ts'

/** Experiment switches (?retrace=zero,noshadow,albedo,white): the width taken as 0; no shadow ray; the albedo unlit; no shading at all (the hit's cost alone). reflections.md「耗时拆分」. */
const knobs = new Set((new URLSearchParams(location.search).get('retrace') ?? '').split(','))

type Op = (...args: TSLNode[]) => TSLNode
const T = tsl as unknown as Record<string, Op> & { If: typeof tsl.If }
const { abs, cross, dot, float, length, max, normalize, select, vec3, vec4 } = T

/** The material table for meshes: their slot's, or a plain principled row from the mesh's own material. */
export function materialTable(meshes: Mesh[]) {
  const rows = meshes.flatMap((m) => {
    const slot = m.userData.slot as SlotMaterial | undefined
    if (slot) return tableRow(slot)
    const mat = m.material as { color?: Color; roughness?: number; metalness?: number }
    return tableRow({ kind: 'principled', params: { color: `#${(mat.color ?? new Color(0.8, 0.8, 0.8)).getHexString()}`, roughness: mat.roughness ?? 0.7, metalness: mat.metalness ?? 0 } } as SlotMaterial)
  })
  return tsl.storage(new StorageBufferAttribute(new Float32Array(rows), 4), 'vec4', meshes.length * ROW).toReadOnly()
}

/**
 * Traces origin → dir and shades the first hit: { color, dist } with dist -1 where nothing is hit.
 * width0 is the cone's width at the origin (metres) and spread its growth per metre along the ray.
 */
export function shadeAlong(traceFirst: TraceFirst, store: { transforms: TSLNode; attributes: TSLNode }, table: TSLNode, lights: ReturnType<typeof lightUniforms>, origin: TSLNode, dir: TSLNode, width0: TSLNode, spread: TSLNode) {
  const color = vec3(0).toVar()
  const dist = float(-1).toVar()
  const hit = traceFirst(origin, dir).toVar('retrace') as unknown as { get(k: string): TSLNode }
  T.If(hit.get('didHit'), () => {
    const slot = store.transforms.element(hit.get('objectIndex'))
    const inv = T.mat4(slot.get('inverseMatrixWorld')).toVar()
    const M = T.mat4(slot.get('matrixWorld')).toVar()
    const mi = slot.get('_alignment0').toVar()
    const t = float(hit.get('dist')).toVar()
    const P = origin.add(dir.mul(t)).toVar()
    const scale = vec3(length(M.mul(vec4(1, 0, 0, 0)).xyz), length(M.mul(vec4(0, 1, 0, 0)).xyz), length(M.mul(vec4(0, 0, 1, 0)).xyz)).toVar()
    const toPattern = (v: TSLNode, wc: number) => inv.mul(vec4(v, wc)).xyz.mul(scale)
    const idx = hit.get('indices')
    const b = hit.get('barycoord')
    const vn = (k: TSLNode) => vec3(store.attributes.element(k).get('normal').xyz)
    const nObj = normalize(vn(idx.x).mul(b.x).add(vn(idx.y).mul(b.y)).add(vn(idx.z).mul(b.z))).toVar()
    const nRaw = normalize(vec4(nObj, 0).mul(inv).xyz).toVar()
    const n = select(dot(nRaw, dir).greaterThan(0), nRaw.negate(), nRaw).toVar()
    // The cone where it lands: across the surface along the ray's direction it is stretched by 1/cos.
    const cosI = max(abs(dot(n, dir)), 0.2)
    const w = width0.add(spread.mul(t)).toVar()
    const along = normalize(dir.sub(n.mul(dot(dir, n))).add(vec3(1e-6, 0, 0))).toVar()
    const across = normalize(cross(n, along))
    const dpx = toPattern(along.mul(w.div(cosI)), 0).toVar()
    const dpy = toPattern(across.mul(w), 0).toVar()
    const fp = max(length(abs(dpx).add(abs(dpy))), 1e-6)
    const pos = toPattern(P, 1).toVar()
    if (knobs.has('white')) {
      color.assign(vec3(1))
      dist.assign(t)
      return
    }
    const m = materialAt(table, mi, pos, nObj.div(scale), fp, dpx, dpy, knobs.has('zero') ? 'zero' : 'diff')
    if (knobs.has('albedo')) color.assign(m.albedo)
    else if (knobs.has('noshadow')) color.assign(lit(P, n, dir.negate(), m, lights, () => float(1)))
    else {
      // The sun's shadow: a ray toward it, off the surface by a millimetre.
      const shadowHit = traceFirst(P.add(n.mul(1e-3)), lights.sunDir).toVar('sunShadow') as unknown as { get(k: string): TSLNode }
      color.assign(lit(P, n, dir.negate(), m, lights, () => select(shadowHit.get('didHit'), float(0), float(1))))
    }
    dist.assign(t)
  })
  return { color, dist }
}

export type ReflectionTrace = {
  traceFirst: TraceFirst
  store: { transforms: TSLNode; attributes: TSLNode }
  table: TSLNode
  lights: ReturnType<typeof lightUniforms>
  /** Retraced only below this roughness (U3's r_max). */
  rMax: TSLNode
}

export function reflectionTrace(traceFirst: TraceFirst, store: ReflectionTrace['store'], meshes: Mesh[], L: Lights, rMax: TSLNode): ReflectionTrace {
  return { traceFirst, store, table: materialTable(meshes), lights: lightUniforms(L), rMax }
}
