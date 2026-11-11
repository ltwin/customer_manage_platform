import type { Group, Object3D } from 'three'
import { GLTFExporter } from 'three/examples/jsm/exporters/GLTFExporter.js'
import type { components } from '../../api/schema'
import { GLBError, inspect, type Manifest } from '../sandbox/inspect.ts'
import { ImportRejected, readContainer, writeContainer } from './parse.ts'

// The converted scene as the GLB the store keeps (design「模块职责与协作」
// write): three's exporter, then the same static-subset check the server runs,
// so a file over a budget is refused here, with the reason, before anything
// is uploaded.
//
// The exporter numbers the nodes afresh, so what a rig names by node is
// followed through it (design「导入时保留骨骼并回写节点下标」): each such
// object is marked in userData, which the exporter writes as the node's
// extras; the marks are read back as the new indices and taken out again.

type Rig = components['schemas']['CreativeModelRig']

/** nodes: each tracked object's node index in glb. */
export type Written = { glb: ArrayBuffer; manifest: Manifest; nodes: ReadonlyMap<Object3D, number> }

const mark = 'ltiNode'

const limitWords: [RegExp, (m: RegExpMatchArray) => string][] = [
  [/file is (\d+) bytes, limit (\d+)/, (m) => `换算后的模型 ${mib(+m[1]!)} MiB，超过 ${mib(+m[2]!)} MiB`],
  [/more than (\d+) triangles/, (m) => `模型超过 ${m[1]} 个三角形`],
  [/(\d+) nodes, limit (\d+)/, (m) => `模型有 ${m[1]} 个节点，最多 ${m[2]} 个`],
  [/more than (\d+) primitive instances/, (m) => `模型的网格实例超过 ${m[1]} 个`],
  [/(\d+) material slots, limit (\d+)/, (m) => `模型有 ${m[1]} 种材质，最多 ${m[2]} 种`],
]
const mib = (bytes: number) => Math.round((bytes / 1024 / 1024) * 10) / 10

function limitMessage(detail: string): string {
  for (const [re, say] of limitWords) {
    const m = re.exec(detail)
    if (m) return say(m)
  }
  return `模型超出上限（${detail}）`
}

/** The GLB with the marks taken out of its nodes' extras, and the node each mark was on. */
function unmark(raw: ArrayBuffer): { glb: ArrayBuffer; found: Map<number, number> } {
  const { json, bin } = readContainer(new Uint8Array(raw))
  const found = new Map<number, number>()
  json.nodes?.forEach((n, i) => {
    const k = n.extras?.[mark]
    if (typeof k !== 'number') return
    found.set(k, i)
    delete n.extras![mark]
    if (Object.keys(n.extras!).length === 0) delete n.extras
  })
  return found.size === 0 ? { glb: raw, found } : { glb: writeContainer(json, bin), found }
}

/** Exports the scene; track names the objects whose node indices the caller needs. */
export async function writeGLB(scene: Group, track: readonly Object3D[] = []): Promise<Written> {
  track.forEach((o, k) => (o.userData[mark] = k))
  let raw: ArrayBuffer
  try {
    raw = (await new GLTFExporter().parseAsync(scene, { binary: true })) as ArrayBuffer
  } finally {
    for (const o of track) delete o.userData[mark]
  }
  const { glb, found } = unmark(raw)
  const nodes = new Map<Object3D, number>()
  track.forEach((o, k) => {
    const n = found.get(k)
    if (n === undefined) throw new ImportRejected('unreadable', `换算后找不到骨骼 ${o.name || k}`)
    nodes.set(o, n)
  })
  try {
    return { glb, manifest: inspect(glb), nodes }
  } catch (e) {
    if (!(e instanceof GLBError)) throw e
    if (e.reason === 'limit') throw new ImportRejected('limit', `${limitMessage(e.detail)}。请在 Blender 里减面或拆分后再导出`)
    // Anything else is the conversion's fault, not the file's.
    throw new ImportRejected('unreadable', `换算后的模型没有通过检查（${e.reason}: ${e.detail}）`)
  }
}

/** The objects a rig by the file's node indices names, to track through the export. */
export function rigObjects(rig: Rig, objects: ReadonlyMap<number, Object3D>): Object3D[] {
  const indices = [...Object.values(rig.humanoid ?? {}), ...(rig.constraints ?? []).flatMap((c) => [c.node, c.source])]
  return [...new Set(indices.map((i) => objects.get(i)!))]
}

/** The rig by the written GLB's node indices. */
export function writtenRig(rig: Rig, objects: ReadonlyMap<number, Object3D>, nodes: ReadonlyMap<Object3D, number>): Rig {
  const at = (i: number) => nodes.get(objects.get(i)!)!
  const out: Rig = {}
  if (rig.humanoid) out.humanoid = Object.fromEntries(Object.entries(rig.humanoid).map(([b, i]) => [b, at(i)]))
  if (rig.constraints?.length) out.constraints = rig.constraints.map((c) => ({ ...c, node: at(c.node), source: at(c.source) }))
  return out
}
