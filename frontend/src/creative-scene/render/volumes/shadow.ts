import { Vector3, type DirectionalLight, type Light, type LightShadow, type Object3D, type PointLight, type RectAreaLight, type SpotLight } from 'three'
import * as tsl from 'three/tsl'
import { PointShadowNode, ShadowNode, type NodeBuilder } from 'three/webgpu'
import type { TSLNode } from '../../materials/patterns.ts'
import { gpuTestHooks } from '../gpu.ts'
import { chordThrough } from './medium.ts'
import { V } from './state.ts'

// Volumes' shadows on surfaces (W5 volume.cast_shadow; scene-volumes design
// 「表面阴影系数」): a light's shadow at a surface times what the casting
// volumes between the surface and the light let through, each its σt over
// its chord toward the light. The casting volumes are a uniform list
// (V.castList, V.castCount): more of them compile nothing. The materials
// carry it only while a volume casts (volumesCast): the first to cast
// compiles them again, once, and a scene without one compiles no more for a
// new casting light than before W5 (AC-W5-11, owner 2026-10-23). three takes
// the first shadow node a light builds, so the nodes go on when the light is
// made.

type Op = (...args: TSLNode[]) => TSLNode
const { exp, float, length, normalize, reference, tan, uniform, vec3 } = tsl as unknown as Record<string, Op>
const { normalWorld, positionWorld, renderGroup, shadowPositionWorld } = tsl as unknown as Record<string, TSLNode>
const Fn = tsl.Fn as unknown as (fn: () => TSLNode) => () => TSLNode
const Loop = tsl.Loop as unknown as (count: TSLNode | number, body: (i: { i: TSLNode }) => void) => void
type Uniform<T> = TSLNode & { value: T }

/** Whether the lights a material is built for include the mark that a volume casts (objects/SceneLights.tsx VolumesCastLight). */
export function volumesCast(builder: NodeBuilder): boolean {
  const lights = (builder as unknown as { lightsNode?: { getLights(): Object3D[] } | null }).lightsNode?.getLights() ?? []
  return lights.some((l) => l.userData.volumesCast === true)
}

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
  return uniform(at).setGroup(renderGroup).onRenderUpdate(() => light.getWorldPosition(at))
}

/**
 * How far a light of size 0 lifts its receivers along their normal before
 * looking up its map (scene-light-radius design「尺寸为 0 时的条纹」): 1.5
 * texels at the receiver, as the sun's cascades do (render/sunShadow.ts), so
 * a wall the light grazes has no self-shadowing stripes. The design's
 * fallback, 0.05 m, stays for the owner's comparison images
 * (gpuTestHooks.stripeFixed).
 */
const STRIPE_TEXELS = 1.5
const STRIPE_FIXED_M = 0.05

type Receiving = { context: { shadowPositionWorld?: TSLNode }; material: { receivedShadowPositionNode?: TSLNode | null } }
type Filtered = { filterNode?: unknown }

/**
 * three r186's ShadowBaseNode.setupShadowPosition, one line, with the lift
 * added; texelAt gives a map's texel in metres at d from the light. Two
 * conditions: renderer.highPrecision stays off (on, ShadowNode.setupShadow
 * goes by positionLocal and leaves shadowPositionWorld unread), and a sized
 * light's shadow, which filters (render/lightModel/shadowFilter.ts), is not
 * lifted: its PCSS compares against the receiver's plane instead.
 */
function lift(builder: NodeBuilder, at: Uniform<Vector3>, texelAt: (d: TSLNode) => TSLNode) {
  const { context, material } = builder as unknown as Receiving
  const from = material.receivedShadowPositionNode ?? context.shadowPositionWorld ?? positionWorld
  const by = gpuTestHooks.stripeFixed ? float(STRIPE_FIXED_M) : texelAt(length(at.sub(positionWorld))).mul(STRIPE_TEXELS)
  shadowPositionWorld.assign(from.add(normalWorld.mul(by)))
}

/** A number the shadow holds, read as each render starts. */
const held = (shadow: LightShadow, path: string, type = 'float') => reference(path, type, shadow as unknown as TSLNode).setGroup(renderGroup)

/** A spot's shadow with the volumes' (objects/lights.ts buildLight). */
export class VolumeShadowNode extends ShadowNode {
  private readonly at: Uniform<Vector3>

  constructor(light: SpotLight) {
    super(light, light.shadow)
    this.at = positionOf(light)
  }

  setup(builder: NodeBuilder) {
    const shadow = super.setup(builder)
    return shadow && volumesCast(builder) ? fromPosition(shadow, this.at) : shadow
  }

  // A texel of a perspective map at d: 2 d tan(fov/2) / mapSize.
  setupShadowPosition(builder: NodeBuilder) {
    const s = (this as unknown as { shadow: LightShadow & Filtered }).shadow
    if (s.filterNode) return super.setupShadowPosition(builder)
    lift(builder, this.at, (d) => d.mul(2).mul(tan(held(s, 'camera.fov').mul(Math.PI / 360))).div(held(s, 'mapSize', 'vec2').x))
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
    return shadow && volumesCast(builder) ? fromPosition(shadow, this.at) : shadow
  }

  // A texel of a cube face at d: the face spans 2 d.
  setupShadowPosition(builder: NodeBuilder) {
    const s = (this as unknown as { shadow: LightShadow & Filtered }).shadow
    if (s.filterNode) return super.setupShadowPosition(builder)
    lift(builder, this.at, (d) => d.mul(2).div(held(s, 'mapSize', 'vec2').x))
  }
}

type Shadowed = { shadow: LightShadow & { shadowNode?: unknown } }

/** Gives a spot, point or area light its shadow node with the volumes' (an area light's a point light's cube); returns the undo. */
export function volumeShadowNode(light: SpotLight | PointLight | RectAreaLight): () => void {
  const shadow = (light as unknown as Shadowed).shadow
  const cube = (light as PointLight).isPointLight || (light as RectAreaLight).isRectAreaLight
  const node = cube ? new VolumePointShadowNode(light as unknown as PointLight) : new VolumeShadowNode(light as SpotLight)
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
  if (node instanceof VolumeShadowNode || node instanceof VolumePointShadowNode) return volumeShadowNode(copy as SpotLight | PointLight | RectAreaLight)
  return null
}
