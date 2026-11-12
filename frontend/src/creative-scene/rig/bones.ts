// The standard humanoid (patterns: 标准人形骨骼): VRM 1.0's 55 bones and their
// chain. backend/internal/creativescene/rig.go keeps the same list in the same
// order, so both report the same first problem for the shared vectors.

const fingerJoints: readonly [string, readonly string[]][] = [
  ['Thumb', ['Metacarpal', 'Proximal', 'Distal']],
  ...['Index', 'Middle', 'Ring', 'Little'].map((f): [string, readonly string[]] => [f, ['Proximal', 'Intermediate', 'Distal']]),
]

const fingers = (s: 'left' | 'right') => fingerJoints.flatMap(([f, joints]) => joints.map((j) => `${s}${f}${j}`))

/** Every standard bone, parents before children: torso and head, legs, arms, fingers, left before right. */
export const humanBones: readonly string[] = [
  'hips',
  'spine',
  'chest',
  'upperChest',
  'neck',
  'head',
  'leftEye',
  'rightEye',
  'jaw',
  ...(['left', 'right'] as const).flatMap((s) => ['UpperLeg', 'LowerLeg', 'Foot', 'Toes'].map((b) => s + b)),
  ...(['left', 'right'] as const).flatMap((s) => ['Shoulder', 'UpperArm', 'LowerArm', 'Hand'].map((b) => s + b)),
  ...fingers('left'),
  ...fingers('right'),
]

/** Each standard bone's parent on the standard chain; hips has none. */
export const standardParent: ReadonlyMap<string, string | null> = (() => {
  const p = new Map<string, string | null>([
    ['hips', null],
    ['spine', 'hips'],
    ['chest', 'spine'],
    ['upperChest', 'chest'],
    ['neck', 'upperChest'],
    ['head', 'neck'],
    ['leftEye', 'head'],
    ['rightEye', 'head'],
    ['jaw', 'head'],
  ])
  const chain = (first: string, bones: string[]) => bones.forEach((b, i) => p.set(b, i === 0 ? first : bones[i - 1]))
  for (const s of ['left', 'right']) {
    chain('hips', ['UpperLeg', 'LowerLeg', 'Foot', 'Toes'].map((b) => s + b))
    chain('upperChest', ['Shoulder', 'UpperArm', 'LowerArm', 'Hand'].map((b) => s + b))
  }
  for (const s of ['left', 'right'] as const) {
    const fs = fingers(s)
    for (let i = 0; i < fs.length; i += 3) chain(`${s}Hand`, fs.slice(i, i + 3))
  }
  return p
})()

export const requiredBones: readonly string[] = [
  'hips',
  'spine',
  'head',
  ...(['left', 'right'] as const).flatMap((s) => ['UpperArm', 'LowerArm', 'Hand', 'UpperLeg', 'LowerLeg', 'Foot'].map((b) => s + b)),
]

export const isHumanBone = (name: string) => standardParent.has(name)

/** Whether a is a strict ancestor of b on the standard chain. */
export function standardAncestor(a: string, b: string): boolean {
  for (let p = standardParent.get(b); p; p = standardParent.get(p)) if (p === a) return true
  return false
}

/** The bone on the other side: leftHand ↔ rightHand; centre bones are their own. */
export const mirrorBone = (b: string) => (b.startsWith('left') ? `right${b.slice(4)}` : b.startsWith('right') ? `left${b.slice(5)}` : b)
