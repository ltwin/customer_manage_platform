import * as tsl from 'three/tsl'
import { DirectionalLightNode, PointLightNode, SpotLightNode, type NodeBuilder, type WebGPURenderer } from 'three/webgpu'
import type { TSLNode } from '../../materials/patterns.ts'
import { effectiveFalloff } from './formulas.ts'
import { SceneSphereLight, SceneSpotLight, SceneSunLight, type SizedLight } from './lights.ts'

// The light nodes of the lights with a size (Epic design「直接光的入口」).
// three maps a light to its node by the light's exact class (NodeLibrary),
// so these draw the document's lights and the skies' sun and moon only.
//
// A light of size 0 as it builds draws as three's: the node returns its
// parent's result untouched, and the shader is the one before sizes
// (AC-S1-2). Its size moving off 0 builds the light anew
// (objects/SceneLights.tsx), so the size it builds with tells which. With a
// size, a sphere's light falls off with the effective distance max(d, r) in
// place of d (Epic design I2).

type Op = (...args: TSLNode[]) => TSLNode
const { length, reference } = tsl as unknown as Record<string, Op>
const { renderGroup } = tsl as unknown as Record<string, TSLNode>

type Direct = { lightColor: TSLNode; lightDirection: TSLNode }
type NodeOf<L> = { light: L; decayExponentNode: TSLNode; getLightVector(builder: NodeBuilder): TSLNode }

/** The light's size as each render starts: sizes other than 0 change no shader. */
export const sizeNode = (light: SizedLight): TSLNode => reference('size', 'float', light as unknown as TSLNode).setGroup(renderGroup)

/** A sphere's direct light: three's, its colour times the effective distance's falloff over d's. */
function sphereDirect(node: NodeOf<SceneSphereLight | SceneSpotLight>, base: Direct, builder: NodeBuilder): Direct {
  if (!(node.light.size > 0)) return base
  const d = length(node.getLightVector(builder))
  return { ...base, lightColor: base.lightColor.mul(effectiveFalloff(d, sizeNode(node.light), node.decayExponentNode)) }
}

class SphereLightNode extends PointLightNode {
  setupDirect(builder: NodeBuilder) {
    const base = super.setupDirect(builder) as Direct
    return sphereDirect(this as unknown as NodeOf<SceneSphereLight>, base, builder)
  }
}

class SizedSpotLightNode extends SpotLightNode {
  setupDirect(builder: NodeBuilder) {
    const base = super.setupDirect(builder) as Direct
    return sphereDirect(this as unknown as NodeOf<SceneSpotLight>, base, builder)
  }
}

/** A disc at infinity: no falloff, its size only shapes the light (render/lightModel/direct.ts). */
class SunDiscLightNode extends DirectionalLightNode {}

const installed = new WeakSet<WebGPURenderer>()

/** Maps the sized lights to their nodes for renderer; once a renderer (render/gpu.ts). */
export function installLightModel(renderer: WebGPURenderer) {
  if (installed.has(renderer)) return
  installed.add(renderer)
  const library = renderer.library as unknown as { addLight(node: unknown, light: unknown): void }
  library.addLight(SphereLightNode, SceneSphereLight)
  library.addLight(SizedSpotLightNode, SceneSpotLight)
  library.addLight(SunDiscLightNode, SceneSunLight)
}
