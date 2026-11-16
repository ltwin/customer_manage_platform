import { useEffect, useMemo, useRef, useState } from 'react'
import { Check, ChevronRight, ListFilter, Search, X } from 'lucide-react'
import type { CanvasNode } from './api.ts'
import { InlineNodeTitle } from './InlineNodeEditor.tsx'
import { KindIcon } from './KindIcon.tsx'
import { generationFailure, generationPhase } from './labelMeta.ts'
import { useMediaURL, useNearViewport } from './MediaView.tsx'
import {
  buildOutline,
  outlineTitle,
  outlineTypes,
  type OutlineRow,
} from './outline.ts'
import './outline.css'

const typeNames: Record<(typeof outlineTypes)[number], string> = {
  'core.image': '图片',
  'core.video': '视频',
  'core.audio': '音频',
  'core.text': '文本',
  'core.link': '链接',
  'core.scene': '3D 场景',
  'core.character': '角色卡',
  'core.group': '分组',
}

/**
 * 画布节点树：按分组嵌套列出全部节点，新建的在前；按标题搜索、按类型筛选。
 * 选中与画布双向同步——点一行选中并把画布带过去，画布上选中的在这里高亮。
 */
export default function NodeOutline({
  nodes,
  pending,
  selection,
  editable,
  active,
  onSelect,
  onRename,
}: {
  nodes: CanvasNode[]
  /** 还没被服务端确认的节点。 */
  pending: ReadonlySet<string>
  selection: string[]
  editable: boolean
  /** 节点树正显示着；变为显示时把焦点交给搜索框。 */
  active: boolean
  /** reveal 为要带到视口里的节点；修饰键点击只改选中，为 null。 */
  onSelect: (ids: string[], reveal: string | null) => void
  onRename: (id: string, title: string) => void
}) {
  const [query, setQuery] = useState('')
  const [types, setTypes] = useState<ReadonlySet<string>>(new Set())
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set())
  const [renaming, setRenaming] = useState<string | null>(null)
  const [filterOpen, setFilterOpen] = useState(false)
  const [focusID, setFocusID] = useState<string | null>(null)
  const anchor = useRef<string | null>(null)
  const search = useRef<HTMLInputElement>(null)
  const list = useRef<HTMLDivElement>(null)

  const outline = useMemo(
    () => buildOutline(nodes, { query, types, collapsed, pending }),
    [nodes, query, types, collapsed, pending],
  )
  const filtering = outline.matched !== null
  const selected = useMemo(() => new Set(selection), [selection])

  useEffect(() => {
    if (active) search.current?.focus({ preventScroll: true })
  }, [active])

  // 画布上选中的节点：在折叠分组里时展开其祖先，再滚动到可见。
  const first = selection[0]
  useEffect(() => {
    if (!first || !active) return
    if (!filtering && !outline.rows.some((r) => r.id === first)) {
      const byID = new Map(nodes.map((n) => [n.id, n]))
      const ancestors: string[] = []
      for (
        let p = byID.get(first)?.parent_id ?? null;
        p && !ancestors.includes(p);
        p = byID.get(p)?.parent_id ?? null
      )
        ancestors.push(p)
      if (ancestors.some((id) => collapsed.has(id))) {
        setCollapsed((old) => {
          const next = new Set(old)
          for (const id of ancestors) next.delete(id)
          return next
        })
        return
      }
    }
    list.current
      ?.querySelector<HTMLElement>(`[data-node="${CSS.escape(first)}"]`)
      ?.scrollIntoView({ block: 'nearest' })
    // 只跟着选中变化走；行列表变化不该把滚动拽回来。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [first, active, collapsed])

  const visibleIDs = outline.rows.map((r) => r.id)
  const focusable =
    focusID && visibleIDs.includes(focusID) ? focusID : (visibleIDs[0] ?? null)

  function toggle(id: string, open?: boolean) {
    if (filtering) return
    setCollapsed((old) => {
      const next = new Set(old)
      const shut = open === undefined ? !old.has(id) : !open
      if (shut) next.add(id)
      else next.delete(id)
      return next
    })
  }
  function click(id: string, mods: { meta: boolean; shift: boolean }) {
    setFocusID(id)
    if (mods.shift) {
      const from = visibleIDs.indexOf(anchor.current ?? selection[0] ?? id)
      const to = visibleIDs.indexOf(id)
      if (from >= 0 && to >= 0) {
        const [a, b] = from < to ? [from, to] : [to, from]
        onSelect(visibleIDs.slice(a, b + 1), null)
        return
      }
    }
    anchor.current = id
    if (mods.meta)
      onSelect(
        selected.has(id)
          ? selection.filter((v) => v !== id)
          : [...selection, id],
        null,
      )
    else onSelect([id], id)
  }
  function startRename(row: OutlineRow) {
    if (editable && row.node.capabilities.actions.includes('rename'))
      setRenaming(row.id)
  }
  function focusRow(id: string | null | undefined) {
    if (!id) return
    setFocusID(id)
    list.current
      ?.querySelector<HTMLElement>(`[data-node="${CSS.escape(id)}"]`)
      ?.focus()
  }
  function clearFilters() {
    setQuery('')
    setTypes(new Set())
    search.current?.focus()
  }

  return (
    <div className="cc-outline">
      <div className="cc-outline-tools">
        <label className="cl-find cc-outline-find">
          <Search size={14} />
          <input
            ref={search}
            type="search"
            aria-label="搜索节点名称"
            placeholder="搜索节点名称"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.nativeEvent.isComposing) return
              if (e.key === 'Escape' && query) {
                e.preventDefault()
                setQuery('')
              } else if (e.key === 'ArrowDown') {
                e.preventDefault()
                focusRow(focusable)
              }
            }}
          />
          {query && (
            <button
              type="button"
              aria-label="清空搜索"
              onClick={() => {
                setQuery('')
                search.current?.focus()
              }}
            >
              <X size={12} />
            </button>
          )}
        </label>
        <div className="cc-outline-filter">
          <button
            type="button"
            className={`cl-icon ${types.size ? 'is-on' : ''}`}
            aria-label={types.size ? `按类型筛选（已选 ${types.size} 类）` : '按类型筛选'}
            title="按类型筛选"
            aria-haspopup="menu"
            aria-expanded={filterOpen}
            onClick={() => setFilterOpen(!filterOpen)}
          >
            <ListFilter size={15} />
          </button>
          {filterOpen && (
            <TypeMenu
              types={types}
              onChange={setTypes}
              onClose={() => setFilterOpen(false)}
            />
          )}
        </div>
      </div>

      <div
        ref={list}
        className="cc-outline-list"
        role="tree"
        aria-label="画布节点"
        aria-multiselectable="true"
        onKeyDown={(e) => {
          if (renaming || e.nativeEvent.isComposing) return
          const at = visibleIDs.indexOf(focusable ?? '')
          const row = outline.rows[at]
          if (!row) return
          switch (e.key) {
            case 'ArrowDown':
              e.preventDefault()
              focusRow(visibleIDs[at + 1])
              break
            case 'ArrowUp':
              e.preventDefault()
              if (at === 0) search.current?.focus()
              else focusRow(visibleIDs[at - 1])
              break
            case 'ArrowRight':
              if (row.hasChildren && !row.expanded) {
                e.preventDefault()
                toggle(row.id, true)
              }
              break
            case 'ArrowLeft':
              e.preventDefault()
              if (row.hasChildren && row.expanded && !filtering)
                toggle(row.id, false)
              else focusRow(row.node.parent_id)
              break
            case 'Enter':
              e.preventDefault()
              click(row.id, { meta: e.metaKey || e.ctrlKey, shift: e.shiftKey })
              break
            case 'F2':
              e.preventDefault()
              startRename(row)
              break
          }
        }}
      >
        {outline.rows.map((row) => (
          <div
            key={row.id}
            className={`cc-outline-row ${selected.has(row.id) ? 'is-selected' : ''} ${filtering && !row.match ? 'is-path' : ''}`}
            style={{ paddingLeft: 8 + row.depth * 16 }}
            role="treeitem"
            data-node={row.id}
            aria-level={row.depth + 1}
            aria-selected={selected.has(row.id)}
            aria-expanded={row.hasChildren ? row.expanded : undefined}
            tabIndex={row.id === focusable ? 0 : -1}
            onFocus={() => setFocusID(row.id)}
            onClick={(e) =>
              click(row.id, {
                meta: e.metaKey || e.ctrlKey,
                shift: e.shiftKey,
              })
            }
          >
            <span className="cc-outline-twist">
              {row.hasChildren && (
                <button
                  type="button"
                  tabIndex={-1}
                  aria-label={row.expanded ? '折叠分组' : '展开分组'}
                  className={row.expanded ? 'is-open' : ''}
                  disabled={filtering}
                  onClick={(e) => {
                    e.stopPropagation()
                    toggle(row.id)
                  }}
                >
                  <ChevronRight size={13} />
                </button>
              )}
            </span>
            <Thumb node={row.node} />
            {renaming === row.id ? (
              <InlineNodeTitle
                title={row.node.metadata.title}
                disabled={!editable}
                onSave={(title) => onRename(row.id, title)}
                onClose={() => {
                  setRenaming(null)
                  focusRow(row.id)
                }}
              />
            ) : (
              <span
                className="cc-outline-title"
                title={outlineTitle(row.node)}
                onDoubleClick={(e) => {
                  e.stopPropagation()
                  startRename(row)
                }}
              >
                {outlineTitle(row.node)}
              </span>
            )}
            <Status node={row.node} />
          </div>
        ))}
        {!outline.rows.length && (
          <div className="cc-outline-empty">
            {filtering ? (
              <>
                <p>没有匹配的节点</p>
                <button type="button" className="btn" onClick={clearFilters}>
                  清除筛选
                </button>
              </>
            ) : (
              <p>画布上还没有节点</p>
            )}
          </div>
        )}
      </div>
      <footer className="cc-outline-foot">
        {filtering
          ? `${outline.matched} / 共 ${outline.total} 节点`
          : `共 ${outline.total} 节点`}
      </footer>
    </div>
  )
}

// 图片（视频有缩略图时）显示缩略图，滚到附近才取；其余类型、取不到时显示类型图标。
function Thumb({ node }: { node: CanvasNode }) {
  const { ref, near } = useNearViewport<HTMLSpanElement>()
  const content = node.content
  const media = content?.media
  const hasThumb = !!media?.some((m) => m.role === 'thumbnail')
  const show =
    content?.kind === 'image' || (content?.kind === 'video' && hasThumb)
  const { url } = useMediaURL(
    show ? content.id : null,
    media,
    hasThumb ? 'thumbnail' : 'display',
    near,
  )
  const [broken, setBroken] = useState<string | null>(null)
  return (
    <span ref={ref} className="cc-outline-thumb" aria-hidden="true">
      {url && broken !== url ? (
        <img src={url} alt="" draggable={false} onError={() => setBroken(url)} />
      ) : (
        <KindIcon typeKey={node.metadata.type_key} size={14} />
      )}
    </span>
  )
}

function Status({ node }: { node: CanvasNode }) {
  if (node.metadata.type_key === 'core.group') return null
  if (generationPhase(node.status.generation_state))
    return (
      <span className="cc-outline-status is-running" title="生成中">
        <span className="cc-outline-dot" />
        <span className="sr-only">生成中</span>
      </span>
    )
  if (generationFailure(node.status))
    return (
      <span className="cc-outline-status is-failed" title="最近一次生成失败">
        失败
      </span>
    )
  return null
}

function TypeMenu({
  types,
  onChange,
  onClose,
}: {
  types: ReadonlySet<string>
  onChange: (types: ReadonlySet<string>) => void
  onClose: () => void
}) {
  const menu = useRef<HTMLDivElement>(null)
  useEffect(() => {
    menu.current?.querySelector<HTMLElement>('[role="menuitemcheckbox"]')?.focus()
    const away = (e: PointerEvent) => {
      if (!menu.current?.parentElement?.contains(e.target as Node)) onClose()
    }
    document.addEventListener('pointerdown', away)
    return () => document.removeEventListener('pointerdown', away)
  }, [onClose])
  return (
    <div
      ref={menu}
      className="cc-outline-menu"
      role="menu"
      aria-label="节点类型"
      onKeyDown={(e) => {
        const items = Array.from(
          menu.current?.querySelectorAll<HTMLElement>(
            '[role^="menuitem"]:not(:disabled)',
          ) ?? [],
        )
        const at = items.indexOf(document.activeElement as HTMLElement)
        if (e.key === 'Escape') {
          e.preventDefault()
          e.stopPropagation()
          onClose()
        } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
          e.preventDefault()
          const next =
            (at + (e.key === 'ArrowDown' ? 1 : -1) + items.length) %
            items.length
          items[next]?.focus()
        }
      }}
    >
      {outlineTypes.map((t) => {
        const on = types.has(t)
        return (
          <button
            key={t}
            type="button"
            role="menuitemcheckbox"
            aria-checked={on}
            onClick={() => {
              const next = new Set(types)
              if (on) next.delete(t)
              else next.add(t)
              onChange(next)
            }}
          >
            <span className="cc-outline-check">{on && <Check size={13} />}</span>
            <KindIcon typeKey={t} size={14} />
            {typeNames[t]}
          </button>
        )
      })}
      <div className="cc-outline-menu-line" />
      <button
        type="button"
        role="menuitem"
        disabled={!types.size}
        onClick={() => onChange(new Set())}
      >
        <span className="cc-outline-check" />
        显示全部类型
      </button>
    </div>
  )
}
