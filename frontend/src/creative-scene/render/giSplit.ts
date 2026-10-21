import { context, diffuseColor, metalness, property, vec4 } from 'three/tsl'
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

/** The context mark: set on the lighting pass's contextNode. */
const mark = 'giSplit'

/** The indirect diffuse light, alpha 1 where a material was taken apart. */
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

/** The real-time GI's lighting pass: draws with the materials taken apart. */
export class GILightingPass extends PassNode {
  updateBefore(frame: Parameters<PassNode['updateBefore']>[0]) {
    const renderer = frame.renderer!
    const base = renderer.contextNode
    renderer.contextNode = splitContext(base as Context)
    try {
      return super.updateBefore(frame)
    } finally {
      renderer.contextNode = base
    }
  }
}

type Builder = NodeBuilder & {
  context: Record<string, unknown> & { reflectedLight: { indirectDiffuse: ReturnType<typeof vec4>; indirectSpecular: ReturnType<typeof vec4> } }
  material: { transparent: boolean; transmission?: number }
}

/** Wraps PhysicalLightingModel.indirect once; nothing changes until a pass carries the mark. */
export function installGISplit(model: { prototype: { indirect(builder: NodeBuilder): void } } = PhysicalLightingModel): void {
  const proto = model.prototype as { indirect(builder: NodeBuilder): void; giSplit?: true }
  if (proto.giSplit) return
  const indirect = proto.indirect
  proto.indirect = function (this: unknown, builder: NodeBuilder) {
    indirect.call(this, builder)
    const { context: ctx, material } = builder as Builder
    if (ctx[mark] !== true || material.transparent || (material.transmission ?? 0) > 0) return
    // What indirect() leaves, AO included: the whole of the material's indirect light.
    giAmbient.assign(vec4(ctx.reflectedLight.indirectDiffuse, 1))
    giEnvSpec.assign(vec4(ctx.reflectedLight.indirectSpecular, 1))
    giBase.assign(vec4(diffuseColor.rgb, metalness))
  }
  proto.giSplit = true
}
