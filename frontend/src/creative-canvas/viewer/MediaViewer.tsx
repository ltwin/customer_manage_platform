import { useEffect, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import {
  AudioLines,
  ChevronLeft,
  ChevronRight,
  ExternalLink,
  Globe,
  X,
} from 'lucide-react'
import type { Content } from '../editor/api.ts'
import { useFocusTrap } from '../../components/useFocusTrap'
import { useMediaURL } from '../editor/MediaView.tsx'
import { contentText } from '../editor/content.ts'
import { clampPan, zoomAt, wheelZoom, type Zoom } from './zoom.ts'
import './viewer.css'

export type ViewerItem = {
  id: string
  kind: string
  title: string
  content?: Content
  unavailable?: boolean
}
/** 通用查看外框；宿主负责列表、详情读取、信息面板与动作。 */
export default function MediaViewer({
  item,
  index,
  count,
  panel,
  overlays,
  blocked,
  loading,
  error,
  onRetry,
  onStep,
  onClose,
  onRate,
  label = '查看',
}: {
  item: ViewerItem
  index: number
  count: number
  panel: ReactNode
  overlays?: ReactNode
  blocked: boolean
  loading: boolean
  error: string
  onRetry: () => void
  onStep: (delta: -1 | 1) => void
  onClose: () => void
  onRate: (rating: number | null) => void
  label?: string
}) {
  const ref = useFocusTrap<HTMLDivElement>(true, onClose, !blocked)
  const media = useRef<HTMLMediaElement | null>(null)
  // 背景媒体不与浮窗同时播放；卸载当前项的原生媒体会停止播放。
  useEffect(() => {
    document
      .querySelectorAll<HTMLMediaElement>(
        '.cc-node video, .cc-node audio, .al-tile video, .cl-tile video',
      )
      .forEach((el) => el.pause())
  }, [])
  return createPortal(
    <div
      className="viewer-back"
      onClick={(e) => {
        if (e.target === e.currentTarget && !blocked) onClose()
      }}
    >
      <div
        className="viewer"
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-label={`${label}：${item.title}`}
        tabIndex={-1}
        onKeyDown={(e) => {
          const target = e.target instanceof Element ? e.target : null
          if (
            target?.closest('[role="dialog"], [role="alertdialog"]') !==
            ref.current
          )
            return
          if (e.key === 'Escape' || e.key === 'Tab') return // 顶层 focus trap 管理嵌套弹层。
          e.stopPropagation()
          if (
            e.key === ' ' &&
            target?.closest('button') &&
            !target.closest('.v-close')
          )
            return
          if (
            e.nativeEvent.isComposing ||
            target?.closest(
              'input, textarea, select, [contenteditable], [role="slider"]',
            )
          )
            return
          if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
            if (target?.closest('video, audio')) return
            e.preventDefault()
            if (!blocked) onStep(e.key === 'ArrowRight' ? 1 : -1)
          } else if (e.key === ' ') {
            e.preventDefault()
            if (media.current) {
              if (media.current.paused)
                void media.current.play().catch(() => {})
              else media.current.pause()
            } else if (!blocked) onClose()
          } else if (
            !blocked &&
            !e.metaKey &&
            !e.ctrlKey &&
            !e.altKey &&
            /^[0-5]$/.test(e.key)
          ) {
            e.preventDefault()
            onRate(Number(e.key) || null)
          }
        }}
      >
        <div className="v-stage" data-kind={item.kind}>
          {error ? (
            <div className="v-state" role="alert">
              <p>{error}</p>
              <button className="btn" onClick={onRetry}>
                重试
              </button>
            </div>
          ) : loading ? (
            <div className="v-state" role="status">
              正在读取素材…
            </div>
          ) : item.unavailable ? (
            <div className="v-state" role="status">
              素材当前不可用
            </div>
          ) : (
            <MediaStage
              key={`${item.id}:${item.content?.id ?? ''}`}
              item={item}
              mediaRef={(el) => {
                media.current = el
              }}
            />
          )}
          <button
            className="v-nav prev"
            aria-label="上一项"
            disabled={index === 0 || blocked}
            onClick={() => onStep(-1)}
          >
            <ChevronLeft size={20} />
          </button>
          <button
            className="v-nav next"
            aria-label="下一项"
            disabled={index === count - 1 || blocked}
            onClick={() => onStep(1)}
          >
            <ChevronRight size={20} />
          </button>
          <span className="v-count">
            {index + 1} / {count}
          </span>
        </div>
        <aside className="v-panel" aria-label="素材信息">
          {panel}
        </aside>
        <button
          className="v-close al-icon-btn"
          aria-label="关闭查看"
          data-autofocus
          disabled={blocked}
          onClick={onClose}
        >
          <X size={18} />
        </button>
        {overlays}
      </div>
    </div>,
    document.querySelector('.cc-workspace') ?? document.body,
  )
}

function MediaStage({
  item,
  mediaRef,
}: {
  item: ViewerItem
  mediaRef: (el: HTMLMediaElement | null) => void
}) {
  const isMedia = ['image', 'video', 'audio'].includes(item.kind)
  // 浮窗用于看清细节，图片也取原图；列表与画布仍用展示图。
  const ticket = useMediaURL(
    item.content?.id,
    item.content?.media,
    'original',
    isMedia,
  )
  const [failed, setFailed] = useState(false)
  const text = contentText(item.content?.payload)
  if (isMedia) {
    if (ticket.error || failed)
      return (
        <div className="v-state" role="alert">
          <p>{ticket.error || '媒体加载失败'}</p>
          <button
            className="btn"
            onClick={() => {
              setFailed(false)
              ticket.retry()
            }}
          >
            重试媒体
          </button>
        </div>
      )
    if (!ticket.url)
      return (
        <div className="v-state" role="status">
          正在加载媒体…
        </div>
      )
    if (item.kind === 'image')
      return (
        <ImageStage
          key={ticket.url}
          url={ticket.url}
          title={item.title}
          onError={() => setFailed(true)}
        />
      )
    return (
      <>
        {item.kind === 'video' && (
          <div className="v-backdrop" aria-hidden="true">
            <video
              src={ticket.url}
              muted
              playsInline
              preload="metadata"
              tabIndex={-1}
            />
          </div>
        )}
        <div className={`v-media v-${item.kind}`}>
          {item.kind === 'video' ? (
            <video
              ref={mediaRef}
              tabIndex={0}
              src={ticket.url}
              aria-label={item.title}
              controls
              playsInline
              preload="metadata"
              onError={() => setFailed(true)}
            />
          ) : (
            <div className="v-audio-player">
              <AudioLines size={64} strokeWidth={1} />
              <strong>{item.title}</strong>
              <audio
                ref={mediaRef}
                tabIndex={0}
                src={ticket.url}
                aria-label={item.title}
                controls
                preload="metadata"
                onError={() => setFailed(true)}
              />
            </div>
          )}
        </div>
      </>
    )
  }
  if (item.kind === 'text')
    return (
      <article className="v-media v-text">
        <h2>{item.title}</h2>
        <p>{text}</p>
      </article>
    )
  let url: URL | null = null
  try {
    const candidate = new URL(text)
    if (
      ['http:', 'https:'].includes(candidate.protocol) &&
      !candidate.username &&
      !candidate.password
    )
      url = candidate
  } catch {
    /* 无效网址不创建可点击链接。 */
  }
  return (
    <div className="v-media v-link">
      <div className="v-link-blank">
        <Globe size={44} />
      </div>
      <div>
        <strong>{item.title}</strong>
        <span>{url?.hostname ?? '网址不可用'}</span>
        <p>{text}</p>
        {url && (
          <a
            className="btn btn-primary"
            href={url.href}
            target="_blank"
            rel="noopener noreferrer"
          >
            <ExternalLink size={14} />
            打开网址
          </a>
        )}
      </div>
    </div>
  )
}

function ImageStage({
  url,
  title,
  onError,
}: {
  url: string
  title: string
  onError: () => void
}) {
  const image = useRef<HTMLImageElement>(null)
  const surface = useRef<HTMLDivElement>(null)
  const [zoom, setZoom] = useState<Zoom>({ s: 1, x: 0, y: 0 })
  const current = useRef(zoom)
  const drag = useRef<{ pointer: number; x: number; y: number } | null>(null)
  const [panning, setPanning] = useState(false)
  const bounds = () => ({
    width: surface.current?.clientWidth ?? 0,
    height: surface.current?.clientHeight ?? 0,
    imageWidth: image.current?.offsetWidth ?? 0,
    imageHeight: image.current?.offsetHeight ?? 0,
  })
  function paint(next: Zoom) {
    current.current = next
    setZoom(next)
  }
  useEffect(() => {
    const el = surface.current
    if (!el) return
    const at = (s: number, x: number, y: number) => {
      const r = el.getBoundingClientRect()
      paint(
        zoomAt(
          current.current,
          s,
          { x: x - r.left - r.width / 2, y: y - r.top - r.height / 2 },
          bounds(),
        ),
      )
    }
    const wheel = (e: WheelEvent) => {
      e.preventDefault()
      const factor = wheelZoom(e)
      if (factor !== null) at(current.current.s * factor, e.clientX, e.clientY)
      else
        paint(
          clampPan(
            {
              ...current.current,
              x: current.current.x - e.deltaX,
              y: current.current.y - e.deltaY,
            },
            bounds(),
          ),
        )
    }
    let base = 1
    const gestureStart = (e: Event) => {
      e.preventDefault()
      base = current.current.s
    }
    const gestureChange = (e: Event) => {
      e.preventDefault()
      if (
        'scale' in e &&
        typeof e.scale === 'number' &&
        'clientX' in e &&
        typeof e.clientX === 'number' &&
        'clientY' in e &&
        typeof e.clientY === 'number'
      )
        at(base * e.scale, e.clientX, e.clientY)
    }
    el.addEventListener('wheel', wheel, { passive: false })
    el.addEventListener('gesturestart', gestureStart, { passive: false })
    el.addEventListener('gesturechange', gestureChange, { passive: false })
    const resize = new ResizeObserver(() =>
      paint(clampPan(current.current, bounds())),
    )
    resize.observe(el)
    return () => {
      resize.disconnect()
      el.removeEventListener('wheel', wheel)
      el.removeEventListener('gesturestart', gestureStart)
      el.removeEventListener('gesturechange', gestureChange)
    }
  }, [])
  return (
    <>
      <div
        className="v-backdrop"
        style={{ backgroundImage: `url(${JSON.stringify(url)})` }}
        aria-hidden="true"
      />
      <div
        className={`v-media v-image${zoom.s > 1 ? ' zoomed' : ''}${panning ? ' panning' : ''}`}
        ref={surface}
        onPointerDown={(e) => {
          if (e.button !== 0 || current.current.s === 1) return
          e.preventDefault()
          e.currentTarget.setPointerCapture(e.pointerId)
          drag.current = {
            pointer: e.pointerId,
            x: e.clientX - current.current.x,
            y: e.clientY - current.current.y,
          }
          setPanning(true)
        }}
        onPointerMove={(e) => {
          if (drag.current?.pointer === e.pointerId)
            paint(
              clampPan(
                {
                  ...current.current,
                  x: e.clientX - drag.current.x,
                  y: e.clientY - drag.current.y,
                },
                bounds(),
              ),
            )
        }}
        onPointerUp={() => {
          drag.current = null
          setPanning(false)
        }}
        onPointerCancel={() => {
          drag.current = null
          setPanning(false)
        }}
      >
        <img
          ref={image}
          src={url}
          alt={title}
          draggable={false}
          onError={onError}
          style={{
            transform: `translate(${zoom.x}px, ${zoom.y}px) scale(${zoom.s})`,
          }}
        />
      </div>
      {zoom.s > 1 && (
        <button
          className="v-zoom"
          aria-label="还原缩放"
          onClick={() => paint({ s: 1, x: 0, y: 0 })}
        >
          {Math.round(zoom.s * 100)}% · 还原
        </button>
      )}
    </>
  )
}
