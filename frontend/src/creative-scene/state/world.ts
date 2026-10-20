import { Matrix4, Quaternion, Vector3 } from 'three'
import type { SceneObject, SceneTransform } from '../api.ts'

// World matrices in document space (Y up), for edits that keep where things
// are while their parent changes. The server does the same for the
// assistant (backend creativescene/reparent.go), with three.js's formulas.

const maxPosition = 100000
const minScale = 0.001
const maxScale = 1000

export function localMatrix(t: SceneTransform): Matrix4 {
  return new Matrix4().compose(new Vector3().fromArray(t.position), new Quaternion().fromArray(t.rotation), new Vector3().fromArray(t.scale))
}

/**
 * World matrices of objects by ID, computed once per call site. `extra` are
 * objects not in the scene yet (new groups); a parent loop or a missing
 * parent ends the chain there.
 */
export function worldOf(objects: readonly SceneObject[], extra: readonly SceneObject[] = []): (id: string | null) => Matrix4 {
  const byID = new Map([...objects, ...extra].map((o) => [o.id, o]))
  const memo = new Map<string, Matrix4>()
  const visiting = new Set<string>()
  const world = (id: string | null): Matrix4 => {
    if (!id) return new Matrix4()
    const cached = memo.get(id)
    if (cached) return cached.clone()
    const o = byID.get(id)
    if (!o || visiting.has(id)) return new Matrix4()
    visiting.add(id)
    const m = world(o.parent_id ?? null).multiply(localMatrix(o.transform))
    visiting.delete(id)
    memo.set(id, m)
    return m.clone()
  }
  return world
}

/**
 * The local transform that puts a world matrix under a parent with world
 * matrix parentWorld: inverse(parentWorld) × world, decomposed as three.js
 * does. Null when the result is out of the scene's range (a tiny parent
 * scale makes a huge local one), which callers refuse rather than clamp.
 */
export function localUnder(parentWorld: Matrix4, world: Matrix4): SceneTransform | null {
  if (parentWorld.determinant() === 0) return null
  const local = parentWorld.clone().invert().multiply(world)
  const position = new Vector3()
  const rotation = new Quaternion()
  const scale = new Vector3()
  local.decompose(position, rotation, scale)
  rotation.normalize()
  const p = position.toArray()
  const s = scale.toArray()
  if (p.some((v) => !Number.isFinite(v) || Math.abs(v) > maxPosition)) return null
  if (s.some((v) => !Number.isFinite(v) || v < minScale || v > maxScale)) return null
  return { position: p, rotation: rotation.toArray(), scale: s }
}

/** Equal within float noise: recomputed local transforms are never exact. */
export function sameTransform(a: SceneTransform, b: SceneTransform): boolean {
  const near = (x: readonly number[], y: readonly number[]) => x.length === y.length && x.every((v, i) => Math.abs(v - y[i]) < 1e-6)
  if (!near(a.position, b.position) || !near(a.scale, b.scale)) return false
  const dot = a.rotation.reduce((sum, v, i) => sum + v * b.rotation[i], 0)
  return Math.abs(dot) > 1 - 1e-9
}
