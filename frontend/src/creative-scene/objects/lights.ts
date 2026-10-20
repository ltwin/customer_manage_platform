import { DirectionalLight, Object3D, PointLight, SpotLight, type Light as ThreeLight } from 'three'
import type { SceneObject } from '../api.ts'
import { lightDefaults } from '../state/apply.ts'
import { fitSunShadow } from '../render/sunShadow.ts'

// Light objects in the lit look (design「最终渲染的光照」): three.js lights
// with the document's fields as they are (the fields are three.js's), built
// in the object's document-space group. Directional and spot lights shine
// along the object's local −Z: their target is a child at (0, 0, −1), so it
// turns with the object.

type Light = NonNullable<SceneObject['light']>

/** Shadow maps of point and spot lights; directional ones fit like the sun. */
const shadowMapSize = 1024

export type BuiltLight = { light: DirectionalLight | PointLight | SpotLight; target: Object3D | null; stop: (() => void) | null }

export function buildLight(type: Light['type']): BuiltLight {
  if (type === 'point') return { light: new PointLight(), target: null, stop: null }
  const target = new Object3D()
  target.position.set(0, 0, -1)
  const light = type === 'spot' ? new SpotLight() : new DirectionalLight()
  light.target = target
  // three puts a directional light at (0, 1, 0); here it sits at the object's origin.
  light.position.set(0, 0, 0)
  return { light, target, stop: null }
}

/** Writes the document's fields onto a light built for the same type. */
export function applyLight(built: BuiltLight, l: Light) {
  const { light } = built
  const v = { ...lightDefaults, ...l }
  light.color.set(v.color)
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
    if (shadows && !(light instanceof DirectionalLight)) light.shadow.mapSize.set(shadowMapSize, shadowMapSize)
  }
  built.stop?.()
  built.stop = shadows && light instanceof DirectionalLight ? fitSunShadow(light) : null
}

export function disposeLight(built: BuiltLight) {
  built.stop?.()
  ;(built.light as ThreeLight).dispose()
}
