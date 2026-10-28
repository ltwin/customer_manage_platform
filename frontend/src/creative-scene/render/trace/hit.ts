import type { Scene, Texture } from 'three'
import * as tsl from 'three/tsl'
import { materialEntry } from '../../materials/catalog.ts'
import { patternFns, type PatternInput, type PatternOut, type TSLNode } from '../../materials/patterns.ts'
import { preExposure } from '../../world/exposure.ts'
import { gpuTestHooks } from '../gpu.ts'
import { probeIrradiance, type ProbeNodes } from '../probes/lookup.ts'
import type { TraceLayer } from './layer.ts'
import { ROW as NORMAL_ROW, STEPS } from './normals.ts'
import { HEADER, LIGHT, LIGHT_ROW, MATERIAL_ROW, presets, type TraceTables } from './tables.ts'
import { traceFirst, traceWithin, type Hit } from './wgsl.ts'
import { coneSample, effectiveFalloff, horizonDirection, hybridLobe, sinHalfOf, sphereEntry } from '../lightModel/formulas.ts'
import { clippedFormFactor, ellipseViewFormFactor, ltcEllipse, ltcTables, rectVectorFormFactor } from '../lightModel/ltc.ts'

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
//
// A probe's ray (radiance, S2 design「探针光线」) is shaded as DDGI shades it:
// the diffuse light alone, the lights that cast drawn one at a time.

type Op = (...args: TSLNode[]) => TSLNode
const T = tsl as unknown as Record<string, Op>
const { frameId, screenCoordinate } = tsl as unknown as Record<string, TSLNode>
const { BRDF_GGX, BRDF_Lambert, DFGLUT, F_Schlick, If, LTC_Evaluate, LTC_Uv, Loop, Switch, abs, clamp, cos, cross, dot, float, floor, getDistanceAttenuation, isolate, length, luminance, mat3, mat4, max, min, mix, normalize, pmremTexture, pow, reflect, select, sin, smoothstep, sqrt, sRGBTransferEOTF, texture, uint, vec2, vec3, vec4 } = T

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
  /**
   * A probe ray's radiance and hit distance (S2 design「探针光线」): the
   * diffuse light at its first hit, kd · c / π · (E_direct + indirect(P, n)
   * + the ambient lights × fill) + emission, as exposed as the raster's. A
   * back face, one-sided or not, is evidence of being inside: radiance 0,
   * the distance negative. A miss is the environment at roughness 0.5 along
   * the ray (no distance: MISS). indirect gives the irradiance at a hit from
   * the probes, exposed (the bounces); seed is the ray's own, for which light
   * casts its shadow.
   */
  radiance(origin: TSLNode, dir: TSLNode, seed: TSLNode, indirect: (P: TSLNode, n: TSLNode) => TSLNode, fill: TSLNode): TSLNode
  /**
   * radiance's hit and where a path goes on from it (checks.ts's per-pixel
   * reference): L and t as radiance gives them; on a front face, the point,
   * the point a ray leaves from (lifted), the normal towards the ray and the
   * diffuse albedo kd · c (front 1); a miss or a back face, front 0.
   */
  bounce(origin: TSLNode, dir: TSLNode, seed: TSLNode, indirect: (P: TSLNode, n: TSLNode) => TSLNode, fill: TSLNode): Bounce
  /** The environment's irradiance at a normal, exposed, turned as the scene's materials turn it: what the probes fall back to. */
  environmentIrradiance(n: TSLNode): TSLNode
  /** Before each frame: the environment as the scene lights with it now (a new sky is a new texture, not a new shader). */
  update(): void
}

export type Bounce = { L: TSLNode; t: TSLNode; P: TSLNode; lifted: TSLNode; n: TSLNode; albedo: TSLNode; front: TSLNode }

/** What radiance gives as the distance of a ray that hits nothing: past any probe's depth range. */
export const MISS = 1e4

/** A ray's cone spread a metre for a probe's 64 rays: the solid angle each covers (S2 design「探针光线」). */
const PROBE_SPREAD = Math.sqrt((4 * Math.PI) / 64)

/** PCG's hash of a uint (Jarzynski and Olano 2020): the probes' random numbers, from the frame, the probe and the ray alone (P3). */
export function pcg(v: TSLNode): TSLNode {
  const state = v.mul(uint(747796405)).add(uint(2891336453)).toVar()
  const word = state.shiftRight(state.shiftRight(uint(28)).add(uint(4))).bitXor(state).mul(uint(277803737)).toVar()
  return word.shiftRight(uint(22)).bitXor(word)
}

/** A uniform number in [0, 1) from a hash. */
export const unit = (h: TSLNode) => float(h.shiftRight(uint(8))).div(16777216)

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

/**
 * Shading at the hits of rays through one layer: the reflections' (handle.reflect) or the probes' (handle.probes).
 * world, the probes once the screen draws with them (S2 design「命中点的间接光」): shade's light from the
 * environment, its irradiance and its radiance along the reflection, becomes the probes' where they cover the hit,
 * the ambient lights' times fill. Without (the probes' own rays, checks.ts's hitCheck), the environment's.
 */
export function hitShading(layer: TraceLayer, tables: TraceTables, scene: Scene, world: { probes: ProbeNodes; fill: number } | null = null): HitShading {
  const table = tables.node
  // three's LTC tables for the area lights' specular, the half-float ones: filterable on every device.
  const ltc = ltcTables()
  const { transforms, attributes } = layer.stores
  const colors = layer.colorsNode
  const header = (k: number) => table.element(uint(k))
  // The environment the scene lights with: made once there is one, its texture kept up to date (update).
  let shown: Texture | null = scene.environment
  const environment: TSLNode | null = shown ? pmremTexture(shown) : null

  /** The environment along a world direction at a roughness level, turned as the scene's materials turn it, times its intensity (tables.ts header). */
  const environmentAt = (dir: TSLNode, level: TSLNode) => {
    if (!environment) return vec3(0)
    const turn = mat3(header(3).xyz, header(4).xyz, header(5).xyz)
    // Each sample isolated, as EnvironmentNode's: one node in two contexts would otherwise build once.
    return isolate(environment.context({ getUV: () => turn.mul(dir), getTextureLevel: () => level }).mul(header(2).x))
  }

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
   * A light of the table at P as a hit takes it (scene-light-radius design「灯光表与命中点」): the unit way to its
   * centre, its colour there (falloff at the effective distance, a spot's cone), how far a shadow ray toward the
   * centre goes (0: all the way), and its size: s, the sine of its angular radius (0 without a size), and the way
   * to its centre d long (a directional light's at d = 1), as render/lightModel/formulas.ts takes them.
   */
  function tableLight(at: TSLNode, P: TSLNode) {
    const kind = table.element(at).toVar()
    const where = table.element(at.add(uint(1))).toVar()
    const shine = table.element(at.add(uint(2))).toVar()
    const size = table.element(at.add(uint(4))).x.toVar()
    const l = vec3(0).toVar()
    const toCentre = vec3(0).toVar()
    const color = vec3(0).toVar()
    const reach = float(0).toVar()
    const sinHalf = float(0).toVar()
    If(kind.x.lessThan(LIGHT.point - 0.5), () => {
      l.assign(where.xyz)
      toCentre.assign(where.xyz)
      color.assign(shine.xyz)
      // Held off 0 and 1 as the raster's sun is (lightModel/nodes.ts).
      sinHalf.assign(select(size.greaterThan(0), clamp(size, 1e-3, 0.9999), float(0)))
    }).Else(() => {
      const toLight = where.xyz.sub(P).toVar()
      const d = length(toLight).toVar()
      l.assign(toLight.div(d))
      toCentre.assign(toLight)
      // At the effective distance max(d, r): a factor 1 at r = 0 (formulas.ts effectiveFalloff).
      const attenuation = getDistanceAttenuation({ lightDistance: d, cutoffDistance: kind.z, decayExponent: kind.w } as never).mul(effectiveFalloff(d, size, kind.w)).toVar()
      If(kind.x.greaterThan(LIGHT.spot - 0.5), () => {
        const aim = table.element(at.add(uint(3))).xyz
        attenuation.mulAssign(smoothstep(where.w, shine.w, dot(l.negate(), aim)))
      })
      color.assign(shine.xyz.mul(attenuation))
      reach.assign(d.sub(1e-3))
      sinHalf.assign(select(size.greaterThan(0), sinHalfOf(size, d), float(0)))
    })
    const sized = size.greaterThan(0).toVar()
    // The diffuse's way: Snyder's bent toward the part above the horizon (formulas.ts horizonDirection); a light without a size, its centre's.
    const diffuseWay = (n: TSLNode) => select(sized, horizonDirection({ lightVector: toCentre, sinHalf }, n), l)
    /**
     * A shadow ray's way and reach toward a random point of the light, u1 and u2 uniform: within its cone, stopping
     * 1 mm short of a sphere's surface, not its centre, so a bulb model round the light does not shadow it. Without
     * a size, the centre as before.
     */
    const shadowRay = (u1: TSLNode, u2: TSLNode) => {
      const way = select(sized, coneSample(l, sinHalf, u1, u2), l).toVar()
      const sphere = sized.and(kind.x.greaterThan(LIGHT.point - 0.5))
      return { way, reach: select(sphere, max(sphereEntry(toCentre, size, way).sub(1e-3), 1e-4), reach) }
    }
    return { kind, l, color, sized, sinHalf, toCentre, diffuseWay, shadowRay }
  }

  /**
   * An area light of the table at P (scene-area-light design「命中点」), as the raster's (lightModel/direct.ts): its
   * vector form factor from P with the identity LTC (|F| over its face, F / |F| its average way, lightModel/ltc.ts),
   * its colour × luminance, its face's centre and half axes, three's corners of it, and a shadow ray from a point
   * toward a uniform point of its face.
   */
  function areaLight(at: TSLNode, P: TSLNode) {
    const kind = table.element(at).toVar()
    const centre = table.element(at.add(uint(1))).xyz.toVar()
    const color = table.element(at.add(uint(2))).xyz.toVar()
    const halfX = table.element(at.add(uint(3))).xyz.toVar()
    const halfY = table.element(at.add(uint(4))).xyz.toVar()
    const ellipse = kind.z.greaterThan(0.5).toVar()
    // Variables here, before the branches: an expression first built inside one is declared there, out of reach of the next.
    const corners = [centre.add(halfX).sub(halfY).toVar(), centre.sub(halfX).sub(halfY).toVar(), centre.sub(halfX).add(halfY).toVar(), centre.add(halfX).add(halfY).toVar()] as const
    const F = vec3(0).toVar()
    If(ellipse, () => F.assign(ellipseViewFormFactor(P, centre, halfX, halfY))).Else(() => F.assign(rectVectorFormFactor(P, ...corners)))
    const shadowRay = (u1: TSLNode, u2: TSLNode, from: TSLNode) => {
      // A uniform point of the face: the square [−1, 1]², the disc by √u.
      const r = sqrt(u1)
      const phi = u2.mul(Math.PI * 2)
      const s = select(ellipse, vec2(r.mul(cos(phi)), r.mul(sin(phi))), vec2(u1.mul(2).sub(1), u2.mul(2).sub(1)))
      const toward = centre.add(halfX.mul(s.x)).add(halfY.mul(s.y)).sub(from).toVar()
      const d = length(toward).toVar()
      return { way: toward.div(d), reach: max(d.sub(1e-3), 1e-4) }
    }
    return { kind, centre, color, halfX, halfY, ellipse, corners, F, shadowRay }
  }

  /** Two uniform numbers for light i's shadow ray, from a seed. */
  const pair = (seed: TSLNode, i: TSLNode) => {
    const h = pcg(seed.add(i.mul(uint(0x85ebca6b))).add(uint(0x27d4eb2f))).toVar()
    return [unit(h), unit(pcg(h))] as const
  }

  /**
   * Lit at P (world), normal n towards the viewer, v towards the viewer:
   * PhysicalLightingModel's direct and indirect terms. Shadow rays leave from
   * lifted, P off its face; toward a sized light, one a pixel and frame to a
   * random point of it, which the real-time GI's temporal accumulation averages.
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
    // A pixel's and frame's seed for the shadow rays toward sized lights.
    const seed = uint(screenCoordinate.x).add(uint(screenCoordinate.y).mul(uint(65536))).add(uint(frameId).mul(uint(0x9e3779b9))).toVar()
    // The direct lights.
    const first = header(0).y.toUint().toVar()
    Loop({ start: uint(0), end: header(0).x.toUint(), type: 'uint', condition: '<' }, ({ i }: { i: TSLNode }) => {
      const at = first.add(i.mul(uint(LIGHT_ROW))).toVar()
      If(table.element(at).x.greaterThan(LIGHT.spot + 0.5), () => {
        // An area light: its irradiance π · L · F; the diffuse toward its average way, the specular its LTC's.
        const a = areaLight(at, P)
        const E = clippedFormFactor(a.F, n).mul(Math.PI).toVar()
        If(E.greaterThan(0), () => {
          const visible = float(1).toVar()
          if (shadows)
            If(a.kind.y.greaterThan(0.5), () => {
              const [u1, u2] = pair(seed, i)
              const ray = a.shadowRay(u1, u2, lifted)
              const blocked = traceWithin(layer, lifted, ray.way, ray.reach).toVar() as Hit
              If(blocked.get('didHit'), () => visible.assign(0))
            })
          const along = normalize(a.F).toVar()
          const wl = normalize(along.add(n.mul(max(float(1e-3).sub(dot(along, n)), 0))))
          const h = normalize(wl.add(v))
          const F = F_Schlick({ f0: specularColor, f90: specularF90, dotVH: clamp(dot(v, h), 0, 1) } as never)
          light.addAssign(a.color.mul(E).mul(visible).mul(BRDF_Lambert({ diffuseColor: diffuseContribution } as never)).mul(float(1).sub(F)))
          const uv = LTC_Uv({ N: n, V: v, roughness } as never)
          const t1 = texture(ltc.LTC_HALF_1, uv).level(0).toVar()
          const t2 = texture(ltc.LTC_HALF_2, uv).level(0).toVar()
          const mInv = mat3(vec3(t1.x, 0, t1.y), vec3(0, 1, 0), vec3(t1.z, 0, t1.w)).toVar()
          // Both shapes, one selected: three r186 leaves a float empty when either is built in a branch of its own here.
          const [p0, p1, p2, p3] = a.corners
          const form = select(a.ellipse, ltcEllipse({ N: n, V: v, P, mInv, centre: a.centre, halfX: a.halfX, halfY: a.halfY }).x, LTC_Evaluate({ N: n, V: v, P, mInv, p0, p1, p2, p3 } as never).x)
          // LTC Fresnel Approximation by Stephen Hill, as three's directRectArea.
          const fresnel = specularColorBlended.mul(t2.x).add(specularF90.sub(specularColorBlended).mul(t2.y))
          light.addAssign(a.color.mul(fresnel).mul(form).mul(visible).mul(msComp))
        })
      }).Else(() => {
        const { kind, l, color, sized, sinHalf, toCentre, diffuseWay, shadowRay } = tableLight(at, P)
        const wl = diffuseWay(n).toVar()
        const dotNL = clamp(dot(n, wl), 0, 1).toVar()
        If(dotNL.greaterThan(0), () => {
          const visible = float(1).toVar()
          if (shadows)
            If(kind.y.greaterThan(0.5), () => {
              const [u1, u2] = pair(seed, i)
              const ray = shadowRay(u1, u2)
              const blocked = traceWithin(layer, lifted, ray.way, ray.reach).toVar() as Hit
              If(blocked.get('didHit'), () => visible.assign(0))
            })
          const irradiance = color.mul(dotNL).mul(visible).toVar()
          const h = normalize(wl.add(v))
          const F = F_Schlick({ f0: specularColor, f90: specularF90, dotVH: clamp(dot(v, h), 0, 1) } as never)
          light.addAssign(irradiance.mul(BRDF_Lambert({ diffuseColor: diffuseContribution } as never)).mul(float(1).sub(F)))
          // The specular toward the hybrid lobe of a sized light (formulas.ts hybridLobe), as the raster's (lightModel/direct.ts).
          const lobe = hybridLobe({ lightVector: toCentre, sinHalf }, roughness, n, v)
          const way = select(sized, lobe.direction, l).toVar()
          const specularIrradiance = select(sized, color.mul(clamp(dot(n, way), 0, 1)).mul(lobe.energy).mul(visible), irradiance)
          light.addAssign(specularIrradiance.mul(BRDF_GGX({ lightDirection: way, f0: specularColorBlended, f90: 1, roughness: select(sized, lobe.roughness, roughness), normalView: n, viewDirection: v } as never)).mul(msComp))
        })
      })
    })
    if (!indirect) return light.add(s.emission)
    // The ambient lights: irradiance through the indirect diffuse term.
    const [ssD, msD] = multiscatter(specularColor)
    const keptDiffuse = float(1).sub(ssD.add(msD))
    light.addAssign(header(1).xyz.mul(world ? world.fill : 1).mul(BRDF_Lambert({ diffuseColor: diffuseContribution } as never)).mul(keptDiffuse))
    // The environment (EnvironmentNode): radiance along the reflection bent towards the normal as roughness grows,
    // irradiance at its roughest level (over π, as three's diffuse and multiscatter terms take it); turned as the
    // scene's materials turn it (tables.ts environmentLookup).
    // With the probes, where they cover the hit, both are theirs: the irradiance their E over π, the radiance along r
    // their E about r over π (DDGI's glossy fallback). A hit has no reflections of its own, and the environment's
    // radiance is the sky unoccluded: indoors, at a grazing view, a wall or floor in a mirror turned sky blue.
    const r = normalize(mix(reflect(v.negate(), n), n, pow(roughness, 4))).toVar()
    const irradiance = (environment ? environmentAt(n, float(1)) : vec3(0)).toVar()
    const radiance = (environment ? environmentAt(r, roughness) : vec3(0)).toVar()
    if (world) {
      const diffuse = probeIrradiance(world.probes, P, n, v)
      const along = probeIrradiance(world.probes, P, r, v)
      irradiance.assign(irradiance.mul(float(1).sub(diffuse.coverage)).add(diffuse.pre.div(Math.PI)))
      radiance.assign(radiance.mul(float(1).sub(along.coverage)).add(along.pre.div(Math.PI)))
    }
    if (environment || world) {
      const [ssM, msM] = multiscatter(s.albedo)
      light.addAssign(radiance.mul(mix(ssD, ssM, s.metal)).add(mix(msD, msM, s.metal).mul(irradiance)))
      light.addAssign(diffuseContribution.mul(keptDiffuse).mul(irradiance))
    }
    return light.add(s.emission)
  }

  /**
   * A hit as the material at it shades: where it is (P), its normal towards
   * the ray (n), the surface, the point shadow rays leave from (lifted) and
   * its material row (mi). t is the distance along the ray from origin; the
   * width a preset filters by is the ray's cone where it lands, or from the
   * neighbours' rays (a primary ray's).
   */
  function surfaceHit(origin: TSLNode, dir: TSLNode, hit: Hit, t: TSLNode, coneWidth: TSLNode, coneSpread: TSLNode, neighbours?: { dx: TSLNode; dy: TSLNode }) {
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
    return { P, n, s, lifted, mi }
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
      const { P, n, s, lifted, mi } = surfaceHit(origin, dir, hit, t, coneWidth, coneSpread, neighbours)
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

  /**
   * The direct light's irradiance at P (normal n): the lights that do not
   * cast added up as the raster adds them; of those that cast, one drawn by
   * its unshadowed share (weighted reservoir sampling, one pass) and a
   * shadow ray towards it alone, its irradiance over the chance it was drawn.
   * Unbiased; one shadow ray a hit however many lights; the probes' 64 rays
   * and their history average the noise (S2 design「探针光线」, D6).
   */
  function directAt(P: TSLNode, n: TSLNode, lifted: TSLNode, seed: TSLNode): TSLNode {
    const e = vec3(0).toVar()
    const total = float(0).toVar()
    const picked = vec3(0).toVar()
    const towards = vec3(0).toVar()
    const reach = float(0).toVar()
    const first = header(0).y.toUint().toVar()
    Loop({ start: uint(0), end: header(0).x.toUint(), type: 'uint', condition: '<' }, ({ i }: { i: TSLNode }) => {
      const at = first.add(i.mul(uint(LIGHT_ROW))).toVar()
      /** A light of irradiance at P: added, or if it casts, weighed into the draw, its shadow ray made once drawn. */
      const consider = (kind: TSLNode, irradiance: TSLNode, shadowRay: (u1: TSLNode, u2: TSLNode) => { way: TSLNode; reach: TSLNode }) => {
        If(kind.y.lessThan(0.5), () => {
          e.addAssign(irradiance)
        }).Else(() => {
          const w = luminance(irradiance).toVar()
          If(w.greaterThan(0), () => {
            total.addAssign(w)
            // This light takes the place of the one drawn so far with chance w / total.
            If(unit(pcg(seed.add(i.mul(uint(0x9e3779b9))))).mul(total).lessThan(w), () => {
              const [u1, u2] = pair(seed, i)
              const ray = shadowRay(u1, u2)
              picked.assign(irradiance)
              towards.assign(ray.way)
              reach.assign(ray.reach)
            })
          })
        })
      }
      If(table.element(at).x.greaterThan(LIGHT.spot + 0.5), () => {
        // An area light: its irradiance π · L · F, the weight and what is picked alike (I4).
        const a = areaLight(at, P)
        consider(a.kind, a.color.mul(clippedFormFactor(a.F, n).mul(Math.PI)).toVar(), (u1, u2) => a.shadowRay(u1, u2, lifted))
      }).Else(() => {
        const { kind, color, diffuseWay, shadowRay } = tableLight(at, P)
        // Weighed by its irradiance toward the bent way, as lightAt lights by it: past the terminator a sized light still lights.
        consider(kind, color.mul(clamp(dot(n, diffuseWay(n)), 0, 1)).toVar(), shadowRay)
      })
    })
    If(total.greaterThan(0), () => {
      const blocked = traceWithin(layer, lifted, towards, reach).toVar() as Hit
      If(blocked.get('didHit').not(), () => {
        e.addAssign(picked.mul(total.div(max(luminance(picked), 1e-30))))
      })
    })
    return e
  }

  function bounce(origin: TSLNode, dir: TSLNode, seed: TSLNode, indirect: (P: TSLNode, n: TSLNode) => TSLNode, fill: TSLNode): Bounce {
    const out = { L: environmentAt(dir, float(0.5)).toVar(), t: float(MISS).toVar(), P: vec3(0).toVar(), lifted: vec3(0).toVar(), n: vec3(0, 1, 0).toVar(), albedo: vec3(0).toVar(), front: float(0).toVar() }
    const hit = traceFirst(layer, origin, dir).toVar() as Hit
    If(hit.get('didHit'), () => {
      const t = hit.get('dist').toVar()
      If(hit.get('side').lessThan(0), () => {
        out.L.assign(vec3(0))
        out.t.assign(t.negate())
      }).Else(() => {
        // The cone a probe's ray covers, from a point.
        const { P, n, s, lifted } = surfaceHit(origin, dir, hit, t, float(0), float(PROBE_SPREAD))
        // c as the composite's (giSplit.ts diffuseShare): what the specular lobe leaves the diffuse layer, a hit's
        // dielectric specular as lightAt's.
        const dfg = DFGLUT({ roughness: min(max(s.rough, 0.0525), 1), dotNV: clamp(dot(n, dir.negate()), 0, 1) } as never).toVar()
        const Ess = dfg.x.add(dfg.y)
        const FssEss = float(0.04).mul(dfg.x).add(dfg.y)
        const Favg = float(0.04).add(float(0.96).mul(0.047619))
        const c = float(1).sub(FssEss.add(FssEss.mul(Favg).div(float(1).sub(float(1).sub(Ess).mul(Favg))).mul(float(1).sub(Ess))))
        const kd = s.albedo.mul(float(1).sub(s.metal))
        // A metal's specular reflectance taken as diffuse (S2 tasks 5.2b): diffuse alone, a mirror is black to the
        // probes, and a ball before it, lit by its probes there, black in the mirror where it sees the sky.
        const albedo = kd.mul(c).add(s.albedo.mul(s.metal)).toVar()
        const E = directAt(P, n, lifted, seed).add(indirect(P, n)).add(header(1).xyz.mul(fill))
        out.L.assign(albedo.mul(E).div(Math.PI).add(s.emission))
        out.t.assign(t)
        out.P.assign(P)
        out.lifted.assign(lifted)
        out.n.assign(n)
        out.albedo.assign(albedo)
        out.front.assign(1)
      })
    })
    return out
  }

  function radiance(origin: TSLNode, dir: TSLNode, seed: TSLNode, indirect: (P: TSLNode, n: TSLNode) => TSLNode, fill: TSLNode): TSLNode {
    const b = bounce(origin, dir, seed, indirect, fill)
    return vec4(b.L, b.t)
  }

  return {
    shade,
    radiance,
    bounce,
    // π: the roughest level is the cosine-weighted mean radiance, as EnvironmentNode's irradiance multiplies it.
    environmentIrradiance: (n) => environmentAt(n, float(1)).mul(Math.PI),
    update() {
      const env = scene.environment
      if (!environment || !env || env === shown) return
      shown = env
      environment.value = env
    },
  }
}
