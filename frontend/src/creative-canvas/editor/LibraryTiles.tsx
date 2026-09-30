import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Check, Ellipsis, Heart, Plus } from 'lucide-react'
import * as api from './api.ts'
import { MediaFigure } from './MediaView.tsx'
import { isMediaKind } from './media.ts'
import { contentText } from './content.ts'
import { PREVIEW, kindLabel, tileRatio } from './libraryState.ts'
import { errorMessage } from './queue.ts'

export type TileMode = 'browse' | 'fill' | 'pick'
export type TileProps = {
  asset: api.Asset
  selected: boolean
  mode: TileMode
  used: number
  square?: boolean
  canPlace: boolean
  draggable: boolean
  onClick: (a: api.Asset, e: React.MouseEvent) => void
  onToggle: (a: api.Asset, range: boolean) => void
  onPlace: (a: api.Asset) => void
  onMenu: (a: api.Asset, at: { x: number; y: number }) => void
  onLocate: (a: api.Asset) => void
  onDragStart: (a: api.Asset, e: React.DragEvent) => void
  onKey: (a: api.Asset, e: React.KeyboardEvent<HTMLElement>) => void
  onPeek: (a: api.Asset | null, el?: HTMLElement) => void
}
export function Tile(p: TileProps) {
  const a = p.asset
  const media = isMediaKind(a.kind)
  return (
    <div
      className={`cl-tile cl-tile-${media ? 'media' : 'text'} ${p.selected ? 'is-on' : ''}`}
      data-asset={a.id}
      data-kind={a.kind}
      tabIndex={0}
      role="button"
      aria-label={a.title}
      aria-pressed={p.mode !== 'fill' ? p.selected : undefined}
      draggable={p.draggable}
      style={!p.square && media ? { aspectRatio: `1 / ${tileRatio(a)}` } : undefined}
      onClick={(e) => p.onClick(a, e)}
      onKeyDown={(e) => p.onKey(a, e)}
      onContextMenu={(e) => {
        if (p.mode !== 'browse') return
        e.preventDefault()
        p.onMenu(a, { x: e.clientX, y: e.clientY })
      }}
      onDragStart={(e) => {
        p.onPeek(null)
        p.onDragStart(a, e)
      }}
      onPointerEnter={(e) => p.onPeek(a, e.currentTarget)}
      onPointerLeave={() => p.onPeek(null)}
    >
      {media ? (
        a.unavailable || !a.content ? (
          <p className="cl-tile-note">{a.unavailable ? '当前无权展示' : ''}</p>
        ) : (
          <MediaFigure content={a.content} kind={a.kind} title={a.title} className="cl-media" />
        )
      ) : (
        <>
          <b>{a.title}</b>
          <span className="cl-tile-body">
            {a.unavailable ? '当前无权展示' : contentText(a.content?.payload)}
          </span>
        </>
      )}
      {media && <span className="cl-tile-name">{a.title}</span>}
      {a.kind === 'video' || a.kind === 'audio' || a.kind === 'link' ? (
        <span className="cl-tile-kind">{kindLabel[a.kind]}</span>
      ) : null}
      {a.is_favorite && (
        <span className="cl-tile-fav" aria-label="最爱">
          <Heart size={12} />
        </span>
      )}
      {p.used > 0 && (
        <button
          type="button"
          className="cl-tile-used"
          aria-label={`已在画布上，定位：${a.title}`}
          title="已在画布上，点击定位"
          onClick={(e) => {
            e.stopPropagation()
            p.onLocate(a)
          }}
        >
          <Check size={11} />
          {p.used > 1 ? p.used : ''}
        </button>
      )}
      {p.mode !== 'fill' && (
        <button
          type="button"
          className="cl-tile-check"
          aria-label={`选择资产：${a.title}`}
          aria-pressed={p.selected}
          onClick={(e) => {
            e.stopPropagation()
            p.onToggle(a, e.shiftKey)
          }}
        >
          <Check size={13} />
        </button>
      )}
      {p.mode === 'browse' && (
        <span className="cl-tile-actions">
          <button
            type="button"
            aria-label={`资产操作：${a.title}`}
            onClick={(e) => {
              e.stopPropagation()
              const r = e.currentTarget.getBoundingClientRect()
              p.onMenu(a, { x: r.left, y: r.bottom + 4 })
            }}
          >
            <Ellipsis size={13} />
          </button>
          {p.canPlace && (
            <button
              type="button"
              className="is-primary"
              aria-label={`放入画布：${a.title}`}
              disabled={a.unavailable}
              onClick={(e) => {
                e.stopPropagation()
                p.onPlace(a)
              }}
            >
              <Plus size={13} />
            </button>
          )}
        </span>
      )}
    </div>
  )
}

// 瀑布流：按累计高度分到最矮的一列，保持从左到右、从上到下的阅读顺序。
export function Masonry({
  items,
  columns,
  tile,
}: {
  items: api.Asset[]
  columns: number
  tile: (a: api.Asset) => React.ReactNode
}) {
  const cols: api.Asset[][] = Array.from({ length: columns }, () => [])
  const heights = Array.from({ length: columns }, () => 0)
  for (const a of items) {
    const i = heights.indexOf(Math.min(...heights))
    cols[i]!.push(a)
    heights[i]! += tileRatio(a)
  }
  return (
    <div className="cl-masonry">
      {cols.map((c, i) => (
        <div key={i} className="cl-masonry-col">
          {c.map(tile)}
        </div>
      ))}
    </div>
  )
}

// 平铺浏览用等大方格，一屏能扫过几个组；超出的最后一格给「+N」进组。
export function PreviewGrid({
  page,
  tile,
  onMore,
  label,
}: {
  page: api.AssetPage
  tile: (a: api.Asset) => React.ReactNode
  onMore: () => void
  label: string
}) {
  const total = Number(page.total_count)
  const more = total > PREVIEW
  const shown = page.items.slice(0, more ? PREVIEW - 1 : PREVIEW)
  const rest = page.items[PREVIEW - 1]
  return (
    <div className="cl-grid">
      {shown.map(tile)}
      {more && (
        <button
          type="button"
          className="cl-more-cell"
          aria-label={`查看「${label}」全部 ${total} 项`}
          onClick={onMore}
        >
          {rest?.content && rest.kind === 'image' && (
            <MediaFigure content={rest.content} kind="image" title="" className="cl-media" />
          )}
          <span>+{total - shown.length}</span>
        </button>
      )}
    </div>
  )
}

// 悬停放大：出现在抽屉右侧，不挡住正在看的那一列。
export function Peek({
  asset,
  anchor,
  meta,
}: {
  asset: api.Asset
  anchor: HTMLElement
  meta: string
}) {
  const ref = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    const el = ref.current
    const drawer = document.getElementById('creative-library')
    if (!el || !drawer) return
    const r = anchor.getBoundingClientRect()
    el.style.left = `${Math.min(drawer.getBoundingClientRect().right + 12, window.innerWidth - el.offsetWidth - 8)}px`
    el.style.top = `${Math.max(8, Math.min(r.top + r.height / 2 - el.offsetHeight / 2, window.innerHeight - el.offsetHeight - 8))}px`
  })
  return createPortal(
    <div ref={ref} className="cl-peek" aria-hidden="true">
      {isMediaKind(asset.kind) && asset.content ? (
        <MediaFigure content={asset.content} kind={asset.kind} title={asset.title} className="cl-media" />
      ) : (
        <div className="cl-peek-text">{contentText(asset.content?.payload)}</div>
      )}
      <div className="cl-peek-cap">
        <b>{asset.title}</b>
        <span>{meta}</span>
      </div>
    </div>,
    document.querySelector('.cc-workspace') ?? document.body,
  )
}

// 空格快速查看：左右键在当前列表里翻看，空格或 Esc 关闭并把焦点还给卡片。
export function QuickLook({
  asset,
  meta,
  canPlace,
  disabled,
  onClose,
  onStep,
  onPlace,
  onFavorite,
}: {
  asset: api.Asset
  meta: string
  canPlace: boolean
  disabled: boolean
  onClose: () => void
  onStep: (delta: 1 | -1) => void
  onPlace: () => void
  onFavorite: () => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  const [full, setFull] = useState<{ key: string; text?: string; error?: string } | null>(null)
  const key = `${asset.id}:${asset.content_revision_id}`
  const media = isMediaKind(asset.kind)
  useEffect(() => {
    ref.current?.focus()
  }, [key])
  useEffect(() => {
    if (media || asset.unavailable || !asset.content?.truncated) return
    const controller = new AbortController()
    void api
      .read<api.Content>(`/content-revisions/${asset.content_revision_id}`, controller.signal)
      .then((c) => setFull({ key, text: contentText(c.payload) }))
      .catch((e: unknown) => {
        if (!controller.signal.aborted) setFull({ key, error: errorMessage(e) })
      })
    return () => controller.abort()
  }, [asset, key, media])
  const loaded = full?.key === key ? full : null
  const text = asset.unavailable
    ? '当前无权展示此内容'
    : loaded?.error ||
      loaded?.text ||
      (asset.content?.truncated ? '正在读取内容…' : contentText(asset.content?.payload))
  return createPortal(
    <div
      ref={ref}
      className="cl-look"
      role="dialog"
      aria-modal="true"
      aria-label={`快速查看：${asset.title}`}
      tabIndex={-1}
      onClick={(e) => {
        if (!(e.target as HTMLElement).closest('figure')) onClose()
      }}
      onKeyDown={(e) => {
        e.stopPropagation()
        if (e.key === ' ' || e.key === 'Escape') {
          e.preventDefault()
          onClose()
        } else if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
          e.preventDefault()
          onStep(e.key === 'ArrowRight' ? 1 : -1)
        }
      }}
    >
      <figure>
        {media && asset.content && !asset.unavailable ? (
          <MediaFigure
            content={asset.content}
            kind={asset.kind}
            title={asset.title}
            controls
            prefer="original"
            className="cl-look-media"
          />
        ) : (
          <div className="cl-look-text">{text}</div>
        )}
        <figcaption>
          <b>{asset.title}</b>
          <span>{meta}</span>
          <span className="cl-grow" />
          <button type="button" className="btn" disabled={disabled} onClick={onFavorite}>
            <Heart size={14} />
            {asset.is_favorite ? '取消最爱' : '加入最爱'}
          </button>
          {canPlace && (
            <button
              type="button"
              className="btn btn-primary"
              disabled={disabled || asset.unavailable}
              onClick={onPlace}
            >
              <Plus size={14} />
              放入画布
            </button>
          )}
        </figcaption>
      </figure>
    </div>,
    document.querySelector('.cc-workspace') ?? document.body,
  )
}
