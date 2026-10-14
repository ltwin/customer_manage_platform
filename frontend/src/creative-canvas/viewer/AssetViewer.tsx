import { useEffect, useState } from 'react'
import * as api from '../editor/api.ts'
import { errorMessage } from '../editor/queue.ts'
import { useActions } from '../assets/useActions.ts'
import { useOrganization } from '../assets/useOrganization.tsx'
import type { LibraryCatalog } from '../assets/useLibrary.ts'
import MediaViewer from './MediaViewer.tsx'
import AssetInfoPanel from './AssetInfoPanel.tsx'
import '../assets/organize.css'

/** 列表在打开时冻结顺序，移到回收站后仍能在原位置直接恢复。 */
export default function AssetViewer({
  items,
  initialID,
  catalog,
  actions,
  organization,
  refresh,
  onClose,
  blocked = false,
  onPlace,
  label,
}: {
  items: api.Asset[]
  initialID: string
  catalog: LibraryCatalog | null
  actions: ReturnType<typeof useActions>
  organization: ReturnType<typeof useOrganization>
  refresh: number
  onClose: () => void
  blocked?: boolean
  onPlace?: (asset: api.Asset) => void
  label?: string
}) {
  const [index, setIndex] = useState(() =>
    Math.max(
      0,
      items.findIndex((a) => a.id === initialID),
    ),
  )
  const seed = items[index]!
  // 只在这次浮窗内缓存最近读过的详情；保存刷新和关闭都会使缓存失效。
  const [details, setDetails] = useState<{
    refresh: number
    items: Map<string, { asset?: api.Asset; error: string; readAt: number }>
  }>(() => ({ refresh, items: new Map() }))
  const cached = details.items.get(seed.id)
  const loaded =
    cached && Date.now() - cached.readAt < 30_000 ? cached : undefined
  useEffect(() => {
    if (loaded && details.refresh === refresh) return
    const controller = new AbortController()
    function remember(value: { asset?: api.Asset; error: string }) {
      if (controller.signal.aborted) return
      setDetails((old) => {
        const items = old.refresh === refresh ? new Map(old.items) : new Map()
        items.delete(seed.id)
        items.set(seed.id, { ...value, readAt: Date.now() })
        if (items.size > 20) items.delete(items.keys().next().value!)
        return { refresh, items }
      })
    }
    void api
      .read<api.Asset>(
        `/assets/${encodeURIComponent(seed.id)}`,
        controller.signal,
      )
      .then(async (asset) => {
        // 兼容仍返回摘要的旧投影，完整阅读内容只在打开当前项时读取。
        if (asset.content?.truncated && !asset.unavailable) {
          const content = await api.read<api.Content>(
            `/content-revisions/${encodeURIComponent(asset.content_revision_id)}`,
            controller.signal,
          )
          asset = { ...asset, content }
        }
        remember({ asset, error: '' })
      })
      .catch((e: unknown) => {
        remember({ error: errorMessage(e) })
      })
    return () => controller.abort()
  }, [seed.id, refresh, loaded, details.refresh])
  const full = loaded?.asset ?? seed
  const override = actions.overrides.get(seed.id)
  const asset =
    override && BigInt(override.revision) >= BigInt(full.revision)
      ? override
      : full
  const locked = actions.blocked
  const editingBlocked = blocked || locked || !loaded?.asset || !!loaded.error
  return (
    <MediaViewer
      item={asset}
      index={index}
      count={items.length}
      blocked={locked}
      loading={!loaded}
      error={loaded?.error ?? ''}
      onRetry={() =>
        setDetails((old) => {
          const items = new Map(old.items)
          items.delete(seed.id)
          return { ...old, items }
        })
      }
      onStep={(delta) => {
        if (!locked)
          setIndex((i) => Math.max(0, Math.min(items.length - 1, i + delta)))
      }}
      onClose={() => {
        if (!actions.isBlocked()) onClose()
      }}
      onRate={(value) => {
        if (!editingBlocked && !asset.deleted_at) actions.rate([asset], value)
      }}
      label={label}
      panel={
        <AssetInfoPanel
          asset={asset}
          catalog={catalog}
          organization={organization}
          blocked={editingBlocked}
          onRate={(value) => actions.rate([asset], value)}
          onPlace={
            onPlace
              ? () => {
                  if (!blocked && !actions.isBlocked()) onPlace(asset)
                }
              : undefined
          }
        />
      }
      overlays={organization.overlays}
    />
  )
}
