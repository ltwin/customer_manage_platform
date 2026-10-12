import { useEffect, useState } from 'react'
import * as api from '../editor/api.ts'
import { errorMessage } from '../editor/queue.ts'
import { useActions } from '../assets/useActions.ts'
import { useOrganization } from '../assets/useOrganization.tsx'
import { useCatalog, type LibraryCatalog } from '../assets/useLibrary.ts'
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
  const [detail, setDetail] = useState<{
    asset?: api.Asset
    id: string
    error: string
  } | null>(null)
  const [retry, setRetry] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
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
        if (!controller.signal.aborted)
          setDetail({ id: seed.id, asset, error: '' })
      })
      .catch((e: unknown) => {
        if (!controller.signal.aborted)
          setDetail({ id: seed.id, error: errorMessage(e) })
      })
    return () => controller.abort()
  }, [seed.id, refresh, retry])
  const loaded = detail?.id === seed.id ? detail : null
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
      onRetry={() => setRetry((n) => n + 1)}
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

/** 抽屉宿主：目录与写入生命周期独立，关闭前等待未决操作完成。 */
export function LibraryViewer({
  items,
  initialID,
  onClose,
  onChanged,
  blocked,
  onPlace,
}: {
  items: api.Asset[]
  initialID: string
  onClose: () => void
  onChanged: () => void
  blocked: boolean
  onPlace?: (asset: api.Asset) => void
}) {
  const account = api.currentAccount()!
  const [refresh, setRefresh] = useState(0)
  const reload = () => {
    setRefresh((n) => n + 1)
    onChanged()
  }
  const actions = useActions(account, reload)
  const { catalog } = useCatalog(refresh)
  const actual = (a: api.Asset) => {
    const override = actions.overrides.get(a.id)
    return override && BigInt(override.revision) >= BigInt(a.revision)
      ? override
      : a
  }
  const organization = useOrganization({ account, catalog, actions, actual })
  return (
    <AssetViewer
      items={items}
      initialID={initialID}
      catalog={catalog}
      actions={actions}
      organization={organization}
      refresh={refresh}
      onClose={onClose}
      blocked={blocked}
      onPlace={onPlace}
      label="快速查看"
    />
  )
}
