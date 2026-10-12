import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Check, Search } from 'lucide-react'
import type * as api from './api.ts'
import './library.css'

/** 标签筛选弹层：按分类列出，带当前条件下的数量。抽屉和资产库页共用。 */
export default function TagPopover({
  anchor,
  tags: allTags,
  categories,
  selected,
  mode,
  counts,
  onToggle,
  onMode,
  onClear,
  onClose,
}: {
  anchor: DOMRect
  tags: api.Tag[]
  categories: api.Category[]
  selected: string[]
  mode: string
  /** 当前条件下每个标签的数量；还在读取时为空。 */
  counts: Map<string, number> | null
  onToggle: (id: string) => void
  onMode: () => void
  onClear: () => void
  onClose: () => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  const [q, setQ] = useState('')
  const close = useRef(onClose)
  close.current = onClose
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    el.style.left = `${Math.max(8, Math.min(anchor.right - el.offsetWidth, window.innerWidth - el.offsetWidth - 8))}px`
    el.style.top = `${Math.min(anchor.bottom + 4, window.innerHeight - el.offsetHeight - 8)}px`
  }, [anchor])
  useEffect(() => {
    const outside = (e: PointerEvent) => {
      if (e.target instanceof Node && !ref.current?.contains(e.target))
        close.current()
    }
    document.addEventListener('pointerdown', outside, true)
    return () => document.removeEventListener('pointerdown', outside, true)
  }, [])
  const match = (t: api.Tag) =>
    !q.trim() || t.name.toLowerCase().includes(q.trim().toLowerCase())
  const cats: { id: string | null; name: string }[] = [
    ...categories.map((c) => ({ id: c.id as string | null, name: c.name })),
    { id: null, name: '未分类' },
  ]
  return (
    <div
      ref={ref}
      className="cl-tag-pop"
      role="dialog"
      aria-label="标签筛选"
      onKeyDown={(e) => {
        e.stopPropagation()
        if (e.key === 'Escape') {
          e.preventDefault()
          onClose()
        }
      }}
    >
      <label className="cl-find">
        <Search size={13} />
        <input
          aria-label="筛选标签"
          placeholder="查找标签"
          value={q}
          autoFocus
          onChange={(e) => setQ(e.target.value)}
        />
      </label>
      <div className="cl-tag-list">
        {cats.map((c) => {
          const tags = allTags.filter(
            (t) => (t.category_id ?? null) === c.id && match(t),
          )
          if (!tags.length) return null
          return (
            <div key={c.id ?? 'none'}>
              <div className="cl-tag-cat">{c.name}</div>
              {tags.map((t) => (
                <button
                  key={t.id}
                  type="button"
                  className="cl-tag-item"
                  aria-pressed={selected.includes(t.id)}
                  onClick={() => onToggle(t.id)}
                >
                  <i style={{ background: t.color }} />
                  <span>{t.name}</span>
                  <small className="cl-tag-n">{counts?.get(t.id) ?? ''}</small>
                  {selected.includes(t.id) ? (
                    <Check size={13} />
                  ) : (
                    <span className="cl-tag-blank" aria-hidden="true" />
                  )}
                </button>
              ))}
            </div>
          )
        })}
        {!allTags.some(match) && (
          <p className="cl-sec-empty">
            {allTags.length ? '没有匹配的标签' : '还没有标签'}
          </p>
        )}
      </div>
      <div className="cl-tag-foot">
        {/* 选中两个及以上才有「同时满足 / 满足任一」之分 */}
        {selected.length > 1 && (
          <button type="button" onClick={onMode}>
            {mode === 'all' ? '同时满足全部所选' : '满足任一所选'}
          </button>
        )}
        <span className="cl-grow" />
        <button type="button" disabled={!selected.length} onClick={onClear}>
          清除
        </button>
      </div>
    </div>
  )
}
