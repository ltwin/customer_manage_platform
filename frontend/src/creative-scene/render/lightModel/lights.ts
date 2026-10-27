import { DirectionalLight, PointLight, SpotLight, type Light, type LightShadow } from 'three'

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

/** A light's size: metres of radius, degrees across for a directional light; 0 for any light without one. */
export function sizeOf(light: Light): number {
  return light instanceof SceneSphereLight || light instanceof SceneSpotLight || light instanceof SceneSunLight ? light.size : 0
}
