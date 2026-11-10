import { useEffect, useMemo, useRef, useState, type DragEvent, type KeyboardEvent, type MouseEvent, type PointerEvent } from 'react'
import { ExternalLink, Grid2x2, Grid3x3, Search, Upload } from 'lucide-react'
import * as api from '../../creative-canvas/editor/api.ts'
import { MediaFigure } from '../../creative-canvas/editor/MediaView.tsx'
import { groupOptions } from '../../creative-canvas/editor/libraryState.ts'
import { blank, type LibraryQuery } from '../../creative-canvas/assets/query.ts'
import { boxSelection, clickSelection, type Box, type Selection } from '../../creative-canvas/assets/selection.ts'
import { useAssetList, useCatalog } from '../../creative-canvas/assets/useLibrary.ts'
import { assetMaterial, materialRows, modelInfo } from '../../creative-canvas/viewer/modelInfo.ts'
import { builtinSlot, filterBuiltin, type BuiltinMaterial } from '../../creative-canvas/assets/builtin.ts'
import type { LibraryAsset } from '../saveModel.ts'
import { ContextMenu, type MenuItem } from './ContextMenu.tsx'

type Size = 'small' | 'large'
type Kind = 'model' | 'material'
type MaterialSlot = NonNullable<Parameters<typeof materialRows>[0]>
const kindName: Record<Kind, string> = { model: '模型', material: '材质' }
const sizeKey = 'creative-scene-assets:size'
const none: Selection = { ids: [], anchor: null }

function readSize(): Size {
  try {
    return localStorage.getItem(sizeKey) === 'large' ? 'large' : 'small'
  } catch {
    return 'small'
  }
}
function writeSize(size: Size) {
  try {
    localStorage.setItem(sizeKey, size)
  } catch {
    // Only a display preference.
  }
}

/** A model's: how it stands, its size, and what can be changed after placing; a material's: what it is made of. */
function facts(a: LibraryAsset): string[] {
  const content = a.unavailable ? undefined : a.content
  const material = assetMaterial(content)
  if (material) return materialRows(material).map(([label, value]) => `${label}：${value}`)
  const info = modelInfo(content)
  return info ? [`参考点${info.origin} · ${info.size}`, `${info.params.length} 个可调参数 · ${info.slots.length} 个材质槽`] : []
}

/**
 * The editor's own asset panel: the account's model assets, to put into the
 * scene, and its material assets, dragged onto a slot (C5c1). A material
 * goes on the slot it is let go over, in the viewport or the properties; it
 * is not placed, so a double click or Enter leaves it be. Cards select as on the library page: a click, Cmd/Ctrl to add or
 * remove, Shift for a range, a drag over the blank to box them; a click on the
 * blank clears. A double click or Enter puts that card's model under the
 * view's centre, 「放入场景」 puts all selected ones in a row there, a drag
 * puts one where it is let go. The panel stays open, so more can follow.
 * Organizing the library is left to the library page.
 */
export function AssetPanel({
  refresh,
  hidden,
  recent,
  readOnly,
  focus,
  placeable,
  onPlace,
  onDrag,
  onDragBuiltin,
  onExit,
  onImport,
}: {
  /** Changes when the page wrote to the library (存入资产库); the lists are read again. */
  refresh: number
  /** Another sidebar tab is showing. The panel stays mounted, so its lists, scroll and selection survive the switch. */
  hidden: boolean
  /** Ids of the models last put into a scene here, newest first. */
  recent: readonly string[]
  readOnly: boolean
  /** Changes when the search box should take the keys (Shift+A 从资产库添加…, the shortcut). */
  focus: number
  /** Whether an asset can go into a scene: a usable model with its generator, or a material. */
  placeable: (asset: LibraryAsset) => boolean
  /** Puts these models into the scene, as one edit. */
  onPlace: (assets: LibraryAsset[]) => void
  /** A drag towards the viewport or a slot begins (the asset) or ends (null). */
  onDrag: (asset: LibraryAsset | null) => void
  /** A built-in textured material (C5c2) is dragged towards the viewport or a slot (the slot it writes), or the drag ends (null). */
  onDragBuiltin: (material: { title: string; slot: MaterialSlot } | null) => void
  /** Escape in an empty search box: keys go back to the editor. */
  onExit: () => void
  /** 「导入模型」: picks a GLB and imports it into the library (scene-glb-import); without it the button is hidden. */
  onImport?: () => void
}) {
  const [kind, setKind] = useState<Kind>('model')
  /** Which materials show: the account's (我的) or the built-in ones (内置), never mixed. */
  const [source, setSource] = useState<'mine' | 'builtin'>('mine')
  const [builtinPick, setBuiltinPick] = useState<BuiltinMaterial | null>(null)
  const [text, setText] = useState('')
  const [q, setQ] = useState('')
  const [group, setGroup] = useState('')
  const [tag, setTag] = useState('')
  const [size, setSize] = useState(readSize)
  const [selection, setSelection] = useState<Selection>(none)
  const [menu, setMenu] = useState<{ x: number; y: number; assets: LibraryAsset[] } | null>(null)
  const [marquee, setMarquee] = useState<Box | null>(null)
  const dragBox = useRef<{ x: number; y: number; base: string[]; moved: boolean } | null>(null)
  const suppressClick = useRef(false)
  const search = useRef<HTMLInputElement>(null)
  const body = useRef<HTMLDivElement>(null)
  const end = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const t = window.setTimeout(() => setQ(text.trim().slice(0, 200)), 250)
    return () => window.clearTimeout(t)
  }, [text])
  useEffect(() => {
    if (focus) search.current?.focus()
  }, [focus])

  const query = useMemo<LibraryQuery>(() => ({ ...blank(), kind, q, group, tags: tag ? [tag] : [] }), [kind, q, group, tag])
  const { catalog } = useCatalog(refresh)
  const list = useAssetList(query, refresh)
  const items = useMemo(() => (list.page?.items ?? []).filter((a) => !a.deleted_at), [list.page])
  const narrowed = !!(q || group || tag)

  // Recent models may be outside the current page, so those are read one by
  // one. What is known (read before, or on the page) is not read again: putting
  // a model in only reorders the list. Null marks one that is gone.
  const [recentAssets, setRecentAssets] = useState<LibraryAsset[]>([])
  const known = useRef({ refresh, assets: new Map<string, LibraryAsset | null>() })
  const recentKey = recent.join(',')
  useEffect(() => {
    if (known.current.refresh !== refresh) known.current = { refresh, assets: new Map() }
    const cache = known.current.assets
    for (const a of list.page?.items ?? []) cache.set(a.id, a)
    const ids = recentKey ? recentKey.split(',') : []
    const pick = () => ids.flatMap((id) => {
      const a = cache.get(id)
      return a && a.kind === 'model' && !a.deleted_at ? [a] : []
    })
    setRecentAssets(pick())
    const missing = ids.filter((id) => !cache.has(id))
    if (!missing.length) return
    const controller = new AbortController()
    void Promise.all(missing.map((id) => api.read<LibraryAsset>(`/assets/${encodeURIComponent(id)}`, controller.signal).catch(() => null))).then((found) => {
      if (controller.signal.aborted) return
      missing.forEach((id, i) => cache.set(id, found[i] ?? null))
      setRecentAssets(pick())
    })
    return () => controller.abort()
  }, [recentKey, refresh, list.page])

  const loadMore = list.loadMore
  useEffect(() => {
    const el = end.current
    if (!el) return
    const seen = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) loadMore()
    })
    seen.observe(el)
    return () => seen.disconnect()
  }, [loadMore])

  // 最近使用 is the models put in here; materials have none.
  const shownRecent = narrowed || kind !== 'model' ? [] : recentAssets
  // A model in 最近使用 and the list is one selection; the list's copy is newer.
  const byID = new Map([...shownRecent, ...items].map((a) => [a.id, a]))
  const selected = selection.ids.flatMap((id) => byID.get(id) ?? [])
  const canPlace = (a: LibraryAsset) => !readOnly && placeable(a)
  const place = (assets: LibraryAsset[]) => {
    const ok = assets.filter((a) => a.kind === 'model' && canPlace(a))
    if (ok.length) onPlace(ok)
  }
  const openInLibrary = (a: LibraryAsset) =>
    window.open(`/creative/library?${new URLSearchParams({ kind: a.kind, q: a.title }).toString()}`, '_blank', 'noopener')

  /** The cards under a dragged box, joined to what Cmd/Ctrl kept. */
  const boxAt = (e: PointerEvent<HTMLDivElement>) => {
    const start = dragBox.current
    if (!start || Math.abs(e.clientX - start.x) + Math.abs(e.clientY - start.y) < 6) return
    start.moved = true
    const bounds = e.currentTarget.getBoundingClientRect()
    const rect = {
      left: Math.max(bounds.left, Math.min(start.x, e.clientX)),
      right: Math.min(bounds.right, Math.max(start.x, e.clientX)),
      top: Math.max(bounds.top, Math.min(start.y, e.clientY)),
      bottom: Math.min(bounds.bottom, Math.max(start.y, e.clientY)),
    }
    setMarquee(rect)
    const cards = Array.from(body.current?.querySelectorAll<HTMLElement>('[data-asset]') ?? []).map((el) => {
      const r = el.getBoundingClientRect()
      return { id: el.dataset.asset ?? '', left: r.left, right: r.right, top: r.top, bottom: r.bottom }
    })
    const ids = boxSelection(start.base, cards, rect)
    setSelection({ ids, anchor: ids.at(-1) ?? null })
  }

  const card = (a: LibraryAsset, order: string[]) => {
    const on = selection.ids.includes(a.id)
    return (
      <div
        key={a.id}
        data-asset={a.id}
        className={`cc-scene-asset${on ? ' is-selected' : ''}${placeable(a) ? '' : ' is-unusable'}`}
        role="option"
        aria-selected={on}
        aria-label={a.title}
        tabIndex={0}
        title={!placeable(a) ? `${a.title} · 这个${kindName[kind]}不可用` : a.kind === 'material' ? `${a.title} · 拖到物件或槽位上` : `${a.title} · 双击放入场景`}
        draggable={canPlace(a)}
        onClick={(e: MouseEvent<HTMLElement>) => {
          // Keys (Enter, Esc) go to this card, also when the click landed on the thumbnail's retry.
          e.currentTarget.focus()
          setSelection((s) => clickSelection(s, a.id, order, { additive: e.metaKey || e.ctrlKey, shift: e.shiftKey }))
        }}
        onDoubleClick={() => place([a])}
        onKeyDown={(e: KeyboardEvent) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            e.stopPropagation()
            setSelection({ ids: [a.id], anchor: a.id })
            place([a])
          } else if (e.key === 'Escape' && selection.ids.length) {
            // Esc clears the cards first; the next one goes to the editor.
            e.stopPropagation()
            setSelection(none)
          }
        }}
        onContextMenu={(e) => {
          e.preventDefault()
          // On a selected card the menu acts on the selection, as on the library page.
          if (!on) setSelection({ ids: [a.id], anchor: a.id })
          setMenu({ x: e.clientX, y: e.clientY, assets: on ? selected : [a] })
        }}
        onDragStart={(e: DragEvent) => {
          if (!canPlace(a)) return e.preventDefault()
          // One asset at a time: the viewport shows one ghost. It only checks the
          // id; which asset it is comes through onDrag, since the data cannot be
          // read during dragover.
          e.dataTransfer.setData('application/creative-asset', a.id)
          e.dataTransfer.setData('application/creative-asset+json', JSON.stringify([a]))
          e.dataTransfer.effectAllowed = 'copy'
          onDrag(a)
        }}
        onDragEnd={() => onDrag(null)}
      >
        <MediaFigure content={a.unavailable ? undefined : a.content} kind={a.kind} title={a.title} className="cc-scene-asset-thumb" />
        <span className="cc-scene-asset-name">{a.title}</span>
      </div>
    )
  }

  const menuItems = (assets: LibraryAsset[]): MenuItem[] => {
    const ok = assets.filter(canPlace)
    const first = assets[0]
    if (kind === 'material') return assets.length === 1 && first ? [{ label: '在资产库中查看', run: () => openInLibrary(first) }] : []
    return [
      {
        label: assets.length > 1 ? `放入 ${ok.length} 个模型` : '放入场景',
        disabled: !ok.length,
        hint: readOnly ? '当前不能编辑场景' : ok.length < assets.length ? `${assets.length - ok.length} 个模型不可用` : undefined,
        run: () => place(ok),
      },
      ...(assets.length === 1 && first ? [{ label: '在资产库中查看', run: () => openInLibrary(first) }] : []),
    ]
  }

  const builtinView = kind === 'material' && source === 'builtin'
  const builtinItems = builtinView ? filterBuiltin(text) : []
  const builtinCard = (m: BuiltinMaterial) => (
    <div
      key={m.id}
      data-builtin={m.id}
      className={`cc-scene-asset${builtinPick?.id === m.id ? ' is-selected' : ''}`}
      role="option"
      aria-selected={builtinPick?.id === m.id}
      aria-label={m.label}
      tabIndex={0}
      title={`${m.label} · 拖到物件或槽位上`}
      draggable={!readOnly}
      onClick={(e) => {
        e.currentTarget.focus()
        setBuiltinPick(m)
      }}
      onKeyDown={(e) => {
        if (e.key !== 'Escape' || !builtinPick) return
        e.stopPropagation()
        setBuiltinPick(null)
      }}
      onDragStart={(e: DragEvent) => {
        if (readOnly) return e.preventDefault()
        // Like a material asset's drag; the slot it writes comes through onDragBuiltin.
        e.dataTransfer.setData('application/creative-asset+json', JSON.stringify([{ builtin_id: m.id }]))
        e.dataTransfer.effectAllowed = 'copy'
        onDragBuiltin({ title: m.label, slot: builtinSlot(m.id) })
      }}
      onDragEnd={() => onDragBuiltin(null)}
    >
      <span className="cc-scene-asset-thumb">
        <img src={m.thumbnail} alt="" draggable={false} />
      </span>
      <span className="cc-scene-asset-name">{m.label}</span>
    </div>
  )

  const single = selected.length === 1 ? selected[0] : undefined
  const usable = selected.filter(canPlace)
  const recentOrder = shownRecent.map((a) => a.id)
  const listOrder = items.map((a) => a.id)
  return (
    <div className="cc-scene-assets" aria-label="资产" hidden={hidden}>
      <div className="cc-scene-assets-filters">
        <div className="cc-scene-assets-top">
          <label className="cc-scene-assets-search">
            <Search size={14} aria-hidden="true" />
            <input
              ref={search}
              type="search"
              placeholder={`搜索${kindName[kind]}`}
              aria-label={`搜索${kindName[kind]}`}
              value={text}
              onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => {
                if (e.key !== 'Escape') return
                e.preventDefault()
                if (text) setText('')
                else onExit()
              }}
            />
          </label>
          <div className="cc-scene-assets-kinds" role="tablist" aria-label="资产种类">
            {(['model', 'material'] as const).map((k) => (
              <button
                key={k}
                type="button"
                role="tab"
                aria-selected={kind === k}
                onClick={() => {
                  setKind(k)
                  setSelection(none)
                }}
              >
                {kindName[k]}
              </button>
            ))}
          </div>
        </div>
        <div className="cc-scene-assets-row">
          {kind === 'model' && onImport && (
            <button type="button" className="cc-scene-assets-import" title="导入 GLB 模型到资产库" onClick={onImport}>
              <Upload size={13} aria-hidden="true" />
              导入模型
            </button>
          )}
          {kind === 'material' && (
            <div className="cc-scene-assets-kinds" role="tablist" aria-label="材质来源">
              {(['mine', 'builtin'] as const).map((s) => (
                <button
                  key={s}
                  type="button"
                  role="tab"
                  aria-selected={source === s}
                  onClick={() => {
                    setSource(s)
                    setSelection(none)
                    setBuiltinPick(null)
                  }}
                >
                  {s === 'mine' ? '我的' : '内置'}
                </button>
              ))}
            </div>
          )}
          {builtinView ? <span className="cc-scene-assets-fill" /> : <>
          <select aria-label="分组" value={group} onChange={(e) => setGroup(e.target.value)}>
            <option value="">全部分组</option>
            {groupOptions(catalog?.groups ?? []).map((g) => (
              <option key={g.value} value={g.value}>
                {`${'　'.repeat(g.depth ?? 0)}${g.label}`}
              </option>
            ))}
          </select>
          <select aria-label="标签" value={tag} onChange={(e) => setTag(e.target.value)}>
            <option value="">全部标签</option>
            {(catalog?.tags ?? []).map((t) => (
              <option key={t.id} value={t.id}>
                {t.name}
              </option>
            ))}
          </select>
          </>}
          <span className="cc-scene-assets-size" role="group" aria-label="卡片大小">
            {(['small', 'large'] as const).map((s) => (
              <button
                key={s}
                type="button"
                aria-pressed={size === s}
                aria-label={s === 'small' ? '小卡片' : '大卡片'}
                title={s === 'small' ? '小卡片' : '大卡片'}
                onClick={() => {
                  setSize(s)
                  writeSize(s)
                }}
              >
                {s === 'small' ? <Grid3x3 size={14} /> : <Grid2x2 size={14} />}
              </button>
            ))}
          </span>
        </div>
      </div>
      <div
        ref={body}
        className={`cc-scene-assets-body is-${size}`}
        onPointerDown={(e) => {
          if (e.button !== 0 || !(e.target instanceof Element) || e.target.closest('.cc-scene-asset, button')) return
          dragBox.current = { x: e.clientX, y: e.clientY, base: e.metaKey || e.ctrlKey ? selection.ids : [], moved: false }
          e.currentTarget.setPointerCapture(e.pointerId)
        }}
        onPointerMove={boxAt}
        onPointerUp={(e) => {
          suppressClick.current = !!dragBox.current?.moved
          if (e.currentTarget.hasPointerCapture(e.pointerId)) e.currentTarget.releasePointerCapture(e.pointerId)
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
          if (e.target instanceof Element && !e.target.closest('.cc-scene-asset, button')) setSelection(none)
        }}
      >
        {marquee && (
          <div
            className="cc-scene-assets-marquee"
            style={{ left: marquee.left, top: marquee.top, width: marquee.right - marquee.left, height: marquee.bottom - marquee.top }}
          />
        )}
        {builtinView ? (
          <section aria-label="内置材质">
            {builtinItems.length ? (
              <div className="cc-scene-assets-grid" role="listbox" aria-label="内置材质">
                {builtinItems.map(builtinCard)}
              </div>
            ) : (
              <p className="cc-scene-assets-note">没有符合条件的内置材质</p>
            )}
          </section>
        ) : <>
        {shownRecent.length > 0 && (
          <section aria-label="最近使用">
            <h3>最近使用</h3>
            <div className="cc-scene-assets-grid" role="listbox" aria-label="最近使用的模型" aria-multiselectable="true">
              {shownRecent.map((a) => card(a, recentOrder))}
            </div>
          </section>
        )}
        <section aria-label={kindName[kind]}>
          {shownRecent.length > 0 && <h3>全部模型</h3>}
          {list.error && !list.page ? (
            <p className="cc-scene-assets-note" role="alert">
              {list.error}
            </p>
          ) : list.loading ? (
            <p className="cc-scene-assets-note" role="status">
              正在读取资产库…
            </p>
          ) : !items.length ? (
            <p className="cc-scene-assets-note">
              {narrowed
                ? `没有符合条件的${kindName[kind]}`
                : kind === 'model'
                  ? `资产库里还没有模型。${onImport ? '点「导入模型」导入 GLB，或' : ''}选中场景里的代码模型，在属性栏点「存入资产库」`
                  : '资产库里还没有材质。在属性栏的材质槽上点「存为材质资产…」'}
            </p>
          ) : (
            <div className="cc-scene-assets-grid" role="listbox" aria-label={kindName[kind]} aria-multiselectable="true">
              {items.map((a) => card(a, listOrder))}
            </div>
          )}
          <div ref={end} className="cc-scene-assets-end">
            {list.more === 'loading' && <span role="status">正在加载更多…</span>}
            {list.more === 'error' && (
              <button type="button" className="btn" onClick={list.loadMore}>
                加载失败，重试
              </button>
            )}
          </div>
        </section>
        </>}
      </div>
      <footer className="cc-scene-assets-detail">
        {builtinView ? (
          builtinPick ? (
            <>
              <strong>{builtinPick.label}</strong>
              <small>
                内置材质 · {builtinPick.categoryLabel} · {builtinPick.sizeM} 米一块
              </small>
              <small>拖到视图里的物件或属性栏的材质槽上使用</small>
            </>
          ) : (
            <small>{readOnly ? '当前不能编辑场景' : '平台提供的真实照片贴图材质，拖到视图里的物件或属性栏的材质槽上'}</small>
          )
        ) : single ? (
          <>
            <strong>{single.title}</strong>
            {facts(single).map((line) => (
              <small key={line}>{line}</small>
            ))}
            {!placeable(single) && <small>这个{kindName[kind]}不可用</small>}
            {single.kind === 'material' ? (
              <small>拖到视图里的物件或属性栏的材质槽上，复制一份用到那个槽位</small>
            ) : null}
            <span className="cc-scene-assets-actions">
              {single.kind === 'model' && (
              <button type="button" className="btn btn-primary" disabled={!canPlace(single)} onClick={() => place([single])}>
                放入场景
              </button>
              )}
              <button type="button" className="btn" onClick={() => openInLibrary(single)} title="在资产库中查看（新标签页）">
                <ExternalLink size={14} />
                在资产库中查看
              </button>
            </span>
          </>
        ) : selected.length > 1 && kind === 'model' ? (
          <>
            <strong>已选 {selected.length} 个模型</strong>
            <small>{usable.length < selected.length ? `其中 ${selected.length - usable.length} 个不能放入` : '放入时在视图中心下方排成一行'}</small>
            <span className="cc-scene-assets-actions">
              <button type="button" className="btn btn-primary" disabled={!usable.length} onClick={() => place(usable)}>
                放入 {usable.length} 个模型
              </button>
              <button type="button" className="btn" onClick={() => setSelection(none)}>
                取消选择
              </button>
            </span>
          </>
        ) : (
          <small>
            {readOnly
              ? '当前不能编辑场景'
              : kind === 'material'
                ? '把材质拖到视图里的物件或属性栏的材质槽上'
                : '双击放到视图中心，或拖到想放的位置；⌘/Ctrl、Shift 或在空白处拖动可多选'}
          </small>
        )}
      </footer>
      {menu && <ContextMenu x={menu.x} y={menu.y} items={menuItems(menu.assets)} onClose={() => setMenu(null)} />}
    </div>
  )
}
