// 分享反馈引用镜头时的展示标签：用位置序号代替原始 shot_id（原型口径「第 07 镜」）。

export function shotPositionLabel(position: number): string {
  return `第 ${String(position).padStart(2, '0')} 镜`
}

export function shotReferenceLabel(position: number, title: string): string {
  return `${shotPositionLabel(position)} · ${title}`
}

export const removedShotLabel = '已移除的镜头'
