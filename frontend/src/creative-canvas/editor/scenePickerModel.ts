import type { Selection } from '../../creative-scene/input/selection.ts'
import { outlineRows } from '../../creative-scene/outlineRows.ts'

// 助手 @ 菜单里场景的物件列表：按层级列出，可点选、修饰键增减、Shift 范围、
// 在列表里拖过连续多行。不依赖 three，画布页也能用。

export type PickerObject = {
  id: string
  name: string
  kind: string
  parent_id?: string | null
  order: number
}
// matched 为 false 的行是搜索命中项的上级：留着显示层级，但置灰、不能选。
export type PickerRow = { object: PickerObject; depth: number; matched: boolean }

export function pickerRows(
  objects: readonly PickerObject[],
  query: string,
): PickerRow[] {
  const rows = outlineRows(objects)
  const needle = query.trim().toLowerCase()
  if (!needle)
    return rows.map((row) => ({ ...row, matched: true }))
  const byID = new Map(objects.map((o) => [o.id, o]))
  const matched = new Set(
    objects
      .filter((o) => (o.name || '未命名').toLowerCase().includes(needle))
      .map((o) => o.id),
  )
  const kept = new Set(matched)
  for (const id of matched) {
    let parent = byID.get(id)?.parent_id
    while (parent && !kept.has(parent)) {
      kept.add(parent)
      parent = byID.get(parent)?.parent_id
    }
  }
  return rows
    .filter((row) => kept.has(row.object.id))
    .map((row) => ({ ...row, matched: matched.has(row.object.id) }))
}

// 拖选：从按下的那一行到指针所在的行。按着修饰键拖时并进原来的选中，
// 否则只留这一段。active 跟着指针走，之后 Shift 点选从这里算起。
export function dragSelection(
  base: Selection,
  anchor: string,
  current: string,
  order: readonly string[],
  additive: boolean,
): Selection {
  const a = order.indexOf(anchor)
  const b = order.indexOf(current)
  if (a < 0 || b < 0) return base
  const range = order.slice(Math.min(a, b), Math.max(a, b) + 1)
  const selected = additive
    ? [...base.selected, ...range.filter((id) => !base.selected.includes(id))]
    : range
  return { selected, active: current }
}

// 插入的物件按列表顺序排，跟摄影师看到的一致；被搜索过滤掉的选中项照样算。
export function pickedObjects(
  selection: Selection,
  objects: readonly PickerObject[],
): PickerObject[] {
  const chosen = new Set(selection.selected)
  return outlineRows(objects)
    .map((row) => row.object)
    .filter((o) => chosen.has(o.id))
}

// 拖到列表上下边缘 24px 以内时自动滚动，每帧 8px。
export const edgeZone = 24
export const edgeStep = 8
export function edgeScroll(y: number, top: number, bottom: number): number {
  if (y < top + edgeZone) return -edgeStep
  if (y > bottom - edgeZone) return edgeStep
  return 0
}
