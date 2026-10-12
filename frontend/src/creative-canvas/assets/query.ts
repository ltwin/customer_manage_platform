import type * as api from '../editor/api.ts'

// 资产库页的浏览条件：网址参数 ↔ 条件 ↔ 列表请求，以及记在本机的显示偏好。
// 参数名与画布抽屉（lgroup 等）不同，两边互不干扰。

export const VIEWS = [
  { key: 'all', label: '全部' },
  { key: 'unclassified', label: '未归类' },
  { key: 'favorites', label: '最爱' },
  { key: 'recent', label: '最近添加' },
  { key: 'trash', label: '回收站' },
] as const
export type View = (typeof VIEWS)[number]['key']

export const KINDS = [
  { key: 'image', label: '图片' },
  { key: 'video', label: '视频' },
  { key: 'audio', label: '音频' },
  { key: 'text', label: '文字' },
  { key: 'link', label: '链接' },
] as const
export type Kind = (typeof KINDS)[number]['key']

export const SORTS = [
  { value: 'recent', label: '最近添加' },
  { value: 'oldest', label: '最早添加' },
  { value: 'name', label: '名称' },
  { value: 'rating', label: '评分' },
] as const
export type Sort = (typeof SORTS)[number]['value']

// 评分筛选：'none' 是未评分，其余是最低分。
export const RATINGS = [
  { value: '5', label: '5 星' },
  { value: '4.5', label: '4.5 星以上' },
  { value: '4', label: '4 星以上' },
  { value: '3', label: '3 星以上' },
  { value: 'none', label: '未评分' },
] as const
export type Rating = (typeof RATINGS)[number]['value']

export type LibraryQuery = {
  view: View
  group: string
  tags: string[]
  mode: 'all' | 'any'
  kind: Kind | ''
  q: string
  rating: Rating | ''
  sort: Sort
}

const pick = <T extends string>(
  value: string | null,
  allowed: readonly T[],
  fallback: T,
): T => (allowed.includes(value as T) ? (value as T) : fallback)

/** 从网址读条件；不认识的值按缺省处理，进分组时视图固定为全部。 */
export function readQuery(params: URLSearchParams): LibraryQuery {
  const group = params.get('group') ?? ''
  return {
    view: group
      ? 'all'
      : pick(
          params.get('view'),
          VIEWS.map((v) => v.key),
          'all',
        ),
    group,
    tags: [
      ...new Set((params.get('tags') ?? '').split(',').filter(Boolean)),
    ].slice(0, 50),
    mode: params.get('mode') === 'any' ? 'any' : 'all',
    kind: pick(params.get('kind'), [...KINDS.map((k) => k.key), ''], ''),
    q: (params.get('q') ?? '').trim().slice(0, 200),
    rating: pick(
      params.get('rating'),
      [...RATINGS.map((r) => r.value), ''],
      '',
    ),
    sort: pick(
      params.get('sort'),
      SORTS.map((s) => s.value),
      'recent',
    ),
  }
}

/** 条件写回网址：缺省值不写，网址保持干净。 */
export function writeQuery(q: LibraryQuery): URLSearchParams {
  const out = new URLSearchParams()
  if (q.group) out.set('group', q.group)
  else if (q.view !== 'all') out.set('view', q.view)
  if (q.tags.length) out.set('tags', q.tags.join(','))
  if (q.tags.length > 1 && q.mode === 'any') out.set('mode', 'any')
  if (q.kind) out.set('kind', q.kind)
  if (q.q) out.set('q', q.q)
  if (q.rating) out.set('rating', q.rating)
  if (q.sort !== 'recent') out.set('sort', q.sort)
  return out
}

/** 换视图或分组时清空筛选，只保留排序。 */
export function enter(q: LibraryQuery, view: View, group = ''): LibraryQuery {
  return { ...blank(), view: group ? 'all' : view, group, sort: q.sort }
}
export const blank = (): LibraryQuery => ({
  view: 'all',
  group: '',
  tags: [],
  mode: 'all',
  kind: '',
  q: '',
  rating: '',
  sort: 'recent',
})

/** 有没有会缩小结果的条件（类型、关键词、标签、评分）。 */
export const narrowed = (q: LibraryQuery) =>
  !!(q.kind || q.q || q.tags.length || q.rating)
export const clearFilters = (q: LibraryQuery): LibraryQuery => ({
  ...q,
  kind: '',
  q: '',
  tags: [],
  mode: 'all',
  rating: '',
})

/** 首屏请求带上计数；翻页只带游标，不再统计。 */
export function listPath(q: LibraryQuery, limit: number, cursor = ''): string {
  const out = new URLSearchParams({
    view: q.view,
    limit: String(limit),
    sort: q.sort,
  })
  if (q.group) {
    out.set('group_id', q.group)
    out.set('include_descendants', 'true')
  }
  if (q.kind) out.set('kind', q.kind)
  if (q.q) out.set('q', q.q)
  if (q.tags.length) {
    out.set('tag_ids', q.tags.join(','))
    out.set('tag_mode', q.mode)
  }
  if (q.rating === 'none') out.set('unrated', 'true')
  else if (q.rating) out.set('min_rating', q.rating)
  if (cursor) out.set('cursor', cursor)
  else out.set('include_facets', 'true')
  return `/assets?${out.toString()}`
}

export type Prefs = {
  layout: 'masonry' | 'grid'
  size: number
  inspector: boolean
}
export const SIZE = { min: 140, max: 360, step: 20, initial: 220 } as const
const prefsKey = 'creative-assets:prefs'

export function readPrefs(): Prefs {
  const fallback: Prefs = {
    layout: 'masonry',
    size: SIZE.initial,
    inspector: true,
  }
  try {
    const raw: unknown = JSON.parse(localStorage.getItem(prefsKey) ?? 'null')
    if (!raw || typeof raw !== 'object') return fallback
    const v = raw as Partial<Record<keyof Prefs, unknown>>
    const size = typeof v.size === 'number' ? v.size : SIZE.initial
    return {
      layout: v.layout === 'grid' ? 'grid' : 'masonry',
      size: Math.min(
        SIZE.max,
        Math.max(SIZE.min, Math.round(size / SIZE.step) * SIZE.step),
      ),
      inspector: v.inspector !== false,
    }
  } catch {
    return fallback
  }
}
export function writePrefs(p: Prefs) {
  try {
    localStorage.setItem(prefsKey, JSON.stringify(p))
  } catch {
    // 无痕窗口等情况下存不了，只影响下次打开时的默认显示
  }
}

/** 两层分组：先列顶层，子组紧跟在父组后面。 */
export function groupTree(groups: api.Group[]) {
  const roots = groups.filter((g) => !g.parent_id)
  return roots.map((g) => ({
    group: g,
    children: groups.filter((c) => c.parent_id === g.id),
  }))
}

/** 按分类列出标签；没有分类的放在最后的「未分类」里，空分类不显示。 */
export function tagSections(tags: api.Tag[], categories: api.Category[]) {
  const sections = [
    ...categories.map((c) => ({ id: c.id, name: c.name })),
    { id: null as string | null, name: '未分类' },
  ]
  return sections
    .map((s) => ({
      ...s,
      tags: tags.filter((t) => (t.category_id ?? null) === s.id),
    }))
    .filter((s) => s.tags.length)
}
