import { Box3, Matrix4, Quaternion, Vector3 } from 'three'
import type { Scene, SceneObject } from '../api.ts'
import { primitiveGeometry } from '../builtins/geometry.ts'
import { cameraDefaults } from '../state/apply.ts'
import type { SceneModel } from '../models/cache.ts'
import { docToView, positionToView, type Quat, type Vec3 } from '../space.ts'

// Where render_view's camera stands. Everything here is in view space (Z up),
// the space the scene is drawn in: a scene camera keeps its own lens and
// picture shape, a viewpoint and the presets use a 35 mm-equivalent lens at
// 4:3. Presets frame the world bounds of every visible object; the ground,
// lights and cameras do not count.

export type Preset = 'overview' | 'top' | 'front' | 'side'
export type RenderRequest = {
  camera_object_id?: string
  viewpoint?: { position: number[]; target: number[]; focal_mm: number }
  preset?: Preset
}

/** A perspective camera in view space and the picture it takes. `fov` is vertical, in degrees. */
export type Framing = { position: Vec3; quaternion: Quat; fov: number; near: number; far: number; width: number; height: number }

export const longSide = 1024
const sensorHeightMM = 24
const presetFocalMM = 35
const presetAspect = 4 / 3
/** The framed bounds take up at most 1 / 1.1 of the picture. */
const margin = 1.1
const overviewAzimuth = (45 * Math.PI) / 180
const overviewElevation = (30 * Math.PI) / 180
const near = 0.05
const far = 5000

const Z = new Vector3(0, 0, 1)
const presets: Record<Preset, { toward: Vector3; up: Vector3 }> = {
  overview: {
    toward: new Vector3(Math.sin(overviewAzimuth) * Math.cos(overviewElevation), -Math.cos(overviewAzimuth) * Math.cos(overviewElevation), Math.sin(overviewElevation)),
    up: Z,
  },
  top: { toward: new Vector3(0, 0, 1), up: new Vector3(0, 1, 0) },
  // The front faces -Y in view space; the side view looks from +X, as Blender's right view.
  front: { toward: new Vector3(0, -1, 0), up: Z },
  side: { toward: new Vector3(1, 0, 0), up: Z },
}

const docToViewMatrix = new Matrix4().makeRotationFromQuaternion(docToView)
const modelFallback = { min: [-0.5, 0, -0.5], max: [0.5, 1, 0.5] }
const emptyBounds = new Box3(new Vector3(-1, -1, 0), new Vector3(1, 1, 2))

const vec = (v: Vector3): Vec3 => [v.x, v.y, v.z]
const quat = (q: Quaternion): Quat => [q.x, q.y, q.z, q.w]
const fovFor = (focalMM: number, sensorMM = sensorHeightMM) => (2 * Math.atan(sensorMM / 2 / focalMM) * 180) / Math.PI

/** The camera for a render request, or why there is none. */
export function framingFor(scene: Scene, models: Readonly<Record<string, SceneModel>>, request: RenderRequest): Framing | { error: string } {
  if (request.camera_object_id) return cameraFraming(scene, request.camera_object_id)
  if (request.viewpoint) return viewpointFraming(request.viewpoint)
  if (request.preset && request.preset in presets) return presetFraming(scene, models, request.preset)
  return { error: '截图请求没有给出摄像机、视点或预设' }
}

/** The object's matrix in view space, as the viewport draws its content: the parent chain in document space, then the change of basis. */
function viewMatrix(objects: ReadonlyMap<string, SceneObject>, o: SceneObject): Matrix4 {
  const chain: SceneObject[] = []
  for (let at: SceneObject | undefined = o; at && chain.length <= objects.size; at = at.parent_id ? objects.get(at.parent_id) : undefined) chain.unshift(at)
  const m = docToViewMatrix.clone()
  for (const x of chain) {
    const t = x.transform
    m.multiply(new Matrix4().compose(new Vector3().fromArray(t.position), new Quaternion().fromArray(t.rotation), new Vector3().fromArray(t.scale)))
  }
  return m
}

function cameraFraming(scene: Scene, id: string): Framing | { error: string } {
  const objects = new Map(scene.objects.map((o) => [o.id, o]))
  const o = objects.get(id)
  if (!o || o.kind !== 'camera' || !o.camera) return { error: `摄像机 ${id} 已不在场景里` }
  const position = new Vector3()
  const rotation = new Quaternion()
  viewMatrix(objects, o).decompose(position, rotation, new Vector3())
  // The server stores every field; the defaults only satisfy the shared type.
  const camera = { ...cameraDefaults, ...o.camera }
  const [w, h] = camera.aspect.split(':').map(Number)
  const aspect = w > 0 && h > 0 ? w / h : 3 / 2
  // As the frustum the editor draws: vertical angle from the sensor height.
  return {
    position: vec(position),
    quaternion: quat(rotation.normalize()),
    fov: fovFor(camera.focal_mm, camera.sensor_mm[1]),
    near: camera.near,
    far: camera.far,
    ...pictureSize(aspect),
  }
}

function pictureSize(aspect: number): { width: number; height: number } {
  return aspect >= 1 ? { width: longSide, height: Math.round(longSide / aspect) } : { width: Math.round(longSide * aspect), height: longSide }
}

function lookFrom(position: Vector3, target: Vector3, up: Vector3): Quat {
  const forward = target.clone().sub(position).normalize()
  const safeUp = Math.abs(forward.dot(up)) > 0.999 ? new Vector3(0, 1, 0) : up
  return quat(new Quaternion().setFromRotationMatrix(new Matrix4().lookAt(position, target, safeUp)))
}

function viewpointFraming(v: NonNullable<RenderRequest['viewpoint']>): Framing {
  const position = new Vector3().fromArray(positionToView(v.position))
  const target = new Vector3().fromArray(positionToView(v.target))
  return { position: vec(position), quaternion: lookFrom(position, target, Z), fov: fovFor(v.focal_mm), near, far, ...pictureSize(presetAspect) }
}

/** World bounds of what the scene draws, in view space; the ground, groups, lights and cameras do not count. */
export function sceneBounds(scene: Scene, models: Readonly<Record<string, SceneModel>>): Box3 {
  const objects = new Map(scene.objects.map((o) => [o.id, o]))
  const bounds = new Box3()
  for (const o of scene.objects) {
    if (!o.visible) continue
    let local: Box3 | null = null
    if (o.kind === 'model_instance' && o.model?.content_revision_id) {
      const b = models[o.model.content_revision_id]?.manifest?.bounds ?? modelFallback
      local = new Box3(new Vector3().fromArray(b.min), new Vector3().fromArray(b.max))
    } else if (o.kind === 'primitive' && o.primitive && o.primitive.builtin !== 'ground@1') {
      const geometry = primitiveGeometry(o.primitive.builtin, o.primitive.params)
      if (geometry) {
        geometry.computeBoundingBox()
        local = geometry.boundingBox
        geometry.dispose()
      }
    }
    if (local && !local.isEmpty()) bounds.union(local.clone().applyMatrix4(viewMatrix(objects, o)))
  }
  return bounds.isEmpty() ? emptyBounds.clone() : bounds
}

function presetFraming(scene: Scene, models: Readonly<Record<string, SceneModel>>, preset: Preset): Framing {
  const { toward, up } = presets[preset]
  const bounds = sceneBounds(scene, models)
  const center = bounds.getCenter(new Vector3())
  const fov = fovFor(presetFocalMM)
  const tanV = Math.tan((fov * Math.PI) / 360)
  const tanH = tanV * presetAspect
  const forward = toward.clone().negate()
  const right = forward.clone().cross(up).normalize()
  const camUp = right.clone().cross(forward)
  // Back off along the view direction until every corner fits, with the margin.
  let distance = 0
  const { min, max } = bounds
  for (const x of [min.x, max.x])
    for (const y of [min.y, max.y])
      for (const z of [min.z, max.z]) {
        const q = new Vector3(x, y, z).sub(center)
        const depth = q.dot(toward)
        distance = Math.max(distance, depth + (Math.abs(q.dot(right)) * margin) / tanH, depth + (Math.abs(q.dot(camUp)) * margin) / tanV)
      }
  distance = Math.max(distance, 1)
  const position = center.clone().addScaledVector(toward, distance)
  const size = bounds.getSize(new Vector3()).length()
  return { position: vec(position), quaternion: lookFrom(position, center, up), fov, near, far: Math.max(far, distance + size * 2), ...pictureSize(presetAspect) }
}
