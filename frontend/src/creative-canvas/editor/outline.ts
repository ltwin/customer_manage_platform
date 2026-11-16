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

/** 节点树只读这些字段；页面传完整的画布节点，测试可以只给这些。 */
export type OutlineNode = Pick<CanvasNode, 'id' | 'parent_id' | 'created_at'> & {
  metadata: Pick<CanvasNode['metadata'], 'type_key' | 'title'>
}

export type OutlineOptions = {
  /** 标题搜索词；去首尾空白后为空视为没搜。 */
  query: string
  /** 选中的类型；空集合表示不按类型筛。 */
  types: ReadonlySet<string>
  /** 摄影师折叠的分组；有搜索或筛选时不生效，也不被改写。 */
  collapsed: ReadonlySet<string>
  /** 还没被服务端确认的节点，排在同级最前；彼此之间同样按创建时间。 */
  pending: ReadonlySet<string>
}

export type OutlineRow<N extends OutlineNode = CanvasNode> = {
  id: string
  depth: number
  node: N
  isGroup: boolean
  /** 只对有子节点的分组有意义。 */
  expanded: boolean
  hasChildren: boolean
  /** 自身命中搜索与筛选；作为路径保留的分组为 false。 */
  match: boolean
}

export type Outline<N extends OutlineNode = CanvasNode> = {
  rows: OutlineRow<N>[]
  /** 画布全部节点数，含分组。 */
  total: number
  /** 命中节点数；没有搜索和筛选时为 null。 */
  matched: number | null
}

export const outlineTitle = (n: OutlineNode) =>
  n.metadata.title || untitled(n.metadata.type_key)

const normalize = (s: string) => s.normalize('NFKC').toLowerCase()

// RFC 3339 时间排序键：整秒部分按 Date.parse，小数部分补到纳秒按字符串比，
// 微秒精度不丢；解析不了的排到最后。
type TimeKey = { seconds: number; fraction: string }
const rfc3339 = /^(.*T\d\d:\d\d:\d\d)(?:\.(\d+))?(Z|[+-]\d\d:\d\d)$/i
function timeKey(value: string): TimeKey {
  const m = rfc3339.exec(value)
  const seconds = m ? Date.parse(m[1]! + m[3]!) : NaN
  return Number.isNaN(seconds)
    ? { seconds: -Infinity, fraction: '' }
    : { seconds, fraction: (m![2] ?? '').padEnd(9, '0').slice(0, 9) }
}

// 把画布节点排成树：分组嵌套，同级新建的在前（未确认的最前），按标题与类型筛选。
export function buildOutline<N extends OutlineNode>(
  nodes: readonly N[],
  { query, types, collapsed, pending }: OutlineOptions,
): Outline<N> {
  const q = normalize(query.trim())
  const filtering = q !== '' || types.size > 0
  const byID = new Map(nodes.map((n) => [n.id, n]))
  const time = new Map(nodes.map((n) => [n.id, timeKey(n.created_at)]))

  const children = new Map<string | null, N[]>()
  for (const n of nodes) {
    const parent = n.parent_id && byID.has(n.parent_id) ? n.parent_id : null
    const list = children.get(parent) ?? []
    list.push(n)
    children.set(parent, list)
  }
  // 同一次保存建的节点服务端给同一时间，按 id 兜底；本机未确认的同批节点
  // 也共用一个时间（见 outbox），所以确认前后顺序一致。
  const rank = (a: N, b: N) => {
    const pa = pending.has(a.id),
      pb = pending.has(b.id)
    if (pa !== pb) return pa ? -1 : 1
    const ta = time.get(a.id)!,
      tb = time.get(b.id)!
    if (ta.seconds !== tb.seconds) return tb.seconds > ta.seconds ? 1 : -1
    if (ta.fraction !== tb.fraction) return tb.fraction > ta.fraction ? 1 : -1
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

  const rows: OutlineRow<N>[] = []
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
