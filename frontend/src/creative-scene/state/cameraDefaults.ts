import type { SceneObject } from '../api.ts'

/** The camera fields the server stores for those left out. */
export const cameraDefaults: Required<NonNullable<SceneObject['camera']>> = {
  focal_mm: 35,
  sensor_mm: [36, 24],
  aspect: '3:2',
  near: 0.1,
  far: 1000,
  label: '',
  depth_of_field: false,
  f_stop: 2.8,
  focus: 10,
  focus_object_id: '',
  iso: 100,
  shutter_s: 1 / 4000,
  exposure_mode: 'ambient',
  exposure_compensation: 0,
}
