import type { Object3D } from 'three'
import { humanBones } from '../rig/bones.ts'
import type { Constraint } from '../rig/constraints.ts'
import type { FileSkeleton } from '../rig/detect.ts'
import { checkRig, placementProblem, pruneOptional, type Rig, type RigProblem } from '../rig/humanoid.ts'
import { checkGeometry } from '../rig/retarget.ts'
import type { ImportRig } from './parse.ts'

// What the import dialog says of a skinned file's rig, and the rig it submits
// (design「导入时保留骨骼并回写节点下标」4–5): the mapping found, or set by
// hand, checked as the server will check it and on the rest skeleton's
// geometry; the file's constraints, kept when they pass. Everything here is by
// the file's node indices; write.ts follows them into the stored GLB.

/** Standard bone → file node index, as found or as set by hand; optional bones the server would refuse are still in it. */
export type Humanoid = Readonly<Record<string, number>>

export type MappingCheck = {
  /** The mapping as submitted: the refused optional bones taken out. */
  humanoid: Record<string, number>
  /** The optional bones taken out, and why. */
  dropped: RigProblem[]
  /** Why the mapping does not make a humanoid; null when it does. */
  problem: string | null
  /** Mapped bones that scale unevenly at rest: posing may distort them. */
  nonUniform: string[]
}

/** The mapping checked against the file's skeleton, and the scene's rest pose (root: the converted scene). */
export function checkMapping(rig: ImportRig, root: Object3D, h: Humanoid): MappingCheck {
  const { humanoid, dropped } = pruneOptional(h, rig.skeleton)
  if (Object.keys(humanoid).length === 0) return { humanoid, dropped, problem: '没有从骨骼名认出人形骨骼（能认 VRM、Mixamo、UE 小白人和 UpperLeg.L 这类通用命名）', nonUniform: [] }
  const refused = checkRig({ humanoid }, rig.skeleton)
  // Bones missing, where the mapped ones hang comes first: a skeleton that cannot embed is not mended by mapping more (AC-4).
  const misplaced = refused?.reason === 'missing_required' ? placementProblem(humanoid, rig.skeleton) : null
  if (misplaced) return { humanoid, dropped, problem: `${misplaced.message}（另外${refused!.message}）`, nonUniform: [] }
  if (refused) return { humanoid, dropped, problem: refused.message, nonUniform: [] }
  const geometry = checkGeometry(root, Object.fromEntries(Object.entries(humanoid).map(([b, n]) => [b, rig.objects.get(n)!])))
  return { humanoid, dropped, problem: geometry.problem?.message ?? null, nonUniform: geometry.nonUniform }
}

/** The file's constraints the server will take, and what was left out of them. */
export function fileConstraints(rig: ImportRig): { constraints: Constraint[]; notes: string[] } {
  const notes = rig.detection.droppedConstraints.map((c) => `节点约束 ${c.name || `节点 ${c.node}`} 的节点或源不是骨骼，已去掉`)
  const { constraints } = rig.detection
  const refused = constraints.length > 0 && checkRig({ constraints }, rig.skeleton)
  if (refused) return { constraints: [], notes: [...notes, `文件里的节点约束没有通过检查（${refused.message}），已全部去掉`] }
  return { constraints, notes }
}

/** The rig to submit, by the file's node indices; undefined when it says nothing. humanoid null: a non-humanoid skeleton. */
export function submittedRig(humanoid: Record<string, number> | null, constraints: readonly Constraint[]): Rig | undefined {
  const rig: Rig = {}
  if (humanoid) rig.humanoid = humanoid
  if (constraints.length > 0) rig.constraints = [...constraints]
  return rig.humanoid || rig.constraints ? rig : undefined
}

/** The joints in tree order, each with its depth among the joints: the choices of the manual mapping. */
export function jointTree(skeleton: FileSkeleton): { node: number; name: string; depth: number }[] {
  const children = new Map<number | null, FileSkeleton['joints'][number][]>()
  for (const j of skeleton.joints) children.set(j.parent, [...(children.get(j.parent) ?? []), j])
  const out: { node: number; name: string; depth: number }[] = []
  const walk = (parent: number | null, depth: number) => {
    for (const j of children.get(parent) ?? []) {
      out.push({ node: j.node, name: j.name || `节点 ${j.node}`, depth })
      walk(j.node, depth + 1)
    }
  }
  walk(null, 0)
  return out
}

/** The objects of the joints, to track through the export. */
export const jointObjects = (rig: ImportRig, objects: ReadonlyMap<number, Object3D> = rig.objects) => rig.skeleton.joints.map((j) => objects.get(j.node)!)

/** objects followed into copy, a clone of source (SkeletonUtils.clone keeps the tree's order). */
export function followClone(objects: ReadonlyMap<number, Object3D>, source: Object3D, copy: Object3D): Map<number, Object3D> {
  const to = new Map<Object3D, Object3D>()
  const walk = (a: Object3D, b: Object3D) => {
    to.set(a, b)
    a.children.forEach((c, i) => walk(c, b.children[i]!))
  }
  walk(source, copy)
  return new Map([...objects].map(([n, o]) => [n, to.get(o)!]))
}

const fingerBones = new Set(humanBones.slice(humanBones.indexOf('leftThumbMetacarpal')))

/** How many of the 30 finger bones the mapping has. */
export const fingerCount = (h: Humanoid) => Object.keys(h).filter((b) => fingerBones.has(b)).length
