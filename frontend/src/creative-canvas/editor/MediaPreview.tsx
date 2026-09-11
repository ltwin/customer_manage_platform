import { useEffect, useRef } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'
import { useFocusTrap } from '../../components/useFocusTrap'
import type { Content } from './api.ts'
import { MediaFigure } from './MediaView.tsx'

// Full-viewport preview: media plus one close control, per the v5 contract.
export default function MediaPreview({
  content,
  kind,
  title,
  onClose,
}: {
  content: Content
  kind: string
  title: string
  onClose: () => void
}) {
  const ref = useFocusTrap<HTMLDivElement>(true, onClose)
  const pressedOutside = useRef(false)
  useEffect(() => {
    document
      .querySelectorAll<HTMLMediaElement>('.cc-node video, .cc-node audio')
      .forEach((el) => el.pause())
  }, [])
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
      onPointerDown={(e) => {
        pressedOutside.current = outside(e.target)
      }}
      onClick={(e) => {
        if (pressedOutside.current && outside(e.target)) onClose()
        pressedOutside.current = false
      }}
    >
      <button
        className="cc-icon-button cc-glass cc-media-preview-close"
        aria-label="关闭预览"
        onClick={onClose}
      >
        <X size={18} />
      </button>
      <div className="cc-media-preview-content">
        <MediaFigure
          content={content}
          kind={kind}
          title={title}
          controls
          prefer="original"
        />
      </div>
    </div>,
    document.querySelector('.cc-workspace') ?? document.body,
  )
}
