// 摄取会话 409（ingestion_revision_conflict）后的差异摘要：
// 本地未提交的确认状态 vs 服务端最新会话，输出给「查看差异」面板逐行展示。

export interface IngestionDiffSide {
  revision: number
  keptCandidates: number
  totalCandidates: number
  keptReferences: number
  totalReferences: number
  sourceChecksum: string
}

export function staleDiffLines(local: IngestionDiffSide, server: IngestionDiffSide): string[] {
  const lines: string[] = []
  if (local.revision !== server.revision) {
    lines.push(`会话版本：本地基于第 ${local.revision} 版，服务端已是第 ${server.revision} 版`)
  }
  lines.push(`候选：本地保留 ${local.keptCandidates} / 共 ${local.totalCandidates} 条；服务端最新解析共 ${server.totalCandidates} 条`)
  lines.push(`参考链接：本地保留 ${local.keptReferences} / 共 ${local.totalReferences} 条；服务端最新 ${server.totalReferences} 条`)
  lines.push(local.sourceChecksum === server.sourceChecksum ? '原文校验和：未变化' : '原文校验和：已变化（服务端按新原文重建过候选）')
  return lines
}
