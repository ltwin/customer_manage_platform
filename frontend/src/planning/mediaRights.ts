// 素材来源×权利×用途矩阵的前端镜像：裁决权威在服务端
//（planningmedia/matrix.go），这里只做选择器与提示。
// 规则：来源决定唯一权利依据；「生成参考」仅限本人创作，或已取得许可证
// 且显式授予生成参考权；其余来源只可情绪板/镜头参考展示。

export type MediaSourceClass =
  | 'official' | 'anime_screenshot' | 'setting_book' | 'fan' | 'unknown_web'
  | 'photographer_owned' | 'licensed' | 'customer_supplied'

export type MediaRightsBasis =
  | 'citation_or_display' | 'ownership_attested' | 'license_recorded' | 'display_consent'

export type MediaPurpose = 'moodboard_display' | 'shot_reference_display' | 'generation_reference'

export const mediaSourceOptions: ReadonlyArray<{
  value: MediaSourceClass
  label: string
  basis: MediaRightsBasis
  basisLabel: string
}> = [
  { value: 'photographer_owned', label: '本人拍摄或创作', basis: 'ownership_attested', basisLabel: '权利自证' },
  { value: 'licensed', label: '已取得许可证', basis: 'license_recorded', basisLabel: '许可在档' },
  { value: 'official', label: '官方素材', basis: 'citation_or_display', basisLabel: '引用展示' },
  { value: 'anime_screenshot', label: '动画截图', basis: 'citation_or_display', basisLabel: '引用展示' },
  { value: 'setting_book', label: '设定集', basis: 'citation_or_display', basisLabel: '引用展示' },
  { value: 'fan', label: '同人作品', basis: 'citation_or_display', basisLabel: '引用展示' },
  { value: 'unknown_web', label: '网络来源待确认', basis: 'citation_or_display', basisLabel: '引用展示' },
  { value: 'customer_supplied', label: '客户提供', basis: 'display_consent', basisLabel: '展示同意' },
]

export const mediaPurposeOptions: ReadonlyArray<{ value: MediaPurpose; label: string }> = [
  { value: 'moodboard_display', label: '情绪板参考' },
  { value: 'shot_reference_display', label: '镜头参考' },
  { value: 'generation_reference', label: '生成参考' },
]

export function mediaSourceMeta(source: MediaSourceClass) {
  return mediaSourceOptions.find((option) => option.value === source)
    ?? { value: source, label: source, basis: 'citation_or_display' as MediaRightsBasis, basisLabel: '引用展示' }
}

export function rightsDeclarationFor(
  source: MediaSourceClass,
  generationGrantGranted: boolean,
): { source_class: MediaSourceClass; rights_basis: MediaRightsBasis; license_generation_reference_granted: boolean } {
  const meta = mediaSourceMeta(source)
  return {
    source_class: source,
    rights_basis: meta.basis,
    license_generation_reference_granted: source === 'licensed' && generationGrantGranted,
  }
}

export function purposeAllowed(
  source: MediaSourceClass,
  generationGrantGranted: boolean,
  purpose: MediaPurpose,
): boolean {
  if (purpose !== 'generation_reference') return true
  return source === 'photographer_owned' || (source === 'licensed' && generationGrantGranted)
}

export function generationGrantSelectable(source: MediaSourceClass): boolean {
  return source === 'licensed'
}

export const matrixRejectionNote = '不合规的组合会在上传、绑定与使用三个环节被一律拒绝；「生成参考」只有本人创作，或已取得许可证并勾选授权时可选。'
