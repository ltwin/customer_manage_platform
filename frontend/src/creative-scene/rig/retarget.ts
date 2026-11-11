import { MathUtils, Matrix4, Quaternion, Vector3, type Object3D } from 'three'
import { humanBones, mirrorBone, standardParent } from './bones.ts'

// Putting a pose stored on the standard humanoid onto a character
// (patterns: 标准人形骨骼, after VRM's normalized bones; spike ① in
// discovery/research/2026-11-09-retarget-spike.md).
//
// Standard space: Y up, the character faces +Z, its left is +X; the rest is a
// T pose. A pose gives each standard bone a rotation q_b in that space, as a
// change from the T pose; along the standard chain G(b) = G(parent) · q_b, and
// bones the character lacks still pass their q on. A character's facing F
// turns standard space into its own; T(n) is its rest with the limbs swung to
// the standard directions. A posed bone's rotation is F · G(b) · F⁻¹ · T(n),
// set top-down; nodes between mapped bones keep their rest locals.
//
// Everything here is in the model root's space (the root's own transform left
// out), so what prepare finds for one model serves every instance of it.

/** Standard bone → the character's node for it. */
export type Mapping = Readonly<Partial<Record<string, Object3D>>>

/** Standard bone → its rotation in standard space, as a change from the T pose. */
export type Pose = Readonly<Partial<Record<string, Quaternion>>>

export type Prepared = {
  /** Standard space into the model root's. */
  facing: Quaternion
  /** Each mapped bone's rotation in root space in the character's T pose. */
  tPose: ReadonlyMap<string, Quaternion>
  /** How far the T correction turned each bone, in degrees (the hand's roll as "<bone> roll"). */
  corrected: Readonly<Record<string, number>>
}

/** o's matrix in root's space: its own and its ancestors' below root. */
function inRoot(root: Object3D, o: Object3D): Matrix4 {
  const m = new Matrix4()
  for (let a: Object3D | null = o; a && a !== root; a = a.parent) {
    a.updateMatrix()
    m.premultiply(a.matrix)
  }
  return m
}

const posIn = (root: Object3D, o: Object3D) => new Vector3().setFromMatrixPosition(inRoot(root, o))

function quatIn(root: Object3D, o: Object3D): Quaternion {
  const q = new Quaternion()
  inRoot(root, o).decompose(new Vector3(), q, new Vector3())
  return q
}

/** Sets o's local rotation so its rotation in root space is q, its parent as it is now. */
function setQuatIn(root: Object3D, o: Object3D, q: Quaternion) {
  const parent = o.parent && o.parent !== root ? quatIn(root, o.parent) : new Quaternion()
  o.quaternion.copy(parent.invert().multiply(q))
}

/** The standard direction of a corrected bone in the T pose, and the node at its tail. */
function tDirection(b: string, m: Mapping): { dir: Vector3; tail: Object3D } | null {
  const s = b.startsWith('left') ? 'left' : 'right'
  const out = new Vector3(s === 'left' ? 1 : -1, 0, 0)
  const down = new Vector3(0, -1, 0)
  const tail = (...names: string[]) => names.map((n) => m[n]).find((o) => o !== undefined)
  const [dir, at] = b.endsWith('UpperArm')
    ? [out, tail(`${s}LowerArm`)]
    : b.endsWith('LowerArm')
      ? [out, tail(`${s}Hand`)]
      : b.endsWith('Hand')
        ? [out, tail(`${s}MiddleProximal`, `${s}RingProximal`, `${s}IndexProximal`)]
        : b.endsWith('UpperLeg')
          ? [down, tail(`${s}LowerLeg`)]
          : b.endsWith('LowerLeg')
            ? [down, tail(`${s}Foot`)]
            : [null, undefined]
  return dir && at ? { dir, tail: at } : null
}

const correctedBones = humanBones.filter((b) => /^(left|right)(UpperArm|LowerArm|Hand|UpperLeg|LowerLeg)$/.test(b))

/** Why a mapped skeleton gives no facing or T pose (patterns: 标准人形骨骼「骨架的几何条件」). */
export type GeometryProblem = { reason: 'facing' | 'direction'; bone?: string; message: string }

export type Geometry = {
  problem: GeometryProblem | null
  /** Mapped bones whose matrix (with their ancestors') scales unevenly: posing may distort them. */
  nonUniform: string[]
}

/** The facing from the rest: up hips → head (or neck), +X right → left upper arm. */
function facingOf(root: Object3D, m: Mapping): Quaternion | null {
  const hips = posIn(root, m.hips!)
  const up = posIn(root, (m.head ?? m.neck)!).sub(hips)
  const s = up.length()
  if (s < 1e-4) return null
  up.divideScalar(s)
  const left = posIn(root, m.leftUpperArm!).sub(posIn(root, m.rightUpperArm!))
  left.addScaledVector(up, -left.dot(up))
  if (left.length() < 0.05 * s) return null
  left.normalize()
  const forward = new Vector3().crossVectors(left, up).normalize()
  return new Quaternion().setFromRotationMatrix(new Matrix4().makeBasis(left, up, forward))
}

/**
 * Whether a mapped skeleton at rest gives a facing and a T pose, and where it
 * scales unevenly. m has the required bones (checkRig passed).
 */
export function checkGeometry(root: Object3D, m: Mapping): Geometry {
  const nonUniform = humanBones.filter((b) => {
    const o = m[b]
    if (!o) return false
    const scale = new Vector3()
    inRoot(root, o).decompose(new Vector3(), new Quaternion(), scale)
    const [lo, hi] = [Math.min(scale.x, scale.y, scale.z), Math.max(scale.x, scale.y, scale.z)]
    return hi > 1.01 * lo
  })
  const s = posIn(root, (m.head ?? m.neck)!).distanceTo(posIn(root, m.hips!))
  if (s < 1e-4) return { problem: { reason: 'facing', message: '头与髋几乎重合，无法确定角色的朝向' }, nonUniform }
  if (!facingOf(root, m)) return { problem: { reason: 'facing', message: '左右上臂在身体的竖直方向上几乎重合，无法确定角色的朝向' }, nonUniform }
  for (const b of correctedBones) {
    const n = m[b]
    const t = n && tDirection(b, m)
    if (t && posIn(root, t.tail).distanceTo(posIn(root, n)) < 1e-3 * s) {
      return { problem: { reason: 'direction', bone: b, message: `${b} 到下一根骨骼的距离几乎为 0，无法确定它的方向` }, nonUniform }
    }
  }
  return { problem: null, nonUniform }
}

/**
 * The roll about the hand's own direction axis that turns the back of the
 * hand (index × little, or ring, about the hand; reversed on the right) to
 * the standard up, both projected off the axis.
 */
function palmRoll(root: Object3D, b: string, m: Mapping, facing: Quaternion, axis: Vector3): { q: Quaternion; angle: number } | null {
  const s = b.startsWith('left') ? 'left' : 'right'
  const index = m[`${s}IndexProximal`]
  const outer = m[`${s}LittleProximal`] ?? m[`${s}RingProximal`]
  if (!index || !outer) return null
  const at = posIn(root, m[b]!)
  const back = new Vector3().crossVectors(posIn(root, index).sub(at), posIn(root, outer).sub(at))
  if (s === 'right') back.negate()
  const flat = (v: Vector3) => v.addScaledVector(axis, -v.dot(axis))
  const from = flat(back)
  const to = flat(new Vector3(0, 1, 0).applyQuaternion(facing))
  if (from.lengthSq() < 1e-12 || to.lengthSq() < 1e-12) return null
  const angle = Math.atan2(new Vector3().crossVectors(from, to).dot(axis), from.dot(to))
  return Math.abs(angle) > 1e-6 ? { q: new Quaternion().setFromAxisAngle(axis, angle), angle } : null
}

/**
 * The facing and T pose of a mapped skeleton at rest, or null when its
 * geometry gives none (checkGeometry says why). The nodes are left at rest.
 */
export function prepare(root: Object3D, m: Mapping): Prepared | null {
  if (checkGeometry(root, m).problem) return null
  const facing = facingOf(root, m)!
  const rest = new Map(humanBones.flatMap((b) => (m[b] ? [[m[b], m[b].quaternion.clone()] as const] : [])))
  const corrected: Record<string, number> = {}
  for (const b of correctedBones) {
    const n = m[b]
    const t = n && tDirection(b, m)
    if (!n || !t) continue
    const want = t.dir.clone().applyQuaternion(facing)
    const have = posIn(root, t.tail).sub(posIn(root, n)).normalize()
    const swing = new Quaternion().setFromUnitVectors(have, want)
    const deg = MathUtils.radToDeg(2 * Math.acos(Math.min(1, Math.abs(swing.w))))
    if (deg > 0.01) corrected[b] = Number(deg.toFixed(2))
    setQuatIn(root, n, swing.multiply(quatIn(root, n)))
    if (b.endsWith('Hand')) {
      const roll = palmRoll(root, b, m, facing, want)
      if (roll) {
        corrected[`${b} roll`] = Number(MathUtils.radToDeg(roll.angle).toFixed(2))
        setQuatIn(root, n, roll.q.multiply(quatIn(root, n)))
      }
    }
  }
  const tPose = new Map(humanBones.flatMap((b) => (m[b] ? [[b, quatIn(root, m[b])] as const] : [])))
  for (const [o, q] of rest) o.quaternion.copy(q)
  return { facing, tPose, corrected }
}

/**
 * Puts the pose on the mapped bones, top-down. Nodes that are not mapped keep
 * the local rotation they have, which is their rest unless a node constraint
 * moved them (reset those first). The caller updates the world matrices.
 */
export function apply(p: Prepared, root: Object3D, m: Mapping, pose: Pose) {
  const g = new Map<string, Quaternion>()
  const facingInv = p.facing.clone().invert()
  for (const b of humanBones) {
    const parent = standardParent.get(b)
    const gb = (parent ? g.get(parent)!.clone() : new Quaternion()).multiply(pose[b] ?? new Quaternion())
    g.set(b, gb)
    const n = m[b]
    if (!n) continue
    setQuatIn(root, n, p.facing.clone().multiply(gb).multiply(facingInv).multiply(p.tPose.get(b)!))
  }
}

/** The pose mirrored left to right: sides swapped, each rotation reflected in the YZ plane. */
export function mirror(pose: Pose): Pose {
  const out: Partial<Record<string, Quaternion>> = {}
  for (const [b, q] of Object.entries(pose)) if (q) out[mirrorBone(b)] = new Quaternion(q.x, -q.y, -q.z, q.w)
  return out
}

/** A rotation by deg degrees about an axis in standard space. */
export const turn = (axis: readonly [number, number, number], deg: number) =>
  new Quaternion().setFromAxisAngle(new Vector3(...axis).normalize(), MathUtils.degToRad(deg))

/** Where p is from origin in standard space: F⁻¹ · (p − origin), both in root space. */
export function standardOffset(p: Prepared, root: Object3D, at: Object3D, origin: Object3D): Vector3 {
  return posIn(root, at).sub(posIn(root, origin)).applyQuaternion(p.facing.clone().invert())
}
