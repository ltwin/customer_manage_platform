// 摄取丢弃原因的中文对照镜像：reason 全集来自服务端
//（shootplanning/ingestion/parser.go：blank/duplicate/unsupported/over_limit），
// manual 是前端为「用户在确认步骤取消勾选」补的展示分类，不回传服务端。

export type IngestionDropReason = 'blank' | 'duplicate' | 'unsupported' | 'over_limit' | 'manual'

export const ingestionDropReasons: ReadonlyArray<{
  value: IngestionDropReason
  label: string
  note: string
}> = [
  { value: 'duplicate', label: '重复内容', note: '与保留的候选重复，只保留其中一条' },
  { value: 'unsupported', label: '无法识别', note: '解析器无法把这段归入镜头或准备项' },
  { value: 'over_limit', label: '超出上限', note: '候选数量超过单次整理上限，需拆分原文分批整理' },
  { value: 'blank', label: '空白段', note: '该段没有可解析的有效内容' },
  { value: 'manual', label: '手动丢弃', note: '你在确认步骤取消勾选的候选，仍在上方列表可重新勾选' },
]

// 未知的 reason（服务端未来扩展）按原值展示，不做猜测翻译。
export function dropReasonLabel(reason: string): string {
  return ingestionDropReasons.find((entry) => entry.value === reason)?.label ?? reason
}

export function dropReasonNote(reason: string): string {
  return ingestionDropReasons.find((entry) => entry.value === reason)?.note ?? '解析层丢弃项，可恢复为候选'
}

// 丢弃摘录：折叠空白与换行，超长截断；原文缺失时返回空串。
export function droppedExcerpt(original: string | null | undefined, maxLength = 60): string {
  const text = (original ?? '').replace(/\s+/g, ' ').trim()
  if (text === '') return ''
  return text.length <= maxLength ? text : `${text.slice(0, maxLength)}…`
}

export const droppedLegendLine = ingestionDropReasons
  .map((entry) => `${entry.label}＝${entry.note}`)
  .join('；')
