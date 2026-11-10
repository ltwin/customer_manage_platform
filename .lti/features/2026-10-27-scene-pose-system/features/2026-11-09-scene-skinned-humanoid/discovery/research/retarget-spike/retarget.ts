// Spike ① (scene-skinned-humanoid): retargeting a pose stored on the standard
// humanoid to a character, after VRM's normalized bones.
//
// Standard space: Y up, the character faces +Z, its left is +X; the rest is a
// T pose. A pose is each standard bone's rotation q_b in that space, as a
// change from the T pose: the bone's world change is G(b) = G(parent) · q_b,
// along the standard chain (bones the character lacks still pass their q on).
//
// A character: F turns standard space into its own (from the rest skeleton:
// hips→head is up, right→left upper arm is +X). T is its rest corrected to a
// T pose: upper and lower arms, hands, upper and lower legs swung (shortest
// arc) to the standard directions. A posed bone's world rotation is
// F · G(b) · F⁻¹ · T(n); nodes between mapped bones keep their rest locals.
import * as THREE from './three.ts'

export const humanBones = [
  'hips', 'spine', 'chest', 'upperChest', 'neck', 'head', 'leftEye', 'rightEye', 'jaw',
  'leftUpperLeg', 'leftLowerLeg', 'leftFoot', 'leftToes', 'rightUpperLeg', 'rightLowerLeg', 'rightFoot', 'rightToes',
  'leftShoulder', 'leftUpperArm', 'leftLowerArm', 'leftHand', 'rightShoulder', 'rightUpperArm', 'rightLowerArm', 'rightHand',
  ...['left', 'right'].flatMap((s) =>
    [['Thumb', ['Metacarpal', 'Proximal', 'Distal']], ...['Index', 'Middle', 'Ring', 'Little'].map((f) => [f, ['Proximal', 'Intermediate', 'Distal']] as const)].flatMap(
      ([f, js]) => (js as readonly string[]).map((j) => `${s}${f}${j}`),
    ),
  ),
] as const
export type HumanBone = (typeof humanBones)[number]

/** Each standard bone's parent on the standard chain. */
export const standardParent: Record<string, string | null> = (() => {
  const p: Record<string, string | null> = { hips: null, spine: 'hips', chest: 'spine', upperChest: 'chest', neck: 'upperChest', head: 'neck', leftEye: 'head', rightEye: 'head', jaw: 'head' }
  for (const s of ['left', 'right']) {
    Object.assign(p, {
      [`${s}UpperLeg`]: 'hips', [`${s}LowerLeg`]: `${s}UpperLeg`, [`${s}Foot`]: `${s}LowerLeg`, [`${s}Toes`]: `${s}Foot`,
      [`${s}Shoulder`]: 'upperChest', [`${s}UpperArm`]: `${s}Shoulder`, [`${s}LowerArm`]: `${s}UpperArm`, [`${s}Hand`]: `${s}LowerArm`,
    })
    for (const f of ['Thumb', 'Index', 'Middle', 'Ring', 'Little']) {
      const js = f === 'Thumb' ? ['Metacarpal', 'Proximal', 'Distal'] : ['Proximal', 'Intermediate', 'Distal']
      js.forEach((j, i) => (p[`${s}${f}${j}`] = i === 0 ? `${s}Hand` : `${s}${f}${js[i - 1]}`))
    }
  }
  return p
})()

/** Top-down order: parents before children. */
export const topDown: string[] = (() => {
  const out: string[] = []
  const visit = (b: string) => {
    if (out.includes(b)) return
    const p = standardParent[b]
    if (p) visit(p)
    out.push(b)
  }
  humanBones.forEach(visit)
  return out
})()

export type Mapping = Partial<Record<string, THREE.Object3D>>

const mixamo: Record<string, string> = {
  Hips: 'hips', Spine: 'spine', Spine1: 'chest', Spine2: 'upperChest', Neck: 'neck', Head: 'head', LeftEye: 'leftEye', RightEye: 'rightEye',
  ...Object.fromEntries(['Left', 'Right'].flatMap((S) => {
    const s = S.toLowerCase()
    return [
      [`${S}Shoulder`, `${s}Shoulder`], [`${S}Arm`, `${s}UpperArm`], [`${S}ForeArm`, `${s}LowerArm`], [`${S}Hand`, `${s}Hand`],
      [`${S}UpLeg`, `${s}UpperLeg`], [`${S}Leg`, `${s}LowerLeg`], [`${S}Foot`, `${s}Foot`], [`${S}ToeBase`, `${s}Toes`],
      [`${S}HandThumb1`, `${s}ThumbMetacarpal`], [`${S}HandThumb2`, `${s}ThumbProximal`], [`${S}HandThumb3`, `${s}ThumbDistal`],
      ...(['Index', 'Middle', 'Ring', 'Pinky'] as const).flatMap((f) => {
        const F = f === 'Pinky' ? 'Little' : f
        return [[`${S}Hand${f}1`, `${s}${F}Proximal`], [`${S}Hand${f}2`, `${s}${F}Intermediate`], [`${S}Hand${f}3`, `${s}${F}Distal`]]
      }),
    ]
  })),
}

/** Mixamo's names, any `mixamorig*:` prefix. */
export function mixamoMapping(root: THREE.Object3D): Mapping {
  const m: Mapping = {}
  root.traverse((o) => {
    if (o.userData.joint === false) return
    const name = o.userData.gltfName ?? o.name
    const bare = /^mixamorig\d*[:_]?(.*)$/.exec(name)?.[1]
    if (bare && mixamo[bare]) m[mixamo[bare]] = o
  })
  return m
}

/** A mapping by node name from a table. */
export function tableMapping(root: THREE.Object3D, table: Record<string, string>): Mapping {
  const m: Mapping = {}
  root.traverse((o) => {
    if (o.userData.joint === false) return
    const name = o.userData.gltfName ?? o.name
    if (table[name]) m[table[name]] = o
  })
  return m
}

const worldPos = (o: THREE.Object3D) => o.getWorldPosition(new THREE.Vector3())
const worldQuat = (o: THREE.Object3D) => o.getWorldQuaternion(new THREE.Quaternion())

/** Sets o's world rotation, its parent's as it is now. */
function setWorldQuat(o: THREE.Object3D, q: THREE.Quaternion) {
  const parent = o.parent ? worldQuat(o.parent) : new THREE.Quaternion()
  o.quaternion.copy(parent.invert().multiply(q))
  o.updateMatrixWorld(true)
}

/** The standard direction of a bone in the T pose, and the bone its tail is at. */
function tDirection(b: string, m: Mapping): { dir: THREE.Vector3; tail: THREE.Object3D } | null {
  const side = b.startsWith('left') ? 1 : -1
  const x = new THREE.Vector3(side, 0, 0)
  const down = new THREE.Vector3(0, -1, 0)
  const s = b.startsWith('left') ? 'left' : 'right'
  const tail = (...names: string[]) => names.map((n) => m[n]).find(Boolean) ?? null
  const t = (() => {
    if (b.endsWith('UpperArm')) return [x, tail(`${s}LowerArm`)] as const
    if (b.endsWith('LowerArm')) return [x, tail(`${s}Hand`)] as const
    if (b.endsWith('Hand')) return [x, tail(`${s}MiddleProximal`, `${s}RingProximal`, `${s}IndexProximal`)] as const
    if (b.endsWith('UpperLeg')) return [down, tail(`${s}LowerLeg`)] as const
    if (b.endsWith('LowerLeg')) return [down, tail(`${s}Foot`)] as const
    return null
  })()
  return t && t[1] ? { dir: t[0], tail: t[1] } : null
}

export type Character = {
  root: THREE.Object3D
  mapping: Mapping
  /** Standard space into the character's world. */
  facing: THREE.Quaternion
  /** Each mapped bone's world rotation in its T pose. */
  tWorld: Map<string, THREE.Quaternion>
  /** Each node's local rotation at rest, to go back to. */
  rest: Map<THREE.Object3D, THREE.Quaternion>
  /** The bones the T correction turned, and by how much (degrees). */
  corrected: Record<string, number>
}

/** The facing from the rest skeleton: up hips → head (or neck), +X right → left upper arm. */
export function facingOf(m: Mapping): THREE.Quaternion {
  const up = worldPos((m.head ?? m.neck)!).sub(worldPos(m.hips!)).normalize()
  const left = worldPos(m.leftUpperArm!).sub(worldPos(m.rightUpperArm!))
  left.addScaledVector(up, -left.dot(up)).normalize()
  const forward = new THREE.Vector3().crossVectors(left, up).normalize()
  return new THREE.Quaternion().setFromRotationMatrix(new THREE.Matrix4().makeBasis(left, up, forward))
}

/**
 * The roll that turns the back of the hand up (standard +Y), about the hand's
 * own direction: from the plane of the index and little (or ring) finger roots.
 */
function palmRoll(b: string, m: Mapping, facing: THREE.Quaternion, axis: THREE.Vector3): { q: THREE.Quaternion; angle: number } | null {
  const s = b.startsWith('left') ? 'left' : 'right'
  const hand = m[b]!
  const index = m[`${s}IndexProximal`]
  const outer = m[`${s}LittleProximal`] ?? m[`${s}RingProximal`]
  if (!index || !outer) return null
  const at = worldPos(hand)
  const back = new THREE.Vector3().crossVectors(worldPos(index).sub(at), worldPos(outer).sub(at))
  if (s === 'right') back.negate()
  const want = new THREE.Vector3(0, 1, 0).applyQuaternion(facing)
  const flat = (v: THREE.Vector3) => v.addScaledVector(axis, -v.dot(axis)).normalize()
  const from = flat(back)
  const to = flat(want)
  const angle = Math.atan2(new THREE.Vector3().crossVectors(from, to).dot(axis), from.dot(to))
  return Math.abs(angle) > 1e-6 ? { q: new THREE.Quaternion().setFromAxisAngle(axis, angle), angle } : null
}

/** Whether prepare turns each hand about itself so the back of it is up (spike option). */
export let palmTwist = true
export const setPalmTwist = (on: boolean) => (palmTwist = on)

export function prepare(root: THREE.Object3D, mapping: Mapping): Character {
  root.updateMatrixWorld(true)
  const rest = new Map<THREE.Object3D, THREE.Quaternion>()
  root.traverse((o) => rest.set(o, o.quaternion.clone()))
  const facing = facingOf(mapping)
  const corrected: Record<string, number> = {}
  for (const b of topDown) {
    const n = mapping[b]
    const t = n && tDirection(b, mapping)
    if (!n || !t) continue
    const want = t.dir.clone().applyQuaternion(facing)
    const have = worldPos(t.tail).sub(worldPos(n)).normalize()
    const swing = new THREE.Quaternion().setFromUnitVectors(have, want)
    const deg = THREE.MathUtils.radToDeg(2 * Math.acos(Math.min(1, Math.abs(swing.w))))
    if (deg > 0.01) corrected[b] = Number(deg.toFixed(2))
    setWorldQuat(n, swing.multiply(worldQuat(n)))
    if (b.endsWith('Hand') && palmTwist) {
      const roll = palmRoll(b, mapping, facing, want)
      if (roll) {
        corrected[`${b} roll`] = Number(THREE.MathUtils.radToDeg(roll.angle).toFixed(2))
        setWorldQuat(n, roll.q.multiply(worldQuat(n)))
      }
    }
  }
  const tWorld = new Map<string, THREE.Quaternion>()
  for (const b of topDown) if (mapping[b]) tWorld.set(b, worldQuat(mapping[b]!))
  restore({ root, rest } as Character)
  return { root, mapping, facing, tWorld, rest, corrected }
}

export function restore(c: Pick<Character, 'root' | 'rest'>) {
  c.root.traverse((o) => {
    const q = c.rest.get(o)
    if (q) o.quaternion.copy(q)
  })
  c.root.updateMatrixWorld(true)
}

export type Pose = Partial<Record<string, THREE.Quaternion>>

/** Puts the pose on the character: from its T pose, every mapped bone. */
export function apply(c: Character, pose: Pose) {
  restore(c)
  const g = new Map<string, THREE.Quaternion>()
  const fInv = c.facing.clone().invert()
  for (const b of topDown) {
    const parent = standardParent[b]
    const gb = (parent ? g.get(parent)! : new THREE.Quaternion()).clone().multiply(pose[b] ?? new THREE.Quaternion())
    g.set(b, gb)
    const n = c.mapping[b]
    if (!n) continue
    const world = c.facing.clone().multiply(gb).multiply(fInv).multiply(c.tWorld.get(b)!)
    setWorldQuat(n, world)
  }
}

/** The pose mirrored left to right: sides swapped, each rotation reflected in the YZ plane. */
export function mirror(pose: Pose): Pose {
  const out: Pose = {}
  for (const [b, q] of Object.entries(pose)) {
    const other = b.startsWith('left') ? `right${b.slice(4)}` : b.startsWith('right') ? `left${b.slice(5)}` : b
    out[other] = new THREE.Quaternion(q!.x, -q!.y, -q!.z, q!.w)
  }
  return out
}

/** A rotation by deg degrees about an axis in standard space. */
export const turn = (axis: [number, number, number], deg: number) => new THREE.Quaternion().setFromAxisAngle(new THREE.Vector3(...axis).normalize(), THREE.MathUtils.degToRad(deg))

/** A point's position in standard space, relative to another: F⁻¹ · (p − origin). */
export function standardOffset(c: Character, p: THREE.Object3D, origin: THREE.Object3D) {
  return worldPos(p).sub(worldPos(origin)).applyQuaternion(c.facing.clone().invert())
}

// --- VRMC_node_constraint (after three-vrm's node constraints): run after the
// pose, before skinning, in an order where each source is settled first.
export type Constraint = { dst: THREE.Object3D; src: THREE.Object3D; weight: number; update: () => void; deps: THREE.Object3D[] }

const axisOf = (a: string) => new THREE.Vector3(a.endsWith('X') ? 1 : 0, a.endsWith('Y') ? 1 : 0, a.endsWith('Z') ? 1 : 0).multiplyScalar(a.startsWith('Negative') ? -1 : 1)

/** The file's constraints, their rest taken from the nodes as they are now (the rest). */
export function constraintsOf(json: { nodes: { extensions?: Record<string, any> }[] }, byIndex: (i: number) => THREE.Object3D): Constraint[] {
  const list: Constraint[] = []
  json.nodes.forEach((n, i) => {
    const c = n.extensions?.VRMC_node_constraint?.constraint
    if (!c) return
    const dst = byIndex(i)
    const dstRest = dst.quaternion.clone()
    if (c.roll) {
      const src = byIndex(c.roll.source)
      const axis = axisOf(c.roll.rollAxis)
      const invDstRest = dstRest.clone().invert()
      const invSrcRestMulDstRest = src.quaternion.clone().invert().multiply(dstRest)
      list.push({ dst, src, weight: c.roll.weight ?? 1, deps: [src], update() {
        const delta = invDstRest.clone().multiply(src.quaternion).multiply(invSrcRestMulDstRest)
        const n1 = axis.clone().applyQuaternion(delta)
        const target = new THREE.Quaternion().setFromUnitVectors(n1, axis).premultiply(dstRest).multiply(delta)
        dst.quaternion.copy(dstRest).slerp(target, this.weight)
      } })
    } else if (c.aim) {
      const src = byIndex(c.aim.source)
      const axis = axisOf(c.aim.aimAxis)
      list.push({ dst, src, weight: c.aim.weight ?? 1, deps: [src, ...(dst.parent ? [dst.parent] : [])], update() {
        dst.updateWorldMatrix(true, false)
        src.updateWorldMatrix(true, false)
        const parent = dst.parent ? dst.parent.getWorldQuaternion(new THREE.Quaternion()) : new THREE.Quaternion()
        const a0 = axis.clone().applyQuaternion(dstRest).applyQuaternion(parent)
        const a1 = src.getWorldPosition(new THREE.Vector3()).sub(dst.getWorldPosition(new THREE.Vector3())).normalize()
        const target = new THREE.Quaternion().setFromUnitVectors(a0, a1).premultiply(parent.clone().invert()).multiply(parent).multiply(dstRest)
        dst.quaternion.copy(dstRest).slerp(target, this.weight)
      } })
    } else if (c.rotation) {
      const src = byIndex(c.rotation.source)
      const invSrcRest = src.quaternion.clone().invert()
      list.push({ dst, src, weight: c.rotation.weight ?? 1, deps: [src], update() {
        const target = dstRest.clone().multiply(invSrcRest.clone().multiply(src.quaternion))
        dst.quaternion.copy(dstRest).slerp(target, this.weight)
      } })
    }
  })
  // Sources (and aim's parents) before what depends on them.
  const order: Constraint[] = []
  const byDst = new Map(list.map((c) => [c.dst, c]))
  const visit = (c: Constraint, seen: Set<Constraint>) => {
    if (order.includes(c) || seen.has(c)) return
    seen.add(c)
    for (const d of c.deps) for (let a: THREE.Object3D | null = d; a; a = a.parent) if (byDst.get(a)) visit(byDst.get(a)!, seen)
    order.push(c)
  }
  list.forEach((c) => visit(c, new Set()))
  return order
}

export function runConstraints(list: Constraint[]) {
  for (const c of list) {
    c.update()
    c.dst.updateMatrixWorld(true)
  }
}
