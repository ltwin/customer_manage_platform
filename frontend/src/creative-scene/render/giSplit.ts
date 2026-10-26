import { Color, type Fog, type Scene } from 'three'
import { context, diffuseColor, float, Fn, luminance, metalness, normalView, positionViewDirection, property, sheen, sheenRoughness, specularColor, specularF90, uniform, vec3, vec4 } from 'three/tsl'
import { PassNode, PhysicalLightingModel, type NodeBuilder } from 'three/webgpu'

// The real-time GI's lighting pass takes the materials' indirect light apart
// (R2 design「合成」): the AO is to shade only the indirect light, and the
// screen-space reflections to take the place of the materials' own
// environment reflection. three's examples multiply the AO over the whole
// picture, or rewrite PhysicalLightingModel.indirectSpecular for every
// material in every pass; here the materials' lighting is left whole and the
// indirect light copied out, in the one pass whose context says so.
//
// three has no public way to do this: indirect() and the reflectedLight it
// adds to are touched here only, and a unit test keeps them in sight.
//  - Built without the mark (the workbench, thumbnails, previews), a
//    material compiles to exactly what it did before.
//  - A render object's cache key holds the renderer's context, so the same
//    material in the lighting pass is a pipeline of its own. The context is
//    one per renderer context (splitContext), not one per pass as
//    PassNode.contextNode would make it: with a pipeline built per picture,
//    every export would otherwise leave a set of render objects behind.
//  - Transparent and transmissive materials are not taken apart, as three's
//    builtinAOContext leaves them: they write nothing (alpha 0), and with the
//    material's blending the outputs keep what is behind them.
//  - The indirect diffuse light's alpha is c, the share of the light falling
//    on the surface that its diffuse layer reflects (S2 design「合成」): the
//    world-space layer's composite puts the probes' light through it, and
//    takes the scene's ambient light out of the material's by it.

/** The context mark: set on the lighting pass's contextNode. */
const mark = 'giSplit'

/** The indirect diffuse light; alpha c (diffuseShare), above 0 where a material was taken apart. */
export const giAmbient = property('vec4', 'GIAmbient', vec4(0))
/** The indirect specular light (the environment's reflection). */
export const giEnvSpec = property('vec4', 'GIEnvSpec', vec4(0))
/** The base colour and metalness, for the GI's albedo and the reflections' Fresnel. */
export const giBase = property('vec4', 'GIBase', vec4(0))

/** getFlowContextData is three's, missing from its typings: the data of a context and the contexts it wraps. */
type Context = ReturnType<typeof context> & { getFlowContextData(): Record<string, unknown> }
const contexts = new WeakMap<Context, { version: number; context: Context }>()

/** The renderer's context with the mark: the same node for as long as the renderer's is unchanged. */
export function splitContext(base: Context): Context {
  let made = contexts.get(base)
  if (!made || made.version !== base.version) {
    made = { version: base.version, context: context({ ...base.getFlowContextData(), [mark]: true }) as Context }
    contexts.set(base, made)
  }
  return made.context
}

/**
 * The real-time GI's lighting pass: draws with the materials taken apart, and
 * without the scene's fog (linear, objects/Lighting.tsx). Fogged here, the
 * picture would have the fog where the composite takes the indirect light
 * out, and none on the GI, reflections and contact shadows it puts in: the
 * composite lays the fog over it all (realtimeGI.ts), from these uniforms.
 */
export class GILightingPass extends PassNode {
  readonly fog = { on: uniform(0), color: uniform(new Color()), near: uniform(0), far: uniform(1) }

  updateBefore(frame: Parameters<PassNode['updateBefore']>[0]) {
    const renderer = frame.renderer!
    const scene = this.scene as Scene
    const base = renderer.contextNode
    const fog = scene.fog as Fog | null
    // Another kind of fog (none the editor makes) stays the materials'.
    const linear = fog?.isFog === true
    this.fog.on.value = linear ? 1 : 0
    if (linear) {
      this.fog.color.value.copy(fog.color)
      this.fog.near.value = fog.near
      this.fog.far.value = fog.far
      scene.fog = null
    }
    renderer.contextNode = splitContext(base as Context)
    try {
      return super.updateBefore(frame)
    } finally {
      renderer.contextNode = base
      scene.fog = fog
    }
  }
}

type Node = ReturnType<typeof vec4>

type Builder = NodeBuilder & {
  context: Record<string, unknown> & { reflectedLight: { indirectDiffuse: Node; indirectSpecular: Node }; ambientOcclusion: Node }
  material: { transparent: boolean; transmission?: number }
}

/** The PhysicalLightingModel's members diffuseShare reads, untyped in @types/three. */
type Model = {
  computeMultiscattering(singleScatter: Node, multiScatter: Node, specularF90: Node, f0: Node, iridescenceF0?: Node | null): void
  iridescenceF0Dielectric: Node | null
  sheen: boolean
}

/**
 * three r186's IBLSheenBRDF (nodes/functions/PhysicalLightingModel.js), which
 * it does not export: the sheen's albedo indirectDiffuse compensates the
 * diffuse layer for. A unit test keeps it the same as three's.
 */
const sheenAlbedo = Fn(([normal, viewDir, roughness]: Node[]) => {
  const dotNV = normal.dot(viewDir).saturate()
  const r2 = roughness.mul(roughness)
  const rInv = roughness.add(0.1).reciprocal()
  const a = float(-1.9362).add(roughness.mul(1.0678)).add(r2.mul(0.4573)).sub(rInv.mul(0.8469))
  const b = float(-0.6014).add(roughness.mul(0.5538)).sub(r2.mul(0.467)).sub(rInv.mul(0.1255))
  return a.mul(dotNV).add(b).exp().saturate()
})

/**
 * c: what indirectDiffuse multiplies the Lambert term by, as one number. The
 * share the specular lobe leaves the diffuse layer (three's own
 * computeMultiscattering, with indirectDiffuse's arguments), the sheen's
 * energy compensation where the material has sheen, and the material's AO,
 * which ambientOcclusion() multiplies the diffuse light by. specular_color
 * tints the share channel by channel; alpha holds its luminance. Never below
 * 1e-3: a smooth dielectric at grazing angles leaves the diffuse layer about
 * a tenth, and the composite tells a material taken apart by alpha above 0.
 */
function diffuseShare(model: Model, ambientOcclusion: Node): Node {
  const single = vec3().toVar()
  const multi = vec3().toVar()
  model.computeMultiscattering(single, multi, specularF90, specularColor, model.iridescenceF0Dielectric)
  let share = luminance(single.add(multi).oneMinus())
  if (model.sheen === true) share = share.mul(sheen.r.max(sheen.g).max(sheen.b).mul(sheenAlbedo(normalView, positionViewDirection, sheenRoughness)).oneMinus())
  return share.mul(ambientOcclusion).max(1e-3)
}

/** Wraps PhysicalLightingModel.indirect once; nothing changes until a pass carries the mark. */
export function installGISplit(model: { prototype: { indirect(builder: NodeBuilder): void } } = PhysicalLightingModel): void {
  const proto = model.prototype as { indirect(builder: NodeBuilder): void; giSplit?: true }
  if (proto.giSplit) return
  const indirect = proto.indirect
  proto.indirect = function (this: Model, builder: NodeBuilder) {
    indirect.call(this, builder)
    const { context: ctx, material } = builder as Builder
    if (ctx[mark] !== true || material.transparent || (material.transmission ?? 0) > 0) return
    // What indirect() leaves, AO included: the whole of the material's indirect light.
    giAmbient.assign(vec4(ctx.reflectedLight.indirectDiffuse, diffuseShare(this, ctx.ambientOcclusion)))
    giEnvSpec.assign(vec4(ctx.reflectedLight.indirectSpecular, 1))
    giBase.assign(vec4(diffuseColor.rgb, metalness))
  }
  proto.giSplit = true
}
