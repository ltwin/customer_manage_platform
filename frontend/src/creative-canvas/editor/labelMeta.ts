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

// 节点最近一次生成没有产出可用作品时返回原因码，否则为 null。只看最近一次：
// 下一次生成开始后它就不再是最近一次，失败自然收起（owner 2026-10-16）。
// 主动停止（cancelled + discarded）是摄影师自己的操作，不算失败。
export function generationFailure(status: CanvasNode['status']): string | null {
  if (!status.latest_execution_id || status.active_execution_id) return null
  const apply = status.apply_state
  switch (status.generation_state) {
    case 'failed':
      return status.error ?? 'generation_failed'
    case 'cancelled':
      return apply === 'expired' ? 'generation_timeout' : null
    case 'succeeded':
      return apply === 'expired'
        ? 'generation_timeout'
        : apply === 'discarded'
          ? 'generation_not_applied'
          : null
    default:
      return null
  }
}

// 失败原因的展示文案；平台侧问题（额度、凭证、部署）后端已归为通用失败。
export function generationFailureText(code: string): string {
  switch (code) {
    case 'generation_content_rejected':
      return '内容未通过审核，调整提示词或参考图后重试'
    case 'generation_invalid_input':
      return '模型不支持当前参数或参考图，调整后重试'
    case 'generation_timeout':
      return '生成超时，可以重试'
    case 'generation_unverified':
      return '没能确认生成结果，可以重试'
    case 'generation_not_applied':
      return '生成期间节点或参考有变化，结果没有采用'
    default:
      return '生成失败，可以重试'
  }
}
