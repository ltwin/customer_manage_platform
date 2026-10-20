import { Box3, Matrix4, type DirectionalLight, type Mesh, type Object3D, type OrthographicCamera } from 'three'

// The sun's shadow map covers what casts a shadow and nothing more. A fixed
// box spends its texels on empty ground and leaves big scenes unshadowed past
// its edge; fitted in the light's own frame, every texel lands on a caster
// and the shadows they throw, however long. The fit runs inside three's own
// shadow update, so the viewport and an offscreen shot both get it.

/** How far behind the last caster, along the light, a receiver still takes its shadow (metres). */
const reach = 1000
/** Receivers look up their shadow this many texels out along their normal: no self-shadowing stripes. */
const normalBiasTexels = 1.5

/**
 * Fits the light's shadow camera to the scene's shadow casters on every
 * shadow update; returns the undo.
 */
export function fitSunShadow(light: DirectionalLight): () => void {
  const shadow = light.shadow
  const update = shadow.updateMatrices
  shadow.updateMatrices = function (this: typeof shadow, l) {
    update.call(this, l)
    // The projection changed under the matrices just built: build them again.
    if (fitCasters(this.camera, rootOf(l), this.mapSize.x)) {
      this.normalBias = texelSize(this.camera, this.mapSize.x) * normalBiasTexels
      update.call(this, l)
    }
  }
  return () => {
    shadow.updateMatrices = update
  }
}

const box = new Box3()
const part = new Box3()
const toLight = new Matrix4()

/**
 * Sets an orthographic shadow camera's bounds around the visible shadow
 * casters under root, as its current view sees them; false, leaving the
 * camera as it was, when nothing casts.
 */
export function fitCasters(camera: OrthographicCamera, root: Object3D, mapSize: number): boolean {
  box.makeEmpty()
  root.traverseVisible((o) => {
    if (!o.castShadow || !(o as Mesh).isMesh) return
    const geometry = (o as Mesh).geometry
    if (!geometry.boundingBox) geometry.computeBoundingBox()
    toLight.multiplyMatrices(camera.matrixWorldInverse, o.matrixWorld)
    box.union(part.copy(geometry.boundingBox!).applyMatrix4(toLight))
  })
  if (box.isEmpty()) return false
  // A texel of margin keeps the outermost edges off the map's border.
  const pad = Math.max(box.max.x - box.min.x, box.max.y - box.min.y, 0.01) / mapSize
  camera.left = box.min.x - pad
  camera.right = box.max.x + pad
  camera.bottom = box.min.y - pad
  camera.top = box.max.y + pad
  // The camera looks down -Z: the nearest caster has the largest z.
  camera.near = -box.max.z - pad
  camera.far = -box.min.z + reach
  camera.updateProjectionMatrix()
  return true
}

function texelSize(camera: OrthographicCamera, mapSize: number): number {
  return Math.max(camera.right - camera.left, camera.top - camera.bottom) / mapSize
}

function rootOf(o: Object3D): Object3D {
  let root = o
  while (root.parent) root = root.parent
  return root
}
