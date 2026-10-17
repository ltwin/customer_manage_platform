import * as THREE from 'three'
import { ADDITION, Brush, Evaluator, INTERSECTION, SUBTRACTION, type CSGOperation } from 'three-bvh-csg'

// The modelling library injected into geometry modules as `lib`. A published
// version never changes behaviour: modules baked with lib@1 must rebake to the
// same geometry. New capabilities go into lib@2, and every published version
// stays in the sandbox.

type Point2 = readonly [number, number] | THREE.Vector2
type Axis = 'x' | 'y' | 'z'

const slotPattern = /^[A-Za-z0-9_-]{1,64}$/
const maxCopies = 1000

function fail(fn: string, message: string): never {
  throw new TypeError(`lib.${fn}: ${message}`)
}

function finite(v: unknown): v is number {
  return typeof v === 'number' && Number.isFinite(v)
}

function copies(fn: string, count: unknown): number {
  if (!Number.isInteger(count) || (count as number) < 1 || (count as number) > maxCopies) {
    fail(fn, `count 须为 1 到 ${maxCopies} 的整数`)
  }
  return count as number
}

function points(fn: string, pts: unknown): THREE.Vector2[] {
  if (!Array.isArray(pts) || pts.length < 2) fail(fn, '轮廓须为至少 2 个二维点')
  return pts.map((p: unknown) => {
    if (p instanceof THREE.Vector2 && finite(p.x) && finite(p.y)) return p.clone()
    if (Array.isArray(p) && p.length === 2 && finite(p[0]) && finite(p[1])) return new THREE.Vector2(p[0], p[1])
    return fail(fn, '点须为 [x, y] 或 THREE.Vector2')
  })
}

function mesh(fn: string, v: unknown): THREE.Mesh {
  if (!(v instanceof THREE.Mesh) || !(v.geometry instanceof THREE.BufferGeometry)) fail(fn, '参数须为 THREE.Mesh')
  return v
}

// A brush carries the mesh's world transform; the geometry is cloned so the
// operation never changes a geometry the module still uses.
function brush(m: THREE.Mesh): Brush {
  m.updateWorldMatrix(true, false)
  const b = new Brush(m.geometry.clone(), m.material)
  m.matrixWorld.decompose(b.position, b.quaternion, b.scale)
  b.updateMatrixWorld()
  return b
}

function boolean(fn: string, operation: CSGOperation, a: unknown, b: unknown): THREE.Mesh {
  const ba = brush(mesh(fn, a))
  const bb = brush(mesh(fn, b))
  const evaluator = new Evaluator()
  evaluator.attributes = ['position', 'normal', 'uv'].filter(
    (name) => ba.geometry.hasAttribute(name) && bb.geometry.hasAttribute(name),
  )
  if (!evaluator.attributes.includes('position')) fail(fn, '几何缺少 position')
  const result = evaluator.evaluate(ba, bb, operation)
  // The result is in brush A's local frame; bake A's transform so the mesh
  // sits at the origin with both operands where they were in the world.
  result.geometry.applyMatrix4(result.matrixWorld)
  return new THREE.Mesh(result.geometry, result.material)
}

const following: Record<Axis, THREE.Vector3> = {
  x: new THREE.Vector3(0, 1, 0),
  y: new THREE.Vector3(0, 0, 1),
  z: new THREE.Vector3(1, 0, 0),
}
const axes: Record<Axis, THREE.Vector3> = {
  x: new THREE.Vector3(1, 0, 0),
  y: new THREE.Vector3(0, 1, 0),
  z: new THREE.Vector3(0, 0, 1),
}

function object3D(fn: string, v: unknown): THREE.Object3D {
  if (!(v instanceof THREE.Object3D)) fail(fn, '参数须为 THREE.Object3D')
  return v
}

export function createLib1() {
  const materials = new Map<string, THREE.MeshStandardMaterial>()
  return Object.freeze({
    /** `slot:<slot>` material; the same slot returns the same material within one compile. */
    material(slot: string, hint?: string): THREE.MeshStandardMaterial {
      if (typeof slot !== 'string' || !slotPattern.test(slot)) fail('material', '槽位名须为 1–64 个字母、数字、下划线或连字符')
      if (hint !== undefined && typeof hint !== 'string') fail('material', 'hint 须为字符串')
      let m = materials.get(slot)
      if (!m) {
        m = new THREE.MeshStandardMaterial({ name: `slot:${slot}` })
        if (hint !== undefined) m.userData.hint = hint
        materials.set(slot, m)
      }
      return m
    },
    subtract: (a: THREE.Mesh, b: THREE.Mesh) => boolean('subtract', SUBTRACTION, a, b),
    union: (a: THREE.Mesh, b: THREE.Mesh) => boolean('union', ADDITION, a, b),
    intersect: (a: THREE.Mesh, b: THREE.Mesh) => boolean('intersect', INTERSECTION, a, b),
    /**
     * Extrudes along +Z from 0 to depth. `bevel` is the bevel size in metres
     * (thickness and size alike, 2 segments); 0 or omitted means no bevel.
     */
    extrude(shape: THREE.Shape | Point2[], depth: number, options: { bevel?: number } = {}): THREE.BufferGeometry {
      if (!finite(depth) || depth <= 0) fail('extrude', 'depth 须大于 0')
      const bevel = options.bevel ?? 0
      if (!finite(bevel) || bevel < 0) fail('extrude', 'bevel 须为不小于 0 的数')
      const s = shape instanceof THREE.Shape ? shape : new THREE.Shape(points('extrude', shape))
      return new THREE.ExtrudeGeometry(s, {
        depth,
        bevelEnabled: bevel > 0,
        bevelThickness: bevel,
        bevelSize: bevel,
        bevelSegments: 2,
      })
    },
    /** Revolves a 2D profile [x, y] around the Y axis. */
    lathe(profile: Point2[], segments = 32): THREE.BufferGeometry {
      if (!Number.isInteger(segments) || segments < 3 || segments > 512) fail('lathe', 'segments 须为 3 到 512 的整数')
      return new THREE.LatheGeometry(points('lathe', profile), segments)
    },
    /** `count` copies; copy i is offset by i × step from the original's position. */
    array(object: THREE.Object3D, count: number, step: readonly [number, number, number]): THREE.Group {
      const source = object3D('array', object)
      const n = copies('array', count)
      if (!Array.isArray(step) || step.length !== 3 || !step.every(finite)) fail('array', 'step 须为 [x, y, z]')
      const offset = new THREE.Vector3(step[0], step[1], step[2])
      const group = new THREE.Group()
      for (let i = 0; i < n; i++) {
        const copy = source.clone()
        copy.position.addScaledVector(offset, i)
        group.add(copy)
      }
      return group
    },
    /**
     * `count` copies evenly around `axis` through the origin. Each copy is
     * pushed `radius` along the next axis (x→+Y, y→+Z, z→+X) and turned with
     * its angle, so copy 0 of a Y ring sits at +Z facing outward.
     */
    radial(object: THREE.Object3D, count: number, radius: number, axis: Axis = 'y'): THREE.Group {
      const source = object3D('radial', object)
      const n = copies('radial', count)
      if (!finite(radius) || radius < 0) fail('radial', 'radius 须为不小于 0 的数')
      if (axis !== 'x' && axis !== 'y' && axis !== 'z') fail('radial', "axis 须为 'x'、'y' 或 'z'")
      const group = new THREE.Group()
      const turn = new THREE.Quaternion()
      for (let i = 0; i < n; i++) {
        turn.setFromAxisAngle(axes[axis], (2 * Math.PI * i) / n)
        const copy = source.clone()
        copy.position.addScaledVector(following[axis], radius).applyQuaternion(turn)
        copy.quaternion.premultiply(turn)
        group.add(copy)
      }
      return group
    },
  })
}

export type Lib1 = ReturnType<typeof createLib1>
