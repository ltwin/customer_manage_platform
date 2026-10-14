import type { CanvasNode } from './api.ts'

// 卡片外标签的元信息（PRD CV-10）：多版本显示「主版本 i/N」，单版本显示模型名；
// 运行中、文本、链接和组不显示。模型名优先用生成目录里的 label，找不到时显示 key。
export function nodeLabelMeta(
  node: CanvasNode,
  modelLabels: Record<string, string>,
): string {
  const kind = node.metadata.type_key
  if (!['core.image', 'core.video', 'core.audio'].includes(kind)) return ''
  if (generationPhase(node.status.generation_state)) return ''
  const summary = node.version_summary
  if (!summary) return ''
  if (summary.count > 1)
    return `主版本 ${summary.main_ordinal}/${summary.count}`
  const key = summary.main_model_key
  return key ? (modelLabels[key] ?? key) : ''
}

export type GenerationPhase = 'queued' | 'running' | 'reconciling'

// 节点生成进行中的阶段；空闲或已结束时为 null。
export function generationPhase(state: string): GenerationPhase | null {
  return state === 'queued' || state === 'running' || state === 'reconciling'
    ? state
    : null
}
