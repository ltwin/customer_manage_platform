import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { ChevronLeft, ChevronRight, X } from 'lucide-react'
import { useFocusTrap } from '../../components/useFocusTrap'
import * as api from './api.ts'
import type { Versions } from './generation.ts'
import { MediaFigure } from './MediaView.tsx'
import { errorMessage } from './queue.ts'

// 全屏版本浏览保持临时状态；只有「设为主版本」进入画布保存队列。
export default function MediaPreview({
  initialRevision,
  content,
  kind,
  title,
  canvasID,
  nodeID,
  dataRevision,
  selectedVersion,
  disabled,
  onSelectVersion,
  onClose,
}: {
  initialRevision?: string
  content: api.Content
  kind: string
  title: string
  canvasID: string
  nodeID: string
  dataRevision: string
  selectedVersion: string | null
  disabled: boolean
  onSelectVersion: (id: string) => void
  onClose: () => void
}) {
  const ref = useFocusTrap<HTMLDivElement>(true, onClose)
  const pressedOutside = useRef(false)
  const [versions, setVersions] = useState<Versions | null>(null)
  const [viewing, setViewing] = useState<string | null>(initialRevision ?? null)
  const [image, setImage] = useState<api.Content>(content)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [retry, setRetry] = useState(0)
  const items = [...(versions?.items ?? [])].sort((a, b) =>
    BigInt(a.version_no) < BigInt(b.version_no) ? -1 : 1,
  )
  const current = items.findIndex(
    (v) => v.content_revision_id === (viewing ?? content.id),
  )
  const version = items[current]
  const changeVersion = (step: number) => {
    if (items.length > 1) {
      const next =
        items[(Math.max(current, 0) + step + items.length) % items.length]!
      setViewing(next.content_revision_id)
    }
  }
  useEffect(() => {
    document
      .querySelectorAll<HTMLMediaElement>('.cc-node video,.cc-node audio')
      .forEach((el) => el.pause())
  }, [])
  useEffect(() => {
    const controller = new AbortController()
    void api
      .read<Versions>(
        `/canvases/${encodeURIComponent(canvasID)}/nodes/${encodeURIComponent(nodeID)}/versions`,
        controller.signal,
      )
      .then((v) => {
        if (!controller.signal.aborted) setVersions(v)
      })
      .catch((e) => {
        if (!controller.signal.aborted) setError(errorMessage(e))
      })
    return () => controller.abort()
  }, [canvasID, nodeID, dataRevision, retry])
  useEffect(() => {
    if (!viewing || viewing === content.id) {
      setImage(content)
      setLoading(false)
      return
    }
    const controller = new AbortController()
    setLoading(true)
    setError('')
    void api
      .read<api.Content>(
        `/content-revisions/${encodeURIComponent(viewing)}`,
        controller.signal,
      )
      .then((c) => {
        if (!controller.signal.aborted) {
          setImage(c)
          setLoading(false)
        }
      })
      .catch((e) => {
        if (!controller.signal.aborted) {
          setError(errorMessage(e))
          setLoading(false)
        }
      })
    return () => controller.abort()
  }, [viewing, content, retry])
  const outside = (target: EventTarget) =>
    target instanceof HTMLElement &&
    (target.classList.contains('cc-media-preview') ||
      target.classList.contains('cc-media-preview-content'))
  return createPortal(
    <div
      ref={ref}
      className="cc-media-preview"
      role="dialog"
      aria-modal="true"
      aria-label={`媒体预览：${title}`}
      tabIndex={-1}
      onKeyDown={(e) => {
        if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
          e.preventDefault()
          e.stopPropagation()
          changeVersion(e.key === 'ArrowLeft' ? -1 : 1)
        }
      }}
      onPointerDown={(e) => {
        pressedOutside.current = outside(e.target)
      }}
      onClick={(e) => {
        if (pressedOutside.current && outside(e.target)) onClose()
        pressedOutside.current = false
      }}
    >
      <button
        className="cc-icon-button cc-surface cc-media-preview-close"
        aria-label="关闭预览"
        onClick={onClose}
      >
        <X size={18} />
      </button>
      {items.length > 1 && (
        <>
          <button
            className="cc-icon-button cc-surface cc-media-preview-nav is-prev"
            aria-label="上一版"
            onClick={() => changeVersion(-1)}
          >
            <ChevronLeft size={24} />
          </button>
          <button
            className="cc-icon-button cc-surface cc-media-preview-nav is-next"
            aria-label="下一版"
            onClick={() => changeVersion(1)}
          >
            <ChevronRight size={24} />
          </button>
        </>
      )}
      <div className="cc-media-preview-content">
        {loading ? (
          <p role="status">正在读取版本…</p>
        ) : error ? (
          <div role="alert">
            {error}
            <button onClick={() => setRetry((n) => n + 1)}>重试</button>
          </div>
        ) : (
          <MediaFigure
            content={image}
            kind={kind}
            title={title}
            controls
            prefer="original"
          />
        )}
      </div>
      <div className="cc-media-preview-caption">
        <span>
          {title}
          {version && ` · 版本 ${version.version_no}/${items.length}`}
        </span>
        {version &&
          (version.id === selectedVersion ? (
            <span className="cc-main-state">主版本</span>
          ) : (
            <button
              disabled={disabled || loading || !!error}
              onClick={() => onSelectVersion(version.id)}
            >
              设为主版本
            </button>
          ))}
      </div>
    </div>,
    document.querySelector('.cc-workspace') ?? document.body,
  )
}
