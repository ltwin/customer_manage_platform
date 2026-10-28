import { Object3D, type Light as ThreeLight } from 'three'
import type { SceneObject } from '../api.ts'
import { lightDefaults, lightDefaultsOf, lightHasSize, lightSize } from '../state/apply.ts'
import { SceneAreaLight, SceneSphereLight, SceneSpotLight, SceneSunLight, type SizedLight } from '../render/lightModel/lights.ts'
import { attachSoftShadow } from '../render/lightModel/shadowFilter.ts'
import { cascadeSunShadow } from '../render/sunShadow.ts'
import { volumeShadowNode } from '../render/volumes/shadow.ts'

// Light objects in the lit look (design「最终渲染的光照」): three.js lights
// with the document's fields as they are (the fields are three.js's), built
// in the object's document-space group. Directional and spot lights shine
// along the object's local −Z: their target is a child at (0, 0, −1), so it
// turns with the object. The data's intensity is kept in
// userData.baseIntensity; each picture draws it × the exposure's k
// (world/exposure.ts applyExposure), 1 under any sky but the physical one.
// A spot's and a point's shadow nodes take the volumes' shadows (W5,
// render/volumes/shadow.ts); a directional light's cascades do too.
// Lights have a size (data: 光源尺寸): a point or spot light's radius_m, a
// directional light's angular_diameter_deg, as the light's size
// (render/lightModel/lights.ts). A light is built for its type and whether
// it has a size (lightKey): three keeps the light node and shadow node a
// light first builds, and a size moving off 0 or back takes other ones.
// An area light (data: 面光源) always has one, its width and height; it is
// built for its shape, which its light node fixes (render/lightModel/nodes.ts).

type Light = NonNullable<SceneObject['light']>

/** Shadow maps of every light; a directional one's, one per cascade like the sun's. */
const shadowMapSize = 1024

/** stop undoes the light's shadow node: a directional light's cascades once it casts, a spot's or point's from the start. */
export type BuiltLight = { light: SizedLight | SceneAreaLight; target: Object3D | null; stop: (() => void) | null }

/** What a light is built for: its type and whether it has a size, an area light's shape; a change in any builds it anew. */
export const lightKey = (l: Light): string => (l.type === 'area' ? `area+${l.shape ?? lightDefaults.shape}` : `${l.type}${lightHasSize(l) ? '+size' : ''}`)

export function buildLight(type: Light['type']): BuiltLight {
  // Before the light first builds: three keeps the shadow node it builds first.
  if (type === 'area') {
    // Its shadow a point light's cube from its centre, the volumes' too.
    const light = new SceneAreaLight()
    return { light, target: null, stop: volumeShadowNode(light) }
  }
  if (type === 'point') {
    const light = new SceneSphereLight()
    return { light, target: null, stop: volumeShadowNode(light) }
  }
  const target = new Object3D()
  target.position.set(0, 0, -1)
  const light = type === 'spot' ? new SceneSpotLight() : new SceneSunLight()
  light.target = target
  // three puts a directional light at (0, 1, 0); here it sits at the object's origin.
  light.position.set(0, 0, 0)
  return { light, target, stop: light instanceof SceneSpotLight ? volumeShadowNode(light) : null }
}

/** Writes the document's fields onto a light built for the same type; final, an export's (an area light's shadow samples). */
export function applyLight(built: BuiltLight, l: Light, final = false) {
  const { light } = built
  const v = { ...lightDefaultsOf(l.type), ...l }
  light.color.set(v.color)
  light.userData.baseIntensity = v.intensity
  light.intensity = v.intensity
  if (light instanceof SceneAreaLight) {
    light.shape = v.shape
    light.width = v.width
    light.height = v.height
  } else light.size = lightSize(l)
  if (light instanceof SceneSphereLight || light instanceof SceneSpotLight) {
    light.distance = v.distance
    light.decay = v.decay
  }
  if (light instanceof SceneSpotLight) {
    light.angle = v.angle
    light.penumbra = v.penumbra
  }
  const shadows = v.cast_shadow
  if (light.castShadow !== shadows) {
    light.castShadow = shadows
    if (shadows) light.shadow.mapSize.set(shadowMapSize, shadowMapSize)
  }
  // A sized light's soft shadow, before the cascades below clone its shadow (render/lightModel/shadowFilter.ts).
  if (shadows) attachSoftShadow(light, final)
  // Once, and kept: three's light node holds on to the first shadow node it builds.
  if (shadows && light instanceof SceneSunLight && !built.stop) built.stop = cascadeSunShadow(light)
}

export function disposeLight(built: BuiltLight) {
  built.stop?.()
  ;(built.light as ThreeLight).dispose()
}
