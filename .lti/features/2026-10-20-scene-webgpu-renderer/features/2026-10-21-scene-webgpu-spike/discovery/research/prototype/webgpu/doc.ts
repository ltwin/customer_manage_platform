import { Matrix4, PerspectiveCamera, Quaternion, Vector3 } from 'three'
import type { Scene, SceneObject } from '../../src/creative-scene/api.ts'
import { aspectOf, verticalFov } from '../../src/creative-scene/cameraOptics.ts'
import { docToView } from '../../src/creative-scene/space.ts'
import { worldOf } from '../../src/creative-scene/state/world.ts'

// R0 spike: the reference scenes (scripts/fixtures/scene-reference) and what
// the prototypes need from them, without the editor. Document space is Y up;
// everything drawn sits under one docToView turn, as in the editor.

const files = import.meta.glob<Scene>('../../scripts/fixtures/scene-reference/*.json', { eager: true, import: 'default' })

export const sceneNames = Object.keys(files).map((p) => p.split('/').at(-1)!.replace('.json', ''))

export function referenceScene(name: string): Scene {
  const doc = files[`../../scripts/fixtures/scene-reference/${name}.json`]
  if (!doc) throw new Error(`no reference scene ${name}`)
  return doc
}

export const docToViewMatrix = new Matrix4().makeRotationFromQuaternion(docToView)

export function cameras(doc: Scene): SceneObject[] {
  return doc.objects.filter((o) => o.kind === 'camera')
}

/** A three camera through a scene camera, in view space, for a picture of the given aspect. */
export function shotCamera(doc: Scene, cameraID: string | null, aspect: number): { camera: PerspectiveCamera; object: SceneObject } {
  const all = cameras(doc)
  const object = all.find((o) => o.id === cameraID) ?? all[0]
  if (!object?.camera) throw new Error('the scene has no camera')
  const c = object.camera
  const world = docToViewMatrix.clone().multiply(worldOf(doc.objects)(object.id))
  const position = new Vector3()
  const quaternion = new Quaternion()
  world.decompose(position, quaternion, new Vector3())
  const fov = verticalFov(c.focal_mm ?? 35, c.sensor_mm ?? [36, 24], aspectOf(c.aspect ?? '3:2'))
  const camera = new PerspectiveCamera(fov, aspect, c.near ?? 0.1, c.far ?? 1000)
  camera.position.copy(position)
  camera.quaternion.copy(quaternion)
  camera.updateMatrixWorld()
  return { camera, object }
}

/** The sun's direction in document space, as render/sky.ts sunDirection (not imported: it loads the WebGL Sky). */
export function sunDirection(doc: Scene): Vector3 {
  const sky = doc.environment.sky
  const el = ((sky.sun_elevation_deg ?? 35) * Math.PI) / 180
  const az = ((sky.sun_azimuth_deg ?? 135) * Math.PI) / 180
  return new Vector3(Math.sin(az) * Math.cos(el), Math.sin(el), Math.cos(az) * Math.cos(el))
}
