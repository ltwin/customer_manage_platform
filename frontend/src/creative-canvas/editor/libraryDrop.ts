// 画布节点拖到资产抽屉上即存入资产库：落在分组标题上归入该组，落在抽屉其他
// 位置归入当前浏览的分组（没有则只存入）。被拖的节点在抽屉下层，
// elementFromPoint 拿不到抽屉元素，所以按矩形判定。
export type LibraryDropZone = { el: HTMLElement; groupID: string | null }

const inside = (r: DOMRect, x: number, y: number) =>
  x >= r.left && x <= r.right && y >= r.top && y <= r.bottom

export function libraryDropZone(x: number, y: number): LibraryDropZone | null {
  const drawer = document.getElementById('creative-library')
  if (!drawer || drawer.hidden || !inside(drawer.getBoundingClientRect(), x, y))
    return null
  const heads = drawer.querySelectorAll<HTMLElement>('[data-drop-group]')
  for (const el of Array.from(heads))
    if (inside(el.getBoundingClientRect(), x, y))
      return { el, groupID: el.dataset.dropGroup || null }
  return { el: drawer, groupID: drawer.dataset.scopeGroup || null }
}

let hot: HTMLElement | null = null
export function markLibraryDrop(zone: LibraryDropZone | null) {
  const el = zone?.el ?? null
  if (hot === el) return
  hot?.classList.remove('is-drop-hot')
  hot = el
  hot?.classList.add('is-drop-hot')
}
