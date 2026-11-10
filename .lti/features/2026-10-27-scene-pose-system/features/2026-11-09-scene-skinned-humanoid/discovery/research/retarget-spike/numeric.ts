// Spike ①: the retargeting checked in numbers, on real characters and on
// variants of one made to differ in a single way each.
import { readFileSync } from 'node:fs'
import * as THREE from './three.ts'
import { apply, mirror, mixamoMapping, prepare, setPalmTwist, standardOffset, standardParent, tableMapping, topDown, turn, type Character, type Mapping, type Pose } from './retarget.ts'

const dir = process.argv[2]!

type Node = { name?: string; children?: number[]; translation?: number[]; rotation?: number[]; scale?: number[]; matrix?: number[] }
type GLTF = { nodes: Node[]; scenes: { nodes: number[] }[]; scene?: number; skins?: { joints: number[] }[]; extensions?: Record<string, any> }

function readGLB(file: string): GLTF {
  const b = readFileSync(`${dir}/${file}`)
  return JSON.parse(b.subarray(20, 20 + b.readUInt32LE(12)).toString('utf8'))
}

/** The node tree as Object3Ds, by index; joints marked. */
function tree(j: GLTF): { root: THREE.Object3D; byIndex: THREE.Object3D[] } {
  const joints = new Set((j.skins ?? []).flatMap((s) => s.joints))
  const byIndex = j.nodes.map((n, i) => {
    const o = new THREE.Object3D()
    o.name = n.name ?? `node${i}`
    o.userData = { gltfName: n.name ?? '', index: i, joint: joints.has(i) }
    if (n.matrix) new THREE.Matrix4().fromArray(n.matrix).decompose(o.position, o.quaternion, o.scale)
    if (n.translation) o.position.fromArray(n.translation)
    if (n.rotation) o.quaternion.fromArray(n.rotation)
    if (n.scale) o.scale.fromArray(n.scale)
    return o
  })
  j.nodes.forEach((n, i) => n.children?.forEach((c) => byIndex[i]!.add(byIndex[c]!)))
  const root = new THREE.Object3D()
  for (const r of j.scenes[j.scene ?? 0]!.nodes) root.add(byIndex[r]!)
  root.updateMatrixWorld(true)
  return { root, byIndex }
}

const jointsOnly = (m: Mapping): Mapping => Object.fromEntries(Object.entries(m).filter(([, o]) => o!.userData.joint))

function vrmMapping(j: GLTF, byIndex: THREE.Object3D[]): Mapping {
  if (j.extensions?.VRMC_vrm) return Object.fromEntries(Object.entries(j.extensions.VRMC_vrm.humanoid.humanBones as Record<string, { node: number }>).map(([b, { node }]) => [b, byIndex[node]!]))
  const vrm0: Record<string, string> = { leftThumbProximal: 'leftThumbMetacarpal', leftThumbIntermediate: 'leftThumbProximal', rightThumbProximal: 'rightThumbMetacarpal', rightThumbIntermediate: 'rightThumbProximal' }
  return Object.fromEntries((j.extensions!.VRM.humanoid.humanBones as { bone: string; node: number }[]).map(({ bone, node }) => [vrm0[bone] ?? bone, byIndex[node]!]))
}

const robotTable = (hipsIsBody: boolean): Record<string, string> => ({
  ...(hipsIsBody ? { Body: 'hips', Hips: 'spine', Abdomen: 'chest', Torso: 'upperChest' } : { Hips: 'hips', Abdomen: 'spine', Torso: 'chest' }),
  Neck: 'neck', Head: 'head',
  ...Object.fromEntries(['L', 'R'].flatMap((x) => {
    const s = x === 'L' ? 'left' : 'right'
    return [
      [`Shoulder.${x}`, `${s}Shoulder`], [`UpperArm.${x}`, `${s}UpperArm`], [`LowerArm.${x}`, `${s}LowerArm`], [`Palm2.${x}`, `${s}Hand`],
      [`Middle1.${x}`, `${s}MiddleProximal`], [`Middle2.${x}`, `${s}MiddleIntermediate`], [`Index.${x}`, `${s}IndexProximal`], [`Index2.${x}`, `${s}IndexIntermediate`],
      [`Ring1.${x}`, `${s}RingProximal`], [`Ring2.${x}`, `${s}RingIntermediate`], [`Thumb.${x}`, `${s}ThumbProximal`], [`Thumb2.${x}`, `${s}ThumbDistal`],
      [`UpperLeg.${x}`, `${s}UpperLeg`], [`LowerLeg.${x}`, `${s}LowerLeg`], [`Foot.${x}`, `${s}Foot`],
    ]
  })),
})

/** Where the standard chain and the node tree disagree: a mapped bone whose nearest mapped ancestor is not a standard ancestor. */
function hierarchyIssues(m: Mapping): string[] {
  const boneOf = new Map(Object.entries(m).map(([b, o]) => [o!, b]))
  const issues: string[] = []
  for (const [b, o] of Object.entries(m)) {
    if (b === 'hips') continue
    let p = o!.parent
    while (p && !boneOf.has(p)) p = p.parent
    const got = p ? boneOf.get(p)! : null
    const ancestors: string[] = []
    for (let a = standardParent[b]; a; a = standardParent[a]!) ancestors.push(a)
    if (!got || !ancestors.includes(got)) issues.push(`${b} under ${got ?? 'no mapped bone'}`)
  }
  return issues
}

// --- the poses ---------------------------------------------------------------
const poses: Record<string, Pose> = {
  'T pose': {},
  'left arm forward': { leftUpperArm: turn([0, 1, 0], -90) },
  'left elbow bent up': { leftUpperArm: turn([0, 1, 0], -90), leftLowerArm: turn([0, 0, 1], 90) },
  'head turned left 45°': { head: turn([0, 1, 0], 45) },
  'mirror of elbow bent': mirror({ leftUpperArm: turn([0, 1, 0], -90), leftLowerArm: turn([0, 0, 1], 90) }),
  'spine 30° forward, left thigh forward': { spine: turn([1, 0, 0], 30), leftUpperLeg: turn([1, 0, 0], -90) },
}
/** What each pose should give: a bone's direction in standard space. */
const expected: Record<string, [string, string, [number, number, number]][]> = {
  'T pose': [['leftUpperArm', 'leftLowerArm', [1, 0, 0]], ['leftLowerArm', 'leftHand', [1, 0, 0]], ['rightUpperArm', 'rightLowerArm', [-1, 0, 0]], ['leftUpperLeg', 'leftLowerLeg', [0, -1, 0]], ['leftLowerLeg', 'leftFoot', [0, -1, 0]]],
  'left arm forward': [['leftUpperArm', 'leftLowerArm', [0, 0, 1]], ['leftLowerArm', 'leftHand', [0, 0, 1]], ['rightUpperArm', 'rightLowerArm', [-1, 0, 0]]],
  'left elbow bent up': [['leftUpperArm', 'leftLowerArm', [0, 0, 1]], ['leftLowerArm', 'leftHand', [0, 1, 0]]],
  'mirror of elbow bent': [['rightUpperArm', 'rightLowerArm', [0, 0, 1]], ['rightLowerArm', 'rightHand', [0, 1, 0]], ['leftUpperArm', 'leftLowerArm', [1, 0, 0]]],
  'spine 30° forward, left thigh forward': [['leftUpperLeg', 'leftLowerLeg', [0, 0, 1]], ['rightUpperLeg', 'rightLowerLeg', [0, -1, 0]]],
  'head turned left 45°': [],
}

const angle = (a: THREE.Vector3, b: THREE.Vector3) => THREE.MathUtils.radToDeg(a.angleTo(b))

function checkReal(name: string, c: Character) {
  const rows: string[] = []
  let worst = 0
  for (const [pose, p] of Object.entries(poses)) {
    apply(c, p)
    for (const [from, to, want] of expected[pose]!) {
      if (!c.mapping[from] || !c.mapping[to]) continue
      const e = angle(standardOffset(c, c.mapping[to]!, c.mapping[from]!), new THREE.Vector3(...want))
      worst = Math.max(worst, e)
      if (e > 1) rows.push(`    ${pose}: ${from} off by ${e.toFixed(1)}°`)
    }
  }
  // Head: the eyes' midpoint about the head, turned 45° to the left (+X) in the horizontal plane.
  let head = '—'
  if (c.mapping.leftEye && c.mapping.rightEye) {
    const look = () => {
      const l = standardOffset(c, c.mapping.leftEye!, c.mapping.head!)
      const r = standardOffset(c, c.mapping.rightEye!, c.mapping.head!)
      const mid = l.add(r).multiplyScalar(0.5)
      return THREE.MathUtils.radToDeg(Math.atan2(mid.x, mid.z))
    }
    apply(c, {})
    const before = look()
    apply(c, poses['head turned left 45°']!)
    head = `${(look() - before).toFixed(1)}°`
  }
  // The palm at the T pose: index × little about the hand, in standard space; down is (0, −1, 0).
  apply(c, {})
  let palm = '—'
  if (c.mapping.leftIndexProximal && c.mapping.leftLittleProximal) {
    const i = standardOffset(c, c.mapping.leftIndexProximal, c.mapping.leftHand!)
    const l = standardOffset(c, c.mapping.leftLittleProximal, c.mapping.leftHand!)
    const n = new THREE.Vector3().crossVectors(i, l).normalize()
    palm = `palm normal (${n.x.toFixed(2)}, ${n.y.toFixed(2)}, ${n.z.toFixed(2)})`
  } else if (c.mapping.leftIndexProximal && c.mapping.leftRingProximal) {
    const i = standardOffset(c, c.mapping.leftIndexProximal, c.mapping.leftHand!)
    const l = standardOffset(c, c.mapping.leftRingProximal, c.mapping.leftHand!)
    const n = new THREE.Vector3().crossVectors(i, l).normalize()
    palm = `palm normal (index × ring) (${n.x.toFixed(2)}, ${n.y.toFixed(2)}, ${n.z.toFixed(2)})`
  }
  const fwd = new THREE.Vector3(0, 0, 1).applyQuaternion(c.facing)
  console.log(`${name}: ${Object.keys(c.mapping).length} bones; faces (${fwd.x.toFixed(2)}, ${fwd.y.toFixed(2)}, ${fwd.z.toFixed(2)}) in its world; worst limb direction ${worst.toFixed(2)}°; head turn ${head}; ${palm}`)
  console.log(`    T correction: ${Object.entries(c.corrected).filter(([b]) => b.startsWith('left')).map(([b, d]) => `${b} ${d}°`).join(', ') || 'none'}`)
  const issues = hierarchyIssues(c.mapping)
  if (issues.length) console.log(`    hierarchy: ${issues.join('; ')}`)
  rows.forEach((r) => console.log(r))
}

// --- real characters ---------------------------------------------------------
const characters: [string, () => { root: THREE.Object3D; mapping: Mapping }][] = [
  ['Xbot (Mixamo)', () => { const t = tree(readGLB('Xbot.glb')); return { root: t.root, mapping: jointsOnly(mixamoMapping(t.root)) } }],
  ['Michelle (Mixamo, Z-up root)', () => { const t = tree(readGLB('Michelle.glb')); return { root: t.root, mapping: jointsOnly(mixamoMapping(t.root)) } }],
  ['VRM 1.0 twist sample', () => { const j = readGLB('VRM1_Constraint_Twist_Sample.vrm'); const t = tree(j); return { root: t.root, mapping: vrmMapping(j, t.byIndex) } }],
  ['VRM 0.x girl (faces −Z)', () => { const j = readGLB('three-vrm-girl.vrm'); const t = tree(j); return { root: t.root, mapping: vrmMapping(j, t.byIndex) } }],
  ['RobotExpressive, by hand (Hips = hips)', () => { const t = tree(readGLB('RobotExpressive.glb')); return { root: t.root, mapping: jointsOnly(tableMapping(t.root, robotTable(false))) } }],
  ['RobotExpressive, by hand (Body = hips)', () => { const t = tree(readGLB('RobotExpressive.glb')); return { root: t.root, mapping: jointsOnly(tableMapping(t.root, robotTable(true))) } }],
]
for (const twist of [false, true]) {
  setPalmTwist(twist)
  console.log(`== real characters, ${twist ? 'with' : 'without'} the palm roll`)
  for (const [name, load] of characters) {
    const { root, mapping } = load()
    checkReal(name, prepare(root, mapping))
  }
}

// --- variants of Xbot --------------------------------------------------------
// Probes: a point 5 cm in front of and 3 cm above each mapped bone, fixed to it,
// so a bone's whole rotation shows, its roll included.
function withProbes(root: THREE.Object3D, m: Mapping) {
  const c = prepare(root, m)
  for (const [b, o] of Object.entries(m)) {
    const probe = new THREE.Object3D()
    probe.name = `probe:${b}`
    const world = worldPos(o!).add(new THREE.Vector3(0, 0.03, 0.05).applyQuaternion(c.facing))
    o!.add(probe)
    probe.position.copy(o!.worldToLocal(world))
  }
  root.updateMatrixWorld(true)
}
const worldPos = (o: THREE.Object3D) => o.getWorldPosition(new THREE.Vector3())

/** Every mapped bone's probe and tail in standard space about the hips, scaled by the hips' height, for each pose. */
function signature(root: THREE.Object3D, m: Mapping) {
  const c = prepare(root, m)
  const out: Record<string, THREE.Vector3[]> = {}
  for (const [pose, p] of Object.entries(poses)) {
    apply(c, p)
    out[pose] = topDown.filter((b) => m[b]).flatMap((b) => {
      const probe = m[b]!.children.find((ch) => ch.name === `probe:${b}`)!
      return [standardOffset(c, probe, m[b]!).normalize()]
    })
  }
  return out
}

function compare(name: string, base: Record<string, THREE.Vector3[]>, other: Record<string, THREE.Vector3[]>) {
  let worst = 0
  let where = ''
  for (const pose of Object.keys(base)) {
    base[pose]!.forEach((v, i) => {
      const e = angle(v, other[pose]![i]!)
      if (e > worst) [worst, where] = [e, pose]
    })
  }
  console.log(`${name}: every bone's whole rotation within ${worst.toFixed(3)}° of Xbot's${worst > 0.01 ? ` (worst in "${where}")` : ''}`)
}

function xbot() {
  const t = tree(readGLB('Xbot.glb'))
  const m = jointsOnly(mixamoMapping(t.root))
  withProbes(t.root, m)
  return { root: t.root, m }
}

const rng = (() => {
  let s = 12345
  return () => ((s = (s * 1103515245 + 12345) & 0x7fffffff) / 0x7fffffff)
})()
const randomQuat = () => new THREE.Quaternion().setFromEuler(new THREE.Euler(rng() * 6.28, rng() * 6.28, rng() * 6.28))

console.log('\n== variants of Xbot, each differing in one way')
const base = xbot()
const baseSig = signature(base.root, base.m)

{
  // A pose: the upper arms 45° down at rest, the mesh following (the skeleton is all there is here).
  const v = xbot()
  for (const [b, side] of [['leftUpperArm', 1], ['rightUpperArm', -1]] as const) {
    const o = v.m[b]!
    const c = prepare(v.root, v.m)
    const down = new THREE.Quaternion().setFromAxisAngle(new THREE.Vector3(0, 0, 1).applyQuaternion(c.facing), (-side * Math.PI) / 4)
    const parent = o.parent!.getWorldQuaternion(new THREE.Quaternion())
    o.quaternion.copy(parent.clone().invert().multiply(down).multiply(o.getWorldQuaternion(new THREE.Quaternion())))
    v.root.updateMatrixWorld(true)
  }
  compare('A pose at rest (upper arms 45° down)', baseSig, signature(v.root, v.m))
}
{
  // Every bone's local axes turned at random, the skeleton's shape unchanged.
  const v = xbot()
  v.root.traverse((o) => {
    if (!o.userData.joint) return
    const r = randomQuat()
    o.quaternion.multiply(r)
    const inv = r.clone().invert()
    for (const ch of o.children) {
      ch.position.applyQuaternion(inv)
      ch.quaternion.premultiply(inv)
    }
  })
  v.root.updateMatrixWorld(true)
  compare('local axes turned at random on every joint', baseSig, signature(v.root, v.m))
}
{
  // A twist helper between upper and lower arm, turned at random.
  const v = xbot()
  for (const s of ['left', 'right']) {
    const upper = v.m[`${s}UpperArm`]!
    const lower = v.m[`${s}LowerArm`]!
    const helper = new THREE.Object3D()
    helper.name = `${s}ArmTwist`
    upper.add(helper)
    helper.position.copy(lower.position).multiplyScalar(0.5)
    helper.quaternion.copy(randomQuat())
    v.root.updateMatrixWorld(true)
    helper.attach(lower)
  }
  v.root.updateMatrixWorld(true)
  compare('a twist helper bone inside each arm', baseSig, signature(v.root, v.m))
}
{
  // Longer arms and legs.
  const v = xbot()
  for (const b of ['leftLowerArm', 'rightLowerArm', 'leftHand', 'rightHand', 'leftLowerLeg', 'rightLowerLeg']) v.m[b]!.position.multiplyScalar(1.3)
  v.root.updateMatrixWorld(true)
  compare('arms and legs 30% longer', baseSig, signature(v.root, v.m))
}
{
  // Facing −Z, as VRM 0.x does, with a Z-up export's root turn on top.
  const v = xbot()
  const top = v.root.children[0]!
  top.quaternion.premultiply(new THREE.Quaternion().setFromAxisAngle(new THREE.Vector3(0, 1, 0), Math.PI)).premultiply(new THREE.Quaternion().setFromAxisAngle(new THREE.Vector3(1, 0, 0), -Math.PI / 2))
  v.root.updateMatrixWorld(true)
  compare('facing −Z under a Z-up root turn', baseSig, signature(v.root, v.m))
}
{
  // No upper chest and no fingers: the pose's upper chest goes on to the shoulders.
  const v = xbot()
  delete v.m.upperChest
  for (const b of Object.keys(v.m)) if (/(Thumb|Index|Middle|Ring|Little)/.test(b)) delete v.m[b]
  const c = prepare(v.root, v.m)
  apply(c, { upperChest: turn([0, 1, 0], 30) })
  const arm = standardOffset(c, v.m.leftLowerArm!, v.m.leftUpperArm!)
  console.log(`no upper chest: its 30° turn still reaches the arms, upper arm at ${THREE.MathUtils.radToDeg(Math.atan2(-arm.z, arm.x)).toFixed(1)}° (want 30°)`)
}
