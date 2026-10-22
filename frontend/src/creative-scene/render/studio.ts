import { MathUtils, Vector3, type Camera, type DirectionalLight, type Object3D } from 'three'

// The clay look's studio light (W1 scene-display-look, design「工作室光」), as
// Blender's Solid shading has it by default: the lights turn with the view
// (World Space Lighting off), so every camera and the orthographic view are
// lit from the front, and nothing casts shadows. The scene's sky, sun,
// ambient, fog and lights do not count. The intensities are set under AgX.

/** The clay background, chosen by how it shows: it is tone mapped with the picture. */
export const STUDIO_BACKGROUND = '#8a8f98'

/** The key light comes from the view's upper left: this far to the left of the view axis, and up. */
const KEY_LEFT_DEG = 40
const KEY_UP_DEG = 40
/** How far from its target the key light stands; only its direction lights. */
const KEY_DISTANCE = 60

export const STUDIO_KEY_INTENSITY = 6
/** The fill from above (white) and below (warm grey); up is the vertical, not the view's. */
export const STUDIO_FILL = { sky: '#ffffff', ground: '#6b6760', intensity: 0.6 }
/** So the sides the key light misses do not go black. */
export const STUDIO_AMBIENT = 0.7

/** The key light's direction in view space (x right, y up, the camera looking down −z), towards the light. */
const keyInView = new Vector3(
  -Math.cos(MathUtils.degToRad(KEY_UP_DEG)) * Math.sin(MathUtils.degToRad(KEY_LEFT_DEG)),
  Math.sin(MathUtils.degToRad(KEY_UP_DEG)),
  Math.cos(MathUtils.degToRad(KEY_UP_DEG)) * Math.cos(MathUtils.degToRad(KEY_LEFT_DEG)),
)
const direction = /*@__PURE__*/ new Vector3()

/**
 * Turns the studio's key light (userData.studio) to come from the upper left
 * of camera. A scene without one, in any look but clay, is left as it is.
 */
export function aimStudio(scene: Object3D, camera: Camera) {
  let key: DirectionalLight | undefined
  scene.traverse((o) => {
    if (o.userData.studio === true && (o as DirectionalLight).isDirectionalLight) key = o as DirectionalLight
  })
  if (!key) return
  direction.copy(keyInView).applyQuaternion(camera.quaternion)
  key.position.copy(key.target.position).addScaledVector(direction, KEY_DISTANCE)
}
