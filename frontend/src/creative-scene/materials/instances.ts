import { Color, DoubleSide, FrontSide, Vector2, type BufferGeometry, type Material } from 'three'
import {
  cameraViewMatrix,
  dFdx,
  dFdy,
  faceDirection,
  float,
  fwidth,
  length,
  materialColor,
  materialEmissive,
  materialMetalness,
  materialRoughness,
  max,
  modelNormalMatrix,
  modelScale,
  normalLocal,
  normalView,
  positionLocal,
  positionView,
  reference,
  texture,
  uniform,
  uv,
  varying,
  vec4,
} from 'three/tsl'
import { MeshPhysicalNodeMaterial } from 'three/webgpu'
import { pieceViews } from '../render/pieces.ts'
import { preExposure } from '../world/exposure.ts'
import { materialEntry } from './catalog.ts'
import { acquireTexture, releaseTexture, textureKeys } from './images.ts'
import { bendNormal, mappingOf, shaderKit, triplanar, uvMapped, uvNormal, uvSurface, type Has, type MapKind, type MappingIn } from './maps.ts'
import { bumpNormal, patternFns, type TSLNode } from './patterns.ts'
import { principledProps, renderPolicy, type RenderPolicy } from './principled.ts'
import type { SlotMaterial, Texturing } from './resolve.ts'

// Material instances shared by every mesh that asks for the same thing:
// kind, preset, complete parameters and sidedness. A preset's node graph is
// the same whatever its parameters, which are uniforms, so any number of
// parameter sets share one program. An instance nobody uses is freed a
// moment later, not at once: switching shading back and forth would
// recompile it.

export const unknownGrey = '#8a8a8a'
const releaseAfterMs = 1000

type Entry = { material: MeshPhysicalNodeMaterial; refs: number; timer: ReturnType<typeof setTimeout> | null }
const instances = new Map<string, Entry>()

/** v with every object's keys in order: equal slots key alike whatever order they were written in. */
function canonical(v: unknown): unknown {
  if (Array.isArray(v)) return v.map(canonical)
  if (v === null || typeof v !== 'object') return v
  const o = v as Record<string, unknown>
  return Object.fromEntries(Object.keys(o).sort().map((k) => [k, canonical(o[k])]))
}

/** The whole slot assignment: parameters, and a principled slot's textures, mapping and alpha mode. */
export function keyOf(m: SlotMaterial, doubleSided: boolean): string {
  if (m.kind === 'unknown') return JSON.stringify({ kind: 'unknown', doubleSided })
  const id = m.kind === 'pattern' ? m.id : 'principled'
  const vertexColors = m.kind === 'principled' && !!m.vertexColors
  const texturing = m.kind === 'principled' && m.texturing ? { texturing: canonical(m.texturing) } : {}
  return JSON.stringify({ kind: m.kind, id, params: canonical(m.params), doubleSided, vertexColors, ...texturing })
}

/** The geometries a slot of material m is drawn as: piece by piece if see-through, sorted as its parts were (render/pieces.ts). */
export function drawnGeometries(geometry: BufferGeometry, m: SlotMaterial): readonly BufferGeometry[] {
  return renderPolicy(m).seeThrough ? pieceViews(geometry) : [geometry]
}

// The object-space position in true metres, so a scaled object does not
// stretch its pattern, and the normal for the projection weights.
const patPos: TSLNode = varying(positionLocal.mul(modelScale), 'vPatPos')
const patNormal: TSLNode = varying(normalLocal.div(modelScale), 'vPatNormal')
const footprint: TSLNode = max(length(fwidth(patPos)), float(1e-6))

// Emission is a light: under a physical sky it is cd/m² and drawn × k like
// the lamps (world/exposure.ts preExposure, 1 under any other sky).
const exposedEmission: TSLNode = materialEmissive.mul(preExposure)

/** The slot material each instance was made from, for the world-space layer's material table (render/trace). */
const madeFrom = new WeakMap<object, SlotMaterial>()

/** The slot material a mesh's material instance draws, or undefined for any other material (clay, ghosts, helpers). */
export const slotOf = (material: object): SlotMaterial | undefined => madeFrom.get(material)

export function makeMaterial(m: SlotMaterial, doubleSided: boolean): MeshPhysicalNodeMaterial {
  const material = build(m, doubleSided ? DoubleSide : FrontSide)
  madeFrom.set(material, m)
  // Into shadow maps with the faces toward the light: three draws a one-sided
  // material's back faces, and a receiver's lookup, lifted off it by the
  // normal bias, then reads past a caster's underside where they touch: a
  // lit ring around the contact (fix/scene-sun-cascaded-shadows).
  material.shadowSide = DoubleSide
  return material
}

function build(m: SlotMaterial, side: typeof DoubleSide | typeof FrontSide): MeshPhysicalNodeMaterial {
  if (m.kind === 'unknown') return new MeshPhysicalNodeMaterial({ color: unknownGrey, roughness: 0.9, metalness: 0, side })
  if (m.kind === 'principled') {
    const policy = renderPolicy(m)
    const material = new MeshPhysicalNodeMaterial({ ...principledProps(m.params, policy), side, vertexColors: !!m.vertexColors })
    material.emissiveNode = exposedEmission
    if (m.texturing) applyTextures(material, m.texturing, policy)
    return material
  }
  const p = m.params
  const entry = materialEntry(m.id)
  const fn = patternFns[m.id]
  const material = new MeshPhysicalNodeMaterial({ color: p.color as string, roughness: p.roughness as number, metalness: p.metalness as number, side })
  if (!entry || !fn) return material
  const pm = Object.fromEntries(Object.entries(entry.params).map(([k, spec]) => [k, spec.type === 'color' ? uniform(new Color(p[k] as string)) : uniform(p[k] as number)]))
  const o = fn({ pos: patPos, width: fwidth, normal: patNormal, footprint, pm })
  material.colorNode = o.albedo
  material.roughnessNode = o.rough
  material.metalnessNode = o.metal
  material.normalNode = bumpNormal(positionView, normalView, o.height)
  material.emissiveNode = o.emission.mul(preExposure)
  material.name = m.id
  return material
}

/**
 * A principled slot's textures and alpha mode as nodes (C5c1 design「光栅材质的
 * 构建」): each map multiplies its constant, as three.js and glTF have it, AO
 * darkens only the indirect light, and the normal bends by the mapping's own
 * frame. The textures are held until the material is disposed. Without maps,
 * only the alpha mode's test.
 */
/**
 * A slot's textures as the raster samples them, acquired till material is
 * disposed: the base colour with its alpha, the ORM, and the normal they
 * bend to in view space (null without a normal map). The e2e's checks
 * (render/trace/checks.ts) take the same.
 */
export function slotTextures(material: Material, t: Texturing): { color: TSLNode; orm: TSLNode; has: Has; normal: TSLNode | null } {
  const keys = textureKeys(t)
  const textures: Partial<Record<MapKind, ReturnType<typeof acquireTexture>>> = {}
  for (const [which, key] of Object.entries(keys) as [MapKind, string][]) textures[which] = acquireTexture(key)
  material.addEventListener('dispose', () => Object.values(keys).forEach(releaseTexture))
  const has = { color: !!keys.color, normal: !!keys.normal, orm: !!keys.orm }
  const mapping = mappingOf(t)
  // A tile is size_m wide, as tall as its image says (the first map's, once it is here).
  const first = textures.color ?? textures.normal ?? textures.orm
  const scale = uniform(new Vector2(...mapping.repeat))
  if (mapping.kind === 'triplanar') scale.onRenderUpdate(() => new Vector2(1 / mapping.size, ((first?.userData.aspect as number | undefined) ?? 1) / mapping.size))
  const m: MappingIn<TSLNode> = { scale, cos: uniform(Math.cos(mapping.rotation)), sin: uniform(Math.sin(mapping.rotation)), strength: uniform(mapping.strength) }
  const sample = (which: MapKind, coord: TSLNode) => texture(textures[which]!, coord)
  if (mapping.kind === 'triplanar') {
    const s = triplanar(shaderKit, patPos, patNormal, m, has, sample)
    // The gradient is a covector: into local space by the scale, on to view space by the normal matrices, unnormalised.
    // normalView is already turned to a back face's side, the gradient not: turned with it, the back of a double-sided
    // surface gets the front's bent normal negated (as BumpMapNode does, and the traced hit: render/trace/hit.ts).
    const world: TSLNode | null = s.bend && (modelNormalMatrix as TSLNode).mul(s.bend.mul(modelScale))
    return { color: s.color, orm: s.orm, has, normal: world && bendNormal(shaderKit, normalView, cameraViewMatrix.mul(vec4(world, 0)).xyz.mul(faceDirection)) }
  }
  const b = uvMapped(shaderKit, uv(), m)
  const s = uvSurface(shaderKit, b, m, has, sample)
  return { color: s.color, orm: s.orm, has, normal: s.normal && uvNormal(shaderKit, normalView, dFdx(positionView), dFdy(positionView), dFdx(b), dFdy(b), s.normal) }
}

function applyTextures(material: MeshPhysicalNodeMaterial, t: Texturing, policy: RenderPolicy) {
  const { color, orm, has, normal } = slotTextures(material, t)
  if (normal) material.normalNode = normal
  if (has.color) material.colorNode = vec4(materialColor.mul(color.xyz), 1)
  if (has.orm) {
    material.roughnessNode = materialRoughness.mul(orm.y)
    material.metalnessNode = materialMetalness.mul(orm.z)
    material.aoNode = orm.x
  }
  // opaque leaves the texture's alpha out; clip keeps alpha ≥ cutoff, in the shadow maps too (task 1.1: three's shadow pass reads maskShadowNode).
  // A mask, not alphaTestNode, which keeps alpha > cutoff: the shadow and the traced hits (hit.ts) keep it at the cutoff as well.
  // The opacity is this material's own: the shadow pass builds the mask under its override material, whose materialOpacity is 1.
  const alpha = reference('opacity', 'float', material).mul(color.w)
  if (policy.alphaCutoff !== null) {
    // A cutoff of 0 still cuts what is wholly clear, as the traced hits do (render/trace/maps.ts rowMaps).
    const kept = alpha.greaterThanEqual(uniform(Math.max(policy.alphaCutoff, 1e-6)))
    material.opacityNode = alpha
    material.maskNode = kept
    material.maskShadowNode = kept
  } else if (t.alpha_mode === 'blend') {
    material.opacityNode = alpha
  }
}

/** The shared instance for key, made from m the first time. */
export function materialFor(key: string, m: SlotMaterial, doubleSided: boolean): MeshPhysicalNodeMaterial {
  let e = instances.get(key)
  if (!e) {
    e = { material: makeMaterial(m, doubleSided), refs: 0, timer: null }
    instances.set(key, e)
    // One made for a render that never mounted is freed like an unused one.
    schedule(key, e)
  }
  return e.material
}

export function retain(key: string) {
  const e = instances.get(key)
  if (!e) return
  e.refs++
  if (e.timer !== null) clearTimeout(e.timer)
  e.timer = null
}

export function release(key: string) {
  const e = instances.get(key)
  if (!e) return
  e.refs = Math.max(0, e.refs - 1)
  if (e.refs === 0) schedule(key, e)
}

function schedule(key: string, e: Entry) {
  if (e.timer !== null) clearTimeout(e.timer)
  e.timer = setTimeout(() => {
    e.timer = null
    if (e.refs > 0 || instances.get(key) !== e) return
    instances.delete(key)
    e.material.dispose()
  }, releaseAfterMs)
}

/** How many instances are alive, for tests. */
export const instanceCount = () => instances.size
