import { Quaternion, Vector3 } from 'three'
import type { SceneTransform } from './api.ts'

// The editor speaks Blender: Z up, the front facing -Y. Documents keep the
// contract's three.js / glTF convention: Y up, the front facing +Z. One
// change of basis, a quarter turn about X, maps document space to view
// space; it happens only at the view's edges (rendering, committing a
// transform, the properties panel, placing on the ground), so stored data
// and every other consumer stay Y up.

/** Document → view: (x, y, z) → (x, -z, y). */
export const docToView = new Quaternion().setFromAxisAngle(new Vector3(1, 0, 0), Math.PI / 2)
const viewToDoc = docToView.clone().invert()

export type Vec3 = [number, number, number]
export type Quat = [number, number, number, number]
export type ViewTransform = { position: Vec3; rotation: Quat; scale: Vec3 }

const clean = (v: number) => (Object.is(v, -0) ? 0 : v)

export function positionToView(p: readonly number[]): Vec3 {
  return [clean(p[0]), clean(-p[2]), clean(p[1])]
}

export function positionToDoc(p: readonly number[]): Vec3 {
  return [clean(p[0]), clean(p[2]), clean(-p[1])]
}

const conjugate = (q: readonly number[], by: Quaternion, inverse: Quaternion): Quat => {
  const r = by.clone().multiply(new Quaternion(q[0], q[1], q[2], q[3])).multiply(inverse)
  return [clean(r.x), clean(r.y), clean(r.z), clean(r.w)]
}

export function rotationToView(q: readonly number[]): Quat {
  return conjugate(q, docToView, viewToDoc)
}

export function rotationToDoc(q: readonly number[]): Quat {
  return conjugate(q, viewToDoc, docToView)
}

/** Scale follows its axes: document Y (up) is view Z, document Z is view Y. */
export function scaleToView(s: readonly number[]): Vec3 {
  return [s[0], s[2], s[1]]
}

export const scaleToDoc = scaleToView

export function transformToView(t: SceneTransform): ViewTransform {
  return { position: positionToView(t.position), rotation: rotationToView(t.rotation), scale: scaleToView(t.scale) }
}

export function transformToDoc(t: { position: readonly number[]; rotation: readonly number[]; scale: readonly number[] }): ViewTransform {
  return { position: positionToDoc(t.position), rotation: rotationToDoc(t.rotation), scale: scaleToDoc(t.scale) }
}
