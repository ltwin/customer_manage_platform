import type { Scene, Texture } from 'three'
import * as tsl from 'three/tsl'
import { materialEntry } from '../../materials/catalog.ts'
import { patternFns, type PatternInput, type PatternOut, type TSLNode } from '../../materials/patterns.ts'
import { preExposure } from '../../world/exposure.ts'
import { gpuTestHooks } from '../gpu.ts'
import type { TraceHandle } from './base.ts'
import { ROW as NORMAL_ROW, STEPS } from './normals.ts'
import { HEADER, LIGHT, LIGHT_ROW, MATERIAL_ROW, presets } from './tables.ts'
import { traceFirst, traceWithin, type Hit } from './wgsl.ts'

// A ray's first hit through the world-space layer, shaded as the raster
// shades that surface (S1 design「命中点着色」, Epic I2): the same preset
// functions at the hit, from the material table, lit by three's physical
// model's own formulas (PhysicalLightingModel, MeshPhysicalNodeMaterial's
// defaults: ior 1.5, specular 1) with BRDF_GGX, BRDF_Lambert, F_Schlick and
// DFGLUT as three exports them. No bump, no clearcoat, sheen or other
// principled extras, no fog: the reflections' pass has none of those
// either (Epic「命中点着色」).
//
//   geometry  the transform slot's matrices and material row; the vertex
//             normals decoded and interpolated, turned to world space and
//             towards the ray; the object-space position in metres as
//             instances.ts's patPos
//   width     a ray cone (S0 hitShade.ts): its width where it lands,
//             stretched by 1/cos along the ray across the surface, gives
//             two ray differentials; each quantity a preset hands its width
//             function is taken again one differential over, and the width
//             is |dx| + |dy| as fwidth's
//   lights    the light rows (tables.ts): a shadow ray towards each light
//             that casts, 1 mm off the surface; the ambient lights; the
//             environment's PMREM, turned as the scene's materials turn it

type Op = (...args: TSLNode[]) => TSLNode
const T = tsl as unknown as Record<string, Op>
const { BRDF_GGX, BRDF_Lambert, DFGLUT, F_Schlick, If, Loop, Switch, abs, clamp, cross, dot, float, floor, getDistanceAttenuation, isolate, length, mat3, mat4, max, min, mix, normalize, pmremTexture, pow, reflect, select, smoothstep, sRGBTransferEOTF, uint, vec3, vec4 } = T

/** A material at a hit, as the lighting takes it: emission already exposed. */
export type Surface = { albedo: TSLNode; rough: TSLNode; metal: TSLNode; emission: TSLNode }

/**
 * What shade gives: lit, all of it; and for the e2e's checks (checks.ts
 * hitCheck): unshadowed (no shadow rays), direct and directUnshadowed (the
 * lights alone and the emission, with and without), the material's outputs (albedo; and
 * roughness, metalness, emission's luminance), or id (the material row); and for a defect's
 * source (gpuTestHooks.traceShade): object (the transform slot hashed to a colour) or distance
 * (grey over 30 m).
 */
export type ShadeMode = 'lit' | 'unshadowed' | 'direct' | 'directUnshadowed' | 'albedo' | 'outputs' | 'id' | 'object' | 'distance'

export type HitShading = {
  /**
   * The first hit along origin → dir (normalised), shaded: color, and dist
   * along the ray (−1 where nothing is hit). coneWidth is the ray's width
   * at the origin and coneSpread its growth a metre (S1 design: the pixel's
   * angle × the view distance, and the pixel's angle + 2 roughness²).
   * neighbours, a primary ray's (the e2e's checks): the rays of the pixels
   * beside it from the same origin, whose meeting with the hit's plane
   * gives the differentials exactly, as fwidth's screen pixels would.
   */
  shade(origin: TSLNode, dir: TSLNode, coneWidth: TSLNode, coneSpread: TSLNode, mode?: ShadeMode, neighbours?: { dx: TSLNode; dy: TSLNode }): { color: TSLNode; dist: TSLNode }
  /** Before each frame: the environment as the scene lights with it now (a new sky is a new texture, not a new shader). */
  update(): void
}

/** The unit normal an encoded w stands for (normals.ts decodeNormal). */
function decodeNormal(w: TSLNode): TSLNode {
  const qv = floor(w.div(NORMAL_ROW))
  const qu = w.sub(qv.mul(NORMAL_ROW))
  const x = qu.div(STEPS).mul(2).sub(1).toVar()
  const y = qv.div(STEPS).mul(2).sub(1).toVar()
  const z = float(1).sub(abs(x)).sub(abs(y)).toVar()
  const t = max(z.negate(), 0)
  return normalize(vec3(x.add(select(x.greaterThanEqual(0), t.negate(), t)), y.add(select(y.greaterThanEqual(0), t.negate(), t)), z))
}

/** A vertex's colour, RGBA8 sRGB in a u32 (build.worker.ts), linear. */
function vertexColor(c: TSLNode): TSLNode {
  const channel = (k: number) => float(c.shiftRight(uint(8 * k)).bitAnd(uint(255))).div(255)
  return sRGBTransferEOTF(vec3(channel(0), channel(1), channel(2)))
}

/**
 * A preset at a hit with its width from the ray differentials: run one
 * differential over in x and in y first, recording what it hands width,
 * then at the hit with width the sum of the two differences (S0 hit-shading.md).
 */
function atHit(fn: (i: PatternInput) => PatternOut, base: Omit<PatternInput, 'width'>, dpx: TSLNode, dpy: TSLNode): PatternOut {
  const record = (list: TSLNode[]) => (x: TSLNode) => (list.push(x), float(0))
  const X: TSLNode[] = []
  const Y: TSLNode[] = []
  fn({ ...base, pos: base.pos.add(dpx), width: record(X) })
  fn({ ...base, pos: base.pos.add(dpy), width: record(Y) })
  let k = 0
  return fn({
    ...base,
    width: (x) => {
      const i = k++
      return abs(X[i].sub(x)).add(abs(Y[i].sub(x)))
    },
  })
}

export function hitShading(handle: TraceHandle, scene: Scene): HitShading {
  const { reflect: layer, tables } = handle
  const table = tables.node
  const { transforms, attributes } = layer.stores
  const colors = layer.colorsNode
  const header = (k: number) => table.element(uint(k))
  // The environment the scene lights with: made once there is one, its texture kept up to date (update).
  let shown: Texture | null = scene.environment
  const environment: TSLNode | null = shown ? pmremTexture(shown) : null

  /** The material row's vec4s, by material index. */
  const rowOf = (mi: TSLNode) => {
    const base = uint(HEADER).add(mi.mul(uint(MATERIAL_ROW))).toVar()
    return (k: number) => table.element(base.add(uint(k)))
  }

  /** The material at a hit: the presets behind one Switch, the principled and unknown materials in its Default. */
  function surfaceAt(row: (k: number) => TSLNode, posIn: TSLNode, normalIn: TSLNode, footprintIn: TSLNode, dpxIn: TSLNode, dpyIn: TSLNode): Surface {
    // Every input a variable before the Switch: an expression the cases share is otherwise made a variable where
    // TSL first builds it, inside the first case, and the others read it unassigned (S0).
    const pos = posIn.toVar()
    const normal = normalIn.toVar()
    const footprint = footprintIn.toVar()
    const dpx = dpxIn.toVar()
    const dpy = dpyIn.toVar()
    const albedo = vec3(0).toVar()
    const rough = float(0).toVar()
    const metal = float(0).toVar()
    const emission = vec3(0).toVar()
    const sw = Switch(row(0).x.toUint())
    presets.forEach((id, k) => {
      sw.Case(uint(2 + k), () => {
        const pm = Object.fromEntries(Object.entries(materialEntry(id)?.params ?? {}).map(([name, spec], j) => [name, spec.type === 'color' ? row(1 + j).xyz : row(1 + j).x]))
        const o = atHit(patternFns[id], { pos, normal, footprint, pm }, dpx, dpy)
        albedo.assign(o.albedo)
        rough.assign(o.rough)
        metal.assign(o.metal)
        emission.assign(o.emission)
      })
    })
    // tables.ts rowParams: colour, roughness, metalness, emissive, emissive_intensity.
    sw.Default(() => {
      albedo.assign(row(1).xyz)
      rough.assign(row(2).x)
      metal.assign(row(3).x)
      emission.assign(row(4).xyz.mul(row(5).x))
    })
    return { albedo, rough, metal, emission: emission.mul(preExposure) }
  }

  /**
   * Lit at P (world), normal n towards the viewer, v towards the viewer:
   * PhysicalLightingModel's direct and indirect terms. Shadow rays leave from
   * lifted, P off its face.
   */
  function lightAt(P: TSLNode, n: TSLNode, v: TSLNode, s: Surface, shadows: boolean, indirect: boolean, lifted: TSLNode): TSLNode {
    const specularColor = vec3(0.04)
    const specularF90 = float(1)
    const diffuseContribution = s.albedo.mul(float(1).sub(s.metal)).toVar()
    const specularColorBlended = mix(specularColor, s.albedo, s.metal).toVar()
    // getRoughness without its geometry term (screen derivatives of the normal: none at a hit).
    const roughness = min(max(s.rough, 0.0525), 1).toVar()
    const dotNV = clamp(dot(n, v), 0, 1)
    const dfg = DFGLUT({ roughness, dotNV } as never).toVar()
    const Ess = dfg.x.add(dfg.y).toVar()
    const msComp = specularColorBlended.mul(Ess.reciprocal().sub(1)).add(1).toVar()
    const multiscatter = (f0: TSLNode) => {
      const FssEss = f0.mul(dfg.x).add(specularF90.mul(dfg.y))
      const Ems = float(1).sub(Ess)
      const Favg = f0.add(float(1).sub(f0).mul(0.047619))
      const Fms = FssEss.mul(Favg).div(float(1).sub(Ems.mul(Favg)))
      return [FssEss.toVar(), Fms.mul(Ems).toVar()]
    }
    const light = vec3(0).toVar()
    // The direct lights.
    const first = header(0).y.toUint().toVar()
    Loop({ start: uint(0), end: header(0).x.toUint(), type: 'uint', condition: '<' }, ({ i }: { i: TSLNode }) => {
      const at = first.add(i.mul(uint(LIGHT_ROW))).toVar()
      const kind = table.element(at).toVar()
      const where = table.element(at.add(uint(1))).toVar()
      const shine = table.element(at.add(uint(2))).toVar()
      const l = vec3(0).toVar()
      const color = vec3(0).toVar()
      // How far a shadow ray goes: 0 is all the way (a directional light).
      const reach = float(0).toVar()
      If(kind.x.lessThan(LIGHT.point - 0.5), () => {
        l.assign(where.xyz)
        color.assign(shine.xyz)
      }).Else(() => {
        const toLight = where.xyz.sub(P).toVar()
        const d = length(toLight).toVar()
        l.assign(toLight.div(d))
        const attenuation = getDistanceAttenuation({ lightDistance: d, cutoffDistance: kind.z, decayExponent: kind.w } as never).toVar()
        If(kind.x.greaterThan(LIGHT.spot - 0.5), () => {
          const aim = table.element(at.add(uint(3))).xyz
          attenuation.mulAssign(smoothstep(where.w, shine.w, dot(l.negate(), aim)))
        })
        color.assign(shine.xyz.mul(attenuation))
        reach.assign(d.sub(1e-3))
      })
      const dotNL = clamp(dot(n, l), 0, 1).toVar()
      If(dotNL.greaterThan(0), () => {
        const irradiance = color.mul(dotNL).toVar()
        if (shadows)
          If(kind.y.greaterThan(0.5), () => {
            const blocked = traceWithin(layer, lifted, l, reach).toVar() as Hit
            If(blocked.get('didHit'), () => irradiance.assign(vec3(0)))
          })
        const h = normalize(l.add(v))
        const F = F_Schlick({ f0: specularColor, f90: specularF90, dotVH: clamp(dot(v, h), 0, 1) } as never)
        light.addAssign(irradiance.mul(BRDF_Lambert({ diffuseColor: diffuseContribution } as never)).mul(float(1).sub(F)))
        light.addAssign(irradiance.mul(BRDF_GGX({ lightDirection: l, f0: specularColorBlended, f90: 1, roughness, normalView: n, viewDirection: v } as never)).mul(msComp))
      })
    })
    if (!indirect) return light.add(s.emission)
    // The ambient lights: irradiance through the indirect diffuse term.
    const [ssD, msD] = multiscatter(specularColor)
    const keptDiffuse = float(1).sub(ssD.add(msD))
    light.addAssign(header(1).xyz.mul(BRDF_Lambert({ diffuseColor: diffuseContribution } as never)).mul(keptDiffuse))
    // The environment (EnvironmentNode): radiance along the reflection bent towards the normal as roughness grows,
    // irradiance at its roughest level; turned as the scene's materials turn it (tables.ts environmentLookup).
    if (environment) {
      const [ssM, msM] = multiscatter(s.albedo)
      const turn = mat3(header(3).xyz, header(4).xyz, header(5).xyz).toVar()
      const intensity = header(2).x
      const r = normalize(mix(reflect(v.negate(), n), n, pow(roughness, 4)))
      // Each sample isolated, as EnvironmentNode's: one node in two contexts would otherwise build once.
      const radiance = isolate(environment.context({ getUV: () => turn.mul(r), getTextureLevel: () => roughness }).mul(intensity))
      const irradiance = isolate(environment.context({ getUV: () => turn.mul(n), getTextureLevel: () => float(1) }).mul(intensity)).toVar()
      light.addAssign(radiance.mul(mix(ssD, ssM, s.metal)).add(mix(msD, msM, s.metal).mul(irradiance)))
      light.addAssign(diffuseContribution.mul(keptDiffuse).mul(irradiance))
    }
    return light.add(s.emission)
  }

  function shade(origin: TSLNode, dir: TSLNode, coneWidth: TSLNode, coneSpread: TSLNode, mode: ShadeMode = gpuTestHooks.traceShade ?? 'lit', neighbours?: { dx: TSLNode; dy: TSLNode }) {
    const color = vec3(0).toVar()
    const dist = float(-1).toVar()
    const hit = traceFirst(layer, origin, dir).toVar() as Hit
    const found = hit.get('didHit').toVar()
    const travelled = float(0).toVar()
    // A one-sided material's back face (D8): the raster culls it, so the ray goes on past it, once.
    const passes = () => hit.get('side').lessThan(0).and(rowOf(transforms.element(hit.get('objectIndex')).get('_alignment0'))(0).y.lessThan(0.5))
    If(found.and(passes()), () => {
      travelled.assign(hit.get('dist').add(1e-4))
      hit.assign(traceFirst(layer, origin.add(dir.mul(travelled)), dir))
      found.assign(hit.get('didHit').and(passes().not()))
    })
    If(found, () => {
      const t = travelled.add(hit.get('dist')).toVar()
      const slot = transforms.element(hit.get('objectIndex'))
      // The library's struct types its matrices 'mat4x4f', a name TSL does not know: as mat4 they convert as matrices.
      const M = mat4(slot.get('matrixWorld')).toVar()
      const inv = mat4(slot.get('inverseMatrixWorld')).toVar()
      const mi = slot.get('_alignment0').toVar()
      const row = rowOf(mi)
      const P = origin.add(dir.mul(t)).toVar()
      const idx = hit.get('indices').toVar()
      const b = hit.get('barycoord').toVar()
      const wA = attributes.element(idx.x).get('position').w.toVar()
      const wB = attributes.element(idx.y).get('position').w.toVar()
      const wC = attributes.element(idx.z).get('position').w.toVar()
      // The face's own normal where the geometry has none (normals.ts NONE), back in object space.
      const faceObject = vec4(hit.get('normal'), 0).mul(M).xyz
      const smooth = decodeNormal(wA).mul(b.x).add(decodeNormal(wB).mul(b.y)).add(decodeNormal(wC).mul(b.z))
      const normalObject = normalize(select(min(wA, min(wB, wC)).lessThan(0), faceObject, smooth)).toVar()
      const normalWorld = normalize(vec4(normalObject, 0).mul(inv).xyz).toVar()
      const n = select(dot(normalWorld, dir).greaterThan(0), normalWorld.negate(), normalWorld).toVar()
      // Off the face itself on the ray's side, not along the smooth normal, by a margin that grows with the float
      // error of P (its coordinates, the distance it was found at): a shadow ray does not find its own face.
      const face = normalize(hit.get('normal')).toVar()
      const lifted = P.add(select(dot(face, dir).greaterThan(0), face.negate(), face).mul(max(1e-3, t.add(length(P)).mul(1e-5))))
      // The columns' lengths (a column read as M[i] loses its swizzle in r186's WGSL: the axes transformed instead).
      const scale = vec3(length(M.mul(vec4(1, 0, 0, 0)).xyz), length(M.mul(vec4(0, 1, 0, 0)).xyz), length(M.mul(vec4(0, 0, 1, 0)).xyz)).toVar()
      const toPattern = (x: TSLNode, w: number) => inv.mul(vec4(x, w)).xyz.mul(scale)
      const dpx = vec3(0).toVar()
      const dpy = vec3(0).toVar()
      if (neighbours) {
        // Where the neighbours' rays meet the hit's plane, less the hit (S0 hit-shading.md).
        const g = hit.get('normal')
        const onPlane = (r: TSLNode) => origin.add(r.mul(dot(P.sub(origin), g).div(dot(r, g)))).sub(P)
        dpx.assign(toPattern(onPlane(neighbours.dx), 0))
        dpy.assign(toPattern(onPlane(neighbours.dy), 0))
      } else {
        // The cone where it lands, across the surface along the ray stretched by 1/cos.
        const cosI = max(abs(dot(n, dir)), 0.2)
        const width = coneWidth.add(coneSpread.mul(t)).toVar()
        // The ray along the surface; head on, any tangent (Duff et al. 2017, a basis from n without a branch).
        const sz = select(n.z.lessThan(0), float(-1), float(1))
        const a = float(-1).div(sz.add(n.z))
        const tangent = vec3(float(1).add(sz.mul(n.x).mul(n.x).mul(a)), sz.mul(n.x).mul(n.y).mul(a), sz.mul(n.x).negate())
        const onSurface = dir.sub(n.mul(dot(dir, n))).toVar()
        const along = select(length(onSurface).greaterThan(1e-5), normalize(onSurface), tangent).toVar()
        const across = cross(n, along)
        dpx.assign(toPattern(along.mul(width.div(cosI)), 0))
        dpy.assign(toPattern(across.mul(width), 0))
      }
      const s = surfaceAt(row, toPattern(P, 1), normalObject.div(scale), max(length(abs(dpx).add(abs(dpy))), 1e-6), dpx, dpy)
      If(row(0).z.greaterThan(0.5), () => {
        const c = (k: TSLNode) => vertexColor(colors.element(k))
        s.albedo.mulAssign(c(idx.x).mul(b.x).add(c(idx.y).mul(b.y)).add(c(idx.z).mul(b.z)))
      })
      if (mode === 'albedo') color.assign(s.albedo)
      else if (mode === 'outputs') color.assign(vec3(s.rough, s.metal, tsl.luminance(s.emission as never) as TSLNode))
      else if (mode === 'id') color.assign(vec3(float(mi), 0, 0))
      else if (mode === 'object') color.assign(T.fract(T.sin(float(hit.get('objectIndex')).mul(vec3(12.9898, 78.233, 37.719))).mul(43758.5453)))
      else if (mode === 'distance') color.assign(vec3(t.div(30)))
      else color.assign(lightAt(P, n, dir.negate(), s, mode === 'lit' || mode === 'direct', mode === 'lit' || mode === 'unshadowed', lifted))
      dist.assign(t)
    })
    return { color, dist }
  }

  return {
    shade,
    update() {
      const env = scene.environment
      if (!environment || !env || env === shown) return
      shown = env
      environment.value = env
    },
  }
}
