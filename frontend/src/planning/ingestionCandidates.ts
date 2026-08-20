export type RestoredCandidateKindSource = ReadonlyArray<{
  candidate_id: string
  kind: 'shot' | 'readiness'
}>

// 恢复被丢弃的候选时，kind 由服务端 override 契约显式携带；重复项跟随
// winner 的既有分类，其余解析层丢弃项（blank/unsupported/over-limit）默认镜头。
export function restoredCandidateKind(
  dropped: { winner_candidate_id?: string | null },
  candidates: RestoredCandidateKindSource,
): 'shot' | 'readiness' {
  const winnerID = dropped.winner_candidate_id
  if (!winnerID) return 'shot'
  const winner = candidates.find((candidate) => candidate.candidate_id === winnerID)
  return winner?.kind === 'readiness' ? 'readiness' : 'shot'
}
