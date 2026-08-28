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

export type ShotTagOption = { value: string; label: string }
export type ShotTagField = { label: string; options: readonly ShotTagOption[] }

// 规范标签的中文显示唯一权威源：存储值与 API 契约仍是英文枚举，UI 一律走这里取中文。
// 任何页面都不得再硬编码枚举值或自备一套中文，否则同一标签会在镜头卡、现场模式和
// 整理候选里显示成三种说法。
const shotTagFieldMap: Record<ShotTagFieldKey, ShotTagField> = {
  framing_tag: {
    label: '景别',
    options: [
      { value: 'extreme_closeup', label: '大特写' },
      { value: 'closeup', label: '特写' },
      { value: 'medium_closeup', label: '近景' },
      { value: 'medium', label: '中景' },
      { value: 'full', label: '全景' },
      { value: 'wide', label: '远景' },
      { value: 'extreme_wide', label: '大远景' },
      { value: 'other', label: '其他' },
    ],
  },
  lighting_direction_tag: {
    label: '光线方向',
    options: [
      { value: 'front', label: '顺光' },
      { value: 'side', label: '侧光' },
      { value: 'back', label: '逆光' },
      { value: 'top', label: '顶光' },
      { value: 'bottom', label: '底光' },
      { value: 'mixed', label: '混合' },
      { value: 'natural', label: '自然光' },
      { value: 'other', label: '其他' },
    ],
  },
  lighting_quality_tag: {
    label: '光质',
    options: [
      { value: 'hard', label: '硬光' },
      { value: 'soft', label: '柔光' },
      { value: 'mixed', label: '混合' },
      { value: 'natural', label: '自然光' },
      { value: 'other', label: '其他' },
    ],
  },
  palette_tag: {
    label: '色调',
    options: [
      { value: 'warm', label: '暖调' },
      { value: 'cool', label: '冷调' },
      { value: 'neutral', label: '中性' },
      { value: 'monochrome', label: '单色' },
      { value: 'high_saturation', label: '高饱和' },
      { value: 'low_saturation', label: '低饱和' },
      { value: 'mixed', label: '混合' },
      { value: 'other', label: '其他' },
    ],
  },
  shot_type_tag: {
    label: '镜头类型',
    options: [
      { value: 'portrait', label: '人像' },
      { value: 'action', label: '动作' },
      { value: 'interaction', label: '互动' },
      { value: 'environment', label: '环境' },
      { value: 'detail', label: '细节' },
      { value: 'silhouette', label: '剪影' },
      { value: 'narrative', label: '叙事' },
      { value: 'other', label: '其他' },
    ],
  },
}

export const shotTagFields: ReadonlyArray<{ key: ShotTagFieldKey } & ShotTagField> =
  (Object.keys(shotTagFieldMap) as ShotTagFieldKey[]).map((key) => ({ key, ...shotTagFieldMap[key] }))

export function shotTagField(key: ShotTagFieldKey): ShotTagField {
  return shotTagFieldMap[key]
}

// 未选就是未选：空值统一显示「未填」，渲染点不必各自处理这个边界。
// 遇到映射外的值（契约先扩、前端后跟）保留原值，宁可露出英文也不吞掉信息。
export function shotTagLabel(key: ShotTagFieldKey, value: string | null | undefined): string {
  if (!value) return '未填'
  return shotTagFieldMap[key].options.find((option) => option.value === value)?.label ?? value
}

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
