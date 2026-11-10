// Quaternius Universal Base Characters through the spike ① retargeting, in numbers.
import { readFileSync } from 'node:fs'
import * as THREE from '../retarget-spike/three.ts'
import { apply, mirror, prepare, standardOffset, standardParent, tableMapping, turn, type Character, type Mapping, type Pose } from '../retarget-spike/retarget.ts'
import { ueTable } from './ue.ts'

type Node = { name?: string; children?: number[]; translation?: number[]; rotation?: number[]; scale?: number[] }
type GLTF = { nodes: Node[]; scenes: { nodes: number[] }[]; scene?: number; skins?: { joints: number[] }[] }

function tree(j: GLTF) {
  const joints = new Set((j.skins ?? []).flatMap((s) => s.joints))
  const byIndex = j.nodes.map((n, i) => {
    const o = new THREE.Object3D()
    o.name = n.name ?? `node${i}`
    o.userData = { gltfName: n.name ?? '', joint: joints.has(i) }
    if (n.translation) o.position.fromArray(n.translation)
    if (n.rotation) o.quaternion.fromArray(n.rotation)
    if (n.scale) o.scale.fromArray(n.scale)
    return o
  })
  j.nodes.forEach((n, i) => n.children?.forEach((c) => byIndex[i]!.add(byIndex[c]!)))
  const root = new THREE.Object3D()
  for (const r of j.scenes[j.scene ?? 0]!.nodes) root.add(byIndex[r]!)
  root.updateMatrixWorld(true)
  return root
}

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
    if (!got || !ancestors.includes(got)) issues.push(`${b} under ${got ?? 'none'}`)
  }
  return issues
}

const angle = (a: THREE.Vector3, b: THREE.Vector3) => THREE.MathUtils.radToDeg(a.angleTo(b))
const wp = (o: THREE.Object3D) => o.getWorldPosition(new THREE.Vector3())

const elbow: Pose = { leftUpperArm: turn([0, 1, 0], -90), leftLowerArm: turn([0, 0, 1], 90) }
// Left fingers point +X with the back of the hand up: curling them is about −Z.
const fist: Pose = Object.fromEntries(['Index', 'Middle', 'Ring', 'Little'].flatMap((f) => ['Proximal', 'Intermediate', 'Distal'].map((j) => [`left${f}${j}`, turn([0, 0, 1], -80)])))
const poses: [string, Pose, [string, string, [number, number, number]][]][] = [
  ['T pose', {}, [['leftUpperArm', 'leftLowerArm', [1, 0, 0]], ['leftLowerArm', 'leftHand', [1, 0, 0]], ['rightUpperArm', 'rightLowerArm', [-1, 0, 0]], ['leftUpperLeg', 'leftLowerLeg', [0, -1, 0]], ['leftLowerLeg', 'leftFoot', [0, -1, 0]]]],
  ['left arm forward, elbow up', elbow, [['leftUpperArm', 'leftLowerArm', [0, 0, 1]], ['leftLowerArm', 'leftHand', [0, 1, 0]]]],
  ['mirror', mirror(elbow), [['rightUpperArm', 'rightLowerArm', [0, 0, 1]], ['rightLowerArm', 'rightHand', [0, 1, 0]], ['leftUpperArm', 'leftLowerArm', [1, 0, 0]]]],
  ['spine forward, left thigh up', { spine: turn([1, 0, 0], 30), leftUpperLeg: turn([1, 0, 0], -90) }, [['leftUpperLeg', 'leftLowerLeg', [0, 0, 1]], ['rightUpperLeg', 'rightLowerLeg', [0, -1, 0]]]],
]

for (const who of ['Male', 'Female']) {
  const root = tree(JSON.parse(readFileSync(`${process.argv[2]}/Superhero_${who}_FullBody.gltf`, 'utf8')))
  const mapping = Object.fromEntries(Object.entries(tableMapping(root, ueTable)).filter(([, o]) => o!.userData.joint))
  const c: Character = prepare(root, mapping)
  const fwd = new THREE.Vector3(0, 0, 1).applyQuaternion(c.facing)
  console.log(`${who}: ${Object.keys(mapping).length} standard bones mapped of ${Object.keys(ueTable).length}; faces (${fwd.x.toFixed(2)}, ${fwd.y.toFixed(2)}, ${fwd.z.toFixed(2)})`)
  const hips = wp(mapping.hips!), head = wp(mapping.head!)
  const foot = wp(mapping.leftFoot!)
  console.log(`  height hips ${hips.y.toFixed(3)} head ${head.y.toFixed(3)} foot ${foot.y.toFixed(3)} (world units, metres)`)
  console.log(`  hierarchy issues: ${hierarchyIssues(mapping).join('; ') || 'none'}`)
  console.log(`  T correction (left): ${Object.entries(c.corrected).filter(([b]) => b.startsWith('left')).map(([b, d]) => `${b} ${d}°`).join(', ')}`)
  let worst = 0
  for (const [name, p, want] of poses) {
    apply(c, p)
    for (const [from, to, dir] of want) {
      const e = angle(standardOffset(c, mapping[to]!, mapping[from]!), new THREE.Vector3(...dir))
      worst = Math.max(worst, e)
      if (e > 1) console.log(`    ${name}: ${from} off by ${e.toFixed(1)}°`)
    }
  }
  console.log(`  worst limb direction ${worst.toFixed(2)}°`)
  apply(c, {})
  const i = standardOffset(c, mapping.leftIndexProximal!, mapping.leftHand!)
  const l = standardOffset(c, mapping.leftLittleProximal!, mapping.leftHand!)
  const n = new THREE.Vector3().crossVectors(i, l).normalize()
  console.log(`  T-pose back of left hand (index × little): (${n.x.toFixed(2)}, ${n.y.toFixed(2)}, ${n.z.toFixed(2)})`)
  // Fist: each finger's distal joint relative to the hand, in standard space; a curled finger's tip ends under the palm (−Y) and back toward the wrist.
  apply(c, fist)
  for (const f of ['Index', 'Middle', 'Ring', 'Little']) {
    const tip = standardOffset(c, mapping[`left${f}Distal`]!, mapping.leftHand!)
    const root = standardOffset(c, mapping[`left${f}Proximal`]!, mapping.leftHand!)
    const d = tip.sub(root)
    console.log(`    fist ${f}: distal joint from knuckle (${d.x.toFixed(3)}, ${d.y.toFixed(3)}, ${d.z.toFixed(3)})`)
  }
}
