import { useAssetImport } from './AssetImport.tsx'
import TrashControls from './TrashControls.tsx'
import AssetViewer from '../viewer/AssetViewer.tsx'
import { useOrganization } from './useOrganization.tsx'
import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import * as api from '../editor/api.ts'
import CanvasContextMenu, {
  type CanvasMenuItem,
} from '../editor/CanvasContextMenu.tsx'
import '../editor/workspace.css'
import '../editor/library.css'
import { useCreativeShell, useSidebarRefresh } from '../home/creativeShell.ts'
import AssetBrowser from './AssetBrowser.tsx'
import AssetInspector from './AssetInspector.tsx'
import LibraryTree from './LibraryTree.tsx'
import BatchBar, { type Action } from './BatchBar.tsx'
import type { Point } from './popover.ts'
import { assetMenuItems } from './assetMenu.tsx'
import { useTreeCommands } from './useTreeCommands.ts'
import { useActions } from './useActions.ts'
import { clickSelection, type Modifiers, type Selection } from './selection.ts'
import {
  enter,
  readPrefs,
  readQuery,
  writePrefs,
  writeQuery,
  type LibraryQuery,
} from './query.ts'
import { useAssetList, useCatalog } from './useLibrary.ts'
import './assets.css'
import './organize.css'

type Menu = { at: Point; items: CanvasMenuItem[]; label: string }
const newer = (a: api.Asset, b?: api.Asset) =>
  !b || BigInt(a.revision) >= BigInt(b.revision)
export default function AssetsPage() {
  const account = api.currentAccount()
  return account ? (
    <Assets key={account} account={account} />
  ) : (
    <main>请先登录</main>
  )
}
function Assets({ account }: { account: string }) {
  const [params, setParams] = useSearchParams()
  const query = readQuery(params)
  const [refresh, setRefresh] = useState(0)
  const reload = () => setRefresh((n) => n + 1)
  const [prefs, setPrefs] = useState(readPrefs)
  const [selection, setSelection] = useState<Selection>({
    ids: [],
    anchor: null,
  })
  const [viewer, setViewer] = useState<{
    items: api.Asset[]
    initialID: string
  } | null>(null)
  const returnFocus = useRef<string | null>(null)
  const [menu, setMenu] = useState<Menu | null>(null)
  const { catalog, error: catalogError } = useCatalog(refresh)
  const rawList = useAssetList(query, refresh)
  const actions = useActions(account, reload)
  useEffect(() => {
    if (viewer || !returnFocus.current || rawList.refreshing) return
    const frame = requestAnimationFrame(() => {
      const id = returnFocus.current
      if (!id) return
      const card =
        document.querySelector<HTMLElement>(
          `[data-asset="${CSS.escape(id)}"]`,
        ) ?? document.querySelector<HTMLElement>('.al-tile, .al-board')
      card?.focus({ preventScroll: true })
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
  const list = {
    ...rawList,
    page: rawList.page
      ? {
          ...rawList.page,
          items: rawList.page.items
            .map(actual)
            .filter((a) =>
              query.view === 'trash' ? !!a.deleted_at : !a.deleted_at,
            ),
        }
      : null,
  }
  const selected = selection.ids.flatMap((id) => {
    const a = known.current.get(id)
    return a ? [actual(a)] : []
  })
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
  const { metadata, organize, popup, confirm, setPopup } = organization
  const action = (kind: Action, at: Point, assets = selected) =>
    organization.action(kind, at, assets)
  const importer = useAssetImport({
    account,
    group: query.group,
    actions,
    blocked: actions.blocked || !!viewer || !!popup || !!confirm || !!menu,
    onDone: reload,
    onStart: () => {
      if (!query.group && query.view !== 'all' && query.view !== 'recent')
        go(enter(query, 'all'), true)
    },
  })
  const blocked = actions.blocked || selected.length > 100
  useSidebarRefresh(refresh)
  const navigationBlocked = actions.blocked || importer.busy
  const { setNavigationBlocked } = useCreativeShell()
  useEffect(() => {
    setNavigationBlocked(navigationBlocked)
    return () => setNavigationBlocked(false)
  }, [navigationBlocked, setNavigationBlocked])
  function pick(id: string | null, keys: Modifiers = {}) {
    if (!id) return clear()
    setSelection((s) =>
      clickSelection(s, id, list.page?.items.map((a) => a.id) ?? [], keys),
    )
  }
  function go(next: LibraryQuery, push = false) {
    clear()
    setPopup(null)
    setParams(writeQuery(next), { replace: !push })
  }
  const queryKey = writeQuery(query).toString()
  useEffect(() => {
    clear()
    setPopup(null)
  }, [queryKey, setPopup])
  useEffect(() => {
    const old = document.title
    document.title = '资产库 · 创意空间'
    return () => {
      document.title = old
    }
  }, [])
  useEffect(() => {
    const visible = () => {
      if (document.visibilityState === 'visible') reload()
    }
    document.addEventListener('visibilitychange', visible)
    return () => document.removeEventListener('visibilitychange', visible)
  }, [])

  function assetMenu(asset: api.Asset, at: Point) {
    const assets = selection.ids.includes(asset.id) ? selected : [asset]
    if (!selection.ids.includes(asset.id)) pick(asset.id)
    const menu = assetMenuItems({
      assets,
      blocked: actions.blocked,
      group: query.group,
      onAction: (kind) => action(kind, at, assets),
      onRemoveFromGroup: () => organize(assets, 'group', query.group, false),
    })
    if (menu) setMenu({ at, ...menu })
  }
  const tree = useTreeCommands({
    account,
    catalog,
    actions,
    organization,
    query,
    go,
    resolve: (ids) =>
      ids.flatMap((id) => {
        const a = known.current.get(id)
        return a ? [actual(a)] : []
      }),
  })
  // 按键只作用于素材选择；输入框、弹层和菜单有各自的键盘行为。
  const keyboard = useRef<(e: KeyboardEvent) => void>(() => {})
  keyboard.current = (e) => {
    const target = e.target instanceof Element ? e.target : null
    if (
      e.isComposing ||
      target?.closest(
        'input, textarea, select, [contenteditable="true"], [role="dialog"], [role="alertdialog"], [role="menu"]',
      ) ||
      importer.open ||
      viewer ||
      popup ||
      confirm ||
      menu
    )
      return
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'a') {
      e.preventDefault()
      setSelection({
        ids: list.page?.items.map((a) => a.id) ?? [],
        anchor: selection.anchor,
      })
      return
    }
    if (e.key === 'Escape') {
      e.preventDefault()
      clear()
      return
    }
    if (
      !e.metaKey &&
      !e.ctrlKey &&
      !e.altKey &&
      /^[0-5]$/.test(e.key) &&
      selected.length &&
      !blocked
    ) {
      e.preventDefault()
      actions.rate(selected, Number(e.key) || null)
    }
  }
  useEffect(() => {
    const key = (e: KeyboardEvent) => keyboard.current(e)
    document.addEventListener('keydown', key)
    return () => document.removeEventListener('keydown', key)
  }, [])
  return (
    <>
      <main
        aria-busy={actions.busy}
        className={`cs-main al-page${prefs.inspector ? '' : ' no-inspector'}`}
      >
        <LibraryTree
          importButton={importer.button()}
          query={query}
          catalog={catalog}
          error={catalogError}
          facets={list.facets}
          onView={(v) => go(enter(query, v), true)}
          onGroup={(id) => go(enter(query, 'all', id), true)}
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
        <AssetBrowser
          importButton={importer.button('al-phone-import')}
          onImportFiles={importer.files}
          importBlocked={
            actions.blocked ||
            importer.open ||
            !!viewer ||
            !!confirm ||
            !!popup ||
            !!menu
          }
          notice={
            query.view === 'trash' ? (
              <TrashControls
                account={account}
                refresh={refresh}
                actions={actions}
                onDone={() => {
                  clear()
                  reload()
                }}
              />
            ) : null
          }
          query={query}
          catalog={catalog}
          list={list}
          prefs={prefs}
          selected={selection.ids}
          onQuery={(q) => go(q)}
          onView={(v) => go(enter(query, v), true)}
          onGroup={(id) => go(enter(query, 'all', id), true)}
          onPrefs={(p) => {
            setPrefs(p)
            writePrefs(p)
          }}
          onSelect={pick}
          onBox={(ids) => setSelection({ ids, anchor: ids[0] ?? null })}
          onMenu={assetMenu}
          onOpen={(asset) => {
            setViewer({
              items: list.page?.items ?? [asset],
              initialID: asset.id,
            })
          }}
          onRetry={reload}
          toolbar={
            <BatchBar
              assets={selected}
              blocked={blocked}
              onAction={action}
              onClear={clear}
            />
          }
        />
        {prefs.inspector && (
          <AssetInspector
            onColor={(color) => go({ ...query, color })}
            assets={selected}
            catalog={catalog}
            blocked={blocked}
            onAction={action}
            onRate={(rating) => actions.rate(selected, rating)}
            onMetadata={metadata}
            onOrganize={(kind, id, add) => organize(selected, kind, id, add)}
          />
        )}
      </main>
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
