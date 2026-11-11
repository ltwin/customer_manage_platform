import type { components } from '../../api/schema'
import { humanBones, isHumanBone, requiredBones, standardAncestor, standardParent } from './bones.ts'
import { constraintOrder, jointParents, type JointParents } from './constraints.ts'

// The checks a rig must pass (data: 模型内容「骨架与人形映射」), in the order
// and with the field, reason and message of
// backend/internal/creativescene/rig.go's Validate: the server refuses what
// this refuses, and the shared vectors (humanoid_rig.gen.json) hold both to it.

export type Rig = components['schemas']['CreativeModelRig']
export type Skeleton = { joints: readonly { node: number; parent: number | null }[] }

export type RigReason =
  | 'no_skeleton'
  | 'unknown_bone'
  | 'missing_required'
  | 'not_joint'
  | 'duplicate_node'
  | 'hierarchy'
  | 'invalid'
  | 'limit'
  | 'source_is_node'
  | 'cycle'

/** The first problem with a rig: field is a path under "rig", as the server's details.field. */
export type RigProblem = { field: string; reason: RigReason; message: string }

const maxConstraints = 256
const rollAxes = ['X', 'Y', 'Z']
const aimAxes = ['PositiveX', 'NegativeX', 'PositiveY', 'NegativeY', 'PositiveZ', 'NegativeZ']

const problem = (field: string, reason: RigReason, message: string): RigProblem => ({ field, reason, message })

/** The first problem with rig on a model with this skeleton, or null when the server accepts it. */
export function checkRig(rig: Rig, skeleton: Skeleton | null | undefined): RigProblem | null {
  if (!skeleton) return problem('rig', 'no_skeleton', '模型没有骨架，不能带 rig')
  const parents = jointParents(skeleton)
  return (rig.humanoid && checkHumanoid(rig.humanoid, parents)) || checkConstraints(rig.constraints ?? [], parents)
}

function checkHumanoid(h: Readonly<Record<string, number>>, parents: JointParents): RigProblem | null {
  for (const b of Object.keys(h).sort()) {
    if (!isHumanBone(b)) return problem(`rig.humanoid.${b}`, 'unknown_bone', `${b} 不是标准人形骨骼名`)
  }
  for (const b of requiredBones) {
    if (!(b in h)) return problem(`rig.humanoid.${b}`, 'missing_required', `人形映射缺少必需骨骼 ${b}`)
  }
  const boneOf = new Map<number, string>()
  for (const b of humanBones) {
    const n = h[b]
    if (n === undefined) continue
    if (!parents.has(n)) return problem(`rig.humanoid.${b}`, 'not_joint', `${b} 映射的节点 ${n} 不是关节`)
    const other = boneOf.get(n)
    if (other) return problem(`rig.humanoid.${b}`, 'duplicate_node', `${b} 与 ${other} 映射到同一节点 ${n}`)
    boneOf.set(n, b)
  }
  for (const b of humanBones.slice(1)) {
    const n = h[b]
    if (n === undefined) continue
    const issue = embedding(b, n, boneOf, parents)
    if (issue) return problem(`rig.humanoid.${b}`, 'hierarchy', issue)
  }
  return null
}

/**
 * Why bone b on node n does not embed in the standard chain, or null when it
 * does: the nearest mapped joint above n must map a standard ancestor of b
 * (not necessarily its parent).
 */
function embedding(b: string, n: number, boneOf: ReadonlyMap<number, string>, parents: JointParents): string | null {
  let got = ''
  for (let a = parents.get(n); a != null; a = parents.get(a)) {
    const m = boneOf.get(a)
    if (m) {
      got = m
      break
    }
  }
  if (standardAncestor(got, b)) return null
  const parent = standardParent.get(b)
  return got === '' ? `${b} 上面没有映射过的骨骼，应在 ${parent} 之下` : `${b} 的上级是 ${got}，应为 ${parent} 或它的上级骨骼`
}

function checkConstraints(cs: NonNullable<Rig['constraints']>, parents: JointParents): RigProblem | null {
  if (cs.length > maxConstraints) return problem('rig.constraints', 'limit', `节点约束不能超过 ${maxConstraints} 条`)
  for (const [i, c] of cs.entries()) {
    const f = `rig.constraints[${i}]`
    if (c.kind !== 'roll' && c.kind !== 'aim' && c.kind !== 'rotation') return problem(`${f}.kind`, 'invalid', '约束种类须为 roll、aim 或 rotation')
    if (!(c.weight >= 0 && c.weight <= 1)) return problem(`${f}.weight`, 'invalid', '约束权重须在 0 到 1 之间')
    if ((c.kind === 'roll') !== (c.roll_axis !== undefined) || (c.roll_axis !== undefined && !rollAxes.includes(c.roll_axis))) {
      return problem(`${f}.roll_axis`, 'invalid', 'roll 约束须有 roll_axis（X、Y、Z），其他种类不能有')
    }
    if ((c.kind === 'aim') !== (c.aim_axis !== undefined) || (c.aim_axis !== undefined && !aimAxes.includes(c.aim_axis))) {
      return problem(`${f}.aim_axis`, 'invalid', 'aim 约束须有 aim_axis，其他种类不能有')
    }
  }
  const owned = new Set<number>()
  for (const [i, c] of cs.entries()) {
    const f = `rig.constraints[${i}].node`
    if (!parents.has(c.node)) return problem(f, 'not_joint', `被约束的节点 ${c.node} 不是关节`)
    if (owned.has(c.node)) return problem(f, 'duplicate_node', `节点 ${c.node} 有不止一条约束`)
    owned.add(c.node)
  }
  for (const [i, c] of cs.entries()) {
    const f = `rig.constraints[${i}].source`
    if (c.source === c.node) return problem(f, 'source_is_node', '约束的源不能是被约束的节点自己')
    if (!parents.has(c.source)) return problem(f, 'not_joint', `约束的源 ${c.source} 不是关节`)
  }
  if (!constraintOrder(cs, parents)) return problem('rig.constraints', 'cycle', '节点约束的依赖成环')
  return null
}

/**
 * The mapping with the optional bones the server would refuse taken out, as
 * the browser does before it submits (patterns: 标准人形骨骼「层级嵌入」): one
 * on a node that is not a joint, on a node another bone has (a required bone
 * keeps it), or not embedding in the standard chain. Checked top-down, each
 * against what is left. Required bones stay as they are, for checkRig to judge.
 */
export function pruneOptional(h: Readonly<Record<string, number>>, skeleton: Skeleton): { humanoid: Record<string, number>; dropped: RigProblem[] } {
  const parents = jointParents(skeleton)
  const required = new Set(requiredBones)
  const kept: Record<string, number> = { ...h }
  const dropped: RigProblem[] = []
  const drop = (b: string, reason: RigReason, message: string) => {
    delete kept[b]
    dropped.push(problem(`rig.humanoid.${b}`, reason, message))
  }
  const requiredNodes = new Map(requiredBones.filter((b) => h[b] !== undefined).map((b) => [h[b], b]))
  const taken = new Map<number, string>()
  for (const b of humanBones) {
    const n = kept[b]
    if (n === undefined || required.has(b)) continue
    const other = requiredNodes.get(n) ?? taken.get(n)
    if (!parents.has(n)) drop(b, 'not_joint', `${b} 映射的节点 ${n} 不是关节`)
    else if (other) drop(b, 'duplicate_node', `${b} 与 ${other} 映射到同一节点 ${n}`)
    else taken.set(n, b)
  }
  for (const b of humanBones.slice(1)) {
    const n = kept[b]
    if (n === undefined || required.has(b)) continue
    const boneOf = new Map(Object.entries(kept).map(([k, v]) => [v, k]))
    const issue = embedding(b, n, boneOf, parents)
    if (issue) drop(b, 'hierarchy', issue)
  }
  return { humanoid: kept, dropped }
}
