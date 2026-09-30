import { useState } from 'react'
import { createPortal } from 'react-dom'
import { FileText, X } from 'lucide-react'
import type { Content } from './api.ts'
import { contentText } from './content.ts'
import { MediaFigure } from './MediaView.tsx'

// 参考的预览独立于节点当前查看的版本，始终展示实际发送的主版本。
export default function GenerationReference({
  title,
  content,
  disabled,
  onRemove,
  onMention,
  label,
  updated = false,
}: {
  title: string
  content?: Content
  disabled: boolean
  onRemove?: () => void
  onMention?: () => void
  label?: string
  updated?: boolean
}) {
  const [peek, setPeek] = useState<{ x: number; y: number } | null>(null)
  const media = content && ['image', 'video', 'audio'].includes(content.kind)
  const open = (el: HTMLElement) => {
    const r = el.getBoundingClientRect()
    setPeek({
      x: Math.max(8, Math.min(r.left, window.innerWidth - 216)),
      y: r.top,
    })
  }
  return (
    <span
      className={`cc-generation-ref ${updated ? 'is-updated' : ''}`}
      onMouseEnter={(e) => open(e.currentTarget)}
      onMouseLeave={() => setPeek(null)}
      onFocus={(e) => open(e.currentTarget)}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget)) setPeek(null)
      }}
      onKeyDown={(e) => {
        if (e.key === 'Escape') setPeek(null)
      }}
    >
      <button
        type="button"
        className="cc-generation-ref-body"
        disabled={disabled && !!onMention}
        onMouseDown={(e) => {
          if (onMention) e.preventDefault()
        }}
        onClick={onMention}
        aria-label={`参考：${title}`}
      >
        {media ? (
          <MediaFigure content={content} kind={content.kind} title={title} />
        ) : (
          <FileText size={13} />
        )}
        {label && (
          <small className="cc-ref-no">
            {content?.kind === 'text' ? '文本' : label}
          </small>
        )}
        <span>{title}</span>
        {updated && <small className="cc-ref-updated">已更新</small>}
      </button>
      {onRemove && (
        <button
          type="button"
          aria-label={`移除参考：${title}`}
          disabled={disabled}
          onClick={() => {
            setPeek(null)
            onRemove()
          }}
        >
          <X size={11} />
        </button>
      )}
      {peek &&
        createPortal(
          <div
            className="cc-generation-ref-peek"
            role="tooltip"
            style={{
              left: peek.x,
              ...(peek.y > 310
                ? { bottom: window.innerHeight - peek.y + 8 }
                : { top: peek.y + 36 }),
            }}
          >
            {media ? (
              <MediaFigure
                content={content}
                kind={content.kind}
                title={title}
              />
            ) : (
              <p>{content ? contentText(content.payload) : '节点尚无内容'}</p>
            )}
            <div>
              <b>{title}</b>
              <span>
                {label
                  ? content?.kind === 'text'
                    ? '文本参考 · 引用处展开为原文'
                    : `发送时记为 [${label}]`
                  : '生成时使用的参考'}
              </span>
            </div>
          </div>,
          document.querySelector('.cc-workspace') ?? document.body,
        )}
    </span>
  )
}
