import { useState } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'
import { useFocusTrap } from '../../components/useFocusTrap'
import * as api from '../editor/api.ts'
import AssetViewer from '../viewer/AssetViewer.tsx'
import AssetBrowser from './AssetBrowser.tsx'
import LibraryTree from './LibraryTree.tsx'
import { blank, enter, type LibraryQuery, type Prefs } from './query.ts'
import { useActions } from './useActions.ts'
import { useAssetList, useCatalog } from './useLibrary.ts'
import { useOrganization } from './useOrganization.tsx'
import '../editor/library.css'
import './assets.css'
import './organize.css'

const PREFS: Prefs = { layout: 'grid', size: 140, inspector: false }

/**
 * 从资产库挑素材引用（助手、首页参考图）：和资产库页同一套资料库树、浏览区和卡片，
 * 只读；点卡片勾选，放大按钮或双击用快速查看看大图，确认后一次交回勾选的素材。
 * 不收进回收站的、当前无权展示的和 accept 不认的素材。
 */
export default function LibraryPicker({
  title,
  note,
  confirm = '引用',
  kind = '',
  max,
  accept,
  onPick,
  onClose,
}: {
  title: string
  note: string
  confirm?: string
  /** 只浏览一种类型（首页参考图只要图片）。 */
  kind?: LibraryQuery['kind']
  max?: number
  accept: (asset: api.Asset) => boolean
  onPick: (assets: api.Asset[]) => void
  onClose: () => void
}) {
  const [query, setQuery] = useState<LibraryQuery>(() => ({ ...blank(), kind }))
  const { catalog, error: catalogError } = useCatalog(0)
  const [refresh, setRefresh] = useState(0)
  const raw = useAssetList(query, refresh)
  const items = (raw.page?.items ?? []).filter(
    (a) => !a.deleted_at && !a.unavailable && accept(a),
  )
  const list = { ...raw, page: raw.page ? { ...raw.page, items } : null }
  // 换分组后勾选的仍然算数，按素材记下来，不按当前页。
  const [picked, setPicked] = useState<api.Asset[]>([])
  // 快速查看只看不改：和资产库页同一个浮窗，编辑一律锁住。
  const [viewer, setViewer] = useState<{
    items: api.Asset[]
    initialID: string
  } | null>(null)
  const account = api.currentAccount() ?? ''
  const actions = useActions(account, () => setRefresh((n) => n + 1))
  const organization = useOrganization({
    account,
    catalog,
    actions,
    actual: (a) => a,
  })
  const [narrow] = useState(() => window.innerWidth <= 760)
  const ref = useFocusTrap<HTMLDivElement>(true, onClose, !viewer)
  const over = max !== undefined && picked.length > max

  function go(next: LibraryQuery) {
    // 回收站里的不能引用，不进。
    if (next.view === 'trash') return
    setQuery(kind ? { ...next, kind } : next)
  }
  const isPicked = (id: string) => picked.some((a) => a.id === id)
  function toggle(id: string | null) {
    const asset = items.find((a) => a.id === id)
    if (!asset) return
    setPicked(
      isPicked(asset.id)
        ? picked.filter((a) => a.id !== asset.id)
        : [...picked, asset],
    )
  }
  const host = document.querySelector('.cc-workspace') ?? document.body
  return createPortal(
    <div className="overlay open cl-picker-overlay">
      <div
        ref={ref}
        className="cl-drawer is-wide is-pick cl-picker"
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
      >
        <header className="cl-head">
          <h2>{title}</h2>
          <span className="cl-grow" />
          <button
            type="button"
            className="cl-icon"
            aria-label="关闭"
            onClick={onClose}
          >
            <X size={16} />
          </button>
        </header>
        <div className="cl-body">
          {!narrow && (
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
              onDropAssets={() => {}}
              onDeleteGroup={() => {}}
              onSaveGroup={() => {}}
              onTagCommand={() => {}}
              blocked
            />
          )}
          <AssetBrowser
            drawer
            views={narrow}
            query={query}
            catalog={catalog}
            list={list}
            prefs={PREFS}
            selected={picked.map((a) => a.id)}
            importBlocked
            onImportFiles={() => {}}
            onQuery={go}
            onView={(v) => go(enter(query, v))}
            onGroup={(id) => go(enter(query, 'all', id))}
            onPrefs={() => {}}
            onSelect={toggle}
            onBox={(ids) =>
              setPicked([
                ...picked.filter((a) => !ids.includes(a.id)),
                ...items.filter((a) => ids.includes(a.id)),
              ])
            }
            onMenu={() => {}}
            onOpen={(asset) => setViewer({ items, initialID: asset.id })}
            onRetry={() => setRefresh((n) => n + 1)}
            onCardDrag={(_, e) => e.preventDefault()}
          />
        </div>
        <footer className="cl-foot is-task">
          <span>
            {over
              ? `最多选 ${max} 项`
              : picked.length
                ? `已选 ${picked.length} 项`
                : note}
          </span>
          {picked.length > 0 && (
            <button type="button" className="btn" onClick={() => setPicked([])}>
              清空
            </button>
          )}
          <span className="cl-grow" />
          <button type="button" className="btn" onClick={onClose}>
            取消
          </button>
          <button
            type="button"
            className="btn btn-primary"
            disabled={!picked.length || over}
            onClick={() => onPick(picked)}
          >
            {confirm}
            {picked.length ? ` ${picked.length} 项` : ''}
          </button>
        </footer>
      </div>
      {viewer && (
        <AssetViewer
          {...viewer}
          catalog={catalog}
          actions={actions}
          organization={organization}
          refresh={refresh}
          blocked
          label="快速查看"
          onClose={() => setViewer(null)}
          foot={(asset) => (
            <button
              type="button"
              className={`btn wide${isPicked(asset.id) ? '' : ' btn-primary'}`}
              onClick={() => toggle(asset.id)}
            >
              {isPicked(asset.id) ? '取消选用' : '选用'}
            </button>
          )}
        />
      )}
    </div>,
    host,
  )
}
