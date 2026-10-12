import {
  Check,
  Maximize2,
  ChevronDown,
  Heart,
  LayoutDashboard,
  LayoutGrid,
  PanelRightClose,
  PanelRightOpen,
  Search,
  Star,
  Tag,
  X,
} from 'lucide-react'
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import type * as api from '../editor/api.ts'
import { Masonry, TileFace } from '../editor/LibraryTiles.tsx'
import { tileRatio } from '../editor/libraryState.ts'
import Picker from '../editor/Picker.tsx'
import { Stars } from '../editor/StarRating.tsx'
import TagPopover from '../editor/TagPopover.tsx'
import {
  KINDS,
  RATINGS,
  SIZE,
  SORTS,
  VIEWS,
  clearFilters,
  narrowed,
  writeQuery,
  type LibraryQuery,
  type Prefs,
  type View,
} from './query.ts'
import { boxSelection, type Modifiers, type Box } from './selection.ts'
import type { Point } from './popover.ts'
import type { LibraryCatalog, useAssetList } from './useLibrary.ts'

type List = ReturnType<typeof useAssetList>
const GAP = 12

/**
 * 浏览区（V6-067 AC-03）：标题与面包屑、搜索、排序、布局；类型分段带数量；
 * 评分和标签筛选；生效的条件以 pill 列出；进入分组后显示子分组 chip。
 */
export default function AssetBrowser({
  query,
  catalog,
  list,
  prefs,
  selected,
  onQuery,
  onView,
  onGroup,
  onPrefs,
  onSelect,
  onRetry,
  onBox,
  onMenu,
  onOpen,
  toolbar,
}: {
  query: LibraryQuery
  catalog: LibraryCatalog | null
  list: List
  prefs: Prefs
  selected: string[]
  onQuery: (q: LibraryQuery) => void
  onView: (view: View) => void
  onGroup: (id: string) => void
  onPrefs: (p: Prefs) => void
  onSelect: (id: string | null, keys?: Modifiers) => void
  onRetry: () => void
  onBox: (ids: string[]) => void
  onMenu: (asset: api.Asset, at: Point) => void
  onOpen: (asset: api.Asset) => void
  toolbar?: React.ReactNode
}) {
  const groups = catalog?.groups ?? []
  const group = groups.find((g) => g.id === query.group)
  const parent = groups.find((g) => g.id === group?.parent_id)
  const tagName = (id: string) =>
    catalog?.tags.find((t) => t.id === id)?.name ?? '已删除的标签'
  const [tagsAt, setTagsAt] = useState<DOMRect | null>(null)
  const kinds = list.facets?.kinds
  const total = kinds ? Object.values(kinds).reduce((a, b) => a + b, 0) : null
  const filtered = narrowed(query)
  const children =
    group && !query.q ? groups.filter((g) => g.parent_id === group.id) : []
  const toggleTag = (id: string) =>
    onQuery({
      ...query,
      tags: query.tags.includes(id)
        ? query.tags.filter((t) => t !== id)
        : [...query.tags, id],
    })

  return (
    <section className="al-board" aria-label="资产" tabIndex={-1}>
      <header className="al-head">
        <div className="al-crumb">
          <h2>
            {group ? (
              <>
                {parent && (
                  <>
                    <button
                      type="button"
                      className="al-crumb-name"
                      title={parent.name}
                      onClick={() => onGroup(parent.id)}
                    >
                      {parent.name}
                    </button>
                    <span className="al-sep">/</span>
                  </>
                )}
                <span className="al-crumb-name" title={group.name}>
                  {group.name}
                </span>
              </>
            ) : (
              VIEWS.find((v) => v.key === query.view)?.label
            )}
          </h2>
          {list.page && <small>{list.page.total_count} 项</small>}
        </div>
        <SearchBox value={query.q} onSearch={(q) => onQuery({ ...query, q })} />
        <Picker
          label="排序"
          options={SORTS.map((s) => ({ value: s.value, label: s.label }))}
          value={query.sort}
          align="end"
          minWidth={140}
          onChange={(sort) =>
            onQuery({ ...query, sort: sort as LibraryQuery['sort'] })
          }
        />
        <div className="al-seg al-seg-icon" role="group" aria-label="布局">
          <button
            type="button"
            aria-pressed={prefs.layout === 'masonry'}
            aria-label="瀑布流"
            title="瀑布流"
            onClick={() => onPrefs({ ...prefs, layout: 'masonry' })}
          >
            <LayoutDashboard size={14} />
          </button>
          <button
            type="button"
            aria-pressed={prefs.layout === 'grid'}
            aria-label="方格"
            title="方格"
            onClick={() => onPrefs({ ...prefs, layout: 'grid' })}
          >
            <LayoutGrid size={14} />
          </button>
        </div>
        <input
          className="al-zoom"
          type="range"
          min={SIZE.min}
          max={SIZE.max}
          step={SIZE.step}
          value={prefs.size}
          aria-label="缩略图大小"
          onChange={(e) =>
            onPrefs({ ...prefs, size: Number(e.currentTarget.value) })
          }
        />
        <button
          type="button"
          className="al-icon-btn al-inspector-toggle"
          aria-pressed={prefs.inspector}
          aria-label="检查器"
          title={prefs.inspector ? '收起检查器' : '打开检查器'}
          onClick={() => onPrefs({ ...prefs, inspector: !prefs.inspector })}
        >
          {prefs.inspector ? (
            <PanelRightClose size={16} />
          ) : (
            <PanelRightOpen size={16} />
          )}
        </button>
      </header>

      <PhoneViews
        query={query}
        groups={groups}
        onView={onView}
        onGroup={onGroup}
      />

      <div className="al-filters">
        <div className="al-seg" role="group" aria-label="类型">
          <button
            type="button"
            aria-pressed={!query.kind}
            onClick={() => onQuery({ ...query, kind: '' })}
          >
            全部{total !== null && <small>{total}</small>}
          </button>
          {KINDS.filter(
            (k) => query.kind === k.key || (kinds?.[k.key] ?? 0) > 0,
          ).map((k) => (
            <button
              key={k.key}
              type="button"
              aria-pressed={query.kind === k.key}
              onClick={() => onQuery({ ...query, kind: k.key })}
            >
              {k.label}
              {kinds && <small>{kinds[k.key]}</small>}
            </button>
          ))}
        </div>
        <Picker
          label="评分"
          className={query.rating ? 'is-on' : ''}
          options={[
            { value: '', label: '不限' },
            ...RATINGS.map((r) => ({ value: r.value, label: r.label })),
          ]}
          value={query.rating}
          minWidth={140}
          onChange={(rating) =>
            onQuery({ ...query, rating: rating as LibraryQuery['rating'] })
          }
        >
          <Star size={13} />
          <span>
            {RATINGS.find((r) => r.value === query.rating)?.label ?? '评分'}
          </span>
          <ChevronDown size={12} />
        </Picker>
        <button
          type="button"
          className={`al-chip${query.tags.length ? ' is-on' : ''}`}
          aria-expanded={!!tagsAt}
          aria-haspopup="dialog"
          onClick={(e) =>
            setTagsAt(tagsAt ? null : e.currentTarget.getBoundingClientRect())
          }
        >
          <Tag size={13} />
          {query.tags.length ? `标签 · ${query.tags.length}` : '标签'}
          <ChevronDown size={12} />
        </button>
      </div>

      {(filtered || query.tags.length > 0) && (
        <div className="al-pills" aria-label="生效的条件">
          {query.rating && (
            <Pill
              label={RATINGS.find((r) => r.value === query.rating)?.label ?? ''}
              onRemove={() => onQuery({ ...query, rating: '' })}
            />
          )}
          {query.tags.map((id) => (
            <Pill key={id} label={tagName(id)} onRemove={() => toggleTag(id)} />
          ))}
          {query.tags.length > 1 && (
            <button
              type="button"
              className="al-pill al-pill-mode"
              aria-label={`标签条件：${query.mode === 'all' ? '同时满足' : '满足任一'}，点击切换`}
              onClick={() =>
                onQuery({
                  ...query,
                  mode: query.mode === 'all' ? 'any' : 'all',
                })
              }
            >
              {query.mode === 'all' ? '同时满足' : '满足任一'}
            </button>
          )}
          {query.q && (
            <Pill
              label={`“${query.q}”`}
              onRemove={() => onQuery({ ...query, q: '' })}
            />
          )}
          <button
            type="button"
            className="al-pill al-pill-clear"
            onClick={() => onQuery(clearFilters(query))}
          >
            清除全部
          </button>
        </div>
      )}

      {children.length > 0 && (
        <div className="al-subgroups" aria-label="子分组">
          {children.map((c) => (
            <button
              key={c.id}
              type="button"
              className="al-subgroup"
              onClick={() => onGroup(c.id)}
            >
              <span title={c.name}>{c.name}</span>
              <small>{catalog?.counts[c.id] ?? 0}</small>
            </button>
          ))}
        </div>
      )}

      <Tiles
        query={query}
        list={list}
        prefs={prefs}
        selected={selected}
        filtered={filtered}
        onSelect={onSelect}
        onBox={onBox}
        onMenu={onMenu}
        onOpen={onOpen}
        onRetry={onRetry}
        onClear={() => onQuery(clearFilters(query))}
        onWiden={() => onQuery({ ...query, view: 'all', group: '' })}
      />

      {toolbar}
      {tagsAt && catalog && (
        <TagPopover
          anchor={tagsAt}
          tags={catalog.tags}
          categories={catalog.categories}
          selected={query.tags}
          mode={query.mode}
          counts={
            list.facets
              ? new Map(list.facets.tags.map((t) => [t.tag_id, t.count]))
              : null
          }
          onToggle={toggleTag}
          onMode={() =>
            onQuery({ ...query, mode: query.mode === 'all' ? 'any' : 'all' })
          }
          onClear={() => onQuery({ ...query, tags: [], mode: 'all' })}
          onClose={() => setTagsAt(null)}
        />
      )}
    </section>
  )
}

function Pill({ label, onRemove }: { label: string; onRemove: () => void }) {
  return (
    <span className="al-pill">
      <span title={label}>{label}</span>
      <button
        type="button"
        aria-label={`移除条件：${label}`}
        onClick={onRemove}
      >
        <X size={11} />
      </button>
    </span>
  )
}

/** 搜索框：输入防抖 300ms 再生效，输入法组字时不提交；按 / 聚焦。 */
function SearchBox({
  value,
  onSearch,
}: {
  value: string
  onSearch: (q: string) => void
}) {
  const input = useRef<HTMLInputElement>(null)
  // 草稿跟着网址里的搜索词走：网址被别处改了（移除 pill、换视图）就显示网址里的。
  const [draft, setDraft] = useState({ q: value, text: value })
  const [composing, setComposing] = useState(false)
  const text = draft.q === value ? draft.text : value
  const search = useRef(onSearch)
  search.current = onSearch
  useEffect(() => {
    const next = text.trim().slice(0, 200)
    if (composing || next === value) return
    const timer = setTimeout(() => {
      setDraft({ q: next, text })
      search.current(next)
    }, 300)
    return () => clearTimeout(timer)
  }, [text, value, composing])
  useEffect(() => {
    const focus = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement | null
      if (
        e.key !== '/' ||
        e.metaKey ||
        e.ctrlKey ||
        t?.closest(
          'input, textarea, [contenteditable="true"], [role="dialog"], [role="alertdialog"]',
        )
      )
        return
      e.preventDefault()
      input.current?.focus()
    }
    document.addEventListener('keydown', focus)
    return () => document.removeEventListener('keydown', focus)
  }, [])
  return (
    <label className="al-search">
      <Search size={14} />
      <input
        ref={input}
        type="search"
        aria-label="搜索素材"
        placeholder="搜索标题、备注、正文、网址、标签"
        maxLength={200}
        value={text}
        onChange={(e) => setDraft({ q: value, text: e.currentTarget.value })}
        onCompositionStart={() => setComposing(true)}
        onCompositionEnd={() => setComposing(false)}
      />
      <kbd aria-hidden="true">/</kbd>
    </label>
  )
}

/** 手机上没有资料库树：视图和分组收进顶部的「视图」下拉。 */
function PhoneViews({
  query,
  groups,
  onView,
  onGroup,
}: {
  query: LibraryQuery
  groups: api.Group[]
  onView: (view: View) => void
  onGroup: (id: string) => void
}) {
  return (
    <label className="al-phone-views">
      <span>视图</span>
      <select
        value={query.group ? `g:${query.group}` : `v:${query.view}`}
        onChange={(e) => {
          const [kind, id] = [
            e.currentTarget.value.slice(0, 2),
            e.currentTarget.value.slice(2),
          ]
          if (kind === 'g:') onGroup(id)
          else onView(id as View)
        }}
      >
        {VIEWS.map((v) => (
          <option key={v.key} value={`v:${v.key}`}>
            {v.label}
          </option>
        ))}
        {groups.length > 0 && (
          <optgroup label="分组">
            {groups
              .filter((g) => !g.parent_id)
              .flatMap((g) => [
                g,
                ...groups.filter((c) => c.parent_id === g.id),
              ])
              .map((g) => (
                <option key={g.id} value={`g:${g.id}`}>
                  {g.parent_id ? `　${g.name}` : g.name}
                </option>
              ))}
          </optgroup>
        )}
      </select>
    </label>
  )
}

function Tiles({
  query,
  list,
  prefs,
  selected,
  filtered,
  onSelect,
  onRetry,
  onClear,
  onWiden,
  onBox,
  onMenu,
  onOpen,
}: {
  query: LibraryQuery
  list: List
  prefs: Prefs
  selected: string[]
  filtered: boolean
  onSelect: (id: string | null, keys?: Modifiers) => void
  onRetry: () => void
  onClear: () => void
  onWiden: () => void
  onBox: (ids: string[]) => void
  onMenu: (asset: api.Asset, at: Point) => void
  onOpen: (asset: api.Asset) => void
}) {
  const dragBox = useRef<{
    x: number
    y: number
    base: string[]
    moved: boolean
  } | null>(null)
  const suppressClick = useRef(false)
  const [marquee, setMarquee] = useState<Box | null>(null)
  const scroller = useRef<HTMLDivElement>(null)
  const box = useRef<HTMLDivElement>(null)
  const end = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)
  // 条件变了就回到顶部，从新结果的第一项看起。
  const queryKey = writeQuery(query).toString()
  useEffect(() => {
    scroller.current?.scrollTo?.({ top: 0 })
  }, [queryKey])
  useLayoutEffect(() => {
    const el = box.current
    if (!el || typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(([entry]) =>
      setWidth(entry?.contentRect.width ?? 0),
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [])
  // 滚到底部附近再取下一页。
  const loadMore = useRef(list.loadMore)
  loadMore.current = list.loadMore
  const items = list.page?.items
  useEffect(() => {
    const el = end.current
    if (!el || typeof IntersectionObserver === 'undefined') return
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry?.isIntersecting) loadMore.current()
      },
      { rootMargin: '600px 0px' },
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [items])
  // 手机上没有缩放滑杆，至少排两列，卡片不至于占满一屏。
  const columns = Math.max(
    width <= 560 ? 2 : 1,
    Math.floor((width + GAP) / (prefs.size + GAP)),
  )
  const card = (a: api.Asset) => (
    <AssetCard
      key={a.id}
      asset={a}
      square={prefs.layout === 'grid'}
      selected={selected.includes(a.id)}
      selection={selected}
      onSelect={onSelect}
      onMenu={onMenu}
      onOpen={onOpen}
    />
  )
  return (
    <div
      ref={scroller}
      className="al-scroll"
      onPointerDown={(e) => {
        if (
          e.button !== 0 ||
          !(e.target instanceof Element) ||
          e.target.closest('.al-tile, button, input, textarea, .al-empty')
        )
          return
        dragBox.current = {
          x: e.clientX,
          y: e.clientY,
          base: e.metaKey || e.ctrlKey ? selected : [],
          moved: false,
        }
        e.currentTarget.setPointerCapture(e.pointerId)
      }}
      onPointerMove={(e) => {
        const start = dragBox.current
        if (
          !start ||
          Math.abs(e.clientX - start.x) + Math.abs(e.clientY - start.y) < 6
        )
          return
        start.moved = true
        const bounds = e.currentTarget.getBoundingClientRect()
        const rect = {
          left: Math.max(bounds.left, Math.min(start.x, e.clientX)),
          right: Math.min(bounds.right, Math.max(start.x, e.clientX)),
          top: Math.max(bounds.top, Math.min(start.y, e.clientY)),
          bottom: Math.min(bounds.bottom, Math.max(start.y, e.clientY)),
        }
        setMarquee(rect)
        const cards = Array.from(
          box.current?.querySelectorAll<HTMLElement>('[data-asset]') ?? [],
        ).map((el) => ({
          id: el.dataset.asset!,
          ...(() => {
            const r = el.getBoundingClientRect()
            return {
              left: r.left,
              right: r.right,
              top: r.top,
              bottom: r.bottom,
            }
          })(),
        }))
        onBox(boxSelection(start.base, cards, rect))
      }}
      onPointerUp={(e) => {
        suppressClick.current = !!dragBox.current?.moved
        if (e.currentTarget.hasPointerCapture(e.pointerId))
          e.currentTarget.releasePointerCapture(e.pointerId)
        dragBox.current = null
        setMarquee(null)
      }}
      onPointerCancel={() => {
        dragBox.current = null
        setMarquee(null)
      }}
      onClick={(e) => {
        if (suppressClick.current) {
          suppressClick.current = false
          return
        }
        if (
          e.target instanceof Element &&
          !e.target.closest('.al-tile, button, input, textarea')
        )
          onSelect(null)
      }}
    >
      {marquee && (
        <div
          className="al-marquee"
          style={{
            left: marquee.left,
            top: marquee.top,
            width: marquee.right - marquee.left,
            height: marquee.bottom - marquee.top,
          }}
        />
      )}
      {list.error && (
        <div className="al-state" role="alert">
          <p>{list.error}</p>
          <button type="button" className="btn" onClick={onRetry}>
            重试
          </button>
        </div>
      )}
      {list.stale && (
        <div className="al-state al-state-inline" role="status">
          <p>资产库刚有变化，后面的内容需要重新读取。</p>
          <button type="button" className="btn" onClick={onRetry}>
            刷新
          </button>
        </div>
      )}
      <div
        ref={box}
        className={`al-tiles${prefs.layout === 'grid' ? ' is-grid' : ''}`}
        style={{ '--al-size': `${prefs.size}px` } as React.CSSProperties}
        aria-busy={list.loading}
      >
        {items &&
          (prefs.layout === 'grid' ? (
            items.map(card)
          ) : (
            <Masonry items={items} columns={columns} tile={card} />
          ))}
      </div>
      {list.loading && !list.error && (
        <p className="al-loading" role="status">
          正在读取素材…
        </p>
      )}
      {items?.length === 0 && (
        <div className="al-empty">
          {filtered ? (
            <>
              <strong>没有符合条件的素材</strong>
              <span>放宽条件，或者到「全部」里找找。</span>
              <div className="al-empty-btns">
                <button type="button" className="btn" onClick={onClear}>
                  清除条件
                </button>
                {(query.group || query.view !== 'all') && (
                  <button type="button" className="btn" onClick={onWiden}>
                    在全部里查找
                  </button>
                )}
              </div>
            </>
          ) : query.view === 'trash' && !query.group ? (
            <strong>回收站是空的</strong>
          ) : (
            <>
              <strong>这里还没有素材</strong>
              <span>
                {query.group
                  ? '在画布或其他视图里把素材归入这个分组，就会出现在这里。'
                  : '在画布里上传或生成的内容存入资产库后，会出现在这里。'}
              </span>
            </>
          )}
        </div>
      )}
      <div ref={end} className="al-end">
        {list.more === 'loading' && <span role="status">正在加载更多…</span>}
        {list.more === 'error' && (
          <button type="button" className="btn" onClick={list.loadMore}>
            加载失败，重试
          </button>
        )}
      </div>
    </div>
  )
}

/** 卡片下方只放标题（仅图片和视频）和只读星级（V6-067 AC-09）。 */
function AssetCard({
  asset: a,
  square,
  selected,
  onSelect,
  onMenu,
  onOpen,
  selection,
}: {
  asset: api.Asset
  square: boolean
  selected: boolean
  onSelect: (id: string | null, keys?: Modifiers) => void
  onMenu: (asset: api.Asset, at: Point) => void
  onOpen: (asset: api.Asset) => void
  selection: string[]
}) {
  const visual = a.kind === 'image' || a.kind === 'video'
  return (
    <div
      className={`al-tile${selected ? ' is-on' : ''}`}
      data-asset={a.id}
      data-kind={a.kind}
      role="button"
      tabIndex={0}
      aria-pressed={selected}
      aria-label={a.title}
      draggable={!a.deleted_at}
      onDragStart={(e) => {
        const ids = selected ? selection : [a.id]
        if (!selected) onSelect(a.id)
        e.dataTransfer.setData(
          'application/creative-library-selection',
          ids.join(','),
        )
        e.dataTransfer.effectAllowed = 'copy'
      }}
      onContextMenu={(e) => {
        e.preventDefault()
        onMenu(a, { x: e.clientX, y: e.clientY })
      }}
      onClick={(e) => {
        e.currentTarget.focus()
        onSelect(a.id, { additive: e.metaKey || e.ctrlKey, shift: e.shiftKey })
      }}
      onDoubleClick={(e) => {
        if (!(e.target instanceof Element) || !e.target.closest('button')) {
          e.currentTarget.focus()
          onOpen(a)
        }
      }}
      onKeyDown={(e) => {
        if (e.target !== e.currentTarget || !['Enter', ' '].includes(e.key))
          return
        e.preventDefault()
        onSelect(a.id)
        onOpen(a)
      }}
    >
      <button
        type="button"
        className="al-tick"
        aria-label={`选择：${a.title}`}
        aria-pressed={selected}
        onClick={(e) => {
          e.stopPropagation()
          onSelect(a.id, { additive: true })
        }}
      >
        <Check size={11} />
      </button>
      <button
        type="button"
        className="al-view"
        aria-label={`查看：${a.title}`}
        onClick={(e) => {
          e.stopPropagation()
          e.currentTarget.closest<HTMLElement>('.al-tile')?.focus()
          onOpen(a)
        }}
      >
        <Maximize2 size={13} />
      </button>
      <div
        className={`al-thumb${visual ? '' : ' is-card'}`}
        style={
          visual && !square ? { aspectRatio: `1 / ${tileRatio(a)}` } : undefined
        }
      >
        <TileFace asset={a} />
        {a.is_favorite && (
          <span className="al-fav" role="img" aria-label="最爱">
            <Heart size={12} />
          </span>
        )}
      </div>
      {visual ? (
        <div className="al-cap">
          <span>{a.title}</span>
          <Stars value={a.rating} />
        </div>
      ) : (
        a.rating !== null && (
          <div className="al-cap">
            <span />
            <Stars value={a.rating} />
          </div>
        )
      )}
    </div>
  )
}
