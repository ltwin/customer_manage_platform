import { Box3, Matrix4, Vector3, type Camera, type DirectionalLight, type DirectionalLightShadow, type Mesh, type Object3D, type OrthographicCamera, type PerspectiveCamera, type Vector2 } from 'three'
import type { NodeFrame } from 'three/webgpu'
import { CSMShadowNode } from 'three/addons/csm/CSMShadowNode.js'

// The sun's shadow in cascades. One map over every caster spent its texels
// on whatever stood far off: a big box 200 m away made the shadows by the
// camera jagged, and the normal bias, a texel and a half, lifted them off
// their casters. The view is cut by depth into slices, a map each; each map
// covers what in its slice can be shadowed (the slice, cut to the casters
// that throw into it) and nothing more, so near shadows keep their texels
// whatever stands far off. three's CSMShadowNode draws and samples the maps;
// where they go is worked out here, for whichever camera draws (the
// viewport's or an offscreen shot's), in world space (a document light sits
// in its object's group).

const cascades = 4
/** Where the log split of the view starts (metres): 0.5–5, 5–50, 50–500 m and on, under the viewport's 5000 m far. */
const splitFrom = 0.5
/** Receivers look up their shadow this many texels out along their normal: no self-shadowing stripes. */
const normalBiasTexels = 1.5
/** Room kept toward the light past the highest caster and behind the slice (metres). */
const margin = 1
/** A map's size moves in steps of 2^(1/4): the camera moving within a step leaves the texels where they are. */
const sizeSteps = 4

/** A cascade's light, as CSMShadowNode makes them: an object with a target and its own shadow. */
export type CascadeLight = Object3D & { target: Object3D; shadow?: DirectionalLightShadow }

class SunCascades extends CSMShadowNode {
  /** Each cascade's slice as a share of near..far, which the shader picks the map by. */
  declare _cascades: Vector2[]

  constructor(light: DirectionalLight) {
    super(light, { cascades })
  }

  // Replaces three's placement, which takes the camera of the first build and the light's local position.
  override updateBefore(frame: NodeFrame): undefined {
    if (!frame.camera) return
    this.camera = frame.camera
    fitCascades(frame.camera, this.light as DirectionalLight, this.lights, this._cascades)
  }
}

/** Draws a directional light's shadow in cascades; returns the undo. */
export function cascadeSunShadow(light: DirectionalLight): () => void {
  const shadow = light.shadow as DirectionalLightShadow & { shadowNode?: SunCascades }
  const node = new SunCascades(light)
  shadow.shadowNode = node
  return () => {
    if (shadow.shadowNode === node) delete shadow.shadowNode
    node.dispose()
  }
}

const toLight = new Matrix4()
const fromLight = new Matrix4()
const toCaster = new Matrix4()
const origin = new Vector3()
const lightAt = new Vector3()
const targetAt = new Vector3()
const direction = new Vector3()
const slice = new Box3()
const point = new Vector3()
const nears = Array.from({ length: 4 }, () => new Vector3())
const fars = Array.from({ length: 4 }, () => new Vector3())
const corners = [
  [1, 1],
  [1, -1],
  [-1, -1],
  [-1, 1],
] as const
const casters: Box3[] = []

/**
 * Places each cascade's light and sizes its map for the camera: the view
 * split by depth, log from splitFrom, its share of near..far into ranges;
 * each map square over the casters that throw into its slice, clipped to
 * the slice, snapped to its texels; its depth from past the highest of them
 * to behind the slice.
 */
export function fitCascades(camera: Camera, sun: DirectionalLight, lights: readonly CascadeLight[], ranges: readonly Vector2[]) {
  const n = lights.length
  if (n === 0) return
  sun.getWorldPosition(lightAt)
  sun.target.getWorldPosition(targetAt)
  direction.subVectors(targetAt, lightAt).normalize()
  // The light's frame as three's lookAt turns the shadow camera: a texel here is a texel there. +z toward the light.
  fromLight.lookAt(origin, direction, lights[0].shadow!.camera.up)
  toLight.copy(fromLight).invert()

  let count = 0
  rootOf(sun).traverseVisible((o) => {
    if (!o.castShadow || !(o as Mesh).isMesh) return
    const geometry = (o as Mesh).geometry
    if (!geometry.boundingBox) geometry.computeBoundingBox()
    toCaster.multiplyMatrices(toLight, o.matrixWorld)
    ;(casters[count++] ??= new Box3()).copy(geometry.boundingBox!).applyMatrix4(toCaster)
  })

  // Two points on each corner's ray, in view space: a point at a view depth is on the line through them, perspective or orthographic.
  for (let j = 0; j < 4; j++) {
    nears[j].set(corners[j][0], corners[j][1], 0.25).applyMatrix4(camera.projectionMatrixInverse)
    fars[j].set(corners[j][0], corners[j][1], 0.75).applyMatrix4(camera.projectionMatrixInverse)
  }
  const { near, far } = camera as PerspectiveCamera | OrthographicCamera
  const from = Math.max(near, splitFrom)
  const depth = (i: number) => (i === 0 ? near : i === n ? far : Math.min(from * (far / from) ** (i / n), far))

  for (let i = 0; i < n; i++) {
    const d0 = depth(i)
    const d1 = depth(i + 1)
    ranges[i].set((d0 - near) / (far - near), (d1 - near) / (far - near))
    slice.makeEmpty()
    for (let j = 0; j < 4; j++) {
      for (const d of [d0, d1]) {
        const a = nears[j]
        const b = fars[j]
        point.subVectors(b, a).multiplyScalar((-d - a.z) / (b.z - a.z)).add(a)
        slice.expandByPoint(point.applyMatrix4(camera.matrixWorld).applyMatrix4(toLight))
      }
    }
    // What can be shadowed in the slice lies under a caster, seen along the light, and below its top.
    let minX = Infinity
    let minY = Infinity
    let maxX = -Infinity
    let maxY = -Infinity
    let highest = slice.max.z
    for (let k = 0; k < count; k++) {
      const c = casters[k]
      if (c.max.z < slice.min.z || c.max.x < slice.min.x || c.min.x > slice.max.x || c.max.y < slice.min.y || c.min.y > slice.max.y) continue
      minX = Math.min(minX, c.min.x)
      minY = Math.min(minY, c.min.y)
      maxX = Math.max(maxX, c.max.x)
      maxY = Math.max(maxY, c.max.y)
      highest = Math.max(highest, c.max.z)
    }
    minX = Math.max(minX, slice.min.x)
    minY = Math.max(minY, slice.min.y)
    maxX = Math.min(maxX, slice.max.x)
    maxY = Math.min(maxY, slice.max.y)
    if (!(minX <= maxX && minY <= maxY)) {
      // Nothing throws into it: the slice, lit throughout.
      ;({ x: minX, y: minY } = slice.min)
      ;({ x: maxX, y: maxY } = slice.max)
    }

    const shadow = lights[i].shadow!
    const mapSize = shadow.mapSize.x
    const size = 2 ** (Math.ceil(Math.log2(Math.max(maxX - minX, maxY - minY, 0.01)) * sizeSteps) / sizeSteps)
    // Two texels of room a side: the centre snaps by up to half of one.
    const half = (size / 2) * (1 + 4 / mapSize)
    const texel = (2 * half) / mapSize
    const lift = highest + margin
    point.set(Math.round((minX + maxX) / 2 / texel) * texel, Math.round((minY + maxY) / 2 / texel) * texel, lift).applyMatrix4(fromLight)

    const light = lights[i]
    light.position.copy(point)
    light.target.position.copy(point).add(direction)
    light.updateMatrixWorld()
    light.target.updateMatrixWorld()
    const cam = shadow.camera
    cam.left = -half
    cam.right = half
    cam.bottom = -half
    cam.top = half
    cam.near = 0
    cam.far = lift - slice.min.z + margin
    cam.updateProjectionMatrix()
    shadow.normalBias = texel * normalBiasTexels
  }
}

function rootOf(o: Object3D): Object3D {
  let root = o
  while (root.parent) root = root.parent
  return root
}
