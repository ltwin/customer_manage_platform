import { readFileSync } from 'node:fs'
import { Matrix4, Quaternion, Vector3 } from 'three'

// Skinned GLBs for the rig tests (scene-skinned-humanoid), made from JSON so
// every case says what it holds: the accessors go in one binary chunk in
// order, the first accessor index 0. The characters' node trees come from
// skeletons.json (names, transforms, skins' joints, VRM extensions only).

type Typed = Float32Array | Uint8Array | Uint16Array
const componentTypes = new Map<unknown, number>([
  [Float32Array, 5126],
  [Uint8Array, 5121],
  [Uint16Array, 5123],
])
const sizes: Record<string, number> = { SCALAR: 1, VEC2: 2, VEC3: 3, VEC4: 4, MAT4: 16 }

export type Accessor = { array: Typed; type: string; normalized?: boolean }

export type NodeJSON = {
  name?: string
  children?: number[]
  translation?: number[]
  rotation?: number[]
  scale?: number[]
  matrix?: number[]
  mesh?: number
  skin?: number
  extensions?: Record<string, unknown>
}

/** A GLB of json with each accessor's array in its own buffer view, in order; bufferViews and accessors in json are added to. */
export function glbOf(json: Record<string, unknown>, accessors: Accessor[]): ArrayBuffer {
  const chunks: Buffer[] = []
  const bufferViews: Record<string, unknown>[] = []
  let offset = 0
  const acc = accessors.map((a, i) => {
    const bytes = Buffer.from(a.array.buffer, a.array.byteOffset, a.array.byteLength)
    const pad = Buffer.alloc((4 - (bytes.length % 4)) % 4)
    bufferViews.push({ buffer: 0, byteOffset: offset, byteLength: bytes.length })
    chunks.push(bytes, pad)
    offset += bytes.length + pad.length
    const n = sizes[a.type]
    const out: Record<string, unknown> = { bufferView: i, componentType: componentTypes.get(a.array.constructor), count: a.array.length / n, type: a.type }
    if (a.normalized) out.normalized = true
    if (a.type === 'VEC3') {
      const lo = [0, 1, 2].map((c) => Math.min(...a.array.filter((_, k) => k % 3 === c)))
      const hi = [0, 1, 2].map((c) => Math.max(...a.array.filter((_, k) => k % 3 === c)))
      Object.assign(out, { min: lo, max: hi })
    }
    return out
  })
  const bin = Buffer.concat(chunks)
  const text = Buffer.from(JSON.stringify({ asset: { version: '2.0' }, scene: 0, ...json, accessors: acc, bufferViews, buffers: [{ byteLength: bin.length }] }))
  const jsonChunk = Buffer.concat([text, Buffer.alloc((4 - (text.length % 4)) % 4, 0x20)])
  const chunk = (body: Buffer, type: number) => {
    const h = Buffer.alloc(8)
    h.writeUInt32LE(body.length, 0)
    h.writeUInt32LE(type, 4)
    return Buffer.concat([h, body])
  }
  const body = Buffer.concat([chunk(jsonChunk, 0x4e4f534a), chunk(bin, 0x004e4942)])
  const header = Buffer.alloc(12)
  header.writeUInt32LE(0x46546c67, 0)
  header.writeUInt32LE(2, 4)
  header.writeUInt32LE(12 + body.length, 8)
  const out = Buffer.concat([header, body])
  return out.buffer.slice(out.byteOffset, out.byteOffset + out.byteLength) as ArrayBuffer
}

/** Each node's matrix in the scene, the way glTF composes them. */
export function globals(nodes: NodeJSON[], roots: number[]): Matrix4[] {
  const out: Matrix4[] = []
  const local = (n: NodeJSON) =>
    n.matrix
      ? new Matrix4().fromArray(n.matrix)
      : new Matrix4().compose(new Vector3(...(n.translation ?? [0, 0, 0])), new Quaternion(...(n.rotation ?? [0, 0, 0, 1])), new Vector3(...(n.scale ?? [1, 1, 1])))
  const walk = (i: number, parent: Matrix4) => {
    out[i] = parent.clone().multiply(local(nodes[i]))
    for (const c of nodes[i].children ?? []) walk(c, out[i])
  }
  for (const r of roots) walk(r, new Matrix4())
  return out
}

/** Inverse bind matrices that make the rest the identity skin: vertices are where their positions say. */
export const inverses = (ms: Matrix4[]) => new Float32Array(ms.flatMap((m) => m.clone().invert().toArray()))

export const quat = (axis: [number, number, number], deg: number) =>
  new Quaternion().setFromAxisAngle(new Vector3(...axis).normalize(), (deg * Math.PI) / 180).toArray() as [number, number, number, number]

export type Character = {
  source: string
  scene: number
  scenes: { nodes: number[] }[]
  nodes: NodeJSON[]
  skins: { joints: number[] }[]
  extensions?: Record<string, unknown>
}

export const characters = JSON.parse(readFileSync(new URL('skeletons.json', import.meta.url), 'utf8')) as Record<string, Character>

/**
 * A character's node tree with one skin over all its joints and a triangle
 * at node `at`, skinned wholly to it: enough of a model for the loader, the
 * converter and the exporter, the file's extensions kept.
 */
export function characterGLB(c: Character, at: number): ArrayBuffer {
  const joints = [...new Set(c.skins.flatMap((s) => s.joints))].sort((a, b) => a - b)
  const roots = c.scenes[c.scene].nodes
  const nodes: NodeJSON[] = [...structuredClone(c.nodes), { name: 'Mark', mesh: 0, skin: 0 }]
  const g = globals(nodes, roots)
  const p = new Vector3().setFromMatrixPosition(g[at])
  const j = joints.indexOf(at)
  return glbOf(
    {
      scenes: [{ nodes: [...roots, nodes.length - 1] }],
      nodes,
      meshes: [{ primitives: [{ attributes: { POSITION: 0, JOINTS_0: 1, WEIGHTS_0: 2 } }] }],
      skins: [{ joints, inverseBindMatrices: 3 }],
      ...(c.extensions ? { extensions: c.extensions, extensionsUsed: Object.keys(c.extensions) } : {}),
    },
    [
      { array: new Float32Array([p.x, p.y, p.z, p.x + 0.01, p.y, p.z, p.x, p.y + 0.01, p.z]), type: 'VEC3' },
      { array: new Uint16Array([j, 0, 0, 0, j, 0, 0, 0, j, 0, 0, 0]), type: 'VEC4' },
      { array: new Float32Array([1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0]), type: 'VEC4' },
      { array: inverses(joints.map((n) => g[n])), type: 'MAT4' },
    ],
  )
}
