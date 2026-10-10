import type * as api from './api.ts'
import type { NewTag } from './api.ts'
import type { PickerOption } from './Picker.tsx'
export type Catalog = {
  groups: api.Group[]
  tags: api.Tag[]
  categories: api.Category[]
  hierarchy: string
  settings: api.LibrarySettings | null
}
export const emptyCatalog = (): Catalog => ({
  groups: [],
  tags: [],
  categories: [],
  hierarchy: '1',
  settings: null,
})
export type Editor = {
  kind:
    | 'group'
    | 'move'
    | 'tag'
    | 'category'
    | 'metadata'
    | 'organize'
    | 'settings'
  id?: string
  name?: string
  description?: string
  parent?: string
  position?: number
  color?: string
  revision?: string
  hierarchy: string
  selected?: api.Asset[]
}
export type Organization = {
  groupIDs: string[]
  tagIDs: string[]
  newTags: NewTag[]
}
export const emptyOrganization = (): Organization => ({
  groupIDs: [],
  tagIDs: [],
  newTags: [],
})

export const kindLabel: Record<string, string> = {
  text: '文字',
  link: '链接',
  image: '图片',
  video: '视频',
  audio: '音频',
}
// 宽高比来自已核实的原图尺寸；文字与音频按固定比例排版。
export function tileRatio(a: api.Asset): number {
  const m = a.content?.media?.find((x) => x.role === 'original') ?? a.content?.media?.[0]
  if ((a.kind === 'image' || a.kind === 'video') && m?.width && m.height)
    return m.height / m.width
  return a.kind === 'audio' ? 0.5 : 0.62
}
export function metaLine(a: api.Asset, groups: api.Group[], tags: api.Tag[]) {
  const m = a.content?.media?.find((x) => x.role === 'original')
  const size = m?.width && m.height ? `${m.width}×${m.height}` : kindLabel[a.kind]
  const inGroups = a.group_ids
    .map((id) => groups.find((g) => g.id === id)?.name)
    .filter(Boolean)
  const tagNames = a.tag_ids
    .map((id) => tags.find((t) => t.id === id)?.name)
    .filter(Boolean)
  return [size, inGroups.join('、') || '未归类', tagNames.join(' · ')]
    .filter(Boolean)
    .join('　')
}

// 分组平铺浏览每组预览的格数。
export const PREVIEW = 6

/** 分组按层级排成下拉选项：父分组在前，子分组紧随其后并缩进。 */
export function groupOptions(
  groups: api.Group[],
  exclude?: string,
): PickerOption[] {
  const out: PickerOption[] = []
  const visit = (parent: string | null, depth: number) => {
    for (const g of groups) {
      if ((g.parent_id ?? null) !== parent || g.id === exclude) continue
      out.push({ value: g.id, label: g.name, depth })
      visit(g.id, depth + 1)
    }
  }
  visit(null, 0)
  return out
}
