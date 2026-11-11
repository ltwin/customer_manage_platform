import { isHumanBone } from './bones.ts'
import type { Constraint } from './constraints.ts'

// Finding a skinned file's standard humanoid mapping and node constraints at
// import (design「导入时保留骨骼并回写节点下标」): VRM 1.0 or 0.x says it
// outright; otherwise the joints' names are read against the Mixamo and Unreal
// mannequin tables. Names are looked for among the skin joints only: a mesh
// node may share a bone's name (RobotExpressive's do). What this finds still
// goes through pruneOptional and checkRig; the photographer can correct it.

export type NodeJSON = { name?: string; children?: number[]; extensions?: Record<string, unknown> }
export type RigJSON = { nodes?: NodeJSON[]; skins?: { joints: number[] }[]; extensions?: Record<string, unknown> }

export type Detection = {
  /** Where the mapping came from; null when nothing was found. */
  via: 'vrm1' | 'vrm0' | 'mixamo' | 'ue' | null
  /** Standard bone → node index; empty when nothing was found. */
  humanoid: Record<string, number>
  constraints: Constraint[]
  /** VRMC_node_constraint entries left out: their node or source is not a joint (Epic 修订 v7). */
  droppedConstraints: { node: number; name: string }[]
}

const mixamoNames: Record<string, string> = {
  Hips: 'hips',
  Spine: 'spine',
  Spine1: 'chest',
  Spine2: 'upperChest',
  Neck: 'neck',
  Head: 'head',
  LeftEye: 'leftEye',
  RightEye: 'rightEye',
  ...Object.fromEntries(
    (['Left', 'Right'] as const).flatMap((S) => {
      const s = S.toLowerCase()
      return [
        [`${S}Shoulder`, `${s}Shoulder`],
        [`${S}Arm`, `${s}UpperArm`],
        [`${S}ForeArm`, `${s}LowerArm`],
        [`${S}Hand`, `${s}Hand`],
        [`${S}UpLeg`, `${s}UpperLeg`],
        [`${S}Leg`, `${s}LowerLeg`],
        [`${S}Foot`, `${s}Foot`],
        [`${S}ToeBase`, `${s}Toes`],
        [`${S}HandThumb1`, `${s}ThumbMetacarpal`],
        [`${S}HandThumb2`, `${s}ThumbProximal`],
        [`${S}HandThumb3`, `${s}ThumbDistal`],
        ...(['Index', 'Middle', 'Ring', 'Pinky'] as const).flatMap((f) => {
          const F = f === 'Pinky' ? 'Little' : f
          return [
            [`${S}Hand${f}1`, `${s}${F}Proximal`],
            [`${S}Hand${f}2`, `${s}${F}Intermediate`],
            [`${S}Hand${f}3`, `${s}${F}Distal`],
          ]
        }),
      ]
    }),
  ),
}

// Unreal's mannequin (Quaternius Universal Base Characters, the built-in bodies).
const ueNames: Record<string, string> = {
  pelvis: 'hips',
  spine_01: 'spine',
  spine_02: 'chest',
  spine_03: 'upperChest',
  neck_01: 'neck',
  Head: 'head',
  ...Object.fromEntries(
    (['l', 'r'] as const).flatMap((x) => {
      const s = x === 'l' ? 'left' : 'right'
      return [
        [`clavicle_${x}`, `${s}Shoulder`],
        [`upperarm_${x}`, `${s}UpperArm`],
        [`lowerarm_${x}`, `${s}LowerArm`],
        [`hand_${x}`, `${s}Hand`],
        [`thigh_${x}`, `${s}UpperLeg`],
        [`calf_${x}`, `${s}LowerLeg`],
        [`foot_${x}`, `${s}Foot`],
        [`ball_${x}`, `${s}Toes`],
        [`thumb_01_${x}`, `${s}ThumbMetacarpal`],
        [`thumb_02_${x}`, `${s}ThumbProximal`],
        [`thumb_03_${x}`, `${s}ThumbDistal`],
        ...(['index', 'middle', 'ring', 'pinky'] as const).flatMap((f) => {
          const F = f === 'pinky' ? 'Little' : f[0].toUpperCase() + f.slice(1)
          return [
            [`${f}_01_${x}`, `${s}${F}Proximal`],
            [`${f}_02_${x}`, `${s}${F}Intermediate`],
            [`${f}_03_${x}`, `${s}${F}Distal`],
          ]
        }),
      ]
    }),
  ),
}

// VRM 0.x counts the thumb from the proximal; VRM 1.0 from the metacarpal.
const vrm0Thumbs: Record<string, string> = {
  leftThumbProximal: 'leftThumbMetacarpal',
  leftThumbIntermediate: 'leftThumbProximal',
  rightThumbProximal: 'rightThumbMetacarpal',
  rightThumbIntermediate: 'rightThumbProximal',
}

type Obj = Record<string, unknown>
const isObj = (v: unknown): v is Obj => typeof v === 'object' && v !== null && !Array.isArray(v)
const isIndex = (v: unknown): v is number => Number.isInteger(v) && (v as number) >= 0

function vrmHumanoid(ext: Obj | undefined): Pick<Detection, 'via' | 'humanoid'> | null {
  const vrm1 = ext?.VRMC_vrm
  if (isObj(vrm1) && isObj(vrm1.humanoid) && isObj(vrm1.humanoid.humanBones)) {
    const humanoid: Record<string, number> = {}
    for (const [b, v] of Object.entries(vrm1.humanoid.humanBones)) if (isHumanBone(b) && isObj(v) && isIndex(v.node)) humanoid[b] = v.node
    return { via: 'vrm1', humanoid }
  }
  const vrm0 = ext?.VRM
  if (isObj(vrm0) && isObj(vrm0.humanoid) && Array.isArray(vrm0.humanoid.humanBones)) {
    const humanoid: Record<string, number> = {}
    for (const v of vrm0.humanoid.humanBones as unknown[]) {
      if (!isObj(v) || typeof v.bone !== 'string' || !isIndex(v.node)) continue
      const b = vrm0Thumbs[v.bone] ?? v.bone
      if (isHumanBone(b)) humanoid[b] = v.node
    }
    return { via: 'vrm0', humanoid }
  }
  return null
}

/** The mapping a name table gives the joints, the first joint (by index) taking a name. */
function byNames(nodes: readonly NodeJSON[], joints: readonly number[], name: (n: string) => string | undefined): Record<string, number> {
  const humanoid: Record<string, number> = {}
  for (const j of joints) {
    const b = name(nodes[j]?.name ?? '')
    if (b && humanoid[b] === undefined) humanoid[b] = j
  }
  return humanoid
}

const mixamoBone = (n: string) => {
  const bare = /^mixamorig\d*[:_]?(.*)$/.exec(n)?.[1]
  return bare === undefined ? undefined : mixamoNames[bare]
}
const ueBone = (n: string) => (Object.hasOwn(ueNames, n) ? ueNames[n] : undefined)

const axes = { roll: 'rollAxis', aim: 'aimAxis' } as const

function constraintsOf(nodes: readonly NodeJSON[], joints: ReadonlySet<number>): Pick<Detection, 'constraints' | 'droppedConstraints'> {
  const constraints: Constraint[] = []
  const droppedConstraints: Detection['droppedConstraints'] = []
  nodes.forEach((n, node) => {
    const ext = n.extensions?.VRMC_node_constraint
    const def = isObj(ext) && isObj(ext.constraint) ? ext.constraint : null
    if (!def) return
    for (const kind of ['roll', 'aim', 'rotation'] as const) {
      const c = def[kind]
      if (!isObj(c)) continue
      if (!joints.has(node) || !isIndex(c.source) || !joints.has(c.source)) {
        droppedConstraints.push({ node, name: n.name ?? '' })
        return
      }
      const out: Constraint = { node, kind, source: c.source, weight: typeof c.weight === 'number' ? c.weight : 1 }
      if (kind !== 'rotation') {
        const axis = c[axes[kind]]
        if (kind === 'roll') out.roll_axis = axis as Constraint['roll_axis']
        else out.aim_axis = axis as Constraint['aim_axis']
      }
      constraints.push(out)
      return
    }
  })
  return { constraints, droppedConstraints }
}

/** A skeleton summary by the file's node indices, as the server works it out of the stored GLB (data: 模型内容 manifest.skeleton). */
export type FileSkeleton = { joints: { node: number; parent: number | null; name: string }[] }

/** The union of the skins' joints by node index, each with its nearest joint ancestor; null without skins. */
export function skeletonOf(json: RigJSON): FileSkeleton | null {
  const joints = new Set((json.skins ?? []).flatMap((s) => s.joints))
  if (joints.size === 0) return null
  const nodes = json.nodes ?? []
  const parents = new Map<number, number>()
  nodes.forEach((n, i) => n.children?.forEach((c) => parents.set(c, i)))
  return {
    joints: [...joints]
      .sort((a, b) => a - b)
      .map((node) => {
        let parent = parents.get(node)
        while (parent !== undefined && !joints.has(parent)) parent = parents.get(parent)
        return { node, parent: parent ?? null, name: nodes[node]?.name ?? '' }
      }),
  }
}

/** The mapping and constraints found in a skinned file's JSON. */
export function detectRig(json: RigJSON): Detection {
  const nodes = json.nodes ?? []
  const jointSet = new Set((json.skins ?? []).flatMap((s) => s.joints))
  const joints = [...jointSet].sort((a, b) => a - b)
  const constraints = constraintsOf(nodes, jointSet)
  const vrm = vrmHumanoid(json.extensions)
  if (vrm) return { ...vrm, ...constraints }
  // A table counts only when it finds the hips: a lone common name (Head) says nothing.
  const found = (
    [
      ['mixamo', byNames(nodes, joints, mixamoBone)],
      ['ue', byNames(nodes, joints, ueBone)],
    ] as const
  ).filter(([, h]) => h.hips !== undefined)
  const best = found.reduce<(typeof found)[number] | null>((a, b) => (!a || Object.keys(b[1]).length > Object.keys(a[1]).length ? b : a), null)
  return best ? { via: best[0], humanoid: best[1], ...constraints } : { via: null, humanoid: {}, ...constraints }
}
