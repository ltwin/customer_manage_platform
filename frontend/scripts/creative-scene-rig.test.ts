import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { MathUtils, Matrix4, Object3D, Quaternion, Vector3 } from 'three'
import { humanBones, requiredBones, standardParent } from '../src/creative-scene/rig/bones.ts'
import { bindConstraints, jointParents, resetConstraints, runConstraints, type Constraint } from '../src/creative-scene/rig/constraints.ts'
import { detectRig, type RigJSON } from '../src/creative-scene/rig/detect.ts'
import { checkRig, pruneOptional, type Rig, type Skeleton } from '../src/creative-scene/rig/humanoid.ts'
import { apply, checkGeometry, mirror, prepare, standardOffset, turn, type Mapping, type Pose, type Prepared } from '../src/creative-scene/rig/retarget.ts'

// The skeleton core (scene-skinned-humanoid task 3.1): the shared vectors, the
// mapping found on the five characters of spike ① and the two built-in bodies,
// and spike ①'s numeric checks as tests. The characters are their node trees
// only (fixtures/rig/skeletons.json: names, transforms, skin joints, the VRM
// extensions), taken out of the files the spike used.

type Vectors = {
  skeletons: Record<string, Skeleton>
  cases: { name: string; skeleton: string; rig: Rig; valid: boolean; field?: string; reason?: string }[]
}
const vectors = JSON.parse(readFileSync(new URL('../src/creative-scene/rig/humanoid_rig.gen.json', import.meta.url), 'utf8')) as Vectors

type NodeJSON = { name: string; children?: number[]; translation?: number[]; rotation?: number[]; scale?: number[]; matrix?: number[] }
type Character = RigJSON & { nodes: NodeJSON[]; scenes: { nodes: number[] }[]; scene: number; skins: { joints: number[] }[] }
const characters = JSON.parse(readFileSync(new URL('fixtures/rig/skeletons.json', import.meta.url), 'utf8')) as Record<string, Character>

const vectorCase = (name: string) => vectors.cases.find((c) => c.name === name)!
const spikeMapping = (skeleton: string) => vectorCase(`${skeleton}: 穿刺①/素体检查的映射`).rig.humanoid!

/** A character's node tree under a fresh root, by node index. */
function tree(j: Character) {
  const nodes = j.nodes.map((n, i) => {
    const o = new Object3D()
    o.name = n.name || `node${i}`
    if (n.matrix) new Matrix4().fromArray(n.matrix).decompose(o.position, o.quaternion, o.scale)
    if (n.translation) o.position.fromArray(n.translation)
    if (n.rotation) o.quaternion.fromArray(n.rotation)
    if (n.scale) o.scale.fromArray(n.scale)
    return o
  })
  j.nodes.forEach((n, i) => n.children?.forEach((c) => nodes[i].add(nodes[c])))
  const root = new Object3D()
  for (const r of j.scenes[j.scene].nodes) root.add(nodes[r])
  root.updateMatrixWorld(true)
  return { root, nodes }
}

const toMapping = (h: Record<string, number>, nodes: Object3D[]): Mapping => Object.fromEntries(Object.entries(h).map(([b, n]) => [b, nodes[n]]))

function load(name: string) {
  const { root, nodes } = tree(characters[name])
  const m = toMapping(spikeMapping(name), nodes)
  const p = prepare(root, m)
  assert.ok(p, `${name} prepares`)
  return { root, nodes, m, p }
}

const degrees = (a: Vector3, b: Vector3) => MathUtils.radToDeg(a.angleTo(b))

test('rig checks agree with the server on the shared vectors', () => {
  for (const c of vectors.cases) {
    const got = checkRig(c.rig, vectors.skeletons[c.skeleton])
    assert.deepEqual(got && { field: got.field, reason: got.reason }, c.valid ? null : { field: c.field, reason: c.reason }, c.name)
  }
  assert.equal(checkRig({ humanoid: spikeMapping('xbot') }, null)?.reason, 'no_skeleton')
})

test('the standard chain', () => {
  assert.equal(humanBones.length, 55)
  assert.equal(requiredBones.length, 15)
  for (const [i, b] of humanBones.entries()) {
    const p = standardParent.get(b)
    assert.ok(p === null ? b === 'hips' : humanBones.indexOf(p!) < i, `${b} after its parent`)
  }
  assert.equal(standardParent.get('leftThumbMetacarpal'), 'leftHand')
  assert.equal(standardParent.get('rightLittleDistal'), 'rightLittleIntermediate')
  assert.equal(standardParent.get('rightShoulder'), 'upperChest')
})

test('the browser drops the optional bones the server would refuse, never a required one', () => {
  const xbot = vectors.skeletons.xbot
  const dropped = (name: string) => {
    const c = vectorCase(name)
    const out = pruneOptional(c.rig.humanoid!, xbot)
    return { out, reasons: out.dropped.map((d) => `${d.field} ${d.reason}`) }
  }
  {
    const { out, reasons } = dropped('xbot: 可选骨骼违反嵌入（食指第一、三节对调）')
    assert.deepEqual(reasons, ['rig.humanoid.leftIndexProximal hierarchy', 'rig.humanoid.leftIndexIntermediate hierarchy'])
    assert.equal(checkRig({ humanoid: out.humanoid }, xbot), null)
  }
  {
    const { out, reasons } = dropped('xbot: 映射到不是关节的节点')
    assert.deepEqual(reasons, ['rig.humanoid.leftEye not_joint'])
    assert.equal(checkRig({ humanoid: out.humanoid }, xbot), null)
  }
  {
    const { out, reasons } = dropped('xbot: leftToes 与 leftFoot 映射到同一节点')
    assert.deepEqual(reasons, ['rig.humanoid.leftToes duplicate_node'])
    assert.equal(out.humanoid.leftFoot, vectorCase('xbot: leftToes 与 leftFoot 映射到同一节点').rig.humanoid!.leftFoot)
  }
  {
    // Required bones stay, for checkRig to refuse.
    const { out, reasons } = dropped('xbot: 左右腿对调')
    assert.deepEqual(reasons, [])
    assert.equal(checkRig({ humanoid: out.humanoid }, xbot)?.field, 'rig.humanoid.leftLowerLeg')
  }
})

test('finds the mapping of each character, and none on RobotExpressive', () => {
  const want: Record<string, string> = { xbot: 'mixamo', michelle: 'mixamo', vrm1_twist: 'vrm1', vrm0_girl: 'vrm0', ubc_male: 'ue', ubc_female: 'ue' }
  for (const [name, via] of Object.entries(want)) {
    const d = detectRig(characters[name])
    const skeleton = vectors.skeletons[name]
    const { humanoid } = pruneOptional(d.humanoid, skeleton)
    assert.equal(d.via, via, name)
    assert.deepEqual(humanoid, spikeMapping(name), name)
    assert.equal(checkRig({ humanoid, constraints: d.constraints }, skeleton), null, name)
  }
  assert.equal(Object.keys(spikeMapping('ubc_male')).length, 52)
  assert.equal(Object.keys(spikeMapping('ubc_female')).length, 52)

  const twist = detectRig(characters.vrm1_twist)
  assert.deepEqual(twist.constraints, vectorCase('vrm1_twist: VRMC_node_constraint 原样').rig.constraints)
  assert.deepEqual(twist.droppedConstraints, [])

  // RobotExpressive: no names to go by; mapped by hand, its feet hang under IK
  // controls, so either choice of hips leaves the mapping refused.
  const robot = detectRig(characters.robot_expressive)
  assert.equal(robot.via, null)
  assert.deepEqual(robot.humanoid, {})
  for (const [name, field] of [
    ['robot_expressive: Hips 作 hips，腿挂在 Body 下', 'rig.humanoid.leftUpperLeg'],
    ['robot_expressive: Body 作 hips，脚挂在控制骨下', 'rig.humanoid.leftFoot'],
  ]) {
    const c = vectorCase(name)
    const { humanoid } = pruneOptional(c.rig.humanoid!, vectors.skeletons.robot_expressive)
    const got = checkRig({ humanoid }, vectors.skeletons.robot_expressive)
    assert.equal(got?.field, field, name)
    assert.equal(got?.reason, 'hierarchy', name)
  }
})

test('a constraint whose source is not a joint is left out at import', () => {
  const j = structuredClone(characters.vrm1_twist)
  const joints = new Set(j.skins.flatMap((s) => s.joints))
  const notJoint = j.nodes.findIndex((_, i) => !joints.has(i))
  const at = j.nodes.findIndex((n) => n.name === 'J_Roll_L_Hand')
  ;(j.nodes[at] as { extensions: { VRMC_node_constraint: { constraint: { roll: { source: number } } } } }).extensions.VRMC_node_constraint.constraint.roll.source = notJoint
  const d = detectRig(j)
  assert.deepEqual(d.droppedConstraints, [{ node: at, name: 'J_Roll_L_Hand' }])
  assert.equal(d.constraints.length, 13)
})

// --- spike ①'s numeric checks -------------------------------------------------

const elbow: Pose = { leftUpperArm: turn([0, 1, 0], -90), leftLowerArm: turn([0, 0, 1], 90) }
/** Each pose, and the direction it should give a limb in standard space. */
const poses: [string, Pose, [string, string, [number, number, number]][]][] = [
  ['T pose', {}, [['leftUpperArm', 'leftLowerArm', [1, 0, 0]], ['leftLowerArm', 'leftHand', [1, 0, 0]], ['rightUpperArm', 'rightLowerArm', [-1, 0, 0]], ['leftUpperLeg', 'leftLowerLeg', [0, -1, 0]], ['leftLowerLeg', 'leftFoot', [0, -1, 0]]]],
  ['left arm forward', { leftUpperArm: turn([0, 1, 0], -90) }, [['leftUpperArm', 'leftLowerArm', [0, 0, 1]], ['leftLowerArm', 'leftHand', [0, 0, 1]], ['rightUpperArm', 'rightLowerArm', [-1, 0, 0]]]],
  ['left elbow bent up', elbow, [['leftUpperArm', 'leftLowerArm', [0, 0, 1]], ['leftLowerArm', 'leftHand', [0, 1, 0]]]],
  ['mirror of the elbow', mirror(elbow), [['rightUpperArm', 'rightLowerArm', [0, 0, 1]], ['rightLowerArm', 'rightHand', [0, 1, 0]], ['leftUpperArm', 'leftLowerArm', [1, 0, 0]]]],
  ['spine forward, left thigh up', { spine: turn([1, 0, 0], 30), leftUpperLeg: turn([1, 0, 0], -90) }, [['leftUpperLeg', 'leftLowerLeg', [0, 0, 1]], ['rightUpperLeg', 'rightLowerLeg', [0, -1, 0]]]],
]

const posed = (c: { root: Object3D; m: Mapping; p: Prepared }, pose: Pose) => {
  apply(c.p, c.root, c.m, pose)
  c.root.updateMatrixWorld(true)
}

test('every pose points the limbs the standard way on each character (< 1°)', () => {
  for (const name of ['xbot', 'michelle', 'vrm1_twist', 'vrm0_girl', 'ubc_male', 'ubc_female']) {
    const c = load(name)
    for (const [pose, q, want] of poses) {
      posed(c, q)
      for (const [from, to, dir] of want) {
        const e = degrees(standardOffset(c.p, c.root, c.m[to]!, c.m[from]!), new Vector3(...dir))
        assert.ok(e < 1, `${name}, ${pose}: ${from} off by ${e.toFixed(2)}°`)
      }
    }
    // The back of the left hand is up in the T pose.
    posed(c, {})
    const outer = c.m.leftLittleProximal ?? c.m.leftRingProximal
    if (c.m.leftIndexProximal && outer) {
      const i = standardOffset(c.p, c.root, c.m.leftIndexProximal, c.m.leftHand!)
      const back = i.cross(standardOffset(c.p, c.root, outer, c.m.leftHand!)).normalize()
      assert.ok(back.y > 0.99, `${name}: back of the hand ${back.toArray()}`)
    }
    // The head turns 45° to the left: the eyes' midpoint about the head.
    if (c.m.leftEye && c.m.rightEye) {
      const look = () => {
        const mid = standardOffset(c.p, c.root, c.m.leftEye!, c.m.head!).add(standardOffset(c.p, c.root, c.m.rightEye!, c.m.head!))
        return MathUtils.radToDeg(Math.atan2(mid.x, mid.z))
      }
      const before = look()
      posed(c, { head: turn([0, 1, 0], 45) })
      assert.ok(Math.abs(look() - before - 45) < 0.1, `${name}: head turned ${look() - before}°`)
    }
  }
})

test('the facing comes from the rest skeleton, whatever way the file faces', () => {
  const forward = (name: string) => new Vector3(0, 0, 1).applyQuaternion(load(name).p.facing)
  assert.ok(forward('vrm0_girl').z < -0.99, 'VRM 0.x faces −Z')
  assert.ok(forward('xbot').z > 0.99)
  // Michelle's root is turned 90° about X (a Z-up export); its facing is +Z in the scene all the same.
  assert.ok(forward('michelle').z > 0.99)
})

// Probes 5 cm in front of and 3 cm above each mapped bone, fixed to it, so a
// bone's whole rotation shows, its roll included.
function withProbes(c: { root: Object3D; m: Mapping }) {
  const p = prepare(c.root, c.m)!
  for (const [b, o] of Object.entries(c.m)) {
    const probe = new Object3D()
    probe.name = `probe:${b}`
    const at = o!.getWorldPosition(new Vector3()).add(new Vector3(0, 0.03, 0.05).applyQuaternion(p.facing))
    o!.add(probe)
    probe.position.copy(o!.worldToLocal(at))
  }
  c.root.updateMatrixWorld(true)
}

/** Every mapped bone's probe direction in standard space, for each pose. */
function signature(root: Object3D, m: Mapping): Vector3[] {
  const p = prepare(root, m)!
  return poses.flatMap(([, pose]) => {
    posed({ root, m, p }, pose)
    return humanBones.filter((b) => m[b]).map((b) => standardOffset(p, root, m[b]!.children.find((ch) => ch.name === `probe:${b}`)!, m[b]!).normalize())
  })
}

const rng = (() => {
  let s = 12345
  return () => (s = (s * 1103515245 + 12345) & 0x7fffffff) / 0x7fffffff
})()
const randomQuat = () => new Quaternion().setFromAxisAngle(new Vector3(rng() - 0.5, rng() - 0.5, rng() - 0.5).normalize(), rng() * 6.28)

test('characters differing in one way get the same rotation on every bone (< 0.01°)', () => {
  const xbot = () => {
    const { root, nodes } = tree(characters.xbot)
    const m = toMapping(spikeMapping('xbot'), nodes)
    withProbes({ root, m })
    return { root, m }
  }
  const base = xbot()
  const want = signature(base.root, base.m)
  const variants: [string, (v: { root: Object3D; m: Mapping }) => void][] = [
    [
      'A pose at rest',
      (v) => {
        const p = prepare(v.root, v.m)!
        for (const [b, side] of [['leftUpperArm', 1], ['rightUpperArm', -1]] as const) {
          const o = v.m[b]!
          const down = new Quaternion().setFromAxisAngle(new Vector3(0, 0, 1).applyQuaternion(p.facing), (-side * Math.PI) / 4)
          o.quaternion.copy(o.parent!.getWorldQuaternion(new Quaternion()).invert().multiply(down).multiply(o.getWorldQuaternion(new Quaternion())))
          v.root.updateMatrixWorld(true)
        }
      },
    ],
    [
      'local axes turned at random on every bone',
      (v) => {
        for (const o of Object.values(v.m)) {
          const r = randomQuat()
          o!.quaternion.multiply(r)
          const inv = r.clone().invert()
          for (const ch of o!.children) {
            ch.position.applyQuaternion(inv)
            ch.quaternion.premultiply(inv)
          }
        }
      },
    ],
    [
      'a twist helper inside each arm',
      (v) => {
        for (const s of ['left', 'right']) {
          const lower = v.m[`${s}LowerArm`]!
          const helper = new Object3D()
          v.m[`${s}UpperArm`]!.add(helper)
          helper.position.copy(lower.position).multiplyScalar(0.5)
          helper.quaternion.copy(randomQuat())
          v.root.updateMatrixWorld(true)
          helper.attach(lower)
        }
      },
    ],
    [
      'arms and legs 30% longer',
      (v) => {
        for (const b of ['leftLowerArm', 'rightLowerArm', 'leftHand', 'rightHand', 'leftLowerLeg', 'rightLowerLeg']) v.m[b]!.position.multiplyScalar(1.3)
      },
    ],
    [
      'facing −Z under a Z-up root turn',
      (v) => {
        v.root.children[0].quaternion
          .premultiply(new Quaternion().setFromAxisAngle(new Vector3(0, 1, 0), Math.PI))
          .premultiply(new Quaternion().setFromAxisAngle(new Vector3(1, 0, 0), -Math.PI / 2))
      },
    ],
    [
      'the model root placed, turned and scaled in the scene',
      (v) => {
        v.root.position.set(3, 1, -2)
        v.root.quaternion.copy(randomQuat())
        v.root.scale.setScalar(2.5)
      },
    ],
  ]
  for (const [name, change] of variants) {
    const v = xbot()
    change(v)
    v.root.updateMatrixWorld(true)
    const got = signature(v.root, v.m)
    const worst = Math.max(...got.map((g, i) => degrees(g, want[i])))
    assert.ok(worst < 0.01, `${name}: off by ${worst}°`)
  }
})

test('a missing upper chest passes its turn on to the arms', () => {
  const { root, nodes } = tree(characters.xbot)
  const h = { ...spikeMapping('xbot') }
  delete h.upperChest
  const m = toMapping(h, nodes)
  const p = prepare(root, m)!
  posed({ root, m, p }, { upperChest: turn([0, 1, 0], 30) })
  const arm = standardOffset(p, root, m.leftLowerArm!, m.leftUpperArm!)
  assert.ok(Math.abs(MathUtils.radToDeg(Math.atan2(-arm.z, arm.x)) - 30) < 0.01)
})

test('a fist curls each finger of the built-in bodies under the palm, back toward the wrist', () => {
  // Left fingers point +X with the back of the hand up: curling is about −Z.
  const fist: Pose = Object.fromEntries(['Index', 'Middle', 'Ring', 'Little'].flatMap((f) => ['Proximal', 'Intermediate', 'Distal'].map((j) => [`left${f}${j}`, turn([0, 0, 1], -80)])))
  for (const name of ['ubc_male', 'ubc_female']) {
    const c = load(name)
    posed(c, fist)
    for (const f of ['Index', 'Middle', 'Ring', 'Little']) {
      const d = standardOffset(c.p, c.root, c.m[`left${f}Distal`]!, c.m[`left${f}Proximal`]!)
      assert.ok(d.y < 0 && d.x < 0, `${name} ${f}: distal from knuckle ${d.toArray()}`)
    }
  }
})

test('mirror swaps the sides and reflects each rotation in the YZ plane', () => {
  const q = new Quaternion(0.1, 0.2, 0.3, 0.927).normalize()
  const m = mirror({ leftHand: q, spine: q })
  assert.deepEqual(m.rightHand!.toArray(), [q.x, -q.y, -q.z, q.w])
  assert.deepEqual(m.spine!.toArray(), [q.x, -q.y, -q.z, q.w])
  assert.equal(m.leftHand, undefined)
})

test('prepare leaves the skeleton at rest', () => {
  const { root, nodes } = tree(characters.michelle)
  const before = nodes.map((o) => o.quaternion.toArray())
  prepare(root, toMapping(spikeMapping('michelle'), nodes))
  assert.deepEqual(
    nodes.map((o) => o.quaternion.toArray()),
    before,
  )
})

// --- node constraints -------------------------------------------------------------

test('an aim under a constrained parent points at its source, as three-vrm evaluates it', () => {
  // root → a → parent → aimed; root → turner (turns parent by a rotation
  // constraint); root → target (what aimed looks at, along its +X).
  const root = new Object3D()
  const [a, parent, aimed, turner, target] = Array.from({ length: 5 }, () => new Object3D())
  root.add(a, turner, target)
  a.add(parent)
  parent.add(aimed)
  parent.position.set(0, 1, 0)
  aimed.position.set(0, 0.5, 0)
  aimed.quaternion.setFromAxisAngle(new Vector3(0, 0, 1), 0.3)
  target.position.set(1, 2, 0.5)
  root.updateMatrixWorld(true)
  const nodes = [a, parent, aimed, turner, target]
  // Listed aim first: the order must come from the dependencies.
  const cs: Constraint[] = [
    { node: 2, kind: 'aim', source: 4, weight: 1, aim_axis: 'PositiveX' },
    { node: 1, kind: 'rotation', source: 3, weight: 1 },
  ]
  const parents = jointParents({ joints: [{ node: 0, parent: null }, { node: 1, parent: 0 }, { node: 2, parent: 1 }, { node: 3, parent: null }, { node: 4, parent: null }] })
  const aimError = (list: ReturnType<typeof bindConstraints>) => {
    resetConstraints(list)
    turner.quaternion.setFromAxisAngle(new Vector3(0, 1, 0).normalize(), 1.1)
    root.updateMatrixWorld(true)
    runConstraints(list)
    const axis = new Vector3(1, 0, 0).applyQuaternion(aimed.getWorldQuaternion(new Quaternion()))
    const toTarget = target.getWorldPosition(new Vector3()).sub(aimed.getWorldPosition(new Vector3()))
    return degrees(axis, toTarget)
  }
  const ordered = bindConstraints(cs, parents, (i) => nodes[i])
  assert.ok(aimError(ordered) < 1e-3, 'aim after its parent')
  assert.ok(!parent.quaternion.equals(new Quaternion()), 'the parent turned')
  // Without the dependency known, the aim runs on the parent's old transform and misses.
  for (const o of nodes) o.quaternion.identity()
  aimed.quaternion.setFromAxisAngle(new Vector3(0, 0, 1), 0.3)
  turner.quaternion.identity()
  root.updateMatrixWorld(true)
  const blind = bindConstraints(cs, new Map(), (i) => nodes[i])
  assert.ok(aimError(blind) > 1, 'aim before its parent misses')
})

test("the VRM twist sample's roll bones take their share of the hand's turn", () => {
  const { root, nodes } = tree(characters.vrm1_twist)
  const d = detectRig(characters.vrm1_twist)
  const m = toMapping(d.humanoid, nodes)
  const p = prepare(root, m)!
  const list = bindConstraints(d.constraints, jointParents(vectors.skeletons.vrm1_twist), (i) => nodes[i])
  const named = (n: string) => nodes[characters.vrm1_twist.nodes.findIndex((x) => x.name === n)]
  const rest = new Map(['J_Roll_L_Hand', 'J_Roll_L_LowerArm'].map((n) => [n, named(n).quaternion.clone()]))
  // The left hand turned 90° about the arm (standard +X). The roll bones take
  // the hand's turn about their own X, a little under 90° as the hand's rest
  // sits some 6° off the forearm: the hand's roll bone all of it, the
  // forearm's half (weight 0.5).
  const evaluate = (pose: Pose) => {
    resetConstraints(list)
    apply(p, root, m, pose)
    root.updateMatrixWorld(true)
    runConstraints(list)
    return list.map((c) => c.node.quaternion.clone())
  }
  const twisted = evaluate({ leftHand: turn([1, 0, 0], 90) })
  const turned = (n: string) => MathUtils.radToDeg(rest.get(n)!.angleTo(named(n).quaternion))
  const hand = turned('J_Roll_L_Hand')
  assert.ok(hand > 80 && hand <= 90, `J_Roll_L_Hand ${hand}°`)
  assert.ok(Math.abs(turned('J_Roll_L_LowerArm') - hand / 2) < 0.5, `J_Roll_L_LowerArm ${turned('J_Roll_L_LowerArm')}° of ${hand}°`)
  // Another pose in between leaves nothing behind.
  evaluate(elbow)
  const again = evaluate({ leftHand: turn([1, 0, 0], 90) })
  // Component by component: the file's rotations are not exactly unit length, which angleTo assumes.
  const drift = Math.max(...again.flatMap((q, i) => q.toArray().map((x, k) => Math.abs(x - twisted[i].toArray()[k]))))
  assert.ok(drift < 1e-12, `drift ${drift}`)
})

// --- the skeleton's geometric conditions ------------------------------------------

/** A small humanoid at rest in a T pose, facing +Z, 1.7 m tall; change() may move it before it is mapped. */
function figure(change: (o: Record<string, Object3D>) => void = () => {}) {
  const root = new Object3D()
  const o: Record<string, Object3D> = {}
  const at: Record<string, [string | null, [number, number, number]]> = {
    hips: [null, [0, 1, 0]],
    spine: ['hips', [0, 0.2, 0]],
    head: ['spine', [0, 0.5, 0]],
    leftUpperArm: ['spine', [0.2, 0.3, 0]],
    leftLowerArm: ['leftUpperArm', [0.3, 0, 0]],
    leftHand: ['leftLowerArm', [0.25, 0, 0]],
    rightUpperArm: ['spine', [-0.2, 0.3, 0]],
    rightLowerArm: ['rightUpperArm', [-0.3, 0, 0]],
    rightHand: ['rightLowerArm', [-0.25, 0, 0]],
    leftUpperLeg: ['hips', [0.1, 0, 0]],
    leftLowerLeg: ['leftUpperLeg', [0, -0.45, 0]],
    leftFoot: ['leftLowerLeg', [0, -0.45, 0]],
    rightUpperLeg: ['hips', [-0.1, 0, 0]],
    rightLowerLeg: ['rightUpperLeg', [0, -0.45, 0]],
    rightFoot: ['rightLowerLeg', [0, -0.45, 0]],
  }
  for (const [b, [parent, pos]] of Object.entries(at)) {
    o[b] = new Object3D()
    o[b].position.set(...pos)
    ;(parent ? o[parent] : root).add(o[b])
  }
  change(o)
  root.updateMatrixWorld(true)
  return { root, m: o as Mapping }
}

test('a skeleton with no facing or no limb direction is not humanoid; uneven scale only warns', () => {
  const ok = figure()
  assert.deepEqual(checkGeometry(ok.root, ok.m), { problem: null, nonUniform: [] })
  assert.ok(prepare(ok.root, ok.m))

  const cases: [string, (o: Record<string, Object3D>) => void, string, string | undefined][] = [
    ['head on the hips', (o) => void (o.spine.position.set(0, 0, 0), o.head.position.set(0, 0, 0)), 'facing', undefined],
    ['upper arms one above the other', (o) => void (o.leftUpperArm.position.set(0, 0.3, 0.001), o.rightUpperArm.position.set(0, 0.1, 0)), 'facing', undefined],
    ['a lower arm of no length', (o) => void o.leftHand.position.set(0, 0, 0), 'direction', 'leftLowerArm'],
  ]
  for (const [name, change, reason, bone] of cases) {
    const f = figure(change)
    const g = checkGeometry(f.root, f.m)
    assert.equal(g.problem?.reason, reason, name)
    assert.equal(g.problem?.bone, bone, name)
    assert.equal(prepare(f.root, f.m), null, name)
  }

  const stretched = figure((o) => o.spine.scale.set(1, 1.5, 1))
  const g = checkGeometry(stretched.root, stretched.m)
  assert.equal(g.problem, null)
  assert.deepEqual(g.nonUniform, ['spine', 'head', 'leftUpperArm', 'leftLowerArm', 'leftHand', 'rightUpperArm', 'rightLowerArm', 'rightHand'])
  assert.ok(prepare(stretched.root, stretched.m))
})
