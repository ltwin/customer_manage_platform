import { DirectionalLight, Object3D, PointLight, SpotLight, type Light as ThreeLight } from 'three'
import type { SceneObject } from '../api.ts'
import { lightDefaults } from '../state/apply.ts'
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

type Light = NonNullable<SceneObject['light']>

/** Shadow maps of every light; a directional one's, one per cascade like the sun's. */
const shadowMapSize = 1024

/** stop undoes the light's shadow node: a directional light's cascades once it casts, a spot's or point's from the start. */
export type BuiltLight = { light: DirectionalLight | PointLight | SpotLight; target: Object3D | null; stop: (() => void) | null }

export function buildLight(type: Light['type']): BuiltLight {
  // Before the light first builds: three keeps the shadow node it builds first.
  if (type === 'point') {
    const light = new PointLight()
    return { light, target: null, stop: volumeShadowNode(light) }
  }
  const target = new Object3D()
  target.position.set(0, 0, -1)
  const light = type === 'spot' ? new SpotLight() : new DirectionalLight()
  light.target = target
  // three puts a directional light at (0, 1, 0); here it sits at the object's origin.
  light.position.set(0, 0, 0)
  return { light, target, stop: light instanceof SpotLight ? volumeShadowNode(light) : null }
}

/** Writes the document's fields onto a light built for the same type. */
export function applyLight(built: BuiltLight, l: Light) {
  const { light } = built
  const v = { ...lightDefaults, ...l }
  light.color.set(v.color)
  light.userData.baseIntensity = v.intensity
  light.intensity = v.intensity
  if (light instanceof PointLight || light instanceof SpotLight) {
    light.distance = v.distance
    light.decay = v.decay
  }
  if (light instanceof SpotLight) {
    light.angle = v.angle
    light.penumbra = v.penumbra
  }
  const shadows = v.cast_shadow
  if (light.castShadow !== shadows) {
    light.castShadow = shadows
    if (shadows) light.shadow.mapSize.set(shadowMapSize, shadowMapSize)
  }
  // Once, and kept: three's light node holds on to the first shadow node it builds.
  if (shadows && light instanceof DirectionalLight && !built.stop) built.stop = cascadeSunShadow(light)
}

export function disposeLight(built: BuiltLight) {
  built.stop?.()
  ;(built.light as ThreeLight).dispose()
}
