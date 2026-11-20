import { useState } from 'react'
import { Contact, FileText, X } from 'lucide-react'
import type { Content } from './api.ts'
import { characterOf } from '../assets/character.ts'
import { ReferenceContent, ReferencePreview } from './ReferencePreview.tsx'
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
  const [peek, setPeek] = useState<HTMLElement | null>(null)
  const card = characterOf(content)
  const media = content && ['image', 'video', 'audio'].includes(content.kind)
  const open = (el: HTMLElement) => setPeek(el)
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
        ) : card ? (
          <Contact size={13} />
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
      {peek && (
        <ReferencePreview
          anchor={peek}
          title={title}
          description={
            card ? `生成时展开 ${card.views.length} 张参考图`
              : label ? content?.kind === 'text'
                ? '文本参考 · 引用处展开为原文'
                : `发送时记为 [${label}]`
                : '生成时使用的参考'
          }
        >
          {content ? <ReferenceContent content={content} title={title} /> : <p>节点尚无内容</p>}
        </ReferencePreview>
      )}
    </span>
  )
}
