import type { CanvasNode } from './api.ts'
import { untitled } from './nodeKind.ts'

// 节点树的类型筛选项，顺序即菜单顺序。
export const outlineTypes = [
  'core.image',
  'core.video',
  'core.audio',
  'core.text',
  'core.link',
  'core.scene',
  'core.character',
  'core.group',
] as const

export type OutlineOptions = {
  /** 标题搜索词；去首尾空白后为空视为没搜。 */
  query: string
  /** 选中的类型；空集合表示不按类型筛。 */
  types: ReadonlySet<string>
  /** 摄影师折叠的分组；有搜索或筛选时不生效，也不被改写。 */
  collapsed: ReadonlySet<string>
  /** 还没被服务端确认的节点，排在同级最前。 */
  pending: ReadonlySet<string>
}

export type OutlineRow = {
  id: string
  depth: number
  node: CanvasNode
  isGroup: boolean
  /** 只对有子节点的分组有意义。 */
  expanded: boolean
  hasChildren: boolean
  /** 自身命中搜索与筛选；作为路径保留的分组为 false。 */
  match: boolean
}

export type Outline = {
  rows: OutlineRow[]
  /** 画布全部节点数，含分组。 */
  total: number
  /** 命中节点数；没有搜索和筛选时为 null。 */
  matched: number | null
}

export const outlineTitle = (n: CanvasNode) =>
  n.metadata.title || untitled(n.metadata.type_key)

const normalize = (s: string) => s.normalize('NFKC').toLowerCase()

// 把画布节点排成树：分组嵌套，同级新建的在前（未确认的最前），按标题与类型筛选。
export function buildOutline(
  nodes: readonly CanvasNode[],
  { query, types, collapsed, pending }: OutlineOptions,
): Outline {
  const q = normalize(query.trim())
  const filtering = q !== '' || types.size > 0
  const byID = new Map(nodes.map((n) => [n.id, n]))
  const order = new Map(nodes.map((n, i) => [n.id, i]))
  const time = new Map(nodes.map((n) => [n.id, Date.parse(n.created_at)]))

  const children = new Map<string | null, CanvasNode[]>()
  for (const n of nodes) {
    const parent = n.parent_id && byID.has(n.parent_id) ? n.parent_id : null
    const list = children.get(parent) ?? []
    list.push(n)
    children.set(parent, list)
  }
  const rank = (a: CanvasNode, b: CanvasNode) => {
    const pa = pending.has(a.id),
      pb = pending.has(b.id)
    if (pa !== pb) return pa ? -1 : 1
    if (pa) return order.get(b.id)! - order.get(a.id)!
    const ta = time.get(a.id) || 0,
      tb = time.get(b.id) || 0
    if (ta !== tb) return tb - ta
    return a.id < b.id ? -1 : a.id > b.id ? 1 : 0
  }
  for (const list of children.values()) list.sort(rank)

  const hits = new Set<string>()
  const visible = new Set<string>()
  if (filtering) {
    for (const n of nodes) {
      if (types.size && !types.has(n.metadata.type_key)) continue
      if (q && !normalize(outlineTitle(n)).includes(q)) continue
      hits.add(n.id)
      // 祖先作为路径保留；防环。
      for (
        let id: string | null = n.id;
        id && !visible.has(id);
        id = byID.get(id)?.parent_id ?? null
      )
        visible.add(id)
    }
  }

  const rows: OutlineRow[] = []
  const seen = new Set<string>()
  const walk = (parent: string | null, depth: number) => {
    for (const n of children.get(parent) ?? []) {
      if (seen.has(n.id) || (filtering && !visible.has(n.id))) continue
      seen.add(n.id)
      const isGroup = n.metadata.type_key === 'core.group'
      const hasChildren = (children.get(n.id)?.length ?? 0) > 0
      const expanded = filtering || !collapsed.has(n.id)
      rows.push({
        id: n.id,
        depth,
        node: n,
        isGroup,
        expanded,
        hasChildren,
        match: filtering ? hits.has(n.id) : true,
      })
      if (hasChildren && expanded) walk(n.id, depth + 1)
    }
  }
  walk(null, 0)
  return {
    rows,
    total: nodes.length,
    matched: filtering ? hits.size : null,
  }
}
