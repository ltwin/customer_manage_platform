// 执行结果与准备项状态的用户语言标签（工作台镜头卡 / 准备项列表共用）。

export const skipReasonLabels: Record<string, string> = {
  preparation_missing: '准备物料缺失',
  time_insufficient: '时间不够',
  location_unavailable: '场地不可用',
  subject_unavailable: '主体不可用',
  creative_change: '创作方向变更',
  technical_failure: '技术故障',
  other: '其他',
}

export function skipReasonLabel(reason: string | null | undefined): string {
  return (reason && skipReasonLabels[reason]) || '未说明'
}

export function captureModeShortLabel(mode: string | null | undefined): string {
  if (mode === 'live') return '现场'
  if (mode === 'backfill') return '补记'
  return '未判定时段'
}

// 现场缺失 = 关联本准备项、且当前结果为「跳过（准备未完成）」的镜头。
// 只看当前投影：镜头之后补拍成功就自动解除红标。
export function preparationMissingShotPositions(
  shots: Array<{
    position: number
    readiness_item_ids: string[]
    current_outcome?: { result: string; skip_reason?: string | null } | null
  }>,
  readinessID: string,
): number[] {
  return shots
    .filter((shot) =>
      shot.readiness_item_ids.includes(readinessID)
      && shot.current_outcome?.result === 'skipped'
      && shot.current_outcome?.skip_reason === 'preparation_missing')
    .map((shot) => shot.position)
}

export function readinessTag(
  item: { requirement: string; preflight_status: string },
  siteMissing: boolean,
): { label: string; className: string } {
  if (siteMissing) {
    return { label: item.requirement === 'required' ? '必需 · 现场缺失' : '现场缺失', className: 'badge badge-danger' }
  }
  if (item.requirement === 'required') {
    return item.preflight_status === 'unchecked'
      ? { label: '必需 · 待核对', className: 'badge badge-warning' }
      : { label: '必需', className: 'badge badge-accent' }
  }
  return { label: '可选', className: 'badge badge-muted' }
}
