import { useState } from 'react'
import { Folder, Plus, X } from 'lucide-react'
import type { components } from '../../api/schema'
import * as api from '../editor/api.ts'
import { downloadMedia } from '../editor/MediaView.tsx'
import ConfirmDialog from '../../components/ConfirmDialog'
import { StarRating } from '../editor/StarRating.tsx'
import ActionPopover from './ActionPopover.tsx'
import type { Point } from './popover.ts'
import ProjectPicker from './ProjectPicker.tsx'
import TagAssigner from './TagAssigner.tsx'
import { operation, projectPlacement } from './operations.ts'
import type { useActions } from './useActions.ts'
import type { Action } from './BatchBar.tsx'
import type { LibraryCatalog } from './useLibrary.ts'

type Popup = {
  kind: 'group' | 'tag' | 'rate' | 'project'
  at: Point
  assets: api.Asset[]
}
type Confirmation = {
  title: string
  body: string
  label?: string
  run: () => void
}
/** 独立页与查看浮窗共用同一组保存、整理、放入与恢复动作。 */
export function useOrganization({
  account,
  catalog,
  actions,
  actual,
  onTrash,
  onPurge,
}: {
  account: string
  catalog: LibraryCatalog | null
  actions: ReturnType<typeof useActions>
  actual: (asset: api.Asset) => api.Asset
  onTrash?: () => void
  onPurge?: (ids: string[]) => void
}) {
  const [popup, setPopup] = useState<Popup | null>(null)
  const [confirm, setConfirm] = useState<Confirmation | null>(null)
  const [groupDraft, setGroupDraft] = useState('')
  const blocked = actions.blocked
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
  function action(kind: Action, at: Point, assets: api.Asset[]) {
    if (kind === 'restore' || kind === 'purge') {
      if (
        actions.isBlocked() ||
        !assets.length ||
        assets.length > 100 ||
        assets.some((a) => !a.deleted_at)
      )
        return
      const run = () =>
        void actions.execute({
          before: kind === 'restore' ? assets : undefined,
          optimistic:
            kind === 'restore'
              ? assets.map((a) => ({
                  ...a,
                  deleted_at: null,
                  purge_after: null,
                }))
              : undefined,
          run: operation(account, `/assets/batch-${kind}`, {
            assets: assets.map((a) => ({
              asset_id: a.id,
              expected_revision: a.revision,
            })),
          } satisfies components['schemas']['CreativeAssetBatchPayload']),
          success: () => {
            if (kind === 'purge') onPurge?.(assets.map((a) => a.id))
            onTrash?.()
            return {
              text: kind === 'restore' ? '已恢复素材' : '已永久删除素材',
            }
          },
        })
      if (kind === 'restore') run()
      else
        setConfirm({
          title: `永久删除 ${assets.length} 项？`,
          body: '画布里已有的节点不受影响，此操作不可撤销。',
          run,
        })
      return
    }
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
          onTrash?.()
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
  function restore(asset: api.Asset) {
    if (actions.blocked || !asset.deleted_at) return
    void actions.execute({
      before: [asset],
      optimistic: [{ ...asset, deleted_at: null, purge_after: null }],
      run: operation(
        account,
        `/assets/${encodeURIComponent(asset.id)}/restore`,
        {
          expected_revision: asset.revision,
        } satisfies components['schemas']['CreativeAssetStatePayload'],
      ),
      success: () => ({ text: '已恢复素材' }),
    })
  }
  const targets = popup?.assets.map(actual) ?? []
  const overlays = (
    <>
      {confirm && (
        <ConfirmDialog
          title={confirm.title}
          body={confirm.body}
          confirmLabel={confirm.label}
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
    </>
  )
  return {
    action,
    metadata,
    organize,
    restore,
    overlays,
    popup,
    confirm,
    setPopup,
    setConfirm,
  }
}
