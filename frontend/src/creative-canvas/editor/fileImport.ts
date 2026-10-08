import {
  classifyFile,
  readableFileName,
  type MediaCapabilities,
  type MediaKind,
} from './media.ts'
import { defaultNodeSize } from './outbox.ts'

// 拖入画布的一批文件：可导入的按顺序建节点，其余逐项说明原因。
export type ImportPlan = {
  accepted: { file: File; kind: MediaKind }[]
  rejected: { file: File; message: string }[]
}

export function importPlan(
  files: File[],
  capabilities: MediaCapabilities,
): ImportPlan {
  const plan: ImportPlan = { accepted: [], rejected: [] }
  for (const file of files) {
    const classified = classifyFile(file, capabilities)
    if ('error' in classified)
      plan.rejected.push({ file, message: classified.error })
    else if (plan.accepted.length >= capabilities.batch_limit)
      plan.rejected.push({
        file,
        message: `一次最多导入 ${capabilities.batch_limit} 个文件`,
      })
    else plan.accepted.push({ file, kind: classified.kind })
  }
  return plan
}

// 节点间距 330（默认宽 300 + 30）：第一个以落点为中心，其余向右排开。
export const importSpacing = 330

export function importSlot(
  index: number,
  drop: { x: number; y: number },
  typeKey: string,
): { x: number; y: number } {
  const size = defaultNodeSize(typeKey)
  return {
    x: drop.x + index * importSpacing - size.width / 2,
    y: drop.y - size.height / 2,
  }
}

// 节点标题：去掉扩展名的文件名，最多 200 字。
export function fileTitle(name: string): string {
  const readable = readableFileName(name)
  const title = readable.replace(/\.[^.]+$/, '') || readable
  return [...title].slice(0, 200).join('')
}
