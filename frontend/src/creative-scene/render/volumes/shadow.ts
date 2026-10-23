import { Vector3, type DirectionalLight, type Light, type LightShadow, type PointLight, type SpotLight } from 'three'
import * as tsl from 'three/tsl'
import { PointShadowNode, ShadowNode, type NodeBuilder } from 'three/webgpu'
import type { TSLNode } from '../../materials/patterns.ts'
import { chordThrough } from './medium.ts'
import { V } from './state.ts'

// Volumes' shadows on surfaces (W5 volume.cast_shadow; scene-volumes design
// 「表面阴影系数」): a light's shadow at a surface times what the casting
// volumes between the surface and the light let through, each its σt over
// its chord toward the light. The casting volumes are a uniform list
// (V.castList, V.castCount): switching one on or adding one compiles
// nothing, and with none the loop runs no times. three takes the first
// shadow node a light builds, so the nodes go on when the light is made.

type Op = (...args: TSLNode[]) => TSLNode
const { exp, float, length, normalize, uniform, vec3 } = tsl as unknown as Record<string, Op>
const { positionWorld } = tsl as unknown as Record<string, TSLNode>
const Fn = tsl.Fn as unknown as (fn: () => TSLNode) => () => TSLNode
const Loop = tsl.Loop as unknown as (count: TSLNode | number, body: (i: { i: TSLNode }) => void) => void
type Uniform<T> = TSLNode & { value: T }

/** What the casting volumes let through from world p toward a light along toward, as far as maxT. */
export function volumeShadow(p: TSLNode, toward: TSLNode, maxT: TSLNode): TSLNode {
  return Fn(() => {
    const tau = vec3(0).toVar()
    Loop(V.castCount, ({ i: k }) => {
      const i = V.castList.element(k)
      tau.addAssign(V.ext.element(i).xyz.mul(chordThrough(V, i, p, toward, maxT)))
    })
    return exp(tau.negate())
  })()
}

/** A shadow value times the volumes' between the surface and a light at world position at (spot, point). */
function fromPosition(shadow: TSLNode, at: Uniform<Vector3>): TSLNode {
  const p = positionWorld
  const to = at.sub(p)
  return vec3(shadow).mul(volumeShadow(p, normalize(to), length(to)))
}

/** A shadow value times the volumes' toward a directional light (the way to it, a unit vector). */
export function towardSun(shadow: TSLNode, toward: TSLNode): TSLNode {
  return vec3(shadow).mul(volumeShadow(positionWorld, toward, float(1e4)))
}

/** A light's world position, read as each render starts. */
function positionOf(light: Light): Uniform<Vector3> {
  const at = new Vector3()
  return uniform(at).onRenderUpdate(() => light.getWorldPosition(at))
}

/** A spot's shadow with the volumes' (objects/lights.ts buildLight). */
export class VolumeShadowNode extends ShadowNode {
  private readonly at: Uniform<Vector3>

  constructor(light: SpotLight) {
    super(light, light.shadow)
    this.at = positionOf(light)
  }

  setup(builder: NodeBuilder) {
    const shadow = super.setup(builder)
    return shadow && fromPosition(shadow, this.at)
  }
}

/** A point light's shadow with the volumes'. */
export class VolumePointShadowNode extends PointShadowNode {
  private readonly at: Uniform<Vector3>

  constructor(light: PointLight) {
    super(light, light.shadow)
    this.at = positionOf(light)
  }

  setup(builder: NodeBuilder) {
    const shadow = super.setup(builder)
    return shadow && fromPosition(shadow, this.at)
  }
}

type Shadowed = { shadow: LightShadow & { shadowNode?: unknown } }

/** Gives a spot or point light its shadow node with the volumes'; returns the undo. */
export function volumeShadowNode(light: SpotLight | PointLight): () => void {
  const shadow = (light as unknown as Shadowed).shadow
  const node = (light as PointLight).isPointLight ? new VolumePointShadowNode(light as PointLight) : new VolumeShadowNode(light as SpotLight)
  shadow.shadowNode = node
  return () => {
    if (shadow.shadowNode === node) delete shadow.shadowNode
    node.dispose()
  }
}

/** The shadow node a copy of light needs to build the shader light builds: the same kind (sceneRenderer.ts standIn). */
export function shadowLike(light: Light, copy: Light, sun: (l: DirectionalLight) => () => void): (() => void) | null {
  const node = (light as unknown as Shadowed).shadow?.shadowNode
  if (!node) return null
  if ((copy as DirectionalLight).isDirectionalLight) return sun(copy as DirectionalLight)
  if (node instanceof VolumeShadowNode || node instanceof VolumePointShadowNode) return volumeShadowNode(copy as SpotLight | PointLight)
  return null
}
