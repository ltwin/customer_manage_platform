import { useEffect, useRef, useState } from 'react'
import { Maximize, Minimize, PanelLeftClose, X } from 'lucide-react'
import * as api from './api.ts'
import CanvasContextMenu, { type CanvasMenuItem } from './CanvasContextMenu.tsx'
import { kindLabel, type Catalog } from './libraryState.ts'
import { isCanvasAsset } from './assetPlacement.ts'
import AssetViewer from '../viewer/AssetViewer.tsx'
import AssetBrowser from '../assets/AssetBrowser.tsx'
import LibraryTree from '../assets/LibraryTree.tsx'
import { useAssetImport } from '../assets/AssetImport.tsx'
import { assetMenuItems } from '../assets/assetMenu.tsx'
import type { Point } from '../assets/popover.ts'
import { blank, enter, type LibraryQuery, type Prefs } from '../assets/query.ts'
import {
  clickSelection,
  type Modifiers,
  type Selection,
} from '../assets/selection.ts'
import { useActions } from '../assets/useActions.ts'
import { useAssetList, useCatalog } from '../assets/useLibrary.ts'
import { useOrganization } from '../assets/useOrganization.tsx'
import { useTreeCommands } from '../assets/useTreeCommands.ts'
import '../assets/assets.css'
import '../assets/organize.css'
import './library.css'

/** 带着任务打开资产库：给空节点选一份素材填进去，或为节点选参考。 */
export type LibraryTask =
  | {
      kind: 'fill'
      nodeID: string
      mediaKind: 'image' | 'video' | 'audio'
      title: string
    }
  | { kind: 'pick'; nodeID: string; title: string }
type Menu = { at: Point; items: CanvasMenuItem[]; label: string }

const NARROW = 344
const WIDE = 760
// 抽屉不显示布局与缩放：瀑布流，卡片宽度让 344px 正好两列。
const PREFS: Prefs = { layout: 'masonry', size: 140, inspector: false }
const newer = (a: api.Asset, b?: api.Asset) =>
  !b || BigInt(a.revision) >= BigInt(b.revision)

/**
 * 画布里的资产库抽屉：与独立资产库页共用浏览区、卡片、右键菜单、查看浮窗和
 * 写入逻辑；只多出画布相关的部分——拖进画布、放入画布、给空节点填充、选参考。
 * 加宽后左侧出现资料库树，窄时视图与分组收进顶部下拉。
 */
export default function LibraryDrawer({
  hidden,
  tick,
  task,
  canPlace,
  onPlace,
  onFill,
  onPick,
  onClose,
  onWidth,
  onAssets,
  onCatalog,
}: {
  hidden: boolean
  /** 画布侧写入了资产库（如存入节点）时变化，抽屉随之重读。 */
  tick: number
  task: LibraryTask | null
  canPlace: boolean
  onPlace: (assets: api.Asset[]) => void
  onFill: (asset: api.Asset) => void
  onPick: (assets: api.Asset[]) => void
  onClose: () => void
  onWidth: (width: number) => void
  onAssets: (assets: api.Asset[]) => void
  onCatalog: (catalog: Catalog) => void
}) {
  const account = api.currentAccount() ?? ''
  const [query, setQuery] = useState<LibraryQuery>(blank)
  const [own, setOwn] = useState(0)
  const refresh = tick + own
  const reload = () => setOwn((n) => n + 1)
  const [wide, setWide] = useState(false)
  const width = wide ? WIDE : NARROW
  useEffect(() => onWidth(width), [width, onWidth])
  const [selection, setSelection] = useState<Selection>({
    ids: [],
    anchor: null,
  })
  const [viewer, setViewer] = useState<{
    items: api.Asset[]
    initialID: string
  } | null>(null)
  const [menu, setMenu] = useState<Menu | null>(null)
  // 填充只接受节点同类的素材，也不从回收站里挑。
  const effective: LibraryQuery =
    task?.kind === 'fill'
      ? {
          ...query,
          kind: task.mediaKind,
          view: query.view === 'trash' ? 'all' : query.view,
        }
      : query
  const { catalog, error: catalogError } = useCatalog(refresh)
  const rawList = useAssetList(effective, refresh)
  const actions = useActions(account, reload)
  // 浮窗关闭后把焦点还给那张卡片；浮窗里改过素材时等列表重读完再还。
  const returnFocus = useRef<string | null>(null)
  useEffect(() => {
    if (viewer || !returnFocus.current || rawList.refreshing) return
    const frame = requestAnimationFrame(() => {
      const id = returnFocus.current
      if (!id) return
      document
        .querySelector<HTMLElement>(
          `#creative-library [data-asset="${CSS.escape(id)}"]`,
        )
        ?.focus({ preventScroll: true })
      returnFocus.current = null
    })
    return () => cancelAnimationFrame(frame)
  }, [viewer, rawList.page, rawList.refreshing])

  const known = useRef(new Map<string, api.Asset>())
  for (const a of rawList.page?.items ?? [])
    if (newer(a, known.current.get(a.id))) known.current.set(a.id, a)
  const actual = (a: api.Asset) => {
    const patch = actions.overrides.get(a.id)
    return patch && newer(patch, a) ? patch : a
  }
  const items = (rawList.page?.items ?? [])
    .map(actual)
    .filter((a) =>
      effective.view === 'trash' ? !!a.deleted_at : !a.deleted_at,
    )
  const list = {
    ...rawList,
    page: rawList.page ? { ...rawList.page, items } : null,
  }
  const resolve = (ids: string[]) =>
    ids.flatMap((id) => {
      const a = known.current.get(id)
      return a ? [actual(a)] : []
    })
  const selected = resolve(selection.ids)
  const usable = (list: api.Asset[]) =>
    list.filter((a) => !a.unavailable && !a.deleted_at)
  const clear = () => setSelection({ ids: [], anchor: null })
  const organization = useOrganization({
    account,
    catalog,
    actions,
    actual: (a) =>
      actual(newer(a, known.current.get(a.id)) ? a : known.current.get(a.id)!),
    onTrash: clear,
    onPurge: () => setViewer(null),
  })
  // 换视图或分组才清空选择；改关键词、筛选时保留（Esc 先清关键词、再清选择，V6-057 AC-01）。
  function go(next: LibraryQuery) {
    if (next.view !== query.view || next.group !== query.group) clear()
    organization.setPopup(null)
    setQuery(next)
  }
  const tree = useTreeCommands({
    account,
    catalog,
    actions,
    organization,
    query,
    go,
    resolve,
  })
  const importer = useAssetImport({
    account,
    group: query.group,
    actions,
    blocked:
      actions.blocked ||
      !!viewer ||
      !!organization.popup ||
      !!organization.confirm ||
      !!menu,
    onDone: reload,
    paste: false,
    onStart: () => {
      if (!query.group && query.view !== 'all' && query.view !== 'recent')
        go(enter(query, 'all'))
    },
  })

  // 画布侧要用的：已加载的素材（拖放、参考）与目录（存入节点时选分组）。
  const itemsKey = items.map((a) => `${a.id}:${a.revision}`).join(',')
  const latest = useRef({ items, onAssets })
  latest.current = { items, onAssets }
  useEffect(() => {
    latest.current.onAssets(latest.current.items)
  }, [itemsKey])
  useEffect(() => {
    if (catalog)
      onCatalog({
        groups: catalog.groups,
        tags: catalog.tags,
        categories: catalog.categories,
        hierarchy: catalog.hierarchy,
        settings: null,
      })
  }, [catalog, onCatalog])
  // 换了任务就从全部素材开始找，不停在上次的条件里。
  const taskKey = task ? `${task.kind}:${task.nodeID}` : ''
  useEffect(() => {
    setQuery(blank())
    setSelection({ ids: [], anchor: null })
  }, [taskKey])

  function pick(id: string | null, keys: Modifiers = {}) {
    if (!id) return task?.kind === 'pick' ? undefined : clear()
    const a = known.current.get(id)
    if (task?.kind === 'fill') {
      if (a && usable([a]).length) onFill(actual(a))
      return
    }
    // 参考接到画布节点上，模型不能当参考。
    if (task?.kind === 'pick' && a && !isCanvasAsset(a)) return
    setSelection((s) =>
      clickSelection(
        s,
        id,
        items.map((x) => x.id),
        task?.kind === 'pick' ? { additive: true } : keys,
      ),
    )
  }
  const placeable = canPlace && effective.view !== 'trash' && !task
  function place(assets: api.Asset[]) {
    const ok = usable(assets).filter(isCanvasAsset)
    if (!ok.length) return
    onPlace(ok)
    clear()
  }
  function assetMenu(asset: api.Asset, at: Point) {
    if (task) return
    const assets = selection.ids.includes(asset.id) ? selected : [asset]
    if (!selection.ids.includes(asset.id)) pick(asset.id)
    const next = assetMenuItems({
      assets,
      blocked: actions.blocked,
      group: query.group,
      onAction: (kind) => organization.action(kind, at, assets),
      onRemoveFromGroup: () =>
        organization.organize(assets, 'group', query.group, false),
      onPlace: placeable ? () => place(assets) : undefined,
    })
    if (next) setMenu({ at, ...next })
  }
  // 拖出卡片：画布按 creative-asset 放置，资料库树按选择归入分组。
  function dragCard(asset: api.Asset, e: React.DragEvent) {
    const list = usable(
      selection.ids.includes(asset.id) && selected.length > 1
        ? selected
        : [actual(asset)],
    )
    if (!list.length || task) return e.preventDefault()
    e.dataTransfer.setData(
      'application/creative-asset',
      list.map((x) => x.id).join(','),
    )
    e.dataTransfer.setData(
      'application/creative-asset+json',
      JSON.stringify(list),
    )
    e.dataTransfer.setData(
      'application/creative-library-selection',
      list.map((x) => x.id).join(','),
    )
    e.dataTransfer.effectAllowed = 'copy'
    if (list.length > 1) {
      const ghost = document.createElement('div')
      ghost.className = 'cl-drag-ghost'
      ghost.textContent = `${list.length} 项`
      document.body.append(ghost)
      e.dataTransfer.setDragImage(ghost, 20, 14)
      setTimeout(() => ghost.remove())
    }
  }

  const title =
    task?.kind === 'fill'
      ? `选择${kindLabel[task.mediaKind]}`
      : task?.kind === 'pick'
        ? '选择参考素材'
        : '资产库'
  const blocked = actions.blocked || selected.length > 100
  return (
    <>
      <aside
        id="creative-library"
        className={`cc-library cl-drawer${wide ? ' is-wide' : ''}${task ? ` is-${task.kind}` : ''}`}
        style={{ width }}
        tabIndex={-1}
        aria-label="个人资产库"
        aria-busy={actions.busy}
        data-scope-group={query.group}
        hidden={hidden}
        // 和独立页一样：Esc 取消选择（没有选择时收起），⌘A 全选已加载的，数字键评分。
        onKeyDown={(e) => {
          const target = e.target instanceof Element ? e.target : null
          if (
            e.defaultPrevented ||
            e.nativeEvent.isComposing ||
            target?.closest(
              'input:not([type="search"]), textarea, select, [role="dialog"], [role="alertdialog"], [role="menu"], [role="slider"]',
            )
          )
            return
          if (e.key === 'Escape') {
            e.preventDefault()
            if (selection.ids.length && task?.kind !== 'pick') clear()
            else onClose()
          } else if (target?.closest('input')) {
            return
          } else if (
            (e.metaKey || e.ctrlKey) &&
            e.key.toLowerCase() === 'a' &&
            task?.kind !== 'fill'
          ) {
            e.preventDefault()
            setSelection({
              ids: items.map((a) => a.id),
              anchor: selection.anchor,
            })
          } else if (
            !e.metaKey &&
            !e.ctrlKey &&
            !e.altKey &&
            /^[0-5]$/.test(e.key) &&
            selected.length &&
            !task &&
            !blocked
          ) {
            e.preventDefault()
            actions.rate(selected, Number(e.key) || null)
          }
        }}
      >
        <header className="cl-head">
          <h2>{title}</h2>
          <span className="cl-grow" />
          {!task && importer.button()}
          <button
            type="button"
            className="cl-icon"
            aria-label={wide ? '收窄资产库' : '加宽资产库'}
            title={wide ? '收窄' : '加宽并显示资料库'}
            onClick={() => setWide(!wide)}
          >
            {wide ? <Minimize size={15} /> : <Maximize size={15} />}
          </button>
          <button
            type="button"
            className="cl-icon"
            aria-label="收起资产库"
            title="收起"
            onClick={onClose}
          >
            <PanelLeftClose size={16} />
          </button>
        </header>
        <div className="cl-body">
          {wide && (
            <LibraryTree
              query={query}
              catalog={catalog}
              error={catalogError}
              facets={list.facets}
              onView={(v) => go(enter(query, v))}
              onGroup={(id) => go(enter(query, 'all', id))}
              onTag={(id) =>
                go({
                  ...query,
                  tags: query.tags.includes(id)
                    ? query.tags.filter((t) => t !== id)
                    : [...query.tags, id],
                })
              }
              {...tree}
              blocked={actions.blocked}
            />
          )}
          <AssetBrowser
            drawer
            views={!wide}
            query={effective}
            catalog={catalog}
            list={list}
            prefs={PREFS}
            selected={selection.ids}
            importBlocked={
              !!task ||
              actions.blocked ||
              importer.open ||
              !!viewer ||
              !!menu ||
              !!organization.popup ||
              !!organization.confirm
            }
            onImportFiles={importer.files}
            onQuery={(q) =>
              go(task?.kind === 'fill' ? { ...q, kind: query.kind } : q)
            }
            onView={(v) => go(enter(query, v))}
            onGroup={(id) => go(enter(query, 'all', id))}
            onPrefs={() => {}}
            onSelect={pick}
            onBox={(ids) =>
              task?.kind !== 'fill' &&
              setSelection({ ids, anchor: ids[0] ?? null })
            }
            onMenu={assetMenu}
            onOpen={(asset) =>
              setViewer({
                items: list.page?.items ?? [asset],
                initialID: asset.id,
              })
            }
            onRetry={reload}
            onCardDrag={dragCard}
          />
        </div>
        {task?.kind === 'fill' ? (
          <footer className="cl-foot is-task">
            <span>
              点一份{kindLabel[task.mediaKind]}填进「{task.title}」
            </span>
            <span className="cl-grow" />
            <button type="button" className="btn" onClick={onClose}>
              取消
            </button>
          </footer>
        ) : task?.kind === 'pick' ? (
          <footer className="cl-foot is-task">
            <span>
              为「{task.title}」选参考
              {selected.length ? ` · 已选 ${selected.length}` : ''}
            </span>
            <span className="cl-grow" />
            <button type="button" className="btn" onClick={onClose}>
              取消
            </button>
            <button
              type="button"
              className="btn btn-primary"
              disabled={!usable(selected).filter(isCanvasAsset).length}
              onClick={() => onPick(usable(selected).filter(isCanvasAsset))}
            >
              添加{selected.length ? ` ${selected.length} 项` : ''}
            </button>
          </footer>
        ) : selected.length ? (
          <footer
            className="cl-foot is-task"
            role="toolbar"
            aria-label="批量操作"
          >
            <span>已选 {selected.length} 项</span>
            <span className="cl-grow" />
            <button
              type="button"
              className="btn"
              aria-label="更多操作"
              disabled={blocked}
              onClick={(e) => {
                const r = e.currentTarget.getBoundingClientRect()
                assetMenu(selected[0]!, { x: r.left, y: r.top - 8 })
              }}
            >
              整理…
            </button>
            {placeable && selected.some(isCanvasAsset) && (
              <button
                type="button"
                className="btn btn-primary"
                disabled={!usable(selected).filter(isCanvasAsset).length}
                onClick={() => place(selected)}
              >
                放入画布
              </button>
            )}
            <button
              type="button"
              className="cl-icon"
              aria-label="取消选择"
              onClick={clear}
            >
              <X size={15} />
            </button>
          </footer>
        ) : (
          <footer className="cl-foot">
            {placeable
              ? '拖进画布放置 · 双击查看 · ⌘ 点击多选'
              : '双击查看 · ⌘ 点击多选 · 拖入文件即导入'}
          </footer>
        )}
      </aside>
      {importer.overlays}
      {menu && (
        <CanvasContextMenu
          x={menu.at.x}
          y={menu.at.y}
          label={menu.label}
          items={menu.items}
          onClose={() => setMenu(null)}
        />
      )}
      {viewer ? (
        <AssetViewer
          {...viewer}
          catalog={catalog}
          actions={actions}
          organization={organization}
          refresh={refresh}
          label="快速查看"
          onPlace={
            placeable
              ? (asset) => {
                  setViewer(null)
                  place([asset])
                }
              : undefined
          }
          onClose={() => {
            returnFocus.current = viewer.initialID
            setViewer(null)
          }}
        />
      ) : (
        organization.overlays
      )}
    </>
  )
}
