import { Matrix4 } from 'three'
import { RectAreaLightTexturesLib } from 'three/addons/lights/RectAreaLightTexturesLib.js'
import * as tsl from 'three/tsl'
import { DirectionalLightNode, PointLightNode, RectAreaLightNode, SpotLightNode, type NodeBuilder, type NodeFrame, type WebGPURenderer } from 'three/webgpu'
import type { TSLNode } from '../../materials/patterns.ts'
import { installAreaDirect, installDirect } from './direct.ts'
import { effectiveFalloff, sinHalfOf, type Sized } from './formulas.ts'
import { SceneAreaLight, SceneSphereLight, SceneSpotLight, SceneSunLight, type SizedLight } from './lights.ts'

// The light nodes of the lights with a size (Epic design「直接光的入口」).
// three maps a light to its node by the light's exact class (NodeLibrary),
// so these draw the document's lights and the skies' sun and moon only.
//
// A light of size 0 as it builds draws as three's: the node returns its
// parent's result untouched, and the shader is the one before sizes
// (AC-S1-2). Its size moving off 0 builds the light anew
// (objects/SceneLights.tsx), so the size it builds with tells which. With a
// size, a sphere's light falls off with the effective distance max(d, r) in
// place of d (Epic design I2), and every sized light hands the lighting model
// its shape besides its direction (sized, render/lightModel/direct.ts).
//
// An area light (data: 面光源) is three's RectAreaLight drawn with its face
// as its world transform scales it, its parents' included (AreaLightNode),
// casting a cube's shadow, its ellipse drawn by direct.ts.
//
// Checked against three r186. This file (RectAreaLightNode's update and
// setupDirectRectArea), direct.ts (PhysicalLightingModel's direct and
// directRectArea), ltc.ts (LTC.js's LTC_ClippedSphereFormFactor),
// shadowFilter.ts (rawDepth, ShadowNode's coordinates) and sunShadow.ts
// (ownNames) lean on its internals: on an upgrade, read them again against
// the new source and run the AC-S1-10 and area light e2e.

type Op = (...args: TSLNode[]) => TSLNode
const { clamp, length, pointShadow, reference, sin } = tsl as unknown as Record<string, Op>
const { renderGroup } = tsl as unknown as Record<string, TSLNode>

type Direct = { lightColor: TSLNode; lightDirection: TSLNode; sized?: Sized }
type NodeOf<L> = { light: L; decayExponentNode: TSLNode; getLightVector(builder: NodeBuilder): TSLNode }

/** The light's size as each render starts: sizes other than 0 change no shader. */
export const sizeNode = (light: SizedLight): TSLNode => reference('size', 'float', light as unknown as TSLNode).setGroup(renderGroup)

/** A sphere's direct light: three's, its colour times the effective distance's falloff over d's. */
function sphereDirect(node: NodeOf<SceneSphereLight | SceneSpotLight>, base: Direct, builder: NodeBuilder): Direct {
  if (!(node.light.size > 0)) return base
  const lightVector = node.getLightVector(builder).toVar()
  const d = length(lightVector).toVar()
  const r = sizeNode(node.light)
  return {
    ...base,
    lightColor: base.lightColor.mul(effectiveFalloff(d, r, node.decayExponentNode)),
    sized: { lightVector, sinHalf: sinHalfOf(r, d).toVar() },
  }
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

/** A disc at infinity: no falloff, its size only shapes the light; s is the sine of half its angular diameter. */
class SunDiscLightNode extends DirectionalLightNode {
  setupDirect(builder: NodeBuilder) {
    const base = super.setupDirect(builder) as Direct
    const light = (this as unknown as NodeOf<SceneSunLight>).light
    if (!(light.size > 0)) return base
    const sinHalf = clamp(sin(sizeNode(light).mul(Math.PI / 360)), 1e-3, 0.9999).toVar()
    return { ...base, sized: { lightVector: base.lightDirection, sinHalf } }
  }
}

const viewRotation = new Matrix4()

/**
 * An area light: its face's half axes as the light keeps them in world space
 * (SceneAreaLight), scale and all, in place of three's, which takes the
 * rotation alone; a cube's shadow, as a point light's (the volumes' when they
 * set one, AnalyticLightNode prefers the shadow's own node); and whether it
 * is an ellipse, fixed as it builds (objects/lights.ts lightKey).
 */
class AreaLightNode extends RectAreaLightNode {
  declare light: SceneAreaLight
  declare halfWidth: TSLNode & { value: SceneAreaLight['halfX'] }
  declare halfHeight: TSLNode & { value: SceneAreaLight['halfY'] }

  update(frame: NodeFrame) {
    const out = super.update(frame)
    if (!frame.camera) return out
    viewRotation.extractRotation(frame.camera.matrixWorldInverse)
    this.halfWidth.value.copy(this.light.halfX).applyMatrix4(viewRotation)
    this.halfHeight.value.copy(this.light.halfY).applyMatrix4(viewRotation)
    return out
  }

  setupShadowNode() {
    return pointShadow(this.light as unknown as TSLNode)
  }

  setupDirectRectArea(builder: NodeBuilder) {
    const base = super.setupDirectRectArea(builder)
    return base && { ...base, ellipse: this.light.shape === 'ellipse' }
  }
}

const installed = new WeakSet<WebGPURenderer>()

/** Maps the sized lights to their nodes for renderer; once a renderer (render/gpu.ts). */
export function installLightModel(renderer: WebGPURenderer) {
  if (installed.has(renderer)) return
  installed.add(renderer)
  const library = renderer.library as unknown as { addLight(node: unknown, light: unknown): void }
  library.addLight(SphereLightNode, SceneSphereLight)
  library.addLight(SizedSpotLightNode, SceneSpotLight)
  library.addLight(SunDiscLightNode, SceneSunLight)
  library.addLight(AreaLightNode, SceneAreaLight)
  // three's area light tables, once for every renderer.
  RectAreaLightNode.setLTC(RectAreaLightTexturesLib.init())
  installDirect()
  installAreaDirect()
}
