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
  Plus,
} from 'lucide-react'
import type { components } from '../../api/schema'
import * as api from '../editor/api.ts'
import { downloadMedia } from '../editor/MediaView.tsx'
import CanvasContextMenu, {
  type CanvasMenuItem,
} from '../editor/CanvasContextMenu.tsx'
import ConfirmDialog from '../../components/ConfirmDialog'
import { StarRating } from '../editor/StarRating.tsx'
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
import ActionPopover from './ActionPopover.tsx'
import type { Point } from './popover.ts'
import ProjectPicker from './ProjectPicker.tsx'
import TagAssigner from './TagAssigner.tsx'
import { operation, projectPlacement } from './operations.ts'
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

type Popup = {
  kind: 'group' | 'tag' | 'rate' | 'project'
  at: Point
  assets: api.Asset[]
}
type Menu = { at: Point; items: CanvasMenuItem[]; label: string }
type Confirmation = { title: string; body: string; run: () => void }
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
  const [popup, setPopup] = useState<Popup | null>(null)
  const [menu, setMenu] = useState<Menu | null>(null)
  const [confirm, setConfirm] = useState<Confirmation | null>(null)
  const [groupDraft, setGroupDraft] = useState('')
  const { catalog, error: catalogError } = useCatalog(refresh)
  const rawList = useAssetList(query, refresh)
  const actions = useActions(account, reload)
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
  }, [queryKey])
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

  function organize(
    assets: api.Asset[],
    kind: 'group' | 'tag',
    id: string,
    add: boolean,
  ) {
    const field = kind === 'group' ? 'group_ids' : 'tag_ids'
    const payload: components['schemas']['CreativeAssetOrganizePayload'] = {
      assets: assets.map((a) => ({
        asset_id: a.id,
        expected_revision: a.revision,
      })),
      [`${add ? 'add' : 'remove'}_${field}`]: [id],
    }
    actions.mutate(
      assets,
      '/assets/batch-organize',
      payload,
      (a) => ({
        ...a,
        [field]: add
          ? [...new Set([...a[field], id])]
          : a[field].filter((v) => v !== id),
      }),
      add ? '已完成归类' : '已移除归类',
    )
  }
  function metadata(
    asset: api.Asset,
    patch: { title?: string; description?: string },
  ) {
    actions.mutate(
      [asset],
      `/assets/${encodeURIComponent(asset.id)}/metadata`,
      {
        expected_revision: asset.revision,
        ...patch,
      } satisfies components['schemas']['CreativeAssetMetadataPayload'],
      (a) => ({ ...a, ...patch }),
      '已保存',
    )
  }
  function action(kind: Action, at: Point, assets = selected) {
    if (
      actions.blocked ||
      !assets.length ||
      assets.length > 100 ||
      assets.some((a) => a.deleted_at) ||
      ((kind === 'project' || kind === 'download') &&
        assets.some((a) => a.unavailable))
    )
      return
    if (
      kind === 'group' ||
      kind === 'tag' ||
      kind === 'rate' ||
      kind === 'project'
    ) {
      setGroupDraft('')
      setPopup({ kind, at, assets })
      return
    }
    if (kind === 'favorite') {
      const favorite = !assets.every((a) => a.is_favorite)
      const sends = assets.map((a) =>
        operation(account, `/assets/${encodeURIComponent(a.id)}/metadata`, {
          expected_revision: a.revision,
          is_favorite: favorite,
        } satisfies components['schemas']['CreativeAssetMetadataPayload']),
      )
      let done = 0
      void actions.execute({
        before: assets,
        optimistic: assets.map((a) => ({ ...a, is_favorite: favorite })),
        run: async () => {
          while (done < sends.length) {
            await sends[done]!()
            done++
          }
        },
        success: () => ({ text: favorite ? '已加入最爱' : '已取消最爱' }),
      })
    } else if (kind === 'trash')
      setConfirm({
        title: '移到回收站？',
        body: `${assets.length} 项素材；分组和最爱关系会保留。`,
        run: () => {
          actions.mutate(
            assets,
            '/assets/batch-trash',
            {
              assets: assets.map((a) => ({
                asset_id: a.id,
                expected_revision: a.revision,
              })),
            } satisfies components['schemas']['CreativeAssetBatchPayload'],
            (a) => ({ ...a, deleted_at: new Date().toISOString() }),
            '已移到回收站',
          )
          clear()
        },
      })
    else if (kind === 'download') {
      void actions.execute({
        readOnly: true,
        run: async () => {
          for (const a of assets) {
            const full = await api.read<api.Asset>(
              `/assets/${encodeURIComponent(a.id)}`,
            )
            if (!full.content || full.unavailable)
              throw new Error('素材当前无法下载')
            if (['image', 'video', 'audio'].includes(full.kind))
              await downloadMedia(full.content, full.title)
            else {
              const blob = new Blob(
                [JSON.stringify(full.content.payload, null, 2)],
                { type: 'application/json' },
              )
              const url = URL.createObjectURL(blob)
              const link = document.createElement('a')
              link.href = url
              link.download = `${full.title}.json`
              link.click()
              setTimeout(() => URL.revokeObjectURL(url), 1000)
            }
          }
        },
        success: () => ({ text: '已开始下载' }),
      })
    }
  }
  function assetMenu(asset: api.Asset, at: Point) {
    const assets = selection.ids.includes(asset.id) ? selected : [asset]
    if (!selection.ids.includes(asset.id)) pick(asset.id)
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
  const targets =
    popup?.assets.map((a) => actual(known.current.get(a.id) ?? a)) ?? []
  return (
    <div
      className="cc-workspace cs-app"
      onClickCapture={(e) => {
        if (
          actions.blocked &&
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
      {menu && (
        <CanvasContextMenu
          x={menu.at.x}
          y={menu.at.y}
          label={menu.label}
          items={menu.items}
          onClose={() => setMenu(null)}
        />
      )}
      {confirm && (
        <ConfirmDialog
          title={confirm.title}
          body={confirm.body}
          danger
          busy={actions.busy}
          onCancel={() => setConfirm(null)}
          onConfirm={() => {
            const run = confirm.run
            setConfirm(null)
            run()
          }}
        />
      )}
      {popup && (
        <ActionPopover
          at={popup.at}
          label={
            {
              group: '归入分组',
              tag: '打标签',
              rate: '评分',
              project: '放入项目',
            }[popup.kind]
          }
          onClose={() => setPopup(null)}
        >
          {popup.kind === 'rate' && (
            <div className="al-rate-pop">
              <p>给 {targets.length} 项评分</p>
              <StarRating
                value={
                  targets.every((a) => a.rating === targets[0]?.rating)
                    ? (targets[0]?.rating ?? null)
                    : null
                }
                disabled={blocked}
                onChange={(rating) => {
                  actions.rate(targets, rating)
                  setPopup(null)
                }}
              />
              <small>点星星左半边是半颗，数字键 1–5 打整星，0 清除</small>
            </div>
          )}
          {popup.kind === 'group' && (
            <>
              <div className="al-pop-heading">归入分组</div>
              <div className="al-pop-list">
                {(catalog?.groups ?? [])
                  .filter((g) => !g.parent_id)
                  .flatMap((g) => [
                    g,
                    ...(catalog?.groups.filter((c) => c.parent_id === g.id) ??
                      []),
                  ])
                  .map((g) => (
                    <button
                      type="button"
                      key={g.id}
                      disabled={blocked}
                      aria-pressed={targets.every((a) =>
                        a.group_ids.includes(g.id),
                      )}
                      onClick={() => {
                        organize(targets, 'group', g.id, true)
                        setPopup(null)
                      }}
                    >
                      <Folder size={14} />
                      <span>
                        {g.parent_id ? '　' : ''}
                        {g.name}
                      </span>
                      {targets.every((a) => a.group_ids.includes(g.id)) && (
                        <span>✓</span>
                      )}
                    </button>
                  ))}
              </div>
              <form
                className="al-pop-foot"
                onSubmit={(e) => {
                  e.preventDefault()
                  if (!catalog || !groupDraft.trim()) return
                  const assets = targets
                  const create = operation<
                    components['schemas']['CreativeLibraryResult']
                  >(account, '/asset-groups', {
                    name: groupDraft.trim(),
                    parent_id: null,
                    hierarchy_revision: catalog.hierarchy,
                  } satisfies components['schemas']['CreativeGroupCreatePayload'])
                  let id = ''
                  let apply: (() => Promise<unknown>) | null = null
                  void actions.execute({
                    before: assets,
                    run: async () => {
                      if (!id) id = (await create()).id
                      apply ??= operation(account, '/assets/batch-organize', {
                        assets: assets.map((a) => ({
                          asset_id: a.id,
                          expected_revision: a.revision,
                        })),
                        add_group_ids: [id],
                      } satisfies components['schemas']['CreativeAssetOrganizePayload'])
                      await apply()
                    },
                    success: () => ({ text: '已新建分组并归入素材' }),
                  })
                  setPopup(null)
                }}
              >
                <input
                  aria-label="新分组名称"
                  placeholder="新分组名称"
                  maxLength={80}
                  value={groupDraft}
                  onChange={(e) => setGroupDraft(e.currentTarget.value)}
                />
                <button
                  type="submit"
                  className="btn"
                  disabled={blocked || !groupDraft.trim()}
                >
                  <Plus size={13} />
                  新建分组并归入
                </button>
              </form>
            </>
          )}
          {popup.kind === 'tag' && (
            <TagAssigner
              assets={targets}
              catalog={catalog}
              blocked={blocked}
              onToggle={(id, add) => organize(targets, 'tag', id, add)}
              onCreate={(name, done) => {
                if (!catalog) return
                const assets = targets
                const create = operation<
                  components['schemas']['CreativeLibraryResult']
                >(account, '/tags', {
                  name,
                  color: '#a1a1a7',
                  category_id: null,
                  hierarchy_revision: catalog.tagHierarchy,
                } satisfies components['schemas']['CreativeTagCreatePayload'])
                let tagID = ''
                let apply: (() => Promise<unknown>) | null = null
                void actions.execute({
                  before: assets,
                  run: async () => {
                    if (!tagID) tagID = (await create()).id
                    apply ??= operation(account, '/assets/batch-organize', {
                      assets: assets.map((a) => ({
                        asset_id: a.id,
                        expected_revision: a.revision,
                      })),
                      add_tag_ids: [tagID],
                    } satisfies components['schemas']['CreativeAssetOrganizePayload'])
                    await apply()
                  },
                  success: () => {
                    done()
                    return { text: '已新建标签并添加到素材' }
                  },
                })
              }}
            />
          )}
          {popup.kind === 'project' && (
            <ProjectPicker
              onChoose={(target) => {
                const run = projectPlacement(account, targets, target)
                let canvas = ''
                void actions.execute({
                  retryRejected: true,
                  run: async () => {
                    canvas = await run()
                  },
                  success: () => ({
                    text: `已放入 ${targets.length} 项，在画布右侧横排摆开`,
                    action: {
                      label: '打开画布',
                      run: () => {
                        window.location.assign(
                          `/creative/canvases/${encodeURIComponent(canvas)}`,
                        )
                      },
                    },
                  }),
                })
                setPopup(null)
              }}
            />
          )}
        </ActionPopover>
      )}
      {actions.toast && (
        <div className="al-toast" role="status">
          <span>{actions.toast.text}</span>
          {actions.toast.action && (
            <button
              type="button"
              disabled={actions.busy}
              onClick={actions.toast.action.run}
            >
              {actions.toast.action.label}
            </button>
          )}
          <button
            type="button"
            aria-label="关闭提示"
            disabled={actions.blocked}
            onClick={() => actions.setToast(null)}
          >
            <X size={13} />
          </button>
        </div>
      )}
    </div>
  )
}
