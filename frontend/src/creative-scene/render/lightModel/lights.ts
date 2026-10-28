import { DirectionalLight, PointLight, RectAreaLight, SpotLight, Vector2, Vector3, type Light, type LightShadow, type PerspectiveCamera } from 'three'

// Lights with a size (Epic scene-light-size design「领域模型与业务规则」):
// a point or spot light is a sphere `size` metres in radius, a directional
// light a disc `size` degrees across, as Blender's Radius and Angle. The
// document's lights, and the skies' sun and moon, are these classes; three's
// own (the clay's studio light, thumbnails, previews) are not, and three maps
// light nodes by exact class (render/lightModel/nodes.ts), so they draw as
// before.
//
// three's LightShadow.copy leaves out filterNode: a stand-in's lights
// (clone(false), sceneRenderer.ts) and the sun's cascades (shadow.clone(),
// CSMShadowNode) would lose the soft shadow's filter. Each light's shadow is
// a subclass whose copy() carries it and the light's size, and each light's
// copy() its size; constructors take no arguments, as clone() calls them.

/** A shadow that knows its light's size, which its filter reads (render/lightModel/shadowFilter.ts). */
export type SizedShadow = LightShadow & { filterNode?: unknown; lightSize: number; capTexel: number }

type ShadowClass = new () => LightShadow

/** A shadow class whose copy() keeps the filter and the light's size. */
function sized(Base: ShadowClass) {
  return class extends Base {
    lightSize = 0
    /** A sun cascade's: its coarsest cascade's texel, metres, which its penumbra's cap counts (render/sunShadow.ts fitCascades). */
    capTexel = 0
    declare filterNode?: unknown

    copy(source: LightShadow): this {
      super.copy(source)
      const s = source as SizedShadow
      if (s.filterNode) this.filterNode = s.filterNode
      this.lightSize = s.lightSize
      this.capTexel = s.capTexel
      return this
    }
  }
}

// three exports none of its shadow classes but LightShadow: each is the class of a light's own.
const SizedPointShadow = sized(new PointLight().shadow.constructor as ShadowClass)
const SizedSpotShadow = sized(new SpotLight().shadow.constructor as ShadowClass)
const SizedSunShadow = sized(new DirectionalLight().shadow.constructor as ShadowClass)

/** A point light of radius size metres (Blender's Point light). */
export class SceneSphereLight extends PointLight {
  /** The radius, metres; 0 a point. */
  size = 0
  declare shadow: PointLight['shadow'] & SizedShadow

  constructor() {
    super()
    this.shadow = new SizedPointShadow() as this['shadow']
  }

  copy(source: this, recursive?: boolean): this {
    super.copy(source, recursive)
    this.size = source.size
    return this
  }
}

/** A spot light of radius size metres (Blender's Spot light; its cone and blend stay angle and penumbra). */
export class SceneSpotLight extends SpotLight {
  /** The radius, metres; 0 a point. */
  size = 0
  declare shadow: SpotLight['shadow'] & SizedShadow

  constructor() {
    super()
    this.shadow = new SizedSpotShadow() as this['shadow']
  }

  copy(source: this, recursive?: boolean): this {
    super.copy(source, recursive)
    this.size = source.size
    return this
  }
}

/** A directional light, a disc size degrees across (Blender's Sun light's Angle). */
export class SceneSunLight extends DirectionalLight {
  /** The angular diameter, degrees; 0 parallel light. */
  size = 0
  declare shadow: DirectionalLight['shadow'] & SizedShadow

  constructor() {
    super()
    this.shadow = new SizedSunShadow() as this['shadow']
  }

  // three's DirectionalLight.copy takes no recursive flag.
  copy(source: this): this {
    super.copy(source)
    this.size = source.size
    return this
  }
}

export type SizedLight = SceneSphereLight | SceneSpotLight | SceneSunLight

/** A light's size: metres of radius, degrees across for a directional light; 0 for any light without one, an area light too (hasSize). */
export function sizeOf(light: Light): number {
  return light instanceof SceneSphereLight || light instanceof SceneSpotLight || light instanceof SceneSunLight ? light.size : 0
}

/**
 * An area light's shadow: a cube map from its centre, as a point light's
 * (Epic design U3), its near 0.05 m (three's 0.5 m would let a flag or a grid
 * in front of a softbox cast nothing; the volumes read the same near), and
 * the light's world axes and half sizes, which its filter's kernel reads
 * (render/lightModel/shadowFilter.ts).
 */
class SizedAreaShadow extends SizedPointShadow {
  /** The emitting face's world axes, unit; along them, half its width and height in metres (SceneAreaLight.updateMatrixWorld). */
  axisX = new Vector3(1, 0, 0)
  axisY = new Vector3(0, 1, 0)
  half = new Vector2()

  constructor() {
    super()
    ;(this.camera as PerspectiveCamera).near = 0.05
  }

  copy(source: LightShadow): this {
    super.copy(source)
    const s = source as SizedAreaShadow
    this.axisX.copy(s.axisX)
    this.axisY.copy(s.axisY)
    this.half.copy(s.half)
    return this
  }
}

export type AreaShape = 'rectangle' | 'ellipse'

/**
 * An area light (data: 面光源; Blender's Area light): a rectangle or an
 * ellipse width × height, as the document has them, in the object's local XY
 * plane, lit toward its −Z. Its face is scaled by its world transform, its
 * parents' included: three's RectAreaLightNode takes the rotation alone, so
 * the light keeps its face's world half axes (halfX, halfY) as its matrix
 * updates, and its node (render/lightModel/nodes.ts) and shadow read them.
 * A stand-in's copy (sceneRenderer.ts) works out its own. three's
 * RectAreaLight casts no shadow and has none: this one has a cube's.
 */
export class SceneAreaLight extends RectAreaLight {
  shape: AreaShape = 'rectangle'
  /** Half the face's world width and height, as vectors along its world axes. */
  readonly halfX = new Vector3()
  readonly halfY = new Vector3()
  declare shadow: InstanceType<typeof SizedAreaShadow> & SizedShadow

  constructor() {
    super()
    this.shadow = new SizedAreaShadow() as this['shadow']
  }

  updateMatrixWorld(force?: boolean) {
    super.updateMatrixWorld(force)
    this.updateHalves()
  }

  updateWorldMatrix(updateParents: boolean, updateChildren: boolean) {
    super.updateWorldMatrix(updateParents, updateChildren)
    this.updateHalves()
  }

  private updateHalves() {
    const e = this.matrixWorld.elements
    this.halfX.set(e[0], e[1], e[2]).multiplyScalar(this.width / 2)
    this.halfY.set(e[4], e[5], e[6]).multiplyScalar(this.height / 2)
    const s = this.shadow
    s.half.set(this.halfX.length(), this.halfY.length())
    s.axisX.copy(this.halfX).normalize()
    s.axisY.copy(this.halfY).normalize()
  }

  /** The emitting area, m²: the parallelogram's |2 halfX × 2 halfY| or its inscribed ellipse's (data: 面光源). */
  area(): number {
    const a = new Vector3().crossVectors(this.halfX, this.halfY).length()
    return this.shape === 'ellipse' ? Math.PI * a : 4 * a
  }

  // three's RectAreaLight.copy takes no recursive flag.
  copy(source: this): this {
    super.copy(source)
    this.shape = source.shape
    this.shadow = source.shadow.clone() as this['shadow']
    this.halfX.copy(source.halfX)
    this.halfY.copy(source.halfY)
    return this
  }
}

/** Whether a light has a size: an area light always, any other at a size above 0 (its soft shadow, its light node). */
export function hasSize(light: Light): boolean {
  return light instanceof SceneAreaLight || sizeOf(light) > 0
}
