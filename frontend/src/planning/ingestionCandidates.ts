import type { components } from '../api/schema'

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

type IngestionContentCandidate = components['schemas']['IngestionContentCandidate']
type IngestionContentOverride = components['schemas']['IngestionContentOverride']

export type CandidateFieldSource = Pick<
  IngestionContentCandidate,
  | 'title'
  | 'normalized_content'
  | 'category'
  | 'requirement'
  | 'responsibility_hint'
  | 'default_preparation_lead_days'
  | 'framing_tag'
  | 'lighting_direction_tag'
  | 'lighting_quality_tag'
  | 'palette_tag'
  | 'shot_type_tag'
>

export type ShotTagFieldKey =
  | 'framing_tag'
  | 'lighting_direction_tag'
  | 'lighting_quality_tag'
  | 'palette_tag'
  | 'shot_type_tag'

export const shotTagFields: ReadonlyArray<{ key: ShotTagFieldKey; label: string; values: readonly string[] }> = [
  { key: 'framing_tag', label: '取景', values: ['extreme_closeup', 'closeup', 'medium_closeup', 'medium', 'full', 'wide', 'extreme_wide', 'other'] },
  { key: 'lighting_direction_tag', label: '灯光方向', values: ['front', 'side', 'back', 'top', 'bottom', 'mixed', 'natural', 'other'] },
  { key: 'lighting_quality_tag', label: '灯光质感', values: ['hard', 'soft', 'mixed', 'natural', 'other'] },
  { key: 'palette_tag', label: '色调', values: ['warm', 'cool', 'neutral', 'monochrome', 'high_saturation', 'low_saturation', 'mixed', 'other'] },
  { key: 'shot_type_tag', label: '类型', values: ['portrait', 'action', 'interaction', 'environment', 'detail', 'silhouette', 'narrative', 'other'] },
]

// 「未填」选择恢复为 null：规范标签未知就是未知，不从文本猜测。
export function shotTagPatch(key: ShotTagFieldKey, value: string): Pick<IngestionContentCandidate, ShotTagFieldKey> {
  return { [key]: value === '' ? null : value } as Pick<IngestionContentCandidate, ShotTagFieldKey>
}

// commit 决策里的 ShotWrite：标签未选时显式为 null，与服务端「未知不等于 0」语义一致。
export function shotDecisionShot(candidate: CandidateFieldSource) {
  return {
    title: candidate.title,
    notes: candidate.normalized_content,
    framing_tag: candidate.framing_tag ?? null,
    lighting_direction_tag: candidate.lighting_direction_tag ?? null,
    lighting_quality_tag: candidate.lighting_quality_tag ?? null,
    palette_tag: candidate.palette_tag ?? null,
    shot_type_tag: candidate.shot_type_tag ?? null,
  }
}

type OverrideFieldPatch = Partial<
  Pick<
    IngestionContentOverride,
    'category' | 'requirement' | 'responsibility_hint' | 'default_preparation_lead_days' |
    'framing_tag' | 'lighting_direction_tag' | 'lighting_quality_tag' | 'palette_tag' | 'shot_type_tag'
  >
>

// preview override 只携带本次有值的字段编辑；nil 字段在服务端语义是「未编辑」。
// category/requirement/responsibility_hint 在候选 schema 上是宽松 string，值只能来自
// 服务端解析或受控下拉，这里收窄回 override 的枚举联合。
export function contentOverrideFields(candidate: CandidateFieldSource): OverrideFieldPatch {
  return {
    ...(candidate.category ? { category: candidate.category as OverrideFieldPatch['category'] } : {}),
    ...(candidate.requirement ? { requirement: candidate.requirement as OverrideFieldPatch['requirement'] } : {}),
    ...(candidate.responsibility_hint ? { responsibility_hint: candidate.responsibility_hint as OverrideFieldPatch['responsibility_hint'] } : {}),
    ...(candidate.default_preparation_lead_days != null ? { default_preparation_lead_days: candidate.default_preparation_lead_days } : {}),
    ...(candidate.framing_tag ? { framing_tag: candidate.framing_tag } : {}),
    ...(candidate.lighting_direction_tag ? { lighting_direction_tag: candidate.lighting_direction_tag } : {}),
    ...(candidate.lighting_quality_tag ? { lighting_quality_tag: candidate.lighting_quality_tag } : {}),
    ...(candidate.palette_tag ? { palette_tag: candidate.palette_tag } : {}),
    ...(candidate.shot_type_tag ? { shot_type_tag: candidate.shot_type_tag } : {}),
  }
}
