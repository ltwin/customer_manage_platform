// The object tree in sibling order, flattened for display. No three.js: the
// assistant panel's object picker uses it on the canvas page too.

type TreeObject = { id: string; parent_id?: string | null; order: number }

export type OutlineRow<T> = { object: T; depth: number }

export function outlineRows<T extends TreeObject>(objects: readonly T[]): OutlineRow<T>[] {
  const byParent = new Map<string, T[]>()
  for (const o of objects) {
    const key = o.parent_id ?? ''
    byParent.set(key, [...(byParent.get(key) ?? []), o])
  }
  const out: OutlineRow<T>[] = []
  const walk = (parent: string, depth: number) => {
    for (const o of (byParent.get(parent) ?? []).sort((a, b) => a.order - b.order)) {
      out.push({ object: o, depth })
      walk(o.id, depth + 1)
    }
  }
  walk('', 0)
  return out
}
