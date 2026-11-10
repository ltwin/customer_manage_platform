import type { Group } from 'three'
import { GLTFExporter } from 'three/examples/jsm/exporters/GLTFExporter.js'
import { GLBError, inspect, type Manifest } from '../sandbox/inspect.ts'
import { ImportRejected } from './parse.ts'

// The converted scene as the GLB the store keeps (design「模块职责与协作」
// write): three's exporter, then the same static-subset check the server runs,
// so a file over a budget is refused here, with the reason, before anything
// is uploaded.

export type Written = { glb: ArrayBuffer; manifest: Manifest }

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

export async function writeGLB(scene: Group): Promise<Written> {
  const glb = (await new GLTFExporter().parseAsync(scene, { binary: true })) as ArrayBuffer
  try {
    return { glb, manifest: inspect(glb) }
  } catch (e) {
    if (!(e instanceof GLBError)) throw e
    if (e.reason === 'limit') throw new ImportRejected('limit', `${limitMessage(e.detail)}。请在 Blender 里减面或拆分后再导出`)
    // Anything else is the conversion's fault, not the file's.
    throw new ImportRejected('unreadable', `换算后的模型没有通过检查（${e.reason}: ${e.detail}）`)
  }
}
