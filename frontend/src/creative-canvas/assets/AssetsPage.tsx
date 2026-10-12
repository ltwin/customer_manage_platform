import { useAssetImport } from './AssetImport.tsx'
import TrashControls from './TrashControls.tsx'
import AssetViewer from '../viewer/AssetViewer.tsx'
import { useOrganization } from './useOrganization.tsx'
import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  ArrowUpRight,
  Download,
  Folder,
  Heart,
  Star,
  Tag,
  Trash2,
  X,
  Undo2,
} from 'lucide-react'
import type { components } from '../../api/schema'
import * as api from '../editor/api.ts'
import CanvasContextMenu, {
  type CanvasMenuItem,
} from '../editor/CanvasContextMenu.tsx'
import {
  deleteGroupCommand,
  groupRestoreCommand,
} from '../editor/groupDeletion.ts'
import '../editor/workspace.css'
import '../editor/library.css'
import AppSidebar from '../home/AppSidebar.tsx'
import AssetBrowser from './AssetBrowser.tsx'
import AssetInspector from './AssetInspector.tsx'
import LibraryTree from './LibraryTree.tsx'
import BatchBar, { type Action } from './BatchBar.tsx'
import type { Point } from './popover.ts'
import { operation } from './operations.ts'
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
  const { metadata, organize, popup, confirm, setPopup, setConfirm } =
    organization
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
    if (assets.every((a) => a.deleted_at)) {
      setMenu({
        at,
        label: '回收站操作',
        items: [
          {
            label: '恢复',
            icon: <Undo2 size={14} />,
            disabled: actions.blocked || assets.length > 100,
            action: () => action('restore', at, assets),
          },
          {
            label: '永久删除',
            icon: <Trash2 size={14} />,
            danger: true,
            disabled: actions.blocked || assets.length > 100,
            action: () => action('purge', at, assets),
          },
        ],
      })
      return
    }
    if (assets.some((a) => a.deleted_at)) return
    const entries: { kind: Action; label: string; Icon: typeof Star }[] = [
      { kind: 'rate', label: '评分…', Icon: Star },
      { kind: 'project', label: '放入项目…', Icon: ArrowUpRight },
      { kind: 'group', label: '归入分组…', Icon: Folder },
      { kind: 'tag', label: '打标签…', Icon: Tag },
      {
        kind: 'favorite',
        label: assets.every((a) => a.is_favorite) ? '取消最爱' : '加入最爱',
        Icon: Heart,
      },
      { kind: 'download', label: '下载', Icon: Download },
      { kind: 'trash', label: '移到回收站', Icon: Trash2 },
    ]
    const items: CanvasMenuItem[] = entries.map(({ kind, label, Icon }) => ({
      label,
      icon: <Icon size={14} />,
      danger: kind === 'trash',
      disabled:
        actions.blocked ||
        assets.length > 100 ||
        ((kind === 'project' || kind === 'download') &&
          assets.some((a) => a.unavailable)),
      action: () => action(kind, at, assets),
    }))
    if (query.group)
      items.splice(5, 0, {
        label: '移出这个分组',
        icon: <X size={14} />,
        disabled: actions.blocked || assets.length > 100,
        action: () => organize(assets, 'group', query.group, false),
      })
    setMenu({ at, label: '素材操作', items })
  }
  function deleteGroup(group: api.Group) {
    if (!catalog || actions.blocked) return
    const command = deleteGroupCommand(group, catalog.hierarchy)
    setConfirm({
      title: `删除分组「${group.name}」？`,
      body: '素材保留在资产库，子分组移到上一级。',
      run: () =>
        void actions.execute({
          run: operation(account, command.path, command.payload),
          success: (result) => {
            if (query.group === group.id)
              go(enter(query, 'all', group.parent_id ?? ''), true)
            const undo = groupRestoreCommand(result)
            return {
              text: '已删除分组',
              ...(undo
                ? {
                    action: {
                      label: '撤销',
                      run: () =>
                        void actions.execute({
                          conflictText:
                            '分组已经有新的调整，无法撤销；请刷新后查看',
                          run: operation(account, undo.path, undo.payload),
                          success: () => ({ text: '已恢复分组与归类' }),
                        }),
                    },
                  }
                : {}),
            }
          },
        }),
    })
  }
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
    <div
      className="cc-workspace cs-app"
      onClickCapture={(e) => {
        if (
          (actions.blocked || importer.busy) &&
          e.target instanceof Element &&
          e.target.closest('a[href]')
        )
          e.preventDefault()
      }}
    >
      <AppSidebar current="library" refresh={refresh} />
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
          onDropAssets={(ids, group) => {
            if (actions.blocked) return
            const assets = ids.flatMap((id) => {
              const a = known.current.get(id)
              return a ? [actual(a)] : []
            })
            if (assets.length !== ids.length || assets.length > 100) {
              actions.setToast({ text: '一次最多处理 100 项，请重新选择素材' })
              return
            }
            organize(assets, 'group', group, true)
          }}
          blocked={actions.blocked}
          onDeleteGroup={deleteGroup}
          onSaveGroup={(group, parent, name, done) => {
            if (!catalog) return
            const path = group
              ? `/asset-groups/${encodeURIComponent(group.id)}/rename`
              : '/asset-groups'
            const payload = group
              ? ({
                  name,
                  expected_revision: group.revision,
                  hierarchy_revision: catalog.hierarchy,
                } satisfies components['schemas']['CreativeGroupRenamePayload'])
              : ({
                  name,
                  parent_id: parent,
                  hierarchy_revision: catalog.hierarchy,
                } satisfies components['schemas']['CreativeGroupCreatePayload'])
            void actions.execute({
              run: operation(account, path, payload),
              success: () => {
                done()
                return { text: group ? '已重命名分组' : '已新建分组' }
              },
            })
          }}
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
    </div>
  )
}
