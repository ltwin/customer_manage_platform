import { Matrix4, Plane, Quaternion, Ray, Vector2, Vector3 } from 'three'
import { numericActive, numericValues, startNumeric, type Numeric } from './numeric.ts'

// Blender-style modal transforms (G / R / S), pure: given where things were
// when the transform started and where the pointer is now, compute every
// target's new local transform. Nothing is written until confirm.

export type Mode = 'grab' | 'rotate' | 'scale'
export type Axis = 0 | 1 | 2
export type Constraint =
  | { kind: 'none' }
  | { kind: 'axis' | 'plane'; axis: Axis; space: 'global' | 'local' }

export type Target = {
  id: string
  /** World matrix when the transform started. */
  world: Matrix4
  /** The parent's world matrix; identity at the root. */
  parentWorld: Matrix4
}

export type View = {
  /** World-space ray through a screen point in pixels. */
  ray(screen: Vector2): Ray
  /** Screen position in pixels of a world point. */
  toScreen(world: Vector3): Vector2
  /** Unit vector from a world point toward the camera. */
  toViewer(at: Vector3): Vector3
}

export type ModalState = {
  mode: Mode
  targets: Target[]
  pivot: Vector3
  start: Vector2
  constraint: Constraint
  numeric: Numeric
  /** Pointer when Shift went down; movement after it counts one tenth. */
  precisionFrom: Vector2 | null
  /** Last rotation angle, so turning past 180° keeps counting. */
  lastAngle: number
  view: View
}

export type Modifiers = { snap: boolean; shift: boolean }

export type LocalTransform = {
  id: string
  position: [number, number, number]
  rotation: [number, number, number, number]
  scale: [number, number, number]
}

export type Evaluation = {
  state: ModalState
  results: LocalTransform[]
  /** What the status bar shows: metres, degrees or a factor, per axis. */
  values: number[]
}

const minScale = 0.001
const maxScale = 1000
const unit = [new Vector3(1, 0, 0), new Vector3(0, 1, 0), new Vector3(0, 0, 1)]

/** The world-space pivot: the mean of the targets' world positions. */
export function pivotOf(targets: readonly Target[]): Vector3 {
  const sum = new Vector3()
  for (const t of targets) sum.add(new Vector3().setFromMatrixPosition(t.world))
  return targets.length ? sum.divideScalar(targets.length) : sum
}

export function startModal(mode: Mode, targets: Target[], pointer: Vector2, view: View): ModalState {
  return {
    mode,
    targets,
    pivot: pivotOf(targets),
    start: pointer.clone(),
    constraint: { kind: 'none' },
    numeric: startNumeric(axesFor(mode, { kind: 'none' })),
    precisionFrom: null,
    lastAngle: 0,
    view,
  }
}

/** Switching G / R / S mid-transform starts the new mode from the start pose. */
export function switchMode(s: ModalState, mode: Mode, pointer: Vector2): ModalState {
  return { ...s, mode, start: pointer.clone(), constraint: { kind: 'none' }, numeric: startNumeric(axesFor(mode, { kind: 'none' })), precisionFrom: null, lastAngle: 0 }
}

/** How many typed values a mode and constraint take. */
export function axesFor(mode: Mode, c: Constraint): number {
  if (mode === 'rotate') return 1
  if (c.kind === 'axis') return 1
  if (c.kind === 'plane') return 2
  return mode === 'scale' ? 1 : 3
}

/**
 * X / Y / Z: the first press locks the global axis, the second the objects'
 * own axis, the third unlocks. Shift + axis does the same for the plane
 * that excludes it.
 */
export function cycleConstraint(s: ModalState, axis: Axis, plane: boolean): ModalState {
  const kind = plane ? 'plane' : 'axis'
  const c = s.constraint
  let next: Constraint
  if (c.kind === kind && c.axis === axis) next = c.space === 'global' ? { kind, axis, space: 'local' } : { kind: 'none' }
  else next = { kind, axis, space: 'global' }
  return { ...s, constraint: next, numeric: startNumeric(axesFor(s.mode, next)) }
}

/** The axes the constraint names, in order, for labels and typed values. */
export function constraintAxes(c: Constraint): Axis[] {
  if (c.kind === 'axis') return [c.axis]
  if (c.kind === 'plane') return ([0, 1, 2] as Axis[]).filter((a) => a !== c.axis)
  return [0, 1, 2]
}

function axisDir(t: Target, axis: Axis, space: 'global' | 'local'): Vector3 {
  if (space === 'global') return unit[axis].clone()
  const q = new Quaternion()
  t.world.decompose(new Vector3(), q, new Vector3())
  return unit[axis].clone().applyQuaternion(q).normalize()
}

/** Shift: movement after the press counts one tenth. */
function effective(s: ModalState, pointer: Vector2, shift: boolean): Vector2 {
  if (!shift || !s.precisionFrom) return pointer.clone()
  return s.precisionFrom.clone().add(pointer.clone().sub(s.precisionFrom).multiplyScalar(0.1))
}

/** Holding or releasing Shift: remember where, keeping the value continuous. */
export function setPrecision(s: ModalState, pointer: Vector2, shift: boolean): ModalState {
  if (shift && !s.precisionFrom) return { ...s, precisionFrom: pointer.clone() }
  if (!shift && s.precisionFrom) {
    // Shift the start so the reduced movement so far is kept.
    const lost = pointer.clone().sub(effective(s, pointer, true))
    return { ...s, start: s.start.clone().add(lost), precisionFrom: null }
  }
  return s
}

function snapTo(v: number, step: number): number {
  return Math.round(v / step) * step
}

function steps(mode: Mode, m: Modifiers): number | null {
  if (!m.snap) return null
  const fine = m.shift
  if (mode === 'grab') return fine ? 0.1 : 1
  if (mode === 'rotate') return fine ? 1 : 5
  return fine ? 0.01 : 0.1
}

/** Closest-point parameter on the line pivot + t·dir to a ray. */
function lineParam(pivot: Vector3, dir: Vector3, ray: Ray): number {
  const w0 = pivot.clone().sub(ray.origin)
  const b = dir.dot(ray.direction)
  const d = dir.dot(w0)
  const e = ray.direction.dot(w0)
  const denom = 1 - b * b
  if (Math.abs(denom) < 1e-9) return 0
  return (b * e - d) / denom
}

function onPlane(normal: Vector3, pivot: Vector3, ray: Ray): Vector3 | null {
  const plane = new Plane().setFromNormalAndCoplanarPoint(normal, pivot)
  return ray.intersectPlane(plane, new Vector3())
}

function screenAngle(center: Vector2, p: Vector2): number {
  return Math.atan2(center.y - p.y, p.x - center.x)
}

const round = (v: number) => Math.round(v * 1e6) / 1e6

function toLocal(t: Target, world: Matrix4, keep?: { scale?: Vector3 }): LocalTransform {
  const local = t.parentWorld.clone().invert().multiply(world)
  const p = new Vector3()
  const q = new Quaternion()
  const s = new Vector3()
  local.decompose(p, q, s)
  q.normalize()
  const scale = keep?.scale ?? s
  return {
    id: t.id,
    position: [round(p.x), round(p.y), round(p.z)],
    rotation: [q.x, q.y, q.z, q.w],
    scale: [round(scale.x), round(scale.y), round(scale.z)],
  }
}

function localScale(t: Target): Vector3 {
  const local = t.parentWorld.clone().invert().multiply(t.world)
  const s = new Vector3()
  local.decompose(new Vector3(), new Quaternion(), s)
  return s
}

export function evaluate(s: ModalState, pointer: Vector2, m: Modifiers): Evaluation {
  const p = effective(s, pointer, m.shift && !m.snap)
  const typed = numericActive(s.numeric) ? numericValues(s.numeric) : null
  const step = steps(s.mode, m)
  const c = s.constraint
  if (s.mode === 'grab') return grab(s, p, c, typed, step)
  if (s.mode === 'rotate') return rotate(s, p, c, typed, step)
  return scale(s, p, c, typed, step)
}

function grab(s: ModalState, p: Vector2, c: Constraint, typed: (number | null)[] | null, step: number | null): Evaluation {
  const r0 = s.view.ray(s.start)
  const r1 = s.view.ray(p)
  const active = s.targets[0]
  let deltas: Vector3[]
  let values: number[]
  if (c.kind === 'axis') {
    const dir = axisDir(active, c.axis, c.space)
    let t = lineParam(s.pivot, dir, r1) - lineParam(s.pivot, dir, r0)
    if (step) t = snapTo(t, step)
    if (typed?.[0] != null) t = typed[0]
    deltas = s.targets.map((tg) => axisDir(tg, c.axis, c.space).multiplyScalar(t))
    values = [t]
  } else {
    const toViewer = s.view.toViewer(s.pivot)
    const normal = c.kind === 'plane' ? axisDir(active, c.axis, c.space) : toViewer
    const a = onPlane(normal, s.pivot, r0)
    const b = onPlane(normal, s.pivot, r1)
    let d: Vector3
    if (a && b && Math.abs(normal.dot(toViewer)) > 0.2) d = b.sub(a)
    else {
      // Seen edge-on the plane gives no usable hit: move in the view plane
      // and drop the locked axis instead.
      const va = onPlane(toViewer, s.pivot, r0)
      const vb = onPlane(toViewer, s.pivot, r1)
      d = va && vb ? vb.sub(va) : new Vector3()
      d.addScaledVector(normal, -d.dot(normal))
    }
    const axes = constraintAxes(c)
    const comps = [d.x, d.y, d.z]
    if (step) for (const ax of axes) comps[ax] = snapTo(comps[ax], step)
    if (typed) axes.forEach((ax, i) => { if (typed[i] != null) comps[ax] = typed[i] })
    if (c.kind === 'plane') comps[c.axis] = 0
    const delta = new Vector3(comps[0], comps[1], comps[2])
    deltas = s.targets.map(() => delta.clone())
    values = axes.map((ax) => comps[ax])
  }
  const results = s.targets.map((t, i) => {
    const world = new Matrix4().makeTranslation(deltas[i].x, deltas[i].y, deltas[i].z).multiply(t.world)
    return toLocal(t, world, { scale: localScale(t) })
  })
  return { state: s, results, values }
}

function rotate(s: ModalState, p: Vector2, c: Constraint, typed: (number | null)[] | null, step: number | null): Evaluation {
  const center = s.view.toScreen(s.pivot)
  let theta = screenAngle(center, p) - screenAngle(center, s.start)
  // Keep counting past ±180° by picking the turn nearest the last angle.
  theta += Math.round((s.lastAngle - theta) / (2 * Math.PI)) * 2 * Math.PI
  const state = { ...s, lastAngle: theta }
  const toViewer = s.view.toViewer(s.pivot)
  let degrees = (theta * 180) / Math.PI
  if (step) degrees = snapTo(degrees, step)
  const fromTyped = typed?.[0] != null
  if (fromTyped) degrees = typed![0]!
  const results = s.targets.map((t) => {
    let axis: Vector3
    let sign = 1
    if (c.kind === 'axis') {
      axis = axisDir(t, c.axis, c.space)
      // Turn the way the pointer turns, whichever way the axis faces.
      if (!fromTyped && axis.dot(toViewer) < 0) sign = -1
    } else axis = toViewer.clone()
    const angle = (sign * degrees * Math.PI) / 180
    const rot = new Matrix4().makeRotationAxis(axis, angle)
    const world = new Matrix4()
      .makeTranslation(s.pivot.x, s.pivot.y, s.pivot.z)
      .multiply(rot)
      .multiply(new Matrix4().makeTranslation(-s.pivot.x, -s.pivot.y, -s.pivot.z))
      .multiply(t.world)
    return toLocal(t, world, { scale: localScale(t) })
  })
  return { state, results, values: [degrees] }
}

function scale(s: ModalState, p: Vector2, c: Constraint, typed: (number | null)[] | null, step: number | null): Evaluation {
  const center = s.view.toScreen(s.pivot)
  const d0 = s.start.distanceTo(center)
  let f = d0 < 1 ? 1 : p.distanceTo(center) / d0
  if (step) f = snapTo(f, step)
  if (typed?.[0] != null) f = typed[0]
  // Object scale always uses the object's own axes; a global axis on a
  // rotated object would shear it.
  const axes = c.kind === 'none' ? [0, 1, 2] : constraintAxes(c)
  const results = s.targets.map((t) => {
    const startScale = localScale(t)
    const comps = [startScale.x, startScale.y, startScale.z]
    for (const ax of axes) comps[ax] = Math.min(maxScale, Math.max(minScale, comps[ax] * f))
    const pos = new Vector3().setFromMatrixPosition(t.world)
    const offset = pos.clone().sub(s.pivot)
    if (c.kind === 'none') offset.multiplyScalar(f)
    else
      for (const ax of axes) {
        const dir = axisDir(t, ax as Axis, c.space)
        offset.addScaledVector(dir, (f - 1) * offset.dot(dir))
      }
    const world = t.world.clone().setPosition(s.pivot.clone().add(offset))
    return toLocal(t, world, { scale: new Vector3(comps[0], comps[1], comps[2]) })
  })
  return { state: s, results, values: [f] }
}

/** Builds a target from a local transform and its parent's world matrix. */
export function targetFrom(
  id: string,
  transform: { position: number[]; rotation: number[]; scale: number[] },
  parentWorld: Matrix4 = new Matrix4(),
): Target {
  const local = new Matrix4().compose(
    new Vector3(...(transform.position as [number, number, number])),
    new Quaternion(...(transform.rotation as [number, number, number, number])),
    new Vector3(...(transform.scale as [number, number, number])),
  )
  return { id, world: parentWorld.clone().multiply(local), parentWorld: parentWorld.clone() }
}
