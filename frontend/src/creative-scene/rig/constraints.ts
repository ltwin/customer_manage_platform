import { Matrix4, Quaternion, Vector3, type Object3D } from 'three'
import type { components } from '../../api/schema'

// Node constraints (VRMC_node_constraint 1.0's roll, aim and rotation, as
// three-vrm evaluates them), run after the pose and before skinning
// (patterns: 标准人形骨骼「求值顺序」), in the dependency order of data: 模型内容
// 「骨架与人形映射」. backend/internal/creativescene/rig.go judges loops by the
// same rule; the shared vectors check both.

export type Constraint = components['schemas']['CreativeModelConstraint']

/** Each joint's nearest joint ancestor, by node index (manifest.skeleton). */
export type JointParents = ReadonlyMap<number, number | null>

export const jointParents = (skeleton: { joints: readonly { node: number; parent: number | null }[] }): JointParents =>
  new Map(skeleton.joints.map((j) => [j.node, j.parent]))

/**
 * The indices of cs, each after the constraints it depends on, or null when
 * they loop. roll and rotation read their source's local rotation; aim reads
 * its source's world position and its node's parent's world transform, so it
 * also waits for constraints on their joint ancestors.
 */
export function constraintOrder(cs: readonly Constraint[], parents: JointParents): number[] | null {
  const owner = new Map(cs.map((c, i) => [c.node, i]))
  const deps = cs.map((c) => {
    const nodes = [c.source]
    if (c.kind === 'aim') {
      for (const from of [parents.get(c.source), parents.get(c.node)]) {
        for (let a = from; a != null; a = parents.get(a)) nodes.push(a)
      }
    }
    return nodes.flatMap((n) => (owner.has(n) ? [owner.get(n)!] : []))
  })
  const order: number[] = []
  const state = new Array<0 | 1 | 2>(cs.length).fill(0)
  const visit = (i: number): boolean => {
    if (state[i] === 2) return true
    if (state[i] === 1) return false
    state[i] = 1
    for (const d of deps[i]) if (!visit(d)) return false
    state[i] = 2
    order.push(i)
    return true
  }
  for (let i = 0; i < cs.length; i++) if (!visit(i)) return null
  return order
}

/** A constraint bound to the nodes of one skeleton instance. */
export type BoundConstraint = {
  readonly node: Object3D
  /** Puts the node back to its rest rotation. */
  reset(): void
  /** Sets the node's rotation from its source's current state. */
  update(): void
}

const axisOf = (a: string) =>
  new Vector3(a.endsWith('X') ? 1 : 0, a.endsWith('Y') ? 1 : 0, a.endsWith('Z') ? 1 : 0).multiplyScalar(a.startsWith('Negative') ? -1 : 1)

/**
 * Where o is in root's space: its matrix there, or its world matrix without a root. Aim works there, so a pose
 * comes out alike in every instance, whatever the object's scale (design「坐标空间」: instances share it).
 */
const inRoot = (o: Object3D | null, root: Object3D | null) => {
  const m = o ? o.matrixWorld.clone() : new Matrix4()
  return root ? m.premultiply(root.matrixWorld.clone().invert()) : m
}
const rotationOf = (m: Matrix4) => {
  const q = new Quaternion()
  m.decompose(new Vector3(), q, new Vector3())
  return q
}

function bind(c: Constraint, node: Object3D, source: Object3D, root: Object3D | null): BoundConstraint {
  const rest = node.quaternion.clone()
  const reset = () => void node.quaternion.copy(rest)
  const blend = (target: Quaternion) => node.quaternion.copy(rest).slerp(target, c.weight)
  switch (c.kind) {
    case 'roll': {
      const axis = axisOf(c.roll_axis ?? 'X')
      const invRest = rest.clone().invert()
      const invSourceRestRest = source.quaternion.clone().invert().multiply(rest)
      return {
        node,
        reset,
        update() {
          const delta = invRest.clone().multiply(source.quaternion).multiply(invSourceRestRest)
          const turned = axis.clone().applyQuaternion(delta)
          blend(new Quaternion().setFromUnitVectors(turned, axis).premultiply(rest).multiply(delta))
        },
      }
    }
    case 'aim': {
      const axis = axisOf(c.aim_axis ?? 'PositiveX')
      return {
        node,
        reset,
        update() {
          node.updateWorldMatrix(true, false)
          source.updateWorldMatrix(true, false)
          const parent = rotationOf(inRoot(node.parent, root))
          const from = axis.clone().applyQuaternion(rest).applyQuaternion(parent)
          const to = new Vector3().setFromMatrixPosition(inRoot(source, root)).sub(new Vector3().setFromMatrixPosition(inRoot(node, root))).normalize()
          blend(new Quaternion().setFromUnitVectors(from, to).premultiply(parent.clone().invert()).multiply(parent).multiply(rest))
        },
      }
    }
    case 'rotation': {
      const invSourceRest = source.quaternion.clone().invert()
      return {
        node,
        reset,
        update() {
          blend(rest.clone().multiply(invSourceRest.clone().multiply(source.quaternion)))
        },
      }
    }
  }
}

/**
 * The constraints bound to an instance's nodes, in evaluation order. The nodes
 * must be at rest: each constraint keeps its node's and source's rest
 * rotations. Throws when the constraints loop, which a stored rig never does.
 * root: the model's root, whose space aim works in; without it, the world's.
 */
export function bindConstraints(cs: readonly Constraint[], parents: JointParents, nodeOf: (index: number) => Object3D, root: Object3D | null = null): BoundConstraint[] {
  const order = constraintOrder(cs, parents)
  if (!order) throw new Error('node constraints loop')
  return order.map((i) => bind(cs[i], nodeOf(cs[i].node), nodeOf(cs[i].source), root))
}

/** Puts every constrained node back to rest, before a pose is applied. */
export function resetConstraints(list: readonly BoundConstraint[]) {
  for (const c of list) c.reset()
}

/** Runs the constraints in order, each node's world matrices updated for the next. */
export function runConstraints(list: readonly BoundConstraint[]) {
  for (const c of list) {
    c.update()
    c.node.updateMatrixWorld(true)
  }
}
